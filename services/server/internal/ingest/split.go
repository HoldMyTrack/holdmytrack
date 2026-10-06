package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/parse"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// SplitRange is the stretch of the recorded points one piece of a split activity covers
// (IMPLEMENTATION.md §4.7.8): activities.split_from and split_to, unix milliseconds, both ends
// inclusive, so the two pieces either side of a split share the point it was made at. A nil
// end is that end of the recording; an activity never split has both nil.
type SplitRange struct {
	From *int64
	To   *int64
}

// IsWhole reports whether the range covers the whole recording.
func (r SplitRange) IsWhole() bool { return r.From == nil && r.To == nil }

// contains reports whether a point timestamp lies inside the range.
func (r SplitRange) contains(t int64) bool {
	return (r.From == nil || t >= *r.From) && (r.To == nil || t <= *r.To)
}

// Apply returns the points inside the range, with its ends clipped against zones again. The
// points come in clipped already, but a split made inside a Private location — at home,
// between a ride there and a walk from there — leaves a new end inside it, the same way an
// edit's Chop can (TrackEdit.ApplyClipped).
func (r SplitRange) Apply(points []parse.Point, zones []Zone) []parse.Point {
	if r.IsWhole() {
		return points
	}
	out := make([]parse.Point, 0, len(points))
	for _, p := range points {
		if r.contains(p.Time.UnixMilli()) {
			out = append(out, p)
		}
	}
	return ClipEnds(out, zones)
}

// LoadPiecePoints is loadClippedPoints narrowed to one piece's range — the points the track
// editor shows and the split check counts — plus the zones they were clipped with. Nil points
// when the piece lies entirely inside Private locations.
func LoadPiecePoints(ctx context.Context, pool *pgxpool.Pool, store *storage.Store, userID, sourceDetail, rawKey string, rng SplitRange) ([]parse.Point, []Zone, error) {
	_, points, zones, err := loadClippedPoints(ctx, pool, store, userID, sourceDetail, rawKey)
	if err != nil || points == nil {
		return nil, zones, err
	}
	if points = rng.Apply(points, zones); len(points) < 2 {
		return nil, zones, nil
	}
	return points, zones, nil
}

// ErrSplitTooClose is a split leaving one side with less than a line.
var ErrSplitTooClose = errors.New("a split needs at least two points on each side")

// ErrSplitHidden is a split leaving one side entirely inside Private locations — a part saved
// with no track, as any activity entirely inside them is (FR-8.1), which comes back when the
// location moves. Allowed once the user has been told. Reachable from the editor, whose preview shows the points
// a track edit's second clip (TrackEdit.ApplyClipped) hides: a walk home ending in a stray fix
// outside the circle, that fix deleted, keeps every point inside the circle on show there.
var ErrSplitHidden = errors.New("one side of the split lies inside a private location")

// CheckSplit reports whether splitting at the point timestamped at leaves both pieces a track.
// points are the piece's own clipped points, before the edit. A side with fewer than two
// points once the edit is applied is too close (ErrSplitTooClose); one that has them, but
// loses them when it's worked out the way reprocessActivity will — narrowed to its side of at
// (the point itself on both), clipped against zones, edit applied, clipped again — lies inside
// a Private location (ErrSplitHidden).
func CheckSplit(points []parse.Point, zones []Zone, edit *TrackEdit, at int64) error {
	for _, side := range []SplitRange{{To: &at}, {From: &at}} {
		plain := side.Apply(points, nil)
		if edit != nil {
			plain = edit.Apply(plain)
		}
		if len(plain) < 2 {
			return ErrSplitTooClose
		}
		p := side.Apply(points, zones)
		if edit != nil && len(p) >= 2 {
			p = edit.ApplyClipped(p, zones)
		}
		if len(p) < 2 {
			return ErrSplitHidden
		}
	}
	return nil
}

// ErrSplitConflict is a split or merge the activity's current state no longer allows: a split
// point outside the piece's range, or pieces that aren't the adjacent, live, idle pieces of one
// split.
var ErrSplitConflict = errors.New("these activities can't be split or merged that way any more")

