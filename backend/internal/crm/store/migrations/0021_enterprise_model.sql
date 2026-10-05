-- D-111…D-116: relationships between any two records, forecasting, payments, SLA.
-- Additive only. Payments, contracts, assets, entitlements, appointments and SLA policies
-- are standard objects on the object engine (crm.object_records); the tables below are the
-- infrastructure they need.

-- ---- relationship engine ----
CREATE TABLE IF NOT EXISTS crm.relationship_types (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id  uuid REFERENCES crm.workspaces ON DELETE CASCADE, -- NULL = built in, every business
  key           text NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]{1,40}$'),
  label         text NOT NULL,           -- read from the source: "works for"
  inverse_label text NOT NULL,           -- read from the target: "employs"
  source_object text,                    -- NULL = any object
  target_object text,
  cardinality   text NOT NULL DEFAULT 'many_to_many'
                  CHECK (cardinality IN ('one_to_one', 'one_to_many', 'many_to_one', 'many_to_many')),
  is_system     boolean NOT NULL DEFAULT false,
  created_by    uuid REFERENCES crm.identities,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS relationship_types_system_key ON crm.relationship_types (key) WHERE workspace_id IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS relationship_types_ws_key ON crm.relationship_types (workspace_id, key) WHERE workspace_id IS NOT NULL;

INSERT INTO crm.relationship_types (key, label, inverse_label, source_object, target_object, cardinality, is_system) VALUES
  ('related_to',        'Related to',         'Related to',          NULL,            NULL,            'many_to_many', true),
  ('works_for',         'Also works for',     'Has contact',         'contacts',      'accounts',      'many_to_many', true),
  ('decision_maker_for','Decision maker for', 'Decision maker',      'contacts',      'opportunities', 'many_to_many', true),
  ('influencer_for',    'Influencer for',     'Influencer',          'contacts',      'opportunities', 'many_to_many', true),
  ('contact_for',       'Contact for',        'Contact',             'contacts',      NULL,            'many_to_many', true),
  ('attendee',          'Attends',            'Attendee',            'contacts',      'appointments',  'many_to_many', true),
  ('partner_of',        'Partner of',         'Partner',             'accounts',      'accounts',      'many_to_many', true),
  ('competitor_on',     'Competitor on',      'Competitor',          'accounts',      'opportunities', 'many_to_many', true),
  ('supplies',          'Supplies',           'Supplied by',         'accounts',      'catalog_items', 'many_to_many', true),
  ('includes',          'Includes',           'Included in',         NULL,            'catalog_items', 'many_to_many', true),
  ('covers',            'Covers',             'Covered by',          'contracts',     NULL,            'many_to_many', true),
  ('renewal_of',        'Renewal of',         'Renewed by',          'contracts',     'contracts',     'one_to_one',   true),
  ('owns',              'Owns',               'Owned by',            'accounts',      'assets',        'one_to_many',  true)
ON CONFLICT DO NOTHING;

CREATE TABLE IF NOT EXISTS crm.record_relationships (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id  uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  type_key      text NOT NULL,
  source_object text NOT NULL,
  source_id     uuid NOT NULL,
  target_object text NOT NULL,
  target_id     uuid NOT NULL,
  note          text NOT NULL DEFAULT '',
  metadata      jsonb NOT NULL DEFAULT '{}',
  created_by    uuid REFERENCES crm.identities,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now(),
  CHECK (NOT (source_object = target_object AND source_id = target_id)),
  UNIQUE (workspace_id, type_key, source_object, source_id, target_object, target_id)
);
CREATE INDEX IF NOT EXISTS record_relationships_source_idx ON crm.record_relationships (workspace_id, source_object, source_id);
CREATE INDEX IF NOT EXISTS record_relationships_target_idx ON crm.record_relationships (workspace_id, target_object, target_id);

-- ---- payments: which invoice a payment (or part of it) pays ----
CREATE TABLE IF NOT EXISTS crm.payment_allocations (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  payment_id   uuid NOT NULL REFERENCES crm.object_records ON DELETE CASCADE,
  invoice_id   uuid NOT NULL REFERENCES crm.object_records ON DELETE CASCADE,
  amount       numeric(16, 2) NOT NULL CHECK (amount > 0),
  created_by   uuid REFERENCES crm.identities,
  created_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (payment_id, invoice_id)
);
CREATE INDEX IF NOT EXISTS payment_allocations_invoice_idx ON crm.payment_allocations (workspace_id, invoice_id);
CREATE INDEX IF NOT EXISTS payment_allocations_payment_idx ON crm.payment_allocations (workspace_id, payment_id);

