// Package httpapi is cmd/holdmytrack serve — currently just the Path 3 upload endpoint
// (IMPLEMENTATION.md §4.0/§4.1 step 1). Uses stdlib net/http's ServeMux
// method+pattern routing (Go 1.22+) rather than a router dependency — the "HTTP router and
// database access" open decision in services/server/README.md, resolved toward the smallest
// dependency set that does the job, per PostGIS being the deciding constraint on the DB side
// (pgx directly, no ORM) rather than the router choice mattering much either way.
package httpapi

import (
	"archive/zip"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/mail"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/mapstyle"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/metrics"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/unpack"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/web"
)

// maxUploadBytes bounds the single read into memory this handler does. The parse step
// (worker, ingest.Process) streams the format from object storage without buffering — this
// cap is specifically about not accepting an unbounded HTTP body here (§5.1: "cap file
// size... before reading the body fully"), not a claim that the whole pipeline never
// buffers. A single activity file is realistically well under this.
const maxUploadBytes = 64 << 20 // 64 MiB

// maxZipUploadBytes bounds a `.zip` archive's own compressed size — IMPLEMENTATION.md
// §4.0.1's bulk-historical-import case (a Strava export can be thousands of files),
// so this is deliberately much larger than a single activity file ever needs to be.
const maxZipUploadBytes = 512 << 20 // 512 MiB

// multipartMemoryBytes is how much of an upload's multipart body ParseMultipartForm keeps in
// memory; a larger file part goes to a temp file. Passing maxZipUploadBytes here instead kept
// a whole archive in RAM, and the zip branch then copied it again — about 1 GiB per upload,
// so two or three large exports at once could get the API killed for out-of-memory.
const multipartMemoryBytes = 32 << 20

// corsAllowedOrigins lists the origins a credentialed cross-origin request may come from —
// needed now that sessions are cookies (see serve's own doc comment for why "*" no
// longer works once a request carries credentials). apps/web's dev server (5173), plus the
// dev servers apps/web/tests/smoke.mjs and build.mjs spawn for themselves (5180, 4183) so
// those suites can sign in the same way a real browser does; a production origin gets added
// here the day one exists.
var corsAllowedOrigins = map[string]bool{
	"http://localhost:5173": true,
	"http://localhost:5180": true,
	"http://localhost:4183": true,
}

// apiPrefix and tilesPrefix are the sole place the API's major version lives — route() and
// tileRoute() below are the only things that reference them, so a future /v2 (a real breaking
// change; additive changes need no bump at all) is a matter of adding a second constant and a
// second set of registrations, not a hunt-and-replace across every route below and every
// frontend call site.
const apiPrefix = "/v1"
const tilesPrefix = "/tiles/v1"

func route(method, path string) string     { return method + " " + apiPrefix + path }
func tileRoute(method, path string) string { return method + " " + tilesPrefix + path }

type Server struct {
	pool                  *pgxpool.Pool
	store                 *storage.Store
	log                   *slog.Logger
	mux                   *http.ServeMux
	mailer                mail.Sender
	appBaseURL            string
	basemapOrigin         string
	satellite             mapstyle.Satellite
	version               string
	skipEmailVerification bool
	google                googleOAuth
	facebook              facebookOAuth
	pages                 *web.Renderer
}

