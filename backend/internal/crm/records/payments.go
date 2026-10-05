package records

import (
	"context"
	"errors"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Payments (D-113). A payment is a record (object "payments") like any other. What it
// pays is kept in crm.payment_allocations: one payment can pay several invoices, one
// invoice can be paid by several payments, and whatever isn't applied yet stays visible as
// "not yet applied". An invoice's paid amount, balance and paid status are worked out from
// those rows inside the same transaction — nobody types them in.
//
// Money is added up in SQL as numeric(16,2); it never passes through a float sum.
//
//	GET    /w/{code}/payments/{id}/allocations
//	POST   /w/{code}/payments/{id}/allocations        {invoiceId, amount}
//	DELETE /w/{code}/payments/{id}/allocations/{allocationId}
//	POST   /w/{code}/payments/{id}/refund             {amount, reason}
//	GET    /w/{code}/invoices/{id}/payments           the invoice's ledger
//	GET    /w/{code}/finance/receivables              what customers owe

const sourcePayments = "payments"

// paymentCounts: the payment statuses whose money has actually arrived.
const paymentCounts = `('paid', 'partially_refunded')`

func (h *Handler) paymentRoutes(r chi.Router) {
	r.Get("/payments/{id}/allocations", h.handleListAllocations)
	r.Post("/payments/{id}/allocations", h.handleAllocate)
	r.Delete("/payments/{id}/allocations/{allocationId}", h.handleUnallocate)
	r.Post("/payments/{id}/refund", h.handleRefund)
	r.Get("/invoices/{id}/payments", h.handleInvoiceLedger)
	r.Get("/finance/receivables", h.handleReceivables)
}

// money reads a JSON text value as numeric(16,2) in SQL; anything that isn't a number is 0.
func money(expr string) string {
	return `(CASE WHEN ` + expr + ` ~ '^-?[0-9]+(\.[0-9]+)?$' THEN round((` + expr + `)::numeric, 2) ELSE 0 END)`
}

// paymentSaved keeps allocations and the invoices they pay in step with a payment.
func (h *Handler) paymentSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, op string, before, after *Row) (*Row, error) {
	id := after.uuid()
	invoices := map[uuid.UUID]bool{}
	rows, err := tx.Query(ctx, `SELECT invoice_id FROM crm.payment_allocations WHERE payment_id = $1 AND workspace_id = $2`, id, ws)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var inv uuid.UUID
		if err := rows.Scan(&inv); err != nil {
			rows.Close()
			return nil, err
		}
		invoices[inv] = true
	}
	rows.Close()

	// The invoice named on the payment gets as much of it as the invoice still needs.
	invoiceID := after.id("invoiceId")
	changedInvoice := op == "create" || (op == "update" && before.id("invoiceId") != invoiceID)
	if changedInvoice && op == "update" {
		if old := before.id("invoiceId"); old != uuid.Nil {
			if _, err := tx.Exec(ctx, `DELETE FROM crm.payment_allocations WHERE payment_id = $1 AND invoice_id = $2 AND workspace_id = $3`, id, old, ws); err != nil {
				return nil, err
			}
			invoices[old] = true
		}
	}
	if changedInvoice && invoiceID != uuid.Nil {
		if err := h.fillPaymentFromInvoice(ctx, tx, ws, id, invoiceID); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.payment_allocations (workspace_id, payment_id, invoice_id, amount, created_by)
			SELECT $1, $2, $3, least(pay.unapplied, inv.balance), $4
			FROM (SELECT `+money(`p.custom->>'amount'`)+` - COALESCE((SELECT sum(amount) FROM crm.payment_allocations WHERE payment_id = p.id), 0) AS unapplied
			      FROM crm.object_records p WHERE p.id = $2 AND p.workspace_id = $1) pay,
			     (SELECT CASE WHEN i.custom ? 'balanceDue' THEN `+money(`i.custom->>'balanceDue'`)+`
			                  ELSE `+money(`i.custom->>'total'`)+` - COALESCE((SELECT sum(al.amount) FROM crm.payment_allocations al
			              JOIN crm.object_records q ON q.id = al.payment_id AND q.deleted_at IS NULL AND q.status NOT IN ('failed', 'cancelled', 'refunded')
			              WHERE al.invoice_id = i.id), 0) END AS balance
			      FROM crm.object_records i WHERE i.id = $3 AND i.workspace_id = $1 AND i.object_key = 'invoices' AND i.deleted_at IS NULL) inv
			WHERE least(pay.unapplied, inv.balance) > 0
			ON CONFLICT (payment_id, invoice_id) DO NOTHING`, ws, id, invoiceID, a.ID); err != nil {
			return nil, err
		}
		invoices[invoiceID] = true
	}

	// A payment can't be worth less than what has been applied from it.
	var short bool
	if err := tx.QueryRow(ctx, `
		SELECT `+money(`p.custom->>'amount'`)+` < COALESCE((SELECT sum(amount) FROM crm.payment_allocations WHERE payment_id = p.id), 0)
		FROM crm.object_records p WHERE p.id = $1 AND p.workspace_id = $2`, id, ws).Scan(&short); err != nil {
		return nil, err
	}
	if short && op != "delete" {
		return nil, shared.Validation(map[string]string{"amount": "This is less than what has already been applied to invoices. Remove an allocation first."})
	}
	if err := h.refreshPayment(ctx, tx, ws, id); err != nil {
		return nil, err
	}
	for inv := range invoices {
		if err := h.recomputeInvoice(ctx, tx, ws, inv); err != nil {
			return nil, err
		}
	}
	if op == "delete" {
		return after, nil
	}
	row, _, err := h.getRow(ctx, tx, ws, specFor("payments"), id, nil)
	return row, err
}

// fillPaymentFromInvoice copies the invoice's account and contact onto a payment that has none.
func (h *Handler) fillPaymentFromInvoice(ctx context.Context, tx pgx.Tx, ws, paymentID, invoiceID uuid.UUID) error {
	_, err := tx.Exec(ctx, `
		UPDATE crm.object_records p SET custom = p.custom
		  || CASE WHEN COALESCE(p.custom->>'accountId', '') = '' AND COALESCE(i.custom->>'accountId', '') <> '' THEN jsonb_build_object('accountId', i.custom->>'accountId') ELSE '{}'::jsonb END
		  || CASE WHEN COALESCE(p.custom->>'contactId', '') = '' AND COALESCE(i.custom->>'contactId', '') <> '' THEN jsonb_build_object('contactId', i.custom->>'contactId') ELSE '{}'::jsonb END
		FROM crm.object_records i
		WHERE p.id = $1 AND p.workspace_id = $3 AND i.id = $2 AND i.workspace_id = $3 AND i.object_key = 'invoices'`, paymentID, invoiceID, ws)
	return err
}

// refreshPayment re-derives everything that hangs off a payment's allocations: which
// invoices its refunds come off (crm.refund_allocations) and its "applied" and "not yet
// applied" amounts. Money refunded comes out of the unapplied part first.
func (h *Handler) refreshPayment(ctx context.Context, tx pgx.Tx, ws, id uuid.UUID) error {
	if err := h.rebuildRefundAllocations(ctx, tx, ws, id); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `
		UPDATE crm.object_records p SET custom = p.custom || jsonb_build_object(
		  'allocatedAmount', x.applied,
		  'unappliedAmount', greatest(`+money(`p.custom->>'amount'`)+` - x.applied - greatest(`+money(`p.custom->>'refundedAmount'`)+` - x.refunded_off_invoices, 0), 0))
		FROM (SELECT COALESCE((SELECT sum(amount) FROM crm.payment_allocations WHERE payment_id = $1 AND workspace_id = $2), 0) AS applied,
		             COALESCE((SELECT sum(amount) FROM crm.refund_allocations WHERE payment_id = $1 AND workspace_id = $2), 0) AS refunded_off_invoices) x
		WHERE p.id = $1 AND p.workspace_id = $2 AND p.object_key = 'payments'`, id, ws)
	return err
}

// invoicePaid is what has really been received for an invoice: the applied part of every
// payment whose money arrived, less the refunded share of each.
const invoicePaidSQL = `
	SELECT COALESCE(sum(al.amount - COALESCE((SELECT sum(ra.amount) FROM crm.refund_allocations ra
	                                           WHERE ra.payment_id = al.payment_id AND ra.invoice_id = al.invoice_id), 0)), 0)
	FROM crm.payment_allocations al
	JOIN crm.object_records pay ON pay.id = al.payment_id AND pay.deleted_at IS NULL AND pay.status IN ` + paymentCounts + `
	WHERE al.invoice_id = $1 AND al.workspace_id = $2`

// recomputeInvoice sets an invoice's paid amount, balance and paid status from its payments.
func (h *Handler) recomputeInvoice(ctx context.Context, tx pgx.Tx, ws, invoiceID uuid.UUID) error {
	spec := specFor("invoices")
	if spec == nil {
		return nil
	}
	var paid, total, wasPaid, wasBalance float64
	var status string
	var hasAllocations, deleted, hadBalance bool
	err := tx.QueryRow(ctx, `
		SELECT (`+invoicePaidSQL+`)::float8, `+money(`i.custom->>'total'`)+`::float8, `+money(`i.custom->>'amountPaid'`)+`::float8,
		       `+money(`i.custom->>'balanceDue'`)+`::float8, i.custom ? 'balanceDue', COALESCE(i.status, ''),
		       EXISTS (SELECT 1 FROM crm.payment_allocations WHERE invoice_id = i.id), i.deleted_at IS NOT NULL
		FROM crm.object_records i WHERE i.id = $1 AND i.workspace_id = $2 AND i.object_key = 'invoices'`, invoiceID, ws).
		Scan(&paid, &total, &wasPaid, &wasBalance, &hadBalance, &status, &hasAllocations, &deleted)
	if errors.Is(err, pgx.ErrNoRows) || deleted {
		return nil
	}
	if err != nil {
		return err
	}
	// An invoice no payment has ever been applied to keeps the paid amount it was given
	// before payments existed; only its balance is worked out.
	if !hasAllocations {
		paid = wasPaid
	}
	// Credit notes and write-offs lower what is owed, debit notes raise it (D-126). Each is
	// a record of its own; nothing here is typed onto the invoice.
	extra, err := h.invoiceExtras(ctx, tx, ws, invoiceID)
	if err != nil {
		return err
	}
	credited, debited, writtenOff := extra.credited.Float(), extra.debited.Float(), extra.writtenOff.Float()
	balance := math.Round((total+debited-paid-credited-writtenOff)*100) / 100
	if balance < 0 {
		balance = 0
	}
	values := map[string]any{}
	if paid != wasPaid {
		values["amountPaid"] = paid
	}
	for key, v := range map[string]Cents{"creditedAmount": extra.credited, "debitedAmount": extra.debited, "writtenOffAmount": extra.writtenOff} {
		if had, ok := parseCents(extra.stored[key]); had != v || (!ok && v != 0) {
			values[key] = v.Float()
		}
	}
	if balance != wasBalance || !hadBalance {
		values["balanceDue"] = balance
	}
	next := status
	switch {
	case status == "draft" || status == "void":
	case total > 0 && balance <= 0:
		next = "paid"
	case status == "overdue":
		// Still owed and past due: a part payment doesn't make it less late.
	case paid > 0 || credited > 0 || writtenOff > 0:
		next = "partially_paid"
	case status == "paid" || status == "partially_paid":
		next = "sent" // the payment was removed, failed or fully refunded
	}
	if next != status {
		values["status"] = next
	}
	if len(values) == 0 {
		return nil
	}
	_, err = h.updateValues(ctx, tx, ws, spec, invoiceID, systemActor(sourcePayments), values, nil, nil)
	return err
}

func (h *Handler) invoiceSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, after *Row) (*Row, error) {
	if op == "delete" {
		return after, nil
	}
	if err := h.recomputeInvoice(ctx, tx, ws, after.uuid()); err != nil {
		return nil, err
	}
	row, _, err := h.getRow(ctx, tx, ws, specFor("invoices"), after.uuid(), nil)
	return row, err
}

// ---- API ----

type Allocation struct {
	ID        string    `json:"id"`
	InvoiceID string    `json:"invoiceId"`
	Invoice   string    `json:"invoice"`
	Code      string    `json:"invoiceCode"`
	Amount    float64   `json:"amount"`
	CreatedAt time.Time `json:"createdAt"`
}

// paymentScope checks the caller may act on a payment (need = read | update) and returns it.
func (h *Handler) paymentScope(r *http.Request, need string) (*Scope, *Row, error) {
	sc := scopeFrom(r.Context())
	spec := specFor("payments")
	if spec == nil || !sc.Enabled("payments") {
		return nil, nil, shared.NotFound("object_not_found")
	}
	if !sc.Can("payments", need) {
		return nil, nil, errForbidden
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return nil, nil, shared.NotFound("record_not_found")
	}
	row, _, err := h.getRow(r.Context(), h.store.Pool, sc.WS, spec, id, sc.OwnersFor("payments", actor(r)))
	return sc, row, err
}

func (h *Handler) listAllocations(ctx context.Context, q querier, ws, paymentID uuid.UUID) ([]Allocation, error) {
	rows, err := q.Query(ctx, `
		SELECT al.id::text, i.id::text, i.name, i.code, al.amount::float8, al.created_at
		FROM crm.payment_allocations al JOIN crm.object_records i ON i.id = al.invoice_id
		WHERE al.payment_id = $1 AND al.workspace_id = $2 ORDER BY al.created_at`, paymentID, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Allocation{}
	for rows.Next() {
		var a Allocation
		if err := rows.Scan(&a.ID, &a.InvoiceID, &a.Invoice, &a.Code, &a.Amount, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (h *Handler) handleListAllocations(w http.ResponseWriter, r *http.Request) {
	sc, row, err := h.paymentScope(r, "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := h.listAllocations(r.Context(), h.store.Pool, sc.WS, row.uuid())
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": list})
}

func (h *Handler) handleAllocate(w http.ResponseWriter, r *http.Request) {
	sc, pay, err := h.paymentScope(r, "update")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in struct {
		InvoiceID string  `json:"invoiceId"`
		Amount    float64 `json:"amount"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	me := actor(r)
	invoiceID, err := uuid.Parse(in.InvoiceID)
	fe := map[string]string{}
	if err != nil {
		fe["invoiceId"] = "Choose an invoice."
	}
	if in.Amount <= 0 {
		fe["amount"] = "Enter an amount above zero."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	// The invoice must be one the caller can open, in this business.
	if !sc.Can("invoices", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	if _, _, err := h.getRow(ctx, h.store.Pool, sc.WS, specFor("invoices"), invoiceID, sc.OwnersFor("invoices", me)); err != nil {
		shared.WriteError(w, r, shared.Validation(map[string]string{"invoiceId": "Choose an invoice of this business."}))
		return
	}
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var unapplied, balance float64
		if err := tx.QueryRow(ctx, `
			SELECT (`+money(`p.custom->>'amount'`)+` - COALESCE((SELECT sum(amount) FROM crm.payment_allocations WHERE payment_id = p.id AND invoice_id <> $3), 0))::float8,
			       (SELECT (`+money(`i.custom->>'total'`)+` - COALESCE((SELECT sum(al.amount) FROM crm.payment_allocations al
			                 JOIN crm.object_records q ON q.id = al.payment_id AND q.deleted_at IS NULL AND q.status NOT IN ('failed', 'cancelled', 'refunded')
			                 WHERE al.invoice_id = i.id AND al.payment_id <> p.id), 0))::float8
			        FROM crm.object_records i WHERE i.id = $3 AND i.workspace_id = $2)
			FROM crm.object_records p WHERE p.id = $1 AND p.workspace_id = $2 FOR UPDATE`, pay.uuid(), sc.WS, invoiceID).Scan(&unapplied, &balance); err != nil {
			return err
		}
		if in.Amount > unapplied+0.004 {
			return shared.Validation(map[string]string{"amount": "That is more than this payment has left to apply."})
		}
		if in.Amount > balance+0.004 {
			return shared.Validation(map[string]string{"amount": "That is more than the invoice still needs."})
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.payment_allocations (workspace_id, payment_id, invoice_id, amount, created_by) VALUES ($1, $2, $3, round($4::numeric, 2), $5)
			ON CONFLICT (payment_id, invoice_id) DO UPDATE SET amount = EXCLUDED.amount`, sc.WS, pay.uuid(), invoiceID, in.Amount, me); err != nil {
			return err
		}
		if err := h.refreshPayment(ctx, tx, sc.WS, pay.uuid()); err != nil {
			return err
		}
		if err := h.recomputeInvoice(ctx, tx, sc.WS, invoiceID); err != nil {
			return err
		}
		pid := pay.uuid()
		if err := insertActivity(ctx, tx, sc.WS, "payments", pid, "payment.applied", "Applied to an invoice", map[string]any{"invoiceId": invoiceID.String(), "amount": in.Amount}, a.ID); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "payment.allocated", "payment", &pid, nil, map[string]any{"invoiceId": invoiceID.String(), "amount": in.Amount}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.bus.Kick()
	list, err := h.listAllocations(ctx, h.store.Pool, sc.WS, pay.uuid())
	respond(w, r, http.StatusOK, map[string]any{"data": list}, err)
}

func (h *Handler) handleUnallocate(w http.ResponseWriter, r *http.Request) {
	sc, pay, err := h.paymentScope(r, "update")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	allocID, err := uuid.Parse(chi.URLParam(r, "allocationId"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("allocation_not_found"))
		return
	}
	ctx := r.Context()
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var invoiceID uuid.UUID
		var amount float64
		err := tx.QueryRow(ctx, `DELETE FROM crm.payment_allocations WHERE id = $1 AND payment_id = $2 AND workspace_id = $3 RETURNING invoice_id, amount::float8`,
			allocID, pay.uuid(), sc.WS).Scan(&invoiceID, &amount)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("allocation_not_found")
		}
		if err != nil {
			return err
		}
		if err := h.refreshPayment(ctx, tx, sc.WS, pay.uuid()); err != nil {
			return err
		}
		if err := h.recomputeInvoice(ctx, tx, sc.WS, invoiceID); err != nil {
			return err
		}
		pid := pay.uuid()
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "payment.unallocated", "payment", &pid, map[string]any{"invoiceId": invoiceID.String(), "amount": amount}, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.bus.Kick()
	w.WriteHeader(http.StatusNoContent)
}

// handleRefund records money given back on a payment: it creates a refund record (D-126),
// and everything else — the payment's refunded amount and status, which invoices lose the
// money — follows from that record.
func (h *Handler) handleRefund(w http.ResponseWriter, r *http.Request) {
	sc, pay, err := h.paymentScope(r, "update")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if specFor("refunds") == nil || !sc.Can("refunds", "create") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	var in struct {
		Amount    any    `json:"amount"`
		Reason    string `json:"reason"`
		Reference string `json:"reference"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	amount, ok := parseCents(in.Amount)
	if !ok || amount <= 0 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"amount": "Enter an amount up to what is left of this payment."}))
		return
	}
	ctx := r.Context()
	a := actorFromRequest(r, "ui")
	var out *Row
	var refund *Row
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		v := map[string]any{"name": "Refund of " + pay.Code, "paymentId": pay.ID, "amount": amount.Float(), "refundDate": time.Now().Format("2006-01-02")}
		if s := strings.TrimSpace(in.Reason); s != "" {
			v["reason"] = clipText(s, 300)
		}
		if s := strings.TrimSpace(in.Reference); s != "" {
			v["reference"] = clipText(s, 120)
		}
		var err error
		if refund, err = h.createRecord(ctx, tx, sc.WS, specFor("refunds"), a, v); err != nil {
			return err
		}
		out, _, err = h.getRow(ctx, tx, sc.WS, specFor("payments"), pay.uuid(), nil)
		return err
	})
	if err == nil {
		h.bus.Kick()
		out.Values["refundId"] = refund.ID
		out.Values["refundStatus"] = refund.text("status")
	}
	respond(w, r, http.StatusOK, out, err)
}

