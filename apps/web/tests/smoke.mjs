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
    assert.match(hash, /^#map=13\.00\/41\.499\d+\/-81\.694\d+&theme=light$/, hash);
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
});
