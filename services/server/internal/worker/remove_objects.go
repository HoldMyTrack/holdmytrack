package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// removeActivityObjectsJob is a `remove_activity_objects` job's payload: the raw uploads of
// activities a migration deleted, which SQL can't reach (`0028_drop_superseded.sql`), each
// listed only when no remaining activity points at it. The payload's `activity_ids`, whose
// mask objects the job also removed until masks moved into Postgres (ADR-0040), are ignored.
type removeActivityObjectsJob struct {
	RawKeys []string `json:"raw_keys"`
}

// runRemoveActivityObjects removes them, refusing anything that isn't a key under `raw/`: a
// malformed payload must never widen into a prefix that takes more. Removing what's already
// gone succeeds, so a retry is safe.
func runRemoveActivityObjects(ctx context.Context, store *storage.Store, payload []byte) error {
	var j removeActivityObjectsJob
	if err := json.Unmarshal(payload, &j); err != nil {
		return fmt.Errorf("unmarshal remove_activity_objects job: %w", err)
	}
	for _, key := range j.RawKeys {
		if !strings.HasPrefix(key, "raw/") || strings.Contains(key, "..") || strings.HasSuffix(key, "/") {
			return fmt.Errorf("remove_activity_objects: invalid raw key %q", key)
		}
	}
	for _, key := range j.RawKeys {
		if err := store.Remove(ctx, key); err != nil {
			return fmt.Errorf("remove_activity_objects: %s: %w", key, err)
		}
	}
	return nil
}