func New(pool *pgxpool.Pool, store *storage.Store, log *slog.Logger, mailer mail.Sender, appBaseURL, basemapOrigin string, satellite mapstyle.Satellite, version string, skipEmailVerification bool, google GoogleOAuthConfig, facebook FacebookOAuthConfig, pages *web.Renderer) *Server {
	s := &Server{
		pool: pool, store: store, log: log, mux: http.NewServeMux(), mailer: mailer,
		appBaseURL: appBaseURL, basemapOrigin: basemapOrigin, satellite: satellite, version: version,
		skipEmailVerification: skipEmailVerification,
		google:                newGoogleOAuth(google),
		facebook:              newFacebookOAuth(facebook),
		pages:                 pages,
	}
	s.registerPages()
	s.mux.HandleFunc(route("POST", "/auth/signup"), s.handleSignup)
	s.mux.HandleFunc(route("POST", "/auth/login"), s.handleLogin)
	s.mux.HandleFunc(route("POST", "/auth/logout"), s.handleLogout)
	s.mux.HandleFunc(route("GET", "/auth/me"), s.handleMe)
	s.mux.HandleFunc(route("POST", "/auth/demo"), s.handleDemoStart)
	s.mux.HandleFunc(route("POST", "/auth/forgot-password"), s.handleForgotPassword)
	s.mux.HandleFunc(route("POST", "/auth/reset-password"), s.handleResetPassword)
	s.mux.HandleFunc(route("POST", "/auth/verify-email"), s.handleVerifyEmail)
	// Sign in with Google (google_auth.go) and Facebook (facebook_auth.go) — GET, not POST:
	// start and callback are all full-page browser navigations, not fetch() calls.
	s.mux.HandleFunc(route("GET", "/auth/providers"), s.handleAuthProviders)
	s.mux.HandleFunc(route("GET", "/auth/google/start"), s.handleGoogleStart)
	s.mux.HandleFunc(route("GET", "/auth/google/callback"), s.handleGoogleCallback)
	s.mux.HandleFunc(route("GET", "/auth/facebook/start"), s.handleFacebookStart)
	s.mux.HandleFunc(route("GET", "/auth/facebook/callback"), s.handleFacebookCallback)
	// Native clients (docs/adr/0016-native-sign-in.md): an id_token from Android's Credential
	// Manager, and the one-time code a browser-tab round trip above ends with for an app.
	s.mux.HandleFunc(route("POST", "/auth/google/token"), s.handleGoogleToken)
	s.mux.HandleFunc(route("POST", "/auth/handoff"), s.handleAuthHandoff)
	// Plain requireAuth, not requireVerified — these two exist specifically to help an
	// account that hasn't verified yet (auth.go's own doc comments on each).
	s.mux.HandleFunc(route("POST", "/auth/resend-verification"), s.requireAuth(s.handleResendVerification))
	s.mux.HandleFunc(route("PATCH", "/auth/email"), s.requireAuth(s.handleChangeEmail))
	// requireNotDemo below: account/activity mutations docs/ROADMAP.md's "Email verification
	// + demo without real ingest" says a demo account must never reach. requireVerified alone
	// covers everything else a signed-in-but-unverified real account must also not reach yet.
	s.mux.HandleFunc(route("PATCH", "/account/settings"), s.requireNotDemo(s.handleUpdateSettings))
	// Readable by a demo session, whose Settings screen shows the fields, disabled.
	s.mux.HandleFunc(route("GET", "/account/settings/options"), s.requireVerified(s.handleSettingsOptions))
	s.mux.HandleFunc(route("POST", "/account/avatar"), s.requireNotDemo(s.handleUploadAvatar))
	s.mux.HandleFunc(route("GET", "/account/avatar"), s.requireVerified(s.handleGetAvatar))
	s.mux.HandleFunc(route("DELETE", "/account/avatar"), s.requireNotDemo(s.handleDeleteAvatar))
	// requireAuth alone: an unverified account can be deleted too. closeAccount refuses a demo.
	s.mux.HandleFunc(route("DELETE", "/account"), s.requireAuth(s.handleDeleteAccount))
	// exports.go — downloading your data. A demo session sees "none" and is refused a request.
	s.mux.HandleFunc(route("POST", "/account/export"), s.requireNotDemo(s.handleRequestExport))
	s.mux.HandleFunc(route("GET", "/account/export"), s.requireVerified(s.handleGetExport))
	s.mux.HandleFunc(route("GET", "/account/exports/{id}/parts/{n}"), s.requireVerified(s.handleDownloadExportPart))
	s.mux.HandleFunc(route("POST", "/activities/upload"), s.requireNotDemo(s.handleUpload))
	s.mux.HandleFunc(route("GET", "/activities"), s.requireVerified(s.handleListActivities))
	s.mux.HandleFunc(route("PATCH", "/activities/{id}"), s.requireNotDemo(s.handleUpdateActivity))
	s.mux.HandleFunc(route("DELETE", "/activities/{id}"), s.requireNotDemo(s.handleDeleteActivity))
	s.mux.HandleFunc(route("GET", "/activities/duplicates"), s.requireVerified(s.handleListDuplicates))
	s.mux.HandleFunc(route("GET", "/activities/summary"), s.requireVerified(s.handleActivitySummary))
	s.mux.HandleFunc(route("GET", "/activities/histogram"), s.requireVerified(s.handleActivityHistogram))
	s.mux.HandleFunc(route("GET", "/activities/graph-stats"), s.requireVerified(s.handleActivityGraphStats))
	s.mux.HandleFunc(route("GET", "/activities/trends"), s.requireVerified(s.handleActivityTrends))
	s.mux.HandleFunc(route("GET", "/activities/status/{external_id}"), s.requireVerified(s.handleActivityStatus))
	s.mux.HandleFunc(route("GET", "/activities/track-metrics/{id}"), s.requireVerified(s.handleActivityTrackMetrics))
	s.mux.HandleFunc(route("GET", "/activities/track-points/{id}"), s.requireVerified(s.handleActivityTrackPoints))
	s.mux.HandleFunc(route("POST", "/activities/track-edit/{id}"), s.requireNotDemo(s.handleActivityTrackEdit))
	s.mux.HandleFunc(route("POST", "/activities/track-merge"), s.requireNotDemo(s.handleActivityTrackMerge))
	s.mux.HandleFunc(route("GET", "/private-locations"), s.requireVerified(s.handleListPrivateLocations))
	s.mux.HandleFunc(route("POST", "/private-locations"), s.requireNotDemo(s.handleCreatePrivateLocation))
	s.mux.HandleFunc(route("PATCH", "/private-locations/{id}"), s.requireNotDemo(s.handleUpdatePrivateLocation))
	s.mux.HandleFunc(route("DELETE", "/private-locations/{id}"), s.requireNotDemo(s.handleDeletePrivateLocation))
	s.mux.HandleFunc(route("GET", "/stories"), s.requireVerified(s.handleListStories))
	s.mux.HandleFunc(route("POST", "/stories"), s.requireNotDemo(s.handleCreateStory))
	s.mux.HandleFunc(route("GET", "/stories/{id}"), s.requireVerified(s.handleGetStory))
	s.mux.HandleFunc(route("PATCH", "/stories/{id}"), s.requireNotDemo(s.handleUpdateStory))
	s.mux.HandleFunc(route("DELETE", "/stories/{id}"), s.requireNotDemo(s.handleDeleteStory))
	s.mux.HandleFunc(route("POST", "/stories/{id}/activities"), s.requireNotDemo(s.handleAddStoryActivities))
	s.mux.HandleFunc(route("DELETE", "/stories/{id}/activities"), s.requireNotDemo(s.handleRemoveStoryActivities))
	// Sending a copy of a Story (story_sends.go).
	s.mux.HandleFunc(route("POST", "/stories/{id}/send"), s.requireNotDemo(s.handleSendStory))
	s.mux.HandleFunc(route("GET", "/story-sends"), s.requireVerified(s.handleListStorySends))
	s.mux.HandleFunc(route("POST", "/story-sends/{id}/accept"), s.requireNotDemo(s.handleAcceptStorySend))
	s.mux.HandleFunc(route("DELETE", "/story-sends/{id}"), s.requireNotDemo(s.handleDeclineStorySend))
	// Activity photos (photos.go) — under /photos rather than /activities/{id}/photos, which
	// would collide with /activities/track-points/{id} and the other fixed segments above.
	s.mux.HandleFunc(route("GET", "/photos"), s.requireVerified(s.handleListPhotos))
	s.mux.HandleFunc(route("POST", "/photos"), s.requireNotDemo(s.handleUploadPhoto))
	s.mux.HandleFunc(route("POST", "/photos/place"), s.requireNotDemo(s.handlePlacePhoto))
	s.mux.HandleFunc(route("GET", "/photos/{id}"), s.requireVerified(s.handleGetPhoto))
	s.mux.HandleFunc(route("GET", "/photos/{id}/thumb"), s.requireVerified(s.handleGetPhotoThumb))
	s.mux.HandleFunc(route("PATCH", "/photos/{id}"), s.requireNotDemo(s.handleUpdatePhoto))
	s.mux.HandleFunc(route("DELETE", "/photos/{id}"), s.requireNotDemo(s.handleDeletePhoto))
	s.mux.HandleFunc(route("GET", "/uploads"), s.requireVerified(s.handleListUploads))
	s.mux.HandleFunc(route("GET", "/uploads/active"), s.requireVerified(s.handleActiveUploads))
	s.mux.HandleFunc(route("GET", "/coverage/status"), s.requireVerified(s.handleCoverageStatus))
	s.mux.HandleFunc(route("POST", "/sync/activities"), s.requireNotDemo(s.handleSyncActivities))
	s.mux.HandleFunc(tileRoute("GET", "/tracks/{z}/{x}/{y}"), s.requireVerified(s.handleTracksTile))
	s.mux.HandleFunc(tileRoute("GET", "/fog/{z}/{x}/{y}"), s.requireVerified(s.handleFogTile))
	s.mux.HandleFunc(tileRoute("GET", "/heatmap/{z}/{x}/{y}"), s.requireVerified(s.handleHeatmapTile))
	s.mux.HandleFunc(tileRoute("GET", "/spots/{z}/{x}/{y}"), s.requireVerified(s.handleSpotsTile))
	s.mux.HandleFunc(route("GET", "/spots"), s.requireVerified(s.handleSpotsInArea))
	s.mux.HandleFunc(route("GET", "/spots/captures"), s.requireVerified(s.handleListSpotCaptures))
	s.mux.HandleFunc(route("GET", "/spots/{id}"), s.requireVerified(s.handleSpotDetail))
	s.mux.HandleFunc(route("POST", "/spots/{id}/captures"), s.requireNotDemo(s.handleCaptureSpot))
	// §4.2.4's Country/Region zoom tiers — live MVT, not precomputed, see
	// admin_country_tiles.go's own doc comment for why that's safe here despite the
	// live-heatmap-compositing cost fog/heatmap's own tiles were moved away from.
	s.mux.HandleFunc(tileRoute("GET", "/country-fog/{z}/{x}/{y}"), s.requireVerified(s.handleCountryFogTile))
	s.mux.HandleFunc(tileRoute("GET", "/country-heatmap/{z}/{x}/{y}"), s.requireVerified(s.handleCountryHeatmapTile))
	s.mux.HandleFunc(tileRoute("GET", "/region-fog/{z}/{x}/{y}"), s.requireVerified(s.handleRegionFogTile))
	s.mux.HandleFunc(tileRoute("GET", "/region-heatmap/{z}/{x}/{y}"), s.requireVerified(s.handleRegionHeatmapTile))
	// Deliberately not behind requireAuth, unlike every /v1 route above it. The style
	// document is derived entirely from the public Protomaps basemap and contains no
	// per-user data — only layer definitions and the asset URLs a client would need
	// anyway to fetch an archive this server already publishes unauthenticated. Requiring
	// a session would also couple basemap rendering to session state on mobile, where the
	// app can usefully draw a map before the user has signed in.
	s.mux.HandleFunc(route("GET", "/map/style/{flavor}"), s.handleMapStyle)
	s.mux.HandleFunc("GET /healthz", s.handleHealthz)
	return s
}

