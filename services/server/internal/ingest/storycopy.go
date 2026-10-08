package ingest

import (
	"context"
	"encoding/json"
)

// StoryCopyJob is the `story_copy` job's payload (IMPLEMENTATION.md §4.23, ADR-0036): copy the
// Story as it is now into the recipient's account. Accepting a sent copy enqueues it.
type StoryCopyJob struct {
	StoryID     string `json:"story_id"`
	RecipientID string `json:"recipient_id"`
}

// EnqueueStoryCopy adds a `story_copy` job for the recipient, on db — the transaction that
// takes the copy out of their inbox.
func EnqueueStoryCopy(ctx context.Context, db Querier, job StoryCopyJob) error {
	payload, err := json.Marshal(job)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO jobs (kind, user_id, payload) VALUES ('story_copy', $1, $2)`, job.RecipientID, payload)
	return err
}
