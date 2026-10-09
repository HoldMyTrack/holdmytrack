package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/i18n"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storycopy"
)

// Sending a copy of a Story (IMPLEMENTATION.md §4.23, ADR-0036). A Story's owner sends a copy
// to an email address; the recipient finds it in their inbox and accepts or declines it, and
// accepting enqueues the `story_copy` job that copies the Story as it is then into their
// account (storycopy.Process). Stored in story_sends until accepted or declined.

// storySendLimiter bounds how many copies one account sends an hour: each is an email to
// someone else's inbox.
var storySendLimiter = newFixedWindowLimiter(30, time.Hour)

type storySendRequest struct {
	Email string `json:"email"`
}

// handleSendStory serves `POST /v1/stories/{id}/send` with `{email}`. The answer is `204`
// whether or not the address belongs to an account that can receive it — anything else would
// tell the sender who has an account. Only a verified real account other than the sender's
// own gets the copy, and an email saying so (sendStoryCopyEmail); a repeat send while the last
// one still waits refreshes it rather than adding another, and emails again.
func (s *Server) handleSendStory(w http.ResponseWriter, r *http.Request) {
	storyID, ok := storyIDFromPath(w, r)
	if !ok {
		return
	}
	var req storySendRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	email, err := normalizeEmail(req.Email)
	if err != nil {
		httpErrorT(w, r, http.StatusBadRequest, "error.invalid_email")
		return
	}
	ctx := r.Context()
	userID := userIDFromContext(ctx)
	owns, err := s.ownsStory(ctx, userID, storyID)
	if err != nil {
		s.log.Error("story send: story lookup failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if !owns {
		http.Error(w, errStoryNotFound.Error(), http.StatusNotFound)
		return
	}
	if !storySendLimiter.allow(userID) {
		httpErrorT(w, r, http.StatusTooManyRequests, "error.story_send_limit")
		return
	}
	var lang string
	err = s.pool.QueryRow(ctx, `
		INSERT INTO story_sends (story_id, recipient_id)
		SELECT $1, id FROM users
		WHERE email = $2 AND id <> $3 AND email_verified AND demo_expires_at IS NULL AND deleted_at IS NULL
		ON CONFLICT (story_id, recipient_id) DO UPDATE SET sent_at = NOW()
		RETURNING (SELECT COALESCE(locale, '') FROM users WHERE id = recipient_id)
	`, storyID, email, userID).Scan(&lang)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		s.log.Error("story send failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if err == nil {
		// A failed email is logged, not answered: the copy waits in the inbox either way, and
		// a different answer would say the address has an account.
		if err := s.sendStoryCopyEmail(ctx, userID, storyID, email, lang); err != nil {
			s.log.Error("story copy email failed", "err", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// sendStoryCopyEmail tells the recipient at email, in their own language (lang, or English
// when they've chosen none), that the sender sent them a copy of the Story, with a link to
// the Stories tab where they answer it.
func (s *Server) sendStoryCopyEmail(ctx context.Context, senderID, storyID, email, lang string) error {
	var from, story string
	var activities int64
	if err := s.pool.QueryRow(ctx, `
		SELECT `+senderNameSQL+`, st.name,
		       (SELECT count(*) FROM story_activities sa JOIN activities a ON a.id = sa.activity_id
		        WHERE sa.story_id = st.id)
		FROM stories st JOIN users u ON u.id = st.user_id
		WHERE st.id = $1 AND u.id = $2`, storyID, senderID).Scan(&from, &story, &activities); err != nil {
		return err
	}
	if lang == "" {
		lang = "en"
	}
	l := i18n.Get(lang)
	return s.mailer.Send(ctx, email,
		l.T("email.story_copy.subject", "from", from, "story", story),
		l.T("email.story_copy.body", "from", from, "story", story,
			"activities", l.N("count.activities", activities), "link", s.appBaseURL+"/?tab=stories"))
}

// storySendJSON is one copy waiting in the recipient's inbox. From is the sender's name, or
// their email address when they've set none.
type storySendJSON struct {
	ID            string    `json:"id"`
	StoryName     string    `json:"story_name"`
	From          string    `json:"from"`
	SentAt        time.Time `json:"sent_at"`
	ActivityCount int64     `json:"activity_count"`
}

// storyCopyingJSON is a copy the account accepted that hasn't finished arriving: its Story
// appears once the `story_copy` job has run.
type storyCopyingJSON struct {
	StoryName string `json:"story_name"`
	From      string `json:"from"`
}

type storySendsResponse struct {
	Sends   []storySendJSON    `json:"sends"`
	Copying []storyCopyingJSON `json:"copying"`
}

// senderNameSQL is how a sender is named to the people they send to.
const senderNameSQL = `COALESCE(NULLIF(u.display_name, ''), u.email)`

// handleListStorySends serves `GET /v1/story-sends`: the copies waiting for the caller, newest
// first, and the ones they accepted that are still being copied. A send from an account being
// deleted is left out, as it can no longer be copied.
func (s *Server) handleListStorySends(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	userID := userIDFromContext(ctx)
	rows, err := s.pool.Query(ctx, `
		SELECT ss.id, st.name, `+senderNameSQL+`, ss.sent_at,
		       (SELECT count(*) FROM story_activities sa JOIN activities a ON a.id = sa.activity_id
		        WHERE sa.story_id = st.id)
		FROM story_sends ss
		JOIN stories st ON st.id = ss.story_id
		JOIN users u ON u.id = st.user_id
		WHERE ss.recipient_id = $1 AND u.deleted_at IS NULL
		ORDER BY ss.sent_at DESC, ss.id DESC
	`, userID)
	if err != nil {
		s.log.Error("story sends list failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	sends, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (storySendJSON, error) {
		var v storySendJSON
		err := row.Scan(&v.ID, &v.StoryName, &v.From, &v.SentAt, &v.ActivityCount)
		return v, err
	})
	if err != nil {
		s.log.Error("story sends list failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if sends == nil {
		sends = []storySendJSON{}
	}
	rows, err = s.pool.Query(ctx, `
		SELECT st.name, `+senderNameSQL+`
		FROM jobs j
		JOIN stories st ON st.id = (j.payload->>'story_id')::uuid
		JOIN users u ON u.id = st.user_id
		WHERE j.user_id = $1 AND j.kind = 'story_copy' AND j.state = 'pending'
		ORDER BY j.id
	`, userID)
	if err != nil {
		s.log.Error("story copies list failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	copying, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (storyCopyingJSON, error) {
		var v storyCopyingJSON
		err := row.Scan(&v.StoryName, &v.From)
		return v, err
	})
	if err != nil {
		s.log.Error("story copies list failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if copying == nil {
		copying = []storyCopyingJSON{}
	}
	writeJSON(w, http.StatusOK, storySendsResponse{Sends: sends, Copying: copying})
}

var errStorySendNotFound = errors.New("story send not found")

// takeStorySend removes one of the caller's waiting copies, returning the Story it offered —
// errStorySendNotFound when there's no such copy for them.
func takeStorySend(ctx context.Context, tx pgx.Tx, userID, sendID string) (string, error) {
	var storyID string
	err := tx.QueryRow(ctx, `DELETE FROM story_sends WHERE id = $1 AND recipient_id = $2 RETURNING story_id`,
		sendID, userID).Scan(&storyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", errStorySendNotFound
	}
	return storyID, err
}

// handleAcceptStorySend serves `POST /v1/story-sends/{id}/accept`: the copy leaves the inbox and
// a `story_copy` job is queued for it in the same transaction. `202`: the Story arrives once the
// job has run.
func (s *Server) handleAcceptStorySend(w http.ResponseWriter, r *http.Request) {
	s.answerStorySend(w, r, func(ctx context.Context, tx pgx.Tx, userID, storyID string) error {
		return storycopy.Enqueue(ctx, tx, storycopy.Job{StoryID: storyID, RecipientID: userID}, 0)
	}, http.StatusAccepted)
}

// handleDeclineStorySend serves `DELETE /v1/story-sends/{id}`: the copy leaves the inbox and
// nothing is copied.
func (s *Server) handleDeclineStorySend(w http.ResponseWriter, r *http.Request) {
	s.answerStorySend(w, r, nil, http.StatusNoContent)
}

func (s *Server) answerStorySend(w http.ResponseWriter, r *http.Request,
	then func(ctx context.Context, tx pgx.Tx, userID, storyID string) error, status int) {
	sendID := r.PathValue("id")
	if !uuidPattern.MatchString(sendID) {
		http.Error(w, errStorySendNotFound.Error(), http.StatusNotFound)
		return
	}
	ctx := r.Context()
	userID := userIDFromContext(ctx)
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		storyID, err := takeStorySend(ctx, tx, userID, sendID)
		if err != nil || then == nil {
			return err
		}
		return then(ctx, tx, userID, storyID)
	})
	if errors.Is(err, errStorySendNotFound) {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	if err != nil {
		s.log.Error("story send answer failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.WriteHeader(status)
}
