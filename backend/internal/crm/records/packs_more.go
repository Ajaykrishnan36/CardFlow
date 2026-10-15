package records

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// More of the industry packs and the privacy desk (D-149):
//
//	insurance    commission_rules, insurance_claims
//	salon        salon_products, product_usage, makeup_kits, kit_bookings, client_packages
//	real_estate  properties, property_bookings, property_visits
//	privacy      privacy_requests (access, correction, erasure, grievance)
//
// Salon and real estate had no use-case catalogue, only a page each in the master PRD,
// so the rules here are the ones that page names as must-not-break: stock never goes
// negative and expired stock isn't used; a kit is in one place at a time; a package
// session is used once; a unit has one live hold or booking.

const sourcePack = "pack"

func packMoreObjects() []ObjectDefinition {
	return []ObjectDefinition{
		// ---- insurance ----
		{Key: "commission_rules", Module: "insurance", Singular: "Commission rule", Plural: "Commission rules", Icon: "sliders", Prefix: "CRL",
			Description: "The commission rate an insurer pays, by plan or class and by policy year. New policies take their rate from here.",
			ObjectBody: ObjectBody{NameLabel: "Rule", StatusLabel: "Status", Statuses: []StatusOption{st("active", "Active", "success"), st("inactive", "Inactive", "neutral")},
				Fields: []ObjectField{
					ofReq(ofLookup("insurerId", "Insurer", "accounts")), ofLookup("itemId", "Plan (leave empty for every plan)", "catalog_items"),
					ofSelect("policyClass", "Class (leave empty for every class)", opts("life", "Life", "health", "Health", "motor", "Motor", "general", "General")),
					of("yearFrom", "From policy year", "number"), of("yearTo", "To policy year", "number"),
					ofReq(of("rate", "Commission rate", "percent"))}}},
		{Key: "insurance_claims", Module: "insurance", Singular: "Claim", Plural: "Claims", Icon: "life-buoy", Prefix: "CLM",
			Description: "A claim on a policy from intimation to settlement: what is claimed, the insurer's reference, the papers still needed and the outcome.",
			ObjectBody: ObjectBody{NameLabel: "Claim", StatusLabel: "Status", Statuses: []StatusOption{
				st("intimated", "Intimated", "primary"), st("documents_pending", "Documents pending", "warning"), st("submitted", "Submitted to insurer", "primary"),
				st("under_review", "Under review", "primary"), st("settled", "Settled", "success"), st("rejected", "Rejected", "danger"), st("withdrawn", "Withdrawn", "neutral")},
				Fields: []ObjectField{
					ofReq(ofLookup("policyId", "Policy", "insurance_policies")), ofLookup("contactId", "Policyholder", "contacts"),
					ofSelect("claimType", "Type", opts("health_cashless", "Health — cashless", "health_reimbursement", "Health — reimbursement", "motor_own_damage", "Motor — own damage",
						"motor_third_party", "Motor — third party", "death", "Death", "maturity", "Maturity / survival benefit", "other", "Other")),
					of("intimatedOn", "Intimated on", "date"), of("incidentDate", "Date of the incident", "date"),
					of("insurerClaimNumber", "Insurer's claim number", "text"), of("amountClaimed", "Amount claimed", "currency"),
					of("documentsPending", "Documents still needed", "text"), of("amountSettled", "Amount settled", "currency"), of("settledOn", "Settled on", "date"),
					of("rejectionReason", "Why it was rejected", "text"), of("notes", "Notes", "textarea")}}},

		// ---- salon ----
		{Key: "salon_products", Module: "salon", Singular: "Product", Plural: "Product stock", Icon: "package", Prefix: "SPR",
			Description: "What is on the shelf: brand, shade, batch, how much is left and when it expires.",
			ObjectBody: ObjectBody{NameLabel: "Product", StatusLabel: "Status", Statuses: []StatusOption{
				st("available", "Available", "success"), st("low", "Running low", "warning"), st("out_of_stock", "Out of stock", "danger"),
				st("expired", "Expired", "danger"), st("quarantined", "Do not use", "danger")},
				Fields: []ObjectField{
					of("brand", "Brand", "text"), of("shade", "Shade / variant", "text"), of("batchNumber", "Batch number", "text"),
					ofSelect("unit", "Measured in", opts("ml", "Millilitres", "g", "Grams", "piece", "Pieces")),
					of("quantity", "Quantity left", "number"), of("reorderLevel", "Reorder when below", "number"),
					of("expiryDate", "Expires on", "date"), of("openedOn", "Opened on", "date"), of("costPerUnit", "Cost per unit", "currency"),
					of("supplier", "Supplier", "text"), of("notes", "Notes", "textarea")}}},
		{Key: "product_usage", Module: "salon", Singular: "Product use", Plural: "Product usage", Icon: "clipboard", Prefix: "USE",
			Description: "Each time product leaves the shelf: how much, for which appointment, or why it was lost.",
			ObjectBody: ObjectBody{NameLabel: "Use", Statuses: []StatusOption{},
				Fields: []ObjectField{
					ofReq(ofLookup("productId", "Product", "salon_products")), ofReq(of("quantity", "Quantity used", "number")),
					ofSelect("reason", "Used for", opts("service", "A service", "sample", "Sample / trial", "damage", "Damaged", "loss", "Lost", "expired", "Thrown away (expired)", "training", "Training")),
					ofLookup("appointmentId", "Appointment", "appointments"), ofLookup("kitId", "Kit", "makeup_kits"),
					of("usedOn", "Date", "date"), of("cost", "Cost", "currency"), of("notes", "Notes", "text")}}},
		{Key: "makeup_kits", Module: "salon", Singular: "Kit", Plural: "Kits", Icon: "briefcase", Prefix: "KIT",
			Description: "A named kit: what is in it, who looks after it and whether it is ready, out or waiting to be cleaned.",
			ObjectBody: ObjectBody{NameLabel: "Kit name", StatusLabel: "Status", Statuses: []StatusOption{
				st("ready", "Ready", "success"), st("checked_out", "Out on a job", "warning"), st("needs_cleaning", "Needs cleaning", "warning"),
				st("incomplete", "Items missing", "danger"), st("retired", "Retired", "neutral")},
				Fields: []ObjectField{
					ofLookup("custodian", "Looked after by", "users"), of("contents", "What is in it", "textarea"),
					of("suitableFor", "Suitable for", "text"), of("lastCleanedOn", "Last cleaned on", "date"), of("notes", "Notes", "textarea")}}},
		{Key: "kit_bookings", Module: "salon", Singular: "Kit booking", Plural: "Kit bookings", Icon: "calendar", Prefix: "KBK",
			Description: "A kit reserved for a job: from when to when, who takes it, and how it came back.",
			ObjectBody: ObjectBody{NameLabel: "Booking", StatusLabel: "Status", Statuses: []StatusOption{
				st("reserved", "Reserved", "primary"), st("checked_out", "Taken out", "warning"), st("returned", "Returned", "success"), st("cancelled", "Cancelled", "neutral")},
				Fields: []ObjectField{
					ofReq(ofLookup("kitId", "Kit", "makeup_kits")), ofLookup("appointmentId", "Appointment", "appointments"), ofLookup("contactId", "Client", "contacts"),
					of("eventName", "Event", "text"), ofReq(of("startsAt", "From", "datetime")), ofReq(of("endsAt", "Until", "datetime")),
					ofLookup("artist", "Taken by", "users"),
					ofSelect("returnCondition", "Came back", opts("complete", "Complete", "damaged", "With damage", "missing", "With items missing")),
					of("returnNotes", "What was damaged or missing", "text"), of("cleaned", "Cleaned and sanitised", "boolean")}}},
		{Key: "client_packages", Module: "salon", Singular: "Package", Plural: "Client packages", Icon: "ticket", Prefix: "PKG",
			Description: "A package or membership a client bought: sessions bought, used and left, and until when it is valid.",
			ObjectBody: ObjectBody{NameLabel: "Package", StatusLabel: "Status", Statuses: []StatusOption{
				st("active", "Active", "success"), st("used_up", "Used up", "neutral"), st("expired", "Expired", "danger"), st("cancelled", "Cancelled", "neutral")},
				Fields: []ObjectField{
					ofReq(ofLookup("contactId", "Client", "contacts")), ofLookup("itemId", "Service", "catalog_items"),
					ofReq(of("sessionsTotal", "Sessions bought", "number")), of("sessionsUsed", "Sessions used", "number"), of("sessionsLeft", "Sessions left", "number"),
					of("price", "Price paid", "currency"), of("purchasedOn", "Bought on", "date"), of("validUntil", "Valid until", "date"),
					of("lastUsedOn", "Last used on", "date"), of("notes", "Notes", "textarea")}}},

		// ---- real estate ----
		{Key: "property_units", Module: "real_estate", Singular: "Unit", Plural: "Units & listings", Icon: "home", Prefix: "PRP",
			Description: "Every unit or listing: project, tower, floor, size, price and whether it is free, on hold, booked or sold.",
			ObjectBody: ObjectBody{NameLabel: "Unit / listing", StatusLabel: "Status", Statuses: []StatusOption{
				st("available", "Available", "success"), st("on_hold", "On hold", "warning"), st("booked", "Booked", "primary"),
				st("sold", "Sold", "neutral"), st("leased", "Leased", "neutral"), st("withdrawn", "Withdrawn", "neutral")},
				Fields: []ObjectField{
					of("project", "Project", "text"), of("tower", "Tower / block", "text"), of("floor", "Floor", "text"),
					ofSelect("listingType", "For", opts("sale", "Sale", "rent", "Rent")),
					ofSelect("configuration", "Configuration", opts("plot", "Plot", "studio", "Studio", "1bhk", "1 BHK", "2bhk", "2 BHK", "3bhk", "3 BHK", "4bhk", "4 BHK+", "villa", "Villa", "shop", "Shop", "office", "Office")),
					of("area", "Area (sq ft)", "number"), ofSelect("areaType", "Area is", opts("carpet", "Carpet", "built_up", "Built-up", "super_built_up", "Super built-up", "plot", "Plot")),
					of("price", "Price / rent", "currency"), of("facing", "Facing", "text"), of("locality", "Locality", "text"),
					ofLookup("ownerAccountId", "Seller / landlord", "accounts"), ofLookup("bookingId", "Held or booked by", "property_bookings"),
					of("description", "Description", "textarea")}}},
		{Key: "property_bookings", Module: "real_estate", Singular: "Hold / booking", Plural: "Holds & bookings", Icon: "clipboard", Prefix: "PBK",
			Description: "A buyer's or tenant's hold or booking on one unit: until when it is held, the agreed price, the token and where the deal stands.",
			ObjectBody: ObjectBody{NameLabel: "Booking", StatusLabel: "Status", Statuses: []StatusOption{
				st("hold", "On hold", "warning"), st("booked", "Booked", "primary"), st("agreement", "Agreement signed", "primary"),
				st("completed", "Completed", "success"), st("cancelled", "Cancelled", "danger"), st("expired", "Hold expired", "neutral")},
				Fields: []ObjectField{
					ofReq(ofLookup("propertyId", "Unit", "property_units")), ofReq(ofLookup("contactId", "Buyer / tenant", "contacts")),
					of("holdUntil", "Held until", "date"), of("agreedPrice", "Agreed price / rent", "currency"), of("tokenAmount", "Token / deposit received", "currency"),
					ofLookup("brokerId", "Channel partner / co-broker", "accounts"), of("commissionPercent", "Commission", "percent"), of("commissionAmount", "Commission amount", "currency"),
					ofLookup("opportunityId", "Opportunity", "opportunities"),
					ofSelect("cancellationReason", "Why it was cancelled", opts("changed_mind", "Changed their mind", "loan_rejected", "Loan not approved", "price", "Price",
						"bought_elsewhere", "Chose another property", "documents", "Paperwork problem", "other", "Other")),
					of("completedOn", "Registered / handed over on", "date"), of("notes", "Notes", "textarea")}}},
		{Key: "property_visits", Module: "real_estate", Singular: "Site visit", Plural: "Site visits", Icon: "map-pin", Prefix: "SIV",
			Description: "A visit to a unit: who came, when, with which agent, and what they thought.",
			ObjectBody: ObjectBody{NameLabel: "Visit", StatusLabel: "Status", Statuses: []StatusOption{
				st("scheduled", "Scheduled", "primary"), st("completed", "Completed", "success"), st("no_show", "No show", "danger"), st("cancelled", "Cancelled", "neutral")},
				Fields: []ObjectField{
					ofReq(ofLookup("propertyId", "Unit", "property_units")), ofLookup("contactId", "Visitor", "contacts"), ofLookup("leadId", "Lead", "leads"),
					ofReq(of("scheduledAt", "When", "datetime")), ofLookup("agent", "Shown by", "users"),
					ofSelect("interest", "Interest after the visit", opts("hot", "Hot", "warm", "Warm", "cold", "Cold")),
					of("feedback", "What they said", "textarea"), ofLookup("opportunityId", "Opportunity", "opportunities")}}},

		// ---- privacy ----
		{Key: "privacy_requests", Module: "contacts", Singular: "Privacy request", Plural: "Privacy requests", Icon: "flag", Prefix: "PVR",
			Description: "A customer asking to see, correct or erase their data, or raising a grievance: when it came, when it is due and what was done.",
			ObjectBody: ObjectBody{NameLabel: "Request", StatusLabel: "Status", Statuses: []StatusOption{
				st("received", "Received", "primary"), st("verified", "Identity verified", "primary"), st("completed", "Completed", "success"), st("rejected", "Rejected", "danger")},
				Fields: []ObjectField{
					ofLookup("contactId", "Contact", "contacts"), ofLookup("leadId", "Lead", "leads"),
					ofReq(ofSelect("kind", "They asked to", opts("erasure", "Erase their data", "access", "See their data", "correction", "Correct their data",
						"withdraw", "Stop being contacted", "grievance", "Raise a grievance"))),
					ofSelect("channel", "Came by", opts("in_person", "In person", "phone", "Phone", "email", "Email", "message", "Message", "letter", "Letter")),
					of("receivedOn", "Received on", "date"), of("dueBy", "Answer by", "date"),
					of("legalHold", "Must be kept (legal hold)", "boolean"), of("holdReason", "Why it must be kept", "text"),
					of("details", "What they asked", "textarea"), of("outcome", "What was done", "textarea"), of("completedOn", "Completed on", "date")}}},
	}
}

