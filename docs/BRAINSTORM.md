# Brainstorm

Ideas raised but not decided, and not sequenced against `docs/ROADMAP.md`. Milestone 3 stays in `VISION.md` §5.7–§5.8, since it already has a stated position. A decided idea leaves this file: accepted work goes to `ROADMAP.md`, or into `SPEC.md`/`IMPLEMENTATION.md` if small enough to build, and a rejected one is deleted.

---

### More languages, and language-specific URLs

English and Russian ship (`IMPLEMENTATION.md` §4.21). Each further language is a translation pass over three catalogs and two prose pages, once someone can review it. `/ru/help`-style URLs with `hreflang` would let search engines index each language.

* Spanish and German are next: reach, and long words to stress layout.
* The Android app could apply the account's Language (in `GET /v1/auth/me`) as its per-app language, so the user doesn't set it twice.

### Prefill Country at first run from a coarse IP lookup

Every new account picks a Country on the first-run screen (`SPEC.md` FR-1.7) from about 250 with none preselected. Prefilling it from the signup request's IP would make it a confirmation for most people. The map's zero-history fallback (`SPEC.md` FR-4.5) reads whatever `users.country` holds.

* Resolve it from a self-hosted database such as MaxMind's free GeoLite2 and preselect it for the user to confirm, reusing the demo rate limiter's remote-address extraction (`IMPLEMENTATION.md` §4.10). It stays editable in Settings, since VPNs and travel make it wrong.
* Rejected signals: `Accept-Language` reflects a language preference, not location, and the Geolocation API needs a permission prompt, which is the friction this removes.
* A CDN country header (Cloudflare's `CF-IPCountry` on a proxied hostname) would replace the lookup.

### Cap Normal mode at the 500 most recent activities

`GET /v1/activities` has no limit, the Activities panel renders every row, and track tiles grow with the number of tracks. A 611-activity account's all-time list was 189 KB and its home-area track tile 44 KB; ten years of daily walks would be ~5,000 rows and ~1.5 MB. Proposed: if a range holds more than 500 activities, show the 500 most recent with a notice. Fog and Heatmap are precomputed rasters whose cost doesn't grow with history, so they still show everything.

* Decided: the 500 most recent in the range; the range stays as picked; Android gets the cap and notice too.
* Mechanism: the server finds the start of the 500th most recent activity in the range (`OFFSET 499 LIMIT 1` with `COUNT(*) OVER ()` on `idx_activities_user_time`) and narrows the filter's `from` to it in the list, the summary totals and every track tile. Tiles are queried independently, so only a shared cut-off keeps all three on the same 500, which a `LIMIT` wouldn't. A tie at the cut-off second may admit a 501st.
* The list response gains `total` (before the cap) and `limit` (500). Left uncapped: the histogram, the profile page's stats and trends, per-activity endpoints.
* Web: a note atop the Activities list pointing to a narrower range or to Fog and Heatmap. Type facets, Select all, Invert selection and the edit dialog's type picker derive from the loaded list and follow unchanged.
* Android requests all-time tiles and reads the unfiltered list for `latestActivityBounds` and the Type picker's `activityTypeCounts`. With the cap its map shows the 500 most recent tracks, `latestActivityBounds` reads `total`/`limit` and shows a long Toast, and the Type picker, which takes free text, counts only those 500.
* The per-account limits (ADR-0043) leave one view reading up to 50,000 activities; this bounds it.
* Verify on a load-test account over 500 activities (`docs/PERFORMANCE.md`): the all-time list returns 500 rows with `total` the full count, summary `count: 500`, an all-time z4 tile holds only those ids, and a two-week range is unchanged.

### Elevation map layer

A layer showing terrain height under your tracks. Elevation stays in the data as route information (ADR-0017); this would show it as exploration instead of a per-activity chart.

* Two different things: a basemap hillshade/contour layer from a public elevation model, which is a hosting and cost question (`VISION.md` §4.3), or the user's tracks colored by recorded elevation, which reuses pace's band machinery with elevation read from each original upload, since per-point elevation isn't stored.
* Many GPS-only recordings have no elevation, so a track-colored version would be patchy.

### Per-region stacks if a data-localization law applies

Most privacy laws outside the EU (Brazil's LGPD, Australia's APP 8, India's DPDP) restrict transfers abroad, which contracts and disclosures satisfy. A few (China's PIPL, Russia's 152-FZ, parts of Vietnam's and Indonesia's rules) require in-country storage. If one applies with enough users to justify it, the shape is one independent copy of the stack per region (`compose.prod.yml` with its own Postgres, worker and object storage), each account in exactly one. `ROADMAP.md`'s EU-region hosting is the one-region case and doesn't need this.

* No feature reads another account's data except the Story copy job (ADR-0036), which would have to work across regions or be refused across them, as would a Milestone 3 social graph (`VISION.md` §5.7). User IDs are UUIDs, and the public basemap stays global on one CDN.
* To build: a global directory from sign-in identity to home region (or per-region subdomains), a home region chosen at signup and defaulted from Country, deploys to N stacks, and the Demo Customer and admin boundaries seeded in each. Download your data (`internal/export`) plus a matching import would move an account.
* Each region is a fixed monthly cost (`VISION.md` §6); for a country with few users the alternative is declining signups there.
* Until then: keep each deployment configured entirely by env, and check that any new cross-account query would still work split by region.

### Photos as links to the user's own cloud storage

ADR-0024 keeps a resized copy of each photo on our storage. If photos become a material share of the bill, the alternative it names is keeping only our thumbnail and linking to the original in Google Drive, Dropbox or OneDrive.

* About a fifteenth of the storage per photo, at the cost of an OAuth integration per provider, originals shared "with anyone with the link", and links that break when the user tidies files.
* Google Photos can't serve: its API's image URLs expire within the hour.
* Decide once cost per active user is known (`IMPLEMENTATION.md` §4.32 keeps the active counts) and photos show up in it.

### Deploy on merge

Deploys to `holdmytrack.com` run by hand over SSH (`docs/DEPLOY.md` §6). Nightly backups and a monthly restore drill (`docs/DEPLOY.md` §11) mean a bad deploy can be undone.

* A GitHub Actions job's SSH key in the repository's secrets is a new way in, so it should only run the deploy (`restrict,command="…"` in root's `authorized_keys`; the server takes SSH as root by key only, `docs/DEPLOY.md` §12).
* A deploy with a migration needs `backup.sh` first, and maintenance mode when the migration says so (`docs/DEPLOY.md` §7). The job can read that marker but can't tell whether a migration will go wrong.
* The build loads the 2 vCPU / 4 GB box for minutes; building images in CI avoids that but needs a container registry.

### Garmin activities beyond file upload

Garmin users come in through file upload (Path 3, `VISION.md` §4.1); no Garmin connector is planned. Other ways in, as researched on 2026-10-11:

* **Account export.** Garmin Connect's Data Management export keeps each activity's original FIT, GPX or TCX in nested zips under `DI_CONNECT/DI-Connect-Uploaded-Files/` (`DI-Connect-Fitness-Uploaded-Files/` in some), arriving within 48 hours to 30 days. Archives are refused (ADR-0039), so the user unzips and picks files; a Help entry could walk through it.
* **Official API.** The Connect Developer Program has taken no applications since about spring 2026 ("Under Construction", no reopening date; [Terra, 2026-09-07](https://tryterra.co/blog/garmin-connect-developer-program-pause)). It admits only legal entities, with no exemption found for free apps. The [agreement](https://developerportal.garmin.com/sites/default/files/Garmin%20Connect%20Developer%20Program%20Agreement.pdf) charges nothing today but allows a fee on 30 days' notice and usage charges over limits; a ~$5,000 one-time fee appears only in third-party reports. Applying needs the legal entity first (as `ROADMAP.md`'s Facebook item does); the connector would follow Path 1's picker and deauthorization deletion.
* **Via Health Connect.** Garmin Connect writes to it since 5.14.1 (July 2025), on Android 14+. Its published workout fields include no route, and iPhone users report Garmin workouts reaching Apple Health without one, so Garmin sessions would reach the Sync sheet routeless and not be offered, like Samsung's. A phone paired with a Garmin watch would settle it.
* **Unofficial client.** [python-garminconnect](https://github.com/cyberjunky/python-garminconnect) (MIT) logs in as Garmin's Android app: email and password to `sso.garmin.com`, an MFA code if asked, then a ticket exchanged at `diauth.garmin.com` for tokens. On the server it would hold every user's credentials and send all requests from one address that Garmin can block at once. In the browser a CORS preflight from `https://holdmytrack.org` got `403` with no `Access-Control-Allow-Origin` (2026-10-11), so it would need the server as a proxy. In the Android app credentials stay on the phone, Garmin activities with a route join the Sync sheet, and only each ticked one's original FIT is sent, so the server is unchanged and heart rate stays unread (ADR-0017).
* Open for the Android version: it breaks whenever Garmin changes its login (see the library's issue #369); Play review might object; Garmin's consumer terms have no clause found on automated access. It would want an ADR and copy saying it's unofficial.

### Cloud connectors (Path 1)

No connector is planned before launch; which services to connect is decided from what people ask for after it, Wahoo and COROS being the likeliest (`VISION.md` §4.1). Any of them would take:

* A partner-API application per provider, filed as soon as one is decided, since approval takes time.
* Provider-agnostic OAuth scaffolding: authorize, callback and token refresh on the `connections` table (`IMPLEMENTATION.md` §3.2), with a picker that imports only what the user ticks (ADR-0039), which is how a connector's history would arrive.
* Deauthorization deletion shipped with each connector; Wahoo and COROS require it contractually (`VISION.md` §7).
* Connect and disconnect on the web's Sync page, `/sync` (`IMPLEMENTATION.md` §4.0), and, once token refresh runs on a schedule, a last-synced status beside Health Connect's and GPS Logger's rows.