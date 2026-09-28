package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---- Product configuration (versioned; PRD §2.3, §13.2 product_versions.config) ----

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type UserType struct {
	Key          string   `json:"key"`
	Label        string   `json:"label"`
	Description  string   `json:"description,omitempty"`
	AllowedRoles []string `json:"allowedRoles"`
}

type RoleTemplate struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Enabled bool   `json:"enabled"`
}

type Stage struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Probability int    `json:"probability"`
}

type ProductConfig struct {
	AccentColor    string         `json:"accentColor"`
	Modules        []string       `json:"modules"`
	UserTypes      []UserType     `json:"userTypes"`
	Roles          []RoleTemplate `json:"roles"`
	LeadStatuses   []Option       `json:"leadStatuses"`
	PipelineStages []Stage        `json:"pipelineStages"`
	Conversion     struct {
		CreateContact     bool `json:"createContact"`
		CreateOpportunity bool `json:"createOpportunity"`
		RequireQualified  bool `json:"requireQualified"`
	} `json:"conversion"`
	LoginMethods struct {
		Password bool `json:"password"`
		OTP      bool `json:"otp"`
		Google   bool `json:"google"`
		LinkedIn bool `json:"linkedin"`
	} `json:"loginMethods"`
	SelfRegistration bool `json:"selfRegistration"`
	Integrations     struct {
		APIAccess bool `json:"apiAccess"`
		Webhooks  bool `json:"webhooks"`
	} `json:"integrations"`
}

type ModuleInfo struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Group       string `json:"group"`
}

// ModuleCatalog lists the shared modules a product can switch on. Products are
// metadata over these modules — never product-specific code (PRD §14.3, §17.1).
var ModuleCatalog = []ModuleInfo{
	{"leads", "Leads", "Capture, qualify, score and convert prospects.", "Sales"},
	{"accounts", "Accounts", "Organisations and individuals you do business with.", "Sales"},
	{"contacts", "Contacts", "People linked to one or more accounts.", "Sales"},
	{"opportunities", "Opportunities", "Pipelines, stages, amounts and forecasts.", "Sales"},
	{"tasks", "Tasks", "Assignable to-dos with due dates and reminders.", "Productivity"},
	{"calendar", "Calendar", "Meetings and events linked to records.", "Productivity"},
	{"notes", "Notes", "Rich-text notes with mentions on any record.", "Productivity"},
	{"files", "Files", "Virus-scanned attachments with signed links.", "Productivity"},
	{"communications", "Communications", "Email, SMS and WhatsApp logs and templates.", "Engagement"},
	{"forms", "Forms", "Multi-step public and internal forms.", "Engagement"},
	{"submissions", "Submissions", "Form submissions with review and approvals.", "Engagement"},
	{"tickets", "Support", "Tickets, SLAs and a customer help portal.", "Service"},
	{"subscriptions", "Subscriptions", "Plans, payments and entitlements for end users.", "Commerce"},
	{"catalog", "Catalog & quotes", "Sellable items, price books and quotes.", "Commerce"},
	{"workflows", "Workflows", "Triggers, conditions and automated actions.", "Automation"},
	{"reports", "Reports", "Dashboards, charts and scheduled exports.", "Insights"},
}

var systemRoleKeys = []string{"SUPER_ADMIN", "ADMIN", "STAFF", "END_USER"}

var systemRoleLabels = map[string]string{
	"SUPER_ADMIN": "Super Admin", "ADMIN": "Admin", "STAFF": "Staff", "END_USER": "End user",
}

