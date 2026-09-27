/**
 * The account's tile version, sent as `cv` on every Fog/Heatmap/Country/Region tile URL (fog.ts,
 * heatmap.ts) and on the tracks URL (tracks.ts). The server marks a tile carrying it cacheable
 * for good (IMPLEMENTATION.md §4.2's "Caching"): anything that changes a tile also changes the
 * version, so each change is a new URL, and a revisited area or a reload is answered from the
 * browser's cache instead of the server.
 *
 * The page's shell carries the version the account was at when it loaded (`<meta
 * name="tile-version">`, so the first tiles don't wait on a status request); after that it
 * comes from `GET /v1/coverage/status` (useCoverageRefresh.ts). A change under an open page
 * (an upload, a delete, an Edit track) sets the new one and re-points every coverage source at
 * it, which MapLibre treats as new tiles. Module-level rather than per-map on purpose: a source
 * re-created after a theme swap (`styledata`) has to come back at the current version too.
 * Without one (the meta tag missing) tiles still load, just uncached.
 */
let tileVersion = readInitialTileVersion();

function readInitialTileVersion(): string {
  if (typeof document === 'undefined') return '';
  return document.querySelector<HTMLMetaElement>('meta[name="tile-version"]')?.content ?? '';
}

export function getTileVersion(): string {
  return tileVersion;
}

export function versionedTileURL(url: string): string {
  return tileVersion ? `${url}?cv=${encodeURIComponent(tileVersion)}` : url;
}

export function setTileVersion(next: string): void {
  tileVersion = next;
}
