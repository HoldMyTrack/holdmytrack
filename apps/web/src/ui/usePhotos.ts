import { useCallback, useEffect, useState } from 'react';
import { listPhotos, type Photo } from '../api';

/** Whose photos to show: one activity's, a Story's, or none. */
export type PhotoScope = { activity: string } | { story: string } | null;

export interface PhotosState {
  photos: readonly Photo[];
  error: string | null;
  reload: () => void;
  /** Puts a photo a PATCH answered with in place of the one in hand, without a refetch. */
  replace: (photo: Photo) => void;
  remove: (id: string) => void;
}

function scopeKey(scope: PhotoScope): string | null {
  if (scope === null) return null;
  return 'activity' in scope ? `activity:${scope.activity}` : `story:${scope.story}`;
}

/**
 * The photos (FR-16) of the focused activity or the open Story — `GET /v1/photos`, refetched
 * whenever the scope or `version` changes or `reload` is called (after an upload). `version` is
 * whatever else should refetch them: MapView's `trackMetricsVersion`, bumped when a track edit
 * or a Private location change has been reprocessed — either can move a photo, or take it off
 * the map — and an open Story's members. A new scope starts empty
 * rather than showing the last one's photos while it loads.
 */
export function usePhotos(scope: PhotoScope, version: number | string = 0): PhotosState {
  const key = scopeKey(scope);
  const [photos, setPhotos] = useState<readonly Photo[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [loadedKey, setLoadedKey] = useState<string | null>(null);
  const [nonce, setNonce] = useState(0);

  useEffect(() => {
    setError(null);
    if (key === null) {
      setPhotos([]);
      setLoadedKey(null);
      return;
    }
    const [kind, id] = key.split(':') as ['activity' | 'story', string];
    const controller = new AbortController();
    listPhotos(kind === 'activity' ? { activity: id } : { story: id }, controller.signal)
      .then((list) => {
        setPhotos(list);
        setLoadedKey(key);
      })
      .catch((err: unknown) => {
        if (controller.signal.aborted) return;
        setPhotos([]);
        setError(err instanceof Error ? err.message : String(err));
      });
    return () => controller.abort();
  }, [key, nonce, version]);

  const reload = useCallback(() => setNonce((n) => n + 1), []);
  const replace = useCallback((photo: Photo) => setPhotos((list) => list.map((p) => (p.id === photo.id ? photo : p))), []);
  const remove = useCallback((id: string) => setPhotos((list) => list.filter((p) => p.id !== id)), []);
  return { photos: loadedKey === key ? photos : [], error, reload, replace, remove };
}
