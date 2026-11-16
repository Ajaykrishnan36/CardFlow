-- A business's own lead statuses and deal stages (D-182). When set, they replace the ones
-- its product setup gives; the fixed keys (converted, lost, closed_won, closed_lost) stay.
-- Only additions.
ALTER TABLE crm.workspaces ADD COLUMN IF NOT EXISTS stage_settings jsonb NOT NULL DEFAULT '{}'::jsonb;
