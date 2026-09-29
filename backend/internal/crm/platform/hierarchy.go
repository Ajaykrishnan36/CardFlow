package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"cardflow-backend/internal/crm/access"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Role hierarchy (D-48). Roles are positions in a tree: Super Admin at the top (full
// access to the workspace), then whatever the workspace needs (Admin → Staff → End user
// by default, plus custom roles anywhere below Super Admin). A role grants no
// permissions by itself — permission sets do — it widens record sharing: "own records"
// means your records plus those owned by people in roles below yours.

// DefaultPermissionSets are created in every workspace from the built-in role templates,
// so an Admin or Staff member keeps the same access they had when roles carried it.
var defaultSets = []struct{ key, name, desc string }{
	{"ADMIN", "Admin access", "Every record in the workspace; no exports or customizing. Given to Admins by default."},
	{"STAFF", "Staff access", "View, create and edit their own records (and their team's, through the role hierarchy)."},
}

// DefaultPermissionSetFor returns the default permission set for a built-in role key, if any.
func DefaultPermissionSetFor(ctx context.Context, tx pgx.Tx, ws uuid.UUID, roleKey string) (uuid.UUID, bool) {
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM crm.permission_sets WHERE workspace_id = $1 AND system_key = $2`, ws, roleKey).Scan(&id); err != nil {
		return uuid.Nil, false
	}
	return id, true
}

// EnsureRoleTree places every role in the tree and creates the default permission sets
// (idempotent; runs for every workspace at start-up and when one is provisioned).
func EnsureRoleTree(ctx context.Context, tx pgx.Tx, ws uuid.UUID) error {
	steps := []string{
		`UPDATE crm.roles c SET parent_role_id = p.id FROM crm.roles p
		 WHERE c.workspace_id = $1 AND p.workspace_id = $1 AND c.parent_role_id IS NULL
		   AND ((c.key = 'ADMIN' AND p.key = 'SUPER_ADMIN') OR (c.key = 'STAFF' AND p.key = 'ADMIN') OR (c.key = 'END_USER' AND p.key = 'STAFF'))`,
		// Custom roles without a place: privileged ones under Super Admin, the rest under Admin.
		`UPDATE crm.roles c SET parent_role_id = p.id FROM crm.roles p
		 WHERE c.workspace_id = $1 AND p.workspace_id = $1 AND c.parent_role_id IS NULL AND NOT c.is_system
		   AND p.key = CASE WHEN c.rank >= 80 THEN 'SUPER_ADMIN' ELSE 'ADMIN' END`,
		`UPDATE crm.roles SET parent_role_id = NULL WHERE workspace_id = $1 AND key = 'SUPER_ADMIN'`,
	}
	for _, sql := range steps {
		if _, err := tx.Exec(ctx, sql, ws); err != nil {
			return err
		}
	}
	for _, s := range defaultSets {
		if _, ok := DefaultPermissionSetFor(ctx, tx, ws, s.key); ok {
			continue
		}
		sr, _ := access.FindSystemRole(s.key)
		raw, _ := json.Marshal(sr.Rules)
		name := s.name
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.permission_sets WHERE workspace_id = $1 AND lower(name) = lower($2))`, ws, name).Scan(&taken); err != nil {
			return err
		}
		if taken {
			name += " (default)"
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.permission_sets (workspace_id, name, description, rules, system_key) VALUES ($1, $2, $3, $4, $5)`,
			ws, name, s.desc, raw, s.key); err != nil {
			return err
		}
	}
	return nil
}

const roleMigrationMarker = "roles-to-permission-sets-v1"

// MigrateRoles (start-up) places roles in the tree everywhere and, once, moves the
// permissions roles used to carry into permission sets assigned to the same people and
// pending invitations — so nobody's access changes when roles stop granting it.
func MigrateRoles(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `SELECT id FROM crm.workspaces`)
	if err != nil {
		return err
	}
	var wss []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		wss = append(wss, id)
	}
	rows.Close()
	var done bool
	_ = pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.connector_state WHERE key = $1)`, roleMigrationMarker).Scan(&done)
	for _, ws := range wss {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return err
		}
		err = EnsureRoleTree(ctx, tx, ws)
		if err == nil && !done {
			err = moveRolePermissions(ctx, tx, ws)
		}
		if err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("workspace %s: %w", ws, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
	}
	if !done {
		if _, err := pool.Exec(ctx, `INSERT INTO crm.connector_state (key, value) VALUES ($1, '{"done":true}') ON CONFLICT (key) DO NOTHING`, roleMigrationMarker); err != nil {
			return err
		}
		slog.Info("CRM roles now form a hierarchy; their permissions moved to permission sets", "workspaces", len(wss))
	}
	return nil
}

