package httpapi

import (
	"reflect"
	"testing"
	"time"
)

func TestRequestClient(t *testing.T) {
	for _, tc := range []struct {
		name, agent string
		inApp       bool
		client      client
		build       int
	}{
		{"the app", "HoldMyTrack-Android/0.14 (9537c263; 1255)", false, clientAndroid, 1255},
		{"an app from before the build in the agent", "HoldMyTrack-Android/0.9", false, clientAndroid, 0},
		{"a browser", "Mozilla/5.0 (X11; Linux x86_64) Chrome/140.0", false, clientWeb, 0},
		{"no agent", "", false, clientWeb, 0},
		{"the app's WebView", "Mozilla/5.0 (Linux; Android 16; wv) Chrome/140.0", true, clientNone, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, build := requestClient(tc.agent, tc.inApp)
			if c != tc.client || build != tc.build {
				t.Errorf("requestClient = %v, %d; want %v, %d", c, build, tc.client, tc.build)
			}
		})
	}
}

func TestSeenDue(t *testing.T) {
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	today, yesterday := now.Add(-time.Hour), now.Add(-24*time.Hour)
	build := func(n int) *int { return &n }
	for _, tc := range []struct {
		name   string
		was    seen
		client client
		build  int
		want   seenWrite
	}{
		{"first ever request, from the web", seen{}, clientWeb, 0, seenWrite{last: true, web: true}},
		{"web again the same day", seen{last: &today, web: &today}, clientWeb, 0, seenWrite{}},
		{"the app after the web the same day", seen{last: &today, web: &today}, clientAndroid, 1255,
			seenWrite{android: true, build: 1255}},
		{"the app again the same day", seen{last: &today, android: &today, androidBuild: build(1255)}, clientAndroid, 1255,
			seenWrite{build: 1255}},
		{"the app updated during the day", seen{last: &today, android: &today, androidBuild: build(1250)}, clientAndroid, 1255,
			seenWrite{android: true, build: 1255}},
		{"a new day in the app", seen{last: &yesterday, android: &yesterday, androidBuild: build(1255)}, clientAndroid, 1255,
			seenWrite{last: true, android: true, build: 1255}},
		{"the app's WebView marks only the visit", seen{last: &yesterday}, clientNone, 0, seenWrite{last: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := seenDue(tc.was, tc.client, tc.build, now); got != tc.want {
				t.Errorf("seenDue = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestSeenWriteUpdate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		w     seenWrite
		query string
		args  []any
	}{
		{"nothing", seenWrite{build: 1255}, "", nil},
		{"the visit and the web", seenWrite{last: true, web: true},
			"UPDATE users SET last_seen_at = NOW(), web_seen_at = NOW() WHERE id = $1", nil},
		{"the app and its build", seenWrite{android: true, build: 1255},
			"UPDATE users SET android_seen_at = NOW(), android_build = $2 WHERE id = $1", []any{1255}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			query, args := tc.w.update()
			if query != tc.query || !reflect.DeepEqual(args, tc.args) {
				t.Errorf("update = %q %v, want %q %v", query, args, tc.query, tc.args)
			}
		})
	}
}
