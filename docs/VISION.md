# Vision: HoldMyTrack

## 1. Executive Summary

HoldMyTrack is a **free, community-funded platform for tracking outdoor activities and seeing the accumulated shape of where you have been** — how much ground you've covered, how well you've explored the places you live and travel through, and where you go most. It aggregates the activity history you already have — from watches, from cloud services, from files — and turns it into maps and exploration stats worth looking at.

**What it is for.** A watch or tracking app shows one activity at a time, and years of history end up split across whichever devices and apps someone has used. HoldMyTrack puts all of it on one map, so "where have I been?" is answered by a picture rather than a list — and the same picture shows, just as clearly, the streets, parks and trails nearby that are still unexplored. A trip that spans several activities — a multi-day hike, a holiday — can be kept as one Story with its own map and totals (§4.2), instead of dissolving into the rest of the history. It doesn't tell anyone where to go (Spots, §1.1, is the one place it suggests somewhere); it shows what's left and leaves the choice to them.

It targets people who already track workouts or travel and want a better way to *see* the result: all their history on one map, and each trip kept as a Story with its photos, without adopting another real-time GPS tracker.

HoldMyTrack's wedge is:

1. **Visual quality as the product.** Competitors are functional and largely ugly. HoldMyTrack treats the render — smooth fog edges, considered typography, print-grade output — as the thing worth building.
2. **Source independence.** Three independent ingest paths (§4.1) mean the product survives any single provider revoking access, changing terms, or pricing itself out of reach. This is a deliberate structural choice, not a convenience.
3. **Free, and funded in the open.** No subscription and no paywalled features today, and user data is never sold. Running costs are covered by recurring community funding with public accounting (§6). How it's funded past the community milestone is decided from that milestone's numbers, not promised now (§5, ADR-0031).

### 1.1 What HoldMyTrack is not

Worth stating early, because a map of your activities is easy to mistake for a bigger product than this one is, in four specific ways:

* **HoldMyTrack is not a fitness tracker.** The mobile app can record a plain GPS track as a convenience — a road trip, a dog walk, a forest walk, anything you'd otherwise need a separate tool running for (§4.1) — but it captures GPS only: no heart rate, cadence, power or other sensor data, no training metrics, no ambition to match a dedicated watch's battery life or accuracy. If you already track workouts on a watch, that stays the better tool for the job; HoldMyTrack keeps ingesting its output exactly as it always has.
* **HoldMyTrack is not a location logger.** It imports a Google Maps Timeline export (§5.5), but it doesn't log where someone is all day in the background the way Timeline does. A central store of everyone's round-the-clock location is what draws requests for everyone who was near a place at a given time — the reason Google moved Timeline onto the phone — and it would be a storage line growing without bound (§4.3) and a background-location permission the Play Store reviews strictly. If continuous logging is ever wanted, it stays on the device, like Google's own.
* **HoldMyTrack has no social network yet.** No feed, no follows, no kudos, no segments, no leaderboards. A social graph is the third milestone (§5), built only if the second milestone's numbers say it can be paid for — and not building it is an acceptable outcome. See §5.8 for why that ordering is not just caution.
* **HoldMyTrack is not a health or fitness advisor, and keeps no health data.** We don't keep your health profile — only the geographical data you trust us with, and the photos you choose to add to it. No HR zones, no training load, no recovery or readiness scores, no sleep tracking — and no heart rate at all: it is never read from a file or from Health Connect, never stored and never shown. An activity is where you went, when, and at what elevation; pace is derived from that and shown only as the color of a selected route. An outdoor GPS tracker is what this is, not a health platform wearing a map as a skin (ADR-0017).

What is left is an aggregator and a map for exploring where you've been — not an analytics platform and not a coach. That is a deliberately small product, and a more defensible one for it: it is built around seeing where you've been, not around recording, analysis or a network.

**One place it suggests where to go: Spots.** The fog shows what's left and leaves the choice to the user; Spots (§4.2) goes one step further and names places — a playground, a dog park, a viewpoint — and that is deliberately as far as discovery goes: outdoor places from OpenStreetMap only, not a "things to do" app, with no venues, reviews or ratings. The one live step is capture: standing in a place for half a minute with the Android app open marks it captured, a private mark of having gone out to find it, not a check-in anyone else sees (ADR-0023). It is the only mark a place carries (ADR-0028).

---

## 2. Company Description & Vision

