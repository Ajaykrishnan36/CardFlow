package records

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Records that mirror something in a connected app (D-72), e.g. Business Card Snap's
// support tickets as Cases. They go through the normal write path (validation, history,
// audit, events), so lists, workflows and webhooks treat them like any other record.

// SyncRecord creates the record whose custom field keyField equals key, or updates it.
// A record someone deleted in the CRM is left alone. Returns the record id and whether
// it was created.
func SyncRecord(ctx context.Context, tx pgx.Tx, ws uuid.UUID, object, keyField, key string, values map[string]any, actor uuid.UUID, source string) (uuid.UUID, bool, error) {
	h := eventHandler
	spec := specFor(object)
	if h == nil || spec == nil {
		return uuid.Nil, false, fmt.Errorf("object %q isn't loaded", object)
	}
	a := actorInfo{ID: &actor, Kind: "system", Source: source}
	var id uuid.UUID
	var deleted bool
	err := tx.QueryRow(ctx, `SELECT id, deleted_at IS NOT NULL FROM crm.`+spec.Table+`
		WHERE workspace_id = $1 AND custom->>$2::text = $3 ORDER BY created_at LIMIT 1`, ws, keyField, key).Scan(&id, &deleted)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		values[keyField] = key
		row, err := h.createRecord(ctx, tx, ws, spec, a, values)
		if err != nil {
			return uuid.Nil, false, err
		}
		nid, _ := uuid.Parse(row.ID)
		return nid, true, nil
	case err != nil:
		return uuid.Nil, false, err
	case deleted:
		return id, false, nil
	}
	_, err = h.updateValues(ctx, tx, ws, spec, id, a, values, nil, nil)
	return id, false, err
}
