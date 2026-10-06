import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs, like editTrackOps.test.mjs: Node strips the types from the imported .ts module.
import { distanceBounds, nextDistanceFilter, passesDistance } from '../src/ui/activityFacets.ts';

// Real distances carry centimeters, so the span isn't a whole number of meters.
const bounds = { min: 1200.35, max: 16093.44 };

test('a knob moved in narrows the filter', () => {
  assert.deepEqual(nextDistanceFilter(bounds, bounds, 'max', 12000), { min: 1200.35, max: 12000 });
  assert.deepEqual(nextDistanceFilter(bounds, bounds, 'min', 5000), { min: 5000, max: 16093.44 });
});

test('both knobs back at the bounds is no filter at all, so the longest activity comes back', () => {
  const narrowed = nextDistanceFilter(bounds, bounds, 'max', 15000);
  const back = nextDistanceFilter(bounds, narrowed, 'max', bounds.max);
  assert.equal(back, null);
  assert.equal(passesDistance({ distanceMeters: 16093.44 }, back), true);
});

test('one knob back at its end keeps the other one filtering', () => {
  const narrowed = { min: 5000, max: 15000 };
  assert.deepEqual(nextDistanceFilter(bounds, narrowed, 'max', bounds.max), { min: 5000, max: 16093.44 });
});

test('the knobs never cross', () => {
  const narrowed = { min: 5000, max: 8000 };
  assert.deepEqual(nextDistanceFilter(bounds, narrowed, 'max', 3000), { min: 5000, max: 5000 });
  assert.deepEqual(nextDistanceFilter(bounds, narrowed, 'min', 9000), { min: 8000, max: 8000 });
});

test('a private activity has no distance to bound or filter by', () => {
  const walk = { distanceMeters: 967.55, private: false };
  const home = { distanceMeters: 0, private: true };
  assert.deepEqual(distanceBounds([walk, home]), { min: 967.55, max: 967.55 });
  assert.equal(distanceBounds([home]), null);
  assert.equal(passesDistance(home, { min: 0, max: 5000 }), false);
  assert.equal(passesDistance(home, null), true);
});
