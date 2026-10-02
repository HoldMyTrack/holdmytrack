# HoldMyTrack: Spec

| | |
| :-- | :-- |
| **Version** | 1.0 |
| **Status** | Current — describes Phase 0/1 functionality as built |
| **Last updated** | 2026-10-02 |
| **Related documents** | `VISION.md` (product scope, market rationale, phase roadmap — the authority on *what ships and why*); `ARCHITECTURE.md` (system-level shape, key decisions, the stack); `IMPLEMENTATION.md` (schema, each feature's own implementation — the authority on *how it's built*); `AGENTS.md` (repository orientation) |

## 1. Introduction

### 1.1 Purpose

This document specifies HoldMyTrack's functional behavior as currently implemented: what the system does, from the point of view of a user or of another system calling its API — not why it was built that way (`VISION.md`) or how it is implemented internally (`IMPLEMENTATION.md`). Each functional requirement (FR) is written to be independently testable: given the stated preconditions and inputs, the stated behavior and outputs should be observable.

### 1.2 Scope

**In scope**: every feature currently built and shipped, as of this document's last-updated date — authentication and account management (including account settings — avatar, name, country, and timezone, FR-1.7; email verification, FR-1.8; Sign in with Google and with Facebook on the web and in the Android app, FR-1.9 and FR-1.10), the no-signup demo (now read-only, seeded from a persistent, richly-populated Demo Customer account rather than a fresh per-visitor preset — FR-2.1), activity upload and ingestion (file upload, `.zip` bulk import, Google Takeout import, Android's Health Connect mobile sync — FR-3.6, and Android's in-app GPS recording — FR-3.8), cross-source duplicate detection (FR-3.7), map visualization (track rendering, Fog of War, Heatmap, pace-colored segments, high-resolution export), the Activities panel and its filters, track editing (FR-5.14), Private locations (FR-8.1), the date slider, the per-account activity graph, password recovery, distance/time trends (FR-9 below), the public About, Help and Contacts pages (FR-10), the Donate link out to Open Collective (FR-11), the read-only admin panel (FR-12), the interface language — English or Russian (FR-13), Stories on the web and in the Android app (FR-14), Spots' places on the web and in the Android app (FR-15), and photos on an activity, on the web (FR-16).

