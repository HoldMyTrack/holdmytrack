-- A pending ingest job's raw payload, as the request delivered it (IMPLEMENTATION.md §4.1 step 1).
-- The request stores it here rather than in object storage so it can answer at once; the worker
-- writes it to the job's raw_payload_key and clears this before ingesting. Its own column rather
-- than part of payload, which the import queries read on every row.

ALTER TABLE jobs ADD COLUMN raw BYTEA;