-- ---- forecasting ----
-- A target for a period: for the whole business, a team or one person.
CREATE TABLE IF NOT EXISTS crm.forecast_quotas (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  period_key   text NOT NULL,            -- 2026-10 | 2026-Q3 | FY2026
  scope        text NOT NULL CHECK (scope IN ('company', 'team', 'user')),
  owner_id     uuid REFERENCES crm.identities ON DELETE CASCADE,
  team_id      uuid REFERENCES crm.teams ON DELETE CASCADE,
  pipeline     text NOT NULL DEFAULT '',  -- '' = every pipeline
  amount       numeric(16, 2) NOT NULL CHECK (amount >= 0),
  created_by   uuid REFERENCES crm.identities,
  updated_by   uuid REFERENCES crm.identities,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  CHECK ((scope = 'company' AND owner_id IS NULL AND team_id IS NULL)
      OR (scope = 'team' AND team_id IS NOT NULL AND owner_id IS NULL)
      OR (scope = 'user' AND owner_id IS NOT NULL AND team_id IS NULL))
);
CREATE UNIQUE INDEX IF NOT EXISTS forecast_quotas_one ON crm.forecast_quotas
  (workspace_id, period_key, scope, COALESCE(owner_id, '00000000-0000-0000-0000-000000000000'::uuid),
   COALESCE(team_id, '00000000-0000-0000-0000-000000000000'::uuid), pipeline);

-- What a person committed to for a period, and what their manager made of it.
CREATE TABLE IF NOT EXISTS crm.forecast_submissions (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id    uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  period_key      text NOT NULL,
  owner_id        uuid NOT NULL REFERENCES crm.identities ON DELETE CASCADE,
  pipeline        text NOT NULL DEFAULT '',
  closed_won      numeric(16, 2) NOT NULL DEFAULT 0,
  commit_amount   numeric(16, 2) NOT NULL DEFAULT 0,
  best_case       numeric(16, 2) NOT NULL DEFAULT 0,
  pipeline_amount numeric(16, 2) NOT NULL DEFAULT 0,
  forecast_amount numeric(16, 2) NOT NULL DEFAULT 0, -- what the person stands behind
  quota           numeric(16, 2),
  comment         text NOT NULL DEFAULT '',
  status          text NOT NULL DEFAULT 'submitted' CHECK (status IN ('submitted', 'approved', 'rejected')),
  override_amount numeric(16, 2),                    -- the manager's number, when different
  manager_comment text NOT NULL DEFAULT '',
  submitted_at    timestamptz NOT NULL DEFAULT now(),
  approved_at     timestamptz,
  approved_by     uuid REFERENCES crm.identities,
  UNIQUE (workspace_id, period_key, owner_id, pipeline)
);

-- ---- SLA: the clocks running on a case ----
CREATE TABLE IF NOT EXISTS crm.sla_timers (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id   uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  case_id        uuid NOT NULL REFERENCES crm.object_records ON DELETE CASCADE,
  policy_id      uuid REFERENCES crm.object_records ON DELETE SET NULL,
  milestone      text NOT NULL CHECK (milestone IN ('first_response', 'resolution')),
  target_minutes int NOT NULL CHECK (target_minutes > 0),
  hours          jsonb NOT NULL DEFAULT '{}',   -- the business hours the clock runs in (copied from the policy)
  started_at     timestamptz NOT NULL,
  due_at         timestamptz NOT NULL,
  paused_at      timestamptz,
  paused_seconds int NOT NULL DEFAULT 0,
  completed_at   timestamptz,
  warned_at      timestamptz,             -- "nearly due" notice sent
  breached_at    timestamptz,
  escalated_at   timestamptz,
  created_at     timestamptz NOT NULL DEFAULT now(),
  UNIQUE (case_id, milestone)
);
-- Clocks still running, by when they are due (the breach sweep reads this).
CREATE INDEX IF NOT EXISTS sla_timers_due_idx ON crm.sla_timers (due_at)
  WHERE completed_at IS NULL AND breached_at IS NULL AND paused_at IS NULL;
CREATE INDEX IF NOT EXISTS sla_timers_case_idx ON crm.sla_timers (workspace_id, case_id);

-- ---- lookups the new features run often ----
CREATE INDEX IF NOT EXISTS object_records_ws_obj_status_idx ON crm.object_records (workspace_id, object_key, status) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS object_records_ws_obj_owner_idx ON crm.object_records (workspace_id, object_key, owner_id) WHERE deleted_at IS NULL;
-- Opportunities by close date (forecast periods).
CREATE INDEX IF NOT EXISTS opportunities_close_date_idx ON crm.object_records (workspace_id, (custom->>'closeDate'))
  WHERE object_key = 'opportunities' AND deleted_at IS NULL;
-- Contracts by end date (expiry sweep) and invoices by due date (overdue sweep).
CREATE INDEX IF NOT EXISTS contracts_end_date_idx ON crm.object_records ((custom->>'endDate'))
  WHERE object_key = 'contracts' AND deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS invoices_due_date_idx ON crm.object_records ((custom->>'dueDate'))
  WHERE object_key = 'invoices' AND deleted_at IS NULL;
-- Payments of an invoice.
CREATE INDEX IF NOT EXISTS payments_invoice_idx ON crm.object_records (workspace_id, (custom->>'invoiceId'))
  WHERE object_key = 'payments' AND deleted_at IS NULL;
