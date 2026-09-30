package records

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/platform"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// API keys (D-61). A key belongs to one workspace and works only while the product's
// setup has "API access" on. It has full access or exactly one permission set's access;
// records it creates belong to the person who made the key, and "own" access means that
// person's records. The secret is shown once; only its SHA-256 is stored.

type apiKeyInfo struct {
	ID              uuid.UUID
	WorkspaceID     uuid.UUID
	WorkspaceCode   string
	Name            string
	CreatorID       uuid.UUID
	PermissionSetID *uuid.UUID
	Full            bool
}

type apiKeyCtxKey struct{}

func apiKeyFrom(ctx context.Context) *apiKeyInfo {
	k, _ := ctx.Value(apiKeyCtxKey{}).(*apiKeyInfo)
	return k
}

func hashAPIKey(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// ---- per-key rate limit: 100 requests a minute ----

type bucket struct {
	start time.Time
	n     int
}

var apiRate = struct {
	sync.Mutex
	m map[uuid.UUID]*bucket
}{m: map[uuid.UUID]*bucket{}}

func apiAllow(id uuid.UUID) (bool, int) {
	apiRate.Lock()
	defer apiRate.Unlock()
	b := apiRate.m[id]
	now := time.Now()
	if b == nil || now.Sub(b.start) >= time.Minute {
		apiRate.m[id] = &bucket{start: now, n: 1}
		return true, 0
	}
	if b.n >= 100 {
		return false, int(time.Minute.Seconds() - now.Sub(b.start).Seconds() + 1)
	}
	b.n++
	return true, 0
}

var lastUsed = struct {
	sync.Mutex
	m map[uuid.UUID]time.Time
}{m: map[uuid.UUID]time.Time{}}

// ResolveAPIKey is installed into identity: it checks a bearer key and returns the
// session of its creator plus the key for the record engine.
func (h *Handler) ResolveAPIKey(ctx context.Context, token string, r *http.Request) (context.Context, *identity.Session, error) {
	invalid := shared.NewError(http.StatusUnauthorized, "invalid_api_key", "This API key isn't valid. Check it, or create a new one.")
	k := &apiKeyInfo{}
	var roleKey *string
	var expires *time.Time
	var displayName string
	err := h.store.Pool.QueryRow(ctx, `
		SELECT k.id, k.workspace_id, w.code, k.name, k.created_by, k.permission_set_id, r.key, k.expires_at, i.display_name
		FROM crm.api_keys k JOIN crm.workspaces w ON w.id = k.workspace_id AND w.status = 'active'
		JOIN crm.identities i ON i.id = k.created_by AND i.status = 'active'
		LEFT JOIN crm.roles r ON r.id = k.role_id
		WHERE k.key_hash = $1 AND k.revoked_at IS NULL`, hashAPIKey(token)).
		Scan(&k.ID, &k.WorkspaceID, &k.WorkspaceCode, &k.Name, &k.CreatorID, &k.PermissionSetID, &roleKey, &expires, &displayName)
	if err != nil {
		return ctx, nil, invalid
	}
	if expires != nil && expires.Before(time.Now()) {
		return ctx, nil, shared.NewError(http.StatusUnauthorized, "api_key_expired", "This API key has expired. Create a new one.")
	}
	if ok, retry := apiAllow(k.ID); !ok {
		e := shared.NewError(http.StatusTooManyRequests, "rate_limited", "Too many requests — an API key can make 100 requests a minute.")
		e.RetryAfter = retry
		return ctx, nil, e
	}
	k.Full = roleKey != nil && *roleKey == "SUPER_ADMIN"
	lastUsed.Lock()
	if time.Since(lastUsed.m[k.ID]) > time.Minute {
		lastUsed.m[k.ID] = time.Now()
		ip := shared.ClientIP(r)
		go func(id uuid.UUID) {
			c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_, _ = h.store.Pool.Exec(c, `UPDATE crm.api_keys SET last_used_at = now(), last_used_ip = NULLIF($2, '')::inet WHERE id = $1`, id, ip)
		}(k.ID)
	}
	lastUsed.Unlock()
	now := time.Now()
	sess := &identity.Session{ID: k.ID, IdentityID: k.CreatorID, Audience: "api", MFAPassed: true, RecentAuthAt: &now, DisplayName: displayName,
		IdleExpiresAt: now.Add(time.Minute), AbsoluteExpiresAt: now.Add(time.Minute)}
	return context.WithValue(ctx, apiKeyCtxKey{}, k), sess, nil
}

// apiKeyScope builds the scope of an API key request inside /w/{code}.
func (h *Handler) apiKeyScope(ctx context.Context, code string, k *apiKeyInfo) (*Scope, error) {
	if k.WorkspaceCode != code {
		return nil, shared.Forbidden("wrong_workspace", "This API key belongs to another workspace.")
	}
	setup, err := platform.WorkspaceSetup(ctx, h.store.Pool, k.WorkspaceID)
	if err != nil {
		return nil, err
	}
	if !setup.Integrations.APIAccess {
		return nil, shared.Forbidden("api_access_off", "API access is turned off in this product's setup.")
	}
	sc := &Scope{WS: k.WorkspaceID, Code: code}
	if err := h.store.Pool.QueryRow(ctx, `SELECT name, is_platform FROM crm.workspaces WHERE id = $1`, k.WorkspaceID).Scan(&sc.Name, &sc.IsPlatformWS); err != nil {
		return nil, err
	}
	var rules access.Rules
	label := "API key: " + k.Name
	switch {
	case k.Full:
		sa, _ := access.FindSystemRole("SUPER_ADMIN")
		rules = sa.Rules
	case k.PermissionSetID != nil:
		var raw []byte
		if err := h.store.Pool.QueryRow(ctx, `SELECT rules FROM crm.permission_sets WHERE id = $1`, *k.PermissionSetID).Scan(&raw); err != nil {
			return nil, shared.Forbidden("api_key_no_access", "This API key's permission set no longer exists.")
		}
		rules = access.ParseRules(raw)
	}
	// Keys never manage people, roles or other keys.
	rules.Capabilities = without(rules.Capabilities, access.CapAccessManage, access.CapMembersManage, access.CapDeveloper)
	if sc.Eff, err = access.ForRules(ctx, h.store.Pool, k.WorkspaceID, label, rules); err != nil {
		return nil, err
	}
	return sc, nil
}

func without(list []string, drop ...string) []string {
	out := []string{}
	for _, v := range list {
		keep := true
		for _, d := range drop {
			if v == d {
				keep = false
			}
		}
		if keep {
			out = append(out, v)
		}
	}
	return out
}

// ---- management: /w/{code}/developer/api-keys ----

type APIKey struct {
	ID              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	Prefix          string     `json:"prefix"`
	Access          string     `json:"access"` // full | permission_set
	PermissionSetID *uuid.UUID `json:"permissionSetId,omitempty"`
	PermissionSet   string     `json:"permissionSet,omitempty"`
	CreatedBy       string     `json:"createdBy"`
	CreatedAt       time.Time  `json:"createdAt"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty"`
	LastUsedAt      *time.Time `json:"lastUsedAt,omitempty"`
	RevokedAt       *time.Time `json:"revokedAt,omitempty"`
	Secret          string     `json:"secret,omitempty"` // only when created
}

func (h *Handler) requireDeveloper(w http.ResponseWriter, r *http.Request, needWebhooks bool) (*Scope, *platform.ProductConfig, bool) {
	sc := scopeFrom(r.Context())
	if apiKeyFrom(r.Context()) != nil || (!sc.Owner && !sc.Eff.HasCapability(access.CapDeveloper)) {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "You need the “Manage API keys & webhooks” permission."))
		return nil, nil, false
	}
	setup, err := platform.WorkspaceSetup(r.Context(), h.store.Pool, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return nil, nil, false
	}
	return sc, &setup, true
}

