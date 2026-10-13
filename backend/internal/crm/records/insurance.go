package records

import (
	"context"
	"fmt"
	"math"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The insurance pack (D-148), from the Insurance Agent use-case catalogue: an agent's
// book is people, policies, dates and money.
//
//	insurance_policies  one policy term: insurer, plan, premium, commission, how it is collected
//	premium_dues        the premium schedule, one row per instalment
//	commissions         what the insurer owes the agent per policy year, and what arrived
//
// The agent types the premium and the commission once. The CRM shows what the customer
// pays, what goes to the insurer and what the agent keeps; builds the premium schedule
// and the expected commission when the policy is issued; and keeps the renewal alive.
//
// Two ways of collecting, per policy: the customer pays the insurer directly, or the
// agent collects and remits. Money collected for the insurer is never the agent's income:
// it shows as an amount to remit until it is passed on.

const sourceInsurance = "insurance"

func insuranceObjects() []ObjectDefinition {
	return []ObjectDefinition{
		{Key: "insurance_policies", Module: "insurance", Singular: "Policy", Plural: "Policies", Icon: "file-text", Prefix: "POL",
			Description: "Each policy term: who is covered, insurer and plan, premium, commission, dates and how the premium is collected.",
			ObjectBody: ObjectBody{NameLabel: "Policy", StatusLabel: "Status", Statuses: []StatusOption{
				st("proposal", "Proposal", "neutral"), st("in_force", "In force", "success"), st("lapsed", "Lapsed", "danger"),
				st("renewed", "Renewed", "primary"), st("cancelled", "Cancelled", "danger"), st("matured", "Matured / expired", "neutral")},
				Fields: []ObjectField{
					ofReq(ofLookup("contactId", "Policyholder", "contacts")), ofLookup("accountId", "Household / company", "accounts"),
					ofReq(ofLookup("insurerId", "Insurer", "accounts")), ofLookup("itemId", "Plan", "catalog_items"),
					ofSelect("policyClass", "Class", opts("life", "Life", "health", "Health", "motor", "Motor", "general", "General")),
					{Key: "policyNumber", Label: "Policy number", Type: "text", Unique: true},
					of("sumAssured", "Sum assured / cover", "currency"), of("startDate", "Starts", "date"), of("endDate", "Ends / renewal date", "date"),
					ofSelect("frequency", "Premium paid", opts("yearly", "Yearly", "half_yearly", "Half-yearly", "quarterly", "Quarterly", "monthly", "Monthly", "single", "Single premium")),
					of("premium", "Premium for the year, before tax", "currency"), of("taxOnPremium", "Tax on premium", "currency"), of("agentFee", "Your fee to the customer", "currency"),
					of("totalToCollect", "Customer pays in the year", "currency"),
					ofSelect("collectionMode", "Premium is paid", opts("direct", "By the customer to the insurer", "agent", "To us, and we remit")),
					of("commissionRate", "Commission rate", "percent"), of("commissionAmount", "Commission for the year", "currency"),
					of("directCosts", "Direct costs (sub-agent share, charges)", "currency"), of("profit", "Your profit for the year", "currency"),
					of("amountToRemit", "To remit to the insurer in the year", "currency"), of("graceDays", "Grace period (days)", "number"),
					of("nominee", "Nominee", "text"), ofLookup("assetId", "Insured vehicle or item", "assets"),
					ofLookup("previousPolicyId", "Renewal of", "insurance_policies"),
					ofSelect("cancellationReason", "Why it was cancelled", opts("free_look", "Free-look cancellation", "customer", "Customer's request", "non_payment", "Premium not paid",
						"switched", "Moved to another insurer", "other", "Other")),
					of("renewalRung", "Last renewal reminder (days before)", "number"), of("notes", "Notes", "textarea")}}},
		{Key: "premium_dues", Module: "insurance", Singular: "Premium due", Plural: "Premium schedule", Icon: "calendar", Prefix: "PRM",
			Description: "Every premium instalment: when it is due, whether it was paid, and — when you collect it — when it was remitted.",
			ObjectBody: ObjectBody{NameLabel: "Premium", StatusLabel: "Status", Statuses: []StatusOption{
				st("due", "Due", "primary"), st("paid", "Paid", "success"), st("overdue", "Overdue", "warning"),
				st("lapsed", "Lapsed", "danger"), st("cancelled", "Cancelled", "neutral")},
				Fields: []ObjectField{
					ofReq(ofLookup("policyId", "Policy", "insurance_policies")), ofLookup("contactId", "Policyholder", "contacts"),
					of("instalmentNumber", "Instalment", "number"), of("dueDate", "Due date", "date"), of("amount", "Amount", "currency"),
					of("paidOn", "Paid on", "date"), of("paidAmount", "Amount paid", "currency"),
					ofSelect("paymentMode", "Paid by", opts("upi", "UPI", "bank", "Bank transfer", "card", "Card", "cheque", "Cheque", "cash", "Cash", "mandate", "Auto-debit")),
					of("paymentReference", "Payment reference", "text"),
					of("remittedOn", "Remitted to insurer on", "date"), of("remittanceReference", "Remittance reference", "text"),
					of("notes", "Notes", "textarea")}}},
		{Key: "commissions", Module: "insurance", Singular: "Commission", Plural: "Commissions", Icon: "banknote", Prefix: "CMS",
			Description: "What each insurer owes you per policy and year, what actually arrived, and the difference.",
			ObjectBody: ObjectBody{NameLabel: "Commission", StatusLabel: "Status", Statuses: []StatusOption{
				st("expected", "Expected", "primary"), st("received", "Received", "success"), st("short_paid", "Short paid", "warning"),
				st("clawback", "Clawed back", "danger"), st("written_off", "Not coming", "neutral")},
				Fields: []ObjectField{
					ofReq(ofLookup("policyId", "Policy", "insurance_policies")), ofLookup("insurerId", "Insurer", "accounts"),
					of("policyYear", "Policy year", "number"),
					ofSelect("kind", "Kind", opts("first_year", "First year", "renewal", "Renewal", "incentive", "Incentive / bonus", "clawback", "Clawback")),
					of("expectedOn", "Expected by", "date"), of("expectedAmount", "Expected", "currency"),
					of("receivedAmount", "Received", "currency"), of("receivedOn", "Received on", "date"), of("tdsDeducted", "TDS deducted", "currency"),
					of("statementReference", "Insurer statement reference", "text"), of("variance", "Difference", "currency"),
					of("notes", "Notes", "textarea")}}},
	}
}

func round2(n float64) float64 { return math.Round(n*100) / 100 }

func (h *Handler) insuranceSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, a actorInfo, op string, before, after *Row) (*Row, error) {
	if a.Source == sourceInsurance {
		return after, nil
	}
	switch spec.Key {
	case "insurance_policies":
		return h.policySaved(ctx, tx, ws, op, before, after)
	case "premium_dues":
		return h.premiumSaved(ctx, tx, ws, op, before, after)
	case "commissions":
		return h.commissionSaved(ctx, tx, ws, op, before, after)
	}
	return after, nil
}

