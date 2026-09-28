package identity

import (
	"context"
	"errors"
	"net/http"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const (
	SessionCookie = "crm_sid"
	CSRFCookie    = "crm_csrf"
	CSRFHeader    = "X-CSRF-Token"

	idlePrivileged   = 30 * time.Minute   // PRD AUTH-03
	idleStandard     = 7 * 24 * time.Hour // PRD AUTH-03
	absoluteLifetime = 30 * 24 * time.Hour
	touchInterval    = time.Minute
	maxMFAAttempts   = 5
)

// Session is a validated, unexpired, unrevoked session joined with its identity.
type Session struct {
	ID                 uuid.UUID
	IdentityID         uuid.UUID
	Audience           string
	MFARequired        bool
	MFAPassed          bool
	MFAAttempts        int
	RecentAuthAt       *time.Time
	Privileged         bool
	LastSeenAt         time.Time
	IdleExpiresAt      time.Time
	AbsoluteExpiresAt  time.Time
	DisplayName        string
	IsPlatformOwner    bool
	MustChangePassword bool
}

// MFAComplete reports whether the second factor (if any) has been satisfied (D-13).
func (s *Session) MFAComplete() bool { return !s.MFARequired || s.MFAPassed }

// Ready reports whether the session may use the application beyond auth screens.
func (s *Session) Ready() bool { return s.MFAComplete() && !s.MustChangePassword }

func idleFor(privileged bool) time.Duration {
	if privileged {
		return idlePrivileged
	}
	return idleStandard
}

type newSession struct {
	identityID  uuid.UUID
	audience    string
	privileged  bool
	mfaRequired bool
	ip          string
	userAgent   string
}

// createSession stores a fresh session and returns its raw token (only the hash is kept).
func createSession(ctx context.Context, tx pgx.Tx, n newSession) (string, time.Time, error) {
	token, err := shared.RandomToken(32)
	if err != nil {
		return "", time.Time{}, err
	}
	now := time.Now()
	absolute := now.Add(absoluteLifetime)
	_, err = tx.Exec(ctx, `
		INSERT INTO crm.sessions
			(identity_id, token_hash, audience, privileged, mfa_required, mfa_passed, recent_auth_at,
			 ip, user_agent, idle_expires_at, absolute_expires_at)
		VALUES ($1, $2, $3, $4, $5, false, $6, NULLIF($7, '')::inet, NULLIF($8, ''), $9, $10)`,
		n.identityID, shared.HashToken(token), n.audience, n.privileged, n.mfaRequired, now,
		n.ip, truncate(n.userAgent, 400), now.Add(idleFor(n.privileged)), absolute)
	if err != nil {
		return "", time.Time{}, err
	}
	return token, absolute, nil
}

func (s *Service) lookupSession(ctx context.Context, token string) (*Session, error) {
	if token == "" || len(token) > 128 {
		return nil, nil
	}
	var sess Session
	err := s.store.Pool.QueryRow(ctx, `
		SELECT s.id, s.identity_id, s.audience, s.mfa_required, s.mfa_passed, s.mfa_attempts, s.recent_auth_at,
		       s.privileged, s.last_seen_at, s.idle_expires_at, s.absolute_expires_at,
		       i.display_name, i.is_platform_owner, COALESCE(pc.must_change, false)
		FROM crm.sessions s
		JOIN crm.identities i ON i.id = s.identity_id
		LEFT JOIN crm.password_credentials pc ON pc.identity_id = i.id
		WHERE s.token_hash = $1
		  AND s.revoked_at IS NULL
		  AND s.idle_expires_at > now()
		  AND s.absolute_expires_at > now()
		  AND i.status = 'active'`, shared.HashToken(token)).Scan(
		&sess.ID, &sess.IdentityID, &sess.Audience, &sess.MFARequired, &sess.MFAPassed, &sess.MFAAttempts,
		&sess.RecentAuthAt, &sess.Privileged, &sess.LastSeenAt, &sess.IdleExpiresAt, &sess.AbsoluteExpiresAt,
		&sess.DisplayName, &sess.IsPlatformOwner, &sess.MustChangePassword)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &sess, nil
}

// touchSession slides the idle window, at most once a minute per session.
func (s *Service) touchSession(ctx context.Context, sess *Session) {
	if time.Since(sess.LastSeenAt) < touchInterval {
		return
	}
	idle := time.Now().Add(idleFor(sess.Privileged))
	if idle.After(sess.AbsoluteExpiresAt) {
		idle = sess.AbsoluteExpiresAt
	}
	_, _ = s.store.Pool.Exec(ctx, `UPDATE crm.sessions SET last_seen_at = now(), idle_expires_at = $2 WHERE id = $1`, sess.ID, idle)
	sess.IdleExpiresAt = idle
}

// rotateToken issues a new token for an existing session (login, privilege change,
// MFA completion — PRD AUTH-03) so a token seen before the change is useless after it.
func rotateToken(ctx context.Context, q shared.Execer, sessionID uuid.UUID) (string, error) {
	token, err := shared.RandomToken(32)
	if err != nil {
		return "", err
	}
	if _, err := q.Exec(ctx, `UPDATE crm.sessions SET token_hash = $2 WHERE id = $1`, sessionID, shared.HashToken(token)); err != nil {
		return "", err
	}
	return token, nil
}

func (s *Service) setSessionCookie(w http.ResponseWriter, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Service) clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.cfg.CookieSecure,
		SameSite: http.SameSiteLaxMode,
	})
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
