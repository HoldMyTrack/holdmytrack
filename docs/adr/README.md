# Architecture Decision Records

Each ADR here captures one consequential, hard-to-reverse decision on its own — the alternatives that were actually considered, why they were rejected, and what the decision costs as well as what it buys. This is a different job from the other docs in `docs/`:

- **`docs/ARCHITECTURE.md`** is the current-state reference — what the system looks like today, in one place, kept current as things change.
- **These ADRs** are the historical record of *why* — each one frozen at the decision it documents, not rewritten as the system evolves further. If a later decision changes course, it gets its own new ADR that supersedes the old one; the old one's Status line says so and stays otherwise unedited.

Numbered in rough order of how foundational the decision is, not strictly chronological.

| ADR | Decision |
| :-- | :-- |
| [0001](0001-three-independent-ingest-paths.md) | Three independent ingest paths, none of them Strava |
| [0002](0002-privacy-applied-at-ingest.md) | Privacy is applied at ingest, never at render |
| [0003](0003-precomputed-raster-fog-pyramid.md) | Precomputed raster fog pyramid, not per-request geometry union or H3 |
| [0004](0004-demo-account-reuses-real-pipeline.md) | The no-signup demo reuses the real account pipeline, bounded by a TTL |
| [0005](0005-client-side-export-rendering.md) | High-resolution export renders client-side, not in a headless worker pool |
| [0006](0006-minimal-deployment-same-origin-caddy.md) | Minimal deployment serves frontend and API from one origin via Caddy |
| [0007](0007-in-app-gps-recording-submits-directly.md) | In-app GPS recording submits directly to the sync endpoint, not via a Health Connect/HealthKit round-trip |
| [0008](0008-vector-tiles-for-country-region-boundary-tiers.md) | The Country/Region zoom tiers are live vector tiles, not a second precomputed raster pyramid |
| [0009](0009-google-sign-in-server-side-code-flow.md) | Sign in with Google is a server-side authorization-code flow with no OAuth library, auto-linking by verified email |
| [0010](0010-private-locations-replace-endpoint-trim.md) | Private locations replace the fixed endpoint trim |
| [0011](0011-takeout-reader-in-go.md) | The Takeout reader is Go code in the server, not a pathify subprocess |
| [0012](0012-server-rendered-pages-react-for-the-map.md) | Every page is server-rendered HTML sharing one header; React is kept for the map page |
| [0013](0013-admin-panel.md) | An in-app, read-only admin panel, gated by a CLI-granted flag |
| [0014](0014-localization.md) | Localization with in-house catalogs, one language decided by the server |
| [0015](0015-identities-table-and-facebook-sign-in.md) | Sign in with Facebook, never linked by email, with identities in their own table |
| [0016](0016-native-sign-in.md) | Native sign-in: Credential Manager for Google, a browser-tab handoff for Facebook |
| [0017](0017-no-health-data.md) | No health data: heart rate is never read or stored, and the pace/heart-rate profile card is removed |
| [0018](0018-no-explorer-tiles.md) | No explorer tiles: Fog of War is the exploration mechanic, with no tile score |
| [0019](0019-dark-theme.md) | A dark theme: one palette per theme for both clients, chosen per device, with a fog veil per basemap |
| [0020](0020-stories-hand-picked-and-private.md) | Stories are hand-picked, private sets of activities, shown in Normal mode with a story-scoped date picker |
| [0021](0021-spots-from-osm-visits-from-tracks.md) | Spots are bulk-imported from OpenStreetMap, and a visit is five minutes inside one, worked out from tracks |
| [0022](0022-satellite-imagery-optional-base-map.md) | Satellite imagery is an optional base map, opt-in per deployment, drawn under the vector roads and labels |
| [0023](0023-spots-captured-live-on-the-phone.md) | A spot can be captured live in the Android app, by staying 30 seconds inside it, and the capture is kept on the server |
| [0024](0024-activity-photos-resized-and-route-bound.md) | An activity can carry the user's photos, kept as resized copies on our own storage, each placed on the route by the time it was taken |
| [0025](0025-language-menu-for-everyone.md) | A language menu in the header for everyone, remembered in a cookie; signed in, it also saves the account's setting |
| [0026](0026-material-3-on-views.md) | Android's UI is Material 3 on the existing Views, not Compose, without dynamic color |
| [0027](0027-spots-retired-not-deleted.md) | A place gone from OpenStreetMap is retired, never deleted, and still shown to the accounts that captured it |
| [0028](0028-spots-capture-only-no-visits.md) | Spots have one mark, the capture; visits worked out from tracks are dropped |
| [0029](0029-backups-to-a-second-r2-bucket.md) | Nightly backups go to a second R2 bucket, and only what can't be rebuilt is copied |
| [0030](0030-monitoring-on-grafana-cloud.md) | Monitoring runs on Grafana Cloud's free tier, fed by one Alloy collector, with personal data scrubbed before anything leaves |
| [0031](0031-three-milestones-funding-decided-on-evidence.md) | Three milestones (MVP, community, social graph); the funding model is decided on Milestone 2's evidence, and data is never sold |
| [0032](0032-accept-and-enqueue.md) | A request that brings activities only accepts them; the worker does the storage work, and unpacks archives |
| [0033](0033-android-bottom-navigation-single-activity.md) | Android's main screens are tabs of one Activity, each a Fragment, under a bottom navigation bar |
| [0034](0034-country-region-outlines-from-overture.md) | Country and region outlines come from Overture Maps' divisions, land-clipped, loaded from our own copy at deploy time |
| [0035](0035-activity-local-timezone.md) | An activity's times show, and its days group, in the timezone it was recorded in |
| [0036](0036-send-a-copy-of-a-story.md) | A Story can be sent as a copy to another account, which accepts it into a Story and activities of its own; nothing syncs afterwards |
| [0037](0037-fog-heatmap-64px-tiles.md) | Fog and Heatmap tiles are 64 px, stretched smoothly by the clients |
| [0038](0038-fog-heatmap-pixels-from-afar.md) | Fog and Heatmap show square pixels from afar and are smoothed from zoom 14 |

## Writing a new one

Copy the shape of an existing ADR: Status, Context, Decision, Alternatives considered, Consequences (including the honest costs, not just the benefits). Number it one past the current highest. Link it from the table above in the same change.