var packMoreKeys = func() map[string]bool {
	out := map[string]bool{}
	for _, d := range packMoreObjects() {
		out[d.Key] = true
	}
	return out
}()

func lockRecord(ctx context.Context, tx pgx.Tx, ws uuid.UUID, object string, id uuid.UUID) (status, name string, err error) {
	err = tx.QueryRow(ctx, `SELECT COALESCE(status, ''), name FROM crm.object_records WHERE id = $1 AND workspace_id = $2 AND object_key = $3
		AND deleted_at IS NULL FOR UPDATE`, id, ws, object).Scan(&status, &name)
	return
}

func (h *Handler) packSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, a actorInfo, op string, before, after *Row) (*Row, error) {
	if a.Source == sourcePack {
		return after, nil
	}
	sys := systemActor(sourcePack)
	set := map[string]any{}
	apply := func() (*Row, error) {
		clean2 := map[string]any{}
		for k, v := range set {
			clean2[k] = v
		}
		if len(clean2) == 0 {
			return after, nil
		}
		return h.updateValues(ctx, tx, ws, spec, after.uuid(), sys, clean2, nil, nil)
	}
	status, was := after.text("status"), before.text("status")
	changed := op == "create" || status != was

	switch spec.Key {
	case "commission_rules":
		if after.num("rate") < 0 || after.num("rate") > 100 {
			return nil, refuse("rate", "A rate is between 0 and 100 percent.")
		}
		if after.has("yearFrom") && after.has("yearTo") && after.num("yearTo") < after.num("yearFrom") {
			return nil, refuse("yearTo", "The last year can't be before the first.")
		}

	case "insurance_claims":
		if status == "settled" && !after.has("amountSettled") {
			return nil, refuse("amountSettled", "Enter how much the insurer settled.")
		}
		if status == "rejected" && !after.has("rejectionReason") {
			return nil, refuse("rejectionReason", "Say why the insurer rejected the claim.")
		}
		if op == "update" && (was == "settled" || was == "rejected") && status != was {
			return nil, refuse("status", "This claim is closed. An appeal or a new claim is a new record.")
		}
		if !after.has("intimatedOn") {
			set["intimatedOn"] = today()
		}
		if status == "settled" && !after.has("settledOn") {
			set["settledOn"] = today()
		}
		if !after.has("contactId") {
			if p, _, err := h.getRow(ctx, tx, ws, specFor("insurance_policies"), after.id("policyId"), nil); err == nil && p.has("contactId") {
				set["contactId"] = p.text("contactId")
			}
		}

	case "salon_products":
		if after.num("quantity") < 0 {
			return nil, refuse("quantity", "Stock can't be below zero.")
		}
		want := status
		switch {
		case status == "quarantined":
		case after.has("expiryDate") && after.text("expiryDate") < today():
			want = "expired"
		case after.has("quantity") && after.num("quantity") <= 0:
			want = "out_of_stock"
		case after.has("reorderLevel") && after.num("quantity") <= after.num("reorderLevel"):
			want = "low"
		default:
			want = "available"
		}
		if want != status {
			set["status"] = want
		}

	case "product_usage":
		if op != "create" {
			if before.num("quantity") != after.num("quantity") || before.text("productId") != after.text("productId") {
				return nil, refuse("quantity", "A use that is recorded can't be changed. Record a correction as a new entry.")
			}
			break
		}
		qty := after.num("quantity")
		if qty <= 0 {
			return nil, refuse("quantity", "Enter how much was used.")
		}
		pStatus, pName, err := lockRecord(ctx, tx, ws, "salon_products", after.id("productId"))
		if err != nil {
			return nil, refuse("productId", "That product isn't in your stock.")
		}
		p, _, err := h.getRow(ctx, tx, ws, specFor("salon_products"), after.id("productId"), nil)
		if err != nil {
			return nil, err
		}
		disposal := after.text("reason") == "expired" || after.text("reason") == "damage" || after.text("reason") == "loss"
		if !disposal && (pStatus == "expired" || pStatus == "quarantined" || (p.has("expiryDate") && p.text("expiryDate") < today())) {
			return nil, refuse("productId", pName+" is expired or marked Do not use. It can't be used on a client.")
		}
		if qty > p.num("quantity")+0.0001 {
			return nil, refuse("quantity", fmt.Sprintf("Only %g %s of %s is left.", p.num("quantity"), p.text("unit"), pName))
		}
		// The product's own rules then set Low / Out of stock.
		if _, err := h.updateValues(ctx, tx, ws, specFor("salon_products"), p.uuid(), actorInfo{Kind: "system", Source: "usage"},
			map[string]any{"quantity": round2(p.num("quantity") - qty)}, nil, nil); err != nil {
			return nil, err
		}
		if !after.has("usedOn") {
			set["usedOn"] = today()
		}
		if p.has("costPerUnit") && !after.has("cost") {
			set["cost"] = round2(qty * p.num("costPerUnit"))
		}

	case "kit_bookings":
		kit := after.id("kitId")
		kStatus, kName, err := lockRecord(ctx, tx, ws, "makeup_kits", kit)
		if err != nil {
			return nil, refuse("kitId", "That kit doesn't exist.")
		}
		if after.text("endsAt") <= after.text("startsAt") {
			return nil, refuse("endsAt", "The booking must end after it starts.")
		}
		if op == "update" && (was == "returned" || was == "cancelled") && status != was {
			return nil, refuse("status", "This booking is finished. Make a new one.")
		}
		live := status == "reserved" || status == "checked_out"
		if live {
			if kStatus == "incomplete" || kStatus == "retired" {
				return nil, refuse("kitId", kName+" has items missing or is retired. It can't be booked until it is complete again.")
			}
			var clash string
			err := tx.QueryRow(ctx, `SELECT code FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'kit_bookings' AND deleted_at IS NULL
				AND id <> $2 AND custom->>'kitId' = $3 AND status IN ('reserved', 'checked_out')
				AND (custom->>'startsAt')::timestamptz < $5::timestamptz AND (custom->>'endsAt')::timestamptz > $4::timestamptz LIMIT 1`,
				ws, after.uuid(), kit.String(), after.text("startsAt"), after.text("endsAt")).Scan(&clash)
			if err == nil {
				return nil, refuse("kitId", fmt.Sprintf("%s is already booked for that time (%s). Pick another kit or another time.", kName, clash))
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return nil, err
			}
		}
		kitSet := map[string]any{}
		switch {
		case status == "checked_out" && changed:
			if kStatus != "ready" {
				return nil, refuse("status", fmt.Sprintf("%s isn't ready to go out (it is %s).", kName, kStatus))
			}
			kitSet["status"] = "checked_out"
		case status == "returned" && changed:
			if !after.has("returnCondition") {
				return nil, refuse("returnCondition", "Say how the kit came back.")
			}
			if after.text("returnCondition") != "complete" && !after.has("returnNotes") {
				return nil, refuse("returnNotes", "Say what was damaged or missing.")
			}
			switch {
			case after.text("returnCondition") == "missing":
				kitSet["status"] = "incomplete"
			case after.flag("cleaned"):
				kitSet["status"], kitSet["lastCleanedOn"] = "ready", today()
			default:
				kitSet["status"] = "needs_cleaning"
			}
		case status == "cancelled" && was == "checked_out":
			kitSet["status"] = "needs_cleaning"
		}
		if len(kitSet) > 0 && kStatus != "retired" {
			if _, err := h.updateValues(ctx, tx, ws, specFor("makeup_kits"), kit, sys, kitSet, nil, nil); err != nil {
				return nil, err
			}
		}

	case "client_packages":
		total, used := after.num("sessionsTotal"), after.num("sessionsUsed")
		if total <= 0 {
			return nil, refuse("sessionsTotal", "Enter how many sessions were bought.")
		}
		if used < 0 || used > total {
			return nil, refuse("sessionsUsed", fmt.Sprintf("This package has %g sessions. It can't have %g used.", total, used))
		}
		redeeming := op == "update" && used > before.num("sessionsUsed")
		if redeeming && (was == "cancelled" || was == "expired" || (after.has("validUntil") && after.text("validUntil") < today())) {
			return nil, refuse("sessionsUsed", "This package is no longer valid, so a session can't be used from it.")
		}
		if redeeming {
			set["lastUsedOn"] = today()
		}
		if left := total - used; !after.has("sessionsLeft") || after.num("sessionsLeft") != left {
			set["sessionsLeft"] = left
		}
		if !after.has("purchasedOn") {
			set["purchasedOn"] = today()
		}
		want := status
		switch {
		case status == "cancelled":
		case used >= total:
			want = "used_up"
		case after.has("validUntil") && after.text("validUntil") < today():
			want = "expired"
		default:
			want = "active"
		}
		if want != status {
			set["status"] = want
		}

	case "property_units":
		if op == "update" && before.text("bookingId") != after.text("bookingId") {
			return nil, refuse("bookingId", "A unit is held or booked from its hold or booking, not from here.")
		}
		held := after.has("bookingId")
		if changed && op == "update" && (held || status == "on_hold" || status == "booked") && a.Source != "booking" {
			return nil, refuse("status", "This status follows the hold or booking on the unit: change that instead.")
		}
		if op == "create" && (status == "on_hold" || status == "booked") {
			return nil, refuse("status", "Add the unit as Available, then put a hold or booking on it.")
		}

	case "property_bookings":
		unit := after.id("propertyId")
		if op == "update" && before.id("propertyId") != unit {
			return nil, refuse("propertyId", "A hold or booking is for one unit. Cancel this one and make another.")
		}
		closed := func(s string) bool { return s == "cancelled" || s == "expired" || s == "completed" }
		if op == "update" && closed(was) && status != was {
			return nil, refuse("status", "This hold or booking is finished. Make a new one.")
		}
		uStatus, uName, err := lockRecord(ctx, tx, ws, "property_units", unit)
		if err != nil {
			return nil, refuse("propertyId", "That unit isn't in your listings.")
		}
		u, _, err := h.getRow(ctx, tx, ws, specFor("property_units"), unit, nil)
		if err != nil {
			return nil, err
		}
		mine := u.text("bookingId") == after.ID
		if !closed(status) && !mine && (u.has("bookingId") || uStatus != "available") {
			why := "it is " + uStatus
			if u.has("bookingId") {
				why = "it is held by " + h.codeOf(ctx, tx, ws, u.text("bookingId"))
			}
			return nil, refuse("propertyId", fmt.Sprintf("%s can't be held or booked: %s.", uName, why))
		}
		if status == "cancelled" && !after.has("cancellationReason") {
			return nil, refuse("cancellationReason", "Say why it was cancelled.")
		}
		if status == "hold" && !after.has("holdUntil") {
			set["holdUntil"] = time.Now().AddDate(0, 0, 3).Format("2006-01-02")
		}
		if (status == "booked" || status == "agreement" || status == "completed") && after.num("agreedPrice") <= 0 {
			return nil, refuse("agreedPrice", "Enter the agreed price or rent.")
		}
		if after.has("commissionPercent") && after.has("agreedPrice") {
			if c := round2(after.num("agreedPrice") * after.num("commissionPercent") / 100); c != after.num("commissionAmount") {
				set["commissionAmount"] = c
			}
		}
		if status == "completed" && !after.has("completedOn") {
			set["completedOn"] = today()
		}
		unitSet := map[string]any{}
		switch status {
		case "hold":
			unitSet["status"], unitSet["bookingId"] = "on_hold", after.ID
		case "booked", "agreement":
			unitSet["status"], unitSet["bookingId"] = "booked", after.ID
		case "completed":
			unitSet["status"], unitSet["bookingId"] = "sold", after.ID
			if u.text("listingType") == "rent" {
				unitSet["status"] = "leased"
			}
		case "cancelled", "expired":
			if mine {
				unitSet["status"], unitSet["bookingId"] = "available", nil
			}
		}
		if len(unitSet) > 0 && (fmt.Sprint(unitSet["status"]) != uStatus || fmt.Sprint(unitSet["bookingId"]) != u.text("bookingId")) {
			if _, err := h.updateValues(ctx, tx, ws, specFor("property_units"), unit, actorInfo{Kind: "system", Source: sourcePack}, unitSet, nil, nil); err != nil {
				return nil, err
			}
		}

	case "property_visits":
		if !after.has("contactId") && !after.has("leadId") {
			return nil, refuse("contactId", "Pick the lead or contact who is visiting.")
		}
		if status == "completed" && changed && specFor("tasks") != nil {
			t := map[string]any{"name": "Follow up after the site visit — " + after.Title, "status": "not_started", "priority": "high", "ownerId": after.text("ownerId"),
				"dueDate": time.Now().AddDate(0, 0, 1).Format("2006-01-02"), "contactId": after.text("contactId"), "leadId": after.text("leadId"), "opportunityId": after.text("opportunityId")}
			clean(t)
			if _, err := h.createRecord(ctx, tx, ws, specFor("tasks"), sys, t); err != nil {
				return nil, err
			}
		}

	case "privacy_requests":
		if !after.has("contactId") && !after.has("leadId") {
			return nil, refuse("contactId", "Pick the contact or lead who made the request.")
		}
		if op == "update" && (was == "completed" || was == "rejected") && status != was {
			return nil, refuse("status", "This request is closed. A new request is a new record.")
		}
		if after.flag("legalHold") && !after.has("holdReason") {
			return nil, refuse("holdReason", "Say why the data must be kept.")
		}
		if !after.has("receivedOn") {
			set["receivedOn"] = today()
		}
		if !after.has("dueBy") {
			set["dueBy"] = time.Now().AddDate(0, 0, 30).Format("2006-01-02")
		}
		if status == "rejected" && !after.has("outcome") {
			return nil, refuse("outcome", "Say why the request was rejected.")
		}
		if status == "completed" && changed {
			if op == "create" || was != "verified" {
				return nil, refuse("status", "Verify who is asking first (Identity verified), then complete the request.")
			}
			kind := after.text("kind")
			if kind == "erasure" && after.flag("legalHold") {
				return nil, refuse("legalHold", "This person's data is on legal hold and can't be erased. Reject the request with the reason, or lift the hold.")
			}
			if kind == "erasure" || kind == "withdraw" {
				for _, who := range []struct{ object, key string }{{"contacts", "contactId"}, {"leads", "leadId"}} {
					if id := after.id(who.key); id != uuid.Nil {
						if err := h.stopContact(ctx, tx, ws, who.object, who.key, id, kind == "erasure"); err != nil {
							return nil, err
						}
					}
				}
			}
			set["completedOn"] = today()
			if !after.has("outcome") {
				switch kind {
				case "erasure":
					set["outcome"] = "Personal details erased from the contact or lead; consents withdrawn. Invoices and payments are kept as the law requires."
				case "withdraw":
					set["outcome"] = "All consents withdrawn; no more campaign emails."
				default:
					return nil, refuse("outcome", "Say what was done.")
				}
			}
		}
	}
	return apply()
}

