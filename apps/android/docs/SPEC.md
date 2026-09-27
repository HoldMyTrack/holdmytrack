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

**In scope**: everything currently built and verified on a physical device — sign in, sign up, and starting a demo account (FR-1); an unconfirmed email held at a "check your email" screen (FR-1.4, verified on an emulator); session persistence and server-side re-verification on cold start (FR-1); a full-screen map with Normal, Fog of War, and Heatmap modes, Normal's tracks narrowed by a date-range slider (FR-2), and the web's phone Activities panel over it — the range's activities listed, filtered by type and distance, selected — with its pace bands — hidden, edited (its track too) and deleted, with the account's import history and Private locations as its other two tabs (FR-2.7, verified on an emulator); Health Connect onboarding and permission acquisition (FR-3); a foreground sync run with resumable watermark tracking (FR-3); per-activity sync rejection feedback (FR-3); and a sync status/history screen that also surfaces cross-source duplicates (FR-4). Also in scope, verified on an emulator rather than a physical device (noted where it matters — §7): casual in-app GPS recording started from a button on the map, with a live track on the map and notification controls, its recordings listed for review and sync on the Sync Source screen, with a route preview per row, a searchable activity-type picker in Edit and GPX download (FR-5).

**Out of scope, not yet built**: everything root `docs/ROADMAP.md` Phase 3 (design finalization) and `apps/android/docs/ROADMAP.md` Phase 5 gate — finished screen designs. Out of scope as a platform limitation rather than a "not yet": background Health Connect sync (will not be built — see §3.3 note; this does not apply to in-app recording, which is not subject to the same platform constraint — [ADR-0007](../../../docs/adr/0007-in-app-gps-recording-submits-directly.md)), and Samsung Galaxy Watch sync (Samsung does not expose route geometry to Health Connect at all, so every Samsung-sourced session is rejected for having no route, same as any other route-less activity). iOS does not exist. A spoken TalkBack pass on a real device hasn't been done (§9). See §9 for the complete list.

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

**Description**: From a single screen (`SignInActivity`) with the web's two pages as two modes — sign in (`/signin`) and create an account (`/signup`) — an anonymous visitor signs in to an existing account, creates a new one, or starts a no-signup demo — the same three operations `docs/SPEC.md` FR-1.1/FR-1.2/FR-2.1 define server-side — or continues with Google or Facebook, when the server has them configured (`docs/SPEC.md` FR-1.9, FR-1.10).

**Preconditions**: No active session.

**Inputs**: For Google, an account picked in the system's Google account picker; for Facebook, a Facebook login in a browser tab. For sign-in/sign-up, an email address and password, checked client-side only for "both fields non-empty" — every other validation (address format, password length, whether the account exists) is left to the server, and the server's own response text is shown verbatim rather than reworded, so the two can never disagree about what is acceptable. Starting a demo takes no input.

**Behavior**:
1. The screen opens in sign-in mode: the providers, email, password, "Forgot password?", Sign in, "Don't have an account? Create one", and Try the demo. That link switches to sign-up mode: titled "Create your account", with the providers, email, password, Create account, and "Already have an account? Sign in" — no demo and no "Forgot password?", as on the web. The typed email carries over a switch. Back in sign-up mode returns to sign-in mode; in sign-in mode it leaves the app.
2. Sign in calls `POST /v1/auth/login`; Create account calls `POST /v1/auth/signup`; demo calls `POST /v1/auth/demo` — the same three endpoints the web client uses.
3. "Forgot password?" opens the web's own "Forgot password?" page (`docs/SPEC.md` FR-1.5) in a browser tab.
4. On success, the response's `session_token` field (present on all four session-minting endpoints, specifically for a native caller that has no cookie jar to rely on) is stored via `Session.start` with the response's `email` and `email_verified`, and the map replaces this screen — or, for an account whose email isn't confirmed yet, the "check your email" screen (FR-1.4).
5. On failure, the server's own error message is shown inline; the action buttons are disabled while a request is in flight and re-enabled after.
6. "Continue with Google" and "Continue with Facebook" appear above the email field only when `GET /v1/auth/providers` reports each configured (Google also needs its `google_client_id`); if that request fails, neither shows.
7. Google: the system's Google account picker (Credential Manager) returns an ID token, which is posted to `POST /v1/auth/google/token` with the device's timezone, and the answer is handled as in step 4. Closing the picker does nothing; any other failure shows "Couldn't sign in with Google. Please try again."
8. Facebook: the server's own Facebook sign-in opens in a browser tab (not Meta's SDK), started with the S256 hash of a random verifier the app keeps. The tab closes when the server redirects to `holdmytrack://oauth`: with a code, the app redeems it and the verifier at `POST /v1/auth/handoff`, then continues as in step 4; with an error code, it shows the web's message for it (no email shared, email already has an account, or a generic failure). Closing the tab returns to this screen with nothing shown.

**Outputs**: A stored bearer token and the account's email (empty for a demo account, which the server deliberately never returns a real email for).

**Notes**: This is the app's first screen whenever no session is held — on launch, after sign-out (FR-1.3), and after a stored token is rejected (FR-1.2) — and nothing sits behind it: Back leaves the app (from sign-in mode; from sign-up mode it returns to sign-in). The map is never shown signed out, since a basemap with none of the account's layers is a weak demonstration of the product next to the Demo button here.

### FR-1.2 Session persistence and re-verification

**Description**: A session survives the app being closed and relaunched, but a restored token is not trusted until the server confirms it — the same caution `docs/SPEC.md` FR-1.4 describes for the web client, applied to a store that isn't a cookie jar.

**Behavior**:
1. On cold start, if a token is stored but not yet marked verified in this process, the map defers attaching any signed-in-only layer and calls `GET /v1/auth/me`; if the answer takes more than 600ms, a notice under the map's chrome reads "Checking your session…".
2. A `200` marks the token verified for the rest of the process's lifetime and records the response's `email_verified`; the map then proceeds as signed in, or, if the email isn't confirmed, is replaced by the "check your email" screen (FR-1.4).
3. A `401` — and only a `401` — clears the stored token and replaces the map with the sign-in screen (FR-1.1). Any other failure (no network, an unreachable dev stack) leaves the token in place and is retried on the next resume, since it says nothing about whether the credential itself is still good; meanwhile the notice says "Couldn't reach HoldMyTrack, so your tracks aren't on the map yet." with Try again.

