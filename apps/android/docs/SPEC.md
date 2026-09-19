# FitMap for Android: Spec

| | |
| :-- | :-- |
| **Version** | 1.0 |
| **Status** | Current — describes the app as built through Phase 4 of `apps/android/docs/ROADMAP.md` |
| **Last updated** | 2026-09-17 (scope note updated same day for the planned in-app GPS recording capability — see §1.2, §7) |
| **Related documents** | `apps/android/docs/ROADMAP.md` (the phase plan, platform-constraint findings, and the verification record this document's behavior claims are drawn from — the authority on *how each finding was reached*); `apps/android/docs/ARCHITECTURE.md` (this app's shape, stack, and key decisions); `apps/android/docs/IMPLEMENTATION.md` (file-by-file "how it's built" detail); `docs/SPEC.md`/`docs/IMPLEMENTATION.md` (the server behavior and schema this app is a client of); `docs/VISION.md` (why Path 2 exists at all, §4.1 and §5.4) |

## 1. Introduction

### 1.1 Purpose

This document specifies the Android app's functional behavior as currently implemented: what it does, from the point of view of the person holding the phone. It does not cover why it was built this way (`apps/android/docs/ARCHITECTURE.md`, `docs/VISION.md`) or the file-level detail of how (`apps/android/docs/IMPLEMENTATION.md`). Server-side behavior this app merely calls into — session semantics, ingestion, fog/heatmap rendering — is specified once, in `docs/SPEC.md`, and referenced rather than restated here.

### 1.2 Scope

**In scope**: everything currently built and verified on a physical device — sign in, sign up, and starting a demo account (FR-1); session persistence and server-side re-verification on cold start (FR-1); a full-screen map with Normal, Fog of War, and Heatmap modes over the account's entire history (FR-2); Health Connect onboarding and permission acquisition (FR-3); a foreground sync run with resumable watermark tracking (FR-3); per-activity sync rejection feedback (FR-3); and a sync status/history screen that also surfaces cross-source duplicates (FR-4).

