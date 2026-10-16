# Vision: HoldMyTrack

## 1. Executive Summary

HoldMyTrack is a **free, community-funded platform for tracking outdoor activities and seeing the accumulated shape of where you have been**: how much ground you've covered, how well you've explored the places you live and travel through, and where you go most. It keeps the journeys you choose, from a watch, the phone, a cloud service or a file, and turns them into maps and exploration stats.

**What it is for.** A watch app shows one activity at a time, and years of history end up split across devices. HoldMyTrack puts the journeys the user picks from each source on one map (ADR-0039). "Where have I been?" is answered by a picture, which also shows the streets, parks and trails nearby that are still unexplored. Stories are the main feature: a trip spanning several activities, such as a multi-day hike or a holiday, is kept as one Story with its own map and totals (§4.2). HoldMyTrack shows what's left and leaves the choice of where to go to the user; Spots is the one place it suggests somewhere.

It targets people who already track workouts or travel and want a better way to *see* the result, without adopting another real-time GPS tracker. Its wedge is:

1. **Visual quality as the product.** Competitors are functional and largely ugly. HoldMyTrack treats the render, from smooth fog edges to print-grade output, as the thing worth building.
2. **Source independence.** Three independent ingest paths (§4.1) mean the product survives any single provider revoking access, changing terms or pricing itself out of reach.
3. **Free, and funded in the open.** No subscription, no paywalled features, and user data is never sold. Recurring community funding with public accounting covers running costs (§6); funding past the community milestone is decided from that milestone's numbers (§5, ADR-0031).

### 1.1 What HoldMyTrack is not

* **Not a fitness tracker.** The mobile app can record a plain GPS track as a convenience, for a road trip or a dog walk (§4.1). It captures GPS only: no heart rate, cadence, power or other sensor, no training metrics, and no attempt to match a watch's battery life or accuracy. A watch stays the better tool for workouts, and HoldMyTrack ingests its output.
* **Not a location logger.** It doesn't log where someone is all day in the background the way Google Maps Timeline does, and doesn't import such a log: a Timeline export is mostly the daily routine, which can't be picked trip by trip (ADR-0039). A central store of round-the-clock location draws requests for everyone who was near a place at a given time, grows without bound (§4.3), and needs a background-location permission the Play Store reviews strictly. Continuous logging, if ever wanted, stays on the device.
* **No social network.** No feed, follows, kudos, segments or leaderboards. A social graph is the third milestone, built only if the second milestone's numbers say it can be paid for, and not building it is an acceptable outcome (§5, §5.8). Stories are private like everything in an account: sharing one means an exported image, and a copy sent to someone who was on the trip (§4.2) becomes theirs and is shown to nobody.
* **Not a health or fitness advisor, and no health data.** No training load, readiness scores or sleep tracking, and no heart rate: never read from a file or from Health Connect, never stored, never shown. An activity is where you went, when, and at what elevation; pace is derived from that and shown only as the color of a selected route (ADR-0017).

The small scope is what makes it defensible and affordable (§6.3).

**Spots is the one place it suggests where to go.** It names outdoor places from OpenStreetMap, such as a playground, a dog park or a viewpoint, with no venues, reviews or ratings (§4.2). Standing in one for half a minute with the Android app open captures it: a private mark nobody else sees, and the only mark a place carries.

---

## 2. Company Description & Vision

### 2.1 Mission Statement

To let athletes, runners, cyclists, travellers and explorers see and keep the shape of where they have been, without surrendering their data.

**Tagline**: "HoldMyTrack — Every journey, mapped."

---

## 3. Market Analysis

The major fitness platforms focus on real-time logging, social feeds and health metrics. The opening is quality and openness in an occupied market.

### 3.1 Target Audience

* **Endurance athletes**: cyclists, runners and hikers who already log meticulously.
* **Urban explorers** who gamify coverage of their city.
* **Casual smartwatch owners** who want a seasonal or annual summary rather than analytics.
* **Multi-device athletes** with a bike computer for rides and a watch for runs, served by the three ingest paths (§4.1) and by no single-source competitor.
* **Travellers** who want a trip kept as one journey with its photos (§4.2). Their trace usually sits in Google Maps Timeline, which isn't imported (§1.1), so a trip comes in only if recorded in the app or on a watch.

### 3.3 Competitive Landscape

