package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
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

// After 10 wrong passwords for one email in the window, even the right one is refused: the
// limit is checked before the password is.
func TestSignInFailuresAreLimitedPerEmail(t *testing.T) {
	d := newDBTest(t)
	a := d.newAccount(false)
	email, _ := d.email(a)
	hash, err := bcrypt.GenerateFromPassword([]byte("right-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.pool.Exec(context.Background(), `UPDATE users SET password_hash = $2 WHERE id = $1`, a.id, hash); err != nil {
		t.Fatal(err)
	}
	login := func(password string) int {
		return d.do(account{}, http.MethodPost, "/v1/auth/login", map[string]string{"email": email, "password": password}).Code
	}
	if code := login("right-password"); code != http.StatusOK {
		t.Fatalf("first sign-in: %d", code)
	}
	for i := 0; i < 10; i++ {
		if code := login("wrong-password"); code != http.StatusUnauthorized {
			t.Fatalf("wrong password %d: %d, want 401", i+1, code)
		}
	}
	if code := login("right-password"); code != http.StatusTooManyRequests {
		t.Fatalf("right password after 10 failures: %d, want 429", code)
	}
}

// The emailed /verify link verifies whoever's link it is, but doesn't switch a browser signed
// in to a different account over to that one — a link someone sent from their own inbox would
// otherwise move the reader into the sender's account.
func TestVerifyLinkKeepsAnotherAccountsSession(t *testing.T) {
	d := newDBTest(t)
	d.srv.mailer = &sentMail{}
	reader, sender := d.newAccount(false), d.newAccount(false)
	if _, err := d.pool.Exec(context.Background(), `UPDATE users SET email_verified = false WHERE id = $1`, sender.id); err != nil {
		t.Fatal(err)
	}
	email, _ := d.email(sender)
	if err := d.srv.sendVerificationEmail(context.Background(), sender.id, email, "en"); err != nil {
		t.Fatal(err)
	}

	rec := d.do(reader, http.MethodGet, "/verify?token="+d.verificationToken(sender), nil)
	if rec.Code != http.StatusSeeOther {
		t.Fatalf("status %d, want a redirect", rec.Code)
	}
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookieName {
			t.Fatalf("set a session cookie for the sender's account: %v", c)
		}
	}
	if _, verified := d.email(sender); !verified {
		t.Fatal("the sender's address wasn't verified")
	}
}

// One address trying many accounts' passwords is stopped after 50 failures, even though no
// single email reaches its own limit.
func TestSignInFailuresAreLimitedPerAddress(t *testing.T) {
	d := newDBTest(t)
	login := func(i int) int {
		body := fmt.Sprintf(`{"email":"nobody-%d-%d@holdmytrack.invalid","password":"wrong-password"}`, time.Now().UnixNano(), i)
		req := httptest.NewRequest(http.MethodPost, "/v1/auth/login", strings.NewReader(body))
		req.RemoteAddr = "198.51.100.77:4321"
		rec := httptest.NewRecorder()
		d.srv.ServeHTTP(rec, req)
		return rec.Code
	}
	for i := 0; i < 50; i++ {
		if code := login(i); code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: %d, want 401", i+1, code)
		}
	}
	if code := login(50); code != http.StatusTooManyRequests {
		t.Fatalf("attempt 51: %d, want 429", code)
	}
}
