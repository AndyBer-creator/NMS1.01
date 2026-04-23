package http

import (
	"context"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"go.uber.org/zap"
)

const (
	loginWindow          = 10 * time.Minute
	loginMaxAttemptsIP   = 30
	loginMaxAttemptsUser = 10
	loginLockoutDuration = 15 * time.Minute
)

type loginAttemptState struct {
	count       int
	firstFailAt time.Time
	lockedUntil time.Time
}

type loginLimiter struct {
	mu     sync.Mutex
	byIP   map[string]loginAttemptState
	byUser map[string]loginAttemptState
	lastGC time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{
		byIP:   make(map[string]loginAttemptState),
		byUser: make(map[string]loginAttemptState),
	}
}

var authLoginLimiter = newLoginLimiter()

func (h *Handlers) loginLimiterCheck(ip, user string, now time.Time) (bool, time.Duration) {
	if h == nil || h.repo == nil {
		return authLoginLimiter.check(ip, user, now)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ipAllowed, ipRetry, err := h.repo.LoginRateLimitCheck(ctx, "ip", ip, now, loginWindow, loginLockoutDuration, loginMaxAttemptsIP)
	if err != nil {
		h.logger.Warn("shared login limiter check failed for ip, fallback to local", zap.Error(err))
		return authLoginLimiter.check(ip, user, now)
	}
	if !ipAllowed {
		return false, ipRetry
	}
	userAllowed, userRetry, err := h.repo.LoginRateLimitCheck(ctx, "user", user, now, loginWindow, loginLockoutDuration, loginMaxAttemptsUser)
	if err != nil {
		h.logger.Warn("shared login limiter check failed for user, fallback to local", zap.Error(err))
		return authLoginLimiter.check(ip, user, now)
	}
	if !userAllowed {
		return false, userRetry
	}
	return true, 0
}

func (h *Handlers) loginLimiterFailure(ip, user string, now time.Time) {
	authLoginLimiter.onFailure(ip, user, now)
	if h == nil || h.repo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.repo.LoginRateLimitOnFailure(ctx, "ip", ip, now, loginWindow, loginLockoutDuration, loginMaxAttemptsIP); err != nil {
		h.logger.Warn("shared login limiter failure update failed for ip", zap.Error(err))
	}
	if err := h.repo.LoginRateLimitOnFailure(ctx, "user", user, now, loginWindow, loginLockoutDuration, loginMaxAttemptsUser); err != nil {
		h.logger.Warn("shared login limiter failure update failed for user", zap.Error(err))
	}
}

func (h *Handlers) loginLimiterSuccess(ip, user string) {
	authLoginLimiter.onSuccess(ip, user)
	if h == nil || h.repo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := h.repo.LoginRateLimitOnSuccess(ctx, ip, user); err != nil {
		h.logger.Warn("shared login limiter success reset failed", zap.Error(err))
	}
}

func clientIP(r *http.Request) string {
	if fromTrustedProxy(r) {
		if xff := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); xff != "" {
			parts := strings.Split(xff, ",")
			if len(parts) > 0 {
				return strings.TrimSpace(parts[0])
			}
		}
		if xrip := strings.TrimSpace(r.Header.Get("X-Real-IP")); xrip != "" {
			return xrip
		}
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}

func resetWindowIfExpired(st loginAttemptState, now time.Time) loginAttemptState {
	if st.firstFailAt.IsZero() || now.Sub(st.firstFailAt) > loginWindow {
		st.count = 0
		st.firstFailAt = time.Time{}
	}
	return st
}

func (l *loginLimiter) check(ip, user string, now time.Time) (allowed bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gcLocked(now)

	ipSt := resetWindowIfExpired(l.byIP[ip], now)
	userSt := resetWindowIfExpired(l.byUser[user], now)

	if now.Before(ipSt.lockedUntil) {
		return false, ipSt.lockedUntil.Sub(now)
	}
	if now.Before(userSt.lockedUntil) {
		return false, userSt.lockedUntil.Sub(now)
	}
	if ipSt.count >= loginMaxAttemptsIP {
		ipSt.lockedUntil = now.Add(loginLockoutDuration)
		l.byIP[ip] = ipSt
		return false, loginLockoutDuration
	}
	if userSt.count >= loginMaxAttemptsUser {
		userSt.lockedUntil = now.Add(loginLockoutDuration)
		l.byUser[user] = userSt
		return false, loginLockoutDuration
	}
	return true, 0
}

func (l *loginLimiter) onFailure(ip, user string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.gcLocked(now)

	ipSt := resetWindowIfExpired(l.byIP[ip], now)
	if ipSt.firstFailAt.IsZero() {
		ipSt.firstFailAt = now
	}
	ipSt.count++
	if ipSt.count >= loginMaxAttemptsIP {
		ipSt.lockedUntil = now.Add(loginLockoutDuration)
	}
	l.byIP[ip] = ipSt

	userSt := resetWindowIfExpired(l.byUser[user], now)
	if userSt.firstFailAt.IsZero() {
		userSt.firstFailAt = now
	}
	userSt.count++
	if userSt.count >= loginMaxAttemptsUser {
		userSt.lockedUntil = now.Add(loginLockoutDuration)
	}
	l.byUser[user] = userSt
}

func (l *loginLimiter) onSuccess(ip, user string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.byIP, ip)
	delete(l.byUser, user)
}

func (l *loginLimiter) gcLocked(now time.Time) {
	if !l.lastGC.IsZero() && now.Sub(l.lastGC) < time.Minute {
		return
	}
	cutoff := now.Add(-2 * loginWindow)
	for k, st := range l.byIP {
		if st.lockedUntil.Before(now) && (st.firstFailAt.IsZero() || st.firstFailAt.Before(cutoff)) {
			delete(l.byIP, k)
		}
	}
	for k, st := range l.byUser {
		if st.lockedUntil.Before(now) && (st.firstFailAt.IsZero() || st.firstFailAt.Before(cutoff)) {
			delete(l.byUser, k)
		}
	}
	l.lastGC = now
}
