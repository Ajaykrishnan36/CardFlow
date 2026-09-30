-- Everyday CRM features (D-54 … D-66): saved views, favorites, timeline for every
-- object, recycle bin, files, unique fields and custom lookups, API keys, webhooks,
-- workflows, notifications, email & calendar sync, campaigns, SSO and teams.
-- Additive only: no existing row is changed or removed (except dropping the unused,
-- empty crm.branches table).

-- ---- Saved views (table / kanban / calendar; personal or shared) ----
CREATE TABLE crm.views (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces,
  object_key   text NOT NULL,
  name         text NOT NULL,
  kind         text NOT NULL DEFAULT 'table' CHECK (kind IN ('table', 'kanban', 'calendar')),
  visibility   text NOT NULL DEFAULT 'personal' CHECK (visibility IN ('personal', 'shared')),
  owner_id     uuid REFERENCES crm.identities ON DELETE CASCADE,
  definition   jsonb NOT NULL DEFAULT '{}', -- filter tree, sorts, columns, group-by, kanban/calendar field
  position     int NOT NULL DEFAULT 0,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX views_ws_obj_idx ON crm.views (workspace_id, object_key, position);

-- ---- Favorites (records and views, per person) ----
CREATE TABLE crm.favorites (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces,
  identity_id  uuid NOT NULL REFERENCES crm.identities ON DELETE CASCADE,
  kind         text NOT NULL CHECK (kind IN ('record', 'view')),
  object_key   text NOT NULL,
  target_id    uuid NOT NULL,
  position     int NOT NULL DEFAULT 0,
  created_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, identity_id, kind, target_id)
);

-- ---- Timeline for every object (field changes, notes, emails, events…) ----
ALTER TABLE crm.activities ADD COLUMN object_key text;
ALTER TABLE crm.activities ADD COLUMN record_id uuid;
ALTER TABLE crm.activities ADD COLUMN actor_id uuid;
CREATE INDEX activities_record_idx ON crm.activities (workspace_id, object_key, record_id, occurred_at DESC);

-- ---- Recycle bin: who deleted a record ----
ALTER TABLE crm.leads ADD COLUMN deleted_by uuid;
ALTER TABLE crm.accounts ADD COLUMN deleted_by uuid;
ALTER TABLE crm.contacts ADD COLUMN deleted_by uuid;
ALTER TABLE crm.object_records ADD COLUMN deleted_by uuid;
-- The per-object views read every column of object_records; recreate them so the new one shows.
DO $$
DECLARE k text;
BEGIN
  FOR k IN SELECT key FROM crm.object_definitions WHERE key ~ '^[a-z][a-z0-9_]{1,40}$' LOOP
    EXECUTE format('CREATE OR REPLACE VIEW crm.%I AS SELECT * FROM crm.object_records WHERE object_key = %L WITH CASCADED CHECK OPTION', 'obj_' || k, k);
    EXECUTE format('ALTER VIEW crm.%I ALTER COLUMN object_key SET DEFAULT %L', 'obj_' || k, k);
  END LOOP;
END $$;

-- ---- Files attached to records ----
CREATE TABLE crm.files (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces,
  object_key   text NOT NULL,
  record_id    uuid NOT NULL,
  field_key    text,                -- set when the file belongs to a "files" field
  name         text NOT NULL,
  content_type text NOT NULL,
  size_bytes   int NOT NULL,
  data         bytea NOT NULL,
  created_by   uuid,
  created_at   timestamptz NOT NULL DEFAULT now(),
  deleted_at   timestamptz
);
CREATE INDEX files_record_idx ON crm.files (workspace_id, object_key, record_id) WHERE deleted_at IS NULL;

-- ---- Custom fields: unique values and links to other records ----
ALTER TABLE crm.field_definitions ADD COLUMN is_unique boolean NOT NULL DEFAULT false;
ALTER TABLE crm.field_definitions ADD COLUMN lookup_target text;

-- ---- API keys: a role decides what a key may do ----
ALTER TABLE crm.api_keys ALTER COLUMN permission_set_id DROP NOT NULL;
ALTER TABLE crm.api_keys ALTER COLUMN product_ids SET DEFAULT '{}';
ALTER TABLE crm.api_keys ADD COLUMN role_id uuid REFERENCES crm.roles;
ALTER TABLE crm.api_keys ADD COLUMN expires_at timestamptz;
ALTER TABLE crm.api_keys ADD COLUMN last_used_ip inet;
CREATE INDEX api_keys_ws_idx ON crm.api_keys (workspace_id) WHERE revoked_at IS NULL;

-- ---- Webhooks (signed, retried) ----
CREATE TABLE crm.webhooks (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id     uuid NOT NULL REFERENCES crm.workspaces,
  url              text NOT NULL,
  description      text NOT NULL DEFAULT '',
  secret_enc       bytea NOT NULL,
  events           text[] NOT NULL,   -- record.created | record.updated | record.deleted | record.restored
  objects          text[] NOT NULL DEFAULT '{}', -- empty = every object
  status           text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'paused')),
  last_delivery_at timestamptz,
  last_status      int,
  created_by       uuid,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
