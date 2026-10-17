import { useCallback, useEffect, useMemo, useState } from 'react';
import type { Activity } from '../api';
import { editPhotosBlocker, editTrackBlocker } from '../ui/editAvailability';
import type { EditTab } from '../ui/EditActivityWindow';
import type { MessageKey } from '../i18n/en';

export interface EditWindowState {
  /** The window's activities, by id; null while it's closed. */
  ids: string[] | null;
  open: boolean;
  /** The Track tab's activity (§4.7.7), until the window closes. Every other track is hidden and TrackEditor owns the map. */
  editingId: string | null;
  tab: EditTab;
  setTab: (tab: EditTab) => void;
  /** The window's activities as listed; null while closed or once one has left the list. */
  activities: Activity[] | null;
  trackBlocker: MessageKey | null;
  photosBlocker: MessageKey | null;
  openFor: (group: Activity[]) => void;
  startTrack: (id: string) => void;
  /** Closes the window, returning the activity whose track was being edited, if any. */
  close: () => string | null;
}

/**
 * The Edit window over the toolbar's target. While it's open the panel is inert and the map's own
 * controls step aside. It closes when its activities leave the list (deleted elsewhere, say), or
 * the map would stay locked with no editor to close it, and it opens on its Activity tab.
 */
export function useEditWindow(listed: readonly Activity[], loading: boolean): EditWindowState {
  const [ids, setIds] = useState<string[] | null>(null);
  const [editingId, setEditingId] = useState<string | null>(null);
  const [tab, setTab] = useState<EditTab>('activity');
  const open = ids !== null;

  const activities = useMemo(() => {
    if (ids === null) return null;
    const group = listed.filter((a) => ids.includes(a.id));
    return group.length === ids.length ? group : null;
  }, [listed, ids]);

  useEffect(() => {
    if (open && activities === null && !loading) {
      setIds(null);
      setEditingId(null);
    }
  }, [open, activities, loading]);

  useEffect(() => {
    setTab('activity');
  }, [open]);

  const openFor = useCallback((group: Activity[]) => setIds(group.map((a) => a.id)), []);
  const startTrack = useCallback((id: string) => setEditingId(id), []);
  const close = useCallback(() => {
    setIds(null);
    setEditingId(null);
    return editingId;
  }, [editingId]);

  return {
    ids,
    open,
    editingId,
    tab,
    setTab,
    activities,
    trackBlocker: editTrackBlocker(activities),
    photosBlocker: editPhotosBlocker(activities),
    openFor,
    startTrack,
    close,
  };
}
