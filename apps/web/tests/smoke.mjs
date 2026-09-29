/**
 * Headless verification of the basemap foundation.
 *
 * This runs the checks below against a live dev server rather than eyeballing the map
 * in a browser. It starts its own dev server on a fixed port so a run is self-contained
 * and cannot accidentally test a stale server on 5173.
 *
 *   npm run verify:map
 *
 * Plain .mjs on purpose: Node 22's TypeScript stripping is still flagged, and a
 * verification script that needs its own build step is one more thing to break.
 */
import { spawn } from 'node:child_process';
import { mkdir, writeFile } from 'node:fs/promises';
import { after, before, describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { chromium } from 'playwright';

const PORT = 5180;
const BASE = `http://localhost:${PORT}`;
const OHIO_BOUNDS = { minLon: -84.85, minLat: 38.35, maxLon: -80.5, maxLat: 42.35 };
const SHOTS = new URL('./screenshots/', import.meta.url);

// App.tsx sends a signed-out visit to the sign-in page (services/server/internal/httpapi/
// auth_pages.go) — a fresh browser context has no session cookie, so MapView (and window.__holdmytrack) would
// never mount without signing in first. A dedicated test account, not the real one: `signup`
// only *claims* the seeded placeholder user on the very first signup ever, so reusing this
// fixed email is safe to call every run — after the first, the email is taken and this just
// logs in instead.
const API_BASE = process.env.VITE_API_BASE_URL ?? 'http://localhost:8080';
const TEST_EMAIL = 'smoke-test@holdmytrack.local';
const TEST_PASSWORD = 'smoke-test-password';

async function ensureSignedIn(context) {
  const signup = await context.request.post(`${API_BASE}/v1/auth/signup`, {
    data: { email: TEST_EMAIL, password: TEST_PASSWORD },
  });
  if (signup.status() === 409) {
    const login = await context.request.post(`${API_BASE}/v1/auth/login`, {
      data: { email: TEST_EMAIL, password: TEST_PASSWORD },
    });
    if (!login.ok()) {
      throw new Error(`smoke test login failed: ${login.status()} ${await login.text()}`);
    }
  } else if (!signup.ok()) {
    throw new Error(`smoke test signup failed: ${signup.status()} ${await signup.text()}`);
  }
  // A fresh account opens on the first-run setup screen, not the map, until it has a Country
  // (App.tsx, FR-1.7). Saving the same settings on every run is a no-op after the first, and
  // makes a run against an empty database (CI's) reach the map like a long-used one does.
  const settings = await context.request.patch(`${API_BASE}/v1/account/settings`, {
    data: { display_name: '', country: 'US', timezone: 'America/New_York' },
  });
  if (!settings.ok()) {
    throw new Error(`smoke test settings failed: ${settings.status()} ${await settings.text()}`);
  }
}

let server;
let browser;
let page;
/** @type {{consoleErrors: string[], failed: string[], partial: number, fullPmtiles: number}} */
let net;

async function waitForServer(url, timeoutMs = 60_000) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    try {
      const res = await fetch(url, { method: 'GET' });
      if (res.ok) return;
    } catch {
      // not up yet
    }
    if (Date.now() > deadline) throw new Error(`dev server never became ready at ${url}`);
    await new Promise((r) => setTimeout(r, 250));
  }
}

/** Resolves once MapLibre reports the style fully loaded. */
async function styleLoaded() {
  const stillLoaded = () =>
    page.waitForFunction(() => Boolean(window.__holdmytrack) && window.__holdmytrack.isStyleLoaded(), undefined, {
      timeout: 45_000,
    });
  await stillLoaded();
  // A `styledata`-triggered overlay attach (the tracks source now, fog later) can add a
  // new source right after the base style first settles, which makes isStyleLoaded() dip
  // back to false for a moment while that source's own tiles load. Settle-check: wait a
  // beat, then confirm it's still true, so a caller here can't resolve inside that dip and
  // hand back a style that immediately un-loads again a few milliseconds later.
  await page.waitForTimeout(150);
  await stillLoaded();
}

const camera = () =>
  page.evaluate(() => {
    const m = window.__holdmytrack;
    const c = m.getCenter();
    return { lng: c.lng, lat: c.lat, zoom: m.getZoom() };
  });

