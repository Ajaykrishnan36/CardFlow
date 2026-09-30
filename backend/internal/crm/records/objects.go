package records

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/platform"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Objects defined as data (D-45). Standard objects back the product modules
// (opportunities, tasks, calendar, notes, communications, subscriptions, catalog,
// support cases); the owner adds custom objects the same way. A definition becomes a
// record spec over the view crm.obj_<key>, so list / detail / create / edit / layouts /
// custom fields / permissions / the REST API work exactly as for leads and accounts.

type ObjectField struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required,omitempty"`
	Options  []Option `json:"options,omitempty"`
	Lookup   string   `json:"lookup,omitempty"`
	Unique   bool     `json:"unique,omitempty"`
	HelpText string   `json:"helpText,omitempty"`
}

type ObjectBody struct {
	NameLabel   string         `json:"nameLabel"`
	StatusLabel string         `json:"statusLabel,omitempty"`
	Statuses    []StatusOption `json:"statuses"`
	Fields      []ObjectField  `json:"fields"`
}

type ObjectDefinition struct {
	Key         string `json:"key"`
	Module      string `json:"module"`
	Singular    string `json:"singular"`
	Plural      string `json:"plural"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Prefix      string `json:"prefix"`
	ObjectBody
	Standard bool `json:"standard"`
	// WorkspaceID: the product that created it (D-79); empty = platform-wide.
	WorkspaceID   *uuid.UUID `json:"workspaceId,omitempty"`
	WorkspaceName string     `json:"workspaceName,omitempty"`
	Status        string     `json:"status"`
	Records       int        `json:"records"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

// ObjectIcons are the icons an object can use (the web app maps each to a glyph).
var ObjectIcons = []string{
	"box", "handshake", "check-square", "calendar", "sticky-note", "message-square", "repeat", "package", "life-buoy",
	"folder", "star", "flag", "clipboard", "truck", "graduation-cap", "heart", "home", "wrench", "file-text", "ticket",
	"shopping-cart", "banknote", "building", "users", "briefcase", "map-pin", "car", "stethoscope", "book", "zap",
}

var (
	objectKeyRe   = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}$`)
	objFieldKeyRe = regexp.MustCompile(`^[a-z][a-zA-Z0-9]{1,40}$`)
	prefixRe      = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,5}$`)
	reservedKeys  = map[string]bool{"leads": true, "accounts": true, "contacts": true, "users": true, "products": true, "workspaces": true,
		"activities": true, "tickets": true, "meta": true, "objects": true, "support": true, "home": true, "settings": true, "setup": true,
		"dashboard": true, "context": true, "admin": true, "app": true, "businesses": true}
	reservedFields = map[string]bool{"id": true, "name": true, "status": true, "code": true, "ownerId": true, "createdAt": true,
		"createdBy": true, "updatedAt": true, "updatedBy": true, "version": true, "custom": true, "title": true}
	reservedPrefixes = map[string]bool{"L": true, "A": true, "C": true}
	tones            = map[string]bool{"neutral": true, "primary": true, "success": true, "warning": true, "danger": true}
)

// ---- standard objects (seeded once; the owner may edit them afterwards) ----

func st(value, label, tone string) StatusOption { return StatusOption{Option{value, label}, tone} }
func of(key, label, typ string) ObjectField     { return ObjectField{Key: key, Label: label, Type: typ} }
func ofLookup(key, label, target string) ObjectField {
	return ObjectField{Key: key, Label: label, Type: "lookup", Lookup: target}
}
func ofSelect(key, label string, o []Option) ObjectField {
	return ObjectField{Key: key, Label: label, Type: "select", Options: o}
}
func ofReq(f ObjectField) ObjectField { f.Required = true; return f }