// registerPages adds the server-rendered HTML pages (pages.go, auth_pages.go, ADR-0012) —
// outside /v1, since they're pages a browser navigates to, not API. In production Caddy sends
// this server everything that isn't a static asset (apps/web/docker/Caddyfile); in dev, Vite's
// proxy (apps/web/vite.config.ts) lists these paths one by one, so a new page goes there too.
func (s *Server) registerPages() {
	// About is the signed-out home page too (appShell), so its canonical link is `/`.
	s.mux.HandleFunc("GET /about", s.staticPage("about", "meta.about_title", "meta.home_description", "/"))
	s.mux.HandleFunc("GET /help", s.staticPage("help", "meta.help_title", "meta.help_description", ""))
	s.mux.HandleFunc("GET /contacts", s.staticPage("contacts", "meta.contacts_title", "meta.contacts_description", ""))
	s.mux.HandleFunc("GET /privacy", s.staticPage("privacy", "meta.privacy_title", "meta.privacy_description", ""))
	// Step-by-step export guides, linked from the Upload menu and from Help.
	s.mux.HandleFunc("GET /help/timeline-export", s.staticPage("guide-timeline", "meta.guide_timeline_title", "meta.guide_timeline_description", ""))
	s.mux.HandleFunc("GET /help/google-health-export", s.staticPage("guide-google-health", "meta.guide_google_health_title", "meta.guide_google_health_description", ""))
	// The Android test suite for Play's closed testers, its sample files under /static/testing/.
	s.mux.HandleFunc("GET /testing", s.unlistedPage("testing", "meta.testing_title", "meta.testing_description"))
	s.mux.Handle("GET /static/", s.pages.StaticHandler())
	s.mux.HandleFunc("POST /logout", s.sameOrigin(s.handleLogoutPage))
	s.mux.HandleFunc("POST /language", s.sameOrigin(s.handleLanguageForm)) // language.go
	// auth_pages.go — every POST is a form, so every POST is behind sameOrigin.
	s.mux.HandleFunc("GET /signin", s.handleSignInPage)
	s.mux.HandleFunc("POST /signin", s.sameOrigin(s.handleSignInForm))
	s.mux.HandleFunc("GET /signup", s.handleSignUpPage)
	s.mux.HandleFunc("POST /signup", s.sameOrigin(s.handleSignUpForm))
	s.mux.HandleFunc("POST /demo", s.sameOrigin(s.handleDemoForm))
	s.mux.HandleFunc("GET /forgot", s.handleForgotPage)
	s.mux.HandleFunc("POST /forgot", s.sameOrigin(s.handleForgotForm))
	s.mux.HandleFunc("GET /reset", s.handleResetPage)
	s.mux.HandleFunc("POST /reset", s.sameOrigin(s.handleResetForm))
	s.mux.HandleFunc("GET /verify", s.handleVerifyPage)
	s.mux.HandleFunc("GET /verify-pending", s.handleVerifyPendingPage)
	s.mux.HandleFunc("POST /verify-pending/resend", s.sameOrigin(s.handleVerifyResendForm))
	s.mux.HandleFunc("POST /verify-pending/email", s.sameOrigin(s.handleVerifyChangeEmailForm))
	// settings_page.go.
	s.mux.HandleFunc("GET /settings", s.handleSettingsPage)
	s.mux.HandleFunc("POST /settings", s.sameOrigin(s.handleSettingsForm))
	s.mux.HandleFunc("POST /settings/avatar", s.sameOrigin(s.handleSettingsAvatarForm))
	s.mux.HandleFunc("POST /settings/avatar/remove", s.sameOrigin(s.handleSettingsAvatarRemoveForm))
	s.mux.HandleFunc("POST /settings/export", s.sameOrigin(s.handleSettingsExportForm))
	s.mux.HandleFunc("POST /settings/delete", s.sameOrigin(s.handleSettingsDeleteForm))
	s.mux.HandleFunc("GET /profile", s.handleProfilePage) // profile_page.go
	s.mux.HandleFunc("GET /sync", s.handleSyncPage)       // sync_page.go
	s.mux.HandleFunc("GET /admin", s.handleAdminPage)     // admin_pages.go
	s.mux.HandleFunc("GET /admin/users/{id}", s.handleAdminUserPage)
	// The React app — the map (pages.go's appShell). `/{$}` is the root alone; "/" below is
	// everything else nothing more specific claims.
	s.mux.HandleFunc("GET /{$}", s.appShell("meta.home_title"))
	s.mux.HandleFunc("/", s.notFound)
}

