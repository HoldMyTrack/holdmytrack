/**
 * A cache-busting version for the Fog/Heatmap tile URLs (fog.ts, heatmap.ts). Those URLs are
 * otherwise fixed — neither mode is scoped by any client-side filter — so once MapLibre has a
 * tile, nothing would ever make it ask again. An Edit track (IMPLEMENTATION.md §4.7.7)
 * changes the account's coverage under an open page, so its completion bumps this and
 * re-points every coverage source at the new URL, which MapLibre (and any HTTP cache in
 * between) treats as new tiles. Module-level rather than per-map on purpose: a source
 * re-created after a theme swap (`styledata`) has to come back at the current version too.
 */
let version = 0;

export function versionedTileURL(url: string): string {
  return version === 0 ? url : `${url}?v=${version}`;
}

export function bumpCoverageVersion(): void {
  version += 1;
}
