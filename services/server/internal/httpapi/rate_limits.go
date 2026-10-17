package httpapi

import (
	"net/http"
	"sync/atomic"
	"time"
)

// rateLimitsOff lets every fixedWindowLimiter through, the per-account and per-address limits
// alike, for local development only: config's RATE_LIMITS=off, refused for an APP_BASE_URL
// that isn't local. The R2 budget (internal/budget) is a cost cap, not one of these.
var rateLimitsOff atomic.Bool

// SetRateLimitsOff turns every per-account and per-address request limit off, or back on.
func SetRateLimitsOff(off bool) { rateLimitsOff.Store(off) }

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

// syncActivitiesPerHour bounds the activities `POST /v1/sync/activities` takes from one account
// in an hour, beside syncRequestsPerHour's count of requests: at 100 a batch, that one alone
// would take 6,000 an hour. A phone lists three months of Health Connect sessions and its own
// recordings, a few hundred at most, so this only slows an account syncing, deleting and
// syncing again, which costs a render each time and stores nothing (IMPLEMENTATION.md §5.7).
const syncActivitiesPerHour = 1000

// defaultSignupsPerHour bounds the accounts one address creates with an email and password in
// an hour. Each sends a verification email to whatever address was typed, and every
// per-account limit counts one account. Only an account created counts, so a mistyped
// password or a taken address costs nothing; Google and Facebook sign-in, whose providers
// gate account creation themselves, aren't counted. SIGNUPS_PER_HOUR overrides it, for a load
// test creating its accounts.
const defaultSignupsPerHour = 5

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

// SetSignupsPerHour replaces the per-address signup limit (defaultSignupsPerHour), starting
// every address's count afresh.
func (s *Server) SetSignupsPerHour(n int) {
	s.signupLimiter = newFixedWindowLimiter(n, time.Hour)
}

// SetTilesPerMinute replaces the per-account tile limit (defaultTilesPerMinute), starting
// every account's count afresh.
func (s *Server) SetTilesPerMinute(n int) {
	s.tileLimiter = newFixedWindowLimiter(n, time.Minute)
}
