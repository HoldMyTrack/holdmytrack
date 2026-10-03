package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/export"
)

// Downloading your data (SPEC FR-1.12, IMPLEMENTATION.md §4.29): a request makes an exports
// row and an `export` job (internal/worker/export_job.go), which builds the archive and emails
// the owner; these endpoints say where it stands and serve its parts. The Settings page
// (settings_page.go) shares requestExport and latestExport.

// exportStatus is an account's latest export, as GET /v1/account/export answers and the
// Settings page shows it.
type exportStatus struct {
	// State is "none" (never asked, or the last one expired and was removed), "preparing",
	// "ready" or "failed".
	State       string       `json:"state"`
	ID          string       `json:"id,omitempty"`
	RequestedAt *time.Time   `json:"requested_at,omitempty"`
	ExpiresAt   *time.Time   `json:"expires_at,omitempty"`
	TotalSize   int64        `json:"total_size,omitempty"`
	Parts       []exportPart `json:"parts,omitempty"`
}

type exportPart struct {
	N    int    `json:"n"`
	Size int64  `json:"size"`
	Name string `json:"name"` // the file it downloads as
	URL  string `json:"url"`  // relative to the site
}

func exportPartURL(exportID string, n int) string {
	return fmt.Sprintf("/v1/account/exports/%s/parts/%d", exportID, n)
}

// latestExport is the account's most recent export request, or State "none".
func (s *Server) latestExport(ctx context.Context, userID string) (exportStatus, error) {
	var st exportStatus
	var requested time.Time
	var ready, expires *time.Time
	var sizes []int64
	var jobState *string
	err := s.pool.QueryRow(ctx, `
		SELECT e.id, e.requested_at, e.ready_at, e.expires_at, e.part_sizes, j.state
		FROM exports e LEFT JOIN jobs j ON j.id = e.job_id
		WHERE e.user_id = $1 ORDER BY e.requested_at DESC LIMIT 1`, userID,
	).Scan(&st.ID, &requested, &ready, &expires, &sizes, &jobState)
	if errors.Is(err, pgx.ErrNoRows) {
		return exportStatus{State: "none"}, nil
	}
	if err != nil {
		return exportStatus{}, fmt.Errorf("latest export: %w", err)
	}
	st.RequestedAt = &requested
	switch {
	case ready != nil && expires != nil && expires.After(time.Now()):
		st.State = "ready"
		st.ExpiresAt = expires
		for i, size := range sizes {
			n := i + 1
			st.Parts = append(st.Parts, exportPart{N: n, Size: size, Name: export.FileName(requested, n, len(sizes)), URL: exportPartURL(st.ID, n)})
			st.TotalSize += size
		}
	case ready != nil:
		return exportStatus{State: "none"}, nil // expired, waiting for the sweep
	case jobState != nil && *jobState == "pending":
		st.State = "preparing"
	default:
		st.State = "failed"
	}
	return st, nil
}

// requestExport asks for a new export, unless one is already being prepared, which it
// returns instead. A new one ends any earlier one's downloads at once (the worker's sweep
// removes its parts). lang is the language its email and README are written in.
func (s *Server) requestExport(ctx context.Context, userID string, isDemo bool, lang string) (exportStatus, error) {
	if isDemo {
		return exportStatus{}, accountFailure(http.StatusForbidden, "error.demo_read_only")
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return exportStatus{}, fmt.Errorf("request export: begin: %w", err)
	}
	defer tx.Rollback(ctx)
	// The account's row as a lock: two requests at once make one export, not two.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, userID); err != nil {
		return exportStatus{}, fmt.Errorf("request export: lock: %w", err)
	}
	var preparing bool
	if err := tx.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM exports e JOIN jobs j ON j.id = e.job_id
		               WHERE e.user_id = $1 AND e.ready_at IS NULL AND j.state = 'pending')`, userID,
	).Scan(&preparing); err != nil {
		return exportStatus{}, fmt.Errorf("request export: check: %w", err)
	}
	if !preparing {
		if _, err := tx.Exec(ctx, `UPDATE exports SET expires_at = NOW() WHERE user_id = $1 AND ready_at IS NOT NULL`, userID); err != nil {
			return exportStatus{}, fmt.Errorf("request export: expire old: %w", err)
		}
		var exportID string
		if err := tx.QueryRow(ctx, `INSERT INTO exports (user_id, lang) VALUES ($1, $2) RETURNING id`, userID, lang).Scan(&exportID); err != nil {
			return exportStatus{}, fmt.Errorf("request export: insert: %w", err)
		}
		payload, _ := json.Marshal(map[string]string{"export_id": exportID})
		if _, err := tx.Exec(ctx, `
			WITH j AS (INSERT INTO jobs (kind, user_id, payload) VALUES ('export', $1, $2) RETURNING id)
			UPDATE exports SET job_id = (SELECT id FROM j) WHERE id = $3`, userID, payload, exportID); err != nil {
			return exportStatus{}, fmt.Errorf("request export: enqueue: %w", err)
		}
		s.log.Info("export requested", "export_id", exportID, "user_id", userID)
	}
	if err := tx.Commit(ctx); err != nil {
		return exportStatus{}, fmt.Errorf("request export: commit: %w", err)
	}
	return s.latestExport(ctx, userID)
}

// POST /v1/account/export.
func (s *Server) handleRequestExport(w http.ResponseWriter, r *http.Request) {
	info := authInfoFromContext(r.Context())
	st, err := s.requestExport(r.Context(), info.userID, info.isDemo, requestLang(r))
	if err != nil {
		s.writeAccountError(w, r, "request export", err)
		return
	}
	writeJSON(w, http.StatusAccepted, st)
}

// GET /v1/account/export.
func (s *Server) handleGetExport(w http.ResponseWriter, r *http.Request) {
	st, err := s.latestExport(r.Context(), userIDFromContext(r.Context()))
	if err != nil {
		s.log.Error("get export failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// GET /v1/account/exports/{id}/parts/{n} — one part, while its export is ready and the
// caller's. Served with http.ServeContent, so a dropped download resumes with a Range request.
func (s *Server) handleDownloadExportPart(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := userIDFromContext(ctx)
	exportID := r.PathValue("id")
	n, err := strconv.Atoi(r.PathValue("n"))
	if !uuidPattern.MatchString(exportID) || err != nil || n < 1 {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	var requested time.Time
	var sizes []int64
	err = s.pool.QueryRow(ctx, `
		SELECT requested_at, part_sizes FROM exports
		WHERE id = $1 AND user_id = $2 AND ready_at IS NOT NULL AND expires_at > NOW()`, exportID, userID,
	).Scan(&requested, &sizes)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && n > len(sizes)) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	if err != nil {
		s.log.Error("export download lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	obj, err := s.store.Open(ctx, export.PartKey(userID, exportID, n))
	if err != nil {
		s.log.Error("export download open failed", "export_id", exportID, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	defer obj.Close()
	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+export.FileName(requested, n, len(sizes))+`"`)
	w.Header().Set("Cache-Control", "private, no-store")
	http.ServeContent(w, r, "", requested, obj)
}
