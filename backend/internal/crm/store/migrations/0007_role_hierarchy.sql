-- Role hierarchy (D-48): roles form a tree and only decide record sharing (you see your
-- own records and those owned by people in roles below yours); permissions come from
-- permission sets. system_key marks the default permission sets a workspace starts with.
ALTER TABLE crm.roles ADD COLUMN parent_role_id uuid REFERENCES crm.roles (id) ON DELETE SET NULL;
ALTER TABLE crm.permission_sets ADD COLUMN system_key text;
CREATE UNIQUE INDEX permission_sets_system_key_idx ON crm.permission_sets (workspace_id, system_key) WHERE system_key IS NOT NULL;
