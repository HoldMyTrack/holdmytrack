# ADR-0022: Satellite imagery is an optional base map, opt-in per deployment, drawn under the vector roads and labels

## Status

Accepted. Built on the web and Android (`SPEC.md` FR-4.14, `IMPLEMENTATION.md` §4.26). holdmytrack.com uses MapTiler Satellite on MapTiler's Free plan.

## Context

HoldMyTrack renders over a self-hosted Protomaps vector basemap, and imagery was ruled out because a metered tile provider bills per request on pan and zoom traffic that earns nothing (`VISION.md` §2, `IMPLEMENTATION.md` §4.2.1). Users still want to see the ground itself: a trailhead, a clearing, a field edge a vector map draws as one flat colour. gpx.studio's "Liberty Satellite" is the reference the request pointed at: MapTiler's `satellite-v2` raster with the OpenFreeMap Liberty style's roads and labels over it, from an OpenMapTiles vector source. Four choices decide what this costs and how it behaves: whether imagery is fixed or configurable, which provider, what is drawn over it, and how clients switch it.

## Decision

**Imagery is deployment config, not code.** A deployment sets an XYZ tile template with its provider's key, the tile size, the deepest zoom and the credit the provider's terms require (`VITE_SATELLITE_*` for the web bundle, `SATELLITE_*` for the API's served style). With none set, the style has no satellite source, and no client shows a switch. The cost is opt-in, and a self-hoster pays nothing for a feature they don't turn on.

**holdmytrack.com uses MapTiler Satellite on the Free plan.** 100k tile requests a month, non-commercial. Past that quota MapTiler pauses the imagery until the next month rather than billing, so the worst case is a month with no satellite, never an unplanned bill. Declared at its native 512 px, so each tile counts once rather than as four 256 px ones. The key is restricted to the site's origin in MapTiler's dashboard. Esri World Imagery (2M free tiles a month, then metered) or MapTiler's paid Flex plan are the next step if usage outgrows it, and either is only a change of config.

**Hybrid, over our own vector basemap.** The imagery is one raster layer right above the background of the same Protomaps style. Satellite mode shows it and hides the background and the area fills (earth, water, landuse, landcover, buildings). Roads stay at 40% opacity, since the flavors' road colors are made for a flat background and at full strength cover much of the photo. Boundaries and labels stay as they are, and so do tracks, Fog and Heatmap, which are inserted above it as they always were.

**A layer switch, not a style swap.** The imagery ships hidden in every style document and is flipped at runtime with layer visibility, like the trails and bike paths. The list of fills to hide is derived from layer type in `style.ts` and written into the style's `metadata`, so Android reads it instead of keeping its own copy.

## Alternatives considered

**A second style on OpenMapTiles, like gpx.studio.** Liberty's layers expect the OpenMapTiles schema, not Protomaps'. It would mean a second planet archive, or a third-party vector tile host, plus rewriting the trails, tracks and bike-path layers for another schema, all for roads and labels we already draw.

**Keep vector only.** It keeps the imagery bill at zero. But the ground is what a user wants to see when choosing a route or checking where a track actually went, and the Free plan's pause-at-quota caps the cost at zero too.

**Imagery only, with no roads or labels.** Simpler, but a satellite view without names or roads is hard to find your way in. Everything we draw over the map reads better with the roads to anchor it.

**Sentinel-2 cloudless (EOX).** Free and keyless for the 2016 CC BY edition, but 10 m per pixel: fine from continent to town level, a blur at street level, where people actually look at a track.

**A separate style document per mode, swapped with `setStyle`.** It doubles the served documents and reloads every overlay on each switch, where a visibility flip costs nothing.

## Consequences

The product claim that HoldMyTrack deliberately does not match fog-over-imagery apps no longer holds for a deployment with imagery configured. Fog over satellite is now possible and takes the cream veil, because imagery reads dark.

On the Free plan, heavy use can exhaust the month's quota. Satellite then shows no imagery for the rest of the month, only the vector roads and labels on the background-less style. That is accepted as the price of a bill that can't grow.

The API key is in the web bundle and the served style, readable by anyone. The origin restriction is what protects it on the web. Android requests carry no browser origin, so they depend on MapTiler accepting them from an origin-restricted key; if they don't, the Android key needs its own restriction.

Every exported PNG with satellite on carries the provider's credit next to OSM's.