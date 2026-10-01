import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs, like editTrackOps.test.mjs: Node strips the types from the imported module itself.
import { fractionAt, photoTrack, pointAt } from '../src/ui/photoTrack.ts';

// North along a meridian: 1 km in the first 100 s, then a 300 s stop, then 1 km more in 100 s.
const T0 = 1_780_000_000;
const deg = 1000 / 111_195; // ~1 km of latitude
const track = photoTrack([
  { lon: 10, lat: 50, t: T0 },
  { lon: 10, lat: 50 + deg, t: T0 + 100 },
  { lon: 10, lat: 50 + deg, t: T0 + 400 },
  { lon: 10, lat: 50 + 2 * deg, t: T0 + 500 },
]);

test('the slider runs by distance, so a stop takes up none of it', () => {
  assert.ok(Math.abs(pointAt(track, 0.25).lat - (50 + deg / 2)) < 1e-9);
  assert.equal(pointAt(track, 0.25).t, T0 + 50);
  assert.equal(pointAt(track, 0.75).t, T0 + 450);
  assert.deepEqual(pointAt(track, 0), track.points[0]);
  assert.deepEqual(pointAt(track, 1), track.points[3]);
});

test('a moment is found back where it is along the track', () => {
  assert.ok(Math.abs(fractionAt(track, T0 + 50) - 0.25) < 1e-9);
  assert.ok(Math.abs(fractionAt(track, T0 + 450) - 0.75) < 1e-9);
  // During the stop, at the stop.
  assert.ok(Math.abs(fractionAt(track, T0 + 250) - 0.5) < 1e-9);
  assert.equal(fractionAt(track, T0 - 60), 0);
  assert.equal(fractionAt(track, T0 + 9999), 1);
});

test('round trip: a place, its moment, and back', () => {
  for (const f of [0.1, 0.3, 0.6, 0.9]) {
    assert.ok(Math.abs(fractionAt(track, pointAt(track, f).t) - f) < 0.01, `fraction ${f}`);
  }
});

test('a track with no length goes by time', () => {
  const still = photoTrack([
    { lon: 10, lat: 50, t: T0 },
    { lon: 10, lat: 50, t: T0 + 100 },
  ]);
  assert.equal(pointAt(still, 0.5).t, T0 + 50);
  assert.equal(fractionAt(still, T0 + 25), 0.25);
});