**Outputs**: Either a confirmed, attached session, or a clean return to the sign-in screen — never a map that silently 401s on every tile request while looking merely blank.

### FR-1.3 Sign out

**Description**: Ends the current session, both on the device and on the server.

**Behavior**: `POST /v1/auth/logout` deletes the session row server-side; the stored token and email are cleared locally regardless of whether the request succeeded (a token that can't reach the server to be revoked is not one worth keeping either way). The app then shows the sign-in screen (FR-1.1) with every other screen cleared from the back stack.

### FR-1.4 Email verification

**Description**: An account whose email isn't confirmed yet — a new email-and-password account when the server sends verification emails, or a new Sign in with Facebook account (`docs/SPEC.md` FR-1.8, FR-1.10) — sees a "check your email" screen (`VerifyEmailActivity`) instead of the map, the app's counterpart of the web's `/verify-pending`. The server refuses such an account every tile and sync request (`403 email_not_verified`), so the map would otherwise open empty with nothing saying why.

**Preconditions**: A session whose `email_verified` is false, from the sign-in response (FR-1.1) or from `GET /v1/auth/me` (FR-1.2). A demo account is always verified.

**Behavior**:
1. The screen says a confirmation link was sent to the account's address, and offers Continue, "Resend verification email", and Sign out. It is the root of its own task: Back leaves the app.
2. On every resume — coming back from the mail app or the browser included — the screen asks `GET /v1/auth/me` again, silently. Once it reports `email_verified: true`, the map replaces the screen.
3. Continue asks the same question; if the email still isn't confirmed it says so ("Your email isn't confirmed yet…"), and if the server can't be reached it says that.
4. Resend calls `POST /v1/auth/resend-verification` and shows the server's reply verbatim — its confirmation, or its refusal once the 5-per-hour limit is reached.
5. A `401` from `GET /v1/auth/me` clears the session and shows the sign-in screen, as on the map (FR-1.2). Sign out behaves as FR-1.3.
6. The Sync screen, which Health Connect can open without the map, shows "Confirm your email address first…" and hides every sync control and the history for such an account.

**Notes**: Changing a mistyped address (`docs/SPEC.md` FR-1.8 step 4) is web-only: the app keys its local recordings and sync watermark by email (FR-5.3), which a change would orphan. Verified on an emulator rather than a physical device.

### FR-1.5 Settings

**Description**: The account's Settings — Avatar, Name, Country, Timezone and Language, the same five the web's page edits (`docs/SPEC.md` FR-1.7) — on a Settings screen reached from the map's menu, and shown in place of the map on a new account's first run.

**Preconditions**: A session whose email is confirmed (FR-1.4), or a demo account.

**Behavior**:
1. The screen loads the account (`GET /v1/auth/me`) and the Country, Timezone and Language lists (`GET /v1/account/settings/options`, the server's own, named in the app's language) together; nothing is editable until both are in.
2. Avatar: "Choose an image" opens Android's photo picker (no storage permission); the picked image is shrunk to at most 512 px on its long side and uploaded as JPEG straight away (`POST /v1/account/avatar`), showing "Avatar updated."; Remove deletes it (`DELETE /v1/account/avatar`). Profile shows the avatar and the Name too.
3. Country and Timezone each open a searchable list; Language is a drop-down of Automatic (phone), English and Русский. Save sends Name, Country, Timezone and Language together (`PATCH /v1/account/settings`) and shows "Saved."; a refusal shows the server's own message.
4. **Country sets the app's units**: miles, feet and mph for the United States, Liberia and Myanmar, kilometres, metres and km/h everywhere else — the recording stats and notification, Sync Source's recordings and sync history — as on the web. Metric until a Country is saved.
5. **Language sets the app's language** (its per-app language, the setting Android's own "App languages" screen writes): saving English or Русский switches the app at once, and saving Automatic returns it to the phone's language. A sign-in, or a session check, applies the account's language when it is English or Русский, and leaves the app alone when it is Automatic.
6. **First run**: a real, confirmed account with no Country — every new account — opens on Settings instead of the map, titled "Welcome — set up your account", with the web's short explanation, and "Save and continue" takes it on to the map. There is no way past it but saving. A new email-and-password account starts on the phone's timezone.
7. A demo account sees every field and button disabled, with a note that the shared demo account can't be changed.

**Error cases**: The server's own wording, in the error box — a missing Country, an image it won't take.

## 4. FR-2 — Map Visualization

### FR-2.1 Base map

**Description**: A full-screen map renders on launch for a signed-in account; with no session, the sign-in screen (FR-1.1) is shown instead and the map is not created at all.

**Behavior**: The style document is fetched unauthenticated from `GET /v1/map/style/{flavor}`, `flavor` chosen from the system's day/night setting (`light` or `dark` — two of the five the API serves; the app does not offer a way to pick the other three or to override the system setting, per `apps/android/docs/ARCHITECTURE.md` §2.1). The camera opens on a whole-world view (equator, zoom 1) until an account's own activity extent is known (FR-2.3). A style load failure is reported on screen, not only in logcat: a notice under the chrome reads "The map couldn't load.", with the API origin and MapLibre's error in small print and Try again, which loads the style afresh.

### FR-2.2 Three map modes: Normal, Fog of War, Heatmap

**Description**: The same three mutually exclusive views `docs/SPEC.md` FR-4.1–FR-4.4 define, switched by a toggle at the map's top-left, in one row beside the menu button, with a divider between Normal and the two coverage views (Normal | Fog, Heatmap), as on the web. Only visible/available once signed in.

