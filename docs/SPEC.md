# FitMap: Spec

| | |
| :-- | :-- |
| **Version** | 1.0 |
| **Status** | Current — describes Phase 0/1 functionality as built |
| **Last updated** | 2026-09-20 |
| **Related documents** | `VISION.md` (product scope, market rationale, phase roadmap — the authority on *what ships and why*); `ARCHITECTURE.md` (system-level shape, key decisions, the stack); `IMPLEMENTATION.md` (schema, each feature's own implementation — the authority on *how it's built*); `AGENTS.md` (repository orientation) |

## 1. Introduction

### 1.1 Purpose

This document specifies FitMap's functional behavior as currently implemented: what the system does, from the point of view of a user or of another system calling its API — not why it was built that way (`VISION.md`) or how it is implemented internally (`IMPLEMENTATION.md`). Each functional requirement (FR) is written to be independently testable: given the stated preconditions and inputs, the stated behavior and outputs should be observable.

### 1.2 Scope

**In scope**: every feature currently built and shipped, as of this document's last-updated date — authentication and account management (including account settings — avatar, name, country, and privacy trim, FR-1.7; email verification, FR-1.8), the no-signup demo (now read-only, seeded from a persistent, richly-populated Demo Customer account rather than a fresh per-visitor preset — FR-2.1), activity upload and ingestion (file upload, `.zip` bulk import, Google Takeout import, Android's Health Connect mobile sync — FR-3.6, and Android's in-app GPS recording — FR-3.8), cross-source duplicate detection (FR-3.7), map visualization (track rendering, Fog of War, Heatmap, colored zone segments, the pace/heart-rate + elevation profile, high-resolution export), the Activities panel and its filters, the date-range picker, the per-account activity graph, password recovery, and distance/time trends (FR-9 below).

**Out of scope**: functionality named in `VISION.md`'s roadmap (§5.3 onward) but not yet built — Path 1 cloud-provider connectors (Garmin/Wahoo/COROS), Path 2 on-device sync's iOS/HealthKit half (no iOS app exists yet; Android's Health Connect half shipped — FR-3.6), explorer-tile gamification, the rest of "Export" (story cards, animated reveals — high-resolution map export itself is built, FR-4.10 below), and user-defined privacy zones (a `privacy_zones` table exists in the schema — `IMPLEMENTATION.md` §3.7 — but no endpoint or UI creates or applies one today; only the endpoint-trim privacy control in FR-8.1 below, user-adjustable via FR-1.7's Settings page, is functional). Also deliberately out of scope, not a "not yet" — best-effort curves, personal bests, power curves, and training load were built and then cut: `VISION.md` §1.1 draws a hard line against FitMap being a health or fitness advisor, and pace/heart-rate stay as per-activity route context (FR-4.9) rather than an analysed, all-time performance record. This document will be extended with new FR sections as in-scope functionality ships, not rewritten in place of them.

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
| **Fog of War** | A map mode that shows a dark veil over everywhere the signed-in user has *not* recorded an activity, so recorded routes appear as "cleared" ground. |
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
3. Server hashes the password (bcrypt) and creates a new `users` row — always a fresh row, whether or not the caller's browser holds a live demo session (see FR-2.3, revised).
4. Server creates a session (30-day expiry) and sets it as an `HttpOnly` cookie, and sends a verification email (FR-1.8).
5. Client is signed in, but held on FR-1.8's "verify your email" screen rather than shown the map, until the account is verified.

**Outputs**: A valid session cookie; the account's email and its (unverified) status are returned to the client.

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

**Notes**: A session's validity is checked in the database on every request (not trusted from the cookie's own stated expiry), so a session ended server-side (FR-1.3, or invalidated by a password reset, FR-1.6) stops working immediately even if the browser still holds the cookie. Sessions last 30 days from creation. The response also reports whether the account's email is verified (always `true` for a demo account) — the client uses this to decide whether to show the map or FR-1.8's verify screen.

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

### FR-1.8 Email verification

**Description**: A real (non-demo) account created by FR-1.1 must confirm its email address before it can use anything beyond this screen — the map and every other authenticated route are gated on it. A demo account (FR-2) is never subject to this gate.

**Preconditions**: An active session for a real account whose email is not yet verified.

**Behavior**:
1. On signup (FR-1.1) and whenever the email address changes (step 4 below), the server emails a link containing a verification token (valid 24 hours, single-use) to the address on file.
2. Clicking the link submits the token to `POST /v1/auth/verify-email`. On success, the server marks the account verified, invalidates every other outstanding verification token for it, and creates a fresh session for whichever browser opened the link — regardless of whether that browser already held a session of its own, so the link works from any device.
3. While waiting, the account holder can request another copy of the link (`POST /v1/auth/resend-verification`, rate-limited to 5 per hour per account) without needing to already know it was lost or expired.
4. The account holder can also change the address on file (`PATCH /v1/auth/email`) before ever verifying — correcting a typo the original signup made, since a resend alone cannot fix a wrong address. Any change resets the account back to unverified and sends a new link to the new address, whether or not the account was already verified.
5. Until verified, every route other than `GET /v1/auth/me`, `POST /v1/auth/logout`, and the three endpoints above returns `403 Forbidden` with a distinguishable error rather than the normal response.

