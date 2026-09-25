package mail

import (
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
