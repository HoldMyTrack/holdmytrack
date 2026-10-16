import { test } from 'node:test';
import assert from 'node:assert/strict';
import { GLOBE_MAX_ZOOM, globeProjection } from '../src/map/globe.ts';

test('the web map is a globe out to GLOBE_MAX_ZOOM and flat one zoom further in', () => {
  assert.equal(GLOBE_MAX_ZOOM, 3);
  assert.deepEqual(globeProjection().type, ['interpolate', ['linear'], ['zoom'], 3, 'vertical-perspective', 4, 'mercator']);
});
