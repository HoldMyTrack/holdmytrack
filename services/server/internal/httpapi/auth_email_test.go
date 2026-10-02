package httpapi

import (
	"context"
	"net/http"
	"sync"
	"testing"
)

// sentMail records what a test's server would have mailed.
type sentMail struct {
	mu sync.Mutex
	to []string
}

func (m *sentMail) Send(_ context.Context, to, _, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.to = append(m.to, to)
	return nil
}

func (d *dbTest) email(a account) (email string, verified bool) {
	d.t.Helper()
	if err := d.pool.QueryRow(context.Background(),
		`SELECT email, email_verified FROM users WHERE id = $1`, a.id).Scan(&email, &verified); err != nil {
		d.t.Fatalf("read email: %v", err)
	}
	return email, verified
}

func (d *dbTest) verificationToken(a account) string {
	d.t.Helper()
	var token string
	if err := d.pool.QueryRow(context.Background(),
		`SELECT id FROM email_verifications WHERE user_id = $1 ORDER BY created_at DESC LIMIT 1`, a.id).Scan(&token); err != nil {
		d.t.Fatalf("read verification token: %v", err)
	}
	return token
}

// An email change is the account's only once the new address's owner confirms it: until then
// the account keeps its address and its verified state, and no one else is kept from the
// address.
func TestChangeEmailIsPendingUntilConfirmed(t *testing.T) {
	d := newDBTest(t)
	mails := &sentMail{}
	d.srv.mailer = mails
	a := d.newAccount(false)
	before, _ := d.email(a)
	target := "new-" + before

	d.decode(d.do(a, http.MethodPatch, "/v1/auth/email", map[string]string{"email": target}), http.StatusOK, nil)
	if email, verified := d.email(a); email != before || !verified {
		t.Fatalf("after change: email %q verified %v, want %q still verified", email, verified, before)
	}
	if len(mails.to) != 1 || mails.to[0] != target {
		t.Fatalf("mailed %v, want the new address", mails.to)
	}

	token := d.verificationToken(a)
	d.decode(d.do(account{}, http.MethodPost, "/v1/auth/verify-email", map[string]string{"token": token}), http.StatusOK, nil)
	if email, verified := d.email(a); email != target || !verified {
		t.Fatalf("after confirming: email %q verified %v, want %q verified", email, verified, target)
	}
}

// A link sent to the account's address before a change confirms nothing after it: it can't
// mark the new, unproven address verified.
func TestVerificationLinkConfirmsOnlyItsOwnAddress(t *testing.T) {
	d := newDBTest(t)
	d.srv.mailer = &sentMail{}
	a := d.newAccount(false)
	if _, err := d.pool.Exec(context.Background(), `UPDATE users SET email_verified = false WHERE id = $1`, a.id); err != nil {
		t.Fatal(err)
	}
	before, _ := d.email(a)
	if err := d.srv.sendVerificationEmail(context.Background(), a.id, before, "en"); err != nil {
		t.Fatal(err)
	}
	oldToken := d.verificationToken(a)

	d.decode(d.do(a, http.MethodPatch, "/v1/auth/email", map[string]string{"email": "other-" + before}), http.StatusOK, nil)
	rec := d.do(account{}, http.MethodPost, "/v1/auth/verify-email", map[string]string{"token": oldToken})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("old link: status %d, want 400", rec.Code)
	}
	if email, verified := d.email(a); email != before || verified {
		t.Fatalf("email %q verified %v, want %q unverified", email, verified, before)
	}
}

func TestChangeEmailToAnotherAccountsAddressIsRefused(t *testing.T) {
	d := newDBTest(t)
	d.srv.mailer = &sentMail{}
	a, b := d.newAccount(false), d.newAccount(false)
	taken, _ := d.email(b)
	rec := d.do(a, http.MethodPatch, "/v1/auth/email", map[string]string{"email": taken})
	if rec.Code != http.StatusConflict {
		t.Fatalf("status %d, want 409", rec.Code)
	}
}
