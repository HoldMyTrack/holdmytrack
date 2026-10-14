package httpapi

import (
	"context"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/web"
)

// The per-account limits (ADR-0043, IMPLEMENTATION.md §5.7): how much one account may keep,
// beside the rate limits on how fast it may add (rate_limits.go). Both sit far above anything a
// person records, so only a script or a runaway import ever meets them, and one limit holds for
// every account: a raised one for some would be a tier (ADR-0031).
const (
	// maxActivitiesPerAccount is about nine activities a day for fifteen years.
	maxActivitiesPerAccount = 50_000
	// maxOriginalBytesPerAccount bounds the originals an account's activities keep (5 GB,
	// decimal, as the Settings page words it), which the count alone doesn't: 16 MiB files.
	maxOriginalBytesPerAccount = 5_000_000_000
)

// accountUsage is what an account holds against its limits, counting the ingest jobs still
// waiting as if they had landed, so a large import can't slip past the check while it's queued.
type accountUsage struct {
	activities    int64
	originalBytes int64
}

// usageQuery reads accountUsage. An original shared by split pieces counts once; one whose
// size isn't recorded (before migration 0039's backfill) counts nothing. idx_jobs_user serves
// the jobs half, the activities' user index the rest.
const usageQuery = `
	SELECT
		(SELECT count(*) FROM activities WHERE user_id = $1)
		+ (SELECT count(*) FROM jobs WHERE user_id = $1 AND kind = 'ingest' AND state = 'pending'),
		(SELECT coalesce(sum(b), 0) FROM (
			SELECT max(raw_bytes) AS b FROM activities
			WHERE user_id = $1 AND raw_payload_key IS NOT NULL GROUP BY raw_payload_key
		) o)
		+ (SELECT coalesce(sum(coalesce((payload->>'raw_bytes')::bigint, octet_length(raw), 0)), 0)
		   FROM jobs WHERE user_id = $1 AND kind = 'ingest' AND state = 'pending')`

func readUsage(ctx context.Context, db *pgxpool.Pool, userID string) (accountUsage, error) {
	var u accountUsage
	if err := db.QueryRow(ctx, usageQuery, userID).Scan(&u.activities, &u.originalBytes); err != nil {
		return u, fmt.Errorf("account usage: %w", err)
	}
	return u, nil
}

// Which limit refused something, as the error message key that names it.
const (
	limitActivities = "error.activity_limit"
	limitStorage    = "error.storage_limit"
)

// fitWithinLimits reports how many of the items arriving, in order and each a new activity of
// the given original size, fit under the limits on top of u, and for the first that doesn't,
// which limit refused it ("" when they all fit). Checked before the jobs are queued, so two
// requests at once can each fit and together go a little over; the limits are far enough above
// any person's use for that not to matter.
func fitWithinLimits(u accountUsage, sizes []int64) (n int, refused string) {
	for _, size := range sizes {
		switch {
		case u.activities+1 > maxActivitiesPerAccount:
			return n, limitActivities
		case u.originalBytes+size > maxOriginalBytesPerAccount:
			return n, limitStorage
		}
		u.activities++
		u.originalBytes += size
		n++
	}
	return n, ""
}

// refuseOverLimit answers 409 with the limit's message and reports true when the account has no
// room for one more activity of size bytes (0 for one whose size isn't known yet, a Story copy).
// A failed read is a 500.
func (s *Server) refuseOverLimit(w http.ResponseWriter, r *http.Request, userID string, size int64) bool {
	u, err := readUsage(r.Context(), s.pool, userID)
	if err != nil {
		s.log.Error("account limits: read usage failed", "err", err)
		writeAPIError(w, "internal error", http.StatusInternalServerError)
		return true
	}
	if _, refused := fitWithinLimits(u, []int64{size}); refused != "" {
		writeLimitError(w, r, refused)
		return true
	}
	return false
}

func writeLimitError(w http.ResponseWriter, r *http.Request, refused string) {
	writeError(w, http.StatusConflict, "account_limit", limitMessage(i18n.Get(requestLang(r)), refused))
}

// usageLine is Settings' "1,240 of 50,000 activities · 310 MB of 5 GB of original files".
func usageLine(l *i18n.Localizer, u accountUsage) string {
	return l.T("settings.usage_line",
		"activities", l.Int(u.activities), "max_activities", l.Int(maxActivitiesPerAccount),
		"storage", web.FormatBytes(l, u.originalBytes), "max_storage", maxStorageText(l))
}

func maxStorageText(l *i18n.Localizer) string {
	return l.T("unit.size.gb", "v", l.Int(maxOriginalBytesPerAccount/1e9))
}

// limitMessage is refused's message, naming its limit in l's language ("50,000", "5 GB").
func limitMessage(l *i18n.Localizer, refused string) string {
	if refused == limitActivities {
		return l.T(limitActivities, "max", l.Int(maxActivitiesPerAccount))
	}
	return l.T(limitStorage, "max", maxStorageText(l))
}