func standardObjects() []ObjectDefinition {
	priority := opts("high", "High", "normal", "Normal", "low", "Low")
	return []ObjectDefinition{
		{Key: "opportunities", Module: "opportunities", Singular: "Opportunity", Plural: "Opportunities", Icon: "handshake", Prefix: "OPP",
			Description: "Deals in your pipeline: stage, amount and expected close date.",
			ObjectBody: ObjectBody{NameLabel: "Opportunity name", StatusLabel: "Stage", Statuses: []StatusOption{
				st("prospecting", "Prospecting", "neutral"), st("qualification", "Qualification", "primary"), st("proposal", "Proposal", "primary"),
				st("negotiation", "Negotiation", "warning"), st("closed_won", "Closed won", "success"), st("closed_lost", "Closed lost", "danger")},
				Fields: []ObjectField{
					ofLookup("accountId", "Account", "accounts"), ofLookup("contactId", "Primary contact", "contacts"),
					of("amount", "Amount", "currency"), of("probability", "Probability", "percent"), ofReq(of("closeDate", "Close date", "date")),
					ofSelect("type", "Type", opts("new_business", "New business", "existing_business", "Existing business", "renewal", "Renewal")),
					ofSelect("leadSource", "Lead source", sources), of("nextStep", "Next step", "text"), of("description", "Description", "richtext")}}},
		{Key: "tasks", Module: "tasks", Singular: "Task", Plural: "Tasks", Icon: "check-square", Prefix: "TSK",
			Description: "To-dos with a due date, priority and the record they're about.",
			ObjectBody: ObjectBody{NameLabel: "Subject", StatusLabel: "Status", Statuses: []StatusOption{
				st("not_started", "Not started", "neutral"), st("in_progress", "In progress", "primary"), st("waiting", "Waiting on someone", "warning"),
				st("completed", "Completed", "success"), st("deferred", "Deferred", "neutral")},
				Fields: []ObjectField{
					of("dueDate", "Due date", "date"), ofSelect("priority", "Priority", priority),
					ofLookup("accountId", "Account", "accounts"), ofLookup("contactId", "Contact", "contacts"), ofLookup("leadId", "Lead", "leads"),
					ofLookup("opportunityId", "Opportunity", "opportunities"), of("description", "Comments", "richtext")}}},
		{Key: "events", Module: "calendar", Singular: "Event", Plural: "Calendar events", Icon: "calendar", Prefix: "EVT",
			Description: "Meetings, calls and visits with a time and place.",
			ObjectBody: ObjectBody{NameLabel: "Subject", StatusLabel: "Status", Statuses: []StatusOption{
				st("planned", "Planned", "primary"), st("held", "Held", "success"), st("cancelled", "Cancelled", "danger")},
				Fields: []ObjectField{
					ofReq(of("startsAt", "Starts", "datetime")), of("endsAt", "Ends", "datetime"), of("location", "Location", "text"),
					of("meetingLink", "Meeting link", "url"), ofLookup("accountId", "Account", "accounts"), ofLookup("contactId", "Contact", "contacts"),
					ofLookup("leadId", "Lead", "leads"), of("description", "Description", "richtext")}}},
		{Key: "notes", Module: "notes", Singular: "Note", Plural: "Notes", Icon: "sticky-note", Prefix: "NTE",
			Description: "Notes attached to an account, contact, lead or opportunity.",
			ObjectBody: ObjectBody{NameLabel: "Title", Statuses: []StatusOption{},
				Fields: []ObjectField{
					ofReq(of("body", "Note", "richtext")), ofLookup("accountId", "Account", "accounts"), ofLookup("contactId", "Contact", "contacts"),
					ofLookup("leadId", "Lead", "leads"), ofLookup("opportunityId", "Opportunity", "opportunities")}}},
		{Key: "communications", Module: "communications", Singular: "Communication", Plural: "Communications", Icon: "message-square", Prefix: "COM",
			Description: "A log of emails, SMS, WhatsApp messages and calls with customers.",
			ObjectBody: ObjectBody{NameLabel: "Subject", StatusLabel: "Status", Statuses: []StatusOption{
				st("logged", "Logged", "neutral"), st("sent", "Sent", "success"), st("received", "Received", "primary"), st("failed", "Failed", "danger")},
				Fields: []ObjectField{
					ofReq(ofSelect("channel", "Channel", opts("email", "Email", "sms", "SMS", "whatsapp", "WhatsApp", "call", "Call", "meeting", "Meeting"))),
					ofSelect("direction", "Direction", opts("outbound", "Outbound", "inbound", "Inbound")), of("occurredAt", "When", "datetime"),
					ofLookup("accountId", "Account", "accounts"), ofLookup("contactId", "Contact", "contacts"), ofLookup("leadId", "Lead", "leads"),
					of("body", "Message", "textarea")}}},
		{Key: "subscriptions", Module: "subscriptions", Singular: "Subscription", Plural: "Subscriptions", Icon: "repeat", Prefix: "SUB",
			Description: "Plans your customers are on: amount, billing period and renewal.",
			ObjectBody: ObjectBody{NameLabel: "Subscription name", StatusLabel: "Status", Statuses: []StatusOption{
				st("trial", "Trial", "primary"), st("active", "Active", "success"), st("past_due", "Past due", "warning"),
				st("cancelled", "Cancelled", "danger"), st("expired", "Expired", "neutral")},
				Fields: []ObjectField{
					ofReq(ofLookup("accountId", "Account", "accounts")), ofLookup("contactId", "Contact", "contacts"), of("plan", "Plan", "text"),
					of("amount", "Amount", "currency"),
					ofSelect("billingPeriod", "Billing period", opts("monthly", "Monthly", "quarterly", "Quarterly", "yearly", "Yearly", "one_time", "One-time")),
					of("startDate", "Start date", "date"), of("endDate", "End date", "date"), of("autoRenew", "Renews automatically", "boolean"),
					of("description", "Notes", "textarea")}}},
		{Key: "catalog_items", Module: "catalog", Singular: "Catalog item", Plural: "Catalog", Icon: "package", Prefix: "ITM",
			Description: "The products and services you sell, with prices.",
			ObjectBody: ObjectBody{NameLabel: "Item name", StatusLabel: "Status", Statuses: []StatusOption{
				st("active", "Active", "success"), st("inactive", "Inactive", "neutral")},
				Fields: []ObjectField{
					of("sku", "SKU", "text"), of("unitPrice", "Unit price", "currency"), of("unit", "Unit", "text"), of("category", "Category", "text"),
					of("taxRate", "Tax rate", "percent"), of("description", "Description", "textarea")}}},
		{Key: "cases", Module: "tickets", Singular: "Case", Plural: "Cases", Icon: "life-buoy", Prefix: "CS",
			Description: "Customer support requests from first contact to resolution.",
			ObjectBody: ObjectBody{NameLabel: "Subject", StatusLabel: "Status", Statuses: []StatusOption{
				st("new", "New", "primary"), st("working", "Working", "warning"), st("escalated", "Escalated", "danger"),
				st("resolved", "Resolved", "success"), st("closed", "Closed", "neutral")},
				Fields: []ObjectField{
					ofSelect("priority", "Priority", opts("high", "High", "medium", "Medium", "low", "Low")),
					ofSelect("origin", "Origin", opts("email", "Email", "phone", "Phone", "web", "Web", "chat", "Chat", "app", "App")),
					ofLookup("accountId", "Account", "accounts"), ofLookup("contactId", "Contact", "contacts"),
					of("description", "Description", "richtext"), of("resolution", "Resolution", "richtext")}}},
	}
}

