package seed

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/platform"
	"cardflow-backend/internal/crm/shared"
	"cardflow-backend/internal/crm/store"
	"github.com/alexedwards/argon2id"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Demo credentials (PRD §3, §13.5). Local/dev only — production refuses them.
const (
	DemoOwnerEmail      = "ajay@gmail.com"
	DemoOwnerPassword   = "Ajay1234"
	demoSuperAdminEmail = "superadmin@acme-demo.test"
	demoSuperAdminPass  = "Demo1234"
)

// CheckProductionSafety returns an error when the demo credential exists in production
// (PRD AUTH-01). The caller disables the CRM module instead of crashing CardFlow (D-06).
func CheckProductionSafety(ctx context.Context, st *store.Store, cfg shared.Config) error {
	if !cfg.IsProduction() {
		return nil
	}
	var hash string
	err := st.Pool.QueryRow(ctx, `
		SELECT pc.hash FROM crm.verified_identifiers vi
		JOIN crm.password_credentials pc ON pc.identity_id = vi.identity_id
		WHERE vi.kind = 'email' AND vi.namespace = 'global' AND vi.value_normalized = $1`, DemoOwnerEmail).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if ok, _ := argon2id.ComparePasswordAndHash(DemoOwnerPassword, hash); ok {
		return fmt.Errorf("demo owner credential %s is present in production", DemoOwnerEmail)
	}
	return nil
}

// Run seeds the base data everywhere, demo data in local/dev, and the production owner
// bootstrap when configured. Safe to run on every start-up.
func Run(ctx context.Context, st *store.Store, cfg shared.Config) error {
	platformID, err := ensureWorkspace(ctx, st, "platform", "Platform", true)
	if err != nil {
		return fmt.Errorf("seed platform workspace: %w", err)
	}
	if err := ensureSystemRoles(ctx, st, platformID); err != nil {
		return fmt.Errorf("seed system roles: %w", err)
	}

	if cfg.IsLocalOrDev() && cfg.SeedDemo {
		if err := seedDemo(ctx, st); err != nil {
			return fmt.Errorf("seed demo: %w", err)
		}
	}

	if cfg.OwnerEmail != "" && cfg.OwnerBootstrapPassword != "" {
		if err := bootstrapOwner(ctx, st, cfg); err != nil {
			return fmt.Errorf("bootstrap owner: %w", err)
		}
	}
	return nil
}

func ensureWorkspace(ctx context.Context, st *store.Store, code, name string, isPlatform bool) (uuid.UUID, error) {
	var id uuid.UUID
	err := st.Pool.QueryRow(ctx, `
		INSERT INTO crm.workspaces (code, name, is_platform, status)
		VALUES ($1, $2, $3, 'active')
		ON CONFLICT (code) DO UPDATE SET code = EXCLUDED.code
		RETURNING id`, code, name, isPlatform).Scan(&id)
	return id, err
}

func ensureSystemRoles(ctx context.Context, st *store.Store, workspaceID uuid.UUID) error {
	for _, r := range access.SystemRoles() {
		rules, err := json.Marshal(r.Rules)
		if err != nil {
			return err
		}
		if _, err := st.Pool.Exec(ctx, `
			INSERT INTO crm.roles (workspace_id, key, name, is_system, rank, base_rules)
			VALUES ($1, $2, $3, true, $4, $5)
			ON CONFLICT (workspace_id, key) DO NOTHING`, workspaceID, r.Key, r.Name, r.Rank, rules); err != nil {
			return err
		}
	}
	return nil
}

type identitySpec struct {
	email      string
	password   string
	name       string
	isOwner    bool
	mustChange bool
}

// ensureIdentity creates the identity with a verified email and password if the email
// is unknown; an existing identity is left untouched (never overwrite a password).
func ensureIdentity(ctx context.Context, st *store.Store, spec identitySpec) (uuid.UUID, bool, error) {
	var id uuid.UUID
	err := st.Pool.QueryRow(ctx, `
		SELECT identity_id FROM crm.verified_identifiers
		WHERE kind = 'email' AND namespace = 'global' AND value_normalized = $1`, spec.email).Scan(&id)
	if err == nil {
		return id, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, false, err
	}

	hash, err := identity.HashPassword(spec.password)
	if err != nil {
		return uuid.Nil, false, err
	}
	err = st.WithTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `
			INSERT INTO crm.identities (display_name, is_platform_owner) VALUES ($1, $2) RETURNING id`,
			spec.name, spec.isOwner).Scan(&id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.verified_identifiers (identity_id, kind, value_normalized, verified_at)
			VALUES ($1, 'email', $2, now())`, id, spec.email); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.password_credentials (identity_id, hash, must_change) VALUES ($1, $2, $3)`,
			id, hash, spec.mustChange); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{
			ActorKind: "system", Action: "identity.seeded", EntityType: "identity", EntityID: &id,
			After: map[string]any{"email": spec.email, "platformOwner": spec.isOwner},
		})
	})
	return id, err == nil, err
}

