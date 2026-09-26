# HoldMyTrack: Spec

| | |
| :-- | :-- |
| **Version** | 1.0 |
| **Status** | Current — describes Phase 0/1 functionality as built |
| **Last updated** | 2026-09-25 |
| **Related documents** | `VISION.md` (product scope, market rationale, phase roadmap — the authority on *what ships and why*); `ARCHITECTURE.md` (system-level shape, key decisions, the stack); `IMPLEMENTATION.md` (schema, each feature's own implementation — the authority on *how it's built*); `AGENTS.md` (repository orientation) |

## 1. Introduction

### 1.1 Purpose

This document specifies HoldMyTrack's functional behavior as currently implemented: what the system does, from the point of view of a user or of another system calling its API — not why it was built that way (`VISION.md`) or how it is implemented internally (`IMPLEMENTATION.md`). Each functional requirement (FR) is written to be independently testable: given the stated preconditions and inputs, the stated behavior and outputs should be observable.

### 1.2 Scope

**In scope**: every feature currently built and shipped, as of this document's last-updated date — authentication and account management (including account settings — avatar, name, country, and timezone, FR-1.7; email verification, FR-1.8; Sign in with Google and with Facebook on the web and in the Android app, FR-1.9 and FR-1.10), the no-signup demo (now read-only, seeded from a persistent, richly-populated Demo Customer account rather than a fresh per-visitor preset — FR-2.1), activity upload and ingestion (file upload, `.zip` bulk import, Google Takeout import, Android's Health Connect mobile sync — FR-3.6, and Android's in-app GPS recording — FR-3.8), cross-source duplicate detection (FR-3.7), map visualization (track rendering, Fog of War, Heatmap, colored zone segments, the pace/heart-rate + elevation profile, high-resolution export), the Activities panel and its filters, track editing (FR-5.14), Private locations (FR-8.1), the date-range picker, the per-account activity graph, password recovery, distance/time trends (FR-9 below), the public About, Help and Contacts pages (FR-10), the Donate link out to Open Collective (FR-11), the read-only admin panel (FR-12), and the interface language — English or Russian (FR-13).

**Out of scope**: functionality named in `VISION.md`'s roadmap (§5.3 onward) but not yet built — Path 1 cloud-provider connectors (Garmin/Wahoo/COROS), Path 2 on-device sync's iOS/HealthKit half (no iOS app exists yet; Android's Health Connect half shipped — FR-3.6), explorer-tile gamification, the rest of "Export" (story cards, animated reveals — high-resolution map export itself is built, FR-4.10 below). Also deliberately out of scope, not a "not yet" — best-effort curves, personal bests, power curves, and training load were built and then cut: `VISION.md` §1.1 draws a hard line against HoldMyTrack being a health or fitness advisor, and pace/heart-rate stay as per-activity route context (FR-4.9) rather than an analysed, all-time performance record. This document will be extended with new FR sections as in-scope functionality ships, not rewritten in place of them.

### 1.3 Intended audience

Engineers implementing against or modifying this system, QA deriving test cases, and anyone needing an authoritative answer to "what does the system do in this situation" without reading source code.

### 1.4 Definitions

| Term | Meaning |
| :-- | :-- |
| **Activity** | One recorded exercise session (a run, ride, hike, swim, etc.) with a start time, and usually a GPS trajectory and heart-rate data. |
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

**Description**: A signed-in user (real or demo — FR-2) edits their own profile: Avatar, Name, Country, Timezone, and Language. A page at `/settings`, reached from the header's account menu, separate from the activity graph (FR-7) — and, for a real account that has never saved it, shown automatically in place of the map (behavior 5). Private locations (FR-8.1) aren't edited here; they live on the map's Privacy tab.

**Name** is an optional display label, not an identifier: it isn't unique, two accounts may share one, and nothing signs in with it — the email address identifies an account. Nothing outside this page displays it yet.

**Preconditions**: An active session.

**Inputs**: An image file (PNG, JPEG, or WebP, up to 5 MB) for Avatar; free text for Name (optional); a country chosen from a list of ISO 3166-1 countries by name for Country (required — there is no "not set" choice); an IANA timezone for Timezone, chosen from the zones browsers know under IANA's current names (Kolkata, not Calcutta; Kyiv, not Kiev), grouped by region, sorted by place and labelled with its GMT offset today ("New York · GMT−04:00"), with UTC and the account's own saved zone always included. A zone given under a former name — which browsers still report at signup — is stored under its current one. For Language, one of "Automatic (browser)" (the default), English, or Русский — each language named in itself, whatever language the page is in (FR-13).

**Behavior**:
1. Avatar uploads and removals take effect immediately — each is its own action (`POST /settings/avatar`, `POST /settings/avatar/remove`; for an API client, `POST`/`DELETE /v1/account/avatar`), not gated behind a separate save step; choosing a file uploads it. The page, header included, shows the new avatar when it reloads after the upload.
2. Name, Country, Timezone, and Language save together as one action (`POST /settings`; for an API client, `PATCH /v1/account/settings`, where `locale` is optional and left unchanged when absent) — editing one and leaving without saving discards all of them, not just the one touched. A successful save reloads the page with "Saved.", in the language just saved; a failed one shows the error with the submitted values kept.
3. **Country decides which unit system the entire app displays distance, pace, and elevation in** — metric (km, min/km, meters) for every country except the United States, Liberia, and Myanmar, which see imperial (mi, min/mi, feet). An account that has never saved a Country (only possible before its first save — behavior 6) displays metric. It applies everywhere one of these values is shown (the Activities panel, the date-range picker, the activity graph, Trends, the per-activity pace/elevation profile, and the map's own distance scale) from the next time that page is opened after saving — Settings is a page of its own, so leaving it is a page load. Save can't be submitted until a Country is chosen.
4. **Timezone decides which calendar day an activity is grouped under everywhere the app buckets by day** — the date-range picker's histogram, the activity graph's daily grid and stat cards, `GET /v1/activities/trends`, and date-range filtering. Auto-resolved from the browser at signup (FR-1.1) and editable here afterward; unlike Country, it has no "unset" state — every account always has one, defaulting to UTC until changed.
5. **First run.** A real, verified account with no Country — one that has never saved this page, which is every new account right after FR-1.8's verification link — is sent here from the map and Profile (and from sign-in), titled "Welcome — set up your account", with a short explanation of why Country and Timezone matter. There is no way past it: no back link, and the header's links to the map and Profile lead back here. Timezone is prefilled with the one auto-detected at signup, to confirm or change; Country starts on "Choose a country". "Save and continue" can't be submitted until a Country is chosen; saving goes on to the map, and every later visit goes directly to the map. Reloading before saving shows this page again. A demo account never sees it.
6. **A demo account** sees the page with every field and button disabled and a note that the shared demo account can't be changed, linking to "Create your own account" (FR-2.3); a save or avatar change submitted anyway is refused (`403`).

**Outputs**: The account's current Avatar, Name, Country, Timezone, and Language (`locale` in `GET /v1/auth/me`, `""` for automatic), always reflecting the last successful save (or the account's defaults, if never changed) — reloading the app never reverts to something stale.

**Error cases**:
- An unsupported image type or a file over 5 MB → `415`/`413`, and the image is not saved.
- Country missing or outside the supported list, Timezone not a valid IANA zone name, or Language not one FR-13 supports → `400 Bad Request`, and none of the fields in that save are applied (a full-replace save either succeeds as a whole or not at all).

### FR-1.8 Email verification

**Description**: A real (non-demo) account created by FR-1.1 must confirm its email address before it can use anything beyond this screen — the map and every other authenticated route are gated on it. A demo account (FR-2) is never subject to this gate.

**Preconditions**: An active session for a real account whose email is not yet verified.

**Behavior**:
1. On signup (FR-1.1) and whenever the email address changes (step 4 below), the server emails a link containing a verification token (valid 24 hours, single-use) to the address on file.
2. The link opens `/verify?token=…`, which verifies the token the same way `POST /v1/auth/verify-email` does (the endpoint the Android app would call). A missing, expired, used or malformed token shows "This verification link is invalid or has expired." On success, the server marks the account verified, invalidates every other outstanding verification token for it, and creates a fresh session for whichever browser opened the link — regardless of whether that browser already held a session of its own, so the link works from any device.
3. While waiting, the account holder can request another copy of the link (`POST /v1/auth/resend-verification`, rate-limited to 5 per hour per account) without needing to already know it was lost or expired.
4. The account holder can also change the address on file (`PATCH /v1/auth/email`) before ever verifying — correcting a typo the original signup made, since a resend alone cannot fix a wrong address. Any change resets the account back to unverified and sends a new link to the new address, whether or not the account was already verified.
5. Once verified, the account continues to FR-1.7's first-run Settings page, not straight to the map, until Country and Timezone have been saved once.
6. Until verified, every route other than `GET /v1/auth/me`, `POST /v1/auth/logout`, and the three endpoints above returns `403 Forbidden` with a distinguishable error rather than the normal response.

**Outputs**: `verify-email` and `reset-password` both return a valid session cookie for the account on success. `resend-verification` and `change-email` return a confirmation; `change-email` also returns the account's current (now-unverified) profile.

**Error cases**:
- Verification token missing, already used, or expired → `400 Bad Request` with a generic message, same non-distinguishing reasoning as FR-1.6's reset token.
- `change-email`/`resend-verification` attempted by a demo account → `400 Bad Request` (neither concept applies to one).
- More than 5 resend requests for the same account within an hour → `429 Too Many Requests`.

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

A new account made this way is unverified, like one made on the web; the Android app has no verification screen yet (`KNOWN_ISSUES.md`).

## 4. FR-2 — No-Signup Demo

### FR-2.1 Start a demo

**Description**: An anonymous visitor tries the full application without creating an account.

**Preconditions**: No active session.

**Behavior**:
1. Client calls `POST /v1/auth/demo` — on the web, the sign-in page's "Try it now — no signup" button submits `POST /demo`, which does the same and redirects to the map. Starts are rate-limited to 5 per hour per address; over the limit the sign-in page shows the error.
2. Server opens a session against one persistent, shared **Demo Customer** account — not a fresh account created per visitor. That account is pre-seeded, once, out of band (not per request — see FR-2.2), with a real, richly-populated history: roughly 611 activities spanning about 7 months, a mix of walks, dog walks, bike rides, local errands, and a few multi-day road trips, based around Cleveland, OH — which its Settings profile (Name "Demo User," Country United States, Timezone America/New_York) matches, rather than being left at generic column defaults — plus one Private location, "Home," a 250 m circle over the area nearly every trip starts from (FR-8.1).
3. Server creates a session for this visitor (24-hour expiry) and sets it as a cookie. Any number of visitors can hold their own session against the same shared account at once — they all see identical data.
4. Client is signed in and shown the map, with the account's full history already visible in every mode (Normal, Fog of War, Heatmap) and every read-only feature (filtering, the activity graph, export) exactly as a registered user has it. **A demo account cannot upload, sync, edit, or delete an activity, or change its avatar/settings** — every such request is rejected regardless of what a client attempts, whether or not its own UI still offers the control. Creating a real account (FR-1.1) is the only way to save one's own data. Both clients also gate this proactively rather than relying on the rejection alone: the web app replaces the Sync tab's drop zone with an explanation (`SyncTab.tsx`'s `readOnly` prop) and the Android app disables Sync Now and hides the Health Connect permission flow with the same explanation (FR-3.8) — a demo session can still record and manage GPS Logger recordings locally on Android, since nothing about that reaches the server until sync is attempted.

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
1. User drags files onto the drop zone in the Activities panel's Sync tab (FR-3.4), or selects them via a file picker.
2. Each file uploads independently, as its own `POST /v1/activities/upload` request (multipart), and is tracked independently — one file failing does not affect the others.
3. For each file: server validates its extension and size, computes a content hash to check for a duplicate (FR-3.5), persists the raw file, and enqueues a background parsing job.
4. The Sync tab's history shows each file's live status (uploading, with a progress percentage; then "Processing…") until the background job finishes.
5. Once ingestion completes, the activity appears automatically in the Activities panel, the map, and every summary that reflects the current date range — no page reload is required. Its Fog-of-War/Heatmap coverage follows a few seconds later, once the background re-render finishes, also without a reload.

