package records

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"cogniz-crm-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The rest of the Insurance Agent use-case catalogue (D-184), built on the insurance pack:
//
//	policy_years        J8  the whole term of a policy, year by year: premium (fixed, stepped,
//	                        estimated or actual), commission % and expected / actual commission
//	fact_finds          B1–B2 the customer's needs and the protection gap
//	premium_rates       B6  a plan's rate table by age (per ₹1,000 of cover)
//	premium_calcs       B6/J5/J6 a premium worked out from the table, with the commission
//	statement_lines     C7–C8 an insurer's commission statement, line by line, matched to policies
//	commission_invoices C9  the agent's invoice to an insurer for the commission due
//	sales_plans         J16–J24 the annual plan: customers, mix, premium, commission, the
//	                        leads and quotes it needs, and actual against plan
//	agent_licences      F7–F8 licence validity and insurer appointments; sales blocked after expiry
//	consent_requests    I2–I4 consent asked by WhatsApp or a call, two attempts at most
//	book_batches/rows   H1–H20 an existing book of customers and policies brought in

func insuranceMoreObjects() []ObjectDefinition {
	return []ObjectDefinition{
		{Key: "policy_years", Module: "insurance", Singular: "Policy year", Plural: "Policy years", Icon: "calendar", Prefix: "PYR",
			Description: "Every year of a policy's term: the premium (firm or estimated), the commission % and what it should earn, then what it did.",
			ObjectBody: ObjectBody{NameLabel: "Year", StatusLabel: "Status", Statuses: []StatusOption{
				st("due", "Due", "primary"), st("paid", "Paid", "success"), st("future", "Future (firm)", "neutral"), st("estimated", "Estimated", "warning"),
				st("lapsed", "Lapsed", "danger"), st("closed", "Closed", "neutral")},
				Fields: []ObjectField{
					ofReq(ofLookup("policyId", "Policy", "insurance_policies")), ofLookup("termPolicyId", "Term issued for this year", "insurance_policies"),
					ofLookup("insurerId", "Insurer", "accounts"), ofLookup("contactId", "Policyholder", "contacts"),
					of("policyYear", "Policy year", "number"), of("startDate", "Year starts", "date"), of("endDate", "Year ends", "date"), of("renewalDate", "Renewal date", "date"),
					of("premium", "Premium", "currency"), ofSelect("premiumBasis", "Premium is", opts("fixed", "Fixed", "stepped", "Stepped", "estimated", "Estimated", "actual", "Actual")),
					of("estimatedPremium", "Premium estimated at issue", "currency"),
					of("commissionRate", "Commission %", "percent"), of("expectedCommission", "Expected commission", "currency"),
					of("actualCommission", "Commission received", "currency"), of("effectiveRate", "Effective % earned", "percent"), of("variance", "Difference", "currency")}}},
		{Key: "fact_finds", Module: "insurance", Singular: "Fact-find", Plural: "Fact-finds", Icon: "clipboard-list", Prefix: "FF",
			Description: "What the family needs: dependants, income, loans and the cover they hold — and the gap in life and health cover.",
			ObjectBody: ObjectBody{NameLabel: "Fact-find", StatusLabel: "Status", Statuses: []StatusOption{st("draft", "Draft", "neutral"), st("complete", "Complete", "success")},
				Fields: []ObjectField{
					ofReq(ofLookup("contactId", "Customer", "contacts")), ofLookup("accountId", "Household", "accounts"), ofLookup("opportunityId", "Deal", "opportunities"),
					of("age", "Age", "number"), of("familyMembers", "People to cover (health)", "number"), of("dependants", "Dependants", "number"),
					of("annualIncome", "Annual income", "currency"), of("liabilities", "Loans and liabilities", "currency"),
					of("existingLifeCover", "Life cover held", "currency"), of("existingHealthCover", "Health cover held", "currency"),
					ObjectField{Key: "healthConditions", Label: "Health conditions (sensitive)", Type: "textarea", HelpText: "Collected at needs analysis only; hidden from sales and calling staff"},
					of("lifeCoverNeeded", "Life cover needed", "currency"), of("healthCoverNeeded", "Health cover needed", "currency"),
					of("lifeGap", "Life cover shortfall", "currency"), of("healthGap", "Health cover shortfall", "currency"),
					of("missingItems", "Still to ask", "text"), of("chosenPlanReason", "Why the chosen plan (suitability)", "textarea")}}},
		{Key: "premium_rates", Module: "insurance", Singular: "Premium rate", Plural: "Premium rate table", Icon: "table", Prefix: "PRT",
			Description: "A plan's yearly premium per ₹1,000 of cover, by age band — the source of the premium calculator.",
			ObjectBody: ObjectBody{NameLabel: "Rate", StatusLabel: "Status", Statuses: []StatusOption{st("active", "Active", "success"), st("inactive", "Inactive", "neutral")},
				Fields: []ObjectField{ofReq(ofLookup("itemId", "Plan", "catalog_items")), of("ageFrom", "From age", "number"), of("ageTo", "To age", "number"),
					ofReq(of("ratePerThousand", "Yearly premium per ₹1,000 cover", "number")), of("minimumPremium", "Minimum premium", "currency")}}},
		{Key: "premium_calcs", Module: "insurance", Singular: "Premium calculation", Plural: "Premium calculator", Icon: "calculator", Prefix: "PCL",
			Description: "Age, cover and mode in; the premium out from the rate table, with your commission for year 1 and the renewal years.",
			ObjectBody: ObjectBody{NameLabel: "Calculation", StatusLabel: "Status", Statuses: []StatusOption{st("draft", "Worked out", "neutral"), st("chosen", "Chosen", "success")},
				Fields: []ObjectField{
					ofReq(ofLookup("itemId", "Plan", "catalog_items")), ofLookup("contactId", "Customer", "contacts"), ofLookup("opportunityId", "Deal", "opportunities"),
					ofReq(of("age", "Age", "number")), ofReq(of("sumAssured", "Cover / sum assured", "currency")),
					ofSelect("frequency", "Paid", opts("yearly", "Yearly", "half_yearly", "Half-yearly", "quarterly", "Quarterly", "monthly", "Monthly")),
					ofLookup("insurerId", "Insurer", "accounts"), of("ratePerThousand", "Rate per ₹1,000", "number"),
					of("yearlyPremium", "Yearly premium", "currency"), of("instalment", "Each instalment", "currency"),
					of("commissionRateYear1", "Commission rate, year 1", "percent"), of("commissionYear1", "Your commission, year 1", "currency"),
					of("commissionRateRenewal", "Commission rate, renewals", "percent"), of("commissionRenewal", "Your commission, each renewal", "currency"),
					of("message", "Note", "text")}}},
		{Key: "statement_lines", Module: "insurance", Singular: "Statement line", Plural: "Commission statements", Icon: "file-spreadsheet", Prefix: "STL",
			Description: "An insurer's commission statement, one line per policy (import the CSV): matched to the expected commission, short payments flagged.",
			ObjectBody: ObjectBody{NameLabel: "Line", StatusLabel: "Status", Statuses: []StatusOption{
				st("matched", "Matched", "success"), st("short", "Matched, short paid", "warning"), st("unmatched", "No such policy", "danger")},
				Fields: []ObjectField{ofLookup("insurerId", "Insurer", "accounts"), ofReq(of("policyNumber", "Policy number", "text")), ofReq(of("amount", "Commission paid", "currency")),
					of("tds", "TDS deducted", "currency"), of("statementReference", "Statement reference", "text"), of("statementDate", "Statement date", "date"),
					ofLookup("policyId", "Policy", "insurance_policies"), ofLookup("commissionId", "Commission", "commissions"), of("message", "Result", "text")}}},
		{Key: "commission_invoices", Module: "insurance", Singular: "Commission invoice run", Plural: "Commission invoices", Icon: "receipt", Prefix: "CIV",
			Description: "Bill an insurer for the commission due in a period: one invoice with a line per policy, GST and TDS as you set them.",
			ObjectBody: ObjectBody{NameLabel: "Run", StatusLabel: "Status", Statuses: []StatusOption{st("made", "Invoice made", "success"), st("nothing", "Nothing to bill", "neutral")},
				Fields: []ObjectField{ofReq(ofLookup("insurerId", "Insurer", "accounts")), of("periodFrom", "Commission due from", "date"), of("periodTo", "to", "date"),
					of("gstRate", "GST on commission", "percent"), of("tdsRate", "TDS the insurer deducts", "percent"),
					of("commissionTotal", "Commission billed", "currency"), of("gst", "GST", "currency"), of("tdsExpected", "TDS expected", "currency"),
					of("netExpected", "Net payment expected", "currency"), of("policies", "Policies billed", "number"), ofLookup("invoiceId", "Invoice", "invoices")}}},
		{Key: "sales_plans", Module: "insurance", Singular: "Annual plan", Plural: "Annual plans", Icon: "target", Prefix: "PLN",
			Description: "The year's goal — new customers by product, premium and commission — with the leads and quotes it needs and the actuals against it.",
			ObjectBody: ObjectBody{NameLabel: "Plan", StatusLabel: "Status", Statuses: []StatusOption{st("active", "Active", "success"), st("superseded", "Replaced by a newer version", "neutral")},
				Fields: []ObjectField{
					ofReq(of("planStart", "Plan year starts", "date")), of("targetCustomers", "New customers", "number"),
					ObjectField{Key: "targetCommission", Label: "Or: commission goal", Type: "currency", HelpText: "Leave customers empty to work them out from this"},
					of("termShare", "Term life share", "percent"), of("healthShare", "Health share", "percent"), of("motorShare", "Motor share", "percent"),
					of("termPremium", "Average term premium", "currency"), of("healthPremium", "Average health premium", "currency"), of("motorPremium", "Average motor premium", "currency"),
					of("termRate", "Term commission, year 1", "percent"), of("healthRate", "Health commission", "percent"), of("motorRate", "Motor commission", "percent"),
					of("termRenewalRate", "Term commission, renewals", "percent"), of("healthRenewalRate", "Health commission, renewals", "percent"), of("motorRenewalRate", "Motor commission, renewals", "percent"),
					of("renewalPercent", "Customers who renew", "percent"), of("healthIncrease", "Health premium rise at renewal", "percent"), of("motorIncrease", "Motor premium rise at renewal", "percent"),
					of("leadToQuote", "Leads that reach a quote", "percent"), of("quoteToPolicy", "Quotes that become policies", "percent"), of("behindAlert", "Alert when behind by", "percent"),
					ofLookup("previousPlanId", "Replaces plan", "sales_plans"),
					of("year1Premium", "Planned premium, year 1", "currency"), of("year1Commission", "Planned commission, year 1", "currency"), of("year1Rate", "Effective %, year 1", "percent"),
					of("year2Premium", "Premium, year 2 (Estimated)", "currency"), of("year2Commission", "Commission, year 2 (Estimated)", "currency"), of("year2Rate", "Effective %, year 2", "percent"),
					of("quotesNeeded", "Quotes needed in the year", "number"), of("leadsNeeded", "Leads needed in the year", "number"), of("monthlyPlan", "Each month (working)", "textarea"),
					of("actualCustomers", "Actual new customers so far", "number"), of("actualPremium", "Actual premium so far", "currency"), of("actualCommission", "Actual commission so far", "currency"),
					of("planToDate", "Plan to date (customers)", "number"), of("behindBy", "Behind plan by", "percent"), of("pipelineCoverage", "Open quotes cover", "percent"),
					of("changeVsPrevious", "Change against the plan it replaces", "text"), of("alertMonth", "Last alert", "text")}}},
		{Key: "agent_licences", Module: "insurance", Singular: "Agent licence", Plural: "Licences & appointments", Icon: "badge-check", Prefix: "LIC",
			Description: "Each seller's licence, when it expires and which insurers they are appointed with. Policies can't be issued by someone without a valid licence.",
			ObjectBody: ObjectBody{NameLabel: "Licence", StatusLabel: "Status", Statuses: []StatusOption{st("valid", "Valid", "success"), st("expiring", "Expiring in 60 days", "warning"), st("expired", "Expired", "danger")},
				Fields: []ObjectField{ofReq(ofLookup("holderId", "Holder", "users")), of("licenceNumber", "Licence number", "text"), of("validFrom", "Valid from", "date"),
					ofReq(of("validUntil", "Valid until", "date")), {Key: "appointedInsurers", Label: "Appointed with", Type: "relations", Lookup: "accounts"},
					of("trainingDate", "Last product training", "date"), of("alertSent", "Expiry alert sent", "boolean")}}},
		{Key: "consent_requests", Module: "insurance", Singular: "Consent request", Plural: "Consent requests", Icon: "message-circle", Prefix: "CRQ",
			Description: "Consent asked by WhatsApp or a call: sent, delivered, read and the reply — two attempts at most, a wrong number flagged.",
			ObjectBody: ObjectBody{NameLabel: "Request", StatusLabel: "Status", Statuses: []StatusOption{
				st("sent", "Sent", "primary"), st("delivered", "Delivered", "primary"), st("read", "Read", "primary"), st("replied", "Replied", "success"), st("closed", "Closed", "neutral")},
				Fields: []ObjectField{ofReq(ofLookup("contactId", "Customer", "contacts")), ofSelect("channel", "Asked by", opts("whatsapp", "WhatsApp", "voice", "Voice call")),
					ofSelect("purpose", "For", opts("service", "Service messages", "marketing", "Offers and greetings", "health", "Health details for quotes", "sharing", "Sharing with the insurer")),
					of("attempt", "Attempt", "number"), of("sentAt", "Sent at", "datetime"), of("deliveredAt", "Delivered at", "datetime"), of("readAt", "Read at", "datetime"),
					of("repliedAt", "Replied at", "datetime"), ofSelect("outcome", "Outcome", opts("given", "Gave consent", "declined", "Declined", "no_reply", "No reply", "wrong_number", "Wrong number")),
					of("callId", "Call id", "text"), of("recordingRef", "Recording reference", "text"), of("noticeVersion", "Notice version", "text"),
					ofLookup("consentId", "Consent recorded", "consents")}}},
		{Key: "book_batches", Module: "insurance", Singular: "Book import", Plural: "Book imports", Icon: "upload", Prefix: "BKB",
			Description: "Bring in an existing book: import the rows, check them (nothing is written), then import — customers, households and policies, with a report.",
			ObjectBody: ObjectBody{NameLabel: "Batch", StatusLabel: "Status", Statuses: []StatusOption{
				st("draft", "Rows loaded", "neutral"), st("checked", "Checked (dry run)", "primary"), st("imported", "Imported", "success"), st("archived", "Archived", "neutral")},
				Fields: []ObjectField{ObjectField{Key: "source", Label: "Source", Type: "text", HelpText: "Old software, spreadsheet, insurer download…"},
					of("rowsTotal", "Rows", "number"), of("rowsOk", "Good", "number"), of("rowsWarning", "With warnings", "number"), of("rowsHeld", "Held for review", "number"),
					of("rowsRejected", "Rejected", "number"), of("customersCreated", "Customers created", "number"), of("customersMatched", "Customers matched", "number"),
					of("policiesCreated", "Policies created", "number"), of("premiumTotal", "Premium in the file", "currency"), of("premiumImported", "Premium imported", "currency"),
					of("report", "Report", "textarea")}}},
		{Key: "book_rows", Module: "insurance", Singular: "Book row", Plural: "Book rows", Icon: "list", Prefix: "BKR",
			Description: "One line of an existing-book file: the customer, the policy and what the check found.",
			ObjectBody: ObjectBody{NameLabel: "Row", StatusLabel: "Result", Statuses: []StatusOption{
				st("pending", "Not checked", "neutral"), st("ok", "Good", "success"), st("warning", "Warning", "warning"), st("held", "Held", "warning"),
				st("rejected", "Rejected", "danger"), st("imported", "Imported", "success")},
				Fields: []ObjectField{ofReq(ofLookup("batchId", "Batch", "book_batches")), of("rowNumber", "Row", "number"),
					of("customerName", "Customer name", "text"), of("mobile", "Mobile", "text"), of("email", "Email", "text"), of("dateOfBirth", "Date of birth", "text"),
					of("householdRef", "Household reference", "text"), of("relation", "Relation", "text"), of("insurer", "Insurer", "text"), of("plan", "Plan", "text"),
					of("policyClass", "Class", "text"), of("policyNumber", "Policy number", "text"), of("policyStatus", "Policy status", "text"),
					of("startDate", "Start date", "text"), of("endDate", "End / renewal date", "text"), of("sumAssured", "Sum assured", "text"), of("premium", "Premium", "text"),
					of("frequency", "Mode", "text"), of("collectionMode", "Collection mode (1 or 2)", "text"), of("commissionRate", "Commission %", "text"),
					of("agentOfRecord", "Agent of record", "text"), of("nominee", "Nominee", "text"), of("consent", "Marketing consent", "text"), of("notes", "Notes", "text"),
					of("message", "What the check found", "text"), ofLookup("contactId", "Customer", "contacts"), ofLookup("policyId", "Policy", "insurance_policies")}}},
	}
}

