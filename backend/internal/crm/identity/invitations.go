package identity

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Invitation acceptance (PRD §6.2, route /accept-invite): the token is consumed and the
// membership activated in one transaction. An existing identity signs in with its own
// password, which is never overwritten; a new identity sets its first password.

type InvitationPreview struct {
	WorkspaceName  string    `json:"workspaceName"`
	WorkspaceCode  string    `json:"workspaceCode"`
	RoleName       string    `json:"roleName"`
	DisplayName    string    `json:"displayName"`
	Email          string    `json:"email"`
	IdentityExists bool      `json:"identityExists"`
	ExpiresAt      time.Time `json:"expiresAt"`
	Status         string    `json:"status"`
}

type invitationRow struct {
	id            uuid.UUID
	workspaceID   uuid.UUID
	membershipID  uuid.UUID
	identityID    uuid.UUID
	roleID        uuid.UUID
	productIDs    []uuid.UUID
	status        string
	expiresAt     time.Time
	workspaceName string
	workspaceCode string
	roleName      string
	displayName   string
	email         string
	hasPassword   bool
}

const invitationSelect = `
	SELECT inv.id, inv.workspace_id, inv.membership_id, m.identity_id, inv.intended_role_id, inv.product_ids,
	       inv.status, inv.expires_at, w.name, w.code, r.name, i.display_name,
	       COALESCE((SELECT vi.value_normalized FROM crm.verified_identifiers vi
	                  WHERE vi.identity_id = i.id AND vi.kind = 'email' AND vi.namespace = 'global' LIMIT 1), ''),
	       EXISTS (SELECT 1 FROM crm.password_credentials pc WHERE pc.identity_id = i.id)
	FROM crm.invitations inv
	JOIN crm.memberships m ON m.id = inv.membership_id
	JOIN crm.identities i ON i.id = m.identity_id
	JOIN crm.workspaces w ON w.id = inv.workspace_id
	JOIN crm.roles r ON r.id = inv.intended_role_id
	WHERE inv.token_hash = $1`

func scanInvitation(row pgx.Row) (*invitationRow, error) {
	var inv invitationRow
	err := row.Scan(&inv.id, &inv.workspaceID, &inv.membershipID, &inv.identityID, &inv.roleID, &inv.productIDs,
		&inv.status, &inv.expiresAt, &inv.workspaceName, &inv.workspaceCode, &inv.roleName, &inv.displayName,
		&inv.email, &inv.hasPassword)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NotFound("invitation_not_found")
	}
	if err != nil {
		return nil, err
	}
	if (inv.status == "pending" || inv.status == "delivered") && inv.expiresAt.Before(time.Now()) {
		inv.status = "expired"
	}
	return &inv, nil
}

func (s *Service) PreviewInvitation(ctx context.Context, token string) (*InvitationPreview, error) {
	token = strings.TrimSpace(token)
	if token == "" || len(token) > 128 {
		return nil, shared.NotFound("invitation_not_found")
	}
	inv, err := scanInvitation(s.store.Pool.QueryRow(ctx, invitationSelect, shared.HashToken(token)))
	if err != nil {
		return nil, err
	}
	return &InvitationPreview{
		WorkspaceName: inv.workspaceName, WorkspaceCode: inv.workspaceCode, RoleName: inv.roleName,
		DisplayName: inv.displayName, Email: inv.email, IdentityExists: inv.hasPassword,
		ExpiresAt: inv.expiresAt, Status: inv.status,
	}, nil
}

type AcceptInput struct {
	Token       string `json:"token"`
	Password    string `json:"password"`
	DisplayName string `json:"displayName"`
}

func invitationClosed(status string) error {
	switch status {
	case "accepted":
		return shared.NewError(http.StatusConflict, "invitation_accepted", "This invitation was already accepted. Sign in instead.")
	case "expired":
		return shared.NewError(http.StatusGone, "invitation_expired", "This invitation has expired. Ask your administrator for a new one.")
	case "revoked":
		return shared.NewError(http.StatusGone, "invitation_revoked", "This invitation was withdrawn. Ask your administrator for a new one.")
	}
	return nil
}

