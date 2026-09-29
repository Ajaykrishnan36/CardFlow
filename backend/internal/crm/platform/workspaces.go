package platform

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---- DTOs ----

type WorkspaceSummary struct {
	ID             uuid.UUID `json:"id"`
	Code           string    `json:"code"`
	Name           string    `json:"name"`
	Status         string    `json:"status"`
	Timezone       string    `json:"timezone"`
	Locale         string    `json:"locale"`
	Currency       string    `json:"currency"`
	Products       int       `json:"products"`
	Members        int       `json:"members"`
	PendingInvites int       `json:"pendingInvites"`
	CreatedAt      time.Time `json:"createdAt"`
}

type WorkspaceProduct struct {
	ProductID     uuid.UUID `json:"productId"`
	Key           string    `json:"key"`
	Name          string    `json:"name"`
	Status        string    `json:"status"`
	ProductStatus string    `json:"productStatus"`
	ConfigVersion int       `json:"configVersion"`
	LatestVersion *int      `json:"latestVersion,omitempty"`
	AssignedAt    time.Time `json:"assignedAt"`
}

type WorkspaceMember struct {
	MembershipID uuid.UUID  `json:"membershipId"`
	IdentityID   uuid.UUID  `json:"identityId"`
	DisplayName  string     `json:"displayName"`
	Email        string     `json:"email,omitempty"`
	Status       string     `json:"status"`
	RoleKey      string     `json:"roleKey,omitempty"`
	RoleName     string     `json:"roleName,omitempty"`
	LastLoginAt  *time.Time `json:"lastLoginAt,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
}

type Invitation struct {
	ID           uuid.UUID  `json:"id"`
	Email        string     `json:"email,omitempty"`
	DisplayName  string     `json:"displayName"`
	RoleKey      string     `json:"roleKey"`
	RoleName     string     `json:"roleName"`
	Status       string     `json:"status"`
	ExpiresAt    time.Time  `json:"expiresAt"`
	CreatedAt    time.Time  `json:"createdAt"`
	AcceptedAt   *time.Time `json:"acceptedAt,omitempty"`
	DevAcceptURL string     `json:"devAcceptUrl,omitempty"`
}

type LinkedAccount struct {
	ID   uuid.UUID `json:"id"`
	Code string    `json:"code"`
	Name string    `json:"name"`
}

type WorkspaceDetail struct {
	WorkspaceSummary
	ProductList []WorkspaceProduct `json:"productList"`
	MemberList  []WorkspaceMember  `json:"memberList"`
	Invitations []Invitation       `json:"invitations"`
	Account     *LinkedAccount     `json:"account,omitempty"`
}

const workspaceSummarySelect = `
	SELECT w.id, w.code, w.name, w.status, w.timezone, w.locale, w.currency, w.created_at,
	       (SELECT count(*) FROM crm.workspace_products wp WHERE wp.workspace_id = w.id AND wp.status = 'active'),
	       (SELECT count(*) FROM crm.memberships m WHERE m.workspace_id = w.id AND m.status = 'active'),
	       (SELECT count(*) FROM crm.invitations i WHERE i.workspace_id = w.id AND i.status IN ('pending', 'delivered') AND i.expires_at > now())
	FROM crm.workspaces w`

func scanWorkspaceSummary(row pgx.Row) (WorkspaceSummary, error) {
	var s WorkspaceSummary
	err := row.Scan(&s.ID, &s.Code, &s.Name, &s.Status, &s.Timezone, &s.Locale, &s.Currency, &s.CreatedAt,
		&s.Products, &s.Members, &s.PendingInvites)
	return s, err
}

func (h *Handler) listWorkspaces(ctx context.Context, q, status string) ([]WorkspaceSummary, error) {
	rows, err := h.store.Pool.Query(ctx, workspaceSummarySelect+`
		WHERE NOT w.is_platform
		  AND ($1 = '' OR w.name ILIKE $1 OR w.code ILIKE $1)
		  AND ($2 = '' OR w.status = $2)
		ORDER BY w.created_at DESC`, likePattern(q), status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []WorkspaceSummary{}
	for rows.Next() {
		s, err := scanWorkspaceSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (h *Handler) getWorkspace(ctx context.Context, id uuid.UUID) (*WorkspaceDetail, error) {
	sum, err := scanWorkspaceSummary(h.store.Pool.QueryRow(ctx, workspaceSummarySelect+` WHERE w.id = $1 AND NOT w.is_platform`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NotFound("workspace_not_found")
	}
	if err != nil {
		return nil, err
	}
	d := &WorkspaceDetail{WorkspaceSummary: sum, ProductList: []WorkspaceProduct{}, MemberList: []WorkspaceMember{}, Invitations: []Invitation{}}

	rows, err := h.store.Pool.Query(ctx, `
		SELECT p.id, p.key, p.name, wp.status, p.status, wp.config_version, p.current_version, wp.created_at
		FROM crm.workspace_products wp JOIN crm.products p ON p.id = wp.product_id
		WHERE wp.workspace_id = $1 ORDER BY p.name`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p WorkspaceProduct
		if err := rows.Scan(&p.ProductID, &p.Key, &p.Name, &p.Status, &p.ProductStatus, &p.ConfigVersion, &p.LatestVersion, &p.AssignedAt); err != nil {
			rows.Close()
			return nil, err
		}
		d.ProductList = append(d.ProductList, p)
	}
	rows.Close()

	rows, err = h.store.Pool.Query(ctx, `
		SELECT m.id, i.id, i.display_name, COALESCE(e.value_normalized, ''), m.status,
		       COALESCE(r.key, ''), COALESCE(r.name, ''), i.last_login_at, m.created_at
		FROM crm.memberships m
		JOIN crm.identities i ON i.id = m.identity_id
		LEFT JOIN crm.verified_identifiers e ON e.identity_id = i.id AND e.kind = 'email' AND e.namespace = 'global'
		LEFT JOIN LATERAL (
			SELECT ro.key, ro.name FROM crm.role_assignments ra JOIN crm.roles ro ON ro.id = ra.role_id
			WHERE ra.membership_id = m.id ORDER BY ro.rank DESC LIMIT 1
		) r ON true
		WHERE m.workspace_id = $1 AND m.status <> 'invited'
		ORDER BY m.status = 'active' DESC, i.display_name`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m WorkspaceMember
		if err := rows.Scan(&m.MembershipID, &m.IdentityID, &m.DisplayName, &m.Email, &m.Status, &m.RoleKey, &m.RoleName, &m.LastLoginAt, &m.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		d.MemberList = append(d.MemberList, m)
	}
	rows.Close()

	d.Invitations, err = h.listInvitations(ctx, `inv.workspace_id = $1`, id)
	if err != nil {
		return nil, err
	}

	var acc LinkedAccount
	err = h.store.Pool.QueryRow(ctx, `
		SELECT id, code, name FROM crm.accounts WHERE customer_workspace_id = $1 AND deleted_at IS NULL
		ORDER BY created_at LIMIT 1`, id).Scan(&acc.ID, &acc.Code, &acc.Name)
	if err == nil {
		d.Account = &acc
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	return d, nil
}

// ---- Provisioning (PRD §3 step 5; OWN-02) ----

type SuperAdminInput struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type ProvisionInput struct {
	Name       string      `json:"name"`
	Code       string      `json:"code"`
	Timezone   string      `json:"timezone"`
	Locale     string      `json:"locale"`
	Currency   string      `json:"currency"`
	ProductIDs []uuid.UUID `json:"productIds"`
	// ProductName names the project's own product when no existing one is
	// picked (defaults to the project name); its setup is edited afterwards.
	ProductName string           `json:"productName,omitempty"`
	SuperAdmin  *SuperAdminInput `json:"superAdmin,omitempty"`
}

var (
	workspaceCodeRe = regexp.MustCompile(`^[a-z0-9-]{3,40}$`)
	currencyRe      = regexp.MustCompile(`^[A-Z]{3}$`)
	supportedLocale = map[string]bool{"en": true, "hi": true, "ta": true, "te": true, "ml": true, "kn": true, "mr": true, "bn": true, "gu": true}
)

// fieldPrefix lets lead conversion reuse the validation with "provision." keys.
func (in *ProvisionInput) normalizeAndValidate(fieldPrefix string) map[string]string {
	in.Name = strings.TrimSpace(in.Name)
	in.Code = strings.ToLower(strings.TrimSpace(in.Code))
	in.Timezone = strings.TrimSpace(in.Timezone)
	in.Locale = strings.TrimSpace(in.Locale)
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	in.ProductName = strings.TrimSpace(in.ProductName)
	if len(in.ProductName) > 120 {
		in.ProductName = in.ProductName[:120]
	}
	if in.Timezone == "" {
		in.Timezone = "Asia/Kolkata"
	}
	if in.Locale == "" {
		in.Locale = "en"
	}
	if in.Currency == "" {
		in.Currency = "INR"
	}
	f := map[string]string{}
	switch {
	case len(in.Name) < 2:
		f[fieldPrefix+"name"] = "Enter the customer's workspace name."
	case len(in.Name) > 80:
		f[fieldPrefix+"name"] = "Use at most 80 characters."
	}
	if !workspaceCodeRe.MatchString(in.Code) {
		f[fieldPrefix+"code"] = "3–40 characters: lower-case letters, digits and hyphens."
	} else if in.Code == "platform" {
		f[fieldPrefix+"code"] = "This code is reserved."
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		f[fieldPrefix+"timezone"] = "Pick a valid timezone."
	}
	if !supportedLocale[in.Locale] {
		f[fieldPrefix+"locale"] = "Pick a supported language."
	}
	if !currencyRe.MatchString(in.Currency) {
		f[fieldPrefix+"currency"] = "Use a 3-letter currency code."
	}
	// No product picked: the project gets its own setup (D-49), created when it's provisioned.
	if in.SuperAdmin != nil {
		in.SuperAdmin.Name = strings.TrimSpace(in.SuperAdmin.Name)
		if in.SuperAdmin.Name == "" {
			f[fieldPrefix+"superAdmin.name"] = "Enter the Super Admin's name."
		}
		if _, ok := normalizeEmail(in.SuperAdmin.Email); !ok {
			f[fieldPrefix+"superAdmin.email"] = "Enter a valid email address."
		}
	}
	return f
}

// ValidateProvision normalises and validates provisioning input; keys get fieldPrefix.
func ValidateProvision(in *ProvisionInput, fieldPrefix string) map[string]string {
	return in.normalizeAndValidate(fieldPrefix)
}

// ProvisionTx creates an active customer workspace with the system roles and the
// chosen products pinned to their current published version.
func ProvisionTx(ctx context.Context, tx pgx.Tx, actor uuid.UUID, in ProvisionInput, fieldPrefix string) (uuid.UUID, error) {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.workspaces WHERE code = $1)`, in.Code).Scan(&exists); err != nil {
		return uuid.Nil, err
	}
	if exists {
		return uuid.Nil, shared.Validation(map[string]string{fieldPrefix + "code": "Another workspace already uses this code."})
	}
	var wsID uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO crm.workspaces (code, name, status, timezone, locale, currency)
		VALUES ($1, $2, 'active', $3, $4, $5) RETURNING id`,
		in.Code, in.Name, in.Timezone, in.Locale, in.Currency).Scan(&wsID); err != nil {
		if isUniqueViolation(err, "") {
			return uuid.Nil, shared.Validation(map[string]string{fieldPrefix + "code": "Another workspace already uses this code."})
		}
		return uuid.Nil, err
	}
	for _, role := range access.SystemRoles() {
		rules, err := json.Marshal(role.Rules)
		if err != nil {
			return uuid.Nil, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.roles (workspace_id, key, name, is_system, rank, base_rules) VALUES ($1, $2, $3, true, $4, $5)`,
			wsID, role.Key, role.Name, role.Rank, rules); err != nil {
			return uuid.Nil, err
		}
	}
	// Roles form a hierarchy; permissions come from the default permission sets (D-48).
	if err := EnsureRoleTree(ctx, tx, wsID); err != nil {
		return uuid.Nil, err
	}
	// Each app is a project with its own setup (modules, pipeline…): without a product, make one.
	if len(in.ProductIDs) == 0 {
		name := strings.TrimSpace(in.ProductName)
		if name == "" {
			name = in.Name
		}
		pid, err := createProjectSetup(ctx, tx, actor, name, in.Code)
		if err != nil {
			return uuid.Nil, err
		}
		in.ProductIDs = []uuid.UUID{pid}
	}
	seen := map[uuid.UUID]bool{}
	for _, pid := range in.ProductIDs {
		if seen[pid] {
			continue
		}
		seen[pid] = true
		tag, err := tx.Exec(ctx, `
			INSERT INTO crm.workspace_products (workspace_id, product_id, config_version)
			SELECT $1, p.id, p.current_version FROM crm.products p
			WHERE p.id = $2 AND p.status = 'active' AND p.current_version IS NOT NULL`, wsID, pid)
		if err != nil {
			return uuid.Nil, err
		}
		if tag.RowsAffected() == 0 {
			return uuid.Nil, shared.Validation(map[string]string{fieldPrefix + "productIds": "Only published, active products can be assigned."})
		}
	}
	return wsID, shared.WriteAudit(ctx, tx, shared.AuditEvent{
		WorkspaceID: &wsID, ActorID: &actor, Action: "workspace.provisioned", EntityType: "workspace", EntityID: &wsID,
		After: map[string]any{"code": in.Code, "name": in.Name, "products": in.ProductIDs},
	})
}

