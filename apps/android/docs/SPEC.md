# HoldMyTrack for Android: Spec

| | |
| :-- | :-- |
| **Version** | 1.0 |
| **Status** | Current — describes the app as built: sign-in, map, Health Connect sync and sync status (FR-1–FR-4), plus in-app GPS recording (FR-5, built ahead of the design and compliance phases — §1.2, §7 below) |
| **Last updated** | 2026-09-24 (§7: stop is a two-second hold with a countdown ring, and opens a Save screen) |
| **Related documents** | `apps/android/docs/ROADMAP.md` (remaining work and the platform-constraint findings; completed phases, with the verification record this document's behavior claims are drawn from, are removed from it once done and survive in its git history); `apps/android/docs/ARCHITECTURE.md` (this app's shape, stack, and key decisions); `apps/android/docs/IMPLEMENTATION.md` (file-by-file "how it's built" detail); `docs/SPEC.md`/`docs/IMPLEMENTATION.md` (the server behavior and schema this app is a client of); `docs/VISION.md` (why Path 2 exists at all, §4.1 and §5.4) |

## 1. Introduction

### 1.1 Purpose

This document specifies the Android app's functional behavior as currently implemented: what it does, from the point of view of the person holding the phone. It does not cover why it was built this way (`apps/android/docs/ARCHITECTURE.md`, `docs/VISION.md`) or the file-level detail of how (`apps/android/docs/IMPLEMENTATION.md`). Server-side behavior this app merely calls into — session semantics, ingestion, fog/heatmap rendering — is specified once, in `docs/SPEC.md`, and referenced rather than restated here.

### 1.2 Scope

**In scope**: everything currently built and verified on a physical device — sign in, sign up, and starting a demo account (FR-1); session persistence and server-side re-verification on cold start (FR-1); a full-screen map with Normal, Fog of War, and Heatmap modes over the account's entire history (FR-2); Health Connect onboarding and permission acquisition (FR-3); a foreground sync run with resumable watermark tracking (FR-3); per-activity sync rejection feedback (FR-3); and a sync status/history screen that also surfaces cross-source duplicates (FR-4). Also in scope, verified on an emulator rather than a physical device (noted where it matters — §7): casual in-app GPS recording started from a button on the map, with a live track on the map and notification controls, its Recorded Activities review/queue screen with a searchable activity-type picker in Edit with a route preview per row and GPX download, and the activity-type gate that keeps a queued recording from silently defeating FR-4's cross-source dedup (FR-5).

