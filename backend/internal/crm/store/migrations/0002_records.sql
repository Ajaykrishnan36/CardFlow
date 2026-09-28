-- M2/M3 slice: Platform CRM records (leads, accounts, contacts), lead conversion,
-- custom fields and record page layouts. See docs/DECISIONS.md D-21..D-26.

CREATE TABLE crm.code_counters (
  workspace_id uuid NOT NULL,
  prefix       text NOT NULL,
  next_value   bigint NOT NULL DEFAULT 1,
  PRIMARY KEY (workspace_id, prefix)
);

CREATE TABLE crm.leads (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id      uuid NOT NULL REFERENCES crm.workspaces,
  product_id        uuid REFERENCES crm.products, -- interested product (D-22)
  code              text NOT NULL,
  salutation        text,
  first_name        text,
  last_name         text,
  title             text,
  organization      text,
  email             text,
  phone             text,
  mobile            text,
  website           text,
  industry          text,
  source            text NOT NULL DEFAULT 'manual',
  status            text NOT NULL DEFAULT 'new',
  rating            text,
  lost_reason       text,
  annual_revenue    numeric(18, 2),
  employees         int,
  street            text,
  city              text,
  state             text,
  postal_code       text,
  country           text,
  description       text,
  user_type         text,
  intended_role_key text,
  owner_id          uuid REFERENCES crm.identities,
  identity_id       uuid REFERENCES crm.identities,
  score             int NOT NULL DEFAULT 0,
  tags              text[] NOT NULL DEFAULT '{}',
  next_follow_up_at timestamptz,
  last_activity_at  timestamptz,
  converted_at      timestamptz,
  converted_account_id uuid,
  converted_contact_id uuid,
  custom            jsonb NOT NULL DEFAULT '{}',
  version           int NOT NULL DEFAULT 1,
  deleted_at        timestamptz,
  created_by        uuid,
  updated_by        uuid,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, code)
);
CREATE INDEX leads_ws_created_idx ON crm.leads (workspace_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX leads_ws_email_idx ON crm.leads (workspace_id, lower(email)) WHERE deleted_at IS NULL;

CREATE TABLE crm.accounts (
  id                    uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id          uuid NOT NULL REFERENCES crm.workspaces,
  code                  text NOT NULL,
  kind                  text NOT NULL DEFAULT 'business' CHECK (kind IN ('business', 'individual')),
  name                  text NOT NULL,
  type                  text,
  lifecycle             text NOT NULL DEFAULT 'prospect',
  industry              text,
  rating                text,
  ownership             text,
  website               text,
  email                 text,
  phone                 text,
  annual_revenue        numeric(18, 2),
  employees             int,
  billing_street        text,
  billing_city          text,
  billing_state         text,
  billing_postal_code   text,
  billing_country       text,
  shipping_street       text,
  shipping_city         text,
  shipping_state        text,
  shipping_postal_code  text,
  shipping_country      text,
  description           text,
  parent_account_id     uuid REFERENCES crm.accounts,
  customer_workspace_id uuid REFERENCES crm.workspaces, -- provisioned tenant (platform accounts only)
  owner_id              uuid REFERENCES crm.identities,
  identity_id           uuid REFERENCES crm.identities,
  custom                jsonb NOT NULL DEFAULT '{}',
  version               int NOT NULL DEFAULT 1,
  deleted_at            timestamptz,
  created_by            uuid,
  updated_by            uuid,
  created_at            timestamptz NOT NULL DEFAULT now(),
  updated_at            timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, code)
);
CREATE INDEX accounts_ws_created_idx ON crm.accounts (workspace_id, created_at DESC) WHERE deleted_at IS NULL;

CREATE TABLE crm.contacts (
  id                  uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id        uuid NOT NULL REFERENCES crm.workspaces,
  code                text NOT NULL,
  account_id          uuid REFERENCES crm.accounts, -- primary account (D-23)
  salutation          text,
  first_name          text,
  last_name           text,
  title               text,
  department          text,
  email               text,
  phone               text,
  mobile              text,
  lead_source         text,
  birthdate           date,
  mailing_street      text,
  mailing_city        text,
  mailing_state       text,
  mailing_postal_code text,
  mailing_country     text,
  description         text,
  owner_id            uuid REFERENCES crm.identities,
  identity_id         uuid REFERENCES crm.identities,
  custom              jsonb NOT NULL DEFAULT '{}',
  version             int NOT NULL DEFAULT 1,
  deleted_at          timestamptz,
  created_by          uuid,
  updated_by          uuid,
  created_at          timestamptz NOT NULL DEFAULT now(),
  updated_at          timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, code)
);
CREATE INDEX contacts_ws_created_idx ON crm.contacts (workspace_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX contacts_account_idx ON crm.contacts (account_id) WHERE deleted_at IS NULL;

ALTER TABLE crm.leads
  ADD CONSTRAINT leads_converted_account_fk FOREIGN KEY (converted_account_id) REFERENCES crm.accounts,
  ADD CONSTRAINT leads_converted_contact_fk FOREIGN KEY (converted_contact_id) REFERENCES crm.contacts;

CREATE TABLE crm.lead_conversions (
  id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id   uuid NOT NULL,
  lead_id        uuid NOT NULL REFERENCES crm.leads,
  account_id     uuid NOT NULL REFERENCES crm.accounts,
  contact_id     uuid REFERENCES crm.contacts,
  workspace_provisioned_id uuid REFERENCES crm.workspaces,
  invitation_id  uuid REFERENCES crm.invitations,
  trigger        text NOT NULL DEFAULT 'manual',
  status         text NOT NULL DEFAULT 'converted' CHECK (status IN ('in_review', 'converted', 'failed', 'reversed')),
  converted_by   uuid,
  created_at     timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX one_active_conversion ON crm.lead_conversions (workspace_id, lead_id) WHERE status = 'converted';

-- Custom fields per object (standard fields are defined in code; D-24).
CREATE TABLE crm.field_definitions (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces,
  object_key   text NOT NULL,
  key          text NOT NULL CHECK (key ~ '^[a-z][a-z0-9_]{1,40}$'),
  label        text NOT NULL,
  type         text NOT NULL,
  is_required  boolean NOT NULL DEFAULT false,
  options      jsonb NOT NULL DEFAULT '{}',
  help_text    text,
  position     int NOT NULL DEFAULT 0,
  status       text NOT NULL DEFAULT 'published' CHECK (status IN ('draft', 'published', 'archived')),
  created_by   uuid,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, object_key, key)
);

-- Record page layout per object (role-specific layouts later; D-25).
CREATE TABLE crm.layouts (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces,
  object_key   text NOT NULL,
  definition   jsonb NOT NULL,
  status       text NOT NULL DEFAULT 'published',
  updated_by   uuid,
  updated_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, object_key)
);
