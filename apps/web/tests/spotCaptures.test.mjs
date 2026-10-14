import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs, like the other unit tests: Node strips the types from the imported .ts module.
import { capturesSince, mergeSpotCaptures } from '../src/map/spotCaptures.ts';

test('since is the newest capture the map has, as the server wrote it', () => {
  assert.equal(capturesSince([]), undefined);
  assert.equal(
    capturesSince([
      { spot_id: 1, captured_at: '2026-10-14T09:00:00Z' },
      { spot_id: 2, captured_at: '2026-10-14T09:00:00.123456Z' },
      { spot_id: 3, captured_at: '2026-10-13T23:59:59.9Z' },
    ]),
    '2026-10-14T09:00:00.123456Z',
  );
});

test('a merge keeps one per place, newest first', () => {
  const known = [
    { spot_id: 2, captured_at: '2026-10-14T09:00:00Z' },
    { spot_id: 1, captured_at: '2026-10-13T09:00:00Z' },
  ];
  const fresh = [
    { spot_id: 3, captured_at: '2026-10-14T10:00:00Z' },
    { spot_id: 2, captured_at: '2026-10-14T09:00:00Z' },
  ];
  assert.deepEqual(mergeSpotCaptures(known, fresh).map((c) => c.spot_id), [3, 2, 1]);
  assert.deepEqual(mergeSpotCaptures(known, []), known);
});
