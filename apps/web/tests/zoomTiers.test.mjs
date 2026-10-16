// Covers docs/SPEC.md FR-4.2 and FR-4.3 (the zoom levels the map names).
import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs, like bbox.test.mjs: Node strips the types from the imported .ts module itself.
import { levelNoticeKey, zoomTier } from '../src/map/zoomTiers.ts';

test('Fog names the tier in view', () => {
  assert.equal(levelNoticeKey('fog', zoomTier(1)), 'map.level.fog.country');
  assert.equal(levelNoticeKey('fog', zoomTier(5)), 'map.level.fog.region');
  assert.equal(levelNoticeKey('fog', zoomTier(12)), 'map.level.fog.city');
});

test('Heatmap has one message at every zoom, so crossing a tier does not bring it back', () => {
  const keys = new Set([1, 5, 12].map((z) => levelNoticeKey('heatmap', zoomTier(z))));
  assert.deepEqual([...keys], ['map.level.heatmap']);
});