// ---- registry ----

var registry = struct {
	sync.RWMutex
	defs  map[string]*ObjectDefinition
	specs map[string]*objectSpec
}{defs: map[string]*ObjectDefinition{}, specs: map[string]*objectSpec{}}

// specFor is the record spec of any object: built-in or defined as data (active only).
func specFor(key string) *objectSpec {
	if s := specs[key]; s != nil {
		return s
	}
	registry.RLock()
	defer registry.RUnlock()
	return registry.specs[key]
}

// dynamicSpecs lists the active objects defined as data, in module-catalog order.
func dynamicSpecs() []*objectSpec {
	registry.RLock()
	defer registry.RUnlock()
	out := make([]*objectSpec, 0, len(registry.specs))
	for _, s := range registry.specs {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].order < out[j].order || (out[i].order == out[j].order && out[i].Plural < out[j].Plural)
	})
	return out
}

func definitionFor(key string) *ObjectDefinition {
	registry.RLock()
	defer registry.RUnlock()
	return registry.defs[key]
}

func moduleOrder(module string) int {
	for i, m := range platform.ModuleCatalog {
		if m.Key == module {
			return i
		}
	}
	return 1000
}

func buildSpec(d *ObjectDefinition) *objectSpec {
	name := req(text("name", d.NameLabel, "name"))
	if name.Label == "" {
		name.Label = "Name"
	}
	fields := []Field{name}
	hasStatus := len(d.Statuses) > 0
	if hasStatus {
		label := d.StatusLabel
		if label == "" {
			label = "Status"
		}
		fields = append(fields, sel("status", label, "status", statusOptions(d.Statuses)))
	}
	search := []string{"t.name", "t.code"}
	var main, long, highlights, listCols []string
	main = append(main, "name")
	if hasStatus {
		main = append(main, "status")
		highlights = append(highlights, "status")
		listCols = append(listCols, "status")
	}
	for _, f := range d.Fields {
		fields = append(fields, Field{Key: f.Key, Label: f.Label, Type: f.Type, Required: f.Required, Options: f.Options, Lookup: f.Lookup, Unique: f.Unique, HelpText: f.HelpText})
		switch f.Type {
		case "text", "email", "phone":
			search = append(search, "t.custom->>'"+f.Key+"'")
		}
		if f.Type == "textarea" || f.Type == "richtext" {
			long = append(long, f.Key)
			continue
		}
		main = append(main, f.Key)
		if len(highlights) < 5 && f.Type != "multiselect" {
			highlights = append(highlights, f.Key)
		}
		if len(listCols) < 5 {
			listCols = append(listCols, f.Key)
		}
	}
	highlights = append(highlights, "ownerId")
	listCols = append(listCols, "ownerId", "createdAt")
	sections := []Section{{ID: "main", Title: d.Singular + " information", Columns: 2, Fields: main}}
	if len(long) > 0 {
		sections = append(sections, Section{ID: "details", Title: "Details", Columns: 1, Fields: long})
	}
	sections = append(sections, systemSection)
	s := &objectSpec{
		Key: d.Key, Table: "obj_" + d.Key, Singular: d.Singular, Plural: d.Plural, Prefix: d.Prefix,
		TitleSQL: "t.name", SearchSQL: search, Fields: withSystem(fields...), ListColumns: listCols,
		Layout: Layout{Highlights: highlights, Sections: sections}, Icon: d.Icon, Custom: true, order: moduleOrder(d.Module),
	}
	if hasStatus {
		s.StatusField, s.Statuses = "status", d.Statuses
	}
	return s
}

// viewDDL creates the per-object view the record engine reads and writes. The key is
// validated by objectKeyRe (and the table's CHECK), so it is safe in SQL.
func viewDDL(key string) []string {
	return []string{
		`CREATE OR REPLACE VIEW crm.obj_` + key + ` AS SELECT * FROM crm.object_records WHERE object_key = '` + key + `' WITH CASCADED CHECK OPTION`,
		`ALTER VIEW crm.obj_` + key + ` ALTER COLUMN object_key SET DEFAULT '` + key + `'`,
	}
}

