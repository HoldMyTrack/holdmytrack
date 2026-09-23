-- Edit track (IMPLEMENTATION.md §4.7.7): a user's chop/cut/delete-point edits to an activity's
-- recorded points. Stored as a spec in unix-millisecond timestamps, never as the edited points
-- themselves: the raw payload stays untouched, and every (re)processing replays the spec on
-- top of the freshly parsed and privacy-trimmed points. Timestamps rather than point indices,
-- because indices shift whenever the privacy trim does and timestamps don't. NULL means the
-- track has never been edited (or was reset to its original).
ALTER TABLE activities ADD COLUMN track_edit JSONB;

-- True from the moment an edit is accepted until the `edit_track` job has reprocessed the
-- activity (or failed). The Activities panel shows such a row as Pending and won't act on it.
ALTER TABLE activities ADD COLUMN edit_pending BOOLEAN NOT NULL DEFAULT false;
