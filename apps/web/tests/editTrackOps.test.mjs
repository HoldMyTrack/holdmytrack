import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs so tsc (which has no Node types here) leaves it alone; Node strips the types
// from the imported .ts module itself. Run with `npm run test:unit`.
import { applyEdit, chopOp, cutOp, foldEdit, isEmptyEdit, moveOp, splitIndex, splitPoint } from '../src/ui/editTrackOps.ts';

// Ten points one second apart, walking north; each point's index is recoverable from its time.
const T0 = Date.UTC(2026, 8, 1, 8);
const points = Array.from({ length: 10 }, (_, i) => [13, 52 + i * 0.0001, T0 + i * 1000]);
const indices = (pts) => pts.map(([, , t]) => (t - T0) / 1000);
const visibleAfter = (ops) => applyEdit(points, foldEdit(null, ops));

test('chop keeps the knob range, inclusive', () => {
  const op = chopOp(points, 2, 7);
  assert.deepEqual(indices(visibleAfter([op])), [2, 3, 4, 5, 6, 7]);
});

test('cut removes between the knobs and keeps both knob points', () => {
  const op = cutOp(points, 3, 7);
  assert.deepEqual(indices(visibleAfter([op])), [0, 1, 2, 3, 7, 8, 9]);
});

test('knobs with nothing to act on commit no op', () => {
  assert.equal(chopOp(points, 0, 9), null);
  assert.equal(cutOp(points, 4, 5), null);
});

test('undo walks the chain back one step at a time', () => {
  // Chop -> Cut -> delete two points, each op computed from what was visible at the time.
  const ops = [];
  const snapshots = [indices(visibleAfter(ops))];
  ops.push(chopOp(visibleAfter(ops), 1, 8));
  snapshots.push(indices(visibleAfter(ops)));
  let visible = visibleAfter(ops);
  ops.push(cutOp(visible, 2, 5));
  snapshots.push(indices(visibleAfter(ops)));
  visible = visibleAfter(ops);
  ops.push({ kind: 'drop', t: visible[0][2] });
  snapshots.push(indices(visibleAfter(ops)));
  visible = visibleAfter(ops);
  ops.push({ kind: 'drop', t: visible[visible.length - 1][2] });
  snapshots.push(indices(visibleAfter(ops)));

  assert.deepEqual(snapshots.at(-1), [2, 3, 6, 7]);
  // Popping one op at a time lands exactly on the state before it, all the way back.
  for (let i = snapshots.length - 1; i > 0; i--) {
    ops.pop();
    assert.deepEqual(indices(visibleAfter(ops)), snapshots[i - 1]);
  }
  assert.deepEqual(indices(visibleAfter(ops)), [0, 1, 2, 3, 4, 5, 6, 7, 8, 9]);
});

test('fold starts from the saved edit and a reset discards it', () => {
  const base = { keep: [T0 + 2000, T0 + 9000] };
  assert.deepEqual(indices(applyEdit(points, foldEdit(base, []))), [2, 3, 4, 5, 6, 7, 8, 9]);
  const ops = [{ kind: 'drop', t: T0 + 5000 }, { kind: 'reset' }];
  assert.ok(isEmptyEdit(foldEdit(base, ops)));
  ops.pop(); // undoing the reset brings the saved edit back
  assert.deepEqual(indices(applyEdit(points, foldEdit(base, ops))), [2, 3, 4, 6, 7, 8, 9]);
});

test('a second chop narrows the first rather than replacing it', () => {
  const edit = foldEdit(null, [
    { kind: 'chop', keep: [T0 + 1000, T0 + 8000] },
    { kind: 'chop', keep: [T0 + 3000, T0 + 9000] },
  ]);
  assert.deepEqual(edit.keep, [T0 + 3000, T0 + 8000]);
});

test('the fold never carries empty arrays', () => {
  assert.deepEqual(foldEdit(null, []), {});
  assert.deepEqual(foldEdit(null, [{ kind: 'cut', remove: [1, 2] }]), { remove: [[1, 2]] });
});

test('a move puts the point elsewhere and keeps its time', () => {
  const edit = foldEdit(null, [moveOp(T0 + 3000, 13.5, 52.5)]);
  assert.deepEqual(edit, { move: { [String(T0 + 3000)]: [13.5, 52.5] } });
  const after = applyEdit(points, edit);
  assert.deepEqual(after[3], [13.5, 52.5, T0 + 3000]);
  assert.deepEqual(indices(after), [0, 1, 2, 3, 4, 5, 6, 7, 8, 9]);
  assert.deepEqual(points[3], [13, 52 + 3 * 0.0001, T0 + 3000], 'the recorded points stay as they were');
});

test('a point moved twice ends where it was dropped last, and undo takes it back one drop', () => {
  const ops = [moveOp(T0 + 3000, 13.5, 52.5), moveOp(T0 + 3000, 13.6, 52.6)];
  assert.deepEqual(applyEdit(points, foldEdit(null, ops))[3], [13.6, 52.6, T0 + 3000]);
  ops.pop();
  assert.deepEqual(applyEdit(points, foldEdit(null, ops))[3], [13.5, 52.5, T0 + 3000]);
});

test('a moved point that is then deleted is gone, and a reset clears the saved moves', () => {
  const base = { move: { [String(T0 + 3000)]: [13.5, 52.5] } };
  assert.ok(!isEmptyEdit(base));
  assert.deepEqual(indices(applyEdit(points, foldEdit(base, [{ kind: 'drop', t: T0 + 3000 }]))), [0, 1, 2, 4, 5, 6, 7, 8, 9]);
  assert.ok(isEmptyEdit(foldEdit(base, [{ kind: 'reset' }])));
});

test('a move is rounded to the 1e-6 degrees points come in', () => {
  assert.deepEqual(moveOp(1, 13.12345678, 52.98765432).to, [13.123457, 52.987654]);
});

test('split goes at the one knob moved, two points each side', () => {
  assert.equal(splitIndex(points, 0, 9), null);
  assert.equal(splitIndex(points, 4, 9), 4);
  assert.equal(splitIndex(points, 0, 6), 6);
  assert.equal(splitIndex(points, 2, 6), null);
  assert.equal(splitIndex(points, 9, 9), null);
  assert.equal(splitIndex(points, 0, 0), null);
});

test('a split rides beside the edit, not in it', () => {
  const ops = [chopOp(points, 1, 9), { kind: 'split', t: points[5][2] }];
  assert.deepEqual(foldEdit(null, ops), { keep: [points[1][2], points[9][2]] });
  assert.equal(splitPoint(ops), points[5][2]);
  assert.equal(splitPoint(ops.slice(0, 1)), null);
});
