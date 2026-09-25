// Package mail sends the one kind of email this app has ever needed to send — a password
// reset link (IMPLEMENTATION.md §4.11). Stdlib net/smtp against whatever
// SMTP_* config.Config was given, not a vendor SDK or HTTP API: any provider that speaks SMTP
// works, including a personal Gmail account via an app password, which is what this app's own
// single real user already has — no new account to provision just to send one kind of email.
package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"log/slog"
	"mime"
	"net"
	"net/smtp"
)

type Sender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// New picks the real SMTP sender when host is configured, or a sender that logs the message
// instead when it isn't — an empty SMTP_HOST is a valid, intentional default (dev/test never
// need real mail delivery to exercise the reset flow end to end; the log line carries the
// same reset link a real email would), not a placeholder demanding to be filled in.
func New(host, port, username, password, from string, log *slog.Logger) Sender {
	if host == "" {
		return &logSender{log: log}
	}
	return &smtpSender{host: host, port: port, username: username, password: password, from: from}
}

type logSender struct {
	log *slog.Logger
}

func (s *logSender) Send(_ context.Context, to, subject, body string) error {
	s.log.Info("mail: SMTP_HOST not configured, logging instead of sending", "to", to, "subject", subject, "body", body)
	return nil
}

type smtpSender struct {
	host, port, username, password, from string
}

// Send does the manual STARTTLS dance net/smtp has no convenience wrapper for — dial plain,
// upgrade if the server offers STARTTLS (every real provider does), authenticate, then the
// usual MAIL/RCPT/DATA sequence. ctx isn't threaded into net/smtp's own calls (the stdlib
// package predates context and has no cancellable variant); kept on the interface anyway so
// a future non-SMTP Sender (an HTTP API) can honor it without changing every call site.
func (s *smtpSender) Send(ctx context.Context, to, subject, body string) error {
	addr := net.JoinHostPort(s.host, s.port)
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("mail: dial %s: %w", addr, err)
	}
	defer conn.Close()

	client, err := smtp.NewClient(conn, s.host)
	if err != nil {
		return fmt.Errorf("mail: new client: %w", err)
	}
	defer client.Close()

	if ok, _ := client.Extension("STARTTLS"); ok {
		if err := client.StartTLS(&tls.Config{ServerName: s.host}); err != nil {
			return fmt.Errorf("mail: starttls: %w", err)
		}
	}

	if s.username != "" {
		if err := client.Auth(smtp.PlainAuth("", s.username, s.password, s.host)); err != nil {
			return fmt.Errorf("mail: auth: %w", err)
		}
	}

	if err := client.Mail(s.from); err != nil {
		return fmt.Errorf("mail: MAIL FROM: %w", err)
	}
	if err := client.Rcpt(to); err != nil {
		return fmt.Errorf("mail: RCPT TO: %w", err)
	}

	w, err := client.Data()
	if err != nil {
		return fmt.Errorf("mail: DATA: %w", err)
	}
	if _, err := w.Write(message(s.from, to, subject, body)); err != nil {
		return fmt.Errorf("mail: write body: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("mail: close body: %w", err)
	}
	return client.Quit()
}

// message is the whole email as sent. The subject is RFC 2047-encoded: a header may only
// carry ASCII, and a translated subject (a Russian one) isn't — mime.QEncoding leaves an
// ASCII subject exactly as it was. The body needs no such step; Content-Type declares it UTF-8.
func message(from, to, subject, body string) []byte {
	return fmt.Appendf(nil, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n%s\r\n",
		from, to, mime.QEncoding.Encode("utf-8", subject), body)
}
