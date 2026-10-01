import { useEffect, useRef } from 'react';
import { Marker, type Map as MapLibreMap } from 'maplibre-gl';

/**
 * Photo markers (FR-16, IMPLEMENTATION.md §4.27): each photo of the selected activity or the
 * open Story, as its own thumbnail in a round frame, at its point on the track.
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
  onOpen: (id: string) => void;
  /** The photo open in its popup, or being moved in the Photos tab: drawn larger and on top. */
  activeId?: string | null;
}

function markerElement(item: PhotoMarkerItem, onOpen: (id: string) => void): HTMLElement {
  const el = document.createElement('button');
  el.type = 'button';
  el.className = PHOTO_MARKER_CLASS;
  el.dataset.photoId = item.id;
  if (item.caption) el.title = item.caption;
  const img = document.createElement('img');
  img.src = item.thumbSrc;
  img.alt = item.caption ?? '';
  img.decoding = 'async';
  img.draggable = false;
  el.append(img);
  el.addEventListener('click', (event) => {
    event.stopPropagation();
    onOpen(item.id);
  });
  return el;
}

/** Keeps one marker per item on the map, in step with `items`. */
export function usePhotoMarkers(map: MapLibreMap | null, items: readonly PhotoMarkerItem[], { onOpen, activeId = null }: PhotoMarkerOptions): void {
  const markers = useRef(new Map<string, { marker: Marker; item: PhotoMarkerItem }>());
  // Read through a ref, so a new callback each render doesn't rebuild every marker.
  const onOpenRef = useRef(onOpen);
  onOpenRef.current = onOpen;

  useEffect(() => {
    if (!map) return;
    const current = markers.current;
    const keep = new Set(items.map((p) => p.id));
    for (const [id, { marker }] of current) {
      if (!keep.has(id)) {
        marker.remove();
        current.delete(id);
      }
    }
    for (const item of items) {
      const existing = current.get(item.id);
      if (existing && existing.item.thumbSrc === item.thumbSrc && existing.item.caption === item.caption) {
        existing.marker.setLngLat([item.lon, item.lat]);
        existing.item = item;
        continue;
      }
      existing?.marker.remove();
      const marker = new Marker({ element: markerElement(item, (id) => onOpenRef.current(id)) })
        .setLngLat([item.lon, item.lat])
        .addTo(map);
      current.set(item.id, { marker, item });
    }
  }, [map, items]);

  useEffect(() => {
    for (const [id, { marker }] of markers.current) {
      marker.getElement().classList.toggle(`${PHOTO_MARKER_CLASS}--active`, id === activeId);
    }
  }, [activeId, items]);

  // Gone with the map view itself.
  useEffect(() => {
    const current = markers.current;
    return () => {
      for (const { marker } of current.values()) marker.remove();
      current.clear();
    };
  }, [map]);
}