// stopContact withdraws every consent of a person and takes their email off campaigns;
// with erase it also blanks their personal details. Records that others point to
// (invoices, policies, bookings) stay, now pointing at a person with no details.
func (h *Handler) stopContact(ctx context.Context, tx pgx.Tx, ws uuid.UUID, object, key string, id uuid.UUID, erase bool) error {
	spec := specFor(object)
	row, _, err := h.getRow(ctx, tx, ws, spec, id, nil)
	if err != nil {
		return nil // already gone
	}
	if email := row.text("email"); email != "" {
		if _, err := tx.Exec(ctx, `INSERT INTO crm.unsubscribes (workspace_id, email) VALUES ($1, lower($2)) ON CONFLICT DO NOTHING`, ws, email); err != nil {
			return err
		}
	}
	if specFor("consents") != nil {
		rows, err := tx.Query(ctx, `SELECT id FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'consents' AND deleted_at IS NULL
			AND custom->>$2 = $3 AND status = 'given'`, ws, key, id.String())
		if err != nil {
			return err
		}
		var ids []uuid.UUID
		for rows.Next() {
			var c uuid.UUID
			if rows.Scan(&c) == nil {
				ids = append(ids, c)
			}
		}
		rows.Close()
		for _, c := range ids {
			if _, err := h.updateValues(ctx, tx, ws, specFor("consents"), c, actorInfo{Kind: "system", Source: "privacy"}, map[string]any{"status": "withdrawn"}, nil, nil); err != nil {
				return err
			}
		}
	}
	if !erase {
		return nil
	}
	fields, err := allFieldsRaw(ctx, tx, ws, spec)
	if err != nil {
		return err
	}
	blank := map[string]any{}
	for _, f := range fields {
		if f.system || f.ReadOnly || f.Key == "code" {
			continue
		}
		switch f.Type {
		case "text", "textarea", "richtext", "email", "phone", "url", "date", "address", "fullName", "emails", "phones", "links", "json":
			if !isEmpty(row.Values[f.Key]) {
				blank[f.Key] = nil
			}
		}
	}
	blank["lastName"] = "Erased on request"
	delete(blank, "firstName")
	if !isEmpty(row.Values["firstName"]) {
		blank["firstName"] = nil
	}
	if _, err := h.updateValues(ctx, tx, ws, spec, id, actorInfo{Kind: "system", Source: "privacy"}, blank, nil, nil); err != nil {
		return err
	}
	// Notes and the change history hold the old values in words: remove them too.
	if _, err = tx.Exec(ctx, `DELETE FROM crm.activities WHERE workspace_id = $1 AND object_key = $2 AND record_id = $3`, ws, object, id); err != nil {
		return err
	}
	// The audit trail keeps that the record changed, who changed it and when — not the old values.
	_, err = tx.Exec(ctx, `UPDATE crm.audit_events SET before = NULL, after = '{"erased": true}'::jsonb
		WHERE workspace_id = $1 AND entity_id = $2 AND (before IS NOT NULL OR after IS NOT NULL)`, ws, id)
	return err
}