type ProvisionResult struct {
	Workspace  *WorkspaceDetail `json:"workspace"`
	Invitation *Invitation      `json:"invitation,omitempty"`
}

func (h *Handler) handleProvisionWorkspace(w http.ResponseWriter, r *http.Request) {
	var in ProvisionInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if f := in.normalizeAndValidate(""); len(f) > 0 {
		shared.WriteError(w, r, shared.Validation(f))
		return
	}
	status, resp, err := shared.Idempotent(r.Context(), h.store.Pool, actorID(r), r.Header.Get("Idempotency-Key"), in, func() (int, any, error) {
		var wsID uuid.UUID
		var sent *SentInvitation
		err := h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
			var err error
			if wsID, err = ProvisionTx(r.Context(), tx, actorID(r), in, ""); err != nil {
				return err
			}
			if in.SuperAdmin != nil {
				sent, err = h.InviteTx(r.Context(), tx, actorID(r), wsID, InviteInput{
					Name: in.SuperAdmin.Name, Email: in.SuperAdmin.Email, RoleKey: "SUPER_ADMIN",
				}, "superAdmin.")
				return err
			}
			return nil
		})
		if err != nil {
			return 0, nil, err
		}
		res := ProvisionResult{}
		if sent != nil {
			res.Invitation = h.DeliverInvitation(r.Context(), sent)
		}
		d, err := h.getWorkspace(r.Context(), wsID)
		if err != nil {
			return 0, nil, err
		}
		res.Workspace = d
		return http.StatusCreated, res, nil
	})
	writeResult(w, r, status, resp, err)
}

