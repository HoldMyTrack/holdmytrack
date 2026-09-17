# FitMap: Spec

| | |
| :-- | :-- |
| **Version** | 1.0 |
| **Status** | Current — describes Phase 0/1 functionality as built |
| **Last updated** | 2026-09-14 |
| **Related documents** | `VISION.md` (product scope, market rationale, phase roadmap — the authority on *what ships and why*); `ARCHITECTURE.md` (system-level shape, key decisions, the stack); `IMPLEMENTATION.md` (schema, each feature's own implementation — the authority on *how it's built*); `AGENTS.md` (repository orientation) |

## 1. Introduction

### 1.1 Purpose

This document specifies FitMap's functional behavior as currently implemented: what the system does, from the point of view of a user or of another system calling its API — not why it was built that way (`VISION.md`) or how it is implemented internally (`IMPLEMENTATION.md`). Each functional requirement (FR) is written to be independently testable: given the stated preconditions and inputs, the stated behavior and outputs should be observable.

### 1.2 Scope

**In scope**: every feature currently built and shipped, as of this document's last-updated date — authentication and account management (including account settings — avatar, name, country, and privacy trim, FR-1.7), the no-signup demo, activity upload and ingestion (file upload, `.zip` bulk import, Google Takeout import), map visualization (track rendering, Fog of War, Heatmap, colored zone segments, the pace/heart-rate + elevation profile, high-resolution export), the Activities panel and its filters, the date-range picker, the per-account activity graph, password recovery, and distance/time trends (FR-9 below).

**Out of scope**: functionality named in `VISION.md`'s roadmap (§5.3 onward) but not yet built — Path 1 cloud-provider connectors (Garmin/Wahoo/COROS), Path 2 on-device sync (HealthKit/Health Connect), cross-source deduplication, explorer-tile gamification, the rest of "Export" (story cards, animated reveals — high-resolution map export itself is built, FR-4.10 below), and user-defined privacy zones (a `privacy_zones` table exists in the schema — `IMPLEMENTATION.md` §3.7 — but no endpoint or UI creates or applies one today; only the endpoint-trim privacy control in FR-8.1 below, user-adjustable via FR-1.7's Settings page, is functional). Also deliberately out of scope, not a "not yet" — best-effort curves, personal bests, power curves, and training load were built and then cut: `VISION.md` §1.1 draws a hard line against FitMap being a health or fitness advisor, and pace/heart-rate stay as per-activity route context (FR-4.9) rather than an analysed, all-time performance record. This document will be extended with new FR sections as in-scope functionality ships, not rewritten in place of them.

### 1.3 Intended audience

Engineers implementing against or modifying this system, QA deriving test cases, and anyone needing an authoritative answer to "what does the system do in this situation" without reading source code.

### 1.4 Definitions

| Term | Meaning |
| :-- | :-- |
| **Activity** | One recorded exercise session (a run, ride, hike, swim, etc.) with a start time, and usually a GPS trajectory and heart-rate data. |
| **Track** | An activity's GPS trajectory, as rendered on the map. |
| **Session** | A signed-in browser's authentication state, held as an opaque cookie. |
| **Registered user** | An account with a real email and password, created via sign-up or by upgrading a demo account. |
| **Demo user** | An ephemeral account created via "Try it now — no signup," functionally identical to a registered user except for its lifetime (FR-2.2 below). |
| **Fog of War** | A map mode that shows a white veil over everywhere the signed-in user has *not* recorded an activity, so recorded routes appear as "cleared" ground. |
| **Heatmap** | A map mode that shades every recorded location by how many times it's been crossed, brightest where crossed most. |
| **Ingest** | The server-side process of turning an uploaded file into a persisted `Activity` — parsing, privacy trimming, simplification, and storage. |

## 2. Actors

| Actor | Description |
| :-- | :-- |
| **Anonymous visitor** | Has not signed in and holds no session. Can reach the sign-in/sign-up screen and start a demo. Cannot see any activity data or use the map. |
| **Demo user** | Holds a session tied to an ephemeral account (FR-2). Full functional access to every feature a registered user has, except the account itself expires after 24 hours unless upgraded (FR-2.3). |
| **Registered user** | Holds a session tied to a permanent account (email + password). Full functional access to every feature in this document. |

There is no administrator role, no multi-tenancy beyond per-account data isolation, and no concept of one account viewing another's data (FR-8.2).

## 3. FR-1 — Authentication & Account Management

### FR-1.1 Sign up

**Description**: An anonymous visitor creates a new registered account.

**Preconditions**: No active session, or an active demo session (see note below).

**Inputs**: Email address, password (minimum 8 characters).

**Behavior**:
1. Client submits email + password to `POST /v1/auth/signup`.
2. Server validates the email is a syntactically valid address and the password meets the minimum length.
3. Server hashes the password (bcrypt) and creates or claims a `users` row (see note).
4. Server creates a session (30-day expiry) and sets it as an `HttpOnly` cookie.
5. Client is signed in and shown the map.

**Note — "claim" semantics**: if the caller's browser holds a live demo session, that same account is converted in place (its uploaded activities are preserved) rather than a new one being created (FR-2.3). Otherwise, if no account anywhere has ever set a password, the system's original seeded account is claimed in place instead of inserting a new row (a one-time migration convenience, not something a normal user observes differently). In every other case, a new account row is created.

**Outputs**: A valid session cookie; the account's email is returned to the client.

**Error cases**:
- Invalid email format → `400 Bad Request`.
- Password shorter than 8 characters → `400 Bad Request`.
- Email already registered to a different, real account → `409 Conflict`.

### FR-1.2 Sign in

