# Brainstorm

Ideas that have been raised but not decided — not yet reviewed for whether HoldMyTrack should build them, and not sequenced against anything in `docs/ROADMAP.md`. This is the opposite direction from that file: `ROADMAP.md` is committed, sequenced work; this file is a holding area for things that need a decision before they could become a `ROADMAP.md` item at all. `docs/VISION.md`'s own "not committed" phases (§5.7 Social, §5.8) stay in `VISION.md` itself rather than here, since they already have a stated position (deliberately unscheduled, pending §6's funding numbers) — an idea moves here when it has no such position yet, just a description and open questions.

Once an idea here is decided, it moves out: accepted work goes to `docs/ROADMAP.md` (or straight into `SPEC.md`/`IMPLEMENTATION.md` if small enough to just build), and rejected ideas are deleted rather than kept as a record — this file has no obligation to remember what was said no to.

---

### Place names on the map in the reader's language

The map's labels (cities, countries, streets) are whatever `@protomaps/basemaps` picks, which is mostly each place's local name, while the interface around it is in English or Russian (FR-13). Protomaps' layers take a `lang` option and fall back to the local name where OSM has no translation.

* The style is built once, ahead of time (`npm run build:style`, checked by `verify:style`), and served as a static document, so this would mean one style per language (served by `mapstyle`, keyed by the page's language), not a runtime switch.
* The export image (FR-4.10) would carry the labels too, which is either a feature or a surprise, depending on who the image is for.

### More languages, and language-specific URLs

English and Russian ship (ADR-0014). Every further language is a translation pass over three catalogs and two prose pages, which is cheap once someone can review it. If search traffic in other languages ever matters, `/ru/help`-style URLs with `hreflang` would let search engines index each language, which the current one-URL-per-page approach can't.

* Spanish and German are the obvious next candidates, for reach and for their long words respectively (the second is a good layout stress test).
* The Android app could read the account's Language (it's in `GET /v1/auth/me`) and apply it as its per-app language, so the two apps agree without the user setting it twice.

### Prefill Country at first run from a coarse IP lookup

Every new account now picks a Country before it reaches the map (`SPEC.md` FR-1.7 behavior 5, the first-run setup screen), from a list of about 250 with no preselection — one more decision standing between signup and a first look at the map. Prefilling it from the signup request's IP would turn that into a confirmation for most people. Until Country is saved the app displays metric (FR-1.7 behavior 3), and the map's zero-history fallback (`IMPLEMENTATION.md` §4.13) reads whatever `users.country` holds.

* Resolve country from the signup request's IP address (a self-hosted database like MaxMind's free GeoLite2 — no external API call needed, no extra request from the client) and either store it as a suggestion or preselect it on the first-run screen — the user still confirms it there, so it never becomes a silent default. Same remote-address extraction the demo rate limiter already does (`IMPLEMENTATION.md` §4.10) — build it as shared server-side logic rather than a second lookup.
* Would stay a normal, editable Settings field afterward — never locked. Country-level accuracy from a database like GeoLite2 is good but not perfect (VPNs, corporate networks, travel), so this would be a sensible default, not an authoritative fact about the account.
* Browser locale (`Accept-Language`) and the browser Geolocation API were both considered and rejected as the signal here: locale reflects a language/OS preference, not physical location (someone with `en-GB` set while living elsewhere gets the wrong answer), and the Geolocation API needs an explicit permission prompt — exactly the kind of signup friction an easier path would want to avoid. IP geolocation needs neither.
* If the app itself is ever served through a CDN that injects a country header on every request (e.g. Cloudflare's `CF-IPCountry` on a proxied hostname), that becomes a free replacement for the self-hosted lookup — worth revisiting then.

### Cap Normal mode at the 500 most recent activities

Normal mode fetches every activity in the selected date range in one response (`GET /v1/activities` has no limit) and the Activities panel renders every row; track tiles grow with the number of tracks. Fine today — measured with the Demo Customer's 611 activities, the all-time list is 189 KB and the home-area track tile 44 KB — but unbounded: ten years of daily walks is ~5,000 rows, ~1.5 MB and 5,000 rows in the page. Proposed: the app handles 500 activities at once; if a range holds more, it shows the 500 most recent and declines the rest, with a notice. Fog and Heatmap are how you see everything — precomputed rasters whose cost doesn't grow with history — so a cap on Normal mode isn't a paywall on the whole picture.

* Decided so far: the 500 *most recent* in the range; the selected range itself is left as the user picked it; Android gets the cap and the notice too.
* Mechanism: a start-time cut-off, not a `LIMIT`. The server finds the start of the 500th most recent activity in the range (one query on `idx_activities_live`, `OFFSET 499 LIMIT 1` with `COUNT(*) OVER ()` for the total) and narrows the filter's `from` to it — applied in the list, the summary totals and every track tile. Tiles are queried independently, so only a shared cut-off keeps the list, the totals and the map agreeing on the same 500; per-request top-500s wouldn't. Ties at the exact cut-off second may admit a 501st; acceptable.
* The list response gains `total` (activities in the range before the cap) and `limit` (500), so no client hard-codes the number. Deliberately uncapped: the histogram (the backdrop showing where activity exists), duplicates, the profile page's graph stats and trends, per-activity endpoints.
* Web: a note at the top of the Activities list — "Showing the 500 most recent of 1,234 activities in this range. Narrow the range to see older ones, or switch to Fog or Heatmap to see everything." Type facets, Select all, Invert selection and the edit dialog's type picker already derive from the loaded list, so they follow the cap unchanged.
* Android has no date range: it requests all-time tiles and reads the unfiltered list for its opening view (`activityBounds`) and the recording Type picker's counts (`activityTypeCounts`). With the cap its map shows the 500 most recent tracks; `activityBounds` would read `total`/`limit` from the same response and show a long Toast (the map screen's existing notice style). The Type picker would count only the 500 most recent — acceptable, since it also takes free text.
* Would make the per-user quota item in `ROADMAP.md` Phase 5 purely about storage and ingest, since viewing is bounded.
* Verify with the demo account: all-time list returns 500 rows with `total: 611`; summary `count: 500`; an all-time z4 tile holds only those 500 ids; a two-week range is unchanged. The existing suites use an account under 500, so they should pass unchanged.