// LoadObjects seeds the standard objects (first run), makes sure every object has its
// view, and loads the registry.
func (h *Handler) LoadObjects(ctx context.Context) error {
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		for _, d := range standardObjects() {
			raw, _ := json.Marshal(d.ObjectBody)
			if _, err := tx.Exec(ctx, `
				INSERT INTO crm.object_definitions (key, module, singular, plural, description, icon, prefix, definition, is_standard)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, true) ON CONFLICT (key) DO NOTHING`,
				d.Key, d.Module, d.Singular, d.Plural, d.Description, d.Icon, d.Prefix, raw); err != nil {
				return err
			}
			if err := addMissingStandardOptions(ctx, tx, d); err != nil {
				return err
			}
		}
		rows, err := tx.Query(ctx, `SELECT key FROM crm.object_definitions`)
		if err != nil {
			return err
		}
		var keys []string
		for rows.Next() {
			var k string
			if err := rows.Scan(&k); err != nil {
				rows.Close()
				return err
			}
			keys = append(keys, k)
		}
		rows.Close()
		for _, k := range keys {
			if !objectKeyRe.MatchString(k) {
				continue
			}
			for _, ddl := range viewDDL(k) {
				if _, err := tx.Exec(ctx, ddl); err != nil {
					return fmt.Errorf("object view %s: %w", k, err)
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return h.reloadObjects(ctx)
}

func (h *Handler) listDefinitions(ctx context.Context, q querier) ([]*ObjectDefinition, error) {
	rows, err := q.Query(ctx, `
		SELECT d.key, d.module, d.singular, d.plural, d.description, d.icon, d.prefix, d.definition, d.is_standard, d.status,
		       d.created_at, d.updated_at,
		       (SELECT count(*) FROM crm.object_records r WHERE r.object_key = d.key AND r.deleted_at IS NULL),
		       d.workspace_id, COALESCE(w.name, '')
		FROM crm.object_definitions d LEFT JOIN crm.workspaces w ON w.id = d.workspace_id
		ORDER BY d.is_standard DESC, d.created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*ObjectDefinition{}
	for rows.Next() {
		d := &ObjectDefinition{}
		var raw []byte
		if err := rows.Scan(&d.Key, &d.Module, &d.Singular, &d.Plural, &d.Description, &d.Icon, &d.Prefix, &raw, &d.Standard, &d.Status,
			&d.CreatedAt, &d.UpdatedAt, &d.Records, &d.WorkspaceID, &d.WorkspaceName); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(raw, &d.ObjectBody)
		if d.Statuses == nil {
			d.Statuses = []StatusOption{}
		}
		if d.Fields == nil {
			d.Fields = []ObjectField{}
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// reloadObjects rebuilds the registry, the access catalog and the module catalog.
func (h *Handler) reloadObjects(ctx context.Context) error {
	defs, err := h.listDefinitions(ctx, h.store.Pool)
	if err != nil {
		return err
	}
	byKey := map[string]*ObjectDefinition{}
	specsByKey := map[string]*objectSpec{}
	catalog := []access.CatalogObject{}
	modules := []platform.ModuleInfo{}
	for _, d := range defs {
		byKey[d.Key] = d
		if d.Status != "active" {
			continue
		}
		specsByKey[d.Key] = buildSpec(d)
		catalog = append(catalog, access.CatalogObject{
			Key: d.Key, Label: d.Plural, Module: d.Module, Actions: access.RecordActions(),
			Custom: true, Route: d.Key, Icon: d.Icon, WorkspaceID: d.WorkspaceID,
		})
		// A product's own objects aren't modules the owner can put in other setups.
		if !d.Standard && d.WorkspaceID == nil {
			modules = append(modules, platform.ModuleInfo{Key: d.Module, Label: d.Plural, Description: d.Description, Group: "Custom objects",
				Available: true, Custom: true, Objects: []string{d.Key}})
		}
	}
	registry.Lock()
	registry.defs, registry.specs = byKey, specsByKey
	registry.Unlock()
	// The same order in every product's sidebar: the module catalog's (Salesforce-like) order.
	sort.SliceStable(catalog, func(i, j int) bool { return moduleOrder(catalog[i].Module) < moduleOrder(catalog[j].Module) })
	access.SetCustomObjects(catalog)
	// Standard modules are available when their object is active; custom objects are modules of their own.
	avail := map[string][]string{}
	for _, d := range defs {
		if d.Standard && d.Status == "active" {
			avail[d.Module] = append(avail[d.Module], d.Key)
		}
	}
	platform.SetObjectModules(avail, modules)
	return nil
}

// ---- owner API: /platform/objects ----

type objectInput struct {
	Key         *string         `json:"key"`
	Singular    *string         `json:"singular"`
	Plural      *string         `json:"plural"`
	Description *string         `json:"description"`
	Icon        *string         `json:"icon"`
	Prefix      *string         `json:"prefix"`
	NameLabel   *string         `json:"nameLabel"`
	StatusLabel *string         `json:"statusLabel"`
	Statuses    *[]StatusOption `json:"statuses"`
	Fields      *[]ObjectField  `json:"fields"`
}

// pluralize suggests an English plural: Property → Properties, Class → Classes.
func pluralize(w string) string {
	l := strings.ToLower(w)
	switch {
	case len(l) > 1 && strings.HasSuffix(l, "y") && !strings.ContainsRune("aeiou", rune(l[len(l)-2])):
		return w[:len(w)-1] + "ies"
	case strings.HasSuffix(l, "s") || strings.HasSuffix(l, "x") || strings.HasSuffix(l, "z") || strings.HasSuffix(l, "ch") || strings.HasSuffix(l, "sh"):
		return w + "es"
	}
	return w + "s"
}

func camelKey(label string) string {
	var b strings.Builder
	up := false
	for _, r := range strings.TrimSpace(label) {
		switch {
		case unicode.IsLetter(r) && r < 128 || unicode.IsDigit(r):
			if b.Len() == 0 {
				if unicode.IsDigit(r) {
					b.WriteString("f")
				}
				b.WriteRune(unicode.ToLower(r))
			} else if up {
				b.WriteRune(unicode.ToUpper(r))
			} else {
				b.WriteRune(r)
			}
			up = false
		default:
			up = b.Len() > 0
		}
	}
	k := b.String()
	if len(k) > 40 {
		k = k[:40]
	}
	return k
}

func autoPrefix(plural string, taken map[string]bool) string {
	letters := []rune{}
	for _, r := range strings.ToUpper(plural) {
		if r >= 'A' && r <= 'Z' {
			letters = append(letters, r)
		}
	}
	base := "OBJ"
	if len(letters) >= 3 {
		base = string(letters[:3])
	}
	if !taken[base] && !reservedPrefixes[base] {
		return base
	}
	for i := 2; i < 100; i++ {
		p := fmt.Sprintf("%s%d", base, i)
		if len(p) <= 6 && !taken[p] {
			return p
		}
	}
	return "X" + strings.ToUpper(strings.ReplaceAll(time.Now().Format("150405"), ":", ""))[:5]
}

// validateObject applies in to d (create when d.Key == "") and checks it.
func (h *Handler) validateObject(ctx context.Context, d *ObjectDefinition, in objectInput, creating bool) error {
	fe := map[string]string{}
	trim := func(p *string) string {
		if p == nil {
			return ""
		}
		return strings.TrimSpace(*p)
	}
	if in.Singular != nil || creating {
		d.Singular = trim(in.Singular)
		if d.Singular == "" || len(d.Singular) > 60 {
			fe["singular"] = "Enter a name up to 60 characters, e.g. Project."
		}
	}
	if in.Plural != nil || creating {
		d.Plural = trim(in.Plural)
		if d.Plural == "" && d.Singular != "" {
			d.Plural = pluralize(d.Singular)
		}
		if len(d.Plural) > 60 {
			fe["plural"] = "Use at most 60 characters."
		}
	}
	if in.Description != nil {
		d.Description = trim(in.Description)
		if len(d.Description) > 300 {
			fe["description"] = "Use at most 300 characters."
		}
	}
	if in.Icon != nil || creating {
		d.Icon = trim(in.Icon)
		if d.Icon == "" {
			d.Icon = "box"
		}
		known := false
		for _, i := range ObjectIcons {
			known = known || i == d.Icon
		}
		if !known {
			fe["icon"] = "Pick one of the icons."
		}
	}
	if in.NameLabel != nil || creating {
		d.NameLabel = trim(in.NameLabel)
		if d.NameLabel == "" {
			d.NameLabel = d.Singular + " name"
		}
		if len(d.NameLabel) > 60 {
			fe["nameLabel"] = "Use at most 60 characters."
		}
	}
	if in.StatusLabel != nil {
		d.StatusLabel = trim(in.StatusLabel)
	}
	if in.Statuses != nil {
		seen := map[string]bool{}
		out := []StatusOption{}
		for _, s := range *in.Statuses {
			s.Label = strings.TrimSpace(s.Label)
			s.Value = strings.TrimSpace(s.Value)
			if s.Value == "" {
				s.Value = snakeKey(s.Label)
			}
			if !tones[s.Tone] {
				s.Tone = "neutral"
			}
			if s.Label == "" || s.Value == "" || len(s.Label) > 60 || seen[s.Value] {
				fe["statuses"] = "Every status needs a unique name (up to 60 characters)."
				continue
			}
			seen[s.Value] = true
			out = append(out, s)
		}
		if len(out) > 30 {
			fe["statuses"] = "Use at most 30 statuses."
		}
		d.Statuses = out
	}
	if in.Fields != nil {
		seen := map[string]bool{}
		out := []ObjectField{}
		for i, f := range *in.Fields {
			f.Label = strings.TrimSpace(f.Label)
			if f.Key == "" {
				f.Key = camelKey(f.Label)
			}
			path := fmt.Sprintf("fields.%d", i)
			switch {
			case f.Label == "" || len(f.Label) > 80:
				fe[path] = "Every field needs a label (up to 80 characters)."
			case !objFieldKeyRe.MatchString(f.Key) || reservedFields[f.Key]:
				fe[path] = fmt.Sprintf("%q can't be used as a field name here — try another label.", f.Label)
			case seen[f.Key]:
				fe[path] = fmt.Sprintf("Field %q is listed twice.", f.Label)
			case !customTypes[f.Type] && f.Type != "lookup":
				fe[path] = fmt.Sprintf("Pick a type for %q.", f.Label)
			}
			if f.Unique && !uniqueTypes[f.Type] {
				f.Unique = false
			}
			if isLinkType(f.Type) {
				if f.Lookup != "users" && specFor(f.Lookup) == nil && f.Lookup != d.Key {
					fe[path] = fmt.Sprintf("Pick which object %q links to.", f.Label)
				}
			} else {
				f.Lookup = ""
			}
			if f.Type == "select" || f.Type == "multiselect" {
				o, msg := validateOptions(f.Type, f.Options)
				if msg != "" {
					fe[path] = f.Label + ": " + msg
				}
				f.Options = o
			} else {
				f.Options = nil
			}
			f.HelpText = strings.TrimSpace(f.HelpText)
			if len(f.HelpText) > 300 {
				fe[path] = "Keep help text under 300 characters."
			}
			seen[f.Key] = true
			out = append(out, f)
		}
		if len(out) > 100 {
			fe["fields"] = "Use at most 100 fields."
		}
		d.Fields = out
	}
	if creating {
		defs, err := h.listDefinitions(ctx, h.store.Pool)
		if err != nil {
			return err
		}
		keys, prefixes := map[string]bool{}, map[string]bool{}
		for _, x := range defs {
			keys[x.Key], prefixes[x.Prefix] = true, true
		}
		d.Key = trim(in.Key)
		if d.Key == "" {
			d.Key = snakeKey(d.Plural)
		}
		switch {
		case !objectKeyRe.MatchString(d.Key) || reservedKeys[d.Key]:
			fe["key"] = "Use lowercase letters, numbers and underscores (e.g. projects); some names are reserved."
		case keys[d.Key] || platform.IsModuleKey(d.Key):
			fe["key"] = "An object or module with this API name already exists."
		}
		d.Prefix = strings.ToUpper(trim(in.Prefix))
		if d.Prefix == "" {
			d.Prefix = autoPrefix(d.Plural, prefixes)
		}
		if !prefixRe.MatchString(d.Prefix) || reservedPrefixes[d.Prefix] {
			fe["prefix"] = "Use 2–6 capital letters or digits, starting with a letter (e.g. PRJ)."
		} else if prefixes[d.Prefix] {
			fe["prefix"] = "Another object already uses this record ID prefix."
		}
		d.Module = d.Key
		if d.Statuses == nil {
			d.Statuses = []StatusOption{}
		}
		if d.Fields == nil {
			d.Fields = []ObjectField{}
		}
	}
	if len(fe) > 0 {
		return shared.Validation(fe)
	}
	return nil
}

func (h *Handler) handleListObjects(w http.ResponseWriter, r *http.Request) {
	defs, err := h.listDefinitions(r.Context(), h.store.Pool)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": defs, "icons": ObjectIcons, "lookupTargets": h.lookupTargets()})
}

type lookupTarget struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

func (h *Handler) lookupTargets() []lookupTarget {
	out := []lookupTarget{{"accounts", "Accounts"}, {"contacts", "Contacts"}, {"leads", "Leads"}, {"users", "Users"}}
	for _, s := range dynamicSpecs() {
		out = append(out, lookupTarget{s.Key, s.Plural})
	}
	return out
}

func (h *Handler) isLookupTarget(key string) bool {
	for _, t := range h.lookupTargets() {
		if t.Key == key {
			return true
		}
	}
	return false
}

func (h *Handler) findDefinition(ctx context.Context, key string) (*ObjectDefinition, error) {
	defs, err := h.listDefinitions(ctx, h.store.Pool)
	if err != nil {
		return nil, err
	}
	for _, d := range defs {
		if d.Key == key {
			return d, nil
		}
	}
	return nil, shared.NotFound("object_not_found")
}

func (h *Handler) handleGetObject(w http.ResponseWriter, r *http.Request) {
	d, err := h.findDefinition(r.Context(), chi.URLParam(r, "key"))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, d)
}

func (h *Handler) handleCreateObject(w http.ResponseWriter, r *http.Request) {
	h.createObject(w, r, nil)
}

// createObject: ws set = an object the product creates for itself (D-79).
func (h *Handler) createObject(w http.ResponseWriter, r *http.Request, ws *uuid.UUID) {
	ctx := r.Context()
	var in objectInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	d := &ObjectDefinition{Status: "active"}
	if err := h.validateObject(ctx, d, in, true); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	actor := identity.SessionFrom(ctx).IdentityID
	raw, _ := json.Marshal(d.ObjectBody)
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.object_definitions (key, module, singular, plural, description, icon, prefix, definition, created_by, workspace_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`, d.Key, d.Module, d.Singular, d.Plural, d.Description, d.Icon, d.Prefix, raw, actor, ws); err != nil {
			return err
		}
		for _, ddl := range viewDDL(d.Key) {
			if _, err := tx.Exec(ctx, ddl); err != nil {
				return err
			}
		}
		meta := identity.Meta(r)
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: ws, ActorID: &actor, Action: "object.created", EntityType: "object",
			After: map[string]any{"key": d.Key, "label": d.Plural, "fields": len(d.Fields)}, IP: meta.IP, RequestID: meta.RequestID})
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if err := h.reloadObjects(ctx); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out, err := h.findDefinition(ctx, d.Key)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusCreated, out)
}

