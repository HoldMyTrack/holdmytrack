import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs, like editTrackOps.test.mjs: Node strips the types from the imported module itself.
import {
  activityType,
  MODES_OFF_BY_DEFAULT,
  parseLatLng,
  readTimeline,
  selectSegments,
  summarize,
  syncBatches,
  TimelineFormatError,
} from '../src/timeline/reader.ts';

// The shape of a real Android export, with made-up places: a visit, a drive, a walk, and the
// timelinePath buckets they draw their routes from (which don't line up with them).
const exportFile = {
  semanticSegments: [
    {
      startTime: '2026-07-07T08:00:00.000-04:00',
      endTime: '2026-07-07T09:00:00.000-04:00',
      visit: { topCandidate: { semanticType: 'HOME', placeLocation: { latLng: '10.0000000°, 20.0000000°' } } },
    },
    {
      startTime: '2026-07-07T09:00:00.000-04:00',
      endTime: '2026-07-07T09:20:00.000-04:00',
      activity: {
        start: { latLng: '10.0000000°, 20.0000000°' },
        end: { latLng: '10.1000000°, 20.1000000°' },
        distanceMeters: 16000,
        topCandidate: { type: 'IN_PASSENGER_VEHICLE', probability: 0.9 },
      },
    },
    {
      startTime: '2026-07-08T18:00:00.000-04:00',
      endTime: '2026-07-08T18:30:00.000-04:00',
      activity: {
        start: { latLng: '10.2000000°, 20.2000000°' },
        end: { latLng: '10.2010000°, 20.2010000°' },
        topCandidate: { type: 'WALKING' },
      },
    },
    {
      startTime: '2026-07-07T08:00:00.000-04:00',
      endTime: '2026-07-07T10:00:00.000-04:00',
      timelinePath: [
        { point: '10.0000000°, 20.0000000°', time: '2026-07-07T08:59:00.000-04:00' },
        { point: '10.0500000°, 20.0400000°', time: '2026-07-07T09:10:00.000-04:00' },
        { point: '10.0800000°, 20.0900000°', time: '2026-07-07T09:15:00.000-04:00' },
        { point: '10.1000000°, 20.1000000°', time: '2026-07-07T09:20:00.000-04:00' },
      ],
    },
  ],
  rawSignals: [{ position: { LatLng: '10.0°, 20.0°', timestamp: '2026-07-07T09:00:00.000-04:00' } }],
  userLocationProfile: { frequentPlaces: [] },
};

test('each activity becomes a segment routed through the path points inside it', () => {
  const { segments, skipped } = readTimeline(exportFile);
  assert.equal(skipped, 0);
  assert.deepEqual(segments.map((s) => [s.day, s.mode, s.type]), [
    ['2026-07-07', 'IN_PASSENGER_VEHICLE', 'driving'],
    ['2026-07-08', 'WALKING', 'walking'],
  ]);
  const drive = segments[0];
  // Its own start, the two path points strictly inside, its own end; the 08:59 point and the
  // one at exactly 09:20 are outside or duplicate the end.
  assert.deepEqual(drive.points.map((p) => p.time), [
    '2026-07-07T09:00:00.000-04:00',
    '2026-07-07T09:10:00.000-04:00',
    '2026-07-07T09:15:00.000-04:00',
    '2026-07-07T09:20:00.000-04:00',
  ]);
  assert.deepEqual(drive.points[1], { lat: 10.05, lon: 20.04, time: '2026-07-07T09:10:00.000-04:00' });
  assert.equal(drive.id, `seg-${Date.parse('2026-07-07T13:00:00Z') / 1000}-${Date.parse('2026-07-07T13:20:00Z') / 1000}`);
  assert.ok(drive.distanceM > 15000 && drive.distanceM < 17000, `distance ${drive.distanceM}`);
});

test('an activity that never leaves one place is skipped', () => {
  const still = structuredClone(exportFile);
  still.semanticSegments[2].activity.end.latLng = still.semanticSegments[2].activity.start.latLng;
  const { segments, skipped } = readTimeline(still);
  assert.equal(segments.length, 1);
  assert.equal(skipped, 1);
});

test('the other location exports are recognized and refused', () => {
  const ios = [{ startTime: '2026-07-07T09:00:00.000-04:00', activity: { start: 'geo:10,20' } }];
  assert.throws(() => readTimeline(ios), (e) => e instanceof TimelineFormatError && e.format === 'ios');
  assert.throws(() => readTimeline({ timelineObjects: [] }), (e) => e instanceof TimelineFormatError && e.format === 'takeout');
  assert.throws(() => readTimeline({ hello: 1 }), (e) => e instanceof TimelineFormatError && e.format === null);
});

test('selection is by local start day and mode', () => {
  const { segments } = readTimeline(exportFile);
  const all = new Set(['IN_PASSENGER_VEHICLE', 'WALKING']);
  assert.equal(selectSegments(segments, { from: '2026-07-07', to: '2026-07-08', modes: all }).length, 2);
  assert.equal(selectSegments(segments, { from: '2026-07-08', to: '2026-07-08', modes: all }).length, 1);
  assert.equal(selectSegments(segments, { from: '2026-07-01', to: '2026-07-31', modes: new Set(['WALKING']) }).length, 1);
  assert.ok(MODES_OFF_BY_DEFAULT.has('FLYING'));
});

test('summary and batches', () => {
  const { segments } = readTimeline(exportFile);
  assert.deepEqual(summarize(segments).map((m) => [m.mode, m.count]), [['IN_PASSENGER_VEHICLE', 1], ['WALKING', 1]]);
  const batches = syncBatches(segments, 1);
  assert.equal(batches.length, 2);
  assert.deepEqual(Object.keys(batches[0][0]).sort(), ['activity_type', 'external_id', 'points']);
});

test('modes and coordinates', () => {
  assert.equal(activityType('IN_BUS'), 'bus');
  assert.equal(activityType('FLYING'), 'flying');
  assert.equal(activityType('CYCLING'), 'cycling');
  assert.deepEqual(parseLatLng('geo:1.5,-2.25'), { lat: 1.5, lon: -2.25 });
  assert.equal(parseLatLng('91°, 0°'), null);
  assert.equal(parseLatLng(undefined), null);
});