// instalmentsOf is how many premiums a year a frequency means.
func instalmentsOf(frequency string) int {
	switch frequency {
	case "half_yearly":
		return 2
	case "quarterly":
		return 4
	case "monthly":
		return 12
	}
	return 1
}

// policySaved keeps the policy's sums right and, on issue, builds what follows from it.
func (h *Handler) policySaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, after *Row) (*Row, error) {
	spec := specFor("insurance_policies")
	sys := systemActor(sourceInsurance)
	status, was := after.text("status"), before.text("status")
	if s, e := after.text("startDate"), after.text("endDate"); s != "" && e != "" && e < s {
		return nil, refuse("endDate", "A policy can't end before it starts.")
	}
	if op == "update" && (was == "cancelled" || was == "renewed") && status != was {
		return nil, refuse("status", "This policy term is closed. A new term is a new policy.")
	}
	set := map[string]any{}
	// What the customer pays, what the agent earns, what goes to the insurer.
	premium, tax, fee := after.num("premium"), after.num("taxOnPremium"), after.num("agentFee")
	if op == "create" && !after.has("commissionRate") && !after.has("commissionAmount") {
		if rate, ok := h.ruleRate(ctx, tx, ws, after.Values, h.policyYearOf(ctx, tx, ws, after.Values["previousPolicyId"])); ok {
			set["commissionRate"] = rate
			after.Values["commissionRate"] = rate
		}
	}
	commission := after.num("commissionAmount")
	rateChanged := op == "create" || before.num("commissionRate") != after.num("commissionRate") || before.num("premium") != premium
	if after.has("commissionRate") && (rateChanged || !after.has("commissionAmount")) {
		commission = round2(premium * after.num("commissionRate") / 100)
	}
	want := map[string]float64{"totalToCollect": round2(premium + tax + fee), "commissionAmount": commission,
		"profit": round2(commission + fee - after.num("directCosts")), "amountToRemit": 0}
	if after.text("collectionMode") == "agent" {
		want["amountToRemit"] = round2(premium + tax)
	}
	for k, v := range want {
		if !after.has(k) || after.num(k) != v {
			set[k] = v
		}
	}
	if status == "cancelled" && !after.has("cancellationReason") {
		return nil, refuse("cancellationReason", "Say why the policy was cancelled.")
	}
	issuing := status == "in_force" && (op == "create" || was == "proposal" || was == "")
	if status == "in_force" || status == "renewed" {
		fe := map[string]string{}
		if !after.has("policyNumber") {
			fe["policyNumber"] = "Enter the policy number the insurer issued."
		}
		if !after.has("startDate") {
			fe["startDate"] = "Enter the date the cover starts."
		}
		if premium <= 0 {
			fe["premium"] = "Enter the premium."
		}
		if len(fe) > 0 {
			return nil, shared.Validation(fe)
		}
	}
	if issuing && !after.has("endDate") {
		start, _ := time.Parse("2006-01-02", after.text("startDate")[:10])
		set["endDate"] = start.AddDate(1, 0, -1).Format("2006-01-02")
	}
	if issuing && !after.has("frequency") {
		set["frequency"] = "yearly"
	}
	if issuing && !after.has("collectionMode") {
		set["collectionMode"] = "direct"
	}
	if len(set) > 0 {
		var err error
		if after, err = h.updateValues(ctx, tx, ws, spec, after.uuid(), sys, set, nil, nil); err != nil {
			return nil, err
		}
	}
	switch {
	case issuing:
		if err := h.issuePolicy(ctx, tx, ws, after); err != nil {
			return nil, err
		}
	case status == "cancelled" && was != "cancelled":
		if err := h.cancelPolicy(ctx, tx, ws, after); err != nil {
			return nil, err
		}
	case status == "renewed" && was != "renewed":
		if err := h.renewPolicy(ctx, tx, ws, after); err != nil {
			return nil, err
		}
	case status == "in_force" && was == "lapsed":
		// Revived: the instalments that lapsed are due again.
		if _, err := tx.Exec(ctx, `UPDATE crm.object_records SET status = 'overdue', updated_at = now() WHERE workspace_id = $1 AND object_key = 'premium_dues'
			AND deleted_at IS NULL AND custom->>'policyId' = $2 AND status = 'lapsed'`, ws, after.ID); err != nil {
			return nil, err
		}
	}
	return after, nil
}

