package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Stories (IMPLEMENTATION.md §4.23, ADR-0020) — hand-picked, private sets of activities, each
// with a name, a description and joint statistics. Stored in stories and story_activities
// (§3.19). Another account's Story answers exactly like a missing one, `404`, on every route.

const (
	maxStoryNameLen        = 200
	maxStoryDescriptionLen = 2000
	// maxStoryActivityIDs bounds one request's activity_ids — "Create story" sends every
	// checked activity, which with select-all can be a whole history, so this is generous.
	maxStoryActivityIDs = 10000
)

var (
	errStoryNotFound    = errors.New("story not found")
	errActivityNotFound = errors.New("activity not found")
)

// storyTypeStats is one activity type's share of a Story's statistics. MovingSeconds falls
// back to duration_seconds per activity, as Trends does (activityTrendsQuery), for activities
// ingested before moving_seconds was populated.
type storyTypeStats struct {
	ActivityType   string  `json:"activity_type"`
	Count          int64   `json:"count"`
	DistanceMeters float64 `json:"distance_meters"`
	MovingSeconds  int64   `json:"moving_seconds"`
	ElapsedSeconds int64   `json:"elapsed_seconds"`
}

// storyStats is a Story's joint statistics over its live activities (a superseded duplicate,
// §4.6, is a member but isn't counted), with no date bound, plus the same per activity type,
// most frequent first.
type storyStats struct {
	Count          int64            `json:"count"`
	DistanceMeters float64          `json:"distance_meters"`
	MovingSeconds  int64            `json:"moving_seconds"`
	ElapsedSeconds int64            `json:"elapsed_seconds"`
	ByType         []storyTypeStats `json:"by_type"`
}

