package records

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Meta struct {
	Object        string         `json:"object"`
	LabelSingular string         `json:"labelSingular"`
	LabelPlural   string         `json:"labelPlural"`
	CodePrefix    string         `json:"codePrefix"`
	Fields        []Field        `json:"fields"`
	Layout        Layout         `json:"layout"`
	DefaultLayout Layout         `json:"defaultLayout"`
	ListColumns   []string       `json:"listColumns"`
	StatusField   string         `json:"statusField,omitempty"`
	Statuses      []StatusOption `json:"statuses,omitempty"`
	Icon          string         `json:"icon,omitempty"`
	Custom        bool           `json:"custom,omitempty"`
}

var customTypes = map[string]bool{
	"text": true, "textarea": true, "email": true, "phone": true, "url": true, "number": true, "currency": true,
	"percent": true, "date": true, "datetime": true, "select": true, "multiselect": true, "boolean": true,
}

var fieldKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{1,40}$`)

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// customFields returns the published custom fields of an object, in position order.
func customFields(ctx context.Context, q querier, wsID uuid.UUID, object string) ([]Field, error) {
	rows, err := q.Query(ctx, `
		SELECT key, label, type, is_required, options, COALESCE(help_text, '')
		FROM crm.field_definitions
		WHERE workspace_id = $1 AND object_key = $2 AND status = 'published'
		ORDER BY position, created_at`, wsID, object)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Field{}
	for rows.Next() {
		var f Field
		var raw []byte
		if err := rows.Scan(&f.Key, &f.Label, &f.Type, &f.Required, &raw, &f.HelpText); err != nil {
			return nil, err
		}
		var o struct {
			Choices []Option `json:"choices"`
		}
		_ = json.Unmarshal(raw, &o)
		f.Options = o.Choices
		out = append(out, f)
	}
	return out, rows.Err()
}

// allFields returns standard + custom fields.
func allFields(ctx context.Context, q querier, wsID uuid.UUID, spec *objectSpec) ([]Field, error) {
	custom, err := customFields(ctx, q, wsID, spec.Key)
	if err != nil {
		return nil, err
	}
	return append(append([]Field{}, spec.Fields...), custom...), nil
}

func cloneLayout(l Layout) Layout {
	out := Layout{Highlights: append([]string{}, l.Highlights...), Sections: make([]Section, len(l.Sections))}
	for i, s := range l.Sections {
		s.Fields = append([]string{}, s.Fields...)
		out.Sections[i] = s
	}
	return out
}

// sanitizeLayout drops unknown or duplicate keys so a stored layout never references
// a field that was archived since.
func sanitizeLayout(l Layout, fields []Field) Layout {
	known := map[string]bool{}
	for _, f := range fields {
		known[f.Key] = true
	}
	out := Layout{Highlights: []string{}, Sections: []Section{}}
	for _, k := range l.Highlights {
		if known[k] && len(out.Highlights) < 6 && !contains(out.Highlights, k) {
			out.Highlights = append(out.Highlights, k)
		}
	}
	placed := map[string]bool{}
	for _, s := range l.Sections {
		ns := Section{ID: s.ID, Title: s.Title, Columns: s.Columns, Fields: []string{}}
		if ns.Columns != 1 {
			ns.Columns = 2
		}
		for _, k := range s.Fields {
			if known[k] && !placed[k] {
				placed[k] = true
				ns.Fields = append(ns.Fields, k)
			}
		}
		out.Sections = append(out.Sections, ns)
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func loadLayout(ctx context.Context, q querier, wsID uuid.UUID, spec *objectSpec, fields []Field) (Layout, error) {
	var raw []byte
	err := q.QueryRow(ctx, `SELECT definition FROM crm.layouts WHERE workspace_id = $1 AND object_key = $2`, wsID, spec.Key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return sanitizeLayout(cloneLayout(spec.Layout), fields), nil
	}
	if err != nil {
		return Layout{}, err
	}
	var l Layout
	if err := json.Unmarshal(raw, &l); err != nil {
		return sanitizeLayout(cloneLayout(spec.Layout), fields), nil
	}
	return sanitizeLayout(l, fields), nil
}

func (h *Handler) meta(ctx context.Context, wsID uuid.UUID, spec *objectSpec) (*Meta, error) {
	fields, err := allFields(ctx, h.store.Pool, wsID, spec)
	if err != nil {
		return nil, err
	}
	layout, err := loadLayout(ctx, h.store.Pool, wsID, spec, fields)
	if err != nil {
		return nil, err
	}
	return &Meta{
		Object: spec.Key, LabelSingular: spec.Singular, LabelPlural: spec.Plural, CodePrefix: spec.Prefix,
		Fields: fields, Layout: layout, DefaultLayout: sanitizeLayout(cloneLayout(spec.Layout), fields),
		ListColumns: spec.ListColumns, StatusField: spec.StatusField, Statuses: spec.Statuses, Icon: spec.Icon, Custom: spec.Custom,
	}, nil
}

// validateLayout checks an owner-edited layout (PRD LAY-01: layouts only arrange
// fields; they never grant access).
func validateLayout(l Layout, fields []Field) (Layout, error) {
	known := map[string]bool{}
	for _, f := range fields {
		known[f.Key] = true
	}
	fe := map[string]string{}
	if len(l.Sections) > 40 {
		fe["sections"] = "Use at most 40 sections."
	}
	if len(l.Highlights) > 6 {
		fe["highlights"] = "Pick at most 6 highlight fields."
	}
	for _, k := range l.Highlights {
		if !known[k] {
			fe["highlights"] = fmt.Sprintf("Unknown field %q.", k)
		}
	}
	seenIDs := map[string]bool{}
	placed := map[string]bool{}
	for i := range l.Sections {
		s := &l.Sections[i]
		s.Title = strings.TrimSpace(s.Title)
		if s.Title == "" || len(s.Title) > 80 {
			fe["sections"] = "Every section needs a title (up to 80 characters)."
		}
		if s.ID == "" || seenIDs[s.ID] || len(s.ID) > 60 {
			s.ID = "s_" + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
		}
		seenIDs[s.ID] = true
		if s.Columns != 1 && s.Columns != 2 {
			s.Columns = 2
		}
		if s.Fields == nil {
			s.Fields = []string{}
		}
		for _, k := range s.Fields {
			if !known[k] {
				fe["sections"] = fmt.Sprintf("Unknown field %q.", k)
			}
			if placed[k] {
				fe["sections"] = fmt.Sprintf("Field %q is placed twice.", k)
			}
			placed[k] = true
		}
	}
	if l.Highlights == nil {
		l.Highlights = []string{}
	}
	if len(fe) > 0 {
		return l, shared.Validation(fe)
	}
	return l, nil
}

func saveLayout(ctx context.Context, tx pgx.Tx, wsID uuid.UUID, object string, l Layout, actor uuid.UUID) error {
	raw, err := json.Marshal(l)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO crm.layouts (workspace_id, object_key, definition, updated_by) VALUES ($1, $2, $3, $4)
		ON CONFLICT (workspace_id, object_key) DO UPDATE SET definition = EXCLUDED.definition, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		wsID, object, raw, actor)
	return err
}

// snakeKey turns a label into a field key: "Preferred Language" → "preferred_language".
func snakeKey(label string) string {
	var b strings.Builder
	lastUnderscore := true
	for _, r := range strings.ToLower(label) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastUnderscore = false
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '/' || r == '.':
			if !lastUnderscore {
				b.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	k := strings.Trim(b.String(), "_")
	if k != "" && k[0] >= '0' && k[0] <= '9' {
		k = "f_" + k
	}
	if len(k) > 40 {
		k = strings.TrimRight(k[:40], "_")
	}
	return k
}

type fieldInput struct {
	Label     *string   `json:"label"`
	Key       string    `json:"key"`
	Type      string    `json:"type"`
	Required  *bool     `json:"required"`
	Options   *[]Option `json:"options"`
	HelpText  *string   `json:"helpText"`
	SectionID string    `json:"sectionId"`
}

func validateOptions(typ string, options []Option) ([]Option, string) {
	if typ != "select" && typ != "multiselect" {
		return nil, ""
	}
	if len(options) == 0 {
		return nil, "Add at least one option."
	}
	if len(options) > 200 {
		return nil, "Use at most 200 options."
	}
	seen := map[string]bool{}
	out := make([]Option, 0, len(options))
	for _, o := range options {
		o.Label = strings.TrimSpace(o.Label)
		o.Value = strings.TrimSpace(o.Value)
		if o.Value == "" {
			o.Value = snakeKey(o.Label)
		}
		if o.Label == "" || o.Value == "" || len(o.Label) > 80 || len(o.Value) > 80 {
			return nil, "Every option needs a label (up to 80 characters)."
		}
		if seen[o.Value] {
			return nil, fmt.Sprintf("Option %q is listed twice.", o.Label)
		}
		seen[o.Value] = true
		out = append(out, o)
	}
	return out, ""
}

// DefaultLayout returns a copy of an object's default page layout (for connectors that
// add their own section).
func DefaultLayout(object string) Layout {
	if spec := specFor(object); spec != nil {
		return cloneLayout(spec.Layout)
	}
	return Layout{Highlights: []string{}, Sections: []Section{}}
}
