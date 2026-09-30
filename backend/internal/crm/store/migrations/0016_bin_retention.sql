-- D-84: a product may choose to empty its recycle bin automatically. NULL (the default)
-- keeps deleted records until someone restores or deletes them.
ALTER TABLE crm.workspaces ADD COLUMN IF NOT EXISTS bin_retention_days integer
  CHECK (bin_retention_days IS NULL OR bin_retention_days BETWEEN 30 AND 3650);