**Behavior**:
1. **Normal** draws the account's tracks as a single-color vector line layer (`GET /tiles/v1/tracks/{z}/{x}/{y}.mvt`), from zoom 4 inward, the same as the web (`docs/SPEC.md` FR-4.1).
2. **Fog** replaces the tracks with the server-rendered dark-veil raster (`GET /tiles/v1/fog/{z}/{x}/{y}.png`); tracks are hidden. Map labels (place names, street names, points of interest) stay drawn on top of the veil but are dimmed, so they remain readable without competing with the cleared ground — as on the web (`docs/SPEC.md` FR-4.2); they return to full strength in Normal and Heatmap, and while recording.
3. **Heatmap** replaces the tracks with the server-rendered intensity raster (`GET /tiles/v1/heatmap/{z}/{x}/{y}.png`); tracks are hidden.
4. All three layers sit beneath the basemap's first label layer, so place names stay legible; within that, the active raster (fog or heatmap) is drawn beneath the tracks layer so a cleared route reads as visible through the fog rather than obscured by it — the same ordering the web client uses.
5. Exactly one of the three is active at a time; tapping the active one leaves it active. Its button is filled (dark ink, white text); the other two are plain text on the toggle's light panel.
6. While a GPS recording is in progress (FR-5.1) the toggle is hidden and none of the three modes' layers are drawn — the map shows only that recording. The previously selected mode returns when the recording stops.

**Notes — filtering**: Normal's tracks are narrowed to the selected date range (FR-2.6), sent as the tracks tile's `from`/`to`. Fog and Heatmap are never filtered, as on the web (`docs/SPEC.md` FR-4.2, FR-4.3). Within the range, the Activities panel's TYPE and DISTANCE filters, its hidden tracks and Pending rows leave tracks off the map, on the device rather than in the tile request, as on the web (FR-2.7).

**Notes — source of the drawn data**: The map is a pure read against these three server endpoints; it never reads Health Connect directly, at any mode, and Health Connect sync (FR-3) never touches the map. An activity appears here only once FR-3's sync run has posted it to the server and the server has ingested it — there is no direct, on-device path from a Health Connect record to a drawn track (`apps/android/docs/ARCHITECTURE.md` §1).

### FR-2.3 Initial camera framing

**Description**: On first attaching the user layers each app session, the camera flies to fit the account's most recent activity, once — the web's opening view (`docs/SPEC.md` FR-4.5), not the whole history, which for an account with scattered recent history is a near-world view that reads as broken.

**Behavior**: `GET /v1/activities` is read once per session start, and the most recently started row's `bbox` is taken (rows with no `bbox` — no recorded trajectory — and Pending rows are skipped). The camera animates to fit that box, capped at zoom 15 so a single very short activity, or one heavily clipped by a Private location, doesn't zoom in on an empty rectangle past the basemap's own z14 data. An account with no geometry at all stays at the whole-world view, with a notice — "Nothing on your map yet…" and a Sync action — that is asked again on every return to the map and goes, and the camera frames the new history, once something has arrived. A demo account never gets it. Re-attaching the session (e.g., returning from the sign-in screen without actually changing account) does not re-fly the camera a second time in the same app session.

### FR-2.4 Attribution

**Description**: MapLibre Native's own attribution control is left enabled and renders the ODbL credit the style document's basemap source carries — required, not decorative, since the Protomaps basemap is a Produced Work under that license (`docs/IMPLEMENTATION.md` §5.6).

### FR-2.5 Find my location

**Description**: A button near the right end of the map's top row (the row with the menu button and the mode toggle), just before the record button (FR-5.1), that shows the user's position and moves the camera to it — the Android counterpart of the web map's geolocate control.

**Behavior**:
1. The first tap asks for location permission (the system dialog offers Precise or Approximate; either is enough). If it's refused, a message says location access is needed to show where you are, and nothing else happens.
2. With permission, the map shows a position dot and flies to it, at zoom 14 or closer if the map is already zoomed in further. The camera then follows the position until the user pans the map; tapping again re-centres.
3. Nothing is located before the first tap.
4. While a GPS recording is in progress (FR-5.1) the button is hidden; the recording already keeps the camera on its latest fix.

### FR-2.6 Date range

**Description**: A footer along the bottom of the map in Normal mode selects the date range whose tracks are drawn — the web's phone footer (`docs/SPEC.md` §17 item 2) and the same design: a two-knob slider with Earlier/Later buttons either side and the selected start and end dates under it.

**Behavior**:
1. Shown in Normal mode only, once the session is verified and the account has at least one activity day. Hidden in Fog and Heatmap (which ignore the range, as on the web) and while recording. A full-width bar along the bottom edge, as on the phone web, with the Activities panel (FR-2.7) sitting on it; MapLibre's logo and attribution move up above the collapsed panel while they're shown.
2. The range defaults to the 5 most recent days that have an activity, through today (`docs/SPEC.md` FR-6.1). Until the user picks a range themselves, the default is re-derived every time the map is returned to, so a sync or a recording that lands moves it onto what arrived.
3. The slider behaves as the web phone slider does: it counts only days with at least one activity; the track shows a window of 15 of them, the most recent 15 on load; the knobs mark day boundaries, so a one-day selection has its knobs one activity day apart and they can never be closer; pressing the track moves whichever knob is nearer and dragging carries it; Earlier/Later move the window 5 activity days per tap, repeating while held, and stop at the first and most recent activity day; a knob on the edge the window moves toward is pulled along, which is how a selection longer than the window is made. The dates under the track read like the web's ("12 MAR 2026"), in the app's language.
4. The range is applied when a knob or a held button is released: Normal's tracks are redrawn for it, the Activities panel lists its activities, and the camera flies to fit the range's drawn activities (`docs/SPEC.md` FR-6.6). Only a range the user picked moves the camera and clears the panel's selection, checked group, hidden tracks and filters — not the default re-deriving itself; and the camera never moves while recording.
5. A picked range survives rotation.

### FR-2.7 Activities panel

**Description**: The web's Activities panel as it is at phone width (`docs/SPEC.md` FR-5, §17 item 1), in the same layout and with the same rules: a sheet over the date-range footer (FR-2.6) listing the range's activities, with the Type and Distance filters, a checkbox group, a toolbar over the group or the selected row, and a tap on a row or a track selecting it. Normal mode only, and not while recording.

