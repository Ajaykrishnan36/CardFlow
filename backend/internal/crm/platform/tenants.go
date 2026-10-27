package platform

import (
	"context"
	"errors"
	"strings"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Tenants (D-156). A tenant is the brand: one permanent address, /<slug>/..., for all of its
// businesses (PSG → PSG School, PSG College, PSG Medical College). Each business keeps its
// own records, team and roles; the tenant only groups them, names the address and decides
// who may sign in there.

// ReservedSlugs can't be a tenant address: they are the site's own pages.
var ReservedSlugs = map[string]bool{"crm": true, "api": true, "share": true, "owner": true, "login": true, "signup": true, "admin": true,
	"static": true, "assets": true, "health": true, "healthz": true, "public": true, "platform": true, "w": true, "me": true,
	"businesses": true, "home": true, "help": true, "support": true, "www": true, "app": true, "join": true, "index.html": true}

// Tenant is a brand and its permanent address.
type Tenant struct {
	ID           uuid.UUID `json:"id"`
	Slug         string    `json:"slug"`
	Name         string    `json:"name"`
	CustomDomain string    `json:"customDomain"`
}

// slugTaken: a tenant or a business already uses the address, or the site needs it.
func slugTaken(ctx context.Context, q querier, slug string) (bool, error) {
	if ReservedSlugs[slug] {
		return true, nil
	}
	var taken bool
	err := q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.tenants WHERE slug = $1) OR EXISTS (SELECT 1 FROM crm.workspaces WHERE code = $1)`, slug).Scan(&taken)
	return taken, err
}

// createTenantTx makes the tenant of a new business, addressed by the business's code.
func createTenantTx(ctx context.Context, tx pgx.Tx, wsID uuid.UUID, slug, name string, owner *uuid.UUID) (uuid.UUID, error) {
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO crm.tenants (slug, name, owner_identity) VALUES ($1, $2, $3) RETURNING id`, slug, name, owner).Scan(&id); err != nil {
		if isUniqueViolation(err, "") {
			return uuid.Nil, shared.Validation(map[string]string{"name": "This name's address is already taken. Try a slightly different name."})
		}
		return uuid.Nil, err
	}
	_, err := tx.Exec(ctx, `UPDATE crm.workspaces SET tenant_id = $2 WHERE id = $1`, wsID, id)
	return id, err
}

// TenantBySlug finds an active business address. The code may be typed grouped or in any
// case; an old address (a brand slug or business code from before D-158) finds the
// business it now points to, so its Slug can differ from what was asked for.
func TenantBySlug(ctx context.Context, q querier, slug string) (*Tenant, error) {
	slug = strings.ToLower(strings.TrimSpace(slug))
	code := NormalizeCode(slug)
	t := &Tenant{}
	err := q.QueryRow(ctx, `
		WITH f AS (SELECT t.id, t.status, t.redirect_to, 0 AS pri FROM crm.tenants t WHERE t.slug IN ($1, $2)
		           UNION ALL SELECT t.id, t.status, t.redirect_to, 1 FROM crm.workspaces w JOIN crm.tenants t ON t.id = w.tenant_id WHERE w.code IN ($1, $2))
		SELECT t.id, t.slug, t.name, COALESCE(t.custom_domain, '') FROM f
		JOIN crm.tenants t ON t.id = CASE WHEN f.status = 'redirected' THEN f.redirect_to ELSE f.id END
		WHERE t.status = 'active' ORDER BY f.pri LIMIT 1`, slug, code).Scan(&t.ID, &t.Slug, &t.Name, &t.CustomDomain)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, identity.BusinessNotFound()
	}
	return t, err
}
