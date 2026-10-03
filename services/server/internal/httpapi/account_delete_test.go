package httpapi

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestDeleteAccountNeedsItsEmail(t *testing.T) {
	d := newDBTest(t)
	a := d.newAccount(false)
	ctx := context.Background()
	var email string
	d.pool.QueryRow(ctx, `SELECT email FROM users WHERE id = $1`, a.id).Scan(&email)

	if rec := d.do(a, "DELETE", "/v1/account", map[string]string{"email": "someone@else.example"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("wrong email: status %d", rec.Code)
	}
	if rec := d.do(a, "DELETE", "/v1/account", map[string]string{"email": "  " + strings.ToUpper(email) + " "}); rec.Code != http.StatusNoContent {
		t.Fatalf("own email: status %d: %s", rec.Code, rec.Body)
	}

	var stored string
	var deleted, hasPassword bool
	d.pool.QueryRow(ctx, `SELECT email, deleted_at IS NOT NULL, password_hash IS NOT NULL FROM users WHERE id = $1`, a.id).Scan(&stored, &deleted, &hasPassword)
	if !deleted || hasPassword || stored == email {
		t.Errorf("after delete: email %q, deleted %v, password %v", stored, deleted, hasPassword)
	}
	var sessions int
	d.pool.QueryRow(ctx, `SELECT count(*) FROM sessions WHERE user_id = $1`, a.id).Scan(&sessions)
	if sessions != 0 {
		t.Errorf("%d sessions left", sessions)
	}
	if rec := d.do(a, "GET", "/v1/auth/me", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("old session still signed in: %d", rec.Code)
	}
	// The address is free again at once.
	var id string
	if err := d.pool.QueryRow(ctx, `INSERT INTO users (email) VALUES ($1) RETURNING id`, email).Scan(&id); err != nil {
		t.Fatalf("sign up again with the address: %v", err)
	}
	d.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
}

func TestDeleteAccountUnverifiedYesDemoNo(t *testing.T) {
	d := newDBTest(t)
	ctx := context.Background()

	demo := d.newAccount(true)
	if rec := d.do(demo, "DELETE", "/v1/account", map[string]string{"email": "x"}); rec.Code != http.StatusForbidden {
		t.Errorf("demo: status %d", rec.Code)
	}

	a := d.newAccount(false)
	var email string
	d.pool.QueryRow(ctx, `UPDATE users SET email_verified = false WHERE id = $1 RETURNING email`, a.id).Scan(&email)
	if rec := d.do(a, "DELETE", "/v1/account", map[string]string{"email": email}); rec.Code != http.StatusNoContent {
		t.Errorf("unverified: status %d: %s", rec.Code, rec.Body)
	}
}
