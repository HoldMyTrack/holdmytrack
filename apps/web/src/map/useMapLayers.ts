import { useCallback, useEffect, useRef } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import { ensureBikePathLayers, setBikePathsVisible, type BikePathOverlays } from './bikePaths';
import { basemapOrigin, satelliteSource } from './config';
import { ensureCoverageLayers } from './coverageLayers';
import { labelInsertionPoint } from './layers';
import { setMapMode, type MapMode } from './mapMode';
import { raisePathLayers } from './paths';
import { setSatelliteVisible } from './satellite';
import { ensureSpotsLayer, type SpotCategory } from './spots';
import { buildStyle, type Flavor } from './style';
import { ensureBandLayer, setTrackBands } from './trackBands';
import { ensureTrackLayer, refreshTrackLayer, setHiddenTracks, setSelectedTracks } from './tracks';
import type { ActivityQuery, ActivityTrackMetrics } from '../api';
import { lang } from '../i18n';

export interface MapLayersInputs {
  initialFlavor: Flavor;
  flavor: Flavor;
  satellite: boolean;
  mode: MapMode;
  /** The Edit window's Track tab has the map. */
  editingTrack: boolean;
  paths: BikePathOverlays;
  spotsShown: readonly SpotCategory[];
  activityQuery: ActivityQuery;
  mapHiddenIds: ReadonlySet<string>;
  focusedId: string | null;
  /** The focused activity's metrics, if its bands are drawn. */
  bands: ActivityTrackMetrics | null;
}

/**
 * The app's own layers over the basemap: added on every style load, since `setStyle` keeps the
 * camera but drops custom layers, and kept in step with the mode, paths, satellite and the
 * tracks' query between loads. A theme change swaps the style in place.
 */
export function useMapLayers(map: MapLibreMap | null, inputs: MapLayersInputs): void {
  const { initialFlavor, flavor, satellite, mode, editingTrack, paths, spotsShown, activityQuery, mapHiddenIds, focusedId, bands } = inputs;

  // Points the existing tracks source at the new query. Trap: `ensureTrackLayer` only seeds a new
  // source, so it can't do this.
  useEffect(() => {
    if (map) refreshTrackLayer(map, activityQuery);
  }, [map, activityQuery]);

  const reattach = useCallback(
    (instance: MapLibreMap) => {
      const beforeId = labelInsertionPoint(instance);
      // Layer order matters: MapLibre paints later layers on top, all beneath the basemap labels
      // (layers.ts). Fog and heatmap first, so tracks show through the veil.
      ensureCoverageLayers(instance, beforeId);
      // Trails and paths over the veil and under the activity tracks (paths.ts, bikePaths.ts).
      raisePathLayers(instance, beforeId);
      ensureBikePathLayers(instance, beforeId, flavor);
      ensureTrackLayer(instance, beforeId, activityQuery);
      // A recreated source has no feature-state, the focused track's `selected` included.
      setSelectedTracks(instance, focusedId !== null ? [focusedId] : []);
      // Bands after tracks, to paint over the one track they apply to (trackBands.ts).
      ensureBandLayer(instance, beforeId);
      // Spots last, above the basemap's labels too (spots.ts).
      ensureSpotsLayer(instance, spotsShown);
      // Re-applied because new layers start hidden or unfiltered: without these, a style load
      // mid-mode would drop to Normal and un-hide every hidden track. Both diff against the layer's
      // current value first (docs/DEVELOPMENT.md, the setFilter/setLayoutProperty gotcha).
      setMapMode(instance, mode, satellite, editingTrack);
      setBikePathsVisible(instance, paths);
      setSatelliteVisible(instance, satellite, mode);
      setHiddenTracks(instance, [...mapHiddenIds]);
      // The band source is recreated empty.
      if (bands) setTrackBands(instance, bands.points);
    },
    [flavor, satellite, mode, editingTrack, paths, spotsShown, mapHiddenIds, activityQuery, bands, focusedId],
  );

  useEffect(() => {
    if (!map) return;
    const onStyleData = () => reattach(map);
    map.on('styledata', onStyleData);
    // Trap: `map` is set after 'load', and a later 'styledata' isn't guaranteed until a setStyle,
    // so attach directly too or overlays would wait for a theme switch.
    reattach(map);
    return () => {
      map.off('styledata', onStyleData);
    };
  }, [map, reattach]);

  // Declared after the styledata effect on purpose: effects run in order, so the reattach against
  // the outgoing style happens before setStyle starts loading the new one. `diff: false` drops
  // every custom source, so no fog source keeps the other theme's URL.
  const appliedFlavor = useRef(initialFlavor);
  useEffect(() => {
    if (!map || appliedFlavor.current === flavor) return;
    appliedFlavor.current = flavor;
    map.setStyle(buildStyle({ flavor, origin: basemapOrigin(), satellite: satelliteSource(), satelliteOn: satellite, lang, globe: true }), {
      diff: false,
    });
    // Only a flavor change swaps the style; the Satellite button flips layers in place (below).
  }, [map, flavor]);

  // Mode, paths and satellite changes between style loads flip the layers in place.
  useEffect(() => {
    if (!map) return;
    setMapMode(map, mode, satellite, editingTrack);
  }, [map, mode, satellite, editingTrack]);

  useEffect(() => {
    if (!map) return;
    setBikePathsVisible(map, paths);
  }, [map, paths]);

  useEffect(() => {
    if (!map) return;
    setSatelliteVisible(map, satellite, mode);
  }, [map, satellite, mode]);
}
