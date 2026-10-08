// Package storycopy copies a Story into another account — the `story_copy` job behind sending
// a copy of a Story (IMPLEMENTATION.md §4.23, ADR-0036). Its own package because it writes
// each activity out with export.WriteGPX and reads it back in through ingest.Process, and
// export already imports ingest.
package storycopy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/export"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// Job is the `story_copy` job's payload: copy the Story, as it is when the job runs, into the
// recipient's account. Accepting a sent copy enqueues it (httpapi's handleAcceptStorySend).
type Job struct {
	StoryID     string `json:"story_id"`
	RecipientID string `json:"recipient_id"`
}

// Source is activities.source for an activity a copy made.
const Source = "story"

// retryAfter is how long a copy waits before trying again the activities it skipped because
// their owner had an edit still being applied.
const retryAfter = time.Minute

// Querier is what Enqueue runs on: the pool, or the transaction taking the copy out of the
// recipient's inbox.
type Querier = ingest.Querier

// Enqueue adds a `story_copy` job for the recipient, to run after delay.
func Enqueue(ctx context.Context, db Querier, job Job, delay time.Duration) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `
		INSERT INTO jobs (kind, user_id, payload, run_after)
		VALUES ('story_copy', $1, $2, NOW() + make_interval(secs => $3))`,
		job.RecipientID, payload, delay.Seconds())
	return err
}

// source is one activity of the Story being copied, and the original it stands for.
type source struct {
	id, origin, activityType string
	name, description        *string
	pending                  bool
}

// Process copies the Story into the recipient's account (ADR-0036):
//
//   - into the Story an earlier copy of the same Story made for them (story_copies), or a new
//     one with the Story's name and description when there's none, or it was deleted;
//   - each live activity the recipient hasn't received before — an original is received once,
//     ever (received_origins), and one they recorded themselves, or hold a copy of, is skipped;
//   - from the points the sender sees (ingest.DisplayedPoints: their Private locations and track
//     edit applied), written as GPX and run through the ordinary ingest as the recipient's
//     activity, so the recipient's own Private locations and duplicate check apply on top;
//   - with its name, type and description, and its photos as the recipient's rows over the same
//     image files.
//
// An activity with no visible track is left out. One whose owner has an edit still being
// applied is skipped for now, and the job enqueues itself again for it; a copy with nothing
// else to add makes no Story until then. Every step is safe to
// repeat: the ingest is idempotent on (recipient, 'story', origin), and the rest are inserts
// that ignore what's already there, so a copy cut off partway finishes on the next run.
//
// A Story deleted meanwhile, or one whose owner is being deleted, copies nothing.
func Process(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, job Job) error {
	var name string
	var description *string
	err := pool.QueryRow(ctx, `
		SELECT st.name, st.description FROM stories st JOIN users u ON u.id = st.user_id
		WHERE st.id = $1 AND u.deleted_at IS NULL`, job.StoryID).Scan(&name, &description)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("story copy: story: %w", err)
	}

	sources, err := sourcesToCopy(ctx, pool, job)
	if err != nil {
		return err
	}
	ready := 0
	for _, src := range sources {
		if !src.pending {
			ready++
		}
	}
	if ready == 0 && len(sources) > 0 {
		// Everything left to copy waits on an edit: no Story until there's something in it.
		return Enqueue(ctx, pool, job, retryAfter)
	}
	target, err := targetStory(ctx, pool, job, name, description, ready > 0)
	if err != nil || target == "" {
		return err
	}

	retry, added := ready < len(sources), false
	for _, src := range sources {
		if src.pending {
			continue
		}
		copied, err := copyActivity(ctx, pool, store, job.RecipientID, target, src)
		if err != nil {
			return fmt.Errorf("story copy: activity %s: %w", src.id, err)
		}
		added = added || copied
	}
	if added {
		if _, err := pool.Exec(ctx, `UPDATE stories SET updated_at = NOW() WHERE id = $1`, target); err != nil {
			return fmt.Errorf("story copy: touch story: %w", err)
		}
		// The new memberships change what a `?story=` tracks tile returns (§4.23).
		if err := fog.BumpMapVersion(ctx, pool, job.RecipientID); err != nil {
			return fmt.Errorf("story copy: bump map version: %w", err)
		}
	}
	if retry {
		return Enqueue(ctx, pool, job, retryAfter)
	}
	return nil
}