**Out of scope**: functionality named in `VISION.md`'s roadmap (§5.3 onward) but not yet built — Path 1 cloud-provider connectors (Garmin/Wahoo/COROS), Path 2 on-device sync's iOS/HealthKit half (no iOS app exists yet; Android's Health Connect half shipped — FR-3.6), the rest of "Export" (animated reveals — high-resolution map export itself is built, FR-4.10 below), and marking a Spots place visited (ADR-0021). Also deliberately out of scope, not a "not yet" — best-effort curves, personal bests, power curves, and training load were built and then cut: `VISION.md` §1.1 draws a hard line against HoldMyTrack being a health or fitness advisor, and pace stays as per-activity route context (FR-4.8) rather than an analysed, all-time performance record. So is any health data at all: heart rate is never read, stored or shown (ADR-0017). This document will be extended with new FR sections as in-scope functionality ships, not rewritten in place of them.

### 1.3 Intended audience

Engineers implementing against or modifying this system, QA deriving test cases, and anyone needing an authoritative answer to "what does the system do in this situation" without reading source code.

### 1.4 Definitions

| Term | Meaning |
| :-- | :-- |
| **Activity** | One recorded outing (a run, ride, hike, walk, drive, etc.) with a start time, and usually a GPS trajectory with elevation. Never any heart-rate or other health data. |
| **Track** | An activity's GPS trajectory, as rendered on the map. |
| **Session** | A signed-in browser's authentication state, held as an opaque cookie. |
| **Registered user** | An account with a real email and a password, a linked Google or Facebook identity, or any combination — created via sign-up (FR-1.1), Sign in with Google (FR-1.9) or Sign in with Facebook (FR-1.10). |
| **Demo user** | An ephemeral account created via "Try it now — no signup," functionally identical to a registered user except for its lifetime (FR-2.2 below). |
| **Fog of War** | A map mode that shows a dark veil over everywhere the signed-in user has *not* recorded an activity, so recorded routes appear as "cleared" ground. |
| **Heatmap** | A map mode that shades every recorded location by how many times it's been crossed, brightest where crossed most. |
| **Ingest** | The server-side process of turning an uploaded file into a persisted `Activity` — parsing, clipping against Private locations (FR-8.1), simplification, and storage. |

## 2. Actors

| Actor | Description |
| :-- | :-- |
| **Anonymous visitor** | Has not signed in and holds no session. Can reach the sign-in/sign-up screen and start a demo. Cannot see any activity data or use the map. |
| **Demo user** | Holds a session tied to an ephemeral account (FR-2). Full functional access to every feature a registered user has, except the account itself expires after 24 hours unless upgraded (FR-2.3). |
| **Registered user** | Holds a session tied to a permanent account (email + password, or Google — FR-1.9). Full functional access to every feature in this document. |
| **Admin** | A registered user whose account has been made an admin from the server's command line (FR-12.1). Everything a registered user has, plus the read-only admin panel (FR-12): every account, and any account's activities. |

There is no multi-tenancy beyond per-account data isolation. The admin panel is the one place an account sees another's data (FR-8.2, FR-12).

## 3. FR-1 — Authentication & Account Management

**On the web, every screen in this section is a server-rendered page** (`IMPLEMENTATION.md` §4.19) with the shared page header (FR-10.4), and every form on them is refused (`403`) unless its `Origin` — or, without one, its `Referer` — is the app's own origin:

| Page | Purpose |
| :-- | :-- |
| `GET`/`POST /signin` | Sign in (FR-1.2); links to sign-up and "Forgot password?", "Continue with Google" and "Continue with Facebook" when configured (FR-1.9, FR-1.10), and "Try it now — no signup" (`POST /demo`, FR-2.1). `?error=google` shows FR-1.9's failure message; `?error=facebook`, `facebook_no_email` and `facebook_email_in_use` show FR-1.10's. |
| `GET`/`POST /signup` | Sign up (FR-1.1, FR-2.3); `noindex`, since `/signin` links to it and is the one that should appear in search results |
| `GET`/`POST /forgot` | Request a reset link (FR-1.5); answers "Check your email" whatever the address |
| `GET`/`POST /reset?token=` | Set a new password from the emailed link (FR-1.6) |
| `GET /verify?token=` | The emailed verification link itself (FR-1.8) |
| `GET /verify-pending` | A signed-in, unverified account's holding page: resend (`POST /verify-pending/resend`), change the address (`POST /verify-pending/email`), or sign out (FR-1.8) |

A failed form comes back as the same page, at the failure's status (`400`, `401`, `409`, `429`), with the server's message and the typed email kept. A successful one redirects: to `/verify-pending` for a real account that hasn't verified its email, otherwise to the map (`/`). A visit to `/signin` or `/signup` with a real account already signed in redirects the same way; a demo session can still sign in, or sign up (FR-2.3). The JSON endpoints named below are what the Android app calls and what these pages share their behavior with. Links in emails sent before the pages existed pointed at `/?reset_token=` and `/?verify_token=`, and a failed Google sign-in at `/?auth_error=google`; `/` forwards those to `/reset`, `/verify` and `/signin?error=google`, before any session check.

### FR-1.1 Sign up

**Description**: An anonymous visitor creates a new registered account.

**Preconditions**: No active session, or an active demo session (see note below).

**Inputs**: Email address, password (minimum 8 characters); the browser's own IANA timezone, sent automatically (not user-entered) and optional — see step 3.

**Behavior**:
1. Client submits email + password + its own detected timezone to `POST /v1/auth/signup` (the web's `/signup` form fills the timezone from the browser with a one-line script; without it the account starts on UTC).
2. Server validates the email is a syntactically valid address and the password meets the minimum length.
3. Server hashes the password (bcrypt) and creates a new `users` row — always a fresh row, whether or not the caller's browser holds a live demo session (see FR-2.3, revised). The submitted timezone is validated against the IANA tz database and stored if valid; missing or invalid falls back to UTC rather than rejecting the signup (FR-1.7 covers editing it afterward).
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
- Email not found, account with no password set (never claimed, or Google- or Facebook-only — FR-1.9, FR-1.10), or password mismatch → `401 Unauthorized` with a single generic message ("invalid email or password") in every case — the system does not distinguish these to a caller, so it cannot be used to discover which emails are registered.

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
3. Otherwise the server returns `401 Unauthorized`, and the client shows the sign-in screen. On the web the pages check the session themselves: `/profile` and `/settings` redirect a visitor with no session to `/signin` (at `/` they get the front page, FR-10.1), and a signed-in real account whose email isn't verified to `/verify-pending`.

**Notes**: A session's validity is checked in the database on every request (not trusted from the cookie's own stated expiry), so a session ended server-side (FR-1.3, or invalidated by a password reset, FR-1.6) stops working immediately even if the browser still holds the cookie. Sessions last 30 days from creation. The response also reports whether the account's email is verified (always `true` for a demo account) — the client uses this to decide whether to show the map or FR-1.8's verify screen.

### FR-1.5 Forgot password

**Description**: A user who cannot sign in requests a password-reset link by email.

**Preconditions**: None (reachable with no session).

**Inputs**: Email address.

**Behavior**:
1. Client submits an email address to `POST /v1/auth/forgot-password`.
2. If that email matches a real, claimed account — one with a password, or a Google- or Facebook-only account (FR-1.9, FR-1.10), for which this is how a first password gets set — the server creates a reset token (valid 1 hour, single-use) and emails a link containing it to that address.
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

**Description**: A signed-in user (real or demo — FR-2) edits their own profile: Avatar, Name, Country, Timezone, and Language — and, on the same page, this browser's Theme (behavior 8). A page at `/settings`, reached from the header's account menu, separate from the activity graph (FR-7) — and, for a real account that has never saved it, shown automatically in place of the map (behavior 5). Private locations (FR-8.1) aren't edited here; they live on the map's Privacy tab.

**Name** is an optional display label, not an identifier: it isn't unique, two accounts may share one, and nothing signs in with it — the email address identifies an account. Nothing outside this page displays it yet.

**Preconditions**: An active session.

**Inputs**: An image file (PNG, JPEG, or WebP, up to 5 MB) for Avatar; free text for Name (optional); a country chosen from a list of ISO 3166-1 countries by name for Country (required — there is no "not set" choice); an IANA timezone for Timezone, chosen from the zones browsers know under IANA's current names (Kolkata, not Calcutta; Kyiv, not Kiev), grouped by region, sorted within each by its GMT offset today, west to east (then by place), and labelled with that offset first ("(GMT−04:00) - New York"), with UTC and the account's own saved zone always included. Under the field, "Local time: Sep 28, 14:05" shows the current date and time in the selected zone, updating as soon as another zone is picked. Country and Timezone are each a searchable list, like the Edit window's Type (FR-5.14) but closed — no "Add" row: opening one shows the whole list (Timezone under region headings) with a search field on top, and typing narrows it to every entry containing the text, accent- and case-insensitively — "york" finds New York, "-04" every zone four hours behind GMT, and a country's ISO code ("de") puts that country first. Without script they are plain drop-downs. A zone given under a former name — which browsers still report at signup — is stored under its current one. The web page has no Language field: the header's language menu sets the account's language (FR-13.2). The API takes it as `locale` — "" (automatic, the default), `en` or `ru` — which the Android app's Settings screen offers.

**Behavior**:
1. Avatar uploads and removals take effect immediately — each is its own action (`POST /settings/avatar`, `POST /settings/avatar/remove`; for an API client, `POST`/`DELETE /v1/account/avatar`), not gated behind a separate save step; choosing a file uploads it. The page, header included, shows the new avatar when it reloads after the upload.
2. Name, Country and Timezone save together as one action (`POST /settings`, which leaves the account's language as it is; for an API client, `PATCH /v1/account/settings`, which also takes Language as `locale`, optional and left unchanged when absent) — editing one and leaving without saving discards all of them, not just the one touched. A successful save reloads the page with "Saved."; a failed one shows the error with the submitted values kept.
3. **Country decides which unit system the entire app displays distance, pace, and elevation in** — metric (km, min/km, meters) for every country except the United States, Liberia, and Myanmar, which see imperial (mi, min/mi, feet). An account that has never saved a Country (only possible before its first save — behavior 6) displays metric. It applies everywhere one of these values is shown (the Activities panel, the activity graph, Trends, the per-activity pace/elevation profile, and the map's own distance scale) from the next time that page is opened after saving — Settings is a page of its own, so leaving it is a page load. Save can't be submitted until a Country is chosen.
4. **Timezone decides which calendar day an activity is grouped under everywhere the app buckets by day** — the date slider's days (FR-6), the activity graph's daily grid and stat cards, `GET /v1/activities/trends`, and date-range filtering. Auto-resolved from the browser at signup (FR-1.1) and editable here afterward; unlike Country, it has no "unset" state — every account always has one, defaulting to UTC until changed.
5. **First run.** A real, verified account with no Country — one that has never saved this page, which is every new account right after FR-1.8's verification link — is sent here from the map and Profile (and from sign-in), titled "Welcome — set up your account", with a short explanation of why Country and Timezone matter. There is no way past it: no back link, and the header's links to the map and Profile lead back here. Timezone is prefilled with the one auto-detected at signup, to confirm or change; Country starts on "Choose a country". "Save and continue" can't be submitted until a Country is chosen; saving goes on to the map, and every later visit goes directly to the map. Reloading before saving shows this page again. A demo account never sees it.
6. **A demo account** sees the page with every field and button disabled and a note that the shared demo account can't be changed, linking to "Create your own account" (FR-2.3); a save or avatar change submitted anyway is refused (`403`).
7. **A native client** reads the lists this page offers from `GET /v1/account/settings/options` — every Country (code and name in the request's language), every Timezone grouped by region (with the region's name in that language, and the account's own zone always included), and every Language — and saves through the API endpoints above. The Android app's Settings screen is built on it (`apps/android/docs/SPEC.md` FR-1.5).
8. **Theme** (FR-4.12): below Save, a System / Light / Dark toggle. It belongs to the browser, not the account — not part of Save, not disabled for a demo — and a click applies it at once.

**Outputs**: The account's current Avatar, Name, Country, Timezone, and Language (`locale` in `GET /v1/auth/me`, `""` for automatic), always reflecting the last successful save (or the account's defaults, if never changed) — reloading the app never reverts to something stale.

**Error cases**:
- An unsupported image type or a file over 5 MB → `415`/`413`, and the image is not saved.
- Country missing or outside the supported list, Timezone not a valid IANA zone name, or Language not one FR-13 supports → `400 Bad Request`, and none of the fields in that save are applied (a full-replace save either succeeds as a whole or not at all).

### FR-1.8 Email verification

**Description**: A real (non-demo) account created by FR-1.1 must confirm its email address before it can use anything beyond this screen — the map and every other authenticated route are gated on it. A demo account (FR-2) is never subject to this gate.

**Preconditions**: An active session for a real account whose email is not yet verified.

**Behavior**:
1. On signup (FR-1.1) and on an email change (step 4 below), the server emails a link containing a verification token (valid 24 hours, single-use) to the address it confirms: the address on file, or the new address a change asked for.
2. The link opens `/verify?token=…`, which verifies the token the same way `POST /v1/auth/verify-email` does. The Android app doesn't call that endpoint: the link is opened in a browser, and the app finds out by asking `GET /v1/auth/me` again (`apps/android/docs/SPEC.md` FR-1.4). A missing, expired, used or malformed token shows "This verification link is invalid or has expired." A link confirms only the address it was sent to. On success, the server makes that address the account's own if it was a pending change (or answers `409 Conflict` if another account has taken it meanwhile), marks the account verified, invalidates every other outstanding verification token for it — and, when the address changed, every outstanding password-reset link — and creates a fresh session for whichever browser opened the link — regardless of whether that browser already held a session of its own, so the link works from any device.
3. While waiting, the account holder can request another copy of the link (`POST /v1/auth/resend-verification`) without needing to already know it was lost or expired. It goes to the address the newest outstanding link went to.
4. The account holder can also change the address (`PATCH /v1/auth/email`) before ever verifying — correcting a typo the original signup made, since a resend alone cannot fix a wrong address. The change is pending until the link sent to the new address is opened: until then the account keeps its address on file and its verified state, the new address stays free for anyone else, and any link sent before the change stops working. `/verify-pending` names the address the newest link went to. Resends and changes together are rate-limited to 5 per hour per account.
5. Once verified, the account continues to FR-1.7's first-run Settings page, not straight to the map, until Country and Timezone have been saved once.
6. Until verified, every route other than `GET /v1/auth/me`, `POST /v1/auth/logout`, and the three endpoints above returns `403 Forbidden` with a distinguishable error rather than the normal response.

**Outputs**: `verify-email` and `reset-password` both return a valid session cookie for the account on success. `resend-verification` and `change-email` return a confirmation; `change-email` also returns the account's profile as it stands, still with its address on file.

**Error cases**:
- Verification token missing, already used, or expired → `400 Bad Request` with a generic message, same non-distinguishing reasoning as FR-1.6's reset token.
- `change-email`/`resend-verification` attempted by a demo account → `400 Bad Request` (neither concept applies to one).
- `change-email` to an address another account has → `409 Conflict`.
- More than 5 resend or change-email requests for the same account within an hour → `429 Too Many Requests`.

**Notes**: This reverses an earlier, deliberately simpler version of FR-1 that had no email verification at all — added once real Health Connect/cloud sync made an unrecoverable, mistyped-email account a real cost (server-side ingest work stranded on an account nobody can get back into), not because the original simplicity was a mistake.

### FR-1.9 Sign in with Google

**Description**: A visitor signs in, or creates an account, with a Google account instead of an email and password, on the web or in the Android app.

**Preconditions**: The deployment has Google OAuth credentials configured. Without them, `GET /v1/auth/providers` reports `{"google": false}`, the client shows no Google button, and the start and callback endpoints below, and Android's `POST /v1/auth/google/token`, return `404 Not Found`. With them, it also reports the web client ID as `google_client_id`, which the Android app needs.

**Inputs**: The Google account the visitor picks on Google's own account chooser; the browser's IANA timezone, sent automatically as with FR-1.1.

**Behavior**:
1. When the server has Google credentials configured (what `GET /v1/auth/providers` reports as `google: true`), the sign-in and sign-up pages show "Continue with Google" above the email/password form.
2. Clicking it navigates the whole page to `GET /v1/auth/google/start?tz=<timezone>` (a one-line script adds the browser's timezone; without it a new account starts on UTC). The server sets a short-lived (10-minute) cookie holding a random `state` value and a PKCE verifier, and redirects to Google's consent screen, always showing the account chooser.
3. Google redirects back to `GET /v1/auth/google/callback`. The server checks `state` against the cookie (and clears the cookie either way), exchanges the authorization code with Google, and reads the Google account's stable id, email and name. A Google account whose email Google itself reports as unverified is refused.
4. The server picks the account:
   - An account already linked to this Google account is signed in, even if its HoldMyTrack email has since changed.
   - Otherwise, a real (non-demo) account with the same email is linked to this Google account and signed in; the email counts as verified from then on (FR-1.8). If that account's email had **not** been verified, its password and any linked Facebook identity (FR-1.10) are also removed and all its sessions and outstanding reset/verification links are ended — whoever chose that password or linked that identity never proved they own the address.
   - Otherwise, a new account is created: already verified, display name taken from the Google profile, timezone from step 2 (falling back to UTC, as FR-1.1).
5. The server creates a session (30-day expiry) exactly as FR-1.2 does and redirects to the app's root, where FR-1.4's session check picks it up. A new account then continues to FR-1.7's first-run Settings page.

**Outputs**: A valid session cookie and a redirect to the app.

**Error cases**: Every failure — the visitor cancelled on Google's screen, a missing or mismatched `state`, the code exchange failed, the Google email is unverified, or the matching account is already linked to a *different* Google account — redirects to `/signin?error=google`, which shows a generic "Couldn't sign in with Google. Please try again." message. The cause is logged server-side, not revealed in the URL.

**Notes**: A Google-only account has no password, so FR-1.2 rejects it with the same generic error as any other mismatch; it can set a password at any time through FR-1.5/FR-1.6, after which both ways in work. Changing the account's email (FR-1.8 step 4) leaves the Google link in place.

**On Android** (ADR-0016):
1. The sign-in screen shows "Continue with Google" when `GET /v1/auth/providers` reports `google: true` with a `google_client_id`.
2. Tapping it opens the system's Google account picker (Credential Manager), which returns a Google ID token issued for that client ID.
3. The app posts `{id_token, tz}` to `POST /v1/auth/google/token`, `tz` being the device's IANA timezone. The server checks the token's signature against Google's published keys, then everything in steps 3–4 above applies unchanged: the same claim checks, the same choice of account.
4. It answers with the same body as `POST /v1/auth/login` (FR-1.2), including `session_token`, and the app opens the map.
5. Closing the picker does nothing. Any other failure (an invalid token, the email check, the account already linked to a different Google account, or the picker itself failing) shows "Couldn't sign in with Google. Please try again."; the server's answer is a `401` with that text.

### FR-1.10 Sign in with Facebook

**Description**: A visitor signs in, or creates an account, with a Facebook account instead of an email and password, on the web or in the Android app. Unlike FR-1.9, a Facebook identity is never linked to an existing account by email: Facebook doesn't say whether the email it returns has been verified.

**Preconditions**: The deployment has a Facebook app configured. Without it, `GET /v1/auth/providers` reports `{"facebook": false}`, the client shows no Facebook button, and the start and callback endpoints below return `404 Not Found`.

**Inputs**: The Facebook account the visitor is signed in to on Facebook, and the permission to share its email; the browser's IANA timezone, sent automatically as with FR-1.1.

**Behavior**:
1. When the server has a Facebook app configured (`GET /v1/auth/providers` reports `facebook: true`), the sign-in and sign-up pages show "Continue with Facebook" above the email/password form, below "Continue with Google" when both are configured.
2. Clicking it navigates the whole page to `GET /v1/auth/facebook/start?tz=<timezone>`. The server sets a short-lived (10-minute) cookie holding a random `state` value, and redirects to Facebook's login dialog asking for the `email` and `public_profile` permissions — again for the email, if the visitor declined it on an earlier attempt.
3. Facebook redirects back to `GET /v1/auth/facebook/callback`. The server checks `state` against the cookie (and clears the cookie either way), exchanges the authorization code with Facebook, and reads the Facebook account's app-scoped id, name and email.
4. The server picks the account:
   - An account already linked to this Facebook account is signed in, even if its HoldMyTrack email has since changed.
   - Otherwise, if any account already has that email, nothing is linked and the sign-in is refused (below).
   - Otherwise, a new account is created with that email, **unverified**, display name taken from the Facebook profile, timezone from step 2 (falling back to UTC, as FR-1.1), and a verification email is sent exactly as for a sign-up (FR-1.8).
5. The server creates a session (30-day expiry) exactly as FR-1.2 does and redirects to the app's root. A new account then waits on `/verify-pending` until its email is verified (FR-1.8), then continues to FR-1.7's first-run Settings page.

**Outputs**: A valid session cookie and a redirect to the app.

**Error cases**:
- The Facebook account shared no email (it was registered with a phone number, or the visitor declined the permission) → `/signin?error=facebook_no_email`: "Your Facebook account didn't share an email address, and HoldMyTrack needs one. Try again and allow access to your email, or sign up with email and password."
- An account with that email already exists → `/signin?error=facebook_email_in_use`: "An account with this email already exists. Sign in the way you usually do, or use “Forgot password?” to set a password."
- Anything else — the visitor cancelled on Facebook's screen, a missing or mismatched `state`, the code exchange or profile request failed → `/signin?error=facebook`: "Couldn't sign in with Facebook. Please try again." The cause is logged server-side, not revealed in the URL.

**Notes**: A Facebook-only account has no password, so FR-1.2 rejects it with the same generic error as any other mismatch; it can set one through FR-1.5/FR-1.6. Changing the account's email (FR-1.8 step 4) leaves the Facebook link in place. An account created here whose email is later claimed through Sign in with Google while still unverified loses its Facebook link (FR-1.9 step 4). There is no way yet to link Facebook to an existing account.

**On Android** (ADR-0016). The app runs the same flow in a browser tab rather than through Meta's SDK:
1. The sign-in screen shows "Continue with Facebook" when `GET /v1/auth/providers` reports `facebook: true`.
2. Tapping it makes a random verifier and opens `GET /v1/auth/facebook/start?tz=<timezone>&app_challenge=<S256 hash of the verifier>` in a browser tab. A malformed `app_challenge` → `400 Bad Request`.
3. Steps 2–4 above happen in the tab, unchanged, including the verification email for a new account.
4. Instead of step 5, the server stores a one-time code, valid for 2 minutes and tied to the challenge, and redirects the tab to `holdmytrack://oauth?code=<code>`, which closes the tab and returns to the app. The app posts `{code, verifier}` to `POST /v1/auth/handoff` and gets the same body as `POST /v1/auth/login` (FR-1.2), including `session_token`.
5. A code works once. An unknown, already used or expired code, or a verifier that doesn't match → `401` "This sign-in link has expired or was already used. Please try again.", and the code is used up either way.
6. The three error cases above redirect to `holdmytrack://oauth?error=<code>` instead, and the app shows the same messages. Closing the tab returns to the sign-in screen with no message.

A new account made this way is unverified, like one made on the web; the Android app shows its own "check your email" screen until it is (`apps/android/docs/SPEC.md` FR-1.4).

## 4. FR-2 — No-Signup Demo

### FR-2.1 Start a demo

**Description**: An anonymous visitor tries the full application without creating an account.

**Preconditions**: No active session.

**Behavior**:
1. Client calls `POST /v1/auth/demo` — on the web, the sign-in page's "Try it now — no signup" button submits `POST /demo`, which does the same and redirects to the map. Starts are rate-limited to 5 per hour per address; over the limit the sign-in page shows the error.
2. Server opens a session against one persistent, shared **Demo Customer** account — not a fresh account created per visitor. That account is pre-seeded, once, out of band (not per request — see FR-2.2), with a history of real activities picked from a live deployment: currently sixteen from July–October 2026 around Cleveland, OH — a bike ride to Bonnie Park Picnic Area; a Solowheel ride through Cleveland's West Side, with seven photos along it (FR-16); walks at Big Creek Reservation, Andrew's Nature Play Area, Lakefront Reservation, Solon Community Park and Radlick Park; Brecksville Reservation and Clague Park trips (each a drive there, a walk, a drive back); and three days of Solowheel rides on the Emerald Necklace Trail. The real set is still being added to; it replaced an earlier synthetic history of 611 generated activities. Its Settings profile is preset rather than left at generic column defaults (Name "Demo User," Country United States, Timezone America/New_York), and so are three Private locations (FR-8.1), each a 305 m (1000 ft) circle, where the real history starts and ends: "Home," "Eva's school" and "Sonya's school," in Lakewood/Cleveland, OH.
3. Server creates a session for this visitor (24-hour expiry) and sets it as a cookie. Any number of visitors can hold their own session against the same shared account at once — they all see identical data.
4. Client is signed in and shown the map, with the account's full history already visible in every mode (Normal, Fog of War, Heatmap) and every read-only feature (filtering, the activity graph, export) exactly as a registered user has it. **A demo account cannot upload, sync, edit, or delete an activity, or change its avatar/settings** — every such request is rejected regardless of what a client attempts, whether or not its own UI still offers the control. Creating a real account (FR-1.1) is the only way to save one's own data. Both clients also gate this proactively rather than relying on the rejection alone: the web app shows the header's Upload button disabled, its tooltip explaining why (FR-3.4), and the Android app disables Sync Now and hides the Health Connect permission flow with the same explanation (FR-3.8) — a demo session can still record and manage GPS Logger recordings locally on Android, since nothing about that reaches the server until sync is attempted.

**Outputs**: A valid session cookie for the shared Demo Customer account, already showing its full activity history.

**Rate limiting**: Limited to 5 demo session starts per hour per client IP address; further requests receive `429 Too Many Requests`.

### FR-2.2 Demo account seeding and lifetime

**Description**: Unlike a demo account under the earlier per-visitor design, the shared Demo Customer account (FR-2.1) and its activity history are not created or deleted per visit — only each visitor's own *session* is temporary.

**Behavior**: The Demo Customer account is seeded once, out of band, via a `seed-demo-customer` CLI subcommand run at deploy time (not from any HTTP request) — re-running it is safe and only fills in anything missing. Because a plain re-run never changes or removes an activity already seeded, `seed-demo-customer --reset` first deletes every activity the account has (and everything derived from them: fog/heatmap tiles, photos, stored files) and then seeds from scratch, so the account ends up matching the shipped history exactly — the step for a deployment whose demo history has changed. The shipped history is a set of activity files committed to the repository; an `export-demo-activities <out-dir> <activity-id>...` subcommand writes chosen activities from a deployment into that set as GPX, one file per activity (named by start date and activity name, with a numeric suffix on a clash), holding exactly the track its owner sees — after their Private locations and track edits, never the original upload — plus its photos (FR-16) as the stored, resized images, and a manifest recording each one's name, activity type, description and photos (each with its place on the track, capture time and caption), which the seed applies; a re-run of the seed updates a photo already seeded rather than adding it again. The manifest also lists the account's Stories (FR-14) — each a name, a description and its activities by file — which the seed creates, and on a re-run holds to the manifest (description and exactly those activities); `--reset` deletes the account's Stories with its activities. The shipped history has three: "Brecksville Reservation trip" (the drive there, the walk, the drive back), "Emerald Necklace Trail" (its three days' rides, "Day #1" to "Day #3") and "Greater Cleveland trails" (those rides and the bike ride to Bonnie Park Picnic Area). The export only reads the deployment, and keeps the manifest's Stories as they are. Its `demo_expires_at` is set to a fixed far-future timestamp rather than left null, which is what keeps it read-only (FR-2.1) without ever matching the background purge sweep's `< NOW()` condition, so the account and its data are never deleted. Each visitor's own *session* still expires 24 hours after `POST /v1/auth/demo` was called, same as any other session — a visitor who stays past that just calls it again for a fresh session against the same account.

**Notes**: This replaces an earlier design where each demo visitor got their own new, ephemeral account seeded with two small fixed presets, deleted 24 hours later by the same background sweep. That per-visitor purge sweep still exists (`internal/worker/demo_purge.go`) and still runs, but has nothing left to act on under the current design — it would only matter again if a future change reintroduced per-visitor demo accounts.

### FR-2.3 Create a registered account from a demo session

**Description**: A demo user creates a real, permanent account of their own to start saving data. The shared Demo Customer account (FR-2.1) belongs to no one visitor in particular and is never modified by one, so this is an ordinary new signup, not a conversion: nothing from the demo carries over, and nothing the person does in a demo session is "theirs" to keep in the first place.

**Preconditions**: An active demo session.

**Inputs**: Email address, password (minimum 8 characters) — the same inputs as FR-1.1.

**Behavior**:
1. From the account menu, which shows the demo account's name ("Demo User") where a real account shows its email, the user selects "Create your own account," which opens the same sign-up page a new visitor sees (`/signup`, FR-1.1), with "← Back to the map" in place of the sign-in page's "try demo" option (starting a second demo while already in one would abandon the first).
2. The user submits an email and password.
3. The server creates a plain new account (FR-1.1's normal behavior) — the shared Demo Customer account itself is untouched, exactly as every other concurrent demo visitor's session leaves it.
4. The user is signed in to the new account and sent to FR-1.8's verify-email page (`/verify-pending`), exactly like any other fresh signup.

**Outputs**: A new, unverified registered account — not the demo account, and not carrying any of its activity history.

**Note — cancellation**: selecting "← Back" (or navigating away without submitting) returns to the map with the demo session untouched — nothing is lost, and it remains subject to FR-2.1's 24-hour session expiry (the underlying account itself is unaffected either way, per FR-2.2).

## 5. FR-3 — Activity Upload & Ingestion

All upload functionality requires an active session (demo or registered — FR-1/FR-2); there is no unauthenticated upload path.

### FR-3.1 Upload individual files

**Description**: A signed-in user uploads one or more individual activity files.

**Preconditions**: Active session.

**Inputs**: One or more files, each a `.gpx`, `.fit`, or `.tcx` file no larger than 64 MiB. Up to 20 individually-selected files per batch (a larger selection is rejected client-side in full, before any upload begins, with a message directing the user to a `.zip` archive instead — FR-3.2).

**Behavior**:
1. User chooses files from the header's **Upload** menu (FR-3.4), on any page, or drops them on the map.
2. Each file uploads independently, as its own `POST /v1/activities/upload` request (multipart), and is tracked independently — one file failing does not affect the others.
3. For each file: server validates its extension and size, computes a content hash to check for a duplicate (FR-3.5), persists the raw file, and enqueues a background parsing job.
4. The Upload menu shows each file's live status (uploading, with a progress percentage; then "Processing…") until the background job finishes; then it leaves the menu for the Sync page (FR-3.9).
5. Once ingestion completes, the activity appears automatically in the Activities panel, the map, and every summary that reflects the current date range — no page reload is required. Its Fog-of-War/Heatmap coverage follows a few seconds later, once the background re-render finishes, also without a reload.

**Outputs**: One new `Activity` per successfully ingested file, each with a parsed trajectory, distance, duration, and (where the source file provides it) elevation data. Heart rate and any other health data in the file are ignored — never read out of it (`VISION.md` §1.1).

**Error cases**:
- Unsupported file extension → `415 Unsupported Media Type`.
- File too large, or malformed request → `413 Request Entity Too Large`.
- Empty file → `400 Bad Request`.
- Unparseable/corrupt file content → the background job fails; the Sync page (FR-3.9) shows it "Failed" with a reason, the header's Sync item carries a red dot until that page has been opened (FR-3.9), and no `Activity` is created.
- A file whose points carry no timestamps at all (a planned route rather than a recorded activity) → the background job fails the same way, with a reason saying the file has no timestamps. Points without a timestamp inside an otherwise timed track are dropped, not failed on.

### FR-3.2 Bulk upload via `.zip` archive

**Description**: A signed-in user uploads many activity files at once as a single `.zip` archive, bypassing FR-3.1's 20-file batch limit.

**Preconditions**: Active session.

**Inputs**: One `.zip` archive, at most 512 MiB compressed, containing at most 5,000 entries, each contained file at most 64 MiB.

**Behavior**:
1. User uploads a `.zip` file the same way as FR-3.1 (the Upload menu, or dropped on the map). While its files are processed it is one row in the Upload menu, counting them (FR-3.4).
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
1. User uploads the Takeout export `.zip` the same way as FR-3.1/FR-3.2.
2. Server recognizes the archive's shape as a Takeout export (rather than a plain `.zip`) and extracts one activity file per recorded activity that has GPS data (activity types with no GPS in the export — e.g. a logged swim with no route — are skipped, not treated as errors).
3. Each extracted activity is ingested exactly as FR-3.1 describes, attributed to the Takeout source.

**Outputs**: One new `Activity` per activity in the export that had GPS data.

### FR-3.4 Import status

**Description**: What's being imported right now is the header's **Upload** menu, on every page; what an import came to once it has finished is the **Sync** page, `/sync` (FR-3.9), the header's next item. Imports from every source show in both: uploaded files and archives alongside activities synced from the Android app (Health Connect and in-app GPS recording — FR-3.6, FR-3.8). The web can't start a phone sync — that sync is phone-triggered, and nothing in the web app can request it; the Android app shows its own history of the same imports on its Sync screen (`apps/android/docs/SPEC.md` FR-4.1).

**Preconditions**: Active session.

**Behavior** — an **Upload** button in the header of every page opens a menu of what is being imported right now:
1. **Choose files…** opens the file picker (`.gpx`, `.fit`, `.tcx`, `.zip` — FR-3.1–FR-3.3); files dropped anywhere on the map, which shows a dashed "Drop to upload" cover while they're dragged over it, go to the same place. Choosing or dropping opens the menu.
2. The menu lists every import in progress: files waiting their turn ("Queued"), a file being sent ("Uploading 45%"), then each one the server is processing ("Processing…"), including activities synced from the phone, named after their source. A `.zip` or Takeout export is one row counting its files — "Processing 120 of 340". The button shows how many rows there are.
3. An import leaves the menu as soon as it has finished, whether it succeeded or failed; what it came to is the Sync page's (FR-3.9). The map refreshes itself as each finishes.
4. Messages about this page's own uploads — a file already uploaded before, how many of a `.zip`'s or Takeout export's files were already uploaded before ("Already uploaded before, in Trips.zip: 12"), the files an archive skipped, too many files chosen, a file the server refused outright (with the reason) — show in the menu until dismissed, one by one.
5. The menu closes on a click outside it or Escape. It has nothing about finished imports: those are the header's Sync item's (FR-3.9).
6. What's in progress is read from the server every 2 seconds while anything is, every 20 otherwise.
7. A demo session sees the button disabled, its tooltip saying why.

**Outputs**: `GET /v1/uploads/active` returns the imports still in progress — each with a title, its source, how many of its jobs have finished and how many there are, and when it was submitted — and how many failed imports the account hasn't seen yet on the Sync page. `GET /v1/uploads?limit=&offset=&source=` (the Android app's Sync screen) returns one page of every import, the total count (scoped to `source` when given), and how many are still processing; each row carries `source` and, once the job has produced one, the resulting activity's own `id`.

### FR-3.5 Duplicate detection

**Description**: Re-uploading a file whose content was already ingested does not create a second activity or a second processing job.

**Behavior**: The server checks a content-derived identifier before enqueuing a new job. If that exact content was already ingested for this account, the upload responds immediately with an "already processed" status (`200 OK`) and no new job is created; the client shows a notice that the file was already uploaded rather than silently doing nothing.

**Notes**: This guarantee holds even under concurrent duplicate uploads (e.g., two tabs uploading the same file at once) — at most one `Activity` is ever created for the same content.

### FR-3.6 Mobile sync (Android / Health Connect)

**Description**: The Android app reads a signed-in account's exercise history from Health Connect and syncs it to HoldMyTrack — a second ingest path (Path 2) alongside file upload above, distinct from a file the user explicitly picked.

**Preconditions**: Signed in on the Android app (`apps/android/holdmytrack`); Health Connect installed, with the Exercise permission granted plus the separately-granted "Access exercise routes" permission — a session with no route geometry can't be placed on the map, so it's rejected at sync time rather than persisted without one (see step 3).

**Inputs**: Health Connect exercise sessions with route geometry, read **foreground-only** — `READ_EXERCISE_ROUTES` returns `ConsentRequired` in the background regardless of what's granted, a platform constraint rather than a client choice. `READ_HEALTH_DATA_HISTORY`, requested separately, extends the otherwise 30-day-only read window. Only the session and its route (position, time, altitude) are read — never heart rate or any other health measurement; the app doesn't request those permissions at all (`VISION.md` §1.1).

**Behavior**:
1. User opens the Sync screen (the burger menu's Sync); the app walks through granting whichever Health Connect permissions are still missing, and Sync now includes Health Connect whenever it can be read (`apps/android/docs/SPEC.md` FR-3.5).
2. A foreground sync run reads sessions ascending from the account's own last confirmed position (a cursor keyed on both an instant and the record ids already handled at it, not a bare timestamp — two sessions can share a start instant), classifies each one, and posts batches to `POST /v1/sync/activities`.
3. A session with no route (an indoor workout, or any Samsung Galaxy Watch session — Samsung doesn't expose route geometry via Health Connect at all) is skipped and reported as such, not treated as a failure; the cursor still advances past it.
4. A route that exists but can't be read this run (`ConsentRequired`, e.g. the app was backgrounded mid-run) is reported distinctly from "no route" and blocks the cursor from advancing past it, so a resumed run retries it rather than skipping it permanently.
5. Each synced activity is idempotent on the Health Connect record's own id and flows through the exact same ingest pipeline FR-3.1's file upload uses. Activity type is normalized onto HoldMyTrack's existing vocabulary (Health Connect's `biking` becomes `cycling`, etc.), so it doesn't fragment the TYPE filter.

**Outputs**: One new `Activity` per synced session with a route; a per-run summary (synced / skipped-no-route / rejected, each with its own reason) on the sync screen; and a persistent history via the same `GET /v1/uploads` FR-3.4 already describes — a Health Connect sync and a file upload are the same kind of ingest job, not two separate histories.

**Error cases**: Server unreachable mid-run — the run stops, reports the failure, and the cursor does not advance past anything the server never confirmed, so a retried run resumes rather than re-sending everything already accepted.

**Not yet built**: the iOS/HealthKit half of Path 2 — no iOS app exists yet (`apps/ios` is a placeholder). `apps/android/docs/ROADMAP.md` is the authority on Android's own remaining work (UI design, Play Store compliance).

### FR-3.7 Cross-source duplicate detection

**Description**: The same real-world activity arriving from two different sources (e.g. synced from a watch via Health Connect, then later also uploaded as an exported `.fit` file) is recognized as one activity, not two — distinct from FR-3.5, which only catches identical re-uploaded file content from the same source.

**Preconditions**: At least two ingest sources have produced activities for the account close enough in time to compare (FR-3.6's mobile sync is what makes this reachable at all today; Path 1 cloud connectors will be a third source once built).

**Behavior**:
1. On ingest, a new activity is compared against the account's existing ones by time: two activities are the same one when their time ranges overlap for at least 80% of the longer one's duration. Activity type and distance are not compared, since sources routinely disagree on both for the same activity ("walking" vs "hiking", a few percent of distance). Two activities that only touch — back-to-back recordings with a few seconds of clock skew — or where one is a small part of the other (a short auto-detected walk inside a long hike, a day hike inside a multi-day recording) stay separate. An activity with no duration is never matched.
2. A match is resolved by keeping the richer record (route geometry over none; then elevation data over none) and marking the other `superseded_by` the winner, rather than deleting it.
3. Every user-facing read — the Activities list, totals, histogram, day pages, trends, graph stats, map tiles, and both Fog of War and Heatmap composites — excludes superseded activities automatically. A superseded copy's photos (FR-16) move to the kept copy, at the same moment on its track.
4. Deleting the kept copy of a matched pair promotes the next-richest superseded copy back to live, rather than leaving both gone.

**Outputs**: At most one live `Activity` per real-world activity, regardless of how many sources reported it.

5. The web's Sync page (FR-3.9) lists the superseded activities under its history whenever there are any, with each one's start time, distance and source and which source's copy superseded it; so does the Android app's Sync screen (`apps/android/docs/SPEC.md` FR-4.2). `GET /v1/activities/duplicates` returns the same rows.

### FR-3.8 In-app GPS recording (Android)

**Description**: The Android app records a casual, GPS-only activity itself — a walk, hike, or drive someone would not otherwise bother tracking — started and stopped from one button on the map, asking nothing while recording. Recording and syncing are two separate, explicit steps: Stop only saves a finished recording to the device; nothing reaches the server until the user taps the Sync screen's "Sync now" — a third way an activity can originate on the Android app, alongside FR-3.6's Health Connect sync and a manual file upload (FR-3.1) done from the phone's browser.

**Preconditions**: A session — the Android app shows nothing but its sign-in screen without one — and location permission granted to record. Local recordings are scoped to whichever account is currently signed in — switching accounts on one device never shows one account's recordings under another's.

**Inputs**: The device's own GPS, read while a recording is in progress; a name, an activity type (free text, not a fixed list), and a description, set after the fact from the recording's Edit button on the **Sync** screen — never while recording.

**Behavior**:
1. A record button sits at the right of the map, on its own row under the top row's Find my location (the bottom of the screen is the Activities panel's, `apps/android/docs/SPEC.md` FR-2.7). **Tap** starts recording (asking for location and notification permission the first time); **tap** again pauses — with a short "Hold to stop recording" hint — and again resumes; **holding it for two seconds** stops, with a ring filling around the button as the hold counts down (letting go early cancels). Pause/resume is user-initiated only — there is no automatic pause, and nothing is recorded while paused. Recording runs as a foreground service, so it survives the screen turning off and the app being closed.
2. While recording, the map shows only the recording in progress — its line and current position, with the camera following — and none of the account's other activities or map modes (Normal/Fog/Heatmap). The previous mode returns when recording stops.
3. A notification is shown for as long as a recording is in progress, saying whether it is recording or paused, with elapsed time, distance, current speed and current altitude, and **Pause**/**Resume** and **Stop** buttons that work like the map button.
4. **Stop** saves the recording to an on-device store only — no network request happens. It is saved with no name or description and with the activity type most recently set in Edit on this account (`"unknown"` if none ever has been), and a **Save** screen then opens on it — the Edit form below, pre-filled with that type, with **Discard** in place of Download GPX. Leaving it with Back keeps the recording as saved; Discard (after confirmation) deletes it. A recording with under one minute of moving time (pauses excluded), or fewer than 2 points, is not saved at all. If the app is killed mid-recording (the system reclaiming memory, a crash, Force stop), what was recorded up to the last GPS fix is kept on the device, and the next time the map opens under that account a dialog offers **Resume** (back paused, the time the app wasn't running counted as a pause), **Save** (as Stop) or **Discard** (after confirmation) — nothing restarts on its own (`apps/android/docs/SPEC.md` FR-5.1 step 9).
5. The **Sync** screen (a menu item, FR-3.6) lists every recording on the device that hasn't synced yet under Recorded activities, newest first. Each row has a small preview of the recorded route's shape, its distance, moving time and type, and Edit and Delete buttons.
6. Every row goes out with the next Sync now, whatever its type, including the `"unknown"` default — FR-3.7 matches on time, so an untyped recording still matches the same walk arriving typed from another source (e.g. Health Connect). A recording not wanted on the map is deleted instead.
7. **Edit** shows Name, Activity Type, Description, the recording's time and distance, Save and **Download GPX**. Name and Description are plain text; Activity Type is a searchable picker behaving like the web edit dialog's (FR-5.10): the account's existing types, most-used first with how many activities use each, then walk/hike/run/ride/drive if unused, filtered as you type, with an **Add "…"** row that accepts any other text exactly as typed. A saved type also becomes the type later recordings start with (step 4). **Download GPX** saves the track as a GPX 1.1 file (name, description, type, and each point's position, time and elevation where known) wherever the user picks in the system's save screen — a file FR-3.1's upload accepts back.
8. The same screen's **Sync now** (FR-3.6) submits every row — after the Health Connect sync when Health Connect can be read, on its own otherwise — each as its own `POST /v1/sync/activities` call under `source = "recorded"` with a client-generated `external_id` (a UUID minted when the recording starts) — the same batched wire shape FR-3.6 uses, reusing its ingest pipeline unchanged (`docs/IMPLEMENTATION.md` §4.0.4, [ADR-0007](adr/0007-in-app-gps-recording-submits-directly.md)). A row that syncs successfully is deleted from the device — from then on it exists on the server, on the map and in the activity list, like any other activity. A row the server rejects, or that fails outright, stays on the device and is retried automatically on the next "Sync now" — no action needed from the user.
9. **Delete** asks for confirmation — noting the recording hasn't synced, so this is the only copy — and then removes the row from the device.
10. **A recording never appears on the map after it stops until it has synced** — the map draws only what the server holds (FR-4).
11. **A demo session** (FR-2.1) can record and manage rows here like any other account, but the screen hides Sync now, with a notice explaining why (FR-2.1).

**Outputs**: One new `Activity` per successfully synced recording, titled and described from the moment it's created if the user set a name or description in Edit — the one ingest path where that's possible (every other path leaves both fields unset at ingest). Reported through the same `GET /v1/uploads` history FR-3.4 already describes.

**Error cases**: A too-long custom activity type (over 50 characters, the column's own bound) is rejected by the sync endpoint with a clear reason rather than failing as a raw database error at insert time. A row that fails to sync (network error, server rejection) is left on the device, so a retried "Sync now" tries it again without the user doing anything.

**Not yet built**: a discard confirmation before Stop finalizes a save; the iOS half (`docs/ROADMAP.md` Phase 2 tracks it as a combined Android/iOS item; Android's half is what this FR describes).

### FR-3.9 The Sync page

**Description**: **Sync**, an item of its own in the header of every page for a signed-in account (after Upload), opens `/sync`, which lists every import that has finished — uploaded files, the files inside an archive and activities synced from the phone alike — and the duplicates cross-source detection took out of circulation (FR-3.7).

**Preconditions**: Signed in; signed out, the page sends you to sign in.

**Behavior**:
1. Newest first by when each finished, 20 per page. With more than one page, a pager under the list: **← Newer** on the left, "1–20 of 57" (or "21 of 21") between, **Older →** on the right — both always in their places, the one with nowhere to go shown disabled. An import still being processed is not listed: the page shows only finished ones, so it doesn't change while it's open.
2. Each row has a title — the file's name, or for a phone sync its source ("Health Connect", "GPS Logger") — then **Ready** with the activity's date and distance and a **View on map** link, or **Failed** with the reason in the reader's language (§17's error messages).
3. **View on map** opens the map on that activity: on the Activities tab, the date range narrowed to its day if it isn't already in view, the date slider's window moved to show that day with both knobs on it (FR-6), the activity selected (FR-5.5) and the camera fitted to it; on a phone the Activities sheet stays collapsed, so the track is visible. The link's parameters leave the address bar once read, so a refresh doesn't do it again.
4. Under the history, when there are any, the duplicates: each one's start date and time and distance, and which source it came from and which copy replaced it — "From Health Connect, replaced by the copy from an uploaded file."
5. A failed import the account hasn't seen yet puts a red dot on the header's Sync item, its tooltip saying how many ("Failed imports: 1"); opening the page counts every failure so far as seen and clears it. The item is highlighted while the page is open, and on a phone it's its icon alone.
6. A demo session sees the Demo Customer's history, with a line saying to create an account to import one's own.

## 6. FR-4 — Map Visualization

### FR-4.1 Track rendering (Normal mode)

**Description**: The map displays a user's uploaded activities as drawn lines over a base map.

**Preconditions**: Active session.

**Behavior**:
1. The map renders every activity within the currently selected date range (FR-6) that has not been individually hidden (FR-5.8), filtered out by TYPE/DISTANCE (FR-5.2/FR-5.3), or left out while Pending (FR-5.15), as a colored line following its recorded route.
2. Hovering a track on the map draws it thicker; the corresponding row in the Activities panel is highlighted to match (FR-5.4's reverse direction).
3. Clicking a track on the map sets it as the row-click focus (FR-5.5) — emphasizes it, flies the camera to fit it, and replaces whichever activity was previously focused. It does not add to or remove from the checkbox group (FR-5.6) in either direction.
4. Clicking anywhere on the map that is not a track clears the row-click focus, if any — the focused activity loses its focus treatment and returns to Normal. Clicking empty space in the Activities panel's list does the same (FR-5.5). Neither affects the checkbox group.
5. Tracks are drawn from zoom 4 — a few states on screen — inward. Zoomed out further, Normal mode shows the base map alone; unlike Fog and Heatmap, it has no country/region fallback (FR-4.2, FR-4.3). Fitting the camera to an activity (FR-5.5, FR-5.7) lands at zoom 4 or closer for anything spanning up to about 60° of longitude at desktop width — a US coast-to-coast drive included — and about 25° on a phone; a wider activity is flown to but isn't drawn until the user zooms in.
6. Every track is always in one of three states — Normal, Hovered, or Focused — each drawn distinctly (*Track states*, below). Checking a row's checkbox (FR-5.6) is not a track state: a checked track draws in whichever of these it's otherwise in, and its row shows only the ticked checkbox.

**Track states**:

| State | Entered by | Track on the map | Also on the map | Its row in the Activities panel | Camera |
| :-- | :-- | :-- | :-- | :-- | :-- |
| Normal | default | thin gold line | — | plain | — |
| Hovered | pointer over the track, or over its row (FR-5.4) | the thickest line, in a darker gold, no outline | — | title underlined | doesn't move |
| Focused | clicking the track (behavior 3) or its row's text (FR-5.5) | thicker gold line with a dark outline, fully opaque | pace-colored segments over the line (FR-4.8) | tinted background with a gold bar on its left edge; checkbox unchanged | flies to fit that one track |

Only one track is hovered and only one is focused at a time. Hovering the focused track draws the hover line inside its outline, and underlines its row title. A hidden track (FR-5.8) draws nothing in any state, and none of these states exist outside Normal mode (FR-4.2, FR-4.3). An emphasized track is not raised above other tracks where they overlap; its outline is what sets it apart.

### FR-4.2 Fog of War mode

**Description**: An alternate map mode showing a veil over everywhere the user has not recorded an activity — true all-time coverage, ignoring every other filter. The veil is the opposite of the basemap under it: dark over the light basemap, a light cream mist over the dark one (FR-4.12).

**Behavior**:
1. Selecting "Fog" from the map-mode toggle replaces the track lines with a raster veil: any area a recorded route has ever passed through is rendered clear; everywhere else stays fogged.
2. Fog ignores the date range and the Type/Distance/hidden-track filters entirely — it always shows every activity the account has ever recorded, not just what Normal mode currently has selected. Entering Fog hides the Activities panel, and with it the date slider (there is nothing for either to filter) and clears any checked or focused activity. The camera is left exactly where it was — switching modes never moves it, since flying to the coverage on entry proved disorienting.
3. Individual track lines are not drawn in this mode (the veil itself is the information). Map labels (place names, street names, points of interest) stay drawn on top of the veil but are dimmed, so they remain readable without competing with the cleared ground; they return to full strength in Normal and Heatmap. An export (FR-4.10) in Fog mode captures the dimmed labels too.
4. Below a threshold zoom, the veil switches from per-pixel coverage to a coarser reveal: a country renders fully clear the moment the account has at least one activity anywhere inside it, however small; one zoom step in, the same applies one administrative level down, at state/region granularity. Zooming back in past the threshold returns to exact per-pixel coverage — the two never blend or overlap, only one is ever shown at a given zoom. The map names the level in view: on switching to Fog, and whenever a zoom ends in another level, a notice under the mode toggle says which — "Country view — a whole country clears once you've been anywhere in it", "Region view — …a whole state or province…", "City view — cleared exactly where you've been" — and fades after about 3 seconds. It sits on a line of its own under the toggles however they wrap, never takes a click, and never shows in Normal mode.
5. Returning to Normal mode restores the previously checked/focused activities, the date range, and the panel/picker exactly as they were before switching to Fog — without moving the camera, even if the restored activities are off screen.

### FR-4.3 Heatmap mode

**Description**: An alternate map mode shading locations by how often they've been visited recently — a rolling window, not an all-time record, so a route no longer visited can cool off.

**Behavior**:
1. Selecting "Heatmap" replaces the track lines with a raster overlay: anywhere recorded activity has crossed is plainly visible, deep red where it was crossed rarely, turning orange and then bright yellow the more often it was crossed (a daily commute reads hotter than a once-ridden road). The basemap is washed lighter beneath it (darker on a dark map), so the heat stands off roads and land. Its colors are the same whether the map is light or dark.
2. Like Fog of War, Heatmap ignores the date range and the Type/Distance/hidden-track filters, hides the Activities panel and with it the date slider, clears any checked or focused activity, and leaves the camera exactly where it was (see FR-4.2's note on why). Unlike Fog, it only considers activities within a fixed rolling window (the last 365 days, not user-configurable).
3. Individual track lines are not drawn in this mode.
4. How much crossing traffic it takes to reach full brightness adapts to the account's own history, recomputed daily — a new account and a long-running one don't saturate at the same point, so each account's own most-used spot is what reads as hottest, not a fixed number of visits everyone shares.
5. Below the same threshold zoom Fog switches at, the graded overlay is replaced by a flat "visited" highlight at country granularity, and one step in at state/region granularity — not graded by how much, only whether the account has a currently-in-window activity there. The same level notice as Fog's (FR-4.2 behavior 4) names the level in view, worded for heat: "Country view — every country you've been to in the last year is highlighted", "Region view — …every state or province…", "City view — the more often you go somewhere, the hotter it glows". A country/region whose only activity has aged out of the rolling window shows no highlight at this zoom either, matching what the per-pixel overlay already shows at city zoom.
6. Returning to Normal mode restores the previously checked/focused activities, the date range, and the panel/picker exactly as they were before switching to Heatmap — without moving the camera (FR-4.2 behavior 5).

### FR-4.4 Mode is mutually exclusive

**Description**: Normal, Fog, and Heatmap are three views of the same underlying data, not independent toggles — exactly one is active at a time. The toggle separates Normal from the two coverage views with a divider (Normal | Fog, Heatmap), since the choice is two-level: the plain map, or one of the two coverage views; the Android app uses the same divider (`apps/android/docs/SPEC.md` FR-2.2).

### FR-4.5 Base map, theming, and the opening view

**Description**: The map renders a self-hosted vector base map (streets, labels) in a light or dark flavor that follows the page's light/dark theme (FR-4.12), switching live when the theme does. Where the deployment configures imagery, Satellite can be shown under its roads and labels instead (FR-4.14). A URL can pin one of the three other flavors (`&theme=white`, `black` or `grayscale`), kept in the URL until the page's theme next changes, when the map goes back to following it; `&theme=light` and `&theme=dark` pin nothing and are dropped from the URL, since the theme already picks between those two. The current camera position (center, zoom) is reflected in the URL and restored on reload, so a specific view is shareable via link.

**Preconditions**: Active session.

**Behavior**:
1. If the URL carries a saved or shared position (`#map=...`), it wins outright — restored on load, ahead of every fallback below.
2. Otherwise, the account's own most recent activity determines the opening view: the camera flies to fit that single activity, not the full default date-range selection (FR-6.1) — an account with scattered recent history (one activity in another country yesterday, one locally today) would otherwise fly to a near-world view that reads as broken rather than just generic.
3. An account with no activity history at all falls back to its Country setting (FR-1.7), at that country's own view, if one is set.
4. If none of the above applies — no saved position, no activity history, no Country set — the camera opens on a fixed, deliberately zoomed-out world view.
5. Panning or zooming rewrites the URL's saved position continuously, so the current view is always what a copied link restores.

**Notes**: A fresh session (signing out, then signing in or starting a demo session) never inherits a previous session's saved camera position — only an unmodified reload of the same session does. This resolution order never requests the browser's geolocation permission; FR-4.7's "Find my location" is a separate, always-available, user-clicked control, not part of it.

### FR-4.6 Coverage notice

**Description**: The base map covers the whole planet at every zoom level, so there is no out-of-coverage area and no notice for one. Streets and labels are present wherever the user pans.

### FR-4.7 Find my location

**Description**: A one-shot control moves the camera to the browser's current geolocation (via the browser's geolocation permission). It does not continuously track the device's position, and nothing about the click is recorded or sent to the server.

### FR-4.8 Pace-colored segments

**Description**: When an activity has row-click focus (FR-5.5), its track is overdrawn with colored segments reflecting its pace along the route, instead of the single flat color Normal mode (FR-4.1) otherwise uses.

**Preconditions**: An activity currently has row-click focus. Not shown in Fog or Heatmap mode, or when nothing is focused. Independent of the checkbox group (FR-5.6) — checking a box, including when it's the only one checked, does not show bands on its own.

**Behavior**:
1. The track is split into five colored bands (slowest=blue through fastest=red), each band's range computed from that activity's own slowest and fastest speed — not a fixed scale shared across activities.
2. Pace is the only metric: there is no toggle and no heart-rate alternative (`VISION.md` §1.1).
3. Focusing a different activity updates the bands to match it; clicking anywhere on the map that isn't a track (FR-4.1) clears focus and removes the bands, the same as clearing focus any other way.
4. There is no legend and no readout of exact values — the bands show where on the route the activity was faster or slower, relative to itself, and nothing more.

**Notes**: Band boundaries follow the same simplified vertices the track's own line already renders from — a geometrically simple stretch (little directional change) can carry very few of those vertices regardless of how much its speed actually varied there, so the bands can occasionally read coarser than the underlying data on a mostly-straight route. See `IMPLEMENTATION.md` §4.3.1 for the full account.

### FR-4.10 Interactive frame-and-capture map export

**Description**: A header control shows a movable, resizable frame on the live map, anchored to the map itself, and captures exactly the region inside that frame as a high-resolution PNG image, downloaded directly to the caller's device.

**Preconditions**: Active session; the map has finished its initial load. No activity selection is required — the user frames whatever region of the map they want manually, independent of any checked/focused activity.

**Behavior**:
1. Clicking the camera button ("Export map image") in the map's top-right control stack, under zoom and locate, shows a Custom frame — no fixed aspect ratio — centred on the current view at about 70% of the map's size, marked only by a dashed border. Clicking it again while the frame is shown puts it away. The map is not dimmed or obscured anywhere.
2. The frame is anchored to the map: panning the map carries the frame with it, and zooming keeps the frame the same size on screen (so it holds more or less of the map). The frame may be panned partly or fully out of view and still be captured.
3. The frame blocks nothing: pan, zoom and click all work inside the frame exactly as outside it. Dragging the frame's border moves the frame; dragging one of its four corner handles resizes it, with the opposite corner staying put. A Custom frame resizes freely; a platform preset keeps its aspect ratio while resizing.
4. A toolbar sits centred just above the frame's top edge (moving inside the frame when there's no room above it) with a shape dropdown, Close and Capture. The dropdown offers Custom plus every platform image-size preset, grouped by platform (Instagram, Facebook, X) and listing each resolution/shape that platform has (Square, Portrait, Landscape, Story) with its pixel size; not every platform has every shape (e.g. X has no Story or Portrait). The Custom option shows its pixel size too — the frame's own on-screen size, which is exactly what a Custom capture produces — updating live as the frame is resized. Choosing a shape keeps the frame's center and fits the new aspect ratio within its current size. Close cancels with nothing captured. Capture captures exactly the region inside the frame at that moment — the current zoom, rotation, theme, and map mode, at the picked preset's exact declared pixel dimensions, or for Custom at the frame's own on-screen size in pixels. In Normal mode the captured region reflects the current date-range/hidden-track filters; Fog captures its all-time coverage and Heatmap its current rolling window (FR-4.2/FR-4.3), regardless of what Normal mode's filters were set to before switching.
5. The live map is not disturbed by a capture — camera, zoom, and mode remain exactly as they were before it was triggered.
6. While a capture is generating, the Capture button shows a busy state; a failure (e.g. a timeout waiting for tiles to load at export resolution) is reported inline, near the buttons, and the frame stays in place rather than being discarded — the user is not forced to reposition it and retry from scratch.
7. Escape, or the frame's own Close button, puts the frame away and returns to the normal map view with nothing captured. A successful capture also puts it away.

**Outputs**: On a successful capture, a PNG file download, named `holdmytrack-{date}.png`. OSM/Protomaps attribution is baked into the image's own pixels, in the bottom-right corner over a translucent backing plate — not optional or user-removable, since the basemap is an ODbL "Produced Work" and credit is a license requirement on any distributed export, not a preference (`IMPLEMENTATION.md` §5.6). With Satellite on (FR-4.14), the imagery provider's credit follows OSM's on the same line. A small HoldMyTrack logo and wordmark is always baked into the bottom-left corner — not user-removable either — at 70% opacity with no backing plate, its bottom edge level with the attribution plate's and scaled with it; its text is dark on light themes and light on the dark/black themes and over satellite imagery.

**Notes**: This is one of two things `VISION.md` §4.2 groups under "Export" — animated reveals are not built. Pace-colored segments (FR-4.8) are not reflected in a capture even when currently shown on screen — exporting a single focused activity's bands is a narrower case not covered by this slice. Vector/SVG output is not offered; raster (PNG) only. Platform preset dimensions are curated from Hootsuite's social-media-image-sizes guide; profile-picture/cover-photo sizes are excluded, since this feature frames map content, not an account avatar.

### FR-4.11 Map tile caching

**Description**: The map's own tiles (tracks, Fog, Heatmap, and their Country/Region tiers) are kept by the browser or app that fetched them, so a revisited area or a reload doesn't ask the server again, and never shown after they've gone stale.

**Preconditions**: Active session.

**Behavior**:
1. Every account has a tile version, which changes whenever anything that could change one of its tiles happens: an upload or sync landing, a delete, an edit (type, name, description, or track), a Private location change, an activity leaving the heatmap window, a heatmap re-scale, or a change of timezone. It never goes back to an earlier value.
2. The web map and the Android app request every tile with the account's current tile version, which the page carries when it loads and `GET /v1/coverage/status` returns as `tile_version`.
3. A tile requested with the account's own tile version is sent `Cache-Control: private, max-age=31536000, immutable`. A tile requested without one, or with another account's, is sent `Cache-Control: private, no-cache`. No response lets a shared cache keep a tile.
4. After a change on the page itself (FR-3, FR-5), the map fetches the affected tiles again without a reload, as before. After a change made elsewhere (another device, a background sync), a reload or the next app launch shows it.

**Outputs**: Tile responses carrying the caching headers above; `tile_version` in `GET /v1/coverage/status`.

**Notes**: Caching is per device, not shared: the app's origin is not behind a CDN, and a tile points at where the account's owner has been. `IMPLEMENTATION.md` §4.2.6 covers how the version is kept.

### FR-4.12 Light and dark theme

**Description**: Every page and the Android app draw in a light or a dark palette. By default they follow the device's own light/dark setting; the user can instead pin this device to Light or Dark.

**Preconditions**: None to follow the device setting; an active session to change it on the web (the control is on the Settings page, FR-1.7).

**Behavior**:
1. On the web, the Settings page has a Theme control: System (the default), Light, Dark. A choice applies at once, without a reload, and is kept by this browser only — not on the account, and not on the user's other devices; every page opened afterwards, the map included, draws in it.
2. In the Android app, Settings has the same three-way Theme control, applied at once and kept on the phone only.
3. With System chosen, a change of the device's own setting applies while the page or app is open.
4. The map follows the theme: the light basemap flavor in the light theme, the dark flavor in the dark one (FR-4.5), with Fog of War's veil switching to match (FR-4.2).
5. A page opened with a saved dark choice draws dark from its first frame, never flashing light first.

**Notes**: A signed-out visitor gets the device setting; the control is only on the signed-in Settings page. `IMPLEMENTATION.md` §4.18 covers the two palettes.

### FR-4.13 Trails and bike paths

**Description**: The base map's cycleways and walking/hiking paths drawn as lines of their own, standing out from the streets, where otherwise they are barely visible hairlines. They are three entries of the Layers menu under Paths — **Trails**, **Tracks** and **Bike paths**.

**Preconditions**: Active session. The Layers menu is beside the map-mode toggle (FR-4.1–FR-4.3) — in the Android app, an icon button under the menu button — and is hidden while the Edit window is open (FR-5.10), like that toggle. The Layers button opens a panel of three groups: Base map (FR-4.14), then checkboxes under Paths and Points of interest (FR-15.2). The button shows how many paths and places are on; a press on the button again or outside the panel, or Escape (Back on Android), closes it. Tracks has an info button beside it that shows, under the entry, what tracks are — dirt, farm and forest roads, unpaved and wide enough for a vehicle — and hides it again on a second press; it doesn't tick the box.

**Behavior**:
1. Off by default. All three start at zoom 13, the zoom points of interest start at too (FR-15.2), so paths and places appear and disappear together. Bike paths draws cycleways as a solid blue line, Trails draws paths, footways and bridleways as a dashed green line, and Tracks draws OSM's `highway=track` — dirt, farm and forest roads — as a longer-dashed, slightly wider brown line, under the trails; each on its own, both widening as the map zooms in. The monochrome flavors (FR-4.5) draw all three in greys, telling them apart by the dash and width.
2. The choice is kept by this browser (or, in the Android app, this phone) only, not on the account, and applies again on the next visit. A browser that had the former single web toggle on, or a phone that had the former Trails & bike paths toggle on, starts with all three on, and one whose saved choice predates Tracks shows tracks whenever it shows trails.
3. It is independent of the map mode: it works over Normal, Fog and Heatmap alike, and survives a theme change (FR-4.12).
4. An export (FR-4.10) draws each kind of path when it's on and leaves it out when it's off.

**Notes**: It shows what OpenStreetMap tags as a path and is already in the base map, nothing more. Sidewalks, crossings, steps and pedestrian areas keep the base map's own faint styling, since in a city they would bury the real paths. Bike lanes painted on a street, and named routes such as long-distance cycle or hiking networks, are not in the base map and are not shown.

### FR-4.14 Satellite mode

**Description**: Satellite imagery as a second base map, with the vector base map's roads, boundaries and labels drawn over it. It is the **Base map** choice at the top of the Layers menu (FR-4.13), **Map** or **Satellite**.

**Preconditions**: Active session. The deployment configures imagery (`IMPLEMENTATION.md` §4.26); without it there is no Base map section, and a saved Satellite choice shows the vector map.

**Behavior**:
1. Map by default. Choosing Satellite shows the imagery in place of the base map's background, land, water, landuse and building fills; roads stay on top at 40% opacity, so the ground shows through them; boundaries and place labels stay as they are, and so do tracks, Fog, Heatmap, trails and bike paths (FR-4.13) and Spots (FR-15). Choosing Map puts the fills back.
2. Switching doesn't reload the map: camera, mode and overlays stay as they were.
3. It is independent of the map mode and survives a theme change (FR-4.12). Over imagery, Fog uses the cream veil the dark theme's base map has, whatever the theme.
4. The choice is kept by this browser (or, in the Android app, this phone) only, not on the account, and applies again on the next visit. The Layers button's count (FR-4.13) doesn't include it.
5. The imagery provider's credit appears in the map's attribution while the imagery is on.
6. An export (FR-4.10) with Satellite on draws the imagery, with the provider's credit baked in after OSM's and the watermark in its light colors.

**Notes**: The imagery is fetched from a metered provider (ADR-0022). On a quota-capped plan such as MapTiler's Free plan, the imagery stops loading once the month's quota is used up and returns the next month; the roads and labels still draw.

### FR-4.15 Zoom levels at a glance

**Description**: What the map draws at each zoom level, in one place. Each requirement it summarizes remains the authority for its own feature: tracks FR-4.1, Fog FR-4.2, Heatmap FR-4.3, paths FR-4.13, satellite FR-4.14, points of interest FR-15.2 and FR-15.5. Paths and points of interest show only for what is ticked in the Layers menu; with nothing ticked they draw at no zoom.

| Zoom | A 1280 px-wide view spans | Tracks (Normal) | Fog and Heatmap | Paths (ticked) | Points of interest (ticked) |
| :-- | :-- | :-- | :-- | :-- | :-- |
| 0–3 | A continent to the world | — | Whole countries | — | — |
| 4 | Several countries (~6,000 km) | Drawn | Whole countries | — | — |
| 5–7 | A country to a state (~3,000–800 km) | Drawn | States and regions | — | — |
| 8–9 | A state to a region (~400–200 km) | Drawn | Street level | — | — |
| 10–12 | A metro area to a city (~100–25 km) | Drawn | Street level | — | On request: **Show in this area** |
| 13 and up | A few neighbourhoods or less (~12 km and less) | Drawn | Street level | Drawn | Drawn, loaded as the map moves |

**Behavior**:
1. Each band starts at its first zoom and runs up to, not including, the next band's: zoom 7.9 is still States and regions, and zoom 12.9 still offers Show in this area.
2. Zoomed in far enough, the map runs out of stored detail and enlarges the most detailed level it has instead: tracks, Fog, Heatmap and points of interest past zoom 14, the base map past zoom 15, and satellite imagery (FR-4.14) past the deployment's deepest level (zoom 18 on holdmytrack.com). Lines, labels and badges — the base map, tracks, paths and points of interest — stay sharp when enlarged, though a track's shape gets no more detailed; Fog, Heatmap and satellite imagery are pictures and grow softer.
3. The Android app follows the same bands for tracks, Fog, Heatmap, paths and points of interest.

**Notes**: The widths are at the equator; further from it the same zoom spans less ground (at Ohio's latitude, about a quarter less).

## 7. FR-5 — Activities Panel

### FR-5.1 Activity list

**Description**: A permanent sidebar lists every activity within the currently selected date range (FR-6), narrowed by the TYPE and DISTANCE filters below.

**Behavior**: Each row's primary line is the activity's own name if one has been set (FR-5.10), or its start date/time otherwise — an activity has a name only once a person has typed one in via FR-5.10's edit dialog, never from parsing a source file. A row whose primary line is a name still shows its date/time as part of the row's secondary line, alongside distance, duration, and type — a row's only other elements are its checkbox and this text; there is no separate per-row column or icon of any kind, and no per-row action controls (edit/delete/hide are reached via FR-5.6's checkbox plus the header toolbar, not from the row itself). Regardless of what a row displays, **the list itself is always ordered by start date/time, newest first** — a name never affects sort order. The list is not paginated — every matching activity is shown at once. The panel also shows a running count of matching activities and total distance for the range (independent of the TYPE/DISTANCE filters, which narrow the visible rows without changing this total).

**Story badge**: A row whose activity is in a Story (FR-14) carries a badge beside the Pending and Hidden ones (FR-5.15), in the accent color: "Story" for one, "2 stories" for more, its tooltip naming them — "In a story: Brecksville Reservation". Under the open Story on the Stories tab (FR-14.6) that Story doesn't count, since every row there is in it: a row there is badged only for its other Stories ("Also in: …"). Clicking the badge opens the Story on the Stories tab (FR-14.6) — its tooltip then says so, "Open “Brecksville Reservation”" — or, for more than one, opens a small menu of their names under it, and picking one opens that Story; Escape or a click outside closes the menu. On a phone the expanded sheet collapses as the Story opens, so its tracks can be seen. `GET /v1/activities` carries each row's Stories, newest first, as `stories` (`[{id, name}]`, empty for none).

### FR-5.2 TYPE filter

**Description**: The user can narrow the visible activities to one or more activity types (e.g., "Run," "Ride"), via a Type dropdown on the panel's filter row, beside the Distance slider (FR-5.3).

**Behavior**: Clicking "Type" — at the left of the filter row, the Distance slider (FR-5.3) taking the rest of that row, between the date slider (FR-6) and the header toolbar — opens a dropdown listing every distinct type present in the current date range (not a fixed, predefined list) as a checkbox — checked means shown, unchecked means excluded. The list scrolls rather than collapsing behind a "show more" control once it's long enough to need one. Filtering is purely client-side over the already-fetched list; it narrows the Activities panel, the map's drawn tracks, and the header's activity count. The dropdown closes on an outside click, Escape, or clicking its own trigger again.

### FR-5.3 DISTANCE filter

**Description**: The user can narrow the visible activities to a minimum/maximum distance range via a slider, bounded by the shortest and longest activity in the current date range. Same scope as FR-5.2 (client-side, narrows the same things). Standalone and always visible, between the date slider and the header toolbar — not folded into the Type dropdown or hidden behind any toggle.

**Behavior**: Each knob reaches the exact shortest and longest distance at its end of the track, so the activities at either end are inside the filter there. With both knobs back at their ends there is no distance filter: the readout says "any distance" and nothing is filtered out by distance.

### FR-5.4 Row hover preview

**Description**: Hovering a row (anywhere on it) previews that activity's track on the map — drawn thicker, FR-4.1's Hovered state — with no camera movement. The preview clears the instant the pointer leaves the row. This works in both directions: hovering an activity's track directly on the map previews it the same way, and additionally underlines that row's title in the Activities panel — so either surface can be used to identify which row an unlabeled track on the map belongs to, not only the reverse.

### FR-5.5 Row click — focus and fly

**Description**: Clicking a row's text — or clicking that activity's track directly on the map (FR-4.1) — sets it as the single row-click *focus*: highlights it and flies the camera to fit it, so the activity spans 75% of the map along whichever axis is tighter (capped at zoom 18 for a very short track). Whichever activity was previously focused this way loses its highlight (only ever one activity is "just clicked" at a time, regardless of which surface the click came from). This is independent of FR-5.6: neither a row-text click nor a map click ever checks or unchecks any checkbox, in either direction. Clicking empty space in the Activities panel's list — below the last row, or on its "no activities" note — clears the focus, the same as clicking the map away from every track (FR-4.1 behavior 4); the checked group is untouched. While nothing is checked, the focused activity is what the header toolbar acts on (FR-5.7).

**Notes**: If the clicked activity has no recorded track (e.g., a source with no GPS), no fly occurs, since there is nothing to fit the camera to. The pace-colored segments (FR-4.8) shown for a single focused activity are driven by this mechanism specifically, not by FR-5.6.

### FR-5.6 Checkbox — build a group

**Description**: Each row also has a checkbox that adds or removes it from the current checked group without replacing the rest of it, for building a multi-activity group for the header toolbar to act on (FR-5.7). Checking a second row never unchecks the first. A checkbox click changes only the checkbox: it doesn't emphasize the track on the map or highlight the row (FR-4.1's track states), and it never moves the camera — Focus on map (FR-5.7) flies to the group when wanted. This is independent of FR-5.5 in both directions: checking a box never sets or clears the row-click focus.

**Notes**: A hidden activity (FR-5.8) can still be focused (FR-5.5) or checked; the system excludes hidden activities from the fly-to bounds specifically so the camera never flies to an area with nothing drawn on it.

### FR-5.7 Select all / Clear / Invert selection / Focus on map

**Description**: A master checkbox in the header toolbar selects or clears every currently listed activity at once, a ▾ beside it opens a menu of **All**, **None** and **Invert**, and a separate toolbar icon flies to fit the toolbar's target on demand.

**Toolbar target**: The header toolbar's actions — Show/hide (FR-5.12), Edit (FR-5.10), Delete (FR-5.11) and Focus on map — act on its *target*: the listed activities in the checked group (FR-5.6) whenever at least one is checked, otherwise the row-click focus (FR-5.5) alone. With neither, they're disabled. Checked wins rather than the two combining: a focused row is never swept into an action on a checked group, and focusing a row never takes the toolbar away from a group already checked. Each action's tooltip names its target — "3 checked activities", or the focused activity's own name (or date/time) — and the footer's "N selected · X km" summary describes the same target.

**Behavior**: The header checkbox reflects the checked group's state against the currently listed (TYPE/DISTANCE-filtered) rows — checked once every listed row is checked, unchecked once none are, and indeterminate for a partial selection. Clicking it when unchecked or indeterminate checks every listed row; clicking it when fully checked empties the checked group. The ▾ beside it, disabled when no activity is listed, opens a menu under the checkbox: **All** checks every listed row (disabled when every one already is), **None** empties the checked group (disabled when it's empty), and **Invert** checks every listed row that wasn't checked and unchecks every one that was — a checked activity that isn't currently listed (excluded by TYPE/DISTANCE) ends up unchecked. Picking one closes the menu, and so do a click outside it and Escape. None of these moves the camera, the same as a single checkbox (FR-5.6). The toolbar's accent-tinted **Focus on map** icon, disabled when there is no target, flies to fit the target without changing it — the one way to fly to a checked group, and a way back to the focused activity after panning away from it.

**Notes**: None of these controls affects the row-click focus (FR-5.5) — a focused row keeps its own highlight regardless of the header checkbox, its menu, or Focus on map.

### FR-5.8 Hide/show a track

**Description**: The header toolbar's Show/hide icon (FR-5.12) hides or shows the tracks of the toolbar's target (FR-5.7) — the checked group, else the focused activity. There is no per-row hide/show control — hiding or showing a single activity means focusing it (FR-5.5) or checking just its own box, the same as any other single-item action. A hidden activity's track is not drawn in Normal mode until shown again — Fog of War and Heatmap ignore the hidden set (FR-4.2, FR-4.3); its row dims in place and carries a "Hidden" badge so its hidden state is still visible at a glance. Hiding/showing is purely client-side and does not refetch data. See FR-5.12 for the exact toggle rule.

### FR-5.9 Resizable panel

**Description**: The Activities panel can be resized by dragging its right edge, between 260 and 560 pixels wide (default 380). Not persisted across reloads.

### FR-5.10 Edit activity type, name, and description

**Description**: A signed-in user renames an activity's type, gives it a name, and/or attaches a free-text description to it — for example, re-labeling an activity a fitness tracker logged under the wrong category, naming a road trip so it's identifiable in the Activities panel at a glance, or describing a non-sport GPS trace in more detail than a name allows.

**Preconditions**: Active session; the caller owns the activity.

**Inputs**: The Edit window's **Activity** tab, reached via the header toolbar's Edit icon over its target (FR-5.7) — the checked group, else the focused activity — there is no per-row edit control. The same window's **Track** tab is FR-5.14. Editing exactly one activity accepts a new type (required, 1–50 characters), a name (optional, up to 200 characters), and a description (optional, up to 2000 characters). Editing more than one activity at once accepts only a new type — the Name and Description fields are disabled, since there is nothing consistent to set across several different activities' names/descriptions in one request.

**Behavior**:
1. Clicking the toolbar's Edit icon opens the Edit window over its target over the top-left of the map, on its Activity tab; the map doesn't move. It has two tabs, **Activity** and **Track** (FR-5.14), and one **Save** and one **Cancel** shared by both: switching tabs keeps whatever is unsaved in either. While it's open the Activities panel (its date slider included) and the map-mode toggle are inert, and clicking a track on the map does nothing. With exactly one activity in the target, it is pre-filled with that activity's current type, name, and description, all three editable. With more than one, only the type field is editable, seeded from the first of them; the Name and Description fields render disabled with an explanation of why.
2. The type field is plain free text — the same "whatever the source reports, not a controlled vocabulary" rule FR-5.2's TYPE filter already follows (`IMPLEMENTATION.md` §4.7.2) applies equally to a manual rename. The field is a searchable picker, like Settings' Country and Timezone (FR-1.7): opening it shows this account's existing types, each with how many activities use it, and typing filters that list. It is only a convenience — nothing is enforced against it: whenever the typed text doesn't exactly match an existing type (ignoring case), the list also offers an "Add" row for that text, and picking it saves the value exactly as typed, even one nobody has used before. The name field is always plain free text, with no source to ever populate it automatically — an activity has a name only once a person types one in here (`IMPLEMENTATION.md` §4.7).
3. Save commits the fields in one request per activity — only when at least one of them differs from what's saved. For a single activity, all three fields commit together. For a group, the Type field starts on the type its activities share, or empty with "Mixed types — pick one to set it for all" when they don't; left empty, Save keeps each activity's own type. Once a type is set, each activity's own request carries it alongside that activity's own existing name and description unchanged — a group edit never touches Name or Description, even though the underlying request is a full replace. Save then applies the Track tab's edit if there is one (FR-5.14 behavior 5) and closes the window. If the fields save but the track edit fails, the window stays open showing the error; saving again retries only the track edit. Cancel, or Escape, discards every unsaved change on both tabs and closes the window.
4. Once saved: each affected row's displayed type updates immediately; the new/renamed type becomes (or remains) a real entry in FR-5.2's TYPE filter with a live count; a single-activity edit's row shows the name in place of its start date/time if one is set, or the start date/time as before if the name is cleared (FR-5.1); and its description becomes visible as a hover tooltip on the row — not a second visible line. **The Activities panel's sort order never changes**: rows stay ordered by start date/time (FR-5.1) regardless of what a row displays or whether it has a name at all.

**Outputs**: Each edited activity's `activity_type` is updated (plus `name`/`description` for a single-activity edit); every other computed value for that activity (distance, duration, its Fog-of-War/Heatmap coverage, its inclusion in FR-9's performance-analysis aggregates) is unaffected, since none of those are keyed on type, name, or description.

**Error cases**:
- Empty or over-length type, an over-length name, or an over-length description → `400 Bad Request`, no change applied for that activity.
- An activity does not exist or belongs to another account → `404 Not Found`, indistinguishable from each other.

### FR-5.11 Delete an activity

**Description**: A signed-in user permanently deletes one or more of their own activities — a full purge, not a soft delete or an archive: each activity itself, its track, and its contribution to Fog-of-War/Heatmap coverage are all removed. There is no undo. Deleting a single activity and deleting a group are the same mechanism (FR-5.13) — there is no separate per-row delete control; deleting one activity means focusing it (FR-5.5) or checking just its own box.

**Preconditions**: Active session; the caller owns every activity in the toolbar's target.

**Inputs**: The toolbar's target (FR-5.7) — the checked group (FR-5.6), else the focused activity (FR-5.5) — reached via the header toolbar's Delete icon.

**Behavior**:
1. Clicking the toolbar's Delete icon opens a confirmation dialog naming its target — how many activities are checked, or the focused activity's own name or date/time — and their combined distance, stating plainly that this can't be undone; nothing is deleted until the user confirms.
2. Confirming removes every activity in the target and everything derived from each one: its recorded stream data, its rendered coverage masks, and its photos (FR-16).
3. The Fog-of-War/Heatmap view updates to reflect the deletion — coverage a deleted activity was the only source for reverts to unrevealed, not left showing stale coverage for data that no longer exists. An open page picks this up on its own once the background re-render finishes, without a reload.
4. Canceling the confirmation, or dismissing it, leaves every activity untouched.

**Outputs**: Every deleted activity, and everything derived from it, no longer exists; every list, filter, total, and aggregate that previously included it reflects the removal, in one combined refresh rather than once per deleted activity.

**Error cases**:
- An activity in the target does not exist or belongs to another account → `404 Not Found`, indistinguishable from each other.

### FR-5.12 Group visible

**Description**: An icon-only header toolbar button toggles whether every activity in the toolbar's target (FR-5.7) is drawn on the map, in bulk — this is FR-5.8's entire hide/show mechanism, applied to the target, one activity or many.

**Preconditions**: The toolbar has a target — at least one listed activity is checked, or one is focused; the button is disabled otherwise.

**Behavior**: If any activity in the target is currently hidden, clicking shows all of them (removes them from the hidden set). If every one is already visible, clicking hides them all instead.

**Outputs**: The hidden-activity set updates; the map's drawn tracks reflect it immediately (Fog-of-War/Heatmap coverage doesn't change — FR-5.8).

### FR-5.13 Delete group

**Description**: The header toolbar's Delete icon — see FR-5.11, which this number and FR-5.11 both describe: deleting one activity and deleting several are the same mechanism, not two.

### FR-5.14 Edit track

**Description**: A signed-in user removes unwanted recorded points from one of their activities — typically the stretch recorded after forgetting to stop a GPS recorder (wandering a mall, sitting at home), which inflates distance and duration and draws noise onto the map and into Fog-of-War/Heatmap coverage. Three edits are offered: Chop (keep only the part between two points), Cut (remove the part between two points and join them), and Delete point (remove single points). The original recording is never modified, so an edit can always be reset.

**Preconditions**: Active session, not a demo account; exactly one activity is checked (FR-5.6); that activity has a recorded track, isn't a superseded duplicate (FR-3.7), and isn't already Pending (behavior 7 below). The Edit window's Track tab is disabled otherwise, with a tooltip saying why.

**Inputs**: The **Track** tab of the Edit window (FR-5.10), opened with the toolbar's Edit icon over a single activity (FR-5.7's target: one checked activity, or the focused one when nothing is checked); then, inside the tab, a two-knob range slider, the Chop/Cut/Delete point/Undo/Reset buttons, clicks on points on the map, and the window's shared Save and Cancel.

**Behavior**:
1. Opening the Track tab for the first time flies the map to the activity (the same fit as FR-5.5's row click), hides every other activity's track, and draws this one from its full-resolution recorded points — every point visible — rather than the simplified display track. The points are the ones the activity is processed from: already clipped against the account's current Private locations (FR-8.1), so the hidden ends are never sent to the client. An activity entirely inside Private locations has no points to show and can't be edited. The other tracks stay hidden, and the edits made so far stay in place, until the window closes — switching back to the Activity tab keeps them. Delete point mode turns off on leaving the tab, and Cmd/Ctrl+Z undoes a track step only while the Track tab is showing.
2. The slider's two knobs start at the track's ends and are positioned along the track in recording order (point by point, not by distance — a stationary stretch is many points over almost no distance). The readout shows each knob's distance from the start and its clock time. Moving the knobs only previews: the part outside the knobs is drawn dashed and faded, and hovering Cut switches the preview to the part between them, with the two knob points joined straight across.
3. **Chop** keeps only the points from one knob to the other, inclusive. **Cut** removes the points strictly between the knobs, keeping both knob points and joining them. **Delete point** toggles a mode in which clicking a point on the map removes it. Each is one step; after Chop or Cut the knobs return to the new track's ends. None of them can leave fewer than two points — the buttons are disabled, and a point click is ignored, when it would.
4. **Undo** reverts exactly one step, and can be repeated back through every step of the session to the track as it was when the editor opened (also Cmd/Ctrl+Z). **Reset** (shown whenever the track currently has any edit, including one saved in an earlier session) returns to the track as originally recorded, as one more undoable step. The window's **Cancel** discards every step of the session, along with any unsaved Activity tab change, restores the map, and closes the window; nothing is saved.
5. The window's **Save** sends the session's net result as one edit, if there is at least one step, after saving the Activity tab's fields (FR-5.10 behavior 3), and closes the window; with no step, the track is left as it is. The server stores it and reprocesses the activity in the background from its original recording: distance, duration, moving time, elevation gain, start time (a Chop can move it), the display track, per-point stream data, its Fog-of-War/Heatmap coverage (FR-4.2/FR-4.3), and its country/region matches (FR-4.2's zoomed-out tiers) are all recomputed. A Cut's joining segment counts toward distance; the time gap it spans counts toward elapsed duration but not moving time. The edited track's ends are clipped against Private locations again (FR-8.1 behavior 1): an edit that removes the last point outside one at either end — a stray GPS fix beside home, or a Chop — leaves the points still inside it hidden, the track starting or ending on that circle's edge. The editor's preview doesn't show this re-clip; the saved track does.
6. Reprocessing does not re-run cross-source duplicate detection (FR-3.7): the edited activity stays whichever copy it was.
7. Until reprocessing finishes the activity is **Pending** (FR-5.15): its row shows a Pending badge with its pre-edit numbers, its text and checkbox are disabled, and the Edit window's Track tab and Delete are unavailable for it; its track isn't drawn, and it drops out of Fog of War and Heatmap (Country/Region tiers included). The Activities panel re-reads the list every few seconds while any row is Pending. When it clears, the row, the totals, the date slider's days and the drawn track update, and the Fog-of-War/Heatmap layers (at every zoom tier) follow once their re-render lands — all without a page reload, and without moving the camera. If reprocessing fails, the activity simply stops being Pending and keeps its previous track and numbers.
8. An edit is stored as ranges and points identified by recording time, not by position in the point list, so it keeps meaning the same points if a Private location later changes. A track whose timestamps are missing or run backwards can't be edited.

**Outputs**: The activity's stored edit (none, after a Reset) and every value derived from its points, as listed in behavior 5.

**Error cases**:
- The activity doesn't exist, belongs to another account, is a superseded duplicate, or already has an edit Pending → `409 Conflict` on Apply, indistinguishable from each other; opening the editor on an activity that doesn't exist or belongs to another account → `404 Not Found`.
- The activity has no stored original recording, or its timestamps are missing or run backwards → `409 Conflict` when opening the editor, shown in the editor window.
- A range whose start is after its end → `400 Bad Request`.
- An edit that would leave fewer than two points (possible only through the API directly) → accepted, then fails during reprocessing; the activity is left as it was.

### FR-5.15 Activity states

**Description**: Every activity listed in the Activities panel is in exactly one of three states. They describe whether and how it takes part in the map, separately from the per-track display states (Hovered, Focused — FR-4.1's *Track states*), which apply only to a Normal activity.

| State | Entered by | Left by | Track on the map | Fog of War / Heatmap | Its row | Camera | Kept across reloads |
| :-- | :-- | :-- | :-- | :-- | :-- | :-- | :-- |
| Normal | default | — | drawn (FR-4.1) | counted | plain | flies to it on the user's own actions (FR-5.5–FR-5.7) | — |
| Pending | a track edit saved from the Edit window (FR-5.14), or a Private location change that could clip it (FR-8.1) | its reprocess finishing, or failing | not drawn | not counted, at any zoom tier | Pending badge, pre-reprocess numbers, disabled | never flies to it, and doesn't move when it enters or leaves Pending | yes — it's the server's state |
| Hidden | Visibility off (FR-5.8, FR-5.12) | Visibility on, or a new date range (FR-6.6) | not drawn | counted — Fog and Heatmap ignore the hidden set (FR-4.2, FR-4.3) | dimmed, "Hidden" badge | excluded from every fly-to fit | no — this tab only |

**Behavior**:
1. Pending takes precedence: an activity hidden before it went Pending is simply not drawn either way, and is still Hidden once Pending clears — reprocessing never changes the user's own Visibility choice.
2. A Pending activity's row can't be focused onto the map or checked (a row already checked stays checked); its absence is what the map, Fog of War and Heatmap all show until its reprocess lands.
3. Leaving Pending brings the activity back everywhere — its track at once, Fog of War and Heatmap once their re-render lands (FR-5.14 behavior 7) — without moving the camera.

**Notes**: An activity entirely inside Private locations (FR-8.1 behavior 3) is Normal with no geometry — nothing to draw or count — rather than a fourth state; superseded duplicates (FR-3.7) are never listed at all.

### FR-5.16 Add to story

**Description**: A book-with-a-plus icon in the Activities panel's header toolbar, between Edit and Delete, puts the toolbar's target (FR-5.7) — the checked activities, else the selected one — into a Story (FR-14): a new one, or one that already exists. This is where activities go into Stories; they come out on the Stories tab (FR-14.6).

**Preconditions**: A real account (a demo session sees the icon disabled, its tooltip saying the demo can't make stories); a target. With no target the icon is disabled, its tooltip saying to select or check activities first, like the toolbar's other actions; with one, the tooltip names it — "Add 3 checked activities to a story".

**Behavior**:
1. The icon opens a menu under the toolbar: **New story…** first, then every Story of the account, newest first, each with how many activities it holds. The Stories are read each time the menu opens. A Story that already holds all of the target is ticked and can't be picked.
2. Picking a Story adds the target to it in one request (FR-14.3) — any of them already there stay — and closes the menu. The list stays as it is; the rows' Story badges (FR-5.1) follow.
3. **New story…** opens a Create story dialog over the map naming what it's made of — "A story of 3 checked activities (58 km). Only you can see it." — with Name (required, up to 200 characters) and Description (optional, up to 2000). "Create story" stays disabled until Name has something besides spaces; Enter in Name does the same as clicking it. Cancel, Escape or a click outside closes the dialog with nothing made.
4. Creating makes the Story with the target's activities in one step (FR-14.2) and opens it on the Stories tab (FR-14.6).
5. The menu closes on Escape, a click outside it, or its icon clicked again, and whenever the target changes.

**Error cases**: A failed add keeps the menu open with the server's message at its foot. A failed create keeps the dialog open with the server's message under the fields, the fields as typed.

## 8. FR-6 — Date Range Slider

The date range is picked with a two-knob slider at the top of the Activities tab (FR-5), above the Type and Distance filters, with Earlier/Later buttons either side and the selected start and end dates under it. It exists on that tab alone: the Stories and Privacy tabs (FR-14.6, FR-8.1) are not filtered by a date range, and neither are Fog of War and Heatmap (FR-4.2, FR-4.3). Switching to another tab and back keeps the range as it was. On a phone it sits at the bottom of the screen instead (§19 item 2).

The slider counts only days with at least one activity; days without one take no room on the track. The track shows a window of 15 activity days, which on load is the 15 most recent. A selection is applied when a knob or a held button is released, not while it moves.

### FR-6.1 Default selection

**Description**: On first reaching the map with no prior selection, the range defaults to the 5 most recent days that have at least one activity (not the account's entire history) — what opening the map is usually about, with both knobs inside the slider's window. An account with no activity history at all defaults to "today" only, and the default is recalculated automatically as new activities arrive until the user makes their own explicit choice (moving a knob, or paging with one pulled along). While the Edit window (FR-5) is open the default holds still, so activities still arriving from a large upload never move the activity being edited out of the range and close the window; it catches up once the window closes.

### FR-6.2 Move a knob

**Description**: The knobs mark day boundaries: the start knob where the first selected day begins, the end knob where the last one ends — so a one-day selection has its knobs one activity day apart, and the knobs can never be dragged closer than that. Pressing the track moves whichever knob is nearer to the pressed day boundary, and dragging carries it, always within the window. Each knob is also keyboard-operable: arrow keys move it one activity day, Page Up/Down five, Home/End to the window's edges.

### FR-6.4 Page through history

**Description**: Earlier and Later move the window 5 activity days per press, repeating while held, and stop at the account's first and most recent activity day. A knob sitting on the edge the window moves toward is pulled along with it, extending the selection by up to 5 activity days per step — so a selection longer than the window is made by parking a knob on an edge and pressing or holding that edge's button. Otherwise paging never changes what is selected: a knob that scrolls out of view is simply not drawn until it comes back, and its date stays in the label under the track.

### FR-6.6 Changing the selection resets dependent state

**Description**: Committing a new date range (via FR-6.2 or FR-6.4) clears both the row-click focus (FR-5.5) and the checked group (FR-5.6) independently, the hidden-activity set (FR-5.8), and both the TYPE and DISTANCE filters (FR-5.2/FR-5.3) together — all of these could otherwise silently describe activities the new range no longer lists. The camera flies to fit the new range's drawn activities once, when its list arrives. Only a range the user picked moves it: an upload or sync landing never does — not when it refreshes the same range, and not when it shifts the automatic default range (FR-6.1) — and neither does a Pending activity (FR-5.15) finishing or a Private location change.

## 9. FR-7 — Activity Graph (Profile)

### FR-7.1 Contribution grid

**Description**: A private, per-account page at `/profile` (reached from the header's account menu) showing a GitHub-style daily contribution grid — one cell per calendar day, one block per calendar year (most recent first, back to the account's first-ever activity).

**Behavior**: Each day's cell is shaded by intensity, toggle-able between two measures (the "Shade by" switch — `/profile?shade=distance` for Distance, the plain `/profile` for Count):
- **Count**: number of activities that day (empty / one / two / three-or-more).
- **Distance**: quantile-based thresholds computed over that account's own active days for that year (so "a busy day" is relative to this account's own typical distances, not a fixed absolute number).

Hovering a day shows its date, activity count and distance. The page needs a session like the map does: no session goes to `/signin`, an unverified account to `/verify-pending`, and an account that has never saved Settings to `/settings` (FR-1.7).

### FR-7.2 All-time and per-year stat cards

**Description**: Four statistics — Activities, Distance, Active Days, and Longest Streak (the longest run of consecutive calendar days with at least one activity) — shown once for the account's entire history, and again as a subtotal under each year's own grid.

## 10. FR-8 — Privacy Controls

### FR-8.1 Private locations

**Description**: A user marks places they don't want their tracks to reveal — home, work — as Private locations: circles on the map, each a center and a radius (50–2000 meters, 200 by default), with an optional name. The part of an activity that starts or ends inside one is hidden everywhere, the user's own map included. Nothing else is trimmed: an activity outside every Private location keeps its full recorded geometry, so consecutive days of a multi-day trail join up with no gap at each day's start and end.

**Preconditions**: An active session. Creating, moving, resizing, renaming, and deleting need a real account (FR-2's demo can see its own, read-only).

**Inputs**: The Activities panel's **Privacy** tab, shown in Normal map mode only (the panel isn't shown in Fog or Heatmap), and also reached by `/?private-locations`, which opens the map on that tab (the parameter is then removed from the URL, so a refresh doesn't reopen it). The tab lists the account's Private locations (name and radius), each row with a Delete button (confirmed first), and a **Create** button that puts a new 200 m circle in the middle of the map (zoomed out past street level, it flies in to that spot first — a circle there would be under a pixel). Clicking a row — or, while the tab is showing, a saved circle or its center dot on the map (every saved location shows a fixed-size center dot at any zoom) — opens that location's editor: a window over the map, like Edit track's (FR-5.14), below the map-mode toggle, where the circle's center drags, a slider sets its radius, and Save or Cancel closes it; on a phone, opening it collapses the Activities sheet. Switching tabs, or away from Normal, drops an unsaved edit. For the demo account the list is read-only: no Create or Delete, and a row only shows its circle. Saved through `GET`/`POST /v1/private-locations` and `PATCH`/`DELETE /v1/private-locations/{id}`; at most 20 per account.

**Behavior**:
1. Applied at ingest, server-side, before anything is stored: the leading points inside any Private location are dropped, and the track starts on that circle's edge instead (a point interpolated onto the boundary, whatever the recording's point density); the trailing points likewise. The same clip runs again on a track edit's result (FR-5.14 behavior 5), so an edit can't uncover an end inside one. A track that only passes *through* a Private location mid-way is shown whole, by design: what a Private location protects is where a track starts and ends, and passing through one reveals neither.
2. An activity's distance, duration, elevation gain, pace, streams, fog, heatmap, and Country/Region matches all come from the visible part only.
3. An activity entirely inside Private locations is still kept, with no geometry: zero distance, nothing on the map, no fog. Deleting or shrinking the location brings it back.
4. Every change to a Private location is retroactive. The activities the old or new circle could clip are marked Pending (FR-5.15, the same badge FR-5.14's reprocessing shows) and reprocessed in the background from their original recorded points; while Pending they are neither drawn nor counted in fog or heatmap. The badges clear once each activity's track and stats are current, and the map refreshes itself — fog and heatmap a moment later, once re-rendered — without moving the camera.
5. The circles are drawn only while the Privacy tab is showing, never in the normal view and never in an Export (FR-4.10).
6. Activities ingested before Private locations existed kept the fixed endpoint trim they were processed with (100 m by default). There's no backfill: one gets its full ends back only when something reprocesses it — an Edit track (FR-5.14), or a Private location change that includes it.

**Outputs**: The account's Private locations; every activity's stored geometry and stats clipped against them.

**Error cases**:
- Radius outside 50–2000 meters, a coordinate out of range, or a name over 100 characters → `400 Bad Request`.
- A 21st Private location → `409 Conflict`.
- An id that isn't one of the caller's own Private locations → `404 Not Found`.
- Any change from the demo account → `403 Forbidden`.

### FR-8.2 Data isolation

**Description**: An account can only ever see its own activities, uploads, and profile data. Every data-returning endpoint derives the account from the caller's session; no endpoint accepts a user or account identifier as a request parameter that could be substituted for another account's. The one exception is the admin panel (FR-12), whose pages take an account id in the path and are shown only to an admin.

## 11. FR-9 — Trends

### FR-9.1 Trends

**Description**: A signed-in account's own activity history, aggregated into weekly or monthly totals, viewable on the Profile page below the activity grid — how much ground was covered over recent weeks or months.

**Preconditions**: The caller has a valid session (real or demo user).

**Inputs**: `bucket` — `week` or `month`; `from`/`to` (`YYYY-MM-DD`, both optional, default to the trailing 12 months).

**Behavior**:
1. Every activity in the window is grouped into the requested bucket by its `started_at` date, in the account's own timezone (FR-1.7), one bucket per calendar week or month that has at least one activity — buckets with nothing recorded are omitted rather than returned as zeroes, the same convention the date slider's days (FR-6) use.
2. Each bucket reports: activity count, total distance, total moving time, and total elevation gain.
3. The Profile page renders the trailing 12 months as one bar per bucket, height scaled (logarithmically) to the window's busiest bucket by distance, with a Week/Month switch (`?bucket=month`; the Distance/Count grid setting is kept). Hovering a bar shows that bucket's full breakdown (distance, activity count, moving time, elevation gain); tapping or clicking one shows it in a line under the chart, and tapping it again hides it.

**Outputs**: `{bucket, from, to, periods: [{period_start, count, distance_meters, moving_seconds, elevation_gain_m}, ...]}`.

**Notes**: "Moving time" falls back to elapsed time for any activity without a moving-time figure of its own, so this bucket-level total uses whichever one each activity actually has, rather than a bucket going silently short. Best-effort curves and personal bests are deliberately out of scope (§1.2, §20).

## 12. FR-10 — Public pages: About, Help, Contacts

These are server-rendered pages (`IMPLEMENTATION.md` §4.19): each is a plain HTML page that needs no JavaScript and makes no API calls from the browser, with the page header every page shares (FR-10.4).

### FR-10.1 About page

**Description**: A public page at `/about` that explains what HoldMyTrack is, who it is for, what it deliberately is not, and how it is funded. A visitor or a search engine can read it without an account.

**Preconditions**: None — no session is needed; having one changes only the header (FR-10.4).

**Behavior**:
1. `GET /about` returns the page. It is also the site's front page: a visit to `/` without a session gets the same page (titled "HoldMyTrack — Every journey, mapped."), and both carry a canonical link to `/`, so search engines index the one URL. With a session, `/` is the map (FR-4).
2. It has sections for: what HoldMyTrack is and what it is for, how it works (three numbered steps: bring what you already recorded, see it on one map, go somewhere new), why someone might want it, what it isn't, how it is funded (section id `funding`), and a pointer to Contacts (FR-10.3).
3. "Try the demo — no signup" links to `/signin`, where the demo starts from its own button (FR-2.1). The page never starts a demo session itself. "Get the Android app" downloads the Android app's installable file, `/download/holdmytrack.apk` (`docs/DEPLOY.md` §10).
4. About, Help and Contacts are reachable from every page's header and footer (FR-10.4) — the map and the sign-in pages included.
5. `/robots.txt` allows crawling except for `/v1/` and `/tiles/`, and points to `/sitemap.xml`, which lists `/`, `/help` and `/contacts` (`/about` is the same page as `/`).
6. The front page carries structured data (a schema.org `WebApplication`, as JSON-LD) naming the site, its URL, description and share image, and that it's free.

### FR-10.2 Help page

**Description**: A public page at `/help` that explains how HoldMyTrack works, in ten sections reachable from jump links under its title: the map (the opening view, the Normal / Fog of War / Heatmap modes, how Fog and Heatmap switch to whole states or regions, then whole countries, as the map zooms out, and the Layers menu's base map and paths — FR-4, FR-4.13, FR-4.14), the date slider (what its slots are, moving a knob, and paging back through history with a knob pulled along — FR-6), editing and deleting activities (anchor `#edit`: the Edit window's Activity and Track tabs, Hide, and Delete — FR-5.10–FR-5.14), Stories (making one, adding and removing activities, the Stories tab, renaming and deleting, the badge — FR-14, FR-5.16), photos (anchor `#photos`: adding them in the Edit window, how each is placed on the route, moving and captioning one, looking at them, and the resized copy kept and the 2,000-photo limit — FR-16), points of interest (anchor `#places`: turning categories on in Layers, where badges and areas show and Show in this area, the popup, capturing a place in the Android app, and that the places come from OpenStreetMap — FR-15), getting activities in (files, `.zip` archives, Google Takeout with a link to Google's own download guide, the Android app with a link to download it, and what happens on a repeated or cross-source import — FR-3), exporting a map image (FR-4.10), the Profile page (anchor `#profile`: the activity grid, its totals, and Trends — FR-7, FR-9), and settings and privacy (Country, Timezone, Language, Theme, Private locations — FR-1.7, FR-4.12, FR-13, FR-8.1 — what HoldMyTrack stores and doesn't: no health data, only the route, and the original upload kept only to rebuild it (`VISION.md` §1.1; anchor `#what-we-store`) — and how to ask for an account to be deleted, which is by email to the Contacts address since there is no self-service deletion yet, and how to revoke Google's or Facebook's access on their side; its `#delete-account` anchor is the deployment's data-deletion instructions URL for Facebook, FR-1.10).

**Preconditions**: None.

**Behavior**:
1. `GET /help` returns the page. It is indexable and listed in `/sitemap.xml`.
2. Its facts are the behavior this document specifies; a change to one of those FRs that the page describes (a limit, a mode's window, a zoom tier) is a change to the page too.

### FR-10.3 Contacts page

**Description**: A public page at `/contacts` with how to reach the project.

**Preconditions**: None.

**Behavior**:
1. `GET /contacts` returns the page, listing the email address `hello@holdmytrack.com` and the GitHub repository `https://github.com/HoldMyTrack/holdmytrack` for code and issues.

### FR-10.4 Page header and footer

**Description**: Every page shares one header — the map and Profile included — and every page other than those two shares one footer.

**Behavior**:
1. The header shows the logo, wordmark and tagline (the brand links to `/`), then Donate (FR-11.1), the language menu (FR-13.2), an "Info" menu listing About, Help and Contacts with the current page marked, and the account area.
2. Signed out, the account area is a "Sign in" link to `/signin`. With a session (real or demo), it is an account menu showing the account's avatar (or a generic icon) that opens to the account's email (a demo session shows its display name instead, followed by "Create your own account", FR-2.3), then "Profile" (`/profile`), "Settings" (`/settings`), "Admin" (`/admin`, an admin only — FR-12.1) and "Sign out".
3. The menus open and close without JavaScript.
4. "Sign out" submits `POST /logout`, which ends the session the same way `POST /v1/auth/logout` does and redirects to `/`. The request is refused (`403`) unless its `Origin` header — or, without one, its `Referer` — is the app's own origin.
5. On a desktop the header's controls are sized to it (about 36px tall, 14px text), and each is an icon with its label — Donate a heart, Upload an arrow, Sync two arrows, the language menu a globe beside the page's language code ("EN"), Info an "i" in a circle; none has a caret, though Upload, the language menu, Info and the account menu open menus. It gives way in steps as the window narrows: at 1180px and below the tagline is hidden; at 960px and below Upload, Sync and Info show their icons alone. On a phone-width screen (≤768px) the controls turn compact, Donate shows its heart alone too, and the language menu its code alone; every control stays. Narrower than 360px, the wordmark is hidden too, leaving the logo. Nothing in the header runs past the screen's edge at any width, in either language.
6. The footer links to the map (`/`), About, Help, Contacts and the GitHub repository.
7. With a session, pages are sent with `Cache-Control: no-store`, since the header names the signed-in account. Without one, the front page, About, Help and Contacts are the same for every visitor and are sent `Cache-Control: public, max-age=300` with `Vary: Cookie`, so a copy cached before signing in is never reused after.
8. An address no page answers gets a "Page not found" page (`404`) with the same header; under `/v1/` and `/tiles/` it's a plain `404`, not a page.
9. Every indexable page carries link-preview tags (Open Graph and a large-image Twitter card), with a 1200×630 share image: a Fog of War map with the HoldMyTrack logo, tagline and a one-line pitch.

## 13. FR-11 — Donations

### FR-11.1 Donate

**Description**: The header's Donate button (FR-10.4) is how a visitor reaches HoldMyTrack's funding — recurring community donations with a public ledger on Open Collective (`VISION.md` §6.1). HoldMyTrack itself takes no payment and stores nothing about a donation.

**Preconditions**: None.

**Behavior**:
1. The button shows a heart icon followed by "Donate"; on a phone-width screen (≤768px) it shows the heart alone.
2. While no Open Collective is configured (the slug is empty), Donate links to About's funding section (`/about#funding`), which says donations aren't open yet.
3. Once one is configured, Donate is a link to `https://opencollective.com/<slug>/donate`, opened in a new tab so the map is kept; choosing an amount, one-off or monthly, and paying all happen on Open Collective.

## 14. FR-12 — Admin panel

A read-only view of every account and every account's activities, for the people operating HoldMyTrack. Server-rendered pages (`IMPLEMENTATION.md` §4.20) with the shared page header (FR-10.4), `noindex`:

| Page | Purpose |
| :-- | :-- |
| `GET /admin` | Every account (FR-12.2) |
| `GET /admin/users/{id}` | One account and its activities (FR-12.3) |

### FR-12.1 Access

**Description**: Only an admin can open the admin panel; to anyone else it doesn't exist.

**Behavior**:
1. An account is made an admin, or stops being one, only from the server's command line: `holdmytrack set-admin <email> true|false`. It fails for an email no account has, and for the demo account. Nothing on the web can make an account an admin.
2. An admin's account menu (FR-10.4) has an "Admin" item, after "Settings", linking to `/admin`. No one else's does.
3. Signed out, signed in as a non-admin, or in a demo session, both admin pages answer with the ordinary "Page not found" page (`404`), exactly like a URL that doesn't exist.
4. An admin whose own account isn't past email verification or first-run Settings is sent there first (FR-1.8, FR-1.7), as on every other signed-in page.
5. Revoking takes effect on the admin's next page load; their session is not ended.

### FR-12.2 Accounts

**Description**: `/admin` lists every account, demo accounts included, newest signup first.

**Behavior**: Each row shows the email (a link to FR-12.3's page), the display name when set, badges for admin, demo and unverified email, the signup date, country, timezone, the number of live activities, the first and last activity's dates, and their total distance. Counts, dates and distance cover live activities only — a duplicate superseded by another copy (FR-3.7) isn't counted. Dates are in that account's timezone, distance in the admin's own units (FR-1.7).

### FR-12.3 An account's activities

**Description**: `/admin/users/{id}` shows one account and every activity it has stored, with each activity's id.

**Behavior**:
1. The header repeats FR-12.2's row, plus the account id.
2. Activities are listed newest first, 100 per page, with "← Newer" and "Older →" links (`?page=`) and a "1–100 of 250" count.
3. Each row shows the full activity id (selected whole with one click), the start date and time in the account's timezone, type, name, distance, duration (hours:minutes), source (`upload`, `takeout`, `healthconnect`, `recorded`, …), and the countries and regions it passes through (FR-4.2's boundary tiers).
4. Unlike every other list in the product, superseded duplicates and activities entirely inside a Private location are included, badged "duplicate of <id>" (linking to that row when it's on the same page) and "hidden"; an activity with a track edit (FR-5.14) is badged "edited".
5. An id that isn't a UUID, or no account's, is `404`. A page past the last shows no rows and a link back to the first.
6. The pages only read. Nothing on them changes an account or an activity.

## 15. FR-13 — Language

### FR-13.1 Which language a person gets

**Description**: HoldMyTrack's interface is in English or Russian. Every page, the map, the emails, and the error messages the web and Android apps show come in one language per request.

**Behavior**:
1. The language is, in order: the account's Language setting (FR-1.7) when it is English or Русский; otherwise the language last chosen on this browser with the header's language menu (FR-13.2); otherwise the language the browser or app asks for (`Accept-Language`, its most preferred supported language, matching `ru-RU` as Russian); otherwise English. URLs don't change with language.
2. The whole page is in that language: the shared header and footer (FR-10.4), every page including About and Help, the map app, `<html lang>`, the page title and description. The map app always shows the language the page around it is in.
3. Numbers and dates follow the language: `1,234.5 km` and `Sep 8` in English, `1 234,5 км` and `8 сент.` in Russian. Units are unchanged by language — they follow Country (FR-1.7).
4. Counts take the language's plural forms (`1 занятие`, `2 занятия`, `5 занятий`).
5. The verification and password-reset emails (FR-1.8, FR-1.5) are in the account's language, or when it has none set, the language of the request that sent them.
6. Error messages a person can see (a wrong password, a taken email, a rejected upload or track edit) are full sentences in the request's language, in page forms and in the JSON API's plain-text and `message` bodies alike. Machine-readable codes (`email_not_verified`, `demo_read_only`) never change with language. An import's failure reason, in the upload history, and the Android app's per-activity sync rejections are in the language of the request reading them, however long ago the import failed.
7. A language chosen in the header's menu (FR-13.2) takes effect from that choice's own reload onward; other open pages change on their next load. Saved from the Android app's Settings, it applies to the web from each page's next load.
8. The Android app follows its own per-app language (Android's Settings → Apps → HoldMyTrack → Language), else the phone's. Its Settings screen's Language sets that per-app language and the account's setting together, and signing in applies the account's language when it is English or Русский (`apps/android/docs/SPEC.md` FR-1.5). It sends its language as `Accept-Language`, so the server's messages match it.

**Not translated**: activity names and descriptions people type, place names on the map, activity types outside the common set (shown as recorded), and the reason a `.zip` entry was skipped.

### FR-13.2 The language menu

**Description**: Anyone can choose the language, signed in or not, from the shared header (ADR-0025).

**Inputs**: The header's language menu (FR-10.4), on every page, signed in or not, and the web's only language control: it shows the page's language code and opens to each language named in itself (English, Русский), the current one bold, then, under a rule, "Automatic (browser)" in the page's language. Choosing one submits `POST /language` with `lang`: `en`, `ru`, or empty for Automatic; it works without JavaScript.

**Behavior**:
1. The browser remembers the choice for a year (cookie `hmt_lang`), and every later page and request from it is in that language, unless a signed-in account's own Language setting says otherwise (FR-13.1).
2. Signed in to a real account, the choice is also saved as the account's Language setting (FR-1.7), on every device. A demo account's choice is remembered on the browser only.
3. Automatic forgets the browser's choice and, signed in to a real account, sets the account's Language setting back to automatic, so the browser's own language decides again (FR-13.1).
4. The browser goes back to the page it was on, query string included, now in the chosen language.

**Errors**:
- A language other than `en`, `ru` or empty → `400`.
- A POST from another site (no matching `Origin` or `Referer`) → `403`, like every page form.

## 16. FR-14 — Stories

A Story is a hand-picked, private set of the account's activities — a hike, a holiday, a race weekend — with a name, an optional description and joint statistics (`VISION.md` §4.2, ADR-0020). An activity can be in any number of Stories. This section covers the API and the Activities panel's Stories tab (FR-14.6), where a Story is opened on the map; a Story is made, and activities go into Stories, with the Activities toolbar's Add to story (FR-5.16); activities come out of a Story from its rows on the Stories tab; and a row's Story badge is FR-5.1.

| Endpoint | Purpose |
| :-- | :-- |
| `GET /v1/stories` | Every Story of the account, newest first |
| `POST /v1/stories` | Create one, optionally with its first activities |
| `GET /v1/stories/{id}` | One Story |
| `PATCH /v1/stories/{id}` | Rename it, or change its description |
| `DELETE /v1/stories/{id}` | Delete it |
| `POST /v1/stories/{id}/activities` | Add activities, `{activity_ids}` |
| `DELETE /v1/stories/{id}/activities` | Remove activities, `{activity_ids}` |

### FR-14.1 A Story

**Description**: What every Story endpoint but `DELETE /v1/stories/{id}` answers with — the list as `{stories: [...]}`.

**Outputs**: `id`, `name`, `description` (`null` when none), `created_at`, `updated_at`, `activity_ids` (every member, earliest activity first) and `stats`: `count`, `distance_meters`, `moving_seconds` and `elapsed_seconds` over the Story's activities, with no date bound, and the same four per activity type in `by_type`, the most frequent type first. Like every other total (FR-3.7), `stats` leaves out a duplicate superseded by another copy, though it stays in `activity_ids`. An activity with no moving time of its own counts its elapsed time as moving, as in Trends (FR-9.1).

### FR-14.2 Create, rename and delete

**Behavior**:
1. `POST /v1/stories` takes `name`, `description` and, optionally, `activity_ids` — the Story's first activities, added in the same request — and answers `201` with the new Story.
2. `PATCH /v1/stories/{id}` takes `name` and `description`, both sent every time: an empty description clears it.
3. Name and description are trimmed. The name is required, up to 200 characters; the description up to 2000.
4. `DELETE /v1/stories/{id}` answers `204`. The Story's activities stay.
5. Renaming, a new description, and a change to the Story's activities (FR-14.3) move `updated_at`.

**Error cases**:
- A blank name, or a name or description over its limit → `400` with a message in the request's language (FR-13.1).
- `activity_ids` naming an activity that isn't the account's own → `404`, and nothing is created.

### FR-14.3 Add and remove activities

**Behavior**:
1. `POST /v1/stories/{id}/activities` with `{activity_ids}` adds them; one already in the Story stays as it is. It answers with the updated Story.
2. `DELETE /v1/stories/{id}/activities` with `{activity_ids}` removes them; an id not in the Story is ignored. The activities themselves stay. It answers with the updated Story.
3. Deleting an activity (FR-5.11) takes it out of every Story holding it. A Story left with no activities stays, with zero statistics.

**Error cases**:
- `activity_ids` empty, over 10,000 ids, or holding an id that isn't a UUID → `400`.
- Adding an activity that isn't the account's own → `404`, and none of the request's activities are added.

### FR-14.4 The `story` filter

**Description**: One Story's activities, through the endpoints that already draw and count the account's history.

**Behavior**:
1. `story=<id>` narrows the activity list (`GET /v1/activities`), its summary (`GET /v1/activities/summary`) and the tracks tiles (`GET /tiles/v1/tracks/{z}/{x}/{y}.mvt`) to the Story's activities, combined with the `from`/`to` and `types` they already take.
2. It narrows the activity-day histogram (`GET /v1/activities/histogram`, both its `days` and its `from`/`to` mode) the same way, and `earliest` becomes the Story's first activity day. The histogram otherwise ignores the list's filters (FR-6); inside a Story, the Story is all there is.
3. Another account's Story, or an empty one, matches nothing: an empty list, zero totals, no days, no tracks.
4. Adding or removing a Story's activities, creating a Story with activities and deleting one that has any all change the account's tile version, so no tile cached before the change is reused for the Story. Renaming doesn't.

**Error cases**:
- `story` that isn't a UUID → `400`.

### FR-14.5 Privacy and the demo

**Behavior**:
1. A Story is visible only to its own account. Another account's Story, a Story that doesn't exist and an id that isn't a UUID are all `404` on every endpoint, indistinguishable from each other; the list holds only the account's own Stories.
2. A demo session reads the Demo Customer's Stories like any other account, and every write — create, rename, delete, add, remove — is refused with `403` `demo_read_only`, as for every other write (FR-2.1).

### FR-14.6 The Stories tab

**Description**: The Activities panel's second tab, after Activities (FR-5), lists the account's Stories and shows one of them on the map at a time.

**Preconditions**: Signed in, on the map in Normal mode (FR-4.1).

**Behavior**:
1. Every Story is a folder row: a triangle, then its name. The triangle points right while the Story is folded and turns to point down when it's open. Stories are ordered by when they were made, newest first. The tab has no Type or Distance filters, no checkboxes and no toolbar.
2. Exactly one Story is open at a time. Opening the tab opens the newest. Clicking another Story opens it and folds the one that was open. Clicking the open Story does nothing, so there is no way to fold them all.
3. The open Story shows its description, when there is one, and then its activities as rows, newest first, like the Activities tab's rows (FR-5.1) but without checkboxes. Hovering a row previews its track (FR-5.4) and clicking one focuses it (FR-5.5). Each row has an × at its end, shown while the pointer is over the row or it has keyboard focus, and always on a phone: it takes that activity out of the Story (FR-14.3) with no confirmation — the activity itself stays, and Add to story (FR-5.16) puts it back. The row leaves the list, its track leaves the map and the footer's statistics follow; if it was focused, the focus clears. A removal that fails shows the server's message above the rows.
4. While a Story is open, the drawn tracks, the listed rows and an exported image (FR-4.10) are all of that Story's activities, whatever date range the Activities tab has; the tab has no date slider (FR-6).
5. The footer at the bottom of the panel shows the open Story's statistics: its activity count, distance and moving time (FR-14.1), then the same per activity type, the most frequent first, each type by its display name. A Story with no activities says so instead.
6. Opening a Story fits the camera to its drawn tracks. Opened by URL, a camera in the URL wins; without one the camera fits the Story. On a phone, opening a Story collapses the panel's sheet so the tracks can be seen.
7. Each folder row has a pencil and a bin, shown while the pointer is over the row or it has keyboard focus, and always on a phone. The pencil opens Edit story, Add to story's Create story dialog (FR-5.16) with Name and Description filled in and Save (FR-14.2). The bin asks "Delete “<name>”? Its activities stay." with a "Delete story" button, then deletes the Story (FR-14.2). Deleting the open Story opens the next newest one.
8. Leaving the tab closes the open Story: the map goes back to the Activities tab's date range, which opening a Story never changes — or the usual default range (FR-6.1) for a page that opened straight into a Story. The camera stays. Coming back to the tab opens the newest Story again.
9. The URL carries the open Story, `/?story=<id>`: a refresh or a shared link opens the Stories tab with that Story open, and Back and Forward move between Stories and to and from the tab. A new Story from Add to story (FR-5.16) opens the same way.
10. Normal mode only: Fog of War and Heatmap stay all-time (FR-4.2, FR-4.3), and returning to Normal shows the open Story again.
11. With no Stories, the tab says how to make one: select or check activities on the Activities tab, then choose Add to story. The map stays as on the Activities tab.
12. A demo session sees the Demo Customer's Stories the same way, with the pencil, the bin and the rows' × disabled.

**Error cases**:
- A `?story=` for a Story that doesn't exist or isn't the account's shows "This story doesn't exist, or isn't yours." at the top of the tab, with no Story open and no activities; clicking a Story opens it.
- A rename or delete that fails keeps its dialog open with the server's message.

## 17. FR-15 — Spots

Outdoor places from OpenStreetMap on the map, in five categories, with what OSM says about each (`VISION.md` §4.2, ADR-0021). Built on the web and in the Android app (`apps/android/docs/SPEC.md` FR-2.8); the places are loaded by the operator (FR-15.1), not by users. Places aren't yet marked visited — `ROADMAP.md` Phase 1.

| Endpoint | Purpose |
| :-- | :-- |
| `GET /tiles/v1/spots/{z}/{x}/{y}.mvt` | The places in one tile, and their areas |
| `GET /v1/spots` | The places in a box — "Show in this area" (FR-15.5) |
| `GET /v1/spots/{id}` | One place with its whole area, and when the caller captured it (FR-15.6) |
| `GET /v1/spots/captures` | The places the caller has captured (FR-15.6) |
| `POST /v1/spots/{id}/captures` | Capture a place (FR-15.6) |

### FR-15.1 Places

**Description**: What a place is, and where it comes from.

**Behavior**:
1. A place is an OpenStreetMap feature in one of five categories: **Playground** (`leisure=playground`), **Dog park** (`leisure=dog_park`), **Monument** (`historic=monument` or `memorial`), **Mesmerizing view** (`tourism=viewpoint`) and **History** (`historic=castle`, `ruins`, `fort` or `archaeological_site`). A feature tagged for two is the first of History, Monument, Mesmerizing view, Dog park, Playground.
2. A place has, each only when OSM has it: a name; an address built from its `addr:*` tags — `addr:full`, or a house number, street (or `addr:place`), city and postcode — when it has at least a street or place; a description; an inscription; a memorial type (a statue, a plaque, a war memorial…); a start date as OSM writes it (a year, a date, "~1850"); and a Wikipedia article, from a `wikipedia` tag in OSM's "language:Title" form (any other form is left out).
3. A place's area is its OSM outline, or a 30 m circle around a place mapped as a single point.
4. The operator loads the places with `holdmytrack import-spots <file>`, from an extract made off the server (`docs/DEPLOY.md` §6). Loading the same place again updates it rather than adding a second one; a place missing from a newer extract stays.

### FR-15.2 Points of interest

**Description**: The places on the map, one Layers entry per category.

**Behavior**:
1. The Layers menu's Points of interest group (FR-4.13 describes the menu) has one checkbox per category — Playgrounds, Dog parks, Monuments, Mesmerizing views, Historic sites. Each works the same over Normal, Fog and Heatmap. All start off; each browser (or, in the Android app, each phone) remembers its choice, and a browser that had the former Show POI toggle on starts with every category on.
2. From zoom 13 up — where the paths (FR-4.13) start too — each place in a ticked category is a round badge — its category's icon in ink on white, ringed in gold — at a point on its area, loaded as the map moves. Between zoom 10 and 13 places show only on request (FR-15.5); below zoom 10, none. Every place is drawn, however close to others. Playground's icon is a seesaw. A place the account has captured (FR-15.6) has a filled badge instead — its icon in white on gold, ringed darker — on the web and in the Android app alike; the web reads the account's captures again whenever the page comes back into view or its window gets focus.
3. From zoom 13 up, under each badge its area is shaded faintly in gold: the place's outline from OpenStreetMap, edged with a solid line, or — for a place mapped only as a point — its 30 m circle, edged with a dashed line. Clicking an area does nothing, and a track under it can still be clicked.
4. Badges and areas are drawn over everything else, the Fog veil and map labels included, and Fog doesn't dim them.
5. They hide during an Edit track session (FR-5.14) and come back after it.

### FR-15.3 The popup

**Behavior**:
1. Clicking a badge opens a popup at the place: its name (or its category, when OSM has no name), then — each only when the place has it — its category (when the name is the title), its memorial type, "Since" its start date, its description, its inscription (quoted, keeping its line breaks), its address, and "Captured" with the date for a place the account has captured (FR-15.6). A long description or inscription scrolls within the popup. Only one popup is open at a time.
2. **Copy address** copies the address; for a place with none it reads **Copy location** and copies its coordinates as `latitude, longitude`. Either shows **Copied** for a moment.
3. **Wikipedia**, shown only for a place with an article, opens that article on that language's Wikipedia in a new tab.
4. Clicking a badge doesn't select or unfocus a track under it (FR-4.1). The popup closes with its × button, a click elsewhere on the map, or unticking its category.

### FR-15.5 Show in this area

**Description**: Between zoom 10 (a metro area in view) and zoom 13 a view holds too many places to load on every pan, so they load when asked, for the visible map.

**Behavior**:
1. With at least one category ticked and the map between zoom 10 and 13, a **Show in this area** button shows over the map, under the toggles (in the Android app, between the Layers and Record buttons). Outside that range, or with no category ticked, it doesn't.
2. Pressing it loads the ticked categories' places inside the visible map and draws their badges (no areas). They stay drawn until the next press; unticking a category hides its places at once.
3. Until the map moves or another category is ticked, a line replaces the button: how many places the area has ("912 places in this area"), "No places in this area", or, when there were more than 2,000, "Showing 2,000 of N places — zoom in for the rest". The 2,000 are the named places first, spread evenly over the area. After a move or a newly ticked category, the button comes back.
4. A badge from it opens the same popup (FR-15.3). From zoom 13 up the tiles' badges take over and these aren't drawn.
5. If the request fails, the button reads "Couldn't load places — try again".

**Inputs** (`GET /v1/spots`): `bbox` — `west,south,east,north` in degrees, west below east and south below north (clamped to the world); `categories` — one or more of `playground`, `dog_park`, `monument`, `viewpoint`, `history`, comma-separated.

**Outputs**: `{spots, total}`: `spots`, up to 2,000 places whose anchor is inside the box, each with `id`, `category`, `lon`, `lat` and the text fields of FR-15.4's `spots` layer (absent when none); `total`, how many the box holds in those categories.

**Error cases**:
- A missing or malformed `bbox`, a box with west at or past east (the antimeridian), or a missing or unknown category → `400`.
- No session → `401`.

### FR-15.4 The tiles

**Inputs**: `z`, `x`, `y`; `cv`, the tile version (FR-4.11).

**Outputs**: A vector tile with two layers. `spots`: a point per place whose anchor falls in the tile, with `id`, `category` (`playground`, `dog_park`, `monument`, `viewpoint` or `history`), `lon`, `lat`, and `name`, `address`, `description`, `inscription`, `memorial`, `start_date` and `wikipedia`, each absent when the place has none. `spot_areas`: each place's area that reaches into the tile, clipped to it, with `id`, `category` and `circle` (`true` for the 30 m circle of a place mapped as a point). Below zoom 13, an empty tile.

**Behavior**:
1. The same places for every account, behind the session like every other map tile, and cached like them (FR-4.11). Loading places moves every account's tile version.

**Error cases**:
- No session → `401`. A demo session sees the places too.
- Non-numeric coordinates → `400`.

### FR-15.6 Captures

**Description**: A place an account has captured by staying inside it for 30 seconds with the Android app's capture mode on (`apps/android/docs/SPEC.md` FR-2.8, ADR-0023). Only the app captures; the web and the app both show what's captured (FR-15.2, FR-15.3). A capture is separate from a visit (ADR-0021), which isn't built.

**Behavior**:
1. `GET /v1/spots/{id}` answers one place: the fields of FR-15.5's places, `area` — its whole area as a GeoJSON MultiPolygon — and `captured_at` when the caller has captured it (absent otherwise).
2. `GET /v1/spots/captures` answers `{captures}`: the caller's captured places as `{spot_id, captured_at}`, newest first; an empty list when there are none.
3. `POST /v1/spots/{id}/captures` takes `{lat, lon}`, the position the phone last measured inside the place. The position must be inside the place's area, or within 10 m of it. The first capture of a place is kept: a new one answers `201`, a repeat `200` with the first one's `captured_at`, both as `{spot_id, captured_at}`.
4. A capture belongs to its account and is deleted with it, or with its place.

**Error cases**:
- An unknown place, or an `id` that isn't a positive number → `404`.
- A body without numeric `lat` and `lon`, or outside ±90/±180 → `400`.
- A position farther than 10 m outside the place → `422`.
- No session → `401`. A demo session can read but not capture → `403` (`demo_read_only`).

## 18. Non-Functional Requirements (summary)

This section summarizes cross-cutting behavior specified elsewhere in this document, for convenience — it does not introduce new requirements.

| Concern | Behavior |
| :-- | :-- |
| **Password storage** | bcrypt-hashed; plaintext is never stored or logged (FR-1.1). |
| **Session security** | Server-side, revocable sessions (not client-decodable tokens); expiry enforced server-side on every request, not trusted from the cookie alone (FR-1.4). |
| **Rate limiting** | Per-IP limits on the two endpoints reachable with no credentials at all: demo creation (FR-2.1) and password-reset requests (FR-1.5), 5/hour each. |
| **Non-blocking uploads** | No modal or locked UI during upload or processing (FR-3.1); the user can keep using the app while files process. |
| **No reload required** | Every list/summary this document describes updates itself automatically as background processing completes (FR-3.1, FR-3.4) — a manual page reload is never required to see current data. |
| **Idempotency** | Re-submitting the same activity content (FR-3.5) or the same password-reset token (FR-1.6) never has an effect beyond the first time. |

## 19. Mobile Browser Support

**Known issue**: The behavior below is what was designed and implemented, but the actual mobile experience has been reported directly as unusable, not just rough. Four causes a phone has and desktop emulation doesn't were found and fixed (items 4–6 below); the behavior is still unverified on a real device, so treat it as unconfirmed until it is — see `docs/ROADMAP.md`'s "Mobile browser support" item (Phase 3).

**Description**: The application is usable in a phone-sized mobile browser, not just at desktop widths. This is a cross-cutting behavior, not a separate feature — it modifies how several of the FRs above render and are interacted with, rather than adding new ones.

**Behavior**:
1. Below approximately 768px viewport width, the Activities panel (FR-5.1) is a collapsible bottom sheet instead of a permanent sidebar — on the Activities tab sitting directly above the date slider (item 2 below), on the other tabs on the bottom edge of the screen — collapsed by default to a slim strip that is the panel's tab row (Activities, Stories, Privacy) with an expand chevron at its end — with the map fully interactive above it; tapping the chevron, or any tab, expands it upward, onto that tab, over the map to show the tab's description, full list, filters, and footer actions (FR-5.2–FR-5.7), exactly as they behave at desktop width. Expanding or collapsing the sheet never changes the checked group (FR-5.6) or the row-click focus (FR-5.5). While the Edit window (FR-5.10) is open the sheet is shown collapsed, whatever its state, so the window and the track stay visible; closing it returns the sheet to the state it was in.
2. The date slider (FR-6) leaves the sheet for the very bottom of the screen, where it stays visible in either sheet state, so the range can be changed with the sheet collapsed and the map in view. As on a desktop it shows only while the Activities tab is showing.
3. Trends (FR-9.1) — a hover-tooltip chart at desktop width — also responds to a tap: tapping a bar shows the same tooltip a hover would, tapping it again (or tapping empty chart space) hides it. A tap and a mouse hover never conflict with each other on the same chart.
4. The page fills the visible area as the browser's own toolbar slides in and out: the date slider stays at the bottom edge of what's visible, never under the toolbar, and dragging the page's own controls never scrolls, rubber-bands or pull-to-refreshes the page. Focusing a text field (a name, a search, a sign-in field) never zooms the page in.
5. Tapping within about 14px of a track focuses it, as a click does within about 4px (FR-4.1) — a fingertip lands less precisely than a pointer.
6. Focusing an activity by tapping its track leaves the collapsed sheet as it was: its strip stays visible and tappable, and the focused row is scrolled into view within the list when the sheet is expanded.
7. Every other behavior in this document (upload, all three map modes, filtering, account settings) works the same way at mobile widths as at desktop widths.

**Explicitly not built** (hover-only, no touch equivalent, unlike Trends above — a continuous position read with no discrete point to tap, not a per-bar value): the two-way map-track-hover ↔ Activities-row-underline highlight (FR-4.1, FR-5.4). It remains mouse-only; a touchscreen user can still focus/select a track by tapping it.

## 20. Out-of-scope items, tracked for future revisions of this document

The following are named in `VISION.md`'s roadmap but have no functional requirements in this document because they are not yet built:

- Path 1 cloud-provider connectors (Garmin, Wahoo, COROS)
- Path 2 on-device sync's iOS half (Apple HealthKit — Android's Health Connect half is FR-3.6)
- The rest of "Export" — animated reveals (high-resolution map export itself is built, FR-4.10)
- Marking a Spots place visited once an activity spends five minutes inside it (ADR-0021, `ROADMAP.md` Phase 1)
- Dark-theme variant of the Fog of War veil (the theme parameter is accepted but currently has no visual effect on the veil itself)

Deliberately out of scope, not a "not yet", and not planned: Oura and other recovery-data sources (sleep, HRV, readiness), best-effort curves, personal bests, power curves, and training load. Also deliberately out of scope, never built: explorer-tile scoring — Fog of War is the exploration mechanic (ADR-0018). And splitting a track that passes through a Private location mid-way (FR-8.1 hides only the leading and trailing portions, by design). `VISION.md` §1.1 draws a hard line against HoldMyTrack being a health or fitness advisor; pace stays as per-activity route context (FR-4.8), not an analysed, all-time performance record, and heart rate and every other health measurement are out of scope entirely — never read, stored or shown (ADR-0017).

## 21. FR-16 — Activity photos

**Description**: The user's own photos added to an activity, each placed at the point of its route where it was taken, so an activity — and a Story — shows its pictures on the map (`VISION.md` §4.2, ADR-0024). Stored as a resized copy and a thumbnail with no EXIF; private to the account. The web and the Android app add and show them (`apps/android/docs/SPEC.md` FR-2.10); FR-16.6 and FR-16.7 are the web's.

### FR-16.1 Upload

**Behavior**:
1. `POST /v1/photos` takes a multipart body: `activity_id`; `file`, the resized photo, and `thumb`, its thumbnail, each a JPEG or WebP; and optionally what the client read from the original's EXIF — `taken_at` (RFC 3339) or `taken_local` (`YYYY-MM-DDTHH:MM:SS`, a capture time with no time zone), and `lat`/`lon`; and optionally `route_at` (RFC 3339), the place on the track the user chose, and `caption` (trimmed, at most 500 characters). It answers `201` with the photo (FR-16.3), placed per FR-16.2.
2. Each image's type is read from its bytes, never from its filename or declared type.
3. An account holds at most 2,000 photos.
4. `POST /v1/photos/place` takes the same placement fields as JSON — `{activity_id, taken_at, taken_local, lat, lon}` — and answers where an upload with them would be placed, `{route_at, taken_at}`, storing nothing. It refuses as the upload does: `photo_needs_place` (`422`), no track (`409`), a field that doesn't parse (`400`), another account's activity (`404`), a demo session (`403`).

**Error cases**:
- No session → `401`. A demo session → `403` (`demo_read_only`).
- An activity that isn't the caller's, or a missing or malformed `activity_id` → `404`.
- The account already holds 2,000 photos → `409`.
- A missing `file` or `thumb` → `400`; a `taken_at`, `taken_local`, `lat`/`lon` or `route_at` that doesn't parse → `400`; a caption over 500 characters → `400`.
- An activity with no track → `409`: a photo needs a place on one.
- A photo FR-16.2 can't place → `422` with the error code `photo_needs_place` and a message; nothing is stored. The client asks the user where it goes and sends it again with `route_at`.
- An image that isn't a decodable JPEG or WebP → `415`.
- `file` larger than 3 MiB or `thumb` larger than 256 KiB (or the body over its limit) → `413`.
- `file` larger than 2560 px, or `thumb` larger than 640 px, on either side → `422`: an original must be resized first.

### FR-16.2 Placement

**Behavior**:
1. Every photo has a place on its activity's track: a moment on it (`route_at`), never a stored position. Its position is that moment's point on the track, worked out on every read.
2. A `route_at` the user chose places it there, clamped to the track's first and last moment.
3. Otherwise it's placed at its capture time when that falls within the track; a capture time up to 5 minutes before the track starts or after it ends places it at that end.
4. A `taken_local` time is read in the account's time zone; if that misses the track, in the UTC offset (in 15-minute steps, −12:00 to +14:00) nearest the account's own that puts it on the track. `taken_at` (FR-16.3) is the instant it resolved to.
5. Without a capture time that places it, an EXIF position within 500 m of the track places it at the track's nearest point.
6. With none of these, the upload is refused for the user to choose (FR-16.1).
7. A photo is always on the track as it's drawn: a moment the track no longer covers — cut off by Edit track or a Private location at its start or end — reads as the track's nearest end, and `route_at` is kept, so the photo returns to its moment if the track does. A track that passes through a Private location is drawn whole (FR-8.1), so a photo on that stretch shows nothing the track doesn't.

### FR-16.3 Listing and images

**Behavior**:
1. `GET /v1/photos?activity={id}` answers `{photos}`: the activity's photos as `{id, activity_id, taken_at, route_at, lon, lat, caption, width, height, url, thumb_url}`, in route order (by `route_at`, then upload). `lon`/`lat` are null only when the activity has no track left at all. `width`/`height` are the stored copy's. `GET /v1/photos?story={id}` answers the same for every activity in a Story (FR-14) that isn't a superseded duplicate, in the same order across all of them.
2. `GET /v1/photos/{id}` and `GET /v1/photos/{id}/thumb` serve the stored copy and its thumbnail with their sniffed content type, to their owner only, cacheable for good (a photo's images never change).

**Error cases**:
- Neither `activity` nor `story` → `400`. An activity, Story or photo that isn't the caller's, or a malformed id → `404`.

### FR-16.4 Editing

**Behavior**:
1. `PATCH /v1/photos/{id}` takes a JSON object; a field present changes, a field absent doesn't. `caption`: trimmed; empty or null clears it; at most 500 characters. `route_at`: RFC 3339, the photo's new moment on the track, clamped to it. A photo can't be taken off the track. It answers `200` with the photo.
2. `GET /v1/activities/track-metrics/{id}` (FR-4.8) carries each display point's moment as `time_s` (epoch seconds), so a client can turn a place on the track into a `route_at`.

**Error cases**:
- A body that isn't a JSON object, or a `route_at` that is null or not an RFC 3339 time → `400`. A caption over 500 characters → `400`.
- A `route_at` for an activity with no track → `409`.
- A photo that isn't the caller's → `404`. A demo session → `403`.

### FR-16.5 Deleting

**Behavior**:
1. `DELETE /v1/photos/{id}` deletes the photo and both its images, answering `204`; a photo that isn't the caller's → `404`, a demo session → `403`.
2. Deleting an activity deletes its photos (FR-5.11). A duplicate's photos move to the copy that's kept (FR-3.7).

### FR-16.6 The Photos tab (web)

**Preconditions**: The Edit window (FR-5.10) open on exactly one activity, which has a track; otherwise the tab is disabled, with a tooltip saying why.

**Behavior**:
1. The Edit window's Photos tab, beside Activity and Track, lists the activity's photos in route order (FR-16.3): each with its thumbnail, its caption (or "Photo n"), the time of its place on the route, Edit and Delete. Its label shows how many photos Save would leave, and a dot while it holds unsaved changes.
2. Nothing on the tab is written until the window's Save, which writes the activity's fields, then the photos, then a track edit; Cancel throws every photo change away, new photos included. Escape asks first while there are photo changes ("Discard photo changes?" — Discard or Keep editing; Escape in the question keeps editing), and closes at once when there are none. While there are photo changes, or while they're being written, leaving the page — a reload, closing the tab, following a link — has the browser ask first. While the photos are written the Save button reads "Saving photos n of m…". If a write fails, the window stays open with the error, and Save again carries on from the first unwritten change; whatever was written stays written even if the window is then cancelled.
3. Add photos opens the browser's file picker for any number of images. Each is read for its EXIF capture time (with its zone, when the file has one or a GPS clock to derive it from) and position, redrawn at most 2048 px on its long side plus a 320 px thumbnail, upright per its EXIF orientation, and its place asked of the placement check (FR-16.1), with "Preparing n of m…" while it runs. One the check places joins the list, marked New, and the map.
4. A photo the check can't place (FR-16.2) waits at the top of the list, saying so, with a slider along the route and its thumbnail on the map where the slider has it. Place here puts it there, joining the list as New; Remove drops it. Several wait their turn, one slider at a time. A waiting photo's slider starts just after the photo picked before it — where the check placed that one, or where the user put it (past a removed one, to the one before) — 1% of the route further on; the first of a batch with nothing before it starts at the beginning of the route. Photos picked in the order they were taken so walk forward along the route. Save is refused while any photo waits, with a message, on the Photos tab.
5. Edit opens a photo's slider, at its place, and its caption; Done closes it. Moving the slider moves the photo along the route on the map, with the time at that point shown. A saved photo with a change is marked Changed; changing it back unmarks it. The list keeps its order while a photo is open and takes up the new order when it closes.
6. The slider runs along the track as drawn, by distance, so a stop takes up none of it; the place is kept as that point's moment.
7. Delete marks a saved photo Will be deleted, struck through and off the map, with a button to keep it after all; on a New photo it simply removes it. No confirmation: Cancel undoes it all.
8. A file that fails is listed by name with the reason — one the browser can't open (most HEIC files outside Safari), or the server's refusal — and the rest carry on.
9. The Edit window isn't available to a demo session (FR-5.10), so neither is the tab.

### FR-16.7 On the map (web)

**Behavior**:
1. Whenever an activity is selected (FR-5.5) — and while the Edit window is open on one, as its Photos tab would leave them — each of its photos is drawn on its route as its thumbnail in a round frame, over the tracks. With no activity selected and a Story open (FR-14), every photo of the Story's activities is. None show in Fog of War or Heatmap, for an activity hidden on the map (FR-5.8), or while the Edit window's Track tab has the map. The photo in hand in the Photos tab is drawn larger.
2. Photos whose markers would overlap at the current zoom — a whole trip seen zoomed out, or several taken at one spot — share one marker: the first of them in route order, stacked, with their count. The groups are worked out again after every move of the map, so zooming in splits them. The photo in hand in the Photos tab is never grouped.
3. The photos follow the track: they're refetched when an Edit track or a Private location change has been reprocessed (FR-5.14, FR-8.1), and sit at the track's nearest end when their stretch was cut away (FR-16.2).
4. Clicking a group's marker zooms the map in to fit its photos (to at most z19) when they lie more than 15 m apart; when they don't — photos taken at one spot, which no zoom separates — it opens the popup on the first of them, with ‹ › and "n of m" to step through the rest in route order, the popup staying where it opened.
5. Clicking a photo's marker opens a popup at it, on the map rather than over the page: the picture, which opens the photo viewer when clicked (FR-16.8), its caption, when it was taken (if known), and Full size, which opens the stored copy in a new browser tab. It opens on whichever side of the marker it fits, moves with the map, and closes with its ×; its marker is drawn larger meanwhile. A marker's click doesn't change the selection. A demo session sees the same.
6. Changing or deleting a photo is the Photos tab's (FR-16.6), not the popup's.

### FR-16.8 The photo viewer (web)

**Behavior**:
1. Clicking the picture in a photo's popup (FR-16.7) shows it over the whole page on a dark background, as large as fits but never larger than the stored copy, with its caption and capture time (if known), and with ‹ › and "n of m" when the popup holds a group. Stepping moves the popup along with it.
2. Zooming: the mouse wheel or a trackpad pinch zooms about the pointer, a two-finger pinch about the point between the fingers, and a double-click zooms in to 3× about the clicked point (or back to fitted, if zoomed in). Zoom in and Zoom out (and the `+`/`=` and `-` keys) zoom by 1.5× about the center, and Fit to screen (and `0`) goes back to fitted. Zoom runs from fitted to 8× fitted.
3. Zoomed in, the picture is dragged with the mouse or a finger. It can't be dragged past its edges, and a side that still fits the screen stays centered. A step to another photo starts it fitted. The ← and → keys step through a group.
4. A Full size link opens the stored copy in a new browser tab, as the popup's does. The × button or Escape closes the viewer and leaves the popup open. A demo session sees the same.