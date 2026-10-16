# HoldMyTrack Server

`cmd/holdmytrack` is the server: the HTTP API, the job worker and the migration runner. This file records the decisions below next to the code, so no one has to re-derive them.

## The contract

**One binary, two modes.** Everything server-side runs as one binary in two modes against one database (`docs/ARCHITECTURE.md` §1.2): `cmd/holdmytrack` with subcommands, instead of `cmd/server` plus `cmd/worker`.

| Subcommand | What it is |
| :-- | :-- |
| `serve` | The HTTP API: ingest and sync endpoints, tile and raster routes, auth, the served style document. |
| `work` | The job worker: parsing, fog raster generation, tile precomputation. Map export renders in the browser (`docs/IMPLEMENTATION.md` §5.5). Dequeues from Postgres with `FOR UPDATE SKIP LOCKED` (`docs/IMPLEMENTATION.md` §3.8, §4.1), with no broker. |
| `migrate` | Applies the SQL in `migrations/`, embedded with `go:embed`, through a small runner (`internal/db`) instead of a library's CLI, so the deployed binary can always migrate the database it is about to talk to. |

Idiomatic Go would split `serve` and `work` into separate binaries. Don't: one deployable is a key decision (`docs/ARCHITECTURE.md` §1.1), and splitting the binary is the first step away from it.

**A new directory under `services/` requires a matching row in `docs/ARCHITECTURE.md` §1.3's "add it when" table, with the measurement that justified it.** A directory named `services/` invites splitting into `api/`, `worker/` and `tiles/` by naming convention alone.