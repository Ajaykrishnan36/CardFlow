-- 0022 — commercial, sales-execution and service model (D-118 … D-127).
-- Additive only. Dedicated tables where integrity, money or scheduling need them; everything
-- else (territories, work orders, service resources, refunds, credit and debit notes,
-- adjustments) is an object in the generic engine.

-- ---- currencies and exchange rates (D-118) ----
CREATE TABLE IF NOT EXISTS crm.currencies (
  code           text PRIMARY KEY CHECK (code ~ '^[A-Z]{3}$'),
  name           text NOT NULL,
  symbol         text NOT NULL,
  decimal_places int  NOT NULL DEFAULT 2 CHECK (decimal_places BETWEEN 0 AND 4),
  is_active      boolean NOT NULL DEFAULT true
);
INSERT INTO crm.currencies (code, name, symbol, decimal_places) VALUES
  ('INR', 'Indian rupee', '₹', 2), ('USD', 'US dollar', '$', 2), ('EUR', 'Euro', '€', 2), ('GBP', 'Pound sterling', '£', 2),
  ('AED', 'UAE dirham', 'AED', 2), ('SGD', 'Singapore dollar', 'S$', 2), ('AUD', 'Australian dollar', 'A$', 2),
  ('CAD', 'Canadian dollar', 'C$', 2), ('JPY', 'Japanese yen', '¥', 0), ('SAR', 'Saudi riyal', 'SAR', 2),
  ('QAR', 'Qatari riyal', 'QAR', 2), ('MYR', 'Malaysian ringgit', 'RM', 2), ('LKR', 'Sri Lankan rupee', 'Rs', 2),
  ('BDT', 'Bangladeshi taka', '৳', 2), ('NPR', 'Nepalese rupee', 'Rs', 2), ('CHF', 'Swiss franc', 'CHF', 2),
  ('CNY', 'Chinese yuan', '¥', 2), ('ZAR', 'South African rand', 'R', 2)
ON CONFLICT (code) DO NOTHING;

-- One unit of from_currency is worth `rate` units of to_currency, from effective_from on.
CREATE TABLE IF NOT EXISTS crm.exchange_rates (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id   uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  from_currency  text NOT NULL REFERENCES crm.currencies,
  to_currency    text NOT NULL REFERENCES crm.currencies,
  rate           numeric(20,8) NOT NULL CHECK (rate > 0),
  effective_from date NOT NULL,
  effective_to   date,
  source         text NOT NULL DEFAULT 'manual',
  created_by     uuid REFERENCES crm.identities,
  created_at     timestamptz NOT NULL DEFAULT now(),
  CHECK (from_currency <> to_currency),
  CHECK (effective_to IS NULL OR effective_to >= effective_from),
  UNIQUE (workspace_id, from_currency, to_currency, effective_from)
);

-- ---- price book entries (D-119) ----
CREATE TABLE IF NOT EXISTS crm.price_book_entries (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id     uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  price_book_id    uuid NOT NULL REFERENCES crm.object_records ON DELETE CASCADE,
  catalog_item_id  uuid NOT NULL REFERENCES crm.object_records ON DELETE CASCADE,
  currency         text NOT NULL REFERENCES crm.currencies,
  list_price       numeric(16,2) NOT NULL CHECK (list_price >= 0),
  unit_price       numeric(16,2) NOT NULL CHECK (unit_price >= 0),
  cost             numeric(16,2) CHECK (cost IS NULL OR cost >= 0),
  max_discount_pct numeric(5,2) CHECK (max_discount_pct IS NULL OR max_discount_pct BETWEEN 0 AND 100),
  min_quantity     numeric(14,3) NOT NULL DEFAULT 1 CHECK (min_quantity > 0),
  max_quantity     numeric(14,3) CHECK (max_quantity IS NULL OR max_quantity >= min_quantity),
  valid_from       date NOT NULL DEFAULT CURRENT_DATE,
  valid_to         date CHECK (valid_to IS NULL OR valid_to >= valid_from),
  is_active        boolean NOT NULL DEFAULT true,
  version          int NOT NULL DEFAULT 1,
  created_by       uuid REFERENCES crm.identities,
  updated_by       uuid REFERENCES crm.identities,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now(),
  deleted_at       timestamptz
);
-- One live price per book, item, currency, quantity break and start date; history stays.
CREATE UNIQUE INDEX IF NOT EXISTS price_book_entries_one ON crm.price_book_entries
  (workspace_id, price_book_id, catalog_item_id, currency, min_quantity, valid_from) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS price_book_entries_item_idx ON crm.price_book_entries (workspace_id, catalog_item_id) WHERE deleted_at IS NULL;

