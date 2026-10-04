package middleware

import (
	"context"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/auth"
	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/database"
	"cardflow-backend/internal/domain"
	"cardflow-backend/pkg/response"
	"github.com/google/uuid"
)

type Middleware struct {
	jwt   *auth.JWTService
	db    *database.DB
	ident *identity.Service
}

// SetIdentity lets this API accept the unified session token (D-93) next to the older JWT.
func (m *Middleware) SetIdentity(i *identity.Service) { m.ident = i }

func NewMiddleware(jwt *auth.JWTService, db *database.DB) *Middleware {
	return &Middleware{jwt: jwt, db: db}
}

// UserContextKey must be a plain string (not a custom type) so handlers in
// packages that cannot import middleware (e.g. auth) can read it via "user".
const UserContextKey = "user"

// userForSession resolves a unified session token to the app profile of its identity.
func (m *Middleware) userForSession(r *http.Request, token string) (*domain.User, bool) {
	if m.ident == nil || m.db == nil || m.db.Pool == nil {
		return nil, false
	}
	sess, err := m.ident.LookupBearer(r.Context(), token)
	if err != nil || sess == nil || !sess.Ready() {
		return nil, false
	}
	user := &domain.User{}
	var roleStr, planStr string
	err = m.db.Pool.QueryRow(r.Context(), `
		SELECT id, COALESCE(phone, ''), COALESCE(name, ''), role::text, plan::text,
		       is_subscribed, subscription_plan_id, subscription_expires_at
		FROM users WHERE identity_id = $1 AND deleted_at IS NULL`, sess.IdentityID).Scan(
		&user.ID, &user.Phone, &user.Name, &roleStr, &planStr, &user.IsSubscribed, &user.SubscriptionPlanID, &user.SubscriptionExpiresAt)
	if err != nil {
		return nil, false
	}
	user.Role = domain.UserRole(roleStr)
	user.Plan = domain.SubscriptionPlan(planStr)
	return user, true
}

func (m *Middleware) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if authHeader == "" {
			response.Unauthorized(w, "missing Authorization header")
			return
		}

		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || strings.ToLower(parts[0]) != "bearer" {
			response.Unauthorized(w, "invalid Authorization header format. Expected 'Bearer <token>'")
			return
		}

		tokenString := parts[1]
		// Unified session (D-93): a revocable token from the identity service. The person's
		// app profile is the row linked to that identity.
		if strings.HasPrefix(tokenString, identity.BearerPrefix) {
			user, ok := m.userForSession(r, tokenString)
			if !ok {
				response.Unauthorized(w, "your session has ended. Sign in again")
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), UserContextKey, user)))
			return
		}
		claims, err := m.jwt.ValidateAccessToken(tokenString)
		if err != nil || claims == nil {
			response.Unauthorized(w, "invalid or expired token")
			return
		}

		userUUID, err := uuid.Parse(claims.UserID)
		if err != nil {
			response.Unauthorized(w, "invalid token subject")
			return
		}

		user := &domain.User{
			ID:    userUUID,
			Phone: claims.Phone,
			Role:  domain.UserRole(claims.Role),
			Plan:  domain.SubscriptionPlan(claims.Plan),
		}

		// Prefer live role/plan from DB when available
		if m.db != nil && m.db.Pool != nil {
			var roleStr, planStr, name, phone string
			var isSubscribed bool
			var subPlanID *string
			var subExpiresAt *time.Time
			qErr := m.db.Pool.QueryRow(r.Context(), `
				SELECT phone, COALESCE(name, ''), role::text, plan::text,
				       is_subscribed, subscription_plan_id, subscription_expires_at
				FROM users WHERE id = $1 AND deleted_at IS NULL
			`, userUUID).Scan(&phone, &name, &roleStr, &planStr, &isSubscribed, &subPlanID, &subExpiresAt)
			if qErr == nil {
				user.Phone = phone
				user.Name = name
				user.Role = domain.UserRole(roleStr)
				user.Plan = domain.SubscriptionPlan(planStr)
				user.IsSubscribed = isSubscribed
				user.SubscriptionPlanID = subPlanID
				user.SubscriptionExpiresAt = subExpiresAt
			}
		}

		ctx := context.WithValue(r.Context(), UserContextKey, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
