# Architecture Decision Records

Each ADR here captures one consequential, hard-to-reverse decision on its own — the alternatives that were actually considered, why they were rejected, and what the decision costs as well as what it buys. This is a different job from the other docs in `docs/`:

- **`docs/ARCHITECTURE.md`** is the current-state reference — what the system looks like today, in one place, kept current as things change.
- **These ADRs** are the historical record of *why* — each one frozen at the decision it documents, not rewritten as the system evolves further. If a later decision changes course, it gets its own new ADR that supersedes the old one; the old one's Status line says so and stays otherwise unedited.

Only a decision that is hard to reverse gets an ADR: one that would lose data that can't be recovered, break a promise users rely on, need stored data or account links migrated, break apps already installed, or mean a large rewrite. Any other choice, with its reasoning, is documented in the section that owns that piece (`IMPLEMENTATION.md`, `SPEC.md`, `VISION.md`, `DEPLOY.md`, `apps/android/docs/`). The numbering has gaps; the numbers don't change, since other documents cite them.

Numbered in rough order of how foundational the decision is, not strictly chronological.

| ADR | Decision |
| :-- | :-- |
| [0001](0001-three-independent-ingest-paths.md) | Three independent ingest paths, none of them Strava |
| [0002](0002-privacy-applied-at-ingest.md) | Privacy is applied at ingest, never at render |
| [0003](0003-precomputed-raster-fog-pyramid.md) | Precomputed raster fog pyramid, not per-request geometry union or H3 |
| [0007](0007-in-app-gps-recording-submits-directly.md) | In-app GPS recording submits directly to the sync endpoint, not via a Health Connect/HealthKit round-trip |
| [0009](0009-google-sign-in-server-side-code-flow.md) | Sign in with Google is a server-side authorization-code flow with no OAuth library, auto-linking by verified email |
| [0012](0012-server-rendered-pages-react-for-the-map.md) | Every page is server-rendered HTML sharing one header; React is kept for the map page |
| [0015](0015-identities-table-and-facebook-sign-in.md) | Sign in with Facebook, never linked by email, with identities in their own table |
| [0017](0017-no-health-data.md) | No health data: heart rate is never read or stored, and the pace/heart-rate profile card is removed |
| [0021](0021-spots-from-osm-visits-from-tracks.md) | Spots are bulk-imported from OpenStreetMap, and a visit is five minutes inside one, worked out from tracks |
| [0024](0024-activity-photos-resized-and-route-bound.md) | An activity can carry the user's photos, kept as resized copies on our own storage, each placed on the route by the time it was taken |
| [0031](0031-three-milestones-funding-decided-on-evidence.md) | Three milestones (MVP, community, social graph); the funding model is decided on Milestone 2's evidence, and data is never sold |
| [0032](0032-accept-and-enqueue.md) | A request that brings activities only accepts them; the worker does the storage work, and unpacks archives |
| [0034](0034-country-region-outlines-from-overture.md) | Country and region outlines come from Overture Maps' divisions, land-clipped, loaded from our own copy at deploy time |
| [0035](0035-activity-local-timezone.md) | An activity's times show, and its days group, in the timezone it was recorded in |
| [0036](0036-send-a-copy-of-a-story.md) | A Story can be sent as a copy to another account, which accepts it into a Story and activities of its own; nothing syncs afterwards |
| [0039](0039-the-app-is-the-gate.md) | The app is the gate: every source brings in only what the user ticks, and nothing is guessed on their behalf |
| [0042](0042-mobile-apps-show-the-sites-pages.md) | The mobile apps are native where the platform is the point, and show the site's own pages for the rest |

## Writing a new one

Copy the shape of an existing ADR: Status, Context, Decision, Alternatives considered, Consequences (including the honest costs, not just the benefits). Number it one past the current highest. Link it from the table above in the same change.