func (h *Handler) handleListWorkspaces(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	switch status {
	case "active", "suspended", "draft", "provisioning", "failed":
	default:
		status = ""
	}
	list, err := h.listWorkspaces(r.Context(), r.URL.Query().Get("q"), status)
	writeResult(w, r, http.StatusOK, map[string]any{"data": list, "total": len(list)}, err)
}

func (h *Handler) handleGetWorkspace(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id", "workspace_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	d, err := h.getWorkspace(r.Context(), id)
	writeResult(w, r, http.StatusOK, d, err)
}

type workspaceUpdateInput struct {
	Name     *string `json:"name"`
	Timezone *string `json:"timezone"`
	Locale   *string `json:"locale"`
	Currency *string `json:"currency"`
	Status   *string `json:"status"`
}

func (h *Handler) handleUpdateWorkspace(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id", "workspace_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in workspaceUpdateInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var cur ProvisionInput
		var status string
		err := tx.QueryRow(r.Context(), `
			SELECT name, code, timezone, locale, currency, status FROM crm.workspaces WHERE id = $1 AND NOT is_platform FOR UPDATE`, id).
			Scan(&cur.Name, &cur.Code, &cur.Timezone, &cur.Locale, &cur.Currency, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("workspace_not_found")
		}
		if err != nil {
			return err
		}
		before := map[string]any{"name": cur.Name, "timezone": cur.Timezone, "locale": cur.Locale, "currency": cur.Currency, "status": status}
		if in.Name != nil {
			cur.Name = *in.Name
		}
		if in.Timezone != nil {
			cur.Timezone = *in.Timezone
		}
		if in.Locale != nil {
			cur.Locale = *in.Locale
		}
		if in.Currency != nil {
			cur.Currency = *in.Currency
		}
		cur.ProductIDs = []uuid.UUID{uuid.Nil} // not validated here
		f := cur.normalizeAndValidate("")
		delete(f, "productIds")
		delete(f, "code")
		if in.Status != nil {
			if *in.Status != "active" && *in.Status != "suspended" {
				f["status"] = "Status must be active or suspended."
			} else {
				status = *in.Status
			}
		}
		if len(f) > 0 {
			return shared.Validation(f)
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE crm.workspaces SET name = $2, timezone = $3, locale = $4, currency = $5, status = $6, updated_at = now() WHERE id = $1`,
			id, cur.Name, cur.Timezone, cur.Locale, cur.Currency, status); err != nil {
			return err
		}
		if in.Status != nil && status == "suspended" {
			// Suspension ends every session that could reach the workspace right away.
			if _, err := tx.Exec(r.Context(), `
				UPDATE crm.sessions s SET revoked_at = now()
				FROM crm.memberships m JOIN crm.identities i ON i.id = m.identity_id
				WHERE m.workspace_id = $1 AND s.identity_id = m.identity_id AND s.revoked_at IS NULL AND NOT i.is_platform_owner`, id); err != nil {
				return err
			}
		}
		return shared.WriteAudit(r.Context(), tx, auditEvent(r, "workspace.updated", "workspace", &id, &id, before,
			map[string]any{"name": cur.Name, "timezone": cur.Timezone, "locale": cur.Locale, "currency": cur.Currency, "status": status}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	d, err := h.getWorkspace(r.Context(), id)
	writeResult(w, r, http.StatusOK, d, err)
}

func (h *Handler) handleAssignProduct(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id", "workspace_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in struct {
		ProductID uuid.UUID `json:"productId"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var isPlatform bool
		if err := tx.QueryRow(r.Context(), `SELECT is_platform FROM crm.workspaces WHERE id = $1`, id).Scan(&isPlatform); err != nil || isPlatform {
			if err == nil || errors.Is(err, pgx.ErrNoRows) {
				return shared.NotFound("workspace_not_found")
			}
			return err
		}
		var status string
		err := tx.QueryRow(r.Context(), `SELECT status FROM crm.workspace_products WHERE workspace_id = $1 AND product_id = $2`, id, in.ProductID).Scan(&status)
		if err == nil {
			if status == "active" {
				return shared.Validation(map[string]string{"productId": "This product is already assigned."})
			}
			if _, err := tx.Exec(r.Context(), `UPDATE crm.workspace_products SET status = 'active' WHERE workspace_id = $1 AND product_id = $2`, id, in.ProductID); err != nil {
				return err
			}
		} else if errors.Is(err, pgx.ErrNoRows) {
			tag, err := tx.Exec(r.Context(), `
				INSERT INTO crm.workspace_products (workspace_id, product_id, config_version)
				SELECT $1, p.id, p.current_version FROM crm.products p
				WHERE p.id = $2 AND p.status = 'active' AND p.current_version IS NOT NULL`, id, in.ProductID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return shared.Validation(map[string]string{"productId": "Only published, active products can be assigned."})
			}
		} else {
			return err
		}
		pid := in.ProductID
		return shared.WriteAudit(r.Context(), tx, auditEvent(r, "workspace.product_assigned", "product", &pid, &id, nil, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	d, err := h.getWorkspace(r.Context(), id)
	writeResult(w, r, http.StatusOK, d, err)
}

func (h *Handler) handleUpdateWorkspaceProduct(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id", "workspace_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	pid, err := idParam(r, "productId", "product_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in struct {
		Status        *string `json:"status"`
		ConfigVersion *int    `json:"configVersion"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var status string
		var version int
		err := tx.QueryRow(r.Context(), `
			SELECT status, config_version FROM crm.workspace_products WHERE workspace_id = $1 AND product_id = $2 FOR UPDATE`, id, pid).
			Scan(&status, &version)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("assignment_not_found")
		}
		if err != nil {
			return err
		}
		before := map[string]any{"status": status, "configVersion": version}
		if in.Status != nil {
			if *in.Status != "active" && *in.Status != "suspended" {
				return shared.Validation(map[string]string{"status": "Status must be active or suspended."})
			}
			status = *in.Status
		}
		if in.ConfigVersion != nil {
			var ok bool
			if err := tx.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM crm.product_versions WHERE product_id = $1 AND version = $2)`,
				pid, *in.ConfigVersion).Scan(&ok); err != nil {
				return err
			}
			if !ok {
				return shared.Validation(map[string]string{"configVersion": "That version doesn't exist."})
			}
			version = *in.ConfigVersion
		}
		if _, err := tx.Exec(r.Context(), `UPDATE crm.workspace_products SET status = $3, config_version = $4 WHERE workspace_id = $1 AND product_id = $2`,
			id, pid, status, version); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, auditEvent(r, "workspace.product_updated", "product", &pid, &id, before,
			map[string]any{"status": status, "configVersion": version}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	d, err := h.getWorkspace(r.Context(), id)
	writeResult(w, r, http.StatusOK, d, err)
}

// createProjectSetup makes a project's own configuration (a product used only by it),
// published as version 1 with the default modules, so the project works right away.
func createProjectSetup(ctx context.Context, tx pgx.Tx, actor uuid.UUID, name, code string) (uuid.UUID, error) {
	key := strings.ReplaceAll(code, "-", "_")
	base := key
	for i := 2; ; i++ {
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.products WHERE key = $1)`, key).Scan(&taken); err != nil {
			return uuid.Nil, err
		}
		if !taken {
			break
		}
		key = base + "_" + strconv.Itoa(i)
	}
	cfg := DefaultProductConfig()
	raw, _ := json.Marshal(cfg)
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO crm.products (key, name, description, icon, status, current_version, draft_config, created_by)
		VALUES ($1, $2, $3, 'boxes', 'active', 1, $4, $5) RETURNING id`,
		key, name, "Setup of the "+name+" project.", raw, actor).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO crm.product_versions (product_id, version, config, published_by) VALUES ($1, 1, $2, $3)`, id, raw, actor); err != nil {
		return uuid.Nil, err
	}
	return id, nil
}
