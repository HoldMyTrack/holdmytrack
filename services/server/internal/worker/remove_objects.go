package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
)

// removeActivityObjectsJob is a `remove_activity_objects` job's payload: the stored objects of
// activities a migration deleted, which SQL can't reach (`0028_drop_superseded.sql`). Each
// activity's masks go (`activity-masks/{id}/`), and each raw key, which the migration lists
// only when no remaining activity points at it.
type removeActivityObjectsJob struct {
	ActivityIDs []string `json:"activity_ids"`
	RawKeys     []string `json:"raw_keys"`
}

var activityIDPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// runRemoveActivityObjects removes them, refusing anything that isn't an activity id or a key
// under `raw/`: a malformed payload must never widen into a prefix that takes more. Removing
// what's already gone succeeds, so a retry is safe.
func runRemoveActivityObjects(ctx context.Context, store *storage.Store, payload []byte) error {
	var j removeActivityObjectsJob
	if err := json.Unmarshal(payload, &j); err != nil {
		return fmt.Errorf("unmarshal remove_activity_objects job: %w", err)
	}
	for _, id := range j.ActivityIDs {
		if !activityIDPattern.MatchString(id) {
			return fmt.Errorf("remove_activity_objects: invalid activity id %q", id)
		}
	}
	for _, key := range j.RawKeys {
		if !strings.HasPrefix(key, "raw/") || strings.Contains(key, "..") || strings.HasSuffix(key, "/") {
			return fmt.Errorf("remove_activity_objects: invalid raw key %q", key)
		}
	}
	for _, id := range j.ActivityIDs {
		if err := store.RemoveByPrefix(ctx, "activity-masks/"+id+"/"); err != nil {
			return fmt.Errorf("remove_activity_objects: masks of %s: %w", id, err)
		}
	}
	for _, key := range j.RawKeys {
		if err := store.Remove(ctx, key); err != nil {
			return fmt.Errorf("remove_activity_objects: %s: %w", key, err)
		}
	}
	return nil
}