| Product | Overlap | Pricing | Gap HoldMyTrack exploits |
| :--- | :--- | :--- | :--- |
| **Strava** | Recording, analysis, social, heatmap | Free tier + ~$12/mo | Paywalls analysis; single-source; no fog |
| **Statshunters** | Explorer tiles + heatmap on Strava | Free / donation | Utilitarian; Strava-only |
| **VeloViewer** | Explorer tiles | ~£10/yr | Dated; cycling-centric |
| **Wandrer.earth** | % of streets covered | ~$3–8/mo | Data-first, not visual |
| **Squadrats** | Tile gamification | Free / cheap | Game only, no artifact |
| **CityStrides** | Street completion | Free tier + sub | Running-only; sparse visuals |
| **Fog of World** | The fog mechanic | One-time | Requires its own tracking |
| **Polarsteps** | Trips on a map with photos | Free; printed books paid | Travel-only; no fog |
| **Runalyze / Intervals.icu** | Free performance analysis | Free / donation | Analysis-first, plain; the closest funding model |

**Almost every product here ingests via the Strava API**, so they inherit Strava's terms and die if Strava changes them; the three independent paths (§4.1) are HoldMyTrack's answer. Strava itself is not a source, so the largest activity archive arrives only through manual export.

**Intervals.icu and Runalyze prove the funding model**: free, donation-funded analysis with real users. They also show that it stays small; HoldMyTrack does not plan to be a venture-scale business.

**HoldMyTrack does not copy the tile games.** Counting visited squares measures what the fog already shows, only coarser, and would be a second per-account coverage record that every deletion, track edit and Private location change has to un-visit. Fog of War is the exploration mechanic.

Feature parity takes weeks and is not defensible; render quality and breadth of ingest are.

**The default map is vector.** Fog of World's reveal shows satellite imagery. Imagery is billed per request on traffic that earns nothing, so HoldMyTrack renders over a self-hosted vector basemap, with Satellite an optional base map whose quota pauses it instead of billing past it (`IMPLEMENTATION.md` §4.26). Render quality has to carry the difference.

---

## 4. Product & Data Strategy

### 4.1 Ingest — three independent paths

Each path fails differently, so no single provider can take the product down (ADR-0001).

#### Path 1 — Cloud-to-cloud integration

Server-side OAuth connections to services holding the user's history. None is planned before launch; which to build, if any, is decided from what users ask for (§5). The likeliest are **Wahoo** and **COROS**, both partner APIs with an approval process. **Strava is not a candidate**: HoldMyTrack competes with it, its post-2024 terms restrict apps that replicate its features, and building on the incumbent's API is a poor bet.

#### Path 2 — On-device sync

Native apps reading the platform health store: **Apple Watch (HealthKit)** and **Samsung Galaxy Watch (Health Connect)**.

* **HealthKit** exposes `HKWorkoutRoute`, so full GPS geometry is available to a native iOS app.
* **Health Connect** does not expose Samsung Health's routes: Samsung's developer documentation states that `EXERCISE_ROUTE` data cannot be accessed from Samsung Health via Health Connect. HoldMyTrack ingests only activities with a route, so every Samsung session is rejected at sync time, never dropped silently or shown without a route. Samsung Galaxy Watch is unsupported, and product copy must not promise otherwise.
* On Android, `READ_EXERCISE_ROUTES` cannot be requested programmatically; the user grants it in Health Connect settings or the route request activity. Routes written by other apps can't be read in the background, so sync is foreground-only (`IMPLEMENTATION.md` §4.1).

#### Path 3 — Direct manual file upload

`.GPX`, `.FIT` and `.TCX` files, up to 20 at a time, picked from any service's export, Strava's included. A `.zip` or Google Takeout archive is not imported: it brings in thousands of activities at once, past the gate (ADR-0039). The demo account's history came in this way (§8.2).

**Path 3 needs no one's permission.** Paths 1 and 2 depend on partner approval and on what HealthKit and Health Connect expose. File upload has no API terms or permission model and works with every service's export, which GDPR requires. If all else fails, it still delivers the whole product.

#### Casual in-app GPS recording — mobile-only, and not a fourth path

The mobile app can also originate an activity, for someone with no watch running who doesn't want a separate tracker for a walk or drive: a GPS-only recording that, on stop, goes through the same ingest pipeline as every other source (ADR-0007). It depends on no provider, so it is a convenience, and it stays GPS only (§1.1).

### 4.2 Core Features

| Feature | What it gives |
| :--- | :--- |
| **Multi-source ingest** | Each source shows what it would bring in; only what the user ticks comes in |
| **Map** | Fog of War, heatmap and track modes; curated themes; smooth fog edges |
| **Per-activity detail** | The selected route colored by pace |
| **Stories** | Hand-picked activities kept as one trip, with joint stats and their own map |
| **Photos** | The user's photos, each placed on its activity's route |
| **Spots** | Outdoor places from OpenStreetMap, captured by staying in one with the Android app |
| **Activity graph** | A private daily grid, year by year |
| **Trends** | Distance, moving time and elevation, this period against the last |
| **Export** | Print-grade map images with a small signature, free |

