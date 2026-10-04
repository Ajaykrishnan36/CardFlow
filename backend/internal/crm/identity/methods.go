package identity

import (
	"context"
	"net/http"
	"strings"

	"cardflow-backend/internal/crm/shared"
)

// Sign-in methods per product (D-64). A product's setup lists how its users may sign in:
// password, email code, Google, Microsoft, LinkedIn or its own SAML identity provider.
// The policy comes from the platform (set at start-up) so identity doesn't depend on it.

// MethodPolicy returns the sign-in methods a workspace allows ("" = no restriction).
type MethodPolicy func(ctx context.Context, workspaceCode string) ([]string, error)

var methodPolicy MethodPolicy

func SetMethodPolicy(p MethodPolicy) { methodPolicy = p }

// MethodLabels name sign-in methods for people.
var MethodLabels = map[string]string{"password": "a password", "otp": "an email sign-in code", "phone": "a code sent to your phone", "google": "Google",
	"microsoft": "Microsoft", "linkedin": "LinkedIn", "sso": "your company's single sign-on"}

// SelfServePolicy says whether people may create their own business (D-94). It is set by
// the platform at start-up; without one, self-serve is on.
var selfServePolicy func(ctx context.Context) bool

func SetSelfServePolicy(p func(ctx context.Context) bool) { selfServePolicy = p }

// SelfServeEnabled reports whether a signed-in person without a business may create one.
func SelfServeEnabled(ctx context.Context) bool {
	if selfServePolicy == nil {
		return true
	}
	return selfServePolicy(ctx)
}

// AllowedMethods is the list for a workspace (nil = anything).
func AllowedMethods(ctx context.Context, code string) []string {
	if methodPolicy == nil || code == "" {
		return nil
	}
	list, err := methodPolicy(ctx, code)
	if err != nil {
		return nil
	}
	return list
}

func methodAllowed(list []string, method string) bool {
	if list == nil {
		return true
	}
	if method == "invite" || method == "reset" {
		method = "password"
	}
	for _, m := range list {
		if m == method {
			return true
		}
	}
	return false
}

// MethodError explains which methods a product accepts.
func MethodError(list []string) error {
	names := []string{}
	for _, m := range list {
		names = append(names, MethodLabels[m])
	}
	e := shared.Forbidden("sign_in_method_not_allowed", "This product asks you to sign in with "+joinOr(names)+".")
	e.FieldErrors = map[string]string{"methods": strings.Join(list, ",")}
	return e
}

func joinOr(list []string) string {
	switch len(list) {
	case 0:
		return "another method"
	case 1:
		return list[0]
	}
	return strings.Join(list[:len(list)-1], ", ") + " or " + list[len(list)-1]
}

// CheckMethod fails when a product doesn't accept a sign-in method.
func CheckMethod(ctx context.Context, code, method string) error {
	list := AllowedMethods(ctx, code)
	if methodAllowed(list, method) {
		return nil
	}
	return MethodError(list)
}

// SessionMethodAllowed reports whether a signed-in session may open a workspace.
func SessionMethodAllowed(ctx context.Context, sess *Session, code string) error {
	// Sessions from before methods were recorded (AuthMethod "") stay valid until they expire.
	if sess == nil || sess.IsPlatformOwner || sess.Audience == "api" || sess.AuthMethod == "" {
		return nil
	}
	return CheckMethod(ctx, code, sess.AuthMethod)
}

// GET /auth/methods?product=<code> — the sign-in options to show.
func (s *Service) handleMethods(w http.ResponseWriter, r *http.Request) {
	code := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("product")))
	list := AllowedMethods(r.Context(), code)
	if list == nil {
		list = []string{"password", "otp", "phone", "google", "microsoft", "linkedin"}
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"methods": list, "product": code, "signup": SignupAllowed(r.Context(), code)})
}
