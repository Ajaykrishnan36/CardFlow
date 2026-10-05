package records

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Finance documents (D-126). Everything that changes what a customer owes is a record of
// its own, and an invoice's balance is worked out from those records:
//
//	balance = total + debit notes − payments (less refunds) − credit applied − write-offs
//
//   - Refund: money given back on a payment. The payment's refunded amount is the sum of
//     its succeeded refunds; crm.refund_allocations says which invoices lose it.
//   - Credit note: credit to a customer, applied to invoices through crm.credit_allocations;
//     what isn't applied stays on the note for a later invoice.
//   - Debit note: an extra charge on an issued invoice.
//   - Adjustment: a write-off or correction, with a reason.
//
// Refunds, credit notes, debit notes and adjustments above the business's limits wait for
// approval (D-121) before they count.
//
//	GET    /w/{code}/credit_notes/{id}/allocations
//	POST   /w/{code}/credit_notes/{id}/apply            {invoiceId, amount}
//	DELETE /w/{code}/credit_notes/{id}/allocations/{allocationId}
//	GET    /w/{code}/accounts/{id}/statement

func (h *Handler) financeRoutes(r chi.Router) {
	r.Get("/credit_notes/{id}/allocations", h.handleCreditAllocations)
	r.Post("/credit_notes/{id}/apply", h.handleApplyCredit)
	r.Delete("/credit_notes/{id}/allocations/{allocationId}", h.handleUnapplyCredit)
	r.Get("/accounts/{id}/statement", h.handleAccountStatement)
}

const sourceFinance = "finance"

