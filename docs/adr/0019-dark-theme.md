# ADR-0019: A dark theme — one palette per theme for both clients, and a fog veil per basemap

## Status

Accepted.

## Context

The Android map already switched to the dark basemap flavor in the phone's dark mode, while the app's own chrome stayed light: a dark map under light controls. The web had a single light palette and chose its map flavor only from the URL. And Fog of War's veil was one fixed color, the dark ink at 0.82, which nearly vanished over the dark basemap, the same failure as the white veil it had replaced over the light basemap (`IMPLEMENTATION.md` §4.2).

## Decision

**Every page and the Android app get a dark palette next to the light one, chosen per device (System, Light, Dark), and the map's flavor and fog veil follow it.**

- One dark palette, defined in `tokens.css` and carried value for value into Android's `res/values-night/colors.xml`, as the light one already was. Only colors change between the two; every scale is shared.
- The choice is per device, not per account: System (the default, following the OS) or a pinned Light or Dark, in the web's account menu and in Android's Settings. On the web a `<head>` script applies it before first paint from localStorage; on Android, AppCompat's night mode.
- The map's flavor follows the theme (light or dark). A web URL can still pin one of the other three flavors (`&theme=black` and so on) until the theme next changes; `&theme=light` or `dark` in a URL is ignored, since the theme already decides between those two.
- Two fog veils, each the opposite of its basemap in lightness: the dark ink (`#202B25` at 0.82) over the light basemap, a cream mist (`#F7F4EC` at 0.6) over the dark one. The server renders the raster tier per `?theme=`, and both clients draw the Country/Region fills in the matching pair.

## Alternatives considered

- **Keep the app light and let only the map follow the OS** (Android's previous behavior). Rejected: a dark map under light chrome reads as unfinished, and the web didn't even have that.
- **Store the theme on the account.** Rejected: the right theme depends on the screen and the room, not the person; the same account on a phone at night and a desktop by day wants two answers, which is what per-device gives. It also keeps the choice working for pages served from one shared cache.
- **Render `data-theme` on the server from a cookie.** Rejected for the web: `RenderPublic` caches one copy of each signed-out page for every visitor, which a per-visitor attribute would break. A three-line `<head>` script does the same without touching caching.
- **A darker veil over the dark basemap** (near-black, more opaque). Rejected: both dark, so explored and unexplored ground would differ only slightly. The veil's job is contrast with the map under it, which only the opposite lightness guarantees.
- **Recolor one fog PNG on the client.** Rejected: MapLibre's raster paint can shift brightness and hue but can't turn a dark veil into a warm cream one; the server already renders per request, so a second veil costs one query parameter.

## Consequences

- Every new color is a token with two values, on both clients. A literal color in a stylesheet or drawable is now a bug in one of the two themes.
- The dark palette is provisional, like the light one, until the design freeze (root `ROADMAP.md` Phase 3) sets the final values; its contrast was checked against WCAG AA for text (`IMPLEMENTATION.md` §4.18).
- The fog raster has two variants per tile. Each is cached under its own URL, so the per-device tile cache (§4.2.6) holds whichever themes that device has used.
- Map overlays other than fog (the track gold, the selected track's dark halo, the heatmap ramp) are unchanged across flavors; they read on both, but they were tuned on the light one.