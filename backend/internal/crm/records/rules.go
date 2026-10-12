package records

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Validation rules and default values (D-141). Both are set per object by whoever may
// customise the business, and both apply however a record is saved: a form, an import,
// the API or a lead conversion.
//
// A rule is a set of conditions — the same ones lists and workflows use — plus a
// message. A record that matches the conditions after a save is not saved.

const maxRulesPerObject = 30

type ValidationRule struct {
	ID        uuid.UUID   `json:"id"`
	Name      string      `json:"name"`
	Message   string      `json:"message"`
	Field     string      `json:"field,omitempty"` // where the message shows; "" = top of the form
	Filter    *FilterNode `json:"filter"`
	Active    bool        `json:"active"`
	UpdatedAt time.Time   `json:"updatedAt"`
}

type ruleInput struct {
	Name    *string     `json:"name"`
	Message *string     `json:"message"`
	Field   *string     `json:"field"`
	Filter  *FilterNode `json:"filter"`
	Active  *bool       `json:"active"`
}

type rulesPage struct {
	Rules    []ValidationRule `json:"rules"`
	Defaults map[string]any   `json:"defaults"`
}

func loadRules(ctx context.Context, q querier, ws uuid.UUID, object string, activeOnly bool) ([]ValidationRule, error) {
	sql := `SELECT id, name, message, COALESCE(field_key, ''), filter, is_active, updated_at
		FROM crm.validation_rules WHERE workspace_id = $1 AND object_key = $2`
	if activeOnly {
		sql += ` AND is_active`
	}
	rows, err := q.Query(ctx, sql+` ORDER BY created_at`, ws, object)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ValidationRule{}
	for rows.Next() {
		var v ValidationRule
		var raw []byte
		if err := rows.Scan(&v.ID, &v.Name, &v.Message, &v.Field, &raw, &v.Active, &v.UpdatedAt); err != nil {
			return nil, err
		}
		var n FilterNode
		if json.Unmarshal(raw, &n) == nil {
			v.Filter = &n
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// checkRules refuses a save that leaves the record matching an active rule. It runs
// inside the save's transaction, after the write, so the conditions see the new values.
// Work the system does on its own (workflows, sweeps) is not held back by them.
func (h *Handler) checkRules(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, id uuid.UUID, a actorInfo) error {
	if a.Kind == "system" {
		return nil
	}
	rules, err := loadRules(ctx, tx, ws, spec.Key, true)
	if err != nil || len(rules) == 0 {
		return err
	}
	fields, err := allFieldsRaw(ctx, tx, ws, spec)
	if err != nil {
		return err
	}
	env := filterEnv{}
	if a.ID != nil {
		env.Me = *a.ID
	}
	fe := map[string]string{}
	for _, rule := range rules {
		if rule.Filter == nil {
			continue
		}
		b := &sqlBuilder{}
		where, errs := whereFor(spec, fields, ws, listParams{IDs: []uuid.UUID{id}, Filter: rule.Filter, Env: env}, b)
		if len(errs) > 0 {
			// A rule on a field that has since been removed can't be judged: skip it.
			continue
		}
		var match bool
		if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM crm."+spec.Table+" t"+where+")", b.args...).Scan(&match); err != nil {
			return err
		}
		if !match {
			continue
		}
		key := rule.Field
		if _, ok := fieldByKey(fields, key); !ok {
			key = rule.Name
		}
		if _, dup := fe[key]; !dup {
			fe[key] = rule.Message
		}
	}
	if len(fe) > 0 {
		return shared.Validation(fe)
	}
	return nil
}

func fieldByKey(fields []Field, key string) (Field, bool) {
	for _, f := range fields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

// fieldDefaults returns what new records of an object start with.
func fieldDefaults(ctx context.Context, q querier, ws uuid.UUID, object string) (map[string]any, error) {
	rows, err := q.Query(ctx, `SELECT field_key, value FROM crm.field_defaults WHERE workspace_id = $1 AND object_key = $2`, ws, object)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]any{}
	for rows.Next() {
		var k string
		var raw []byte
		if err := rows.Scan(&k, &raw); err != nil {
			return nil, err
		}
		var v any
		if json.Unmarshal(raw, &v) == nil && v != nil {
			out[k] = v
		}
	}
	return out, rows.Err()
}

// applyDefaults fills the fields nobody gave a value for. Fields the person can't
// write (read-only or hidden for them) still get their default: it is the business's
// rule, not the person's input.
func applyDefaults(ctx context.Context, q querier, ws uuid.UUID, spec *objectSpec, fields []Field, values map[string]any) {
	defs, err := fieldDefaults(ctx, q, ws, spec.Key)
	if err != nil || len(defs) == 0 {
		return
	}
	for k, v := range defs {
		f, ok := fieldByKey(fields, k)
		if !ok || f.ReadOnly || f.system {
			continue
		}
		if cur, given := values[k]; given && !isEmpty(cur) {
			continue
		}
		values[k] = v
	}
}

func (h *Handler) handleListValidationRules(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "customize")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out, err := h.rulesPage(r.Context(), ws, spec)
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) rulesPage(ctx context.Context, ws uuid.UUID, spec *objectSpec) (*rulesPage, error) {
	rules, err := loadRules(ctx, h.store.Pool, ws, spec.Key, false)
	if err != nil {
		return nil, err
	}
	defs, err := fieldDefaults(ctx, h.store.Pool, ws, spec.Key)
	if err != nil {
		return nil, err
	}
	return &rulesPage{Rules: rules, Defaults: defs}, nil
}

