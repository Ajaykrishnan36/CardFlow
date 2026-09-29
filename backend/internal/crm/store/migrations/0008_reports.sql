-- Reports & dashboards (D-50). A report is a saved question about one object (filters,
-- a group-by, a measure, a chart); it always runs as the person looking at it, so their
-- permissions, field access and role hierarchy decide what it counts. A dashboard is an
-- arrangement of reports.
CREATE TABLE crm.reports (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces,
  name         text NOT NULL,
  description  text NOT NULL DEFAULT '',
  object_key   text NOT NULL,
  definition   jsonb NOT NULL DEFAULT '{}',
  owner_id     uuid REFERENCES crm.identities,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX reports_ws_idx ON crm.reports (workspace_id, updated_at DESC);

CREATE TABLE crm.dashboards (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces,
  name         text NOT NULL,
  description  text NOT NULL DEFAULT '',
  widgets      jsonb NOT NULL DEFAULT '[]',
  owner_id     uuid REFERENCES crm.identities,
  created_at   timestamptz NOT NULL DEFAULT now(),
  updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX dashboards_ws_idx ON crm.dashboards (workspace_id, updated_at DESC);