func (h *Handler) handleListAPIKeys(w http.ResponseWriter, r *http.Request) {
	sc, setup, ok := h.requireDeveloper(w, r, false)
	if !ok {
		return
	}
	rows, err := h.store.Pool.Query(r.Context(), `
		SELECT k.id, k.name, k.prefix, COALESCE(r.key, ''), k.permission_set_id, COALESCE(ps.name, ''), COALESCE(i.display_name, ''),
		       k.created_at, k.expires_at, k.last_used_at, k.revoked_at
		FROM crm.api_keys k LEFT JOIN crm.roles r ON r.id = k.role_id LEFT JOIN crm.permission_sets ps ON ps.id = k.permission_set_id
		LEFT JOIN crm.identities i ON i.id = k.created_by
		WHERE k.workspace_id = $1 ORDER BY k.revoked_at IS NOT NULL, k.created_at DESC`, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		var k APIKey
		var roleKey string
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &roleKey, &k.PermissionSetID, &k.PermissionSet, &k.CreatedBy, &k.CreatedAt, &k.ExpiresAt, &k.LastUsedAt, &k.RevokedAt); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		k.Access = "permission_set"
		if roleKey == "SUPER_ADMIN" {
			k.Access = "full"
		}
		out = append(out, k)
	}
	if err := rows.Err(); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	type choice struct {
		ID   uuid.UUID `json:"id"`
		Name string    `json:"name"`
	}
	sets := []choice{}
	psRows, err := h.store.Pool.Query(r.Context(), `SELECT id, name FROM crm.permission_sets WHERE workspace_id = $1 OR workspace_id IS NULL ORDER BY name`, sc.WS)
	if err == nil {
		for psRows.Next() {
			var c choice
			if psRows.Scan(&c.ID, &c.Name) == nil {
				sets = append(sets, c)
			}
		}
		psRows.Close()
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": out, "apiAccess": setup.Integrations.APIAccess, "webhooks": setup.Integrations.Webhooks,
		"baseUrl": strings.TrimRight(h.cfg.BaseURL, "/") + "/api/crm/v1/w/" + sc.Code, "permissionSets": sets})
}