func moveRolePermissions(ctx context.Context, tx pgx.Tx, ws uuid.UUID) error {
	rows, err := tx.Query(ctx, `SELECT id, key, name, is_system, customized, base_rules FROM crm.roles WHERE workspace_id = $1 AND key <> 'SUPER_ADMIN'`, ws)
	if err != nil {
		return err
	}
	type role struct {
		id             uuid.UUID
		key, name      string
		system, custom bool
		raw            []byte
	}
	var list []role
	for rows.Next() {
		var r role
		if err := rows.Scan(&r.id, &r.key, &r.name, &r.system, &r.custom, &r.raw); err != nil {
			rows.Close()
			return err
		}
		list = append(list, r)
	}
	rows.Close()
	for _, r := range list {
		var used bool
		if err := tx.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM crm.role_assignments WHERE role_id = $1)
			    OR EXISTS (SELECT 1 FROM crm.invitations WHERE intended_role_id = $1 AND status IN ('pending', 'delivered'))`, r.id).Scan(&used); err != nil {
			return err
		}
		if !used {
			continue
		}
		var rules access.Rules
		if sr, ok := access.FindSystemRole(r.key); ok && r.system && !r.custom {
			rules = sr.Rules
		} else {
			_ = json.Unmarshal(r.raw, &rules)
		}
		rules = access.Normalize(rules)
		if len(rules.Objects) == 0 && len(rules.Capabilities) == 0 {
			continue
		}
		var setID uuid.UUID
		if id, ok := DefaultPermissionSetFor(ctx, tx, ws, r.key); ok && r.system && !r.custom {
			setID = id
		} else {
			key := "role:" + r.id.String()
			err := tx.QueryRow(ctx, `SELECT id FROM crm.permission_sets WHERE workspace_id = $1 AND system_key = $2`, ws, key).Scan(&setID)
			if errors.Is(err, pgx.ErrNoRows) {
				raw, _ := json.Marshal(rules)
				name := r.name + " access"
				var taken bool
				if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.permission_sets WHERE workspace_id = $1 AND lower(name) = lower($2))`, ws, name).Scan(&taken); err != nil {
					return err
				}
				if taken {
					name = r.name + " role access"
				}
				if err := tx.QueryRow(ctx, `
					INSERT INTO crm.permission_sets (workspace_id, name, description, rules, system_key)
					VALUES ($1, $2, $3, $4, $5) RETURNING id`, ws, name, "What the "+r.name+" role used to grant.", raw, key).Scan(&setID); err != nil {
					return err
				}
			} else if err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.membership_permission_sets (workspace_id, membership_id, permission_set_id, granted_by)
			SELECT $1, ra.membership_id, $2, ra.granted_by FROM crm.role_assignments ra WHERE ra.role_id = $3
			ON CONFLICT DO NOTHING`, ws, setID, r.id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE crm.invitations SET permission_set_ids = array_append(COALESCE(permission_set_ids, '{}'), $2)
			WHERE intended_role_id = $1 AND status IN ('pending', 'delivered') AND NOT ($2 = ANY(COALESCE(permission_set_ids, '{}')))`, r.id, setID); err != nil {
			return err
		}
	}
	return nil
}
