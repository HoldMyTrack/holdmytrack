import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs so tsc leaves it alone; Node strips the types from the imported .ts module itself.
import { shiftRange } from '../src/ui/rangeShift.ts';

const days = ['2026-09-01', '2026-09-03', '2026-09-06', '2026-09-07', '2026-09-08', '2026-09-09'];
const r = (from, to) => ({ from, to });

test('left moves the same number of activity-days, ending just before the old start', () => {
  assert.deepEqual(shiftRange(days, false, r('2026-09-08', '2026-09-09'), -1), r('2026-09-06', '2026-09-07'));
  // Counted in activity-days, not calendar days: the two days before Sep 6–7 are Sep 1 and 3.
  assert.deepEqual(shiftRange(days, false, r('2026-09-06', '2026-09-07'), -1), r('2026-09-01', '2026-09-03'));
});

test('right moves the same number of activity-days, starting just after the old end', () => {
  assert.deepEqual(shiftRange(days, false, r('2026-09-01', '2026-09-03'), 1), r('2026-09-06', '2026-09-07'));
});

test('near an end of the history the range keeps its size and stops at that end', () => {
  assert.deepEqual(shiftRange(days, false, r('2026-09-03', '2026-09-06'), -1), r('2026-09-01', '2026-09-03'));
  assert.deepEqual(shiftRange(days, false, r('2026-09-06', '2026-09-08'), 1), r('2026-09-07', '2026-09-09'));
});

test('nothing further that way', () => {
  assert.equal(shiftRange(days, false, r('2026-09-01', '2026-09-03'), -1), null);
  // The default range runs to today, past the newest activity-day.
  assert.equal(shiftRange(days, false, r('2026-09-07', '2026-10-03'), 1), null);
  assert.equal(shiftRange([], false, r('2026-09-07', '2026-09-08'), -1), null);
});

test('asks for more history when the days it needs are not loaded', () => {
  assert.equal(shiftRange(days, true, r('2026-09-01', '2026-09-03'), -1), 'load');
  assert.equal(shiftRange(days, true, r('2026-09-03', '2026-09-07'), -1), 'load');
  assert.equal(shiftRange(days, true, r('2026-08-20', '2026-09-03'), 1), 'load');
  assert.deepEqual(shiftRange(days, true, r('2026-09-08', '2026-09-09'), -1), r('2026-09-06', '2026-09-07'));
});

test('a range with no activity-days in it moves one day', () => {
  assert.deepEqual(shiftRange(days, false, r('2026-09-04', '2026-09-05'), -1), r('2026-09-03', '2026-09-03'));
  assert.deepEqual(shiftRange(days, false, r('2026-09-04', '2026-09-05'), 1), r('2026-09-06', '2026-09-06'));
});
