-- 0024 — how a business arranges its dashboard and its menu (D-130): which items show and
-- in what order, kept separately for the desktop layout and the phone layout. The owner
-- console's own arrangement is stored against the platform workspace.
CREATE TABLE IF NOT EXISTS crm.ui_layouts (
  workspace_id uuid NOT NULL REFERENCES crm.workspaces ON DELETE CASCADE,
  surface      text NOT NULL CHECK (surface IN ('dashboard', 'nav')),
  device       text NOT NULL CHECK (device IN ('desktop', 'mobile')),
  definition   jsonb NOT NULL DEFAULT '{}',
  updated_by   uuid REFERENCES crm.identities,
  updated_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (workspace_id, surface, device)
);
