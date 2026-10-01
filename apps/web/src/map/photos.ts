import { useEffect, useRef } from 'react';
import { Marker, type Map as MapLibreMap } from 'maplibre-gl';
import { API_BASE_URL, type Photo } from '../api';

/**
 * Photo markers (FR-16, IMPLEMENTATION.md §4.27): each placed photo of the focused activity or
 * the open Story, as its own thumbnail in a round frame, at its point on the track.
 *
 * HTML markers rather than a symbol layer: there are tens of them, not thousands, each shows
 * its own image (a symbol layer would need every thumbnail added to the style as an icon, and
 * added again after every `setStyle`), and a marker can be dragged. Markers live outside the
 * style, so a theme's `setStyle` leaves them alone and `reattachOverlays` has nothing to do.
 */

export const PHOTO_MARKER_CLASS = 'photo-marker';

export interface PhotoMarkerOptions {
  onOpen: (id: string) => void;
  /** The photo the viewer has open, drawn larger and on top. */
  activeId?: string | null;
}

function markerElement(photo: Photo, onOpen: (id: string) => void): HTMLElement {
  const el = document.createElement('button');
  el.type = 'button';
  el.className = PHOTO_MARKER_CLASS;
  el.dataset.photoId = photo.id;
  if (photo.caption) el.title = photo.caption;
  const img = document.createElement('img');
  img.src = API_BASE_URL + photo.thumbUrl;
  img.alt = photo.caption ?? '';
  img.decoding = 'async';
  img.draggable = false;
  el.append(img);
  el.addEventListener('click', (event) => {
    event.stopPropagation();
    onOpen(photo.id);
  });
  return el;
}

/** Keeps one marker per placed photo on the map, in step with `photos`. */
export function usePhotoMarkers(map: MapLibreMap | null, photos: readonly Photo[], { onOpen, activeId = null }: PhotoMarkerOptions): void {
  const markers = useRef(new Map<string, { marker: Marker; photo: Photo }>());
  // Read through a ref, so a new callback each render doesn't rebuild every marker.
  const onOpenRef = useRef(onOpen);
  onOpenRef.current = onOpen;

  useEffect(() => {
    if (!map) return;
    const current = markers.current;
    const placed = photos.filter((p) => p.lon !== null && p.lat !== null);
    const keep = new Set(placed.map((p) => p.id));
    for (const [id, { marker }] of current) {
      if (!keep.has(id)) {
        marker.remove();
        current.delete(id);
      }
    }
    for (const photo of placed) {
      const existing = current.get(photo.id);
      if (existing && existing.photo.thumbUrl === photo.thumbUrl && existing.photo.caption === photo.caption) {
        existing.marker.setLngLat([photo.lon!, photo.lat!]);
        existing.photo = photo;
        continue;
      }
      existing?.marker.remove();
      const marker = new Marker({ element: markerElement(photo, (id) => onOpenRef.current(id)) })
        .setLngLat([photo.lon!, photo.lat!])
        .addTo(map);
      current.set(photo.id, { marker, photo });
    }
  }, [map, photos]);

  useEffect(() => {
    for (const [id, { marker }] of markers.current) {
      marker.getElement().classList.toggle(`${PHOTO_MARKER_CLASS}--active`, id === activeId);
    }
  }, [activeId, photos]);

  // Gone with the map view itself.
  useEffect(() => {
    const current = markers.current;
    return () => {
      for (const { marker } of current.values()) marker.remove();
      current.clear();
    };
  }, [map]);
}