// insertSplitPiece is the split half of EnqueueTrackEdit: inside its transaction, with the
// activity already flipped Pending, it narrows the activity to the points up to at and inserts
// a new Pending row for the points from at on, returning the new row's id. The new row copies
// the activity's type, name, description, raw payload and track edit, joins its Stories, and
// takes over the photos placed after at. Its external_id is the original's with a suffix, so
// re-importing the original still finds the original row and stays a no-op.
func insertSplitPiece(ctx context.Context, tx pgx.Tx, userID, activityID string, at int64) (string, error) {
	var from, to *int64
	var rawKey *string
	if err := tx.QueryRow(ctx,
		`SELECT split_from, split_to, raw_payload_key FROM activities WHERE id = $1 AND user_id = $2`,
		activityID, userID,
	).Scan(&from, &to, &rawKey); err != nil {
		return "", err
	}
	if rawKey == nil || (from != nil && at <= *from) || (to != nil && at >= *to) {
		return "", ErrSplitConflict
	}

	var pieceID string
	if err := tx.QueryRow(ctx, `
		INSERT INTO activities (
			user_id, source, source_detail, external_id, activity_type, started_at,
			raw_payload_key, name, description, track_edit, edit_pending,
			split_group, split_from, split_to
		)
		SELECT user_id, source, source_detail,
		       CASE WHEN external_id IS NULL THEN NULL
		            ELSE left(split_part(external_id, '#split:', 1), 200) || '#split:' || $3::bigint::text END,
		       activity_type, to_timestamp($3::bigint / 1000.0), raw_payload_key, name, description,
		       track_edit, true, COALESCE(split_group, id), $3::bigint, split_to
		FROM activities WHERE id = $1 AND user_id = $2
		RETURNING id
	`, activityID, userID, at).Scan(&pieceID); err != nil {
		return "", fmt.Errorf("insert split piece: %w", err)
	}
	if _, err := tx.Exec(ctx, `
		UPDATE activities SET split_group = COALESCE(split_group, id), split_to = $2 WHERE id = $1
	`, activityID, at); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO story_activities (story_id, activity_id)
		SELECT story_id, $2 FROM story_activities WHERE activity_id = $1
	`, activityID, pieceID); err != nil {
		return "", err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE activity_photos SET activity_id = $2
		WHERE activity_id = $1 AND route_at > to_timestamp($3::bigint / 1000.0)
	`, activityID, pieceID, at); err != nil {
		return "", err
	}
	return pieceID, nil
}

// piece is one row of a merge, as MergeEdits needs it.
type piece struct {
	ID    string
	Range SplitRange
	Edit  *TrackEdit
}

// maxSafeMs stands in for an open upper end in a merged Keep: the largest integer a JavaScript
// number holds exactly, since the track editor replays the stored spec in the browser.
const maxSafeMs = 1<<53 - 1

// MergeEdits folds the track edits of adjacent pieces, in order, into one edit over their
// joined range that shows exactly the points each piece showed. Each piece's spec only ever
// acted inside its own range, so every part of it is narrowed to that range first: a Remove,
// Drop or Move outside it was inert there and must stay inert after the merge. A piece's Keep
// becomes Remove ranges for the stretch of its range it cut away — except at the outer ends of
// the merged range, where it stays a Keep, so a Chop there still reads as a Chop. The point a
// split was made at belongs to both pieces beside it, so it survives only if neither removed it.
func MergeEdits(pieces []piece) TrackEdit {
	var out TrackEdit
	var keepLo, keepHi *int64
	moved := map[int64][2]float64{}
	dropped := map[int64]bool{}
	last := len(pieces) - 1
	for i, p := range pieces {
		if p.Edit == nil {
			continue
		}
		e, r := p.Edit, p.Range
		if e.Keep != nil {
			lo, hi := e.Keep[0], e.Keep[1]
			if r.From == nil || lo > *r.From {
				if i == 0 {
					keepLo = &lo
				} else {
					out.Remove = append(out.Remove, [2]int64{*r.From, lo - 1})
				}
			}
			if r.To == nil || hi < *r.To {
				if i == last {
					keepHi = &hi
				} else {
					out.Remove = append(out.Remove, [2]int64{hi + 1, *r.To})
				}
			}
		}
		for _, rm := range e.Remove {
			a, b := rm[0], rm[1]
			if r.From != nil && a < *r.From {
				a = *r.From
			}
			if r.To != nil && b > *r.To {
				b = *r.To
			}
			if a <= b {
				out.Remove = append(out.Remove, [2]int64{a, b})
			}
		}
		for _, t := range e.Drop {
			if r.contains(t) && !dropped[t] {
				dropped[t] = true
				out.Drop = append(out.Drop, t)
			}
		}
		for t, c := range e.Move {
			if _, taken := moved[t]; r.contains(t) && !taken {
				moved[t] = c
			}
		}
	}
	if keepLo != nil || keepHi != nil {
		k := [2]int64{0, maxSafeMs}
		if keepLo != nil {
			k[0] = *keepLo
		}
		if keepHi != nil {
			k[1] = *keepHi
		}
		out.Keep = &k
	}
	if len(moved) > 0 {
		out.Move = moved
	}
	slices.Sort(out.Drop)
	return out
}

