import { test } from 'node:test';
import assert from 'node:assert/strict';
import { exportFlowReducer, shapeWindow } from '../src/map/exportFlow.ts';

const container = { width: 1280, height: 800 };
const square = { widthPx: 1080, heightPx: 1080 };
const framing = exportFlowReducer({ stage: 'idle' }, { type: 'toggle', customSize: 'hd', container });

test('the control opens framing on Custom over the whole area, and closes it', () => {
  assert.equal(framing.stage, 'framing');
  assert.equal(framing.preset, 'custom');
  assert.deepEqual(framing.window, shapeWindow('custom', container));
  assert.deepEqual(exportFlowReducer(framing, { type: 'toggle', customSize: 'hd', container }), { stage: 'idle' });
});

test('the control does nothing mid-capture', () => {
  const capturing = exportFlowReducer(framing, { type: 'capture' });
  assert.equal(capturing.stage, 'capturing');
  assert.equal(exportFlowReducer(capturing, { type: 'toggle', customSize: 'hd', container }), capturing);
});

test('another Custom size keeps the window; a preset fits its own shape', () => {
  const moved = exportFlowReducer(framing, { type: 'window', window: { x: 10, y: 10, w: 100, h: 50 } });
  const resized = exportFlowReducer(moved, { type: 'choice', choice: { preset: 'custom', customSize: '4k' }, container });
  assert.deepEqual(resized.window, moved.window);
  assert.equal(resized.customSize, '4k');
  const squared = exportFlowReducer(moved, { type: 'choice', choice: { preset: square }, container });
  assert.equal(squared.preset, square);
  assert.equal(squared.window.w, squared.window.h);
});

test('a failed capture returns to framing with the same window', () => {
  const capturing = exportFlowReducer(framing, { type: 'capture' });
  const failed = exportFlowReducer(capturing, { type: 'failed' });
  assert.deepEqual(failed, framing);
});

test('a window change outside framing is ignored', () => {
  assert.deepEqual(exportFlowReducer({ stage: 'idle' }, { type: 'window', window: { x: 0, y: 0, w: 1, h: 1 } }), { stage: 'idle' });
});
