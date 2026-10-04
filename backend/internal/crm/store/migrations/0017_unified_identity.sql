-- D-93: one identity for the app and the CRM. Customers sign in with a phone number and
-- an SMS code; the native app holds a bearer session instead of a cookie.

-- How the session token travels: an HTTP-only cookie (web) or a bearer token (native app).
ALTER TABLE crm.sessions ADD COLUMN IF NOT EXISTS transport text NOT NULL DEFAULT 'cookie'
  CHECK (transport IN ('cookie', 'bearer'));

-- Where an identity came from: NULL (created in the CRM), 'phone_signup' (signed up with a
-- phone number) or 'app_backfill' (an app user from before the identities were unified).
ALTER TABLE crm.identities ADD COLUMN IF NOT EXISTS source text;

CREATE INDEX IF NOT EXISTS otp_challenges_destination_idx
  ON crm.otp_challenges (destination, purpose, created_at DESC);

-- Platform-wide settings the owner can change without a deploy (key → JSON value).
CREATE TABLE IF NOT EXISTS crm.platform_settings (
  key        text PRIMARY KEY,
  value      jsonb NOT NULL,
  updated_by uuid REFERENCES crm.identities,
  updated_at timestamptz NOT NULL DEFAULT now()
);

-- The app's profile row points at its identity. public.users keeps its id, so cards,
-- businesses and tickets are untouched; the phone number stops being the login record.
DO $$
BEGIN
  IF to_regclass('public.users') IS NOT NULL THEN
    ALTER TABLE public.users ADD COLUMN IF NOT EXISTS identity_id uuid REFERENCES crm.identities;
    CREATE UNIQUE INDEX IF NOT EXISTS uq_users_identity ON public.users (identity_id) WHERE identity_id IS NOT NULL;
  END IF;
END $$;