**HoldMyTrack keeps the route and no more.** An activity is positions, times and elevation; body signals are never read, stored or shown, whatever the source. An original upload is kept as-is so a track edit or Private location change can rebuild the activity, read only for its route and deleted with it (ADR-0017).

**Photos are the one non-geographic thing kept, because the user adds them.** Each sits on the point of its route where it was taken, kept as a resized copy with EXIF stripped; the original stays wherever the user keeps it (ADR-0024).

**Only what the user keeps comes in, and nothing is guessed for them.** Every source is a gate with nothing ticked to start. Fog of War clears only where kept journeys went. An overlapping candidate is marked, never merged or hidden, and tracks are fixed with the edit tools rather than rules on import (ADR-0039).

**The activity graph is private**: motivation from your own history, visible to nobody else. A shareable version would be a §5.7 decision.

**Stories are hand-picked**, because only the person who went knows which drive was part of the trip and which was the commute. A family trip is usually recorded by one device, so a Story can be sent as a copy; once accepted, it is the recipient's, and nothing the sender does takes it away (ADR-0036). A link that opens a Story for others is a §5.7 decision. A Story view draws only its tracks; a fog pyramid per Story would be per-user storage growing with every Story (§4.3).

**Spots are OpenStreetMap's cleanly tagged places, imported in bulk**: a curated or moderated list would cost staff time, the model's main risk (§6.3). They don't change the fog. A place is marked only by capturing it, never by tracks passing through, so nobody has to tell "passed by" from "went to find it" (ADR-0021).

### 4.3 Cost Model — running a free service

With no revenue, costs must be bounded by design.

| Line item | Scales with | Note |
| :--- | :--- | :--- |
| Basemap storage | Fixed | ~138 GB planet `.pmtiles` on R2 |
| Basemap tile reads | **Usage** | Billed per read; a CDN keeps it flat |
| Activity storage | **Users × history** | Mostly original uploads, kept as long as the activity |
| Fog rasters | **Users** | A few MB per user |
| Photos | **Users × photos** | About 0.5 MB each, capped per account |
| Spots | Fixed | Several million OSM places, about 1 GB in Postgres |
| Compute | Users | One server and Postgres/PostGIS to start |
| Compliance | Fixed | DPA/DPIA work, EU-region hosting (§7) |
| Egress | — | Cloudflare R2 charges none, which is why R2 over S3 |

**At 10,000 active users** (300 activities of ~80 KB, ~3 MB of fog and 100 photos each; 20 map sessions a month of ~300 tile reads at a 90% CDN hit rate): about 900 GB of storage at ~$14/month, ~$2/month of tile reads and $100–300/month of compute, **roughly $150–350/month**. At $5/month per supporter, **break-even is 30–70 recurring donors, well under 1% of users**, a ratio Intervals.icu and Runalyze sustain.

**Two things break this model**, likeliest first:

1. **Storage growing without bound.** An account keeps its activities, uploads and photos until its owner deletes them, used or not. Per-account limits, and cost per active user measured from the start, make growth show in the numbers before the bill (`IMPLEMENTATION.md` §5.7).
2. **A viral moment.** A front-page day at 100× traffic is a cost event with no matching revenue, so rate limits and a spend cap on the CDN and object store are load-bearing infrastructure.

---

## 5. Milestones

The product moves through three milestones (ADR-0031); `docs/ROADMAP.md` carries the work in each.

1. **MVP**: activities, the map modes, Spots, Stories and photos, on the web and Android, free for everyone. It ends at the public launch, gated by compliance (§7) and a funding page already standing (§6.1).
2. **Release and community**: open HoldMyTrack to the public and shape features and sources around what users ask for. Its other job is to find out what community funding can carry, which decides Milestone 3.
3. **Social graph** (§5.7): public pages, followers and the rest, if at all. Whether to build it, and whether paid features, community funding or sponsors pay for it, is decided from Milestone 2's numbers. Staying single-player is one of the options.

### 5.7 Social graph (Milestone 3, not committed)

Milestone 3 is unscheduled. It waits for Milestone 2 to show what community funding can carry, because social features bring moderation, abuse handling and safety obligations: **staff costs, not server costs**, the category donations scale to worst. The decision then is how to pay for them, or not to build them (§5.8).

### 5.8 Why social is last, not just later