CREATE TABLE crm.webhook_deliveries (
  id              bigserial PRIMARY KEY,
  webhook_id      uuid NOT NULL REFERENCES crm.webhooks ON DELETE CASCADE,
  workspace_id    uuid NOT NULL,
  event_id        uuid NOT NULL,
  event_type      text NOT NULL,
  payload         jsonb NOT NULL,
  status          text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'delivered', 'failed')),
  attempts        int NOT NULL DEFAULT 0,
  response_status int,
  response_body   text,
  next_attempt_at timestamptz,
  created_at      timestamptz NOT NULL DEFAULT now(),
  delivered_at    timestamptz,
  UNIQUE (webhook_id, event_id)
);
CREATE INDEX webhook_deliveries_due_idx ON crm.webhook_deliveries (next_attempt_at) WHERE status = 'pending';

-- ---- Workflows ----
CREATE TABLE crm.workflows (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id  uuid NOT NULL REFERENCES crm.workspaces,
  name          text NOT NULL,
  description   text NOT NULL DEFAULT '',
  status        text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'inactive')),
  draft         jsonb NOT NULL DEFAULT '{}', -- trigger + steps being edited
  published     jsonb,                       -- the active version's trigger + steps
  version       int NOT NULL DEFAULT 0,
  webhook_token text UNIQUE,
  next_run_at   timestamptz,                 -- schedule triggers
  last_run_at   timestamptz,
  created_by    uuid,
  updated_by    uuid,
  created_at    timestamptz NOT NULL DEFAULT now(),
  updated_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX workflows_ws_idx ON crm.workflows (workspace_id);
CREATE TABLE crm.workflow_versions (
  workflow_id  uuid NOT NULL REFERENCES crm.workflows ON DELETE CASCADE,
  version      int NOT NULL,
  definition   jsonb NOT NULL,
  published_by uuid,
  published_at timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (workflow_id, version)
);
CREATE TABLE crm.workflow_runs (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL,
  workflow_id  uuid NOT NULL REFERENCES crm.workflows ON DELETE CASCADE,
  version      int NOT NULL,
  status       text NOT NULL DEFAULT 'queued' CHECK (status IN ('queued', 'running', 'waiting', 'completed', 'failed', 'stopped')),
  trigger      jsonb NOT NULL DEFAULT '{}',
  context      jsonb NOT NULL DEFAULT '{}',
  step_index   int NOT NULL DEFAULT 0,
  steps        jsonb NOT NULL DEFAULT '[]',
  error        text,
  resume_at    timestamptz,
  started_by   uuid,
  started_at   timestamptz NOT NULL DEFAULT now(),
  finished_at  timestamptz
);
CREATE INDEX workflow_runs_wf_idx ON crm.workflow_runs (workflow_id, started_at DESC);
CREATE INDEX workflow_runs_due_idx ON crm.workflow_runs (resume_at) WHERE status IN ('queued', 'waiting');
CREATE TABLE crm.assignment_state (
  workspace_id uuid NOT NULL,
  key          text NOT NULL,
  last_index   int NOT NULL DEFAULT -1,
  PRIMARY KEY (workspace_id, key)
);