type story struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description *string   `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	// Every member, oldest activity first — what the Edit window's Stories tab checks its
	// boxes from without a request per Story.
	ActivityIDs []string   `json:"activity_ids"`
	Stats       storyStats `json:"stats"`
}

type storiesResponse struct {
	Stories []story `json:"stories"`
}

// storyRequest is POST's body; PATCH reads the same fields but activity_ids, a full replace
// of name and description, the same one-form shape handleUpdateActivity uses.
type storyRequest struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	ActivityIDs []string `json:"activity_ids"`
}

type storyActivitiesRequest struct {
	ActivityIDs []string `json:"activity_ids"`
}

// validateStoryFields trims name and description in place and writes a 400 for a value out of
// bounds, reporting whether they're valid.
func validateStoryFields(w http.ResponseWriter, r *http.Request, name, description *string) bool {
	*name = strings.TrimSpace(*name)
	*description = strings.TrimSpace(*description)
	switch {
	case *name == "":
		httpErrorT(w, r, http.StatusBadRequest, "error.story_name_required")
	case len([]rune(*name)) > maxStoryNameLen:
		httpErrorT(w, r, http.StatusBadRequest, "error.story_name_too_long", "max", maxStoryNameLen)
	case len([]rune(*description)) > maxStoryDescriptionLen:
		httpErrorT(w, r, http.StatusBadRequest, "error.story_description_too_long", "max", maxStoryDescriptionLen)
	default:
		return true
	}
	return false
}

// validateActivityIDs dedupes ids and writes a 400 for a malformed one or too many, reporting
// whether they're valid. required rejects an empty list, which the membership routes have no
// use for.
func validateActivityIDs(w http.ResponseWriter, ids []string, required bool) ([]string, bool) {
	if required && len(ids) == 0 {
		http.Error(w, "activity_ids must not be empty", http.StatusBadRequest)
		return nil, false
	}
	if len(ids) > maxStoryActivityIDs {
		http.Error(w, "too many activity_ids", http.StatusBadRequest)
		return nil, false
	}
	seen := make(map[string]bool, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if !uuidPattern.MatchString(id) {
			http.Error(w, "invalid activity id", http.StatusBadRequest)
			return nil, false
		}
		id = strings.ToLower(id)
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out, true
}

// storyIDFromPath reads {id}, writing a 404 for one that isn't a UUID — it can't be a Story.
func storyIDFromPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if !uuidPattern.MatchString(id) {
		http.Error(w, errStoryNotFound.Error(), http.StatusNotFound)
		return "", false
	}
	return id, true
}

// handleListStories serves `GET /v1/stories`, newest first.
func (s *Server) handleListStories(w http.ResponseWriter, r *http.Request) {
	stories, err := s.loadStories(r.Context(), s.pool, userIDFromContext(r.Context()), nil)
	if s.writeStoryError(w, err) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, storiesResponse{Stories: stories})
}

// handleGetStory serves `GET /v1/stories/{id}`.
func (s *Server) handleGetStory(w http.ResponseWriter, r *http.Request) {
	id, ok := storyIDFromPath(w, r)
	if !ok {
		return
	}
	st, err := s.loadStory(r.Context(), s.pool, userIDFromContext(r.Context()), id)
	if s.writeStoryError(w, err) {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, st)
}

// handleCreateStory serves `POST /v1/stories`, responding 201 with the new Story. The
// optional activity_ids become its first members in the same transaction, so "Create story"
// is one request; any of them that isn't the caller's own activity fails the whole request
// with a 404 and creates nothing.
func (s *Server) handleCreateStory(w http.ResponseWriter, r *http.Request) {
	var req storyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !validateStoryFields(w, r, &req.Name, &req.Description) {
		return
	}
	ids, ok := validateActivityIDs(w, req.ActivityIDs, false)
	if !ok {
		return
	}
	userID := userIDFromContext(r.Context())

	var created story
	err := s.inTx(r.Context(), func(tx pgx.Tx) error {
		var id string
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO stories (user_id, name, description) VALUES ($1, $2, NULLIF($3, ''))
			RETURNING id
		`, userID, req.Name, req.Description).Scan(&id); err != nil {
			return err
		}
		if _, err := addStoryActivities(r.Context(), tx, userID, id, ids); err != nil {
			return err
		}
		var err error
		created, err = s.loadStory(r.Context(), tx, userID, id)
		return err
	})
	if s.writeStoryError(w, err) {
		return
	}
	writeJSON(w, http.StatusCreated, created)
}

// handleUpdateStory serves `PATCH /v1/stories/{id}` — name and description, both sent every
// time; an empty description clears it.
func (s *Server) handleUpdateStory(w http.ResponseWriter, r *http.Request) {
	id, ok := storyIDFromPath(w, r)
	if !ok {
		return
	}
	var req storyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if !validateStoryFields(w, r, &req.Name, &req.Description) {
		return
	}
	userID := userIDFromContext(r.Context())

	tag, err := s.pool.Exec(r.Context(), `
		UPDATE stories SET name = $3, description = NULLIF($4, ''), updated_at = NOW()
		WHERE id = $1 AND user_id = $2
	`, id, userID, req.Name, req.Description)
	if err == nil && tag.RowsAffected() == 0 {
		err = errStoryNotFound
	}
	if s.writeStoryError(w, err) {
		return
	}
	st, err := s.loadStory(r.Context(), s.pool, userID, id)
	if s.writeStoryError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, st)
}

