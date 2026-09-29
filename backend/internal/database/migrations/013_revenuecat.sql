-- 013: RevenueCat subscriptions (replaces the previous checkout flow).
-- Non-destructive: the pre-RevenueCat subscription_payments table and its rows are
-- kept untouched as historical records; existing paid users keep their access.

-- RevenueCat product ids are longer than the old 3m/6m/12m/lifetime codes.
-- Guarded so re-running on every boot doesn't re-lock the users table.
DO $$
BEGIN
    IF (SELECT character_maximum_length FROM information_schema.columns
        WHERE table_schema = current_schema() AND table_name = 'users' AND column_name = 'subscription_plan_id') < 120 THEN
        ALTER TABLE users ALTER COLUMN subscription_plan_id TYPE VARCHAR(120);
    END IF;
END $$;

-- FREE | ACTIVE | CANCELLED | EXPIRED | BILLING_ISSUE (see internal/billing).
ALTER TABLE users ADD COLUMN IF NOT EXISTS subscription_status VARCHAR(20) NOT NULL DEFAULT 'FREE';
-- revenuecat | crm_grant | legacy — who granted the current access, so a
-- RevenueCat sync never wipes access that was granted some other way.
ALTER TABLE users ADD COLUMN IF NOT EXISTS subscription_source VARCHAR(20);
ALTER TABLE users ADD COLUMN IF NOT EXISTS subscription_store VARCHAR(30);
ALTER TABLE users ADD COLUMN IF NOT EXISTS subscription_will_renew BOOLEAN NOT NULL DEFAULT false;
ALTER TABLE users ADD COLUMN IF NOT EXISTS subscription_management_url TEXT;
-- Timestamp of the newest RevenueCat event applied — older, out-of-order
-- webhook deliveries are ignored.
ALTER TABLE users ADD COLUMN IF NOT EXISTS subscription_event_at TIMESTAMPTZ;
ALTER TABLE users ADD COLUMN IF NOT EXISTS subscription_updated_at TIMESTAMPTZ;

-- Users who paid before the migration keep their access until it expires.
UPDATE users
SET subscription_source = 'legacy',
    subscription_status = CASE
        WHEN subscription_expires_at IS NULL OR subscription_expires_at > NOW() THEN 'ACTIVE'
        ELSE 'EXPIRED'
    END
WHERE is_subscribed = true AND subscription_source IS NULL;

-- Every RevenueCat webhook delivery, stored once per event id (idempotency +
-- audit log). Retries of the same event id are detected here.
CREATE TABLE IF NOT EXISTS revenuecat_events (
    event_id        VARCHAR(100) PRIMARY KEY,
    event_type      VARCHAR(40)  NOT NULL,
    app_user_id     VARCHAR(200),
    user_id         UUID REFERENCES users(id) ON DELETE SET NULL,
    product_id      VARCHAR(120),
    entitlement_ids TEXT[],
    store           VARCHAR(30),
    environment     VARCHAR(20),
    price           NUMERIC(12, 4),
    currency        VARCHAR(10),
    transaction_id  VARCHAR(200),
    event_at        TIMESTAMPTZ,
    expires_at      TIMESTAMPTZ,
    payload         JSONB        NOT NULL,
    status          VARCHAR(20)  NOT NULL DEFAULT 'received', -- received | processing | processed | ignored | failed
    error           TEXT,
    attempts        INTEGER      NOT NULL DEFAULT 0,
    received_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    processed_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_revenuecat_events_user ON revenuecat_events(user_id, event_at DESC);