// sourcesToCopy lists the Story's live activities with a track, and a raw payload to rebuild it
// from, that the recipient has neither received, recorded nor holds a copy of, oldest first.
func sourcesToCopy(ctx context.Context, pool *pgxpool.Pool, job Job) ([]source, error) {
	rows, err := pool.Query(ctx, `
		SELECT a.id, COALESCE(a.origin_id, a.id), a.activity_type, a.name, a.description, a.edit_pending
		FROM story_activities sa
		JOIN activities a ON a.id = sa.activity_id
		WHERE sa.story_id = $1 AND a.superseded_by IS NULL AND a.trajectory IS NOT NULL AND a.raw_payload_key IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM received_origins r WHERE r.user_id = $2 AND r.origin_id = COALESCE(a.origin_id, a.id))
		  AND NOT EXISTS (SELECT 1 FROM activities mine
		                  WHERE mine.user_id = $2 AND (mine.id = COALESCE(a.origin_id, a.id) OR mine.origin_id = COALESCE(a.origin_id, a.id)))
		ORDER BY a.started_at, a.id`, job.StoryID, job.RecipientID)
	if err != nil {
		return nil, fmt.Errorf("story copy: activities: %w", err)
	}
	sources, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (source, error) {
		var s source
		err := row.Scan(&s.id, &s.origin, &s.activityType, &s.name, &s.description, &s.pending)
		return s, err
	})
	if err != nil {
		return nil, fmt.Errorf("story copy: activities: %w", err)
	}
	return sources, nil
}

// targetStory is the recipient's Story an earlier copy of this one made, or a new one with its
// name and description. With nothing to copy, a missing Story is made only when the Story being
// copied is itself empty — accepting an empty Story gives an empty Story — and otherwise the
// answer is "", since the recipient already has everything in it.
func targetStory(ctx context.Context, pool *pgxpool.Pool, job Job, name string, description *string, anything bool) (string, error) {
	var target string
	err := pool.QueryRow(ctx, `SELECT copy_story_id FROM story_copies WHERE source_story_id = $1 AND recipient_id = $2`,
		job.StoryID, job.RecipientID).Scan(&target)
	if err == nil {
		return target, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("story copy: earlier copy: %w", err)
	}
	if !anything {
		var empty bool
		if err := pool.QueryRow(ctx, `SELECT NOT EXISTS (SELECT 1 FROM story_activities WHERE story_id = $1)`, job.StoryID).Scan(&empty); err != nil {
			return "", fmt.Errorf("story copy: empty: %w", err)
		}
		if !empty {
			return "", nil
		}
	}
	err = pool.QueryRow(ctx, `
		WITH st AS (INSERT INTO stories (user_id, name, description) VALUES ($2, $3, $4) RETURNING id)
		INSERT INTO story_copies (source_story_id, recipient_id, copy_story_id)
		SELECT $1, $2, id FROM st
		RETURNING copy_story_id`, job.StoryID, job.RecipientID, name, description).Scan(&target)
	if err != nil {
		return "", fmt.Errorf("story copy: new story: %w", err)
	}
	return target, nil
}

// copyActivity makes the recipient's copy of src in target, reporting whether it added one. An
// activity whose track the sender's Private locations hide entirely adds nothing.
func copyActivity(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, recipientID, target string, src source) (bool, error) {
	points, err := ingest.DisplayedPoints(ctx, pool, store, src.id)
	if err != nil {
		return false, err
	}
	if points == nil {
		return false, nil
	}
	var gpx bytes.Buffer
	if err := export.WriteGPX(&gpx, src.activityType, points); err != nil {
		return false, err
	}
	rawKey := fmt.Sprintf("raw/%s/%s/%s.gpx", recipientID, Source, src.origin)
	if err := store.Put(ctx, rawKey, bytes.NewReader(gpx.Bytes()), int64(gpx.Len())); err != nil {
		return false, err
	}
	label := src.activityType
	if src.name != nil && *src.name != "" {
		label = *src.name
	}
	res, err := ingest.Process(ctx, pool, store, ingest.Job{
		UserID:        recipientID,
		Source:        Source,
		SourceDetail:  export.Slug(label) + ".gpx",
		ExternalID:    src.origin,
		RawPayloadKey: rawKey,
		ActivityType:  src.activityType,
	})
	if err != nil {
		return false, err
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		return false, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed
	if _, err := tx.Exec(ctx, `UPDATE activities SET origin_id = $2, name = $3, description = $4 WHERE id = $1`,
		res.ActivityID, src.origin, src.name, src.description); err != nil {
		return false, err
	}
	// FOR KEY SHARE holds the sender's photo rows until commit, so a photo deleted meanwhile
	// waits, and then finds this copy's row naming its files and leaves them
	// (ingest.RemoveUnreferencedPhotoFiles).
	if _, err := tx.Exec(ctx, `
		WITH src AS (SELECT * FROM activity_photos WHERE activity_id = $1 FOR KEY SHARE)
		INSERT INTO activity_photos (user_id, activity_id, taken_at, route_at, content_type, thumb_content_type, width, height, bytes, caption, image_key)
		SELECT $2, $3, taken_at, route_at, content_type, thumb_content_type, width, height, bytes, caption, image_key FROM src
		WHERE NOT EXISTS (SELECT 1 FROM activity_photos p WHERE p.activity_id = $3 AND p.image_key = src.image_key)`,
		src.id, recipientID, res.ActivityID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO story_activities (story_id, activity_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		target, res.ActivityID); err != nil {
		return false, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO received_origins (user_id, origin_id) VALUES ($1, $2) ON CONFLICT DO NOTHING`,
		recipientID, src.origin); err != nil {
		return false, err
	}
	return true, tx.Commit(ctx)
}
