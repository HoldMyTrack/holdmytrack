import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';
import { staticHeader } from './src/about/staticHeader';

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

export default defineConfig({
  plugins: [
    react(),
    {
      // The static pages' shared header (src/about/staticHeader.ts). 'pre' so the injected
      // markup goes through Vite's own HTML processing (the logo becomes a hashed asset).
      name: 'static-header',
      transformIndexHtml: {
        order: 'pre',
        handler: (html, ctx) =>
          html.replace('<!-- static-header -->', () => staticHeader(ctx.path.replace(/\.html$/, ''))),
      },
    },
  ],
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
    // Three pages: the app itself, and the static public About and Help pages (about.html,
    // help.html), which need no JavaScript and stay readable without an account. Caddy
    // serves them at /about and /help.
    rollupOptions: {
      input: { main: 'index.html', about: 'about.html', help: 'help.html' },
    },
  },
});
