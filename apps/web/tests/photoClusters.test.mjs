import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs, like editTrackOps.test.mjs: Node strips the types from the imported module itself.
import { clusterPoints } from '../src/map/photoClusters.ts';

const ids = (clusters) => clusters.map((c) => c.ids);

test('points apart stay apart; points within the radius group', () => {
  const clusters = clusterPoints([
    { id: 'a', x: 0, y: 0 },
    { id: 'b', x: 20, y: 0 },
    { id: 'c', x: 100, y: 0 },
    { id: 'd', x: 0, y: 25 },
  ], 32);
  assert.deepEqual(ids(clusters), [['a', 'b', 'd'], ['c']]);
});

test("a group's marker sits on its first point, in the order given", () => {
  const [group] = clusterPoints([
    { id: 'first', x: 10, y: 10 },
    { id: 'second', x: 30, y: 10 },
  ], 32);
  assert.deepEqual(group, { ids: ['first', 'second'], x: 10, y: 10 });
});

test('grouping is anchored, not chained: a run of close points splits at the radius', () => {
  const run = [0, 20, 40, 60, 80].map((x, i) => ({ id: String(i), x, y: 0 }));
  assert.deepEqual(ids(clusterPoints(run, 32)), [['0', '1'], ['2', '3'], ['4']]);
});

test('nothing in, nothing out', () => {
  assert.deepEqual(clusterPoints([]), []);
});