func (h *Handler) handleCreateAPIKey(w http.ResponseWriter, r *http.Request) {
	sc, setup, ok := h.requireDeveloper(w, r, false)
	if !ok {
		return
	}
	if !setup.Integrations.APIAccess {
		shared.WriteError(w, r, shared.NewError(http.StatusConflict, "api_access_off", "Turn on API access in this product's setup first (Product setup → Sign-in & integrations)."))
		return
	}
	var in struct {
		Name            string     `json:"name"`
		Access          string     `json:"access"`
		PermissionSetID *uuid.UUID `json:"permissionSetId"`
		ExpiresInDays   int        `json:"expiresInDays"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	fe := map[string]string{}
	if in.Name == "" || len(in.Name) > 80 {
		fe["name"] = "Name the key after what uses it (up to 80 characters)."
	}
	var roleID *uuid.UUID
	switch in.Access {
	case "full":
		// Full access needs someone who has it themselves.
		if !sc.Owner && sc.Eff.RoleKey != "SUPER_ADMIN" {
			fe["access"] = "Only a Super Admin can create a key with full access."
		}
		var id uuid.UUID
		if err := h.store.Pool.QueryRow(r.Context(), `SELECT id FROM crm.roles WHERE workspace_id = $1 AND key = 'SUPER_ADMIN'`, sc.WS).Scan(&id); err == nil {
			roleID = &id
		} else {
			fe["access"] = "This workspace has no Super Admin role."
		}
		in.PermissionSetID = nil
	case "permission_set":
		if in.PermissionSetID == nil {
			fe["permissionSetId"] = "Pick the permission set the key works with."
		} else {
			var exists bool
			_ = h.store.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM crm.permission_sets WHERE id = $1 AND (workspace_id = $2 OR workspace_id IS NULL))`,
				*in.PermissionSetID, sc.WS).Scan(&exists)
			if !exists {
				fe["permissionSetId"] = "Pick a permission set of this workspace."
			}
		}
	default:
		fe["access"] = "Choose full access or a permission set."
	}
	var expires *time.Time
	if in.ExpiresInDays > 0 {
		if in.ExpiresInDays > 3650 {
			fe["expiresInDays"] = "Keys can last at most 10 years."
		}
		t := time.Now().AddDate(0, 0, in.ExpiresInDays)
		expires = &t
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	secret, err := shared.RandomToken(32)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	pb := make([]byte, 4)
	_, _ = rand.Read(pb)
	prefix := "crm_" + hex.EncodeToString(pb)
	token := prefix + "_" + secret
	me := actor(r)
	var k APIKey
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `INSERT INTO crm.api_keys (workspace_id, name, prefix, key_hash, permission_set_id, role_id, product_ids, expires_at, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, '{}', $7, $8) RETURNING id, created_at`, sc.WS, in.Name, prefix, hashAPIKey(token), in.PermissionSetID, roleID, expires, me).
			Scan(&k.ID, &k.CreatedAt); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, actorFromRequest(r, "ui").audit(sc.WS, "api_key.created", "api_key", &k.ID, nil,
			map[string]any{"name": in.Name, "access": in.Access}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	k.Name, k.Prefix, k.Access, k.PermissionSetID, k.ExpiresAt, k.Secret = in.Name, prefix, in.Access, in.PermissionSetID, expires, token
	shared.WriteJSON(w, http.StatusCreated, k)
}

func (h *Handler) handleRevokeAPIKey(w http.ResponseWriter, r *http.Request) {
	sc, _, ok := h.requireDeveloper(w, r, false)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "keyId"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("api_key_not_found"))
		return
	}
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `UPDATE crm.api_keys SET revoked_at = now() WHERE id = $1 AND workspace_id = $2 AND revoked_at IS NULL`, id, sc.WS)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return shared.NotFound("api_key_not_found")
		}
		return shared.WriteAudit(r.Context(), tx, actorFromRequest(r, "ui").audit(sc.WS, "api_key.revoked", "api_key", &id, nil, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
