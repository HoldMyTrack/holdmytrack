import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs so tsc leaves it alone; Node strips the types from the imported .ts module itself.
import { coveragePollDelay, FAST_POLL_MS, FAST_POLLS, SLOW_POLL_MS } from '../src/map/coveragePoll.ts';

test('reads every 2 seconds for the first ~3 minutes', () => {
  assert.equal(coveragePollDelay(1), FAST_POLL_MS);
  assert.equal(coveragePollDelay(FAST_POLLS - 1), FAST_POLL_MS);
});

test('then slows down instead of giving up', () => {
  assert.equal(coveragePollDelay(FAST_POLLS), SLOW_POLL_MS);
  assert.equal(coveragePollDelay(10_000), SLOW_POLL_MS);
});
