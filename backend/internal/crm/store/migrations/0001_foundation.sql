-- 0001: CRM foundation — identity, platform, access, audit/outbox (PRD §13.2, §13.3 shared tables).
-- Everything lives in schema "crm" so nothing can collide with CardFlow tables (DECISIONS D-04).

CREATE SCHEMA IF NOT EXISTS crm;

-- ===== Identity (global, no RLS) =====
CREATE TABLE crm.identities (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  display_name      text NOT NULL,
  is_platform_owner boolean NOT NULL DEFAULT false,
  status            text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended', 'deleted')),
  locale            text NOT NULL DEFAULT 'en',
  timezone          text NOT NULL DEFAULT 'Asia/Kolkata',
  last_login_at     timestamptz,
  created_at        timestamptz NOT NULL DEFAULT now(),
  updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE crm.verified_identifiers (
  id               uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  identity_id      uuid NOT NULL REFERENCES crm.identities ON DELETE CASCADE,
  kind             text NOT NULL CHECK (kind IN ('email', 'phone', 'login_id')),
  value_normalized text NOT NULL,              -- lower-case email, E.164 phone
  namespace        text NOT NULL DEFAULT 'global', -- 'global' or workspace code for login_id
  verified_at      timestamptz,
  UNIQUE (kind, namespace, value_normalized)
);
CREATE INDEX verified_identifiers_identity_idx ON crm.verified_identifiers (identity_id);

CREATE TABLE crm.password_credentials (
  identity_id     uuid PRIMARY KEY REFERENCES crm.identities ON DELETE CASCADE,
  hash            text NOT NULL,
  must_change     boolean NOT NULL DEFAULT false,
  failed_attempts int NOT NULL DEFAULT 0,
  locked_until    timestamptz,
  updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE crm.mfa_methods (
  id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  identity_id          uuid NOT NULL REFERENCES crm.identities ON DELETE CASCADE,
  kind                 text NOT NULL DEFAULT 'totp',
  secret_enc           bytea NOT NULL,
  recovery_codes_hash  text[] NOT NULL DEFAULT '{}',
  last_used_step       bigint NOT NULL DEFAULT 0, -- rejects TOTP code replay within its window
  confirmed_at         timestamptz,
  created_at           timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX mfa_methods_identity_idx ON crm.mfa_methods (identity_id);

CREATE TABLE crm.sso_links (
  provider      text NOT NULL,
  subject       text NOT NULL,
  identity_id   uuid NOT NULL REFERENCES crm.identities ON DELETE CASCADE,
  email_at_link text,
  created_at    timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (provider, subject)
);

CREATE TABLE crm.sessions (
  id                     uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  identity_id            uuid NOT NULL REFERENCES crm.identities ON DELETE CASCADE,
  token_hash             bytea NOT NULL UNIQUE,
  audience               text NOT NULL DEFAULT 'workspace' CHECK (audience IN ('owner', 'workspace')),
  selected_membership_id uuid,
  selected_product_id    uuid,
  mfa_required           boolean NOT NULL DEFAULT false, -- DECISIONS D-13
  mfa_passed             boolean NOT NULL DEFAULT false,
  mfa_attempts           int NOT NULL DEFAULT 0,
  recent_auth_at         timestamptz,
  privileged             boolean NOT NULL DEFAULT false,
  ip                     inet,
  user_agent             text,
  created_at             timestamptz NOT NULL DEFAULT now(),
  last_seen_at           timestamptz NOT NULL DEFAULT now(),
  idle_expires_at        timestamptz NOT NULL,
  absolute_expires_at    timestamptz NOT NULL,
  revoked_at             timestamptz
);
CREATE INDEX sessions_identity_idx ON crm.sessions (identity_id) WHERE revoked_at IS NULL;

CREATE TABLE crm.otp_challenges (
  id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  channel     text NOT NULL CHECK (channel IN ('email', 'sms', 'whatsapp')),
  destination text NOT NULL,
  purpose     text NOT NULL,
  code_hash   bytea NOT NULL,
  attempts    int NOT NULL DEFAULT 0,
  expires_at  timestamptz NOT NULL,
  consumed_at timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE crm.password_resets (
  token_hash  bytea PRIMARY KEY,
  identity_id uuid NOT NULL REFERENCES crm.identities ON DELETE CASCADE,
  expires_at  timestamptz NOT NULL,
  used_at     timestamptz,
  created_at  timestamptz NOT NULL DEFAULT now()
);

-- ===== Platform (owner-managed) =====
CREATE TABLE crm.products (
  id              uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  key             text NOT NULL UNIQUE CHECK (key ~ '^[a-z][a-z0-9_]{2,40}$'),
  name            text NOT NULL,
  description     text,
  icon            text,
  status          text NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'active', 'archived')),
  current_version int,
  draft_config    jsonb NOT NULL DEFAULT '{}',
  created_by      uuid REFERENCES crm.identities,
  created_at      timestamptz NOT NULL DEFAULT now(),
  updated_at      timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE crm.product_versions (
  product_id   uuid NOT NULL REFERENCES crm.products,
  version      int NOT NULL,
  config       jsonb NOT NULL,
  published_at timestamptz NOT NULL DEFAULT now(),
  published_by uuid REFERENCES crm.identities,
  PRIMARY KEY (product_id, version)
);

CREATE TABLE crm.workspaces (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  code               text NOT NULL UNIQUE CHECK (code ~ '^[a-z0-9-]{3,40}$'),
  name               text NOT NULL,
  is_platform        boolean NOT NULL DEFAULT false,
  status             text NOT NULL DEFAULT 'draft'
                       CHECK (status IN ('draft', 'provisioning', 'active', 'failed', 'suspended')),
  timezone           text NOT NULL DEFAULT 'Asia/Kolkata',
  locale             text NOT NULL DEFAULT 'en',
  currency           char(3) NOT NULL DEFAULT 'INR',
  branding           jsonb NOT NULL DEFAULT '{}',
  custom_domain      text UNIQUE,
  domain_verified_at timestamptz,
  created_at         timestamptz NOT NULL DEFAULT now(),
  updated_at         timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX one_platform_workspace ON crm.workspaces (is_platform) WHERE is_platform;

CREATE TABLE crm.workspace_products (
  workspace_id   uuid NOT NULL REFERENCES crm.workspaces,
  product_id     uuid NOT NULL REFERENCES crm.products,
  config_version int NOT NULL,
  overrides      jsonb NOT NULL DEFAULT '{}',
  status         text NOT NULL DEFAULT 'active' CHECK (status IN ('active', 'suspended')),
  created_at     timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (workspace_id, product_id)
);

-- ===== Access =====
CREATE TABLE crm.memberships (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces,
  identity_id  uuid NOT NULL REFERENCES crm.identities,
  status       text NOT NULL DEFAULT 'invited' CHECK (status IN ('invited', 'active', 'suspended', 'revoked')),
  user_type    text,
  auth_version int NOT NULL DEFAULT 1,
  created_by   uuid REFERENCES crm.identities,
  created_at   timestamptz NOT NULL DEFAULT now(),
  UNIQUE (workspace_id, identity_id)
);
CREATE INDEX memberships_identity_idx ON crm.memberships (identity_id, status);

CREATE TABLE crm.roles (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces,
  key          text NOT NULL,
  name         text NOT NULL,
  is_system    boolean NOT NULL DEFAULT false,
  rank         int NOT NULL, -- SUPER_ADMIN=100, ADMIN=80, STAFF=50, END_USER=10
  base_rules   jsonb NOT NULL,
  UNIQUE (workspace_id, key)
);

CREATE TABLE crm.permission_sets (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL REFERENCES crm.workspaces,
  name         text NOT NULL,
  rules        jsonb NOT NULL,
  UNIQUE (workspace_id, name)
);

CREATE TABLE crm.role_assignments (
  id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id  uuid NOT NULL,
  membership_id uuid NOT NULL REFERENCES crm.memberships ON DELETE CASCADE,
  role_id       uuid NOT NULL REFERENCES crm.roles,
  product_ids   uuid[] NOT NULL,
  branch_ids    uuid[] NOT NULL DEFAULT '{}',
  granted_by    uuid NOT NULL REFERENCES crm.identities,
  expires_at    timestamptz,
  created_at    timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX role_assignments_membership_idx ON crm.role_assignments (membership_id);

CREATE TABLE crm.membership_permission_sets (
  workspace_id      uuid NOT NULL,
  membership_id     uuid NOT NULL REFERENCES crm.memberships ON DELETE CASCADE,
  permission_set_id uuid NOT NULL REFERENCES crm.permission_sets,
  granted_by        uuid NOT NULL REFERENCES crm.identities,
  expires_at        timestamptz,
  PRIMARY KEY (membership_id, permission_set_id)
);

CREATE TABLE crm.teams (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL,
  name         text NOT NULL,
  UNIQUE (workspace_id, name)
);

CREATE TABLE crm.team_members (
  workspace_id  uuid NOT NULL,
  team_id       uuid REFERENCES crm.teams ON DELETE CASCADE,
  membership_id uuid REFERENCES crm.memberships ON DELETE CASCADE,
  PRIMARY KEY (team_id, membership_id)
);

CREATE TABLE crm.branches (
  id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id uuid NOT NULL,
  name         text NOT NULL,
  UNIQUE (workspace_id, name)
);

CREATE TABLE crm.invitations (
  id                 uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id       uuid NOT NULL,
  membership_id      uuid NOT NULL REFERENCES crm.memberships,
  lead_id            uuid,
  token_hash         bytea NOT NULL UNIQUE,
  intended_role_id   uuid NOT NULL REFERENCES crm.roles,
  product_ids        uuid[] NOT NULL,
  permission_set_ids uuid[] NOT NULL DEFAULT '{}',
  delivery_channel   text NOT NULL,
  status             text NOT NULL DEFAULT 'pending'
                       CHECK (status IN ('pending', 'delivered', 'accepted', 'expired', 'revoked', 'delivery_failed')),
  expires_at         timestamptz NOT NULL,
  accepted_at        timestamptz,
  created_by         uuid NOT NULL REFERENCES crm.identities,
  created_at         timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE crm.api_keys (
  id                uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  workspace_id      uuid NOT NULL,
  name              text NOT NULL,
  prefix            text NOT NULL,
  key_hash          bytea NOT NULL UNIQUE,
  permission_set_id uuid NOT NULL REFERENCES crm.permission_sets,
  product_ids       uuid[] NOT NULL,
  last_used_at      timestamptz,
  revoked_at        timestamptz,
  created_by        uuid NOT NULL,
  created_at        timestamptz NOT NULL DEFAULT now()
);

-- ===== Shared infrastructure =====
CREATE TABLE crm.audit_events (
  id           bigserial PRIMARY KEY,
  workspace_id uuid,
  actor_id     uuid,
  actor_kind   text NOT NULL, -- identity | system | api_key
  action       text NOT NULL,
  entity_type  text,
  entity_id    uuid,
  before       jsonb,
  after        jsonb,
  reason       text,
  ip           inet,
  request_id   text,
  created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_workspace_idx ON crm.audit_events (workspace_id, created_at DESC);
CREATE INDEX audit_events_actor_idx ON crm.audit_events (actor_id, created_at DESC);

CREATE TABLE crm.outbox_events (
  id             bigserial PRIMARY KEY,
  event_id       uuid NOT NULL UNIQUE DEFAULT gen_random_uuid(),
  workspace_id   uuid,
  product_id     uuid,
  event_type     text NOT NULL,
  schema_version int NOT NULL DEFAULT 1,
  actor_id       uuid,
  entity_type    text,
  entity_id      uuid,
  entity_version int,
  payload        jsonb NOT NULL,
  occurred_at    timestamptz NOT NULL DEFAULT now(),
  relayed_at     timestamptz
);
CREATE INDEX outbox_unrelayed ON crm.outbox_events (id) WHERE relayed_at IS NULL;

CREATE TABLE crm.idempotency_keys (
  workspace_id uuid,
  actor_id     uuid NOT NULL,
  key          text NOT NULL,
  request_hash bytea NOT NULL,
  response     jsonb,
  status_code  int,
  created_at   timestamptz NOT NULL DEFAULT now(),
  PRIMARY KEY (actor_id, key)
);