**Description**: A user with an existing registered account establishes a session.

**Preconditions**: No active session.

**Inputs**: Email address, password.

**Behavior**:
1. Client submits email + password to `POST /v1/auth/login`.
2. Server looks up the account by email and compares the submitted password against the stored bcrypt hash.
3. On match, server creates a session (30-day expiry) and sets it as a cookie.

**Outputs**: A valid session cookie; the account's email is returned to the client.

**Error cases**:
- Email not found, account not yet claimed (no password ever set), or password mismatch → `401 Unauthorized` with a single generic message ("invalid email or password") in every case — the system does not distinguish these to a caller, so it cannot be used to discover which emails are registered.

### FR-1.3 Sign out

**Description**: A signed-in user ends their session.

**Preconditions**: An active session.

**Behavior**:
1. Client calls `POST /v1/auth/logout`.
2. Server deletes the session record server-side (not merely instructing the browser to discard the cookie) and clears the cookie.

**Outputs**: `204 No Content`. Any further request using the same (now-deleted) session cookie is treated as unauthenticated.

### FR-1.4 Session check / persistence

**Description**: On loading the app, the client determines whether a valid session already exists (e.g., from a previous visit), without requiring the user to sign in again.

**Behavior**:
1. Client calls `GET /v1/auth/me` with whatever session cookie it currently holds.
2. If the cookie names a live, unexpired session, the server returns the account's identity (`200 OK`).
3. Otherwise the server returns `401 Unauthorized`, and the client shows the sign-in screen.

**Notes**: A session's validity is checked in the database on every request (not trusted from the cookie's own stated expiry), so a session ended server-side (FR-1.3, or invalidated by a password reset, FR-1.6) stops working immediately even if the browser still holds the cookie. Sessions last 30 days from creation.

### FR-1.5 Forgot password

**Description**: A user who cannot sign in requests a password-reset link by email.

**Preconditions**: None (reachable with no session).

**Inputs**: Email address.

**Behavior**:
1. Client submits an email address to `POST /v1/auth/forgot-password`.
2. If that email matches a real, claimed account, the server creates a reset token (valid 1 hour, single-use) and emails a link containing it to that address.
3. The server responds identically (`200 OK`, the same generic confirmation message) whether or not the email matched an account, so the response cannot be used to discover which emails are registered.

**Outputs**: A generic confirmation message. No indication of whether an email was actually sent.

**Rate limiting**: Limited to 5 requests per hour per client IP address; further requests receive `429 Too Many Requests`.

### FR-1.6 Reset password

**Description**: A user completes a password reset using the token from FR-1.5's email.

**Preconditions**: A valid, unexpired, unused reset token (normally reached by clicking the emailed link, which the client recognizes independently of whatever session state currently exists — see note).

**Inputs**: Reset token, new password (minimum 8 characters).

**Behavior**:
1. Client submits the token and new password to `POST /v1/auth/reset-password`.
2. Server verifies the token exists and has not expired.
3. Server updates the account's password.
4. Server invalidates **every** outstanding reset token for that account (not only the one used) and **every** existing session for that account (any device that was previously signed in is signed out).
5. Server creates one new session for the browser completing the reset and sets it as a cookie.

**Outputs**: A valid session cookie for the account whose password was just reset; the account is now signed in.

**Error cases**:
- Token missing, already used, or expired → `400 Bad Request` with a generic message (the system does not distinguish "never existed" from "expired" from "already used").
- Password shorter than 8 characters → `400 Bad Request`.

**Note — reset link takes priority**: if the client detects a reset token in its own URL (the emailed link), it presents the reset screen unconditionally, ahead of whatever session state `GET /v1/auth/me` would otherwise report — including an already-signed-in session. This is the one flow reachable without first checking authentication state.

### FR-1.7 Account settings

**Description**: A signed-in user (real or demo — FR-2) edits their own profile: Avatar, Name, Country, and Privacy Trim (FR-8.1). Reached from the account menu's "Settings" item, a separate screen from the activity graph (FR-7).

**Preconditions**: An active session.

**Inputs**: An image file (PNG, JPEG, or WebP, up to 5 MB) for Avatar; free text for Name; a country selected from a standard list for Country; a whole number of meters (0–5000) for Privacy Trim.

**Behavior**:
1. Avatar uploads and removals take effect immediately (`POST`/`DELETE /v1/account/avatar`) — each is its own action, not gated behind a separate save step. The account menu's own avatar button reflects whichever image is current everywhere in the app the moment it changes, with no reload.
2. Name, Country, and Privacy Trim save together as one action (`PATCH /v1/account/settings`) — editing one and leaving without saving discards all three, not just the one touched.
3. **Country decides which unit system the entire app displays distance, pace, and elevation in** — metric (km, min/km, meters) for every country except the United States, Liberia, and Myanmar, which see imperial (mi, min/mi, feet). Leaving Country unset defaults to metric. This takes effect the moment it's saved, across every screen that shows one of these values (the Activities panel, the date-range picker, the activity graph, Trends, the per-activity pace/elevation profile, and the map's own distance scale) — none of it requires a reload.

**Outputs**: The account's current Avatar, Name, Country, and Privacy Trim value, always reflecting the last successful save (or the account's defaults, if never changed) — reloading the app never reverts to something stale.

**Error cases**:
- An unsupported image type or a file over 5 MB → `415`/`413`, and the image is not saved.
- Country outside the supported list, or Privacy Trim outside 0–5000 → `400 Bad Request`, and none of the three fields in that save are applied (a full-replace save either succeeds as a whole or not at all).

## 4. FR-2 — No-Signup Demo

### FR-2.1 Start a demo

