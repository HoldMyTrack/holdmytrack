package httpapi

// Covers docs/SPEC.md FR-1.1–FR-1.6 and FR-2.1 (accounts, sessions, the demo).

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/png"
	"mime/multipart"
	"net/http"
	"testing"
	"time"
)

// An account's life through the JSON API, end to end: signing up, verifying the address,
// saving Settings and an avatar, signing out and in, and resetting the password. Each step's
// exchange is also checked against the API contract (dbTest.do).
func TestAccountFlow(t *testing.T) {
	d := newDBTestWithS3(t, newMemS3())
	ctx := context.Background()
	email := fmt.Sprintf("flow-%d@holdmytrack.invalid", time.Now().UnixNano())
	t.Cleanup(func() {
		if _, err := d.pool.Exec(context.Background(), `DELETE FROM users WHERE email = $1`, email); err != nil {
			t.Errorf("delete account: %v", err)
		}
	})
	const password = "a long enough password"

	var signedUp authResponseWithSession
	d.decode(d.do(account{}, "POST", "/v1/auth/signup", map[string]any{
		"email": email, "password": password, "timezone": "Europe/Berlin",
	}), http.StatusCreated, &signedUp)
	if signedUp.EmailVerified || signedUp.Timezone != "Europe/Berlin" || signedUp.SessionToken == "" {
		t.Fatalf("signup: %+v", signedUp)
	}
	me := account{session: signedUp.SessionToken}
	if err := d.pool.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&me.id); err != nil {
		t.Fatal(err)
	}
	d.decode(d.do(me, "GET", "/v1/auth/me", nil), http.StatusOK, nil)
	d.decode(d.do(me, "POST", "/v1/auth/resend-verification", nil), http.StatusOK, nil)
	var verified authResponseWithSession
	d.decode(d.do(account{}, "POST", "/v1/auth/verify-email", map[string]any{"token": d.verificationToken(me)}), http.StatusOK, &verified)
	if !verified.EmailVerified {
		t.Fatalf("verify: %+v", verified)
	}

	var saved authResponse
	d.decode(d.do(me, "PATCH", "/v1/account/settings", map[string]any{
		"display_name": " Sam ", "country": "us", "timezone": "America/New_York", "locale": "",
	}), http.StatusOK, &saved)
	if saved.DisplayName != "Sam" || saved.Country != "US" || saved.Timezone != "America/New_York" {
		t.Fatalf("settings: %+v", saved)
	}

	var img bytes.Buffer
	if err := png.Encode(&img, image.NewRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	part, _ := mw.CreateFormFile("file", "me.png")
	part.Write(img.Bytes())
	mw.Close()
	d.decode(d.send(me, "POST", "/v1/account/avatar", mw.FormDataContentType(), body.Bytes()), http.StatusOK, &saved)
	if saved.AvatarURL == "" {
		t.Fatalf("avatar upload: %+v", saved)
	}
	if rec := d.do(me, "GET", "/v1/account/avatar", nil); rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), img.Bytes()) {
		t.Fatalf("avatar read: %d", rec.Code)
	}
	d.decode(d.do(me, "DELETE", "/v1/account/avatar", nil), http.StatusOK, &saved)
	if saved.AvatarURL != "" {
		t.Fatalf("avatar removal: %+v", saved)
	}

	if rec := d.do(me, "POST", "/v1/auth/logout", nil); rec.Code != http.StatusNoContent {
		t.Fatalf("logout: %d", rec.Code)
	}
	if rec := d.do(me, "GET", "/v1/auth/me", nil); rec.Code != http.StatusUnauthorized {
		t.Fatalf("me after logout: %d", rec.Code)
	}
	d.decode(d.do(account{}, "POST", "/v1/auth/login", map[string]any{"email": email, "password": password}), http.StatusOK, nil)

	d.decode(d.do(account{}, "POST", "/v1/auth/forgot-password", map[string]any{"email": email}), http.StatusOK, nil)
	var token string
	if err := d.pool.QueryRow(ctx, `SELECT id FROM password_resets WHERE user_id = $1`, me.id).Scan(&token); err != nil {
		t.Fatal(err)
	}
	d.decode(d.do(account{}, "POST", "/v1/auth/reset-password", map[string]any{"token": token, "password": "another long password"}), http.StatusOK, nil)
	if rec := d.do(account{}, "POST", "/v1/auth/login", map[string]any{"email": email, "password": password}); rec.Code != http.StatusUnauthorized {
		t.Fatalf("old password after reset: %d", rec.Code)
	}
}

// The demo starts a session on the shared demo account, which reads as verified and has no
// address to show.
func TestDemoStart(t *testing.T) {
	d := newDBTest(t)
	var demo authResponseWithSession
	d.decode(d.do(account{}, "POST", "/v1/auth/demo", nil), http.StatusCreated, &demo)
	t.Cleanup(func() {
		if _, err := d.pool.Exec(context.Background(), `DELETE FROM sessions WHERE id = $1`, demo.SessionToken); err != nil {
			t.Errorf("delete demo session: %v", err)
		}
	})
	if !demo.IsDemo || !demo.EmailVerified || demo.Email != "" || demo.SessionToken == "" {
		t.Fatalf("demo: %+v", demo)
	}
}
