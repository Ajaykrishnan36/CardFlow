-- Tenants (D-156): the brand above the businesses. A tenant has one permanent address
-- (/<slug>/login, /<slug>/home …) that never changes, even when a business is renamed;
-- PSG School, PSG College and PSG Medical College are businesses of the one tenant "psg",
-- each with its own records, team and roles. Sign-in on /<slug>/login is only for people
-- who belong to one of its businesses.
CREATE TABLE IF NOT EXISTS crm.tenants (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    slug           text NOT NULL UNIQUE,
    name           text NOT NULL,
    owner_identity uuid REFERENCES crm.identities(id) ON DELETE SET NULL,
    custom_domain  text UNIQUE,
    status         text NOT NULL DEFAULT 'active',
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE crm.workspaces ADD COLUMN IF NOT EXISTS tenant_id uuid REFERENCES crm.tenants(id);
CREATE INDEX IF NOT EXISTS workspaces_tenant_idx ON crm.workspaces (tenant_id);

-- Existing businesses (nothing is moved or removed): everything one person created
-- themselves becomes one tenant, named and addressed after their first business…
INSERT INTO crm.tenants (slug, name, owner_identity, created_at)
SELECT DISTINCT ON (w.created_by_identity) w.code, w.name, w.created_by_identity, w.created_at
FROM crm.workspaces w
WHERE NOT w.is_platform AND w.tenant_id IS NULL AND w.created_by_identity IS NOT NULL AND w.origin = 'self_serve'
ORDER BY w.created_by_identity, w.created_at
ON CONFLICT (slug) DO NOTHING;

UPDATE crm.workspaces w SET tenant_id = t.id
FROM crm.tenants t
WHERE w.tenant_id IS NULL AND NOT w.is_platform AND w.origin = 'self_serve' AND w.created_by_identity = t.owner_identity;

-- …and every other business (set up from the owner console) is a tenant of its own.
INSERT INTO crm.tenants (slug, name, created_at)
SELECT w.code, w.name, w.created_at FROM crm.workspaces w WHERE NOT w.is_platform AND w.tenant_id IS NULL
ON CONFLICT (slug) DO NOTHING;

UPDATE crm.workspaces w SET tenant_id = t.id
FROM crm.tenants t
WHERE w.tenant_id IS NULL AND NOT w.is_platform AND t.slug = w.code;

-- A business's own domain belongs to its tenant now.
UPDATE crm.tenants t SET custom_domain = w.custom_domain
FROM crm.workspaces w
WHERE w.tenant_id = t.id AND w.custom_domain IS NOT NULL AND t.custom_domain IS NULL
  AND NOT EXISTS (SELECT 1 FROM crm.tenants o WHERE o.custom_domain = w.custom_domain);
