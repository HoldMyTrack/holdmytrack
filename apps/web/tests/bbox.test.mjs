import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs, like rangeShift.test.mjs: Node strips the types from the imported .ts module itself.
import { unionBBox } from '../src/map/bbox.ts';

const round = (b) => b && b.map((v) => Math.round(v * 1000) / 1000);

test('boxes on one side of the antimeridian union as they always did', () => {
  assert.equal(unionBBox([]), null);
  assert.deepEqual(unionBBox([[-81.7, 41.4, -81.6, 41.5], [-1, 51, 1, 51.5]]), [-81.7, 41.4, 1, 51.5]);
});

test('boxes either side of it go the short way across the Pacific, east past 180', () => {
  // New Zealand and Hawaii.
  assert.deepEqual(round(unionBBox([[174.7, -41.3, 174.8, -41.2], [-157.9, 21.2, -157.8, 21.3]])), [174.7, -41.3, 202.2, 21.3]);
});

test('a crossing box from the server (east past 180) joins a neighbour on either side', () => {
  assert.deepEqual(round(unionBBox([[179.9, -17, 180.1, -16.9], [-178, -18, -177, -17.5]])), [179.9, -18, 183, -16.9]);
  assert.deepEqual(round(unionBBox([[179.9, -17, 180.1, -16.9], [178, -18, 179, -17.5]])), [178, -18, 180.1, -16.9]);
});

test('boxes all round the world give the whole world', () => {
  assert.deepEqual(unionBBox([[-180, 0, -60, 1], [-60, 0, 60, 1], [60, 0, 180, 1]]), [-180, 0, 180, 1]);
});