// handleDeleteStory serves `DELETE /v1/stories/{id}`, responding 204. Its activities stay;
// only the memberships cascade away.
func (s *Server) handleDeleteStory(w http.ResponseWriter, r *http.Request) {
	id, ok := storyIDFromPath(w, r)
	if !ok {
		return
	}
	tag, err := s.pool.Exec(r.Context(), `DELETE FROM stories WHERE id = $1 AND user_id = $2`,
		id, userIDFromContext(r.Context()))
	if err == nil && tag.RowsAffected() == 0 {
		err = errStoryNotFound
	}
	if s.writeStoryError(w, err) {
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleAddStoryActivities serves `POST /v1/stories/{id}/activities` with `{activity_ids}`,
// responding with the updated Story. An activity already in the Story is left as it is; one
// that isn't the caller's own fails the whole request with a 404 and adds nothing.
func (s *Server) handleAddStoryActivities(w http.ResponseWriter, r *http.Request) {
	s.changeStoryActivities(w, r, addStoryActivities)
}

// handleRemoveStoryActivities serves `DELETE /v1/stories/{id}/activities` with
// `{activity_ids}`, responding with the updated Story. An id that isn't in the Story is
// ignored. The activities themselves stay.
func (s *Server) handleRemoveStoryActivities(w http.ResponseWriter, r *http.Request) {
	s.changeStoryActivities(w, r, func(ctx context.Context, tx pgx.Tx, _, storyID string, ids []string) (bool, error) {
		tag, err := tx.Exec(ctx, `DELETE FROM story_activities WHERE story_id = $1 AND activity_id = ANY($2::uuid[])`,
			storyID, ids)
		return tag.RowsAffected() > 0, err
	})
}

// changeStoryActivities is the two membership routes' shared shape: lock the caller's Story
// (a 404 when it isn't theirs), apply change, and bump updated_at when change reports that
// the membership actually changed.
func (s *Server) changeStoryActivities(w http.ResponseWriter, r *http.Request,
	change func(ctx context.Context, tx pgx.Tx, userID, storyID string, ids []string) (bool, error)) {
	storyID, ok := storyIDFromPath(w, r)
	if !ok {
		return
	}
	var req storyActivitiesRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	ids, ok := validateActivityIDs(w, req.ActivityIDs, true)
	if !ok {
		return
	}
	ctx := r.Context()
	userID := userIDFromContext(ctx)

	var updated story
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		var found string
		if err := tx.QueryRow(ctx, `SELECT id FROM stories WHERE id = $1 AND user_id = $2 FOR UPDATE`,
			storyID, userID).Scan(&found); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errStoryNotFound
			}
			return err
		}
		changed, err := change(ctx, tx, userID, storyID, ids)
		if err != nil {
			return err
		}
		if changed {
			if _, err := tx.Exec(ctx, `UPDATE stories SET updated_at = NOW() WHERE id = $1`, storyID); err != nil {
				return err
			}
		}
		updated, err = s.loadStory(ctx, tx, userID, storyID)
		return err
	})
	if s.writeStoryError(w, err) {
		return
	}
	writeJSON(w, http.StatusOK, updated)
}

// addStoryActivities adds ids to the Story, all or none: errActivityNotFound when any of them
// isn't one of userID's activities. FOR KEY SHARE holds those activities until commit, so a
// concurrent delete waits rather than failing the insert's foreign key.
func addStoryActivities(ctx context.Context, tx pgx.Tx, userID, storyID string, ids []string) (bool, error) {
	if len(ids) == 0 {
		return false, nil
	}
	rows, err := tx.Query(ctx, `SELECT id FROM activities WHERE user_id = $1 AND id = ANY($2::uuid[]) FOR KEY SHARE`,
		userID, ids)
	if err != nil {
		return false, err
	}
	owned, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return false, err
	}
	if len(owned) != len(ids) {
		return false, errActivityNotFound
	}
	tag, err := tx.Exec(ctx, `
		INSERT INTO story_activities (story_id, activity_id)
		SELECT $1, unnest($2::uuid[])
		ON CONFLICT DO NOTHING
	`, storyID, ids)
	return tag.RowsAffected() > 0, err
}

func (s *Server) inTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck // no-op once committed
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// querier is the common surface of the pool and a transaction, so a Story can be read back
// inside the transaction that just changed it.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

// loadStory is loadStories for one Story, errStoryNotFound when it isn't userID's.
func (s *Server) loadStory(ctx context.Context, q querier, userID, storyID string) (story, error) {
	stories, err := s.loadStories(ctx, q, userID, &storyID)
	if err != nil {
		return story{}, err
	}
	if len(stories) == 0 {
		return story{}, errStoryNotFound
	}
	return stories[0], nil
}

