package records

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Shared operations modules (D-152). The 110 business PRDs ask, trade after trade, for the
// same handful of things; they are built once here, for every business, present and future:
//
//	stock_lots, stock_moves          stock by item, place and batch, with expiry and quarantine
//	item_reservations                any item or resource booked for a time, never twice
//	memberships                      packages and memberships: sessions, validity, freeze, renewal
//	checklist_templates/_items       a checklist that must be done before a record reaches a stage
//	doc_revisions                    versions of a document (drawing, artwork, menu…) and sign-off
//	instalments                      a payment plan: what is due when, what was paid
//	payout_entries                   commissions and payouts to staff, partners or referrers
//	business_branches                branches / locations
//
// plus a credit limit on accounts that stops orders and invoices above it.

const sourceOps = "ops"

func commonOpsObjects() []ObjectDefinition {
	refObj := ObjectField{Key: "objectKey", Label: "Applies to (object)", Type: "text", Required: true, HelpText: "The record type's API name, e.g. work_orders, opportunities, vehicle_bookings"}
	return []ObjectDefinition{
		{Key: "stock_lots", Module: "inventory", Singular: "Stock item", Plural: "Stock", Icon: "package", Prefix: "STK",
			Description: "What you hold: item, place, batch or serial, quantity, reserved and available, expiry.",
			ObjectBody: ObjectBody{NameLabel: "Stock item", StatusLabel: "Status", Statuses: []StatusOption{
				st("available", "Available", "success"), st("low", "Running low", "warning"), st("out", "Out of stock", "danger"),
				st("quarantined", "On hold — do not use", "danger"), st("expired", "Expired", "danger")},
				Fields: []ObjectField{
					ofReq(ofLookup("itemId", "Item", "catalog_items")), ofLookup("branchId", "Branch", "business_branches"), of("location", "Shelf / bin / place", "text"),
					of("batchNumber", "Batch / lot", "text"), of("serialNumber", "Serial number", "text"), of("expiryDate", "Expires on", "date"),
					ofSelect("unit", "Counted in", unitOptions),
					of("quantity", "On hand", "number"), of("reserved", "Reserved", "number"), of("available", "Available", "number"),
					of("reorderLevel", "Reorder when available is below", "number"), of("unitCost", "Cost per unit", "currency"),
					of("holdReason", "Why it is on hold", "text"), of("notes", "Notes", "textarea")}}},
		{Key: "stock_moves", Module: "inventory", Singular: "Stock movement", Plural: "Stock movements", Icon: "truck", Prefix: "SMV",
			Description: "Every change to stock — received, issued, returned, reserved, moved, damaged or counted — with what it was for.",
			ObjectBody: ObjectBody{NameLabel: "Movement", Statuses: []StatusOption{},
				Fields: []ObjectField{
					ofReq(ofLookup("lotId", "Stock item", "stock_lots")),
					ofReq(ofSelect("moveType", "Movement", opts("receive", "Received", "issue", "Issued / used", "return", "Returned to stock", "reserve", "Reserved",
						"release", "Reservation released", "transfer", "Moved to another place", "damage", "Damaged / lost", "count", "Counted (stock check)"))),
					ofReq(of("quantity", "Quantity", "number")), of("toLocation", "Moved to (place)", "text"), ofLookup("toBranchId", "Moved to (branch)", "business_branches"),
					ofLookup("contactId", "Customer", "contacts"), ofLookup("salesOrderId", "Sales order", "sales_orders"), ofLookup("workOrderId", "Work order", "work_orders"),
					ofLookup("invoiceId", "Invoice", "invoices"), of("movedOn", "Date", "date"), of("reason", "Reason / reference", "text")}}},
		{Key: "item_reservations", Module: "reservations", Singular: "Reservation", Plural: "Reservations", Icon: "calendar", Prefix: "RSV",
			Description: "An item, room, vehicle, court or piece of equipment booked for a time — never to two people at once.",
			ObjectBody: ObjectBody{NameLabel: "Reservation", StatusLabel: "Status", Statuses: []StatusOption{
				st("hold", "Held (unconfirmed)", "warning"), st("confirmed", "Confirmed", "primary"), st("checked_out", "Handed over / in use", "warning"),
				st("returned", "Returned", "success"), st("cancelled", "Cancelled", "neutral"), st("no_show", "No show", "danger")},
				Fields: []ObjectField{
					ofLookup("assetId", "Item", "assets"), ofLookup("resourceId", "Room / resource", "service_resources"),
					ofLookup("contactId", "Customer", "contacts"), ofLookup("accountId", "Account", "accounts"), ofLookup("branchId", "Branch", "business_branches"),
					ofReq(of("startsAt", "From", "datetime")), ofReq(of("endsAt", "Until", "datetime")),
					of("bufferMinutes", "Gap needed before and after (minutes)", "number"), of("holdUntil", "Hold expires", "datetime"),
					of("price", "Price", "currency"), of("deposit", "Deposit taken", "currency"), of("depositReturned", "Deposit returned", "currency"),
					ofSelect("returnCondition", "Came back", opts("ok", "As given", "dirty", "Needs cleaning", "damaged", "Damaged", "missing", "Parts missing")),
					of("returnNotes", "Damage / missing / deductions", "text"), of("notes", "Notes", "textarea")}}},
		{Key: "memberships", Module: "memberships", Singular: "Membership", Plural: "Memberships & packages", Icon: "ticket", Prefix: "MEM",
			Description: "A membership or package a customer bought: valid from–to, sessions used and left, freezes, and its renewal.",
			ObjectBody: ObjectBody{NameLabel: "Membership", StatusLabel: "Status", Statuses: []StatusOption{
				st("active", "Active", "success"), st("frozen", "Frozen", "warning"), st("used_up", "Used up", "neutral"),
				st("expired", "Expired", "danger"), st("cancelled", "Cancelled", "neutral")},
				Fields: []ObjectField{
					ofReq(ofLookup("contactId", "Member", "contacts")), ofLookup("itemId", "Plan / package", "catalog_items"), ofLookup("orderId", "Sold on order", "sales_orders"), ofLookup("opportunityId", "Opportunity", "opportunities"), ofLookup("branchId", "Branch", "business_branches"),
					ofReq(of("startDate", "Starts", "date")), ofReq(of("endDate", "Ends", "date")),
					of("sessionsTotal", "Sessions included (empty = unlimited)", "number"), of("sessionsUsed", "Sessions used", "number"), of("sessionsLeft", "Sessions left", "number"),
					of("frozenFrom", "Frozen from", "date"), of("frozenUntil", "Frozen until", "date"), of("freezeDays", "Days frozen so far", "number"),
					of("price", "Price paid", "currency"), of("lastUsedOn", "Last used on", "date"), ofLookup("renewalDueId", "Renewal follow-up", "due_items"),
					of("cancelReason", "Why it was cancelled", "text"), of("notes", "Notes", "textarea")}}},
		{Key: "checklist_templates", Module: "checklists", Singular: "Checklist template", Plural: "Checklist templates", Icon: "check-square", Prefix: "CLT",
			Description: "The steps that must be done before a record reaches a stage — inspection before delivery, QC before handover, documents before review.",
			ObjectBody: ObjectBody{NameLabel: "Checklist name", StatusLabel: "Status", Statuses: []StatusOption{st("active", "Active", "success"), st("inactive", "Inactive", "neutral")},
				Fields: []ObjectField{refObj, ofReq(ObjectField{Key: "stage", Label: "Must be done before the stage (status value)", Type: "text", HelpText: "e.g. delivered, completed, ready"}),
					ofReq(ObjectField{Key: "items", Label: "Steps (one per line)", Type: "textarea"}),
					of("allRequired", "Every step is required", "boolean"), of("notes", "Notes", "textarea")}}},
		{Key: "checklist_items", Module: "checklists", Singular: "Checklist step", Plural: "Checklist steps", Icon: "check-square", Prefix: "CLI",
			Description: "One step of a checklist on one record: done or not, by whom and when.",
			ObjectBody: ObjectBody{NameLabel: "Step", Statuses: []StatusOption{},
				Fields: []ObjectField{ofReq(ofLookup("templateId", "Checklist", "checklist_templates")), refObj, ofReq(of("recordId", "Record ID", "text")),
					of("done", "Done", "boolean"), ofLookup("doneBy", "Done by", "users"), of("doneAt", "Done at", "datetime"),
					of("required", "Required", "boolean"), of("position", "Order", "number"), of("note", "Note", "text")}}},
		{Key: "doc_revisions", Module: "checklists", Singular: "Document version", Plural: "Document versions", Icon: "file-text", Prefix: "DOC",
			Description: "Each version of a drawing, design, artwork, menu, itinerary or scope, and whether the customer signed it off. Attach the file to the version.",
			ObjectBody: ObjectBody{NameLabel: "Document", StatusLabel: "Status", Statuses: []StatusOption{
				st("draft", "Draft", "neutral"), st("sent", "Sent to customer", "primary"), st("approved", "Approved", "success"),
				st("rejected", "Changes asked", "danger"), st("superseded", "Replaced by a newer version", "neutral")},
				Fields: []ObjectField{refObj, ofReq(of("recordId", "Record ID", "text")), ofLookup("contactId", "Customer", "contacts"),
					of("version", "Version", "number"), of("summary", "What changed", "textarea"),
					of("approvedByName", "Approved by (customer)", "text"), of("approvedOn", "Approved on", "date"), of("feedback", "Customer feedback", "textarea")}}},
		{Key: "instalments", Module: "sales_docs", Singular: "Instalment", Plural: "Payment plans", Icon: "banknote", Prefix: "INS",
			Description: "A payment plan: each amount due on a date — for an invoice, contract, booking or deal — and what was paid.",
			ObjectBody: ObjectBody{NameLabel: "Instalment", StatusLabel: "Status", Statuses: []StatusOption{
				st("due", "Due", "primary"), st("part_paid", "Part paid", "warning"), st("paid", "Paid", "success"), st("overdue", "Overdue", "danger"), st("waived", "Waived", "neutral")},
				Fields: []ObjectField{ofLookup("contactId", "Customer", "contacts"), ofLookup("accountId", "Account", "accounts"),
					ofLookup("invoiceId", "Invoice", "invoices"), ofLookup("contractId", "Contract", "contracts"), ofLookup("opportunityId", "Deal", "opportunities"),
					of("milestone", "Milestone / stage", "text"), ofReq(of("dueDate", "Due date", "date")), ofReq(of("amount", "Amount due", "currency")),
					of("paidAmount", "Paid", "currency"), of("paidOn", "Paid on", "date"), ofLookup("paymentId", "Payment", "payments"),
					of("isDeposit", "Refundable deposit (held, not income)", "boolean"), of("waiveReason", "Why it was waived", "text"),
					ofLookup("dueItemId", "Follow-up", "due_items"), of("notes", "Notes", "textarea")}}},
		{Key: "payout_entries", Module: "sales_docs", Singular: "Commission / payout", Plural: "Commissions & payouts", Icon: "banknote", Prefix: "PAYO",
			Description: "What is owed to a staff member, partner, broker or referrer for a sale or job: expected, approved, paid, clawed back.",
			ObjectBody: ObjectBody{NameLabel: "Payout", StatusLabel: "Status", Statuses: []StatusOption{
				st("expected", "Expected", "primary"), st("approved", "Approved", "warning"), st("paid", "Paid", "success"),
				st("clawed_back", "Clawed back", "danger"), st("cancelled", "Cancelled", "neutral")},
				Fields: []ObjectField{ofLookup("payeeUserId", "To staff member", "users"), ofLookup("payeeAccountId", "To partner / referrer", "accounts"),
					ofLookup("opportunityId", "Deal", "opportunities"), ofLookup("invoiceId", "Invoice", "invoices"), ofLookup("contactId", "Customer", "contacts"),
					ofSelect("kind", "Kind", opts("commission", "Commission", "referral", "Referral reward", "incentive", "Incentive / bonus", "rebate", "Rebate", "fee_share", "Fee share")),
					of("basisAmount", "Calculated on", "currency"), of("percent", "Rate", "percent"), of("amount", "Amount", "currency"),
					ofLookup("approvedBy", "Approved by", "users"), of("approvedOn", "Approved on", "date"), of("paidOn", "Paid on", "date"),
					of("reference", "Payment reference", "text"), of("reason", "Reason (clawback / cancel)", "text")}}},
		{Key: "business_branches", Module: "accounts", Singular: "Branch", Plural: "Branches", Icon: "building", Prefix: "BR",
			Description: "The business's branches and locations.",
			ObjectBody: ObjectBody{NameLabel: "Branch name", StatusLabel: "Status", Statuses: []StatusOption{st("active", "Active", "success"), st("closed", "Closed", "neutral")},
				Fields: []ObjectField{{Key: "branchCode", Label: "Short code", Type: "text", Unique: true}, of("address", "Address", "textarea"), of("city", "City", "text"),
					of("phone", "Phone", "phone"), ofLookup("managerId", "Manager", "users"), of("openingHours", "Opening hours", "text")}}},
	}
}

