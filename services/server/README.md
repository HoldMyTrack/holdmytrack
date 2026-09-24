# HoldMyTrack Server

`cmd/fitmap serve`, `work` and `migrate` are real now: the Path-3 upload endpoint, its job-queue worker, and the embedded-migration runner. This file still exists for the same reason it always did — so the decisions below are recorded before someone has to re-derive them, and so `IMPLEMENTATION.md`'s contract for this service is written down next to the code that implements it.

## The contract

**One binary, two modes.** `docs/ARCHITECTURE.md` §1.2 is explicit: "Everything server-side runs as one binary in two modes (`serve` and `work`) against one database." That is `cmd/fitmap` with subcommands, not `cmd/server` plus `cmd/worker`:

| Subcommand | What it is |
| :-- | :-- |
| `serve` | The HTTP API: ingest endpoints, tile and raster routes, auth, exports. |
| `work` | The job worker: parsing, fog raster generation, tile precomputation, export rendering. Dequeues from Postgres with `FOR UPDATE SKIP LOCKED` (§3.8, §4.1) — there is no broker, deliberately. |
| `migrate` | Applies the SQL in `migrations/`, embedded with `go:embed`. Not a separate tool and not a library's CLI, so that the deployed binary can always migrate the database it is about to talk to. |

Idiomatic Go would split `serve` and `work` into separate binaries, and a later reader will be tempted to "fix" this toward that convention. Don't: `docs/ARCHITECTURE.md` §1.1 exists largely to collapse a two-language, six-service design into one deployable, and splitting the binary is the first step back toward the thing that was rejected.

The same guard applies one level up. **A new directory under `services/` requires a matching row in `docs/ARCHITECTURE.md` §1.3's "add it when" table, along with the measurement that justified it.** A directory named `services/` invites re-fragmentation into `api/`, `worker/` and `tiles/` by naming convention alone.
