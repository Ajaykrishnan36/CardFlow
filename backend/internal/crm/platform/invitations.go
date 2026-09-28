package platform

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/mail"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const invitationTTL = 72 * time.Hour // PRD §6.2

func normalizeEmail(raw string) (string, bool) { return identity.NormalizeEmail(raw) }

type InviteInput struct {
	Name             string      `json:"name"`
	Email            string      `json:"email"`
	RoleKey          string      `json:"roleKey"`
	ProductIDs       []uuid.UUID `json:"productIds"`
	PermissionSetIDs []uuid.UUID `json:"permissionSetIds"`
}

// SentInvitation is an invitation created inside a transaction; the raw token is only
// held in memory until the email is sent (the DB stores its hash).
type SentInvitation struct {
	Invitation
	IdentityID    uuid.UUID
	MembershipID  uuid.UUID
	WorkspaceID   uuid.UUID
	WorkspaceName string
	token         string
}

// InviteTx implements "Save and Invite" (PRD §6.2): create/link the identity by verified
// email only, create or reuse an invited membership, and issue a 72 h single-use token.
func (h *Handler) InviteTx(ctx context.Context, tx pgx.Tx, actor, workspaceID uuid.UUID, in InviteInput, fieldPrefix string) (*SentInvitation, error) {
	name := strings.TrimSpace(in.Name)
	email, emailOK := normalizeEmail(in.Email)
	fields := map[string]string{}
	if name == "" || len(name) > 80 {
		fields[fieldPrefix+"name"] = "Enter a name (up to 80 characters)."
	}
	if !emailOK {
		fields[fieldPrefix+"email"] = "Enter a valid email address."
	}
	if strings.TrimSpace(in.RoleKey) == "" {
		fields[fieldPrefix+"roleKey"] = "Pick a role."
	}
	if len(fields) > 0 {
		return nil, shared.Validation(fields)
	}

	var wsName string
	var isPlatform bool
	if err := tx.QueryRow(ctx, `SELECT name, is_platform FROM crm.workspaces WHERE id = $1`, workspaceID).Scan(&wsName, &isPlatform); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.NotFound("workspace_not_found")
		}
		return nil, err
	}
	var roleID uuid.UUID
	var roleName string
	if err := tx.QueryRow(ctx, `SELECT id, name FROM crm.roles WHERE workspace_id = $1 AND key = $2`, workspaceID, in.RoleKey).Scan(&roleID, &roleName); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, shared.Validation(map[string]string{fieldPrefix + "roleKey": "Pick a role from this workspace."})
		}
		return nil, err
	}

	// Products: default to every active product of the workspace; otherwise they must be assigned.
	var productIDs []uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT COALESCE(array_agg(product_id), '{}') FROM crm.workspace_products
		WHERE workspace_id = $1 AND status = 'active' AND (COALESCE(cardinality($2::uuid[]), 0) = 0 OR product_id = ANY($2::uuid[]))`,
		workspaceID, in.ProductIDs).Scan(&productIDs); err != nil {
		return nil, err
	}
	if len(in.ProductIDs) > 0 && len(productIDs) != len(uniqueUUIDs(in.ProductIDs)) {
		return nil, shared.Validation(map[string]string{fieldPrefix + "productIds": "Only products assigned to this workspace can be granted."})
	}

	setIDs, err := validatePermissionSets(ctx, tx, workspaceID, in.PermissionSetIDs, fieldPrefix)
	if err != nil {
		return nil, err
	}

	// Identity: link by email identifier; otherwise create one whose email is verified on acceptance.
	var identityID uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT identity_id FROM crm.verified_identifiers WHERE kind = 'email' AND namespace = 'global' AND value_normalized = $1`,
		email).Scan(&identityID)
	if errors.Is(err, pgx.ErrNoRows) {
		if err := tx.QueryRow(ctx, `INSERT INTO crm.identities (display_name) VALUES ($1) RETURNING id`, name).Scan(&identityID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.verified_identifiers (identity_id, kind, value_normalized) VALUES ($1, 'email', $2)`, identityID, email); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}

	var membershipID uuid.UUID
	var mStatus string
	err = tx.QueryRow(ctx, `SELECT id, status FROM crm.memberships WHERE workspace_id = $1 AND identity_id = $2 FOR UPDATE`,
		workspaceID, identityID).Scan(&membershipID, &mStatus)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		if err := tx.QueryRow(ctx, `
			INSERT INTO crm.memberships (workspace_id, identity_id, status, created_by) VALUES ($1, $2, 'invited', $3) RETURNING id`,
			workspaceID, identityID, actor).Scan(&membershipID); err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	case mStatus == "active":
		return nil, shared.Validation(map[string]string{fieldPrefix + "email": "This person is already a member of the workspace."})
	case mStatus == "suspended":
		return nil, shared.Validation(map[string]string{fieldPrefix + "email": "This member is suspended. Reactivate them from their user page instead."})
	default:
		if _, err := tx.Exec(ctx, `UPDATE crm.memberships SET status = 'invited' WHERE id = $1`, membershipID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `
		UPDATE crm.invitations SET status = 'revoked' WHERE membership_id = $1 AND status IN ('pending', 'delivered', 'delivery_failed')`,
		membershipID); err != nil {
		return nil, err
	}

	token, err := shared.RandomToken(32)
	if err != nil {
		return nil, err
	}
	sent := &SentInvitation{
		Invitation: Invitation{
			Email: email, DisplayName: name, RoleKey: in.RoleKey, RoleName: roleName, Status: "pending",
			ExpiresAt: time.Now().Add(invitationTTL),
		},
		IdentityID: identityID, MembershipID: membershipID, WorkspaceID: workspaceID, WorkspaceName: wsName, token: token,
	}
	if err := tx.QueryRow(ctx, `
		INSERT INTO crm.invitations (workspace_id, membership_id, token_hash, intended_role_id, product_ids, permission_set_ids,
		                             delivery_channel, status, expires_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, 'email', 'pending', $7, $8) RETURNING id, created_at`,
		workspaceID, membershipID, shared.HashToken(token), roleID, productIDs, setIDs, sent.ExpiresAt, actor).Scan(&sent.ID, &sent.CreatedAt); err != nil {
		return nil, err
	}
	return sent, shared.WriteAudit(ctx, tx, shared.AuditEvent{
		WorkspaceID: &workspaceID, ActorID: &actor, Action: "invitation.created", EntityType: "invitation", EntityID: &sent.ID,
		After: map[string]any{"email": email, "role": in.RoleKey, "identityId": identityID},
	})
}

func uniqueUUIDs(ids []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := ids[:0:0]
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (h *Handler) acceptURL(token string) string {
	return h.cfg.BaseURL + "/crm/accept-invite?token=" + token
}

// DeliverInvitation emails the invitation after the transaction commits and records
// the outcome. In local/dev the link is also returned so it can be shown in the UI.
func (h *Handler) DeliverInvitation(ctx context.Context, sent *SentInvitation) *Invitation {
	link := h.acceptURL(sent.token)
	sendCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	err := h.mailer.Send(sendCtx, mail.Message{
		To:      sent.Email,
		Subject: fmt.Sprintf("You're invited to %s on %s", sent.WorkspaceName, h.cfg.AppName),
		Heading: "You're invited to " + sent.WorkspaceName,
		Lines: []string{
			"Hi " + sent.DisplayName + ", you've been invited to join " + sent.WorkspaceName + " on " + h.cfg.AppName + " as " + sent.RoleName + ".",
			"Accept the invitation to set your password and sign in.",
		},
		Button: &mail.Button{Label: "Accept invitation", URL: link},
		Footer: "This link works once and expires in 72 hours. If you weren't expecting it, you can ignore this email.",
	})
	status := "delivered"
	if err != nil {
		status = "delivery_failed"
		slog.Warn("crm: invitation email not sent", "invitation", sent.ID, "error", err)
	}
	if _, err := h.store.Pool.Exec(context.WithoutCancel(ctx), `UPDATE crm.invitations SET status = $2 WHERE id = $1 AND status = 'pending'`, sent.ID, status); err == nil {
		sent.Status = status
	}
	inv := sent.Invitation
	if h.cfg.IsLocalOrDev() {
		inv.DevAcceptURL = link
	}
	return &inv
}

func (h *Handler) listInvitations(ctx context.Context, where string, arg any) ([]Invitation, error) {
	rows, err := h.store.Pool.Query(ctx, `
		SELECT inv.id, COALESCE(e.value_normalized, ''), i.display_name, r.key, r.name,
		       CASE WHEN inv.status IN ('pending', 'delivered') AND inv.expires_at < now() THEN 'expired' ELSE inv.status END,
		       inv.expires_at, inv.created_at, inv.accepted_at
		FROM crm.invitations inv
		JOIN crm.memberships m ON m.id = inv.membership_id
		JOIN crm.identities i ON i.id = m.identity_id
		JOIN crm.roles r ON r.id = inv.intended_role_id
		LEFT JOIN crm.verified_identifiers e ON e.identity_id = i.id AND e.kind = 'email' AND e.namespace = 'global'
		WHERE `+where+`
		ORDER BY inv.created_at DESC LIMIT 50`, arg)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invitation{}
	for rows.Next() {
		var inv Invitation
		if err := rows.Scan(&inv.ID, &inv.Email, &inv.DisplayName, &inv.RoleKey, &inv.RoleName, &inv.Status,
			&inv.ExpiresAt, &inv.CreatedAt, &inv.AcceptedAt); err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

func (h *Handler) handleInvite(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id", "workspace_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in InviteInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var sent *SentInvitation
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var isPlatform bool
		if err := tx.QueryRow(r.Context(), `SELECT is_platform FROM crm.workspaces WHERE id = $1`, id).Scan(&isPlatform); err != nil || isPlatform {
			return shared.NotFound("workspace_not_found")
		}
		var err error
		sent, err = h.InviteTx(r.Context(), tx, actorID(r), id, in, "")
		return err
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusCreated, h.DeliverInvitation(r.Context(), sent))
}

func (h *Handler) handleResendInvite(w http.ResponseWriter, r *http.Request) {
	wsID, err := idParam(r, "id", "workspace_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	invID, err := idParam(r, "invitationId", "invitation_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	token, err := shared.RandomToken(32)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	sent := &SentInvitation{token: token, WorkspaceID: wsID}
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var status string
		err := tx.QueryRow(r.Context(), `
			SELECT inv.status, COALESCE(e.value_normalized, ''), i.display_name, r.key, r.name, w.name, inv.created_at, i.id
			FROM crm.invitations inv
			JOIN crm.memberships m ON m.id = inv.membership_id
			JOIN crm.identities i ON i.id = m.identity_id
			JOIN crm.roles r ON r.id = inv.intended_role_id
			JOIN crm.workspaces w ON w.id = inv.workspace_id
			LEFT JOIN crm.verified_identifiers e ON e.identity_id = i.id AND e.kind = 'email' AND e.namespace = 'global'
			WHERE inv.id = $1 AND inv.workspace_id = $2 FOR UPDATE OF inv`, invID, wsID).
			Scan(&status, &sent.Email, &sent.DisplayName, &sent.RoleKey, &sent.RoleName, &sent.WorkspaceName, &sent.CreatedAt, &sent.IdentityID)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("invitation_not_found")
		}
		if err != nil {
			return err
		}
		if status == "accepted" || status == "revoked" {
			return shared.NewError(http.StatusConflict, "invitation_closed", "This invitation can no longer be resent.")
		}
		sent.ID = invID
		sent.Status = "pending"
		sent.ExpiresAt = time.Now().Add(invitationTTL)
		if _, err := tx.Exec(r.Context(), `UPDATE crm.invitations SET token_hash = $2, status = 'pending', expires_at = $3 WHERE id = $1`,
			invID, shared.HashToken(token), sent.ExpiresAt); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, auditEvent(r, "invitation.resent", "invitation", &invID, &wsID, nil, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, h.DeliverInvitation(r.Context(), sent))
}

func (h *Handler) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	wsID, err := idParam(r, "id", "workspace_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	invID, err := idParam(r, "invitationId", "invitation_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var membershipID uuid.UUID
		var status string
		err := tx.QueryRow(r.Context(), `SELECT membership_id, status FROM crm.invitations WHERE id = $1 AND workspace_id = $2 FOR UPDATE`, invID, wsID).
			Scan(&membershipID, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("invitation_not_found")
		}
		if err != nil {
			return err
		}
		if status == "accepted" {
			return shared.NewError(http.StatusConflict, "invitation_accepted", "This invitation was already accepted. Suspend the member instead.")
		}
		if _, err := tx.Exec(r.Context(), `UPDATE crm.invitations SET status = 'revoked' WHERE id = $1`, invID); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE crm.memberships SET status = 'revoked' WHERE id = $1 AND status = 'invited'`, membershipID); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, auditEvent(r, "invitation.revoked", "invitation", &invID, &wsID, nil, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