var unitOptions = opts("piece", "Pieces", "box", "Boxes", "bag", "Bags", "kg", "Kilograms", "g", "Grams", "tonne", "Tonnes", "l", "Litres", "ml", "Millilitres",
	"m", "Metres", "sqft", "Square feet", "hour", "Hours", "day", "Days")

func commonOpsUpgrades() map[string]objectUpgrade {
	branch := ofLookup("branchId", "Branch", "business_branches")
	return map[string]objectUpgrade{
		"catalog_items":  {fields: []ObjectField{ofSelect("unit", "Sold in", unitOptions), of("packSize", "Units per pack", "number"), ofSelect("packUnit", "Pack is a", unitOptions)}},
		"sales_orders":   {fields: []ObjectField{branch, of("creditExceptionReason", "Above credit limit because", "text")}},
		"invoices":       {fields: []ObjectField{branch, of("creditExceptionReason", "Above credit limit because", "text")}},
		"opportunities":  {fields: []ObjectField{branch}},
		"cases":          {fields: []ObjectField{branch}},
		"work_orders":    {fields: []ObjectField{branch}},
		"appointments":   {fields: []ObjectField{branch}},
		"quotes":         {fields: []ObjectField{branch}},
		"care_schedules": {fields: []ObjectField{branch}},
		"due_items":      {fields: []ObjectField{branch}},
	}
}

