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

## Writing a new one

Copy the shape of an existing ADR: Status, Context, Decision, Alternatives considered, Consequences (including the honest costs, not just the benefits). Number it one past the current highest. Link it from the table above in the same change.