// ServeHTTP is serve with each response counted and timed for internal/metrics, and a
// handler's panic recovered into a 500.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rec := &statusRecorder{ResponseWriter: w}
	start := time.Now()
	defer func() {
		// A handler's panic is answered here rather than by net/http, which would only drop
		// the connection and print a plain-text line the log shipping can't parse. Recorded
		// as a 500 like any other, and counted on its own for the alert.
		if p := recover(); p != nil {
			if p == http.ErrAbortHandler {
				panic(p)
			}
			metrics.HTTPPanics.Inc()
			s.log.Error("handler panicked", "method", r.Method, "route", r.Pattern, "panic", p, "stack", string(debug.Stack()))
			if !rec.wroteHeader {
				http.Error(rec, "internal error", http.StatusInternalServerError)
			}
			rec.status = http.StatusInternalServerError
		}
		// r.Pattern is the ServeMux pattern that matched (set by s.mux.ServeHTTP on this same
		// request), never the path itself, so the label has one value per route.
		route := r.Pattern
		if route == "" {
			route = "unmatched"
		}
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		metrics.HTTPRequests.WithLabelValues(route, metrics.StatusClass(status)).Inc()
		metrics.HTTPDuration.WithLabelValues(route).Observe(time.Since(start).Seconds())
	}()
	s.serve(rec, r)
}