-- ---- CPQ: bundles and rules (D-120) ----
CREATE TABLE IF NOT EXISTS crm.product_bundle_items (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id      uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  bundle_item_id    uuid NOT NULL REFERENCES crm.object_records ON DELETE CASCADE,
  component_item_id uuid NOT NULL REFERENCES crm.object_records ON DELETE CASCADE,
  is_required       boolean NOT NULL DEFAULT true,
  quantity          numeric(14,3) NOT NULL DEFAULT 1 CHECK (quantity > 0),
  min_quantity      numeric(14,3) CHECK (min_quantity IS NULL OR min_quantity >= 0),
  max_quantity      numeric(14,3),
  sequence          int NOT NULL DEFAULT 0,
  price_mode        text NOT NULL DEFAULT 'additional' CHECK (price_mode IN ('included', 'additional')),
  created_by        uuid REFERENCES crm.identities,
  created_at        timestamptz NOT NULL DEFAULT now(),
  CHECK (bundle_item_id <> component_item_id),
  UNIQUE (bundle_item_id, component_item_id)
);
CREATE INDEX IF NOT EXISTS product_bundle_items_ws_idx ON crm.product_bundle_items (workspace_id, bundle_item_id);

-- kind: price (set a price), discount (take something off), eligibility (who may buy an
-- item), configuration (what must or must not be bought together).
CREATE TABLE IF NOT EXISTS crm.pricing_rules (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id   uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  name           text NOT NULL,
  kind           text NOT NULL CHECK (kind IN ('price', 'discount', 'eligibility', 'configuration')),
  priority       int NOT NULL DEFAULT 100,
  condition      jsonb NOT NULL DEFAULT '{}',
  action         jsonb NOT NULL DEFAULT '{}',
  effective_from date,
  effective_to   date,
  is_active      boolean NOT NULL DEFAULT true,
  created_by     uuid REFERENCES crm.identities,
  updated_by     uuid REFERENCES crm.identities,
  created_at     timestamptz NOT NULL DEFAULT now(),
  updated_at     timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS pricing_rules_ws_idx ON crm.pricing_rules (workspace_id, kind, priority) WHERE is_active;

-- ---- approvals (D-121): discounts, credit notes, write-offs, refunds ----
CREATE TABLE IF NOT EXISTS crm.approval_rules (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id  uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  kind          text NOT NULL CHECK (kind IN ('discount', 'credit_note', 'write_off', 'refund', 'debit_note')),
  min_value     numeric(16,2) NOT NULL DEFAULT 0,   -- discount: percent; the others: amount in the base currency
  approver_role text NOT NULL DEFAULT 'ADMIN',      -- role key at or above which a member may decide
  label         text NOT NULL DEFAULT '',
  created_by    uuid REFERENCES crm.identities,
  created_at    timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, kind, min_value)
);
CREATE TABLE IF NOT EXISTS crm.approval_requests (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id  uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  kind          text NOT NULL,
  object_key    text NOT NULL,
  record_id     uuid NOT NULL,
  value         numeric(16,2) NOT NULL,
  approver_role text NOT NULL,
  reason        text NOT NULL DEFAULT '',
  status        text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected', 'cancelled')),
  requested_by  uuid REFERENCES crm.identities,
  decided_by    uuid REFERENCES crm.identities,
  decision_note text NOT NULL DEFAULT '',
  created_at    timestamptz NOT NULL DEFAULT now(),
  decided_at    timestamptz
);
CREATE UNIQUE INDEX IF NOT EXISTS approval_requests_one_pending ON crm.approval_requests (workspace_id, object_key, record_id, kind) WHERE status = 'pending';
CREATE INDEX IF NOT EXISTS approval_requests_ws_idx ON crm.approval_requests (workspace_id, status, created_at DESC);

