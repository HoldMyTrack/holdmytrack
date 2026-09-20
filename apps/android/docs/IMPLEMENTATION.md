# FitMap for Android: Implementation

> **Scope note.** This document covers each file's own implementation detail for the Android app — the app shell, auth, map overlays, Health Connect sync, in-app GPS recording, and the sync status screen. See `apps/android/docs/ARCHITECTURE.md` first for the app's shape and the stack this runs on, and `docs/IMPLEMENTATION.md` for the server-side schema and pipeline every call here eventually reaches. The app's local persistence is two `SharedPreferences` files, covered in place below, plus one SQLite database (§7) for recordings pending sync.

---

## 1. App Shell & Lifecycle

### 1.1 `FitMapApplication`

Process-level setup, in a fixed order that matters: `Session.init(this)` before anything reads a stored token; `MapLibre.getInstance(this)` to load the native library and install the module provider a `MapView`'s constructor asks for immediately; then `HttpRequestUtil.setOkHttpClient(FitMapApi.client)`, replacing MapLibre Native's own HTTP stack with the app's shared client. This last call is load-bearing and has to happen in `Application.onCreate`, not an Activity's — the map SDK may issue a request as soon as a `MapView` exists, and installing the client any later means some requests already went out through the SDK's default, tokenless client.

### 1.2 `MainActivity`

The map, and the state that governs what's drawn on it.

- **The basemap renders unconditionally.** The style document is unauthenticated (`docs/ARCHITECTURE.md` §2.1), so `mapView.getMapAsync` sets the style and clears the loading indicator regardless of session state; `syncSession()` is what attaches or detaches the three user layers afterward.
- **`syncSession()` is the single place session state and map state are reconciled**, called from `onResume` (covers returning from `SignInActivity`) and once after the style finishes loading. It: shows "Checking session…" and calls `FitMapApi.verifySession` if a token is stored but not yet `Session.verified`; otherwise sets the account label/button text and attaches (`MapOverlays.attach`) or detaches (`MapOverlays.detach`) the user layers to match `Session.isSignedIn`.
- **`verifying` is a re-entrancy guard**, not a cache — `onResume` fires on every return to this Activity, including from the sign-in screen, so without it a slow verify request could be started twice.
- **Only a `401` from `verifySession` clears the session.** Any other failure (`IOException`, a stopped dev stack) is logged and left alone; the same restored token is tried again on the next resume. This is the one piece of error-handling logic in the whole app that discriminates by HTTP status rather than treating "the request didn't succeed" as one bucket.
- **`frameActivities()` runs once per process, guarded by `framed`.** It reads `GET /v1/activities`, unions every row's `bbox` client-side (§3 covers why this lives in `FitMapApi` rather than a separate model type), and animates the camera to fit the result, capped at zoom 15. The cap exists because a single very short, or heavily privacy-trimmed (`docs/SPEC.md` FR-8.1), activity has a near-zero bounding box — fitting the camera to that literally would zoom in past the basemap's own z14 data onto an empty gray tile.
- **`insetSystemBars()` exists because of a bug found only on a real screen**, not from documentation: at `targetSdk` 35+ the system draws every app edge-to-edge and ignores the old opt-out, so a bottom bar laid out with ordinary padding sat *underneath* the gesture-navigation pill, with the mode-toggle buttons half covered. The fix adds the system-bar inset to each view's own existing padding (not replacing it) and returns the insets unconsumed, so nothing else downstream misses them.
- **MapLibre's `MapView` owns a native renderer and a GL surface**, so every one of `onStart`/`onResume`/`onPause`/`onStop`/`onSaveInstanceState`/`onLowMemory`/`onDestroy` forwards to the equivalent `mapView.on*` call. Missing any one of these is a leaked surface or a crash on rotation, not a subtle bug — this is why the full set is present even though several of them are otherwise empty boilerplate.
- **Style flavor follows the system day/night setting** (`Configuration.UI_MODE_NIGHT_MASK`), resolving to `light` or `dark` — two of the five flavors `GET /v1/map/style/{flavor}` serves. There is deliberately no in-app override; inventing one is Phase 5's decision to make (`apps/android/docs/ROADMAP.md`), not this shell's.

## 2. Authentication & Session

### 2.1 `net/Session`