// statusRecorder keeps the status a handler answered with, for ServeHTTP's metrics. Unwrap
// lets http.ResponseController reach the real writer's Flush and deadlines through it.
type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (r *statusRecorder) WriteHeader(status int) {
	if !r.wroteHeader {
		r.status, r.wroteHeader = status, true
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.status, r.wroteHeader = http.StatusOK, true
	}
	return r.ResponseWriter.Write(b)
}

func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// serve sets CORS headers before delegating to the mux. This has to happen here, not
// just as a nice-to-have: apps/web (:5173) and this server (:8080) are different origins in
// dev, and a cross-origin POST with a multipart/form-data body is a CORS "simple request" —
// the browser sends it through with no preflight, the server processes it fully, but without
// Access-Control-Allow-Origin the browser still blocks the JS from reading the response.
// That surfaces as a generic NetworkError in fetch(), with no indication in the response
// itself that anything went wrong — the request can be seen succeeding in the Network tab
// while the calling code sees only a rejected promise.
//
// Every request now carries (or is trying to establish) a session cookie, so "*" no longer
// works: browsers refuse to honour Access-Control-Allow-Credentials alongside a wildcard
// origin, and a credentialed request without it just has its cookie silently dropped. This
// reflects the request's own Origin back when it's on the allowlist instead — the standard
// pattern for "credentialed CORS from a known set of origins" — rather than accepting any
// origin with credentials, which would let any site ride a visitor's session.
func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	setSecurityHeaders(w.Header())
	if origin := r.Header.Get("Origin"); corsAllowedOrigins[origin] {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Vary", "Origin")
	}
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if s.crossSiteAPIWrite(r) {
		http.Error(w, "cross-origin request refused", http.StatusForbidden)
		return
	}
	if strings.HasPrefix(r.URL.Path, apiPrefix+"/") && !setsOwnBodyLimit(r) {
		r.Body = http.MaxBytesReader(w, r.Body, maxJSONBodyBytes)
	}
	s.mux.ServeHTTP(w, r)
}

