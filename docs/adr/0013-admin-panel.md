# ADR-0013: An in-app, read-only admin panel, gated by a CLI-granted flag

## Status

Accepted.

## Context

HoldMyTrack had no admin role and no admin UI. `SPEC.md` said so outright: "There is no administrator role … and no concept of one account viewing another's data." Operating the product still means looking at accounts and their activities: who signed up, which accounts are empty, what an account has stored, and the id of one specific activity. The only way to answer any of that was raw SQL over SSH against the production database, which is slow, easy to get wrong, and has no guard rails.

The immediate need was looking up specific activities by id, but the gap is general. Every operational feature that comes later (support, abuse handling, moderation once there's a social graph, demo curation) needs a place for an operator to look first.

Users trust the product with raw GPS files, often starting and ending at home. An operator view of their data is expected, as it is for any hosted service. That trust is also why the view needs a hard gate that isn't a web form.

## Decision

**Add an admin panel to the app itself: server-rendered pages at `/admin`, read-only to start.** `/admin` lists every account; `/admin/users/{id}` lists one account's activities, each with its id, dates, type, name, distance, source, countries and regions, and duplicate/hidden/edited state. The pages follow ADR-0012: Go templates with the shared header, no script.

**The role is one boolean column, `users.is_admin`, granted and revoked only by a CLI subcommand**, `holdmytrack set-admin <email> true|false`, run on the server. No HTTP route writes it.

**To everyone who isn't an admin, the panel is a 404**, the same page a mistyped URL gets, not a 403. Demo sessions never get in, whatever the flag says.

## Alternatives considered

- **Keep using SQL over SSH.** No code, but every question is a hand-written query against production. Nothing stops a mistaken `UPDATE`, and no one but the person with the shell can help. It doesn't grow into the operator tooling the product will need.
- **A separate admin app or service** (its own binary, port or subdomain, maybe behind a VPN). Stronger isolation, but a second deployable, a second auth story and a second place for templates, for what is today two read-only tables. The in-app version reuses sessions, the header, the page renderer and the formatting helpers as they are. It can still be moved behind a separate hostname in Caddy later without code changes.
- **An off-the-shelf database admin UI** (pgAdmin, Adminer, a hosted BI tool). Shows raw tables, not the product's own view of an account (live vs superseded, the account's timezone, its units). It's another exposed service with its own credentials, and it offers write access to everything by default.
- **Admin granted from the web** (an allowlist of emails in config, or a UI to promote users). An allowlist in `.env.prod` is only slightly different from the CLI, but it silently grants on signup to whoever registers that email first. A promotion UI makes any admin-session compromise an escalation path. The CLI puts the grant behind the server's own shell access, which is already the trust boundary.
- **403 instead of 404.** More honest to a legitimate user who lost access. It also tells anyone probing that the path exists and is worth attacking, for no benefit to anyone who should see it.
- **An `admin` role inside a general roles/permissions table.** Right once there are several operator roles with different rights. With one role, it's schema and code for a need that doesn't exist yet. A boolean is easy to migrate from when it does.

## Consequences

- An operator can see every account and look up any activity id from a browser, without touching the database.
- `SPEC.md`'s data-isolation rule (FR-8.2) gains one stated exception: the admin pages take an account id in the path. Every other endpoint still derives the account from the session.
- Granting the first admin needs shell access to the server (`DEPLOY.md`). There's no self-service recovery if every admin loses access, by design.
- The flag is read on every admin page load, so revoking takes effect immediately without ending sessions.
- The panel is read-only, so a leaked admin session exposes account metadata and activity lists, but can't change anything. Adding admin *actions* (copying an activity to the demo, disabling an account) or showing tracks raises the stakes. Those should come with an audit log of who did or saw what, and are separate decisions.
- Paging is `LIMIT/OFFSET` and the accounts list is unpaged, fine at today's sizes. Both need revisiting (keyset paging, search) once the account count or a single account's history gets large.