// DefaultProductConfig is the starting draft for a new product.
func DefaultProductConfig() ProductConfig {
	c := ProductConfig{
		AccentColor: "#4F46E5",
		Modules:     []string{"leads", "accounts", "contacts", "opportunities", "tasks", "notes", "files", "tickets", "reports"},
		UserTypes: []UserType{
			{Key: "customer", Label: "Customer", Description: "People who buy from the workspace.", AllowedRoles: []string{"END_USER"}},
			{Key: "team_member", Label: "Team member", Description: "Staff who work leads and accounts.", AllowedRoles: []string{"ADMIN", "STAFF"}},
		},
		LeadStatuses: []Option{
			{"new", "New"}, {"working", "Working"}, {"qualified", "Qualified"}, {"converted", "Converted"}, {"lost", "Closed lost"},
		},
		PipelineStages: []Stage{
			{"prospecting", "Prospecting", 10}, {"qualification", "Qualification", 25}, {"proposal", "Proposal", 50},
			{"negotiation", "Negotiation", 75}, {"closed_won", "Closed won", 100}, {"closed_lost", "Closed lost", 0},
		},
	}
	for _, k := range systemRoleKeys {
		c.Roles = append(c.Roles, RoleTemplate{Key: k, Label: systemRoleLabels[k], Enabled: true})
	}
	c.Conversion.CreateContact = true
	c.Conversion.CreateOpportunity = false
	c.LoginMethods.Password = true
	return c
}

// normalize fills gaps (older drafts, partial client payloads) so every stored config
// has the full shape, and enforces invariants the UI shows as locked.
func (c *ProductConfig) normalize() {
	d := DefaultProductConfig()
	if c.AccentColor == "" {
		c.AccentColor = d.AccentColor
	}
	if c.Modules == nil {
		c.Modules = []string{}
	}
	if c.UserTypes == nil {
		c.UserTypes = []UserType{}
	}
	for i := range c.UserTypes {
		if c.UserTypes[i].AllowedRoles == nil {
			c.UserTypes[i].AllowedRoles = []string{}
		}
	}
	// Roles: always the four system roles in order; Super Admin can't be disabled.
	byKey := map[string]RoleTemplate{}
	for _, r := range c.Roles {
		byKey[r.Key] = r
	}
	c.Roles = c.Roles[:0]
	for _, k := range systemRoleKeys {
		r, ok := byKey[k]
		if !ok {
			r = RoleTemplate{Key: k, Label: systemRoleLabels[k], Enabled: true}
		}
		if strings.TrimSpace(r.Label) == "" {
			r.Label = systemRoleLabels[k]
		}
		r.Key = k
		if k == "SUPER_ADMIN" {
			r.Enabled = true
		}
		c.Roles = append(c.Roles, r)
	}
	if len(c.LeadStatuses) == 0 {
		c.LeadStatuses = d.LeadStatuses
	}
	if c.PipelineStages == nil {
		c.PipelineStages = []Stage{}
	}
	c.LoginMethods.Password = true
}

var (
	productKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{2,40}$`)
	optionKeyRe  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,40}$`)
	hexColorRe   = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

