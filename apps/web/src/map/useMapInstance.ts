import { useEffect, useRef, useState } from 'react';
import {
  AttributionControl,
  GeolocateControl,
  Map as MapLibreMap,
  NavigationControl,
  ScaleControl,
} from 'maplibre-gl';
import { registerPmtilesProtocol } from './protocol';
import { configureMapLibreWorker } from './worker';
import { buildStyle, type Flavor } from './style';
import { basemapOrigin } from './config';
import type { ViewState } from './viewState';
import { API_BASE_URL } from '../api';
import { t } from '../i18n';

/**
 * Owns one MapLibre Map's lifecycle for one container element.
 *
 * The awkward part this contains is React 19 StrictMode, which mounts effects,
 * tears them down, and mounts them again to surface exactly this kind of bug. A
 * WebGL map is expensive and holds a canvas, so the cleanup has to genuinely
 * `remove()` it, and the async "map is ready" path has to notice it was cancelled
 * rather than calling setState against a dead instance.
 *
 * `initialView` and `initialFlavor` are read once, on purpose: later changes to
 * either must move the camera or swap the style, never rebuild the map.
 */
export interface UseMapInstanceOptions {
  container: React.RefObject<HTMLDivElement | null>;
  initialView: ViewState;
  initialFlavor: Flavor;
  /** ScaleControl's own unit — the one piece of map chrome that reads a distance number
   *  directly, so it has to track the account's Country setting (units.ts) the same way
   *  every other distance display in the app now does. Read on every render (not captured
   *  once like initialView/initialFlavor above) via the effect below, since Country can
   *  change at any time after the map is already up. */
  scaleUnit: 'metric' | 'imperial';
}

export function useMapInstance({
  container,
  initialView,
  initialFlavor,
  scaleUnit,
}: UseMapInstanceOptions): MapLibreMap | null {
  const [map, setMap] = useState<MapLibreMap | null>(null);
  const scaleControlRef = useRef<ScaleControl | null>(null);

  // Captured once so the effect below can keep an empty dependency list without
  // lying about what it reads.
  const initial = useRef({ view: initialView, flavor: initialFlavor });

  useEffect(() => {
    if (!container.current) return;

    // Both must precede construction: the style references pmtiles:// immediately,
    // and the worker URL is read when the Map spins up its workers.
    configureMapLibreWorker();
    registerPmtilesProtocol();

    let cancelled = false;
    const instance = new MapLibreMap({
      container: container.current,
      style: buildStyle({ flavor: initial.current.flavor, origin: basemapOrigin() }),
      center: [initial.current.view.longitude, initial.current.view.latitude],
      zoom: initial.current.view.zoom,
      // We render our own, positioned with the rest of the UI.
      attributionControl: false,
      // Nothing in Phase 1 benefits from a globe, and a flat map keeps the
      // later fog raster and print export in one predictable projection.
      maxPitch: 0,
      // Tracks/fog/heatmap tiles now require an authenticated session (requireAuth,
      // server.go) — MapLibre's own tile fetches otherwise carry no cookie at all, since
      // they don't go through api.ts's fetch wrapper. Scoped to just this app's own API
      // origin so the basemap's pmtiles archive, fonts and sprites (a different origin,
      // and not resources auth applies to anyway) aren't sent credentials they don't need.
      transformRequest: (url) => (url.startsWith(API_BASE_URL) ? { url, credentials: 'include' } : { url }),
      // MapLibre's own controls' labels (zoom, locate, the scale bar's units) in the page's
      // language; keys MapLibre has but this app's controls never show stay its English.
      locale: {
        'Map.Title': t('maplibre.map'),
        'NavigationControl.ZoomIn': t('maplibre.zoom_in'),
        'NavigationControl.ZoomOut': t('maplibre.zoom_out'),
        'GeolocateControl.FindMyLocation': t('maplibre.find_location'),
        'GeolocateControl.LocationNotAvailable': t('maplibre.location_unavailable'),
        'AttributionControl.ToggleAttribution': t('maplibre.toggle_attribution'),
        'ScaleControl.Meters': t('unit.m'),
        'ScaleControl.Kilometers': t('unit.km'),
        'ScaleControl.Feet': t('unit.ft'),
        'ScaleControl.Miles': t('unit.mi'),
      },
    });

    instance.addControl(new NavigationControl({ showCompass: false }), 'top-right');
    // One-shot "find me", not continuous tracking: trackUserLocation stays false
    // deliberately, since a "follow me as I move" mode reads as live GPS tracking, which
    // §1.1 of the business plan draws a hard line against ("no start button, no live GPS").
    // This is a map-camera convenience only — a single browser geolocation lookup on click,
    // nothing persisted or sent to the backend; showUserLocation just marks the found point
    // with the usual blue dot so the fly-to has a visible target to confirm against.
    instance.addControl(
      new GeolocateControl({ positionOptions: { enableHighAccuracy: true }, trackUserLocation: false, showUserLocation: true }),
      'top-right',
    );
    // Seeded from whatever scaleUnit is at construction time; the effect below keeps it in
    // sync with every later change (a Settings-page Country save), via setUnit rather than
    // recreating the control.
    const scaleControl = new ScaleControl({ unit: scaleUnit });
    scaleControlRef.current = scaleControl;
    instance.addControl(scaleControl, 'bottom-left');
    instance.addControl(
      new AttributionControl({ compact: false }),
      'bottom-right',
    );

    instance.once('load', () => {
      if (cancelled) return;
      setMap(instance);
      // Handle for the headless verification run. Dev-only: the production bundle
      // should not hand a live WebGL map to anything that asks.
      if (import.meta.env.DEV) {
        (window as unknown as { __holdmytrack?: MapLibreMap }).__holdmytrack = instance;
      }
    });

    return () => {
      cancelled = true;
      setMap(null);
      instance.remove();
    };
  }, [container]);

  // Separate from the mount effect above on purpose: scaleUnit can change at any point after
  // the map already exists (a Settings-page Country save), and ScaleControl's own setUnit is
  // exactly the update path MapLibre provides for it — no need to tear down and recreate the
  // control, or the whole map, just to change which unit its label reads in.
  useEffect(() => {
    scaleControlRef.current?.setUnit(scaleUnit);
  }, [scaleUnit]);

  return map;
}
