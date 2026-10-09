-- ADR-0039: `.zip` and Google Takeout import are gone, and the worker no longer runs `unpack`
-- jobs. Finished ones only fed the Upload menu's notes about an archive; one still pending would
-- never run. Its archive under imports/{user_id}/ stays in object storage.
DELETE FROM jobs WHERE kind = 'unpack';
