package identity

import (
	"context"
	"time"

	"cardflow-backend/internal/crm/access"
	"github.com/google/uuid"
)

type MeIdentity struct {
	ID              uuid.UUID  `json:"id"`
	DisplayName     string     `json:"displayName"`
	Email           string     `json:"email,omitempty"`
	Phone           string     `json:"phone,omitempty"`
	IsPlatformOwner bool       `json:"isPlatformOwner"`
	Locale          string     `json:"locale"`
	Timezone        string     `json:"timezone"`
	LastLoginAt     *time.Time `json:"lastLoginAt,omitempty"`
}

type MeSession struct {
	Audience           string    `json:"audience"`
	MFARequired        bool      `json:"mfaRequired"`
	MFAPassed          bool      `json:"mfaPassed"`
	MFAEnrolled        bool      `json:"mfaEnrolled"`
	MFAEnforced        bool      `json:"mfaEnforced"`
	MustChangePassword bool      `json:"mustChangePassword"`
	Privileged         bool      `json:"privileged"`
	IdleExpiresAt      time.Time `json:"idleExpiresAt"`
}

type MeResponse struct {
	Identity    MeIdentity          `json:"identity"`
	Session     MeSession           `json:"session"`
	Memberships []access.Membership `json:"memberships"`
	AuthVersion int                 `json:"authVersion"`
	Next        string              `json:"next"`
}

// Me is the /me bootstrap (PRD §4): identity, memberships, session state and auth version.
func (s *Service) Me(ctx context.Context, sess *Session) (*MeResponse, error) {
	var out MeResponse
	err := s.store.Pool.QueryRow(ctx, `
		SELECT i.id, i.display_name, i.is_platform_owner, i.locale, i.timezone, i.last_login_at,
		       COALESCE((SELECT value_normalized FROM crm.verified_identifiers WHERE identity_id = i.id AND kind = 'email' AND verified_at IS NOT NULL ORDER BY verified_at LIMIT 1), ''),
		       COALESCE((SELECT value_normalized FROM crm.verified_identifiers WHERE identity_id = i.id AND kind = 'phone' AND verified_at IS NOT NULL ORDER BY verified_at LIMIT 1), ''),
		       EXISTS (SELECT 1 FROM crm.mfa_methods WHERE identity_id = i.id AND confirmed_at IS NOT NULL)
		FROM crm.identities i WHERE i.id = $1`, sess.IdentityID).Scan(
		&out.Identity.ID, &out.Identity.DisplayName, &out.Identity.IsPlatformOwner, &out.Identity.Locale,
		&out.Identity.Timezone, &out.Identity.LastLoginAt, &out.Identity.Email, &out.Identity.Phone,
		&out.Session.MFAEnrolled)
	if err != nil {
		return nil, err
	}

	// A session still waiting for its second factor learns nothing beyond what the
	// MFA screen needs.
	if sess.MFAComplete() {
		ms, err := access.ListActiveMemberships(ctx, s.store.Pool, sess.IdentityID)
		if err != nil {
			return nil, err
		}
		out.Memberships = ms
		for _, m := range ms {
			if m.AuthVersion > out.AuthVersion {
				out.AuthVersion = m.AuthVersion
			}
		}
	} else {
		out.Identity.Email = maskEmail(out.Identity.Email)
		out.Identity.Phone = ""
	}
	if out.Memberships == nil {
		out.Memberships = []access.Membership{}
	}

	out.Session = MeSession{
		Audience:           sess.Audience,
		MFARequired:        sess.MFARequired,
		MFAPassed:          sess.MFAPassed,
		MFAEnrolled:        out.Session.MFAEnrolled,
		MFAEnforced:        s.cfg.MFAEnforced(),
		MustChangePassword: sess.MustChangePassword,
		Privileged:         sess.Privileged,
		IdleExpiresAt:      sess.IdleExpiresAt,
	}
	enrollRequired := sess.MFARequired && !sess.MFAPassed && !out.Session.MFAEnrolled
	out.Next = nextPath(sess.IsPlatformOwner, sess.MFARequired && !sess.MFAPassed, enrollRequired, sess.MustChangePassword)
	return &out, nil
}

func maskEmail(email string) string {
	for i, r := range email {
		if r == '@' {
			if i <= 1 {
				return "•" + email[i:]
			}
			return email[:1] + "•••" + email[i:]
		}
	}
	return ""
}

type SessionInfo struct {
	ID         uuid.UUID `json:"id"`
	IP         string    `json:"ip,omitempty"`
	UserAgent  string    `json:"userAgent,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
	LastSeenAt time.Time `json:"lastSeenAt"`
	Current    bool      `json:"current"`
}

func (s *Service) ListSessions(ctx context.Context, sess *Session) ([]SessionInfo, error) {
	rows, err := s.store.Pool.Query(ctx, `
		SELECT id, COALESCE(host(ip), ''), COALESCE(user_agent, ''), created_at, last_seen_at
		FROM crm.sessions
		WHERE identity_id = $1 AND revoked_at IS NULL AND idle_expires_at > now() AND absolute_expires_at > now()
		ORDER BY last_seen_at DESC LIMIT 50`, sess.IdentityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SessionInfo{}
	for rows.Next() {
		var si SessionInfo
		if err := rows.Scan(&si.ID, &si.IP, &si.UserAgent, &si.CreatedAt, &si.LastSeenAt); err != nil {
			return nil, err
		}
		si.Current = si.ID == sess.ID
		out = append(out, si)
	}
	return out, rows.Err()
}

func (s *Service) RevokeSession(ctx context.Context, sess *Session, id uuid.UUID) error {
	_, err := s.store.Pool.Exec(ctx, `
		UPDATE crm.sessions SET revoked_at = now() WHERE id = $1 AND identity_id = $2 AND revoked_at IS NULL`,
		id, sess.IdentityID)
	return err
}
