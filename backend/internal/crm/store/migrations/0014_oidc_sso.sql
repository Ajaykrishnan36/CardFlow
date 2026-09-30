-- D-82: a product's single sign-on can be OpenID Connect instead of SAML. Additive:
-- existing SAML set-ups keep working unchanged.
ALTER TABLE crm.sso_providers DROP CONSTRAINT IF EXISTS sso_providers_kind_check;
ALTER TABLE crm.sso_providers ADD CONSTRAINT sso_providers_kind_check CHECK (kind IN ('saml', 'oidc'));
ALTER TABLE crm.sso_providers ALTER COLUMN idp_metadata_xml SET DEFAULT '';
ALTER TABLE crm.sso_providers ADD COLUMN IF NOT EXISTS oidc_issuer text NOT NULL DEFAULT '';
ALTER TABLE crm.sso_providers ADD COLUMN IF NOT EXISTS oidc_client_id text NOT NULL DEFAULT '';
ALTER TABLE crm.sso_providers ADD COLUMN IF NOT EXISTS oidc_secret_enc bytea;