A fog map is a precise record of where someone lives and when they are away. Adding a social graph to it changes the threat model. Strava's 2018 heatmap incident is the reference case, and Strava had a large trust-and-safety team at the time.

A product with no headcount should not ship a location-sharing social network: either funding supports moderation or the feature does not ship. Public pages need their privacy defaults decided first: private unless the user chooses otherwise, Private locations applied, and nothing live or recent on show. Sending a copy of a Story (§4.2) stays on this side of the line: it goes to one named person who accepts it, and stays visible to nobody else.

---

## 6. Funding & Sustainability

### 6.1 The model

**HoldMyTrack is free: all features, sources and exports, for everyone, and user data is never sold.** Nothing is promised past Milestone 2 (§5), neither "free forever" nor "no ads"; the one standing commitment is that data is never sold (ADR-0031).

Recurring monthly community funding covers running costs. The funding page stands before launch, because the ask is weaker retrofitted. Open Collective fits best because spending is public by default: for a project pitched as "free and not extracting from you", verifiable accounting is part of the product, so the page shows the monthly bill against monthly income.

### 6.2 What supporters get

**Nothing that non-supporters do not get.** A supporter-only feature would be a paid tier, and whether HoldMyTrack ever has one is the Milestone 3 decision (§5). Supporters get recognition: a badge, a credits page, a say in roadmap priorities.

### 6.3 Risks of this model

* **Donations do not track cost.** A growth spike is a cost spike with no matching income.
* **It depends on one person's continued interest.** Donation-funded projects are typically one maintainer, and the failure mode is burnout. The mitigation is small scope, which is why §1.1 rules out fitness tracking and social waits for Milestone 3.
* **It caps ambition.** It funds a good product, not a team; going further is the Milestone 3 decision (§5).
* **Break-even is low** (§4.3), under 1% of users. That is the strongest argument for the model, and the number to measure through Milestone 2.

---

## 7. Privacy, Security & Compliance

A Fog of War map is a precise map of where a person lives; the 2018 Strava heatmap incident is the canonical warning, and an individual map reveals far more than an aggregate one. `SPEC.md` and `docs/DPIA.md` hold the detailed guarantees.

* **Private locations**: user-named circles (home, work). A track's start or end inside one is hidden from every render and export, which matters because an exported file cannot be recalled; a track only passing through is shown whole.
* **No blanket endpoint trimming.** On a multi-day trail it would cut a gap at every campsite while protecting nothing, and hundreds of tracks cut 100 m from the same door leave a ring with the door at its center. The places worth hiding are the ones the user names.
* **Applied at ingest, server-side**, before anything is stored (`IMPLEMENTATION.md` §4.1): privacy applied at render time leaks through any render bug.
* **Legal basis.** No health data is processed (§1.1), but a precise location history is sensitive personal data: explicit consent, a DPIA before launch, a retention policy, working export and deletion, EU-region hosting for EU users.
* **Deauthorization deletion**: partner APIs such as Wahoo's and COROS's require deleting synced data on disconnect, so any connector does.
* **Health Connect declarations**: requesting more data types than the product uses is a known Play Console rejection cause. HoldMyTrack requests exercise sessions, their routes and the history window, and no health measurement.
* **Photos** reach the server with no EXIF and are placed on the clipped track on every read, so one taken inside a Private location shows at the track's visible end (ADR-0024).
* **Spot captures and Stories are private** to the account. A Story copy is made from the track as the sender sees it, with their Private locations applied and the recipient's on top (ADR-0036).
* **OpenStreetMap's licence.** Spots is an unmodified OSM extract and the country and region outlines are OSM's by way of Overture Maps, both credited on the map; the outlines' derivation is a public script, which keeps them available under the ODbL (ADR-0034).
* **No data sales, ever, written in the privacy policy**, because every user of a free product asks.

---

## 8. Go-To-Market

### 8.1 Launch

* **Community-first**: r/running, r/cycling, r/Garmin, r/Strava, r/FogOfWorld respond to a working demo, and "Free, funded by its users" is a strong post title in each.
* **Creators**: endurance creators and bikepackers, offered a good free tool and a credit.
* **Funding stated from day one**, with a public ledger, before users ask what is monetised instead.

### 8.2 The demo is the ad

One click opens a populated, read-only example account with months of real walks, rides and road trips in every map mode (`SPEC.md` FR-2.1). Sharing it needs no signup, and the screenshot people post of the fog reveal is the marketing. Seeing your own fog takes a free account.

### 8.3 The key validation question

**Will people fund a free tool they like?** (§6.) Under 1% of users at $5/month covers the bill. Measure it through Milestone 2; its answer decides Milestone 3 (§5).