// ---- dispatch ----

var insuranceMoreKeys = map[string]bool{"policy_years": true, "fact_finds": true, "premium_rates": true, "premium_calcs": true, "statement_lines": true,
	"commission_invoices": true, "sales_plans": true, "agent_licences": true, "consent_requests": true, "book_batches": true, "book_rows": true}

func (h *Handler) insuranceMoreSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, a actorInfo, op string, before, after *Row) (*Row, error) {
	if a.Source == sourceInsurance {
		return after, nil
	}
	switch spec.Key {
	case "fact_finds":
		return h.factFindSaved(ctx, tx, ws, after)
	case "premium_calcs":
		return h.premiumCalcSaved(ctx, tx, ws, after)
	case "statement_lines":
		if op == "create" {
			return h.statementLineSaved(ctx, tx, ws, after)
		}
	case "commission_invoices":
		if op == "create" {
			return h.commissionInvoiceSaved(ctx, tx, ws, after)
		}
	case "sales_plans":
		return h.salesPlanSaved(ctx, tx, ws, op, after)
	case "agent_licences":
		return h.licenceSaved(ctx, tx, ws, after)
	case "consent_requests":
		return h.consentRequestSaved(ctx, tx, ws, op, before, after)
	case "book_batches":
		if op == "update" && before.text("status") != after.text("status") {
			return h.bookBatchStatus(ctx, tx, ws, before, after)
		}
	}
	return after, nil
}

func (h *Handler) setSys(ctx context.Context, tx pgx.Tx, ws uuid.UUID, key string, r *Row, set map[string]any) (*Row, error) {
	for k, v := range set {
		if fmt.Sprint(r.Values[k]) == fmt.Sprint(v) {
			delete(set, k)
		}
	}
	if len(set) == 0 {
		return r, nil
	}
	return h.updateValues(ctx, tx, ws, specFor(key), r.uuid(), systemActor(sourceInsurance), set, nil, nil)
}