// maxJSONBodyBytes bounds every /v1/ request body that doesn't set a limit of its own. The
// handlers decode JSON straight from the body and check lengths after, so without it one huge
// "name", or a track edit's list of millions of point indexes, was held whole in memory first.
// The largest legitimate body, a Story's 10,000 activity ids or a 10,000-entry track edit, is a
// few hundred KB.
const maxJSONBodyBytes = 1 << 20

// setsOwnBodyLimit reports the requests whose handler bounds the body itself, at a size above
// maxJSONBodyBytes: every multipart upload (a file, an archive, a photo, an avatar) and a
// phone sync's batch of activities.
func setsOwnBodyLimit(r *http.Request) bool {
	return strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data") ||
		r.URL.Path == apiPrefix+"/sync/activities"
}

// setSecurityHeaders applies to every response. No page here is meant to be framed, so none
// may be (frame-ancestors, and X-Frame-Options for browsers without CSP): framed invisibly by
// another site, a signed-in page's buttons could be clicked through — and the click is
// same-origin, so neither SameSite nor the Origin checks would stop it. nosniff keeps a
// browser from reading a response as a type other than the one it's served as. HSTS is
// Caddy's (apps/web/docker/Caddyfile), which terminates TLS.
func setSecurityHeaders(h http.Header) {
	h.Set("Content-Security-Policy", "frame-ancestors 'none'")
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
}

// crossSiteAPIWrite reports a state-changing /v1/ request a browser sent from another site.
// SameSite=Lax keeps the session cookie off such a request, but not off the response: a
// hostile page's auto-submitted form to /v1/auth/login, with an enctype="text/plain" body
// shaped into valid JSON, set the attacker's session in the victim's browser, so whatever the
// victim uploaded next went to the attacker's account. A browser always sends Origin on a
// POST, PATCH or DELETE, so one naming a different origin than this app's (or the dev
// allowlist's) is refused. The native apps send no Origin, and pass.
func (s *Server) crossSiteAPIWrite(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return false
	}
	if !strings.HasPrefix(r.URL.Path, apiPrefix+"/") {
		return false
	}
	origin := r.Header.Get("Origin")
	if origin == "" {
		return false
	}
	return !corsAllowedOrigins[origin] && !s.isSameOrigin(r)
}

