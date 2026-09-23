/**
 * Where the self-hosted basemap assets live, and the region the archive covers.
 *
 * Paths are root-relative and deliberately origin-free: `style.ts` has to stay
 * DOM-free so it can run headlessly for print export, so whoever builds a style
 * supplies the origin. In development Vite serves `public/` at the web root; in
 * production these become CDN paths beside the `.pmtiles` (§1.6).
 */

/** Must match the string the style uses, so the pmtiles Protocol shares one archive instance. */
export const PMTILES_PATH = '/basemap/basemap.pmtiles';

/** MapLibre substitutes {fontstack} and {range}; the fontstack names contain spaces. */
export const GLYPHS_PATH = '/basemap/fonts/{fontstack}/{range}.pbf';

/** Sprite URLs carry no extension — MapLibre appends `.json` / `.png` (and `@2x`). */
export const SPRITE_BASE_PATH = '/basemap/sprites/v4';

/**
 * The fallback camera target once there is no saved URL position, no activity history to fly
 * to, and no Country setting to fall back to first (MapView.tsx's zero-history fallback,
 * docs/SPEC.md FR-4.5) — deliberately zoomed out far enough to keep every continent in frame,
 * not a regional point like the old Columbus, OH default this replaced.
 */
export const WORLD_VIEW = {
  longitude: 10,
  latitude: 15,
  zoom: 1.3,
} as const;

/**
 * The origin to resolve the paths above against.
 *
 * MapLibre 6 rejects a relative `sprite` URL outright ("must be absolute"), so
 * this is not cosmetic. Reading it here rather than inside `style.ts` is what
 * keeps style construction pure: the print pipeline passes its own origin
 * instead of borrowing the browser's.
 */
export function browserOrigin(): string {
  return window.location.origin;
}

/**
 * Where basemap assets (the pmtiles archive, fonts, sprites) actually live — usually the
 * app's own origin (dev, and any deployment that bundles the basemap alongside itself), but
 * overridable via a build-time `VITE_BASEMAP_ORIGIN` for a deployment that serves the
 * (large — the real planet archive is ~138 GB, `VISION.md` §4.3) archive from object
 * storage/a CDN instead. This isn't optional for a Docker-built production image:
 * `apps/web/.dockerignore` deliberately excludes every `*.pmtiles` file from the build
 * context ("the container reads it through the bind mount at runtime, never from the
 * image") — a production image has no local basemap file to fall back to at all, so
 * `useMapInstance.ts`/`exportMap.ts` call this instead of `browserOrigin()` directly for
 * the `origin` `buildStyle` (style.ts) resolves these paths against.
 */
export function basemapOrigin(): string {
  const configured = import.meta.env.VITE_BASEMAP_ORIGIN as string | undefined;
  return configured ? configured.replace(/\/$/, '') : browserOrigin();
}