// Validate returns publish-blocking problems keyed by wizard section.
func (c ProductConfig) Validate() map[string]string {
	f := map[string]string{}
	known := map[string]bool{}
	for _, m := range ModuleCatalog {
		known[m.Key] = true
	}
	if len(c.Modules) == 0 {
		f["modules"] = "Enable at least one module."
	}
	seen := map[string]bool{}
	for _, m := range c.Modules {
		if !known[m] {
			f["modules"] = fmt.Sprintf("Unknown module %q.", m)
		}
		if seen[m] {
			f["modules"] = "Each module can only be enabled once."
		}
		seen[m] = true
	}
	if !hexColorRe.MatchString(c.AccentColor) {
		f["accentColor"] = "Pick a colour."
	}
	seenUT := map[string]bool{}
	for _, ut := range c.UserTypes {
		switch {
		case !optionKeyRe.MatchString(ut.Key):
			f["userTypes"] = fmt.Sprintf("User type key %q must be lower-case letters, digits or underscores.", ut.Key)
		case strings.TrimSpace(ut.Label) == "":
			f["userTypes"] = "Every user type needs a label."
		case seenUT[ut.Key]:
			f["userTypes"] = fmt.Sprintf("User type key %q is used twice.", ut.Key)
		case len(ut.AllowedRoles) == 0:
			f["userTypes"] = fmt.Sprintf("User type %q needs at least one allowed role.", ut.Label)
		}
		seenUT[ut.Key] = true
		for _, r := range ut.AllowedRoles {
			if systemRoleLabels[r] == "" {
				f["userTypes"] = fmt.Sprintf("Unknown role %q.", r)
			}
		}
	}
	for _, r := range c.Roles {
		if strings.TrimSpace(r.Label) == "" || len(r.Label) > 40 {
			f["roles"] = "Role labels must be 1–40 characters."
		}
	}
	if len(c.LeadStatuses) < 2 {
		f["leadStatuses"] = "Define at least two lead statuses."
	}
	seenLS := map[string]bool{}
	for _, s := range c.LeadStatuses {
		if !optionKeyRe.MatchString(s.Value) || strings.TrimSpace(s.Label) == "" || seenLS[s.Value] {
			f["leadStatuses"] = "Each lead status needs a unique lower-case key and a label."
		}
		seenLS[s.Value] = true
	}
	if seen["opportunities"] && len(c.PipelineStages) < 2 {
		f["pipelineStages"] = "Opportunities need at least two pipeline stages."
	}
	seenST := map[string]bool{}
	for _, s := range c.PipelineStages {
		if !optionKeyRe.MatchString(s.Key) || strings.TrimSpace(s.Label) == "" || seenST[s.Key] {
			f["pipelineStages"] = "Each stage needs a unique lower-case key and a label."
		}
		if s.Probability < 0 || s.Probability > 100 {
			f["pipelineStages"] = "Stage probability must be between 0 and 100."
		}
		seenST[s.Key] = true
	}
	if c.Conversion.CreateOpportunity && !seen["opportunities"] {
		f["pipelineStages"] = "Turn on the Opportunities module to create opportunities on conversion."
	}
	if (c.Conversion.CreateContact || c.Conversion.RequireQualified) && !seen["leads"] {
		f["modules"] = "Lead conversion needs the Leads module."
	}
	return f
}

func decodeConfig(raw []byte) ProductConfig {
	var c ProductConfig
	_ = json.Unmarshal(raw, &c)
	c.normalize()
	return c
}

// ---- DTOs ----

type ProductSummary struct {
	ID                    uuid.UUID `json:"id"`
	Key                   string    `json:"key"`
	Name                  string    `json:"name"`
	Description           string    `json:"description,omitempty"`
	Icon                  string    `json:"icon,omitempty"`
	Status                string    `json:"status"`
	CurrentVersion        *int      `json:"currentVersion,omitempty"`
	Workspaces            int       `json:"workspaces"`
	HasUnpublishedChanges bool      `json:"hasUnpublishedChanges"`
	CreatedAt             time.Time `json:"createdAt"`
	UpdatedAt             time.Time `json:"updatedAt"`
}

type ProductVersion struct {
	Version     int       `json:"version"`
	PublishedAt time.Time `json:"publishedAt"`
	PublishedBy string    `json:"publishedBy,omitempty"`
	Workspaces  int       `json:"workspaces"`
}

type AssignedWorkspace struct {
	ID               uuid.UUID `json:"id"`
	Code             string    `json:"code"`
	Name             string    `json:"name"`
	Status           string    `json:"status"`
	ConfigVersion    int       `json:"configVersion"`
	AssignmentStatus string    `json:"assignmentStatus"`
}

type ProductDetail struct {
	ProductSummary
	DraftConfig        ProductConfig       `json:"draftConfig"`
	PublishedConfig    *ProductConfig      `json:"publishedConfig,omitempty"`
	Versions           []ProductVersion    `json:"versions"`
	AssignedWorkspaces []AssignedWorkspace `json:"assignedWorkspaces"`
	ModuleCatalog      []ModuleInfo        `json:"moduleCatalog"`
}

