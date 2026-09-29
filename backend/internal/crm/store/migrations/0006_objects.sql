-- Objects beyond leads / accounts / contacts (D-45): standard objects behind the product
-- modules (opportunities, tasks, calendar…) and owner-defined custom objects. One
-- definition per object (platform-wide, enabled per product through its module); every
-- record lives in crm.object_records, and each object gets a view crm.obj_<key> so the
-- record engine reads and writes it exactly like a dedicated table.

CREATE TABLE crm.object_definitions (
  key          text PRIMARY KEY CHECK (key ~ '^[a-z][a-z0-9_]{1,40}$'),
  module       text NOT NULL,
  singular     text NOT NULL,
  plural       text NOT NULL,
  description  text NOT NULL DEFAULT '',
  icon         text NOT NULL DEFAULT 'box',
  prefix       text NOT NULL UNIQUE CHECK (prefix ~ '^[A-Z][A-Z0-9]{1,5}$'),
  definition   jsonb NOT NULL DEFAULT '{}', -- name label, statuses, fields, list columns
  is_standard  boolean NOT NULL DEFAULT false,
  status       text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'archived')),
  created_by   uuid,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE crm.object_records (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces,
  object_key   text NOT NULL REFERENCES crm.object_definitions (key),
  code         text NOT NULL,
  name         text NOT NULL,
  status       text,
  owner_id     uuid REFERENCES crm.identities,
  custom       jsonb NOT NULL DEFAULT '{}',
  version      int NOT NULL DEFAULT 1,
  deleted_at   timestamptz,
  created_by   uuid,
  updated_by   uuid,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, object_key, code)
);
CREATE INDEX object_records_ws_obj_idx ON crm.object_records (workspace_id, object_key, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX object_records_custom_idx ON crm.object_records USING gin (custom);
