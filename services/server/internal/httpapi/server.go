// Package httpapi is cmd/holdmytrack serve — currently just the Path 3 upload endpoint
// (IMPLEMENTATION.md §4.0/§4.1 step 1). Uses stdlib net/http's ServeMux
// method+pattern routing (Go 1.22+) rather than a router dependency — the "HTTP router and
// database access" open decision in services/server/README.md, resolved toward the smallest
// dependency set that does the job, per PostGIS being the deciding constraint on the DB side
// (pgx directly, no ORM) rather than the router choice mattering much either way.
package httpapi

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/HoldMyTrack/holdmytrack/services/server/internal/ingest"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/mail"
	"github.com/HoldMyTrack/holdmytrack/services/server/internal/storage"
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

// maxZipEntries bounds how many files inside one archive handleZipUpload will process — not
// a claim that a real import can't have more, just where this server stops rather than
// enqueueing an unbounded number of jobs from one request. §5.1's "one bad file in a bulk
// import cannot abort the batch" still holds beneath this cap; this is a different, coarser
// limit on the batch's total size.
const maxZipEntries = 5000

// maxZipEntryBytes bounds any single file *inside* a zip the same way maxUploadBytes bounds
// a plain upload — checked against the entry's own declared size before it's decompressed at
// all (§5.1's zip-bomb defense), and again against how much is actually read, since a
// declared size is something a malformed or hostile archive can simply lie about.
const maxZipEntryBytes = maxUploadBytes

var allowedExt = map[string]bool{".gpx": true, ".fit": true, ".tcx": true}

// corsAllowedOrigins lists the origins a credentialed cross-origin request may come from —
// needed now that sessions are cookies (see ServeHTTP's own doc comment for why "*" no
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
	version               string
	skipEmailVerification bool
	google                googleOAuth
	facebook              facebookOAuth
	pages                 *web.Renderer
}