type healthzResponse struct {
	Status  string `json:"status"`
	Version string `json:"version"`
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if err := s.pool.Ping(r.Context()); err != nil {
		http.Error(w, "db unreachable", http.StatusServiceUnavailable)
		return
	}
	writeJSON(w, http.StatusOK, healthzResponse{Status: "ok", Version: s.version})
}

type uploadResponse struct {
	Status     string `json:"status"` // "enqueued" | "already_processed"
	ExternalID string `json:"external_id"`
	Filename   string `json:"filename"`
}

// handleUpload is §4.1 step 1 only: validate, then enqueue an `ingest` job carrying the raw
// payload (ingest.EnqueueRaw), return. No parsing happens here — see internal/ingest,
// run by cmd/holdmytrack work.
//
// A `.zip` archive takes a different path entirely (handleZipUpload,
// IMPLEMENTATION.md §4.0.1's bulk-import case) — bypassing §4.0.1's 20-file client-side
// cap rather than being one more file subject to it, and turning into many jobs, by way of
// the worker's `unpack`, instead of one. The body-size ceiling below has to accommodate whichever path a
// given request turns out to need before the multipart form (and therefore the filename) has
// even been parsed, which is why it's sized for a zip archive regardless of what's actually
// uploaded — a single non-zip file is still bounded to maxUploadBytes once read (below), so
// this only changes how much of a too-large *non-zip* body the server bothers reading before
// rejecting it, not what it ultimately accepts.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxZipUploadBytes+1<<20) // +1MiB of multipart overhead
	if err := r.ParseMultipartForm(multipartMemoryBytes); err != nil {
		httpErrorT(w, r, http.StatusRequestEntityTooLarge, "error.upload_too_large")
		return
	}
	defer r.MultipartForm.RemoveAll() //nolint:errcheck // best effort; net/http also cleans up
	file, header, err := r.FormFile("file")
	if err != nil {
		httpErrorT(w, r, http.StatusBadRequest, "error.avatar_missing")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == ".zip" {
		// zip.NewReader reads the part where ParseMultipartForm left it — in memory when
		// small, a temp file otherwise — rather than a second copy of the whole archive.
		if header.Size == 0 {
			httpErrorT(w, r, http.StatusBadRequest, "error.upload_empty")
			return
		}
		ra, ok := file.(io.ReaderAt)
		if !ok {
			httpErrorT(w, r, http.StatusBadRequest, "error.avatar_read")
			return
		}
		zr, err := zip.NewReader(ra, header.Size)
		if err != nil {
			httpErrorT(w, r, http.StatusBadRequest, "error.upload_bad_zip")
			return
		}
		s.handleZipUpload(w, r, zr, ra, header.Size, header.Filename)
		return
	}
	if !unpack.AllowedExt[ext] {
		httpErrorT(w, r, http.StatusUnsupportedMediaType, "error.upload_type", "type", strconv.Quote(ext))
		return
	}

	// Refused rather than cut off at the limit, which would have been ingested as a file
	// that ends mid-track, or reported as unreadable.
	if header.Size > maxUploadBytes {
		httpErrorT(w, r, http.StatusRequestEntityTooLarge, "error.upload_too_large")
		return
	}
	// Read once into memory (bounded by maxUploadBytes above) so the same bytes can be
	// hashed and then uploaded with a known Content-Length. See the maxUploadBytes doc
	// comment for why this is a deliberate, bounded exception to "never buffer a file."
	data, err := io.ReadAll(io.LimitReader(file, maxUploadBytes))
	if err != nil {
		httpErrorT(w, r, http.StatusBadRequest, "error.avatar_read")
		return
	}
	if len(data) == 0 {
		httpErrorT(w, r, http.StatusBadRequest, "error.upload_empty")
		return
	}

	res, err := ingest.EnqueueRaw(r.Context(), s.pool, userIDFromContext(r.Context()), []ingest.RawItem{
		{Source: "upload", Filename: header.Filename, Ext: ext, Data: data},
	})
	if err != nil {
		s.log.Error("upload enqueue failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if res[0].AlreadyProcessed {
		writeJSON(w, http.StatusOK, uploadResponse{Status: "already_processed", ExternalID: res[0].ExternalID, Filename: header.Filename})
		return
	}
	writeJSON(w, http.StatusAccepted, uploadResponse{Status: "enqueued", ExternalID: res[0].ExternalID, Filename: header.Filename})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
