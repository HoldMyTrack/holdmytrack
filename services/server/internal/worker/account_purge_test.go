package worker

import (
	"context"
	"log/slog"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage/storagetest"
)

func TestPurgeDeletedAccountRemovesItsObjectsAndRows(t *testing.T) {
	pool, userID := testPool(t)
	ctx := context.Background()
	s3 := storagetest.New()
	srv := httptest.NewServer(s3)
	t.Cleanup(srv.Close)
	store, err := storage.New(srv.URL, "test", "test", "test")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	var activityID string
	if err := pool.QueryRow(ctx, `
		INSERT INTO activities (user_id, source, activity_type, started_at, timezone) VALUES ($1, 'upload', 'run', NOW(), 'UTC') RETURNING id
	`, userID).Scan(&activityID); err != nil {
		t.Fatalf("create activity: %v", err)
	}
	mine := []string{
		"raw/" + userID + "/a.gpx", "fog/" + userID + "/14/1/2.png", "heatmap/" + userID + "/14/1/2.png",
		"photos/" + userID + "/p1", "photos/" + userID + "/p1-thumb", "avatars/" + userID,
		"exports/" + userID + "/e1/1.zip",
	}
	other := []string{"raw/someone-else/a.gpx", "avatars/" + userID + "x"}
	for _, k := range append(append([]string{}, mine...), other...) {
		s3.Put(k, []byte("x"))
	}

	// A running job holds the sweep off; a pending one doesn't, and goes.
	locked := time.Now()
	running := insertJob(t, pool, userID, "2000-01-01", 1, &locked)
	pending := insertJob(t, pool, userID, "2000-01-01", 0, nil)
	if _, err := pool.Exec(ctx, `UPDATE users SET deleted_at = NOW() WHERE id = $1`, userID); err != nil {
		t.Fatalf("mark deleted: %v", err)
	}
	log := slog.New(slog.DiscardHandler)
	if err := purgeAccount(ctx, pool, store, log, userID); err != nil {
		t.Fatalf("purge: %v", err)
	}
	var jobs int
	pool.QueryRow(ctx, `SELECT count(*) FROM jobs WHERE id IN ($1, $2)`, running, pending).Scan(&jobs)
	if jobs != 1 || !s3.Has(mine[0]) {
		t.Fatalf("with a job running: %d jobs left (want the running one), raw file kept %v", jobs, s3.Has(mine[0]))
	}

	if _, err := pool.Exec(ctx, `UPDATE jobs SET state = 'done' WHERE id = $1`, running); err != nil {
		t.Fatalf("finish job: %v", err)
	}
	if err := purgeAccount(ctx, pool, store, log, userID); err != nil {
		t.Fatalf("purge: %v", err)
	}
	for _, k := range mine {
		if s3.Has(k) {
			t.Errorf("%s still stored", k)
		}
	}
	for _, k := range other {
		if !s3.Has(k) {
			t.Errorf("%s removed, but isn't the account's", k)
		}
	}
	var users, activities int
	pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE id = $1`, userID).Scan(&users)
	pool.QueryRow(ctx, `SELECT count(*) FROM activities WHERE id = $1`, activityID).Scan(&activities)
	if users != 0 || activities != 0 {
		t.Errorf("rows left: %d users, %d activities", users, activities)
	}
}

func TestPurgeLeavesALiveAccountAlone(t *testing.T) {
	pool, userID := testPool(t)
	store, err := storage.New(httptest.NewServer(storagetest.New()).URL, "test", "test", "test")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	if err := purgeDeletedAccounts(context.Background(), pool, store, slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("purge: %v", err)
	}
	var n int
	pool.QueryRow(context.Background(), `SELECT count(*) FROM users WHERE id = $1`, userID).Scan(&n)
	if n != 1 {
		t.Fatal("a live account was purged")
	}
}

func TestPurgeKeepsPhotoFilesAnotherAccountUses(t *testing.T) {
	pool, userID := testPool(t)
	_, otherID := testPool(t)
	ctx := context.Background()
	s3 := storagetest.New()
	srv := httptest.NewServer(s3)
	t.Cleanup(srv.Close)
	store, err := storage.New(srv.URL, "test", "test", "test")
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	activity := func(owner string) string {
		var id string
		if err := pool.QueryRow(ctx, `
			INSERT INTO activities (user_id, source, activity_type, started_at, timezone) VALUES ($1, 'upload', 'run', NOW(), 'UTC') RETURNING id
		`, owner).Scan(&id); err != nil {
			t.Fatalf("create activity: %v", err)
		}
		return id
	}
	photo := func(owner, activityID, key string) {
		if _, err := pool.Exec(ctx, `
			INSERT INTO activity_photos (user_id, activity_id, route_at, content_type, thumb_content_type, width, height, bytes, image_key)
			VALUES ($1, $2, NOW(), 'image/jpeg', 'image/jpeg', 1, 1, 1, $3)`, owner, activityID, key); err != nil {
			t.Fatalf("photo: %v", err)
		}
		s3.Put(key, []byte("x"))
		s3.Put(key+"-thumb", []byte("x"))
	}
	mine, theirs := activity(userID), activity(otherID)
	sharedKey, ownKey := "photos/"+userID+"/shared", "photos/"+userID+"/own"
	receivedKey, receivedAlone := "photos/"+otherID+"/sent", "photos/someone-gone/sent"
	photo(userID, mine, sharedKey)
	photo(otherID, theirs, sharedKey) // the other account's copy of it
	photo(userID, mine, ownKey)
	photo(otherID, theirs, receivedKey)
	photo(userID, mine, receivedKey) // a copy the account received
	photo(userID, mine, receivedAlone)

	if _, err := pool.Exec(ctx, `UPDATE users SET deleted_at = NOW() WHERE id = $1`, userID); err != nil {
		t.Fatalf("mark deleted: %v", err)
	}
	if err := purgeAccount(ctx, pool, store, slog.New(slog.DiscardHandler), userID); err != nil {
		t.Fatalf("purge: %v", err)
	}
	for key, want := range map[string]bool{sharedKey: true, ownKey: false, receivedKey: true, receivedAlone: false} {
		if s3.Has(key) != want || s3.Has(key+"-thumb") != want {
			t.Errorf("%s stored %v, want %v", key, s3.Has(key), want)
		}
	}
}
