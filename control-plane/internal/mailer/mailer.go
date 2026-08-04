// Package mailer delivers alert emails from the control plane.
//
// Layer 4C Step 6: the control plane — never the agent — sends email. The
// MVP ships with an SMTP sender built on the standard library, which works
// with any provider that exposes SMTP (Gmail, Postmark, Resend, SES, ...).
// When SMTP is not configured, a LogSender records the email in the logs so
// behaviour stays observable during development.
//
// Upgrading to an HTTP API provider (Resend, Postmark) or a full notification
// platform (Knock) later only requires a new Sender implementation.
package mailer

import (
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"

	"github.com/yourname/yourplatform/control-plane/internal/config"
)

// Sender delivers a single plain-text email.
type Sender interface {
	Send(to, subject, body string) error
}

// SMTP sends email through an SMTP server using PLAIN auth (STARTTLS on port
// 587, TLS on 465).
type SMTP struct {
	Host string
	Port int
	User string
	Pass string
	From string
}

// Send builds a minimal RFC 5322 message and hands it to net/smtp.SendMail.
func (s *SMTP) Send(to, subject, body string) error {
	if s.Host == "" || to == "" {
		return fmt.Errorf("mailer: empty host or recipient")
	}
	addr := fmt.Sprintf("%s:%d", s.Host, s.Port)
	var auth smtp.Auth
	if s.User != "" {
		auth = smtp.PlainAuth("", s.User, s.Pass, s.Host)
	}
	msg := buildMessage(s.From, to, subject, body)
	return smtp.SendMail(addr, auth, s.From, []string{to}, []byte(msg))
}

// buildMessage assembles a MIME text/plain message with CRLF line endings
// (SMTP requires CRLF; body newlines are normalized).
func buildMessage(from, to, subject, body string) string {
	var b strings.Builder
	b.WriteString("From: " + from + "\r\n")
	b.WriteString("To: " + to + "\r\n")
	b.WriteString("Subject: " + subject + "\r\n")
	b.WriteString("MIME-Version: 1.0\r\n")
	b.WriteString("Content-Type: text/plain; charset=UTF-8\r\n")
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	b.WriteString("\r\n")
	return b.String()
}

// LogSender records emails in the server log instead of sending them. Used
// when email is not configured so alert delivery stays visible in dev.
type LogSender struct {
	From string
}

func (s *LogSender) Send(to, subject, body string) error {
	slog.Info("alert email (not sent: SMTP not configured)",
		"from", s.From, "to", to, "subject", subject, "body_preview", preview(body, 200))
	return nil
}

func preview(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// NewFromConfig returns the Sender for the control-plane config: an SMTP
// sender when EMAIL_ENABLED + SMTP_HOST are set, otherwise a LogSender.
func NewFromConfig(cfg *config.Config) Sender {
	if cfg != nil && cfg.EmailConfigured() {
		return &SMTP{
			Host: cfg.SMTPHost,
			Port: cfg.SMTPPort,
			User: cfg.SMTPUser,
			Pass: cfg.SMTPPass,
			From: cfg.SMTPFrom,
		}
	}
	return &LogSender{From: smtpFrom(cfg)}
}

func smtpFrom(cfg *config.Config) string {
	if cfg != nil && cfg.SMTPFrom != "" {
		return cfg.SMTPFrom
	}
	return "alerts@yourplatform.app"
}
