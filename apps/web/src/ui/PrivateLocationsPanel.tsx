import { useCallback, useEffect, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import {
  deletePrivateLocation,
  listPrivateLocations,
  PRIVATE_LOCATION_MAX_RADIUS_M,
  PRIVATE_LOCATION_MIN_RADIUS_M,
  savePrivateLocation,
  type PrivateLocation,
} from '../api';
import { labelInsertionPoint } from '../map/layers';
import {
  attachPrivateLocationsHandlers,
  clearPrivateLocations,
  ensurePrivateLocationsLayer,
  PRIVATE_LOCATIONS_MIN_PLACE_ZOOM,
  setPrivateLocationsData,
  type PrivateLocationsClick,
} from '../map/privateLocations';
import { ConfirmDialog } from './ConfirmDialog';
import { elevationUnitLabel, metersToFeet } from './format';
import { useUnitSystem } from './units';

/**
 * The Private locations window (FR-8.1) — floats over the map, like EditTrackPanel, while the
 * account's circles are shown and edited. Clicking empty map places a new circle there,
 * clicking a saved one selects it, and the selected circle's center handle drags. Save and
 * Delete go straight to the server, which reprocesses every activity the change could clip —
 * `onChanged` has MapView reload the list so those rows show Pending straight away.
 *
 * Read-only for the demo account: its circle is shown, nothing is editable.
 */
export interface PrivateLocationsPanelProps {
  map: MapLibreMap;
  readOnly: boolean;
  onChanged: () => void;
  onClose: () => void;
}

/** The circle being edited — a saved one (with its id) or a new one not saved yet. */
interface Draft {
  id?: string;
  name: string;
  lon: number;
  lat: number;
  radiusM: number;
}

const DEFAULT_RADIUS_M = 200;

function sameDraft(draft: Draft, saved: PrivateLocation | undefined): boolean {
  return (
    saved !== undefined &&
    draft.name.trim() === saved.name &&
    draft.lon === saved.lon &&
    draft.lat === saved.lat &&
    draft.radiusM === saved.radiusM
  );
}

export function PrivateLocationsPanel({ map, readOnly, onChanged, onClose }: PrivateLocationsPanelProps) {
  const system = useUnitSystem();
  const [locations, setLocations] = useState<PrivateLocation[] | null>(null);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [draft, setDraft] = useState<Draft | null>(null);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [confirmDelete, setConfirmDelete] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    listPrivateLocations(controller.signal)
      .then(setLocations)
      .catch((err: unknown) => {
        if (!controller.signal.aborted) setLoadError(err instanceof Error ? err.message : String(err));
      });
    return () => controller.abort();
  }, []);

  // Draw, and redraw after a theme swap discards custom layers (styledata) — owned here, not
  // by MapView's reattachOverlays, since the overlay only exists while this panel is open.
  useEffect(() => {
    const draw = () => {
      ensurePrivateLocationsLayer(map, labelInsertionPoint(map));
      setPrivateLocationsData(map, locations ?? [], draft);
    };
    draw();
    map.on('styledata', draw);
    return () => {
      map.off('styledata', draw);
    };
  }, [map, locations, draft]);
  useEffect(() => () => clearPrivateLocations(map), [map]);

  const select = useCallback((location: PrivateLocation) => {
    setDraft({ id: location.id, name: location.name, lon: location.lon, lat: location.lat, radiusM: location.radiusM });
    setError(null);
  }, []);

  useEffect(() => {
    if (!locations) return;
    return attachPrivateLocationsHandlers(map, {
      onClick: (target: PrivateLocationsClick) => {
        if (target.kind === 'selected') return;
        if (target.kind === 'saved') {
          const location = locations.find((l) => l.id === target.id);
          if (location) select(location);
          return;
        }
        if (readOnly) {
          setDraft(null);
          return;
        }
        // Zoomed out, a click means "take me there", not "put a circle here".
        if (map.getZoom() < PRIVATE_LOCATIONS_MIN_PLACE_ZOOM) {
          map.flyTo({ center: target.lngLat, zoom: PRIVATE_LOCATIONS_MIN_PLACE_ZOOM + 2 });
          return;
        }
        // Empty map: a new circle there — or, while a new one is unsaved, move it there.
        setDraft((d) =>
          d && d.id === undefined
            ? { ...d, lon: target.lngLat.lng, lat: target.lngLat.lat }
            : { name: '', lon: target.lngLat.lng, lat: target.lngLat.lat, radiusM: DEFAULT_RADIUS_M },
        );
        setError(null);
      },
      onDrag: (lngLat) => {
        if (!readOnly) setDraft((d) => (d ? { ...d, lon: lngLat.lng, lat: lngLat.lat } : d));
      },
    });
  }, [map, locations, readOnly, select]);

  const saved = draft?.id === undefined ? undefined : locations?.find((l) => l.id === draft.id);
  const dirty = draft !== null && !sameDraft(draft, saved);

  async function save() {
    if (!draft) return;
    setBusy(true);
    setError(null);
    try {
      const result = await savePrivateLocation(
        { name: draft.name.trim(), lon: draft.lon, lat: draft.lat, radiusM: draft.radiusM },
        draft.id,
      );
      setLocations((prev) =>
        draft.id === undefined ? [...(prev ?? []), result] : (prev ?? []).map((l) => (l.id === result.id ? result : l)),
      );
      setDraft(null);
      onChanged();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  }

  async function remove() {
    if (!draft?.id) return;
    const id = draft.id;
    await deletePrivateLocation(id);
    setLocations((prev) => (prev ?? []).filter((l) => l.id !== id));
    // Unmounting the dialog with the draft skips its own onClose, so close it here too.
    setConfirmDelete(false);
    setDraft(null);
    onChanged();
  }

  const unit = elevationUnitLabel(system);
  const radiusDisplay = (m: number) => Math.round(system === 'imperial' ? metersToFeet(m) : m).toLocaleString();

  return (
    <section className="edit-track private-locations" aria-label="Private locations" data-testid="private-locations">
      <header className="edit-track__head">
        <span className="edit-track__title">Private locations</span>
        <span className="edit-track__subtitle">
          {readOnly ? 'Tracks never show inside these circles.' : 'Tracks never show inside these circles. Zoom in and click the map to add one.'}
        </span>
      </header>

      {loadError && <p className="edit-track__error">{loadError}</p>}
      {!locations && !loadError && <p className="edit-track__note">Loading…</p>}

      {locations && (
        <ul className="private-locations__list">
          {locations.length === 0 && draft === null && <li className="edit-track__note">None yet.</li>}
          {locations.map((l) => (
            <li key={l.id}>
              <button
                type="button"
                className="private-locations__item"
                aria-pressed={draft?.id === l.id}
                onClick={() => {
                  select(l);
                  map.flyTo({ center: [l.lon, l.lat], zoom: Math.max(map.getZoom(), 14) });
                }}
              >
                <span className="private-locations__name">{l.name || 'Unnamed'}</span>
                <span className="private-locations__radius">
                  {radiusDisplay(l.radiusM)} {unit}
                </span>
              </button>
            </li>
          ))}
        </ul>
      )}

      {draft && !readOnly && (
        <div className="private-locations__editor">
          <label className="private-locations__field">
            <span className="activity-filters__label">Name</span>
            <input
              className="settings-page__input"
              type="text"
              value={draft.name}
              maxLength={100}
              placeholder="e.g. Home"
              onChange={(e) => setDraft({ ...draft, name: e.target.value })}
            />
          </label>
          <label className="private-locations__field">
            <span className="activity-filters__head">
              <span className="activity-filters__label">Radius</span>
              <span className="activity-filters__readout">
                {radiusDisplay(draft.radiusM)} {unit}
              </span>
            </span>
            <input
              type="range"
              className="private-locations__slider"
              min={PRIVATE_LOCATION_MIN_RADIUS_M}
              max={PRIVATE_LOCATION_MAX_RADIUS_M}
              step={10}
              value={draft.radiusM}
              onChange={(e) => setDraft({ ...draft, radiusM: Number(e.target.value) })}
            />
          </label>
          <p className="edit-track__note">
            Drag the center to move it. Don't center it exactly on your door — a circle's middle is the first place anyone looks.
          </p>
          {error && <p className="edit-track__error">{error}</p>}
          <div className="edit-track__row edit-track__row--footer">
            {draft.id !== undefined && (
              <button type="button" className="edit-track__btn edit-track__btn--quiet" disabled={busy} onClick={() => setConfirmDelete(true)}>
                Delete
              </button>
            )}
            <span className="edit-track__spacer" />
            <button type="button" className="edit-track__btn" disabled={busy} onClick={() => setDraft(null)}>
              Cancel
            </button>
            <button type="button" className="edit-track__btn edit-track__btn--primary" disabled={busy || !dirty} onClick={() => void save()}>
              {busy ? 'Saving…' : 'Save'}
            </button>
          </div>
        </div>
      )}

      {!draft && (
        <div className="edit-track__row edit-track__row--footer">
          <p className="edit-track__note">Saving a change reprocesses the activities it touches — they show Pending until done.</p>
          <span className="edit-track__spacer" />
          <button type="button" className="edit-track__btn" onClick={onClose}>
            Done
          </button>
        </div>
      )}

      {confirmDelete && draft?.id !== undefined && (
        <ConfirmDialog
          title="Delete this private location?"
          message="Activities that start or end inside it will show those parts again once they're reprocessed."
          confirmLabel="Delete"
          busyLabel="Deleting…"
          onConfirm={remove}
          onClose={() => setConfirmDelete(false)}
        />
      )}
    </section>
  );
}