**Outputs**: `verify-email` and `reset-password` both return a valid session cookie for the account on success. `resend-verification` and `change-email` return a confirmation; `change-email` also returns the account's current (now-unverified) profile.

**Error cases**:
- Verification token missing, already used, or expired → `400 Bad Request` with a generic message, same non-distinguishing reasoning as FR-1.6's reset token.
- `change-email`/`resend-verification` attempted by a demo account → `400 Bad Request` (neither concept applies to one).
- More than 5 resend requests for the same account within an hour → `429 Too Many Requests`.

**Notes**: This reverses an earlier, deliberately simpler version of FR-1 that had no email verification at all — added once real Health Connect/cloud sync made an unrecoverable, mistyped-email account a real cost (server-side ingest work stranded on an account nobody can get back into), not because the original simplicity was a mistake.

## 4. FR-2 — No-Signup Demo

### FR-2.1 Start a demo

**Description**: An anonymous visitor tries the full application without creating an account.

**Preconditions**: No active session.

**Behavior**:
1. Client calls `POST /v1/auth/demo`.
2. Server opens a session against one persistent, shared **Demo Customer** account — not a fresh account created per visitor. That account is pre-seeded, once, out of band (not per request — see FR-2.2), with a real, richly-populated history: roughly 611 activities spanning about 7 months, a mix of walks, dog walks, bike rides, local errands, and a few multi-day road trips.
3. Server creates a session for this visitor (24-hour expiry) and sets it as a cookie. Any number of visitors can hold their own session against the same shared account at once — they all see identical data.
4. Client is signed in and shown the map, with the account's full history already visible in every mode (Normal, Fog of War, Heatmap) and every read-only feature (filtering, the activity graph, export) exactly as a registered user has it. **A demo account cannot upload, sync, edit, or delete an activity, or change its avatar/settings** — every such request is rejected regardless of what a client attempts, whether or not its own UI still offers the control. Creating a real account (FR-1.1) is the only way to save one's own data. Both clients also gate this proactively rather than relying on the rejection alone: the web app disables the Import control with an explanation (`ImportPanel.tsx`'s `readOnly` prop) and the Android app disables Sync Now and hides the Health Connect permission flow with the same explanation (FR-3.8) — a demo session can still record and manage GPS Logger recordings locally on Android, since nothing about that reaches the server until sync is attempted.

**Outputs**: A valid session cookie for the shared Demo Customer account, already showing its full activity history.

**Rate limiting**: Limited to 5 demo session starts per hour per client IP address; further requests receive `429 Too Many Requests`.

### FR-2.2 Demo account seeding and lifetime

**Description**: Unlike a demo account under the earlier per-visitor design, the shared Demo Customer account (FR-2.1) and its activity history are not created or deleted per visit — only each visitor's own *session* is temporary.

**Behavior**: The Demo Customer account is seeded once, out of band, via a `seed-demo-customer` CLI subcommand run at deploy time (not from any HTTP request) — re-running it is safe and only fills in anything missing. Its `demo_expires_at` is set to a fixed far-future timestamp rather than left null, which is what keeps it read-only (FR-2.1) without ever matching the background purge sweep's `< NOW()` condition, so the account and its data are never deleted. Each visitor's own *session* still expires 24 hours after `POST /v1/auth/demo` was called, same as any other session — a visitor who stays past that just calls it again for a fresh session against the same account.

**Notes**: This replaces an earlier design where each demo visitor got their own new, ephemeral account seeded with two small fixed presets, deleted 24 hours later by the same background sweep. That per-visitor purge sweep still exists (`internal/worker/demo_purge.go`) and still runs, but has nothing left to act on under the current design — it would only matter again if a future change reintroduced per-visitor demo accounts.

### FR-2.3 Create a registered account from a demo session

**Description**: A demo user creates a real, permanent account of their own to start saving data. The shared Demo Customer account (FR-2.1) belongs to no one visitor in particular and is never modified by one, so this is an ordinary new signup, not a conversion: nothing from the demo carries over, and nothing the person does in a demo session is "theirs" to keep in the first place.

**Preconditions**: An active demo session.

**Inputs**: Email address, password (minimum 8 characters) — the same inputs as FR-1.1.

**Behavior**:
1. From the account menu, the user selects "Demo session — save this," which presents the same sign-up screen a new visitor sees (FR-1.1), with a "← Back" option instead of the "try demo" option (starting a second demo while already in one would abandon the first).
2. The user submits an email and password.
3. The server creates a plain new account (FR-1.1's normal behavior) — the shared Demo Customer account itself is untouched, exactly as every other concurrent demo visitor's session leaves it.
4. The user is signed in to the new account, held on FR-1.8's verify-email screen exactly like any other fresh signup.

**Outputs**: A new, unverified registered account — not the demo account, and not carrying any of its activity history.

**Note — cancellation**: selecting "← Back" (or navigating away without submitting) returns to the map with the demo session untouched — nothing is lost, and it remains subject to FR-2.1's 24-hour session expiry (the underlying account itself is unaffected either way, per FR-2.2).

## 5. FR-3 — Activity Upload & Ingestion

All upload functionality requires an active session (demo or registered — FR-1/FR-2); there is no unauthenticated upload path.

### FR-3.1 Upload individual files

**Description**: A signed-in user uploads one or more individual activity files.

**Preconditions**: Active session.

**Inputs**: One or more files, each a `.gpx`, `.fit`, or `.tcx` file no larger than 64 MiB. Up to 20 individually-selected files per batch (a larger selection is rejected client-side in full, before any upload begins, with a message directing the user to a `.zip` archive instead — FR-3.2).

**Behavior**:
1. User drags files onto the Import panel's Files tab, or selects them via a file picker.
2. Each file uploads independently, as its own `POST /v1/activities/upload` request (multipart), and is tracked independently — one file failing does not affect the others.
3. For each file: server validates its extension and size, computes a content hash to check for a duplicate (FR-3.5), persists the raw file, and enqueues a background parsing job.
4. The Files tab shows each file's live status (uploading, with a progress percentage; then "Processing…") until the background job finishes.
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
1. User uploads the Takeout export `.zip` through the same Files tab as FR-3.1/FR-3.2.
2. Server recognizes the archive's shape as a Takeout export (rather than a plain `.zip`) and extracts one activity file per recorded activity that has GPS data (activity types with no GPS in the export — e.g. a logged swim with no route — are skipped, not treated as errors).
3. Each extracted activity is ingested exactly as FR-3.1 describes, attributed to the Takeout source.

**Outputs**: One new `Activity` per activity in the export that had GPS data.

### FR-3.4 Import status and history

**Description**: A user can see the status of in-progress and past uploads and syncs, and jump from a finished one straight to it on the map. The header's "Import" control (renamed from "Upload activity" once a second ingest source existed — FR-3.6) opens a dropdown with two tabs: **Files** (drag/drop, `.zip`, and Google Takeout — FR-3.1–FR-3.3, unchanged) and **Sync** (Health Connect and in-app GPS recording activity synced from the Android app — FR-3.6, FR-3.8). Sync is a read-only status view, not a "sync now" button: that sync is phone-triggered, and nothing in the web app can request it (`apps/android/docs/SPEC.md` §7.4's own "Ask every time"/"Always allow" split is the closest analogue, and it lives entirely on the phone).

**Preconditions**: Active session.

**Behavior**:
1. The Import control's badge shows a live count of jobs still processing, combined across both tabs, regardless of which tab is currently open.
2. Each tab shows its own paginated list (5 per page) of every matching job ever recorded for this account, each showing: a title (the filename, for a Files row; the source name — "Health Connect," "GPS Logger" — for a Sync row, since a synced job's own filename is a platform-assigned id with nothing human-readable in it), status ("Processing…" / "Ready" / "Failed"), and — once ready — the activity's date and distance.
3. While anything in a tab is still processing, that tab's list refreshes automatically (polled every 1.5 seconds) until every row settles to "Ready" or "Failed" — no manual refresh needed. Both tabs poll independently and simultaneously, regardless of which one is currently showing, since a job finishing in the tab that isn't open still has to reach the map.
4. A "View on map" action appears on every finished (`"Ready"`) row, on either tab. Clicking it closes the Import dropdown, focuses that activity exactly as clicking its row in the Activities panel would (FR-5.5) — track bolded, camera flown to fit it — and, if the activity's own date falls outside the currently selected date range (FR-6), first narrows the selected range to just that one day (the same mechanism a manual single-day pick already uses — FR-6.5) before focusing, rather than focusing something the Activities panel isn't currently showing at all.

**Outputs**: `GET /v1/uploads?limit=&offset=&source=` returns the current page, the total count (scoped to `source` when given), and how many are still processing (always the global count, unscoped, for the badge). `source` is a comma-separated filter — `upload,takeout` for the Files tab, `healthconnect,healthkit,recorded` for Sync — omitted for the unfiltered combined view neither tab actually uses today. Each row also carries `source` and, once the job has produced one, the resulting activity's own `id` — what the "View on map" action targets.

### FR-3.5 Duplicate detection

**Description**: Re-uploading a file whose content was already ingested does not create a second activity or a second processing job.

**Behavior**: The server checks a content-derived identifier before enqueuing a new job. If that exact content was already ingested for this account, the upload responds immediately with an "already processed" status (`200 OK`) and no new job is created; the client shows a notice that the file was already uploaded rather than silently doing nothing.

**Notes**: This guarantee holds even under concurrent duplicate uploads (e.g., two tabs uploading the same file at once) — at most one `Activity` is ever created for the same content.

### FR-3.6 Mobile sync (Android / Health Connect)

**Description**: The Android app reads a signed-in account's exercise history from Health Connect and syncs it to FitMap — a second ingest path (Path 2) alongside file upload above, distinct from a file the user explicitly picked.

**Preconditions**: Signed in on the Android app (`apps/android/fitmap`); Health Connect installed, with the Exercise permission granted plus the separately-granted "Access exercise routes" permission — a session with no route geometry can't be placed on the map, so it's rejected at sync time rather than persisted without one (see step 3).

**Inputs**: Health Connect exercise sessions with route geometry, read **foreground-only** — `READ_EXERCISE_ROUTES` returns `ConsentRequired` in the background regardless of what's granted, a platform constraint rather than a client choice. `READ_HEALTH_DATA_HISTORY`, requested separately, extends the otherwise 30-day-only read window.

**Behavior**:
1. User opens the sync screen; the app walks through granting whichever Health Connect permissions are still missing.
2. A foreground sync run reads sessions ascending from the account's own last confirmed position (a cursor keyed on both an instant and the record ids already handled at it, not a bare timestamp — two sessions can share a start instant), classifies each one, and posts batches to `POST /v1/sync/activities`.
3. A session with no route (an indoor workout, or any Samsung Galaxy Watch session — Samsung doesn't expose route geometry via Health Connect at all) is skipped and reported as such, not treated as a failure; the cursor still advances past it.
4. A route that exists but can't be read this run (`ConsentRequired`, e.g. the app was backgrounded mid-run) is reported distinctly from "no route" and blocks the cursor from advancing past it, so a resumed run retries it rather than skipping it permanently.
5. Each synced activity is idempotent on the Health Connect record's own id and flows through the exact same ingest pipeline FR-3.1's file upload uses. Activity type is normalized onto FitMap's existing vocabulary (Health Connect's `biking` becomes `cycling`, etc.), so it doesn't fragment the TYPE filter or defeat FR-3.7's cross-source matching.

**Outputs**: One new `Activity` per synced session with a route; a per-run summary (synced / skipped-no-route / rejected, each with its own reason) on the sync screen; and a persistent history via the same `GET /v1/uploads` FR-3.4 already describes — a Health Connect sync and a file upload are the same kind of ingest job, not two separate histories.

**Error cases**: Server unreachable mid-run — the run stops, reports the failure, and the cursor does not advance past anything the server never confirmed, so a retried run resumes rather than re-sending everything already accepted.

**Not yet built**: the iOS/HealthKit half of Path 2 — no iOS app exists yet (`apps/ios` is a placeholder). `apps/android/docs/ROADMAP.md` is the authority on Android's own remaining work (UI design, Play Store compliance).

### FR-3.7 Cross-source duplicate detection

**Description**: The same real-world activity arriving from two different sources (e.g. synced from a watch via Health Connect, then later also uploaded as an exported `.fit` file) is recognized as one activity, not two — distinct from FR-3.5, which only catches identical re-uploaded file content from the same source.

**Preconditions**: At least two ingest sources have produced activities for the account close enough in time to compare (FR-3.6's mobile sync is what makes this reachable at all today; Path 1 cloud connectors will be a third source once built).

**Behavior**:
1. On ingest, a new activity is compared against the account's existing ones within a fuzzy window — same activity type, start time within thirty seconds either way, distance within about 1% — rather than exact equality on a pre-rounded bucket, which would miss a pair that happens to straddle a rounding boundary. The type comparison itself is exact, not fuzzy, so a source that cannot state a real type is guarded at that source instead — FR-3.8 step 5 blocks queuing an in-app recording still on its unedited `"unknown"` default for exactly this reason.
2. A match is resolved by keeping the richer record (route geometry over none; more data channels, e.g. heart rate, over fewer) and marking the other `superseded_by` the winner, rather than deleting it.
3. Every user-facing read — the Activities list, totals, histogram, day pages, trends, graph stats, map tiles, and both Fog of War and Heatmap composites — excludes superseded activities automatically.
4. Deleting the kept copy of a matched pair promotes the next-richest superseded copy back to live, rather than leaving both gone.

**Outputs**: At most one live `Activity` per real-world activity, regardless of how many sources reported it.

5. The web app's Activities panel surfaces a "N duplicates found" disclosure whenever `GET /v1/activities/duplicates` returns any rows, listing each superseded activity's start time, type, distance and source, and which source's copy superseded it — mirroring the Android app's own sync screen, which has shown this since FR-3.6 shipped. Hidden entirely when there are none.

### FR-3.8 In-app GPS recording (Android)

**Description**: The Android app records a casual, GPS-only activity itself — a walk, hike, or drive someone would not otherwise bother tracking. Recording and syncing are two separate, explicit steps: Stop only saves a finished recording to the device; nothing reaches the server until the user marks it to sync and taps the Sync screen's "Sync Now" — a third way an activity can originate on the Android app, alongside FR-3.6's Health Connect sync and a manual file upload (FR-3.1) done from the phone's browser.

**Preconditions**: Location permission granted to record. No account or sign-in is needed to record or manage recordings locally — only to actually sync one, the same precondition FR-3.6 has. Local recordings are scoped to whichever account is currently signed in (or to no account, if none is) — switching accounts on one device never shows one account's recordings under another's.

**Inputs**: The device's own GPS, read while a recording is in progress; a name, an activity type (free text, not a fixed list), and a description, editable on the recording screen and, after the fact, from **Recorded Activities**' Edit button.

**Behavior**:
1. User opens **GPS Logger** from the app's menu. Name and Description start empty and Activity Type starts `"unknown"` — all three are plain text fields, editable at any point before Stop; the type field also offers walk/hike/run/ride/drive as tap-to-fill suggestions, but any text is accepted.
2. **Record** starts a foreground-service-backed location recording, so it survives the screen turning off; **Pause**/**Resume** are user-initiated only — there is no automatic pause. A live readout shows elapsed time, distance, current altitude, and current speed while recording.
3. **Stop** ends the recording and saves it to an on-device store only, tagged not-synced — no network request happens. A recording with fewer than 2 points (Stop pressed before any location fix arrived) is not saved at all, matching the sync endpoint's own floor.
4. **Recorded Activities** (a separate menu item) lists every locally saved recording, newest first, filterable by sync status (not synced / queued / synced) and by activity type. Each row has a sync checkbox and an Edit button.
5. Checking a row's checkbox marks it queued; unchecking an unsynced row clears the queue mark. A synced row's checkbox is always checked and cannot be unchecked. A row whose Activity Type is still the unedited `"unknown"` default cannot be checked at all — the row explains why ("Set a type to sync") — since FR-3.7's cross-source match requires an exact activity-type match, and an untyped recording can never match a same-walk activity that arrived typed from another source (e.g. Health Connect). Editing an already-queued row's type back to blank (Edit stays unlocked until sync) reverts it to not-synced for the same reason.
6. **Edit** reopens the same recording screen against the saved row — the Record/Pause/Stop controls are replaced by a single Save button, and the Name/Type/Description fields (plus the recorded stats, read-only) are pre-filled. Editable at any point up until the row syncs; once synced, the fields and Save button are replaced with a notice that the row is locked.
7. The Sync screen's existing "Sync Now" (FR-3.6) submits every queued row in the same run as the Health Connect sync, each as its own `POST /v1/sync/activities` call under `source = "recorded"` with a client-generated `external_id` (a UUID minted at Record) — the same batched wire shape FR-3.6 uses, reusing its ingest pipeline unchanged (`docs/IMPLEMENTATION.md` §4.0.4, [ADR-0007](adr/0007-in-app-gps-recording-submits-directly.md)). A row that syncs successfully is marked synced; a row the server rejects, or that fails outright, stays queued and is retried automatically on the next "Sync Now" — no action needed from the user.
8. **Delete**, on every row regardless of sync status (unlike Edit, which locks once synced), asks for confirmation and then removes the row from this device only. For an already-synced row this is deliberately not the same as FR-5.11's server-side delete: the real `Activity` it produced stays exactly where it is — on the map, in the web app, counted in every total — and the confirmation dialog says so before the user confirms, rather than implying a purge that doesn't happen.
9. **A demo session** (FR-2.1) can record and manage rows here like any other account, but every sync checkbox is disabled and a notice explains why — queuing something "Sync Now" can never actually take would be pointless, and the Sync screen itself disables Sync Now for the same reason (FR-2.1).

**Outputs**: One new `Activity` per successfully synced recording, titled and described from the moment it's created rather than needing an edit afterward — the one ingest path where that's true (every other path leaves both fields unset at ingest). Reported through the same `GET /v1/uploads` history FR-3.4 already describes, and reflected back on the recording's own row (queued → synced) in Recorded Activities.

**Error cases**: A too-long custom activity type (over 50 characters, the column's own bound) is rejected by the sync endpoint with a clear reason rather than failing as a raw database error at insert time. A queued row that fails to sync (network error, server rejection) is left queued rather than reverted, so a retried "Sync Now" tries it again without the user re-checking anything.

**Not yet built**: recovery from an app/process kill mid-recording (the next launch does not offer to resume or discard a buffered-but-unsubmitted in-progress recording — this is distinct from the finished, saved-but-unsynced rows Recorded Activities already manages); a discard confirmation before Stop finalizes a save; the iOS half (`docs/ROADMAP.md` Phase 2 tracks it as a combined Android/iOS item; Android's half is what this FR describes).

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

**Description**: An alternate map mode showing a dark veil over everywhere the user has not recorded an activity — true all-time coverage, ignoring every other filter.

**Behavior**:
1. Selecting "Fog" from the map-mode toggle replaces the track lines with a raster veil: any area a recorded route has ever passed through is rendered clear; everywhere else stays fogged.
2. Fog ignores the date range and the Type/Distance/hidden-track filters entirely — it always shows every activity the account has ever recorded, not just what Normal mode currently has selected. Entering Fog hides the Activities panel and the date-range picker (there is nothing for either to filter), clears any checked or focused activity, and flies the camera to fit the account's full all-time extent.
3. Individual track lines are not drawn in this mode (the veil itself is the information).
4. Below a threshold zoom, the veil switches from per-pixel coverage to a coarser reveal: a country renders fully clear the moment the account has at least one activity anywhere inside it, however small; one zoom step in, the same applies one administrative level down, at state/region granularity. Zooming back in past the threshold returns to exact per-pixel coverage — the two never blend or overlap, only one is ever shown at a given zoom.
5. Returning to Normal mode restores the previously checked/focused activities, the date range, and the panel/picker exactly as they were before switching to Fog.

### FR-4.3 Heatmap mode

**Description**: An alternate map mode shading locations by how often they've been visited recently — a rolling window, not an all-time record, so a route no longer visited can cool off.

**Behavior**:
1. Selecting "Heatmap" replaces the track lines with a raster overlay, brighter wherever more recorded activity has crossed the same location (a daily commute reads brighter than a once-ridden road).
2. Like Fog of War, Heatmap ignores the date range and the Type/Distance/hidden-track filters, hides the Activities panel and date-range picker, and clears any checked or focused activity. Unlike Fog, it only considers activities within a fixed rolling window (the last 365 days, not user-configurable) — the camera flies to fit that window's coverage, not the account's full history.
3. Individual track lines are not drawn in this mode.
4. How much crossing traffic it takes to reach full brightness adapts to the account's own history, recomputed daily — a new account and a long-running one don't saturate at the same point, so each account's own most-used spot is what reads as hottest, not a fixed number of visits everyone shares.
5. Below the same threshold zoom Fog switches at, the graded overlay is replaced by a flat "visited" highlight at country granularity, and one step in at state/region granularity — not graded by how much, only whether the account has a currently-in-window activity there. A country/region whose only activity has aged out of the rolling window shows no highlight at this zoom either, matching what the per-pixel overlay already shows at city zoom.
6. Returning to Normal mode restores the previously checked/focused activities, the date range, and the panel/picker exactly as they were before switching to Heatmap.

### FR-4.4 Mode is mutually exclusive

**Description**: Normal, Fog, and Heatmap are three views of the same underlying data, not independent toggles — exactly one is active at a time.

### FR-4.5 Base map, theming, and the opening view

**Description**: The map renders a self-hosted vector base map (streets, labels) in either a light or dark theme, selected via the page's URL (no in-app toggle). The current camera position (center, zoom) and theme are reflected in the URL and restored on reload, so a specific view is shareable via link.

**Preconditions**: Active session.

**Behavior**:
1. If the URL carries a saved or shared position (`#map=...`), it wins outright — restored on load, ahead of every fallback below.
2. Otherwise, the account's own most recent activity determines the opening view: the camera flies to fit that single activity, not the full default date-range selection (FR-6.1) — an account with scattered recent history (one activity in another country yesterday, one locally today) would otherwise fly to a near-world view that reads as broken rather than just generic.
3. An account with no activity history at all falls back to its Country setting (FR-1.7), at that country's own view, if one is set.
4. If none of the above applies — no saved position, no activity history, no Country set — the camera opens on a fixed, deliberately zoomed-out world view.
5. Panning or zooming rewrites the URL's saved position continuously, so the current view is always what a copied link restores.

**Notes**: A fresh session (signing out, then signing in or starting a demo session) never inherits a previous session's saved camera position — only an unmodified reload of the same session does. This resolution order never requests the browser's geolocation permission; FR-4.7's "Find my location" is a separate, always-available, user-clicked control, not part of it.

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

### FR-4.10 Interactive frame-and-capture map export

**Description**: A header control opens a shape-picker, then a draggable/resizable frame over the live map, and captures exactly the region inside that frame as a high-resolution PNG image, downloaded directly to the caller's device.

**Preconditions**: Active session; the map has finished its initial load. No activity selection is required — the user frames whatever region of the map they want manually, independent of any checked/focused activity.

**Behavior**:
1. Clicking Export opens a dialog showing platform image-size presets as a grid — one column per platform (Instagram, Facebook, X, Pinterest), one row per resolution/shape (Square, Portrait, Landscape, Story, Pin) — with a Custom option, no fixed dimensions, as a single button below the grid. Not every platform fills every row (e.g. X has no Story, only Pinterest has Pin); a cell with no matching preset is left empty.
2. Picking a grid cell or Custom closes the dialog and shows a frame over the live map matching the chosen shape, centered and sized to fit comfortably within the map's own bounds, marked only by a dashed border — the map itself is not dimmed or obscured anywhere, including under the frame, and remains fully interactive (pan/zoom/click) everywhere outside the frame's own bounds.
3. Dragging anywhere inside the frame repositions it anywhere over the map (aspect ratio unchanged); dragging the live map means grabbing anywhere outside the frame, same as normal map panning. If Custom was chosen, an additional corner handle resizes the frame freely, with no fixed aspect ratio; a preset's frame cannot be resized, only repositioned.
4. Two buttons sit inside the frame's own top-right corner: Close, which cancels and returns to the normal map view with nothing captured, and Capture, which captures exactly the region inside the frame at that moment — the current camera position, theme, and map mode, at a resolution well above the on-screen canvas, scaled to the picked preset's exact declared pixel dimensions (or, for Custom, scaled proportionally so the frame's longer side hits a fixed ceiling). In Normal mode the captured region reflects the current date-range/hidden-track filters; Fog captures its all-time coverage and Heatmap its current rolling window (FR-4.2/FR-4.3), regardless of what Normal mode's filters were set to before switching.
5. The live map is not disturbed by a capture — camera, zoom, and mode remain exactly as they were before it was triggered.
6. While a capture is generating, the Capture button shows a busy state; a failure (e.g. a timeout waiting for tiles to load at export resolution) is reported inline, near the buttons, and the frame stays in place rather than being discarded — the user is not forced to reposition it and retry from scratch.
7. Escape, or the frame's own Close button, cancels the current step (the picker dialog, or an open frame) and returns to the normal map view with nothing captured.

**Outputs**: On a successful capture, a PNG file download, named `fitmap-{date}.png`.

**Notes**: This is one of three things `VISION.md` §4.2 groups under "Export" — story cards and animated reveals are not built. Colored zone segments (FR-4.8) are not reflected in a capture even when currently shown on screen — exporting a single focused activity's bands is a narrower case not covered by this slice. Vector/SVG output is not offered; raster (PNG) only. Platform preset dimensions are curated from Hootsuite's social-media-image-sizes guide; profile-picture/cover-photo sizes are excluded, since this feature frames map content, not an account avatar.

## 7. FR-5 — Activities Panel

### FR-5.1 Activity list

**Description**: A permanent sidebar lists every activity within the currently selected date range (FR-6), narrowed by the TYPE and DISTANCE filters below.

**Behavior**: Each row's primary line is the activity's own name if one has been set (FR-5.10), or its start date/time otherwise — an activity has a name only once a person has typed one in via FR-5.10's edit dialog, never from parsing a source file. A row whose primary line is a name still shows its date/time as part of the row's secondary line, alongside distance, duration, and type — a row's only other elements are its checkbox and this text; there is no separate per-row column or icon of any kind, and no per-row action controls (edit/delete/hide are reached via FR-5.6's checkbox plus the header toolbar, not from the row itself). Regardless of what a row displays, **the list itself is always ordered by start date/time, newest first** — a name never affects sort order. The list is not paginated — every matching activity is shown at once. The panel also shows a running count of matching activities and total distance for the range (independent of the TYPE/DISTANCE filters, which narrow the visible rows without changing this total).

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

### FR-5.7 Select all / Clear / Focus on map

**Description**: A master checkbox in the header toolbar selects or clears every currently listed activity at once; a separate toolbar icon re-flies to fit the current checked group on demand.

**Behavior**: The header checkbox reflects the checked group's state against the currently listed (TYPE/DISTANCE-filtered) rows — checked once every listed row is checked, unchecked once none are, and indeterminate for a partial selection. Clicking it when unchecked or indeterminate checks every listed row and flies to fit them all; clicking it when fully checked empties the checked group and flies the camera to fit every currently visible activity in the date range (respecting the hidden-activity set). The toolbar's accent-tinted **Focus on map** icon, disabled when nothing is checked, re-flies to fit the current checked group without changing it — for recovering the view after panning away from it. This is also the only way to fly to a single checked activity's own bounds by group rather than by row-click (FR-5.5).

**Notes**: Neither control affects the row-click focus (FR-5.5) — a focused row keeps its own highlight regardless of the header checkbox or Focus on map.

### FR-5.8 Hide/show a track

**Description**: The header toolbar's Show/hide icon (FR-5.12) hides or shows every currently checked activity's track on the map, independent of the row-click focus (FR-5.5). There is no per-row hide/show control — hiding or showing a single activity means checking just its own box first, the same as any other single-item action. A hidden activity's track is not drawn in any map mode (Normal, Fog, or Heatmap) until shown again; its row dims in place and carries a "Hidden" badge so its hidden state is still visible at a glance. Hiding/showing is purely client-side and does not refetch data. See FR-5.12 for the exact toggle rule.

### FR-5.9 Resizable panel

**Description**: The Activities panel can be resized by dragging its right edge, between 260 and 560 pixels wide (default 380). Not persisted across reloads.

### FR-5.10 Edit activity type, name, and description

**Description**: A signed-in user renames an activity's type, gives it a name, and/or attaches a free-text description to it — for example, re-labeling an activity a fitness tracker logged under the wrong category, naming a road trip so it's identifiable in the Activities panel at a glance, or describing a non-sport GPS trace in more detail than a name allows.

**Preconditions**: Active session; the caller owns the activity.

**Inputs**: Reached via the header toolbar's Edit icon (FR-5.6's checkbox group must be non-empty) or a single row's checkbox followed by that same icon — there is no per-row edit control. Editing exactly one checked activity accepts a new type (required, 1–50 characters), a name (optional, up to 200 characters), and a description (optional, up to 2000 characters). Editing more than one checked activity at once accepts only a new type — the Name and Description fields are disabled, since there is nothing consistent to set across several different activities' names/descriptions in one request.

**Behavior**:
1. Checking one or more rows and clicking the toolbar's Edit icon opens a dialog. With exactly one activity checked, it is pre-filled with that activity's current type, name, and description, all three editable. With more than one checked, only the type field is editable, seeded from the first checked activity; the Name and Description fields render disabled with an explanation of why.
2. The type field is plain free text — the same "whatever the source reports, not a controlled vocabulary" rule FR-5.2's TYPE filter already follows (`IMPLEMENTATION.md` §4.7.2) applies equally to a manual rename. A list of this account's other existing types is offered as suggestions, purely as a convenience; nothing is enforced against it, and a value nobody has used before saves exactly as typed. The name field is always plain free text, with no source to ever populate it automatically — an activity has a name only once a person types one in here (`IMPLEMENTATION.md` §4.7).
3. Saving commits in one request per checked activity. For a single checked activity, all three fields commit together. For a group, each activity's own request carries the new shared type alongside that activity's own existing name and description unchanged — a group edit never touches Name or Description, even though the underlying request is a full replace. Canceling discards any unsaved edits.
4. Once saved: each affected row's displayed type updates immediately; the new/renamed type becomes (or remains) a real entry in FR-5.2's TYPE filter with a live count; a single-activity edit's row shows the name in place of its start date/time if one is set, or the start date/time as before if the name is cleared (FR-5.1); and its description becomes visible as a hover tooltip on the row — not a second visible line. **The Activities panel's sort order never changes**: rows stay ordered by start date/time (FR-5.1) regardless of what a row displays or whether it has a name at all.

**Outputs**: Each edited activity's `activity_type` is updated (plus `name`/`description` for a single-activity edit); every other computed value for that activity (distance, duration, its Fog-of-War/Heatmap coverage, its inclusion in FR-9's performance-analysis aggregates) is unaffected, since none of those are keyed on type, name, or description.

**Error cases**:
- Empty or over-length type, an over-length name, or an over-length description → `400 Bad Request`, no change applied for that activity.
- An activity does not exist or belongs to another account → `404 Not Found`, indistinguishable from each other.

### FR-5.11 Delete an activity

**Description**: A signed-in user permanently deletes one or more of their own activities — a full purge, not a soft delete or an archive: each activity itself, its track, and its contribution to Fog-of-War/Heatmap coverage are all removed. There is no undo. Deleting a single activity and deleting a group are the same mechanism (FR-5.13) — there is no separate per-row delete control; deleting one activity means checking just its own box first.

**Preconditions**: Active session; the caller owns every checked activity.

**Inputs**: The checked group (FR-5.6), reached via the header toolbar's Delete icon.

**Behavior**:
1. Clicking the toolbar's Delete icon opens a confirmation dialog naming how many activities are checked and their combined distance, stating plainly that this can't be undone; nothing is deleted until the user confirms.
2. Confirming removes every checked activity and everything derived from each one: its recorded stream data and its rendered coverage masks.
3. The Fog-of-War/Heatmap view updates to reflect the deletion — coverage a deleted activity was the only source for reverts to unrevealed, not left showing stale coverage for data that no longer exists.
4. Canceling the confirmation, or dismissing it, leaves every checked activity untouched.

**Outputs**: Every deleted activity, and everything derived from it, no longer exists; every list, filter, total, and aggregate that previously included it reflects the removal, in one combined refresh rather than once per deleted activity.

**Error cases**:
- A checked activity does not exist or belongs to another account → `404 Not Found`, indistinguishable from each other.

### FR-5.12 Group visible

**Description**: An icon-only header toolbar button toggles whether every currently checked (FR-5.6) activity is drawn on the map, in bulk — this is FR-5.8's entire hide/show mechanism, applied to whatever is checked, one activity or many.

**Preconditions**: At least one activity is checked; the button is disabled otherwise.

**Behavior**: If any checked activity is currently hidden, clicking shows the entire checked group (removes all of them from the hidden set). If every checked activity is already visible, clicking hides the entire group instead.

**Outputs**: The hidden-activity set updates; the map's drawn tracks and Fog-of-War/Heatmap coverage reflect it immediately.

### FR-5.13 Delete group

**Description**: The header toolbar's Delete icon — see FR-5.11, which this number and FR-5.11 both describe: deleting one activity and deleting several are the same mechanism, not two.

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