func (h *Handler) makeSys(ctx context.Context, tx pgx.Tx, ws uuid.UUID, key string, v map[string]any) (*Row, error) {
	clean(v)
	if _, ok := v["name"]; !ok {
		h.autoName(ctx, tx, ws, specFor(key), v)
	}
	return h.createRecord(ctx, tx, ws, specFor(key), systemActor(sourceInsurance), v)
}

// ---- policy years (J3, J4, J8–J11) ----

// rootPolicy is the first term of a policy (renewals point back to it).
func (h *Handler) rootPolicy(ctx context.Context, q querier, ws uuid.UUID, id string) string {
	for i := 0; i < 60 && id != ""; i++ {
		var prev string
		if err := q.QueryRow(ctx, `SELECT COALESCE(custom->>'previousPolicyId', '') FROM crm.object_records WHERE id::text = $1 AND workspace_id = $2`, id, ws).Scan(&prev); err != nil || prev == "" {
			return id
		}
		id = prev
	}
	return id
}

var stepRe = regexp.MustCompile(`(\d+)\s*[:=]\s*([\d.,]+)`)

// yearPremium is a year's premium and how sure it is.
func yearPremium(p *Row, year int) (float64, string) {
	ppt := int(p.num("premiumPayingTerm"))
	if ppt > 0 && year > ppt {
		return 0, "fixed"
	}
	base := p.num("premium")
	switch p.text("premiumType") {
	case "stepped":
		amount, from := base, 0
		for _, m := range stepRe.FindAllStringSubmatch(p.text("stepSchedule"), -1) {
			y, _ := strconv.Atoi(m[1])
			v, _ := strconv.ParseFloat(strings.ReplaceAll(m[2], ",", ""), 64)
			if y <= year && y >= from {
				amount, from = v, y
			}
		}
		return amount, "stepped"
	case "variable":
		if year == 1 {
			return base, "actual"
		}
		return round2(base * math.Pow(1+p.num("yearlyIncrease")/100, float64(year-1))), "estimated"
	}
	return base, "fixed"
}

// buildPolicyYears writes the year table when a first term is issued, or rewrites the years
// not yet issued when its premium or term changes (G10, J7). Years already issued stay.
func (h *Handler) buildPolicyYears(ctx context.Context, tx pgx.Tx, ws uuid.UUID, p *Row) error {
	if specFor("policy_years") == nil || p.has("previousPolicyId") {
		return nil
	}
	term := int(p.num("policyTerm"))
	if term <= 0 {
		term = 1
	}
	start, err := time.Parse("2006-01-02", firstNonEmpty(p.text("startDate"), today())[:10])
	if err != nil {
		return nil
	}
	have := map[int]*Row{}
	rows, err := tx.Query(ctx, `SELECT id FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'policy_years' AND deleted_at IS NULL AND custom->>'policyId' = $2`, ws, p.ID)
	if err != nil {
		return err
	}
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if r, _, err := h.getRow(ctx, tx, ws, specFor("policy_years"), id, nil); err == nil {
			have[int(r.num("policyYear"))] = r
		}
	}
	for y := 1; y <= term; y++ {
		premium, basis := yearPremium(p, y)
		rate := p.num("commissionRate")
		if y > 1 {
			if r, ok := h.ruleRate(ctx, tx, ws, p.Values, y); ok {
				rate = r
			} else {
				rate = 0
			}
		}
		ys := start.AddDate(y-1, 0, 0)
		ye := ys.AddDate(1, 0, -1)
		v := map[string]any{"policyId": p.ID, "insurerId": p.text("insurerId"), "contactId": p.text("contactId"), "ownerId": p.text("ownerId"),
			"policyYear": float64(y), "startDate": ys.Format("2006-01-02"), "endDate": ye.Format("2006-01-02"),
			"premium": premium, "premiumBasis": basis, "estimatedPremium": premium, "commissionRate": rate, "expectedCommission": round2(premium * rate / 100)}
		if y < term {
			v["renewalDate"] = ye.AddDate(0, 0, 1).Format("2006-01-02")
		}
		switch {
		case y == 1:
			v["status"], v["termPolicyId"], v["premiumBasis"] = "due", p.ID, map[bool]string{true: "actual", false: basis}[basis == "estimated"]
		case basis == "estimated":
			v["status"] = "estimated"
		default:
			v["status"] = "future"
		}
		if old, ok := have[y]; ok {
			if y > 1 && old.text("premiumBasis") == "actual" {
				continue // an issued year keeps its real figures
			}
			delete(v, "status")
			if _, err := h.setSys(ctx, tx, ws, "policy_years", old, v); err != nil {
				return err
			}
			continue
		}
		v["name"] = fmt.Sprintf("Year %d — %s", y, p.Title)
		if _, err := h.makeSys(ctx, tx, ws, "policy_years", v); err != nil {
			return err
		}
	}
	return nil
}

