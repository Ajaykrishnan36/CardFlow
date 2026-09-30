-- 015: A support ticket is a conversation. The ticket's own `message` stays the first
-- message; every later message (the person's follow-ups and support replies) is a row here.
CREATE TABLE IF NOT EXISTS support_ticket_messages (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    ticket_id   VARCHAR(20) NOT NULL REFERENCES support_tickets(id) ON DELETE CASCADE,
    sender      VARCHAR(10) NOT NULL CHECK (sender IN ('user', 'support')),
    author_name VARCHAR(150) NOT NULL DEFAULT '',
    author_role VARCHAR(60) NOT NULL DEFAULT '',
    body        TEXT NOT NULL,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_support_ticket_messages_ticket ON support_ticket_messages(ticket_id, created_at);

-- A reply given before this migration becomes the conversation's first support message (once).
INSERT INTO support_ticket_messages (ticket_id, sender, author_name, author_role, body, created_at)
SELECT t.id, 'support', COALESCE(NULLIF(regexp_replace(t.replied_by, ' \(CRM\)$', ''), ''), 'Support team'), 'Support team',
       t.admin_reply, COALESCE(t.replied_at, t.updated_at)
FROM support_tickets t
WHERE COALESCE(t.admin_reply, '') <> ''
  AND NOT EXISTS (SELECT 1 FROM support_ticket_messages m WHERE m.ticket_id = t.id);
