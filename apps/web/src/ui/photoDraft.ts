import type { Photo } from '../api';
import type { PreparedPhoto } from './photoPrep';

/**
 * The Photos tab's unsaved changes (FR-16.6) — what the Edit window's one Save writes, after the
 * activity's fields and before its track edit, and what its Cancel throws away. Nothing here has
 * reached the server: a new photo is prepared in the browser and its place settled by the
 * placement check (`POST /v1/photos/place`) or by the user, and goes up only on Save.
 */

/** A picked photo, not yet uploaded. `routeAt` (epoch seconds) is null while it waits for the
 *  user to place it. `thumbSrc` is an object URL for its local thumbnail. */
export interface NewPhoto {
  key: string;
  name: string;
  prepared: PreparedPhoto;
  thumbSrc: string;
  routeAt: number | null;
  caption: string;
}

/** A saved photo's pending changes; a field present is a change. */
export interface PhotoChange {
  routeAt?: number;
  caption?: string;
}

export interface PhotoDraft {
  added: readonly NewPhoto[];
  changed: Readonly<Record<string, PhotoChange>>;
  deleted: readonly string[];
}

export const EMPTY_DRAFT: PhotoDraft = { added: [], changed: {}, deleted: [] };

/** Epoch seconds as RFC 3339, the way the API takes a `route_at`. */
export function isoSeconds(seconds: number): string {
  return new Date(seconds * 1000).toISOString().replace(/\.\d{3}Z$/, 'Z');
}

export function draftIsEmpty(draft: PhotoDraft): boolean {
  return draft.added.length === 0 && Object.keys(draft.changed).length === 0 && draft.deleted.length === 0;
}

/** How many writes Save will make for the draft. */
export function draftSize(draft: PhotoDraft): number {
  const changed = Object.keys(draft.changed).filter((id) => !draft.deleted.includes(id));
  return draft.added.length + changed.length + draft.deleted.length;
}

/** One row of the tab's list: a saved photo as the draft would leave it, or a new one. */
export type DraftRow =
  | { kind: 'saved'; id: string; photo: Photo; routeAt: number; caption: string; deleted: boolean }
  | { kind: 'new'; id: string; photo: NewPhoto; routeAt: number; caption: string };

/** The photos as Save would leave them, in route order — saved ones (a deleted one kept, marked,
 *  so it can be brought back) and new ones with a place. New ones still waiting aren't rows. */
export function draftRows(photos: readonly Photo[], draft: PhotoDraft): DraftRow[] {
  const rows: DraftRow[] = photos.map((photo) => {
    const change = draft.changed[photo.id] ?? {};
    return {
      kind: 'saved',
      id: photo.id,
      photo,
      routeAt: change.routeAt ?? Date.parse(photo.routeAt) / 1000,
      caption: change.caption ?? photo.caption ?? '',
      deleted: draft.deleted.includes(photo.id),
    };
  });
  for (const photo of draft.added) {
    if (photo.routeAt !== null) rows.push({ kind: 'new', id: photo.key, photo, routeAt: photo.routeAt, caption: photo.caption });
  }
  return rows.sort((a, b) => a.routeAt - b.routeAt);
}

/** New photos still waiting for the user to place them — Save can't go ahead while there are any. */
export function waitingPhotos(draft: PhotoDraft): NewPhoto[] {
  return draft.added.filter((p) => p.routeAt === null);
}
