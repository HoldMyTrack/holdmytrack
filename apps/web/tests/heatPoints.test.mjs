import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs, like bbox.test.mjs: Node strips the types from the imported .ts module itself.
import { HEAT_POINTS_MAX_ZOOM, heatPointsRadius } from '../src/map/heatPoints.ts';

// The expression's stops as [zoom, radius] pairs.
const stops = () => {
  const e = heatPointsRadius();
  const out = [];
  for (let i = 3; i < e.length; i += 2) out.push([e[i], e[i + 1]]);
  return out;
};

test("the radius doubles across each zoom and drops back where the next zoom's cells take over", () => {
  const s = stops();
  for (let i = 1; i < s.length; i++) assert.ok(s[i][0] > s[i - 1][0], 'zooms ascend');
  const at = (z) => s.find(([sz]) => sz === z)[1];
  for (let z = 0; z < HEAT_POINTS_MAX_ZOOM; z++) {
    assert.equal(s.find(([sz]) => Math.abs(sz - (z + 0.999)) < 1e-9)[1], 2 * at(z), `zoom ${z}`);
    assert.equal(at(z), at(0), `zoom ${z} starts where zoom 0 does`);
  }
});

test('past the last zoom with finer cells, the radius keeps growing with the enlarged ones', () => {
  const s = stops();
  const last = s[s.length - 1];
  const prev = s[s.length - 2];
  assert.equal(prev[0], HEAT_POINTS_MAX_ZOOM);
  assert.equal(last[1] / prev[1], 2 ** (last[0] - prev[0]));
});
