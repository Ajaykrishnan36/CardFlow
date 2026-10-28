-- Paying per member with Razorpay (D-164). Each member of a business is paid for, for 1, 3,
-- 6 or 12 months at a time; until paid (or once the time runs out) the member is locked out
-- of the business. Only additions: a paid-until date on the membership and the orders.
ALTER TABLE crm.memberships ADD COLUMN IF NOT EXISTS paid_until timestamptz;

CREATE TABLE IF NOT EXISTS crm.billing_orders (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id        uuid NOT NULL REFERENCES crm.workspaces(id) ON DELETE CASCADE,
  razorpay_order_id   text NOT NULL UNIQUE,
  razorpay_payment_id text,
  status              text NOT NULL DEFAULT 'created' CHECK (status IN ('created', 'paid', 'failed')),
  period              text NOT NULL,
  months              int  NOT NULL CHECK (months > 0),
  unit_price          int  NOT NULL CHECK (unit_price > 0), -- rupees per member for the period
  amount_paise        bigint NOT NULL CHECK (amount_paise > 0),
  currency            text NOT NULL DEFAULT 'INR',
  membership_ids      uuid[] NOT NULL,
  paid_by             uuid REFERENCES crm.identities(id) ON DELETE SET NULL,
  created_at          timestamptz NOT NULL DEFAULT now(),
  paid_at             timestamptz
);
CREATE INDEX IF NOT EXISTS billing_orders_workspace_idx ON crm.billing_orders (workspace_id, created_at DESC);