func (h *Handler) handleUpdateObject(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d, err := h.findDefinition(ctx, chi.URLParam(r, "key"))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in objectInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if in.Key != nil && strings.TrimSpace(*in.Key) != d.Key || in.Prefix != nil && strings.ToUpper(strings.TrimSpace(*in.Prefix)) != d.Prefix {
		shared.WriteError(w, r, shared.Validation(map[string]string{"key": "The API name and record ID prefix can't change after the object is created."}))
		return
	}
	if err := h.validateObject(ctx, d, in, false); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	actor := identity.SessionFrom(ctx).IdentityID
	raw, _ := json.Marshal(d.ObjectBody)
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `
			UPDATE crm.object_definitions SET singular = $2, plural = $3, description = $4, icon = $5, definition = $6, updated_at = now()
			WHERE key = $1`, d.Key, d.Singular, d.Plural, d.Description, d.Icon, raw); err != nil {
			return err
		}
		meta := identity.Meta(r)
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{ActorID: &actor, Action: "object.updated", EntityType: "object",
			After: map[string]any{"key": d.Key, "label": d.Plural, "fields": len(d.Fields)}, IP: meta.IP, RequestID: meta.RequestID})
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if err := h.reloadObjects(ctx); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out, err := h.findDefinition(ctx, d.Key)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, out)
}

