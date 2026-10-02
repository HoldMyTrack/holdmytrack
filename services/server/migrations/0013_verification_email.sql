-- IMPLEMENTATION.md §3.16: a verification link confirms the address it was sent to, not
-- whatever the account's address happens to be when it is clicked. An email change is
-- pending on this row until then, and verifying applies it.
ALTER TABLE email_verifications ADD COLUMN email VARCHAR(255);
UPDATE email_verifications ev SET email = u.email FROM users u WHERE u.id = ev.user_id;
ALTER TABLE email_verifications ALTER COLUMN email SET NOT NULL;
