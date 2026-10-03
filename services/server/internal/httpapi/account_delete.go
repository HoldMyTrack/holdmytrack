package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// deleteAccountRequest is DELETE /v1/account's body: the account's own email address, typed
// by the person deleting it, as the confirmation.
type deleteAccountRequest struct {
	Email string `json:"email"`
}

// handleDeleteAccount serves `DELETE /v1/account` (SPEC FR-1.11): it closes the caller's
// account and ends the request's session; the worker's sweep removes the data (closeAccount).
// Behind requireAuth, not requireVerified: an account whose email was never confirmed can be
// deleted too.
func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	var req deleteAccountRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	info := authInfoFromContext(r.Context())
	if err := s.closeAccount(r.Context(), info.userID, info.isDemo, req.Email); err != nil {
		s.writeAccountError(w, r, "delete account", err)
		return
	}
	s.endSession(w, r)
	w.WriteHeader(http.StatusNoContent)
}

// closeAccount is account deletion's shared core — the JSON endpoint above and the Settings
// page's form (settings_page.go) both call it. confirm must be the account's email, in any
// case and with surrounding spaces. In one transaction it takes away every way back in —
// sessions, Google and Facebook identities, reset, verification and app sign-in tokens, the
// password — renames the email to a placeholder so the address can sign up again at once,
// and stamps deleted_at. The data stays until internal/worker/account_purge.go removes it,
// within a minute or so: a job of the account's may be running, and its objects have to be
// swept after the last one finishes, not before.
func (s *Server) closeAccount(ctx context.Context, userID string, isDemo bool, confirm string) error {
	if isDemo {
		return accountFailure(http.StatusForbidden, "error.demo_read_only")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("delete account: begin: %w", err)
	}
	defer tx.Rollback(ctx)

	var email string
	if err := tx.QueryRow(ctx, `SELECT email FROM users WHERE id = $1 AND deleted_at IS NULL FOR UPDATE`, userID).Scan(&email); err != nil {
		return fmt.Errorf("delete account: lookup: %w", err)
	}
	if strings.ToLower(strings.TrimSpace(confirm)) != email {
		return accountFailure(http.StatusBadRequest, "error.delete_confirm_mismatch")
	}
	for _, q := range []string{
		`DELETE FROM sessions WHERE user_id = $1`,
		`DELETE FROM user_identities WHERE user_id = $1`,
		`DELETE FROM password_resets WHERE user_id = $1`,
		`DELETE FROM email_verifications WHERE user_id = $1`,
		`DELETE FROM auth_handoffs WHERE user_id = $1`,
		`UPDATE users SET email = 'deleted-' || id || '@holdmytrack.invalid', password_hash = NULL, deleted_at = NOW() WHERE id = $1`,
	} {
		if _, err := tx.Exec(ctx, q, userID); err != nil {
			return fmt.Errorf("delete account: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("delete account: commit: %w", err)
	}
	s.log.Info("account closed for deletion", "user_id", userID)
	return nil
}
