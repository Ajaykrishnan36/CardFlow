package records

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The customer happy flow (D-170): a lead becomes a contact (or account) and an
// opportunity with its products; the quote is made from the opportunity; money is taken
// against the quote, and only once it is paid in full is the invoice made — from the
// accepted quote, through a sales order — with every payment applied to it and the
// opportunity won for what was invoiced. A part payment leaves the balance on the quote and
// a task to collect it.

const sourceFlow = "flow"

// quotePaid is the money that has arrived against a quote.
func quotePaid(ctx context.Context, q querier, ws uuid.UUID, quoteID uuid.UUID) (float64, error) {
	var paid float64
	err := q.QueryRow(ctx, `SELECT COALESCE(sum(`+money(`p.custom->>'amount'`)+`), 0)::float8 FROM crm.object_records p
		WHERE p.workspace_id = $1 AND p.object_key = 'payments' AND p.deleted_at IS NULL AND p.status IN `+paymentCounts+`
		  AND p.custom->>'quoteId' = $2`, ws, quoteID.String()).Scan(&paid)
	return paid, err
}

func (h *Handler) settleQuote(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, quoteID uuid.UUID) error {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM crm.object_records WHERE id = $1 AND workspace_id = $2 FOR UPDATE`, quoteID, ws); err != nil {
		return err
	}
	quote, _, err := h.getRow(ctx, tx, ws, specFor("quotes"), quoteID, nil)
	if err != nil {
		return shared.Validation(map[string]string{"quoteId": "That quote can't be found."})
	}
	switch quote.text("status") {
	case "declined", "expired":
		return shared.Validation(map[string]string{"quoteId": "This quote was declined or has expired: take the money against its latest version."})
	}
	if st := quote.text("approvalStatus"); st == "pending" || st == "rejected" {
		return shared.Validation(map[string]string{"quoteId": "The discount on this quote has not been approved yet, so money can't be taken against it."})
	}
	total := quote.num("total")
	if total <= 0 {
		return shared.Validation(map[string]string{"quoteId": "Add the products to this quote before taking money against it."})
	}
	paid, err := quotePaid(ctx, tx, ws, quoteID)
	if err != nil {
		return err
	}
	balance := math.Max(total-paid, 0)
	if _, err := tx.Exec(ctx, `UPDATE crm.object_records SET custom = custom || jsonb_build_object('amountPaid', $3::numeric, 'balanceDue', $4::numeric)
		WHERE id = $1 AND workspace_id = $2`, quoteID, ws, math.Round(paid*100)/100, math.Round(balance*100)/100); err != nil {
		return err
	}
	sc := &Scope{WS: ws}
	act := actorInfo{ID: a.ID, Kind: a.Kind, Source: sourceFlow}
	if act.Kind == "" {
		act = systemActor(sourceFlow)
	}
	if err := h.applyPlan(ctx, tx, ws, act, quoteID, paid); err != nil {
		return err
	}
	opp := quote.id("opportunityId")
	if paid+0.005 < total {
		// Part paid: the quote is accepted (the customer has committed) and someone collects the rest.
		if quote.text("status") == "" || quote.text("status") == "draft" || quote.text("status") == "sent" {
			if _, err := h.updateValues(ctx, tx, ws, specFor("quotes"), quoteID, act, map[string]any{"status": "accepted"}, nil, nil); err != nil {
				return err
			}
		}
		return h.balanceTask(ctx, tx, ws, quote, total-paid)
	}

	// Paid in full: order (if none yet) → invoice → payments applied → deal won.
	var orderID, invoiceID string
	_ = tx.QueryRow(ctx, `SELECT id::text FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'sales_orders' AND deleted_at IS NULL
		AND custom->>'quoteId' = $2 AND COALESCE(status, '') NOT IN ('cancelled') ORDER BY created_at LIMIT 1`, ws, quoteID.String()).Scan(&orderID)
	_ = tx.QueryRow(ctx, `SELECT id::text FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'invoices' AND deleted_at IS NULL
		AND custom->>'quoteId' = $2 AND COALESCE(status, '') NOT IN ('void', 'cancelled') ORDER BY created_at LIMIT 1`, ws, quoteID.String()).Scan(&invoiceID)
	today := time.Now().In(ist).Format("2006-01-02")
	if orderID == "" && specFor("sales_orders") != nil {
		order, err := h.copyDocument(ctx, tx, sc, act, "quotes", quote, "sales_orders", map[string]any{"quoteId": quote.ID, "status": "confirmed", "orderDate": today})
		if err != nil {
			return err
		}
		orderID = order.ID
	}
	if invoiceID == "" {
		extra := map[string]any{"quoteId": quote.ID, "status": "draft", "invoiceDate": today, "dueDate": today}
		from, src := "quotes", quote
		if orderID != "" {
			extra["orderId"] = orderID
			if o, _, err := h.getRow(ctx, tx, ws, specFor("sales_orders"), uuid.MustParse(orderID), nil); err == nil {
				from, src = "sales_orders", o
			}
		}
		inv, err := h.copyDocument(ctx, tx, sc, act, from, src, "invoices", extra)
		if err != nil {
			return err
		}
		if _, err := h.updateValues(ctx, tx, ws, specFor("invoices"), inv.uuid(), act, map[string]any{"status": "sent"}, nil, nil); err != nil {
			return err
		}
		invoiceID = inv.ID
	}
	if quote.text("status") != "accepted" {
		if _, err := h.updateValues(ctx, tx, ws, specFor("quotes"), quoteID, act, map[string]any{"status": "accepted"}, nil, nil); err != nil {
			return err
		}
	}
	// Every payment taken against the quote now pays the invoice.
	rows, err := tx.Query(ctx, `SELECT id FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'payments' AND deleted_at IS NULL
		AND custom->>'quoteId' = $2 AND COALESCE(custom->>'invoiceId', '') = '' ORDER BY created_at`, ws, quoteID.String())
	if err != nil {
		return err
	}
	var pays []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			pays = append(pays, id)
		}
	}
	rows.Close()
	for _, id := range pays {
		if _, err := h.updateValues(ctx, tx, ws, specFor("payments"), id, act, map[string]any{"invoiceId": invoiceID}, nil, nil); err != nil {
			return err
		}
	}
	if opp != uuid.Nil {
		inv, _, err := h.getRow(ctx, tx, ws, specFor("invoices"), uuid.MustParse(invoiceID), nil)
		if err != nil {
			return err
		}
		o, _, err := h.getRow(ctx, tx, ws, specFor("opportunities"), opp, nil)
		if err == nil && o.text("status") != "closed_won" && o.text("status") != "closed_lost" {
			if _, err := h.updateValues(ctx, tx, ws, specFor("opportunities"), opp, act,
				map[string]any{"status": "closed_won", "amount": inv.num("total"), "closeDate": today}, nil, nil); err != nil {
				return err
			}
		}
	}
	// The balance task is done.
	_, err = tx.Exec(ctx, `UPDATE crm.object_records SET status = 'completed', updated_at = now() WHERE workspace_id = $1 AND object_key = 'tasks'
		AND deleted_at IS NULL AND custom->>'quoteId' = $2 AND COALESCE(status, '') NOT IN ('completed', 'deferred')`, ws, quoteID.String())
	return err
}

// balanceTask keeps one open "collect the balance" task per quote, with the amount still due.
func (h *Handler) balanceTask(ctx context.Context, tx pgx.Tx, ws uuid.UUID, quote *Row, due float64) error {
	tasks := specFor("tasks")
	if tasks == nil {
		return nil
	}
	who := h.labelOf(ctx, tx, ws, "contacts", quote.text("contactId"))
	if who == "" {
		who = h.labelOf(ctx, tx, ws, "accounts", quote.text("accountId"))
	}
	name := strings.TrimSpace(fmt.Sprintf("Collect the balance %s from %s", rupeesText(due), firstNonEmpty(who, quote.Title)))
	var open string
	_ = tx.QueryRow(ctx, `SELECT id::text FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'tasks' AND deleted_at IS NULL
		AND custom->>'quoteId' = $2 AND COALESCE(status, '') NOT IN ('completed', 'deferred') LIMIT 1`, ws, quote.ID).Scan(&open)
	if open != "" {
		_, err := h.updateValues(ctx, tx, ws, tasks, uuid.MustParse(open), systemActor(sourceFlow), map[string]any{"name": name}, nil, nil)
		return err
	}
	v := map[string]any{"name": name, "status": "not_started", "priority": "high", "dueDate": time.Now().In(ist).AddDate(0, 0, 2).Format("2006-01-02"), "quoteId": quote.ID}
	for _, k := range []string{"contactId", "accountId", "opportunityId"} {
		if x := quote.text(k); x != "" {
			v[k] = x
		}
	}
	if owner := quote.id("ownerId"); owner != uuid.Nil {
		v["ownerId"] = owner.String()
	}
	_, err := h.createRecord(ctx, tx, ws, tasks, systemActor(sourceFlow), v)
	return err
}

// POST /w/{code}/quotes/{id}/payment-plan {"instalments": 6, "firstDue": "2026-12-03", "everyDays": 30}
// splits what is still due on a quote into instalments (D-171). Money taken against the
// quote pays them in date order.
func (h *Handler) handlePaymentPlan(w http.ResponseWriter, r *http.Request) {
	sc, quote, err := h.documentScope(r, "quotes", "update")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	spec := specFor("instalments")
	if spec == nil || !sc.Enabled("instalments") || !sc.Can("instalments", "create") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	var in struct {
		Instalments int    `json:"instalments"`
		FirstDue    string `json:"firstDue"`
		EveryDays   int    `json:"everyDays"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	fe := map[string]string{}
	if in.Instalments < 2 || in.Instalments > 60 {
		fe["instalments"] = "Split it into 2 to 60 instalments."
	}
	first, perr := time.Parse("2006-01-02", in.FirstDue)
	if perr != nil {
		fe["firstDue"] = "Pick the date of the first instalment."
	}
	if in.EveryDays == 0 {
		in.EveryDays = 30
	}
	if in.EveryDays < 1 || in.EveryDays > 366 {
		fe["everyDays"] = "Instalments are 1 to 366 days apart."
	}
	switch quote.text("status") {
	case "declined", "expired":
		fe["quote"] = "This quote was declined or has expired."
	}
	if st := quote.text("approvalStatus"); st == "pending" || st == "rejected" {
		fe["quote"] = "The discount on this quote has not been approved yet."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	ctx := r.Context()
	a := actorFromRequest(r, "ui")
	var made int
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT 1 FROM crm.object_records WHERE id = $1 FOR UPDATE`, quote.uuid()); err != nil {
			return err
		}
		var open int
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'instalments' AND deleted_at IS NULL
			AND custom->>'quoteId' = $2 AND COALESCE(status, '') <> 'waived'`, sc.WS, quote.ID).Scan(&open)
		if open > 0 {
			return shared.NewError(http.StatusConflict, "plan_exists", "This quote already has a payment plan. Open Payment plans to change it.")
		}
		paid, err := quotePaid(ctx, tx, sc.WS, quote.uuid())
		if err != nil {
			return err
		}
		due := math.Round((quote.num("total")-paid)*100) / 100
		if due <= 0 {
			return shared.Validation(map[string]string{"quote": "Nothing is left to pay on this quote."})
		}
		who := h.labelOf(ctx, tx, sc.WS, "contacts", quote.text("contactId"))
		if who == "" {
			who = h.labelOf(ctx, tx, sc.WS, "accounts", quote.text("accountId"))
		}
		each := math.Floor(due/float64(in.Instalments)*100) / 100
		for k := 1; k <= in.Instalments; k++ {
			amt := each
			if k == in.Instalments {
				amt = math.Round((due-each*float64(in.Instalments-1))*100) / 100
			}
			v := map[string]any{"name": joinNonEmpty(" — ", fmt.Sprintf("Instalment %d of %d", k, in.Instalments), who),
				"milestone": fmt.Sprintf("%d of %d", k, in.Instalments), "dueDate": first.AddDate(0, 0, (k-1)*in.EveryDays).Format("2006-01-02"),
				"amount": amt, "status": "due", "quoteId": quote.ID}
			for _, f := range []string{"contactId", "accountId", "opportunityId"} {
				if x := quote.text(f); x != "" {
					v[f] = x
				}
			}
			if o := quote.id("ownerId"); o != uuid.Nil {
				v["ownerId"] = o.String()
			}
			if _, err := h.createRecord(ctx, tx, sc.WS, spec, a, v); err != nil {
				return err
			}
			made++
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "quote.payment_plan", "quote", ptrUUID(quote.uuid()), nil,
			map[string]any{"instalments": in.Instalments, "amount": due, "firstDue": in.FirstDue, "everyDays": in.EveryDays}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.bus.Kick()
	shared.WriteJSON(w, http.StatusCreated, map[string]any{"instalments": made})
}

func ptrUUID(id uuid.UUID) *uuid.UUID { return &id }

// applyPlan marks a quote's instalments paid, in date order, for the money taken so far.
func (h *Handler) applyPlan(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, quoteID uuid.UUID, paid float64) error {
	spec := specFor("instalments")
	if spec == nil {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT id, `+money(`custom->>'amount'`)+`::float8, COALESCE(status, ''), `+money(`custom->>'paidAmount'`)+`::float8
		FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'instalments' AND deleted_at IS NULL AND custom->>'quoteId' = $2
		  AND COALESCE(status, '') <> 'waived' ORDER BY custom->>'dueDate', created_at`, ws, quoteID.String())
	if err != nil {
		return err
	}
	type inst struct {
		id         uuid.UUID
		amount     float64
		status     string
		paidBefore float64
	}
	var list []inst
	for rows.Next() {
		var x inst
		if rows.Scan(&x.id, &x.amount, &x.status, &x.paidBefore) == nil {
			list = append(list, x)
		}
	}
	rows.Close()
	today := time.Now().In(ist).Format("2006-01-02")
	left := paid
	for _, x := range list {
		got := math.Min(left, x.amount)
		left -= got
		status := x.status
		switch {
		case got+0.005 >= x.amount:
			status = "paid"
		case got > 0:
			status = "part_paid"
		case x.status != "overdue":
			status = "due"
		}
		if status == x.status && math.Abs(got-x.paidBefore) < 0.005 {
			continue
		}
		set := map[string]any{"status": status, "paidAmount": math.Round(got*100) / 100}
		if status == "paid" {
			set["paidOn"] = today
		}
		if _, err := h.updateValues(ctx, tx, ws, spec, x.id, a, set, nil, nil); err != nil {
			return err
		}
	}
	return nil
}