func New(pool *pgxpool.Pool, store *storage.Store, log *slog.Logger, mailer mail.Sender, appBaseURL, basemapOrigin, version string, skipEmailVerification bool, google GoogleOAuthConfig, facebook FacebookOAuthConfig, pages *web.Renderer) *Server {
	s := &Server{
		pool: pool, store: store, log: log, mux: http.NewServeMux(), mailer: mailer,
		appBaseURL: appBaseURL, basemapOrigin: basemapOrigin, version: version,
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
	// Plain requireAuth, not requireVerified — these two exist specifically to help an
	// account that hasn't verified yet (auth.go's own doc comments on each).
	s.mux.HandleFunc(route("POST", "/auth/resend-verification"), s.requireAuth(s.handleResendVerification))
	s.mux.HandleFunc(route("PATCH", "/auth/email"), s.requireAuth(s.handleChangeEmail))
	// requireNotDemo below: account/activity mutations docs/ROADMAP.md's "Email verification
	// + demo without real ingest" says a demo account must never reach. requireVerified alone
	// covers everything else a signed-in-but-unverified real account must also not reach yet.
	s.mux.HandleFunc(route("PATCH", "/account/settings"), s.requireNotDemo(s.handleUpdateSettings))
	s.mux.HandleFunc(route("POST", "/account/avatar"), s.requireNotDemo(s.handleUploadAvatar))
	s.mux.HandleFunc(route("GET", "/account/avatar"), s.requireVerified(s.handleGetAvatar))
	s.mux.HandleFunc(route("DELETE", "/account/avatar"), s.requireNotDemo(s.handleDeleteAvatar))
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
	s.mux.HandleFunc(route("GET", "/private-locations"), s.requireVerified(s.handleListPrivateLocations))
	s.mux.HandleFunc(route("POST", "/private-locations"), s.requireNotDemo(s.handleCreatePrivateLocation))
	s.mux.HandleFunc(route("PATCH", "/private-locations/{id}"), s.requireNotDemo(s.handleUpdatePrivateLocation))
	s.mux.HandleFunc(route("DELETE", "/private-locations/{id}"), s.requireNotDemo(s.handleDeletePrivateLocation))
	s.mux.HandleFunc(route("GET", "/uploads"), s.requireVerified(s.handleListUploads))
	s.mux.HandleFunc(route("GET", "/coverage/status"), s.requireVerified(s.handleCoverageStatus))
	s.mux.HandleFunc(route("POST", "/sync/activities"), s.requireNotDemo(s.handleSyncActivities))
	s.mux.HandleFunc(tileRoute("GET", "/tracks/{z}/{x}/{y}"), s.requireVerified(s.handleTracksTile))
	s.mux.HandleFunc(tileRoute("GET", "/fog/{z}/{x}/{y}"), s.requireVerified(s.handleFogTile))
	s.mux.HandleFunc(tileRoute("GET", "/heatmap/{z}/{x}/{y}"), s.requireVerified(s.handleHeatmapTile))
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
	s.mux.Handle("GET /static/", s.pages.StaticHandler())
	s.mux.HandleFunc("POST /logout", s.sameOrigin(s.handleLogoutPage))
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
	s.mux.HandleFunc("GET /profile", s.handleProfilePage) // profile_page.go
	s.mux.HandleFunc("GET /admin", s.handleAdminPage)     // admin_pages.go
	s.mux.HandleFunc("GET /admin/users/{id}", s.handleAdminUserPage)
	// The React app — the map (pages.go's appShell). `/{$}` is the root alone; "/" below is
	// everything else nothing more specific claims.
	s.mux.HandleFunc("GET /{$}", s.appShell("meta.home_title"))
	s.mux.HandleFunc("/", s.notFound)
}

// ServeHTTP sets CORS headers before delegating to the mux. This has to happen here, not
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
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	s.mux.ServeHTTP(w, r)
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

// handleUpload is §4.1 step 1 only: validate, compute the idempotency key, persist the raw
// payload, enqueue an `ingest` job, return. No parsing happens here — see internal/ingest,
// run by cmd/holdmytrack work.
//
// A `.zip` archive takes a different path entirely (handleZipUpload,
// IMPLEMENTATION.md §4.0.1's bulk-import case) — bypassing §4.0.1's 20-file client-side
// cap rather than being one more file subject to it, and producing many jobs from one
// request instead of one. The body-size ceiling below has to accommodate whichever path a
// given request turns out to need before the multipart form (and therefore the filename) has
// even been parsed, which is why it's sized for a zip archive regardless of what's actually
// uploaded — a single non-zip file is still bounded to maxUploadBytes once read (below), so
// this only changes how much of a too-large *non-zip* body the server bothers reading before
// rejecting it, not what it ultimately accepts.
func (s *Server) handleUpload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxZipUploadBytes+1<<20) // +1MiB of multipart overhead
	if err := r.ParseMultipartForm(maxZipUploadBytes); err != nil {
		httpErrorT(w, r, http.StatusRequestEntityTooLarge, "error.upload_too_large")
		return
	}
	file, header, err := r.FormFile("file")
	if err != nil {
		httpErrorT(w, r, http.StatusBadRequest, "error.avatar_missing")
		return
	}
	defer file.Close()

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == ".zip" {
		// zip.NewReader needs an io.ReaderAt, which an HTTP body doesn't provide, so there is
		// no streaming alternative here the way ingest.Process manages for a single file —
		// read fully into memory (bounded by maxZipUploadBytes above), once, regardless of
		// which zip-shaped branch below ends up handling it.
		data, err := io.ReadAll(io.LimitReader(file, maxZipUploadBytes))
		if err != nil {
			httpErrorT(w, r, http.StatusBadRequest, "error.avatar_read")
			return
		}
		if len(data) == 0 {
			httpErrorT(w, r, http.StatusBadRequest, "error.upload_empty")
			return
		}
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			httpErrorT(w, r, http.StatusBadRequest, "error.upload_bad_zip")
			return
		}
		if isTakeoutArchive(zr) {
			s.handleTakeoutUpload(w, r, zr, header.Filename)
			return
		}
		s.handleZipUpload(w, r, zr, header.Filename)
		return
	}
	if !allowedExt[ext] {
		httpErrorT(w, r, http.StatusUnsupportedMediaType, "error.upload_type", "type", strconv.Quote(ext))
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

	externalID, alreadyProcessed, err := s.persistAndEnqueue(r.Context(), uploadFileParams{
		UserID: userIDFromContext(r.Context()), Source: "upload", Filename: header.Filename, Ext: ext, Data: data,
	})
	if err != nil {
		s.log.Error("upload persist/enqueue failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	if alreadyProcessed {
		writeJSON(w, http.StatusOK, uploadResponse{Status: "already_processed", ExternalID: externalID, Filename: header.Filename})
		return
	}
	writeJSON(w, http.StatusAccepted, uploadResponse{Status: "enqueued", ExternalID: externalID, Filename: header.Filename})
}

// uploadFileParams is persistAndEnqueue's input — a struct rather than a run of positional
// string parameters (source, filename, ext, activityType all being strings makes positional
// args easy to transpose silently at a call site).
type uploadFileParams struct {
	// The authenticated caller (userIDFromContext) — every call site reads this from the
	// request it's already handling.
	UserID string
	// "upload" (a plain or zip-contained file) or "takeout" (handleTakeoutUpload) — the
	// `activities.source` column's own provenance value, not just a label.
	Source   string
	Filename string
	Ext      string
	// Overrides whatever the parser itself detects, when non-empty. Only the Takeout path
	// sets this today — see ingest.Job.ActivityType's own doc comment for why.
	ActivityType string
	Data         []byte
	// ExternalID, when set, is used verbatim as the idempotency key instead of being derived
	// from a content hash of Data. Only handleSyncActivities sets this: Path 2 activities
	// already carry a stable id from the platform health store (a Health Connect record
	// UUID), which is the record's real identity — hashing the synced JSON bytes instead
	// would mint a new "activity" on every retry that happened to reserialize a field
	// differently, defeating the idempotent-retry requirement rather than serving it. Path 3
	// has no such id of its own, which is why it still hashes.
	ExternalID string
}

// persistAndEnqueue is handleUpload's idempotency-check-then-persist-then-enqueue core,
// factored out so handleZipUpload's and handleTakeoutUpload's per-file loops can reuse
// exactly the same logic a plain single-file upload uses — a file arriving inside a zip or
// pulled out of a Takeout export is not a different kind of upload, just one of many arriving
// from one request. Returns `alreadyProcessed` rather than a status string so callers can't
// typo one of two states; the caller decides what to do with either.
func (s *Server) persistAndEnqueue(ctx context.Context, p uploadFileParams) (externalID string, alreadyProcessed bool, err error) {
	var rawKey string
	if p.ExternalID != "" {
		externalID = p.ExternalID
		// Scoped by source, unlike the content-addressed key below: a caller-supplied id is
		// only unique within its own source's namespace (a Health Connect UUID and a
		// HealthKit UUID could coincide in theory), whereas a hash collision across sources
		// is intentionally shared storage (see handleDeleteActivity's doc comment).
		rawKey = fmt.Sprintf("raw/%s/%s/%s%s", p.UserID, p.Source, externalID, p.Ext)
	} else {
		sum := sha256.Sum256(p.Data)
		externalID = hex.EncodeToString(sum[:])
		rawKey = fmt.Sprintf("raw/%s/%s%s", p.UserID, externalID, p.Ext)
	}

	// Fast-path idempotency check (IMPLEMENTATION.md §4.0's idempotency
	// invariant): this is an optimization to skip a wasted job for an obvious repeat, not
	// the guarantee — that lives in ingest.Process's ON CONFLICT DO NOTHING at persist
	// time, which still fires correctly even if two identical uploads race each other past
	// this check. Scoped by the same `source` the job itself will carry, matching the
	// activities table's own `(user_id, source, external_id)` unique index — a Takeout import
	// and a plain upload are different provenance even if (implausibly) they hashed the same.
	var exists bool
	if err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM activities WHERE user_id = $1 AND source = $2 AND external_id = $3)`,
		p.UserID, p.Source, externalID,
	).Scan(&exists); err != nil {
		return "", false, fmt.Errorf("dedupe check: %w", err)
	}
	if exists {
		return externalID, true, nil
	}

	if err := s.store.Put(ctx, rawKey, bytes.NewReader(p.Data), int64(len(p.Data))); err != nil {
		return "", false, fmt.Errorf("raw payload upload: %w", err)
	}

	job := ingest.Job{
		UserID:        p.UserID,
		Source:        p.Source,
		SourceDetail:  p.Filename,
		ExternalID:    externalID,
		RawPayloadKey: rawKey,
		ActivityType:  p.ActivityType,
	}
	payload, err := json.Marshal(job)
	if err != nil {
		return "", false, fmt.Errorf("job marshal: %w", err)
	}
	if err := enqueue(ctx, s.pool, p.UserID, "ingest", payload); err != nil {
		return "", false, fmt.Errorf("enqueue: %w", err)
	}
	return externalID, false, nil
}

func enqueue(ctx context.Context, pool *pgxpool.Pool, userID, kind string, payload []byte) error {
	_, err := pool.Exec(ctx,
		`INSERT INTO jobs (kind, user_id, payload) VALUES ($1, $2, $3)`,
		kind, userID, payload,
	)
	return err
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
