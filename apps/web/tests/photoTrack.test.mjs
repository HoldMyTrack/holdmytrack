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

test('a waiting photo starts just after the one picked before it', async () => {
  const { startFraction, NEXT_PHOTO_STEP } = await import('../src/ui/photoTrack.ts');
  const none = new Map();
  // First of its batch: the start of the route.
  assert.equal(startFraction(track, null, none, none), 0);
  // After a photo the server placed at a quarter of the way.
  assert.ok(Math.abs(startFraction(track, { t: T0 + 50 }, none, none) - (0.25 + NEXT_PHOTO_STEP)) < 1e-9);
  // After one that waited and was put at 0.6.
  assert.ok(Math.abs(startFraction(track, { waiting: 'a' }, new Map([['a', 0.6]]), new Map([['a', null]])) - (0.6 + NEXT_PHOTO_STEP)) < 1e-9);
  // After one that was skipped: past it, to what came before it.
  const anchors = new Map([['a', { t: T0 + 450 }], ['b', { waiting: 'a' }]]);
  assert.ok(Math.abs(startFraction(track, { waiting: 'b' }, new Map([['a', null], ['b', null]]), anchors) - (0.75 + NEXT_PHOTO_STEP)) < 1e-9);
  // Never past the end.
  assert.equal(startFraction(track, { t: T0 + 500 }, none, none), 1);
});