const listStoriesQuery = `
SELECT id, name, description, created_at, updated_at
FROM stories
WHERE user_id = $1 AND ($2::uuid IS NULL OR id = $2)
ORDER BY created_at DESC, id DESC`

const storyMembersQuery = `
SELECT sa.story_id, sa.activity_id
FROM story_activities sa
JOIN stories s ON s.id = sa.story_id
JOIN activities a ON a.id = sa.activity_id
WHERE s.user_id = $1 AND ($2::uuid IS NULL OR s.id = $2)
ORDER BY a.started_at, a.id`

// storyStatsQuery is every Story's statistics per activity type in one pass; loadStories
// sums the types into each Story's totals.
const storyStatsQuery = `
SELECT sa.story_id, a.activity_type, COUNT(*),
       COALESCE(SUM(a.distance_meters), 0)::float8,
       COALESCE(SUM(COALESCE(a.moving_seconds, a.duration_seconds)), 0),
       COALESCE(SUM(a.duration_seconds), 0)
FROM story_activities sa
JOIN stories s ON s.id = sa.story_id
JOIN activities a ON a.id = sa.activity_id
WHERE s.user_id = $1 AND ($2::uuid IS NULL OR s.id = $2)
  AND a.superseded_by IS NULL
GROUP BY sa.story_id, a.activity_type
ORDER BY COUNT(*) DESC, a.activity_type`

// loadStories reads userID's Stories — every one, or only storyID's when it's set — with
// their members and statistics: three queries, whatever the number of Stories.
func (s *Server) loadStories(ctx context.Context, q querier, userID string, storyID *string) ([]story, error) {
	rows, err := q.Query(ctx, listStoriesQuery, userID, storyID)
	if err != nil {
		return nil, err
	}
	stories, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (story, error) {
		st := story{ActivityIDs: []string{}, Stats: storyStats{ByType: []storyTypeStats{}}}
		err := row.Scan(&st.ID, &st.Name, &st.Description, &st.CreatedAt, &st.UpdatedAt)
		return st, err
	})
	if err != nil {
		return nil, err
	}
	if len(stories) == 0 {
		return []story{}, nil
	}
	byID := make(map[string]*story, len(stories))
	for i := range stories {
		byID[stories[i].ID] = &stories[i]
	}

	rows, err = q.Query(ctx, storyMembersQuery, userID, storyID)
	if err != nil {
		return nil, err
	}
	var sid, aid string
	if _, err := pgx.ForEachRow(rows, []any{&sid, &aid}, func() error {
		if st := byID[sid]; st != nil {
			st.ActivityIDs = append(st.ActivityIDs, aid)
		}
		return nil
	}); err != nil {
		return nil, err
	}

	rows, err = q.Query(ctx, storyStatsQuery, userID, storyID)
	if err != nil {
		return nil, err
	}
	var t storyTypeStats
	if _, err := pgx.ForEachRow(rows, []any{&sid, &t.ActivityType, &t.Count, &t.DistanceMeters, &t.MovingSeconds, &t.ElapsedSeconds}, func() error {
		st := byID[sid]
		if st == nil {
			return nil
		}
		st.Stats.ByType = append(st.Stats.ByType, t)
		st.Stats.Count += t.Count
		st.Stats.DistanceMeters += t.DistanceMeters
		st.Stats.MovingSeconds += t.MovingSeconds
		st.Stats.ElapsedSeconds += t.ElapsedSeconds
		return nil
	}); err != nil {
		return nil, err
	}
	return stories, nil
}

// writeStoryError maps a Story handler's error to a response, reporting whether it wrote one.
func (s *Server) writeStoryError(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, errStoryNotFound), errors.Is(err, errActivityNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	default:
		s.log.Error("story request failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
	return true
}
