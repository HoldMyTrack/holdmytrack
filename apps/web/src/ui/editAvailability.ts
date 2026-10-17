/** Why the Edit window's Track or Photos tab can't be used for its activities, as a message key, or null. */
import type { Activity } from '../api';
import type { MessageKey } from '../i18n/en';

/** The Track tab edits one activity with a finished, visible track. */
export function editTrackBlocker(activities: readonly Activity[] | null): MessageKey | null {
  if (activities === null || activities.length !== 1) return 'activities.edit_track_check_one';
  const [a] = activities;
  if (a!.pending) return 'activities.edit_track_processing';
  if (a!.private) return 'activities.private_title';
  if (a!.bbox === null) return 'activities.no_track';
  return null;
}

/** Photos belong to one activity and need a track to sit on (FR-16.1). */
export function editPhotosBlocker(activities: readonly Activity[] | null): MessageKey | null {
  if (activities === null || activities.length !== 1) return 'photos.check_one';
  const [a] = activities;
  if (a!.private) return 'activities.private_title';
  if (a!.bbox === null) return 'photos.no_track';
  return null;
}