type ledgerExtra struct {
	Kind   string `json:"kind"` // credit_note | debit_note | adjustment
	ID     string `json:"id"`
	Code   string `json:"code"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Amount Cents  `json:"amount"`
}

type invoiceExtra struct {
	credited, debited, writtenOff Cents
	stored                        map[string]any
	lines                         []ledgerExtra
}

// invoiceExtras adds up what besides payments changes an invoice's balance.
func (h *Handler) invoiceExtras(ctx context.Context, q querier, ws, invoice uuid.UUID) (invoiceExtra, error) {
	out := invoiceExtra{stored: map[string]any{}, lines: []ledgerExtra{}}
	var c1, c2, c3 *string
	_ = q.QueryRow(ctx, `SELECT custom->>'creditedAmount', custom->>'debitedAmount', custom->>'writtenOffAmount' FROM crm.object_records WHERE id = $1 AND workspace_id = $2`, invoice, ws).Scan(&c1, &c2, &c3)
	for k, v := range map[string]*string{"creditedAmount": c1, "debitedAmount": c2, "writtenOffAmount": c3} {
		if v != nil {
			out.stored[k] = *v
		}
	}
	rows, err := q.Query(ctx, `
		SELECT 'credit_note', n.id::text, n.code, n.name, COALESCE(n.status, ''), ca.amount::text
		FROM crm.credit_allocations ca JOIN crm.object_records n ON n.id = ca.credit_note_id AND n.deleted_at IS NULL AND n.status IN ('issued', 'partially_applied', 'applied')
		WHERE ca.invoice_id = $1 AND ca.workspace_id = $2
		UNION ALL
		SELECT 'debit_note', d.id::text, d.code, d.name, COALESCE(d.status, ''), `+money(`d.custom->>'total'`)+`::text
		FROM crm.object_records d WHERE d.workspace_id = $2 AND d.object_key = 'debit_notes' AND d.deleted_at IS NULL AND d.status = 'issued' AND d.custom->>'invoiceId' = $1::text
		UNION ALL
		SELECT 'adjustment', x.id::text, x.code, x.name, COALESCE(x.status, ''), `+money(`x.custom->>'amount'`)+`::text
		FROM crm.object_records x WHERE x.workspace_id = $2 AND x.object_key = 'adjustments' AND x.deleted_at IS NULL AND x.status = 'approved' AND x.custom->>'invoiceId' = $1::text
		ORDER BY 3`, invoice, ws)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var l ledgerExtra
		var amount string
		if err := rows.Scan(&l.Kind, &l.ID, &l.Code, &l.Name, &l.Status, &amount); err != nil {
			return out, err
		}
		l.Amount = mustCents(amount)
		switch l.Kind {
		case "credit_note":
			out.credited += l.Amount
		case "debit_note":
			out.debited += l.Amount
		default:
			out.writtenOff += l.Amount
		}
		out.lines = append(out.lines, l)
	}
	return out, rows.Err()
}

// invoiceBalance is what an invoice still owes, from its own derived field.
func (h *Handler) invoiceBalance(ctx context.Context, tx pgx.Tx, ws, invoice uuid.UUID) (Cents, string, error) {
	if err := h.recomputeInvoice(ctx, tx, ws, invoice); err != nil {
		return 0, "", err
	}
	var balance, account string
	err := tx.QueryRow(ctx, `SELECT `+money(`custom->>'balanceDue'`)+`::text, COALESCE(custom->>'accountId', '') FROM crm.object_records
		WHERE id = $1 AND workspace_id = $2 AND object_key = 'invoices' AND deleted_at IS NULL FOR UPDATE`, invoice, ws).Scan(&balance, &account)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, "", shared.Validation(map[string]string{"invoiceId": "Choose an invoice of this business."})
	}
	return mustCents(balance), account, err
}

// needsApproval opens an approval request when a finance document is over the limit, and
// says whether the document has to wait.
func (h *Handler) needsApproval(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, kind, object string, row *Row, amount Cents) (bool, error) {
	if a.Source == sourceApproval {
		return false, nil // this change IS the approval
	}
	need, err := h.approvalNeeded(ctx, tx, ws, kind, amount)
	if err != nil || !need.Required {
		return false, err
	}
	sc := &Scope{WS: ws}
	_ = tx.QueryRow(ctx, `SELECT code FROM crm.workspaces WHERE id = $1`, ws).Scan(&sc.Code)
	st, err := h.requestApproval(ctx, tx, sc, a, kind, object, row.uuid(), amount, need, specFor(object).Singular+" "+row.Code+" · "+amount.String()+" · "+row.text("reason"))
	return st == "pending", err
}

// ---------------------------------------------------------------- refunds

// refundCounts: refunds that hold money back from a payment (a pending one reserves it).
const refundOpen = `('pending', 'processing', 'succeeded')`

func (h *Handler) refundSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, op string, before, after *Row) (*Row, error) {
	spec := specFor("refunds")
	payment := after.id("paymentId")
	if payment == uuid.Nil {
		return nil, shared.Validation(map[string]string{"paymentId": "Choose the payment being refunded."})
	}
	if before != nil && before.id("paymentId") != payment && before.id("paymentId") != uuid.Nil {
		return nil, shared.Validation(map[string]string{"paymentId": "A refund stays with its payment. Cancel this one and make a new refund."})
	}
	// Lock the payment: two refunds at once must not both fit.
	var payAmount, payStatus, payAccount, payCurrency string
	err := tx.QueryRow(ctx, `SELECT `+money(`custom->>'amount'`)+`::text, COALESCE(status, ''), COALESCE(custom->>'accountId', ''), COALESCE(custom->>'currency', '')
		FROM crm.object_records WHERE id = $1 AND workspace_id = $2 AND object_key = 'payments' AND deleted_at IS NULL FOR UPDATE`, payment, ws).Scan(&payAmount, &payStatus, &payAccount, &payCurrency)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.Validation(map[string]string{"paymentId": "Choose a payment of this business."})
	}
	if err != nil {
		return nil, err
	}
	amount, ok := parseCents(after.Values["amount"])
	status := after.text("status")
	set := map[string]any{}
	if op == "create" || op == "update" || op == "restore" {
		if !ok || amount <= 0 {
			return nil, shared.Validation(map[string]string{"amount": "Enter the amount refunded."})
		}
		live := status == "" || status == "pending" || status == "processing" || status == "succeeded"
		if live {
			if payStatus != "paid" && payStatus != "partially_refunded" && payStatus != "refunded" {
				return nil, shared.NewError(http.StatusUnprocessableEntity, "not_refundable", "Only a payment that was received can be refunded.")
			}
			var others string
			if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(`+money(`custom->>'amount'`)+`), 0)::text FROM crm.object_records
				WHERE workspace_id = $1 AND object_key = 'refunds' AND deleted_at IS NULL AND id <> $2 AND custom->>'paymentId' = $3 AND COALESCE(status, 'succeeded') IN `+refundOpen,
				ws, after.uuid(), payment.String()).Scan(&others); err != nil {
				return nil, err
			}
			if amount+mustCents(others) > mustCents(payAmount) {
				return nil, shared.Validation(map[string]string{"amount": "Enter an amount up to what is left of this payment (" + (mustCents(payAmount) - mustCents(others)).String() + ")."})
			}
		}
		if after.text("accountId") == "" && payAccount != "" {
			set["accountId"] = payAccount
		}
		if after.text("currency") == "" && payCurrency != "" {
			set["currency"] = payCurrency
		}
		// A refund that is new, or whose amount went up, may need approval before it counts.
		grew := op == "create" || (before != nil && mustCents(before.Values["amount"]) < amount)
		if grew && (status == "" || status == "succeeded" || status == "processing") {
			wait, err := h.needsApproval(ctx, tx, ws, a, "refund", "refunds", after, amount)
			if err != nil {
				return nil, err
			}
			if wait {
				set["status"] = "pending"
			} else if status == "" {
				set["status"] = "succeeded"
			}
		} else if status == "" {
			set["status"] = "succeeded"
		}
	}
	row := after
	if len(set) > 0 {
		if row, err = h.updateValues(ctx, tx, ws, spec, after.uuid(), systemActor(sourceFinance), set, nil, nil); err != nil {
			return nil, err
		}
	}
	if err := h.applyRefunds(ctx, tx, ws, payment, a); err != nil {
		return nil, err
	}
	return row, nil
}

