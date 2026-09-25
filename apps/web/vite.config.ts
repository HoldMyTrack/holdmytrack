import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

// tsconfig.json's `types` is deliberately just `["vite/client"]` — no @types/node for one
// config file. This config file alone needs `process.env`, so it gets its own ambient type.
declare const process: { env: Record<string, string | undefined> };

// Vite binds loopback-only by default. Inside the `web` compose service that means the
// published port reaches nothing, so compose.yaml sets VITE_DEV_HOST=0.0.0.0 to open it
// up; npm run dev on the host leaves it unset and keeps the safe loopback default.
const devHost = process.env.VITE_DEV_HOST;
// Escape hatch if VirtioFS inotify forwarding goes deaf. Off by default: polling 773 font
// files burns real CPU for no reason on a host where inotify already works.
const watchPoll = process.env.VITE_WATCH_POLL;
// Where the Go server is, for the page routes below — the `api` service inside compose
// (compose.yaml sets it), localhost:8080 for a Go server run on the host.
const pagesTarget = process.env.API_PROXY_TARGET ?? 'http://localhost:8080';
// The server-rendered pages (services/server/internal/web, ADR-0012) and what they load. In
// production Caddy sends these same paths to the Go server (apps/web/docker/Caddyfile);
// proxying them here makes dev behave the same, with the pages on this origin like the app.
// /v1 is for the header's avatar image — the React app itself calls the API directly at
// VITE_API_BASE_URL and never goes through this.
// Vite matches these against the URL with its query string, hence the `(\?|$)` endings.
const pageRoutes = [
  '^/(about|help|contacts|logout|signin|signup|demo|forgot|reset|verify)(\\?|$)',
  '^/verify-pending(/|\\?|$)',
  '^/static/',
  '^/v1/',
];

export default defineConfig({
  plugins: [react()],
  worker: {
    // maplibre-gl creates its worker with { type: 'module' }; see src/map/worker.ts
    // for why the URL is supplied explicitly rather than left to the library.
    format: 'es',
  },
  server: {
    port: 5173,
    // public/basemap/*.pmtiles is read by the client over HTTP range requests.
    // Vite's dev server honours Range on static files, so no tile server is needed.
    ...(devHost ? { host: devHost } : {}),
    // changeOrigin stays false: the Go server checks a form POST's Origin against
    // APP_BASE_URL (this dev server's own origin), and the header must reach it untouched.
    proxy: Object.fromEntries(pageRoutes.map((route) => [route, { target: pagesTarget }])),
    watch: {
      // publicDir is watched by default — 773 glyph files and a 326 MB archive that
      // never change during dev. Worth ignoring on the host too, not just in the
      // container.
      ignored: ['**/public/basemap/**', '**/dist/**', '**/tests/screenshots/**'],
      ...(watchPoll ? { usePolling: true } : {}),
    },
  },
  preview: {
    port: 4173,
    ...(devHost ? { host: devHost } : {}),
  },
  build: {
    // The basemap archive lives in public/ and is copied verbatim into dist/.
    // At ~326 MB that is slow but correct for Phase 1; production moves it to
    // object storage (IMPLEMENTATION.md §5.4).
    chunkSizeWarningLimit: 1500,
  },
});