func (h *Handler) handleSaveValidationRule(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "customize")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in ruleInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var id uuid.UUID
	creating := chi.URLParam(r, "ruleId") == ""
	if !creating {
		if id, err = uuid.Parse(chi.URLParam(r, "ruleId")); err != nil {
			shared.WriteError(w, r, shared.NotFound("rule_not_found"))
			return
		}
	}
	ctx := r.Context()
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		cur := ValidationRule{Active: true}
		if !creating {
			all, err := loadRules(ctx, tx, ws, spec.Key, false)
			if err != nil {
				return err
			}
			found := false
			for _, v := range all {
				if v.ID == id {
					cur, found = v, true
				}
			}
			if !found {
				return shared.NotFound("rule_not_found")
			}
		} else {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm.validation_rules WHERE workspace_id = $1 AND object_key = $2`, ws, spec.Key).Scan(&n); err != nil {
				return err
			}
			if n >= maxRulesPerObject {
				return shared.Validation(map[string]string{"name": "This object already has 30 rules."})
			}
		}
		if in.Name != nil {
			cur.Name = strings.TrimSpace(*in.Name)
		}
		if in.Message != nil {
			cur.Message = strings.TrimSpace(*in.Message)
		}
		if in.Field != nil {
			cur.Field = strings.TrimSpace(*in.Field)
		}
		if in.Filter != nil {
			cur.Filter = in.Filter
		}
		if in.Active != nil {
			cur.Active = *in.Active
		}
		fields, err := allFieldsRaw(ctx, tx, ws, spec)
		if err != nil {
			return err
		}
		fe := map[string]string{}
		if cur.Name == "" || len(cur.Name) > 80 {
			fe["name"] = "Give the rule a name (up to 80 characters)."
		}
		if cur.Message == "" || len(cur.Message) > 300 {
			fe["message"] = "Write what the person should see (up to 300 characters)."
		}
		if cur.Field != "" {
			if _, ok := fieldByKey(fields, cur.Field); !ok {
				fe["field"] = "Pick a field of this object."
			}
		}
		if cur.Filter == nil || countConditions(*cur.Filter) == 0 {
			fe["filter"] = "Add at least one condition."
		} else if sql, errs := compileFilter(cur.Filter, fields, filterEnv{}, &sqlBuilder{}); len(errs) > 0 {
			fe["filter"] = "Fix the conditions: " + errMessage(shared.Validation(errs))
		} else if sql == "" {
			fe["filter"] = "Add at least one condition."
		}
		if len(fe) > 0 {
			return shared.Validation(fe)
		}
		raw, _ := json.Marshal(cur.Filter)
		if creating {
			err = tx.QueryRow(ctx, `
				INSERT INTO crm.validation_rules (workspace_id, object_key, name, message, field_key, filter, is_active, created_by)
				VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8) RETURNING id`,
				ws, spec.Key, cur.Name, cur.Message, cur.Field, raw, cur.Active, actor(r)).Scan(&id)
		} else {
			_, err = tx.Exec(ctx, `
				UPDATE crm.validation_rules SET name = $3, message = $4, field_key = NULLIF($5, ''), filter = $6, is_active = $7, updated_at = now()
				WHERE id = $1 AND workspace_id = $2`, id, ws, cur.Name, cur.Message, cur.Field, raw, cur.Active)
		}
		if err != nil {
			var pg *pgconn.PgError
			if errors.As(err, &pg) && pg.Code == "23505" {
				return shared.Validation(map[string]string{"name": "Another rule on this object has this name."})
			}
			return err
		}
		action := "validation_rule.updated"
		if creating {
			action = "validation_rule.created"
		}
		return shared.WriteAudit(ctx, tx, audit(r, ws, action, "validation_rule", &id, nil,
			map[string]any{"object": spec.Key, "name": cur.Name, "active": cur.Active}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out, err := h.rulesPage(ctx, ws, spec)
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) handleDeleteValidationRule(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "customize")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "ruleId"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("rule_not_found"))
		return
	}
	ctx := r.Context()
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var name string
		if err := tx.QueryRow(ctx, `DELETE FROM crm.validation_rules WHERE id = $1 AND workspace_id = $2 AND object_key = $3 RETURNING name`,
			id, ws, spec.Key).Scan(&name); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return shared.NotFound("rule_not_found")
			}
			return err
		}
		return shared.WriteAudit(ctx, tx, audit(r, ws, "validation_rule.deleted", "validation_rule", &id, map[string]any{"object": spec.Key, "name": name}, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out, err := h.rulesPage(ctx, ws, spec)
	respond(w, r, http.StatusOK, out, err)
}

// handleSaveDefaults replaces the object's default values with the ones given (a null
// or empty value clears that field's default).
func (h *Handler) handleSaveDefaults(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "customize")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in struct {
		Defaults map[string]any `json:"defaults"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		fields, err := allFieldsRaw(ctx, tx, ws, spec)
		if err != nil {
			return err
		}
		keep := map[string]any{}
		for k, v := range in.Defaults {
			if !isEmpty(v) {
				keep[k] = v
			}
		}
		fe := map[string]string{}
		for k := range keep {
			f, ok := fieldByKey(fields, k)
			switch {
			case !ok:
				fe[k] = "Unknown field."
			case f.ReadOnly || f.system:
				fe[k] = "This field is filled in by the CRM."
			case f.Unique:
				fe[k] = "A field that must be unique can't have a default."
			case f.Type == "files":
				fe[k] = "Files can't have a default."
			}
		}
		if len(fe) == 0 {
			// The same checks a save makes: the right type, a real option, a record that exists.
			_, fe = buildChanges(ctx, tx, ws, fields, keep, false)
		}
		if len(fe) > 0 {
			return shared.Validation(fe)
		}
		if _, err := tx.Exec(ctx, `DELETE FROM crm.field_defaults WHERE workspace_id = $1 AND object_key = $2`, ws, spec.Key); err != nil {
			return err
		}
		for k, v := range keep {
			raw, _ := json.Marshal(v)
			if _, err := tx.Exec(ctx, `INSERT INTO crm.field_defaults (workspace_id, object_key, field_key, value, updated_by) VALUES ($1, $2, $3, $4, $5)`,
				ws, spec.Key, k, raw, actor(r)); err != nil {
				return err
			}
		}
		return shared.WriteAudit(ctx, tx, audit(r, ws, "field_defaults.updated", "field_defaults", nil, nil, map[string]any{"object": spec.Key, "fields": len(keep)}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out, err := h.rulesPage(ctx, ws, spec)
	respond(w, r, http.StatusOK, out, err)
}