before(async () => {
  await mkdir(SHOTS, { recursive: true });

  server = spawn('npx', ['vite', '--port', String(PORT), '--strictPort'], {
    cwd: new URL('..', import.meta.url).pathname,
    stdio: 'ignore',
  });
  await waitForServer(BASE);

  browser = await chromium.launch({ args: process.env.PW_CHROMIUM_ARGS?.split(/\s+/) ?? [] });
  const context = await browser.newContext({
    viewport: { width: 1280, height: 800 },
    deviceScaleFactor: 1,
  });
  await ensureSignedIn(context);
  page = await context.newPage();

  net = { consoleErrors: [], failed: [], partial: 0, fullPmtiles: 0 };

  page.on('console', (msg) => {
    if (msg.type() === 'error') net.consoleErrors.push(msg.text());
  });
  page.on('pageerror', (err) => net.consoleErrors.push(`pageerror: ${err.message}`));
  page.on('response', (res) => {
    const url = res.url();
    const status = res.status();
    if (status === 206) net.partial += 1;
    if (status === 200 && url.includes('.pmtiles')) net.fullPmtiles += 1;
    if (status >= 400) net.failed.push(`${status} ${url}`);
  });

  // Open at an explicit hash so step 5 has something deterministic to restore.
  await page.goto(`${BASE}/#map=12.00/39.96120/-82.99880&theme=light`);
  await styleLoaded();
});

after(async () => {
  await browser?.close();
  server?.kill('SIGTERM');
});

