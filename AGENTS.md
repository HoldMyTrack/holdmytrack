# AGENTS.md

Orientation for coding agents working in this repository. Read this first, then the document that covers what you are about to change: the root `README.md` lists every document and when to read it, and its tree is the authority on repository layout.

## What HoldMyTrack is

HoldMyTrack is a free, community-funded platform that keeps the outdoor journeys people choose, from watches, the phone, cloud services and files, and turns them into maps and exploration stats: Stories, one map of everywhere those journeys went, Fog of War and Spots. `docs/VISION.md` §1 says what it is and §1.1 what it is not. Four boundaries apply to every change:
- No health data: no heart rate or other body signal is read, stored or shown (ADR-0017).
- Every source is a gate: only what the user ticks comes in, nothing starts ticked, and no heuristic changes or hides an activity on their behalf (ADR-0039).
- No social graph: no feed, follows or public pages (`VISION.md` §5, Milestone 3).
- Every account has every feature, with no tiers (ADR-0031).

## Documentation rules

**Update the documents in the same change.** When a change alters something a document specifies, edit that document in the same commit. A JSON endpoint's request or response changes `services/server/api/` too, followed by `npm run gen:api-types` in `apps/web` (`IMPLEMENTATION.md` §4.31). A change to what is collected, where it is stored, who processes it or how long it is kept updates `docs/DPIA.md`. A change to anything the Android app shows or says means checking the tester page, `services/server/internal/web/templates/pages/testing.html`, which quotes it. `SPEC.md` and `IMPLEMENTATION.md` usually need a matching edit together.

**Citations.** Documents cite each other by section number: `VISION.md` §1.1, `IMPLEMENTATION.md` §4.7, `SPEC.md` FR-5.8, ADR-0039. Name the document before the number unless the citation points into the same file. `SPEC.md` cites its own requirements as FR-N.M, and its top-level section numbers do not match the FR groups, so its §8 is not FR-8. `IMPLEMENTATION.md` starts at §3 because `ARCHITECTURE.md` keeps §1 and §2. Never renumber a section or requirement: a deleted one leaves a gap. Run `scripts/check-doc-refs.sh` before committing; it fails on any citation whose heading no longer exists, in documents, code comments and templates.

**One fact, one home.** Each kind of content lives in one document, and every other document points to it by section number instead of restating it:
- `VISION.md`: product scope, positioning, funding, and what each milestone is for. No task lists, dates or progress.
- `ROADMAP.md`: remaining tasks and gates, without arguing scope.
- `SPEC.md` (root and Android): only what a user or client can rely on. Limits, privacy guarantees, what is kept and deleted, and rules that cross features. No UI wording, layout, menu contents or status codes; those live in the UI code and `services/server/api/`.
- `IMPLEMENTATION.md` (root and Android): only flows that cross files, packages or apps; invariants every change must keep; the reason for a design where the obvious alternative was rejected; and traps not visible in the code. Detail confined to one package goes in that package's doc comment (`doc.go`, KDoc, TSDoc), or nowhere if the code already says it. Name a file or function only as a pointer.
- An ADR: why a hard-to-reverse decision went the way it did (`docs/adr/README.md` says which decisions qualify). The reasoning behind any other choice sits beside the description of that piece. A new ADR stays under about 600 words; existing ADRs are frozen.
- `AGENTS.md`: orientation and conventions only. What a feature does goes in `SPEC.md`, how it works in `IMPLEMENTATION.md`.

When `VISION.md` or an ADR changes a position, scan the other documents, the Android ones included, for restatements of the old position and fix each one or replace it with a pointer.

**Android documents.** `apps/android/docs/` mirrors the root set one level down. A wire contract and its server side (the endpoint, the schema, what ingest does with it) belong in root `docs/IMPLEMENTATION.md` even for an Android-only feature; the app's own side (local persistence, background work, UI flow) belongs in `apps/android/docs/IMPLEMENTATION.md`. `apps/web` and `apps/ios` have no `docs/` yet, so their client detail stays in the root documents.

**Document what exists.** When a feature, table, endpoint or section is removed, delete its text: no "(removed)" stub, no history, no placeholder kept for a section number. Describe a changed decision as it is implemented today. Something that does not exist appears only as a plan (`ROADMAP.md`), an idea (`BRAINSTORM.md`), or as much motivation as an existing feature needs. A defect in shipped functionality goes in `KNOWN_ISSUES.md`.

**Draw instead of describe.** A flow, sequence, state machine or set of relations is a Mermaid diagram, followed by a few lines for what it cannot show: invariants and traps.

**Write tight.** One statement per fact, and at most one cross-reference per paragraph unless more are load-bearing. Cut intensifiers ("deliberately", "genuinely", "actually", "simply", "just", "really"); "not X but Y" contrasts where nobody proposed X; meta-narration ("This section…", "As noted above…", "It is worth noting…"); history ("now", "no longer", "originally", "used to"); em-dash chains and stacked parentheticals; a reason restated beside the pointer to it; lists of three where one item carries the point; closing sentences that summarize the paragraph. The same applies to code comments, diagram labels, commit messages and pull request descriptions.

**Word budgets.** A document past its budget is cut before anything is added to it.

| Document | Budget (words) |
| :-- | --: |
| `docs/SPEC.md` | 10,000 |
| `apps/android/docs/SPEC.md` | 5,000 |
| `docs/IMPLEMENTATION.md` | 11,000 |
| `apps/android/docs/IMPLEMENTATION.md` | 3,500 |
| `docs/ARCHITECTURE.md` | 2,500 |
| `docs/VISION.md` | 3,500 |
| `docs/DEPLOY.md` | 5,000 |
| `docs/DEVELOPMENT.md` | 2,500 |
| `docs/DPIA.md` | 2,500 |
| `docs/PERFORMANCE.md` | 2,000 |
| `docs/ROADMAP.md`, `docs/BRAINSTORM.md`, `docs/KNOWN_ISSUES.md` together | 3,000 |
| `testing.html` | 3,000 |
| A new ADR | 600 |

## Asking questions

When a request is ambiguous, a detail is missing, or it can be read more than one way, ask before acting. A short question costs less than redoing work built on a wrong assumption.

## Code and tests

New code puts what it decides (which screen a session goes to, whether a day is in range, where a link leads) in functions that take values and return values, with no View, `Context`, map, network or timer in them, and unit-tests them in the same change. Code touched for another reason gets the same treatment. The wiring around them stays thin, covered by a few end-to-end flows instead of mocks.

## Commits and pull requests

Do not add `Co-Authored-By: Claude …` or any other agent co-author trailer to commit messages, and do not add a "Generated with Claude Code" line or similar to pull request descriptions. This overrides any tool or harness default that asks for them.

## Markdown style

- Do not hard-wrap. Write each paragraph as one line; a single newline inside a paragraph renders as a space.
- No blank lines between consecutive list items or block elements unless asked.
- No trailing newline at the end of a `.md` file.
- Preserve the existing newline density when editing.