func (s *Service) AcceptInvitation(ctx context.Context, in AcceptInput, meta RequestMeta) (*loginOutcome, error) {
	ipKey := "ip:" + meta.IP
	if blocked, wait := s.ipLimiter.blocked(ipKey); blocked {
		return nil, shared.TooManyAttempts(int(wait.Seconds()) + 1)
	}
	s.ipLimiter.hit(ipKey)

	token := strings.TrimSpace(in.Token)
	if token == "" || len(token) > 128 {
		return nil, shared.NotFound("invitation_not_found")
	}
	preview, err := scanInvitation(s.store.Pool.QueryRow(ctx, invitationSelect, shared.HashToken(token)))
	if err != nil {
		return nil, err
	}
	if err := invitationClosed(preview.status); err != nil {
		return nil, err
	}

	// Validate outside the transaction (argon2 is slow).
	var newHash string
	if preview.hasPassword {
		if in.Password == "" {
			return nil, shared.Validation(map[string]string{"password": "Enter your password."})
		}
		var hash string
		if err := s.store.Pool.QueryRow(ctx, `SELECT hash FROM crm.password_credentials WHERE identity_id = $1`, preview.identityID).Scan(&hash); err != nil {
			return nil, err
		}
		if !verifyPassword(in.Password, hash) {
			// 422 (not 401): the caller is anonymous, so this is a form error, not an expired session.
			return nil, shared.Validation(map[string]string{"password": "That password isn't right."})
		}
	} else {
		fields := map[string]string{}
		if msg := validateNewPassword(in.Password); msg != "" {
			fields["password"] = msg
		}
		if name := strings.TrimSpace(in.DisplayName); name != "" && len(name) > 80 {
			fields["displayName"] = "Use at most 80 characters."
		}
		if len(fields) > 0 {
			return nil, shared.Validation(fields)
		}
		if newHash, err = HashPassword(in.Password); err != nil {
			return nil, err
		}
	}

	var out loginOutcome
	var isOwner, mfaEnrolled, mustChange bool
	var mfaRequired bool
	err = s.store.WithTx(ctx, func(tx pgx.Tx) error {
		inv, err := scanInvitation(tx.QueryRow(ctx, invitationSelect+` FOR UPDATE OF inv`, shared.HashToken(token)))
		if err != nil {
			return err
		}
		if err := invitationClosed(inv.status); err != nil {
			return err
		}
		var identityStatus string
		if err := tx.QueryRow(ctx, `SELECT status, is_platform_owner FROM crm.identities WHERE id = $1`, inv.identityID).Scan(&identityStatus, &isOwner); err != nil {
			return err
		}
		if identityStatus != "active" {
			return shared.Forbidden("account_suspended", "This account is suspended. Contact your administrator.")
		}

		if newHash != "" {
			if _, err := tx.Exec(ctx, `
				INSERT INTO crm.password_credentials (identity_id, hash) VALUES ($1, $2)
				ON CONFLICT (identity_id) DO NOTHING`, inv.identityID, newHash); err != nil {
				return err
			}
			if name := strings.TrimSpace(in.DisplayName); name != "" {
				if _, err := tx.Exec(ctx, `UPDATE crm.identities SET display_name = $2, updated_at = now() WHERE id = $1`, inv.identityID, name); err != nil {
					return err
				}
			}
		}
		// Clicking the emailed link proves control of the address.
		if _, err := tx.Exec(ctx, `
			UPDATE crm.verified_identifiers SET verified_at = now()
			WHERE identity_id = $1 AND kind = 'email' AND verified_at IS NULL`, inv.identityID); err != nil {
			return err
		}
		if err := activateInvitation(ctx, tx, inv.id, inv.workspaceID, inv.membershipID, inv.roleID, inv.productIDs); err != nil {
			return err
		}

		memberships, err := access.ListActiveMemberships(ctx, tx, inv.identityID)
		if err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.mfa_methods WHERE identity_id = $1 AND confirmed_at IS NOT NULL)`,
			inv.identityID).Scan(&mfaEnrolled); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT must_change FROM crm.password_credentials WHERE identity_id = $1`, inv.identityID).Scan(&mustChange); err != nil {
			return err
		}
		privileged := access.IsPrivileged(isOwner, memberships)
		mfaRequired = mfaEnrolled || (privileged && s.cfg.MFAEnforced())
		if isOwner {
			mfaRequired = false // the owner console has no two-step verification (D-133)
		}
		audience := "workspace"
		if isOwner {
			audience = "owner"
		}
		tok, expires, err := createSession(ctx, tx, newSession{
			identityID: inv.identityID, audience: audience, privileged: privileged, mfaRequired: mfaRequired,
			ip: meta.IP, userAgent: meta.UserAgent,
		})
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE crm.identities SET last_login_at = now() WHERE id = $1`, inv.identityID); err != nil {
			return err
		}
		out = loginOutcome{token: tok, expires: expires}
		wsID := inv.workspaceID
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{
			WorkspaceID: &wsID, ActorID: &inv.identityID, Action: "invitation.accepted", EntityType: "invitation", EntityID: &inv.id,
			After: map[string]any{"membershipId": inv.membershipID, "newIdentity": newHash != ""},
			IP:    meta.IP, RequestID: meta.RequestID,
		})
	})
	if err != nil {
		return nil, err
	}
	enrollRequired := mfaRequired && !mfaEnrolled
	out.step = AuthStep{
		MFARequired:           mfaRequired,
		MFAEnrollmentRequired: enrollRequired,
		MustChangePassword:    mustChange,
		Next:                  nextPath(isOwner, mfaRequired, enrollRequired, mustChange),
	}
	return &out, nil
}

// activateInvitation turns an invited membership into an active one with the role, setups
// and permission sets the invitation carries, and marks the invitation accepted.
func activateInvitation(ctx context.Context, tx pgx.Tx, invID, workspaceID, membershipID, roleID uuid.UUID, productIDs []uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE crm.memberships SET status = 'active', auth_version = auth_version + 1 WHERE id = $1`, membershipID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM crm.role_assignments WHERE membership_id = $1`, membershipID); err != nil {
		return err
	}
	var grantedBy uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT created_by FROM crm.invitations WHERE id = $1`, invID).Scan(&grantedBy); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO crm.role_assignments (workspace_id, membership_id, role_id, product_ids, granted_by)
		VALUES ($1, $2, $3, $4, $5)`, workspaceID, membershipID, roleID, productIDs, grantedBy); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO crm.membership_permission_sets (workspace_id, membership_id, permission_set_id, granted_by)
		SELECT $1, $2, ps.id, $4 FROM crm.permission_sets ps
		JOIN crm.invitations inv ON inv.id = $3 AND ps.id = ANY(inv.permission_set_ids)
		WHERE ps.workspace_id = $1
		ON CONFLICT DO NOTHING`, workspaceID, membershipID, invID, grantedBy); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE crm.invitations SET status = 'accepted', accepted_at = now() WHERE id = $1`, invID)
	return err
}

// acceptByPhone accepts a person's open invitations when they sign in with a code sent to
// their phone: the number was put on the invitation by whoever invited them, so the code
// proves they are the invited person just as the emailed link does. The emailed link still
// works for setting a password; here the email address stays unverified.
func acceptByPhone(ctx context.Context, tx pgx.Tx, identityID uuid.UUID, meta RequestMeta) error {
	type open struct {
		id, ws, membership, role uuid.UUID
		products                 []uuid.UUID
	}
	rows, err := tx.Query(ctx, `
		SELECT inv.id, inv.workspace_id, inv.membership_id, inv.intended_role_id, inv.product_ids
		FROM crm.invitations inv
		JOIN crm.memberships m ON m.id = inv.membership_id
		JOIN crm.workspaces w ON w.id = inv.workspace_id
		WHERE m.identity_id = $1 AND m.status = 'invited' AND w.status = 'active'
		  AND inv.status IN ('pending', 'delivered', 'delivery_failed') AND inv.expires_at > now()
		ORDER BY inv.created_at
		FOR UPDATE OF inv`, identityID)
	if err != nil {
		return err
	}
	var list []open
	for rows.Next() {
		var o open
		if err := rows.Scan(&o.id, &o.ws, &o.membership, &o.role, &o.products); err != nil {
			rows.Close()
			return err
		}
		list = append(list, o)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, o := range list {
		if err := activateInvitation(ctx, tx, o.id, o.ws, o.membership, o.role, o.products); err != nil {
			return err
		}
		ws, id := o.ws, o.id
		if err := shared.WriteAudit(ctx, tx, shared.AuditEvent{
			WorkspaceID: &ws, ActorID: &identityID, Action: "invitation.accepted", EntityType: "invitation", EntityID: &id,
			After: map[string]any{"membershipId": o.membership, "method": "phone"},
			IP:    meta.IP, RequestID: meta.RequestID,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) handleInvitationPreview(w http.ResponseWriter, r *http.Request) {
	p, err := s.PreviewInvitation(r.Context(), r.URL.Query().Get("token"))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, p)
}

func (s *Service) handleInvitationAccept(w http.ResponseWriter, r *http.Request) {
	var in AcceptInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out, err := s.AcceptInvitation(r.Context(), in, Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	s.setSessionCookie(w, out.token, out.expires)
	shared.WriteJSON(w, http.StatusOK, out.step)
}
