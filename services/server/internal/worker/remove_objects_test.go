package worker

import (
	"context"
	"testing"
)

// A payload that isn't activity ids and raw keys is refused before anything is removed: a
// bad one must never widen into a prefix that takes more of the store.
func TestRemoveActivityObjectsRefusesWhatItIsNot(t *testing.T) {
	for _, payload := range []string{
		`{"activity_ids": [""]}`,
		`{"activity_ids": ["../x"]}`,
		`{"raw_keys": [""]}`,
		`{"raw_keys": ["photos/u/p.jpg"]}`,
		`{"raw_keys": ["raw/../photos/x"]}`,
		`{"raw_keys": ["raw/"]}`,
		`not json`,
	} {
		if err := runRemoveActivityObjects(context.Background(), nil, []byte(payload)); err == nil {
			t.Errorf("%s: accepted", payload)
		}
	}
}
