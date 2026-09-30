// Package platform holds the Platform Owner's registry: products, customer workspaces,
// invitations, users and the audit log (PRD §14.1 "Owner" endpoints, OWN-01/OWN-02).
package platform

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/mail"
	"cardflow-backend/internal/crm/shared"
	"cardflow-backend/internal/crm/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type Handler struct {
	store    *store.Store
	cfg      shared.Config
	mailer   mail.Mailer
	identity *identity.Service
}

func NewHandler(st *store.Store, cfg shared.Config, mailer mail.Mailer, idSvc *identity.Service) *Handler {
	return &Handler{store: st, cfg: cfg, mailer: mailer, identity: idSvc}
}

// Routes mounts owner-only platform endpoints; every customer role gets 403 (OWN-01).
func (h *Handler) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(identity.RequireOwner)
		r.Get("/platform/dashboard", h.handleDashboard)

		r.Get("/platform/products", h.handleListProducts)
		r.Post("/platform/products", h.handleCreateProduct)
		r.Get("/platform/products/{id}", h.handleGetProduct)
		r.Patch("/platform/products/{id}", h.handleUpdateProduct)
		r.Post("/platform/products/{id}/publish", h.handlePublishProduct)
		r.Post("/platform/products/{id}/archive", h.handleArchiveProduct)
		r.Post("/platform/products/{id}/restore", h.handleRestoreProduct)

		r.Get("/platform/workspaces", h.handleListWorkspaces)
		r.Post("/platform/workspaces", h.handleProvisionWorkspace)
		r.Get("/platform/workspaces/{id}", h.handleGetWorkspace)
		r.Patch("/platform/workspaces/{id}", h.handleUpdateWorkspace)
		r.Post("/platform/workspaces/{id}/products", h.handleAssignProduct)
		r.Patch("/platform/workspaces/{id}/products/{productId}", h.handleUpdateWorkspaceProduct)
		r.Post("/platform/workspaces/{id}/invitations", h.handleInvite)
		r.Post("/platform/workspaces/{id}/invitations/{invitationId}/resend", h.handleResendInvite)
		r.Post("/platform/workspaces/{id}/invitations/{invitationId}/revoke", h.handleRevokeInvite)

		r.Get("/platform/users", h.handleListUsers)
		r.Get("/platform/users/{id}", h.handleGetUser)
		r.Patch("/platform/users/{id}", h.handleUpdateUser)
		r.Post("/platform/users/{id}/password-reset", h.handleUserPasswordReset)
		r.Post("/platform/users/{id}/revoke-sessions", h.handleUserRevokeSessions)
		r.Post("/platform/users/{id}/reset-mfa", h.handleUserResetMFA)
		r.Patch("/platform/users/{id}/memberships/{membershipId}", h.handleUpdateMembership)
		r.Post("/platform/users", h.handleCreateUser)
		r.Post("/platform/users/{id}/memberships", h.handleAddMembership)

		r.Get("/platform/access/catalog", h.handleAccessCatalog)
		r.Get("/platform/access/workspaces", h.handleAccessWorkspaces)
		r.Get("/platform/workspaces/{id}/permission-sets", h.handleListPermissionSets)
		r.Post("/platform/workspaces/{id}/permission-sets", h.handleCreatePermissionSet)
		r.Patch("/platform/permission-sets/{id}", h.handleUpdatePermissionSet)
		r.Delete("/platform/permission-sets/{id}", h.handleDeletePermissionSet)
		r.Get("/platform/workspaces/{id}/roles", h.handleListRoles)
		r.Post("/platform/workspaces/{id}/roles", h.handleCreateRole)
		r.Patch("/platform/roles/{id}", h.handleUpdateRole)
		r.Delete("/platform/roles/{id}", h.handleDeleteRole)
		r.Post("/platform/roles/{id}/reset", h.handleResetRole)

		r.Get("/platform/audit", h.handleAudit)
		r.Get("/platform/email", h.handleEmailStatus)
		r.Post("/platform/email/test", h.handleEmailTest)
	})
}

// ---- helpers shared by the platform handlers ----

func idParam(r *http.Request, name, notFoundCode string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, shared.NotFound(notFoundCode)
	}
	return id, nil
}

func actorID(r *http.Request) uuid.UUID {
	return identity.SessionFrom(r.Context()).IdentityID
}

func auditEvent(r *http.Request, action, entityType string, entityID *uuid.UUID, workspaceID *uuid.UUID, before, after any) shared.AuditEvent {
	actor := actorID(r)
	meta := identity.Meta(r)
	return shared.AuditEvent{
		WorkspaceID: workspaceID, ActorID: &actor, Action: action, EntityType: entityType, EntityID: entityID,
		Before: before, After: after, IP: meta.IP, RequestID: meta.RequestID,
	}
}

func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return constraint == "" || strings.Contains(pgErr.ConstraintName, constraint)
	}
	return false
}

func queryInt(r *http.Request, name string, def, min, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

func likePattern(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

func writeResult(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, status, v)
}

// PlatformWorkspaceID returns the owner's own workspace (where platform leads live).
func PlatformWorkspaceID(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}) (uuid.UUID, error) {
	var id uuid.UUID
	err := q.QueryRow(ctx, `SELECT id FROM crm.workspaces WHERE is_platform`).Scan(&id)
	return id, err
}

func trimPtr(s *string) *string {
	if s == nil {
		return nil
	}
	v := strings.TrimSpace(*s)
	return &v
}

func nullIfEmpty(s string) *string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return &s
}

// Store exposes the database to the module's own routes (SSO sign-in).
func (h *Handler) Store() *store.Store { return h.store }
