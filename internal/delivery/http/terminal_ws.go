package http

import (
	"NMS1/internal/config"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"go.uber.org/zap"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const (
	defaultTerminalWSReadLimit = int64(64 * 1024)
	maxTerminalWSReadLimit     = int64(1024 * 1024)
	maxTerminalAuthFieldBytes  = 256
)

var terminalUpgrader = websocket.Upgrader{
	ReadBufferSize:  4098,
	WriteBufferSize: 4098,
	CheckOrigin:     terminalCheckOrigin,
}

func terminalCheckOrigin(r *http.Request) bool {
	// Legacy override (не рекомендуется): разрешить любые origin для старых reverse-proxy схем.
	if envBool("NMS_TERMINAL_ALLOW_INSECURE_ORIGIN") {
		return true
	}
	o := strings.TrimSpace(r.Header.Get("Origin"))
	if o == "" {
		// Требуем Origin по умолчанию для защиты от CSWSH.
		return false
	}
	ou, err := url.Parse(o)
	if err != nil {
		return false
	}
	if ou.Scheme != "http" && ou.Scheme != "https" {
		return false
	}
	reqScheme := "http"
	if isHTTPSRequest(r) {
		reqScheme = "https"
	}
	if !strings.EqualFold(ou.Scheme, reqScheme) {
		return false
	}
	return strings.EqualFold(ou.Host, canonicalRequestHost(r))
}

func terminalSSHHostKeyCallback() (ssh.HostKeyCallback, error) {
	if envBool("NMS_TERMINAL_ALLOW_INSECURE_HOSTKEY") {
		return ssh.InsecureIgnoreHostKey(), nil
	}
	knownHostsPath := strings.TrimSpace(config.EnvOrFile("NMS_TERMINAL_SSH_KNOWN_HOSTS"))
	if knownHostsPath == "" {
		return nil, errors.New("ssh host key verification is not configured")
	}
	cb, err := knownhosts.New(knownHostsPath)
	if err != nil {
		return nil, fmt.Errorf("known_hosts: %w", err)
	}
	return cb, nil
}

type terminalInitMsg struct {
	Type     string `json:"type"`
	Username string `json:"username"`
	Password string `json:"password"`
	Port     int    `json:"port"`
}

type terminalResizeMsg struct {
	Type string `json:"type"`
	Cols int    `json:"cols"`
	Rows int    `json:"rows"`
}

func validateTerminalInitMsg(init terminalInitMsg, kind string) error {
	if init.Type != "init" {
		return fmt.Errorf("expected init json")
	}
	if len(init.Username) > maxTerminalAuthFieldBytes {
		return fmt.Errorf("init username too long")
	}
	if len(init.Password) > maxTerminalAuthFieldBytes {
		return fmt.Errorf("init password too long")
	}
	// Explicitly reject absurd port values instead of silently defaulting.
	if init.Port < 0 || init.Port > 65535 {
		return fmt.Errorf("invalid port")
	}
	if kind == "ssh" && strings.TrimSpace(init.Username) == "" {
		return fmt.Errorf("ssh username required")
	}
	return nil
}

type terminalAckMsg struct {
	Type    string `json:"type"`
	Message string `json:"message,omitempty"`
}

type terminalWSClaims struct {
	User string `json:"u"`
	Role string `json:"r"`
	Dev  int    `json:"d"`
	Exp  int64  `json:"exp"`
}

func terminalKindFromQuery(_ *http.Request) string {
	// Web-terminal supports only SSH.
	return "ssh"
}

func deviceDialAddr(host string, port int) string {
	h := strings.TrimSpace(host)
	h = strings.TrimPrefix(h, "[")
	h = strings.TrimSuffix(h, "]")
	return net.JoinHostPort(h, strconv.Itoa(port))
}

func terminalDialTimeout() time.Duration {
	if v := strings.TrimSpace(os.Getenv("NMS_TERMINAL_DIAL_TIMEOUT")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 20 * time.Second
}

func terminalSessionDeadline() time.Time {
	if v := strings.TrimSpace(os.Getenv("NMS_TERMINAL_SESSION_MAX")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return time.Now().Add(d)
		}
	}
	return time.Now().Add(8 * time.Hour)
}

// terminalWSReadIdle — таймаут ReadMessage при отсутствии данных от браузера (не путать с dial).
func terminalWSReadIdle() time.Duration {
	if v := strings.TrimSpace(os.Getenv("NMS_TERMINAL_WS_READ_IDLE")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return 30 * time.Minute
}

// terminalWSReadLimit limits max incoming WS frame size to reduce memory abuse.
func terminalWSReadLimit() int64 {
	if v := strings.TrimSpace(os.Getenv("NMS_TERMINAL_WS_READ_LIMIT_BYTES")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			if n >= 1024 && n <= maxTerminalWSReadLimit {
				return n
			}
		}
	}
	return defaultTerminalWSReadLimit
}

func signTerminalWSToken(user string, rl role, deviceID int) (string, error) {
	claims := terminalWSClaims{
		User: user,
		Role: string(rl),
		Dev:  deviceID,
		Exp:  time.Now().Add(30 * time.Minute).Unix(),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	key := sessionSigningKey()
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(payload)
	sig := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}

func verifyTerminalWSToken(token string, deviceID int) *authUser {
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return nil
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil
	}
	wantSig, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil || len(wantSig) == 0 {
		return nil
	}
	key := sessionSigningKey()
	mac := hmac.New(sha256.New, key[:])
	_, _ = mac.Write(payload)
	got := mac.Sum(nil)
	if subtle.ConstantTimeCompare(got, wantSig) != 1 {
		return nil
	}
	var c terminalWSClaims
	if err := json.Unmarshal(payload, &c); err != nil {
		return nil
	}
	if c.Dev != deviceID || c.Exp < time.Now().Unix() {
		return nil
	}
	if c.Role != string(roleAdmin) {
		return nil
	}
	return &authUser{username: c.User, role: role(c.Role)}
}

func terminalTokenFromSubprotocol(r *http.Request) string {
	raw := strings.TrimSpace(r.Header.Get("Sec-WebSocket-Protocol"))
	if raw == "" {
		return ""
	}
	// Browser may send a comma-separated list; we consume only our auth protocol entry.
	const prefix = "nms-term-auth."
	for _, part := range strings.Split(raw, ",") {
		p := strings.TrimSpace(part)
		if strings.HasPrefix(p, prefix) {
			return strings.TrimSpace(strings.TrimPrefix(p, prefix))
		}
	}
	return ""
}

// TerminalWS: WebSocket к SSH устройства (только admin: subprotocol token или cookie/Basic).
// Первое сообщение — текст JSON: {"type":"init","username":"...","password":"...","port":22}.
// Далее: бинарные кадры — ввод в PTY/TCP; текст JSON {"type":"resize","cols":n,"rows":m} для SSH.
// С сервера: бинарные кадры — вывод терминала; при ошибке — текст JSON {"type":"error",...}.
func (h *Handlers) TerminalWS(w http.ResponseWriter, r *http.Request) {
	// Дублируем в stderr: zap пишет в logs/nms-api.log, а docker logs показывает только stdout/stderr chi.
	log.Printf("nms-api: terminal-ws request path=%s kind=%s remote=%s origin=%q host=%q",
		r.URL.Path, r.URL.Query().Get("kind"), r.RemoteAddr, r.Header.Get("Origin"), r.Host)
	h.logger.Info("terminal ws request",
		zap.String("path", r.URL.Path),
		zap.String("query_kind", r.URL.Query().Get("kind")),
		zap.String("remote_addr", r.RemoteAddr),
	)
	id, err := deviceIDFromChi(r)
	if err != nil {
		log.Printf("nms-api: terminal-ws 400 bad device id: %v", err)
		http.Error(w, "bad device id", http.StatusBadRequest)
		return
	}
	token := terminalTokenFromSubprotocol(r)
	u := verifyTerminalWSToken(token, id)
	if u == nil {
		authRes := adminUserFromRequest(r)
		if authRes.retryAfter > 0 {
			retrySec := int(authRes.retryAfter.Seconds()) + 1
			if retrySec < 1 {
				retrySec = 1
			}
			w.Header().Set("Retry-After", fmt.Sprintf("%d", retrySec))
			log.Printf("nms-api: terminal-ws 429 too many auth attempts device_id=%d remote=%s", id, r.RemoteAddr)
			http.Error(w, "Too many authentication attempts", http.StatusTooManyRequests)
			return
		}
		u = authRes.user
	}
	if u == nil {
		log.Printf("nms-api: terminal-ws 403 forbidden (no admin session/token) device_id=%d remote=%s origin=%q",
			id, r.RemoteAddr, r.Header.Get("Origin"))
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	kind := terminalKindFromQuery(r)
	h.logger.Info("terminal ws kind resolved", zap.String("kind", kind), zap.Int("device_id", id))

	dev, err := h.repo.GetDeviceByID(r.Context(), id)
	if err != nil || dev == nil {
		log.Printf("nms-api: terminal-ws 404 device id=%d err=%v", id, err)
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}

	conn, err := terminalUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("nms-api: terminal-ws upgrade FAILED err=%v host=%q origin=%q xf_proto=%q remote=%s",
			err, r.Host, r.Header.Get("Origin"), r.Header.Get("X-Forwarded-Proto"), r.RemoteAddr)
		h.logger.Warn("terminal ws upgrade failed",
			zap.Error(err),
			zap.String("host", r.Host),
			zap.String("origin", r.Header.Get("Origin")),
			zap.String("xf_proto", r.Header.Get("X-Forwarded-Proto")),
			zap.String("remote_addr", r.RemoteAddr),
		)
		return
	}
	log.Printf("nms-api: terminal-ws upgraded ok device_id=%d kind=%s remote=%s", id, kind, r.RemoteAddr)
	conn.SetReadLimit(terminalWSReadLimit())

	pingStop := make(chan struct{})
	var connWriteMu sync.Mutex

	defer func() {
		close(pingStop)
		if rv := recover(); rv != nil {
			h.logger.Error("terminal ws panic",
				zap.Any("recover", rv),
				zap.String("stack", string(debug.Stack())),
			)
			_ = wsWriteText(conn, &connWriteMu, terminalJSON("error", "internal server error"))
			wsSendCloseFrame(conn, &connWriteMu, websocket.CloseInternalServerErr, "panic")
		}
		_ = conn.Close()
	}()

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
	_, initRaw, err := conn.ReadMessage()
	if err != nil {
		h.logger.Warn("terminal ws init read failed", zap.Error(err), zap.Int("device_id", id), zap.String("kind", kind))
		_ = wsWriteText(conn, &connWriteMu, terminalJSON("error", "read init: "+err.Error()))
		wsSendCloseFrame(conn, &connWriteMu, websocket.CloseGoingAway, "no init")
		return
	}
	var init terminalInitMsg
	if err := json.Unmarshal(initRaw, &init); err != nil {
		h.logger.Warn("terminal ws bad init",
			zap.Error(err),
			zap.Int("payload_size", len(initRaw)),
			zap.Int("device_id", id),
			zap.String("kind", kind),
		)
		_ = wsWriteText(conn, &connWriteMu, terminalJSON("error", "expected init json"))
		wsSendCloseFrame(conn, &connWriteMu, websocket.CloseUnsupportedData, "bad init")
		return
	}
	if err := validateTerminalInitMsg(init, kind); err != nil {
		h.logger.Warn("terminal ws invalid init",
			zap.Error(err),
			zap.Int("payload_size", len(initRaw)),
			zap.Int("device_id", id),
			zap.String("kind", kind),
		)
		_ = wsWriteText(conn, &connWriteMu, terminalJSON("error", err.Error()))
		wsSendCloseFrame(conn, &connWriteMu, websocket.CloseUnsupportedData, "invalid init")
		return
	}
	h.logger.Info("terminal ws init accepted", zap.Int("device_id", id), zap.String("kind", kind), zap.Int("requested_port", init.Port))

	port := init.Port
	if port <= 0 {
		port = 22
	}
	addr := deviceDialAddr(dev.IP, port)
	dialTimeout := terminalDialTimeout()
	deadline := terminalSessionDeadline()

	// Сразу после init — чтобы UI не «висел» молча на долгом Dial.
	_ = wsWriteText(conn, &connWriteMu, terminalJSON("connecting", "параметры приняты, подключаюсь к "+addr+" ("+kind+")…"))

	nmsUser := u.username
	h.logger.Info("terminal session start",
		zap.String("nms_user", nmsUser),
		zap.Int("device_id", id),
		zap.String("device_ip", dev.IP),
		zap.String("kind", kind),
		zap.String("dial_addr", addr),
	)

	if err := h.runTerminalSSH(r.Context(), conn, &connWriteMu, addr, init.Username, init.Password, dialTimeout, deadline, pingStop); err != nil {
		h.logger.Warn("terminal ssh ended", zap.Error(err))
		_ = wsWriteText(conn, &connWriteMu, terminalJSON("error", err.Error()))
		wsSendCloseFrame(conn, &connWriteMu, websocket.CloseNormalClosure, "ssh ended")
	}

	h.logger.Info("terminal session end",
		zap.String("nms_user", nmsUser),
		zap.Int("device_id", id),
		zap.String("kind", kind),
	)
}

func terminalJSON(t, msg string) []byte {
	b, _ := json.Marshal(terminalAckMsg{Type: t, Message: msg})
	return b
}

func wsSendCloseFrame(conn *websocket.Conn, mu *sync.Mutex, code int, reason string) {
	if len(reason) > 120 {
		reason = reason[:117] + "..."
	}
	payload := websocket.FormatCloseMessage(code, reason)
	_ = wsWriteControl(conn, mu, websocket.CloseMessage, payload, 3*time.Second)
}

func terminalWSKeepalive(conn *websocket.Conn, mu *sync.Mutex, stop <-chan struct{}) {
	tick := time.NewTicker(20 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case <-tick.C:
			if err := wsWriteControl(conn, mu, websocket.PingMessage, nil, 5*time.Second); err != nil {
				return
			}
		}
	}
}