describe('basemap foundation', () => {
  it('1. archive reports the expected zoom range and bounds', async () => {
    const src = await page.evaluate(() => {
      const source = window.__holdmytrack.getSource('protomaps');
      return { bounds: source.bounds, minzoom: source.minzoom, maxzoom: source.maxzoom };
    });
    assert.equal(src.minzoom, 0, 'archive minzoom');
    assert.equal(src.maxzoom, 14, 'archive maxzoom — z14 per §1.2');
    assert.deepEqual(
      src.bounds.map((n) => Math.round(n * 100) / 100),
      [OHIO_BOUNDS.minLon, OHIO_BOUNDS.minLat, OHIO_BOUNDS.maxLon, OHIO_BOUNDS.maxLat],
      'TileJSON bounds come from the pmtiles header',
    );
  });

  it('2. style is loaded and carries the full basemap layer stack', async () => {
    const info = await page.evaluate(() => {
      const layers = window.__holdmytrack.getStyle().layers;
      return {
        loaded: window.__holdmytrack.isStyleLoaded(),
        count: layers.length,
        symbols: layers.filter((l) => l.type === 'symbol').length,
        firstSymbol: layers.find((l) => l.type === 'symbol')?.id,
        rendered: window.__holdmytrack.queryRenderedFeatures().length,
      };
    });
    assert.ok(info.loaded, 'isStyleLoaded()');
    assert.ok(info.count > 0, `layers.length > 0 (got ${info.count})`);
    assert.ok(info.symbols > 0, 'style has label layers');
    assert.ok(info.rendered > 0, `real geometry is rendered (got ${info.rendered} features)`);
  });

  it('3. tiles arrive as range requests, never one full-file GET', () => {
    // Threshold, not an exact count: how many range requests a fixed 1280x800 viewport
    // needs depends on how much of it is actually map canvas. The header + bottom
    // histogram chrome took the count from >10 to a stable 8 — verified by rerunning, not
    // assumed to be flaky — since there's simply less canvas to tile now. The point of this
    // assertion is "many, not one or two," not a specific number tied to a since-changed
    // layout.
    assert.ok(net.partial > 5, `expected many 206s, got ${net.partial}`);
    assert.equal(net.fullPmtiles, 0, 'a 200 on the archive means the protocol is not engaged');
  });

  it('4. no console errors and no 404s for glyphs or sprites', () => {
    assert.deepEqual(net.failed, [], 'failed requests');
    assert.deepEqual(net.consoleErrors, [], 'console errors');
  });

  it('5. hash restores the view, and panning rewrites it', async () => {
    const restored = await camera();
    assert.ok(Math.abs(restored.lat - 39.9612) < 1e-4, `lat restored (${restored.lat})`);
    assert.ok(Math.abs(restored.lng - -82.9988) < 1e-4, `lng restored (${restored.lng})`);
    assert.ok(Math.abs(restored.zoom - 12) < 1e-6, `zoom restored (${restored.zoom})`);

    await page.evaluate(() => window.__holdmytrack.jumpTo({ center: [-81.6944, 41.4993], zoom: 13 }));
    await page.waitForFunction(() => window.location.hash.includes('41.49'), undefined, {
      timeout: 10_000,
    });
    const hash = await page.evaluate(() => window.location.hash);
    assert.match(hash, /^#map=13\.00\/41\.499\d+\/-81\.694\d+$/, hash);
  });

  it('6. attribution credits both Protomaps and OpenStreetMap', async () => {
    const html = await page.locator('.maplibregl-ctrl-attrib-inner').innerHTML();
    assert.match(html, /protomaps\.com/, 'Protomaps link');
    assert.match(html, /openstreetmap\.org/, 'OpenStreetMap link');
  });

  it('7. renders a screenshot for visual diffing', async () => {
    await page.evaluate(() => window.__holdmytrack.jumpTo({ center: [-82.9988, 39.9612], zoom: 12 }));
    await styleLoaded();
    await page.waitForFunction(() => window.__holdmytrack.areTilesLoaded(), undefined, {
      timeout: 30_000,
    });
    const shot = new URL('columbus-light.png', SHOTS).pathname;
    await page.screenshot({ path: shot });
    await writeFile(
      new URL('report.json', SHOTS).pathname,
      JSON.stringify({ partialResponses: net.partial, at: new Date().toISOString() }, null, 2),
    );
    assert.ok(true);
  });

  it('8. Layers menu: trails, tracks and bike paths off by default, each shown on its own, remembered across reload', async () => {
    const TRAILS = ['paths_trail', 'paths_bridges_trail'];
    const TRACKS = ['paths_track', 'paths_bridges_track'];
    const BIKES = ['paths_cycleway', 'paths_bridges_cycleway'];
    const ALL = [...TRAILS, ...TRACKS, ...BIKES];
    const visibility = (ids) =>
      page.evaluate((ids) => ids.map((id) => window.__holdmytrack.getLayoutProperty(id, 'visibility')), ids);
    const menu = page.getByTestId('map-overlays');
    // Opens the menu if it's closed; the checkboxes live in its panel.
    const openMenu = async () => {
      const trigger = menu.getByRole('button').first();
      if ((await trigger.getAttribute('aria-expanded')) !== 'true') await trigger.click();
    };

    assert.deepEqual(await visibility(ALL), ALL.map(() => 'none'), 'hidden until turned on');

    // The Olentangy Trail, Columbus: cycleways and footpaths in the extract at z14.
    await page.evaluate(() => window.__holdmytrack.jumpTo({ center: [-83.02, 39.99], zoom: 14 }));
    await openMenu();
    await page.locator('#overlay-bike-paths').check();
    assert.deepEqual(await visibility(BIKES), BIKES.map(() => 'visible'), 'bike paths shown once ticked');
    assert.deepEqual(await visibility([...TRAILS, ...TRACKS]), [...TRAILS, ...TRACKS].map(() => 'none'), 'trails and tracks still hidden');
    await page.locator('#overlay-trails').check();
    assert.deepEqual(await visibility(TRAILS), TRAILS.map(() => 'visible'), 'trails shown once ticked');
    assert.deepEqual(await visibility(TRACKS), TRACKS.map(() => 'none'), 'tracks their own entry');
    await page.locator('#overlay-tracks').check();
    assert.deepEqual(await visibility(TRACKS), TRACKS.map(() => 'visible'), 'tracks shown once ticked');
    await styleLoaded();
    await page.waitForFunction(() => window.__holdmytrack.areTilesLoaded(), undefined, { timeout: 30_000 });
    const rendered = await page.evaluate(() => ({
      cycleway: window.__holdmytrack.queryRenderedFeatures({ layers: ['paths_cycleway'] }).length,
      trail: window.__holdmytrack.queryRenderedFeatures({ layers: ['paths_trail'] }).length,
    }));
    assert.ok(rendered.cycleway > 0, `cycleways drawn (${rendered.cycleway})`);
    assert.ok(rendered.trail > 0, `trails drawn (${rendered.trail})`);
    await page.screenshot({ path: new URL('columbus-paths-light.png', SHOTS).pathname });

    await page.reload();
    await styleLoaded();
    assert.deepEqual(await visibility(ALL), ALL.map(() => 'visible'), 'remembered across reload');
    await openMenu();
    await page.locator('#overlay-trails').uncheck();
    await page.locator('#overlay-tracks').uncheck();
    await page.locator('#overlay-bike-paths').uncheck();
    assert.deepEqual(await visibility(ALL), ALL.map(() => 'none'), 'hidden again once unticked');

    // Tracks explains itself behind an info button, without ticking the box.
    await page.locator('.overlays-menu__info').click();
    assert.ok(await page.locator('#overlay-tracks-info').isVisible(), 'tracks explanation shown');
    assert.equal(await page.locator('#overlay-tracks').isChecked(), false, 'info button leaves the box alone');
    assert.equal(await page.locator('#overlay-all, #overlay-spots-all').count(), 0, 'no All checkboxes');
    await page.screenshot({ path: new URL('layers-menu.png', SHOTS).pathname });
  });

  // Satellite mode (docs/SPEC.md FR-4.14) exists only when the dev server was started with
  // VITE_SATELLITE_TILES: without it the style has no imagery and the menu no Base map section,
  // which is what CI checks; with it, the switch itself.
  it('9. Base map: Satellite shows imagery under roads and labels, remembered across reload, or is absent unconfigured', async () => {
    const menu = page.getByTestId('map-overlays');
    const openMenu = async () => {
      const trigger = menu.getByRole('button').first();
      if ((await trigger.getAttribute('aria-expanded')) !== 'true') await trigger.click();
    };
    const hasImagery = await page.evaluate(() => Boolean(window.__holdmytrack.getLayer('satellite')));
    await openMenu();
    if (!hasImagery) {
      assert.equal(await page.locator('#overlay-basemap-satellite').count(), 0, 'no Base map section without imagery');
      return;
    }

    const visibility = (ids) =>
      page.evaluate((ids) => ids.map((id) => window.__holdmytrack.getLayoutProperty(id, 'visibility') ?? 'visible'), ids);
    const FILLS = ['background', 'earth', 'water', 'buildings'];
    // Drawn over the imagery in either mode: the hybrid.
    const KEPT = ['roads_major', 'places_locality'];

    // Imagery tiles fetched from the configured host once it's on — the raster has no features
    // to query, so the responses are the evidence it drew.
    const host = await page.evaluate(() => new URL(window.__holdmytrack.getStyle().sources.satellite.tiles[0]).host);
    let imageryTiles = 0;
    const countImagery = (res) => {
      if (res.status() === 200 && new URL(res.url()).host === host) imageryTiles += 1;
    };
    page.on('response', countImagery);

    assert.deepEqual(await visibility(['satellite']), ['none'], 'imagery off until chosen');
    await page.locator('#overlay-basemap-satellite').check();
    assert.deepEqual(await visibility(['satellite']), ['visible'], 'imagery on');
    assert.deepEqual(await visibility(FILLS), FILLS.map(() => 'none'), 'fills hidden over it');
    assert.deepEqual(await visibility(KEPT), KEPT.map(() => 'visible'), 'roads and labels kept');
    const roadOpacity = () =>
      page.evaluate(() => ['roads_major', 'roads_minor', 'roads_rail'].map((id) => window.__holdmytrack.getPaintProperty(id, 'line-opacity')));
    assert.deepEqual(await roadOpacity(), [0.4, 0.4, 0.5], 'roads see-through over the imagery, rail as it was');
    const order = await page.evaluate(() => window.__holdmytrack.getStyle().layers.map((l) => l.id));
    assert.ok(order.indexOf('satellite') < order.indexOf('roads_major'), 'imagery under the roads');
    await styleLoaded();
    await page.waitForFunction(() => window.__holdmytrack.areTilesLoaded(), undefined, { timeout: 30_000 });
    page.off('response', countImagery);
    assert.ok(imageryTiles > 0, `imagery tiles fetched (${imageryTiles})`);
    await page.screenshot({ path: new URL('columbus-satellite.png', SHOTS).pathname });

    await page.reload();
    await styleLoaded();
    assert.deepEqual(await visibility(['satellite']), ['visible'], 'remembered across reload');
    await openMenu();
    await page.locator('#overlay-basemap-map').check();
    assert.deepEqual(await visibility(['satellite']), ['none'], 'imagery off again');
    assert.deepEqual(await visibility(FILLS), FILLS.map(() => 'visible'), 'fills back');
    assert.deepEqual(await roadOpacity(), [undefined, undefined, 0.5], 'roads opaque again');
  });

  // Points of interest (docs/SPEC.md FR-15.2, FR-15.5): nothing below zoom 8, "Show in this area"
  // from 8 until the tiles take over at 13, where the paths start too.
  it('10. Show in this area offered only between zoom 8 and 13', async () => {
    const menu = page.getByTestId('map-overlays');
    const trigger = menu.getByRole('button').first();
    if ((await trigger.getAttribute('aria-expanded')) !== 'true') await trigger.click();
    await page.locator('#overlay-spots-playground').check();
    await trigger.click();
    const offeredAt = async (zoom) => {
      await page.evaluate((zoom) => window.__holdmytrack.jumpTo({ center: [-82.9988, 39.9612], zoom }), zoom);
      await page.waitForTimeout(300);
      return page.getByTestId('show-in-area').isVisible();
    };
    assert.equal(await offeredAt(7), false, 'not at zoom 7');
    assert.equal(await offeredAt(8), true, 'offered at zoom 8');
    assert.equal(await offeredAt(12.5), true, 'offered at zoom 12.5');
    assert.equal(await offeredAt(13), false, 'the tiles take over at zoom 13');
    const minzoom = await page.evaluate(() => window.__holdmytrack.getLayer('paths_trail').minzoom);
    assert.equal(minzoom, 13, 'paths start where the tiles do');

    if ((await trigger.getAttribute('aria-expanded')) !== 'true') await trigger.click();
    await page.locator('#overlay-spots-playground').uncheck();
  });
});