// yearRow finds the year row of a policy term (its root's row for that year).
func (h *Handler) yearRow(ctx context.Context, tx pgx.Tx, ws uuid.UUID, root string, year int) *Row {
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'policy_years' AND deleted_at IS NULL
		AND custom->>'policyId' = $2 AND (custom->>'policyYear')::numeric = $3 LIMIT 1`, ws, root, year).Scan(&id); err != nil {
		return nil
	}
	r, _, err := h.getRow(ctx, tx, ws, specFor("policy_years"), id, nil)
	if err != nil {
		return nil
	}
	return r
}

// renewalIssued: the renewal term's real premium and commission replace that year's estimate (J11).
func (h *Handler) renewalIssued(ctx context.Context, tx pgx.Tx, ws uuid.UUID, p *Row) error {
	if specFor("policy_years") == nil || !p.has("previousPolicyId") {
		return nil
	}
	root := h.rootPolicy(ctx, tx, ws, p.ID)
	year := h.policyYearOf(ctx, tx, ws, p.Values["previousPolicyId"])
	set := map[string]any{"premium": p.num("premium"), "premiumBasis": "actual", "commissionRate": p.num("commissionRate"),
		"expectedCommission": p.num("commissionAmount"), "termPolicyId": p.ID, "status": "due"}
	if r := h.yearRow(ctx, tx, ws, root, year); r != nil {
		_, err := h.setSys(ctx, tx, ws, "policy_years", r, set)
		return err
	}
	set["policyId"], set["policyYear"], set["insurerId"], set["contactId"], set["ownerId"] = root, float64(year), p.text("insurerId"), p.text("contactId"), p.text("ownerId")
	set["startDate"], set["endDate"] = p.text("startDate"), p.text("endDate")
	set["name"] = fmt.Sprintf("Year %d — %s", year, p.Title)
	_, err := h.makeSys(ctx, tx, ws, "policy_years", set)
	return err
}

// yearCommission: commission that arrives is written against its policy year (J11, J15).
func (h *Handler) yearCommission(ctx context.Context, tx pgx.Tx, ws uuid.UUID, c *Row) error {
	if specFor("policy_years") == nil || !c.has("receivedAmount") || !c.has("policyId") {
		return nil
	}
	root := h.rootPolicy(ctx, tx, ws, c.text("policyId"))
	year := int(c.num("policyYear"))
	if year <= 0 {
		year = 1
	}
	r := h.yearRow(ctx, tx, ws, root, year)
	if r == nil {
		return nil
	}
	got := round2(c.num("receivedAmount") + c.num("tdsDeducted"))
	set := map[string]any{"actualCommission": got, "variance": round2(got - r.num("expectedCommission"))}
	if r.num("premium") > 0 {
		set["effectiveRate"] = round2(got / r.num("premium") * 100)
	}
	_, err := h.setSys(ctx, tx, ws, "policy_years", r, set)
	return err
}

// ---- C16: the sub-agent's share when commission arrives ----

func (h *Handler) subAgentPayout(ctx context.Context, tx pgx.Tx, ws uuid.UUID, c *Row) error {
	if specFor("payout_entries") == nil || !c.has("receivedAmount") || isTrue(c.Values["payoutCreated"]) {
		return nil
	}
	pid := c.id("policyId")
	if pid == uuid.Nil {
		return nil
	}
	p, _, err := h.getRow(ctx, tx, ws, specFor("insurance_policies"), pid, nil)
	if err != nil || !p.has("subAgentId") || p.num("subAgentShare") <= 0 {
		return nil
	}
	amount := round2(c.num("receivedAmount") * p.num("subAgentShare") / 100)
	if _, err := h.makeSys(ctx, tx, ws, "payout_entries", map[string]any{"name": "Sub-agent share — " + p.Title, "payeeUserId": p.text("subAgentId"),
		"contactId": p.text("contactId"), "kind": "commission", "basisAmount": c.num("receivedAmount"), "percent": p.num("subAgentShare"), "amount": amount, "ownerId": p.text("ownerId")}); err != nil {
		return err
	}
	_, err = h.setSys(ctx, tx, ws, "commissions", c, map[string]any{"payoutCreated": true})
	return err
}

// ---- B1–B2: fact-find and protection gap ----

func (h *Handler) factFindSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, f *Row) (*Row, error) {
	income := f.num("annualIncome")
	lifeNeed := round2(10*income + f.num("liabilities"))
	people := math.Max(1, f.num("familyMembers"))
	healthNeed := round2(500000 * people)
	missing := []string{}
	for _, k := range []struct{ key, label string }{{"age", "age"}, {"annualIncome", "income"}, {"dependants", "dependants"}, {"liabilities", "loans"},
		{"existingLifeCover", "life cover held"}, {"existingHealthCover", "health cover held"}, {"familyMembers", "people to cover"}} {
		if !f.has(k.key) {
			missing = append(missing, k.label)
		}
	}
	set := map[string]any{"lifeCoverNeeded": lifeNeed, "healthCoverNeeded": healthNeed,
		"lifeGap": math.Max(0, round2(lifeNeed-f.num("existingLifeCover"))), "healthGap": math.Max(0, round2(healthNeed-f.num("existingHealthCover"))),
		"missingItems": strings.Join(missing, ", ")}
	if len(missing) == 0 {
		set["status"] = "complete"
	}
	return h.setSys(ctx, tx, ws, "fact_finds", f, set)
}

// ---- B6, J5, J6: premium calculator with commission ----

var frequencyLoad = map[string]float64{"yearly": 0, "half_yearly": 0.02, "quarterly": 0.03, "monthly": 0.05}

// planInsurer is the insurer whose price book lists a plan, and the plan's class.
func (h *Handler) planInsurer(ctx context.Context, q querier, ws uuid.UUID, itemID string) (insurer, class string) {
	_ = q.QueryRow(ctx, `SELECT COALESCE(b.custom->>'accountId', ''), lower(COALESCE(i.custom->>'category', ''))
		FROM crm.object_records i
		LEFT JOIN crm.price_book_entries e ON e.catalog_item_id = i.id AND e.workspace_id = i.workspace_id AND e.is_active AND e.deleted_at IS NULL
		LEFT JOIN crm.object_records b ON b.id = e.price_book_id AND b.object_key = 'price_books' AND b.deleted_at IS NULL
		WHERE i.id::text = $1 AND i.workspace_id = $2 ORDER BY (b.custom->>'accountId') NULLS LAST LIMIT 1`, itemID, ws).Scan(&insurer, &class)
	return
}

func (h *Handler) premiumCalcSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, c *Row) (*Row, error) {
	age := c.num("age")
	var rate, minimum float64
	err := tx.QueryRow(ctx, `SELECT (custom->>'ratePerThousand')::numeric, COALESCE(NULLIF(custom->>'minimumPremium', '')::numeric, 0) FROM crm.object_records
		WHERE workspace_id = $1 AND object_key = 'premium_rates' AND deleted_at IS NULL AND COALESCE(status, 'active') = 'active' AND custom->>'itemId' = $2
		  AND COALESCE(NULLIF(custom->>'ageFrom', '')::numeric, 0) <= $3 AND COALESCE(NULLIF(custom->>'ageTo', '')::numeric, 200) >= $3
		ORDER BY created_at DESC LIMIT 1`, ws, c.text("itemId"), age).Scan(&rate, &minimum)
	if err != nil {
		return nil, refuse("age", "There's no rate for this plan at this age. Add it to the premium rate table.")
	}
	freq := firstNonEmpty(c.text("frequency"), "yearly")
	yearly := math.Max(minimum, round2(c.num("sumAssured")/1000*rate))
	n := float64(instalmentsOf(freq))
	set := map[string]any{"ratePerThousand": rate, "yearlyPremium": yearly, "instalment": round2(yearly * (1 + frequencyLoad[freq]) / n), "frequency": freq}
	insurer, class := h.planInsurer(ctx, tx, ws, c.text("itemId"))
	if insurer != "" {
		set["insurerId"] = insurer
		vals := map[string]any{"insurerId": insurer, "itemId": c.text("itemId"), "policyClass": class}
		if r1, ok := h.ruleRate(ctx, tx, ws, vals, 1); ok {
			set["commissionRateYear1"], set["commissionYear1"] = r1, round2(yearly*r1/100)
		}
		if r2, ok := h.ruleRate(ctx, tx, ws, vals, 2); ok {
			set["commissionRateRenewal"], set["commissionRenewal"] = r2, round2(yearly*r2/100)
		}
	}
	return h.setSys(ctx, tx, ws, "premium_calcs", c, set)
}

// quoteCommission puts the agent's commission on an insurance quote (J6): the quote's price
// book belongs to the insurer; each plan line earns that insurer's rate for its class.
func (h *Handler) quoteCommission(ctx context.Context, tx pgx.Tx, ws uuid.UUID, q *Row) (*Row, error) {
	if specFor("commission_rules") == nil || !q.has("priceBookId") {
		return q, nil
	}
	var insurer string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(custom->>'accountId', '') FROM crm.object_records WHERE id::text = $1 AND workspace_id = $2 AND object_key = 'price_books'`, q.text("priceBookId"), ws).Scan(&insurer); err != nil || insurer == "" {
		return q, nil
	}
	// A line's total is before tax: commission is paid on the premium, not on the tax.
	rows, err := tx.Query(ctx, `SELECT COALESCE(l.custom->>'itemId', ''), `+numeric(`l.custom->>'total'`)+`
		FROM crm.object_records l WHERE l.workspace_id = $1 AND l.object_key = 'line_items' AND l.deleted_at IS NULL AND l.custom->>'quoteId' = $2`, ws, q.ID)
	if err != nil {
		return q, nil
	}
	type line struct {
		item string
		net  float64
	}
	lines := []line{}
	for rows.Next() {
		var l line
		if rows.Scan(&l.item, &l.net) == nil {
			lines = append(lines, l)
		}
	}
	rows.Close()
	var c1, c2, net float64
	for _, l := range lines {
		_, class := h.planInsurer(ctx, tx, ws, l.item)
		vals := map[string]any{"insurerId": insurer, "itemId": l.item, "policyClass": class}
		if r, ok := h.ruleRate(ctx, tx, ws, vals, 1); ok {
			c1 += l.net * r / 100
		}
		if r, ok := h.ruleRate(ctx, tx, ws, vals, 2); ok {
			c2 += l.net * r / 100
		}
		net += l.net
	}
	if net <= 0 {
		return q, nil
	}
	return h.setSys(ctx, tx, ws, "quotes", q, map[string]any{"commissionYear1": round2(c1), "commissionRenewal": round2(c2),
		"commissionRateYear1": round2(c1 / net * 100), "commissionRateRenewal": round2(c2 / net * 100)})
}

// ---- C7–C8: insurer statement lines ----