// ruleRate finds the commission rate for a policy: the active rule of its insurer that
// fits its plan, class and policy year — a rule naming the plan beats one naming only
// the class, which beats one for everything.
func (h *Handler) ruleRate(ctx context.Context, q querier, ws uuid.UUID, p map[string]any, year int) (float64, bool) {
	if specFor("commission_rules") == nil || isEmpty(p["insurerId"]) {
		return 0, false
	}
	var rate float64
	err := q.QueryRow(ctx, `SELECT (custom->>'rate')::numeric FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'commission_rules'
		AND deleted_at IS NULL AND COALESCE(status, 'active') = 'active' AND custom->>'insurerId' = $2
		AND (COALESCE(custom->>'itemId', '') = '' OR custom->>'itemId' = $3)
		AND (COALESCE(custom->>'policyClass', '') = '' OR custom->>'policyClass' = $4)
		AND COALESCE(NULLIF(custom->>'yearFrom', '')::numeric, 1) <= $5 AND COALESCE(NULLIF(custom->>'yearTo', '')::numeric, 999) >= $5
		ORDER BY (COALESCE(custom->>'itemId', '') <> '') DESC, (COALESCE(custom->>'policyClass', '') <> '') DESC, created_at DESC LIMIT 1`,
		ws, asString(p["insurerId"]), asString(p["itemId"]), asString(p["policyClass"]), year).Scan(&rate)
	return rate, err == nil
}

