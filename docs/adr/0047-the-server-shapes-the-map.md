# ADR-0047: The server shapes the map, and the clients apply what it sends

## Status

Accepted. Built for Fog of War and Heatmap: their sources, layers, per-mode views and zoom tiers travel in the served style's metadata (`ARCHITECTURE.md` §2.1, `IMPLEMENTATION.md` §4.2).

## Context

The web, Android and soon iOS draw the same map. The basemap style already had one definition, served as a document (`ARCHITECTURE.md` §2.1), but each client built Fog and Heatmap by hand: the veil colours, the edge widths, the heat ramp and circle radius, the zoom tiers, which layers each mode shows, how Fog mutes the labels, and the veil over satellite imagery. The web and Android held about 25 such values each, kept equal by comments, and the server's neighbour weighting (ADR-0046) assumed one of them, the circle radius, with nothing checking it. An iOS client would have made a third copy, and every change to the look shipped as an app release.

## Decision

- **The server decides what the map shows, how it looks, and every rule about the data; a client decides only what needs the device**: input and gestures, sensors and recording, offline use, the source gate (ADR-0039), and anything that can't wait for a round trip.
- **A rule a client applies instantly travels as data ahead of time.** The style carries every mode's view, and a mode switch applies one locally.
- **Fog and Heatmap are `metadata["holdmytrack:overlays"]` in the served style**: the sources (tile paths and zoom range), the MapLibre layers, a view per mode with and without imagery, and the zoom tiers. Every view sets the same properties, so a switch never undoes one. A client adds the sources with its base URL and the tile version, inserts the layers beneath the labels and applies views, holding no value of its own. The definition is `apps/web/src/map/coverageStyle.ts`, rendered with the rest of the style; a Go test holds the circle radius and tile zooms to `internal/fog`.

## Alternatives considered

- **Each client keeps its copy, checked by a script** comparing the values across files. Built first and dropped: it catches a drifted value, but the copies remain, and so does the layer construction around them in each language.
- **An endpoint of values only.** Each client would still turn the numbers into expressions and layers, the part where the clients differed.
- **A separate overlays route.** A second request before the map can draw, and a second document to cache, while the style document is already fetched, cached by ETag and ignored past what an installed build reads.
- **The overlays among the style's own `layers`.** Their sources are the account's and carry its tile version, which an unauthenticated document shared by every account can't hold, and installed Android builds would draw them twice.

## Consequences

- **A change to how Fog or Heatmap looks is a server deploy**; installed apps take it on their next style load.
- **The overlays are a contract** (`services/server/api`), so while installed builds read them they only grow: a client skips a layer type it doesn't build.
- **MapLibre Native can't add a layer from JSON**, so Android builds each from its type and sets properties by name. Every array in a layer is an expression, by contract, since that is how Native reads it.
- **The style grows by about 10 KB**, about 1 KB compressed.
- **The track lines, pace bands, bike paths and Spots** still define their look in each client, and move under this rule when next changed.