**Outputs**: One new `Activity` per successfully ingested file, each with a parsed trajectory, distance, duration, and (where the source file provides it) heart rate/elevation data.

**Error cases**:
- Unsupported file extension → `415 Unsupported Media Type`.
- File too large, or malformed request → `413 Request Entity Too Large`.
- Empty file → `400 Bad Request`.
- Unparseable/corrupt file content → the background job fails; the upload history shows "Failed" with a reason, and no `Activity` is created.
- A file whose points carry no timestamps at all (a planned route rather than a recorded activity) → the background job fails the same way, with a reason saying the file has no timestamps. Points without a timestamp inside an otherwise timed track are dropped, not failed on.

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
1. User uploads the Takeout export `.zip` through the same Sync-tab drop zone as FR-3.1/FR-3.2.
2. Server recognizes the archive's shape as a Takeout export (rather than a plain `.zip`) and extracts one activity file per recorded activity that has GPS data (activity types with no GPS in the export — e.g. a logged swim with no route — are skipped, not treated as errors).
3. Each extracted activity is ingested exactly as FR-3.1 describes, attributed to the Takeout source.

**Outputs**: One new `Activity` per activity in the export that had GPS data.

### FR-3.4 Import status and history

**Description**: A user can see the status of in-progress and past uploads and syncs, and jump from a finished one straight to it on the map. The Activities panel has three tabs, **Activities** (the list, FR-5), **Sync**, and **Privacy** (Private locations, FR-8.1). The Sync tab holds the file drop zone and picker (`.gpx`/`.fit`/`.tcx`, `.zip`, and Google Takeout — FR-3.1–FR-3.3) above one history list of everything imported into the account, whatever the source: uploaded files alongside activity synced from the Android app (Health Connect and in-app GPS recording — FR-3.6, FR-3.8). The web can't start a phone sync; the history is a read-only status view, not a "sync now" button: that sync is phone-triggered, and nothing in the web app can request it (`apps/android/docs/SPEC.md` §7.4's own "Ask every time"/"Always allow" split is the closest analogue, and it lives entirely on the phone).

**Preconditions**: Active session.

