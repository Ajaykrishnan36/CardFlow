package records

import (
	"context"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"cardflow-backend/internal/crm/shared"
)

// Values a business adds to built-in dropdowns (D-154). A built-in select field keeps its
// own values; the business can add more after them (and remove what it added, while no
// record uses the value). Leads' and opportunities' stages have their own editor.

// addedOptions appends each built-in dropdown's added values, in place, on a copy of its options.
func addedOptions(ctx context.Context, q querier, ws uuid.UUID, spec *objectSpec, fields []Field) error {
	rows, err := q.Query(ctx, `SELECT field_key, value, label FROM crm.field_option_additions
		WHERE workspace_id = $1 AND object_key = $2 ORDER BY position, created_at`, ws, spec.Key)
	if err != nil {
		return err
	}
	defer rows.Close()
	extra := map[string][]Option{}
	for rows.Next() {
		var f string
		var o Option
		if err := rows.Scan(&f, &o.Value, &o.Label); err != nil {
			return err
		}
		extra[f] = append(extra[f], o)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	for i := range fields {
		if add := extra[fields[i].Key]; len(add) > 0 && (fields[i].Type == "select" || fields[i].Type == "multiselect") {
			fields[i].Options = append(append([]Option{}, fields[i].Options...), add...)
			for _, o := range add {
				fields[i].AddedOptions = append(fields[i].AddedOptions, o.Value)
			}
		}
	}
	return nil
}

// saveAddedOptions: the dropdown's values as the customizer left them. Every built-in value
// must still be there; values that aren't built in become the business's additions.
func (h *Handler) saveAddedOptions(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, f Field, want []Option) error {
	if f.Type != "select" && f.Type != "multiselect" {
		return shared.Forbidden("standard_field", "Standard fields can't be changed; hide them from the layout instead.")
	}
	if spec.StatusField == f.Key {
		return shared.Validation(map[string]string{"options": "Statuses drive the CRM's own steps; leads' and opportunities' stages are set in their stage settings."})
	}
	clean, msg := validateOptions(f.Type, want)
	if msg != "" {
		return shared.Validation(map[string]string{"options": msg})
	}
	base := map[string]bool{}
	for _, o := range f.Options {
		base[o.Value] = true
	}
	kept := map[string]bool{}
	var add []Option
	for _, o := range clean {
		kept[o.Value] = true
		if !base[o.Value] {
			add = append(add, o)
		}
	}
	for _, o := range f.Options {
		if !kept[o.Value] {
			return shared.Validation(map[string]string{"options": fmt.Sprintf("“%s” comes with the CRM and can't be removed.", o.Label)})
		}
	}
	// An added value that records still use stays until they are moved off it.
	rows, err := tx.Query(ctx, `SELECT value, label FROM crm.field_option_additions WHERE workspace_id = $1 AND object_key = $2 AND field_key = $3 FOR UPDATE`, ws, spec.Key, f.Key)
	if err != nil {
		return err
	}
	var gone []Option
	for rows.Next() {
		var o Option
		if err := rows.Scan(&o.Value, &o.Label); err != nil {
			rows.Close()
			return err
		}
		if !kept[o.Value] {
			gone = append(gone, o)
		}
	}
	rows.Close()
	expr := fieldExpr(f)
	for _, o := range gone {
		var n int
		cond := expr + " = $2"
		if f.Type == "multiselect" {
			cond = "(" + expr + ")::jsonb ? $2"
		}
		if err := tx.QueryRow(ctx, "SELECT count(*) FROM crm."+spec.Table+" t WHERE t.workspace_id = $1 AND t.deleted_at IS NULL AND "+cond, ws, o.Value).Scan(&n); err != nil {
			return err
		}
		if n > 0 {
			return shared.Validation(map[string]string{"options": fmt.Sprintf("%d %s still use “%s”. Change them first.", n, strings.ToLower(spec.Plural), o.Label)})
		}
	}
	if _, err := tx.Exec(ctx, `DELETE FROM crm.field_option_additions WHERE workspace_id = $1 AND object_key = $2 AND field_key = $3`, ws, spec.Key, f.Key); err != nil {
		return err
	}
	for i, o := range add {
		if _, err := tx.Exec(ctx, `INSERT INTO crm.field_option_additions (workspace_id, object_key, field_key, value, label, position) VALUES ($1, $2, $3, $4, $5, $6)`,
			ws, spec.Key, f.Key, o.Value, o.Label, i); err != nil {
			return err
		}
	}
	return nil
}

// fieldExpr: where a field's value lives in the object's table.
func fieldExpr(f Field) string {
	if f.column != "" {
		return "t." + f.column + "::text"
	}
	return "t.custom->>'" + strings.ReplaceAll(f.Key, "'", "") + "'"
}
