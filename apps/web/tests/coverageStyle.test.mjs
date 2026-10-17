// Covers docs/SPEC.md FR-4.2, FR-4.3 and FR-4.4: Fog and Heatmap as the served style carries them.
import { test } from 'node:test';
import assert from 'node:assert/strict';
// Plain .mjs, like bbox.test.mjs: Node strips the types from the imported .ts module itself.
import { buildOverlays, heatPointsRadius, OVERLAYS_METADATA, readOverlays, viewFor, zoomTier } from '../src/map/coverageStyle.ts';

const labels = ['places_locality', 'roads_labels_minor'];
const overlays = buildOverlays(false, labels);
const MODES = ['normal', 'fog', 'heatmap'];

// Every (layer, property) a view sets, as `kind:layer:property`.
const keys = (view) =>
  ['layout', 'paint'].flatMap((kind) => Object.entries(view[kind]).flatMap(([id, props]) => Object.keys(props).map((p) => `${kind}:${id}:${p}`))).sort();

test('every view sets the same properties, so switching never has to undo one', () => {
  const first = keys(overlays.views.plain.normal);
  for (const base of ['plain', 'satellite']) for (const mode of MODES) assert.deepEqual(keys(overlays.views[base][mode]), first);
});

test('each mode shows only its own layers', () => {
  const visible = (mode) =>
    Object.entries(viewFor(overlays, mode, false).layout)
      .filter(([, props]) => props.visibility === 'visible')
      .map(([id]) => id)
      .sort();
  assert.deepEqual(visible('normal'), []);
  assert.deepEqual(visible('fog'), ['country-fog-fill', 'fog-edge', 'fog-veil', 'region-fog-fill']);
  assert.deepEqual(visible('heatmap'), ['heat-points-circles', 'heatmap-dim']);
});

test('Fog mutes the basemap labels and every other mode restores them', () => {
  for (const mode of MODES) {
    const opacity = mode === 'fog' ? 0.4 : 1;
    for (const id of labels) assert.deepEqual(viewFor(overlays, mode, false).paint[id], { 'text-opacity': opacity, 'icon-opacity': opacity });
  }
});

test('imagery takes the dark veil over a light flavor', () => {
  assert.equal(viewFor(overlays, 'fog', false).paint['fog-veil']['fill-color'], '#202b25');
  assert.equal(viewFor(overlays, 'fog', true).paint['fog-veil']['fill-color'], '#f7f4ec');
  assert.equal(viewFor(buildOverlays(true, labels), 'fog', false).paint['fog-veil']['fill-color'], '#f7f4ec');
});

test('a layer is added looking as the plain Normal view leaves it', () => {
  const veil = overlays.layers.find((l) => l.id === 'fog-veil');
  assert.equal(veil.layout.visibility, 'none');
  assert.equal(veil.paint['fill-color'], '#202b25');
  assert.equal(overlays.layers[0].id, 'heatmap-dim');
});

test('every layer draws from a source the overlays carry', () => {
  for (const layer of overlays.layers) if (layer.source) assert.ok(overlays.sources[layer.source], layer.id);
});

test('the tiers switch where the Country and Region fills hand over', () => {
  assert.equal(zoomTier(overlays.tiers, 2.99), 'country');
  assert.equal(zoomTier(overlays.tiers, 3), 'region');
  assert.equal(zoomTier(overlays.tiers, 6.99), 'region');
  assert.equal(zoomTier(overlays.tiers, 7), 'city');
  const region = overlays.layers.find((l) => l.id === 'region-fog-fill');
  const veil = overlays.layers.find((l) => l.id === 'fog-veil');
  assert.equal(region.maxzoom, veil.minzoom);
  assert.equal(zoomTier([], 3), 'city');
});

test('the overlays are read back from a style metadata, and absent ones are null', () => {
  assert.equal(readOverlays({ [OVERLAYS_METADATA]: overlays }), overlays);
  assert.equal(readOverlays(undefined), null);
  assert.equal(readOverlays({}), null);
});

// The expression's stops as [zoom, radius] pairs.
const stops = () => {
  const e = heatPointsRadius();
  const out = [];
  for (let i = 3; i < e.length; i += 2) out.push([e[i], e[i + 1]]);
  return out;
};

test('the heat radius doubles across each zoom and drops back at the next', () => {
  const all = stops();
  const max = overlays.sources['heat-points'].maxzoom;
  for (let i = 1; i < all.length; i++) assert.ok(all[i][0] > all[i - 1][0], 'zooms ascend');
  const r = all[0][1];
  for (let z = 0; z < max; z++) {
    assert.equal(all.find(([zoom]) => zoom === z)[1], r);
    assert.equal(all.find(([zoom]) => zoom === z + 0.999)[1], 2 * r);
  }
  const [prev, last] = all.slice(-2);
  assert.equal(prev[0], max);
  assert.equal(last[1] / prev[1], 2 ** (last[0] - prev[0]));
});
