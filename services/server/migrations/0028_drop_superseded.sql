-- Deploy: maintenance — drops activities.superseded_by, which the running release reads on every list, total and tile query.
-- ADR-0039: nothing is hidden as a duplicate any more. The copies hidden before go: their
-- photos and Story memberships move to the copy that was kept (photos had already moved when
-- they were hidden, so this is for any added since), their rows go (the cascade takes their
-- streams, masks and country/region rows), and a `remove_activity_objects` job per account
-- removes what SQL can't reach in object storage: each one's masks, and its raw file when no
-- remaining activity shares it. Every read already left them out, so no tile changes.
CREATE TEMP TABLE gone ON COMMIT DROP AS
SELECT a.id, a.user_id, a.superseded_by AS kept,
       CASE WHEN a.raw_payload_key IS NOT NULL AND NOT EXISTS (
              SELECT 1 FROM activities o
              WHERE o.raw_payload_key = a.raw_payload_key AND o.superseded_by IS NULL)
            THEN a.raw_payload_key END AS lone_raw_key
FROM activities a
WHERE a.superseded_by IS NOT NULL;

UPDATE activity_photos p SET activity_id = g.kept
FROM gone g WHERE p.activity_id = g.id;

INSERT INTO story_activities (story_id, activity_id)
SELECT sa.story_id, g.kept
FROM story_activities sa JOIN gone g ON g.id = sa.activity_id
ON CONFLICT DO NOTHING;

INSERT INTO jobs (kind, user_id, payload)
SELECT 'remove_activity_objects', user_id, json_build_object(
         'activity_ids', array_agg(id::text ORDER BY id),
         'raw_keys', COALESCE(array_agg(DISTINCT lone_raw_key) FILTER (WHERE lone_raw_key IS NOT NULL), '{}'))
FROM gone
GROUP BY user_id;

DELETE FROM activities WHERE id IN (SELECT id FROM gone);

DROP INDEX idx_activities_live;
ALTER TABLE activities DROP COLUMN superseded_by;