-- ---- Notifications ----
CREATE TABLE crm.notifications (
  id           bigserial PRIMARY KEY,
  workspace_id uuid,
  identity_id  uuid NOT NULL REFERENCES crm.identities ON DELETE CASCADE,
  kind         text NOT NULL,
  title        text NOT NULL,
  body         text NOT NULL DEFAULT '',
  link         text NOT NULL DEFAULT '',
  actor_id     uuid,
  read_at      timestamptz,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX notifications_identity_idx ON crm.notifications (identity_id, created_at DESC);

-- ---- Email & calendar ----
CREATE TABLE crm.mail_accounts (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id         uuid NOT NULL REFERENCES crm.workspaces,
  identity_id          uuid NOT NULL REFERENCES crm.identities ON DELETE CASCADE,
  provider             text NOT NULL CHECK (provider IN ('google', 'microsoft', 'imap')),
  email                text NOT NULL,
  display_name         text NOT NULL DEFAULT '',
  credentials_enc      bytea NOT NULL,
  status               text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'error', 'paused')),
  error                text,
  sync_email           boolean NOT NULL DEFAULT true,
  sync_calendar        boolean NOT NULL DEFAULT true,
  visibility           text NOT NULL DEFAULT 'share_everything' CHECK (visibility IN ('share_everything', 'subject', 'metadata')),
  auto_create_contacts boolean NOT NULL DEFAULT false,
  cursor               jsonb NOT NULL DEFAULT '{}',
  last_synced_at       timestamptz,
  created_at           timestamptz NOT NULL DEFAULT now(),
  updated_at           timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, identity_id, email)
);
CREATE TABLE crm.messages (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id    uuid NOT NULL REFERENCES crm.workspaces,
  mail_account_id uuid REFERENCES crm.mail_accounts ON DELETE CASCADE, -- NULL = sent by the CRM's own mailer
  provider_id     text,
  thread_id       text,
  direction       text NOT NULL CHECK (direction IN ('inbound', 'outbound')),
  from_addr       text NOT NULL,
  from_name       text NOT NULL DEFAULT '',
  to_addrs        text[] NOT NULL DEFAULT '{}',
  cc_addrs        text[] NOT NULL DEFAULT '{}',
  subject         text NOT NULL DEFAULT '',
  snippet         text NOT NULL DEFAULT '',
  body_text       text NOT NULL DEFAULT '',
  body_html       text NOT NULL DEFAULT '',
  status          text NOT NULL DEFAULT 'sent' CHECK (status IN ('sent', 'received', 'failed', 'queued')),
  error           text,
  sent_by         uuid,
  sent_at         timestamptz NOT NULL DEFAULT now(),
  created_at      timestamptz NOT NULL DEFAULT now(),
  UNIQUE (mail_account_id, provider_id)
);
CREATE INDEX messages_ws_idx ON crm.messages (workspace_id, sent_at DESC);
CREATE INDEX messages_thread_idx ON crm.messages (workspace_id, thread_id);
CREATE TABLE crm.message_links (
  message_id   uuid NOT NULL REFERENCES crm.messages ON DELETE CASCADE,
  workspace_id uuid NOT NULL,
  object_key   text NOT NULL,
  record_id    uuid NOT NULL,
  PRIMARY KEY (message_id, object_key, record_id)
);
CREATE INDEX message_links_record_idx ON crm.message_links (workspace_id, object_key, record_id);
CREATE TABLE crm.mail_blocklist (
  workspace_id uuid NOT NULL,
  identity_id  uuid NOT NULL REFERENCES crm.identities ON DELETE CASCADE,
  pattern      text NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (workspace_id, identity_id, pattern)
);

-- ---- Email campaigns ----
CREATE TABLE crm.campaigns (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces,
  name         text NOT NULL,
  subject      text NOT NULL DEFAULT '',
  body_html    text NOT NULL DEFAULT '',
  from_name    text NOT NULL DEFAULT '',
  reply_to     text NOT NULL DEFAULT '',
  object_key   text NOT NULL DEFAULT 'contacts',
  email_field  text NOT NULL DEFAULT 'email',
  filter       jsonb NOT NULL DEFAULT '{}',
  status       text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'scheduled', 'sending', 'sent', 'failed', 'cancelled')),
  scheduled_at timestamptz,
  stats        jsonb NOT NULL DEFAULT '{}',
  created_by   uuid,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  sent_at      timestamptz
);
CREATE TABLE crm.campaign_recipients (
  campaign_id  uuid NOT NULL REFERENCES crm.campaigns ON DELETE CASCADE,
  workspace_id uuid NOT NULL,
  record_id    uuid NOT NULL,
  email        text NOT NULL,
  name         text NOT NULL DEFAULT '',
  status       text NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'failed', 'unsubscribed', 'skipped')),
  error        text,
  token        text NOT NULL UNIQUE,
  sent_at      timestamptz,
  PRIMARY KEY (campaign_id, record_id)
);
CREATE TABLE crm.unsubscribes (
  workspace_id uuid NOT NULL,
  email        text NOT NULL,
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (workspace_id, email)
);

-- ---- Single sign-on (SAML per product; Google / Microsoft / LinkedIn use sso_links) ----
CREATE TABLE crm.sso_providers (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id     uuid NOT NULL UNIQUE REFERENCES crm.workspaces,
  kind             text NOT NULL DEFAULT 'saml' CHECK (kind IN ('saml')),
  name             text NOT NULL,
  status           text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
  idp_metadata_xml text NOT NULL,
  domains          text[] NOT NULL DEFAULT '{}',
  jit_provisioning boolean NOT NULL DEFAULT false,
  default_role_key text NOT NULL DEFAULT 'STAFF',
  sp_key_enc       bytea,   -- this product's signing key and certificate (generated)
  sp_cert_pem      text,
  created_by       uuid,
  created_at       timestamptz NOT NULL DEFAULT now(),
  updated_at       timestamptz NOT NULL DEFAULT now()
);
-- How a session was signed in, so a product can require its own sign-in methods.
ALTER TABLE crm.sessions ADD COLUMN auth_method text NOT NULL DEFAULT 'password';

-- ---- Teams (record assignment, "my team" filters) ----
ALTER TABLE crm.teams ADD COLUMN description text NOT NULL DEFAULT '';
ALTER TABLE crm.teams ADD COLUMN created_at timestamptz NOT NULL DEFAULT now();
-- crm.branches was never used; drop it only when it holds nothing.
DO $$
BEGIN
  IF NOT EXISTS (SELECT 1 FROM crm.branches) THEN
    DROP TABLE crm.branches;
  END IF;
END $$;

-- ---- Outbox relay bookkeeping ----
ALTER TABLE crm.outbox_events ADD COLUMN attempts int NOT NULL DEFAULT 0;
