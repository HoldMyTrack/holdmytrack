-- The persistent, shared "Demo Customer" account every "Try Demo" visitor's session points
-- at (internal/httpapi.DemoCustomerUserID) — one pre-seeded account with a real, dense
-- activity history (internal/httpapi.SeedDemoCustomer), not a fresh row created and re-ingested
-- per visitor. demo_expires_at is set far in the future rather than NULL: auth.go's
-- `isDemo := demoExpiresAt != nil` needs it non-NULL to keep this account read-only
-- (requireNotDemo) and exempt from email verification (requireVerified), while staying far
-- enough out that internal/worker/demo_purge.go's `demo_expires_at < NOW()` sweep never
-- matches it — no purge-worker changes needed.
INSERT INTO users (id, email, demo_expires_at, privacy_trim_m)
VALUES ('22222222-2222-2222-2222-222222222222', 'demo-customer@holdmytrack.invalid', '9999-12-31 00:00:00+00', 200)
ON CONFLICT (id) DO NOTHING;
