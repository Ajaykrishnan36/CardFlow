package records

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// afterSave runs inside the transaction of every record change, after the record, its
// timeline entry, audit event and outbox event are written. It keeps data that is derived
// from records in step with them (D-113, D-115): an invoice's paid amount from its
// payments, a case's SLA clocks from its status. Changes it makes itself come back here
// with a system actor and are not processed again.
//
// op: create | update | delete | restore. before is nil on create.
func (h *Handler) afterSave(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, a actorInfo, op string, before, after *Row) (*Row, error) {
	// The currency step's own write comes back here: nothing more to do for it.
	if a.Source == sourceCurrency {
		return after, nil
	}
	// Currency, rate and the amount in the base currency (D-118), before anything reads them.
	after, err := h.currencySaved(ctx, tx, ws, spec, op, before, after)
	if err != nil {
		return nil, err
	}
	switch spec.Key {
	case "payments":
		if a.Source == sourcePayments {
			return after, nil
		}
		return h.paymentSaved(ctx, tx, ws, a, op, before, after)
	case "invoices":
		if a.Source == sourcePayments {
			return after, nil
		}
		if op == "update" && before.text("status") == "draft" && after.text("status") != "draft" && after.text("finalizedAt") == "" {
			// Finalized: from here its lines and prices stay as they are (D-120).
			if after, err = h.updateValues(ctx, tx, ws, spec, after.uuid(), systemActor(sourcePayments), map[string]any{"finalizedAt": time.Now().UTC().Format(time.RFC3339)}, nil, nil); err != nil {
				return nil, err
			}
		}
		// The total may have changed: the balance follows.
		return h.invoiceSaved(ctx, tx, ws, op, after)
	case "cases":
		if a.Source == sourceSLA {
			return after, nil
		}
		if after, err = h.caseSaved(ctx, tx, ws, a, op, before, after); err != nil {
			return nil, err
		}
		return h.caseEntitlement(ctx, tx, ws, a, op, before, after)
	case "refunds":
		if a.Source == sourceFinance {
			return after, nil
		}
		return h.refundSaved(ctx, tx, ws, a, op, before, after)
	case "credit_notes", "debit_notes", "adjustments":
		if a.Source == sourceFinance {
			return after, nil
		}
		return h.financeDocSaved(ctx, tx, ws, spec, a, op, before, after)
	case "line_items":
		if a.Source == sourcePricing {
			return after, nil
		}
		return after, h.lineItemSaved(ctx, tx, ws, op, before, after)
	case "quotes":
		if a.Source == sourcePricing || a.Source == sourceApproval {
			return after, nil
		}
		return after, h.quoteSaved(before, after)
	case "opportunities":
		if a.Source == sourceTerritory {
			return after, nil
		}
		return h.opportunityTerritory(ctx, tx, ws, op, before, after)
	case "territories":
		if op == "delete" {
			return after, nil
		}
		return after, h.territorySaved(ctx, tx, ws, after)
	case "appointments":
		if a.Source == sourceScheduling {
			return after, nil
		}
		return h.appointmentSaved(ctx, tx, ws, a, op, before, after)
	case "work_orders":
		if a.Source == sourceScheduling {
			return after, nil
		}
		return h.workOrderSaved(ctx, tx, ws, a, op, before, after)
	}
	return after, nil
}

// text reads a field of a record as text ("" when empty).
func (r *Row) text(key string) string {
	if r == nil {
		return ""
	}
	s, _ := r.Values[key].(string)
	return s
}

// id reads a lookup field of a record as an id (uuid.Nil when empty).
func (r *Row) id(key string) uuid.UUID {
	id, err := uuid.Parse(r.text(key))
	if err != nil {
		return uuid.Nil
	}
	return id
}

func (r *Row) uuid() uuid.UUID {
	id, _ := uuid.Parse(r.ID)
	return id
}

// flag reads a boolean field.
func (r *Row) flag(key string) bool {
	if r == nil {
		return false
	}
	b, _ := r.Values[key].(bool)
	return b
}
