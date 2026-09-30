-- D-70: the product setup's user types become real. A membership may carry the user
-- type it was invited as (e.g. "agent"); the type limits which roles it can hold.
-- Additive only: existing memberships keep NULL (no user type).
ALTER TABLE crm.memberships ADD COLUMN IF NOT EXISTS user_type text;
