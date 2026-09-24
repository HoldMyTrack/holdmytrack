# ADR-0009: Sign in with Google is a server-side authorization-code flow with no OAuth library, auto-linking by verified email

## Status

Accepted.

## Context

Accounts were email+password only (`IMPLEMENTATION.md` §4.9), with email verification (`SPEC.md` FR-1.8) standing between a new signup and the map. That is a signup form, a password to invent and an inbox round trip before a visitor sees anything of their own. Most visitors already have a Google account, and "Continue with Google" removes all three steps, because Google has already verified the address.

Three things had to be decided: where the OAuth flow runs, how much code to take on to run it, and what happens when a Google identity's email already belongs to an account here. A fourth, smaller one: how the link between a Google identity and an account is stored.

## Decision

**The flow runs entirely on the server, as the OAuth 2.0 authorization-code flow with PKCE.** The web client's button is a plain link to `GET /v1/auth/google/start`; the server redirects to Google, Google redirects back to `GET /v1/auth/google/callback`, and the callback sets the same `holdmytrack_session` cookie every other sign-in path sets. The round trip's `state` and PKCE verifier live in a short-lived `HttpOnly` cookie, not a table.

**No OAuth or JWT library.** The `id_token` is received directly from Google's token endpoint over TLS, which OpenID Connect Core §3.1.3.7 accepts in place of verifying its signature. What's left is decoding one base64url segment and checking `iss`, `aud`, `exp` and `email_verified` — a few dozen lines of stdlib, in line with `internal/mail`'s zero-new-dependency SMTP client.

**An existing account with the same email is linked automatically**, since Google only asserts verified addresses. If that account's own email was never verified, linking also removes its password and ends its sessions: whoever set that password never proved they own the address, and leaving it would let someone pre-register a victim's email and keep a way in after the victim signs in with Google. An account already linked to a different Google identity is refused rather than relinked.

**The link is a `users.google_sub` column**, matched by Google's stable `sub` rather than by email, so either side can change its address without breaking it.

The feature is optional per deployment: an empty `GOOGLE_CLIENT_ID` turns it off completely, and email+password never depends on it.

## Alternatives considered

- **Google Identity Services' JavaScript button, posting an ID token to the API.** Rejected. It loads Google's script on the sign-in page of an app whose privacy stance is a selling point, and the ID token arrives from the browser rather than from Google, so the server would have to fetch and cache Google's JWKS and verify RS256 signatures. The server-side flow needs neither. The Android app will need exactly that verification (Credential Manager hands the app an ID token), so this is deferred to when Android sign-in is built, not avoided forever.
- **`golang.org/x/oauth2` plus a JWT/OIDC library (`coreos/go-oidc`).** Rejected for now. They would handle discovery, JWKS rotation and multiple providers, none of which one fixed provider using the code flow needs. Worth revisiting if a second identity provider arrives, or when Android's token verification lands.
- **Refuse to link and ask the person to sign in with their password first.** Rejected. It is safer only in the case that's already covered (an unverified password account), and it strands the common case — someone who signed up with a password months ago and now clicks the Google button — behind an error screen and a Settings flow that would have to be built for it.
- **A separate `user_identities (user_id, provider, subject)` table.** Rejected while Google is the only identity provider. A column is one migration and one unique index; a table is the right move the day Apple sign-in (likely, for the iOS app) makes it two, and migrating one column into it is cheap.

## Consequences

- The web client needs no Google script and no new dependency; nothing about sign-in changes for a deployment that doesn't configure Google.
- The callback is a full-page redirect, so failures can only be reported by redirecting back with a query parameter. They are deliberately generic (`?auth_error=google`), and the cause is only in the server log.
- Auto-linking makes Google's email verification part of this app's security boundary: anyone who controls a Google account for an address can sign in to the HoldMyTrack account with that address. That is the same trust the password-reset email already places in whoever controls the mailbox.
- Google-only accounts have no password until they set one through Forgot password, so `sendPasswordReset`'s filter had to widen to include them.
- In dev, where the app and the API are separate origins, `GOOGLE_REDIRECT_URL` must be set explicitly to the API's own origin; in production (ADR-0006, one origin) the default is right.
- Android sign-in needs a second, client-token endpoint with real signature verification. That work is recorded in `docs/ROADMAP.md`.