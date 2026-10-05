-- 0023 — a copy of the rows of a business that was removed on the owner's instruction (D-129).
-- Not customer-facing; lets a removal be looked at, or put back by hand, afterwards.
CREATE TABLE IF NOT EXISTS crm.deleted_workspace_archive (
  id             bigserial PRIMARY KEY,
  workspace_code text NOT NULL,
  table_name     text NOT NULL,
  row_data       jsonb NOT NULL,
  reason         text NOT NULL DEFAULT '',
  archived_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS deleted_workspace_archive_idx ON crm.deleted_workspace_archive (workspace_code, table_name);
