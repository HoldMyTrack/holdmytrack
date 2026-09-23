import { addProtocol } from 'maplibre-gl';
import { Protocol } from 'pmtiles';

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
