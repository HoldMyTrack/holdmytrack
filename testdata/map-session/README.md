# Shared map rule cases

Test cases for the map rules that every client implements the same way. The web (`apps/web/tests/mapRules.test.mjs`) and Android (`MapRulesTest.kt`) suites both run every file here, so a rule cannot change on one platform without the other failing; the iOS suite will read them too.

Each file holds one rule:

```json
{ "rule": "defaultRange", "description": "…", "cases": [{ "name": "…", "input": { … }, "expected": … }] }
```

The format is platform-neutral: dates are ISO strings, sets are arrays (compared sorted), ids are strings, field names are camelCase, and a missing field means the same as `null`.

| Rule | Input | Expected |
| :-- | :-- | :-- |
| `defaultRange` | `visibleDays` (ascending dates), `earliest`, `today` | `{ from, to }` |
| `pendingChange` | `previous`, `now`, `awaiting` (id sets) | `{ started, finished, left, landed, awaiting }` |
| `toolbarTarget` | `checked` (ids), `focused` | ids |
| `targetVisibility` | `hidden`, `target` (ids) | hidden ids |
| `photoScope` | `mode` (`normal`, `fog`, `heatmap`), `editWindowIds`, `focused`, `storyId` | `{ activity }`, `{ story }` or `null` |
| `photoGroup` | `photos` (`{ lat, lon }`) | `"open"` or `{ fit: [west, south, east, north] }` |

A rule only one platform has, or one where the platforms differ on purpose, is tested on that platform alone.