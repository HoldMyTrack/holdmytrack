import { addProtocol } from 'maplibre-gl';
import { PMTiles, Protocol } from 'pmtiles';

/**
 * Registration of the `pmtiles://` protocol is global to MapLibre and must happen
 * exactly once, before any Map is constructed. React 19 StrictMode deliberately
 * double-mounts effects, so this lives at module scope behind a guard rather than
 * in an effect — a second `addProtocol` call for the same scheme would throw.
 */
let protocol: Protocol | undefined;

export function registerPmtilesProtocol(): Protocol {
  if (!protocol) {
    const created = new Protocol();
    addProtocol('pmtiles', created.tile);
    protocol = created;
  }
  return protocol;
}

/**
 * Get the {@link PMTiles} instance the map itself will use for `url`.
 *
 * The Protocol keys archives by the URL that follows `pmtiles://`, so registering
 * ours under the identical string means a header read here and the map's tile
 * requests share one instance — and therefore one cached header and root
 * directory, rather than each paying their own 16 KB range request.
 */
export function sharedArchive(url: string): PMTiles {
  const p = registerPmtilesProtocol();
  const existing = p.get(url);
  if (existing) return existing;

  const archive = new PMTiles(url);
  p.add(archive);
  return archive;
}