An `object` (process-wide singleton) rather than an instance, because two independent HTTP call sites need the same token without being wired to each other: the app's own `FitMapApi` calls, and MapLibre Native's tile fetching, which goes through the shared `OkHttpClient` but never through `FitMapApi` itself.

The token lives in two places at once, deliberately: a `@Volatile` field for fast, thread-safe reads (the bearer interceptor runs on whichever thread OkHttp is dispatching a tile request from — reading `SharedPreferences` on every one of those would be both slower and not obviously safe to do off the main thread), and `SharedPreferences` (`MODE_PRIVATE`) for persistence across process death. `init(context)` must run before either `token` or `isSignedIn` is read — `FitMapApplication.onCreate` guarantees this by calling it first.

`verified` is a separate boolean from "a token is present." A token can be present and *unverified* right after a cold start (restored from disk, not yet checked against the server) or present and *verified* right after a fresh sign-in (the server just minted it, so there is nothing to check). `markVerified()` and `clear()` are the only two ways `verified` changes; `MainActivity.verifyStoredSession()` is the only caller of either in response to a network result.

### 2.2 `net/FitMapApi`

The entire HTTP surface of the app in one object: one shared `OkHttpClient`, one interceptor, and eight calls (four auth-minting endpoints behind a shared `authenticate` helper, plus `signOut`, `verifySession`, `activityBounds`, `syncHistory`, `duplicates`, and `syncActivities`).

- **`client` is shared with MapLibre Native** (§1.1). Its `Dispatcher` is configured with `maxRequestsPerHost = 20`, matching what MapLibre's own default client would have used — inheriting OkHttp's plain default of 5 here would throttle the map's tile bursts relative to how the SDK behaves when it manages its own client, for no reason tied to this app's needs.
- **`BearerInterceptor` runs per request, not once at startup**, and checks the request's URL against `BuildConfig.API_BASE_URL` before attaching anything. Running per-request is what makes sign-in, sign-out, and token expiry take effect on the very next request — including the very next map tile — with nothing to re-register. The origin check exists because the style document's basemap assets (the pmtiles archive, glyphs, sprites) are unauthenticated and, in a production CDN deployment, live on a domain FitMap doesn't own; sending a live bearer token to that origin would only ever put a working credential in a third party's access logs.
- **Everything except `syncActivities` is callback-based** (`enqueue` + a `Result<T>` callback posted back to the main thread via a `Handler(Looper.getMainLooper())`). This is a deliberate rejection of a coroutines-everywhere style for this file specifically: five-to-eight call sites don't amortize the cost of a coroutines runtime and lifecycle-scope wiring layered on top of OkHttp's own async API, which already does the job.
- **`syncActivities` is the one `suspend` function**, and it wraps a *blocking* `execute()` call inside `withContext(Dispatchers.IO)` rather than using `enqueue`. This is intentional, not an oversight: its only caller (`SyncRunner`) must not send its next batch, or move the watermark, until this one is answered — the call site genuinely wants to block its own coroutine, and Health Connect's own read API already forces that caller onto coroutines regardless.
- **`activityBounds` parses `GET /v1/activities` itself** (`parseBounds`) rather than sharing a model type with anything else in the app, because nothing else in the app needs a full `Activity` model — the map only ever needs the union of every row's bounding box, computed inline, once, at session-attach time.
- **`ApiException` carries the server's own response text as its message**, unreworded. The server writes plain-text errors (`http.Error`), and this app treats that text as user-facing rather than a debugging detail — "invalid email or password" is exactly what a person needs to read, and rewriting it risks saying something the server didn't actually mean.

### 2.3 `SignInActivity`

Three buttons over one shared result handler (`onResult`). Client-side validation is limited to "both fields are non-empty" before a request is even sent — deliberately, so the server remains the single source of truth for what an acceptable email or password looks like, and its error text (via `ApiException.message`) is what the user sees on failure rather than a client-authored message that could drift from server behavior.

Un-styled by design, not by oversight — the file's own header comment states plainly that the visual design pass comes later (root `docs/ROADMAP.md` Phase 3), and building a one-off visual language here would only be thrown away.

## 3. Map Rendering

### 3.1 `map/MapOverlays`