func (h *Handler) statementLineSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, l *Row) (*Row, error) {
	num := strings.ToUpper(strings.Join(strings.Fields(l.text("policyNumber")), ""))
	var pid string
	_ = tx.QueryRow(ctx, `SELECT id::text FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'insurance_policies' AND deleted_at IS NULL
		AND upper(replace(custom->>'policyNumber', ' ', '')) = $2 AND ($3 = '' OR custom->>'insurerId' = $3) ORDER BY created_at LIMIT 1`, ws, num, l.text("insurerId")).Scan(&pid)
	if pid == "" {
		return h.setSys(ctx, tx, ws, "statement_lines", l, map[string]any{"status": "unmatched", "message": "No policy with this number — check the statement or add the policy."})
	}
	var cid uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'commissions' AND deleted_at IS NULL
		AND custom->>'policyId' = $2 AND status IN ('expected', 'short_paid') ORDER BY (custom->>'policyYear')::numeric NULLS LAST, created_at LIMIT 1`, ws, pid).Scan(&cid); err != nil {
		return h.setSys(ctx, tx, ws, "statement_lines", l, map[string]any{"status": "unmatched", "policyId": pid, "message": "The policy has no commission waiting to be paid."})
	}
	c, _, err := h.getRow(ctx, tx, ws, specFor("commissions"), cid, nil)
	if err != nil {
		return nil, err
	}
	got := round2(c.num("receivedAmount") + l.num("amount"))
	set := map[string]any{"receivedAmount": got, "tdsDeducted": round2(c.num("tdsDeducted") + l.num("tds")), "statementReference": l.text("statementReference")}
	if l.has("statementDate") {
		set["receivedOn"] = l.text("statementDate")
	}
	if c, err = h.updateValues(ctx, tx, ws, specFor("commissions"), cid, systemActor("statement"), set, nil, nil); err != nil {
		return nil, err
	}
	status, msg := "matched", "Matched to "+c.Title
	if c.text("status") == "short_paid" {
		status, msg = "short", fmt.Sprintf("Short paid by %.2f against %s", -c.num("variance"), c.Title)
	}
	return h.setSys(ctx, tx, ws, "statement_lines", l, map[string]any{"status": status, "policyId": pid, "commissionId": cid.String(), "message": msg})
}

// ---- C9: commission invoice to an insurer ----

func (h *Handler) commissionInvoiceSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, run *Row) (*Row, error) {
	from, to := firstNonEmpty(run.text("periodFrom"), "1900-01-01"), firstNonEmpty(run.text("periodTo"), "2999-12-31")
	rows, err := tx.Query(ctx, `SELECT c.id, `+numeric(`c.custom->>'expectedAmount'`)+`, COALESCE(p.custom->>'policyNumber', ''), COALESCE(p.name, '')
		FROM crm.object_records c LEFT JOIN crm.object_records p ON p.id::text = c.custom->>'policyId'
		WHERE c.workspace_id = $1 AND c.object_key = 'commissions' AND c.deleted_at IS NULL AND c.custom->>'insurerId' = $2
		  AND c.status IN ('expected', 'short_paid') AND COALESCE(c.custom->>'invoiceId', '') = '' AND COALESCE(c.custom->>'kind', '') <> 'clawback'
		  AND COALESCE(c.custom->>'expectedOn', '') BETWEEN $3 AND $4 ORDER BY p.name`, ws, run.text("insurerId"), from, to)
	if err != nil {
		return nil, err
	}
	type due struct {
		id          uuid.UUID
		amount      float64
		number, pol string
	}
	list := []due{}
	for rows.Next() {
		var d due
		if rows.Scan(&d.id, &d.amount, &d.number, &d.pol) == nil {
			list = append(list, d)
		}
	}
	rows.Close()
	if len(list) == 0 {
		return h.setSys(ctx, tx, ws, "commission_invoices", run, map[string]any{"status": "nothing", "policies": 0.0, "commissionTotal": 0.0})
	}
	gstRate, tdsRate := run.num("gstRate"), run.num("tdsRate")
	if !run.has("gstRate") {
		gstRate = 18
	}
	if !run.has("tdsRate") {
		tdsRate = 5
	}
	var total float64
	for _, d := range list {
		total += d.amount
	}
	total = round2(total)
	gst, tds := round2(total*gstRate/100), round2(total*tdsRate/100)
	inv, err := h.makeSys(ctx, tx, ws, "invoices", map[string]any{"name": "Commission " + from + " to " + to, "accountId": run.text("insurerId"), "invoiceDate": today(),
		"dueDate": time.Now().AddDate(0, 0, 30).Format("2006-01-02"), "subtotal": total, "tax": gst, "total": round2(total + gst), "status": "draft", "ownerId": run.text("ownerId"),
		"description": fmt.Sprintf("Commission on %d policies. GST %.2f%%; TDS %.2f%% expected to be deducted.", len(list), gstRate, tdsRate)})
	if err != nil {
		return nil, err
	}
	for i, d := range list {
		if _, err := h.makeSys(ctx, tx, ws, "line_items", map[string]any{"invoiceId": inv.ID, "name": strings.TrimSpace(d.number + " " + d.pol), "quantity": 1.0,
			"unitPrice": d.amount, "total": d.amount, "sortOrder": float64(i + 1)}); err != nil {
			return nil, err
		}
		if _, err := tx.Exec(ctx, `UPDATE crm.object_records SET custom = custom || jsonb_build_object('invoiceId', $2::text), updated_at = now() WHERE id = $1`, d.id, inv.ID); err != nil {
			return nil, err
		}
	}
	return h.setSys(ctx, tx, ws, "commission_invoices", run, map[string]any{"status": "made", "invoiceId": inv.ID, "policies": float64(len(list)),
		"commissionTotal": total, "gst": gst, "tdsExpected": tds, "netExpected": round2(total + gst - tds), "gstRate": gstRate, "tdsRate": tdsRate})
}

// ---- J16–J24: annual plan ----

func (h *Handler) salesPlanSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, p *Row) (*Row, error) {
	pct := func(k string, def float64) float64 {
		if p.has(k) {
			return p.num(k)
		}
		return def
	}
	shares := map[string]float64{"term": pct("termShare", 40), "health": pct("healthShare", 30), "motor": pct("motorShare", 30)}
	prem := map[string]float64{"term": pct("termPremium", 15000), "health": pct("healthPremium", 20000), "motor": pct("motorPremium", 8000)}
	rate1 := map[string]float64{"term": pct("termRate", 30), "health": pct("healthRate", 15), "motor": pct("motorRate", 12)}
	rate2 := map[string]float64{"term": pct("termRenewalRate", 7.5), "health": pct("healthRenewalRate", rate1["health"]), "motor": pct("motorRenewalRate", rate1["motor"])}
	rise := map[string]float64{"term": 0, "health": pct("healthIncrease", 8), "motor": pct("motorIncrease", 0)}
	if s := shares["term"] + shares["health"] + shares["motor"]; math.Abs(s-100) > 0.01 {
		return nil, refuse("termShare", fmt.Sprintf("The product mix adds up to %.0f%%; make it 100%%.", s))
	}
	customers := p.num("targetCustomers")
	if customers <= 0 && p.num("targetCommission") > 0 {
		perCustomer := 0.0
		for k, sh := range shares {
			perCustomer += sh / 100 * prem[k] * rate1[k] / 100
		}
		if perCustomer > 0 {
			customers = math.Ceil(p.num("targetCommission") / perCustomer)
		}
	}
	if customers <= 0 {
		return nil, refuse("targetCustomers", "Set the number of new customers, or a commission goal.")
	}
	renew := pct("renewalPercent", 90) / 100
	var y1p, y1c, y2p, y2c float64
	for k, sh := range shares {
		n := customers * sh / 100
		y1p += n * prem[k]
		y1c += n * prem[k] * rate1[k] / 100
		p2 := prem[k] * (1 + rise[k]/100)
		y2p += n * renew * p2
		y2c += n * renew * p2 * rate2[k] / 100
	}
	quotes := math.Ceil(customers / (pct("quoteToPolicy", 25) / 100))
	leads := math.Ceil(quotes / (pct("leadToQuote", 50) / 100))
	months := []string{}
	start, _ := time.Parse("2006-01-02", firstNonEmpty(p.text("planStart"), today())[:10])
	for m := 0; m < 12; m++ {
		months = append(months, fmt.Sprintf("%s: %.0f leads, %.0f quotes, %.1f policies", start.AddDate(0, m, 0).Format("Jan 2006"), leads/12, quotes/12, customers/12))
	}
	set := map[string]any{"targetCustomers": customers, "year1Premium": round2(y1p), "year1Commission": round2(y1c), "year1Rate": round2(y1c / y1p * 100),
		"year2Premium": round2(y2p), "year2Commission": round2(y2c), "year2Rate": round2(y2c / y2p * 100), "quotesNeeded": quotes, "leadsNeeded": leads,
		"monthlyPlan": fmt.Sprintf("%.0f customers ÷ %.0f%% quotes that become policies = %.0f quotes; ÷ %.0f%% leads that reach a quote = %.0f leads.\nYear 2 is Estimated (%.0f%% renew; health +%.0f%%).\n%s",
			customers, pct("quoteToPolicy", 25), quotes, pct("leadToQuote", 50), leads, renew*100, rise["health"], strings.Join(months, "\n"))}
	if op == "create" && p.has("previousPlanId") {
		if prev, _, err := h.getRow(ctx, tx, ws, specFor("sales_plans"), p.id("previousPlanId"), nil); err == nil {
			set["changeVsPrevious"] = fmt.Sprintf("Customers %+.0f, year-1 commission %+.0f against %s", customers-prev.num("targetCustomers"), round2(y1c-prev.num("year1Commission")), prev.Title)
			if _, err := h.setSys(ctx, tx, ws, "sales_plans", prev, map[string]any{"status": "superseded"}); err != nil {
				return nil, err
			}
		}
	}
	p, err := h.setSys(ctx, tx, ws, "sales_plans", p, set)
	if err != nil {
		return nil, err
	}
	return h.planActuals(ctx, tx, ws, p, false)
}

// planActuals: customers issued in the plan year counted by issue date (start of the first
// term), against the plan to date; an alert task once a month when behind (J20).
func (h *Handler) planActuals(ctx context.Context, tx pgx.Tx, ws uuid.UUID, p *Row, alert bool) (*Row, error) {
	start, err := time.Parse("2006-01-02", firstNonEmpty(p.text("planStart"), today())[:10])
	if err != nil {
		return p, nil
	}
	end := start.AddDate(1, 0, 0)
	var customers, premium, commission float64
	_ = tx.QueryRow(ctx, `SELECT count(DISTINCT custom->>'contactId'), COALESCE(sum(`+numeric(`custom->>'premium'`)+`), 0), COALESCE(sum(`+numeric(`custom->>'commissionAmount'`)+`), 0)
		FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'insurance_policies' AND deleted_at IS NULL
		AND status IN ('in_force', 'renewed', 'lapsed', 'matured') AND COALESCE(custom->>'previousPolicyId', '') = ''
		AND custom->>'startDate' >= $2 AND custom->>'startDate' < $3`, ws, start.Format("2006-01-02"), end.Format("2006-01-02")).Scan(&customers, &premium, &commission)
	elapsed := time.Since(start).Hours() / 24 / 365
	elapsed = math.Max(0, math.Min(1, elapsed))
	toDate := math.Round(p.num("targetCustomers") * elapsed)
	behind := 0.0
	if toDate > 0 && customers < toDate {
		behind = round2((toDate - customers) / toDate * 100)
	}
	var openQuotes float64
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'quotes' AND deleted_at IS NULL AND status IN ('draft', 'sent')`, ws).Scan(&openQuotes)
	coverage := 0.0
	if need := p.num("targetCustomers") - customers; need > 0 {
		coverage = round2(openQuotes * p.num("quoteToPolicy") / 100 / need * 100)
		if !p.has("quoteToPolicy") {
			coverage = round2(openQuotes * 0.25 / need * 100)
		}
	}
	set := map[string]any{"actualCustomers": customers, "actualPremium": round2(premium), "actualCommission": round2(commission), "planToDate": toDate, "behindBy": behind, "pipelineCoverage": coverage}
	limit := p.num("behindAlert")
	if !p.has("behindAlert") {
		limit = 20
	}
	month := time.Now().Format("2006-01")
	if alert && behind > limit && p.text("alertMonth") != month && p.text("status") != "superseded" {
		set["alertMonth"] = month
		if _, err := h.makeSys(ctx, tx, ws, "tasks", map[string]any{"name": fmt.Sprintf("Behind plan by %.0f%%: %.0f customers against %.0f planned to date", behind, customers, toDate),
			"status": "not_started", "priority": "high", "dueDate": today(), "ownerId": p.text("ownerId")}); err != nil {
			return nil, err
		}
	}
	return h.setSys(ctx, tx, ws, "sales_plans", p, set)
}

// ---- F7–F8: licences ----