// applyRefunds sets a payment's refunded amount and status from its refund records, then
// re-derives the invoices it paid.
func (h *Handler) applyRefunds(ctx context.Context, tx pgx.Tx, ws, payment uuid.UUID, a actorInfo) error {
	var amount, was, refunded, status string
	err := tx.QueryRow(ctx, `
		SELECT `+money(`p.custom->>'amount'`)+`::text, `+money(`p.custom->>'refundedAmount'`)+`::text, COALESCE(p.status, ''),
		       COALESCE((SELECT sum(`+money(`r.custom->>'amount'`)+`) FROM crm.object_records r
		                 WHERE r.workspace_id = p.workspace_id AND r.object_key = 'refunds' AND r.deleted_at IS NULL AND r.status = 'succeeded' AND r.custom->>'paymentId' = p.id::text), 0)::text
		FROM crm.object_records p WHERE p.id = $1 AND p.workspace_id = $2 AND p.object_key = 'payments'`, payment, ws).Scan(&amount, &was, &status, &refunded)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	total, back := mustCents(amount), mustCents(refunded)
	set := map[string]any{}
	if back != mustCents(was) {
		set["refundedAmount"] = back.Float()
	}
	next := status
	switch {
	case status != "paid" && status != "partially_refunded" && status != "refunded":
	case back <= 0:
		next = "paid"
	case back >= total:
		next = "refunded"
	default:
		next = "partially_refunded"
	}
	if next != status {
		set["status"] = next
	}
	if len(set) > 0 {
		if _, err := h.updateValues(ctx, tx, ws, specFor("payments"), payment, systemActor(sourcePayments), set, nil, nil); err != nil {
			return err
		}
		if err := shared.WriteAudit(ctx, tx, a.audit(ws, "payment.refunded", "payment", &payment,
			map[string]any{"refundedAmount": was, "status": status}, map[string]any{"refundedAmount": back.String(), "status": next})); err != nil {
			return err
		}
	}
	if err := h.refreshPayment(ctx, tx, ws, payment); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT invoice_id FROM crm.payment_allocations WHERE payment_id = $1 AND workspace_id = $2`, payment, ws)
	if err != nil {
		return err
	}
	var invoices []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		invoices = append(invoices, id)
	}
	rows.Close()
	for _, inv := range invoices {
		if err := h.recomputeInvoice(ctx, tx, ws, inv); err != nil {
			return err
		}
	}
	return nil
}

// rebuildRefundAllocations works out which invoices a payment's refunds come off: the part
// of the payment not applied to any invoice goes back first, then the invoices it paid,
// the most recently applied first.
func (h *Handler) rebuildRefundAllocations(ctx context.Context, tx pgx.Tx, ws, payment uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM crm.refund_allocations WHERE payment_id = $1 AND workspace_id = $2`, payment, ws); err != nil {
		return err
	}
	var amount string
	if err := tx.QueryRow(ctx, `SELECT `+money(`custom->>'amount'`)+`::text FROM crm.object_records WHERE id = $1 AND workspace_id = $2`, payment, ws).Scan(&amount); err != nil {
		return nil
	}
	type alloc struct {
		invoice uuid.UUID
		left    Cents
	}
	var allocs []alloc
	var applied Cents
	rows, err := tx.Query(ctx, `SELECT invoice_id, amount::text FROM crm.payment_allocations WHERE payment_id = $1 AND workspace_id = $2 ORDER BY created_at DESC, id`, payment, ws)
	if err != nil {
		return err
	}
	for rows.Next() {
		var x alloc
		var v string
		if err := rows.Scan(&x.invoice, &v); err != nil {
			rows.Close()
			return err
		}
		x.left = mustCents(v)
		applied += x.left
		allocs = append(allocs, x)
	}
	rows.Close()
	if len(allocs) == 0 {
		return nil
	}
	type refund struct {
		id     uuid.UUID
		amount Cents
	}
	var refunds []refund
	rows, err = tx.Query(ctx, `SELECT id, `+money(`custom->>'amount'`)+`::text FROM crm.object_records
		WHERE workspace_id = $1 AND object_key = 'refunds' AND deleted_at IS NULL AND status = 'succeeded' AND custom->>'paymentId' = $2 ORDER BY created_at, id`, ws, payment.String())
	if err != nil {
		return err
	}
	for rows.Next() {
		var x refund
		var v string
		if err := rows.Scan(&x.id, &v); err != nil {
			rows.Close()
			return err
		}
		x.amount = mustCents(v)
		refunds = append(refunds, x)
	}
	rows.Close()
	unapplied := mustCents(amount) - applied
	for _, rf := range refunds {
		rest := rf.amount
		if unapplied > 0 {
			take := min(rest, unapplied)
			unapplied -= take
			rest -= take
		}
		for i := range allocs {
			if rest <= 0 {
				break
			}
			take := min(rest, allocs[i].left)
			if take <= 0 {
				continue
			}
			allocs[i].left -= take
			rest -= take
			if _, err := tx.Exec(ctx, `INSERT INTO crm.refund_allocations (workspace_id, refund_id, payment_id, invoice_id, amount) VALUES ($1, $2, $3, $4, $5::numeric)
				ON CONFLICT (refund_id, invoice_id) DO UPDATE SET amount = crm.refund_allocations.amount + EXCLUDED.amount`, ws, rf.id, payment, allocs[i].invoice, take.String()); err != nil {
				return err
			}
		}
	}
	return nil
}

