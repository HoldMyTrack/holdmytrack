-- When the account's last data export was built (IMPLEMENTATION.md §4.29): an account may ask
-- for one a week, and an earlier export's own row is expired and swept when a new one is asked
-- for, so it can't be read back from exports.
ALTER TABLE users ADD COLUMN last_export_ready_at TIMESTAMPTZ;
UPDATE users u SET last_export_ready_at = (SELECT max(ready_at) FROM exports e WHERE e.user_id = u.id);
