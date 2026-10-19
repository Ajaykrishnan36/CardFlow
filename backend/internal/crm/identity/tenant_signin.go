package identity

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"cardflow-backend/internal/crm/shared"
	"github.com/jackc/pgx/v5"
)

// Signing in on a tenant's own address, /<slug>/login (D-156), is only for people who
// belong to one of the tenant's businesses (any role: Super Admin, Admin, Staff…).
// Anyone else is told the account isn't found — before a code is sent, so nobody can
// create an account there or receive a code for a tenant they don't belong to.

// tenantAllows checks the email or phone the person typed against the tenant's members.
func (s *Service) tenantAllows(ctx context.Context, slug, identifier string) error {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return nil
	}
	kind, value := "", ""
	if e, ok := NormalizeEmail(identifier); ok {
		kind, value = "email", e
	} else if p, ok := normalizePhone(strings.TrimSpace(identifier)); ok {
		kind, value = "phone", p
	} else {
		return nil // the usual "enter a valid …" message follows
	}
	var name string
	var member bool
	err := s.store.Pool.QueryRow(ctx, `
		WITH t AS (SELECT id, name FROM crm.tenants WHERE status = 'active' AND (slug = $1 OR id = (SELECT tenant_id FROM crm.workspaces WHERE code = $1)) LIMIT 1)
		SELECT t.name, EXISTS (
			SELECT 1 FROM crm.verified_identifiers v
			JOIN crm.memberships m ON m.identity_id = v.identity_id AND m.status = 'active'
			JOIN crm.workspaces w ON w.id = m.workspace_id AND w.status = 'active' AND w.tenant_id = t.id
			JOIN crm.identities i ON i.id = v.identity_id AND i.status = 'active'
			WHERE v.kind = $2 AND v.value_normalized = $3)
		FROM t`, slug, kind, value).Scan(&name, &member)
	if errors.Is(err, pgx.ErrNoRows) {
		return shared.NotFound("business_not_found")
	}
	if err != nil {
		return err
	}
	if !member {
		return shared.NewError(http.StatusNotFound, "account_not_found", "Account not found. Only people "+name+" has added can sign in here.")
	}
	return nil
}