const productSummarySelect = `
	SELECT p.id, p.key, p.name, COALESCE(p.description, ''), COALESCE(p.icon, ''), p.status, p.current_version,
	       p.created_at, p.updated_at,
	       (SELECT count(*) FROM crm.workspace_products wp JOIN crm.workspaces w ON w.id = wp.workspace_id
	         WHERE wp.product_id = p.id AND NOT w.is_platform),
	       (p.current_version IS NOT NULL AND p.draft_config IS DISTINCT FROM
	         (SELECT v.config FROM crm.product_versions v WHERE v.product_id = p.id AND v.version = p.current_version))
	FROM crm.products p`

func scanProductSummary(row pgx.Row) (ProductSummary, error) {
	var p ProductSummary
	err := row.Scan(&p.ID, &p.Key, &p.Name, &p.Description, &p.Icon, &p.Status, &p.CurrentVersion,
		&p.CreatedAt, &p.UpdatedAt, &p.Workspaces, &p.HasUnpublishedChanges)
	return p, err
}

func (h *Handler) listProducts(ctx context.Context, q, status string) ([]ProductSummary, error) {
	rows, err := h.store.Pool.Query(ctx, productSummarySelect+`
		WHERE ($1 = '' OR p.name ILIKE $1 OR p.key ILIKE $1 OR p.description ILIKE $1)
		  AND ($2 = '' OR p.status = $2)
		ORDER BY (p.status = 'archived'), p.updated_at DESC`, likePattern(q), status)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProductSummary{}
	for rows.Next() {
		p, err := scanProductSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (h *Handler) getProduct(ctx context.Context, id uuid.UUID) (*ProductDetail, error) {
	sum, err := scanProductSummary(h.store.Pool.QueryRow(ctx, productSummarySelect+` WHERE p.id = $1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NotFound("product_not_found")
	}
	if err != nil {
		return nil, err
	}
	d := &ProductDetail{ProductSummary: sum, Versions: []ProductVersion{}, AssignedWorkspaces: []AssignedWorkspace{}, ModuleCatalog: ModuleCatalog}

	var draft, published []byte
	if err := h.store.Pool.QueryRow(ctx, `
		SELECT p.draft_config, v.config FROM crm.products p
		LEFT JOIN crm.product_versions v ON v.product_id = p.id AND v.version = p.current_version
		WHERE p.id = $1`, id).Scan(&draft, &published); err != nil {
		return nil, err
	}
	d.DraftConfig = decodeConfig(draft)
	if published != nil {
		pc := decodeConfig(published)
		d.PublishedConfig = &pc
	}

	rows, err := h.store.Pool.Query(ctx, `
		SELECT v.version, v.published_at, COALESCE(i.display_name, ''),
		       (SELECT count(*) FROM crm.workspace_products wp JOIN crm.workspaces w ON w.id = wp.workspace_id
		         WHERE wp.product_id = v.product_id AND wp.config_version = v.version AND NOT w.is_platform)
		FROM crm.product_versions v LEFT JOIN crm.identities i ON i.id = v.published_by
		WHERE v.product_id = $1 ORDER BY v.version DESC`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var v ProductVersion
		if err := rows.Scan(&v.Version, &v.PublishedAt, &v.PublishedBy, &v.Workspaces); err != nil {
			rows.Close()
			return nil, err
		}
		d.Versions = append(d.Versions, v)
	}
	rows.Close()

	rows, err = h.store.Pool.Query(ctx, `
		SELECT w.id, w.code, w.name, w.status, wp.config_version, wp.status
		FROM crm.workspace_products wp JOIN crm.workspaces w ON w.id = wp.workspace_id
		WHERE wp.product_id = $1 AND NOT w.is_platform ORDER BY w.name`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var a AssignedWorkspace
		if err := rows.Scan(&a.ID, &a.Code, &a.Name, &a.Status, &a.ConfigVersion, &a.AssignmentStatus); err != nil {
			return nil, err
		}
		d.AssignedWorkspaces = append(d.AssignedWorkspaces, a)
	}
	return d, rows.Err()
}

// ---- Handlers ----

func (h *Handler) handleListProducts(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	if status != "" && status != "draft" && status != "active" && status != "archived" {
		status = ""
	}
	list, err := h.listProducts(r.Context(), r.URL.Query().Get("q"), status)
	writeResult(w, r, http.StatusOK, map[string]any{"data": list, "total": len(list)}, err)
}

func (h *Handler) handleGetProduct(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id", "product_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	d, err := h.getProduct(r.Context(), id)
	writeResult(w, r, http.StatusOK, d, err)
}

type productCreateInput struct {
	Name        string `json:"name"`
	Key         string `json:"key"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
}

func validateProductBasics(name, description, icon string, fields map[string]string) {
	if n := strings.TrimSpace(name); n == "" {
		fields["name"] = "Enter a product name."
	} else if len(n) > 80 {
		fields["name"] = "Use at most 80 characters."
	}
	if len(description) > 1000 {
		fields["description"] = "Use at most 1000 characters."
	}
	if len(icon) > 40 {
		fields["icon"] = "Pick an icon from the list."
	}
}

func (h *Handler) handleCreateProduct(w http.ResponseWriter, r *http.Request) {
	var in productCreateInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	in.Key = strings.ToLower(strings.TrimSpace(in.Key))
	fields := map[string]string{}
	validateProductBasics(in.Name, in.Description, in.Icon, fields)
	if !productKeyRe.MatchString(in.Key) {
		fields["key"] = "3–41 characters: start with a letter, then lower-case letters, digits or underscores."
	}
	if len(fields) > 0 {
		shared.WriteError(w, r, shared.Validation(fields))
		return
	}
	icon := strings.TrimSpace(in.Icon)
	if icon == "" {
		icon = "boxes"
	}
	cfg, _ := json.Marshal(DefaultProductConfig())
	var id uuid.UUID
	err := h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		if err := tx.QueryRow(r.Context(), `
			INSERT INTO crm.products (key, name, description, icon, status, draft_config, created_by)
			VALUES ($1, $2, NULLIF($3, ''), $4, 'draft', $5, $6) RETURNING id`,
			in.Key, strings.TrimSpace(in.Name), strings.TrimSpace(in.Description), icon, cfg, actorID(r)).Scan(&id); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, auditEvent(r, "product.created", "product", &id, nil, nil,
			map[string]any{"key": in.Key, "name": in.Name}))
	})
	if isUniqueViolation(err, "") {
		shared.WriteError(w, r, shared.Validation(map[string]string{"key": "Another product already uses this key."}))
		return
	}
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	d, err := h.getProduct(r.Context(), id)
	writeResult(w, r, http.StatusCreated, d, err)
}

type productUpdateInput struct {
	Name        *string        `json:"name"`
	Description *string        `json:"description"`
	Icon        *string        `json:"icon"`
	DraftConfig *ProductConfig `json:"draftConfig"`
}

func (h *Handler) handleUpdateProduct(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id", "product_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in productUpdateInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	in.Name, in.Description, in.Icon = trimPtr(in.Name), trimPtr(in.Description), trimPtr(in.Icon)

	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var name, description, icon, status string
		var draft []byte
		err := tx.QueryRow(r.Context(), `
			SELECT name, COALESCE(description, ''), COALESCE(icon, ''), status, draft_config
			FROM crm.products WHERE id = $1 FOR UPDATE`, id).Scan(&name, &description, &icon, &status, &draft)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("product_not_found")
		}
		if err != nil {
			return err
		}
		if status == "archived" {
			return shared.NewError(http.StatusConflict, "product_archived", "Restore this product before editing it.")
		}
		if in.Name != nil {
			name = *in.Name
		}
		if in.Description != nil {
			description = *in.Description
		}
		if in.Icon != nil {
			icon = *in.Icon
		}
		fields := map[string]string{}
		validateProductBasics(name, description, icon, fields)
		if len(fields) > 0 {
			return shared.Validation(fields)
		}
		if in.DraftConfig != nil {
			in.DraftConfig.normalize()
			if len(in.DraftConfig.UserTypes) > 50 || len(in.DraftConfig.LeadStatuses) > 30 || len(in.DraftConfig.PipelineStages) > 30 {
				return shared.Validation(map[string]string{"draftConfig": "Too many entries."})
			}
			if draft, err = json.Marshal(in.DraftConfig); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE crm.products SET name = $2, description = NULLIF($3, ''), icon = NULLIF($4, ''), draft_config = $5, updated_at = now()
			WHERE id = $1`, id, name, description, icon, draft); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, auditEvent(r, "product.draft_saved", "product", &id, nil, nil,
			map[string]any{"name": name, "configChanged": in.DraftConfig != nil}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	d, err := h.getProduct(r.Context(), id)
	writeResult(w, r, http.StatusOK, d, err)
}

func (h *Handler) handlePublishProduct(w http.ResponseWriter, r *http.Request) {
	id, err := idParam(r, "id", "product_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var name, status string
		var current *int
		var draft []byte
		err := tx.QueryRow(r.Context(), `SELECT name, status, current_version, draft_config FROM crm.products WHERE id = $1 FOR UPDATE`, id).
			Scan(&name, &status, &current, &draft)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("product_not_found")
		}
		if err != nil {
			return err
		}
		if status == "archived" {
			return shared.NewError(http.StatusConflict, "product_archived", "Restore this product before publishing it.")
		}
		cfg := decodeConfig(draft)
		fields := cfg.Validate()
		if strings.TrimSpace(name) == "" {
			fields["name"] = "Enter a product name."
		}
		if len(fields) > 0 {
			e := shared.Validation(fields)
			e.Message = "Fix these before publishing."
			return e
		}
		normalized, _ := json.Marshal(cfg)
		if current != nil {
			var same bool
			if err := tx.QueryRow(r.Context(), `SELECT config = $3::jsonb FROM crm.product_versions WHERE product_id = $1 AND version = $2`,
				id, *current, normalized).Scan(&same); err != nil {
				return err
			}
			if same && status == "active" {
				return shared.NewError(http.StatusConflict, "nothing_to_publish", "There are no changes since the last published version.")
			}
		}
		next := 1
		if current != nil {
			next = *current + 1
		}
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO crm.product_versions (product_id, version, config, published_by) VALUES ($1, $2, $3, $4)`,
			id, next, normalized, actorID(r)); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE crm.products SET status = 'active', current_version = $2, draft_config = $3, updated_at = now() WHERE id = $1`,
			id, next, normalized); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, auditEvent(r, "product.published", "product", &id, nil, nil, map[string]any{"version": next}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	d, err := h.getProduct(r.Context(), id)
	writeResult(w, r, http.StatusOK, d, err)
}

func (h *Handler) setProductStatus(w http.ResponseWriter, r *http.Request, archive bool) {
	id, err := idParam(r, "id", "product_not_found")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var status string
		var current *int
		err := tx.QueryRow(r.Context(), `SELECT status, current_version FROM crm.products WHERE id = $1 FOR UPDATE`, id).Scan(&status, &current)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("product_not_found")
		}
		if err != nil {
			return err
		}
		next := "archived"
		action := "product.archived"
		if !archive {
			if status != "archived" {
				return nil
			}
			next, action = "draft", "product.restored"
			if current != nil {
				next = "active"
			}
		} else if status == "archived" {
			return nil
		}
		if _, err := tx.Exec(r.Context(), `UPDATE crm.products SET status = $2, updated_at = now() WHERE id = $1`, id, next); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, auditEvent(r, action, "product", &id, nil,
			map[string]any{"status": status}, map[string]any{"status": next}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	d, err := h.getProduct(r.Context(), id)
	writeResult(w, r, http.StatusOK, d, err)
}

func (h *Handler) handleArchiveProduct(w http.ResponseWriter, r *http.Request) { h.setProductStatus(w, r, true) }
func (h *Handler) handleRestoreProduct(w http.ResponseWriter, r *http.Request) { h.setProductStatus(w, r, false) }
