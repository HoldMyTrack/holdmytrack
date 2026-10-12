-- The demo account is recognized by its fixed id (internal/httpapi.DemoCustomerUserID), so the
-- expiry that used to mark it is dropped with its index.
DROP INDEX idx_users_demo_expiry;
ALTER TABLE users DROP COLUMN demo_expires_at;
