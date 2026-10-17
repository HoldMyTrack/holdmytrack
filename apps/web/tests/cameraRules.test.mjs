import { test } from 'node:test';
import assert from 'node:assert/strict';
import { activityQuery } from '../src/map/activityQuery.ts';
import { firstListFit, needsFallbackView } from '../src/map/cameraRules.ts';

const activity = (id, startedAt, { bbox = [0, 0, 1, 1], pending = false } = {}) => ({ id, startedAt, bbox, pending });
const older = activity('older', '2026-09-01T08:00:00Z');
const newer = activity('newer', '2026-09-03T08:00:00Z');

test('the first list fits the most recent drawn activity', () => {
  const pendingNewest = activity('pending', '2026-09-05T08:00:00Z', { pending: true });
  assert.deepEqual(firstListFit([older, newer, pendingNewest], null, false), [newer]);
});

test('inside a Story the first list fits all of it', () => {
  assert.deepEqual(firstListFit([older, newer], 's', false), [older, newer]);
});

test('a Story with nothing drawn waits; an empty range settles without a fly', () => {
  assert.equal(firstListFit([activity('x', '2026-09-01T00:00:00Z', { bbox: null })], 's', false), null);
  assert.deepEqual(firstListFit([], null, false), []);
});

test('a view in the URL keeps the camera', () => {
  assert.deepEqual(firstListFit([older, newer], null, true), []);
});

test('only an account with no history at all gets the fallback view', () => {
  assert.equal(needsFallbackView(false, 0, true, null), true);
  assert.equal(needsFallbackView(false, 0, true, '2026-01-01'), false);
  assert.equal(needsFallbackView(false, 0, false, null), false);
  assert.equal(needsFallbackView(true, 0, true, null), false);
  assert.equal(needsFallbackView(false, 2, true, null), false);
});

test('the query is the open Story or the range, never both', () => {
  assert.deepEqual(activityQuery('s', { from: '2026-09-01', to: '2026-09-02' }), { story: 's' });
  assert.deepEqual(activityQuery(null, { from: '2026-09-01', to: '2026-09-02' }), { from: '2026-09-01', to: '2026-09-02' });
  assert.deepEqual(activityQuery(null, null), {});
});