func (h *Handlers) runTerminalSSH(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, addr, user, pass string, dialTimeout time.Duration, deadline time.Time, pingStop <-chan struct{}) error {
	hostKeyCallback, err := terminalSSHHostKeyCallback()
	if err != nil {
		return fmt.Errorf("ssh host key policy: %w", err)
	}
	cfg := &ssh.ClientConfig{
		User:            user,
		Auth:            []ssh.AuthMethod{ssh.Password(pass)},
		HostKeyCallback: hostKeyCallback,
		Timeout:         dialTimeout,
	}
	client, err := ssh.Dial("tcp", addr, cfg)
	if err != nil {
		return fmt.Errorf("ssh dial: %w", err)
	}
	defer func() { _ = client.Close() }()

	sess, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("ssh session: %w", err)
	}
	defer func() { _ = sess.Close() }()

	stdin, err := sess.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := sess.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := sess.StderrPipe()
	if err != nil {
		return err
	}

	modes := ssh.TerminalModes{
		ssh.ECHO:          1,
		ssh.TTY_OP_ISPEED: 14400,
		ssh.TTY_OP_OSPEED: 14400,
	}
	if err := sess.RequestPty("xterm-256color", 80, 24, modes); err != nil {
		return fmt.Errorf("pty: %w", err)
	}
	if err := sess.Shell(); err != nil {
		return fmt.Errorf("shell: %w", err)
	}

	_ = wsWriteText(conn, writeMu, terminalJSON("ok", ""))
	go terminalWSKeepalive(conn, writeMu, pingStop)

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	var closeOnce sync.Once
	closeResources := func() {
		closeOnce.Do(func() {
			_ = stdin.Close()
			_ = sess.Close()
			_ = client.Close()
			_ = conn.Close()
		})
	}
	defer closeResources()

	errCh := make(chan error, 3)

	go func() {
		idle := terminalWSReadIdle()
		for {
			if time.Now().After(deadline) {
				errCh <- context.DeadlineExceeded
				return
			}
			select {
			case <-runCtx.Done():
				errCh <- runCtx.Err()
				return
			default:
			}
			_ = conn.SetReadDeadline(time.Now().Add(idle))
			mt, payload, err := conn.ReadMessage()
			if err != nil {
				errCh <- err
				return
			}
			if mt == websocket.TextMessage {
				var rs terminalResizeMsg
				if json.Unmarshal(payload, &rs) == nil && rs.Type == "resize" && rs.Cols > 0 && rs.Rows > 0 {
					_ = sess.WindowChange(rs.Rows, rs.Cols)
				}
				continue
			}
			if mt == websocket.BinaryMessage {
				if _, werr := stdin.Write(payload); werr != nil {
					errCh <- werr
					return
				}
			}
		}
	}()

	go func() {
		errCh <- copyReaderToWSBinary(runCtx, conn, writeMu, stdout, deadline)
	}()
	go func() {
		errCh <- copyReaderToWSBinary(runCtx, conn, writeMu, stderr, deadline)
	}()

	var first error
	collectErr := func(e error) {
		if e != nil && !errors.Is(e, io.EOF) && !websocket.IsCloseError(e, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
			if first == nil {
				first = e
			}
		}
	}

	// Stop the whole session as soon as any leg exits.
	collectErr(<-errCh)
	cancel()
	closeResources()

	for i := 0; i < 2; i++ {
		select {
		case e := <-errCh:
			collectErr(e)
		case <-time.After(2 * time.Second):
		}
	}
	return first
}

