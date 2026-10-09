package records

import (
	"cardflow-backend/internal/crm/shared"
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
	case "accounts":
		return after, h.noLoop(ctx, tx, ws, op, after, "accounts", "parent_account_id", "parentAccountId", "An account can't be placed under itself or under one of its own child accounts.")
	case "contacts":
		return after, h.noLoop(ctx, tx, ws, op, after, "contacts", "reports_to_id", "reportsToId", "A contact can't report to themselves or to someone who reports to them.")
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
		if a.Source == sourceTerritory || a.Source == sourceSales {
			return after, nil
		}
		if after, err = h.opportunityClosed(ctx, tx, ws, op, before, after); err != nil {
			return nil, err
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

const sourceSales = "sales"

// noLoop refuses a parent that is the record itself or already somewhere below it.
func (h *Handler) noLoop(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, after *Row, table, column, field, message string) error {
	if op != "create" && op != "update" {
		return nil
	}
	parent := after.id(field)
	if parent == uuid.Nil {
		return nil
	}
	var loops bool
	err := tx.QueryRow(ctx, `
		WITH RECURSIVE up AS (
		  SELECT id, `+column+` AS parent, 1 AS depth FROM crm.`+table+` WHERE id = $2 AND workspace_id = $1
		  UNION ALL
		  SELECT t.id, t.`+column+`, up.depth + 1 FROM crm.`+table+` t JOIN up ON t.id = up.parent
		  WHERE t.workspace_id = $1 AND up.depth < 50)
		SELECT EXISTS (SELECT 1 FROM up WHERE id = $3)`, ws, parent, after.uuid()).Scan(&loops)
	if err != nil {
		return err
	}
	if loops {
		return shared.Validation(map[string]string{field: message})
	}
	return nil
}

// opportunityClosed runs when a deal becomes won or lost. A close date still in the future
// moves to today, so the deal counts in the period it was really closed in (a past date,
// as on imported history, is kept). A won deal also turns a prospect account into an
// active customer.
func (h *Handler) opportunityClosed(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, after *Row) (*Row, error) {
	if op != "create" && op != "update" {
		return after, nil
	}
	closed := func(r *Row) bool {
		return r != nil && (r.text("status") == "closed_won" || r.text("status") == "closed_lost")
	}
	if !closed(after) || closed(before) {
		return after, nil
	}
	today := time.Now().In(istLocation).Format("2006-01-02")
	var err error
	if cd := after.text("closeDate"); len(cd) >= 10 && cd[:10] > today {
		if after, err = h.updateValues(ctx, tx, ws, specFor("opportunities"), after.uuid(), systemActor(sourceSales), map[string]any{"closeDate": today}, nil, nil); err != nil {
			return nil, err
		}
	}
	if acc := after.id("accountId"); after.text("status") == "closed_won" && acc != uuid.Nil {
		if _, err := tx.Exec(ctx, `
			UPDATE crm.accounts SET lifecycle = 'active', type = CASE WHEN COALESCE(type, '') IN ('', 'prospect') THEN 'customer' ELSE type END, updated_at = now()
			WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL AND COALESCE(lifecycle, '') IN ('', 'prospect')`, acc, ws); err != nil {
			return nil, err
		}
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
