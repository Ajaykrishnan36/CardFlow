-- New businesses wait for the platform owner's approval (D-161). Someone's first business
-- from "Create your own CRM" is 'pending' until the owner approves it from the lead it
-- created in the owner console; only then does its URL work and get sent. The status list
-- only grows; nothing is changed or removed.
ALTER TABLE crm.workspaces DROP CONSTRAINT IF EXISTS workspaces_status_check;
ALTER TABLE crm.workspaces ADD CONSTRAINT workspaces_status_check
  CHECK (status IN ('draft', 'provisioning', 'active', 'failed', 'suspended', 'pending'));

ALTER TABLE crm.workspaces ADD COLUMN IF NOT EXISTS approved_at timestamptz;
ALTER TABLE crm.workspaces ADD COLUMN IF NOT EXISTS approved_by uuid REFERENCES crm.identities(id) ON DELETE SET NULL;
-- The lead in the platform CRM that the application created.
ALTER TABLE crm.workspaces ADD COLUMN IF NOT EXISTS application_lead_id uuid REFERENCES crm.leads(id) ON DELETE SET NULL;
CREATE INDEX IF NOT EXISTS workspaces_application_lead_idx ON crm.workspaces (application_lead_id) WHERE application_lead_id IS NOT NULL;