func (h *Handler) licenceSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, l *Row) (*Row, error) {
	until, err := time.Parse("2006-01-02", l.text("validUntil")[:min(10, len(l.text("validUntil")))])
	if err != nil {
		return l, nil
	}
	status := "valid"
	switch left := time.Until(until).Hours() / 24; {
	case left < 0:
		status = "expired"
	case left <= 60:
		status = "expiring"
	}
	return h.setSys(ctx, tx, ws, "agent_licences", l, map[string]any{"status": status})
}

// licenceGate refuses to issue a policy for someone whose licence has expired, or with an
// insurer they aren't appointed with — once the business keeps licences at all.
func (h *Handler) licenceGate(ctx context.Context, tx pgx.Tx, ws uuid.UUID, p *Row) error {
	if specFor("agent_licences") == nil {
		return nil
	}
	var any int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'agent_licences' AND deleted_at IS NULL`, ws).Scan(&any)
	if any == 0 {
		return nil
	}
	seller := firstNonEmpty(p.text("subAgentId"), p.text("ownerId"))
	day := firstNonEmpty(p.text("startDate"), today())[:10]
	var valid int
	var appointed []byte
	err := tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE custom->>'validUntil' >= $3), COALESCE(jsonb_agg(custom->'appointedInsurers') FILTER (WHERE custom->>'validUntil' >= $3), '[]')
		FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'agent_licences' AND deleted_at IS NULL AND custom->>'holderId' = $2`, ws, seller, day).Scan(&valid, &appointed)
	if err != nil {
		return err
	}
	if valid == 0 {
		return refuse("status", "The seller's insurance licence isn't valid on the start date. Renew it in Licences & appointments first.")
	}
	if a := string(appointed); strings.Contains(a, "-") && !strings.Contains(a, p.text("insurerId")) {
		return refuse("insurerId", "The seller isn't appointed with this insurer.")
	}
	return nil
}

// ---- I2–I4: consent requests ----

func (h *Handler) consentRequestSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, r *Row) (*Row, error) {
	set := map[string]any{}
	if op == "create" {
		var tries, wrong int
		// A wrong number blocks every request to it; two attempts per purpose at most.
		_ = tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE COALESCE(custom->>'outcome', '') <> 'given' AND COALESCE(custom->>'purpose', '') = $4),
			count(*) FILTER (WHERE custom->>'outcome' = 'wrong_number')
			FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'consent_requests' AND deleted_at IS NULL AND id <> $3
			AND custom->>'contactId' = $2`, ws, r.text("contactId"), r.uuid(), r.text("purpose")).Scan(&tries, &wrong)
		if wrong > 0 {
			return nil, refuse("contactId", "This number was reported as a wrong number. Fix the mobile before asking again.")
		}
		if tries >= 2 {
			return nil, refuse("contactId", "Consent was already asked twice. The customer stays Consent pending; don't ask again.")
		}
		set["attempt"], set["sentAt"] = float64(tries+1), time.Now().Format(time.RFC3339)
		if !r.has("status") {
			set["status"] = "sent"
		}
	}
	out := r.text("outcome")
	if out != "" && (before == nil || before.text("outcome") != out) {
		set["status"] = "closed"
		if !r.has("repliedAt") && out != "no_reply" {
			set["repliedAt"] = time.Now().Format(time.RFC3339)
		}
		switch out {
		case "given":
			c, err := h.makeSys(ctx, tx, ws, "consents", map[string]any{"name": "Consent by " + r.text("channel"), "contactId": r.text("contactId"), "purpose": firstNonEmpty(map[string]string{"health": "sharing"}[r.text("purpose")], r.text("purpose")),
				"channel": map[string]string{"voice": "call"}[r.text("channel")] + map[bool]string{true: "whatsapp"}[r.text("channel") == "whatsapp"], "status": "given",
				"givenOn": time.Now().Format(time.RFC3339), "source": map[string]string{"voice": "phone", "whatsapp": "message"}[r.text("channel")],
				"evidence": firstNonEmpty(r.text("callId"), r.text("recordingRef"), r.Title), "ownerId": r.text("ownerId")})
			if err != nil {
				return nil, err
			}
			set["consentId"] = c.ID
		case "wrong_number":
			if _, err := h.makeSys(ctx, tx, ws, "tasks", map[string]any{"name": "Wrong number reported — check the customer's mobile", "contactId": r.text("contactId"),
				"status": "not_started", "priority": "high", "dueDate": today(), "ownerId": r.text("ownerId")}); err != nil {
				return nil, err
			}
		}
	}
	if len(set) == 0 {
		return r, nil
	}
	return h.setSys(ctx, tx, ws, "consent_requests", r, set)
}

// ---- H: existing book import ----

var (
	mobileRe   = regexp.MustCompile(`^(?:\+?91)?([6-9]\d{9})$`)
	aadhaarRe  = regexp.MustCompile(`\b\d{4}\s?\d{4}\s?\d{4}\b`)
	panRe      = regexp.MustCompile(`\b[A-Z]{5}\d{4}[A-Z]\b`)
	cardRe     = regexp.MustCompile(`\b(?:\d[ -]?){13,19}\b`)
	healthWord = regexp.MustCompile(`(?i)\b(diabet\w*|cancer|hypertension|bp|heart|kidney|asthma|thyroid|hiv|surgery|pregnan\w*)\b`)
)

type bookResult struct {
	status string
	notes  []string
}

func (b *bookResult) warn(s string) {
	if b.status == "ok" {
		b.status = "warning"
	}
	b.notes = append(b.notes, s)
}
func (b *bookResult) hold(s string) {
	if b.status != "rejected" {
		b.status = "held"
	}
	b.notes = append(b.notes, s)
}
func (b *bookResult) reject(s string) { b.status = "rejected"; b.notes = append(b.notes, s) }

func bookDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, f := range []string{"2006-01-02", "02-01-2006", "02/01/2006", "2/1/2006"} {
		if t, err := time.Parse(f, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// checkBookRow validates one row (H4, H10, H11, H17) and finds what it matches.
func (h *Handler) checkBookRow(ctx context.Context, tx pgx.Tx, ws uuid.UUID, r *Row, seen map[string]bool) (bookResult, map[string]any) {
	res := bookResult{status: "ok"}
	clean := map[string]any{}
	// Sensitive data never reaches the CRM: stripped and reported.
	for _, k := range []string{"notes", "nominee", "plan", "customerName", "relation"} {
		v := r.text(k)
		stripped := aadhaarRe.ReplaceAllString(panRe.ReplaceAllString(cardRe.ReplaceAllString(v, "[removed]"), "[removed]"), "[removed]")
		stripped = healthWord.ReplaceAllString(stripped, "[removed]")
		if stripped != v {
			clean[k] = stripped
			res.warn("sensitive data removed from " + k)
		}
	}
	m := mobileRe.FindStringSubmatch(strings.NewReplacer(" ", "", "-", "").Replace(r.text("mobile")))
	if m == nil {
		res.reject("mobile is not a valid 10-digit Indian number")
	} else {
		clean["mobile"] = "+91" + m[1]
	}
	if strings.TrimSpace(r.text("customerName")) == "" {
		res.reject("no customer name (every policy needs a holder)")
	}
	if e := strings.TrimSpace(r.text("email")); e != "" && !strings.Contains(e, "@") {
		res.warn("email looks wrong")
	}
	if dob, ok := bookDate(r.text("dateOfBirth")); r.has("dateOfBirth") && (!ok || dob.After(time.Now())) {
		res.warn("date of birth is not a past date")
	}
	if r.has("policyNumber") {
		num := strings.ToUpper(strings.Join(strings.Fields(r.text("policyNumber")), ""))
		clean["policyNumber"] = num
		key := strings.ToLower(r.text("insurer")) + "|" + num
		if seen[key] {
			res.reject("policy number repeated in the file")
		}
		seen[key] = true
		var dup int
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'insurance_policies' AND deleted_at IS NULL
			AND upper(replace(custom->>'policyNumber', ' ', '')) = $2`, ws, num).Scan(&dup)
		if dup > 0 {
			res.reject("policy number already in the CRM")
		}
		var insurer string
		_ = tx.QueryRow(ctx, `SELECT id::text FROM crm.accounts WHERE workspace_id = $1 AND deleted_at IS NULL AND (lower(name) = lower($2) OR lower(name) LIKE '%' || lower($2) || '%') ORDER BY length(name) LIMIT 1`,
			ws, strings.TrimSpace(r.text("insurer"))).Scan(&insurer)
		if insurer == "" || strings.TrimSpace(r.text("insurer")) == "" {
			res.hold("insurer '" + r.text("insurer") + "' is not in your insurers — add it or fix the name")
		} else {
			clean["insurerId"] = insurer
		}
		start, okS := bookDate(r.text("startDate"))
		end, okE := bookDate(r.text("endDate"))
		if !okS {
			res.reject("start date missing or not a date")
		}
		if okS && okE && end.Before(start) {
			res.reject("end date is before the start date")
		}
		status := strings.ToLower(strings.TrimSpace(r.text("policyStatus")))
		if status == "" || status == "in force" || status == "inforce" {
			status = "in_force"
		}
		if okE && status == "in_force" && end.Before(time.Now()) {
			res.hold("in force but its end date has passed — check the status")
		}
		clean["status"] = status
		if p, err := strconv.ParseFloat(strings.ReplaceAll(r.text("premium"), ",", ""), 64); err != nil || p <= 0 {
			res.reject("premium must be a positive amount")
		} else {
			clean["premium"] = p
			if sa, err := strconv.ParseFloat(strings.ReplaceAll(r.text("sumAssured"), ",", ""), 64); err == nil && sa > 0 && p > sa {
				res.warn("premium is above the sum assured")
			}
		}
		if (strings.Contains(strings.ToLower(r.text("policyClass")), "life")) && !r.has("nominee") {
			res.warn("no nominee on a life policy")
		}
		if aor := strings.ToLower(strings.TrimSpace(r.text("agentOfRecord"))); aor != "" && aor != "self" && aor != "us" && aor != "me" {
			res.warn("agent of record is someone else — imported as Transfer pending")
			clean["transfer"] = true
		}
	}
	return res, clean
}

