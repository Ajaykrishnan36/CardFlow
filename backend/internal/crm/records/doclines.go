package records

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// inheritLines (D-154): a quote made for an opportunity starts with the opportunity's
// products, and a contract made for an order (or a quote, or an opportunity) starts with
// what was sold, priced exactly as there. Only a new document that has no amount of its
// own inherits: revisions, renewals and conversions bring their own lines and totals.
var inheritFrom = map[string][]struct{ field, object string }{
	"quotes":    {{"opportunityId", "opportunities"}},
	"contracts": {{"orderId", "sales_orders"}, {"quoteId", "quotes"}, {"opportunityId", "opportunities"}},
}

func (h *Handler) inheritLines(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, after *Row) (*Row, error) {
	sources, ok := inheritFrom[spec.Key]
	if !ok || !standardObject(spec.Key) || specFor("line_items") == nil {
		return after, nil
	}
	p := lineParents[spec.Key]
	amountField := p.amount
	if p.totals {
		amountField = "total"
	}
	if after.num(amountField) != 0 {
		return after, nil
	}
	for _, s := range sources {
		src := after.id(s.field)
		if src == uuid.Nil {
			continue
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'line_items' AND deleted_at IS NULL
			AND custom->>'`+lineParents[s.object].field+`' = $2`, ws, src.String()).Scan(&n); err != nil {
			return nil, err
		}
		if n == 0 {
			continue
		}
		if err := h.copyLines(ctx, tx, ws, lineParents[s.object].field, src.String(), p.field, after.ID); err != nil {
			return nil, err
		}
		// The document's numbers are the sum of what it now holds.
		var sub, disc, tax float64
		var cur string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(NULLIF(custom->>'total', '')::numeric), 0)::float8,
				COALESCE(sum(NULLIF(custom->>'discountAmount', '')::numeric), 0)::float8,
				COALESCE(sum(NULLIF(custom->>'taxAmount', '')::numeric), 0)::float8
			FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'line_items' AND deleted_at IS NULL AND custom->>'`+p.field+`' = $2`,
			ws, after.ID).Scan(&sub, &disc, &tax); err != nil {
			return nil, err
		}
		_ = tx.QueryRow(ctx, `SELECT COALESCE(custom->>'currency', '') FROM crm.object_records WHERE id = $1`, src).Scan(&cur)
		set := map[string]any{}
		if p.totals {
			set["subtotal"], set["discount"], set["tax"], set["total"] = sub, disc, tax, sub+tax
		} else {
			set[p.amount] = sub + tax
		}
		if cur != "" && !after.has("currency") {
			set["currency"] = cur
		}
		for k := range set {
			if _, ok := spec.field(k); !ok {
				delete(set, k)
			}
		}
		return h.updateValues(ctx, tx, ws, spec, uuid.MustParse(after.ID), systemActor(sourcePricing), set, nil, nil)
	}
	return after, nil
}