**Out of scope, not yet built**: everything root `docs/ROADMAP.md` Phase 3 (design finalization) and `apps/android/docs/ROADMAP.md` Phase 5 gate — a visual design system, an icon set, a launcher icon, and the date-range/type/hidden-track filter controls the web client already has (the map here always shows the account's unfiltered, entire history). Out of scope as a platform limitation rather than a "not yet": background Health Connect sync (will not be built — see §3.3 note; this does not apply to in-app recording, which is not subject to the same platform constraint — [ADR-0007](../../../docs/adr/0007-in-app-gps-recording-submits-directly.md)), and Samsung Galaxy Watch sync (Samsung does not expose route geometry to Health Connect at all, so every Samsung-sourced session is rejected for having no route, same as any other route-less activity). iOS does not exist. Accessibility (content descriptions, touch-target sizing, large-font and TalkBack testing) has not been done and is scoped to Phase 5. See §9 for the complete list.

### 1.3 Intended audience

Engineers modifying this app, anyone verifying its behavior against a device, and anyone needing an authoritative answer to "what does the Android app do in this situation" without reading Kotlin source.

### 1.4 Definitions

In addition to `docs/SPEC.md` §1.4's terms, which this document assumes:

| Term | Meaning |
| :-- | :-- |
| **Session record** | A Health Connect `ExerciseSessionRecord` — one exercise entry in the platform health store, with a start/end time, an exercise type, and (sometimes) route geometry. Not to be confused with a HoldMyTrack login session. |
| **Route / route geometry** | The GPS trajectory attached to a Health Connect session record, read via `ExerciseRouteResult`. A session can exist with no route (an indoor workout). |
| **Readiness** | The app's own name (`health.HealthConnect.Readiness`) for where the user currently stands in the permission-acquisition sequence — one of six states, §3.1. |
| **Watermark / sync cursor** | The point in the account's Health Connect history the last sync run confirmed as fully handled; the next run resumes from there rather than re-reading from the beginning (§3.3). |

## 2. Actors

| Actor | Description |
| :-- | :-- |
| **Anonymous visitor** | Has not signed in and holds no session. Sees only the sign-in screen (FR-1.1) — sign in, sign up, or start a demo — and never the map, matching the web client (`docs/SPEC.md` §2). |
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
2. On success, the response's `session_token` field (present on all four session-minting endpoints, specifically for a native caller that has no cookie jar to rely on) is stored via `Session.start`, and the map replaces this screen.
3. On failure, the server's own error message is shown inline; the three action buttons are disabled while a request is in flight and re-enabled after.

**Outputs**: A stored bearer token and the account's email (empty for a demo account, which the server deliberately never returns a real email for).

**Notes**: This is the app's first screen whenever no session is held — on launch, after sign-out (FR-1.3), and after a stored token is rejected (FR-1.2) — and nothing sits behind it: Back leaves the app. The map is never shown signed out, since a basemap with none of the account's layers is a weak demonstration of the product next to the Demo button here.

### FR-1.2 Session persistence and re-verification

**Description**: A session survives the app being closed and relaunched, but a restored token is not trusted until the server confirms it — the same caution `docs/SPEC.md` FR-1.4 describes for the web client, applied to a store that isn't a cookie jar.

**Behavior**:
1. On cold start, if a token is stored but not yet marked verified in this process, the map defers attaching any signed-in-only layer, shows "Checking session…", and calls `GET /v1/auth/me`.
2. A `200` marks the token verified for the rest of the process's lifetime; the map then proceeds as signed in.
3. A `401` — and only a `401` — clears the stored token and replaces the map with the sign-in screen (FR-1.1). Any other failure (no network, an unreachable dev stack) leaves the token in place and is retried on the next resume, since it says nothing about whether the credential itself is still good.

**Outputs**: Either a confirmed, attached session, or a clean return to the sign-in screen — never a map that silently 401s on every tile request while looking merely blank.

### FR-1.3 Sign out

**Description**: Ends the current session, both on the device and on the server.

**Behavior**: `POST /v1/auth/logout` deletes the session row server-side; the stored token and email are cleared locally regardless of whether the request succeeded (a token that can't reach the server to be revoked is not one worth keeping either way). The app then shows the sign-in screen (FR-1.1) with every other screen cleared from the back stack.

## 4. FR-2 — Map Visualization

### FR-2.1 Base map

**Description**: A full-screen map renders on launch for a signed-in account; with no session, the sign-in screen (FR-1.1) is shown instead and the map is not created at all.

**Behavior**: The style document is fetched unauthenticated from `GET /v1/map/style/{flavor}`, `flavor` chosen from the system's day/night setting (`light` or `dark` — two of the five the API serves; the app does not offer a way to pick the other three or to override the system setting, per `apps/android/docs/ARCHITECTURE.md` §2.1). The camera opens on a whole-world view (equator, zoom 1) until an account's own activity extent is known (FR-2.3). A style or tile load failure is reported on screen as visible text, not only in logcat.

### FR-2.2 Three map modes: Normal, Fog of War, Heatmap

**Description**: The same three mutually exclusive views `docs/SPEC.md` FR-4.1–FR-4.4 define, switched by one three-way toggle in the map's bottom bar. Only visible/available once signed in.

**Behavior**:
1. **Normal** draws the account's tracks as a single-color vector line layer (`GET /tiles/v1/tracks/{z}/{x}/{y}.mvt`).
2. **Fog** replaces the tracks with the server-rendered dark-veil raster (`GET /tiles/v1/fog/{z}/{x}/{y}.png`); tracks are hidden.
3. **Heatmap** replaces the tracks with the server-rendered intensity raster (`GET /tiles/v1/heatmap/{z}/{x}/{y}.png`); tracks are hidden.
4. All three layers sit beneath the basemap's first label layer, so place names stay legible; within that, the active raster (fog or heatmap) is drawn beneath the tracks layer so a cleared route reads as visible through the fog rather than obscured by it — the same ordering the web client uses.
5. The active mode's button is shown bold and at full opacity; the other two are dimmed.
6. While a GPS recording is in progress (FR-5.1) the toggle is hidden and none of the three modes' layers are drawn — the map shows only that recording. The previously selected mode returns when the recording stops.

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
4. **Needs routes permission** — sessions are readable, routes are not. `READ_EXERCISE_ROUTES` cannot be requested programmatically at all (a platform limitation, confirmed on-device — requesting it alongside the others silently grants the others and omits it). The screen names the exact path instead: *Health Connect → HoldMyTrack → Additional access → Access exercise routes → Always allow*, and the primary action opens Health Connect's home screen as the closest a third-party app can get the user there.
5. **Needs history permission** — sessions and routes are both readable, but Health Connect will only serve the last 30 days without `READ_HEALTH_DATA_HISTORY`. Unlike the routes permission, this one *is* requestable, so the primary action opens the ordinary system dialog for it. This state is not blocking: a secondary "Sync now anyway" action is always available, since declining history access is a legitimate answer and syncing a shallower window is still useful.
6. **Ready** — every permission granted; the primary action starts a sync run (§5.2).

**Notes**: This same screen is registered for Health Connect's `ACTION_SHOW_PERMISSIONS_RATIONALE` intent, so when Health Connect itself asks the user why HoldMyTrack wants their data, it opens this screen — meaning the rationale a user sees inside Health Connect and the explanation they see inside HoldMyTrack are the same text, by construction rather than by two authors staying in sync. Readiness is re-read on every resume rather than cached, since the routes permission specifically can only change in another app's settings screen, and returning from it is the only moment this screen can observe that.

### FR-3.2 Foreground sync run

**Description**: Reads exercise sessions from Health Connect in ascending order, sends the ones with usable route geometry to the server, and reports what happened — entirely while the sync screen is on screen.

**Preconditions**: Readiness is `READY`, or the user chose "Sync now anyway" from the history-permission state.

**Behavior**:
1. Sessions are read paged (50 per Health Connect request), starting from the account's watermark (§5.3) or from the beginning if never synced.
2. Each session is classified: a route present and readable is prepared for sending; a session with confirmed no route (`ExerciseRouteResult.NoData`) is counted as skipped, not an error — this is the ordinary case for an indoor workout, a gym session, a swim, or a rowing machine, none of which have a trajectory to begin with; a session whose route exists but could not be read right now (`ExerciseRouteResult.ConsentRequired`) stops the run entirely (§5.3).
3. Sessions with a route are batched — up to 25 activities or 20,000 points per batch, whichever comes first — and posted to `POST /v1/sync/activities` with `source: "healthconnect"`. Each activity's `activity_type` is normalized from Health Connect's own vocabulary onto the one HoldMyTrack's other ingest paths already use (e.g., Health Connect's `biking` becomes `cycling`; `running_treadmill` becomes `running`), so the same physical ride synced from a watch and uploaded from a file describes itself the same way — required for both the Activities panel's TYPE filter and cross-source deduplication (`docs/SPEC.md` FR-5.2, `docs/IMPLEMENTATION.md` §4.6) to work correctly. Elevation is included per-point only when the location itself carries it; heart rate is never sent, since it lives behind a Health Connect permission this app does not declare (§6, `docs/VISION.md` §7).
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

## 7. FR-5 — In-App GPS Recording

A third way an activity can originate on this app, alongside FR-3's Health Connect sync and a manual file upload done from the phone's browser (`docs/SPEC.md` FR-3.1). Architecturally distinct from FR-3: a finished recording submits directly to `POST /v1/sync/activities` under `source = "recorded"` rather than round-tripping through Health Connect ([ADR-0007](../../../docs/adr/0007-in-app-gps-recording-submits-directly.md)), so none of FR-3's foreground-only reasoning carries over — recording and syncing are two separate, explicit steps (below), not one continuous run. `docs/SPEC.md` FR-3.8 is the cross-platform behavior spec this restates at screen level; `docs/IMPLEMENTATION.md` §4.0.4 has the wire contract.

### FR-5.1 Record — one button on the map

**Description**: A round, translucent record button at the bottom centre of the map (`MainActivity`) that records a casual, GPS-only track — a walk, hike, or drive someone would not otherwise bother tracking — in one tap, asking nothing.

**Preconditions**: A session (the button is on the map, FR-2.1) and `ACCESS_FINE_LOCATION` granted to record. A demo session can record and manage rows locally; only syncing them is blocked (FR-5.2 step 10).

**Behavior**:
1. **Tap while idle starts recording.** The first time, it raises the system location dialog (Precise/Approximate × While using the app/Only this time/Don't allow) followed by the notification dialog, then starts once location is granted; notifications are not required. Denying location shows "HoldMyTrack needs location access to record a GPS track." and nothing starts. No name, type or description is asked, now or at any later point in recording. Confirmed on an emulator.
2. Recording runs in `RecordingService`, a declared foreground service (`FOREGROUND_SERVICE_TYPE_LOCATION`), so it survives the screen turning off and the app being swiped out of recents. Confirmed on an emulator: swiping the app away mid-recording and reopening it showed the full track so far, with recording still running.
3. **Tap while recording pauses; tap while paused resumes.** The button shows pause (ringed red) while recording, play (ringed amber) while paused, and a red dot while idle. No points are recorded while paused, so drift at a stop never adds distance — confirmed: a far-off fix sent while paused added no point. There is no automatic pause.
4. **Holding the button for two seconds while recording or paused stops.** A white ring fills clockwise around the button over the two seconds; when it closes, the phone gives haptic feedback and the recording stops. Letting go before then cancels — the ring clears and nothing happens (a release within the system long-press timeout still counts as a tap, pausing/resuming). Holding while idle does nothing. With TalkBack, the stop is offered as the button's long-press action.
5. **The map shows only the recording in progress** (FR-2.2 step 6): its line, in the track colour, and a dot at the latest fix, with the camera flying to street level (zoom 16) on the first fix and following each later one. Other activities and other map modes are not shown until the recording stops. Recordings that haven't synced never appear on the map after they stop — the map draws only what the server has (FR-2.2).
6. **A notification is shown for as long as a recording is in progress**: "Recording" with a running elapsed-time counter, or "Paused · h:mm:ss"; below that, distance · current speed · current altitude, updated on every GPS fix. It carries **Pause**/**Resume** and **Stop** buttons that act exactly like the map button, and tapping it opens the map. Stop from the notification also opens the app (to the map, then the Save screen, step 7). Confirmed on an emulator, including Stop from the notification (before Stop opened the app).
7. **Stop** saves the recording on the device, tagged not-synced, with no name or description and the activity type most recently set in Edit on this account (FR-5.2 step 4) — `"unknown"` for an account's first recording — then opens a **"Save recording"** screen on it: the Edit screen (FR-5.2 step 6) with that type pre-filled, and **Discard** in place of Download GPX. **Save** stores name, type and description (and remembers the type for the next recording); **Back** leaves the recording saved as it was; **Discard** asks "Discard this recording?" and deletes it from the device. No network request happens at this step. Not yet verified on a device or emulator.
8. **A recording shorter than one minute is not saved.** Moving time (pauses excluded) under 60 s — or fewer than 2 GPS points — is discarded on Stop with "Too short — not saved." Confirmed on an emulator: a ~20 s recording left no row.

### FR-5.2 Recorded Activities — review, edit, queue, sync

**Description**: A menu item ("Recorded Activities", `RecordedActivitiesActivity`) listing every recording on this device that hasn't synced yet, where a row actually gets marked to go out.

**Behavior**:
1. Rows are newest first, filterable by two "All"-default dropdowns (sync status — not synced or queued; activity type, rebuilt from whatever distinct types are on file) — confirmed live, including the empty state ("No recorded activities yet.") before any row exists.
2. Each row reads `"{type} · {distance} km · {status}"` (e.g. "walking · 0.23 km · Queued"), with a checkbox, a small preview of the route's shape (the recorded line alone in the map's track colour, no basemap, drawn from the points stored on the device), and Edit/Delete buttons.
3. **A row still on the unedited `"unknown"` default cannot be queued.** Its checkbox renders disabled, and the row grows a fourth clause explaining why — `"unknown · 0.23 km · Not synced · Set a type to sync"` — confirmed live by recording, stopping without touching Type, and confirming both the checkbox's disabled state and the row text. This exists because `docs/SPEC.md` FR-3.7's cross-source dedup match requires an *exact* `activity_type` match, and an untyped recording can never match a same-walk activity that arrived typed from another source — reproduced live against a local server as the root cause of a real production duplicate (two activities for one walk: one via this screen left on `"unknown"`, one via Health Connect sync reporting `"walking"`) before this gate existed.
4. Editing the type to a real value through the Type picker (step 6) and saving re-enables the checkbox; checking it then flips the row to Queued. Confirmed live end to end. The saved type also becomes the type the account's next recordings start with (FR-5.1 step 7), so after the first one a recording can usually be queued without editing — confirmed: after setting one recording to walking, the next was saved as walking.
5. **Editing an already-queued row's type back to `"unknown"` demotes it to not-synced** the moment Save is tapped, disabling the checkbox again — confirmed live. Edit is available on every row, queued or not, so without this the gate in step 3 could be set once and then bypassed by clearing the type afterward; this closes that path.
6. **Edit** (`RecordingActivity`, `EXTRA_RECORDING_ID`) opens an "Edit recording" screen for the row: Name, Type, Description (optional), the recording's time and distance, and **Save** and **Download GPX**. **Type** is a picker, not a text field — the same behavior as the web edit dialog's Type field (`docs/SPEC.md` FR-5.10): tapping it opens a searchable list of the account's existing types, most-used first with how many activities use each (its server-side activities plus this device's recordings), then any of walking/hiking/running/cycling/driving the account hasn't used yet. Typing filters the list (case- and accent-insensitive, an exact name first); while the text doesn't exactly match an existing type, a last **Add "…"** row saves it exactly as typed, up to 50 characters. Names are displayed title-cased with underscores as spaces ("dog_walk" → "Dog walk") and stored raw. Confirmed on an emulator: "dog walk" narrowed the list to "Dog walk 583"; "Solowheel" offered only `Add "Solowheel"`, which set the field and was saved with the recording.
7. **Download GPX** opens the system's save screen with a suggested file name (the recording's name, or `holdmytrack-{id prefix}.gpx`), and writes the track there as GPX 1.1 — name, description and type on the track, and every point's position, time and elevation where known — then confirms with "Track saved." (or "Could not save the track."). No storage permission is involved. Confirmed on an emulator: the saved file in Downloads carried the recorded points and `<type>Solowheel</type>`.
8. **Sync Now** (the existing Health Connect button, FR-3.2) also drains every queued row in the same tap — `flushRecordedQueue()`, run after the Health Connect pass or after it throws, submitting each as its own `POST /v1/sync/activities` call. **A row that syncs is deleted from the device** and disappears from this list; the activity now lives on the server, on the map and in the activity list. A row that fails stays queued for the next Sync Now. Confirmed on an emulator end to end: a queued walking recording became a `walking` activity server-side and the local row was gone.
9. **Delete** asks for confirmation — which says the recording hasn't synced, so this is the only copy — and removes the row from the device.
10. A demo session can record and manage rows here, but every sync checkbox is disabled with an explanatory notice — queuing something "Sync Now" can never actually take would be pointless (matches FR-3's own demo gate).

### FR-5.3 Local storage is scoped per signed-in account

**Description**: Recordings are keyed to whichever account is signed in when they're made (`Session.email`, empty string for a demo account) — the same per-account key `SyncCursor` (FR-3.3) already established.

**Behavior**: Switching accounts on one device never shows one account's recordings under another's. Confirmed live, incidentally: a recording made while signed out did not appear in Recorded Activities after signing into a real account, and so could not be queued or synced from that account either — consistent with `docs/IMPLEMENTATION.md` §7.2's own account-scoping note, not a defect.

## 8. Non-Functional Requirements (summary)

This section summarizes cross-cutting behavior specified elsewhere in this document.

| Concern | Behavior |
| :-- | :-- |
| **Credential handling** | Bearer token, not a cookie — a session token is presented as `Authorization: Bearer <id>`, attached only to requests bound for HoldMyTrack's own API origin, never to third-party basemap-asset origins (FR-1.2). |
| **Session security** | Restored tokens are re-verified against the server before any signed-in layer is attached; sign-out revokes server-side (FR-1.2, FR-1.3). |
| **Foreground-only sync** | No background service or scheduled job ever attempts a Health Connect route read — a platform constraint, not a battery-saving choice (FR-3.2). |
| **Safe interruption** | A sync run cancelled at any point leaves no partial, unconfirmed state — the watermark only advances over confirmed-terminal records (FR-3.3). |
| **No reload required** | The sync history screen updates itself by polling while work is outstanding, and stops polling once settled (FR-4.1). |
| **Idempotency** | Re-syncing the same Health Connect record never creates a duplicate activity — the server keys on the platform's own record id (FR-3.2, `docs/IMPLEMENTATION.md` §4.0.3). |

## 9. Known Limitations & Out-of-Scope Items

Named here rather than left implicit, the way `docs/SPEC.md` §15 does for the wider system:

- **No visual design system.** No icon set, no launcher icon, no color/type/spacing tokens — four functional screens waiting on root `docs/ROADMAP.md` Phase 3's design freeze (`apps/android/docs/ROADMAP.md` Phase 5).
- **No filter controls.** The map always shows the account's complete, unfiltered history; there is no Android equivalent of the web's date-range picker, TYPE/DISTANCE filters, or per-track hide/show.
- **No accessibility work done.** No content descriptions, no verified touch-target sizing, untested under a large system font or TalkBack.
- **Samsung Galaxy Watch is unsupported**, not degraded — Samsung does not expose route geometry to Health Connect at all, so every Samsung-sourced session is rejected for having no route, indistinguishable at sync time from an ordinary indoor workout.
- **Background sync will never be built for Health Connect routes** — a platform constraint (`ConsentRequired` regardless of grant state when backgrounded), not a sequencing gap.
- **In-app GPS recording's iOS half is unbuilt** — FR-5 above is Android-only; `docs/ROADMAP.md` Phase 2 tracks the iOS half as a combined item once an iOS app exists at all.
- **No automated tests.** Every behavior in this document has been verified manually against a live server stack — a physical device for FR-1 through FR-4, an emulator for FR-5 (§7, noted inline where it matters); see `apps/android/docs/IMPLEMENTATION.md` §9 for the verification record.
- **iOS does not exist.** Path 2's HealthKit half is unbuilt; this app defines the sync contract iOS will inherit (`apps/android/docs/ROADMAP.md`).
- **`poc-healthconnect/` is not part of the product** — a throwaway diagnostic app, retained only until Phase 1's findings are fully absorbed elsewhere.
