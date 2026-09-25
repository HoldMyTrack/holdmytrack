package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
)

// The Settings page (Avatar, Name, Country, Timezone) — see
// migrations/0001_init.sql's users table (display_name/country/avatar_key/
// avatar_content_type/avatar_updated_at) and migrations/0021_user_timezone.sql (timezone).
// Name, Country and Timezone are covered by handleUpdateSettings; the avatar is a
// separate content type entirely, so it gets its own three endpoints below.

var countryCodePattern = regexp.MustCompile(`^[A-Z]{2}$`)

type updateSettingsRequest struct {
	DisplayName string `json:"display_name"`
	Country     string `json:"country"`
	Timezone    string `json:"timezone"`
}

// handleUpdateSettings serves `PATCH /v1/account/settings` — a full replace of all three
// fields at once, since the Settings page saves as one form rather than auto-saving each
// field individually. Returns the same authResponse shape handleMe does, so the frontend can
// update its local state directly from this response instead of a follow-up GET.
func (s *Server) handleUpdateSettings(w http.ResponseWriter, r *http.Request) {
	var req updateSettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	s.writeProfile(w, r, "update settings", s.saveSettings(r.Context(), userIDFromContext(r.Context()), req.DisplayName, req.Country, req.Timezone))
}

// saveSettings is the Settings save's shared core — handleUpdateSettings (JSON) and the
// /settings page (settings_page.go) both call it, and it does all the validating. The caller
// has already refused a demo session.
func (s *Server) saveSettings(ctx context.Context, userID, displayName, country, timezone string) error {
	displayName = strings.TrimSpace(displayName)
	country = strings.ToUpper(strings.TrimSpace(country))
	// Required: Country decides metric vs imperial everywhere, and the first-run gate (FR-1.7)
	// treats an empty one as "Settings never saved" — letting a save clear it would send the
	// account back through onboarding.
	if country == "" {
		return accountFailure(http.StatusBadRequest, "country is required")
	}
	if !countryCodePattern.MatchString(country) {
		return accountFailure(http.StatusBadRequest, "country must be a two-letter code")
	}
	// Unlike Country, timezone has no "unset" state (the column is NOT NULL DEFAULT 'UTC' —
	// migrations/0021_user_timezone.sql) — Settings always sends the field's current
	// selection, so an empty/unloadable value here means a malformed request, not a deliberate
	// clear, and is rejected rather than silently defaulted.
	tz, ok := normalizeTimezone(timezone)
	if !ok {
		return accountFailure(http.StatusBadRequest, "timezone must be a valid IANA zone name (e.g. America/New_York)")
	}
	// NULLIF, not a Go-side branch on "" — an empty display name means "unset", same as every
	// other nullable text column this API already treats that way.
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET display_name = NULLIF($2, ''), country = $3, timezone = $4
		WHERE id = $1
	`, userID, displayName, country, tz)
	return err
}

// writeProfile answers one of the account endpoints: err as writeAccountError does, or the
// account's current profile — the same authResponse shape handleMe returns, so the web app
// can update its local state from it instead of a follow-up GET.
func (s *Server) writeProfile(w http.ResponseWriter, r *http.Request, op string, err error) {
	if err != nil {
		s.writeAccountError(w, op, err)
		return
	}
	resp, err := s.loadAuthResponse(r.Context(), userIDFromContext(r.Context()))
	if err != nil {
		s.log.Error(op+" response lookup failed", "err", err)
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
	s.writeProfile(w, r, "avatar upload", s.setAvatar(w, r, userIDFromContext(r.Context())))
}

// setAvatar is the avatar upload's shared core (the JSON endpoint and the /settings page's
// avatar form, both multipart with the image in field "file").
func (s *Server) setAvatar(w http.ResponseWriter, r *http.Request, userID string) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxAvatarBytes+1<<20) // +1MiB of multipart overhead
	if err := r.ParseMultipartForm(maxAvatarBytes); err != nil {
		return accountFailure(http.StatusRequestEntityTooLarge, "file too large or malformed multipart body")
	}
	file, _, err := r.FormFile("file")
	if err != nil {
		return accountFailure(http.StatusBadRequest, `expected a multipart field named "file"`)
	}
	defer file.Close()

	data, err := io.ReadAll(io.LimitReader(file, maxAvatarBytes))
	if err != nil {
		return accountFailure(http.StatusBadRequest, "failed reading upload")
	}
	contentType := http.DetectContentType(data)
	if !allowedAvatarContentType[contentType] {
		return accountFailure(http.StatusUnsupportedMediaType, fmt.Sprintf("unsupported image type %q (want PNG, JPEG or WebP)", contentType))
	}

	ctx := r.Context()
	key := avatarKey(userID)
	if err := s.store.Put(ctx, key, bytes.NewReader(data), int64(len(data))); err != nil {
		return err
	}
	_, err = s.pool.Exec(ctx, `
		UPDATE users SET avatar_key = $2, avatar_content_type = $3, avatar_updated_at = NOW()
		WHERE id = $1
	`, userID, key, contentType)
	return err
}

// handleDeleteAvatar serves `DELETE /v1/account/avatar`. Object removal is best-effort and
// logged rather than surfaced as a failure — the same pattern storage.Store.RemoveByPrefix's
// callers already use — since a stray orphaned blob is a cleanup nuisance, not a reason to
// leave the DB still pointing at an avatar the user just asked to remove.
func (s *Server) handleDeleteAvatar(w http.ResponseWriter, r *http.Request) {
	s.writeProfile(w, r, "avatar removal", s.removeAvatar(r.Context(), userIDFromContext(r.Context())))
}

// removeAvatar is the avatar removal's shared core (the JSON endpoint and the /settings page).
func (s *Server) removeAvatar(ctx context.Context, userID string) error {
	if err := s.store.Remove(ctx, avatarKey(userID)); err != nil {
		s.log.Error("avatar removal failed", "err", err)
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE users SET avatar_key = NULL, avatar_content_type = NULL, avatar_updated_at = NULL
		WHERE id = $1
	`, userID)
	return err
}

// handleGetAvatar serves `GET /v1/account/avatar` — always the signed-in caller's own
// avatar, never anyone else's: there is no userID in the path at all, since this app has no
// social/public-profile surface for one account's avatar to be visible from another's
// session (VISION.md §5.7 — social is explicitly unscheduled).
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