-- ---- contact roles: richer relationships (D-122) ----
ALTER TABLE crm.record_relationships ADD COLUMN IF NOT EXISTS role       text;
ALTER TABLE crm.record_relationships ADD COLUMN IF NOT EXISTS is_primary boolean NOT NULL DEFAULT false;
ALTER TABLE crm.record_relationships ADD COLUMN IF NOT EXISTS is_active  boolean NOT NULL DEFAULT true;
ALTER TABLE crm.record_relationships ADD COLUMN IF NOT EXISTS start_date date;
ALTER TABLE crm.record_relationships ADD COLUMN IF NOT EXISTS end_date   date;
-- One primary contact per record.
CREATE UNIQUE INDEX IF NOT EXISTS record_relationships_one_primary ON crm.record_relationships
  (workspace_id, target_object, target_id) WHERE type_key = 'contact_role' AND is_primary AND is_active;
INSERT INTO crm.relationship_types (key, label, inverse_label, source_object, target_object, cardinality, is_system) VALUES
  ('contact_role', 'Has a role on', 'Contact roles', 'contacts', NULL, 'many_to_many', true),
  ('installed_on', 'Installed on', 'Has installed', 'assets', 'assets', 'many_to_many', true),
  ('component_of', 'Component of', 'Has component', 'assets', 'assets', 'many_to_many', true),
  ('replaced_by',  'Replaced by',  'Replaces',      'assets', 'assets', 'many_to_many', true),
  ('upgraded_to',  'Upgraded to',  'Upgraded from', 'assets', 'assets', 'many_to_many', true),
  ('depends_on',   'Depends on',   'Needed by',     'assets', 'assets', 'many_to_many', true)
ON CONFLICT (key) WHERE workspace_id IS NULL DO NOTHING;

-- ---- record-level teams: account team, opportunity team, case team, contract team (D-123) ----
CREATE TABLE IF NOT EXISTS crm.record_team_members (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  object_key   text NOT NULL,
  record_id    uuid NOT NULL,
  identity_id  uuid NOT NULL REFERENCES crm.identities ON DELETE CASCADE,
  team_role    text NOT NULL DEFAULT '',
  access_level text NOT NULL DEFAULT 'read' CHECK (access_level IN ('read', 'write', 'full')),
  is_primary   boolean NOT NULL DEFAULT false,
  created_by   uuid REFERENCES crm.identities,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, object_key, record_id, identity_id)
);
CREATE INDEX IF NOT EXISTS record_team_members_person_idx ON crm.record_team_members (workspace_id, object_key, identity_id);

-- ---- territories (D-124): the territory itself is an object; who and what belongs to it is here ----
CREATE TABLE IF NOT EXISTS crm.territory_assignments (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id   uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  territory_id   uuid NOT NULL REFERENCES crm.object_records ON DELETE CASCADE,
  kind           text NOT NULL CHECK (kind IN ('account', 'user', 'team')),
  account_id     uuid REFERENCES crm.accounts ON DELETE CASCADE,
  identity_id    uuid REFERENCES crm.identities ON DELETE CASCADE,
  team_id        uuid REFERENCES crm.teams ON DELETE CASCADE,
  is_primary     boolean NOT NULL DEFAULT true,
  effective_from date NOT NULL DEFAULT CURRENT_DATE,
  effective_to   date CHECK (effective_to IS NULL OR effective_to >= effective_from),
  created_by     uuid REFERENCES crm.identities,
  created_at     timestamptz NOT NULL DEFAULT now(),
  CHECK ((kind = 'account' AND account_id IS NOT NULL AND identity_id IS NULL AND team_id IS NULL)
      OR (kind = 'user' AND identity_id IS NOT NULL AND account_id IS NULL AND team_id IS NULL)
      OR (kind = 'team' AND team_id IS NOT NULL AND account_id IS NULL AND identity_id IS NULL))
);
CREATE INDEX IF NOT EXISTS territory_assignments_territory_idx ON crm.territory_assignments (workspace_id, territory_id);
CREATE INDEX IF NOT EXISTS territory_assignments_account_idx ON crm.territory_assignments (workspace_id, account_id) WHERE account_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS territory_assignments_user_idx ON crm.territory_assignments (workspace_id, identity_id) WHERE identity_id IS NOT NULL;
-- An account, person or team is in a territory once at a time.
CREATE UNIQUE INDEX IF NOT EXISTS territory_assignments_one_open ON crm.territory_assignments
  (workspace_id, territory_id, kind, COALESCE(account_id, identity_id, team_id)) WHERE effective_to IS NULL;

