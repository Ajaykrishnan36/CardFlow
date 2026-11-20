-- A document shared with a customer by a link (D-184, catalogue B7): the token is stored
-- hashed; every open is counted and logged on the document. Only additions.
CREATE TABLE IF NOT EXISTS crm.doc_shares (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces(id),
  object_key text NOT NULL,
  record_id uuid NOT NULL,
  token_hash bytea NOT NULL UNIQUE,
  created_by uuid,
  created_at timestamptz NOT NULL DEFAULT now(),
  expires_at timestamptz NOT NULL,
  opens integer NOT NULL DEFAULT 0,
  last_opened_at timestamptz
);
CREATE INDEX IF NOT EXISTS doc_shares_record ON crm.doc_shares (workspace_id, record_id);
