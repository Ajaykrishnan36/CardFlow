-- 012: Persist in-app support tickets (previously kept in memory and lost on restart).
-- Ajay's CRM reads these and writes replies back, so the app user sees the answer.
CREATE TABLE IF NOT EXISTS support_tickets (
    id          VARCHAR(20) PRIMARY KEY,
    user_id     UUID REFERENCES users(id) ON DELETE SET NULL,
    user_name   VARCHAR(150) NOT NULL DEFAULT '',
    user_phone  VARCHAR(20) NOT NULL DEFAULT '',
    user_role   VARCHAR(20) NOT NULL DEFAULT 'user',
    category    VARCHAR(40) NOT NULL DEFAULT 'general',
    subject     VARCHAR(200) NOT NULL,
    message     TEXT NOT NULL,
    status      VARCHAR(20) NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'in_progress', 'resolved')),
    admin_reply TEXT,
    replied_at  TIMESTAMPTZ,
    replied_by  VARCHAR(150),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_support_tickets_user ON support_tickets(user_id);
CREATE INDEX IF NOT EXISTS idx_support_tickets_created ON support_tickets(created_at DESC);