func (h *Handler) bookBatchStatus(ctx context.Context, tx pgx.Tx, ws uuid.UUID, before, b *Row) (*Row, error) {
	to := b.text("status")
	if before.text("status") == "archived" {
		return nil, refuse("status", "An archived batch stays archived.")
	}
	if to == "imported" && before.text("status") != "checked" {
		return nil, refuse("status", "Check the rows first (set the batch to Checked).")
	}
	if to == "archived" {
		_, err := tx.Exec(ctx, `UPDATE crm.object_records SET status = CASE WHEN status = 'imported' THEN status ELSE 'rejected' END, updated_at = now()
			WHERE workspace_id = $1 AND object_key = 'book_rows' AND custom->>'batchId' = $2 AND status <> 'imported'`, ws, b.ID)
		return b, err
	}
	if to != "checked" && to != "imported" {
		return b, nil
	}
	rows, err := tx.Query(ctx, `SELECT id FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'book_rows' AND deleted_at IS NULL AND custom->>'batchId' = $2
		ORDER BY COALESCE(NULLIF(custom->>'rowNumber', '')::numeric, 0), created_at`, ws, b.ID)
	if err != nil {
		return nil, err
	}
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	count := map[string]float64{}
	var premiumAll, premiumIn, made, matched, policies float64
	report := []string{}
	seen := map[string]bool{}
	households := map[string]string{}
	for _, id := range ids {
		r, _, err := h.getRow(ctx, tx, ws, specFor("book_rows"), id, nil)
		if err != nil || r.text("status") == "imported" {
			continue
		}
		res, c := h.checkBookRow(ctx, tx, ws, r, seen)
		if p, ok := c["premium"].(float64); ok {
			premiumAll += p
		}
		count[res.status]++
		msg := strings.Join(res.notes, "; ")
		if msg != "" {
			report = append(report, fmt.Sprintf("Row %.0f (%s): %s — %s", r.num("rowNumber"), r.text("customerName"), res.status, msg))
		}
		set := map[string]any{"status": res.status, "message": msg}
		for _, k := range []string{"notes", "nominee", "plan", "customerName", "relation"} {
			if v, ok := c[k]; ok {
				set[k] = v
			}
		}
		if to == "imported" && (res.status == "ok" || res.status == "warning") {
			// H6–H8: match the customer by mobile, else create; family rows share a household.
			var contact, leadID string
			_ = tx.QueryRow(ctx, `SELECT id::text FROM crm.contacts WHERE workspace_id = $1 AND deleted_at IS NULL AND (mobile = $2 OR phone = $2) LIMIT 1`, ws, c["mobile"]).Scan(&contact)
			if contact == "" {
				// A lead with this mobile becomes the customer and keeps its history (H7).
				_ = tx.QueryRow(ctx, `SELECT id::text FROM crm.leads WHERE workspace_id = $1 AND deleted_at IS NULL AND (mobile = $2 OR phone = $2)
					AND COALESCE(status, '') NOT IN ('converted') ORDER BY created_at LIMIT 1`, ws, c["mobile"]).Scan(&leadID)
			}
			household := households[strings.ToLower(r.text("householdRef"))]
			if household == "" && r.has("householdRef") {
				hh, err := h.makeSys(ctx, tx, ws, "accounts", map[string]any{"name": r.text("householdRef") + " household", "kind": "individual", "type": "customer"})
				if err != nil {
					return nil, err
				}
				household = hh.ID
				households[strings.ToLower(r.text("householdRef"))] = household
			}
			if contact == "" {
				name := strings.Fields(asString(firstNonEmptyAny(set["customerName"], r.text("customerName"))))
				first, last := "", strings.Join(name, " ")
				if len(name) > 1 {
					first, last = name[0], strings.Join(name[1:], " ")
				}
				ct, err := h.createRecord(ctx, tx, ws, specFor("contacts"), systemActor("import"), map[string]any{"firstName": first, "lastName": last, "mobile": c["mobile"],
					"email": strings.TrimSpace(r.text("email")), "accountId": household, "leadSource": "other", "description": "Imported from " + b.Title + ". Consent pending."})
				if err != nil {
					set["status"], set["message"] = "rejected", "customer couldn't be created: "+err.Error()
					count["rejected"]++
					if _, err := h.setSys(ctx, tx, ws, "book_rows", r, set); err != nil {
						return nil, err
					}
					continue
				}
				contact = ct.ID
				if leadID != "" {
					if _, err := tx.Exec(ctx, `UPDATE crm.leads SET status = 'converted', converted_at = now(), converted_contact_id = $3, updated_at = now() WHERE id::text = $1 AND workspace_id = $2`,
						leadID, ws, ct.uuid()); err != nil {
						return nil, err
					}
					matched++
				} else {
					made++
				}
			} else {
				matched++
			}
			set["contactId"] = contact
			if r.has("policyNumber") {
				pv := map[string]any{"name": strings.TrimSpace(r.text("plan") + " — " + r.text("customerName")), "contactId": contact, "accountId": household,
					"insurerId": c["insurerId"], "policyClass": strings.ToLower(strings.Fields(r.text("policyClass") + " general")[0]), "policyNumber": c["policyNumber"],
					"startDate": mustDate(r.text("startDate")), "endDate": mustDate(r.text("endDate")), "premium": c["premium"], "frequency": normFreq(r.text("frequency")),
					"collectionMode": map[bool]string{true: "agent", false: "direct"}[strings.TrimSpace(r.text("collectionMode")) == "2"], "nominee": firstNonEmptyAny(set["nominee"], r.text("nominee")),
					"status": c["status"], "ownerId": b.text("ownerId")}
				if rate, err := strconv.ParseFloat(strings.TrimSuffix(r.text("commissionRate"), "%"), 64); err == nil {
					pv["commissionRate"] = rate
				}
				if c["transfer"] == true {
					pv["status"], pv["agentOfRecord"] = "transfer_pending", "other_agent"
				}
				clean(pv)
				p, err := h.createRecord(ctx, tx, ws, specFor("insurance_policies"), systemActor("import"), pv)
				if err != nil {
					set["status"], set["message"] = "rejected", strings.TrimPrefix(err.Error(), "validation: ")
					count["rejected"]++
				} else {
					set["policyId"] = p.ID
					premiumIn += asFloat(pv["premium"])
					policies++
					set["status"] = "imported"
				}
			} else {
				set["status"] = "imported"
			}
		}
		if _, err := h.setSys(ctx, tx, ws, "book_rows", r, set); err != nil {
			return nil, err
		}
	}
	sort.Strings(report)
	out := map[string]any{"rowsTotal": float64(len(ids)), "rowsOk": count["ok"], "rowsWarning": count["warning"], "rowsHeld": count["held"], "rowsRejected": count["rejected"],
		"premiumTotal": round2(premiumAll), "report": strings.Join(report, "\n")}
	if to == "imported" {
		out["customersCreated"], out["customersMatched"], out["policiesCreated"], out["premiumImported"] = made, matched, policies, round2(premiumIn)
	}
	return h.setSys(ctx, tx, ws, "book_batches", b, out)
}

func mustDate(s string) string {
	if t, ok := bookDate(s); ok {
		return t.Format("2006-01-02")
	}
	return ""
}

func normFreq(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "monthly", "m":
		return "monthly"
	case "quarterly", "q":
		return "quarterly"
	case "half-yearly", "half_yearly", "half yearly", "h":
		return "half_yearly"
	case "single":
		return "single"
	}
	return "yearly"
}

