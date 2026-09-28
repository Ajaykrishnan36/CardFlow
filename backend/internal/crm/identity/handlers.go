package identity

import (
	"context"
	"net/http"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
)

type ctxKey int

const (
	sessionKey ctxKey = iota + 1
	csrfKey
)

// SessionFrom returns the request's session, or nil when unauthenticated.
func SessionFrom(ctx context.Context) *Session {
	s, _ := ctx.Value(sessionKey).(*Session)
	return s
}

func Meta(r *http.Request) RequestMeta {
	return RequestMeta{
		IP:        shared.ClientIP(r),
		UserAgent: r.UserAgent(),
		RequestID: chiMiddleware.GetReqID(r.Context()),
	}
}

func isSafeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// CSRF implements the double-submit check (PRD AUTH-03, D-15): a readable crm_csrf
// cookie is issued when missing, and every unsafe request must echo it in X-CSRF-Token.
func (s *Service) CSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookieVal := ""
		if c, err := r.Cookie(CSRFCookie); err == nil && len(c.Value) == 43 {
			cookieVal = c.Value
		}
		token := cookieVal
		if token == "" {
			fresh, err := shared.RandomToken(32)
			if err != nil {
				shared.WriteError(w, r, err)
				return
			}
			token = fresh
			http.SetCookie(w, &http.Cookie{
				Name:     CSRFCookie,
				Value:    token,
				Path:     "/",
				MaxAge:   int(absoluteLifetime.Seconds()),
				HttpOnly: false, // the SPA must read it
				Secure:   s.cfg.CookieSecure,
				SameSite: http.SameSiteLaxMode,
			})
		}
		if !isSafeMethod(r.Method) {
			header := r.Header.Get(CSRFHeader)
			if cookieVal == "" || header == "" || !shared.ConstantTimeEqual(header, cookieVal) {
				shared.WriteError(w, r, shared.Forbidden("csrf_failed", "Your security token expired. Refresh the page and try again."))
				return
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), csrfKey, token)))
	})
}

// LoadSession attaches the session (if any) to the request context.
func (s *Service) LoadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(SessionCookie)
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		sess, err := s.lookupSession(r.Context(), c.Value)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		if sess == nil {
			s.clearSessionCookie(w)
			next.ServeHTTP(w, r)
			return
		}
		s.touchSession(r.Context(), sess)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey, sess)))
	})
}

// RequireSession allows any valid session, including one still waiting for MFA.
func RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if SessionFrom(r.Context()) == nil {
			shared.WriteError(w, r, shared.Unauthenticated())
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireMFAComplete allows sessions whose second factor (if required) is done.
func RequireMFAComplete(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := SessionFrom(r.Context())
		switch {
		case sess == nil:
			shared.WriteError(w, r, shared.Unauthenticated())
		case !sess.MFAComplete():
			shared.WriteError(w, r, shared.Forbidden("mfa_required", "Complete two-step verification first."))
		default:
			next.ServeHTTP(w, r)
		}
	})
}

