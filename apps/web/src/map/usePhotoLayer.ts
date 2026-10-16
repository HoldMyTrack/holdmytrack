import { useCallback, useEffect, useMemo, useState } from 'react';
import type { Map as MapLibreMap } from 'maplibre-gl';
import type { MapMode } from './mapMode';
import { GROUP_MAX_ZOOM, photoGroupAction, photoMarkerItems, photoScopeFor } from './photoMarkerItems';
import { usePhotoMarkers, type PhotoMarkerItem, type PhotoMarkerOverlay } from './photos';
import { API_BASE_URL, type Photo } from '../api';
import { usePhotos, type PhotosState } from '../ui/usePhotos';

export interface PhotoLayerInputs {
  mode: MapMode;
  editWindowIds: readonly string[] | null;
  focusedId: string | null;
  storyId: string | null;
  /** Changes when the photos may have moved under the scope: a reprocess, a Story's members. */
  version: string;
  /** The Edit window's Track tab has the map, which hides the photos. */
  trackTabShown: boolean;
  mapHiddenIds: ReadonlySet<string>;
}

export interface PhotoLayer {
  photos: PhotosState;
  /** The photos in the open popup, saved ones only, and which is shown. */
  popup: { photos: Photo[]; index: number } | null;
  showPopupIndex: (index: number) => void;
  closePopup: () => void;
  /** The Photos tab's unsaved changes as they alter the markers (PhotosTab.tsx). */
  setOverlay: (overlay: PhotoMarkerOverlay | null) => void;
}

/** Photos on the map (FR-16): the scope's photos as markers on their routes, and the popup a
 *  marker's click opens. The popup closes when the scope changes. */
export function usePhotoLayer(map: MapLibreMap | null, inputs: PhotoLayerInputs): PhotoLayer {
  const { mode, editWindowIds, focusedId, storyId, version, trackTabShown, mapHiddenIds } = inputs;
  const scope = useMemo(() => photoScopeFor(mode, editWindowIds, focusedId, storyId), [mode, editWindowIds, focusedId, storyId]);
  const photos = usePhotos(scope, version);
  const [openGroup, setOpenGroup] = useState<{ ids: string[]; index: number } | null>(null);
  const openPhotos = useMemo(
    () => (openGroup ? openGroup.ids.flatMap((id) => photos.photos.filter((p) => p.id === id)) : []),
    [openGroup, photos.photos],
  );
  const [overlay, setOverlay] = useState<PhotoMarkerOverlay | null>(null);
  const scopeKey = scope === null ? null : JSON.stringify(scope);
  useEffect(() => setOpenGroup(null), [scopeKey]);

  const markers = useMemo<PhotoMarkerItem[]>(
    () => photoMarkerItems(scope, trackTabShown, mapHiddenIds, photos.photos, overlay, API_BASE_URL),
    [scope, trackTabShown, mapHiddenIds, photos.photos, overlay],
  );
  // A marker's click: a spread group zooms to fit, else the popup opens on its first photo.
  const openMarker = useCallback(
    (ids: string[]) => {
      if (!map) return;
      const action = photoGroupAction(markers.filter((item) => ids.includes(item.id)));
      if (action !== 'open') {
        map.fitBounds(action.fit, { padding: 80, maxZoom: GROUP_MAX_ZOOM });
        return;
      }
      setOpenGroup({ ids, index: 0 });
    },
    [map, markers],
  );
  const index = openGroup && openPhotos.length > 0 ? Math.min(openGroup.index, openPhotos.length - 1) : 0;
  const openPhotoId = openGroup && openPhotos.length > 0 ? openPhotos[index]!.id : null;
  usePhotoMarkers(map, markers, {
    onOpen: openMarker,
    activeId: overlay?.activeId ?? openPhotoId,
    loneId: overlay?.activeId ?? null,
  });

  return {
    photos,
    popup: openGroup && openPhotos.length > 0 ? { photos: openPhotos, index } : null,
    showPopupIndex: (next) => setOpenGroup((group) => (group ? { ...group, index: next } : group)),
    closePopup: () => setOpenGroup(null),
    setOverlay,
  };
}