// policyYearOf counts the terms before this one.
func (h *Handler) policyYearOf(ctx context.Context, q querier, ws uuid.UUID, previous any) int {
	year, id := 1, asString(previous)
	for id != "" && year < 60 {
		year++
		var prev string
		if err := q.QueryRow(ctx, `SELECT COALESCE(custom->>'previousPolicyId', '') FROM crm.object_records WHERE id::text = $1 AND workspace_id = $2`, id, ws).Scan(&prev); err != nil {
			break
		}
		id = prev
	}
	return year
}

// sweepPacks: holds that ran out release their unit; stock past its date is marked expired.
func (h *Handler) sweepPacks(ctx context.Context) {
	if specFor("property_bookings") != nil {
		rows, err := h.store.Pool.Query(ctx, `SELECT id, workspace_id FROM crm.object_records WHERE object_key = 'property_bookings' AND deleted_at IS NULL
			AND status = 'hold' AND NULLIF(custom->>'holdUntil', '')::date < now()::date LIMIT 500`)
		if err == nil {
			type ref struct{ id, ws uuid.UUID }
			var list []ref
			for rows.Next() {
				var r ref
				if rows.Scan(&r.id, &r.ws) == nil {
					list = append(list, r)
				}
			}
			rows.Close()
			for _, r := range list {
				_ = h.store.WithTx(ctx, func(tx pgx.Tx) error {
					_, err := h.updateValues(ctx, tx, r.ws, specFor("property_bookings"), r.id, actorInfo{Kind: "system", Source: "sweep"}, map[string]any{"status": "expired"}, nil, nil)
					return err
				})
			}
		}
	}
	if specFor("salon_products") != nil {
		_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.object_records SET status = 'expired', updated_at = now() WHERE object_key = 'salon_products' AND deleted_at IS NULL
			AND status IN ('available', 'low') AND NULLIF(custom->>'expiryDate', '')::date < now()::date`)
	}
}
