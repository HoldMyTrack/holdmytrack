package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// RawItem is one file or synced activity a request hands to the worker: its bytes as they
// arrived, and what the `ingest` job needs to know about them.
type RawItem struct {
	// "upload" (an uploaded file) or a phone sync's source — the
	// `activities.source` column's own provenance value, not just a label.
	Source   string
	Filename string
	Ext      string
	Data     []byte
	// ExternalID, when set, is the idempotency key as given instead of a content hash of
	// Data. Only a phone sync sets it: its activities carry the platform's own stable record
	// id, and hashing the synced JSON instead would mint a new activity on every retry that
	// reserialized a field differently. A file has no such id, so it's hashed.
	ExternalID string
	// Batch is Job's — set when one request enqueues many items.
	Batch string
}

// Enqueued is what EnqueueRaw did with one item.
type Enqueued struct {
	ExternalID string
	// AlreadyProcessed means the account already has this activity, fully ingested, so no job
	// was added. One whose ingest never finished isn't a repeat: enqueuing it again resumes it.
	AlreadyProcessed bool
}

// Querier is what EnqueueRaw runs on: the pool, or a transaction the caller commits.
type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// EnqueueRaw adds an `ingest` job for each item the account doesn't already have, with the
// item's bytes in the job's own `raw` column rather than in object storage: a request answers
// after two queries however many items it carries, and the worker moves the bytes to their
// raw key when it runs the job (PromoteRaw). Results are in the order of items.
//
// The existence check is the fast path of §4.0's idempotency invariant, not the guarantee —
// that is Process's ON CONFLICT DO NOTHING, which still holds when two identical uploads race
// past this check. It's scoped by source, matching activities' (user_id, source, external_id)
// unique index. The jobs go in with one INSERT, so either every new item is enqueued or none.
func EnqueueRaw(ctx context.Context, db Querier, userID string, items []RawItem) ([]Enqueued, error) {
	out := make([]Enqueued, len(items))
	if len(items) == 0 {
		return out, nil
	}
	sources := make([]string, len(items))
	ids := make([]string, len(items))
	keys := make([]string, len(items))
	for i, it := range items {
		sources[i] = it.Source
		if it.ExternalID != "" {
			ids[i] = it.ExternalID
			// Scoped by source, unlike the content-addressed key below: a caller-supplied id
			// is only unique within its own source's namespace (a Health Connect UUID and a
			// HealthKit UUID could coincide in theory), whereas a hash collision across
			// sources is intentionally shared storage (see handleDeleteActivity's comment).
			keys[i] = fmt.Sprintf("raw/%s/%s/%s%s", userID, it.Source, it.ExternalID, it.Ext)
		} else {
			sum := sha256.Sum256(it.Data)
			ids[i] = hex.EncodeToString(sum[:])
			keys[i] = fmt.Sprintf("raw/%s/%s%s", userID, ids[i], it.Ext)
		}
		out[i].ExternalID = ids[i]
	}

	rows, err := db.Query(ctx, `
		SELECT a.source, a.external_id FROM activities a
		JOIN unnest($2::text[], $3::text[]) AS t(source, external_id)
		  ON a.source = t.source AND a.external_id = t.external_id
		WHERE a.user_id = $1 AND a.ingest_complete`,
		userID, sources, ids)
	if err != nil {
		return nil, fmt.Errorf("ingest: dedupe check: %w", err)
	}
	have := map[[2]string]bool{}
	for rows.Next() {
		var k [2]string
		if err := rows.Scan(&k[0], &k[1]); err != nil {
			rows.Close()
			return nil, fmt.Errorf("ingest: dedupe check: %w", err)
		}
		have[k] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("ingest: dedupe check: %w", err)
	}

	var payloads []string
	var raws [][]byte
	for i, it := range items {
		if have[[2]string{sources[i], ids[i]}] {
			out[i].AlreadyProcessed = true
			continue
		}
		payload, err := json.Marshal(Job{
			UserID:        userID,
			Source:        it.Source,
			SourceDetail:  it.Filename,
			ExternalID:    ids[i],
			RawPayloadKey: keys[i],
			Batch:         it.Batch,
		})
		if err != nil {
			return nil, fmt.Errorf("ingest: job marshal: %w", err)
		}
		payloads = append(payloads, string(payload))
		raws = append(raws, it.Data)
	}
	if len(payloads) == 0 {
		return out, nil
	}
	if _, err := db.Exec(ctx, `
		INSERT INTO jobs (kind, user_id, payload, raw)
		SELECT 'ingest', $1, p::jsonb, r FROM unnest($2::text[], $3::bytea[]) AS t(p, r)`,
		userID, payloads, raws); err != nil {
		return nil, fmt.Errorf("ingest: enqueue: %w", err)
	}
	return out, nil
}

// KnownExternalIDs returns which of ids the account already has from source: an activity row
// in any state, or an ingest job still waiting to run. It answers a phone's "is this synced
// yet?" (IMPLEMENTATION.md §4.0.3's `POST /v1/sync/known`), so it counts everything a repeat
// sync would answer `already_processed` for, plus what's
// queued, so a session sent a moment ago doesn't come back while the worker catches up. A
// deleted activity's row is gone and its job finished, so its id isn't known any more.
func KnownExternalIDs(ctx context.Context, db Querier, userID, source string, ids []string) ([]string, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := db.Query(ctx, `
		SELECT external_id FROM activities
		WHERE user_id = $1 AND source = $2 AND external_id = ANY($3::text[])
		UNION
		SELECT payload->>'external_id' FROM jobs
		WHERE user_id = $1 AND kind = 'ingest' AND state = 'pending'
		  AND payload->>'source' = $2 AND payload->>'external_id' = ANY($3::text[])`,
		userID, source, ids)
	if err != nil {
		return nil, fmt.Errorf("ingest: known ids: %w", err)
	}
	known, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, fmt.Errorf("ingest: known ids: %w", err)
	}
	return known, nil
}

// PromoteRaw writes a job's inline raw payload (EnqueueRaw) to its raw key and clears it from
// the row, before the job is processed. A job enqueued with its payload already in object
// storage has none, and this does nothing. A retry after a failed write finds the bytes still
// in the row, and writing the same key twice is harmless.
func PromoteRaw(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, jobID int64, rawKey string) error {
	var raw []byte
	err := pool.QueryRow(ctx, `SELECT raw FROM jobs WHERE id = $1 AND raw IS NOT NULL`, jobID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("ingest: read inline raw payload: %w", err)
	}
	if err := store.Put(ctx, rawKey, bytes.NewReader(raw), int64(len(raw))); err != nil {
		return fmt.Errorf("ingest: raw payload upload: %w", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE jobs SET raw = NULL WHERE id = $1`, jobID); err != nil {
		return fmt.Errorf("ingest: clear inline raw payload: %w", err)
	}
	return nil
}
