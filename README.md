# <picture><source media="(prefers-color-scheme: dark)" srcset="brand/logo-on-dark.png"><img src="brand/logo-on-light.png" alt="" height="48" align="bottom"></picture> HoldMyTrack

[![Go coverage](https://img.shields.io/codecov/c/github/HoldMyTrack/holdmytrack?flag=go&logo=go&logoColor=white&label=Go%20coverage)](https://app.codecov.io/gh/HoldMyTrack/holdmytrack?flags%5B0%5D=go) [![TypeScript coverage](https://img.shields.io/codecov/c/github/HoldMyTrack/holdmytrack?flag=typescript&logo=typescript&logoColor=white&label=TypeScript%20coverage)](https://app.codecov.io/gh/HoldMyTrack/holdmytrack?flags%5B0%5D=typescript) [![Kotlin coverage](https://img.shields.io/codecov/c/github/HoldMyTrack/holdmytrack?flag=kotlin&logo=kotlin&logoColor=white&label=Kotlin%20coverage)](https://app.codecov.io/gh/HoldMyTrack/holdmytrack?flags%5B0%5D=kotlin)

A free, community-funded platform for tracking outdoor activities and seeing the accumulated shape of where you have been. It aggregates the activity history you already have — from watches, from cloud services, from files — and turns it into maps and exploration stats worth looking at. The point is to see, on one map, every place you have already been — and, just as clearly, the streets, parks and trails nearby that you haven't, so the next walk, run or ride can go somewhere new. It is not a fitness tracker (the Android app can record a plain GPS track for a casual walk or drive, nothing more), it is not a health or fitness advisor, and it has no social graph. See [`docs/VISION.md` §1](docs/VISION.md#1-executive-summary).

Live at [holdmytrack.com](https://holdmytrack.com), with a no-signup demo account to explore.

## What it does

Each feature links to the part of [`docs/SPEC.md`](docs/SPEC.md) that defines its behavior.

- **Bring your whole history.** GPX, FIT and TCX files, `.zip` archives and Google Takeout exports, plus Health Connect sync and plain GPS recording from the Android app — all through one ingest pipeline, with cross-source deduplication ([FR-3](docs/SPEC.md#5-fr-3--activity-upload--ingestion)).
- **One map, three views.** Every track drawn together; Fog of War, which clears wherever you have ever been; and a heatmap of the last year. Pick a track and it is colored by pace ([FR-4](docs/SPEC.md#6-fr-4--map-visualization)).
- **Somewhere new, close to home.** The fog shows exactly which streets, parks and trails nearby you haven't been to yet.
- **Points of interest.** Playgrounds, dog parks, monuments, viewpoints and historic sites from OpenStreetMap, with what OSM knows about each; the Android app captures one when you stand in it for 30 seconds ([FR-15](docs/SPEC.md#17-fr-15--spots)).
- **Stories.** Hand-picked sets of activities — a multi-day hike, a holiday, a race weekend — each with its own name, map and totals ([FR-14](docs/SPEC.md#16-fr-14--stories)).
- **Photos.** Your own photos on an activity, each placed on the route where it was taken by its capture time, moved along it with a slider in the Edit window, shown on the route whenever it's selected and across a whole Story; kept as a resized copy, never the original ([FR-16](docs/SPEC.md#21-fr-16--activity-photos)).
- **Activity graph and trends.** A private, GitHub-style daily grid of your activity, and weekly or monthly totals of the ground you covered ([FR-7](docs/SPEC.md#9-fr-7--activity-graph-profile), [FR-9](docs/SPEC.md#11-fr-9--trends)).
- **Private by default.** Nobody else sees your map. Private locations hide where tracks start and end near home or work, and no heart rate or other health data is ever read or stored ([FR-8](docs/SPEC.md#10-fr-8--privacy-controls), [ADR-0017](docs/adr/0017-no-health-data.md)).
- **Share an image, not your map.** Frame any view and export it as a high-resolution image, sized for social media if you like ([FR-4.10](docs/SPEC.md#fr-410-interactive-frame-and-capture-map-export)).

## Layout

```
holdmytrack/
├── compose.yaml                 # single entry point for local dev
├── compose.prod.yml             # minimal single-VPS production deployment
├── Makefile                     # thin wrapper over compose; `make help`, `make test`
├── codecov.yml                  # Go, TypeScript and Kotlin coverage reporting; informational, never a merge gate
├── .github/workflows/ci.yml     # every test suite on push and PR (docs/DEVELOPMENT.md, "CI")
├── ops/alloy/config.alloy       # the alloy service's log and metrics shipping to Grafana Cloud (docs/DEPLOY.md §13)
├── ops/grafana/                 # alert rules (apply.py) and dashboard pushed to the Grafana stack (docs/DEPLOY.md §13)
├── scripts/backup.sh            # nightly Postgres dump and object sync to the backup bucket (docs/DEPLOY.md §11)
├── scripts/maintenance.sh       # flips the deployment's maintenance page (docs/DEPLOY.md §7)
├── scripts/restore-drill.sh     # restores the newest backup into a throwaway container and checks it (docs/DEPLOY.md §11)
├── scripts/spots-extract.sh     # downloads OSM data and filters it into the Spots places file, off the server (docs/DEPLOY.md §6)
├── .env.example                 # Compose interpolation only — never VITE_*
├── .env.prod.example            # compose.prod.yml's own env template
├── .editorconfig
├── docs/
│   ├── VISION.md                # product, market, funding, roadmap
│   ├── ARCHITECTURE.md          # system architecture, key decisions, stack
│   ├── IMPLEMENTATION.md        # schema, workflows, each feature's own detail
│   ├── SPEC.md                  # observable behavior, FR-N.M, independent of the above
│   ├── ROADMAP.md               # the remaining-work checklist
│   ├── KNOWN_ISSUES.md          # currently-open defects in shipped functionality
│   ├── BRAINSTORM.md            # ideas raised but not yet decided
│   ├── adr/                     # Architecture Decision Records — why, not just what
│   ├── DEVELOPMENT.md           # running it locally, verification, gotchas, commands
│   └── DEPLOY.md                # the production deployment runbook
├── AGENTS.md                    # orientation for coding agents
├── brand/                       # logo.svg, the mark's master; make_icons.py renders every logo and icon from it, the README's included
├── apps/
│   ├── android/                 # Kotlin — Phase 2. Health Connect sync + in-app GPS recording; own docs/
│   ├── ios/                     # Swift — Phase 2, HealthKit ingest. Placeholder
│   └── web/                     # the Phase 1 product
│       ├── Dockerfile  .dockerignore  docker/entrypoint.sh
│       ├── package.json  tsconfig.json  vite.config.ts
│       ├── scripts/build-basemap.sh
│       ├── public/basemap/      # .pmtiles gitignored; fonts + sprites committed
│       ├── src/
│       └── tests/
└── services/
    └── server/                  # the api/worker binary of docs/ARCHITECTURE.md §1.2
        ├── go.mod  go.sum
        ├── README.md            # the serve/work/migrate contract and the open decisions
        ├── Dockerfile
        ├── cmd/holdmytrack/     # main.go: serve / work / migrate
        ├── internal/            # config, db, fog, geo, httpapi, i18n, ingest, mail, mapstyle, parse, spots, storage, takeout, tilemath, web, worker
        └── migrations/          # embedded *.sql, applied in order by `cmd/holdmytrack migrate`
```

This tree is the canonical one; do not let a second tree exist anywhere else to disagree with it.

## Documents

| Document | Read it when |
| :-- | :-- |
| [`docs/VISION.md`](docs/VISION.md) | Product scope, market rationale, funding model, phase roadmap. |
| [`docs/ARCHITECTURE.md`](docs/ARCHITECTURE.md) | System-level shape: the key decisions, the target architecture, the stack, and what's deliberately deferred. |
| [`docs/IMPLEMENTATION.md`](docs/IMPLEMENTATION.md) | Database schema, and each feature's own implementation detail — ingest, tile/fog workflows, accounts, deployment, engineering risks. |
| [`docs/SPEC.md`](docs/SPEC.md) | A precise, testable statement of what the system currently does, independent of both the business rationale and the implementation. |
| [`docs/ROADMAP.md`](docs/ROADMAP.md) | The remaining-work checklist — what's left to reach the product `VISION.md` describes. |
| [`docs/KNOWN_ISSUES.md`](docs/KNOWN_ISSUES.md) | Currently-open defects in already-shipped functionality — the opposite direction from `ROADMAP.md`'s planned work. |
| [`docs/BRAINSTORM.md`](docs/BRAINSTORM.md) | Ideas raised but not yet decided — one level upstream of `ROADMAP.md`. |
| [`docs/adr/`](docs/adr/) | Architecture Decision Records — why each consequential, hard-to-reverse decision was made, and what was rejected. |
| [`docs/DEVELOPMENT.md`](docs/DEVELOPMENT.md) | Running it locally, the verification checklist, gotchas worth not rediscovering, and the command reference. |
| [`docs/DEPLOY.md`](docs/DEPLOY.md) | You're standing up an actual deployment. |
| [`AGENTS.md`](AGENTS.md) | You are a coding agent opening the repo cold — this same routing table, self-contained, plus the Markdown formatting convention these docs follow. |

## License

MIT — see [LICENSE](LICENSE).