-- A failed ingest job's reason as a code (ingest.FailureCode), which the handlers that list
-- jobs render from the catalog in the reader's language (IMPLEMENTATION.md §4.21). last_error
-- keeps the English diagnostic for logs and debugging; before this, it was what the apps showed.
ALTER TABLE jobs ADD COLUMN error_code VARCHAR(32);

-- The failures already stored, classified from the English text the worker wrote for each.
UPDATE jobs SET error_code = CASE
    WHEN last_error LIKE 'ingest: parse: parse: unrecognized extension%' THEN 'unsupported_format'
    WHEN last_error LIKE '%no track points found'
      OR last_error LIKE '%no track points with position found'
      OR last_error LIKE '%no record messages with position found' THEN 'no_track'
    WHEN last_error LIKE 'ingest: parse:%' THEN 'unreadable_file'
    WHEN last_error LIKE '%no timestamps%' THEN 'no_timestamps'
    WHEN last_error LIKE 'ingest: fewer than 2 points recorded%' THEN 'too_few_points'
    ELSE 'internal'
END
WHERE kind = 'ingest' AND state = 'failed';