func seedDemo(ctx context.Context, st *store.Store) error {
	ownerID, created, err := ensureIdentity(ctx, st, identitySpec{
		email: DemoOwnerEmail, password: DemoOwnerPassword, name: "Ajay", isOwner: true,
	})
	if err != nil {
		return err
	}
	if created {
		slog.Info("CRM demo owner seeded (local/dev only)", "email", DemoOwnerEmail)
	}

	// Demo product, published at version 1.
	cfgJSON, _ := json.Marshal(platform.DefaultProductConfig())
	var productID uuid.UUID
	if err := st.Pool.QueryRow(ctx, `
		INSERT INTO crm.products (key, name, description, icon, status, current_version, draft_config, created_by)
		VALUES ('demo_product', 'Demo Product', 'Sample product used for local development.', 'boxes', 'active', 1, $1, $2)
		ON CONFLICT (key) DO UPDATE SET key = EXCLUDED.key
		RETURNING id`, cfgJSON, ownerID).Scan(&productID); err != nil {
		return err
	}
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO crm.product_versions (product_id, version, config, published_by)
		VALUES ($1, 1, $2, $3) ON CONFLICT DO NOTHING`, productID, cfgJSON, ownerID); err != nil {
		return err
	}

	// Demo customer workspace with the product and a Super Admin.
	wsID, err := ensureWorkspace(ctx, st, "acme-demo", "Acme Demo", false)
	if err != nil {
		return err
	}
	if err := ensureSystemRoles(ctx, st, wsID); err != nil {
		return err
	}
	if _, err := st.Pool.Exec(ctx, `
		INSERT INTO crm.workspace_products (workspace_id, product_id, config_version)
		VALUES ($1, $2, 1) ON CONFLICT DO NOTHING`, wsID, productID); err != nil {
		return err
	}

	adminID, _, err := ensureIdentity(ctx, st, identitySpec{
		email: demoSuperAdminEmail, password: demoSuperAdminPass, name: "Acme Super Admin",
	})
	if err != nil {
		return err
	}
	var membershipID uuid.UUID
	if err := st.Pool.QueryRow(ctx, `
		INSERT INTO crm.memberships (workspace_id, identity_id, status, created_by)
		VALUES ($1, $2, 'active', $3)
		ON CONFLICT (workspace_id, identity_id) DO UPDATE SET workspace_id = EXCLUDED.workspace_id
		RETURNING id`, wsID, adminID, ownerID).Scan(&membershipID); err != nil {
		return err
	}
	_, err = st.Pool.Exec(ctx, `
		INSERT INTO crm.role_assignments (workspace_id, membership_id, role_id, product_ids, granted_by)
		SELECT $1, $2, r.id, ARRAY[$3::uuid], $4 FROM crm.roles r
		WHERE r.workspace_id = $1 AND r.key = 'SUPER_ADMIN'
		  AND NOT EXISTS (SELECT 1 FROM crm.role_assignments WHERE membership_id = $2)`,
		wsID, membershipID, productID, ownerID)
	return err
}

func bootstrapOwner(ctx context.Context, st *store.Store, cfg shared.Config) error {
	if len(cfg.OwnerBootstrapPassword) < 12 {
		return errors.New("CRM_OWNER_BOOTSTRAP_PASSWORD must be at least 12 characters")
	}
	_, created, err := ensureIdentity(ctx, st, identitySpec{
		email: cfg.OwnerEmail, password: cfg.OwnerBootstrapPassword, name: "Platform Owner",
		isOwner: true, mustChange: true,
	})
	if created {
		slog.Warn("CRM platform owner bootstrapped — change the password and enrol MFA at first sign-in; then remove CRM_OWNER_BOOTSTRAP_PASSWORD", "email", cfg.OwnerEmail)
	}
	return err
}