// Archiving hides the object everywhere (its records stay); restoring brings it back.
func (h *Handler) setObjectStatus(status string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		key := chi.URLParam(r, "key")
		actor := identity.SessionFrom(ctx).IdentityID
		err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
			tag, err := tx.Exec(ctx, `UPDATE crm.object_definitions SET status = $2, updated_at = now() WHERE key = $1`, key, status)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return shared.NotFound("object_not_found")
			}
			meta := identity.Meta(r)
			return shared.WriteAudit(ctx, tx, shared.AuditEvent{ActorID: &actor, Action: "object." + map[string]string{"archived": "archived", "active": "restored"}[status],
				EntityType: "object", After: map[string]any{"key": key}, IP: meta.IP, RequestID: meta.RequestID})
		})
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		if err := h.reloadObjects(ctx); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out, err := h.findDefinition(ctx, key)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, out)
	}
}

func (h *Handler) objectRoutes(r chi.Router) {
	r.Get("/platform/objects", h.handleListObjects)
	r.Post("/platform/objects", h.handleCreateObject)
	r.Get("/platform/objects/{key}", h.handleGetObject)
	r.Patch("/platform/objects/{key}", h.handleUpdateObject)
	r.Post("/platform/objects/{key}/archive", h.setObjectStatus("archived"))
	r.Post("/platform/objects/{key}/restore", h.setObjectStatus("active"))
	r.Get("/platform/workspaces/{id}/fields", h.handleOwnerFieldCatalog)
}