**Out of scope, not yet built**: everything root `docs/ROADMAP.md` Phase 3 (design finalization) and `apps/android/docs/ROADMAP.md` Phase 5 gate — a visual design system, an icon set, a launcher icon, and the date-range/type/hidden-track filter controls the web client already has (the map here always shows the account's unfiltered, entire history). Also not yet built: casual, GPS-only in-app recording (`docs/VISION.md` §1.1, §4.1) — a planned, deliberately separate capability from Health Connect sync, tracked as `apps/android/docs/ROADMAP.md` Phase 7 and not covered by any FR below yet. Out of scope as a platform limitation rather than a "not yet": background Health Connect sync (will not be built — see §3.3 note; this does not apply to the planned in-app recorder, which is not subject to the same platform constraint — [ADR-0007](../../../docs/adr/0007-in-app-gps-recording-submits-directly.md)), and Samsung Galaxy Watch sync (Samsung does not expose route geometry to Health Connect at all, so every Samsung-sourced session is rejected for having no route, same as any other route-less activity). iOS does not exist. Accessibility (content descriptions, touch-target sizing, large-font and TalkBack testing) has not been done and is scoped to Phase 5. See §7 for the complete list.

### 1.3 Intended audience

Engineers modifying this app, anyone verifying its behavior against a device, and anyone needing an authoritative answer to "what does the Android app do in this situation" without reading Kotlin source.

### 1.4 Definitions

In addition to `docs/SPEC.md` §1.4's terms, which this document assumes:

| Term | Meaning |
| :-- | :-- |
| **Session record** | A Health Connect `ExerciseSessionRecord` — one exercise entry in the platform health store, with a start/end time, an exercise type, and (sometimes) route geometry. Not to be confused with a FitMap login session. |
| **Route / route geometry** | The GPS trajectory attached to a Health Connect session record, read via `ExerciseRouteResult`. A session can exist with no route (an indoor workout). |
| **Readiness** | The app's own name (`health.HealthConnect.Readiness`) for where the user currently stands in the permission-acquisition sequence — one of six states, §3.1. |
| **Watermark / sync cursor** | The point in the account's Health Connect history the last sync run confirmed as fully handled; the next run resumes from there rather than re-reading from the beginning (§3.3). |

## 2. Actors

| Actor | Description |
| :-- | :-- |
| **Anonymous visitor** | Has not signed in. Sees a full-screen, working basemap with no user layers — the style endpoint is unauthenticated, so a signed-out FitMap is a real map, not a login wall. Can reach the sign-in screen from the map's account button. |
| **Demo user** | Holds a session tied to an ephemeral account (`docs/SPEC.md` FR-2), started from this app exactly as from the web. Full functional access to every feature in this document; the account expires per `docs/SPEC.md` FR-2.2 regardless of which client created it. |
| **Registered user** | Holds a session tied to a permanent account. Full functional access to every feature in this document, including Health Connect sync — a demo account can sync too, since sync has no account-type branch. |

There is no administrator role and no cross-account visibility, exactly as `docs/SPEC.md` §2 states for the system as a whole.

## 3. FR-1 — Authentication & Session

### FR-1.1 Sign in, sign up, start a demo

**Description**: From a single screen (`SignInActivity`), an anonymous visitor signs in to an existing account, creates a new one, or starts a no-signup demo — the same three operations `docs/SPEC.md` FR-1.1/FR-1.2/FR-2.1 define server-side.

**Preconditions**: No active session.

**Inputs**: For sign-in/sign-up, an email address and password, checked client-side only for "both fields non-empty" — every other validation (address format, password length, whether the account exists) is left to the server, and the server's own response text is shown verbatim rather than reworded, so the two can never disagree about what is acceptable. Starting a demo takes no input.

**Behavior**:
1. Sign in calls `POST /v1/auth/login`; sign up calls `POST /v1/auth/signup`; demo calls `POST /v1/auth/demo` — the same three endpoints the web client uses.
2. On success, the response's `session_token` field (present on all four session-minting endpoints, specifically for a native caller that has no cookie jar to rely on) is stored via `Session.start`, and the screen closes back to the map.
3. On failure, the server's own error message is shown inline; the three action buttons are disabled while a request is in flight and re-enabled after.

**Outputs**: A stored bearer token and the account's email (empty for a demo account, which the server deliberately never returns a real email for).

**Notes**: This screen is dismissable back to the map without signing in — reachable, not mandatory, because the map itself is usable while signed out (§4.1).

### FR-1.2 Session persistence and re-verification

**Description**: A session survives the app being closed and relaunched, but a restored token is not trusted until the server confirms it — the same caution `docs/SPEC.md` FR-1.4 describes for the web client, applied to a store that isn't a cookie jar.

**Behavior**:
1. On cold start, if a token is stored but not yet marked verified in this process, the map defers attaching any signed-in-only layer, shows "Checking session…", and calls `GET /v1/auth/me`.
2. A `200` marks the token verified for the rest of the process's lifetime; the map then proceeds as signed in.
3. A `401` — and only a `401` — clears the stored token and drops to the signed-out map. Any other failure (no network, an unreachable dev stack) leaves the token in place and is retried on the next resume, since it says nothing about whether the credential itself is still good.

**Outputs**: Either a confirmed, attached session, or a clean drop to signed-out — never a map that silently 401s on every tile request while looking merely blank.

### FR-1.3 Sign out

**Description**: Ends the current session, both on the device and on the server.

**Behavior**: `POST /v1/auth/logout` deletes the session row server-side; the stored token and email are cleared locally regardless of whether the request succeeded (a token that can't reach the server to be revoked is not one worth keeping either way). The map's user layers detach immediately and the mode toggle bar hides; the map itself remains, at whatever mode and camera position it was showing.

## 4. FR-2 — Map Visualization

### FR-2.1 Base map, always available

**Description**: A full-screen map renders on launch regardless of session state.

**Behavior**: The style document is fetched unauthenticated from `GET /v1/map/style/{flavor}`, `flavor` chosen from the system's day/night setting (`light` or `dark` — two of the five the API serves; the app does not offer a way to pick the other three or to override the system setting, per `apps/android/docs/ARCHITECTURE.md` §2.1). The camera opens on a whole-world view (equator, zoom 1) until an account's own activity extent is known (FR-2.3). A style or tile load failure is reported on screen as visible text, not only in logcat.

### FR-2.2 Three map modes: Normal, Fog of War, Heatmap

**Description**: The same three mutually exclusive views `docs/SPEC.md` FR-4.1–FR-4.4 define, switched by one three-way toggle in the map's bottom bar. Only visible/available once signed in.

**Behavior**:
1. **Normal** draws the account's tracks as a single-color vector line layer (`GET /tiles/v1/tracks/{z}/{x}/{y}.mvt`).
2. **Fog** replaces the tracks with the server-rendered dark-veil raster (`GET /tiles/v1/fog/{z}/{x}/{y}.png`); tracks are hidden.
3. **Heatmap** replaces the tracks with the server-rendered intensity raster (`GET /tiles/v1/heatmap/{z}/{x}/{y}.png`); tracks are hidden.
4. All three layers sit beneath the basemap's first label layer, so place names stay legible; within that, the active raster (fog or heatmap) is drawn beneath the tracks layer so a cleared route reads as visible through the fog rather than obscured by it — the same ordering the web client uses.
5. The active mode's button is shown bold and at full opacity; the other two are dimmed.

**Notes — no filtering**: Every tile URL is unfiltered. All three endpoints accept `from`/`to`/`types`/`exclude`, and an absent filter already means "no restriction" server-side, so this app always shows the account's complete history — there is no date-range picker, TYPE filter, or per-track hide/show control on Android today (Phase 5 item, `apps/android/docs/ROADMAP.md`). This is a scope gap relative to the web client, not a bug.

**Notes — source of the drawn data**: The map is a pure read against these three server endpoints; it never reads Health Connect directly, at any mode, and Health Connect sync (FR-3) never touches the map. An activity appears here only once FR-3's sync run has posted it to the server and the server has ingested it — there is no direct, on-device path from a Health Connect record to a drawn track (`apps/android/docs/ARCHITECTURE.md` §1).

### FR-2.3 Initial camera framing

**Description**: On first attaching the user layers each app session, the camera flies to fit the account's full activity extent, once.

**Behavior**: `GET /v1/activities` is read once per session start, and the bounding box of every row's `bbox` field is unioned client-side (rows with no `bbox` — no recorded trajectory — are skipped). The camera animates to fit that box, capped at zoom 15 so a single very short or heavily privacy-trimmed activity doesn't zoom in on an empty rectangle past the basemap's own z14 data. An account with no geometry at all stays at the whole-world view. Re-attaching the session (e.g., returning from the sign-in screen without actually changing account) does not re-fly the camera a second time in the same app session.

### FR-2.4 Attribution

**Description**: MapLibre Native's own attribution control is left enabled and renders the ODbL credit the style document's basemap source carries — required, not decorative, since the Protomaps basemap is a Produced Work under that license (`docs/IMPLEMENTATION.md` §5.6).

## 5. FR-3 — Health Connect Sync (Path 2)

All of FR-3 requires an active session (demo or registered); Health Connect sync has no unauthenticated path, and syncing to a demo account works identically to a registered one.

### FR-3.1 Onboarding — the readiness state machine

**Description**: Before any sync can run, the user is walked through a sequence of states, each with exactly one resolving action, reached via `SyncActivity`.

**Behavior**: On every resume, the app re-derives one of six states and renders accordingly (state names from `health.HealthConnect.Readiness`):

1. **Unavailable** — no Health Connect on the device. No action is offered; the sync feature is not usable here.
2. **Update required** — Health Connect is present but needs a Play update. The primary action opens Health Connect's own home screen.
3. **Needs exercise permission** — neither `READ_EXERCISE` nor `READ_HEALTH_DATA_HISTORY` is granted yet. The primary action opens the ordinary system permission dialog for both at once.
4. **Needs routes permission** — sessions are readable, routes are not. `READ_EXERCISE_ROUTES` cannot be requested programmatically at all (a platform limitation, confirmed on-device — requesting it alongside the others silently grants the others and omits it). The screen names the exact path instead: *Health Connect → FitMap → Additional access → Access exercise routes → Always allow*, and the primary action opens Health Connect's home screen as the closest a third-party app can get the user there.
5. **Needs history permission** — sessions and routes are both readable, but Health Connect will only serve the last 30 days without `READ_HEALTH_DATA_HISTORY`. Unlike the routes permission, this one *is* requestable, so the primary action opens the ordinary system dialog for it. This state is not blocking: a secondary "Sync now anyway" action is always available, since declining history access is a legitimate answer and syncing a shallower window is still useful.
6. **Ready** — every permission granted; the primary action starts a sync run (§5.2).

**Notes**: This same screen is registered for Health Connect's `ACTION_SHOW_PERMISSIONS_RATIONALE` intent, so when Health Connect itself asks the user why FitMap wants their data, it opens this screen — meaning the rationale a user sees inside Health Connect and the explanation they see inside FitMap are the same text, by construction rather than by two authors staying in sync. Readiness is re-read on every resume rather than cached, since the routes permission specifically can only change in another app's settings screen, and returning from it is the only moment this screen can observe that.

### FR-3.2 Foreground sync run

**Description**: Reads exercise sessions from Health Connect in ascending order, sends the ones with usable route geometry to the server, and reports what happened — entirely while the sync screen is on screen.

**Preconditions**: Readiness is `READY`, or the user chose "Sync now anyway" from the history-permission state.

**Behavior**:
1. Sessions are read paged (50 per Health Connect request), starting from the account's watermark (§5.3) or from the beginning if never synced.
2. Each session is classified: a route present and readable is prepared for sending; a session with confirmed no route (`ExerciseRouteResult.NoData`) is counted as skipped, not an error — this is the ordinary case for an indoor workout, a gym session, a swim, or a rowing machine, none of which have a trajectory to begin with; a session whose route exists but could not be read right now (`ExerciseRouteResult.ConsentRequired`) stops the run entirely (§5.3).
3. Sessions with a route are batched — up to 25 activities or 20,000 points per batch, whichever comes first — and posted to `POST /v1/sync/activities` with `source: "healthconnect"`. Each activity's `activity_type` is normalized from Health Connect's own vocabulary onto the one FitMap's other ingest paths already use (e.g., Health Connect's `biking` becomes `cycling`; `running_treadmill` becomes `running`), so the same physical ride synced from a watch and uploaded from a file describes itself the same way — required for both the Activities panel's TYPE filter and cross-source deduplication (`docs/SPEC.md` FR-5.2, `docs/IMPLEMENTATION.md` §4.6) to work correctly. Elevation is included per-point only when the location itself carries it; heart rate is never sent, since it lives behind a Health Connect permission this app does not declare (§6, `docs/VISION.md` §7).
4. The screen shows live progress ("Scanned N, synced M") while the run is in flight, and a summary once it finishes or stops (§5.4).
5. The run is cancelled the instant the screen leaves the foreground (`onStop`) — this is a platform constraint, not a preference: Health Connect returns `ConsentRequired` for routes read in the background regardless of what has been granted, so nothing here is designed to survive backgrounding, and nothing here schedules a retry on its own.

**Outputs**: Zero or more activities ingested through the shared pipeline (`docs/IMPLEMENTATION.md` §4.1), a moved watermark covering everything the run confirmed, and an on-screen report of what happened to each session.

### FR-3.3 Sync watermark — safe cancellation and resumption

**Description**: A sync run can be interrupted at any point — the screen backgrounded, the app killed, a network failure — without losing or duplicating data, and without silently skipping a session whose route was never actually read.

**Behavior**: The watermark only ever advances over a *segment* of records whose outcome is confirmed terminal — the server accepted it, the server already had it, the server permanently refused it, or it never had a route to begin with. It never advances past a record that hit `ConsentRequired` or a failed request, because either of those could still be correct on a later attempt, and moving past it would mean that record is never looked at again. A stalled or failed run therefore always resumes exactly where it left off; a re-walk from the beginning (only possible by explicitly resetting the cursor) costs server traffic, not duplicate rows, because the sync endpoint is idempotent on the platform's own record id.

**Notes**: The watermark is kept per signed-in account (keyed by email, empty string for a demo account), so switching accounts on the same device never inherits another account's sync position, and signing back into the same account resumes rather than restarts.

### FR-3.4 Sync rejection feedback

**Description**: Once a run finishes or stops, the user sees three distinct outcomes rather than one undifferentiated "done" or "failed" — because "no route" and "server refused it" are different things, and conflating them would make the second one impossible to notice.

**Behavior**: The summary reports: how many sessions were newly synced or already present; how many were skipped for having no route at all, phrased as an explanation rather than an error (e.g., "Skipped 32 recorded indoors — no route to map"); and, separately, each session the server or the app itself actually refused, alongside the specific reason (too few points, more points than one request accepts, or the server's own rejection text). If the run stopped early — a `ConsentRequired` route or a failed request — that is reported too, with guidance to keep the app in the foreground and try again, and an explicit statement that nothing after that point was skipped, only not yet attempted.

**Outputs**: A textual report on the sync screen. This is the run just watched, not a browsable history — that is FR-4.

## 6. FR-4 — Sync Status & Duplicates

### FR-4.1 Sync history

**Description**: A dedicated screen (`SyncStatusActivity`) lists every ingest job this account has ever produced, from any path — a Health Connect sync and a file upload from the web client appear in the same list, because server-side they are the same kind of job (`docs/IMPLEMENTATION.md` §4.0.1).

**Behavior**: Reads `GET /v1/uploads` (the same endpoint the web client's upload history uses). Each row shows when the activity happened and how far it went once it has finished processing, the server's own error text when it failed, or its submission time while still processing. The screen polls every two seconds only while at least one job is still processing, and stops entirely once everything has settled — a screen of finished rows makes no further requests.

**Outputs**: A list of rows plus overall counts (total, still processing).

### FR-4.2 Duplicates

**Description**: A second section on the same screen answers a question specific to having more than one ingest path: an activity can be missing from the map because it failed, or because it was already present from a different source — and only the first is a fault.

**Behavior**: Reads `GET /v1/activities/duplicates`. Each row names the activity's type and start time, the source it arrived from, and the source of the copy that superseded it (`docs/IMPLEMENTATION.md` §4.6) — phrased as, for example, "cycling from HealthKit — already here from Health Connect, so it is not drawn twice." The section is hidden entirely when there are no duplicates to show.

## 7. Non-Functional Requirements (summary)

This section summarizes cross-cutting behavior specified elsewhere in this document.

| Concern | Behavior |
| :-- | :-- |
| **Credential handling** | Bearer token, not a cookie — a session token is presented as `Authorization: Bearer <id>`, attached only to requests bound for FitMap's own API origin, never to third-party basemap-asset origins (FR-1.2). |
| **Session security** | Restored tokens are re-verified against the server before any signed-in layer is attached; sign-out revokes server-side (FR-1.2, FR-1.3). |
| **Foreground-only sync** | No background service or scheduled job ever attempts a Health Connect route read — a platform constraint, not a battery-saving choice (FR-3.2). |
| **Safe interruption** | A sync run cancelled at any point leaves no partial, unconfirmed state — the watermark only advances over confirmed-terminal records (FR-3.3). |
| **No reload required** | The sync history screen updates itself by polling while work is outstanding, and stops polling once settled (FR-4.1). |
| **Idempotency** | Re-syncing the same Health Connect record never creates a duplicate activity — the server keys on the platform's own record id (FR-3.2, `docs/IMPLEMENTATION.md` §4.0.3). |

## 8. Known Limitations & Out-of-Scope Items

Named here rather than left implicit, the way `docs/SPEC.md` §14 does for the wider system:

- **No visual design system.** No icon set, no launcher icon, no color/type/spacing tokens — four functional screens waiting on root `docs/ROADMAP.md` Phase 3's design freeze (`apps/android/docs/ROADMAP.md` Phase 5).
- **No filter controls.** The map always shows the account's complete, unfiltered history; there is no Android equivalent of the web's date-range picker, TYPE/DISTANCE filters, or per-track hide/show.
- **No accessibility work done.** No content descriptions, no verified touch-target sizing, untested under a large system font or TalkBack.
- **Samsung Galaxy Watch is unsupported**, not degraded — Samsung does not expose route geometry to Health Connect at all, so every Samsung-sourced session is rejected for having no route, indistinguishable at sync time from an ordinary indoor workout.
- **Background sync will never be built for Health Connect routes** — a platform constraint (`ConsentRequired` regardless of grant state when backgrounded), not a sequencing gap.
- **In-app GPS recording is planned, not built.** `docs/VISION.md` §1.1 and §4.1 now describe a casual, GPS-only recording capability for the mobile app — no heart rate/cadence/power, no training metrics, explicitly not a fitness-tracker replacement — tracked as `apps/android/docs/ROADMAP.md` Phase 7. It is architecturally distinct from Health Connect sync (FR-3 above): a finished recording submits directly to the server rather than round-tripping through Health Connect ([ADR-0007](../../../docs/adr/0007-in-app-gps-recording-submits-directly.md)), so none of FR-3's foreground-only reasoning necessarily carries over unchanged.
- **No automated tests.** Every behavior in this document has been verified manually against a physical device and a live server stack; see `apps/android/docs/IMPLEMENTATION.md` §8 and `apps/android/docs/ROADMAP.md` for the verification record.
- **iOS does not exist.** Path 2's HealthKit half is unbuilt; this app defines the sync contract iOS will inherit (`apps/android/docs/ROADMAP.md`).
- **`poc-healthconnect/` is not part of the product** — a throwaway diagnostic app, retained only until Phase 1's findings are fully absorbed elsewhere.
