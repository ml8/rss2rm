// Package mailer provides a simple SMTP email sender for verification emails.
package mailer

import (
	"fmt"
	"net/smtp"
)

// Config holds SMTP connection settings.
type Config struct {
	Host     string
	Port     string
	User     string
	Password string
	From     string
}

// IsConfigured reports whether the SMTP settings are sufficient to send email.
func (c Config) IsConfigured() bool {
	return c.Host != "" && c.Port != "" && c.From != ""
}

// SendVerification sends a verification email with a link containing the token.
func SendVerification(cfg Config, toEmail, token, baseURL string) error {
	verifyURL := fmt.Sprintf("%s/api/v1/auth/verify?token=%s", baseURL, token)

	subject := "Verify your rss2rm account"
	body := fmt.Sprintf("Click the link below to verify your email address:\n\n%s\n\nThis link expires in 24 hours.", verifyURL)

	msg := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s",
		cfg.From, toEmail, subject, body)

	addr := cfg.Host + ":" + cfg.Port
	var auth smtp.Auth
	if cfg.User != "" {
		auth = smtp.PlainAuth("", cfg.User, cfg.Password, cfg.Host)
	}

	return smtp.SendMail(addr, auth, cfg.From, []string{toEmail}, []byte(msg))
}
