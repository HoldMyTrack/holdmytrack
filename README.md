# HoldMyTrack

[![codecov](https://codecov.io/gh/HoldMyTrack/holdmytrack/graph/badge.svg)](https://codecov.io/gh/HoldMyTrack/holdmytrack)

A free, community-funded platform for tracking outdoor activities and seeing the accumulated shape of where you have been. It aggregates the activity history you already have — from watches, from cloud services, from files — and turns it into maps and exploration stats worth looking at. It is not a fitness tracker (the Android app can record a plain GPS track for a casual walk or drive, nothing more), it is not a health or fitness advisor, and it has no social graph. See [`docs/VISION.md` §1](docs/VISION.md#1-executive-summary).

## Layout

```
holdmytrack/
├── compose.yaml                 # single entry point for local dev
├── compose.prod.yml             # minimal single-VPS production deployment
├── Makefile                     # thin wrapper over compose; `make help`, `make test`
├── codecov.yml                  # Go coverage reporting; informational, never a merge gate
├── .github/workflows/ci.yml     # every test suite on push and PR (docs/DEVELOPMENT.md, "CI")
├── scripts/maintenance.sh       # flips the deployment's maintenance page (docs/DEPLOY.md §7)
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
    └── server/                  # the "HoldMyTrack Server" of docs/ARCHITECTURE.md §1.2
        ├── go.mod  go.sum
        ├── README.md            # the serve/work/migrate contract and the open decisions
        ├── Dockerfile
        ├── cmd/holdmytrack/     # main.go: serve / work / migrate
        ├── internal/            # config, db, fog, geo, httpapi, ingest, mail, mapstyle, parse, storage, tilemath, web, worker
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