func copyWSBinaryToWriter(ctx context.Context, conn *websocket.Conn, w io.Writer, deadline time.Time) error {
	idle := terminalWSReadIdle()
	for {
		if time.Now().After(deadline) {
			return context.DeadlineExceeded
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(idle))
		mt, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		if mt == websocket.TextMessage {
			var rs terminalResizeMsg
			if json.Unmarshal(data, &rs) == nil && rs.Type == "resize" {
				continue
			}
		}
		if mt != websocket.BinaryMessage {
			continue
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
}

func copyReaderToWSBinary(ctx context.Context, conn *websocket.Conn, writeMu *sync.Mutex, r io.Reader, deadline time.Time) error {
	buf := make([]byte, 8192)
	for {
		if time.Now().After(deadline) {
			return context.DeadlineExceeded
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		n, err := r.Read(buf)
		if n > 0 {
			if werr := wsWriteBinary(conn, writeMu, buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func wsWriteText(conn *websocket.Conn, mu *sync.Mutex, payload []byte) error {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return conn.WriteMessage(websocket.TextMessage, payload)
}

func wsWriteControl(conn *websocket.Conn, mu *sync.Mutex, messageType int, data []byte, timeout time.Duration) error {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	return conn.WriteControl(messageType, data, time.Now().Add(timeout))
}

func wsWriteBinary(conn *websocket.Conn, mu *sync.Mutex, payload []byte) error {
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	_ = conn.SetWriteDeadline(time.Now().Add(30 * time.Second))
	return conn.WriteMessage(websocket.BinaryMessage, payload)
}

type byteReader struct {
	r   io.Reader
	buf [1]byte
}

func newByteReader(r io.Reader) *byteReader { return &byteReader{r: r} }

func (b *byteReader) ReadByte() (byte, error) {
	_, err := io.ReadFull(b.r, b.buf[:])
	return b.buf[0], err
}

// TerminalPage — HTML с xterm для /devices/{id}/terminal.
func (h *Handlers) TerminalPage(w http.ResponseWriter, r *http.Request) {
	id, err := deviceIDFromChi(r)
	if err != nil {
		http.Error(w, "bad device id", http.StatusBadRequest)
		return
	}
	dev, err := h.repo.GetDeviceByID(r.Context(), id)
	if err != nil || dev == nil {
		http.Error(w, "device not found", http.StatusNotFound)
		return
	}
	kind := terminalKindFromQuery(r)
	u := userFromContext(r.Context())
	if u == nil || u.role != roleAdmin {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	wsToken, err := signTerminalWSToken(u.username, u.role, id)
	if err != nil {
		http.Error(w, "failed to issue terminal token", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	data := struct {
		DeviceID int
		Name     string
		IP       string
		Kind     string
		WSToken  string
		CSPNonce string
	}{
		DeviceID: id,
		Name:     dev.Name,
		IP:       dev.IP,
		Kind:     kind,
		WSToken:  wsToken,
		CSPNonce: cspNonceFromContext(r),
	}
	if err := h.terminalTmpl.Execute(w, data); err != nil {
		h.logger.Error("terminal page", zap.Error(err))
	}
}