### 2.1 Mission Statement
To let athletes, runners, cyclists and explorers see and keep the shape of where they have been — without a subscription, and without surrendering their data.

**Tagline**: "HoldMyTrack — Every journey, mapped." Not a fitness-tracker claim (§1.1 is explicit that HoldMyTrack isn't one) — HoldMyTrack still motivates and supports people doing fitness activities and syncing them in to see the result, it just isn't the tool doing the tracking itself.

### 2.2 Value Proposition
* **No tracking friction** — fits existing workflows; HoldMyTrack never asks to record a workout.
* **Bring everything** — one place for data scattered across a watch, a cloud service and a folder of old exports.
* **Exploration insight** — how much ground you've covered this year versus last, how well a neighborhood is explored, and where you go most.
* **Gamified exploration** — "Fog of War" turns routine training into map discovery.
* **Find somewhere to go** — playgrounds, dog parks, monuments, viewpoints and historic sites on the map, and which of them your history already covers.
* **Remember the trip** — group the activities of a hike, a holiday or an event into a Story with its own map and totals.
* **Beautiful by default** — render quality is the differentiator, not feature count.
* **Manual data manipulation** — stored data can be created, updated or deleted manually.
* **Free, and honest about why** — funded by the people who use it, with the books open.

---

## 3. Market Analysis

### 3.1 Target Audience
* **Endurance athletes** — cyclists, runners and hikers who already log meticulously.
* **Urban explorers** — people who gamify coverage of their city.
* **Casual smartwatch owners** — want a nice seasonal or annual summary, not analytics.
* **The multi-device athlete** — a Garmin for rides, an Apple Watch for runs, and no single place that shows all of it. This segment is served specifically by §4.1's three ingest paths and is underserved by every single-source competitor.
* **Travellers** — people who want a holiday, a road trip or a multi-day trek kept as one journey on a map, with its photos. Served by Stories, photos and in-app recording (§4.2, §4.1); held back by having no route history to bring unless they recorded one, since most travellers' trace sits in Google Maps Timeline, which HoldMyTrack doesn't import.
* **The subscription-fatigued** — people who already pay for Strava and resent it. Being free is not a discount here; it is the pitch.

### 3.2 Market Opportunity
Digital fitness tracking continues to grow, and the major platforms remain focused on real-time logging, social feeds and health metrics rather than spatial artifacts and cross-source aggregation. The opportunity is a **quality and openness** opportunity, not a whitespace opportunity.

### 3.3 Competitive Landscape

| Product | Overlap with HoldMyTrack | Pricing | Gap HoldMyTrack exploits |
| :--- | :--- | :--- | :--- |
| **Strava** | The incumbent: recording, analysis, social, heatmap | Free tier + ~$12/mo | Paywalls analysis; single-source-first; no fog mechanic; visually conservative |
| **Statshunters** | Explorer tiles + heatmap on Strava | Free / donation | Utilitarian UI; Strava-only |
| **VeloViewer** | Explorer tiles, mature, cycling-first | ~£10/yr | Dated interface; cycling-centric |
| **Wandrer.earth** | % of streets covered | ~$3–8/mo | Data-first, not visual |
| **Squadrats** | z14/z17 tile gamification | Free / cheap | Game only, no artifact |
| **CityStrides** | Street completion for runners | Free tier + sub | Running-only; sparse visuals |
| **Fog of World** | The fog mechanic itself | One-time | Requires its own tracking; fog over *satellite* imagery, which we offer only as an optional base map with a capped cost |
| **Polarsteps** | Trips drawn on a map with their photos, tracked automatically | Free; printed travel books paid | Travel-only: a trip is its own journal, apart from everyday walks and rides; no fog or coverage mechanic |
| **Runalyze / Intervals.icu** | Free, deep performance analysis | Free / donation | Analysis-first, visually plain; the closest model for our funding approach |

**Three honest observations about this table.**

First, **almost every product in it ingests via the Strava API**, which makes them Strava satellites — they inherit Strava's terms and die if Strava changes them. HoldMyTrack's three independent paths (§4.1) are the structural answer, and notably **Strava itself is not one of our sources**: the largest existing activity archive reaches us only through manual export (§4.1), which is friction we should be honest about rather than hide.

Second, **Intervals.icu and Runalyze already prove the model we are choosing** — serious, free, donation-funded fitness analysis with real users. They are validation that this can work and evidence that it stays small. Neither is a venture-scale business, and HoldMyTrack should not pretend it is planning to be one.

Third, **the tile games (Statshunters, VeloViewer, Squadrats) are a mechanic HoldMyTrack deliberately does not copy.** Counting visited map squares measures the same thing the fog already shows, only coarser and less pleasant to look at; Fog of War is HoldMyTrack's exploration mechanic, and there is no tile score (ADR-0018).

**Implication for strategy:** feature parity is achievable in weeks and is not defensible. The defensible assets are render quality, breadth of ingest, and being genuinely free.

**A deliberate visual trade-off.** Fog of World and similar apps draw fog over satellite imagery, and much of their appeal is the texture the reveal exposes — rooftops, tree canopy, water. HoldMyTrack renders over a self-hosted *vector* basemap by default. Imagery means a metered tile provider billed per request, on pan/zoom traffic that earns nothing, so it is never the default and never an open-ended bill: it is an optional Satellite base map, switched on per deployment, on a plan whose quota pauses the imagery rather than billing past it (ADR-0022). The consequence should be owned rather than discovered late: the default reveal looks different, and differentiation has to be carried by render quality and typography. Measured reference numbers are in `IMPLEMENTATION.md` §4.2.1.

---

## 4. Product & Data Strategy

### 4.1 Ingest — three independent paths

The central structural decision. Each path has a different failure mode, and no single provider can take the product down.

#### Path 1 — Cloud-to-cloud integration

Server-side OAuth connections to services that hold the user's history. Target set: **Garmin, Wahoo, COROS.**

| Provider | What it gives | Constraint to verify before building |
| :--- | :--- | :--- |
| **Garmin** | Full activities with GPS, HR, power, cadence | Connect Developer Program historically requires a **paid commercial licence**. Whether a free, donation-funded service qualifies as commercial is the open question — get it in writing |
| **Wahoo** | Rides with full sensor data | Partner API with an approval process |
| **COROS** | Runs, rides, multisport | Partner API with an approval process |

**Strava is not on this list.** We are competing with them, their post-2024 terms restrict apps that replicate Strava features, and building on the API of the incumbent you are trying to displace is a poor structural bet. Strava users reach us through Path 3.

#### Path 2 — On-device sync

Native apps reading the platform health store. Target set: **Apple Watch (HealthKit)** and **Samsung Galaxy Watch (Health Connect)**.

These are not equivalent, and the difference is verified rather than assumed:

* **Apple Watch → HealthKit** exposes `HKWorkoutRoute`. Full GPS geometry is available on-device to a native iOS app.
* **Samsung Galaxy Watch → Health Connect** does **not** expose route geometry. Samsung's own developer documentation states that `EXERCISE_ROUTE` data cannot be accessed from Samsung Health via Health Connect. HoldMyTrack only ingests activities that have a route, so Samsung sync cannot deliver activities into HoldMyTrack today — every Samsung-sourced session arrives with no geometry and is rejected at sync time, not silently dropped or shown as a metrics-only entry.

Two further Android platform constraints apply to any Health Connect route read:

* `READ_EXERCISE_ROUTES` **cannot be requested programmatically**; the user must grant it manually in Health Connect settings or via the route request activity.
* **Routes written by other apps cannot be read in the background** — Health Connect returns `ExerciseRouteResult.ConsentRequired` even with "Always allow" granted.

So Android on-device sync is foreground-only, and Samsung Galaxy Watch is unsupported — it never provides the route geometry HoldMyTrack requires. Product copy must not promise otherwise. This is a platform constraint, not an implementation shortcut.

#### Path 3 — Direct manual file upload

`.GPX`, `.FIT`, `.TCX`, plus bulk-export archives from Strava and others. Unglamorous, and the most robust thing in this document: no API terms, no licence, no permission model, no vendor who can revoke it, and it works for every service that offers an export — which is all of them, because GDPR requires it.

**It is also what the no-signup demo is built from** — the demo account's history went in through this same pipeline. See §8.2.

#### The validation gate

Before engineering begins:

1. **Get Garmin's licence position in writing** for a free, donation-funded service. This is the one line item that could carry a fixed annual cost with no revenue behind it (§4.3).
2. **Apply to Wahoo and COROS partner programmes.** Lead time, not cost, is the risk.
3. **Confirm HealthKit route access** with a throwaway iOS app reading `HKWorkoutRoute`.
4. **Confirm the Samsung limitation empirically** rather than trusting documentation — if routes turn out to be reachable, Android's product improves materially.

**Path 3 is unconditional.** If every item above fails, file upload still delivers the entire product to every user willing to export once. That is what source independence buys, and it is why Phase 1 builds Path 3 first (§5).

#### Casual in-app GPS recording — mobile-only, and not a fourth path

Distinct from the three paths above, which each bring in a user's *existing* history from somewhere else: the mobile app can also originate an activity itself, for someone who has no watch running and does not want to install a separate tracker for a one-off walk or drive. Start, optionally pause, and stop a GPS-only recording directly in HoldMyTrack; on stop, the recorded track submits through the same ingest pipeline every other source already uses (`ARCHITECTURE.md` §1.1, `IMPLEMENTATION.md` §4.1) — no new server-side path, no separate privacy story, no dedupe case beyond what already exists for two overlapping recordings of the same activity.

This isn't a resilience decision the way Paths 1–3 are — §4.1's provider-independence argument doesn't apply, since it depends on no external provider at all. It's a convenience feature: one fewer tool to install for someone who just wants a casual walk or drive on the map, with no export and no import in the way. **Scope stays deliberately narrow — GPS only.** No heart rate, cadence, power, or any other sensor; no training-load or coaching output; not a replacement for a dedicated fitness tracker (§1.1). Phased in on Android first, then iOS (§5.3); see `apps/android/docs/ROADMAP.md` for the plan.

### 4.2 Core Features

| Category | Description | Key functionality |
| :--- | :--- | :--- |
| **Multi-source ingest** | The structural differentiator | Cloud connectors, on-device sync, file upload; cross-source deduplication |
| **In-app GPS recording** (mobile) | Convenience capture, not a fitness-tracker replacement | Start/pause/stop a GPS-only track directly in the app; feeds the same ingest pipeline as any other source |
| **Visual Map Engine** | Interactive renderer with custom styles | Fog of War, heatmap and track/normal modes (`IMPLEMENTATION.md` §4.2, §4.2.2); curated themes; smooth (non-hexagonal) fog edges |
| **Per-activity detail** | Pace as route context, not a coaching product | The selected activity's route colored by pace (`IMPLEMENTATION.md` §4.3.1) |
| **Stories** | Keep a trip as one thing | Hand-picked sets of activities, each with a name, a description, joint stats (count, distance, time, a per-type breakdown) and its own map view; an activity can belong to any number of them |
| **Photos** | The pictures from a trip, where they were taken | The user's own photos added to an activity, each placed on its route by the time it was taken (or its position, or by hand), shown on the map for the selected activity or the open Story and managed in the Edit window; kept as a resized copy, never as the original |
| **Spots** | Where to go next, and which places you went out and found | Outdoor places from OpenStreetMap in five categories (Playground, Dog park, Monument, Mesmerizing view, History) each switched on in the map's Layers menu; OSM's description, inscription and Wikipedia article, Copy address, and capturing a spot by staying in it for 30 seconds with the Android app open |
| **Activity graph** | Private, single-player motivation | A GitHub-style daily contribution grid, year by year, shadeable by count or distance (`IMPLEMENTATION.md` §4.8) |
| **Distance & coverage trends** | See how much ground you've covered this period vs last | Weekly/monthly distance, moving-time and elevation trends |
| **Filtering** | Slice the history | Activity type, date range, geographic bounding box, source |
| **Export** | Free, unrestricted | Print-grade raster/vector export, animated reveals — no watermark, no tier |
| **Privacy Controls** | Table stakes, see §7 | Private locations (user-defined privacy zones), per-map share scoping |

**We don't keep your health profile — only the geographical data you trust us with, and the photos you choose to add to it.** An activity is a route: positions, times and elevation. Pace is derived from those and shown as the color of a selected track — a supporting detail on the route, not a pillar and not a training product; the pillars are the map and the exploration stats. Heart rate, cadence, power, calories and every other body signal are never read, stored or shown, whichever source an activity came from. The one place such data can still sit is inside an original upload, which is kept as-is so a track edit or a Private location change can rebuild the activity, is only ever read for its route, and is deleted with the activity. The schema carries per-point streams (`IMPLEMENTATION.md` §3.3) for the route, and no more. A pace/heart-rate/elevation profile card was built and then removed for this reason — see ADR-0017.

**Photos are the one thing kept that isn't geography, and only because the user adds them.** A trip is remembered by what was seen on it as much as by where it went, so an activity can carry the user's own photos, each pinned to the point of the route where it was taken — worked out from the photo's capture time, or its position when the time is unusable, or chosen by the user when neither says, and movable along the route by hand. Every photo has a place on its route; there are no loose ones. They're kept as a resized copy with every EXIF field stripped, never as the original: HoldMyTrack is a map of trips, not a photo backup, and the original stays wherever the user keeps it. They're private like everything else, and always sit on the route as it's drawn, so a Private location that hides a track's end hides the place a photo there was taken too. See ADR-0024.

**The activity graph is deliberately private, not a profile page.** It's the same genre of thing as Fog of War above — motivation through your own history, no comparison required — not a step toward the social features §1.1 and §5.7 explicitly hold off on. It has no follows, no feed, and nothing another user can view; it's a personal dashboard, available once accounts exist (§5.2), not a public artifact. If a shareable version is ever worth building, that's a §5.7 social-phase decision to make deliberately, not a side effect of how this one ships.

**Stories are for remembering a trip, and the user decides what belongs in one.** A multi-day hike, a holiday or a race weekend is several activities spread over several days, sometimes with a drive at each end, and after the fact it disappears into the rest of the history. A Story keeps it as one thing: a name, a description, its joint totals, and a map that shows only its own tracks. Its activities are hand-picked rather than matched by a date span or a rule, because only the person who went knows which drive was part of the trip and which was the commute that week. Stories are private, like the activity graph: a way of keeping your own memories, not a profile or a feed. Sharing one means exporting an image of its map (`IMPLEMENTATION.md` §4.3.3); a link that opens a story for someone else is a §5.7 decision, for the same reason as above. A story view draws tracks only: Fog of War and Heatmap stay all-time, since a fog pyramid per story is a per-user storage cost that grows with every story made (§4.3). See ADR-0020.

**Spots are OpenStreetMap's places, and a place is marked by capturing it.** The places come from a bulk import of OSM, not a curated list: hand-picking places for the whole planet, or moderating users' suggestions, is staff time, and §6.3 names one maintainer's time as the model's main risk. Only categories OSM tags cleanly are imported — `leisure=playground`, `leisure=dog_park`, `historic=monument`/`memorial`, `tourism=viewpoint`, and `historic=castle`/`ruins`/`fort`/`archaeological_site` for History — because a looser tag ("architecture", any `historic=*`) buries the map in boundary stones and plaques. Spots are one map layer, a Layers entry per category, the same in every mode; they don't change the fog, which stays a record of where the tracks went. See ADR-0021. Capture is the one thing done at a spot: the Android app guides you to the place you picked, and once you've been inside it for 30 seconds the place is captured, drawn darker on the map from then on, on the web too. It's the app's small game with the map, private like everything else, and the only mark a place carries — nothing about places is worked out from tracks, so passing through on a long ride marks nothing (ADR-0023, ADR-0028).

### 4.3 Cost Model — running a free service

The critical section for a product with no revenue. Costs must be *bounded by design*, not managed after the fact.

| Line item | Scales with | Note |
| :--- | :--- | :--- |
| Basemap storage | Fixed | ~138 GB planet `.pmtiles` on R2 — a couple of dollars a month |
| Basemap tile reads | **Usage** | Every tile read is a billed Class B GET. The one basemap cost that grows; a CDN in front is what keeps it flat |
| Activity storage | **Users × history** | Per-point streams are the bulk. Grows monotonically and never shrinks on its own |
| Fog raster storage | **Users** | A few MB per user; cheap, but per-user and permanent |
| Photos | **Users × photos** | About 0.5 MB each — a resized copy and a thumbnail, never the original — capped at 2,000 per account |
| Spots | Fixed | Several million OSM places, roughly 1 GB in Postgres; matching them is one indexed query per activity processed |
| Compute | Users | One application server + Postgres/PostGIS to start |
| Garmin licence | Fixed, if applicable | §4.1. The only line that could be large, fixed, and unavoidable |
| Compliance | Fixed | DPA/DPIA work, EU-region hosting (§7) |
| Egress | — | Cloudflare R2 charges none. This is why R2, not S3 |

**Order-of-magnitude at 10,000 active users.** Assumptions stated so they can be argued with: 300 activities per user, ~80 KB of compressed stream data each, ~3 MB of fog rasters per user, 100 photos per user at ~0.5 MB each, 20 map sessions per user per month at ~300 tile reads per session with a 90% CDN hit rate.

| | Estimate |
| :--- | :--- |
| Activity + stream storage | ~240 GB |
| Fog rasters | ~30 GB |
| Photos | ~500 GB |
| Basemap | ~138 GB |
| Storage subtotal (R2) | ~$14/month |
| Tile reads after CDN | ~$2/month |
| Compute + database | $100–300/month |
| **Total** | **roughly $150–350/month** |

That is a genuinely small number, and it is the whole argument for this model working. At $5/month per supporter, **break-even is on the order of 30–70 recurring donors out of 10,000 users — well under 1%.** Intervals.icu and Runalyze sustain themselves on comparable ratios.

**The three things that break this model**, in order of likelihood:

1. **The Garmin licence.** A four-figure annual fee dwarfs every other line and cannot be covered by a sub-1% donation rate at small scale. If it is commercial-only, Garmin support may have to wait for the donor base, or route through Path 3.
2. **Storage growing without bound.** Every user who ever signs up costs money forever. A retention and dormancy policy is not a nicety here; it is what keeps the curve flat. See `IMPLEMENTATION.md` §5.7.
3. **A viral moment.** Free products have no natural throttle. A front-page day at 100× traffic is a cost event with no matching revenue event. Rate limits and a spend cap on the CDN and object store are load-bearing infrastructure, not hardening.

---

## 5. Product Roadmap

The product moves through three milestones (ADR-0031):

1. **MVP** — where HoldMyTrack is now. Activities, the map modes, Spots, Stories and photos, on the web and Android (Phases 0–3 below). Free for everyone, community-funded.
2. **Release and community.** A public launch, then settling: polish, bug fixes, and adjusting features to what the people using it ask for (Phases 4–6). It starts with what a public launch needs anyway — the funding page (§6.1), a privacy policy and a DPIA (§7) — and ends with a measured answer to what community funding can carry: monthly donations against the monthly bill, and the share of active users who give, over several months (§8.3).
3. **Social graph** (Phase 7). Public pages, followers and the rest — if at all. Whether to build it, and whether to pay for it with paid features, community funding or sponsors, is decided from Milestone 2's numbers. Staying a single-player product and never starting this milestone is one of the options.

Within the milestones, phases are sequenced so the unconditional ingest path ships first and the ones that depend on other companies' permission come later. The phase numbers are the ones `docs/ROADMAP.md` uses, which carries each phase's step-by-step detail.

### 5.1 Phase 0: Validation (Weeks 1–2) — *no application code*
* Start the §4.1 gate: Garmin licence position in writing; Wahoo and COROS applications filed (lead time starts now); HealthKit and Samsung route checks.
* Stand up the funding page (§6) before launch, not after — the ask is much weaker retrofitted.
* Post concept renders to r/running, r/cycling, r/Garmin and r/Strava; specifically test whether "free, funded by its users" reads as credible or as doomed.

### 5.2 Phase 1: Upload + Map (Months 1–3)
* **Path 3 first**: `.GPX`/`.FIT`/`.TCX` parsing and bulk-archive import.
* Backend: ingest, storage, fog raster pipeline, tile serving.
* Web app: Fog of War and track modes on the self-hosted planet basemap.
* **The no-signup demo** — a fully populated example account anyone can open and explore before signing up (§8.2).
* Accounts and persistence for anyone who wants to keep it.
* A private activity graph once an account exists — a GitHub-style daily contribution grid shadeable by count or distance, plus active-days and longest-streak stat cards (`IMPLEMENTATION.md` §4.8). The grid itself reuses `IMPLEMENTATION.md` §4.7's histogram query; the streak and active-day stats are small new aggregate queries of their own.
* Free high-resolution export — a framed image of the current map, unwatermarked, rendered in the browser.
* Stories — hand-picked, private sets of activities with their own totals and map view, for keeping a trip or an event as one thing (§4.2). A web feature first; Android follows on the same API.
* Spots — outdoor places from OpenStreetMap in the map's Layers menu, captured by standing in one for 30 seconds with the Android app open (§4.2). A web feature first; Android follows on the same tiles.

### 5.3 Phase 2: Mobile
* Android app — Health Connect. Samsung Galaxy Watch sync is unsupported (Samsung never exposes route geometry, and HoldMyTrack only ingests activities that have one). Built first of the pair regardless, so the Path 2 sync contract is designed against the more constrained platform.
* iOS app — HealthKit and Apple Watch, the stronger of the two on-device paths.
* In-app GPS recording (Android, then iOS) — a plain start/pause/stop track capture for casual, watch-free activities, submitted through the existing ingest pipeline; no new server-side work beyond the mobile clients themselves (§4.1).
* Cross-source deduplication — unavoidable the moment a second source exists, so it arrived with Health Connect sync rather than waiting for Phase 4's connectors.

### 5.4 Phase 3: Finalized design + mobile browser support
* The first shipped UI was functional scaffolding. This pass finished it — one icon set, deliberate typography, design tokens — across desktop and phone browsers, and ended in a design freeze, declared 2026-10-02: the `--fm-*` tokens, Inter and Fraunces (Source Serif 4 for Russian headings), and Lucide icons.
* The mobile apps inherit that freeze rather than inventing a second visual language: two clients that each chose their own would not read as one product.

### 5.5 Phase 4: More sources
* Google Maps Timeline import — a one-time upload of the JSON file the phone's Google Maps exports, for the travellers (§3.1) whose only route history is Timeline. Google moved Timeline onto the phone and closed its web version, so years of history have no map of their own any more; "see it all on one map again, with Fog of War" is a strong reason to arrive. Import only — see §1.1 on why HoldMyTrack doesn't pick up continuous location tracking.
* Path 1 connectors, in whatever order §4.1's approvals actually land.

### 5.6 Phases 5–6: Cost control and compliance
Gates rather than features. Cost control — retention, per-user quotas, rate limits, and measuring cost per active user — can land alongside any phase, and is what keeps §6's funding model honest (§4.3). Compliance (§7) — a DPIA, EU-region hosting, working data export and account deletion — gates any public launch, however small.

### 5.7 Phase 7: Social graph (Milestone 3, not committed)
Public pages, followers and the rest of a social graph make up Milestone 3, and it is deliberately unscheduled. It doesn't start until Milestone 2 has shown what community funding can carry, because social features add moderation, abuse handling and safety obligations that are **staff costs, not server costs** — the one category donations scale to worst. The decision at that point is which way to pay for them — paid features, community funding or sponsors — or not to build them at all. See §5.8.

### 5.8 Why social is last, not just later

Deferred for a reason worth writing down. A fog map is a precise record of where someone lives and when they are away from home. Adding a social graph to that is not an incremental feature; it is a change in threat model. Strava's own 2018 heatmap incident and its subsequent stalking-related redesigns are the reference case, and Strava had a large trust-and-safety team when they hit it.

A product with no headcount should not ship a location-sharing social network. Either the funding supports moderation or the feature does not ship. Whatever pays for it, public pages need their privacy defaults decided first: private unless the user chooses otherwise, Private locations applied, and nothing live or recent on show.

---

## 6. Funding & Sustainability

### 6.1 The model

**HoldMyTrack is free today: all features, all sources, all exports, for everyone, and user data is never sold.** Nothing is promised past Milestone 2 (§5) — not "free forever", not "no ads". The one standing commitment is that data is never sold (ADR-0031).

Running costs are covered by **ongoing community funding** — recurring monthly support through a platform such as Open Collective, GitHub Sponsors or Patreon.

Open Collective is the strongest fit specifically because it makes spending public by default. For a project whose pitch is "free and not extracting from you", **verifiable accounting is part of the product**, not an administrative detail. The funding page should show the actual monthly infrastructure bill against actual monthly income.

### 6.2 What supporters get

Deliberately, **nothing that non-supporters do not get.** A supporter-only feature would be a paid tier, and whether HoldMyTrack ever has one is the Milestone 3 decision (§5), made from Milestone 2's numbers rather than slipped in as a perk.

What is available instead is recognition, not capability: a supporter badge, a credits page, a say in roadmap prioritisation. Anything touching the maps, the analysis or the exports stays free for everyone.

### 6.3 Honest risks of this model

* **Donation revenue is not correlated with cost.** Costs scale with users; donations scale with goodwill. A growth spike is a cost spike, and the funding page does not spike with it.
* **It depends on one person's continued interest.** Donation-funded projects are typically one maintainer, and the failure mode is burnout, not bankruptcy. The mitigation is keeping scope small — which is the real reason §1.1 says no fitness tracking, and why social waits for Milestone 3.
* **It caps ambition, and that is a choice being made.** This model will not fund a team, an office, or a print supply chain. It will fund a good product used by a lot of people. Whether to go past that — a social graph, and a way of paying for one — is the Milestone 3 decision (§5), made once there are numbers to make it from.
* **Break-even is genuinely low** (§4.3) — under 1% of users at 10,000. That is the strongest argument that this works. It is also the number to actually measure in Phase 1 rather than assume.

---

## 7. Privacy, Security & Compliance

Non-negotiable. A Fog of War map is a precise map of where a person lives — the 2018 Strava heatmap incident is the canonical warning, and an individual fog map is far more revealing than an aggregate one.

* **Private locations** — user-defined circles (home, work) excluded from every render and export. The leading and trailing parts of a track inside one are hidden; a track that merely passes through one is shown whole, since passing by reveals nothing about where someone starts or ends.
* **No blanket endpoint trimming** — an earlier default trimmed the first and last N metres of every track. It was dropped: on a multi-day trail every day's start and end is a campsite or trailhead, so it cut a gap into the trail at each day boundary while protecting nothing there. The places worth hiding are the ones the user names — see ADR-0010.
* **Applied at ingest, server-side** — before anything is persisted or indexed, per `IMPLEMENTATION.md` §4.1. Privacy applied at render time leaks through any bug in the render path.
* **Share scoping** — shared maps and exports must respect zones; an exported file is permanent and cannot be recalled.
* **Legal basis** — HoldMyTrack processes no health data (§1.1): heart rate or other body data that happens to sit inside an original upload stays in that file and is never read out of it. A precise location history is sensitive personal data regardless: explicit consent, a DPIA before launch, a documented retention policy, working export and deletion, EU-region hosting for EU users. **Being free changes none of this.** There is no small-project exemption, and the compliance burden is one of the few fixed costs a donation model has to carry regardless of scale.
* **Deauthorization deletion** — Garmin, Wahoo and COROS require deletion of synced data when a user disconnects. Build it with the first connector, not after.
* **Health Connect declarations** — Android health data types must be declared in the Play Console with justified use. Requesting more types than the product demonstrably uses is a known rejection cause. HoldMyTrack requests exercise sessions and their routes (plus the history window over them) and no health measurement at all.
* **Photos are stored stripped and placed after clipping** — the browser re-encodes a photo before upload, so no EXIF field (the position included) reaches the server inside the file; its place on the map is worked out from the clipped track on every read, so a photo taken inside a Private location — even one added afterwards — shows at the visible end of the track, never inside it (ADR-0024).
* **Spot captures are private** — visible only to the account itself.
* **OpenStreetMap's licence** — Spots is an unmodified OSM extract: the map already credits OSM contributors, and the data stays available under the ODbL by pointing to OSM itself.
* **No data sales, ever, stated in the privacy policy.** For a free product this is the question every user will ask, and the answer needs to be a written commitment rather than a reassuring tone.

---

## 8. Go-To-Market

### 8.1 Launch
* **Community-first** — r/running, r/cycling, r/Garmin, r/Strava, r/FogOfWorld. These communities respond to a working demo, not a landing page. "Free, funded by its users" is a strong post title in every one of them.
* **Lead with the demo, not the pitch** (§8.2).
* **Influencer partnerships** — endurance creators and bikepackers, compensated in nothing, because there is no money. What can be offered is a genuinely good free tool and a credit.
* **Be explicit about funding from day one.** Users of free products are rightly suspicious about what is being monetised instead. Answering that question before it is asked — with a public ledger — converts suspicion into support.

### 8.2 The demo is the ad

The single best asset in this plan: **a no-signup demo** — one click opens a fully populated example account, months of real walks, rides and road trips, in every map mode and the activity graph. Shareability at zero friction is worth more than a signup funnel. It was first pitched as a drag-your-own-file page; what shipped is read-only (`SPEC.md` FR-2.1), so seeing your *own* fog still takes a free account.

Everything else follows from it — the screenshot people post is the marketing, and the fog reveal is inherently screenshot-friendly.

### 8.3 The key validation questions

1. **Does Garmin's licence apply to a free service?** (§4.1.) The one item that can impose a fixed cost this model cannot absorb. Settle it in Phase 0 (§5.1).
2. **Will people fund a free tool they like?** (§6.) Under 1% of users at $5/month covers the bill. This is a low bar and a real one — measure it through Milestone 2 rather than assuming it. Its answer is what the Milestone 3 decision (§5) is made from.