// MergePieces joins adjacent pieces of one split back into one activity (§4.7.8): the earliest
// survives — the row first split, when it's among them, since a split always leaves a row the
// earlier part and inserts the later one, so it keeps its own external_id — takes the joined range,
// the merged track edit (MergeEdits), the others' photos and Stories and any duplicate they
// displaced (§4.6), and goes Pending with an `edit_track` job to rebuild it. The others are
// deleted. Returns the survivor and the deleted ids — whose masks in object storage the caller
// removes, since the cascade only reaches their rows. ErrSplitConflict when ids aren't two or
// more adjacent, live, idle pieces of one split owned by userID.
func MergePieces(ctx context.Context, pool *pgxpool.Pool, userID string, ids []string) (string, []string, error) {
	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	if len(ids) < 2 {
		return "", nil, ErrSplitConflict
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return "", nil, err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed

	rows, err := tx.Query(ctx, `
		SELECT id, split_group, split_from, split_to, track_edit, edit_pending, superseded_by IS NOT NULL
		FROM activities WHERE user_id = $1 AND id = ANY($2::uuid[])
		FOR UPDATE
	`, userID, ids)
	if err != nil {
		return "", nil, err
	}
	var pieces []piece
	var group *string
	for rows.Next() {
		var p piece
		var g *string
		var editJSON []byte
		var pending, superseded bool
		if err := rows.Scan(&p.ID, &g, &p.Range.From, &p.Range.To, &editJSON, &pending, &superseded); err != nil {
			rows.Close()
			return "", nil, err
		}
		if g == nil || pending || superseded || (group != nil && *g != *group) {
			rows.Close()
			return "", nil, ErrSplitConflict
		}
		group = g
		if editJSON != nil {
			p.Edit = &TrackEdit{}
			if err := json.Unmarshal(editJSON, p.Edit); err != nil {
				rows.Close()
				return "", nil, fmt.Errorf("stored track edit unreadable: %w", err)
			}
		}
		pieces = append(pieces, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return "", nil, err
	}
	if len(pieces) != len(ids) {
		return "", nil, ErrSplitConflict
	}
	slices.SortFunc(pieces, func(a, b piece) int {
		switch {
		case a.Range.From == nil:
			return -1
		case b.Range.From == nil:
			return 1
		}
		return int(min(max(*a.Range.From-*b.Range.From, -1), 1))
	})
	for i := 1; i < len(pieces); i++ {
		prev, next := pieces[i-1].Range.To, pieces[i].Range.From
		if prev == nil || next == nil || *prev != *next {
			return "", nil, ErrSplitConflict
		}
	}

	survivor := pieces[0].ID
	var removed []string
	for _, p := range pieces[1:] {
		removed = append(removed, p.ID)
	}
	merged := MergeEdits(pieces)
	var edit *TrackEdit
	if !merged.IsEmpty() {
		edit = &merged
	}

	// Tiles of every piece go dirty now, while their masks still exist to say which tiles.
	if err := markPendingTilesDirty(ctx, tx, userID, ids); err != nil {
		return "", nil, err
	}
	for _, q := range []string{
		`UPDATE activity_photos SET activity_id = $1 WHERE activity_id = ANY($2::uuid[])`,
		`INSERT INTO story_activities (story_id, activity_id)
		 SELECT DISTINCT story_id, $1::uuid FROM story_activities WHERE activity_id = ANY($2::uuid[])
		 ON CONFLICT DO NOTHING`,
		`UPDATE activities SET superseded_by = $1 WHERE superseded_by = ANY($2::uuid[])`,
	} {
		if _, err := tx.Exec(ctx, q, survivor, removed); err != nil {
			return "", nil, err
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM activities WHERE id = ANY($1::uuid[])`, removed); err != nil {
		return "", nil, err
	}
	// The whole split joined back up is an ordinary activity again.
	if _, err := tx.Exec(ctx, `
		UPDATE activities a SET
			edit_pending = true,
			split_from = $2::bigint, split_to = $3::bigint,
			split_group = CASE WHEN $2::bigint IS NULL AND $3::bigint IS NULL THEN NULL ELSE split_group END
		WHERE a.id = $1
	`, survivor, pieces[0].Range.From, pieces[len(pieces)-1].Range.To); err != nil {
		return "", nil, err
	}
	if err := fog.BumpMapVersion(ctx, tx, userID); err != nil {
		return "", nil, err
	}
	payload, err := json.Marshal(EditJob{UserID: userID, ActivityID: survivor, Edit: edit})
	if err != nil {
		return "", nil, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO jobs (kind, user_id, payload) VALUES ('edit_track', $1, $2)`, userID, payload,
	); err != nil {
		return "", nil, err
	}
	return survivor, removed, tx.Commit(ctx)
}
