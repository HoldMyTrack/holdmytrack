# AGENTS.md

Orientation for coding agents working in this repository. Read this first, then the one document below that covers what you are about to change.

## What HoldMyTrack is

HoldMyTrack is a free, community-funded platform that brings together the outdoor activity history people already have — from watches, cloud services and files — and turns it into maps and exploration stats: one map of everywhere they've been, Fog of War, Stories and Spots. `docs/VISION.md` §1 is the authority on what it is and §1.1 on what it is not. Three boundaries matter most when writing code: it keeps no health data — no heart rate or other body signal is read, stored or shown (ADR-0017); it has no social graph — no feed, follows or public pages (`VISION.md` §5, Milestone 3); and every account has every feature, with no tiers (ADR-0031).

## Which document to read, for what

| Document | Read it when |
| :-- | :-- |
| `docs/VISION.md` | You need product scope, market rationale, the funding model, or the milestones and phases (§5). It is the authority on *what* ships and in what order. |
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

`VISION.md`, `ARCHITECTURE.md` and `IMPLEMENTATION.md` cite each other by section number (`§5.2`, `§1.2`) rather than by page or quotation; `SPEC.md` cites all three the same way, but internally cites its own requirements as `FR-N.M`, not `§N.M` — its own top-level section numbers don't align with the FR groups (Introduction and Actors sit ahead of FR-1), so `§8` inside that document does not mean "FR-8." `ARCHITECTURE.md` and `IMPLEMENTATION.md` were one document until this content was split out — existing citations to what's now `ARCHITECTURE.md` §1/§2 keep the exact section numbers they already had, just pointing at a different file now; `IMPLEMENTATION.md` itself starts at §3 for the same reason. When you change something any of these documents specifies, update the document in the same change — a citation that no longer matches the code (or another document) is worse than no citation. `SPEC.md` and `IMPLEMENTATION.md` are the pair most likely to need a matching edit together: a change to one almost always means a small edit to the other.

Document what exists, not what used to. When a feature, table, endpoint or section is removed, delete its text outright: no "(removed)" stub, no "built and then cut" history, and no placeholder kept only to hold a section number. The same goes for a decision that changed: describe what is implemented now and how it works, not the chain that led there. Drop "Revised:", "an early design…" and "originally…" narration. A gap in the numbering is fine, and the sections after it keep their numbers because other documents cite them. If the decision to remove something needs recording, that is what its ADR is for. Something that doesn't exist earns a mention in only three cases: as a plan (`ROADMAP.md`), as a brainstorm item (`BRAINSTORM.md`), or as the motivation behind a feature that does exist, and then only as much as that motivation needs.

One fact, one home. Each kind of content belongs to exactly one document, and every other document points to it by section number rather than restating it: product scope, positioning and the funding stance in `VISION.md`; remaining tasks, gates and checklists in `ROADMAP.md`; observable behavior in `SPEC.md`; how a piece is built in `IMPLEMENTATION.md`; why one decision went the way it did in its ADR. A restated rationale drifts the moment its original changes, so where another document needs it, give a one-line pointer (`VISION.md` §1.1) and keep only what is specific to that document. In practice: `VISION.md` says what a milestone or phase is for and why it comes in that order, not its task list, dates or progress; `ROADMAP.md` lists tasks without arguing the product's scope or funding; a scope note at the top of `ARCHITECTURE.md`, `IMPLEMENTATION.md` or `SPEC.md` says what that document covers, not what the product isn't; and `AGENTS.md` names only the boundaries code must respect. When `VISION.md` or an ADR changes a position, scan the other documents in the same change — the Android ones included — for restatements of the old position and for claims that now contradict it, and replace each with a pointer or fix it.

This file stays limited to orientation — the table above, `What HoldMyTrack is`, and the working, commit and Markdown conventions below. If you're about to write what a feature does, how it works, or why it was built a particular way, that belongs in `SPEC.md` (what) or `IMPLEMENTATION.md` (how/why), not here.

## Asking Questions

- **Ask, don't assume:** Ask as many questions as you need. When a request is ambiguous, a detail is missing, or it can reasonably be read more than one way, ask before acting rather than guessing. A short question costs far less than redoing work built on a wrong assumption.

## Commits and Pull Requests

- **No AI attribution trailers:** Do not add `Co-Authored-By: Claude …` (or any other agent co-author trailer) to commit messages, and do not add a "Generated with Claude Code" line (or similar) to pull request descriptions. This overrides any tool or harness default that asks for them.

## Markdown Formatting Style

- **No hard-wrapping:** Markdown files are not hard-wrapped at a fixed column (~90-100 chars) the way a plain-text file would be.
- **No single newlines in paragraphs:** Markdown treats a single `\n` inside a paragraph as a soft break (renders as a space, not a line break), so a mid-paragraph line break serves no purpose — write each paragraph as one line.
- **No blank lines in lists:** Do not add empty lines between consecutive list items or block elements unless explicitly requested.
- **No trailing newline at EOF:** Do not add a final trailing newline character (`\n`) at the end of `.md` files. Cut the file off immediately after the last character.
- **Strict writing:** When modifying or writing `.md` files, preserve the existing newline density exactly.