func firstNonEmptyAny(v ...any) string {
	for _, x := range v {
		if s := asString(x); strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func asFloat(v any) float64 {
	f, _ := v.(float64)
	return f
}

// ---- the date-driven part: licences, maturity, birthdays, claim clocks, plans ----

func (h *Handler) sweepInsuranceMore(ctx context.Context) {
	if specFor("policy_years") == nil {
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
	once := func(ws uuid.UUID, name string, fn func(tx pgx.Tx) error) {
		_ = h.store.WithTx(ctx, func(tx pgx.Tx) error {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'tasks' AND name = $2`, ws, name).Scan(&n); err != nil || n > 0 {
				return err
			}
			return fn(tx)
		})
	}
	// Licences: status by date; an alert 60 days before expiry (F7).
	for _, r := range collect(`SELECT id, workspace_id, COALESCE(custom->>'holderId', '') FROM crm.object_records WHERE object_key = 'agent_licences' AND deleted_at IS NULL
		AND NULLIF(custom->>'validUntil', '')::date <= now()::date + 60 LIMIT 500`) {
		_ = h.store.WithTx(ctx, func(tx pgx.Tx) error {
			l, _, err := h.getRow(ctx, tx, r.ws, specFor("agent_licences"), r.id, nil)
			if err != nil {
				return nil
			}
			if l, err = h.licenceSaved(ctx, tx, r.ws, l); err != nil || isTrue(l.Values["alertSent"]) {
				return err
			}
			if err := task(tx, r.ws, "Licence expires on "+l.text("validUntil")+": renew it ("+l.Title+")", firstNonEmpty(r.extra, l.text("ownerId")), ""); err != nil {
				return err
			}
			_, err = h.setSys(ctx, tx, r.ws, "agent_licences", l, map[string]any{"alertSent": true})
			return err
		})
	}
	// Maturity: a review call 60 days before (D13).
	for _, r := range collect(`SELECT id, workspace_id, name FROM crm.object_records WHERE object_key = 'insurance_policies' AND deleted_at IS NULL AND status = 'in_force'
		AND NULLIF(custom->>'maturityDate', '')::date BETWEEN now()::date AND now()::date + 60 LIMIT 500`) {
		p := r
		once(r.ws, "Maturity coming: list documents and plan the payout — "+r.extra, func(tx pgx.Tx) error {
			row, _, err := h.getRow(ctx, tx, p.ws, specFor("insurance_policies"), p.id, nil)
			if err != nil {
				return nil
			}
			return task(tx, p.ws, "Maturity coming: list documents and plan the payout — "+row.Title, row.text("ownerId"), firstNonEmpty(row.text("payerContactId"), row.text("contactId")))
		})
	}
	// Birthdays of policyholders: one greeting task a year, only with marketing consent (D15, I5).
	year := time.Now().Format("2006")
	for _, r := range collect(`SELECT c.id, c.workspace_id, COALESCE(c.first_name || ' ', '') || c.last_name FROM crm.contacts c
		WHERE c.deleted_at IS NULL AND c.birthdate IS NOT NULL AND to_char(c.birthdate, 'MM-DD') = to_char(now(), 'MM-DD')
		AND EXISTS (SELECT 1 FROM crm.object_records p WHERE p.workspace_id = c.workspace_id AND p.object_key = 'insurance_policies' AND p.deleted_at IS NULL AND p.custom->>'contactId' = c.id::text)
		AND EXISTS (SELECT 1 FROM crm.object_records s WHERE s.workspace_id = c.workspace_id AND s.object_key = 'consents' AND s.deleted_at IS NULL AND s.status = 'given'
		            AND s.custom->>'contactId' = c.id::text AND s.custom->>'purpose' = 'marketing') LIMIT 500`) {
		c := r
		once(r.ws, "Birthday greeting "+year+": "+r.extra, func(tx pgx.Tx) error {
			var owner string
			_ = tx.QueryRow(ctx, `SELECT owner_id::text FROM crm.contacts WHERE id = $1`, c.id).Scan(&owner)
			return task(tx, c.ws, "Birthday greeting "+year+": "+c.extra, owner, c.id.String())
		})
	}
	// Claim clocks: a first response missed is flagged with an urgent task (E5, E11).
	for _, r := range collect(`SELECT id, workspace_id, name FROM crm.object_records WHERE object_key = 'insurance_claims' AND deleted_at IS NULL
		AND status IN ('intimated') AND COALESCE(custom->>'firstResponseAt', '') = '' AND COALESCE(custom->>'responseBreached', 'false') <> 'true'
		AND NULLIF(custom->>'responseDueAt', '')::timestamptz < now() LIMIT 500`) {
		_ = h.store.WithTx(ctx, func(tx pgx.Tx) error {
			c, _, err := h.getRow(ctx, tx, r.ws, specFor("insurance_claims"), r.id, nil)
			if err != nil {
				return nil
			}
			if _, err := h.setSys(ctx, tx, r.ws, "insurance_claims", c, map[string]any{"responseBreached": true}); err != nil {
				return err
			}
			return task(tx, r.ws, "Claim response time missed — respond now: "+c.Title, c.text("ownerId"), c.text("contactId"))
		})
	}
	// Plans: actuals refreshed, an alert when behind (J20).
	for _, r := range collect(`SELECT id, workspace_id, '' FROM crm.object_records WHERE object_key = 'sales_plans' AND deleted_at IS NULL AND status <> 'superseded' LIMIT 200`) {
		_ = h.store.WithTx(ctx, func(tx pgx.Tx) error {
			p, _, err := h.getRow(ctx, tx, r.ws, specFor("sales_plans"), r.id, nil)
			if err != nil {
				return nil
			}
			_, err = h.planActuals(ctx, tx, r.ws, p, true)
			return err
		})
	}
}

// claimClock sets a claim's first-response target when it is logged, and stops it on the
// first response (E5).
func (h *Handler) claimClock(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, c *Row) (*Row, error) {
	set := map[string]any{}
	if !c.has("responseDueAt") {
		hours := 24
		if c.text("claimType") == "health_cashless" {
			hours = 2
		}
		set["responseDueAt"] = time.Now().Add(time.Duration(hours) * time.Hour).Format(time.RFC3339)
	}
	if op == "update" && before.text("status") == "intimated" && c.text("status") != "intimated" && !c.has("firstResponseAt") {
		set["firstResponseAt"] = time.Now().Format(time.RFC3339)
	}
	if c.text("grievanceStage") != "" && c.text("grievanceStage") != "none" && !c.has("escalationResponseDue") {
		set["escalationResponseDue"] = time.Now().AddDate(0, 0, map[string]int{"insurer_grievance": 15, "ombudsman": 90}[c.text("grievanceStage")]).Format("2006-01-02")
	}
	if len(set) == 0 {
		return c, nil
	}
	return h.setSys(ctx, tx, ws, "insurance_claims", c, set)
}

// sensitiveObjects are logged each time someone opens a record of theirs (I7, I11).
var sensitiveObjects = map[string]bool{"fact_finds": true, "insurance_claims": true, "legal_matters": true, "legal_documents": true, "case_updates": true, "conflict_checks": true}

func (h *Handler) logSensitiveView(ctx context.Context, ws uuid.UUID, object string, id uuid.UUID, who uuid.UUID, ip, reqID string) {
	if !sensitiveObjects[object] {
		return
	}
	_ = shared.WriteAudit(ctx, h.store.Pool, shared.AuditEvent{WorkspaceID: &ws, ActorID: &who, Action: "record.viewed", EntityType: object, EntityID: &id, IP: ip, RequestID: reqID})
}

// insuranceTrade: the business is an insurance agency (its quotes carry commission fields).
func (h *Handler) insuranceTrade(ctx context.Context, q querier, ws uuid.UUID) bool {
	var ok bool
	_ = q.QueryRow(ctx, `SELECT lower(COALESCE(profile->>'industry', '')) = 'insurance' OR COALESCE(profile->>'packs', '') ILIKE '%insurance%' FROM crm.workspaces WHERE id = $1`, ws).Scan(&ok)
	return ok
}

// GET /insurance/book-template.csv — the existing-book template with example rows and the
// allowed values (H1). Import it into Book rows, then check and import the batch.
func (h *Handler) handleBookTemplate(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.Can("book_rows", "create") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="insurance-book-template-v1.csv"`)
	_, _ = w.Write([]byte("\ufeffbatchId,rowNumber,customerName,mobile,email,dateOfBirth,householdRef,relation,insurer,plan,policyClass,policyNumber,policyStatus,startDate,endDate,sumAssured,premium,frequency,collectionMode,commissionRate,agentOfRecord,nominee,consent,notes\n" +
		"<batch id>,1,Lata Subramanian,9876543210,lata@example.com,1975-03-14,SUBRAMANIAN,self,LIC of India,Jeevan Anand,life,LIC123456789,in force,2024-04-01,2025-03-31,500000,24000,yearly,1,25,self,Ravi Subramanian (son),yes,\n" +
		"<batch id>,2,Ravi Subramanian,9876543211,,2001-07-02,SUBRAMANIAN,son,Star Health,Family Health Optima,health,STAR-99887,in force,2024-06-10,2025-06-09,500000,18000,yearly,2,15,self,,unknown,\n" +
		"# allowed: policyStatus in force|lapsed|matured|cancelled; frequency yearly|half-yearly|quarterly|monthly|single; collectionMode 1 (customer pays insurer) or 2 (we collect); agentOfRecord self|other agent|insurer direct; consent yes|no|unknown. Never put health details or full Aadhaar/PAN/bank numbers in any column.\n"))
}

// insuranceUpgrades adds this round's fields to the insurance objects businesses already have.
func insuranceUpgrades() map[string]objectUpgrade {
	pick := func(key string, keys ...string) []ObjectField {
		want := map[string]bool{}
		for _, k := range keys {
			want[k] = true
		}
		out := []ObjectField{}
		for _, d := range append(append(insuranceCoreObjects(), packMoreObjects()...), allStandardObjects()...) {
			if d.Key != key {
				continue
			}
			for _, f := range d.Fields {
				if want[f.Key] {
					out = append(out, f)
				}
			}
			break
		}
		return out
	}
	return map[string]objectUpgrade{
		"insurance_policies": {fields: pick("insurance_policies", "premiumType", "policyTerm", "premiumPayingTerm", "yearlyIncrease", "stepSchedule", "maturityDate",
			"payerContactId", "subAgentId", "subAgentShare", "agentOfRecord", "transferConfirmedOn"),
			statuses: []StatusOption{st("transfer_pending", "Transfer pending", "warning")}},
		"commissions":         {fields: pick("commissions", "invoiceId", "payoutCreated")},
		"insurance_claims":    {fields: pick("insurance_claims", "responseDueAt", "firstResponseAt", "responseBreached", "grievanceStage", "escalationReference", "escalationResponseDue")},
		"quotes":              {fields: pick("quotes", "commissionYear1", "commissionRateYear1", "commissionRenewal", "commissionRateRenewal")},
		"checklist_templates": {fields: pick("checklist_templates", "conditionField", "conditionValue")},
	}
}

func isTrue(v any) bool { return fmt.Sprint(v) == "true" }
