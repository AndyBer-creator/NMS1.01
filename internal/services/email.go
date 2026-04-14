package services

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"os"
	"strings"
	"time"
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

func allowPlainSMTP() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("NMS_SMTP_ALLOW_PLAINTEXT")))
	return v == "1" || v == "true" || v == "yes" || v == "on"
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

	// For common SMTPS ports (465), use direct TLS.
	if c.Port == "465" {
		return c.sendTLS(addr, to, []byte(msg))
	}
	// For all non-465 ports, require STARTTLS by default.
	// Legacy plaintext SMTP is allowed only with explicit override.
	if c.Port == "587" || !allowPlainSMTP() {
		return c.sendStartTLS(addr, to, []byte(msg))
	}
	var auth smtp.Auth
	if c.User != "" || c.Pass != "" {
		auth = smtp.PlainAuth("", c.User, c.Pass, c.Host)
	}
	return smtp.SendMail(addr, auth, c.From, []string{to}, []byte(msg))
}

func (c *SMTPClient) dialTimeout(network, addr string) (net.Conn, error) {
	d := net.Dialer{Timeout: 6 * time.Second}
	return d.Dial(network, addr)
}

func (c *SMTPClient) sendTLS(addr, to string, msg []byte) error {
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: 6 * time.Second}, "tcp", addr, &tls.Config{
		ServerName: c.Host,
		MinVersion: tls.VersionTLS12,
	})
	if err != nil {
		return err
	}
	defer func() { _ = conn.Close() }()

	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if c.User != "" || c.Pass != "" {
		if err := client.Auth(smtp.PlainAuth("", c.User, c.Pass, c.Host)); err != nil {
			return err
		}
	}
	if err := client.Mail(c.From); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func (c *SMTPClient) sendStartTLS(addr, to string, msg []byte) error {
	conn, err := c.dialTimeout("tcp", addr)
	if err != nil {
		return err
	}
	client, err := smtp.NewClient(conn, c.Host)
	if err != nil {
		_ = conn.Close()
		return err
	}
	defer func() { _ = client.Close() }()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: c.Host, MinVersion: tls.VersionTLS12}); err != nil {
			return err
		}
	} else if !allowPlainSMTP() {
		return fmt.Errorf("smtp server does not support STARTTLS")
	}
	if c.User != "" || c.Pass != "" {
		if err := client.Auth(smtp.PlainAuth("", c.User, c.Pass, c.Host)); err != nil {
			return err
		}
	}
	if err := client.Mail(c.From); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(msg); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}
