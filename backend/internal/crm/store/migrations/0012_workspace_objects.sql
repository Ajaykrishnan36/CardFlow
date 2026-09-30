-- D-79: a product's admins can create their own objects. Such an object belongs to one
-- product (workspace) and appears only there. Additive: existing objects stay platform-wide.
ALTER TABLE crm.object_definitions ADD COLUMN IF NOT EXISTS workspace_id uuid REFERENCES crm.workspaces;
CREATE INDEX IF NOT EXISTS object_definitions_workspace_idx ON crm.object_definitions (workspace_id) WHERE workspace_id IS NOT NULL;
