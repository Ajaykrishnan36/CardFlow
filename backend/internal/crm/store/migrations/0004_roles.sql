-- Roles become editable per workspace, and workspaces can add custom roles (D-34).
ALTER TABLE crm.roles
  ADD COLUMN description text,
  ADD COLUMN customized  boolean NOT NULL DEFAULT false, -- built-in role whose rules were edited here
  ADD COLUMN created_by  uuid REFERENCES crm.identities,
  ADD COLUMN created_at  timestamptz NOT NULL DEFAULT now(),
  ADD COLUMN updated_at  timestamptz NOT NULL DEFAULT now();
