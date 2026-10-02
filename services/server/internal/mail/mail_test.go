package mail

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
)

func TestMessageEncodesSubject(t *testing.T) {
	ascii := string(message("a@x", "b@x", "Reset your HoldMyTrack password", "body"))
	if !strings.Contains(ascii, "\r\nSubject: Reset your HoldMyTrack password\r\n") {
		t.Errorf("ASCII subject changed:\n%s", ascii)
	}
	ru := string(message("a@x", "b@x", "Сброс пароля HoldMyTrack", "Тело"))
	if !strings.Contains(ru, "\r\nSubject: =?utf-8?q?") {
		t.Errorf("non-ASCII subject not RFC 2047-encoded:\n%s", ru)
	}
	head, _, _ := strings.Cut(ru, "\r\n\r\n")
	for _, c := range head {
		if c > 127 {
			t.Fatalf("non-ASCII byte in headers:\n%s", head)
		}
	}
	if !strings.HasSuffix(ru, "\r\n\r\nТело\r\n") {
		t.Errorf("body not sent as is:\n%s", ru)
	}
}

// Without SMTP, a real deployment's log never carries a message body: it holds a live reset or
// verification link.
func TestLogSenderKeepsBodiesOutOfARealDeploymentsLog(t *testing.T) {
	for _, logBodies := range []bool{false, true} {
		var buf bytes.Buffer
		s := New("", "", "", "", "", logBodies, slog.New(slog.NewTextHandler(&buf, nil)))
		if err := s.Send(context.Background(), "a@x", "Reset", "https://app.example/reset?token=secret"); err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(buf.String(), "secret"); got != logBodies {
			t.Errorf("logBodies %v: body in log = %v\n%s", logBodies, got, buf.String())
		}
	}
}