// issuePolicy writes the first year's premium schedule and the expected commission.
func (h *Handler) issuePolicy(ctx context.Context, tx pgx.Tx, ws uuid.UUID, p *Row) error {
	sys := systemActor(sourceInsurance)
	var have int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'premium_dues' AND deleted_at IS NULL
		AND custom->>'policyId' = $2`, ws, p.ID).Scan(&have); err != nil || have > 0 {
		return err
	}
	n := instalmentsOf(p.text("frequency"))
	start, _ := time.Parse("2006-01-02", p.text("startDate")[:10])
	total := p.num("totalToCollect")
	each := round2(total / float64(n))
	for i := 1; i <= n; i++ {
		amount := each
		if i == n {
			amount = round2(total - each*float64(n-1)) // the last instalment takes the rounding
		}
		due := map[string]any{"status": "due", "ownerId": p.text("ownerId"), "policyId": p.ID, "contactId": p.text("contactId"),
			"instalmentNumber": float64(i), "dueDate": start.AddDate(0, (i-1)*12/n, 0).Format("2006-01-02"), "amount": amount}
		clean(due)
		h.autoName(ctx, tx, ws, specFor("premium_dues"), due)
		if _, err := h.createRecord(ctx, tx, ws, specFor("premium_dues"), sys, due); err != nil {
			return err
		}
	}
	year, kind := 1.0, "first_year"
	if prev := p.id("previousPolicyId"); prev != uuid.Nil {
		kind = "renewal"
		_ = tx.QueryRow(ctx, `SELECT COALESCE(max((custom->>'policyYear')::numeric), 0) + 1 FROM crm.object_records WHERE workspace_id = $1
			AND object_key = 'commissions' AND deleted_at IS NULL AND custom->>'policyId' = $2`, ws, prev.String()).Scan(&year)
	}
	if p.num("commissionAmount") > 0 {
		c := map[string]any{"status": "expected", "ownerId": p.text("ownerId"), "policyId": p.ID, "insurerId": p.text("insurerId"), "policyYear": year,
			"kind": kind, "expectedAmount": p.num("commissionAmount"), "expectedOn": start.AddDate(0, 1, 0).Format("2006-01-02")}
		clean(c)
		h.autoName(ctx, tx, ws, specFor("commissions"), c)
		if _, err := h.createRecord(ctx, tx, ws, specFor("commissions"), sys, c); err != nil {
			return err
		}
	}
	return nil
}

// cancelPolicy stops the schedule and takes back commission that was already received.
func (h *Handler) cancelPolicy(ctx context.Context, tx pgx.Tx, ws uuid.UUID, p *Row) error {
	sys := systemActor(sourceInsurance)
	if _, err := tx.Exec(ctx, `UPDATE crm.object_records SET status = 'cancelled', updated_at = now() WHERE workspace_id = $1 AND object_key = 'premium_dues'
		AND deleted_at IS NULL AND custom->>'policyId' = $2 AND status IN ('due', 'overdue')`, ws, p.ID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE crm.object_records SET status = 'written_off', updated_at = now() WHERE workspace_id = $1 AND object_key = 'commissions'
		AND deleted_at IS NULL AND custom->>'policyId' = $2 AND status = 'expected'`, ws, p.ID); err != nil {
		return err
	}
	var received float64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum((custom->>'receivedAmount')::numeric), 0) FROM crm.object_records WHERE workspace_id = $1
		AND object_key = 'commissions' AND deleted_at IS NULL AND custom->>'policyId' = $2 AND status IN ('received', 'short_paid')`, ws, p.ID).Scan(&received); err != nil {
		return err
	}
	// A free-look or early cancellation: the insurer takes the commission back.
	if received > 0 && (p.text("cancellationReason") == "free_look" || p.text("cancellationReason") == "non_payment") {
		c := map[string]any{"status": "clawback", "ownerId": p.text("ownerId"), "policyId": p.ID, "insurerId": p.text("insurerId"),
			"kind": "clawback", "expectedAmount": -received, "expectedOn": today(), "notes": "Commission to give back after cancellation."}
		clean(c)
		c["name"] = "Clawback — " + p.Title
		if _, err := h.createRecord(ctx, tx, ws, specFor("commissions"), sys, c); err != nil {
			return err
		}
	}
	return nil
}

// renewPolicy opens the next term the day after this one ends, carrying everything over.
// The new term is a proposal until the insurer issues it with its own number.
func (h *Handler) renewPolicy(ctx context.Context, tx pgx.Tx, ws uuid.UUID, p *Row) error {
	if !p.has("endDate") {
		return refuse("endDate", "Enter when this term ends, so the next one can start the day after.")
	}
	var open int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'insurance_policies' AND deleted_at IS NULL
		AND custom->>'previousPolicyId' = $2 AND status <> 'cancelled'`, ws, p.ID).Scan(&open); err != nil || open > 0 {
		return err
	}
	end, _ := time.Parse("2006-01-02", p.text("endDate")[:10])
	next := map[string]any{"status": "proposal", "ownerId": p.text("ownerId"), "previousPolicyId": p.ID,
		"startDate": end.AddDate(0, 0, 1).Format("2006-01-02"), "endDate": end.AddDate(1, 0, 0).Format("2006-01-02")}
	for _, k := range []string{"contactId", "accountId", "insurerId", "itemId", "policyClass", "sumAssured", "frequency", "premium", "taxOnPremium",
		"agentFee", "collectionMode", "commissionRate", "directCosts", "graceDays", "nominee", "assetId"} {
		next[k] = p.Values[k]
	}
	clean(next)
	h.autoName(ctx, tx, ws, specFor("insurance_policies"), next)
	next["name"] = fmt.Sprint(next["name"]) + " (renewal)"
	// Not the insurance source: the new term's own sums are worked out by its rules.
	_, err := h.createRecord(ctx, tx, ws, specFor("insurance_policies"), actorInfo{Kind: "system", Source: "renewal"}, next)
	return err
}

