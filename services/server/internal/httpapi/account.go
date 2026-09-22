package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// The Settings page (Avatar, Name, Country, Timezone, Privacy Trim) — see
// migrations/0001_init.sql's users table (display_name/country/avatar_key/
// avatar_content_type/avatar_updated_at) and migrations/0021_user_timezone.sql (timezone).
// Country, Timezone and Privacy Trim are covered by handleUpdateSettings; the avatar is a
// separate content type entirely, so it gets its own three endpoints below.

var countryCodePattern = regexp.MustCompile(`^[A-Z]{2}$`)

// maxPrivacyTrimCm is a sanity ceiling, not a claim that 200 m of trim is ever a good idea —
// it exists to reject an obviously-wrong value (a typo with an extra digit), not to opine on
// what a reasonable trim looks like.
const maxPrivacyTrimCm = 20000

type updateSettingsRequest struct {
	DisplayName   string `json:"display_name"`
	Country       string `json:"country"`
	PrivacyTrimCm int    `json:"privacy_trim_cm"`
	Timezone      string `json:"timezone"`
}

// handleUpdateSettings serves `PATCH /v1/account/settings` — a full replace of all three
// fields at once, since the Settings page saves as one form rather than auto-saving each
// field individually. Returns the same authResponse shape handleMe does, so the frontend can
// update its local state directly from this response instead of a follow-up GET.
func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())

	var req updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	req.DisplayName = strings.TrimSpace(req.DisplayName)
	req.Country = strings.ToUpper(strings.TrimSpace(req.Country))
	if req.Country != "" && !countryCodePattern.MatchString(req.Country) {
		http.Error(w, "country must be a two-letter code, or empty to unset", http.StatusBadRequest)
		return
	}
	if req.PrivacyTrimCm < 0 || req.PrivacyTrimCm > maxPrivacyTrimCm {
		http.Error(w, fmt.Sprintf("privacy_trim_cm must be between 0 and %d", maxPrivacyTrimCm), http.StatusBadRequest)
		return
	}
	// Unlike Country, timezone has no "unset" state (the column is NOT NULL DEFAULT 'UTC' —
	// migrations/0021_user_timezone.sql) — Settings always sends the field's current
	// selection, so an empty/unloadable value here means a malformed request, not a deliberate
	// clear, and is rejected rather than silently defaulted.
	tz, ok := normalizeTimezone(req.Timezone)
	if !ok {
		http.Error(w, "timezone must be a valid IANA zone name (e.g. America/New_York)", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	// NULLIF, not a Go-side branch on "" — an empty display name or country means "unset",
	// same as every other nullable text column this API already treats that way.
	if _, err := s.pool.Exec(ctx, `
		UPDATE users SET display_name = NULLIF($2, ''), country = NULLIF($3, ''), privacy_trim_cm = $4, timezone = $5
		WHERE id = $1
	`, userID, req.DisplayName, req.Country, req.PrivacyTrimCm, tz); err != nil {
		s.log.Error("update settings failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp, err := s.loadAuthResponse(ctx, userID)
	if err != nil {
		s.log.Error("settings response lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// maxAvatarBytes bounds a profile picture, not a payload that needs upload.go's much larger
// headroom — a multi-megapixel photo straight off a phone still fits comfortably under this.
const maxAvatarBytes = 5 << 20 // 5 MiB

var allowedAvatarContentType = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
}

// avatarKey is a fixed per-user key, not one per upload (unlike upload.go's content-hashed
// raw/{userID}/{hash}{ext} keys) — a re-upload overwrites the previous avatar in place, so
// there is exactly one blob per user ever, with nothing orphaned left behind to clean up.
func avatarKey(userID string) string {
	return "avatars/" + userID
}

// handleUploadAvatar serves `POST /v1/account/avatar` (multipart, field "file"). Sniffs the
// real content type via http.DetectContentType rather than trusting the filename extension
// or the multipart part's own declared Content-Type — both are entirely client-controlled,
// and this sniffed value is exactly what handleGetAvatar serves the file back as, so trusting
// the client here would let an upload masquerade as an image type it isn't.
func (s *Server) handleUploadAvatar(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())

	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBytes+1<<20) // +1MiB of multipart overhead
	if err := r.ParseMultipartForm(maxAvatarBytes); err != nil {
		http.Error(w, "file too large or malformed multipart body", http.StatusRequestEntityTooLarge)
		return
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		http.Error(w, `expected a multipart field named "file"`, http.StatusBadRequest)
		return
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxAvatarBytes))
	if err != nil {
		http.Error(w, "failed reading upload", http.StatusBadRequest)
		return
	}
	contentType := http.DetectContentType(data)
	if !allowedAvatarContentType[contentType] {
		http.Error(w, fmt.Sprintf("unsupported image type %q (want PNG, JPEG or WebP)", contentType), http.StatusUnsupportedMediaType)
		return
	}

	ctx := r.Context()
	key := avatarKey(userID)
	if err := s.store.Put(ctx, key, bytes.NewReader(data), int64(len(data))); err != nil {
		s.log.Error("avatar upload failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE users SET avatar_key = $2, avatar_content_type = $3, avatar_updated_at = NOW()
		WHERE id = $1
	`, userID, key, contentType); err != nil {
		s.log.Error("avatar metadata update failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp, err := s.loadAuthResponse(ctx, userID)
	if err != nil {
		s.log.Error("avatar response lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleDeleteAvatar serves `DELETE /v1/account/avatar`. Object removal is best-effort and
// logged rather than surfaced as a failure — the same pattern storage.Store.RemoveByPrefix's
// callers already use — since a stray orphaned blob is a cleanup nuisance, not a reason to
// leave the DB still pointing at an avatar the user just asked to remove.
func (s *Server) handleDeleteAvatar(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	ctx := r.Context()

	if err := s.store.Remove(ctx, avatarKey(userID)); err != nil {
		s.log.Error("avatar removal failed", "err", err)
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE users SET avatar_key = NULL, avatar_content_type = NULL, avatar_updated_at = NULL
		WHERE id = $1
	`, userID); err != nil {
		s.log.Error("avatar metadata clear failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	resp, err := s.loadAuthResponse(ctx, userID)
	if err != nil {
		s.log.Error("avatar response lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleGetAvatar serves `GET /v1/account/avatar` — always the signed-in caller's own
// avatar, never anyone else's: there is no userID in the path at all, since this app has no
// social/public-profile surface for one account's avatar to be visible from another's
// session (VISION.md §5.5 — social is explicitly unscheduled).
func (s *Server) handleGetAvatar(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	ctx := r.Context()

	var avatarKeyVal, contentType *string
	if err := s.pool.QueryRow(ctx,
		`SELECT avatar_key, avatar_content_type FROM users WHERE id = $1`, userID,
	).Scan(&avatarKeyVal, &contentType); err != nil {
		s.log.Error("avatar lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if avatarKeyVal == nil {
		http.Error(w, "no avatar set", http.StatusNotFound)
		return
	}

	obj, err := s.store.Get(ctx, *avatarKeyVal)
	if err != nil {
		s.log.Error("avatar read failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer obj.Close()

	if contentType != nil {
		w.Header().Set("Content-Type", *contentType)
	}
	// Safe to cache aggressively: loadAuthResponse's avatar_url carries a ?v=<updated_at>
	// param that changes on every re-upload, so a stale cached copy at an old URL is simply
	// never requested again once the avatar actually changes.
	w.Header().Set("Cache-Control", "private, max-age=604800")
	if _, err := io.Copy(w, obj); err != nil {
		s.log.Error("avatar stream failed", "err", err)
	}
}
