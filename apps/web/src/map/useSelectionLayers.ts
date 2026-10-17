import { useEffect } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { setHiddenTracks, setHoveredTrack, setSelectedTracks, setTrackInteractivityHandlers } from './tracks';

export interface SelectionLayerInputs {
  focusedId: string | null;
  hoveredId: string | null;
  mapHiddenIds: ReadonlySet<string>;
  /** The Edit window is open: its group and focus must stay, and a track session's clicks aim at points. */
  editOpen: boolean;
  onFocus: (id: string) => void;
  onHover: (id: string | null) => void;
  onClickAway: () => void;
}

/**
 * The selection on the tracks layer: only the focused track is bold (checked ones aren't), the
 * hovered one is previewed, and hidden, filtered and Pending tracks aren't drawn. A click on a
 * track focuses it as a row click does; a click away clears the focus and keeps the checked group.
 * While the Edit window is open the handlers go quiet.
 */
export function useSelectionLayers(map: MapLibreMap | null, inputs: SelectionLayerInputs): void {
  const { focusedId, hoveredId, mapHiddenIds, editOpen, onFocus, onHover, onClickAway } = inputs;
  useEffect(() => {
    setTrackInteractivityHandlers(
      editOpen ? { onSelect: () => {}, onHover: () => {}, onClickAway: () => {} } : { onSelect: onFocus, onHover, onClickAway },
    );
  }, [onFocus, onClickAway, editOpen]);

  useEffect(() => {
    if (map) setSelectedTracks(map, focusedId !== null ? [focusedId] : []);
  }, [map, focusedId]);

  useEffect(() => {
    if (map) setHoveredTrack(map, hoveredId);
  }, [map, hoveredId]);

  useEffect(() => {
    if (map) setHiddenTracks(map, [...mapHiddenIds]);
  }, [map, mapHiddenIds]);
}