// relatedObjects lists, for a record, the records of every enabled object with a lookup
// to it (an account's opportunities, tasks, notes…), one related list per lookup field.
func (h *Handler) relatedObjects(ctx context.Context, ws string, target, id string, enabled func(*objectSpec) bool) ([]RelatedList, error) {
	out := []RelatedList{}
	for _, s := range dynamicSpecs() {
		if !enabled(s) {
			continue
		}
		var lookups []Field
		for _, f := range s.Fields {
			if f.Type == "lookup" && f.Lookup == target && f.column == "" {
				lookups = append(lookups, f)
			}
		}
		for _, f := range lookups {
			label := s.Plural
			if len(lookups) > 1 {
				label = s.Plural + " (" + f.Label + ")"
			}
			status := "''"
			if s.StatusField != "" {
				status = "COALESCE(t.status, '')"
			}
			subtitle := "''"
			for _, g := range s.Fields {
				if g.column == "" && (g.Type == "date" || g.Type == "datetime" || g.Type == "currency") {
					subtitle = "COALESCE(t.custom->>'" + g.Key + "', '')"
					break
				}
			}
			l, err := h.relatedList(ctx, s.Key+":"+f.Key, label, s.Key, `
				SELECT t.id::text, t.code, t.name, `+subtitle+`, `+status+` FROM crm.`+s.Table+` t
				WHERE t.workspace_id = $2 AND t.custom->>'`+f.Key+`' = $1 AND t.deleted_at IS NULL
				ORDER BY t.created_at DESC LIMIT 50`, id, ws)
			if err != nil {
				return nil, err
			}
			out = append(out, l)
		}
	}
	return out, nil
}

// ---- field catalog for the field-access editor (D-46) ----

type catalogField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Type     string `json:"type"`
	Standard bool   `json:"standard"`
	// Locked fields (required ones, the record ID) are always available and can't be restricted.
	Locked bool `json:"locked"`
}

type catalogObject struct {
	Key    string         `json:"key"`    // permission key (lead, account… or the object key)
	Object string         `json:"object"` // record API key (leads, accounts…)
	Label  string         `json:"label"`
	Fields []catalogField `json:"fields"`
}

