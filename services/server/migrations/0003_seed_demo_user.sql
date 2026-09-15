-- Placeholder pending real accounts. There is no auth system yet, but activities.user_id is
-- NOT NULL, so uploads need a valid user to reference.
-- This is a deliberate, temporary stand-in (services/server/README.md, "Open decisions"),
-- not a design to build further features on. The fixed UUID matches
-- internal/httpapi.PlaceholderUserID.
INSERT INTO users (id, email, privacy_trim_m)
VALUES ('00000000-0000-0000-0000-000000000001', 'demo@fitmap.invalid', 200)
ON CONFLICT (id) DO NOTHING;