// BackfillRefunds (once): a payment that carried a refunded amount from before refund
// records existed gets one refund record for it. The amount comes from the payment.
func (h *Handler) BackfillRefunds(ctx context.Context) {
	const marker = "refunds:from-payments-v1"
	var done bool
	if err := h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.connector_state WHERE key = $1)`, marker).Scan(&done); err != nil || done || specFor("refunds") == nil {
		return
	}
	made := 0
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `
			SELECT p.workspace_id, p.id::text, p.code, `+money(`p.custom->>'refundedAmount'`)+`::text, COALESCE(p.custom->>'paymentDate', ''), p.owner_id
			FROM crm.object_records p
			WHERE p.object_key = 'payments' AND p.deleted_at IS NULL AND `+money(`p.custom->>'refundedAmount'`)+` > 0
			  AND NOT EXISTS (SELECT 1 FROM crm.object_records r WHERE r.workspace_id = p.workspace_id AND r.object_key = 'refunds' AND r.custom->>'paymentId' = p.id::text)`)
		if err != nil {
			return err
		}
		type item struct {
			ws                     uuid.UUID
			id, code, amount, date string
			owner                  *uuid.UUID
		}
		var list []item
		for rows.Next() {
			var x item
			if err := rows.Scan(&x.ws, &x.id, &x.code, &x.amount, &x.date, &x.owner); err != nil {
				rows.Close()
				return err
			}
			list = append(list, x)
		}
		rows.Close()
		for _, x := range list {
			date := x.date
			if len(date) < 10 {
				date = time.Now().Format("2006-01-02")
			}
			v := map[string]any{"name": "Refund of " + x.code, "paymentId": x.id, "amount": mustCents(x.amount).Float(), "refundDate": date[:10], "status": "succeeded",
				"reason": "Recorded on the payment before refunds had their own records"}
			if x.owner != nil {
				v["ownerId"] = x.owner.String()
			}
			// Written as the approval system so an old refund isn't sent for approval now.
			if _, err := h.createRecord(ctx, tx, x.ws, specFor("refunds"), systemActor(sourceApproval), v); err != nil {
				return err
			}
			made++
		}
		_, err = tx.Exec(ctx, `INSERT INTO crm.connector_state (key, value) VALUES ($1, jsonb_build_object('refunds', $2::int)) ON CONFLICT (key) DO NOTHING`, marker, made)
		return err
	})
	if err != nil {
		slog.Error("crm: refund records not created", "error", err)
	}
}

// ---------------------------------------------------------------- credit notes, debit notes, adjustments

// financeDocSaved handles the three documents that change an invoice's balance without
// being a payment.
func (h *Handler) financeDocSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, a actorInfo, op string, before, after *Row) (*Row, error) {
	id := after.uuid()
	invoice := after.id("invoiceId")
	amountKey, kind, live := "total", "credit_note", "issued"
	switch spec.Key {
	case "debit_notes":
		kind = "debit_note"
	case "adjustments":
		amountKey, kind, live = "amount", "write_off", "approved"
	}
	status := after.text("status")
	set := map[string]any{}
	touched := map[uuid.UUID]bool{}
	if before != nil {
		if old := before.id("invoiceId"); old != uuid.Nil {
			touched[old] = true
		}
	}
	if op == "delete" {
		if spec.Key == "credit_notes" {
			if err := h.dropCreditAllocations(ctx, tx, ws, id, touched); err != nil {
				return nil, err
			}
		}
	} else {
		amount, ok := parseCents(after.Values[amountKey])
		if !ok || amount <= 0 {
			return nil, shared.Validation(map[string]string{amountKey: "Enter an amount above 0."})
		}
		if status == "" {
			status = "draft"
			set["status"] = "draft"
		}
		var invoiceAccount string
		if invoice != uuid.Nil {
			var invStatus string
			err := tx.QueryRow(ctx, `SELECT COALESCE(custom->>'accountId', ''), COALESCE(status, '') FROM crm.object_records
				WHERE id = $1 AND workspace_id = $2 AND object_key = 'invoices' AND deleted_at IS NULL`, invoice, ws).Scan(&invoiceAccount, &invStatus)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, shared.Validation(map[string]string{"invoiceId": "Choose an invoice of this business."})
			}
			if err != nil {
				return nil, err
			}
			if after.text("accountId") == "" && invoiceAccount != "" {
				set["accountId"] = invoiceAccount
			} else if invoiceAccount != "" && after.text("accountId") != invoiceAccount {
				return nil, shared.Validation(map[string]string{"invoiceId": "This invoice belongs to another account."})
			}
			if spec.Key != "credit_notes" && status == live && (invStatus == "draft" || invStatus == "void") {
				return nil, shared.Validation(map[string]string{"invoiceId": "The invoice is a draft or void. Change the invoice itself instead."})
			}
			touched[invoice] = true
		} else if spec.Key != "credit_notes" {
			return nil, shared.Validation(map[string]string{"invoiceId": "Choose the invoice this is for."})
		} else if after.text("accountId") == "" {
			return nil, shared.Validation(map[string]string{"accountId": "Choose the customer, or the invoice the credit is for."})
		}
		// Becoming live — issued, or approved for an adjustment — may need approval first.
		becameLive := status == live && (before == nil || before.text("status") != live || mustCents(before.Values[amountKey]) < amount)
		if becameLive {
			wait, err := h.needsApproval(ctx, tx, ws, a, kind, spec.Key, after, amount)
			if err != nil {
				return nil, err
			}
			if wait {
				status = "pending_approval"
				set["status"] = status
			}
		}
		// An adjustment can't take off more than the invoice owes.
		if spec.Key == "adjustments" && status == live && becameLive {
			balance, _, err := h.invoiceBalance(ctx, tx, ws, invoice)
			if err != nil {
				return nil, err
			}
			if amount > balance {
				return nil, shared.Validation(map[string]string{"amount": "The invoice only owes " + balance.String() + "."})
			}
		}
		if spec.Key == "credit_notes" {
			if status == "cancelled" || status == "draft" || status == "pending_approval" {
				if err := h.dropCreditAllocations(ctx, tx, ws, id, touched); err != nil {
					return nil, err
				}
			} else {
				var applied string
				if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(amount), 0)::text FROM crm.credit_allocations WHERE credit_note_id = $1 AND workspace_id = $2`, id, ws).Scan(&applied); err != nil {
					return nil, err
				}
				if mustCents(applied) > amount {
					return nil, shared.Validation(map[string]string{"total": "This is less than what has already been applied to invoices (" + applied + ")."})
				}
				// Newly issued with an invoice named: apply as much as that invoice owes.
				if becameLive && invoice != uuid.Nil && mustCents(applied) == 0 {
					balance, _, err := h.invoiceBalance(ctx, tx, ws, invoice)
					if err != nil {
						return nil, err
					}
					if take := min(amount, balance); take > 0 {
						if _, err := tx.Exec(ctx, `INSERT INTO crm.credit_allocations (workspace_id, credit_note_id, invoice_id, amount, created_by) VALUES ($1, $2, $3, $4::numeric, $5)
							ON CONFLICT (credit_note_id, invoice_id) DO NOTHING`, ws, id, invoice, take.String(), a.ID); err != nil {
							return nil, err
						}
					}
				}
			}
		}
	}
	row := after
	if len(set) > 0 && op != "delete" {
		var err error
		if row, err = h.updateValues(ctx, tx, ws, spec, id, systemActor(sourceFinance), set, nil, nil); err != nil {
			return nil, err
		}
	}
	if spec.Key == "credit_notes" && op != "delete" {
		var err error
		if row, err = h.refreshCreditNote(ctx, tx, ws, id); err != nil {
			return nil, err
		}
	}
	for inv := range touched {
		if err := h.recomputeInvoice(ctx, tx, ws, inv); err != nil {
			return nil, err
		}
	}
	return row, nil
}

