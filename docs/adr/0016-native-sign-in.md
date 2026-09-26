# ADR-0016: Native sign-in — Credential Manager for Google, a browser-tab handoff for Facebook

## Status

Accepted. Extends ADR-0009 (Google) and ADR-0015 (Facebook) to the Android app; both stay as they are for the web.

## Context

The web signs in with Google and Facebook through server-side redirect flows that end in a session cookie (ADR-0009, ADR-0015). The Android app has no cookie jar: it signs in through JSON endpoints and keeps the response's `session_token` as a bearer token (`apps/android/docs/ARCHITECTURE.md`). Each provider therefore needed two things on Android: a way to prove the person's identity on the phone, and a server endpoint that turns that proof into a `session_token`.

The two providers offer different native tools. Google's is Credential Manager: a system account picker that returns a signed Google ID token and needs no browser. Facebook's is Meta's Facebook Login SDK, which signs in through the installed Facebook app and returns an access token. The alternative for Facebook is its web login dialog, which is what the server already runs for the web.

## Decision

**Google: Credential Manager, verified against Google's keys.** The app asks Credential Manager for an ID token for the server's *web* client ID (`serverClientId`), which it reads from `GET /v1/auth/providers` (`google_client_id`) rather than from a build setting that could disagree with the server. It posts the token to `POST /v1/auth/google/token`. The web flow can skip signature checks because its token comes straight from Google's token endpoint; this one comes from a client, so the server verifies its RS256 signature against Google's published keys (`https://www.googleapis.com/oauth2/v3/certs`, cached per its `Cache-Control`) with the standard library, then applies the web flow's claim checks and account resolution unchanged. Still no JWT or OAuth library, as in ADR-0009.

**Facebook: the existing web flow in a browser tab, handed back to the app.** The app opens `GET /v1/auth/facebook/start` in a Chrome Custom Tab with `app_challenge` added: the S256 hash of a random verifier that never leaves the app. The flow runs exactly as on the web until the callback, which, instead of setting a session cookie in the tab, stores a one-time code (`auth_handoffs`, two minutes) and redirects to `holdmytrack://oauth?code=…`. The app redeems the code at `POST /v1/auth/handoff` with its verifier and gets the same session JSON as a password sign-in. Failures return to the app too (`holdmytrack://oauth?error=…`, the web's error codes). The handoff lives in the provider-independent `oauth.go`, so it isn't Facebook-specific, but only Facebook uses it.

The verifier is what makes a custom URL scheme safe to use here. Any app can register `holdmytrack://` and receive the redirect, but a code is useless without the verifier, and a wrong verifier burns the code. This is RFC 8252's pattern for native apps: an external user agent, PKCE, and a redirect back into the app.

## Alternatives considered

- **Meta's Facebook Login SDK.** Rejected. Its one advantage is a smoother sign-in when the Facebook app is installed. Against it:
  - The SDK logs app events and collects the advertising ID by default, which has to be opted out of in the manifest and declared in the Play data-safety form. That's hard to square with a product that promises no tracking.
  - It needs a client token and each signing key's hash registered with Meta.
  - The server would need a second Facebook path: verifying a client-supplied token with `debug_token`, which the web flow never needs (ADR-0015).
  - An iOS app would need Meta's iOS SDK as well; the browser handoff works for it unchanged.
- **Google through the same browser-tab handoff.** It would work, and would need no Android OAuth client in Google Cloud. Rejected: Credential Manager is the platform's own sign-in UI (one tap, the device's accounts, no browser), which the roadmap had already chosen.
- **Returning the session token itself in the redirect URL.** Rejected: any app registered for the scheme could read it, and it would end up in the browser's history. A one-time code bound to the app's verifier has neither problem.
- **An Android App Link (`https://…`) instead of a custom scheme.** Deferred. It would stop other apps receiving the redirect at all, but it needs `assetlinks.json` served per domain and doesn't work against a local dev stack. PKCE already makes an intercepted code worthless.

## Consequences

- Google sign-in on Android needs an **Android OAuth client** in the same Google Cloud project, one per signing key (package name plus SHA-1). Without it Credential Manager fails with a generic error. `docs/DEPLOY.md` covers it.
- Facebook needs no new Meta setup: the tab uses the same callback URL as the web. While the Meta app is in Development mode, only people with a role on the app can sign in, on the web and Android alike.
- Facebook sign-in on Android opens a browser tab rather than the Facebook app, so a person not logged in to Facebook in Chrome types their Facebook password there.
- New Facebook accounts are unverified (ADR-0015), and the Android app has no "verify your email" screen yet. Until it does, such an account sees an empty map on Android until the email link is clicked (`docs/KNOWN_ISSUES.md`). The same is true of an email sign-up.
- The server now fetches Google's keys at runtime. If that fetch fails, Android Google sign-in fails until it succeeds; web Google sign-in doesn't depend on it.