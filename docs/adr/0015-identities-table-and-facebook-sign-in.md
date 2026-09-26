# ADR-0015: Sign in with Facebook, never linked by email, with identities in their own table

## Status

Accepted. Supersedes ADR-0009's decision to store the Google link as a `users.google_sub` column; the rest of ADR-0009 stands.

## Context

Sign in with Google (ADR-0009, `SPEC.md` FR-1.9) was the only external sign-in. Facebook is a second, for visitors who'd rather use it. Two things had to be decided: how a Facebook identity finds its account, and where the link between an account and its external identities is stored now there are two providers.

The first is the one with a security edge. ADR-0009 links Google to an existing account by email, which is safe because Google's id_token carries `email_verified`, and the flow refuses an unverified one. Facebook's web flow returns an access token, not an id_token; the profile it gives (`/me?fields=id,name,email`) has no verified flag, and Meta doesn't document that the email it returns has been confirmed. Anything that trusts that email the way ADR-0009 trusts Google's lets whoever can put an unconfirmed address on a Facebook account into the HoldMyTrack account with that address.

## Decision

**A Facebook identity is matched by its app-scoped Facebook id only.** When no account has that id, and the email Facebook returns already belongs to an account, the sign-in is refused with a message saying so — the person signs in the way they already do. Otherwise a new account is created **unverified** and goes through email verification (FR-1.8) exactly like a password sign-up. A Facebook account with no email (registered by phone, or the permission declined) is refused too, since every account here needs one.

**Google's unverified-account case drops other identities.** Since Facebook now creates unverified accounts, ADR-0009's "an unverified account's password is cleared when Google links it" extends to its Facebook link: whoever linked it never proved they own the address either.

**External identities move to `user_identities (user_id, provider, subject)`**, primary key `(provider, subject)`, one identity per provider per account, cascade-deleted with the user. The migration copies every `google_sub` in and drops the column — the move ADR-0009 said to make once a second provider arrived.

**The flow is Facebook Login's manual code flow, server-side, no library, no SDK** — ADR-0009's reasoning, with two differences Facebook forces: no PKCE (its web flow doesn't document it; the code is useless without the app secret, and `state` still binds the callback to the browser), and a Graph API call for the profile, made with `appsecret_proof`. The provider-independent parts — the round-trip cookie, the callback's checks, the session start — are shared with Google's code rather than copied.

## Alternatives considered

- **Link by email, like Google.** Rejected for the reason in Context. It's the smoothest path for someone who signed up with a password and later clicks the Facebook button, and that person now gets a refusal instead — the cost of this decision.
- **Link by email only to an already-verified account.** Rejected: that's the dangerous direction. The verified account is the victim's; an attacker's Facebook account carrying the victim's address, unconfirmed, would get straight in.
- **Create new Facebook accounts already verified**, skipping the verification email. Rejected: it would let someone claim an address they don't own and hold it — the victim's own later sign-up would find the email taken.
- **Linking from Settings for a signed-in account.** Deferred, not rejected: it's the safe way to connect Facebook to an existing account (the person has already proved who they are), but it needs a Settings flow for managing connected sign-ins. Nobody has asked for it yet.
- **Keep columns — add `users.facebook_id` next to `google_sub`.** Rejected: every lookup, the password-reset filter and the unverified-account cleanup would need a branch per provider, and Apple sign-in (likely, with an iOS app) would add a third.
- **`golang.org/x/oauth2`**, which ADR-0009 said to revisit at a second provider. Rejected again: what it would replace is two small HTTP calls per provider, and the shared helpers already cover the rest.

## Consequences

- Someone with a password or Google account can't start using Facebook with it — they get "An account with this email already exists" and keep signing in the way they did.
- A Facebook sign-up waits for its verification email, where a Google sign-up doesn't. The first sign-in is less smooth; the address is proven the same way a password sign-up's is.
- The error for an existing email tells the caller that address has an account. The sign-up endpoint's `409` already does, so no new enumeration channel opens.
- A Live Meta app needs a privacy policy URL and data-deletion instructions. With no self-service account deletion yet, the instructions are a Help section pointing at email — to revisit when deletion is built.
- The Graph API version is pinned in one constant and needs bumping about every two years as Meta retires versions.
- Android and iOS get no Facebook sign-in from this; a client-token endpoint would need its own `debug_token` verification, as ADR-0009 noted for Google's.