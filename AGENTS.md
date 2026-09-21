# AGENTS.md

Orientation for coding agents working in this repository. Read this first, then the one document below that covers what you are about to change.

## What FitMap is

FitMap is a free, community-funded platform for tracking outdoor activities and seeing the accumulated shape of where you have been. It aggregates activity history the user already has — from watches, from cloud services, from files — and turns it into maps and exploration stats worth looking at. Three things it deliberately is **not**: it does not record workouts (no start button, no live GPS — it begins where the watch's recording ends), it has no social graph yet (no feed, follows, kudos, segments or leaderboards; see `docs/VISION.md` §5.5–§5.6 for why that ordering is deliberate rather than merely cautious), and it is not a health or fitness advisor (no HR zones, no training load, no recovery/readiness, no sleep tracking — pace and heart rate are shown per activity as route context, not analysed as a coaching product). It is funded by recurring community donations with public accounting, so there is no subscription tier to design around and no paywalled feature to hide behind.

## Which document to read, for what

| Document | Read it when |
| :-- | :-- |
| `docs/VISION.md` | You need product scope, market rationale, the funding model, or the phase roadmap (§5). It is the authority on *what* ships and in what order. |
| `docs/ARCHITECTURE.md` | You need the system-level shape — key decisions, the target architecture diagram, the stack, what's deliberately deferred. Read this *before* `IMPLEMENTATION.md` for anything touching more than one feature. |
| `docs/IMPLEMENTATION.md` | You are touching the database schema, ingest or tile workflows, or any specific feature's server-side implementation. It is the authority on *how* each already-built piece works, one level down from `ARCHITECTURE.md`'s system-level view. |
| `docs/SPEC.md` | You need a precise, testable statement of what the system currently does — numbered functional requirements (FR-N.M), by feature area, independent of both the business rationale and the implementation details. The authority on *observable behavior*: given these preconditions and inputs, this is what happens. |
| `docs/ROADMAP.md` | You need the remaining-work checklist — what's left to reach the product `VISION.md` describes, broken into small steps. |
| `docs/BRAINSTORM.md` | You need the list of ideas raised but not yet decided — not sequenced, not committed, one level upstream of `ROADMAP.md`. An idea belongs here until someone decides whether it becomes a `ROADMAP.md` item or gets dropped. |
| `docs/KNOWN_ISSUES.md` | You need the list of currently-open defects in already-shipped functionality — the opposite direction from `ROADMAP.md`'s planned work. Found a bug while building something? It belongs here, not in `ROADMAP.md`. |
| `docs/adr/` | You need *why* one specific consequential decision was made the way it was — the alternatives considered and rejected, not just today's state. `ARCHITECTURE.md` says what's true now; an ADR is frozen at the decision it documents. |
| `docs/DEVELOPMENT.md` | You need to run the stack, verify a change, or you're about to rediscover a gotcha someone already hit. |
| `docs/DEPLOY.md` | You're standing up an actual deployment, not just reading about the deployment scaffolding's design (`IMPLEMENTATION.md` §5.8 covers that). |

For repository layout, the root `README.md` is now the authority — it absorbed a former third planning document once that document's structure had actually been applied and its checklist finished. `services/server/README.md` covers the backend scaffold's open decisions specifically.

`apps/android` has grown its own `docs/` (`ARCHITECTURE.md`, `IMPLEMENTATION.md`, `SPEC.md`, `ROADMAP.md`), mirroring the root set one level down — `apps/android/docs/ARCHITECTURE.md`'s own header covers how the two relate. The split that matters when you're documenting an Android feature: a wire contract and its server-side implementation (the endpoint, the schema, what the ingest pipeline does with it) belongs in root `docs/IMPLEMENTATION.md` even when the feature is Android-only (Health Connect sync, in-app GPS recording) — the Android app's own file-by-file implementation of that feature (which Kotlin class does what, its local persistence, its UI) belongs in `apps/android/docs/IMPLEMENTATION.md` instead. `apps/web` and `apps/ios` have no `docs/` of their own yet, so their client-side implementation detail stays in the root docs for now; the same split applies if either grows one.

`VISION.md`, `ARCHITECTURE.md` and `IMPLEMENTATION.md` cite each other by section number (`§5.2`, `§1.2`) rather than by page or quotation; `SPEC.md` cites all three the same way, but internally cites its own requirements as `FR-N.M`, not `§N.M` — its own top-level section numbers don't align with the FR groups (Introduction and Actors sit ahead of FR-1), so `§8` inside that document does not mean "FR-8." `ARCHITECTURE.md` and `IMPLEMENTATION.md` were one document until this content was split out — existing citations to what's now `ARCHITECTURE.md` §1/§2 keep the exact section numbers they already had, just pointing at a different file now; `IMPLEMENTATION.md` itself starts at §3 for the same reason. When you change something any of these documents specifies, update the document in the same change — a citation that no longer matches the code (or another document) is worse than no citation.

This file stays limited to orientation — the table above, `What FitMap is`, and the Markdown convention below. If you're about to write what a feature does, how it works, or why it was built a particular way, that belongs in `SPEC.md` (what) or `IMPLEMENTATION.md` (how/why), not here.

## Markdown Formatting Style

- **No hard-wrapping:** Markdown files are not hard-wrapped at a fixed column (~90-100 chars) the way a plain-text file would be.
- **No single newlines in paragraphs:** Markdown treats a single `\n` inside a paragraph as a soft break (renders as a space, not a line break), so a mid-paragraph line break serves no purpose — write each paragraph as one line.
- **No blank lines in lists:** Do not add empty lines between consecutive list items or block elements unless explicitly requested.
- **No trailing newline at EOF:** Do not add a final trailing newline character (`\n`) at the end of `.md` files. Cut the file off immediately after the last character.
- **Strict writing:** When modifying or writing `.md` files, preserve the existing newline density exactly.