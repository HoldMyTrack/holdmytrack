package httpapi

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/web"
)

// The Sync page (IMPLEMENTATION.md §4.0.1, docs/SPEC.md FR-3.4): every finished import, newest
// first — uploaded files, archives' files and phone syncs alike, each Ready with its date and
// distance and a link to it on the map, or Failed with why — then the duplicates cross-source
// dedup took out of circulation (FR-3.7). What's still being processed is the header's Upload
// menu's, not this page's, so the page never changes while it's open and needs no script.
// Opening it counts every failure so far as seen (users.imports_seen_at).

const syncPageSize = 20

// syncHistoryQuery is uploadsListQuery's finished rows only, most recently finished first.
const syncHistoryQuery = `
SELECT j.payload->>'source_detail', j.payload->>'source',
       j.state, j.last_error, j.error_code, a.started_at, a.distance_meters, a.id
FROM jobs j
LEFT JOIN activities a
  ON a.user_id = j.user_id AND a.source = j.payload->>'source' AND a.external_id = j.payload->>'external_id'
WHERE j.kind = 'ingest' AND j.user_id = $1 AND j.state IN ('done', 'failed')
ORDER BY j.finished_at DESC NULLS LAST, j.id DESC
LIMIT $2 OFFSET $3`

const syncHistoryCountQuery = `
SELECT COUNT(*) FROM jobs WHERE kind = 'ingest' AND user_id = $1 AND state IN ('done', 'failed')`

// syncView is what templates/pages/sync.html reads from PageData.Page.
type syncView struct {
	IsDemo     bool
	Rows       []syncRow
	Range      string // the pager's "1–20 of 57", "" with nothing imported
	NewerHref  string // "" on the first page
	OlderHref  string // "" on the last
	Duplicates []syncDuplicate
}

type syncRow struct {
	Title  string
	Failed bool
	// Detail is a Ready row's "Sep 26 · 4.1 mi", or a Failed row's reason.
	Detail string
	// MapHref opens the map on the activity (`/?activity=…&day=…`), "" when there is none.
	MapHref string
}

type syncDuplicate struct {
	When string // "Sep 26, 13:31 · 4.1 mi"
	From string // "From Health Connect — replaced by the copy from an uploaded file."
}

// GET /sync.
func (s *Server) handleSyncPage(w http.ResponseWriter, r *http.Request) {
	acct := s.pageAccount(r)
	if acct == nil {
		http.Redirect(w, r, "/signin", http.StatusSeeOther)
		return
	}
	if home := acct.home(); home != "/" {
		http.Redirect(w, r, home, http.StatusSeeOther)
		return
	}
	offset := 0
	if v, err := strconv.Atoi(r.URL.Query().Get("offset")); err == nil && v > 0 {
		offset = v
	}
	lang := pageLang(acct, r)
	l := i18n.Get(lang)
	view, err := s.buildSync(r.Context(), l, acct, offset)
	if err != nil {
		s.log.Error("sync page failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	// Seen once rendered: every failure listed here has now been looked at.
	if !acct.info.isDemo {
		if err := s.markImportsSeen(r, acct.info.userID); err != nil {
			s.log.Error("mark imports seen failed", "err", err)
		}
	}
	s.pages.Render(w, http.StatusOK, "sync", web.PageData{Title: l.T("meta.sync_title"), Path: "/sync", NoIndex: true, User: acct.user, Page: view, Lang: lang})
}

func (s *Server) buildSync(ctx context.Context, l *i18n.Localizer, acct *pageAccount, offset int) (syncView, error) {
	userID := acct.info.userID
	loc, err := time.LoadLocation(acct.info.timezone)
	if err != nil {
		loc = time.UTC
	}
	imperial := web.Imperial(acct.profile.Country)
	view := syncView{IsDemo: acct.info.isDemo}

	var total int
	if err := s.pool.QueryRow(ctx, syncHistoryCountQuery, userID).Scan(&total); err != nil {
		return view, err
	}
	rows, err := s.pool.Query(ctx, syncHistoryQuery, userID, syncPageSize, offset)
	if err != nil {
		return view, err
	}
	defer rows.Close()
	for rows.Next() {
		var filename, source, state string
		var lastError, errorCode, activityID *string
		var startedAt *time.Time
		var distance *float64
		if err := rows.Scan(&filename, &source, &state, &lastError, &errorCode, &startedAt, &distance, &activityID); err != nil {
			return view, err
		}
		row := syncRow{Title: importTitle(l, source, "", filename), Failed: state == "failed"}
		switch {
		case row.Failed:
			row.Detail = jobErrorMessage(l, errorCode, lastError)
		case startedAt != nil:
			day := startedAt.In(loc).Format("2006-01-02")
			row.Detail = web.ShortDate(l, day)
			if distance != nil {
				row.Detail += " · " + web.FormatDistance(l, *distance, imperial)
			}
			if activityID != nil {
				row.MapHref = "/?" + url.Values{"activity": {*activityID}, "day": {day}}.Encode()
			}
		}
		view.Rows = append(view.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return view, err
	}

	if total > 0 && offset < total {
		end := min(offset+syncPageSize, total)
		if end == offset+1 {
			view.Range = l.T("sync.range_one", "n", l.Int(int64(end)), "total", l.Int(int64(total)))
		} else {
			view.Range = l.T("sync.range", "from", l.Int(int64(offset+1)), "to", l.Int(int64(end)), "total", l.Int(int64(total)))
		}
		if offset > 0 {
			view.NewerHref = "/sync"
			if prev := offset - syncPageSize; prev > 0 {
				view.NewerHref += "?offset=" + strconv.Itoa(prev)
			}
		}
		if end < total {
			view.OlderHref = "/sync?offset=" + strconv.Itoa(end)
		}
	}

	dups, err := s.pool.Query(ctx, duplicatesQuery, userID)
	if err != nil {
		return view, err
	}
	defer dups.Close()
	for dups.Next() {
		var d duplicateRow
		if err := dups.Scan(&d.ID, &d.StartedAt, &d.ActivityType, &d.DistanceMeters, &d.Source,
			&d.SupersededBy.ID, &d.SupersededBy.Source, &d.SupersededBy.StartedAt); err != nil {
			return view, err
		}
		when := web.LocalTime(l, d.StartedAt, acct.info.timezone)
		if d.DistanceMeters != nil {
			when += " · " + web.FormatDistance(l, *d.DistanceMeters, imperial)
		}
		view.Duplicates = append(view.Duplicates, syncDuplicate{
			When: when,
			From: l.T("sync.duplicate_from", "source", sourcePhrase(l, d.Source), "kept", sourcePhrase(l, d.SupersededBy.Source)),
		})
	}
	return view, dups.Err()
}

// sourcePhrase is a source said in a sentence — "an uploaded file", "Health Connect" — the web
// app's formatIngestSource and the Android app's sourceName().
func sourcePhrase(l *i18n.Localizer, source string) string {
	switch source {
	case "upload", "takeout", "healthconnect", "healthkit", "recorded":
		return l.T("imports.source_phrase." + source)
	default:
		return source
	}
}
