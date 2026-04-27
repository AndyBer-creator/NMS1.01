package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// TelegramAlert sends trap notifications to Telegram bot API.
type TelegramAlert struct {
	BotToken   string
	ChatID     string
	HTTPClient *http.Client // nil -> http.DefaultClient
}

// NewTelegramAlert creates Telegram sender with bot token and chat ID.
func NewTelegramAlert(botToken, chatID string) *TelegramAlert {
	return &TelegramAlert{
		BotToken: botToken,
		ChatID:   chatID,
	}
}

// SendCriticalTrap sends critical trap notification with background context.
func (t *TelegramAlert) SendCriticalTrap(deviceIP, oid, trapVars string) error {
	return t.SendCriticalTrapContext(context.Background(), deviceIP, oid, trapVars)
}

// SendCriticalTrapContext sends critical trap notification with context.
func (t *TelegramAlert) SendCriticalTrapContext(ctx context.Context, deviceIP, oid, trapVars string) error {
	// Plain text format avoids markdown escaping issues.
	msg := fmt.Sprintf(
		"🔴 CRITICAL TRAP DETECTED\n\n"+
			"📱 Device: %s\n"+
			"🔗 OID: %s\n"+
			"⏰ Time: %s\n"+
			"📦 Vars: %s",
		deviceIP,
		oid,
		time.Now().Format("15:04 02.01.2006"),
		truncateRunes(strings.TrimSpace(trapVars), 100),
	)

	data := map[string]string{
		"chat_id": t.ChatID,
		"text":    msg,
	}

	jsonData, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("json marshal failed: %w", err)
	}

	client := t.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		"https://api.telegram.org/bot"+t.BotToken+"/sendMessage",
		bytes.NewBuffer(jsonData),
	)
	if err != nil {
		return fmt.Errorf("telegram request build failed: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("telegram http post failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("telegram API error: status %d, body: %s", resp.StatusCode, string(body))
	}

	return nil
}

// truncateRunes returns s truncated to at most maxRunes runes.
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= maxRunes {
		return s
	}
	return string(r[:maxRunes])
}
