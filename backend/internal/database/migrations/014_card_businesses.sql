-- 014: One business record per GSTIN, whoever brings it in.
-- A business that isn't registered on CardFlow yet is created the first time
-- anyone scans (or types in) its card. It has no owner until the person whose
-- phone is printed on the card signs up, then it becomes theirs ("claimed").
-- Every later scan links to the same record instead of storing a copy.
-- Non-destructive: only relaxes a constraint and adds nullable columns.

-- Unclaimed businesses have no owner yet.
ALTER TABLE businesses ALTER COLUMN owner_user_id DROP NOT NULL;

-- owner = registered by its owner in the app; card = created from a scanned / typed card.
ALTER TABLE businesses ADD COLUMN IF NOT EXISTS source VARCHAR(20) NOT NULL DEFAULT 'owner';
-- The person and phone printed on the card: the phone is how the owner is matched at sign-up.
ALTER TABLE businesses ADD COLUMN IF NOT EXISTS contact_name VARCHAR(150);
ALTER TABLE businesses ADD COLUMN IF NOT EXISTS contact_designation VARCHAR(150);
ALTER TABLE businesses ADD COLUMN IF NOT EXISTS contact_phone VARCHAR(20);
ALTER TABLE businesses ADD COLUMN IF NOT EXISTS created_by_user_id UUID REFERENCES users(id) ON DELETE SET NULL;
ALTER TABLE businesses ADD COLUMN IF NOT EXISTS claimed_at TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_businesses_unclaimed_phone
    ON businesses (contact_phone) WHERE owner_user_id IS NULL AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_businesses_gstin_upper
    ON businesses (upper(gstin)) WHERE gstin IS NOT NULL AND deleted_at IS NULL;

-- Guarantees one live record per GSTIN (skipped with a notice if old
-- duplicates exist; the app still looks up by GSTIN before inserting).
DO $$
BEGIN
    CREATE UNIQUE INDEX IF NOT EXISTS uq_businesses_gstin_live
        ON businesses (upper(gstin)) WHERE gstin IS NOT NULL AND deleted_at IS NULL;
EXCEPTION WHEN others THEN
    RAISE NOTICE 'uq_businesses_gstin_live not created: %', SQLERRM;
END $$;
