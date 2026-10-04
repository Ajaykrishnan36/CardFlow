-- D-94: a customer creates their own business (a workspace) and becomes its Super Admin.

-- Who created the business and how: 'owner' (from the Owner Console) or 'self_serve'.
ALTER TABLE crm.workspaces ADD COLUMN IF NOT EXISTS created_by_identity uuid REFERENCES crm.identities;
ALTER TABLE crm.workspaces ADD COLUMN IF NOT EXISTS origin text NOT NULL DEFAULT 'owner'
  CHECK (origin IN ('owner', 'self_serve', 'system'));
-- The business profile people fill in (phone, email, website, address, industry, tax ids, logo).
ALTER TABLE crm.workspaces ADD COLUMN IF NOT EXISTS profile jsonb NOT NULL DEFAULT '{}';
CREATE INDEX IF NOT EXISTS workspaces_creator_idx ON crm.workspaces (created_by_identity) WHERE created_by_identity IS NOT NULL;

-- A directory listing can be the public profile of a business (the listing stays public
-- data; the business's CRM records never appear in it).
DO $$
BEGIN
  IF to_regclass('public.businesses') IS NOT NULL THEN
    ALTER TABLE public.businesses ADD COLUMN IF NOT EXISTS workspace_id uuid REFERENCES crm.workspaces;
    CREATE INDEX IF NOT EXISTS idx_businesses_workspace ON public.businesses (workspace_id) WHERE workspace_id IS NOT NULL;
  END IF;
END $$;
