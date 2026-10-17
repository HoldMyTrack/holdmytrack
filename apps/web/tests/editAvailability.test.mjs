import { test } from 'node:test';
import assert from 'node:assert/strict';
import { editPhotosBlocker, editTrackBlocker } from '../src/ui/editAvailability.ts';

const activity = (overrides = {}) => ({ id: 'a', pending: false, private: false, bbox: [0, 0, 1, 1], ...overrides });

test('the Track tab needs one finished, visible track', () => {
  assert.equal(editTrackBlocker(null), 'activities.edit_track_check_one');
  assert.equal(editTrackBlocker([activity(), activity({ id: 'b' })]), 'activities.edit_track_check_one');
  assert.equal(editTrackBlocker([activity({ pending: true })]), 'activities.edit_track_processing');
  assert.equal(editTrackBlocker([activity({ private: true })]), 'activities.private_title');
  assert.equal(editTrackBlocker([activity({ bbox: null })]), 'activities.no_track');
  assert.equal(editTrackBlocker([activity()]), null);
});

test('photos need one activity with a track, pending or not', () => {
  assert.equal(editPhotosBlocker([activity(), activity({ id: 'b' })]), 'photos.check_one');
  assert.equal(editPhotosBlocker([activity({ private: true })]), 'activities.private_title');
  assert.equal(editPhotosBlocker([activity({ bbox: null })]), 'photos.no_track');
  assert.equal(editPhotosBlocker([activity({ pending: true })]), null);
});
