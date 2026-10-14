package httpapi

import (
	"net/http"
	"time"
)

// defaultTilesPerMinute is how many tile requests one account may make in a minute, across
// every /tiles/v1 layer. A pan fetches dozens at once and a high-resolution map export
// (apps/web/src/map/exportMap.ts) many more in one burst, failing outright on a single refused
// tile, so this sits far above any person's use: it stops a script that ignores the clients'
// per-version tile cache (§4.2.6), each Fog or Heatmap tile of which is an R2 read.
// TILES_PER_MINUTE overrides it, for a load test's target.
const defaultTilesPerMinute = 1200

// syncRequestsPerHour bounds `POST /v1/sync/activities` per account. A batch carries up to
// maxSyncBatchActivities, so a first sync of a long Health Connect history still brings in
// thousands an hour; the upload limit (defaultUploadsPerHour) doesn't reach this path.
const syncRequestsPerHour = 60

// trackPointsPerHour bounds `GET /v1/activities/track-points/{id}` per account: each request
// reads an original upload from object storage and parses it, and a person opens the track
// editor a few times an hour at most.
const trackPointsPerHour = 120

// rateKey is who a per-account limit counts a request against: the account, or for the demo
// account, which every visitor shares (DemoCustomerUserID), the visitor's address, so one
// heavy visitor doesn't use up everyone else's demo.
func rateKey(info authInfo, ip string) string {
	if info.isDemo {
		return "ip:" + ip
	}
	return info.userID
}

// limited reports whether l refuses r, answering the 429 itself when it does, with msgKey's
// message naming the limit as {max}.
func limited(w http.ResponseWriter, r *http.Request, l *fixedWindowLimiter, msgKey string) bool {
	if l.allow(rateKey(authInfoFromContext(r.Context()), clientIP(r))) {
		return false
	}
	httpErrorT(w, r, http.StatusTooManyRequests, msgKey, "max", l.limit)
	return true
}

// tile registers one /tiles/v1 layer: signed in and verified, and inside the account's tile
// limit. A refused tile is only a gap the map fills on its next pan, so it carries a
// Retry-After rather than a message anyone sees.
func (s *Server) tile(path string, h http.HandlerFunc) {
	s.mux.HandleFunc(tileRoute("GET", path), s.requireVerified(func(w http.ResponseWriter, r *http.Request) {
		if !s.tileLimiter.allow(rateKey(authInfoFromContext(r.Context()), clientIP(r))) {
			w.Header().Set("Retry-After", "60")
			httpErrorT(w, r, http.StatusTooManyRequests, "error.tile_rate_limit")
			return
		}
		h(w, r)
	}))
}

// SetTilesPerMinute replaces the per-account tile limit (defaultTilesPerMinute), starting
// every account's count afresh.
func (s *Server) SetTilesPerMinute(n int) {
	s.tileLimiter = newFixedWindowLimiter(n, time.Minute)
}
