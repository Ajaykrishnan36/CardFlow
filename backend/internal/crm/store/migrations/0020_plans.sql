-- D-101: plans and entitlements belong to a business (workspace), not to a person.

CREATE TABLE IF NOT EXISTS crm.plans (
  key           text PRIMARY KEY CHECK (key ~ '^[a-z][a-z0-9_]{1,30}$'),
  name          text NOT NULL,
  description   text NOT NULL DEFAULT '',
  price_monthly numeric(12, 2) NOT NULL DEFAULT 0,
  price_yearly  numeric(12, 2) NOT NULL DEFAULT 0,
  currency      char(3) NOT NULL DEFAULT 'INR',
  -- limits: {"members": n, "records": n, "cardScansPerMonth": n}; 0 or missing = unlimited.
  limits        jsonb NOT NULL DEFAULT '{}',
  -- features: what the plan page lists (text shown to customers).
  features      jsonb NOT NULL DEFAULT '[]',
  is_default    boolean NOT NULL DEFAULT false,
  is_active     boolean NOT NULL DEFAULT true,
  position      int NOT NULL DEFAULT 0,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX IF NOT EXISTS one_default_plan ON crm.plans (is_default) WHERE is_default;

INSERT INTO crm.plans (key, name, description, price_monthly, price_yearly, limits, features, is_default, position) VALUES
  ('free', 'Free', 'Everything a small business needs to start.', 0, 0,
   '{"members": 3, "records": 1000, "cardScansPerMonth": 50}',
   '["Leads, contacts, accounts and deals", "Tasks, calendar and cases", "Income and expenses", "Business card scanning", "Up to 3 team members"]', true, 1),
  ('pro', 'Pro', 'For a growing team.', 499, 4999,
   '{"members": 15, "records": 25000, "cardScansPerMonth": 1000}',
   '["Everything in Free", "Up to 15 team members", "25,000 records", "Workflows and email campaigns", "Reports and dashboards"]', false, 2),
  ('business', 'Business', 'No limits.', 1499, 14999,
   '{}',
   '["Everything in Pro", "Unlimited team members and records", "API keys and webhooks", "Single sign-on"]', false, 3)
ON CONFLICT (key) DO NOTHING;

CREATE TABLE IF NOT EXISTS crm.workspace_subscriptions (
  workspace_id         uuid PRIMARY KEY REFERENCES crm.workspaces ON DELETE CASCADE,
  plan_key             text NOT NULL REFERENCES crm.plans,
  status               text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'trialing', 'past_due', 'cancelled', 'expired')),
  source               text NOT NULL DEFAULT 'manual' CHECK (source IN ('manual', 'revenuecat', 'grandfathered', 'system')),
  started_at           timestamptz NOT NULL DEFAULT now(),
  current_period_end   timestamptz,
  external_customer_id text,
  external_ref         text,
  notes                text NOT NULL DEFAULT '',
  updated_by           uuid REFERENCES crm.identities,
  updated_at           timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS crm.subscription_events (
  id           bigserial PRIMARY KEY,
  workspace_id uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  type         text NOT NULL,
  plan_key     text,
  detail       jsonb NOT NULL DEFAULT '{}',
  actor_id     uuid REFERENCES crm.identities,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS subscription_events_ws_idx ON crm.subscription_events (workspace_id, created_at DESC);

-- Monthly counters (period = first day of the month).
CREATE TABLE IF NOT EXISTS crm.usage_counters (
  workspace_id uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  metric       text NOT NULL,
  period       date NOT NULL,
  value        bigint NOT NULL DEFAULT 0,
  PRIMARY KEY (workspace_id, metric, period)
);