**Description**: An anonymous visitor tries the full application without creating an account.

**Preconditions**: No active session.

**Behavior**:
1. Client calls `POST /v1/auth/demo`.
2. Server creates a new account with no email or password a person could use to sign in with directly, marked as ephemeral with a 24-hour expiry.
3. Server creates a session for it (also 24-hour expiry) and sets it as a cookie.
4. Client is signed in and shown the map, with full functional access to every other feature in this document (upload, map modes, filtering, the activity graph) exactly as a registered user has it.

**Outputs**: A valid session cookie for the new ephemeral account.

**Rate limiting**: Limited to 5 demo accounts per hour per client IP address; further requests receive `429 Too Many Requests`.

### FR-2.2 Demo expiry

**Description**: An ephemeral demo account and everything associated with it (uploaded activities, rendered map tiles) are automatically and permanently deleted 24 hours after creation, unless upgraded first (FR-2.3).

**Behavior**: A background sweep runs every 5 minutes, deletes every database row for each expired demo account (cascading to its activities and sessions), and deletes its uploaded files and rendered map tiles from object storage.

**Notes**: There is no warning before expiry and no way to recover a demo account's data after it expires. A user who wants to keep their data must upgrade it (FR-2.3) before the 24-hour window elapses.

### FR-2.3 Upgrade a demo account to a registered account

**Description**: A demo user converts their ephemeral account into a permanent one without losing anything uploaded during the demo.

**Preconditions**: An active demo session.

**Inputs**: Email address, password (minimum 8 characters) — the same inputs as FR-1.1.

**Behavior**:
1. From the account menu, the user selects "Demo session — save this," which presents the same sign-up screen a new visitor sees (FR-1.1), with a "← Back" option instead of the "try demo" option (starting a second demo while already in one would abandon the first).
2. The user submits an email and password.
3. Because the request carries a live demo session, the server updates that same account's row in place (setting its email and password, and clearing its ephemeral-expiry marker) rather than creating a new one — every activity uploaded during the demo remains attached under the same account.
4. The user is returned to the map, now signed in as a registered user.

**Outputs**: The demo account is now a permanent, registered account; all previously uploaded data is preserved and immediately visible.

**Note — cancellation**: selecting "← Back" (or navigating away without submitting) returns to the map with the demo session untouched — nothing is lost, and the account remains subject to FR-2.2's 24-hour expiry.

## 5. FR-3 — Activity Upload & Ingestion

All upload functionality requires an active session (demo or registered — FR-1/FR-2); there is no unauthenticated upload path.

### FR-3.1 Upload individual files

**Description**: A signed-in user uploads one or more individual activity files.

**Preconditions**: Active session.

**Inputs**: One or more files, each a `.gpx`, `.fit`, or `.tcx` file no larger than 64 MiB. Up to 20 individually-selected files per batch (a larger selection is rejected client-side in full, before any upload begins, with a message directing the user to a `.zip` archive instead — FR-3.2).

**Behavior**:
1. User drags files onto the upload panel, or selects them via a file picker.
2. Each file uploads independently, as its own `POST /v1/activities/upload` request (multipart), and is tracked independently — one file failing does not affect the others.
3. For each file: server validates its extension and size, computes a content hash to check for a duplicate (FR-3.5), persists the raw file, and enqueues a background parsing job.
4. The upload panel shows each file's live status (uploading, with a progress percentage; then "Processing…") until the background job finishes.
5. Once ingestion completes, the activity appears automatically in the Activities panel, the map, and every summary that reflects the current date range — no page reload is required.

**Outputs**: One new `Activity` per successfully ingested file, each with a parsed trajectory, distance, duration, and (where the source file provides it) heart rate/elevation data.

**Error cases**:
- Unsupported file extension → `415 Unsupported Media Type`.
- File too large, or malformed request → `413 Request Entity Too Large`.
- Empty file → `400 Bad Request`.
- Unparseable/corrupt file content → the background job fails; the upload history shows "Failed" with a reason, and no `Activity` is created.

### FR-3.2 Bulk upload via `.zip` archive

**Description**: A signed-in user uploads many activity files at once as a single `.zip` archive, bypassing FR-3.1's 20-file batch limit.

**Preconditions**: Active session.

**Inputs**: One `.zip` archive, at most 512 MiB compressed, containing at most 5,000 entries, each contained file at most 64 MiB.

**Behavior**:
1. User uploads a `.zip` file the same way as FR-3.1 (same drop zone / picker).
2. Server extracts the archive and processes each contained `.gpx`/`.fit`/`.tcx` file exactly as FR-3.1 does — one background parsing job per file.
3. A file inside the archive with an unsupported extension, or that is oversized or unreadable, is skipped with a reason recorded; the rest of the batch proceeds regardless.
4. The response reports how many files were accepted and how many were skipped (and why), plus whether the archive was truncated at the entry-count limit.

**Outputs**: One new `Activity` per successfully ingested contained file.

**Error cases**:
- Archive itself exceeds size/entry-count bounds, or is not a valid `.zip` → `400 Bad Request` / `413 Request Entity Too Large` (rejected before any entries are processed).
- An individual bad entry does not fail the request — see step 3.

### FR-3.3 Google Takeout import

**Description**: A user imports their exported Google Health / Fitbit activity history.

**Preconditions**: Active session.

**Inputs**: A Google Takeout export `.zip` archive (detected automatically by its internal folder structure — no separate upload flow to choose).