// fieldCatalog lists the fields of every record object switched on in a workspace.
func (h *Handler) fieldCatalog(ctx context.Context, wsID uuid.UUID) ([]catalogObject, error) {
	modules, err := access.WorkspaceModules(ctx, h.store.Pool, wsID)
	if err != nil {
		return nil, err
	}
	all := []*objectSpec{specs["leads"], specs["accounts"], specs["contacts"]}
	all = append(all, dynamicSpecs()...)
	moduleOf := map[string]string{}
	for _, o := range access.CatalogObjects() {
		moduleOf[o.Key] = o.Module
	}
	out := []catalogObject{}
	for _, s := range all {
		perm := permKey(s.Key)
		if !modules[moduleOf[perm]] {
			continue
		}
		fields, err := allFieldsRaw(ctx, h.store.Pool, wsID, s)
		if err != nil {
			return nil, err
		}
		co := catalogObject{Key: perm, Object: s.Key, Label: s.Plural, Fields: []catalogField{}}
		for _, f := range fields {
			co.Fields = append(co.Fields, catalogField{Key: f.Key, Label: f.Label, Type: f.Type, Standard: f.Standard,
				Locked: f.Required || f.Key == "code"})
		}
		out = append(out, co)
	}
	return out, nil
}

func (h *Handler) handleWorkspaceFieldCatalog(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.Owner && !sc.Eff.HasCapability(access.CapAccessManage) && !sc.Eff.HasCapability(access.CapMembersManage) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	list, err := h.fieldCatalog(r.Context(), sc.WS)
	respond(w, r, http.StatusOK, map[string]any{"data": list}, err)
}

func (h *Handler) handleOwnerFieldCatalog(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("workspace_not_found"))
		return
	}
	list, err := h.fieldCatalog(r.Context(), id)
	respond(w, r, http.StatusOK, map[string]any{"data": list}, err)
}

// addMissingStandardOptions adds pick-list options that a newer release added to a
// standard object (e.g. Case origin "App") to the stored definition. Additive only:
// options the owner renamed or added are kept.
func addMissingStandardOptions(ctx context.Context, tx pgx.Tx, d ObjectDefinition) error {
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT definition FROM crm.object_definitions WHERE key = $1 AND is_standard`, d.Key).Scan(&raw); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	var stored ObjectBody
	if err := json.Unmarshal(raw, &stored); err != nil {
		return nil
	}
	changed := false
	for _, want := range d.Fields {
		if len(want.Options) == 0 && want.Type != "richtext" {
			continue
		}
		for i := range stored.Fields {
			f := &stored.Fields[i]
			if f.Key != want.Key {
				continue
			}
			// Long text that became rich text (D-75): old plain values still show as they are.
			if want.Type == "richtext" && f.Type == "textarea" {
				f.Type = "richtext"
				changed = true
			}
			if f.Type != "select" && f.Type != "multiselect" {
				continue
			}
			have := map[string]bool{}
			for _, o := range f.Options {
				have[o.Value] = true
			}
			for _, o := range want.Options {
				if !have[o.Value] {
					f.Options = append(f.Options, o)
					changed = true
				}
			}
		}
	}
	if !changed {
		return nil
	}
	out, _ := json.Marshal(stored)
	_, err := tx.Exec(ctx, `UPDATE crm.object_definitions SET definition = $2, updated_at = now() WHERE key = $1`, d.Key, out)
	return err
}

// ---- product API: /w/{code}/objects (D-79) ----
// A product's customizers (metadata.manage) create and edit objects that belong to the
// product. They can't touch platform-wide or other products' objects.

func (h *Handler) workspaceObjectRoutes(r chi.Router) {
	r.Get("/objects", h.handleListWorkspaceObjects)
	r.Post("/objects", func(w http.ResponseWriter, r *http.Request) {
		sc, ok := h.requireCustomizer(w, r)
		if !ok {
			return
		}
		ws := sc.WS
		h.createObject(w, r, &ws)
	})
	r.Get("/objects/{key}", h.ownObject(h.handleGetObject))
	r.Patch("/objects/{key}", h.ownObject(h.handleUpdateObject))
	r.Post("/objects/{key}/archive", h.ownObject(h.setObjectStatus("archived")))
	r.Post("/objects/{key}/restore", h.ownObject(h.setObjectStatus("active")))
}

func (h *Handler) requireCustomizer(w http.ResponseWriter, r *http.Request) (*Scope, bool) {
	sc := scopeFrom(r.Context())
	if sc == nil || sc.IsPlatformWS || apiKeyFrom(r.Context()) != nil || !sc.CanCustomize() {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "You need the “Customize page layouts & fields” permission."))
		return nil, false
	}
	return sc, true
}

// ownObject allows only this product's own objects.
func (h *Handler) ownObject(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sc, ok := h.requireCustomizer(w, r)
		if !ok {
			return
		}
		d, err := h.findDefinition(r.Context(), chi.URLParam(r, "key"))
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		if d.WorkspaceID == nil || *d.WorkspaceID != sc.WS {
			shared.WriteError(w, r, shared.Forbidden("not_your_object", "Only objects this product created can be changed here."))
			return
		}
		next(w, r)
	}
}

func (h *Handler) handleListWorkspaceObjects(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireCustomizer(w, r)
	if !ok {
		return
	}
	defs, err := h.listDefinitions(r.Context(), h.store.Pool)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	own := []*ObjectDefinition{}
	for _, d := range defs {
		if d.WorkspaceID != nil && *d.WorkspaceID == sc.WS {
			own = append(own, d)
		}
	}
	targets := []lookupTarget{}
	for _, t := range h.lookupTargets() {
		if s := specFor(t.Key); s == nil || !s.Custom || sc.Enabled(t.Key) {
			targets = append(targets, t)
		}
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": own, "icons": ObjectIcons, "lookupTargets": targets})
}
