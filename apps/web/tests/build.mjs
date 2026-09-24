/**
 * Verification of the *built* output, which is a different question from the dev
 * server and has to be asked separately.
 *
 * This exists because of a real failure: maplibre-gl derives its worker URL from
 * its own `import.meta.url`, which bundling invalidates. Dev passed every check
 * while production rendered a blank grey map — no console error, no failed
 * request, just a map that read its TileJSON header and never asked for a tile.
 * The dev suite could not have caught it, so this runs against `vite preview`.
 *
 * Black-box on purpose: the production bundle does not expose a map handle.
 *
 *   npm run build && npm run verify:build
 */
import { spawn } from 'node:child_process';
import { mkdir } from 'node:fs/promises';
import { after, before, describe, it } from 'node:test';
import assert from 'node:assert/strict';
import { chromium } from 'playwright';

const PORT = 4183;
const BASE = `http://localhost:${PORT}`;
const SHOTS = new URL('./screenshots/', import.meta.url);

let server;
let browser;
let page;
let partial = 0;
const failures = [];

// Same reasoning as smoke.mjs: App.tsx now gates everything behind AuthGate, so a fresh
// browser context needs a real session cookie before the map can mount at all.
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
      throw new Error(`build test login failed: ${login.status()} ${await login.text()}`);
    }
  } else if (!signup.ok()) {
    throw new Error(`build test signup failed: ${signup.status()} ${await signup.text()}`);
  }
}

before(async () => {
  await mkdir(SHOTS, { recursive: true });

  server = spawn('npx', ['vite', 'preview', '--port', String(PORT), '--strictPort'], {
    cwd: new URL('..', import.meta.url).pathname,
    stdio: 'ignore',
  });

  const deadline = Date.now() + 60_000;
  for (;;) {
    try {
      if ((await fetch(BASE)).ok) break;
    } catch {
      /* not up yet */
    }
    if (Date.now() > deadline) throw new Error('vite preview never became ready — run `npm run build` first');
    await new Promise((r) => setTimeout(r, 250));
  }

  browser = await chromium.launch({ args: process.env.PW_CHROMIUM_ARGS?.split(/\s+/) ?? [] });
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  await ensureSignedIn(context);
  page = await context.newPage();
  page.on('console', (m) => {
    if (m.type() === 'error') failures.push(`console: ${m.text()}`);
  });
  page.on('pageerror', (e) => failures.push(`pageerror: ${e.message}`));
  page.on('requestfailed', (r) => failures.push(`requestfailed: ${r.url()}`));
  page.on('response', (r) => {
    if (r.status() === 206) partial += 1;
    if (r.status() >= 400) failures.push(`${r.status()} ${r.url()}`);
  });

  await page.goto(`${BASE}/#map=12.00/39.96120/-82.99880&theme=light`);
  // No handle to await in production, so wait on the observable outcome: tiles.
  await page
    .waitForFunction(() => performance.getEntriesByType('resource').length > 10, undefined, {
      timeout: 30_000,
    })
    .catch(() => {});
  await page.waitForTimeout(Number(process.env.PW_SETTLE_MS ?? 8000));
});

after(async () => {
  await browser?.close();
  server?.kill('SIGTERM');
});

describe('production build', () => {
  it('emits the maplibre worker as a real asset', async () => {
    const res = await fetch(`${BASE}/`);
    const html = await res.text();
    // Whatever module script index.html loads, not a fixed name: the chunk is named after
    // its rollup input (vite.config.ts), which is `main` since about.html became a second one.
    const entry = html.match(/<script type="module"[^>]*src="(\/assets\/[\w-]+\.js)"/)?.[1];
    assert.ok(entry, 'entry chunk in index.html');
    const js = await (await fetch(`${BASE}${entry}`)).text();
    const worker = js.match(/\/assets\/maplibre-gl-worker-[\w-]+\.js/)?.[0];
    assert.ok(worker, 'bundle references an emitted worker asset');
    assert.equal((await fetch(`${BASE}${worker}`)).status, 200, 'worker asset is served');
  });

  it('fetches tiles over range requests, so the worker is alive', () => {
    assert.ok(
      partial > 5,
      `expected many 206s; got ${partial}. One or two means the header was read and ` +
        'the worker never started, which renders a blank map.',
    );
  });

  it('reports no console errors or failed requests', () => {
    assert.deepEqual(failures, []);
  });

  it('paints actual map content, not an empty canvas', async () => {
    // Reading pixels back out of the canvas does not work here: MapLibre runs
    // without preserveDrawingBuffer, so gl.readPixels after presentation returns a
    // cleared buffer and would report "blank" for a perfectly good map.
    //
    // The composited screenshot is the reliable observation, and its compressed
    // size is a blunt but honest proxy for content: the blank-map failure is one
    // flat grey fill, which PNG squeezes to a few KB, while a rendered basemap of
    // Columbus is close to a megabyte. Anything in between still fails loudly.
    const shot = await page.screenshot({ path: new URL('production.png', SHOTS).pathname });
    assert.ok(
      shot.byteLength > 150_000,
      `screenshot is ${Math.round(shot.byteLength / 1024)} kB — a blank grey canvas ` +
        'compresses to roughly 10 kB, so this map is probably not rendering',
    );
  });
});
