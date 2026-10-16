import { test } from 'node:test';
import assert from 'node:assert/strict';
import { readdirSync, readFileSync } from 'node:fs';
// Plain .mjs so tsc leaves it alone; Node strips the types from the imported .ts modules itself.
import { defaultRange } from '../src/map/defaultRange.ts';
import { pendingChange } from '../src/map/pending.ts';
import { photoGroupAction, photoScopeFor } from '../src/map/photoMarkerItems.ts';
import { toggledVisibility, toolbarTarget } from '../src/map/selection.ts';

// The cases every client runs (testdata/map-session/README.md). Mounted at /testdata in the
// web container, which this path also reaches.
const dir = new URL('../../../testdata/map-session/', import.meta.url);
const sorted = (set) => [...set].sort();

const rules = {
  defaultRange: ({ visibleDays, earliest, today }) => defaultRange(visibleDays.map((date) => ({ date })), earliest ?? null, today),
  pendingChange: ({ previous, now, awaiting }) => {
    const change = pendingChange(new Set(previous), now, new Set(awaiting));
    return { started: change.started, finished: change.finished, left: sorted(change.left), landed: sorted(change.landed), awaiting: sorted(change.awaiting) };
  },
  toolbarTarget: ({ checked, focused }) => sorted(toolbarTarget(new Set(checked), focused ?? null)),
  targetVisibility: ({ hidden, target }) => sorted(toggledVisibility(new Set(hidden), new Set(target))),
  photoScope: ({ mode, editWindowIds, focused, storyId }) => photoScopeFor(mode, editWindowIds ?? null, focused ?? null, storyId ?? null),
  photoGroup: ({ photos }) => photoGroupAction(photos),
};

const files = readdirSync(dir).filter((name) => name.endsWith('.json'));

test('every shared rule file has a web implementation', () => {
  for (const name of files) {
    const { rule } = JSON.parse(readFileSync(new URL(name, dir), 'utf8'));
    assert.ok(rules[rule], `${name}: no web implementation of ${rule}`);
  }
});

for (const name of files) {
  const { rule, cases } = JSON.parse(readFileSync(new URL(name, dir), 'utf8'));
  for (const { name: caseName, input, expected } of cases) {
    test(`${rule}: ${caseName}`, () => {
      assert.deepEqual(rules[rule](input), expected);
    });
  }
}