-- ---- campaign members and what a campaign costs (D-125) ----
ALTER TABLE crm.campaigns ADD COLUMN IF NOT EXISTS campaign_type    text NOT NULL DEFAULT 'email';
ALTER TABLE crm.campaigns ADD COLUMN IF NOT EXISTS budgeted_cost    numeric(16,2);
ALTER TABLE crm.campaigns ADD COLUMN IF NOT EXISTS actual_cost      numeric(16,2);
ALTER TABLE crm.campaigns ADD COLUMN IF NOT EXISTS expected_revenue numeric(16,2);
ALTER TABLE crm.campaigns ADD COLUMN IF NOT EXISTS start_date       date;
ALTER TABLE crm.campaigns ADD COLUMN IF NOT EXISTS end_date         date;

CREATE TABLE IF NOT EXISTS crm.campaign_members (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id  uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  campaign_id   uuid NOT NULL REFERENCES crm.campaigns ON DELETE CASCADE,
  lead_id       uuid REFERENCES crm.leads ON DELETE CASCADE,
  contact_id    uuid REFERENCES crm.contacts ON DELETE CASCADE,
  status        text NOT NULL DEFAULT 'sent' CHECK (status IN ('planned', 'sent', 'opened', 'clicked', 'responded', 'converted', 'bounced', 'unsubscribed')),
  source        text NOT NULL DEFAULT 'manual',
  responded     boolean NOT NULL DEFAULT false,
  response_date timestamptz,
  first_touch_at timestamptz NOT NULL DEFAULT now(),
  last_touch_at  timestamptz NOT NULL DEFAULT now(),
  created_by    uuid REFERENCES crm.identities,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  CHECK ((lead_id IS NOT NULL) <> (contact_id IS NOT NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS campaign_members_lead_one ON crm.campaign_members (campaign_id, lead_id) WHERE lead_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS campaign_members_contact_one ON crm.campaign_members (campaign_id, contact_id) WHERE contact_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS campaign_members_ws_idx ON crm.campaign_members (workspace_id, campaign_id);
CREATE INDEX IF NOT EXISTS campaign_members_contact_idx ON crm.campaign_members (workspace_id, contact_id) WHERE contact_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS campaign_members_lead_idx ON crm.campaign_members (workspace_id, lead_id) WHERE lead_id IS NOT NULL;

-- People a campaign email was sent to become members (leads and contacts only; nothing invented).
INSERT INTO crm.campaign_members (workspace_id, campaign_id, lead_id, contact_id, status, source, first_touch_at, last_touch_at)
SELECT r.workspace_id, r.campaign_id,
       CASE WHEN c.object_key = 'leads' THEN r.record_id END, CASE WHEN c.object_key = 'contacts' THEN r.record_id END,
       CASE r.status WHEN 'bounced' THEN 'bounced' WHEN 'unsubscribed' THEN 'unsubscribed' ELSE 'sent' END, 'email',
       COALESCE(r.sent_at, c.sent_at, c.created_at), COALESCE(r.sent_at, c.sent_at, c.created_at)
FROM crm.campaign_recipients r JOIN crm.campaigns c ON c.id = r.campaign_id
WHERE c.object_key IN ('leads', 'contacts') AND r.status IN ('sent', 'bounced', 'unsubscribed')
  AND ((c.object_key = 'leads' AND EXISTS (SELECT 1 FROM crm.leads l WHERE l.id = r.record_id))
    OR (c.object_key = 'contacts' AND EXISTS (SELECT 1 FROM crm.contacts k WHERE k.id = r.record_id)))
ON CONFLICT DO NOTHING;

-- ---- finance (D-126): refunds against invoices, credit applied to invoices ----
-- The share of a refund that comes off an invoice the refunded payment had paid.
CREATE TABLE IF NOT EXISTS crm.refund_allocations (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  refund_id    uuid NOT NULL REFERENCES crm.object_records ON DELETE CASCADE,
  payment_id   uuid NOT NULL REFERENCES crm.object_records ON DELETE CASCADE,
  invoice_id   uuid NOT NULL REFERENCES crm.object_records ON DELETE CASCADE,
  amount       numeric(16,2) NOT NULL CHECK (amount > 0),
  created_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (refund_id, invoice_id)
);
CREATE INDEX IF NOT EXISTS refund_allocations_invoice_idx ON crm.refund_allocations (workspace_id, invoice_id);
CREATE INDEX IF NOT EXISTS refund_allocations_payment_idx ON crm.refund_allocations (workspace_id, payment_id);

-- How much of a credit note is applied to an invoice.
CREATE TABLE IF NOT EXISTS crm.credit_allocations (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id   uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  credit_note_id uuid NOT NULL REFERENCES crm.object_records ON DELETE CASCADE,
  invoice_id     uuid NOT NULL REFERENCES crm.object_records ON DELETE CASCADE,
  amount         numeric(16,2) NOT NULL CHECK (amount > 0),
  created_by     uuid REFERENCES crm.identities,
  created_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (credit_note_id, invoice_id)
);
CREATE INDEX IF NOT EXISTS credit_allocations_invoice_idx ON crm.credit_allocations (workspace_id, invoice_id);

-- ---- service (D-127): more SLA milestones, entitlement usage ----
ALTER TABLE crm.sla_timers DROP CONSTRAINT IF EXISTS sla_timers_milestone_check;
ALTER TABLE crm.sla_timers ADD CONSTRAINT sla_timers_milestone_check
  CHECK (milestone IN ('first_response', 'resolution', 'assignment', 'customer_update'));
ALTER TABLE crm.sla_timers ADD COLUMN IF NOT EXISTS warn_percent int NOT NULL DEFAULT 80 CHECK (warn_percent BETWEEN 1 AND 99);

CREATE TABLE IF NOT EXISTS crm.entitlement_usage (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id   uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  entitlement_id uuid NOT NULL REFERENCES crm.object_records ON DELETE CASCADE,
  kind           text NOT NULL CHECK (kind IN ('case', 'hours')),
  quantity       numeric(12,2) NOT NULL CHECK (quantity > 0),
  source_object  text NOT NULL,
  source_id      uuid NOT NULL,
  note           text NOT NULL DEFAULT '',
  created_by     uuid REFERENCES crm.identities,
  created_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (entitlement_id, kind, source_object, source_id)
);
CREATE INDEX IF NOT EXISTS entitlement_usage_ws_idx ON crm.entitlement_usage (workspace_id, entitlement_id);

-- ---- forecast history (D-112 extended): every submission and decision, kept ----
CREATE TABLE IF NOT EXISTS crm.forecast_history (
  id              bigserial PRIMARY KEY,
  workspace_id    uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  period_key      text NOT NULL,
  owner_id        uuid NOT NULL REFERENCES crm.identities ON DELETE CASCADE,
  pipeline        text NOT NULL DEFAULT '',
  event           text NOT NULL CHECK (event IN ('submitted', 'approved', 'rejected')),
  closed_won      numeric(16,2) NOT NULL DEFAULT 0,
  commit_amount   numeric(16,2) NOT NULL DEFAULT 0,
  best_case       numeric(16,2) NOT NULL DEFAULT 0,
  pipeline_amount numeric(16,2) NOT NULL DEFAULT 0,
  forecast_amount numeric(16,2) NOT NULL DEFAULT 0,
  override_amount numeric(16,2),
  quota           numeric(16,2) NOT NULL DEFAULT 0,
  comment         text NOT NULL DEFAULT '',
  actor_id        uuid REFERENCES crm.identities,
  created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS forecast_history_idx ON crm.forecast_history (workspace_id, period_key, owner_id, created_at);

-- ---- lookups the new features run often ----
CREATE INDEX IF NOT EXISTS line_items_quote_idx ON crm.object_records (workspace_id, (custom->>'quoteId')) WHERE object_key = 'line_items' AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS line_items_order_idx ON crm.object_records (workspace_id, (custom->>'orderId')) WHERE object_key = 'line_items' AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS line_items_invoice_idx ON crm.object_records (workspace_id, (custom->>'invoiceId')) WHERE object_key = 'line_items' AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS appointments_resource_idx ON crm.object_records (workspace_id, (custom->>'resourceId'), (custom->>'startsAt')) WHERE object_key = 'appointments' AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS opportunities_territory_idx ON crm.object_records (workspace_id, (custom->>'territoryId')) WHERE object_key = 'opportunities' AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS refunds_payment_idx ON crm.object_records (workspace_id, (custom->>'paymentId')) WHERE object_key = 'refunds' AND deleted_at IS NULL;
