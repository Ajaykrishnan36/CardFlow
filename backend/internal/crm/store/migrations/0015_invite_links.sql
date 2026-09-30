-- D-83: a product's shareable invite link. Anyone with the link whose email is on one of
-- the listed company domains can join (after proving the address with a one-time code).
CREATE TABLE IF NOT EXISTS crm.invite_links (
  workspace_id uuid PRIMARY KEY REFERENCES crm.workspaces,
  token_hash   bytea NOT NULL UNIQUE,
  token_enc    bytea NOT NULL,
  domains      text[] NOT NULL DEFAULT '{}',
  role_key     text NOT NULL DEFAULT 'STAFF',
  status       text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'disabled')),
  uses         integer NOT NULL DEFAULT 0,
  created_by   uuid,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