**Behavior**:
1. The Sync tab's badge shows a live count of this session's uploads in flight plus every job still processing for the account, visible whichever tab is open.
2. The history is one paginated list (5 per page) of every job ever recorded for this account, newest first, each showing: a title (the filename for an uploaded file; the source name — "Health Connect," "GPS Logger" — for a synced one, since a synced job's own filename is a platform-assigned id with nothing human-readable in it), status ("Processing…" / "Ready" / "Failed"), and — once ready — the activity's date and distance. Files still uploading are listed above it with a progress percentage.
3. While anything is still processing, the history refreshes automatically (polled every 1.5 seconds) until every row settles to "Ready" or "Failed" — no manual refresh needed. Polling and uploads carry on while the Activities tab is showing, or the panel is hidden in Fog of War/Heatmap mode, since a job finishing still has to reach the map.
4. For a demo session, the drop zone is replaced by a note that importing isn't available for demo accounts.
5. A "View on map" action appears on every finished (`"Ready"`) row. Clicking it switches the panel back to the Activities tab, focuses that activity exactly as clicking its row in the Activities panel would (FR-5.5) — track bolded, camera flown to fit it — and, if the activity's own date — its day in the account's timezone, the same day the date range is read in — falls outside the currently selected date range (FR-6), first narrows the selected range to just that one day (the same mechanism a manual single-day pick already uses — FR-6.5) before focusing, rather than focusing something the Activities panel isn't currently showing at all. On a phone (§17) the expanded bottom sheet collapses, so the focused track is visible.

**Outputs**: `GET /v1/uploads?limit=&offset=&source=` returns the current page, the total count (scoped to `source` when given), and how many are still processing (always the global count, unscoped, for the badge). `source` is an optional comma-separated filter (e.g. `upload,takeout`); the Sync tab omits it, for the combined view. Each row also carries `source` and, once the job has produced one, the resulting activity's own `id` — what the "View on map" action targets.

### FR-3.5 Duplicate detection

**Description**: Re-uploading a file whose content was already ingested does not create a second activity or a second processing job.

**Behavior**: The server checks a content-derived identifier before enqueuing a new job. If that exact content was already ingested for this account, the upload responds immediately with an "already processed" status (`200 OK`) and no new job is created; the client shows a notice that the file was already uploaded rather than silently doing nothing.

**Notes**: This guarantee holds even under concurrent duplicate uploads (e.g., two tabs uploading the same file at once) — at most one `Activity` is ever created for the same content.

### FR-3.6 Mobile sync (Android / Health Connect)

**Description**: The Android app reads a signed-in account's exercise history from Health Connect and syncs it to HoldMyTrack — a second ingest path (Path 2) alongside file upload above, distinct from a file the user explicitly picked.

**Preconditions**: Signed in on the Android app (`apps/android/holdmytrack`); Health Connect installed, with the Exercise permission granted plus the separately-granted "Access exercise routes" permission — a session with no route geometry can't be placed on the map, so it's rejected at sync time rather than persisted without one (see step 3).

**Inputs**: Health Connect exercise sessions with route geometry, read **foreground-only** — `READ_EXERCISE_ROUTES` returns `ConsentRequired` in the background regardless of what's granted, a platform constraint rather than a client choice. `READ_HEALTH_DATA_HISTORY`, requested separately, extends the otherwise 30-day-only read window.

**Behavior**:
1. User opens the sync screen; the app walks through granting whichever Health Connect permissions are still missing.
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
2. A match is resolved by keeping the richer record (route geometry over none; more data channels, e.g. heart rate, over fewer) and marking the other `superseded_by` the winner, rather than deleting it.
3. Every user-facing read — the Activities list, totals, histogram, day pages, trends, graph stats, map tiles, and both Fog of War and Heatmap composites — excludes superseded activities automatically.
4. Deleting the kept copy of a matched pair promotes the next-richest superseded copy back to live, rather than leaving both gone.

**Outputs**: At most one live `Activity` per real-world activity, regardless of how many sources reported it.

5. The web app's Activities panel surfaces a "N duplicates found" disclosure whenever `GET /v1/activities/duplicates` returns any rows, listing each superseded activity's start time, type, distance and source, and which source's copy superseded it — mirroring the Android app's own sync screen, which has shown this since FR-3.6 shipped. Hidden entirely when there are none.

### FR-3.8 In-app GPS recording (Android)

**Description**: The Android app records a casual, GPS-only activity itself — a walk, hike, or drive someone would not otherwise bother tracking — started and stopped from one button on the map, asking nothing while recording. Recording and syncing are two separate, explicit steps: Stop only saves a finished recording to the device; nothing reaches the server until the user marks it to sync and taps the Sync screen's "Sync Now" — a third way an activity can originate on the Android app, alongside FR-3.6's Health Connect sync and a manual file upload (FR-3.1) done from the phone's browser.

**Preconditions**: A session — the Android app shows nothing but its sign-in screen without one — and location permission granted to record. Local recordings are scoped to whichever account is currently signed in — switching accounts on one device never shows one account's recordings under another's.

**Inputs**: The device's own GPS, read while a recording is in progress; a name, an activity type (free text, not a fixed list), and a description, set after the fact from **Recorded Activities**' Edit button — never while recording.

**Behavior**:
1. A record button floats at the bottom centre of the map. **Tap** starts recording (asking for location and notification permission the first time); **tap** again pauses, and again resumes; **holding it for two seconds** stops, with a ring filling around the button as the hold counts down (letting go early cancels). Pause/resume is user-initiated only — there is no automatic pause, and nothing is recorded while paused. Recording runs as a foreground service, so it survives the screen turning off and the app being closed.
2. While recording, the map shows only the recording in progress — its line and current position, with the camera following — and none of the account's other activities or map modes (Normal/Fog/Heatmap). The previous mode returns when recording stops.
3. A notification is shown for as long as a recording is in progress, saying whether it is recording or paused, with elapsed time, distance, current speed and current altitude, and **Pause**/**Resume** and **Stop** buttons that work like the map button.
4. **Stop** saves the recording to an on-device store only, tagged not-synced — no network request happens. It is saved with no name or description and with the activity type most recently set in Edit on this account (`"unknown"` if none ever has been), and a **Save** screen then opens on it — the Edit form below, pre-filled with that type, with **Discard** in place of Download GPX. Leaving it with Back keeps the recording as saved; Discard (after confirmation) deletes it. A recording with under one minute of moving time (pauses excluded), or fewer than 2 points, is not saved at all.
5. **Recorded Activities** (a menu item) lists every recording on the device that hasn't synced yet, newest first, filterable by sync status (not synced / queued) and by activity type. Each row has a sync checkbox, a small preview of the recorded route's shape, and Edit and Delete buttons.
6. Checking a row's checkbox marks it queued; unchecking clears the queue mark. Any row can be queued whatever its type, including the `"unknown"` default — FR-3.7 matches on time, so an untyped recording still matches the same walk arriving typed from another source (e.g. Health Connect).
7. **Edit** shows Name, Activity Type, Description, the recording's time and distance, Save and **Download GPX**. Name and Description are plain text; Activity Type is a searchable picker behaving like the web edit dialog's (FR-5.10): the account's existing types, most-used first with how many activities use each, then walk/hike/run/ride/drive if unused, filtered as you type, with an **Add "…"** row that accepts any other text exactly as typed. A saved type also becomes the type later recordings start with (step 4). **Download GPX** saves the track as a GPX 1.1 file (name, description, type, and each point's position, time and elevation where known) wherever the user picks in the system's save screen — a file FR-3.1's upload accepts back.
8. The Sync screen's existing "Sync Now" (FR-3.6) submits every queued row in the same run as the Health Connect sync, each as its own `POST /v1/sync/activities` call under `source = "recorded"` with a client-generated `external_id` (a UUID minted when the recording is saved) — the same batched wire shape FR-3.6 uses, reusing its ingest pipeline unchanged (`docs/IMPLEMENTATION.md` §4.0.4, [ADR-0007](adr/0007-in-app-gps-recording-submits-directly.md)). A row that syncs successfully is deleted from the device — from then on it exists on the server, on the map and in the activity list, like any other activity. A row the server rejects, or that fails outright, stays queued and is retried automatically on the next "Sync Now" — no action needed from the user.
9. **Delete** asks for confirmation — noting the recording hasn't synced, so this is the only copy — and then removes the row from the device.
10. **A recording never appears on the map after it stops until it has synced** — the map draws only what the server holds (FR-4).
11. **A demo session** (FR-2.1) can record and manage rows here like any other account, but every sync checkbox is disabled and a notice explains why — queuing something "Sync Now" can never actually take would be pointless, and the Sync screen itself disables Sync Now for the same reason (FR-2.1).

**Outputs**: One new `Activity` per successfully synced recording, titled and described from the moment it's created if the user set a name or description in Edit — the one ingest path where that's possible (every other path leaves both fields unset at ingest). Reported through the same `GET /v1/uploads` history FR-3.4 already describes.

**Error cases**: A too-long custom activity type (over 50 characters, the column's own bound) is rejected by the sync endpoint with a clear reason rather than failing as a raw database error at insert time. A queued row that fails to sync (network error, server rejection) is left queued rather than reverted, so a retried "Sync Now" tries it again without the user re-checking anything.

**Not yet built**: recovery from an app/process kill mid-recording (the next launch does not offer to resume or discard a buffered-but-unsaved in-progress recording — this is distinct from the finished, saved-but-unsynced rows Recorded Activities already manages); a discard confirmation before Stop finalizes a save; the iOS half (`docs/ROADMAP.md` Phase 2 tracks it as a combined Android/iOS item; Android's half is what this FR describes).

## 6. FR-4 — Map Visualization

### FR-4.1 Track rendering (Normal mode)

**Description**: The map displays a user's uploaded activities as drawn lines over a base map.

**Preconditions**: Active session.

**Behavior**:
1. The map renders every activity within the currently selected date range (FR-6) that has not been individually hidden (FR-5.8), filtered out by TYPE/DISTANCE (FR-5.2/FR-5.3), or left out while Pending (FR-5.15), as a colored line following its recorded route.
2. Hovering a track on the map draws it thicker; the corresponding row in the Activities panel is highlighted to match (FR-5.4's reverse direction).
3. Clicking a track on the map sets it as the row-click focus (FR-5.5) — emphasizes it, flies the camera to fit it, and replaces whichever activity was previously focused. It does not add to or remove from the checkbox group (FR-5.6) in either direction.
4. Clicking anywhere on the map that is not a track clears the row-click focus, if any — the focused activity loses its focus treatment and returns to how it looked before: checked if it is in the checkbox group, normal otherwise. This does not affect the checkbox group.
5. Tracks are drawn from zoom 4 — a few states on screen — inward. Zoomed out further, Normal mode shows the base map alone; unlike Fog and Heatmap, it has no country/region fallback (FR-4.2, FR-4.3). Fitting the camera to an activity (FR-5.5, FR-5.6) lands at zoom 4 or closer for anything spanning up to about 60° of longitude at desktop width — a US coast-to-coast drive included — and about 25° on a phone; a wider activity is flown to but isn't drawn until the user zooms in.
6. Every track is always in one of four states — Normal, Hovered, Checked, or Focused — each drawn distinctly (*Track states*, below).

**Track states**:

| State | Entered by | Track on the map | Also on the map | Its row in the Activities panel | Camera |
| :-- | :-- | :-- | :-- | :-- | :-- |
| Normal | default | thin gold line | — | plain | — |
| Hovered | pointer over the track, or over its row (FR-5.4) | the thickest line, in a darker gold, no outline | — | title underlined | doesn't move |
| Checked | the row's checkbox (FR-5.6), Select all or Invert selection (FR-5.7) | thicker gold line with a dark outline, fully opaque | — | tinted background with a gold bar on its left edge; checkbox ticked | flies to fit the whole checked group 300ms after the last checkbox click |
| Focused | clicking the track (behavior 3) or its row's text (FR-5.5) | as Checked | colored zone segments over the line (FR-4.8) and the profile card (FR-4.9) | as Checked, checkbox unchanged | flies to fit that one track |

Only one track is hovered and only one is focused at a time; any number can be checked. Hovering a checked or focused track draws the hover line inside its outline, and underlines its row title. A track both checked and focused looks focused. A hidden track (FR-5.8) draws nothing in any state, and none of these states exist outside Normal mode (FR-4.2, FR-4.3). An emphasized track is not raised above other tracks where they overlap; its outline is what sets it apart.

### FR-4.2 Fog of War mode

**Description**: An alternate map mode showing a dark veil over everywhere the user has not recorded an activity — true all-time coverage, ignoring every other filter.

**Behavior**:
1. Selecting "Fog" from the map-mode toggle replaces the track lines with a raster veil: any area a recorded route has ever passed through is rendered clear; everywhere else stays fogged.
2. Fog ignores the date range and the Type/Distance/hidden-track filters entirely — it always shows every activity the account has ever recorded, not just what Normal mode currently has selected. Entering Fog hides the Activities panel and the date-range picker (there is nothing for either to filter) and clears any checked or focused activity. The camera is left exactly where it was — switching modes never moves it; an earlier auto-fly-to-coverage on entry was removed after being reported as disorienting.
3. Individual track lines are not drawn in this mode (the veil itself is the information). Map labels (place names, street names, points of interest) stay drawn on top of the veil but are dimmed, so they remain readable without competing with the cleared ground; they return to full strength in Normal and Heatmap. An export (FR-4.10) in Fog mode captures the dimmed labels too.
4. Below a threshold zoom, the veil switches from per-pixel coverage to a coarser reveal: a country renders fully clear the moment the account has at least one activity anywhere inside it, however small; one zoom step in, the same applies one administrative level down, at state/region granularity. Zooming back in past the threshold returns to exact per-pixel coverage — the two never blend or overlap, only one is ever shown at a given zoom.
5. Returning to Normal mode restores the previously checked/focused activities, the date range, and the panel/picker exactly as they were before switching to Fog — without moving the camera, even if the restored activities are off screen.

### FR-4.3 Heatmap mode

**Description**: An alternate map mode shading locations by how often they've been visited recently — a rolling window, not an all-time record, so a route no longer visited can cool off.

**Behavior**:
1. Selecting "Heatmap" replaces the track lines with a raster overlay, brighter wherever more recorded activity has crossed the same location (a daily commute reads brighter than a once-ridden road).
2. Like Fog of War, Heatmap ignores the date range and the Type/Distance/hidden-track filters, hides the Activities panel and date-range picker, clears any checked or focused activity, and leaves the camera exactly where it was (see FR-4.2's note on why). Unlike Fog, it only considers activities within a fixed rolling window (the last 365 days, not user-configurable).
3. Individual track lines are not drawn in this mode.
4. How much crossing traffic it takes to reach full brightness adapts to the account's own history, recomputed daily — a new account and a long-running one don't saturate at the same point, so each account's own most-used spot is what reads as hottest, not a fixed number of visits everyone shares.
5. Below the same threshold zoom Fog switches at, the graded overlay is replaced by a flat "visited" highlight at country granularity, and one step in at state/region granularity — not graded by how much, only whether the account has a currently-in-window activity there. A country/region whose only activity has aged out of the rolling window shows no highlight at this zoom either, matching what the per-pixel overlay already shows at city zoom.
6. Returning to Normal mode restores the previously checked/focused activities, the date range, and the panel/picker exactly as they were before switching to Heatmap — without moving the camera (FR-4.2 behavior 5).

### FR-4.4 Mode is mutually exclusive

**Description**: Normal, Fog, and Heatmap are three views of the same underlying data, not independent toggles — exactly one is active at a time. The toggle separates Normal from the two coverage views with a divider (Normal | Fog, Heatmap), since the choice is two-level: the plain map, or one of the two coverage views; the Android app uses the same divider (`apps/android/docs/SPEC.md` FR-2.2).

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

**Description**: The base map covers the whole planet at every zoom level, so there is no out-of-coverage area and no notice for one. Streets and labels are present wherever the user pans.

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

**Outputs**: On a successful capture, a PNG file download, named `holdmytrack-{date}.png`. OSM/Protomaps attribution is baked into the image's own pixels, in the bottom-right corner over a translucent backing plate — not optional or user-removable, since the basemap is an ODbL "Produced Work" and credit is a license requirement on any distributed export, not a preference (`IMPLEMENTATION.md` §5.6). A small HoldMyTrack logo and wordmark is always baked into the bottom-left corner — not user-removable either — at 70% opacity with no backing plate, its bottom edge level with the attribution plate's and scaled with it; its text is dark on light themes and light on the dark/black themes.

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

**Description**: Hovering a row (anywhere on it) previews that activity's track on the map — drawn thicker, FR-4.1's Hovered state — with no camera movement. The preview clears the instant the pointer leaves the row. This works in both directions: hovering an activity's track directly on the map previews it the same way, and additionally underlines that row's title in the Activities panel — so either surface can be used to identify which row an unlabeled track on the map belongs to, not only the reverse.

### FR-5.5 Row click — focus and fly

**Description**: Clicking a row's text — or clicking that activity's track directly on the map (FR-4.1) — sets it as the single row-click *focus*: highlights it and flies the camera to fit it, so the activity spans 75% of the map along whichever axis is tighter (capped at zoom 18 for a very short track). Whichever activity was previously focused this way loses its highlight (only ever one activity is "just clicked" at a time, regardless of which surface the click came from). This is independent of FR-5.6: neither a row-text click nor a map click ever checks or unchecks any checkbox, in either direction.

**Notes**: If the clicked activity has no recorded track (e.g., a source with no GPS), no fly occurs, since there is nothing to fit the camera to. The colored zone segments (FR-4.8) shown for a single focused activity are driven by this mechanism specifically, not by FR-5.6.

### FR-5.6 Checkbox — build a group

**Description**: Each row also has a checkbox that adds or removes it from the current checked group without replacing the rest of it, for building a multi-activity selection. Checking a second row never unchecks the first. Each checkbox click flies the map to fit the combined bounds of every currently checked activity, 300ms after the last click (so a rapid multi-check settles once, not once per checkbox). Only a checkbox click does this: the group coming back after a Fog/Heatmap round trip (FR-4.2, FR-4.3), the list refreshing, or hiding a checked track (FR-5.8) never moves the camera. This is independent of FR-5.5 in both directions: checking a box never sets or clears the row-click focus.

**Notes**: A hidden activity (FR-5.8) can still be focused (FR-5.5) or checked; the system excludes hidden activities from the fly-to bounds specifically so the camera never flies to an area with nothing drawn on it. A row that is both focused and checked renders with the same single highlight treatment as either alone — there is no visually distinct "both" state.

### FR-5.7 Select all / Clear / Invert selection / Focus on map

**Description**: A master checkbox in the header toolbar selects or clears every currently listed activity at once, an **Invert selection** icon button beside it flips which listed activities are checked, and a separate toolbar icon re-flies to fit the current checked group on demand.

**Behavior**: The header checkbox reflects the checked group's state against the currently listed (TYPE/DISTANCE-filtered) rows — checked once every listed row is checked, unchecked once none are, and indeterminate for a partial selection. Clicking it when unchecked or indeterminate checks every listed row and flies to fit them all; clicking it when fully checked empties the checked group and flies the camera to fit every currently visible activity in the date range (respecting the hidden-activity set). **Invert selection**, disabled when no activity is listed, checks every listed row that wasn't checked and unchecks every one that was; a checked activity that isn't currently listed (excluded by TYPE/DISTANCE) ends up unchecked. It then flies to fit the new group, or — if the result is an empty group — to every currently visible activity, the same as clearing. The toolbar's accent-tinted **Focus on map** icon, disabled when nothing is checked, re-flies to fit the current checked group without changing it — for recovering the view after panning away from it. This is also the only way to fly to a single checked activity's own bounds by group rather than by row-click (FR-5.5).

**Notes**: None of these controls affects the row-click focus (FR-5.5) — a focused row keeps its own highlight regardless of the header checkbox, Invert selection, or Focus on map.

### FR-5.8 Hide/show a track

**Description**: The header toolbar's Show/hide icon (FR-5.12) hides or shows every currently checked activity's track on the map, independent of the row-click focus (FR-5.5). There is no per-row hide/show control — hiding or showing a single activity means checking just its own box first, the same as any other single-item action. A hidden activity's track is not drawn in Normal mode until shown again — Fog of War and Heatmap ignore the hidden set (FR-4.2, FR-4.3); its row dims in place and carries a "Hidden" badge so its hidden state is still visible at a glance. Hiding/showing is purely client-side and does not refetch data. See FR-5.12 for the exact toggle rule.

### FR-5.9 Resizable panel

**Description**: The Activities panel can be resized by dragging its right edge, between 260 and 560 pixels wide (default 380). Not persisted across reloads.

### FR-5.10 Edit activity type, name, and description

**Description**: A signed-in user renames an activity's type, gives it a name, and/or attaches a free-text description to it — for example, re-labeling an activity a fitness tracker logged under the wrong category, naming a road trip so it's identifiable in the Activities panel at a glance, or describing a non-sport GPS trace in more detail than a name allows.

**Preconditions**: Active session; the caller owns the activity.

**Inputs**: Reached via the header toolbar's Edit icon (FR-5.6's checkbox group must be non-empty) or a single row's checkbox followed by that same icon — there is no per-row edit control. Editing exactly one checked activity accepts a new type (required, 1–50 characters), a name (optional, up to 200 characters), and a description (optional, up to 2000 characters). Editing more than one checked activity at once accepts only a new type — the Name and Description fields are disabled, since there is nothing consistent to set across several different activities' names/descriptions in one request.

**Behavior**:
1. Checking one or more rows and clicking the toolbar's Edit icon opens a dialog. With exactly one activity checked, it is pre-filled with that activity's current type, name, and description, all three editable. With more than one checked, only the type field is editable, seeded from the first checked activity; the Name and Description fields render disabled with an explanation of why.
2. The type field is plain free text — the same "whatever the source reports, not a controlled vocabulary" rule FR-5.2's TYPE filter already follows (`IMPLEMENTATION.md` §4.7.2) applies equally to a manual rename. The field is a searchable picker, like Settings' Country and Timezone (FR-1.7): opening it shows this account's existing types, each with how many activities use it, and typing filters that list. It is only a convenience — nothing is enforced against it: whenever the typed text doesn't exactly match an existing type (ignoring case), the list also offers an "Add" row for that text, and picking it saves the value exactly as typed, even one nobody has used before. The name field is always plain free text, with no source to ever populate it automatically — an activity has a name only once a person types one in here (`IMPLEMENTATION.md` §4.7).
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
3. The Fog-of-War/Heatmap view updates to reflect the deletion — coverage a deleted activity was the only source for reverts to unrevealed, not left showing stale coverage for data that no longer exists. An open page picks this up on its own once the background re-render finishes, without a reload.
4. Canceling the confirmation, or dismissing it, leaves every checked activity untouched.

**Outputs**: Every deleted activity, and everything derived from it, no longer exists; every list, filter, total, and aggregate that previously included it reflects the removal, in one combined refresh rather than once per deleted activity.

**Error cases**:
- A checked activity does not exist or belongs to another account → `404 Not Found`, indistinguishable from each other.

### FR-5.12 Group visible

**Description**: An icon-only header toolbar button toggles whether every currently checked (FR-5.6) activity is drawn on the map, in bulk — this is FR-5.8's entire hide/show mechanism, applied to whatever is checked, one activity or many.

**Preconditions**: At least one activity is checked; the button is disabled otherwise.

**Behavior**: If any checked activity is currently hidden, clicking shows the entire checked group (removes all of them from the hidden set). If every checked activity is already visible, clicking hides the entire group instead.

**Outputs**: The hidden-activity set updates; the map's drawn tracks reflect it immediately (Fog-of-War/Heatmap coverage doesn't change — FR-5.8).

### FR-5.13 Delete group

**Description**: The header toolbar's Delete icon — see FR-5.11, which this number and FR-5.11 both describe: deleting one activity and deleting several are the same mechanism, not two.

### FR-5.14 Edit track

**Description**: A signed-in user removes unwanted recorded points from one of their activities — typically the stretch recorded after forgetting to stop a GPS recorder (wandering a mall, sitting at home), which inflates distance and duration and draws noise onto the map and into Fog-of-War/Heatmap coverage. Three edits are offered: Chop (keep only the part between two points), Cut (remove the part between two points and join them), and Delete point (remove single points). The original recording is never modified, so an edit can always be reset.

**Preconditions**: Active session, not a demo account; exactly one activity is checked (FR-5.6); that activity has a recorded track, isn't a superseded duplicate (FR-3.7), and isn't already Pending (behavior 7 below). The toolbar's Edit track icon is disabled otherwise, with a tooltip saying why.

**Inputs**: The toolbar's Edit track icon over the one checked activity; then, inside the editor, a two-knob range slider, the Chop/Cut/Delete point/Undo/Reset/Cancel/Apply buttons, and clicks on points on the map.

**Behavior**:
1. Opening the editor flies the map to the activity (the same fit as FR-5.5's row click), hides every other activity's track, and draws this one from its full-resolution recorded points — every point visible — rather than the simplified display track. The points are the ones the activity is processed from: already clipped against the account's current Private locations (FR-8.1), so the hidden ends are never sent to the client. An activity entirely inside Private locations has no points to show and can't be edited. The Activities panel, the date-range picker, and the map-mode toggle are inert for the whole session.
2. The slider's two knobs start at the track's ends and are positioned along the track in recording order (point by point, not by distance — a stationary stretch is many points over almost no distance). The readout shows each knob's distance from the start and its clock time. Moving the knobs only previews: the part outside the knobs is drawn dashed and faded, and hovering Cut switches the preview to the part between them, with the two knob points joined straight across.
3. **Chop** keeps only the points from one knob to the other, inclusive. **Cut** removes the points strictly between the knobs, keeping both knob points and joining them. **Delete point** toggles a mode in which clicking a point on the map removes it. Each is one step; after Chop or Cut the knobs return to the new track's ends. None of them can leave fewer than two points — the buttons are disabled, and a point click is ignored, when it would.
4. **Undo** reverts exactly one step, and can be repeated back through every step of the session to the track as it was when the editor opened (also Cmd/Ctrl+Z). **Reset** (shown whenever the track currently has any edit, including one saved in an earlier session) returns to the track as originally recorded, as one more undoable step. **Cancel** discards every step of the session, restores the map, and closes the editor; nothing is saved.
5. **Apply** (enabled once there is at least one step) sends the session's net result as one edit and closes the editor. The server stores it and reprocesses the activity in the background from its original recording: distance, duration, moving time, elevation gain, start time (a Chop can move it), the display track, per-point stream data, its Fog-of-War/Heatmap coverage (FR-4.2/FR-4.3), and its country/region matches (FR-4.2's zoomed-out tiers) are all recomputed. A Cut's joining segment counts toward distance; the time gap it spans counts toward elapsed duration but not moving time.
6. Reprocessing does not re-run cross-source duplicate detection (FR-3.7): the edited activity stays whichever copy it was.
7. Until reprocessing finishes the activity is **Pending** (FR-5.15): its row shows a Pending badge with its pre-edit numbers, its text and checkbox are disabled, and Edit track and Delete are unavailable for it; its track isn't drawn, and it drops out of Fog of War and Heatmap (Country/Region tiers included). The Activities panel re-reads the list every few seconds while any row is Pending. When it clears, the row, the totals, the date-range picker's bars and the drawn track update, and the Fog-of-War/Heatmap layers (at every zoom tier) follow once their re-render lands — all without a page reload, and without moving the camera. If reprocessing fails, the activity simply stops being Pending and keeps its previous track and numbers.
8. An edit is stored as ranges and points identified by recording time, not by position in the point list, so it keeps meaning the same points if a Private location later changes. A track whose timestamps are missing or run backwards can't be edited.

**Outputs**: The activity's stored edit (none, after a Reset) and every value derived from its points, as listed in behavior 5.

**Error cases**:
- The activity doesn't exist, belongs to another account, is a superseded duplicate, or already has an edit Pending → `409 Conflict` on Apply, indistinguishable from each other; opening the editor on an activity that doesn't exist or belongs to another account → `404 Not Found`.
- The activity has no stored original recording, or its timestamps are missing or run backwards → `409 Conflict` when opening the editor, shown in the editor window.
- A range whose start is after its end → `400 Bad Request`.
- An edit that would leave fewer than two points (possible only through the API directly) → accepted, then fails during reprocessing; the activity is left as it was.

### FR-5.15 Activity states

**Description**: Every activity listed in the Activities panel is in exactly one of three states. They describe whether and how it takes part in the map, separately from the per-track display states (Hovered, Checked, Focused — FR-4.1's *Track states*), which apply only to a Normal activity.

| State | Entered by | Left by | Track on the map | Fog of War / Heatmap | Its row | Camera | Kept across reloads |
| :-- | :-- | :-- | :-- | :-- | :-- | :-- | :-- |
| Normal | default | — | drawn (FR-4.1) | counted | plain | flies to it on the user's own actions (FR-5.5–FR-5.7) | — |
| Pending | Edit track's Apply (FR-5.14), or a Private location change that could clip it (FR-8.1) | its reprocess finishing, or failing | not drawn | not counted, at any zoom tier | Pending badge, pre-reprocess numbers, disabled | never flies to it, and doesn't move when it enters or leaves Pending | yes — it's the server's state |
| Hidden | Visibility off (FR-5.8, FR-5.12) | Visibility on, or a new date range (FR-6.6) | not drawn | counted — Fog and Heatmap ignore the hidden set (FR-4.2, FR-4.3) | dimmed, "Hidden" badge | excluded from every fly-to fit | no — this tab only |

**Behavior**:
1. Pending takes precedence: an activity hidden before it went Pending is simply not drawn either way, and is still Hidden once Pending clears — reprocessing never changes the user's own Visibility choice.
2. A Pending activity's row can't be focused onto the map or checked (a row already checked stays checked); its absence is what the map, Fog of War and Heatmap all show until its reprocess lands.
3. Leaving Pending brings the activity back everywhere — its track at once, Fog of War and Heatmap once their re-render lands (FR-5.14 behavior 7) — without moving the camera.

**Notes**: An activity entirely inside Private locations (FR-8.1 behavior 3) is Normal with no geometry — nothing to draw or count — rather than a fourth state; superseded duplicates (FR-3.7) are never listed at all.

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

**Description**: Committing a new date range (via FR-6.2, FR-6.3, or FR-6.5) clears both the row-click focus (FR-5.5) and the checked group (FR-5.6) independently, the hidden-activity set (FR-5.8), and both the TYPE and DISTANCE filters (FR-5.2/FR-5.3) together — all of these could otherwise silently describe activities the new range no longer lists. The camera flies to fit the new range's drawn activities once, when its list arrives. Only a range the user picked moves it: an upload or sync landing never does — not when it refreshes the same range, and not when it shifts the automatic default range (FR-6.1) — and neither does a Pending activity (FR-5.15) finishing or a Private location change.

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
1. Applied at ingest, server-side, before anything is stored: the leading points inside any Private location are dropped, and the track starts on that circle's edge instead (a point interpolated onto the boundary, whatever the recording's point density); the trailing points likewise. A track that only passes *through* a Private location mid-way is shown whole, by design: what a Private location protects is where a track starts and ends, and passing through one reveals neither.
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
1. Every activity in the window is grouped into the requested bucket by its `started_at` date, in the account's own timezone (FR-1.7), one bucket per calendar week or month that has at least one activity — buckets with nothing recorded are omitted rather than returned as zeroes, the same convention FR-6's histogram uses.
2. Each bucket reports: activity count, total distance, total moving time, and total elevation gain.
3. The Profile page renders the trailing 12 months as one bar per bucket, height scaled (logarithmically) to the window's busiest bucket by distance, with a Week/Month switch (`?bucket=month`; the Distance/Count grid setting is kept). Hovering a bar shows that bucket's full breakdown (distance, activity count, moving time, elevation gain); tapping or clicking one shows it in a line under the chart, and tapping it again hides it.

**Outputs**: `{bucket, from, to, periods: [{period_start, count, distance_meters, moving_seconds, elevation_gain_m}, ...]}`.

**Notes**: "Moving time" falls back to elapsed time for any activity ingested before moving- time detection existed — those activities have no moving-time figure of their own, so this bucket-level total uses whichever one each activity actually has, rather than a bucket going silently short. Best-effort curves and personal bests (formerly FR-9.2/FR-9.3) were built and then cut — deliberately out of scope, see §1.2 and §14.

## 12. FR-10 — Public pages: About, Help, Contacts

These are server-rendered pages (`IMPLEMENTATION.md` §4.19): each is a plain HTML page that needs no JavaScript and makes no API calls from the browser, with the page header every page shares (FR-10.4).

### FR-10.1 About page

**Description**: A public page at `/about` that explains what HoldMyTrack is, who it is for, what it deliberately is not, and how it is funded. A visitor or a search engine can read it without an account.

**Preconditions**: None — no session is needed; having one changes only the header (FR-10.4).

**Behavior**:
1. `GET /about` returns the page. It is also the site's front page: a visit to `/` without a session gets the same page (titled "HoldMyTrack — Every journey, mapped."), and both carry a canonical link to `/`, so search engines index the one URL. With a session, `/` is the map (FR-4).
2. It has sections for: what HoldMyTrack is, why someone might want it, what it isn't, how it is funded (section id `funding`), and a pointer to Contacts (FR-10.3).
3. "Try the demo — no signup" links to `/signin`, where the demo starts from its own button (FR-2.1). The page never starts a demo session itself.
4. About, Help and Contacts are reachable from every page's header and footer (FR-10.4) — the map and the sign-in pages included.
5. `/robots.txt` allows crawling except for `/v1/` and `/tiles/`, and points to `/sitemap.xml`, which lists `/`, `/help` and `/contacts` (`/about` is the same page as `/`).
6. The front page carries structured data (a schema.org `WebApplication`, as JSON-LD) naming the site, its URL, description and share image, and that it's free.

### FR-10.2 Help page

**Description**: A public page at `/help` that explains how HoldMyTrack works, in five sections reachable from jump links under its title: the map (the opening view, the Normal / Fog of War / Heatmap modes, and how Fog and Heatmap switch to whole states or regions, then whole countries, as the map zooms out — FR-4), the timeline (what a bar is, the selection band and how to extend, move or scroll it, and the phone slider — FR-6), getting activities in (files, `.zip` archives, Google Takeout with a link to Google's own download guide, the Android app, and what happens on a repeated or cross-source import — FR-3), exporting a map image (FR-4.10), and settings and privacy (Country, Timezone, Private locations — FR-1.7, FR-8.1 — and how to ask for an account to be deleted, which is by email to the Contacts address since there is no self-service deletion yet, and how to revoke Google's or Facebook's access on their side; its `#delete-account` anchor is the deployment's data-deletion instructions URL for Facebook, FR-1.10).

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
1. The header shows the logo, wordmark and tagline (the brand links to `/`), then Donate (FR-11.1), an "Info" menu listing About, Help and Contacts with the current page marked, and the account area.
2. Signed out, the account area is a "Sign in" link to `/signin`. With a session (real or demo), it is an account menu showing the account's avatar (or a generic icon) that opens to the account's email (a demo session shows its display name instead, followed by "Create your own account", FR-2.3), then "Profile" (`/profile`), "Settings" (`/settings`), "Admin" (`/admin`, an admin only — FR-12.1) and "Sign out".
3. Both menus open and close without JavaScript.
4. "Sign out" submits `POST /logout`, which ends the session the same way `POST /v1/auth/logout` does and redirects to `/`. The request is refused (`403`) unless its `Origin` header — or, without one, its `Referer` — is the app's own origin.
5. On a phone-width screen (≤768px) the tagline is hidden and Donate shows its heart alone; the Info and account menus stay.
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
1. The language is, in order: the account's Language setting (FR-1.7) when it is English or Русский; otherwise the language the browser or app asks for (`Accept-Language`, its most preferred supported language, matching `ru-RU` as Russian); otherwise English. A signed-out visitor gets their browser's language, or English — there is no switcher, cookie, or URL parameter for it.
2. The whole page is in that language: the shared header and footer (FR-10.4), every page including About and Help, the map app, `<html lang>`, the page title and description. The map app always shows the language the page around it is in.
3. Numbers and dates follow the language: `1,234.5 km` and `Sep 8` in English, `1 234,5 км` and `8 сент.` in Russian. Units are unchanged by language — they follow Country (FR-1.7).
4. Counts take the language's plural forms (`1 занятие`, `2 занятия`, `5 занятий`).
5. The verification and password-reset emails (FR-1.8, FR-1.5) are in the account's language, or when it has none set, the language of the request that sent them.
6. Error messages a person can see (a wrong password, a taken email, a rejected upload or track edit) are full sentences in the request's language, in page forms and in the JSON API's plain-text and `message` bodies alike. Machine-readable codes (`email_not_verified`, `demo_read_only`) never change with language.
7. Saving a different Language in Settings takes effect from that save's own reload onward, everywhere; other open pages change on their next load.
8. The Android app follows the phone's language, or its own per-app language (Android's Settings → Apps → HoldMyTrack → Language). It doesn't read the account's setting, and sends its language as `Accept-Language`, so the server's messages match it.

**Not translated**: activity names and descriptions people type, place names on the map, activity types outside the common set (shown as recorded), and the reasons an ingest or a `.zip` entry failed.

## 16. Non-Functional Requirements (summary)

This section summarizes cross-cutting behavior specified elsewhere in this document, for convenience — it does not introduce new requirements.

| Concern | Behavior |
| :-- | :-- |
| **Password storage** | bcrypt-hashed; plaintext is never stored or logged (FR-1.1). |
| **Session security** | Server-side, revocable sessions (not client-decodable tokens); expiry enforced server-side on every request, not trusted from the cookie alone (FR-1.4). |
| **Rate limiting** | Per-IP limits on the two endpoints reachable with no credentials at all: demo creation (FR-2.1) and password-reset requests (FR-1.5), 5/hour each. |
| **Non-blocking uploads** | No modal or locked UI during upload or processing (FR-3.1); the user can keep using the app while files process. |
| **No reload required** | Every list/summary this document describes updates itself automatically as background processing completes (FR-3.1, FR-3.4) — a manual page reload is never required to see current data. |
| **Idempotency** | Re-submitting the same activity content (FR-3.5) or the same password-reset token (FR-1.6) never has an effect beyond the first time. |

## 17. Mobile Browser Support

**Known issue**: The behavior below is what was designed and implemented, but the actual mobile experience has been reported directly as unusable, not just rough — this section describes intent, not a verified, working feature. Treat it as broken until re-verified on a real device and re-confirmed; see `docs/ROADMAP.md`'s "Mobile browser support" item (Phase 3).

**Description**: The application is usable in a phone-sized mobile browser, not just at desktop widths. This is a cross-cutting behavior, not a separate feature — it modifies how several of the FRs above render and are interacted with, rather than adding new ones.

**Behavior**:
1. Below approximately 768px viewport width, the Activities panel (FR-5.1) is a collapsible bottom sheet instead of a permanent sidebar, sitting directly above the date-range picker (item 2 below), which stays at the very bottom of the screen and visible in either sheet state — collapsed by default to a slim strip showing the range total and the Filter toggle, with the map fully interactive above it; tapping the strip expands it upward over the map to show the full list, filters, and footer actions (FR-5.2–FR-5.7), exactly as they behave at desktop width. Expanding or collapsing the sheet never changes the checked group (FR-5.6) or the row-click focus (FR-5.5). While an Edit track session (FR-5.14) is open the sheet is shown collapsed, whatever its state, so the editor window and the track stay visible; closing the editor returns the sheet to the state it was in.
2. The date-range picker (FR-6) is replaced by a plain two-knob slider with Earlier/Later buttons either side and the selected start and end dates shown under it — no bars drawn and no Shown days/Selected range legend. Like the bar chart it replaces, the slider counts only days with at least one activity; days without one take no room on the track. The track shows a window of 15 activity days, which on load is the 15 most recent, so the default selection (FR-6's 5 most recent activity days) has both knobs in view. The knobs mark day boundaries: the start knob where the first selected day begins, the end knob where the last one ends — so a one-day selection has its knobs one activity day apart, and the knobs can never be dragged closer than that. Earlier/Later move the window 5 activity days per tap, repeating while held, and stop at the first and the most recent activity day. A knob sitting on the edge the window moves toward is pulled along with it, extending the selection by up to 5 activity days per step — so a selection longer than the window is made by parking a knob on an edge and tapping or holding that edge's button; the other knob may scroll out of view, but its date stays in the label under the track. Pressing the track moves whichever knob is nearer to the pressed day boundary, and dragging carries it, always within the window. The selection is applied when a knob or a held button is released, not while it moves. Each knob is also keyboard-operable (arrow keys one activity day, Page Up/Down five, Home/End to the window's edges).
3. Trends (FR-9.1) — a hover-tooltip chart at desktop width — also responds to a tap: tapping a bar shows the same tooltip a hover would, tapping it again (or tapping empty chart space) hides it. A tap and a mouse hover never conflict with each other on the same chart.
4. Every other behavior in this document (upload, all three map modes, filtering, account settings) works the same way at mobile widths as at desktop widths.

**Explicitly not built** (hover-only, no touch equivalent, unlike Trends above — a continuous position read with no discrete point to tap, not a per-bar value): the colored zone segments' and pace/heart-rate + elevation profile's exact hover values (FR-4.8, FR-4.9), and the two-way map-track-hover ↔ Activities-row-underline highlight (FR-4.1, FR-5.4). Both remain mouse-only; a touchscreen user can still see the colored bands and elevation curve themselves, and can still focus/select a track by tapping it, just not read an exact value by touch alone the way a mouse hover shows one.

## 18. Out-of-scope items, tracked for future revisions of this document

The following are named in `VISION.md`'s roadmap but have no functional requirements in this document because they are not yet built:

- Path 1 cloud-provider connectors (Garmin, Wahoo, COROS)
- Path 2 on-device sync's iOS half (Apple HealthKit — Android's Health Connect half is FR-3.6)
- Explorer-tile gamification
- The rest of "Export" — story cards, animated reveals (high-resolution map export itself is built, FR-4.10)
- Dark-theme variant of the Fog of War veil (the theme parameter is accepted but currently has no visual effect on the veil itself)

Deliberately out of scope, not a "not yet" — built and then cut, not planned to return: Oura and other recovery-data sources (sleep, HRV, readiness), best-effort curves, personal bests, power curves, and training load. Also deliberately out of scope: splitting a track that passes through a Private location mid-way (FR-8.1 hides only the leading and trailing portions, by design). `VISION.md` §1.1 draws a hard line against HoldMyTrack being a health or fitness advisor; pace and heart rate stay as per-activity route context (FR-4.9), not an analysed, all-time performance record.