**Behavior**:
1. User uploads the Takeout export `.zip` through the same upload control as FR-3.1/FR-3.2.
2. Server recognizes the archive's shape as a Takeout export (rather than a plain `.zip`) and extracts one activity file per recorded activity that has GPS data (activity types with no GPS in the export — e.g. a logged swim with no route — are skipped, not treated as errors).
3. Each extracted activity is ingested exactly as FR-3.1 describes, attributed to the Takeout source.

**Outputs**: One new `Activity` per activity in the export that had GPS data.

### FR-3.4 Upload status and history

**Description**: A user can see the status of in-progress and past uploads.

**Preconditions**: Active session.

**Behavior**:
1. The upload control's badge shows a live count of files still being processed.
2. Opening the upload panel shows a paginated list (5 per page) of every file ever uploaded to this account, each showing: filename, status ("Processing" / "Ready" / "Failed"), and — once ready — the activity's date and distance.
3. While anything is still processing, the list refreshes automatically (polled every 1.5 seconds) until every row settles to "Ready" or "Failed" — no manual refresh needed.

**Outputs**: `GET /v1/uploads?limit=&offset=` returns the current page, the total count, and how many are still processing.

### FR-3.5 Duplicate detection

**Description**: Re-uploading a file whose content was already ingested does not create a second activity or a second processing job.

**Behavior**: The server checks a content-derived identifier before enqueuing a new job. If that exact content was already ingested for this account, the upload responds immediately with an "already processed" status (`200 OK`) and no new job is created; the client shows a notice that the file was already uploaded rather than silently doing nothing.

**Notes**: This guarantee holds even under concurrent duplicate uploads (e.g., two tabs uploading the same file at once) — at most one `Activity` is ever created for the same content.

## 6. FR-4 — Map Visualization

### FR-4.1 Track rendering (Normal mode)

**Description**: The map displays a user's uploaded activities as drawn lines over a base map.

**Preconditions**: Active session.

**Behavior**:
1. The map renders every activity within the currently selected date range (FR-6) that has not been individually hidden (FR-5.8) or filtered out by TYPE/DISTANCE (FR-5.2/FR-5.3), as a colored line following its recorded route.
2. Hovering a track on the map bolds it; the corresponding row in the Activities panel is highlighted to match (FR-5.4's reverse direction).
3. Clicking a track on the map sets it as the row-click focus (FR-5.5) — bolds it, flies the camera to fit it, and replaces whichever activity was previously focused. It does not add to or remove from the checkbox group (FR-5.6) in either direction.
4. Clicking anywhere on the map that is not a track clears the row-click focus, if any — the focused activity's highlight is removed and it returns to the same flat-color rendering as every other unfocused activity. This does not affect the checkbox group.

### FR-4.2 Fog of War mode

**Description**: An alternate map mode showing a white veil over everywhere the user has not recorded an activity.

**Behavior**:
1. Selecting "Fog" from the map-mode toggle replaces the track lines with a raster veil: any area a recorded route has passed through is rendered clear; everywhere else stays fogged.
2. The veil respects the current date range and hidden-activity set exactly as Normal mode's tracks do (FR-4.1) — narrowing the range or hiding an activity re-fogs the area it covered.
3. Individual track lines are not drawn in this mode (the veil itself is the information).

### FR-4.3 Heatmap mode

**Description**: An alternate map mode shading locations by how often they've been visited.

**Behavior**:
1. Selecting "Heatmap" replaces the track lines with a raster overlay, brighter wherever more recorded activity has crossed the same location (a daily commute reads brighter than a once-ridden road).
2. Like Fog of War, the heatmap respects the current date range and hidden-activity set.
3. Individual track lines are not drawn in this mode.

### FR-4.4 Mode is mutually exclusive

**Description**: Normal, Fog, and Heatmap are three views of the same underlying data, not independent toggles — exactly one is active at a time.

### FR-4.5 Base map and theming

**Description**: The map renders a self-hosted vector base map (streets, labels) in either a light or dark theme, selected via the page's URL (no in-app toggle). The current camera position (center, zoom) and theme are reflected in the URL and restored on reload, so a specific view is shareable via link.

### FR-4.6 Coverage notice

**Description**: If the user pans or zooms the map outside the area the base map actually covers, a notice is shown instead of a blank/grey map; it disappears once the view returns inside the covered area.

### FR-4.7 Find my location

**Description**: A one-shot control moves the camera to the browser's current geolocation (via the browser's geolocation permission). It does not continuously track the device's position, and nothing about the click is recorded or sent to the server.

### FR-4.8 Colored zone segments (speed / heart rate)

**Description**: When an activity has row-click focus (FR-5.5), its track is overdrawn with colored segments reflecting speed or heart-rate change along the route, instead of the single flat color Normal mode (FR-4.1) otherwise uses.

**Preconditions**: An activity currently has row-click focus. Not shown in Fog or Heatmap mode, or when nothing is focused. Independent of the checkbox group (FR-5.6) — checking a box, including when it's the only one checked, does not show bands on its own.

