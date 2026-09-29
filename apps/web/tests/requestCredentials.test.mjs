import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs, like the other unit tests: Node strips the types from the imported .ts module.
import { withApiCredentials } from '../src/map/requestCredentials.ts';

test('a same-origin API base sends the cookie to the app only', () => {
  const transform = withApiCredentials('', 'https://holdmytrack.com/');
  assert.deepEqual(transform('https://holdmytrack.com/tiles/v1/tracks/10/279/382.mvt'), {
    url: 'https://holdmytrack.com/tiles/v1/tracks/10/279/382.mvt',
    credentials: 'include',
  });
  assert.deepEqual(transform('/v1/map/style/light'), { url: '/v1/map/style/light', credentials: 'include' });
  assert.deepEqual(transform('https://api.maptiler.com/tiles/satellite-v2/9/139/191.jpg?key=k'), {
    url: 'https://api.maptiler.com/tiles/satellite-v2/9/139/191.jpg?key=k',
  });
  assert.deepEqual(transform('https://tiles.holdmytrack.com/20260922/basemap/sprites/v4/light.png'), {
    url: 'https://tiles.holdmytrack.com/20260922/basemap/sprites/v4/light.png',
  });
});

test('a separate API origin (dev) sends the cookie there only', () => {
  const transform = withApiCredentials('http://localhost:8080', 'http://localhost:5173/');
  assert.equal(transform('http://localhost:8080/tiles/v1/fog/8/69/95.png').credentials, 'include');
  assert.equal(transform('http://localhost:5173/basemap/sprites/v4/light.png').credentials, undefined);
});

test('a URL that is not a web address gets no cookie', () => {
  const transform = withApiCredentials('', 'https://holdmytrack.com/');
  assert.deepEqual(transform('pmtiles://https://tiles.holdmytrack.com/20260922/basemap.pmtiles'), {
    url: 'pmtiles://https://tiles.holdmytrack.com/20260922/basemap.pmtiles',
  });
});