// RequireReady additionally blocks sessions that must change their password.
func RequireReady(next http.Handler) http.Handler {
	return RequireMFAComplete(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if SessionFrom(r.Context()).MustChangePassword {
			shared.WriteError(w, r, shared.Forbidden("password_change_required", "Set a new password to continue."))
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// RequireOwner allows only a ready Platform Owner session (PRD OWN-01).
func RequireOwner(next http.Handler) http.Handler {
	return RequireReady(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess := SessionFrom(r.Context())
		if !sess.IsPlatformOwner || sess.Audience != "owner" {
			shared.WriteError(w, r, shared.Forbidden("forbidden", "You don't have access to this area."))
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// Routes mounts the auth and /me endpoints (PRD §14.1 Auth, Me).
func (s *Service) Routes(r chi.Router) {
	r.Get("/auth/csrf", s.handleCSRF)
	r.Post("/auth/login", s.handleLogin)
	r.Post("/auth/password/forgot", s.handleForgot)
	r.Post("/auth/password/reset", s.handleReset)
	r.Get("/invitations/preview", s.handleInvitationPreview)
	r.Post("/invitations/accept", s.handleInvitationAccept)

	r.Group(func(r chi.Router) {
		r.Use(RequireSession)
		r.Get("/me", s.handleMe)
		r.Post("/auth/logout", s.handleLogout)
		r.Post("/auth/mfa/verify", s.handleMFAVerify)
		r.Post("/auth/mfa/enroll", s.handleMFAEnroll)
		r.Post("/auth/mfa/confirm", s.handleMFAConfirm)
	})
	r.Group(func(r chi.Router) {
		r.Use(RequireMFAComplete)
		r.Post("/auth/logout-all", s.handleLogoutAll)
		r.Post("/auth/password/change", s.handleChangePassword)
		r.Get("/me/sessions", s.handleListSessions)
		r.Delete("/me/sessions/{id}", s.handleRevokeSession)
	})
	r.Group(func(r chi.Router) {
		r.Use(RequireReady)
		r.Get("/capabilities", s.handleCapabilities)
	})
}

func (s *Service) handleCSRF(w http.ResponseWriter, r *http.Request) {
	token, _ := r.Context().Value(csrfKey).(string)
	shared.WriteJSON(w, http.StatusOK, map[string]string{"csrfToken": token})
}

func (s *Service) handleLogin(w http.ResponseWriter, r *http.Request) {
	var in LoginInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out, err := s.Login(r.Context(), in, Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	s.setSessionCookie(w, out.token, out.expires)
	shared.WriteJSON(w, http.StatusOK, out.step)
}

func (s *Service) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.Logout(r.Context(), SessionFrom(r.Context()), false, Meta(r)); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleLogoutAll(w http.ResponseWriter, r *http.Request) {
	if err := s.Logout(r.Context(), SessionFrom(r.Context()), true, Meta(r)); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	s.clearSessionCookie(w)
	w.WriteHeader(http.StatusNoContent)
}

type codeBody struct {
	Code string `json:"code"`
}

func (s *Service) handleMFAVerify(w http.ResponseWriter, r *http.Request) {
	var in codeBody
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	sess := SessionFrom(r.Context())
	out, err := s.VerifyMFA(r.Context(), sess, in.Code, Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	s.setSessionCookie(w, out.token, sess.AbsoluteExpiresAt)
	shared.WriteJSON(w, http.StatusOK, out)
}

func (s *Service) handleMFAEnroll(w http.ResponseWriter, r *http.Request) {
	out, err := s.EnrollMFA(r.Context(), SessionFrom(r.Context()))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, out)
}

func (s *Service) handleMFAConfirm(w http.ResponseWriter, r *http.Request) {
	var in codeBody
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	sess := SessionFrom(r.Context())
	out, err := s.ConfirmMFA(r.Context(), sess, in.Code, Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if out.token != "" {
		s.setSessionCookie(w, out.token, sess.AbsoluteExpiresAt)
	}
	shared.WriteJSON(w, http.StatusOK, out)
}

func (s *Service) handleForgot(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Identifier string `json:"identifier"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if err := s.ForgotPassword(r.Context(), in.Identifier, Meta(r)); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]string{
		"message": "If an account exists for that email, a reset link is on its way. It expires in 30 minutes.",
	})
}

func (s *Service) handleReset(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if err := s.ResetPassword(r.Context(), in.Token, in.Password, Meta(r)); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	s.clearSessionCookie(w)
	shared.WriteJSON(w, http.StatusOK, map[string]string{"message": "Your password has been updated. Sign in with your new password."})
}

func (s *Service) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var in struct {
		CurrentPassword string `json:"currentPassword"`
		NewPassword     string `json:"newPassword"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	sess := SessionFrom(r.Context())
	out, err := s.ChangePassword(r.Context(), sess, in.CurrentPassword, in.NewPassword, Meta(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	s.setSessionCookie(w, out.token, sess.AbsoluteExpiresAt)
	shared.WriteJSON(w, http.StatusOK, out)
}

func (s *Service) handleMe(w http.ResponseWriter, r *http.Request) {
	out, err := s.Me(r.Context(), SessionFrom(r.Context()))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, out)
}

func (s *Service) handleListSessions(w http.ResponseWriter, r *http.Request) {
	out, err := s.ListSessions(r.Context(), SessionFrom(r.Context()))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

func (s *Service) handleRevokeSession(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("session_not_found"))
		return
	}
	if err := s.RevokeSession(r.Context(), SessionFrom(r.Context()), id); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Service) handleCapabilities(w http.ResponseWriter, r *http.Request) {
	sess := SessionFrom(r.Context())
	if sess.IsPlatformOwner && sess.Audience == "owner" {
		shared.WriteJSON(w, http.StatusOK, access.OwnerCapabilities())
		return
	}
	ms, err := access.ListActiveMemberships(r.Context(), s.store.Pool, sess.IdentityID)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	caps := access.Capabilities{Audience: "workspace", Actions: []string{}, Navigation: []access.NavItem{
		{Key: "settings", Label: "Profile & security", Path: "/crm/me", Icon: "settings", Available: true},
	}}
	if m := access.DefaultMembership(ms); m != nil {
		eff, err := access.ForMembership(r.Context(), s.store.Pool, m.ID)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		caps.Navigation = access.WorkspaceNav(m.WorkspaceCode, eff, access.HasSupport(m.WorkspaceID))
	}
	shared.WriteJSON(w, http.StatusOK, caps)
}

// RecentAuthWithin reports whether the session re-authenticated within d (PRD §4:
// sensitive actions require re-auth within 10 minutes).
func RecentAuthWithin(sess *Session, d time.Duration) bool {
	return sess.RecentAuthAt != nil && time.Since(*sess.RecentAuthAt) <= d
}
