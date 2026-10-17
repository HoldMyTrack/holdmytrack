# Roadmap

Last updated: 2026-10-15.

## How to read this document

The remaining work toward the product `VISION.md` describes, by its §5 milestones; each step points at the §/FR holding its design. A subsection whose every box is checked is deleted.

## Milestone 1 — MVP

The current milestone (`VISION.md` §5, ADR-0031). The product is shipped and deployable (`SPEC.md` FR-1 to FR-17); what's left is what the public launch needs.

### Before launch

Gates the public launch (`VISION.md` §7, §8.3).

- [ ] Stand up the funding page (Open Collective, public ledger, `VISION.md` §6.1) before launch. The app side is built (`SPEC.md` FR-11.1), and the `holdmytrack` collective applied to Open Source Collective as fiscal host on 2026-09-24. Once approved, set `OpenCollectiveSlug` in `services/server/internal/web/web.go` and replace the About page's "donations are not open yet" line with a link.
- [ ] Post concept renders to r/running, r/cycling, r/Garmin, r/Strava, r/FogOfWorld (`VISION.md` §8.1, §8.3).
- [ ] DPIA: drafted in `docs/DPIA.md`; left are its section 7 pre-launch actions and the controller's sign-off.
- [ ] EU-region hosting for EU users.
- [x] A privacy policy page, `/privacy` (`SPEC.md` FR-10.6). It names the email delivery provider only generically; name it once settled, and update the policy when EU-region hosting moves the data.
- [x] Account deletion and data export, on the web and Android (`SPEC.md` FR-1.11, FR-1.12).
- [x] Health Connect data-type declarations in the Play Console, scoped to what's used; filed 2026-10-07.

---

## Milestone 2 — Release and community

Starts at the public launch. Its exit gate is cost per active user (`IMPLEMENTATION.md` §4.32 keeps the active counts) set against monthly donations and the share of active users who give, over several months.

### Cost control

Can land alongside any other work (`IMPLEMENTATION.md` §5.7).

- [ ] Serve the basemap as `/{z}/{x}/{y}` tiles Cloudflare can cache, with `pmtiles serve` on the droplet first: every range read reaches R2, since the free plan caches files only up to 512 MB (`IMPLEMENTATION.md` §5.4). Worth it once traffic grows: about 97,000 basemap requests in the 30 days to 2026-10-14 were under 1% of R2's free reads. The web style (`apps/web/src/map/style.ts`) and the server-built Android style (`mapstyle.go`) move from `pmtiles://` to the tile URL.
- [ ] Re-measure the funding-model assumptions (`VISION.md` §4.3, §6.3) against real usage; they feed the exit gate.

### After launch

- [ ] Review how long original uploads are kept (today, until the activity is deleted) once real accounts show what they cost. They're the most sensitive thing held (`DPIA.md` R2) and likely the largest storage line; an expired one can't be re-clipped, rebuilt or exported (`IMPLEMENTATION.md` §5.7).
- [ ] The iOS app (`VISION.md` §4.1 Path 2, `ARCHITECTURE.md` §2), no date set: first confirm `HKWorkoutRoute` access with a throwaway app; then HealthKit sync with `HKWorkoutRoute` geometry on Android's payload and pick-what-to-send design (`SPEC.md` FR-3.6), with the site's pages in a `WKWebView` for Settings, Profile, the Sync page and About (ADR-0042); then in-app GPS recording on Android's wire shape and `source` convention (`SPEC.md` FR-3.8, ADR-0007).

---

## Milestone 3 — Social graph (not committed)

Public pages, followers and the rest (`VISION.md` §5.7, §5.8). No steps: the first is the Milestone 3 decision, made from Milestone 2's numbers, and not building it is one outcome.

---

## Ongoing

- [x] Describe every `/v1` endpoint in the API contract, `services/server/api/` (`IMPLEMENTATION.md` §4.31).
- [x] One error shape for the JSON API (ADR-0045), described in the contract.
- [x] Generate the web client's types from the contract (`apps/web/src/apiSchema.ts`).
- [x] Typed Android wire classes (`net/Wire.kt`) checked against the contract (`WireContractTest`).
- [ ] Once the Play Console shows no Android build from before Heatmap's circles, or `MIN_ANDROID_BUILD` passes them (`IMPLEMENTATION.md` §4.33), remove the routes only those builds call: the `/heatmap/` raster route, which answers a transparent tile, and the `country-heatmap` and `region-heatmap` tiles with their queries (`IMPLEMENTATION.md` §4.2.2, §4.2.4; ADR-0046).
- [ ] Once the Play Console shows no Android build from before the vector veil, or `MIN_ANDROID_BUILD` passes them, remove the Fog PNG route (`/tiles/v1/fog/`) and its palette rewrite (`internal/fog/tint.go`) (`IMPLEMENTATION.md` §4.2, §4.33).
- [ ] Turn on Sign in with Facebook (`SPEC.md` FR-1.10): Meta publishes the app only after Business Verification, which needs a business document such as an EIN letter or a DBA filing (`docs/DEPLOY.md` §4). Not a launch gate.