package identity

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"cardflow-backend/internal/crm/shared"
	"github.com/jackc/pgx/v5"
)

// Signing in on a business's own address, /<code>/login (D-156, one business per address
// since D-158), is only for people who belong to that business (any role).
// Anyone else is told the account isn't found — before a code is sent, so nobody can
// create an account there or receive a code for a tenant they don't belong to.

// BusinessNotFound is what an unknown or closed business address shows (D-158).
func BusinessNotFound() error {
	return shared.NewError(http.StatusNotFound, "business_not_found", "This business URL is invalid or unavailable. Please check the URL or contact support.")
}

// tenantAllows checks the email or phone the person typed against the tenant's members.
func (s *Service) tenantAllows(ctx context.Context, slug, identifier string) error {
	slug = strings.ToLower(strings.TrimSpace(slug))
	if slug == "" {
		return nil
	}
	// Codes are shown grouped (271026-44-234F-000501) and may be typed in any case.
	code := strings.NewReplacer("-", "", " ", "").Replace(slug)
	kind, value := "", ""
	if e, ok := NormalizeEmail(identifier); ok {
		kind, value = "email", e
	} else if p, ok := normalizePhone(strings.TrimSpace(identifier)); ok {
		kind, value = "phone", p
	} else {
		return nil // the usual "enter a valid …" message follows
	}
	var member bool
	err := s.store.Pool.QueryRow(ctx, `
		WITH f AS (SELECT t.id, t.status, t.redirect_to FROM crm.tenants t WHERE t.slug IN ($1, $4)
		           UNION ALL SELECT t.id, t.status, t.redirect_to FROM crm.workspaces w JOIN crm.tenants t ON t.id = w.tenant_id WHERE w.code IN ($1, $4)),
		     t AS (SELECT t.id, t.name FROM f JOIN crm.tenants t ON t.id = CASE WHEN f.status = 'redirected' THEN f.redirect_to ELSE f.id END
		           WHERE t.status = 'active' LIMIT 1)
		SELECT EXISTS (
			SELECT 1 FROM crm.verified_identifiers v
			JOIN crm.memberships m ON m.identity_id = v.identity_id AND m.status = 'active'
			JOIN crm.workspaces w ON w.id = m.workspace_id AND w.status = 'active' AND w.tenant_id = t.id
			JOIN crm.identities i ON i.id = v.identity_id AND i.status = 'active'
			WHERE v.kind = $2 AND v.value_normalized = $3)
		FROM t`, slug, kind, value, code).Scan(&member)
	if errors.Is(err, pgx.ErrNoRows) {
		return BusinessNotFound()
	}
	if err != nil {
		return err
	}
	if !member {
		return shared.NewError(http.StatusNotFound, "account_not_found", "You don't have access to this business. Please contact your Super Admin.")
	}
	return nil
}
