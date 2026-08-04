package mailer

import (
	"strings"
	"testing"

	"github.com/yourname/yourplatform/control-plane/internal/config"
)

func TestBuildMessage_WellFormedMIME(t *testing.T) {
	msg := buildMessage("alerts@yourplatform.app", "ops@example.com", "CRITICAL: down", "Body line 1\nBody line 2")

	for _, want := range []string{
		"From: alerts@yourplatform.app\r\n",
		"To: ops@example.com\r\n",
		"Subject: CRITICAL: down\r\n",
		"MIME-Version: 1.0\r\n",
		"Content-Type: text/plain; charset=UTF-8\r\n",
		"\r\nBody line 1\r\nBody line 2\r\n",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
	if strings.Contains(msg, "\n\n\n\n") {
		t.Error("message contains excessive blank lines (CRLF expected)")
	}
}

func TestLogSender_RecordsWithoutError(t *testing.T) {
	s := &LogSender{From: "alerts@x.app"}
	if err := s.Send("ops@example.com", "sub", "body"); err != nil {
		t.Fatalf("LogSender.Send returned error: %v", err)
	}
}

func TestNewFromConfig_Selection(t *testing.T) {
	disabled := &config.Config{EmailEnabled: false}
	if _, ok := NewFromConfig(disabled).(*LogSender); !ok {
		t.Error("disabled email should select LogSender")
	}

	partial := &config.Config{EmailEnabled: true, SMTPHost: ""}
	if _, ok := NewFromConfig(partial).(*LogSender); !ok {
		t.Error("missing SMTP_HOST should select LogSender")
	}

	enabled := &config.Config{
		EmailEnabled: true,
		SMTPHost:     "smtp.example.com",
		SMTPPort:     587,
		SMTPUser:     "u",
		SMTPPass:     "p",
		SMTPFrom:     "from@example.com",
	}
	s, ok := NewFromConfig(enabled).(*SMTP)
	if !ok {
		t.Fatal("configured email should select SMTP sender")
	}
	if s.Host != "smtp.example.com" || s.Port != 587 || s.User != "u" || s.From != "from@example.com" {
		t.Fatalf("SMTP sender mismatch: %+v", s)
	}

	nilCfg := NewFromConfig(nil)
	if _, ok := nilCfg.(*LogSender); !ok {
		t.Error("nil config should select LogSender")
	}
}

func TestSMTPSend_RejectsEmptyRecipient(t *testing.T) {
	s := &SMTP{Host: "h", Port: 587, From: "f"}
	if err := s.Send("", "sub", "body"); err == nil {
		t.Fatal("expected error for empty recipient")
	}
}
