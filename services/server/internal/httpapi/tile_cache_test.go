package httpapi

import (
	"context"
	"net/http/httptest"
	"testing"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/fog"
)

func TestSetTileCacheControl(t *testing.T) {
	const user = "11111111-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	for _, tc := range []struct {
		name, query, want string
	}{
		{"own version", "?cv=" + fog.TileVersion(user, 3), "private, max-age=31536000, immutable"},
		{"another account's version", "?cv=" + fog.TileVersion("22222222-bbbb-4bbb-8bbb-bbbbbbbbbbbb", 3), "private, no-cache"},
		{"no version", "", "private, no-cache"},
		{"only an older client's counter", "?v=4", "private, no-cache"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/tiles/v1/fog/14/1/2.png"+tc.query, nil)
			r = r.WithContext(context.WithValue(r.Context(), authContextKey, authInfo{userID: user}))
			w := httptest.NewRecorder()
			setTileCacheControl(w, r)
			if got := w.Header().Get("Cache-Control"); got != tc.want {
				t.Errorf("Cache-Control = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestClientGone(t *testing.T) {
	r := httptest.NewRequest("GET", "/tiles/v1/heatmap/12/1114/1529.png", nil)
	w := httptest.NewRecorder()
	if clientGone(w, r) {
		t.Fatal("clientGone = true for a live request")
	}
	if w.Code != 200 || w.Body.Len() != 0 {
		t.Errorf("a live request was answered: %d %q", w.Code, w.Body.String())
	}

	ctx, cancel := context.WithCancel(r.Context())
	cancel()
	w = httptest.NewRecorder()
	if !clientGone(w, r.WithContext(ctx)) {
		t.Fatal("clientGone = false for a cancelled request")
	}
	if w.Code != statusClientClosedRequest {
		t.Errorf("status %d, want %d", w.Code, statusClientClosedRequest)
	}
}
