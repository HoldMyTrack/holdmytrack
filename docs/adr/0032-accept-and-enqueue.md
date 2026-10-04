# ADR-0032: A request that brings activities only accepts them; the worker does the storage work, and unpacks archives

## Status

Accepted. Built: inline raw payloads (`internal/ingest/enqueue.go`, `IMPLEMENTATION.md` §4.1 step 1) and the worker's `unpack` job (`internal/unpack`, §4.0.1), with the web's Upload menu and the Android app's Upload screen showing an archive's notes once it's unpacked.

## Context

In production, `POST /v1/sync/activities` took 10 seconds. A request handled its activities one after another, and each one cost three round trips: an existence check, a write of the raw payload to R2, and a job insert. The R2 write dominated. A Google Maps Timeline import sends 100 activities a request, so it held its connection for about 10 seconds, and a phone sync of 25 for a few. A `.zip` or Takeout upload ran the same loop over every file it contained, up to 5,000, after the upload itself had arrived, so a large archive could keep its connection open for minutes.

Import is allowed to take a while: the Upload menu already shows it as "Processing 120 of 584". What shouldn't depend on the size of an import is how long the API takes to answer. A long-held connection runs into client and proxy timeouts, leaves a phone waiting in the foreground, and makes the route's latency on the dashboard meaningless.

## Decision

**A request validates, checks for duplicates, and enqueues: nothing more.** The `ingest` job carries the raw payload in a column of its own, `jobs.raw`. One query finds which of a request's activities the account already has, and one multi-row insert adds jobs for the rest (`ingest.EnqueueRaw`). Every path that brings activities goes through it: a single file, a phone sync, and each archive's files. The worker writes the bytes to the job's raw key in object storage and clears the column before ingesting (`ingest.PromoteRaw`).

**The bytes go in a column, not inside `payload`.** The Upload menu's and `/sync`'s queries read `payload->>'batch'` and similar on every ingest row of an account, and Postgres would have to detoast a large JSONB value whole to read one field of it.

**Archives are unpacked by the worker.** The request checks that the file is a zip and whether it is a Takeout export, both from the central directory alone, stores the archive once, and enqueues one `unpack` job. That job walks the archive under the same zip-bomb bounds and enqueues its files through `EnqueueRaw`. The per-file outcomes (already imported, skipped, more files than one upload reads) reach the Upload menu once it finishes, instead of arriving in the upload's response.

## Alternatives considered

**Run the per-activity R2 writes concurrently.** This would take a 100-activity batch from about 10 seconds to 1 or 2, with no schema change. Rejected: the request would still grow with its batch, and an archive of thousands of files would still hold its connection for a long time.

**Store each request's whole batch as one object and split it in a worker job.** One R2 write per request instead of one per activity. Rejected for sync: the Upload menu counts one `ingest` job per activity, and until the split ran an import would show no progress at all. It also adds a job kind where a column is enough. For archives, this is the shape chosen, because an archive can be 512 MiB, too large to hold in Postgres.

**Have the client upload straight to R2 with a presigned URL.** This would take the API out of the transfer entirely. Rejected for now: every client would have to change, and the server would still need to be told the upload had finished. That is a larger change than the problem needs.

## Consequences

- A request's time no longer depends on how many activities it carries: two queries for a sync batch, plus one object write for an archive.
- A pending job's bytes live in Postgres until the worker takes the job. A sync request is at most 64 MiB, and Postgres compresses large `bytea` values. While the worker is behind, the database grows by the size of what is waiting.
- A request's new activities are enqueued together or not at all. An insert that fails rejects the whole request's new activities as an internal error, where the old loop failed them one at a time.
- An archive's skipped and already-imported counts arrive after the upload has finished, through `GET /v1/uploads/active`, so a client has to keep asking after the upload response to show them.