### Elevation map layer

A map layer that shows terrain height, so the shape of the land under your tracks — valleys, climbs, ridgelines — reads at a glance. Raised when the pace/heart-rate + elevation profile card was removed (ADR-0017): elevation stays in the data as route information, and this is the exploration-shaped way to show it, rather than a per-activity chart.

* Two very different things could be meant: a basemap hillshade/contour layer from a public elevation model (the same everywhere, nothing to do with the user's data), or the user's own tracks colored by the elevation they recorded. The first is a basemap and cost question (a DEM tileset to host, `VISION.md` §4.3); the second reuses `activity_streams.elevation_m` and the band machinery pace already has.
* Many GPS-only recordings have no elevation at all, so a track-colored version would be patchy in exactly the way the old all-or-nothing profile was.

### Per-region stacks if a data-localization law applies

Most privacy laws outside the EU (Brazil's LGPD, Australia's Privacy Act APP 8, India's DPDP) restrict transfers abroad rather than requiring local servers, so contracts and disclosures satisfy them without new infrastructure. A few (China's PIPL, Russia's 152-FZ, parts of Vietnam's and Indonesia's rules) do require storing data in-country. If one of those ever applies with enough users behind it to justify the cost, the shape would be one independent copy of today's stack per region (`compose.prod.yml` with its own Postgres, worker and object storage), with every account living in exactly one region. `ROADMAP.md`'s EU-region hosting item is the one-region case of this and doesn't need it: a single EU deployment serves everyone.

* The design already suits it: no social graph (`VISION.md` §5.7–§5.8) means no feature reads another account's data, user IDs are UUIDs so they stay unique across regions, and the basemap is public and stays global on one CDN.
* Still to build: a small global directory mapping an email or sign-in identity to its home region (or per-region subdomains the user picks at sign-in), a home region chosen at signup and defaulted from Country, deploys that roll out to N stacks, and the Demo Customer and admin boundaries seeded in each.
* The takeout export (`internal/takeout`) plus a matching import would move an account between regions.
* Each region is a fixed monthly cost against donation funding (`VISION.md` §6), so the alternative for a localization country with few users is declining signups there rather than running a stack for them.
* Until then, the only cost is not closing the door: keep each deployment fully configured by env, and ask whether any new cross-account query would still work split by region.

### Photos as links to the user's own cloud storage

ADR-0024 keeps a resized copy of each photo on our own storage. If photos ever become a material share of the bill, the alternative it names is to keep only our thumbnail and link to the original wherever the user already keeps it — Google Drive, Dropbox, OneDrive — so the map and Stories keep working on our thumbnails and the full image is a click out.

* Roughly a fifteenth of today's storage per photo, at the cost of an OAuth integration per provider, a photo that only opens once the user shares it "with anyone with the link", and a broken link every time the user tidies their files.
* Google Photos can't serve as one: its API hands out image URLs that expire within the hour.
* Decide only once cost-per-user is measured (`ROADMAP.md` Phase 5) and photos show up in it.

### Deploy on merge

Deploys to `holdmytrack.com` are run by hand over SSH (`docs/DEPLOY.md` §6). What held back deploying every merge to `main` was that the server held the only copy of real synced history; nightly backups and a monthly restore drill now exist (ADR-0029), so a bad deploy can be undone.

* A GitHub Actions job would need an SSH key to the server in the repository's secrets, which is a new way in to the box; that key should only be able to run the deploy, not get a shell (`restrict,command="…"` in root's `authorized_keys`, since the server takes SSH as root by key only, `docs/DEPLOY.md` §12).
* A deploy with a migration needs `backup.sh` and maintenance mode around it (`docs/DEPLOY.md` §7), and the job can't tell in advance whether a migration is going to go wrong.
* The build runs on the 2 vCPU / 4 GB box itself, so every merge would mean several minutes of high load there; building images in CI and pulling them would avoid that but needs a container registry.