**Behavior**:
1. **Collapsed and expanded.** Collapsed, the sheet shows its tab row — **Activities** in the serif with the listed rows' count in a badge, **Sync** (FR-4.1) with, while any import is still processing, how many in an accent badge, and **Privacy** (item 14); the selected tab at full strength and underlined in the accent, the other dimmed — and a line under it with a chevron: "{distance} loaded" on Activities, the whole range's total however the filters narrow the rows, "Everything imported, from the phone or the web" on Sync, and "Tracks never start or end inside these circles." on Privacy. Items 2–13 are the Activities tab. Tapping that line expands the sheet upward to 78% of the screen less the footer, and tapping it again collapses it; there is no drag. The list keeps its scroll position while collapsed. MapLibre's logo and attribution stay above the collapsed sheet whether or not it's expanded.
2. **Rows.** Newest first, in the server's order, every one in the range (no paging). A row's title is its name, or its start date and time ("Sep 24, 2026, 4:00 AM") when it has none; under it, "[date · ] distance · duration · type" — the date only when a name took the title, distances to one decimal ("10.4 km", miles for the imperial countries, FR-1.5), durations as "1h 2m" / "42m" / "30s", and types by their display name ("Walking"), else title-cased from the raw value. A Pending row (a reprocess still running) is dimmed with a **Pending** badge, its text disabled, and its checkbox disabled unless already checked; a hidden row is dimmed with a **Hidden** badge. With nothing to list, the list says "No activities match the current filters." — "Loading…" while reading, and an error when the read fails.
3. **Type filter.** The **Type** button opens a dropdown under it: **All types**, then every type among the activities the Distance band lets through, busiest first, with its count; checked means shown. Unticking a type drops its rows from the list and its tracks from the map, and puts a dot on the Type button; **All types** re-includes every type. A tap outside closes it. With no activities it says "No activities to filter yet."
4. **Distance filter.** Beside Type: DISTANCE, a two-knob slider between the range's shortest and longest activity (both shown under its ends), and a readout — "any distance" until a knob is moved, then "5.7 – 10.4 km". Activities with no distance never pass a moved band. Hidden when the range has fewer than two distinct distances. **Reset filters** appears under it while either filter is on, and clears both.
5. **Selection.** Tapping a row's text, or a track on the map (within 14dp of it), selects that activity: its row is tinted with an accent bar down its left edge, its track is drawn wider over a dark halo, and the camera flies to fit it in the map left showing between the chrome row and the sheet. At most one is selected; selecting another replaces it. A track tapped on the map scrolls its row to the middle of the list without expanding a collapsed sheet. Tapping empty map, or the list's empty space, clears the selection.
6. **Checkbox group.** A row's checkbox only adds it to or removes it from the checked group: no highlight, no fly, and the selection is untouched. The toolbar's master checkbox is checked when every listed row is, partly checked when some are, and a tap on it unchecks all when any are checked, else checks every listed row; **Invert** checks the unchecked listed rows and unchecks the checked ones. Neither reaches rows the filters hide.
7. **Toolbar.** Acts on its *target*: the checked group when anything is checked, else the selected row, else nothing (every action disabled, "Select an activity, or check some, first"). Each action's label names the target ("3 checked activities", or the selected row's title in quotes). **Show/Hide** (the eye) hides every target track from the map if none is hidden, else shows them all, for this session only; **Edit** and **Delete** (items 10 and 11); **Focus on map** flies to fit the target's visible tracks. The footer under the list reads "{n} selected · {distance}" for the target. For the demo account Edit and Delete stay visible but disabled, saying they aren't available for demo accounts; Delete is also disabled while any of the target is Pending.
8. **Duplicates.** When cross-source deduplication has set any activities aside (`docs/SPEC.md` FR-3.7), "N duplicates found" appears at the foot of the sheet; tapping it lists each one — "{start} · {type} · {distance}" over "From {source} — replaced by the copy from {source}." — and tapping again closes it. When the read fails it says so instead; with none it's absent.
9. **Freshness.** The list is read for each new range and again on every return to the map; a refreshed list keeps whatever of the selection, the checked group and the hidden tracks still exists. While any row is Pending the list is read again every 2 seconds, a failed read included; a row that starts or stops being Pending has the tracks fetched again and Fog and Heatmap refreshed once the server's re-render is done (`GET /v1/coverage/status`), and one that stops also refreshes the date-range footer's days.
10. **Edit** (`docs/SPEC.md` FR-5.10) opens a window over the top of the map, under the chrome row: "Edit activity" and the activity's title, or "Edit N activities"; **Type** — the recording form's searchable picker (FR-5.2 step 4), listing the range's types with their counts, with an "Add …" row for a new one — **Name** and **Description**, then Cancel and Save. Several activities edit Type only: Name and Description are disabled, with a note saying they're per-activity. Save checks the server's limits first (a type is required and at most 50 characters, a name 200, a description 2000), writes only what changed — one activity at a time, a group's keeping each its own name and description — and closes, and the row shows the new values; a refusal keeps the window open with the reason. Cancel or Back closes without writing. While it's open the panel is held down to its collapsed strip and, with the footer, takes no touches, the mode toggle is hidden, and a tap on the map selects nothing. Its **Activity** and **Track** tabs share the one Save and Cancel (item 13).
11. **Delete** (`docs/SPEC.md` FR-5.11) asks first — "Delete this activity?" or "Delete this group?", naming the target and its distance and what goes with it, which can't be undone — then deletes one at a time with the confirm reading "Deleting…". Afterwards the list, the tracks, the footer's days and duplicates are read again, and Fog and Heatmap refresh once re-rendered. A failure keeps the dialog open with the server's reason and a retry; whatever was already deleted is reported as gone.
12. **The selected activity's pace bands** (`docs/SPEC.md` FR-4.8). While an activity is selected, its track on the map is drawn in five coloured bands from blue (slowest) to red (fastest), relative to the activity's own range, wider than and over its halo — not while it's hidden, filtered out or Pending. There is no card, toggle or readout: the pace/heart-rate and elevation profile card (`docs/SPEC.md` FR-4.9) was removed on 2026-09-27 (ADR-0017). The bands are read again once a Pending activity's reprocess lands.
13. **Editing a track** (`docs/SPEC.md` FR-5.14), the Edit window's **Track** tab: for exactly one activity with a finished track — otherwise the tab is dimmed and says why ("Check exactly one activity to edit its track", "This track is still being processed", "No track recorded for this activity"). Opening it the first time hides every other track and the bands, flies to the activity in the map left below the window, and loads its recorded points ("Loading points…", or the server's reason they can't be edited). It shows RANGE with the knobs' distances and clock times, a two-knob slider over the points (by point, not distance), the start, the point count and the total under it, then **Chop** (keep only the stretch between the knobs), **Cut** (take out what's between the knobs and join their two points; holding it previews that), **Delete point** (a toggle: while on, "Tap a point on the map to delete it." and each tap on a point removes it, as long as two remain), **Undo** (the last step) and, once anything differs from the recorded track, **Reset** (back to the recording — itself a step Undo takes back). On the map the kept stretch is in the track colour, what would go dashed in red, every point a small ring (faded red if it would go) and the knobs as dots. Chop, Cut, Reset and Undo put the knobs back at the ends. A dot on the tab marks unsaved changes. Save sends the fields first, if they changed, then the whole track edit; the row turns Pending until the reprocess lands — or, since that's often quicker than the next read, counts as landed the first time a list shows it not Pending — and then the tracks, the bands, the profile, the days and Fog/Heatmap refresh. A refusal (a reprocess already running, "Could not save the activity") keeps the window open. Cancel restores the other tracks and sends nothing.
14. **The Privacy tab** (`docs/SPEC.md` FR-8.1): the account's Private locations — circles whose contents are clipped off the ends of every track — listed as rows ("Home", "Radius · 570 m", feet for the imperial countries, "Unnamed" without a name) and drawn on the map while the tab shows: a light purple fill and outline with a dot at the centre, the one being edited stronger. "None yet." with none, "Loading…" and the read's error otherwise, and a footer note that saving reprocesses the activities a change touches. **Create** places a 200 m circle at the map's centre — first flying to zoom 14 when the map is out past zoom 12 — and opens the editor over the top of the map, the circle brought into the map left showing under it and the sheet collapsed. Tapping a row (which flies to it, zoom 14 or closer) or a circle on the map opens it. The editor has **Name** (up to 100 characters, "e.g. Home") and **Radius** (a slider from 50 m to 2000 m in 10 m steps with its readout), a note to drag the centre and not to centre it on one's door, and **Cancel** and **Save**, Save enabled only when something changed; the circle is moved by pressing its centre handle and dragging, the map staying put meanwhile. Saving writes the whole circle and closes; a refusal (a 21st location, out of range) keeps it open with the server's reason. The trash on a row asks "Delete this private location?" — its activities show those parts again once reprocessed — and deletes it. After either, the list is read again, and once the server has reprocessed, the tracks, the list, the days, the selected activity's profile and Fog/Heatmap too. Leaving the tab or Normal mode takes the circles off the map and throws an unsaved draft away. The demo account sees the list and the circles — no Create, no trash, no editor; a tap only highlights a circle. While the tab shows, a tap on the map goes to the circles, not the tracks.

**Notes**: Edit (one activity, then a group of two set to one type, each keeping its own name), Delete, and a row going Pending and back (set directly in the dev database) were verified on an emulator, and so was the profile card (since removed, ADR-0017) with an uploaded GPX carrying heart rate and elevation — both toggles, the readout under a drag, the map's bands — and with a pace-only activity; and track editing on that GPX: a knob dragged, Chop (121 points to 80), two points deleted on the map, one undone, Save (the server stored `keep` and one `drop`, the row came back at 5.5 km and the map redrew it, bands and profile included), and a Cancel that left everything as it was; and the Privacy tab: a circle created, dragged by its handle, renamed and widened to 570 m and saved (the server had it as drawn), reopened by a tap on the map, deleted from its row, and gone from the map on leaving the tab. Verified on an emulator in English and Russian against a local stack, with the web's phone layout side by side (Playwright at 412×915): collapsed and expanded, a row tap and a track tap each selecting and flying to their activity, an empty-map tap clearing it, the Type dropdown dropping a type from list and map, the Distance slider narrowing the list, two checked rows hidden with the partly-checked master checkbox, and the sheet and footer gone in Fog. The "loaded" total is summed from the listed range's activities, where the web reads it from `GET /v1/activities/summary`; the two agree for the same range.

## 5. FR-3 — Health Connect Sync (Path 2)

All of FR-3 requires an active session (demo or registered); Health Connect sync has no unauthenticated path, and syncing to a demo account works identically to a registered one.

### FR-3.1 Onboarding — the readiness state machine

**Description**: Before any sync can run, the user is walked through a sequence of states, each with exactly one resolving action, shown under the Health Connect checkbox on the Sync Source screen (`SyncActivity`, FR-3.5).

**Behavior**: On every resume, the app re-derives one of six states and renders accordingly (state names from `health.HealthConnect.Readiness`):

1. **Unavailable** — no Health Connect on the device. No action is offered; the sync feature is not usable here.
2. **Update required** — Health Connect is present but needs a Play update. The primary action opens Health Connect's own home screen.
3. **Needs exercise permission** — neither `READ_EXERCISE` nor `READ_HEALTH_DATA_HISTORY` is granted yet. The primary action opens the ordinary system permission dialog for both at once.
4. **Needs routes permission** — sessions are readable, routes are not. `READ_EXERCISE_ROUTES` cannot be requested programmatically at all (a platform limitation, confirmed on-device — requesting it alongside the others silently grants the others and omits it). The screen names the exact path instead: *Health Connect → HoldMyTrack → Additional access → Access exercise routes → Always allow*, and the primary action opens Health Connect's home screen as the closest a third-party app can get the user there.
5. **Needs history permission** — sessions and routes are both readable, but Health Connect will only serve the last 30 days without `READ_HEALTH_DATA_HISTORY`. Unlike the routes permission, this one *is* requestable, so the primary action opens the ordinary system dialog for it. This state is not blocking: the Health Connect checkbox stays enabled and Sync now (FR-3.5) syncs the last 30 days, since declining history access is a legitimate answer and syncing a shallower window is still useful.
6. **Ready** — every permission granted; there is no setup action left, and Sync now (FR-3.5) starts a sync run (FR-3.2) while the Health Connect checkbox is ticked.

**Notes**: This same screen is registered for Health Connect's `ACTION_SHOW_PERMISSIONS_RATIONALE` intent, so when Health Connect itself asks the user why HoldMyTrack wants their data, it opens this screen — meaning the rationale a user sees inside Health Connect and the explanation they see inside HoldMyTrack are the same text, by construction rather than by two authors staying in sync. Readiness is re-read on every resume rather than cached, since the routes permission specifically can only change in another app's settings screen, and returning from it is the only moment this screen can observe that.

### FR-3.2 Foreground sync run

**Description**: Reads exercise sessions from Health Connect in ascending order, sends the ones with usable route geometry to the server, and reports what happened — entirely while the sync screen is on screen.

**Preconditions**: Readiness is `READY` or needs-history-permission, and the Health Connect checkbox on Sync Source is ticked when the user taps Sync now (FR-3.5).

**Behavior**:
1. Sessions are read paged (50 per Health Connect request), starting from the account's watermark (§5.3) or from the beginning if never synced.
2. Each session is classified: a route present and readable is prepared for sending; a session with confirmed no route (`ExerciseRouteResult.NoData`) is counted as skipped, not an error — this is the ordinary case for an indoor workout, a gym session, a swim, or a rowing machine, none of which have a trajectory to begin with; a session whose route exists but could not be read right now (`ExerciseRouteResult.ConsentRequired`) stops the run entirely (§5.3).
3. Sessions with a route are batched — up to 25 activities or 20,000 points per batch, whichever comes first — and posted to `POST /v1/sync/activities` with `source: "healthconnect"`. Each activity's `activity_type` is normalized from Health Connect's own vocabulary onto the one HoldMyTrack's other ingest paths already use (e.g., Health Connect's `biking` becomes `cycling`; `running_treadmill` becomes `running`), so the same physical ride synced from a watch and uploaded from a file describes itself the same way — required for both the Activities panel's TYPE filter and cross-source deduplication (`docs/SPEC.md` FR-5.2, `docs/IMPLEMENTATION.md` §4.6) to work correctly. Elevation is included per-point only when the location itself carries it; heart rate is never read or sent — HoldMyTrack keeps no health data (`docs/VISION.md` §1.1), so the app doesn't declare that Health Connect permission at all (§6, `docs/VISION.md` §7).
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

