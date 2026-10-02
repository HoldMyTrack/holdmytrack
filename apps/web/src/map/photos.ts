import { useEffect, useRef, useState } from 'react';
import { Marker, type Map as MapLibreMap } from 'maplibre-gl';
import { clusterPoints, CLUSTER_RADIUS_PX } from './photoClusters';

/**
 * Photo markers (FR-16, IMPLEMENTATION.md §4.27): each photo of the selected activity or the
 * open Story, as its own thumbnail in a round frame, at its point on the track — or, where
 * several would overlap on screen, one marker for the group: its first photo, stacked, with a
 * count (photoClusters.ts). Groups are worked out again after every move of the map, so zooming
 * in splits them.
 *
 * HTML markers rather than a symbol layer: there are tens of them, not thousands, and each shows
 * its own image (a symbol layer would need every thumbnail added to the style as an icon, and
 * added again after every `setStyle`). Markers live outside the style, so a theme's `setStyle`
 * leaves them alone and `reattachOverlays` has nothing to do.
 */

export const PHOTO_MARKER_CLASS = 'photo-marker';

/** One marker: a saved photo, or one being placed in the Photos tab before it's uploaded. */
export interface PhotoMarkerItem {
  id: string;
  lon: number;
  lat: number;
  /** The thumbnail's full URL. */
  thumbSrc: string;
  caption: string | null;
}

/** How the Photos tab's unsaved changes alter the markers: photos moved or added (`upserts`,
 *  replacing a saved one with the same id), photos to be deleted (`hidden`), and the one in hand
 *  (`activeId`, drawn larger). */
export interface PhotoMarkerOverlay {
  upserts: readonly PhotoMarkerItem[];
  hidden: readonly string[];
  activeId: string | null;
}

export interface PhotoMarkerOptions {
  /** A marker was clicked: its photo, or its group's photos in route order. */
  onOpen: (ids: string[]) => void;
  /** The photo open in its popup, or in hand in the Photos tab: its marker is drawn larger. */
  activeId?: string | null;
  /** A photo that always keeps a marker of its own, never grouped — the one being moved along
   *  the route in the Photos tab, so the slider's preview stays in sight. */
  loneId?: string | null;
}

function markerElement(item: PhotoMarkerItem, ids: string[], onOpen: (ids: string[]) => void): HTMLElement {
  const el = document.createElement('button');
  el.type = 'button';
  el.className = ids.length > 1 ? `${PHOTO_MARKER_CLASS} ${PHOTO_MARKER_CLASS}--group` : PHOTO_MARKER_CLASS;
  el.dataset.photoId = item.id;
  if (ids.length === 1 && item.caption) el.title = item.caption;
  const img = document.createElement('img');
  img.src = item.thumbSrc;
  img.alt = item.caption ?? '';
  img.decoding = 'async';
  img.draggable = false;
  el.append(img);
  if (ids.length > 1) {
    const count = document.createElement('span');
    count.className = `${PHOTO_MARKER_CLASS}__count`;
    count.textContent = String(ids.length);
    el.append(count);
  }
  el.addEventListener('click', (event) => {
    event.stopPropagation();
    onOpen(ids);
  });
  return el;
}

/** Keeps the markers on the map in step with `items` (in route order) and the zoom. */
export function usePhotoMarkers(
  map: MapLibreMap | null,
  items: readonly PhotoMarkerItem[],
  { onOpen, activeId = null, loneId = null }: PhotoMarkerOptions,
): void {
  // One marker per group, keyed by its photos' ids.
  const markers = useRef(new Map<string, { marker: Marker; item: PhotoMarkerItem; ids: string[] }>());
  // Read through a ref, so a new callback each render doesn't rebuild every marker.
  const onOpenRef = useRef(onOpen);
  onOpenRef.current = onOpen;

  // Bumped after every move of the map: what overlaps depends on the zoom (and a tilt or turn).
  const [moves, setMoves] = useState(0);
  useEffect(() => {
    if (!map) return;
    const onMoveEnd = () => setMoves((n) => n + 1);
    map.on('moveend', onMoveEnd);
    return () => {
      map.off('moveend', onMoveEnd);
    };
  }, [map]);

  useEffect(() => {
    if (!map) return;
    const byId = new Map(items.map((item) => [item.id, item]));
    const grouped = clusterPoints(
      items
        .filter((item) => item.id !== loneId)
        .map((item) => {
          const p = map.project([item.lon, item.lat]);
          return { id: item.id, x: p.x, y: p.y };
        }),
      CLUSTER_RADIUS_PX,
    ).map((c) => c.ids);
    if (loneId !== null && byId.has(loneId)) grouped.push([loneId]);

    const current = markers.current;
    const wanted = new Map(grouped.map((ids) => [ids.join('|'), ids]));
    for (const [key, { marker }] of current) {
      if (!wanted.has(key)) {
        marker.remove();
        current.delete(key);
      }
    }
    for (const [key, ids] of wanted) {
      const item = byId.get(ids[0]!)!;
      const existing = current.get(key);
      if (existing && existing.item.thumbSrc === item.thumbSrc && existing.item.caption === item.caption) {
        existing.marker.setLngLat([item.lon, item.lat]);
        existing.item = item;
        continue;
      }
      existing?.marker.remove();
      const marker = new Marker({ element: markerElement(item, ids, (opened) => onOpenRef.current(opened)) })
        .setLngLat([item.lon, item.lat])
        .addTo(map);
      current.set(key, { marker, item, ids });
    }
  }, [map, items, loneId, moves]);

  useEffect(() => {
    for (const { marker, ids } of markers.current.values()) {
      marker.getElement().classList.toggle(`${PHOTO_MARKER_CLASS}--active`, activeId !== null && ids.includes(activeId));
    }
  }, [activeId, items, loneId, moves]);

  // Gone with the map view itself.
  useEffect(() => {
    const current = markers.current;
    return () => {
      for (const { marker } of current.values()) marker.remove();
      current.clear();
    };
  }, [map]);
}
