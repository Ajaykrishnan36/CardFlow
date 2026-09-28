package access

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// System role ranks (PRD §13.2 roles.rank).
const (
	RankSuperAdmin = 100
	RankAdmin      = 80
	RankStaff      = 50
	RankEndUser    = 10
)

type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Membership struct {
	ID                  uuid.UUID `json:"id"`
	WorkspaceID         uuid.UUID `json:"workspaceId"`
	WorkspaceCode       string    `json:"workspaceCode"`
	WorkspaceName       string    `json:"workspaceName"`
	IsPlatformWorkspace bool      `json:"isPlatformWorkspace"`
	Status              string    `json:"status"`
	AuthVersion         int       `json:"authVersion"`
	RoleKey             string    `json:"roleKey,omitempty"`
	RoleName            string    `json:"roleName,omitempty"`
	RoleRank            int       `json:"roleRank"`
}

// ListActiveMemberships returns the identity's active memberships in active workspaces,
// each with its highest-ranked unexpired role.
func ListActiveMemberships(ctx context.Context, q Querier, identityID uuid.UUID) ([]Membership, error) {
	rows, err := q.Query(ctx, `
		SELECT m.id, w.id, w.code, w.name, w.is_platform, m.status, m.auth_version,
		       COALESCE(r.key, ''), COALESCE(r.name, ''), COALESCE(r.rank, 0)
		FROM crm.memberships m
		JOIN crm.workspaces w ON w.id = m.workspace_id
		LEFT JOIN LATERAL (
			SELECT ro.key, ro.name, ro.rank
			FROM crm.role_assignments ra
			JOIN crm.roles ro ON ro.id = ra.role_id
			WHERE ra.membership_id = m.id AND (ra.expires_at IS NULL OR ra.expires_at > now())
			ORDER BY ro.rank DESC
			LIMIT 1
		) r ON true
		WHERE m.identity_id = $1 AND m.status = 'active' AND w.status = 'active'
		ORDER BY w.is_platform DESC, w.name`, identityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Membership
	for rows.Next() {
		var m Membership
		if err := rows.Scan(&m.ID, &m.WorkspaceID, &m.WorkspaceCode, &m.WorkspaceName, &m.IsPlatformWorkspace,
			&m.Status, &m.AuthVersion, &m.RoleKey, &m.RoleName, &m.RoleRank); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// IsPrivileged reports whether MFA rules for privileged roles apply (PRD §4: Owner,
// Super Admin, Admin).
func IsPrivileged(isPlatformOwner bool, memberships []Membership) bool {
	if isPlatformOwner {
		return true
	}
	for _, m := range memberships {
		if m.RoleRank >= RankAdmin {
			return true
		}
	}
	return false
}
