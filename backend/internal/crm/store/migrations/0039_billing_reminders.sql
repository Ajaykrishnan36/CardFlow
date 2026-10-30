-- Reminders that a member's paid time is running out (D-169): 7, 5, 3 and 1 day before,
-- and once it has ended. One row per member, paid-until date and step, so each reminder is
-- sent once even though the sweep runs every hour. Only additions.
CREATE TABLE IF NOT EXISTS crm.billing_reminders (
  membership_id uuid NOT NULL REFERENCES crm.memberships(id) ON DELETE CASCADE,
  paid_until    timestamptz NOT NULL,
  days          int NOT NULL,
  sent_at       timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (membership_id, paid_until, days)
);
CREATE INDEX IF NOT EXISTS notifications_inbox_idx ON crm.notifications (identity_id, workspace_id, id DESC);