// premiumSaved: a paid instalment is dated and its amount known; one paid to the agent
// is owed to the insurer until remitted.
func (h *Handler) premiumSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, after *Row) (*Row, error) {
	status := after.text("status")
	set := map[string]any{}
	if status == "paid" {
		if !after.has("paidOn") {
			set["paidOn"] = today()
		}
		if !after.has("paidAmount") {
			set["paidAmount"] = after.num("amount")
		} else if after.num("paidAmount") < after.num("amount")-0.005 {
			return nil, refuse("paidAmount", fmt.Sprintf("The instalment is %.2f. Record the part payment in the notes and mark it Paid when the full amount is in.", after.num("amount")))
		}
	}
	if after.has("remittedOn") && status != "paid" {
		return nil, refuse("remittedOn", "A premium is remitted only after the customer has paid it.")
	}
	if len(set) > 0 {
		var err error
		if after, err = h.updateValues(ctx, tx, ws, specFor("premium_dues"), after.uuid(), systemActor(sourceInsurance), set, nil, nil); err != nil {
			return nil, err
		}
	}
	// A lapsed policy whose overdue premiums are all paid is in force again.
	if status == "paid" && (op == "create" || before.text("status") != "paid") {
		if pid := after.id("policyId"); pid != uuid.Nil {
			var pStatus string
			var waiting int
			_ = tx.QueryRow(ctx, `SELECT COALESCE(status, '') FROM crm.object_records WHERE id = $1 AND workspace_id = $2`, pid, ws).Scan(&pStatus)
			_ = tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'premium_dues' AND deleted_at IS NULL
				AND custom->>'policyId' = $2 AND status IN ('overdue', 'lapsed')`, ws, pid.String()).Scan(&waiting)
			if pStatus == "lapsed" && waiting == 0 {
				if _, err := h.updateValues(ctx, tx, ws, specFor("insurance_policies"), pid, systemActor(sourceInsurance), map[string]any{"status": "in_force"}, nil, nil); err != nil {
					return nil, err
				}
			}
		}
	}
	return after, nil
}

// commissionSaved: what arrived against what was expected.
func (h *Handler) commissionSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, after *Row) (*Row, error) {
	set := map[string]any{}
	status := after.text("status")
	if after.has("receivedAmount") && status != "clawback" && status != "written_off" {
		expected, got := after.num("expectedAmount"), after.num("receivedAmount")+after.num("tdsDeducted")
		if v := round2(got - expected); v != after.num("variance") || !after.has("variance") {
			set["variance"] = v
		}
		want := "received"
		if got < expected-0.005 {
			want = "short_paid"
		}
		if status != want {
			set["status"] = want
		}
		if !after.has("receivedOn") {
			set["receivedOn"] = today()
		}
	} else if (status == "received" || status == "short_paid") && !after.has("receivedAmount") {
		return nil, refuse("receivedAmount", "Enter how much the insurer paid.")
	}
	if len(set) == 0 {
		return after, nil
	}
	return h.updateValues(ctx, tx, ws, specFor("commissions"), after.uuid(), systemActor(sourceInsurance), set, nil, nil)
}

// renewalRungs are the days before a policy ends at which its owner is reminded.
var renewalRungs = []int{60, 30, 15, 7, 1}

// sweepInsurance runs the date-driven part: premiums going overdue and lapsing, and the
// renewal ladder. Each step happens once however often it runs.
func (h *Handler) sweepInsurance(ctx context.Context) {
	if specFor("insurance_policies") == nil || specFor("premium_dues") == nil || specFor("tasks") == nil {
		return
	}
	sys := systemActor(sourceInsurance)
	type ref struct {
		id, ws uuid.UUID
		extra  string
	}
	collect := func(sql string) []ref {
		rows, err := h.store.Pool.Query(ctx, sql)
		if err != nil {
			return nil
		}
		defer rows.Close()
		var out []ref
		for rows.Next() {
			var r ref
			if rows.Scan(&r.id, &r.ws, &r.extra) == nil {
				out = append(out, r)
			}
		}
		return out
	}
	task := func(tx pgx.Tx, ws uuid.UUID, name, owner, contact string) error {
		t := map[string]any{"name": name, "status": "not_started", "priority": "high", "ownerId": owner, "dueDate": today(), "contactId": contact}
		clean(t)
		_, err := h.createRecord(ctx, tx, ws, specFor("tasks"), sys, t)
		return err
	}
	// 1. Due premiums whose date has passed are overdue; past the grace period they lapse,
	//    and so does the policy.
	for _, r := range collect(`SELECT d.id, d.workspace_id, CASE WHEN NULLIF(d.custom->>'dueDate', '')::date + COALESCE(NULLIF(p.custom->>'graceDays', '')::numeric, 30)::int < now()::date
			THEN 'lapsed' ELSE 'overdue' END
		FROM crm.object_records d JOIN crm.object_records p ON p.id::text = d.custom->>'policyId' AND p.deleted_at IS NULL
		WHERE d.object_key = 'premium_dues' AND d.deleted_at IS NULL AND NULLIF(d.custom->>'dueDate', '')::date < now()::date
		  AND (d.status = 'due' OR (d.status = 'overdue' AND NULLIF(d.custom->>'dueDate', '')::date + COALESCE(NULLIF(p.custom->>'graceDays', '')::numeric, 30)::int < now()::date))
		LIMIT 500`) {
		_ = h.store.WithTx(ctx, func(tx pgx.Tx) error {
			var status string
			if err := tx.QueryRow(ctx, `SELECT status FROM crm.object_records WHERE id = $1 FOR UPDATE`, r.id).Scan(&status); err != nil || status == r.extra || status == "paid" {
				return err
			}
			due, err := h.updateValues(ctx, tx, r.ws, specFor("premium_dues"), r.id, sys, map[string]any{"status": r.extra}, nil, nil)
			if err != nil {
				return err
			}
			if r.extra == "overdue" {
				return task(tx, r.ws, "Premium overdue — call: "+due.Title, due.text("ownerId"), due.text("contactId"))
			}
			if pid := due.id("policyId"); pid != uuid.Nil {
				if p, _, err := h.getRow(ctx, tx, r.ws, specFor("insurance_policies"), pid, nil); err == nil && p.text("status") == "in_force" {
					if _, err := h.updateValues(ctx, tx, r.ws, specFor("insurance_policies"), pid, sys, map[string]any{"status": "lapsed"}, nil, nil); err != nil {
						return err
					}
					return task(tx, r.ws, "Policy lapsed — offer revival: "+p.Title, p.text("ownerId"), p.text("contactId"))
				}
			}
			return nil
		})
	}
	// 2. The renewal ladder: one task per rung, and none once the next term exists.
	for _, r := range collect(`SELECT p.id, p.workspace_id, (NULLIF(p.custom->>'endDate', '')::date - now()::date)::text
		FROM crm.object_records p WHERE p.object_key = 'insurance_policies' AND p.deleted_at IS NULL AND p.status = 'in_force'
		  AND NULLIF(p.custom->>'endDate', '')::date BETWEEN now()::date AND now()::date + 60
		  AND NOT EXISTS (SELECT 1 FROM crm.object_records n WHERE n.object_key = 'insurance_policies' AND n.deleted_at IS NULL
		                  AND n.custom->>'previousPolicyId' = p.id::text AND n.status <> 'cancelled')
		LIMIT 500`) {
		var left int
		fmt.Sscan(r.extra, &left)
		rung := 0
		for _, d := range renewalRungs {
			if left <= d {
				rung = d
			}
		}
		if rung == 0 {
			continue
		}
		_ = h.store.WithTx(ctx, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SELECT 1 FROM crm.object_records WHERE id = $1 FOR UPDATE`, r.id); err != nil {
				return err
			}
			p, _, err := h.getRow(ctx, tx, r.ws, specFor("insurance_policies"), r.id, nil)
			if err != nil || (p.has("renewalRung") && p.num("renewalRung") <= float64(rung)) {
				return err
			}
			if _, err := h.updateValues(ctx, tx, r.ws, specFor("insurance_policies"), r.id, sys, map[string]any{"renewalRung": float64(rung)}, nil, nil); err != nil {
				return err
			}
			when := fmt.Sprintf("in %d days", left)
			if left <= 1 {
				when = "tomorrow"
			}
			if left == 0 {
				when = "today"
			}
			return task(tx, r.ws, fmt.Sprintf("Renewal due %s: %s", when, p.Title), p.text("ownerId"), p.text("contactId"))
		})
	}
}