// linkUpgrades (D-154): records made from a sale point back at it.
func linkUpgrades() map[string]objectUpgrade {
	return map[string]objectUpgrade{
		"insurance_policies": {fields: []ObjectField{ofLookup("opportunityId", "Opportunity", "opportunities"), ofLookup("quoteId", "Quote", "quotes")}},
		"memberships":        {fields: []ObjectField{ofLookup("orderId", "Sold on order", "sales_orders"), ofLookup("opportunityId", "Opportunity", "opportunities")}},
	}
}

var commonOpsKeys = map[string]bool{"stock_lots": true, "stock_moves": true, "item_reservations": true, "memberships": true, "checklist_templates": true,
	"checklist_items": true, "doc_revisions": true, "instalments": true, "payout_entries": true, "business_branches": true}

func (h *Handler) opsSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, a actorInfo, op string, before, after *Row) (*Row, error) {
	if a.Source == sourceOps {
		return after, nil
	}
	set := map[string]any{}
	status, was := after.text("status"), before.text("status")
	changed := op == "create" || status != was
	apply := func() (*Row, error) {
		if len(set) == 0 {
			return after, nil
		}
		return h.updateValues(ctx, tx, ws, spec, after.uuid(), systemActor(sourceOps), set, nil, nil)
	}
	switch spec.Key {
	case "stock_lots":
		if op == "update" && a.Source != "stock" && (before.num("quantity") != after.num("quantity") || before.num("reserved") != after.num("reserved")) {
			return nil, refuse("quantity", "Change stock with a stock movement (received, issued, counted…), so every change is recorded.")
		}
		if after.num("quantity") < 0 {
			return nil, refuse("quantity", "Stock can't be below zero.")
		}
		if status == "quarantined" && !after.has("holdReason") {
			return nil, refuse("holdReason", "Say why it is on hold.")
		}
		q, r := after.num("quantity"), after.num("reserved")
		if avail := round2(q - r); !after.has("available") || after.num("available") != avail {
			set["available"] = avail
		}
		want := status
		switch {
		case status == "quarantined":
		case after.has("expiryDate") && after.text("expiryDate") < today():
			want = "expired"
		case q <= 0:
			want = "out"
		case after.has("reorderLevel") && q-r <= after.num("reorderLevel"):
			want = "low"
		default:
			want = "available"
		}
		if want != status {
			set["status"] = want
		}
		return apply()

	case "stock_moves":
		if op != "create" {
			return nil, refuse("quantity", "A stock movement can't be changed. Record a correction as a new movement.")
		}
		return h.applyStockMove(ctx, tx, ws, after)

	case "item_reservations":
		return h.reservationSaved(ctx, tx, ws, op, before, after)

	case "memberships":
		return h.membershipSaved(ctx, tx, ws, op, before, after)

	case "checklist_templates":
		if specFor(after.text("objectKey")) == nil {
			return nil, refuse("objectKey", "There is no record type with that API name. Use one from Settings → Objects & fields, e.g. work_orders.")
		}
		if strings.TrimSpace(after.text("items")) == "" {
			return nil, refuse("items", "Add at least one step.")
		}
		return after, nil

	case "checklist_items":
		if after.flag("done") && (op == "create" || !before.flag("done")) {
			set["doneAt"] = time.Now().UTC().Format(time.RFC3339)
			if a.ID != nil {
				set["doneBy"] = a.ID.String()
			}
		}
		if !after.flag("done") && before.flag("done") {
			set["doneAt"], set["doneBy"] = nil, nil
		}
		return apply()

	case "doc_revisions":
		if op == "update" && was == "approved" && (before.text("summary") != after.text("summary") || before.num("version") != after.num("version") || status != was) && status != "superseded" {
			return nil, refuse("status", "An approved version can't be changed. Make a new version instead.")
		}
		if op == "create" {
			var n float64
			_ = tx.QueryRow(ctx, `SELECT COALESCE(max((custom->>'version')::numeric), 0) + 1 FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'doc_revisions'
				AND deleted_at IS NULL AND custom->>'recordId' = $2 AND name = $3 AND id <> $4`, ws, after.text("recordId"), after.Title, after.uuid()).Scan(&n)
			set["version"] = n
		}
		if status == "approved" && changed {
			if !after.has("approvedByName") {
				return nil, refuse("approvedByName", "Who approved it (the customer's name)?")
			}
			if !after.has("approvedOn") {
				set["approvedOn"] = today()
			}
			// The earlier approved version of the same document is replaced, not lost.
			if _, err := tx.Exec(ctx, `UPDATE crm.object_records SET status = 'superseded', updated_at = now() WHERE workspace_id = $1 AND object_key = 'doc_revisions'
				AND deleted_at IS NULL AND custom->>'recordId' = $2 AND name = $3 AND id <> $4 AND status IN ('approved', 'sent', 'draft')`,
				ws, after.text("recordId"), after.Title, after.uuid()); err != nil {
				return nil, err
			}
		}
		return apply()

	case "instalments":
		amt, paid := after.num("amount"), after.num("paidAmount")
		if amt <= 0 {
			return nil, refuse("amount", "Enter the amount due.")
		}
		if paid < 0 || paid > amt+0.005 {
			return nil, refuse("paidAmount", fmt.Sprintf("Paid can't be more than the %.2f due.", amt))
		}
		if status == "waived" && !after.has("waiveReason") {
			return nil, refuse("waiveReason", "Say why it was waived.")
		}
		want := status
		if status != "waived" {
			switch {
			case paid >= amt-0.005:
				want = "paid"
			case paid > 0:
				want = "part_paid"
			case after.text("dueDate") < today():
				want = "overdue"
			default:
				want = "due"
			}
		}
		if want != status {
			set["status"] = want
		}
		if want == "paid" && !after.has("paidOn") {
			set["paidOn"] = today()
		}
		// Each instalment shows on the Upcoming page until it is paid or waived.
		if op == "create" && specFor("due_items") != nil && (after.has("contactId") || after.has("accountId")) && want != "paid" && want != "waived" {
			v := map[string]any{"name": joinNonEmpty(" — ", "Payment due", after.Title), "status": "open", "ownerId": after.text("ownerId"), "kind": "payment",
				"contactId": after.text("contactId"), "accountId": after.text("accountId"), "dueDate": after.text("dueDate"), "cycle": 1.0}
			clean(v)
			d, err := h.createRecord(ctx, tx, ws, specFor("due_items"), systemActor(sourceCare), v)
			if err != nil {
				return nil, err
			}
			set["dueItemId"] = d.ID
		}
		if (want == "paid" || want == "waived") && after.has("dueItemId") && (op == "create" || was != want) {
			dv := map[string]any{"status": "done", "doneOn": today()}
			if want == "waived" {
				dv = map[string]any{"status": "skipped", "skipReason": "not_needed"}
			}
			if _, err := h.updateValues(ctx, tx, ws, specFor("due_items"), after.id("dueItemId"), systemActor(sourceCare), dv, nil, nil); err != nil {
				return nil, err
			}
		}
		return apply()

	case "payout_entries":
		if !after.has("payeeUserId") && !after.has("payeeAccountId") {
			return nil, refuse("payeeUserId", "Pick who is paid: a staff member or a partner.")
		}
		if after.has("percent") && after.has("basisAmount") {
			if v := round2(after.num("basisAmount") * after.num("percent") / 100); v != after.num("amount") {
				set["amount"] = v
			}
		}
		if op == "update" && (was == "paid" || was == "cancelled" || was == "clawed_back") && status != was && !(was == "paid" && status == "clawed_back") {
			return nil, refuse("status", "This payout is closed.")
		}
		if status == "paid" && changed && was != "approved" {
			return nil, refuse("status", "A payout is paid only after it is approved.")
		}
		if status == "approved" && changed {
			if a.ID != nil && after.text("payeeUserId") == a.ID.String() {
				return nil, refuse("status", "You can't approve your own payout.")
			}
			if a.ID != nil {
				set["approvedBy"] = a.ID.String()
			}
			set["approvedOn"] = today()
		}
		if status == "paid" && changed && !after.has("paidOn") {
			set["paidOn"] = today()
		}
		if (status == "clawed_back" || status == "cancelled") && changed && !after.has("reason") {
			return nil, refuse("reason", "Say why.")
		}
		return apply()
	}
	return after, nil
}