**Outputs**: A report on the Sync Source screen, under Sync now, in two parts: what the run did (synced, already present, skipped, the recorded queue) in a notice, and what it couldn't do — each refused session with its reason, why it stopped early, or the whole run failing — in a separate red error box, which stays after the status line above returns to "Ready to sync". This is the run just watched, not a browsable history — that is FR-4.

### FR-3.5 Sync Source — choosing what Sync now sends

**Description**: The burger menu's **Sync Source** item opens one screen (`SyncActivity`) that lists every place activities come from on this device, each with its own checkbox, and a single **Sync now** that sends whatever is checked.

**Behavior** (top to bottom):
1. The rationale text (FR-3.1 Notes), then — signed out, email unconfirmed, or the demo account — a notice saying why nothing below can sync. Signed out or unconfirmed, nothing else is shown.
2. **Recorded activities**: the recordings on this device that haven't synced yet, one row each with its own checkbox (FR-5.2).
3. **Health Connect**: a checkbox, ticked by default, then its readiness notice and whatever setup step remains (FR-3.1), with "Open Health Connect" alongside except where the setup step is already that button. The checkbox is enabled only while Health Connect can be read — Ready, or needs-history-permission — and shows unticked and disabled otherwise. Unticking it is remembered per signed-in account (`SyncSources`, keyed by email like the watermark, FR-3.3), and a disabled checkbox leaves the remembered choice alone, so finishing setup brings back whatever the user last picked.
4. **Sync now** is enabled while no run is in flight and something is checked — at least one recording, or an enabled, ticked Health Connect. It runs the Health Connect sync (FR-3.2) only when that box is ticked, then submits every checked recording (FR-5.2 step 6), and reports under itself (FR-3.4). While only recordings are going, the progress line reads "Syncing recorded activities…".
5. The sync history isn't on this screen: it's the map panel's **Sync** tab (FR-4.1).
6. The demo account sees its recordings listed with their checkboxes disabled; the Health Connect section and Sync now are hidden (the server refuses a demo account's sync, `requireNotDemo`).

**Notes**: Confirmed on an emulator: with Health Connect not yet permitted its checkbox was disabled and unticked under "Allow Health Connect access", and Sync now was disabled with no recording checked; after granting, the checkbox came up enabled and ticked; unticking it survived a force-stop; with one of two recordings checked and Health Connect unticked, Sync now submitted only the checked one (it reached the server as a `recorded` job and left the list), and the other stayed.

## 6. FR-4 — Sync Status & Duplicates

### FR-4.1 Sync history

**Description**: The map's Activities panel's **Sync** tab (FR-2.7, the web's Sync tab without its upload drop zone) lists every ingest job this account has ever produced, from any path — a Health Connect sync and a file upload from the web client appear in the same list, because server-side they are the same kind of job (`docs/IMPLEMENTATION.md` §4.0.1).

**Behavior**: Reads `GET /v1/uploads` (the same endpoint the web client's Sync tab uses) and shows it the way the web does (`docs/SPEC.md` FR-3.4): five rows a page, newest first. Each row is titled by the file's name for an uploaded file or a Takeout entry, otherwise by its source ("Health Connect", "GPS Logger"), and shows at its other end "Ready" with the activity's date and distance under it once finished, "Failed: " and the server's own error text in red when it failed, or "Processing…". A finished row that became an activity has "View on map", which switches the panel back to its Activities tab and collapses it, switches the map to Normal, and selects that activity as a row tap does (FR-2.7 item 5) — flown to, bold over its halo. When the activity's day, in the account's timezone, is outside the selected range, the range first becomes that one day, as if picked, and the selection follows once that day's list has loaded. Above the rows, a "History" heading carries the overall count and, while any job is still processing, how many are; past five rows a pager under them shows the range ("1–5 of 12") with previous and next buttons. An account with no jobs gets "Nothing imported yet." instead, and a failed read an error box. The tab is read whenever the map is shown and polls every 1.5 seconds only while at least one job is still processing — whichever tab is showing, since the Sync tab's badge counts them — and stops entirely once everything has settled, and while the map isn't on screen.

**Outputs**: A list of rows plus overall counts (total, still processing).

### FR-4.2 Duplicates

**Description**: The "N duplicates found" disclosure at the foot of the Activities tab (FR-2.7 item 8), where the web has it, answers a question specific to having more than one ingest path: an activity can be missing from the map because it failed, or because it was already present from a different source — and only the first is a fault.

**Behavior**: Reads `GET /v1/activities/duplicates` with each list (FR-2.7 item 8). Tapping "N duplicates found" lists each one as the web does: "{start} · {type} · {distance}" over "From {source} — replaced by the copy from {source}." (`docs/IMPLEMENTATION.md` §4.6); tapping again closes it. When the read fails it says "Duplicates — failed to load"; with none it's absent.

## 7. FR-5 — In-App GPS Recording

A third way an activity can originate on this app, alongside FR-3's Health Connect sync and a manual file upload done from the phone's browser (`docs/SPEC.md` FR-3.1). Architecturally distinct from FR-3: a finished recording submits directly to `POST /v1/sync/activities` under `source = "recorded"` rather than round-tripping through Health Connect ([ADR-0007](../../../docs/adr/0007-in-app-gps-recording-submits-directly.md)), so none of FR-3's foreground-only reasoning carries over — recording and syncing are two separate, explicit steps (below), not one continuous run. `docs/SPEC.md` FR-3.8 is the cross-platform behavior spec this restates at screen level; `docs/IMPLEMENTATION.md` §4.0.4 has the wire contract.

### FR-5.1 Record — one button on the map

**Description**: A round, translucent record button at the right end of the map's top row (`MainActivity`) — the bottom of the screen is the Activities panel's (FR-2.7) — that records a casual, GPS-only track — a walk, hike, or drive someone would not otherwise bother tracking — in one tap, asking nothing.

**Preconditions**: A session (the button is on the map, FR-2.1) and `ACCESS_FINE_LOCATION` granted to record. A demo session can record and manage rows locally; only syncing them is blocked (FR-5.2 step 8).

**Behavior**:
1. **Tap while idle starts recording.** The first time, it raises the system location dialog (Precise/Approximate × While using the app/Only this time/Don't allow) followed by the notification dialog, then starts once location is granted; notifications are not required. Denying location shows "HoldMyTrack needs location access to record a GPS track." and nothing starts. No name, type or description is asked, now or at any later point in recording. Confirmed on an emulator.
2. Recording runs in `RecordingService`, a declared foreground service (`FOREGROUND_SERVICE_TYPE_LOCATION`), so it survives the screen turning off and the app being swiped out of recents. Confirmed on an emulator: swiping the app away mid-recording and reopening it showed the full track so far, with recording still running.
3. **Tap while recording pauses; tap while paused resumes.** The button shows pause (ringed red) while recording, play (ringed amber) while paused, and a red dot while idle. A tap that pauses also shows a short "Hold to stop recording" toast, since a tap is the obvious guess at stopping and it only pauses; resuming shows nothing. No points are recorded while paused, so drift at a stop never adds distance — confirmed: a far-off fix sent while paused added no point. There is no automatic pause.
4. **Holding the button for two seconds while recording or paused stops.** A white ring fills clockwise around the button over the two seconds; when it closes, the phone gives haptic feedback and the recording stops. Letting go before then cancels — the ring clears and nothing happens (a release within the system long-press timeout still counts as a tap, pausing/resuming). Holding while idle does nothing. With TalkBack, the stop is offered as the button's long-press action.
5. **The map shows only the recording in progress** (FR-2.2 step 6): its line, in the track colour, and a dot at the latest fix, with the camera flying to street level (zoom 16) on the first fix and following each later one. Other activities and other map modes are not shown until the recording stops. Recordings that haven't synced never appear on the map after they stop — the map draws only what the server has (FR-2.2).
6. **A notification is shown for as long as a recording is in progress**: "Recording" with a running elapsed-time counter, or "Paused · h:mm:ss"; below that, distance · current speed · current altitude, updated on every GPS fix. It carries **Pause**/**Resume** and **Stop** buttons that act exactly like the map button, and tapping it opens the map. Stop from the notification also opens the app (to the map, then the Save screen, step 7). Confirmed on an emulator, including Stop from the notification (before Stop opened the app).
7. **Stop** saves the recording on the device, tagged not-synced, with no name or description and the activity type most recently set in Edit on this account (FR-5.2 step 3) — `"unknown"` for an account's first recording — then opens a **"Save recording"** screen on it: the Edit screen (FR-5.2 step 4) with that type pre-filled, and **Discard** in place of Download GPX. **Save** stores name, type and description (and remembers the type for the next recording); **Back** leaves the recording saved as it was; **Discard** asks "Discard this recording?" and deletes it from the device. No network request happens at this step. Not yet verified on a device or emulator.
8. **A recording shorter than one minute is not saved.** Moving time (pauses excluded) under 60 s — or fewer than 2 GPS points — is discarded on Stop with "Too short — not saved." Confirmed on an emulator: a ~20 s recording left no row.

### FR-5.2 Recorded activities — review, edit, check, sync

**Description**: The Recorded activities section of the Sync Source screen (FR-3.5, `RecordedActivityRows`) lists every recording on this device that hasn't synced yet; a row's checkbox is what marks it to go out with the next Sync now.

**Behavior**:
1. Rows are newest first, unfiltered, with "No recorded activities yet." in their place before any row exists. A row's checkbox is its stored sync status (checked = queued), written on every tap, so what's checked survives leaving the screen.
2. Each row reads `"{type} · {distance}"` (e.g. "walking · 0.23 km"), with the checkbox, a small preview of the route's shape (the recorded line alone in the map's track colour, no basemap, drawn from the points stored on the device), and Edit and Delete as icon buttons (a pencil and a red bin, labelled "Edit" and "Delete" for screen readers).
3. Any row can be queued whatever its type, including the `"unknown"` default — `docs/SPEC.md` FR-3.7's cross-source dedup matches on time overlap, not type, so an untyped recording still matches the same walk arriving typed through Health Connect. A type saved through Edit (step 4) also becomes the type the account's next recordings start with (FR-5.1 step 7), so after the first one a recording usually carries the right type without editing — confirmed: after setting one recording to walking, the next was saved as walking.
4. **Edit** (`RecordingActivity`, `EXTRA_RECORDING_ID`) opens an "Edit recording" screen for the row: Name, Type, Description (optional), the recording's time and distance, and **Save** and **Download GPX**. **Type** is a picker, not a text field — the same behavior as the web edit dialog's Type field (`docs/SPEC.md` FR-5.10): tapping it opens a searchable list of the account's existing types, most-used first with how many activities use each (its server-side activities plus this device's recordings), then any of walking/hiking/running/cycling/driving the account hasn't used yet. Typing filters the list (case- and accent-insensitive, an exact name first); while the text doesn't exactly match an existing type, a last **Add "…"** row saves it exactly as typed, up to 50 characters. Names are displayed title-cased with underscores as spaces ("dog_walk" → "Dog walk") and stored raw. Confirmed on an emulator: "dog walk" narrowed the list to "Dog walk 583"; "Solowheel" offered only `Add "Solowheel"`, which set the field and was saved with the recording.
5. **Download GPX** opens the system's save screen with a suggested file name (the recording's name, or `holdmytrack-{id prefix}.gpx`), and writes the track there as GPX 1.1 — name, description and type on the track, and every point's position, time and elevation where known — then confirms with "Track saved." (or "Could not save the track."). No storage permission is involved. Confirmed on an emulator: the saved file in Downloads carried the recorded points and `<type>Solowheel</type>`.
6. **Sync now** (FR-3.5) submits every checked row — `flushRecordedQueue()`, run after the Health Connect pass when that box is ticked (or after it throws), otherwise on its own, submitting each as its own `POST /v1/sync/activities` call. **A row that syncs is deleted from the device** and disappears from this list; the activity now lives on the server, on the map and in the activity list. A row that fails stays checked for the next Sync now. Confirmed on an emulator end to end: a queued walking recording became a `walking` activity server-side and the local row was gone.
7. **Delete** asks for confirmation — which says the recording hasn't synced, so this is the only copy — and removes the row from the device.
8. A demo session can record and manage rows here, but every checkbox is disabled under the screen's read-only notice — checking something Sync now can never actually take would be pointless (FR-3.5 step 6).

### FR-5.3 Local storage is scoped per signed-in account

**Description**: Recordings are keyed to whichever account is signed in when they're made (`Session.email`, empty string for a demo account) — the same per-account key `SyncCursor` (FR-3.3) already established.

**Behavior**: Switching accounts on one device never shows one account's recordings under another's. Confirmed live, incidentally: a recording made while signed out did not appear in Recorded Activities after signing into a real account, and so could not be queued or synced from that account either — consistent with `apps/android/docs/IMPLEMENTATION.md` §7.2's own account-scoping note, not a defect.

## 8. Non-Functional Requirements (summary)

This section summarizes cross-cutting behavior specified elsewhere in this document.

| Concern | Behavior |
| :-- | :-- |
| **Credential handling** | Bearer token, not a cookie — a session token is presented as `Authorization: Bearer <id>`, attached only to requests bound for HoldMyTrack's own API origin, never to third-party basemap-asset origins (FR-1.2). |
| **Session security** | Restored tokens are re-verified against the server before any signed-in layer is attached; sign-out revokes server-side (FR-1.2, FR-1.3). |
| **Foreground-only sync** | No background service or scheduled job ever attempts a Health Connect route read — a platform constraint, not a battery-saving choice (FR-3.2). |
| **Safe interruption** | A sync run cancelled at any point leaves no partial, unconfirmed state — the watermark only advances over confirmed-terminal records (FR-3.3). |
| **No reload required** | The panel's Sync tab updates itself by polling while work is outstanding, and stops polling once settled (FR-4.1). |
| **Idempotency** | Re-syncing the same Health Connect record never creates a duplicate activity — the server keys on the platform's own record id (FR-3.2, `docs/IMPLEMENTATION.md` §4.0.3). |
| **Language** | English or Russian, following the phone's language or the app's own per-app language setting; any other language gets English. The app sends its language as `Accept-Language`, so the server's messages match it (`docs/SPEC.md` FR-13.1). |

## 9. Known Limitations & Out-of-Scope Items

Named here rather than left implicit, the way `docs/SPEC.md` §18 does for the wider system:

- **No final visual design.** The palette, type scale, fonts and Lucide icons are the web's current ones carried over (Material 3) rather than a frozen design, and the screens are designed after their web counterparts until root `docs/ROADMAP.md` Phase 3's design freeze settles the final look (`apps/android/docs/ROADMAP.md` Phase 5). The app's own chrome is light only; only the map follows the system dark setting.
- **The Activities panel is the web's minus its uploads.** Every tab and action of the phone web's panel is ported (FR-2.7, FR-4.1), except the Sync tab's file upload: this app imports through Sync Source (FR-3.5). Where the web reads a value on hover — the profile's readout, Cut's preview — a touch and hold stands in.
- **Accessibility is audited, not yet listened to.** Every actionable view is labelled and at least 48dp, checked on every screen at the default and the largest font scale; screens stay usable at the largest scale (text wraps, the map's mode toggle scrolls). A spoken TalkBack pass on a real device hasn't been done.
- **Samsung Galaxy Watch is unsupported**, not degraded — Samsung does not expose route geometry to Health Connect at all, so every Samsung-sourced session is rejected for having no route, indistinguishable at sync time from an ordinary indoor workout.
- **Background sync will never be built for Health Connect routes** — a platform constraint (`ConsentRequired` regardless of grant state when backgrounded), not a sequencing gap.
- **In-app GPS recording's iOS half is unbuilt** — FR-5 above is Android-only; `docs/ROADMAP.md` Phase 2 tracks the iOS half as a combined item once an iOS app exists at all.
- **Few automated tests.** Only the Activities panel's filter and selection rules, the track band rules, the track editor's rules and the Private location circle (FR-2.7) have unit tests (`app/src/test`). Every behavior in this document has been verified manually against a live server stack — a physical device for FR-1 through FR-4, an emulator for FR-5 (§7, noted inline where it matters); see `apps/android/docs/IMPLEMENTATION.md` §9 for the verification record.
- **iOS does not exist.** Path 2's HealthKit half is unbuilt; this app defines the sync contract iOS will inherit (`apps/android/docs/ROADMAP.md`).
- **`poc-healthconnect/` is not part of the product** — a throwaway diagnostic app, retained only until Phase 1's findings are fully absorbed elsewhere.