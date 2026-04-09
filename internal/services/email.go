package services

import (
	"fmt"
	"net/smtp"
	"strings"
)

type SMTPClient struct {
	Host string
	Port string
	User string
	Pass string
	From string
}

func NewSMTPClient(host, port, user, pass, from string) *SMTPClient {
	return &SMTPClient{
		Host: strings.TrimSpace(host),
		Port: strings.TrimSpace(port),
		User: strings.TrimSpace(user),
		Pass: pass,
		From: strings.TrimSpace(from),
	}
}

func (c *SMTPClient) Enabled() bool {
	return c.Host != "" && c.Port != "" && c.From != ""
}

func (c *SMTPClient) Send(to, subject, body string) error {
	if !c.Enabled() {
		return fmt.Errorf("smtp is not configured")
	}
	to = strings.TrimSpace(to)
	if to == "" {
		return fmt.Errorf("recipient is empty")
	}
	addr := c.Host + ":" + c.Port
	headers := []string{
		"From: " + c.From,
		"To: " + to,
		"Subject: " + subject,
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
	}
	msg := strings.Join(headers, "\r\n") + "\r\n\r\n" + body + "\r\n"

	var auth smtp.Auth
	if c.User != "" || c.Pass != "" {
		auth = smtp.PlainAuth("", c.User, c.Pass, c.Host)
	}
	return smtp.SendMail(addr, auth, c.From, []string{to}, []byte(msg))
}