**Behavior**:
1. The track is split into five colored bands (low/slow=blue through high/fast=red), each band's range computed from that activity's own minimum and maximum for the active metric — not a fixed scale shared across activities.
2. A Pace/Heart rate toggle selects which metric is shown (the underlying data is speed; displayed as pace — minutes:seconds per km — since that's the unit runners and walkers actually read effort in). The toggle itself only appears when the activity has heart-rate data for its entire duration; an activity with any gap in coverage shows pace only, with no toggle at all — a single-option toggle would have nothing to switch between.
3. Focusing a different activity updates the bands to match it; clicking anywhere on the map that isn't a track (FR-4.1) clears focus and removes the bands, the same as clearing focus any other way.
4. Exact band values are available on hover, in the pace/heart-rate + elevation profile (FR-4.9) rather than in a separate legend.

**Notes**: Band boundaries follow the same simplified vertices the track's own line already renders from — a geometrically simple stretch (little directional change) can carry very few of those vertices regardless of how much its speed or heart rate actually varied there, so the bands can occasionally read coarser than the underlying data on a mostly-straight route. See `IMPLEMENTATION.md` §4.3.1 for the full account.

### FR-4.9 Pace/heart-rate + elevation profile

**Description**: Alongside FR-4.8's colored zone segments, the same floating card shows a straight-line profile — a colored strip (the same bands as FR-4.8, laid out by distance along the route instead of geographic position) with an elevation curve beneath it, so climb and pace/heart-rate effort can be read together.

**Preconditions**: Same as FR-4.8 — an activity currently has row-click focus. The elevation curve specifically requires the activity to have elevation data for its entire duration; an activity with any gap in coverage shows the colored strip alone.

**Behavior**:
1. The colored strip uses the same five bands as FR-4.8, including its toggle — one shared Pace/Heart rate selection drives both this strip and the map's own curved bands together.
2. The x-axis for both the strip and the elevation curve is distance along the route, not vertex order, so the two stay aligned with real distance regardless of how densely the underlying simplified track's vertices happen to fall in any one stretch.
3. Hovering the strip or the elevation curve shows a small tooltip with the distance so far, the pace-or-heart-rate value, and the elevation at that point — this is the only place exact band values are shown; there is no separate legend.

**Notes**: Deliberately compact and unlabeled (no axis ticks or gridlines) — a general overlook alongside FR-4.8's toggle, not a separate detailed chart. See `IMPLEMENTATION.md` §4.3.2 for the full account.

### FR-4.10 High-resolution map export

**Description**: A header control exports the current map view as a high-resolution PNG image, downloaded directly to the caller's device.

**Preconditions**: Active session; the map has finished its initial load.

**Behavior**:
1. The exported image reflects the current camera position, theme, map mode (Normal, Fog, or Heatmap), and the current date-range/hidden-track filters — everything the live map is currently showing, at a resolution well above the on-screen canvas.
2. The live map is not disturbed by an export — camera, zoom, and mode remain exactly as they were before the export was triggered.
3. While an export is generating, the control shows a busy state; a failure (e.g. a timeout waiting for tiles to load at export resolution) is reported inline rather than silently producing nothing.

**Outputs**: A PNG file download, named `fitmap-{date}.png`.

**Notes**: This is one of three things `VISION.md` §4.2 groups under "Export" — story cards and animated reveals are not built. Colored zone segments (FR-4.8) are not reflected in an export even when currently shown on screen — exporting a single focused activity's bands is a narrower case not covered by this first slice. Vector/SVG output is not offered; raster (PNG) only.

## 7. FR-5 — Activities Panel

### FR-5.1 Activity list

**Description**: A permanent sidebar lists every activity within the currently selected date range (FR-6), narrowed by the TYPE and DISTANCE filters below.

**Behavior**: Each row's primary line is the activity's own name if one has been set (FR-5.10), or its start date/time otherwise — an activity has a name only once a person has typed one in via FR-5.10's edit dialog, never from parsing a source file. A row whose primary line is a name still shows its date/time as part of the row's secondary line, alongside distance and duration; a row with no name shows distance and duration alone, since its date/time is already the primary line. Type is also shown per row. Regardless of what a row displays, **the list itself is always ordered by start date/time, newest first** — a name never affects sort order. The list is not paginated — every matching activity is shown at once. The panel also shows a running count of matching activities and total distance for the range (independent of the TYPE/DISTANCE filters, which narrow the visible rows without changing this total).

### FR-5.2 TYPE filter

**Description**: The user can narrow the visible activities to one or more activity types (e.g., "Run," "Ride"), via a dropdown reached from the panel's header toolbar.

**Behavior**: Clicking "Type" in the header toolbar (positioned directly above the row list's own TYPE column) opens a dropdown listing every distinct type present in the current date range (not a fixed, predefined list) as a checkbox — checked means shown, unchecked means excluded. The list scrolls rather than collapsing behind a "show more" control once it's long enough to need one. Filtering is purely client-side over the already-fetched list; it narrows the Activities panel, the map's drawn tracks, and the header's activity count, but not the "total distance for the range" figure. The dropdown closes on an outside click, Escape, or clicking its own trigger again.

### FR-5.3 DISTANCE filter

**Description**: The user can narrow the visible activities to a minimum/maximum distance range via a slider, bounded by the shortest and longest activity in the current date range. Same scope as FR-5.2 (client-side, narrows the same things, doesn't affect the range total). Standalone and always visible, between the panel's subtext line and the header toolbar — not folded into the Type dropdown or hidden behind any toggle.

### FR-5.4 Row hover preview

**Description**: Hovering a row (anywhere on it) previews that activity's track on the map — bolded — with no camera movement. The preview clears the instant the pointer leaves the row. This works in both directions: hovering an activity's track directly on the map previews it the same way, and additionally underlines that row's title in the Activities panel — so either surface can be used to identify which row an unlabeled track on the map belongs to, not only the reverse.

### FR-5.5 Row click — focus and fly

**Description**: Clicking a row's text — or clicking that activity's track directly on the map (FR-4.1) — sets it as the single row-click *focus*: highlights it and flies the camera to fit it. Whichever activity was previously focused this way loses its highlight (only ever one activity is "just clicked" at a time, regardless of which surface the click came from). This is independent of FR-5.6: neither a row-text click nor a map click ever checks or unchecks any checkbox, in either direction.

**Notes**: If the clicked activity has no recorded track (e.g., a source with no GPS), no fly occurs, since there is nothing to fit the camera to. The colored zone segments (FR-4.8) shown for a single focused activity are driven by this mechanism specifically, not by FR-5.6.

### FR-5.6 Checkbox — build a group

**Description**: Each row also has a checkbox that adds or removes it from the current checked group without replacing the rest of it, for building a multi-activity selection. Checking a second row never unchecks the first. The map automatically flies to fit the combined bounds of every currently checked activity, 300ms after the group last changed (so a rapid multi-check settles once, not once per checkbox). This is independent of FR-5.5 in both directions: checking a box never sets or clears the row-click focus.

**Notes**: A hidden activity (FR-5.8) can still be focused (FR-5.5) or checked; the system excludes hidden activities from the fly-to bounds specifically so the camera never flies to an area with nothing drawn on it. A row that is both focused and checked renders with the same single highlight treatment as either alone — there is no visually distinct "both" state.

### FR-5.7 Select all / Clear / Show selected

**Description**: A master checkbox in the header toolbar selects or clears every currently listed activity at once; a separate footer control re-flies to fit the current checked group on demand.

**Behavior**: The header checkbox reflects the checked group's state against the currently listed (TYPE/DISTANCE-filtered) rows — checked once every listed row is checked, unchecked once none are, and indeterminate for a partial selection. Clicking it when unchecked or indeterminate checks every listed row and flies to fit them all; clicking it when fully checked empties the checked group and flies the camera to fit every currently visible activity in the date range (respecting the hidden-activity set). The footer's **"Show selected"** button, disabled when nothing is checked, re-flies to fit the current checked group without changing it — for recovering the view after panning away from it.

**Notes**: Neither control affects the row-click focus (FR-5.5) — a focused row keeps its own highlight regardless of the header checkbox or "Show selected."

### FR-5.8 Hide/show a track (eye icon)

**Description**: Each row has an eye-icon control that hides or shows that activity's track on the map, independent of whether it is selected. A hidden activity's track is not drawn in any map mode (Normal, Fog, or Heatmap) until shown again. Hiding/showing is purely client-side and does not refetch data.

### FR-5.9 Resizable panel

**Description**: The Activities panel can be resized by dragging its right edge, between 260 and 560 pixels wide (default 380). Not persisted across reloads.

### FR-5.10 Edit activity type, name, and description

**Description**: A signed-in user renames an activity's type, gives it a name, and/or attaches a free-text description to it — for example, re-labeling an activity a fitness tracker logged under the wrong category, naming a road trip so it's identifiable in the Activities panel at a glance, or describing a non-sport GPS trace in more detail than a name allows.

**Preconditions**: Active session; the caller owns the activity.

**Inputs**: A new type (required, 1–50 characters), a name (optional, up to 200 characters), and a description (optional, up to 2000 characters) for one activity, entered via a small edit dialog reached from that activity's row.

**Behavior**:
1. Clicking a row's edit (pencil) icon opens a dialog pre-filled with that activity's current type, name, and description.
2. The type field is plain free text — the same "whatever the source reports, not a controlled vocabulary" rule FR-5.2's TYPE filter already follows (`IMPLEMENTATION.md` §4.7.2) applies equally to a manual rename. A list of this account's other existing types is offered as suggestions, purely as a convenience; nothing is enforced against it, and a value nobody has used before saves exactly as typed. The name field is always plain free text, with no source to ever populate it automatically — an activity has a name only once a person types one in here (`IMPLEMENTATION.md` §4.7).
3. Saving all three fields commits together in one request; canceling discards any unsaved edits.
4. Once saved: the row's TYPE label updates immediately; the new/renamed type becomes (or remains) a real entry in FR-5.2's TYPE filter with a live count; the row's primary line shows the name in place of its start date/time if one is set, or the start date/time as before if the name is cleared (FR-5.1); and the description becomes visible as a hover tooltip on the row — not a second visible line. **The Activities panel's sort order never changes**: rows stay ordered by start date/time (FR-5.1) regardless of what a row displays or whether it has a name at all.

**Outputs**: The activity's `activity_type`, `name`, and `description` are updated; every other computed value for that activity (distance, duration, its Fog-of-War/Heatmap coverage, its inclusion in FR-9's performance-analysis aggregates) is unaffected, since none of those are keyed on type, name, or description.

**Error cases**:
- Empty or over-length type, an over-length name, or an over-length description → `400 Bad Request`, no change applied.
- The activity does not exist or belongs to another account → `404 Not Found`, the two cases indistinguishable from each other.

### FR-5.11 Delete an activity

**Description**: A signed-in user permanently deletes one of their own activities — a full purge, not a soft delete or an archive: the activity itself, its track, and its contribution to Fog-of-War/Heatmap coverage are all removed. There is no undo.

**Preconditions**: Active session; the caller owns the activity.

**Inputs**: The activity to delete, reached via that row's delete (trash) icon.

**Behavior**:
1. Clicking the delete icon opens a confirmation dialog naming the activity and stating plainly that this can't be undone; nothing is deleted until the user confirms.
2. Confirming removes the activity and everything derived from it: its recorded stream data and its rendered coverage masks.
3. The Fog-of-War/Heatmap view updates to reflect the deletion — coverage the deleted activity was the only source for reverts to unrevealed, not left showing stale coverage for data that no longer exists.
4. Canceling the confirmation, or dismissing it, leaves the activity untouched.

**Outputs**: The activity and everything derived from it no longer exist; every list, filter, total, and aggregate that previously included it reflects its removal.

**Error cases**:
- The activity does not exist or belongs to another account → `404 Not Found`, indistinguishable from each other, same as FR-5.10.

### FR-5.12 Group visible

**Description**: An icon-only header toolbar button (the same eye icon FR-5.8's per-row control uses, positioned directly above it) toggles whether every currently checked (FR-5.6) activity is drawn on the map, in bulk — the same on/off idea applied to the whole checked group at once.

**Preconditions**: At least one activity is checked; the button is disabled otherwise.

**Behavior**: If any checked activity is currently hidden, clicking shows the entire checked group (removes all of them from the hidden set). If every checked activity is already visible, clicking hides the entire group instead. Each activity's individual FR-5.8 state is otherwise unaffected — hiding or showing the group is equivalent to toggling every checked row's own eye icon to the same state, not a separate mechanism.

**Outputs**: The hidden-activity set updates; the map's drawn tracks and Fog-of-War/Heatmap coverage reflect it immediately, the same as an individual eye-icon toggle does.

### FR-5.13 Delete group

**Description**: An icon-only header toolbar button (the same trash icon FR-5.11's per-row control uses, positioned directly above it) permanently deletes every currently checked (FR-5.6) activity at once — the same full purge FR-5.11 performs per activity, batched.

**Preconditions**: At least one activity is checked; the button is disabled otherwise.

**Inputs**: The checked group, reached via the header toolbar's delete icon.

**Behavior**:
1. Clicking the button opens a confirmation dialog naming how many activities are in the group and their combined distance, stating plainly that this can't be undone; nothing is deleted until the user confirms.
2. Confirming deletes every checked activity and everything derived from each one, the same as FR-5.11 performs for a single activity.
3. Canceling, or dismissing the confirmation, leaves every activity in the group untouched.

**Outputs**: Every activity in the group, and everything derived from each one, no longer exists; every list, filter, total, and aggregate reflects the removal in one combined refresh rather than once per deleted activity.

## 8. FR-6 — Date Range Picker

### FR-6.1 Default selection

**Description**: On first reaching the map with no prior selection, the range defaults to the 5 most recent days that have at least one activity (not the account's entire history) — chosen so the selection band starts with room to demonstrate dragging it, rather than already spanning the full loaded view. An account with no activity history at all defaults to "today" only, and the default is recalculated automatically as new activities arrive until the user makes their own explicit choice (dragging a handle or the band, or clicking a single day).

### FR-6.2 Resize the selection

**Description**: Dragging either edge handle of the highlighted band resizes the selected date range on the low end (start) or high end (end) independently.

**Behavior**: Resizing renders live locally while dragging and only re-fetches the affected lists (Activities panel, map, summary) once the drag is released, not on every pointer movement.

### FR-6.3 Slide the selection

**Description**: Dragging the highlighted band itself (not an edge) moves the selected range by the same number of days without changing its length.

### FR-6.4 Pan through history

**Description**: Independent of the selection, the visible window of the timeline strip can be moved backward/forward through the account's full history, via "Earlier"/"Later" buttons or by dragging the strip itself. Panning never changes what is selected — paging all the way back to the account's very first activity leaves the current selection exactly as it was (even if it scrolls off the visible strip, in which case it is simply not drawn until it comes back into view).

### FR-6.5 Select a single day

**Description**: Clicking a bar outside the current selected band (a plain click, not a drag) selects just that one day.

### FR-6.6 Changing the selection resets dependent state

**Description**: Committing a new date range (via FR-6.2, FR-6.3, or FR-6.5) clears both the row-click focus (FR-5.5) and the checked group (FR-5.6) independently, the hidden-activity set (FR-5.8), and both the TYPE and DISTANCE filters (FR-5.2/FR-5.3) together — all of these could otherwise silently describe activities the new range no longer lists.

## 9. FR-7 — Activity Graph (Profile)

### FR-7.1 Contribution grid

**Description**: A private, per-account page (reached via the account menu's "Profile" item) showing a GitHub-style daily contribution grid — one cell per calendar day, one block per calendar year (most recent first, back to the account's first-ever activity).

**Behavior**: Each day's cell is shaded by intensity, toggle-able between two measures:
- **Count**: number of activities that day (empty / one / two / three-or-more).
- **Distance**: quantile-based thresholds computed over that account's own active days for that year (so "a busy day" is relative to this account's own typical distances, not a fixed absolute number).

### FR-7.2 All-time and per-year stat cards

**Description**: Four statistics — Activities, Distance, Active Days, and Longest Streak (the longest run of consecutive calendar days with at least one activity) — shown once for the account's entire history, and again as a subtotal under each year's own grid.

## 10. FR-8 — Privacy Controls

### FR-8.1 Endpoint trimming

**Description**: Every uploaded activity has its start and end trimmed by a radius (200 meters by default) before being stored or rendered, so a recorded route never reveals the exact location an activity started or ended (typically home). The radius is user-adjustable (FR-1.7's Settings page, "Privacy trim"), 0–5000 meters, 0 meaning trimming is turned off entirely.

**Behavior**: Applied automatically to every ingested activity, at ingest time. Changing the value in Settings only affects uploads from that point forward — it is not retroactive: already-ingested activities keep whatever trim was in effect when they were processed, and the only way to re-trim one against a new value is to re-upload the original file (privacy trimming happens server-side against the raw payload each time, never against already-persisted, already-trimmed points).

### FR-8.2 Data isolation

**Description**: An account can only ever see its own activities, uploads, and profile data. Every data-returning endpoint derives the account from the caller's session; no endpoint accepts a user or account identifier as a request parameter that could be substituted for another account's.

## 11. FR-9 — Trends

### FR-9.1 Trends

**Description**: A signed-in account's own activity history, aggregated into weekly or monthly totals, viewable on the Profile page below the activity grid — how much ground was covered over recent weeks or months.

**Preconditions**: The caller has a valid session (real or demo user).

**Inputs**: `bucket` — `week` or `month`; `from`/`to` (`YYYY-MM-DD`, both optional, default to the trailing 12 months).

**Behavior**:
1. Every activity in the window is grouped into the requested bucket by its `started_at` date (UTC), one bucket per calendar week or month that has at least one activity — buckets with nothing recorded are omitted rather than returned as zeroes, the same convention FR-6's histogram uses.
2. Each bucket reports: activity count, total distance, total moving time, and total elevation gain.
3. The UI (`Trends`, on the Profile page) renders one bar per bucket, height scaled to the window's busiest bucket by distance, with a Week/Month toggle. Hovering a bar shows that bucket's full breakdown (distance, activity count, moving time, elevation gain).

**Outputs**: `{bucket, from, to, periods: [{period_start, count, distance_meters, moving_seconds, elevation_gain_m}, ...]}`.

**Notes**: "Moving time" falls back to elapsed time for any activity ingested before moving- time detection existed — those activities have no moving-time figure of their own, so this bucket-level total uses whichever one each activity actually has, rather than a bucket going silently short. Best-effort curves and personal bests (formerly FR-9.2/FR-9.3) were built and then cut — deliberately out of scope, see §1.2 and §14.

## 12. Non-Functional Requirements (summary)

This section summarizes cross-cutting behavior specified elsewhere in this document, for convenience — it does not introduce new requirements.

| Concern | Behavior |
| :-- | :-- |
| **Password storage** | bcrypt-hashed; plaintext is never stored or logged (FR-1.1). |
| **Session security** | Server-side, revocable sessions (not client-decodable tokens); expiry enforced server-side on every request, not trusted from the cookie alone (FR-1.4). |
| **Rate limiting** | Per-IP limits on the two endpoints reachable with no credentials at all: demo creation (FR-2.1) and password-reset requests (FR-1.5), 5/hour each. |
| **Non-blocking uploads** | No modal or locked UI during upload or processing (FR-3.1); the user can keep using the app while files process. |
| **No reload required** | Every list/summary this document describes updates itself automatically as background processing completes (FR-3.1, FR-3.4) — a manual page reload is never required to see current data. |
| **Idempotency** | Re-submitting the same activity content (FR-3.5) or the same password-reset token (FR-1.6) never has an effect beyond the first time. |

## 13. Mobile Browser Support

**Known issue**: The behavior below is what was designed and implemented, but the actual mobile experience has been reported directly as unusable, not just rough — this section describes intent, not a verified, working feature. Treat it as broken until re-verified on a real device and re-confirmed; see `docs/ROADMAP.md`'s "Mobile browser support" item (Phase 3).

**Description**: The application is usable in a phone-sized mobile browser, not just at desktop widths. This is a cross-cutting behavior, not a separate feature — it modifies how several of the FRs above render and are interacted with, rather than adding new ones.

**Behavior**:
1. Below approximately 768px viewport width, the Activities panel (FR-5.1) is a collapsible bottom sheet instead of a permanent sidebar — collapsed by default to a slim strip showing the range total and the Filter toggle, with the map fully interactive around and under it; tapping the strip expands it over the map to show the full list, filters, and footer actions (FR-5.2–FR-5.7), exactly as they behave at desktop width. Expanding or collapsing the sheet never changes the checked group (FR-5.6) or the row-click focus (FR-5.5).
2. The date-range picker's (FR-6) drag handles present a larger touch target than their visual width, so resizing the selected range is comfortable with a finger, not just a mouse cursor.
3. Trends (FR-9.1) — a hover-tooltip chart at desktop width — also responds to a tap: tapping a bar shows the same tooltip a hover would, tapping it again (or tapping empty chart space) hides it. A tap and a mouse hover never conflict with each other on the same chart.
4. Every other behavior in this document (upload, all three map modes, filtering, the date-range picker's paging/selection, account settings) works the same way at mobile widths as at desktop widths.

**Explicitly not built** (hover-only, no touch equivalent, unlike Trends above — a continuous position read with no discrete point to tap, not a per-bar value): the colored zone segments' and pace/heart-rate + elevation profile's exact hover values (FR-4.8, FR-4.9), and the two-way map-track-hover ↔ Activities-row-underline highlight (FR-4.1, FR-5.4). Both remain mouse-only; a touchscreen user can still see the colored bands and elevation curve themselves, and can still focus/select a track by tapping it, just not read an exact value by touch alone the way a mouse hover shows one.

## 14. Out-of-scope items, tracked for future revisions of this document

The following are named in `VISION.md`'s roadmap but have no functional requirements in this document because they are not yet built:

- Path 1 cloud-provider connectors (Garmin, Wahoo, COROS)
- Path 2 on-device sync (Apple HealthKit, Android Health Connect)
- Cross-source deduplication
- Explorer-tile gamification
- The rest of "Export" — story cards, animated reveals (high-resolution map export itself is built, FR-4.10)
- User-defined privacy zones (beyond the fixed endpoint trim in FR-8.1)
- Dark-theme variant of the Fog of War veil (the theme parameter is accepted but currently has no visual effect on the veil itself)

Deliberately out of scope, not a "not yet" — built and then cut, not planned to return: Oura and other recovery-data sources (sleep, HRV, readiness), best-effort curves, personal bests, power curves, and training load. `VISION.md` §1.1 draws a hard line against FitMap being a health or fitness advisor; pace and heart rate stay as per-activity route context (FR-4.9), not an analysed, all-time performance record.