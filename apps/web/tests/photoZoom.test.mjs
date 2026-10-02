import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs, like editTrackOps.test.mjs: Node strips the types from the imported module itself.
import { FIT_VIEW, MAX_SCALE, clampView, fitSize, panBy, zoomAt } from '../src/ui/photoZoom.ts';

const stage = { width: 1000, height: 800 };

test('a picture fits the stage on its tighter side, and is never blown up past its size', () => {
  assert.deepEqual(fitSize({ width: 2048, height: 1536 }, stage), { width: 1000, height: 750 });
  assert.deepEqual(fitSize({ width: 1536, height: 2048 }, stage), { width: 600, height: 800 });
  assert.deepEqual(fitSize({ width: 400, height: 300 }, stage), { width: 400, height: 300 });
});

test('zooming about a point keeps that point of the picture under it', () => {
  const fitted = { width: 1000, height: 750 };
  const point = { x: 200, y: -100 };
  const view = zoomAt(FIT_VIEW, 2, point, fitted, stage);
  assert.equal(view.scale, 2);
  // The picture point under (200, -100) at scale 1 sits at (200, -100) still.
  assert.equal((point.x - view.x) / view.scale, point.x);
  assert.equal((point.y - view.y) / view.scale, point.y);
});

test('the scale stays between fitted and MAX_SCALE', () => {
  const fitted = { width: 1000, height: 750 };
  assert.deepEqual(zoomAt(FIT_VIEW, 0.5, { x: 0, y: 0 }, fitted, stage), FIT_VIEW);
  assert.equal(zoomAt(FIT_VIEW, 100, { x: 0, y: 0 }, fitted, stage).scale, MAX_SCALE);
});

test("panning stops at the picture's edges, and a side narrower than the stage stays centered", () => {
  const fitted = { width: 600, height: 800 };
  // At 1.5x: 900 wide in a 1000-wide stage (no room to pan), 1200 tall in 800 (200 each way).
  const view = panBy({ scale: 1.5, x: 0, y: 0 }, 500, -500, fitted, stage);
  assert.deepEqual(view, { scale: 1.5, x: 0, y: -200 });
});

test('zooming out pulls a panned picture back in', () => {
  const fitted = { width: 1000, height: 750 };
  const zoomed = { scale: 4, x: 1500, y: 0 };
  assert.deepEqual(clampView(zoomed, fitted, stage), zoomed);
  assert.deepEqual(zoomAt(zoomed, 0.5, { x: 0, y: 0 }, fitted, stage), { scale: 2, x: 500, y: 0 });
  assert.deepEqual(zoomAt(zoomed, 0.25, { x: 0, y: 0 }, fitted, stage), FIT_VIEW);
});