Owns exactly three layers — `tracks` (a `VectorSource` MVT layer), `fog`, and `heatmap` (both `RasterSource` layers, 512px tiles matching the server's own raster tile size, not the MapLibre default of 256) — and the logic to add, remove, and toggle them.

- **`attach`/`detach` are idempotent and re-runnable**, checked with `style.getSource(id) == null` / `getLayer(id) == null` before adding anything. This matters because a style *reload* (switching from `light` to `dark` when the system's day/night setting changes) discards any custom layers a previous `attach` call added — this is not a one-shot setup path.
- **Insertion point is computed, not hardcoded**: `labelInsertionPoint` finds the basemap's first `SymbolLayer` and every custom layer is added *below* it (`addLayerBelow`), so place labels always render on top. Within that, both rasters are added before the tracks vector layer, so tracks paint above whichever raster is currently visible — the same ordering rationale `docs/IMPLEMENTATION.md` §4.2's client-compositing section gives for the web client, reproduced here layer-by-layer rather than shared code, since there is no shared layer-ordering module between the two clients.
- **`setMode` only changes `visibility`**, never adds or removes anything — all three layers exist on the style simultaneously once attached; the toggle is purely a paint-property flip (`Property.VISIBLE` / `Property.NONE`). This keeps mode switching instantaneous and avoids re-adding sources on every toggle.
- **Tile URLs carry no query parameters at all.** `tileUrl(kind, ext)` builds a bare `{z}/{x}/{y}` template; the server's `from`/`to`/`types`/`exclude` filters are simply never sent, which the server already treats as "no restriction" — so this is the whole account history, unfiltered, by omission rather than by an explicit "all" parameter.
- **`TRACK_COLOR = "#b07e2e"` is called out in its own comment as not a token to fold into a future design system** — it is the color `apps/web/src/map/tracks.ts` paints tracks with, and belongs to the shared map style's visual identity across clients, not to this app's own theming.

## 4. Health Connect Integration

### 4.1 `health/HealthConnect`

Two things live here: the three permission constants, and the `Readiness` state machine (`docs/SPEC.md` FR-3.1 specifies the six states from the user's perspective).

- **`READ_EXERCISE_ROUTES` is a raw string constant**, not a library-provided one — `androidx.health.connect` ships a write-side constant for this permission but no read-side one, consistent with a permission the platform won't let an app request programmatically in the first place (verified directly: requesting it alongside the other two silently omits it from what's granted, without ever raising an error).
- **`READ_HISTORY` uses the library's own constant** (`HealthPermission.PERMISSION_READ_HEALTH_DATA_HISTORY`) because, unlike routes, it *is* programmatically requestable — it rides in the same permission-request call as `READ_EXERCISE`.
- **`readiness(context)` is computed fresh on every call, never cached** — the routes permission specifically can only be granted by leaving this app (into Health Connect's settings) and coming back, so caching it across that trip would show a stale state at the exact moment the user needs to see it update.
- **`settingsIntent()` targets Health Connect's home screen (`HEALTH_HOME_SETTINGS`), not its per-app permission page.** The obvious API for the latter, `ACTION_MANAGE_HEALTH_PERMISSIONS`, is guarded by the signature-level `GRANT_RUNTIME_PERMISSIONS` permission — an ordinary app calling it gets a `SecurityException` that kills the process. This was found by hitting the crash on a device, then confirmed a second way by querying the intent resolver directly, and is why `SyncActivity`'s onboarding screen has to spell out the four-tap path by hand instead of deep-linking to it.

### 4.2 `health/ExerciseTypes`

A hand-written `Map<Int, String>` from Health Connect's `EXERCISE_TYPE_*` constants to FitMap's `activity_type` vocabulary.

- **Written against public constants, not the library's own `EXERCISE_TYPE_INT_TO_STRING_MAP`**, which exists and would save writing this table out, but is annotated `@RestrictTo` (library-group internal, removable without a semver bump) — a dependency this code doesn't want to take. Every entry is a named constant on the left-hand side rather than a bare integer literal specifically so a transcription mistake is a compile error, not a silently mislabeled activity type at runtime.
- **A handful of entries deliberately rename Health Connect's own term**: `EXERCISE_TYPE_BIKING`/`BIKING_STATIONARY` → `"cycling"`, `RUNNING_TREADMILL` → `"running"`, both `SWIMMING_OPEN_WATER`/`SWIMMING_POOL` → `"swimming"`. These renames exist because FitMap's other ingest paths already speak the FIT-file vocabulary, and a mismatch here would split one real activity type into two in the Activities panel's TYPE filter, and — more consequentially — break cross-source deduplication, whose matching window is scoped by `(user_id, activity_type, started_at)` (`docs/IMPLEMENTATION.md` §3.3, §4.6): a bike ride arriving as `"biking"` from Health Connect and `"cycling"` from a `.FIT` upload would never even be compared for a dedupe match, let alone merged.
- **`name()` falls back to `"unknown"`** for any exercise type this table doesn't cover — the same fallback value the server itself uses for an activity that arrives with no type at all, so an unmapped Health Connect constant degrades to a value already meaningful elsewhere in the system rather than to something new.
- **Indoor types are included for completeness, not because they're ever sent**: a session with no route never reaches `prepare()` in `SyncRunner` at all (§5.2), so in practice only outdoor types are ever looked up here.

### 4.3 AndroidManifest permission declarations

Exactly three Health Connect permission declarations, matching `health.HealthConnect`'s three constants one-to-one, and no more — deliberately, since declaring an unused data type is a documented Play Store rejection cause (`docs/VISION.md` §7). `READ_HEALTH_DATA_IN_BACKGROUND` is conspicuously absent: Phase 1 measured that granting it changes nothing for route reads (the same 46-session test returned 23 routes in the foreground and zero in the background whether or not this was granted), so declaring it would only be asking for an alarming-sounding permission that buys the app nothing.

`SyncActivity` carries two manifest-level registrations beyond being a normal exported activity: an intent filter for `androidx.health.ACTION_SHOW_PERMISSIONS_RATIONALE` (Health Connect launches this screen itself to show its own rationale for the request, and without a registered handler the permission dialog can refuse to appear at all — found in the Phase 1 proof-of-concept), and an `activity-alias` named `ViewPermissionUsageActivity`, guarded by `START_VIEW_PERMISSION_USAGE` and targeting `SyncActivity`, which is where Health Connect's own "see how this app used your data" link lands.

## 5. Sync Engine

### 5.1 `SyncActivity`

The onboarding UI plus the run's lifecycle owner. Implements the six-state `render()` switch `docs/SPEC.md` FR-3.1 specifies from the outside; the two implementation details worth calling out beyond that spec:

- **`refresh()` is a no-op while `syncJob` is non-null.** Readiness is re-derived on every `onResume`, and a running sync's own coroutine doesn't want its buttons and status text stomped by a concurrent readiness re-render.
- **`onStop` cancels `syncJob` unconditionally.** This is not cleanup after the fact — it *is* the foreground boundary the whole sync design depends on (`docs/ARCHITECTURE.md` §1.1). Because `SyncCursor` only ever advances over confirmed-terminal segments (§5.3), cancellation here is safe by construction: there is no in-flight, half-applied watermark state for `onStop` to worry about tearing down.
- **`openSettings()` catches broadly (`Exception`, not just `ActivityNotFoundException`)** because the actual failure encountered in development was a `SecurityException` from an earlier attempt at `ACTION_MANAGE_HEALTH_PERMISSIONS` (§4.1) — a button that can't open a screen reports that in the status text rather than crashing the app around it.

### 5.2 `sync/SyncRunner`

One foreground run: page through Health Connect ascending from the watermark, classify each record, batch and post the ones with usable geometry, and advance the watermark only over what's confirmed.

- **The classification into terminal vs. blocking happens inline in the paging loop**, not as a separate pass — `ExerciseRouteResult.Data` is prepared and added to the pending batch; `NoData` is counted and added straight to the current *segment* (eligible for the watermark) without ever entering a batch; anything else (`ConsentRequired`) sets `stoppedBecause` and breaks out of the paging loop entirely, mid-page if necessary.
- **`segment` and `batch` are different lists with different lifetimes.** `segment` accumulates every record dealt with since the watermark last moved (both route and no-route ones) and is what `advanceOverSegment()` reads to compute the new watermark position; `batch`/`batchRecords`/`batchPoints` accumulate only records with geometry, pending a `POST` to the sync endpoint, and are cleared on every `flush()` regardless of outcome. A record is only removed from active tracking once it's in `segment` — meaning a record that fails preparation (too many/too few points) still lands in `segment`, because that rejection is itself terminal (§5.3).
- **`flush()` is called both when a batch limit is hit mid-page and once, unconditionally, after the paging loop ends** — the second call is what sends whatever was still buffered when the loop stopped, including when it stopped because of a blocking record: everything gathered *before* that record is still legitimately finished with and must not be abandoned just because something later in the page couldn't be read.
- **A failed request inside `flush()` clears the batch without moving the watermark**, and returns the exception's message as the stop reason. Nothing in an unanswered batch is terminal — the request might succeed on retry, so guessing either way (assume success and advance, or assume failure and skip) is exactly the mistake `SyncCursor`'s design exists to prevent.
- **`prepare()` builds each activity's JSON off the main thread** (`Dispatchers.Default`), because a long ride's point array is real serialization work — tens of thousands of points is the documented ceiling this file itself checks against (`MAX_POINTS_PER_ACTIVITY = 50_000`, matching the server's own limit in `docs/IMPLEMENTATION.md` §4.0.3, checked client-side so a record that could never be accepted is reported as a rejection instead of being sent only to be refused).
- **Elevation is included per-point only when the platform location object actually carries it**; there is no default-to-zero, because zero is a real, wrong value for "unknown altitude" (a hazard the file's own comment calls out explicitly).
- **Batch limits (25 activities / 20,000 points) are stricter than the server's own ceiling (100 activities per request)**, deliberately: a smaller batch keeps a single mobile-data request modest, and makes the watermark advance more often during a long first backfill — a larger batch would mean more work re-attempted if any single request failed.

### 5.3 `sync/SyncCursor`

The watermark itself: an `Instant` plus a `Set<String>` of record ids already handled at that exact instant, persisted per account in its own `SharedPreferences` file (`fitmap.sync`).

- **A bare instant is not sufficient as a position, and this is the one subtlety in the whole sync engine worth re-reading before touching it.** Health Connect's `TimeRangeFilter.after(instant)` is *inclusive* of that instant, so the record the cursor is parked on is read again on the very next run. Simply excluding that instant from the next read would be wrong in the other direction, because two distinct sessions can genuinely share an identical start time — excluding the instant outright would silently drop whichever sibling didn't happen to set the watermark. `handledAtCursor` is the set of ids already dealt with at exactly that instant, and it's what lets the next run skip only those specific records while still picking up any newly-appearing sibling at the same instant.
- **`advanceTo` merges rather than replaces `handledAtCursor` when the new instant equals the current one.** Two sessions sharing a start time can land in different batches within the same run (if a batch-size limit falls between them), and each `flush()`/`advanceOverSegment()` call advances the watermark independently — so the ids recorded for a given instant have to accumulate across calls within a run, not just across runs.
- **`advanceTo` refuses to move backwards.** Defensive against a run that, for whatever reason, reads a narrower window than a previous run already covered — moving the watermark earlier would re-open a range of history already confirmed closed.
- **Keyed by account (email string, empty for a demo account) rather than globally**, in its own preference keys (`at.$account`, `ids.$account`) inside one shared file — this is what prevents two people using the same physical device from inheriting each other's sync position, and what lets the same account resume correctly across a sign-out/sign-in cycle.
- **`reset()` exists but has no UI entry point today** — it's the mechanism a full re-walk would use, relying on the server's idempotency (keyed on the platform's own record id, not a content hash — `docs/IMPLEMENTATION.md` §4.0.3) to make that safe, but nothing in the app currently calls it.

## 6. Sync Status & Duplicates

### `SyncStatusActivity`

Reads two independent endpoints and renders them as two independent sections — deliberately not merged into one list, since "why is this activity not on my map" has two structurally different answers (a fault, or a duplicate) and collapsing them would make the second one look like an instance of the first.

- **Polling is conditional and self-terminating.** `load()` schedules another `load()` via a `Handler.postDelayed` only when the just-fetched page's `processing` count is nonzero, and `main.removeCallbacks(poll)` runs before every reschedule so at most one pending callback ever exists — a screen full of settled rows makes zero further requests once nothing is left in flight.
- **Row labels prefer what the activity actually became over its raw identifier.** `describe()` shows the activity's own date and distance once a job reaches `"done"`; a synced Health Connect job's `source_detail` is the platform's own record UUID, which is meaningless to a person and is never surfaced directly.
- **`sourceName()` is a small closed mapping from the schema's `source` values** (`docs/IMPLEMENTATION.md` §3.3) to human-readable labels, used identically for both the sync-history rows and the duplicate rows, so "Health Connect" and "HealthKit" read the same way in both sections.

## 7. In-App GPS Recording

`recording/` (`RecordingActivity`, `RecordingService`, `RecordedActivitiesActivity`, `recording/db`) owns this feature end to end. The wire contract it submits to — `POST /v1/sync/activities` under `source = "recorded"` — is specified server-side in `docs/IMPLEMENTATION.md` §4.0.4, along with [ADR-0007](../../../docs/adr/0007-in-app-gps-recording-submits-directly.md)'s decision not to add a new endpoint for it. What follows is the client half that decision left to this app to design.

### 7.1 Recording and submitting are two separate steps, not one

Stopping a recording only persists it to `recording/db/RecordedActivityStore.kt`, tagged not-synced; nothing reaches the network until the user marks it to sync from `RecordedActivitiesActivity` (a list filterable by sync status × activity type) and taps the Sync screen's existing "Sync Now." That screen's `flushRecordedQueue` (`SyncActivity.kt`) walks every locally queued row in the same run as Health Connect sync (§5), submitting each as its own call to `POST /v1/sync/activities` and marking it synced on `"enqueued"`/`"already_processed"`. A rejected or failed row is left queued rather than reverted, so the next "Sync Now" retries it with no action needed from the user — the same resumable-retry posture `SyncCursor` (§5.3) already has for Health Connect sync, applied to a per-row local status instead of a single cursor position.

`activity_type` is free text here, not the fixed vocabulary `health/ExerciseTypes.kt` (§4.2) normalizes Health Connect sessions onto — there's no platform exercise-type field to normalize from on this path. `external_id` is a UUID the app mints at recording start (`java.util.UUID.randomUUID()`), not a platform record id — unlike Health Connect/HealthKit, there is no platform health store handing this activity an identity, so the client originates one itself.

### 7.2 `recording/db/RecordedActivityStore`

Plain `SQLiteOpenHelper`, not Room: this project's Kotlin toolchain — AGP's built-in Kotlin at the time this was built — was newer than any published KSP release, confirmed by trying it (a `com.google.devtools.ksp` version matching Kotlin 2.4.0 does not exist on Maven Central) rather than assumed from documentation.

**The table is scoped per account, and wasn't at first — a real bug, found by testing a demo-then-real-account switch on one device, not by inspection.** The first version carried no account column at all: every row was visible regardless of which account was currently signed in, so a recording made under one account (or the shared demo session) would show up in another's Recorded Activities list on the same device. Fixed by adding an `account` column and scoping every query (`all`, `get`, `queued`, `update`, `setSyncStatus`, `delete`) to `Session.email` — the same per-account key `sync/SyncCursor.kt` (§5.3) already established for exactly this reason, including that class's own precedent for a demo account sharing one empty-string key across every demo session. That sharing is harmless here for the same reason it's harmless there: a demo account can never sync regardless (§7.4), so nothing recorded under it ever reaches the server no matter which demo session later sees it locally. `RecordingDbHelper` bumped to schema version 2; `onUpgrade` drops and recreates rather than a hand-written `ALTER TABLE`, the same "no migration tooling needed pre-launch" call the server side already makes.

### 7.3 Delete is local-only

Available on every row regardless of sync status, unlike Edit — there's nothing left to protect once a row can only ever be deleted, not corrupted. For an already-synced row this only removes Recorded Activities' own bookkeeping; the real `Activity` it produced is untouched, because nothing captured here links back to that row's server-assigned id — only the client-generated `external_id` it was submitted under. This is a deliberate scope decision, not an oversight: doing the full purge too (mirroring `handleDeleteActivity`, `docs/IMPLEMENTATION.md` §4.7.4) would mean threading the server's assigned activity id back through the sync response, which nothing today captures. The confirmation dialog says this explicitly for a synced row.

### 7.4 Demo accounts are blocked from sync client-side too

Matching the web's own pattern (`UploadPanel.tsx`'s `readOnly`-plus-tooltip treatment), not just the server's `requireNotDemo`. Found missing, not assumed present: neither `SyncActivity` nor `RecordedActivitiesActivity` checked demo status before this pass, so a demo session could open Sync, tap "Sync Now," or queue a recording, and would only discover the block from the server's raw `demo_read_only` JSON surfacing through a generic failure path — confirmed live, by accident, before the fix. `Session.isDemo` (§2.1, `isSignedIn && email.isEmpty()` — the same emptiness the class already used to mean "demo") is now checked in both places: the Sync screen hides Sync Now, the secondary button, and the Health Connect permission flow behind an explanatory message (sync history stays visible — reading is not a mutation), and Recorded Activities disables every sync checkbox with a banner.

### 7.5 Verified on device

On a Pixel 10a: recorded a short walk with a custom, free-text activity type; confirmed the row landed in Recorded Activities as not-synced with no network request having been made; edited its name/description/type pre-sync and confirmed the edit persisted; checked its sync box (queued); tapped "Sync Now" and confirmed the row synced alongside a Health Connect run in the same combined summary text; confirmed the row's checkbox became checked-and-disabled and its Edit screen became read-only with "Already synced — no longer editable." A real failure case surfaced by accident (the session had signed into the demo account, whose writes the server correctly refuses) confirmed the failed-row behavior live: the row stayed queued and the combined summary reported "N synced, 1 failed — will retry next time" rather than silently dropping it. A second pass verified Delete (removed a synced row locally, confirmed the underlying `activities` row was untouched server-side) and the demo-gating and account-scoping fixes together: signed into the demo account, confirmed Sync Now/Open Health Connect were both hidden with the read-only explanation and Recorded Activities' checkbox rendered `enabled="false"`; recorded under the demo session, confirmed the row appeared locally; signed out and into a real account and confirmed that demo-authored row did **not** appear in the real account's Recorded Activities list.

## 8. Build Configuration

`BuildConfig.API_BASE_URL` is the one build-time-injected value in the app, sourced from the `fitmap.apiBaseUrl` Gradle property (`gradle.properties` defaults it to `http://10.0.2.2:8080`, the emulator's alias for the host loopback). Pointing a physical device at a host dev stack over USB requires `adb reverse` for both the API port and the web dev server port the style document's asset URLs resolve against (`BASEMAP_ORIGIN`, `docs/ARCHITECTURE.md` §2.1) — documented with exact commands in `apps/android/fitmap/README.md`.

**Cleartext `http://` traffic is permitted in debug builds only** (`app/src/debug/AndroidManifest.xml`), so a release build cannot be pointed at a plaintext dev endpoint by accident — the network security configuration itself is what enforces this, not a code-review convention.

`libs.versions.toml` pins every dependency version with an inline rationale for why that version specifically (MapLibre's pmtiles-support floor, the Health Connect version the Phase 1 findings were measured against, OkHttp pinned to what MapLibre's own POM resolves to rather than left implicit) — the versions themselves are covered in `apps/android/docs/ARCHITECTURE.md` §2; this file is where the *reasoning* for each pin lives, so an upgrade is a decision made against that reasoning rather than a blind bump.

## 9. Verification Methodology & Known Gaps

**There are no automated tests anywhere in this app** — no unit tests, no instrumentation tests, no CI. Every behavior described in `apps/android/docs/SPEC.md` has instead been verified manually, on a physical device (a Pixel 10a running Android 17/API 37 throughout), against a live `db`/`minio`/`api`/`worker` stack, with the specific steps and observed results recorded narratively in `apps/android/docs/ROADMAP.md` under each phase — for example, the sync engine's watermark correctness was verified by forcing a mid-run API outage and confirming a subsequent run recovered every record with zero duplicates, and the layer-ordering/visibility logic in `map/MapOverlays` was verified by screenshotting all three map modes against a real account.

This is a real gap relative to the server codebase, which the app inherits no test infrastructure from, and is worth naming rather than leaving implicit:

- **Regressions are only caught by re-running the manual verification steps**, which do not run automatically and are not gated in CI. A change to `SyncCursor` or `SyncRunner` in particular should be re-verified against the specific scenarios `apps/android/docs/ROADMAP.md` Phase 3/4 already walked (mid-run failure, a `ConsentRequired` route, two sessions sharing a start instant), since those are exactly the edge cases a quick manual smoke test tends to skip.
- **Verification has only ever run against one physical device and one Health Connect data provider (Fitbit).** The empirical Samsung Health check (`apps/android/docs/ROADMAP.md` Phase 1) remains unresolved for lack of a Galaxy Watch to test against — the current "Samsung is unsupported" conclusion rests on Samsung's own published documentation plus this app's design stance, not on a device-level measurement the way every other finding in this document is.
- **No test double exists for Health Connect.** `SyncRunner` and `SyncCursor` are exercised only against a real `HealthConnectClient` on a real device; there is no fake or mock implementation of the client to run these classes against in isolation.