// applyStockMove changes the stock item under a row lock: two people can't take the same
// last units, and nothing goes below zero or out while expired or on hold.
func (h *Handler) applyStockMove(ctx context.Context, tx pgx.Tx, ws uuid.UUID, m *Row) (*Row, error) {
	lot := m.id("lotId")
	if _, _, err := lockRecord(ctx, tx, ws, "stock_lots", lot); err != nil {
		return nil, refuse("lotId", "That stock item doesn't exist.")
	}
	l, _, err := h.getRow(ctx, tx, ws, specFor("stock_lots"), lot, nil)
	if err != nil {
		return nil, err
	}
	qty := m.num("quantity")
	if qty <= 0 && m.text("moveType") != "count" {
		return nil, refuse("quantity", "Enter a quantity above zero.")
	}
	onHand, reserved := l.num("quantity"), l.num("reserved")
	avail := onHand - reserved
	blocked := l.text("status") == "quarantined" || l.text("status") == "expired" || (l.has("expiryDate") && l.text("expiryDate") < today())
	set := map[string]any{}
	switch m.text("moveType") {
	case "receive", "return":
		set["quantity"] = round2(onHand + qty)
	case "issue", "transfer":
		if blocked {
			return nil, refuse("lotId", l.Title+" is expired or on hold. It can't be issued or moved for use.")
		}
		if qty > avail+0.0001 {
			return nil, refuse("quantity", fmt.Sprintf("Only %g available (%g on hand, %g reserved).", avail, onHand, reserved))
		}
		set["quantity"] = round2(onHand - qty)
	case "reserve":
		if blocked {
			return nil, refuse("lotId", l.Title+" is expired or on hold. It can't be promised.")
		}
		if qty > avail+0.0001 {
			return nil, refuse("quantity", fmt.Sprintf("Only %g available to reserve.", avail))
		}
		set["reserved"] = round2(reserved + qty)
	case "release":
		if qty > reserved+0.0001 {
			return nil, refuse("quantity", fmt.Sprintf("Only %g is reserved.", reserved))
		}
		set["reserved"] = round2(reserved - qty)
	case "damage":
		if qty > onHand+0.0001 {
			return nil, refuse("quantity", fmt.Sprintf("Only %g on hand.", onHand))
		}
		if !m.has("reason") {
			return nil, refuse("reason", "Say what happened — damage and loss must be explained.")
		}
		set["quantity"] = round2(onHand - qty)
		if reserved > onHand-qty {
			set["reserved"] = round2(onHand - qty)
		}
	case "count":
		if qty < reserved {
			return nil, refuse("quantity", fmt.Sprintf("%g is reserved; the count can't be lower.", reserved))
		}
		if !m.has("reason") {
			return nil, refuse("reason", "Say why the count differs (stock check, found, missing…).")
		}
		set["quantity"] = round2(qty)
	default:
		return nil, refuse("moveType", "Pick the kind of movement.")
	}
	if _, err := h.updateValues(ctx, tx, ws, specFor("stock_lots"), lot, actorInfo{Kind: "system", Source: "stock"}, set, nil, nil); err != nil {
		return nil, err
	}
	if m.text("moveType") == "transfer" {
		to, toBranch := strings.TrimSpace(m.text("toLocation")), m.text("toBranchId")
		if to == "" && toBranch == "" {
			return nil, refuse("toLocation", "Say where it is moved to.")
		}
		// The same item and batch at the destination gains what this one lost.
		var dest uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'stock_lots' AND deleted_at IS NULL
			AND custom->>'itemId' = $2 AND COALESCE(custom->>'batchNumber', '') = $3 AND COALESCE(custom->>'location', '') = $4
			AND COALESCE(custom->>'branchId', '') = $5 AND id <> $6 FOR UPDATE`, ws, l.text("itemId"), l.text("batchNumber"), to,
			firstOf(toBranch, l.text("branchId")), lot).Scan(&dest)
		if errors.Is(err, pgx.ErrNoRows) {
			v := map[string]any{"name": l.Title, "itemId": l.text("itemId"), "batchNumber": l.text("batchNumber"), "expiryDate": l.text("expiryDate"), "unit": l.text("unit"),
				"location": to, "branchId": firstOf(toBranch, l.text("branchId")), "quantity": round2(qty), "reserved": 0.0, "reorderLevel": l.Values["reorderLevel"],
				"unitCost": l.Values["unitCost"], "ownerId": l.text("ownerId")}
			clean(v)
			v["quantity"], v["reserved"] = round2(qty), 0.0
			if _, err := h.createRecord(ctx, tx, ws, specFor("stock_lots"), actorInfo{Kind: "system", Source: "stock"}, v); err != nil {
				return nil, err
			}
		} else if err != nil {
			return nil, err
		} else {
			d, _, err := h.getRow(ctx, tx, ws, specFor("stock_lots"), dest, nil)
			if err != nil {
				return nil, err
			}
			if _, err := h.updateValues(ctx, tx, ws, specFor("stock_lots"), dest, actorInfo{Kind: "system", Source: "stock"}, map[string]any{"quantity": round2(d.num("quantity") + qty)}, nil, nil); err != nil {
				return nil, err
			}
		}
	}
	if !m.has("movedOn") {
		return h.updateValues(ctx, tx, ws, specFor("stock_moves"), m.uuid(), systemActor(sourceOps), map[string]any{"movedOn": today()}, nil, nil)
	}
	return m, nil
}

func firstOf(xs ...string) string {
	for _, x := range xs {
		if x != "" {
			return x
		}
	}
	return ""
}

// reservationSaved: one item or resource is never given to two overlapping reservations
// (counting the gap needed before and after); a hold expires; a return says how it came back.
func (h *Handler) reservationSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, after *Row) (*Row, error) {
	status, was := after.text("status"), before.text("status")
	asset, res := after.id("assetId"), after.id("resourceId")
	if asset == uuid.Nil && res == uuid.Nil {
		return nil, refuse("assetId", "Pick the item or the room/resource being reserved.")
	}
	if after.text("endsAt") <= after.text("startsAt") {
		return nil, refuse("endsAt", "It must end after it starts.")
	}
	if op == "update" && (was == "returned" || was == "cancelled" || was == "no_show") && status != was {
		return nil, refuse("status", "This reservation is finished. Make a new one.")
	}
	set := map[string]any{}
	live := status == "hold" || status == "confirmed" || status == "checked_out"
	if live {
		// Lock what is reserved, then look for anything overlapping it.
		target, obj, col := asset, "assets", "assetId"
		if asset == uuid.Nil {
			target, obj, col = res, "service_resources", "resourceId"
		}
		var exists bool
		_ = tx.QueryRow(ctx, `SELECT true FROM crm.object_records WHERE id = $1 AND workspace_id = $2 AND object_key = $3 AND deleted_at IS NULL FOR UPDATE`, target, ws, obj).Scan(&exists)
		if !exists {
			return nil, refuse(col, "That item doesn't exist.")
		}
		var clash string
		err := tx.QueryRow(ctx, `SELECT o.code FROM crm.object_records o WHERE o.workspace_id = $1 AND o.object_key = 'item_reservations' AND o.deleted_at IS NULL
			AND o.id <> $2 AND o.custom->>'`+col+`' = $3 AND o.status IN ('hold', 'confirmed', 'checked_out')
			AND NOT (o.status = 'hold' AND NULLIF(o.custom->>'holdUntil', '')::timestamptz < now())
			AND (o.custom->>'startsAt')::timestamptz - make_interval(mins => GREATEST(COALESCE(NULLIF(o.custom->>'bufferMinutes', '')::numeric, 0), $6)::int) < $5::timestamptz
			AND (o.custom->>'endsAt')::timestamptz + make_interval(mins => GREATEST(COALESCE(NULLIF(o.custom->>'bufferMinutes', '')::numeric, 0), $6)::int) > $4::timestamptz
			LIMIT 1`, ws, after.uuid(), target.String(), after.text("startsAt"), after.text("endsAt"), after.num("bufferMinutes")).Scan(&clash)
		if err == nil {
			return nil, refuse(col, fmt.Sprintf("Already reserved for that time (%s), counting the gap needed before and after. Pick another time or item.", clash))
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
	}
	if status == "hold" && !after.has("holdUntil") {
		set["holdUntil"] = time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	}
	if status == "returned" && (op == "create" || was != "returned") {
		if was != "checked_out" {
			return nil, refuse("status", "Only something handed over can come back. Mark it handed over first.")
		}
		if !after.has("returnCondition") {
			return nil, refuse("returnCondition", "Say how it came back.")
		}
		if (after.text("returnCondition") == "damaged" || after.text("returnCondition") == "missing") && !after.has("returnNotes") {
			return nil, refuse("returnNotes", "Say what was damaged or missing — the deposit deduction depends on it.")
		}
	}
	if after.num("depositReturned") > after.num("deposit")+0.005 {
		return nil, refuse("depositReturned", "More can't be returned than the deposit taken.")
	}
	if len(set) == 0 {
		return after, nil
	}
	return h.updateValues(ctx, tx, ws, specFor("item_reservations"), after.uuid(), systemActor(sourceOps), set, nil, nil)
}

// membershipSaved: sessions never exceed what was bought or get used outside the
// validity or while frozen; a freeze moves the end date once; the renewal call is planned.
func (h *Handler) membershipSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, after *Row) (*Row, error) {
	status, was := after.text("status"), before.text("status")
	set := map[string]any{}
	if after.text("endDate") < after.text("startDate") {
		return nil, refuse("endDate", "It can't end before it starts.")
	}
	total, used := after.num("sessionsTotal"), after.num("sessionsUsed")
	limited := after.has("sessionsTotal")
	if used < 0 || (limited && used > total) {
		return nil, refuse("sessionsUsed", fmt.Sprintf("This has %g sessions. It can't have %g used.", total, used))
	}
	if op == "update" && used > before.num("sessionsUsed") {
		frozen := after.has("frozenFrom") && after.has("frozenUntil") && after.text("frozenFrom") <= today() && today() <= after.text("frozenUntil")
		if was == "cancelled" || was == "expired" || frozen || today() > after.text("endDate") || today() < after.text("startDate") {
			return nil, refuse("sessionsUsed", "It isn't usable today (not started, frozen, expired or cancelled).")
		}
		set["lastUsedOn"] = today()
	}
	if status == "cancelled" && !after.has("cancelReason") {
		return nil, refuse("cancelReason", "Say why it was cancelled.")
	}
	// A new freeze pushes the end date by its length — once.
	if after.has("frozenFrom") && after.has("frozenUntil") && (op == "create" || before.text("frozenFrom") != after.text("frozenFrom") || before.text("frozenUntil") != after.text("frozenUntil")) {
		from, ok1 := parseDay(after.text("frozenFrom"))
		until, ok2 := parseDay(after.text("frozenUntil"))
		if !ok1 || !ok2 || until.Before(from) {
			return nil, refuse("frozenUntil", "The freeze must end after it starts.")
		}
		days := int(until.Sub(from).Hours()/24) + 1
		prev := 0
		if pf, ok := parseDay(before.text("frozenFrom")); ok {
			if pu, ok := parseDay(before.text("frozenUntil")); ok {
				prev = int(pu.Sub(pf).Hours()/24) + 1
			}
		}
		end, _ := parseDay(after.text("endDate"))
		set["endDate"] = end.AddDate(0, 0, days-prev).Format("2006-01-02")
		set["freezeDays"] = after.num("freezeDays") - float64(prev) + float64(days)
		after.Values["endDate"] = set["endDate"]
	}
	if limited {
		if left := total - used; !after.has("sessionsLeft") || after.num("sessionsLeft") != left {
			set["sessionsLeft"] = left
		}
	}
	want := status
	switch {
	case status == "cancelled":
	case limited && used >= total:
		want = "used_up"
	case asString(after.Values["endDate"]) < today():
		want = "expired"
	case after.has("frozenFrom") && after.has("frozenUntil") && after.text("frozenFrom") <= today() && today() <= after.text("frozenUntil"):
		want = "frozen"
	default:
		want = "active"
	}
	if want != status {
		set["status"] = want
	}
	// The renewal follow-up sits on the Upcoming page at the end date.
	end := asString(after.Values["endDate"])
	if specFor("due_items") != nil && want != "cancelled" {
		if !after.has("renewalDueId") {
			v := map[string]any{"name": "Renewal — " + after.Title, "status": "open", "ownerId": after.text("ownerId"), "kind": "renewal",
				"contactId": after.text("contactId"), "dueDate": end, "cycle": 1.0}
			clean(v)
			d, err := h.createRecord(ctx, tx, ws, specFor("due_items"), systemActor(sourceCare), v)
			if err != nil {
				return nil, err
			}
			set["renewalDueId"] = d.ID
		} else if op == "update" && before.text("endDate") != end {
			if _, err := tx.Exec(ctx, `UPDATE crm.object_records SET custom = jsonb_set(custom, '{dueDate}', to_jsonb($3::text)), updated_at = now()
				WHERE id::text = $2 AND workspace_id = $1 AND status IN ('open', 'contacted')`, ws, after.text("renewalDueId"), end); err != nil {
				return nil, err
			}
		}
	}
	if want == "cancelled" && after.has("renewalDueId") && was != "cancelled" {
		_, _ = h.updateValues(ctx, tx, ws, specFor("due_items"), after.id("renewalDueId"), systemActor(sourceCare), map[string]any{"status": "skipped", "skipReason": "not_needed"}, nil, nil)
	}
	if len(set) == 0 {
		return after, nil
	}
	return h.updateValues(ctx, tx, ws, specFor("memberships"), after.uuid(), systemActor(sourceOps), set, nil, nil)
}

// checklistGate refuses a stage change while a checklist for that stage has steps not
// done. Runs for every object; a business adds templates without any code.
func (h *Handler) checklistGate(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, op string, before, after *Row) error {
	if specFor("checklist_templates") == nil || spec.StatusField == "" || commonOpsKeys[spec.Key] {
		return nil
	}
	to := after.text(spec.StatusField)
	if to == "" || (op == "update" && before.text(spec.StatusField) == to) {
		return nil
	}
	tpls, err := h.checklistsFor(ctx, tx, ws, spec.Key, to)
	if err != nil || len(tpls) == 0 {
		return err
	}
	missing := []string{}
	for _, t := range tpls {
		items, err := h.ensureChecklist(ctx, tx, ws, spec.Key, after.ID, t)
		if err != nil {
			return err
		}
		for _, it := range items {
			if it.required && !it.done {
				missing = append(missing, it.name)
			}
		}
	}
	if len(missing) > 0 {
		if len(missing) > 5 {
			missing = append(missing[:5], fmt.Sprintf("and %d more", len(missing)-5))
		}
		return refuse(spec.StatusField, "Finish the checklist first: "+strings.Join(missing, "; ")+".")
	}
	return nil
}

type checklistTpl struct {
	id, name    string
	stage       string
	steps       []string
	allRequired bool
}

func (h *Handler) checklistsFor(ctx context.Context, q querier, ws uuid.UUID, object, stage string) ([]checklistTpl, error) {
	sql := `SELECT id::text, name, COALESCE(custom->>'stage', ''), COALESCE(custom->>'items', ''), COALESCE((custom->>'allRequired')::boolean, true)
		FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'checklist_templates' AND deleted_at IS NULL
		AND COALESCE(status, 'active') = 'active' AND custom->>'objectKey' = $2`
	args := []any{ws, object}
	if stage != "" {
		sql += ` AND lower(custom->>'stage') = lower($3)`
		args = append(args, stage)
	}
	rows, err := q.Query(ctx, sql+` ORDER BY created_at`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []checklistTpl{}
	for rows.Next() {
		var t checklistTpl
		var items string
		if err := rows.Scan(&t.id, &t.name, &t.stage, &items, &t.allRequired); err != nil {
			return nil, err
		}
		for _, l := range strings.Split(items, "\n") {
			if l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "-•*0123456789.) ")); l != "" {
				t.steps = append(t.steps, l)
			}
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

type checklistStep struct {
	ID       string `json:"id"`
	name     string
	Name     string `json:"name"`
	Done     bool   `json:"done"`
	done     bool
	Required bool `json:"required"`
	required bool
	DoneBy   string     `json:"doneBy,omitempty"`
	DoneAt   *time.Time `json:"doneAt,omitempty"`
	Version  int        `json:"version"`
}

// ensureChecklist makes sure every step of a template exists on the record, and returns them.
func (h *Handler) ensureChecklist(ctx context.Context, tx pgx.Tx, ws uuid.UUID, object, recordID string, t checklistTpl) ([]checklistStep, error) {
	load := func() ([]checklistStep, error) {
		rows, err := tx.Query(ctx, `SELECT r.id::text, r.name, COALESCE((r.custom->>'done')::boolean, false), COALESCE((r.custom->>'required')::boolean, true),
			COALESCE(i.display_name, ''), NULLIF(r.custom->>'doneAt', '')::timestamptz, r.version
			FROM crm.object_records r LEFT JOIN crm.identities i ON i.id::text = r.custom->>'doneBy'
			WHERE r.workspace_id = $1 AND r.object_key = 'checklist_items' AND r.deleted_at IS NULL AND r.custom->>'templateId' = $2 AND r.custom->>'recordId' = $3
			ORDER BY (r.custom->>'position')::numeric NULLS LAST, r.created_at`, ws, t.id, recordID)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []checklistStep
		for rows.Next() {
			var s checklistStep
			if err := rows.Scan(&s.ID, &s.Name, &s.Done, &s.Required, &s.DoneBy, &s.DoneAt, &s.Version); err != nil {
				return nil, err
			}
			s.name, s.done, s.required = s.Name, s.Done, s.Required
			out = append(out, s)
		}
		return out, rows.Err()
	}
	have, err := load()
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for _, s := range have {
		known[strings.ToLower(s.Name)] = true
	}
	added := false
	for i, step := range t.steps {
		if known[strings.ToLower(step)] {
			continue
		}
		v := map[string]any{"name": step, "templateId": t.id, "objectKey": object, "recordId": recordID, "done": false, "required": t.allRequired || i == 0, "position": float64(i + 1)}
		if !t.allRequired {
			v["required"] = !strings.HasSuffix(strings.ToLower(step), "(optional)")
		}
		if _, err := h.createRecord(ctx, tx, ws, specFor("checklist_items"), systemActor(sourceOps), v); err != nil {
			return nil, err
		}
		added = true
	}
	if added {
		return load()
	}
	return have, nil
}

// GET /checklists/{object}/{id} — the checklists of one record, made if missing.
func (h *Handler) handleRecordChecklists(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	object, id := chi.URLParam(r, "object"), chi.URLParam(r, "id")
	spec := specFor(object)
	if spec == nil || !sc.Can(object, "read") || specFor("checklist_templates") == nil {
		shared.WriteError(w, r, shared.NotFound("object_not_found"))
		return
	}
	rid, err := uuid.Parse(id)
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	type block struct {
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Stage string          `json:"stage"`
		Steps []checklistStep `json:"steps"`
	}
	out := []block{}
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		if _, _, err := h.getRow(r.Context(), tx, sc.WS, spec, rid, ownerFilter(r, spec)); err != nil {
			return err
		}
		tpls, err := h.checklistsFor(r.Context(), tx, sc.WS, object, "")
		if err != nil {
			return err
		}
		for _, t := range tpls {
			steps, err := h.ensureChecklist(r.Context(), tx, sc.WS, object, id, t)
			if err != nil {
				return err
			}
			out = append(out, block{ID: t.id, Name: t.name, Stage: t.stage, Steps: steps})
		}
		return nil
	})
	respond(w, r, http.StatusOK, map[string]any{"data": out}, err)
}

// creditCheck stops an order or invoice that would take a customer past their credit
// limit, unless someone writes down why it is allowed.
func (h *Handler) creditCheck(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, op string, before, after *Row) error {
	status := after.text("status")
	if status == "" || status == "draft" || status == "cancelled" || status == "void" || status == "paid" {
		return nil
	}
	if op == "update" && before.text("status") == status && before.num("total") >= after.num("total") {
		return nil
	}
	acc := after.id("accountId")
	if acc == uuid.Nil || after.has("creditExceptionReason") {
		return nil
	}
	var limit *float64
	if err := tx.QueryRow(ctx, `SELECT credit_limit FROM crm.accounts WHERE id = $1 AND workspace_id = $2`, acc, ws).Scan(&limit); err != nil || limit == nil || *limit <= 0 {
		return nil
	}
	var open float64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(COALESCE(NULLIF(custom->>'balanceDue', '')::numeric, (custom->>'total')::numeric)), 0) FROM crm.object_records
		WHERE workspace_id = $1 AND object_key = 'invoices' AND deleted_at IS NULL AND custom->>'accountId' = $2 AND status NOT IN ('draft', 'paid', 'void') AND id <> $3`,
		ws, acc.String(), after.uuid()).Scan(&open); err != nil {
		return err
	}
	if spec.Key == "sales_orders" {
		// Orders not yet invoiced count too.
		var orders float64
		_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(NULLIF(custom->>'total', '')::numeric), 0) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'sales_orders'
			AND deleted_at IS NULL AND custom->>'accountId' = $2 AND status IN ('confirmed') AND id <> $3`, ws, acc.String(), after.uuid()).Scan(&orders)
		open += orders
	}
	this := after.num("total")
	if spec.Key == "invoices" && after.has("balanceDue") {
		this = after.num("balanceDue")
	}
	if open+this > *limit+0.005 {
		return refuse("creditExceptionReason", fmt.Sprintf("This takes the customer to %.2f against a credit limit of %.2f (already owed %.2f). Collect payment first, or write why it is allowed.", open+this, *limit, open))
	}
	return nil
}

// sweepOps: expired holds give their item back; instalments past their date turn overdue;
// stock past its date turns expired.
func (h *Handler) sweepOps(ctx context.Context) {
	if specFor("item_reservations") != nil {
		_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.object_records SET status = 'cancelled', custom = custom || '{"notes":"Hold expired"}'::jsonb, updated_at = now()
			WHERE object_key = 'item_reservations' AND deleted_at IS NULL AND status = 'hold' AND NULLIF(custom->>'holdUntil', '')::timestamptz < now()`)
	}
	if specFor("instalments") != nil {
		_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.object_records SET status = 'overdue', updated_at = now() WHERE object_key = 'instalments' AND deleted_at IS NULL
			AND status = 'due' AND NULLIF(custom->>'dueDate', '')::date < now()::date`)
	}
	if specFor("stock_lots") != nil {
		_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.object_records SET status = 'expired', updated_at = now() WHERE object_key = 'stock_lots' AND deleted_at IS NULL
			AND status IN ('available', 'low') AND NULLIF(custom->>'expiryDate', '')::date < now()::date`)
	}
}
