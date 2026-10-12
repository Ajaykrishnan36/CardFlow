-- Rules an admin writes for an object (D-141): a record that matches a rule's conditions
-- can't be saved, and the person sees the rule's message.
CREATE TABLE IF NOT EXISTS crm.validation_rules (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  object_key   text NOT NULL,
  name         text NOT NULL,
  message      text NOT NULL,
  field_key    text,
  filter       jsonb NOT NULL,
  is_active    boolean NOT NULL DEFAULT true,
  created_by   uuid,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS validation_rules_object ON crm.validation_rules (workspace_id, object_key) WHERE is_active;
CREATE UNIQUE INDEX IF NOT EXISTS validation_rules_name ON crm.validation_rules (workspace_id, object_key, lower(name));

-- What a field starts with on a new record when nothing is given (D-141). Works for
-- standard and custom fields alike.
CREATE TABLE IF NOT EXISTS crm.field_defaults (
  workspace_id uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  object_key   text NOT NULL,
  field_key    text NOT NULL,
  value        jsonb NOT NULL,
  updated_by   uuid,
  updated_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (workspace_id, object_key, field_key)
);
