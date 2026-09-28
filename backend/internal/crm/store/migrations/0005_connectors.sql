-- Connected apps (D-36): Business Card Snap / CardFlow syncs its users into a workspace.
CREATE TABLE crm.external_links (
  workspace_id   uuid NOT NULL REFERENCES crm.workspaces,
  system         text NOT NULL,          -- e.g. 'cardflow'
  external_type  text NOT NULL,          -- e.g. 'user'
  external_id    text NOT NULL,
  lead_id        uuid REFERENCES crm.leads,
  account_id     uuid REFERENCES crm.accounts,
  contact_id     uuid REFERENCES crm.contacts,
  identity_id    uuid REFERENCES crm.identities,
  last_login_at  timestamptz,            -- last app sign-in already turned into an activity
  synced_at      timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (system, external_type, external_id)
);
CREATE INDEX external_links_account_idx ON crm.external_links (account_id);

-- Timeline entries shown on records (sign-ups, sign-ins, tickets…).
CREATE TABLE crm.activities (
  id           bigserial PRIMARY KEY,
  workspace_id uuid NOT NULL REFERENCES crm.workspaces,
  account_id   uuid REFERENCES crm.accounts,
  contact_id   uuid REFERENCES crm.contacts,
  lead_id      uuid REFERENCES crm.leads,
  kind         text NOT NULL,            -- app.signed_up | app.signed_in | ticket.opened | ticket.replied
  title        text NOT NULL,
  detail       jsonb NOT NULL DEFAULT '{}',
  source       text NOT NULL DEFAULT 'crm',
  dedupe_key   text UNIQUE,
  occurred_at  timestamptz NOT NULL DEFAULT now(),
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX activities_account_idx ON crm.activities (account_id, occurred_at DESC);
CREATE INDEX activities_contact_idx ON crm.activities (contact_id, occurred_at DESC);
CREATE INDEX activities_ws_idx ON crm.activities (workspace_id, kind, occurred_at DESC);

CREATE TABLE crm.connector_state (
  key        text PRIMARY KEY,
  value      jsonb NOT NULL DEFAULT '{}',
  updated_at timestamptz NOT NULL DEFAULT now()
);