func (h *Handler) dropCreditAllocations(ctx context.Context, tx pgx.Tx, ws, note uuid.UUID, touched map[uuid.UUID]bool) error {
	rows, err := tx.Query(ctx, `DELETE FROM crm.credit_allocations WHERE credit_note_id = $1 AND workspace_id = $2 RETURNING invoice_id`, note, ws)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return err
		}
		touched[id] = true
	}
	return rows.Err()
}

// refreshCreditNote writes a credit note's applied and remaining amounts and its status.
func (h *Handler) refreshCreditNote(ctx context.Context, tx pgx.Tx, ws, id uuid.UUID) (*Row, error) {
	spec := specFor("credit_notes")
	row, _, err := h.getRow(ctx, tx, ws, spec, id, nil)
	if err != nil {
		return nil, err
	}
	var appliedText string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(amount), 0)::text FROM crm.credit_allocations WHERE credit_note_id = $1 AND workspace_id = $2`, id, ws).Scan(&appliedText); err != nil {
		return nil, err
	}
	total, applied := mustCents(row.Values["total"]), mustCents(appliedText)
	set := map[string]any{}
	if mustCents(row.Values["appliedAmount"]) != applied || row.Values["appliedAmount"] == nil {
		set["appliedAmount"] = applied.Float()
	}
	if mustCents(row.Values["remainingAmount"]) != total-applied || row.Values["remainingAmount"] == nil {
		set["remainingAmount"] = (total - applied).Float()
	}
	if st := row.text("status"); st == "issued" || st == "partially_applied" || st == "applied" {
		next := "issued"
		if applied >= total {
			next = "applied"
		} else if applied > 0 {
			next = "partially_applied"
		}
		if next != st {
			set["status"] = next
		}
	}
	if len(set) == 0 {
		return row, nil
	}
	return h.updateValues(ctx, tx, ws, spec, id, systemActor(sourceFinance), set, nil, nil)
}

type creditAllocation struct {
	ID          string    `json:"id"`
	InvoiceID   string    `json:"invoiceId"`
	Invoice     string    `json:"invoice"`
	InvoiceCode string    `json:"invoiceCode"`
	Amount      Cents     `json:"amount"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (h *Handler) creditAllocations(ctx context.Context, q querier, ws, note uuid.UUID) ([]creditAllocation, error) {
	rows, err := q.Query(ctx, `SELECT ca.id::text, ca.invoice_id::text, COALESCE(i.name, ''), COALESCE(i.code, ''), ca.amount::text, ca.created_at
		FROM crm.credit_allocations ca LEFT JOIN crm.object_records i ON i.id = ca.invoice_id WHERE ca.credit_note_id = $1 AND ca.workspace_id = $2 ORDER BY ca.created_at`, note, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []creditAllocation{}
	for rows.Next() {
		var x creditAllocation
		var v string
		if err := rows.Scan(&x.ID, &x.InvoiceID, &x.Invoice, &x.InvoiceCode, &v, &x.CreatedAt); err != nil {
			return nil, err
		}
		x.Amount = mustCents(v)
		list = append(list, x)
	}
	return list, rows.Err()
}

func (h *Handler) handleCreditAllocations(w http.ResponseWriter, r *http.Request) {
	sc, note, err := h.documentScope(r, "credit_notes", "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := h.creditAllocations(r.Context(), h.store.Pool, sc.WS, note.uuid())
	respond(w, r, http.StatusOK, map[string]any{"data": list, "canEdit": sc.Can("credit_notes", "update")}, err)
}

func (h *Handler) handleApplyCredit(w http.ResponseWriter, r *http.Request) {
	sc, note, err := h.documentScope(r, "credit_notes", "update")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in struct {
		InvoiceID string `json:"invoiceId"`
		Amount    any    `json:"amount"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	me := actor(r)
	invoice, err := uuid.Parse(in.InvoiceID)
	amount, ok := parseCents(in.Amount)
	if err != nil || !sc.Can("invoices", "read") {
		shared.WriteError(w, r, shared.Validation(map[string]string{"invoiceId": "Choose an invoice."}))
		return
	}
	// The invoice must be one the caller can open, in this business.
	if _, _, err := h.getRow(ctx, h.store.Pool, sc.WS, specFor("invoices"), invoice, sc.OwnersFor("invoices", me)); err != nil {
		shared.WriteError(w, r, shared.Validation(map[string]string{"invoiceId": "Choose an invoice of this business."}))
		return
	}
	if !ok || amount <= 0 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"amount": "Enter an amount above 0."}))
		return
	}
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var total, status, account string
		if err := tx.QueryRow(ctx, `SELECT `+money(`custom->>'total'`)+`::text, COALESCE(status, ''), COALESCE(custom->>'accountId', '') FROM crm.object_records
			WHERE id = $1 AND workspace_id = $2 FOR UPDATE`, note.uuid(), sc.WS).Scan(&total, &status, &account); err != nil {
			return err
		}
		if status != "issued" && status != "partially_applied" {
			return shared.NewError(http.StatusUnprocessableEntity, "not_issued", "Only an issued credit note with credit left can be applied.")
		}
		var applied string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(amount), 0)::text FROM crm.credit_allocations WHERE credit_note_id = $1 AND workspace_id = $2`, note.uuid(), sc.WS).Scan(&applied); err != nil {
			return err
		}
		left := mustCents(total) - mustCents(applied)
		balance, invAccount, err := h.invoiceBalance(ctx, tx, sc.WS, invoice)
		if err != nil {
			return err
		}
		switch {
		case account != "" && invAccount != "" && account != invAccount:
			return shared.Validation(map[string]string{"invoiceId": "This invoice belongs to another account."})
		case amount > left:
			return shared.Validation(map[string]string{"amount": "Only " + left.String() + " of this credit note is left."})
		case amount > balance:
			return shared.Validation(map[string]string{"amount": "The invoice only owes " + balance.String() + "."})
		}
		if _, err := tx.Exec(ctx, `INSERT INTO crm.credit_allocations (workspace_id, credit_note_id, invoice_id, amount, created_by) VALUES ($1, $2, $3, $4::numeric, $5)
			ON CONFLICT (credit_note_id, invoice_id) DO UPDATE SET amount = crm.credit_allocations.amount + EXCLUDED.amount`, sc.WS, note.uuid(), invoice, amount.String(), me); err != nil {
			return err
		}
		if _, err := h.refreshCreditNote(ctx, tx, sc.WS, note.uuid()); err != nil {
			return err
		}
		if err := h.recomputeInvoice(ctx, tx, sc.WS, invoice); err != nil {
			return err
		}
		nid := note.uuid()
		if err := insertActivity(ctx, tx, sc.WS, "invoices", invoice, "credit.applied", "Credit applied: "+amount.String(), map[string]any{"creditNote": note.Code}, a.ID); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "credit_note.applied", "credit_note", &nid, nil, map[string]any{"invoiceId": invoice.String(), "amount": amount.String()}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.bus.Kick()
	h.handleCreditAllocations(w, r)
}

func (h *Handler) handleUnapplyCredit(w http.ResponseWriter, r *http.Request) {
	sc, note, err := h.documentScope(r, "credit_notes", "update")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	alloc, err := uuid.Parse(chi.URLParam(r, "allocationId"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("allocation_not_found"))
		return
	}
	ctx := r.Context()
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var invoice uuid.UUID
		var amount string
		err := tx.QueryRow(ctx, `DELETE FROM crm.credit_allocations WHERE id = $1 AND credit_note_id = $2 AND workspace_id = $3 RETURNING invoice_id, amount::text`, alloc, note.uuid(), sc.WS).Scan(&invoice, &amount)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("allocation_not_found")
		}
		if err != nil {
			return err
		}
		if _, err := h.refreshCreditNote(ctx, tx, sc.WS, note.uuid()); err != nil {
			return err
		}
		if err := h.recomputeInvoice(ctx, tx, sc.WS, invoice); err != nil {
			return err
		}
		nid := note.uuid()
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "credit_note.unapplied", "credit_note", &nid, map[string]any{"invoiceId": invoice.String(), "amount": amount}, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.bus.Kick()
	w.WriteHeader(http.StatusNoContent)
}

// handleAccountStatement: everything that makes up what one customer owes, as documents.
func (h *Handler) handleAccountStatement(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	ctx := r.Context()
	me := actor(r)
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || !sc.Can("accounts", "read") || !sc.Can("invoices", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	if _, _, err := h.getRow(ctx, h.store.Pool, sc.WS, specFor("accounts"), id, sc.OwnersFor("accounts", me)); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	type line struct {
		Kind   string `json:"kind"`
		ID     string `json:"id"`
		Code   string `json:"code"`
		Name   string `json:"name"`
		Status string `json:"status"`
		Date   string `json:"date"`
		Amount Cents  `json:"amount"`
		Open   Cents  `json:"open"` // still owed (invoice) or still available (payment, credit note)
	}
	out := struct {
		Currency       string `json:"currency"`
		Invoiced       Cents  `json:"invoiced"`
		Outstanding    Cents  `json:"outstanding"`
		UnappliedCash  Cents  `json:"unappliedPayments"`
		UnusedCredit   Cents  `json:"unusedCredit"`
		NetReceivable  Cents  `json:"netReceivable"`
		Lines          []line `json:"lines"`
		CanSeePayments bool   `json:"canSeePayments"`
	}{Currency: h.baseCurrency(ctx, h.store.Pool, sc.WS), Lines: []line{}, CanSeePayments: sc.Can("payments", "read")}
	parts := []string{`
		SELECT 'invoice', i.id::text, i.code, i.name, COALESCE(i.status, ''), COALESCE(i.custom->>'invoiceDate', ''), ` + money(`i.custom->>'total'`) + `::text,
		       CASE WHEN i.status IN ('draft', 'void') THEN '0' ELSE ` + money(`COALESCE(i.custom->>'balanceDue', i.custom->>'total')`) + `::text END
		FROM crm.object_records i WHERE i.workspace_id = $1 AND i.object_key = 'invoices' AND i.deleted_at IS NULL AND i.custom->>'accountId' = $2
		  AND ($3::uuid[] IS NULL OR i.owner_id = ANY($3))`}
	args := []any{sc.WS, id.String(), sc.OwnersFor("invoices", me)}
	if out.CanSeePayments {
		parts = append(parts, `
		SELECT 'payment', p.id::text, p.code, p.name, COALESCE(p.status, ''), COALESCE(p.custom->>'paymentDate', ''), `+money(`p.custom->>'amount'`)+`::text,
		       CASE WHEN p.status IN `+paymentCounts+` THEN `+money(`p.custom->>'unappliedAmount'`)+`::text ELSE '0' END
		FROM crm.object_records p WHERE p.workspace_id = $1 AND p.object_key = 'payments' AND p.deleted_at IS NULL AND p.custom->>'accountId' = $2`)
	}
	if sc.Can("credit_notes", "read") {
		parts = append(parts, `
		SELECT 'credit_note', n.id::text, n.code, n.name, COALESCE(n.status, ''), COALESCE(n.custom->>'issueDate', ''), `+money(`n.custom->>'total'`)+`::text,
		       CASE WHEN n.status IN ('issued', 'partially_applied') THEN `+money(`n.custom->>'remainingAmount'`)+`::text ELSE '0' END
		FROM crm.object_records n WHERE n.workspace_id = $1 AND n.object_key = 'credit_notes' AND n.deleted_at IS NULL AND n.custom->>'accountId' = $2`)
	}
	rows, err := h.store.Pool.Query(ctx, strings.Join(parts, " UNION ALL ")+` ORDER BY 6 DESC, 3 LIMIT 500`, args...)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var l line
		var amount, open string
		if err := rows.Scan(&l.Kind, &l.ID, &l.Code, &l.Name, &l.Status, &l.Date, &amount, &open); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		l.Amount, l.Open = mustCents(amount), mustCents(open)
		switch l.Kind {
		case "invoice":
			if l.Status != "draft" && l.Status != "void" {
				out.Invoiced += l.Amount
			}
			out.Outstanding += l.Open
		case "payment":
			out.UnappliedCash += l.Open
		default:
			out.UnusedCredit += l.Open
		}
		out.Lines = append(out.Lines, l)
	}
	out.NetReceivable = out.Outstanding - out.UnappliedCash - out.UnusedCredit
	shared.WriteJSON(w, http.StatusOK, out)
}
