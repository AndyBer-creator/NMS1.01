package services

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type TelegramAlert struct {
	BotToken   string
	ChatID     string
	HTTPClient *http.Client // nil — http.DefaultClient (для тестов подставляют Transport)
}

func NewTelegramAlert(botToken, chatID string) *TelegramAlert {
	return &TelegramAlert{
		BotToken: botToken,
		ChatID:   chatID,
	}
}

func (t *TelegramAlert) SendCriticalTrap(deviceIP, oid, trapVars string) error {
	return t.SendCriticalTrapContext(context.Background(), deviceIP, oid, trapVars)
}

func (t *TelegramAlert) SendCriticalTrapContext(ctx context.Context, deviceIP, oid, trapVars string) error {
	// ✅ Простой текст БЕЗ Markdown
	msg := fmt.Sprintf(
		"🔴 CRITICAL TRAP DETECTED\n\n"+
			"📱 Device: %s\n"+
			"🔗 OID: %s\n"+
			"⏰ Time: %s\n"+
			"📦 Vars: %s",
		deviceIP,
		oid,
		time.Now().Format("15:04 02.01.2006"),
		trapVars[:min(100, len(trapVars))],
	)

	data := map[string]string{ // ✅ string вместо interface{}
		"chat_id": t.ChatID,
		"text":    msg,
		// "parse_mode": "Markdown",  // ❌ УДАЛЕНО!
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
