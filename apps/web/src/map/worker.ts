import { setWorkerUrl } from 'maplibre-gl';
// Vite bundles the worker entry (it imports a shared chunk, so it cannot simply be
// copied) and hands back the emitted URL.
import workerUrl from 'maplibre-gl/dist/maplibre-gl-worker.mjs?worker&url';

/**
 * Point MapLibre at a worker URL that survives bundling.
 *
 * Left alone, maplibre-gl derives its worker URL from its own `import.meta.url`
 * and appends `./maplibre-gl-worker.mjs`. That assumption breaks the moment the
 * library is bundled: in a production build `import.meta.url` is the app chunk in
 * /assets/, so the worker resolves to a file that was never emitted. The failure
 * is silent and total — no console error, no failed page request (worker script
 * loads are not page requests), just a map that fetches its TileJSON header and
 * then never requests a tile, because tile loading lives in the worker.
 *
 * Registering an explicit URL makes dev and production take the same path.
 */
let applied = false;

export function configureMapLibreWorker(): void {
  if (applied) return;
  setWorkerUrl(workerUrl);
  applied = true;
}