type ledgerLine struct {
	ID       string  `json:"id"`
	Code     string  `json:"code"`
	Name     string  `json:"name"`
	Status   string  `json:"status"`
	Date     string  `json:"date"`
	Method   string  `json:"method"`
	Amount   float64 `json:"amount"`
	Applied  float64 `json:"applied"`
	Refunded float64 `json:"refunded"`
	Counts   bool    `json:"counts"` // the money has arrived
}

// handleInvoiceLedger: every payment applied to an invoice, with the totals worked out from them.
func (h *Handler) handleInvoiceLedger(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	ctx := r.Context()
	spec := specFor("invoices")
	if spec == nil || !sc.Enabled("invoices") {
		shared.WriteError(w, r, shared.NotFound("object_not_found"))
		return
	}
	if !sc.Can("invoices", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	inv, _, err := h.getRow(ctx, h.store.Pool, sc.WS, spec, id, sc.OwnersFor("invoices", actor(r)))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out := struct {
		Total       float64       `json:"total"`
		Paid        float64       `json:"paid"`
		Balance     float64       `json:"balance"`
		Status      string        `json:"status"`
		Payments    []ledgerLine  `json:"payments"`
		Credited    Cents         `json:"credited"`
		Debited     Cents         `json:"debited"`
		WrittenOff  Cents         `json:"writtenOff"`
		Adjustments []ledgerExtra `json:"adjustments"`
		CanPay      bool          `json:"canRecordPayment"`
		CanSee      bool          `json:"canSeePayments"`
		InvoiceRef  string        `json:"invoice"`
	}{Payments: []ledgerLine{}, CanPay: sc.Enabled("payments") && sc.Can("payments", "create"), CanSee: sc.Enabled("payments") && sc.Can("payments", "read"), InvoiceRef: inv.Code}
	if err := h.store.Pool.QueryRow(ctx, `SELECT `+money(`custom->>'total'`)+`::float8, `+money(`custom->>'amountPaid'`)+`::float8, COALESCE(status, '')
		FROM crm.object_records WHERE id = $1 AND workspace_id = $2`, id, sc.WS).Scan(&out.Total, &out.Paid, &out.Status); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	extra, err := h.invoiceExtras(ctx, h.store.Pool, sc.WS, id)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out.Credited, out.Debited, out.WrittenOff = extra.credited, extra.debited, extra.writtenOff
	out.Balance = math.Round((out.Total+extra.debited.Float()-out.Paid-extra.credited.Float()-extra.writtenOff.Float())*100) / 100
	if out.Balance < 0 {
		out.Balance = 0
	}
	out.Adjustments = extra.lines
	if out.CanSee {
		rows, err := h.store.Pool.Query(ctx, `
			SELECT p.id::text, p.code, p.name, COALESCE(p.status, ''), COALESCE(p.custom->>'paymentDate', ''), COALESCE(p.custom->>'paymentMethod', ''),
			       `+money(`p.custom->>'amount'`)+`::float8,
			       (al.amount - COALESCE((SELECT sum(ra.amount) FROM crm.refund_allocations ra WHERE ra.payment_id = al.payment_id AND ra.invoice_id = al.invoice_id), 0))::float8,
			       COALESCE((SELECT sum(ra.amount) FROM crm.refund_allocations ra WHERE ra.payment_id = al.payment_id AND ra.invoice_id = al.invoice_id), 0)::float8,
			       p.status IN `+paymentCounts+`
			FROM crm.payment_allocations al JOIN crm.object_records p ON p.id = al.payment_id AND p.deleted_at IS NULL
			WHERE al.invoice_id = $1 AND al.workspace_id = $2 ORDER BY p.custom->>'paymentDate', p.created_at`, id, sc.WS)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		defer rows.Close()
		for rows.Next() {
			var l ledgerLine
			if err := rows.Scan(&l.ID, &l.Code, &l.Name, &l.Status, &l.Date, &l.Method, &l.Amount, &l.Applied, &l.Refunded, &l.Counts); err != nil {
				shared.WriteError(w, r, err)
				return
			}
			out.Payments = append(out.Payments, l)
		}
	}
	shared.WriteJSON(w, http.StatusOK, out)
}

// handleReceivables: what customers owe this business, and money received but not yet applied.
func (h *Handler) handleReceivables(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	ctx := r.Context()
	me := actor(r)
	if specFor("invoices") == nil || !sc.Enabled("invoices") || !sc.Can("invoices", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	out := struct {
		Currency    string       `json:"currency"`
		Invoiced    float64      `json:"invoiced"`
		Received    float64      `json:"received"`
		Outstanding float64      `json:"outstanding"`
		Overdue     float64      `json:"overdue"`
		OpenCount   int          `json:"openInvoices"`
		Unapplied   float64      `json:"unappliedPayments"`
		Top         []RelatedRow `json:"topOutstanding"`
	}{Currency: "INR", Top: []RelatedRow{}}
	_ = h.store.Pool.QueryRow(ctx, `SELECT trim(currency) FROM crm.workspaces WHERE id = $1`, sc.WS).Scan(&out.Currency)
	owners := sc.OwnersFor("invoices", me)
	balance := `greatest(` + money(`i.custom->>'total'`) + ` - ` + money(`i.custom->>'amountPaid'`) + `, 0)`
	open := `i.status NOT IN ('draft', 'void')`
	if err := h.store.Pool.QueryRow(ctx, `
		SELECT COALESCE(sum(`+money(`i.custom->>'total'`)+`) FILTER (WHERE `+open+`), 0)::float8,
		       COALESCE(sum(`+money(`i.custom->>'amountPaid'`)+`) FILTER (WHERE `+open+`), 0)::float8,
		       COALESCE(sum(`+balance+`) FILTER (WHERE `+open+`), 0)::float8,
		       COALESCE(sum(`+balance+`) FILTER (WHERE `+open+` AND COALESCE(i.custom->>'dueDate', '') <> '' AND i.custom->>'dueDate' < to_char(now(), 'YYYY-MM-DD')), 0)::float8,
		       count(*) FILTER (WHERE `+open+` AND `+balance+` > 0)
		FROM crm.object_records i
		WHERE i.workspace_id = $1 AND i.object_key = 'invoices' AND i.deleted_at IS NULL AND ($2::uuid[] IS NULL OR i.owner_id = ANY($2))`,
		sc.WS, owners).Scan(&out.Invoiced, &out.Received, &out.Outstanding, &out.Overdue, &out.OpenCount); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if specFor("payments") != nil && sc.Enabled("payments") && sc.Can("payments", "read") {
		if err := h.store.Pool.QueryRow(ctx, `
			SELECT COALESCE(sum(`+money(`p.custom->>'unappliedAmount'`)+`), 0)::float8 FROM crm.object_records p
			WHERE p.workspace_id = $1 AND p.object_key = 'payments' AND p.deleted_at IS NULL AND p.status IN `+paymentCounts+`
			  AND ($2::uuid[] IS NULL OR p.owner_id = ANY($2))`, sc.WS, sc.OwnersFor("payments", me)).Scan(&out.Unapplied); err != nil {
			shared.WriteError(w, r, err)
			return
		}
	}
	rows, err := h.store.Pool.Query(ctx, `
		SELECT i.id::text, i.code, i.name, COALESCE(i.custom->>'dueDate', ''), (`+balance+`)::text
		FROM crm.object_records i
		WHERE i.workspace_id = $1 AND i.object_key = 'invoices' AND i.deleted_at IS NULL AND `+open+` AND `+balance+` > 0
		  AND ($2::uuid[] IS NULL OR i.owner_id = ANY($2))
		ORDER BY `+balance+` DESC LIMIT 10`, sc.WS, owners)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var row RelatedRow
		if err := rows.Scan(&row.ID, &row.Code, &row.Title, &row.Subtitle, &row.Status); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out.Top = append(out.Top, row)
	}
	shared.WriteJSON(w, http.StatusOK, out)
}

// BackfillOpeningPayments (once): an invoice that was marked as partly or fully paid
// before payments existed gets one payment record for that amount, so its balance is
// explained by a record instead of a typed-in number. Nothing is invented: the amount and
// date come from the invoice itself, and the payment says where it came from.
func (h *Handler) BackfillOpeningPayments(ctx context.Context) {
	const marker = "payments:opening-balances-v1"
	var done bool
	if err := h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.connector_state WHERE key = $1)`, marker).Scan(&done); err != nil || done {
		return
	}
	spec := specFor("payments")
	if spec == nil {
		return
	}
	created := 0
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT i.id, i.workspace_id, i.code, `+money(`i.custom->>'amountPaid'`)+`::float8,
			       COALESCE(NULLIF(i.custom->>'invoiceDate', ''), to_char(i.created_at, 'YYYY-MM-DD')), i.owner_id
			FROM crm.object_records i
			WHERE i.object_key = 'invoices' AND i.deleted_at IS NULL AND `+money(`i.custom->>'amountPaid'`)+` > 0
			  AND NOT EXISTS (SELECT 1 FROM crm.payment_allocations al WHERE al.invoice_id = i.id)`)
		if err != nil {
			return err
		}
		type inv struct {
			id, ws uuid.UUID
			code   string
			paid   float64
			date   string
			owner  *uuid.UUID
		}
		var list []inv
		for rows.Next() {
			var x inv
			if err := rows.Scan(&x.id, &x.ws, &x.code, &x.paid, &x.date, &x.owner); err != nil {
				rows.Close()
				return err
			}
			list = append(list, x)
		}
		rows.Close()
		for _, x := range list {
			values := map[string]any{"name": "Opening balance for " + x.code, "amount": x.paid, "paymentDate": x.date, "status": "paid",
				"paymentMethod": "other", "invoiceId": x.id.String(), "notes": "Recorded automatically from the amount entered on the invoice before payments existed."}
			if x.owner != nil {
				values["ownerId"] = x.owner.String()
			}
			// Created as a normal payment, so the allocation and the invoice follow the same code path.
			if _, err := h.createRecord(ctx, tx, x.ws, spec, actorInfo{Kind: "system", Source: "backfill"}, values); err != nil {
				return err
			}
			created++
		}
		_, err = tx.Exec(ctx, `INSERT INTO crm.connector_state (key, value) VALUES ($1, jsonb_build_object('payments', $2::int)) ON CONFLICT (key) DO NOTHING`, marker, created)
		return err
	})
	if err != nil {
		slog.Error("crm: opening payments not created", "error", err)
		return
	}
	if created > 0 {
		slog.Info("crm: opening payments created for invoices paid before payments existed", "payments", created)
	}
}
