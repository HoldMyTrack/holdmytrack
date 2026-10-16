// Covers docs/SPEC.md FR-4.10 (map image export).
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  choiceValue,
  customOutputSize,
  customSizeAvailable,
  fitWindow,
  MIN_WINDOW_PX,
  outputSize,
  parseChoice,
  resizeWindow,
  settleWindow,
  usableCustomSize,
  viewfinderArea,
  windowMoved,
} from '../src/map/exportViewfinder.ts';

const area = viewfinderArea(1032, 708); // { x: 16, y: 64, w: 1000, h: 600 }

test('the area keeps room for the toolbar above and the size below', () => {
  assert.deepEqual(area, { x: 16, y: 64, w: 1000, h: 600 });
});

test('a window fits the area in its shape, centred', () => {
  assert.deepEqual(fitWindow(1, area), { x: 216, y: 64, w: 600, h: 600 });
  assert.deepEqual(fitWindow(4, area), { x: 16, y: 239, w: 1000, h: 250 });
  assert.deepEqual(fitWindow(null, area), area);
});

test('dragging a corner keeps the opposite one where it was', () => {
  const start = fitWindow(null, area);
  assert.deepEqual(resizeWindow(start, 'se', -200, -100, null, area), { x: 16, y: 64, w: 800, h: 500 });
  assert.deepEqual(resizeWindow(start, 'nw', 300, 100, null, area), { x: 316, y: 164, w: 700, h: 500 });
});

test('a corner never crops past the area or below the smallest window', () => {
  const start = fitWindow(null, area);
  assert.deepEqual(resizeWindow(start, 'se', 500, 500, null, area), start);
  const tiny = resizeWindow(start, 'se', -5000, -5000, null, area);
  assert.equal(tiny.w, MIN_WINDOW_PX);
  assert.equal(tiny.h, MIN_WINDOW_PX);
});

test("a preset's shape holds while its corner is dragged", () => {
  const start = fitWindow(1, area);
  const r = resizeWindow(start, 'ne', -300, 0, 1, area);
  assert.equal(r.w, r.h);
  assert.equal(r.w, 300);
  assert.equal(r.x, start.x);
  assert.equal(r.y + r.h, start.y + start.h);
});

test('a crop settles back to the full window and zooms in so the crop fills it', () => {
  const crop = { x: 16, y: 64, w: 500, h: 300 };
  const { window, zoomDelta, focus } = settleWindow(crop, area);
  assert.deepEqual(window, area);
  assert.equal(zoomDelta, 1);
  assert.deepEqual(focus, { x: 266, y: 214 });
});

test('a crop of a new shape settles to the largest window of that shape', () => {
  const { window, zoomDelta } = settleWindow({ x: 100, y: 100, w: 300, h: 300 }, area);
  assert.deepEqual(window, { x: 216, y: 64, w: 600, h: 600 });
  assert.equal(zoomDelta, 1);
});

test('a drag that changed nothing leaves the map alone', () => {
  assert.equal(windowMoved(area, { ...area, w: area.w + 0.2 }), false);
  assert.equal(windowMoved(area, { ...area, w: area.w - 10 }), true);
});

test("a Custom image's long side is the size picked, its short side the window's shape", () => {
  assert.deepEqual(customOutputSize(16 / 9, 1920), { widthPx: 1920, heightPx: 1080 });
  assert.deepEqual(customOutputSize(9 / 16, 3840), { widthPx: 2160, heightPx: 3840 });
  assert.deepEqual(customOutputSize(1, 2560), { widthPx: 2560, heightPx: 2560 });
});

test("a size the device can't draw isn't offered, and falls back to the largest it can", () => {
  assert.equal(customSizeAvailable('4k', 4096), true);
  assert.equal(customSizeAvailable('4k', 2048), false);
  assert.equal(usableCustomSize('4k', 2600), '2k');
  assert.equal(usableCustomSize('2k', 1000), 'hd');
  assert.equal(usableCustomSize('hd', 4096), 'hd');
});

test("a preset makes its own size whatever the window's shape, Custom its size in the window's", () => {
  const preset = { id: 'x-landscape', group: 'X', row: 'Landscape', widthPx: 1280, heightPx: 720 };
  assert.deepEqual(outputSize(preset, '4k', { x: 0, y: 0, w: 300, h: 300 }), { widthPx: 1280, heightPx: 720 });
  assert.deepEqual(outputSize('custom', '2k', { x: 0, y: 0, w: 400, h: 300 }), { widthPx: 2560, heightPx: 1920 });
});

test('the one menu names a Custom size or a preset, and reads back the same choice', () => {
  assert.equal(choiceValue('custom', '2k'), 'custom-2k');
  const presets = [{ id: 'instagram-story', group: 'Instagram', row: 'Story', widthPx: 1080, heightPx: 1920 }];
  assert.deepEqual(parseChoice('custom-2k', presets), { preset: 'custom', customSize: '2k' });
  const story = parseChoice('instagram-story', presets);
  assert.equal(story.preset.id, 'instagram-story');
  assert.equal(choiceValue(story.preset, 'hd'), 'instagram-story');
  assert.equal(parseChoice('custom-8k', presets), null);
  assert.equal(parseChoice('custom', presets), null);
});
