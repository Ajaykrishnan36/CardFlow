package records

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The second slice of the dealership pack (D-147): the on-road price as its parts, the
// customer's exchange vehicle, loan applications per lender, and stock moving between
// locations.

// onRoadParts are the booking fields that add up to the on-road price; the last two
// come off it.
var onRoadParts = []string{"exShowroom", "roadTax", "insurancePremium", "handlingCharges", "hsrpCharges", "tcsAmount", "accessoriesAmount", "extendedWarranty"}

func dealershipMoreObjects() []ObjectDefinition {
	return []ObjectDefinition{
		{Key: "vehicle_exchanges", Module: "dealership", Singular: "Exchange vehicle", Plural: "Exchange vehicles", Icon: "repeat", Prefix: "EXC",
			Description: "A customer's old vehicle offered in exchange: what it is, what they expect, what you offered and until when.",
			ObjectBody: ObjectBody{NameLabel: "Exchange vehicle", StatusLabel: "Status", Statuses: []StatusOption{
				st("declared", "Declared", "neutral"), st("evaluated", "Evaluated", "primary"), st("accepted", "Offer accepted", "success"),
				st("declined", "Offer declined", "danger"), st("taken_in", "Taken in", "success")},
				Fields: []ObjectField{
					ofLookup("contactId", "Customer", "contacts"), ofLookup("bookingId", "Booking", "vehicle_bookings"),
					ofReq(of("makeModel", "Make and model", "text")), of("modelYear", "Year", "number"), of("registrationNumber", "Registration number", "text"),
					of("kilometres", "Kilometres run", "number"), of("owners", "Number of owners", "number"),
					ofSelect("condition", "Condition", opts("excellent", "Excellent", "good", "Good", "fair", "Fair", "poor", "Poor")),
					of("loanOutstanding", "Loan still running on it", "boolean"),
					of("expectedPrice", "Customer expects", "currency"), of("offeredPrice", "Our offer", "currency"), of("validUntil", "Offer valid until", "date"),
					ofLookup("evaluatedBy", "Evaluated by", "users"), of("notes", "Evaluation notes", "textarea")}}},
		{Key: "finance_applications", Module: "dealership", Singular: "Loan application", Plural: "Loan applications", Icon: "banknote", Prefix: "LNA",
			Description: "A loan application to one lender for a booking: amount, rate, tenure, EMI and where it stands.",
			ObjectBody: ObjectBody{NameLabel: "Loan application", StatusLabel: "Status", Statuses: []StatusOption{
				st("draft", "Preparing", "neutral"), st("submitted", "Sent to lender", "primary"), st("approved", "Approved", "success"),
				st("rejected", "Rejected", "danger"), st("disbursed", "Disbursed", "success"), st("closed", "Not taken", "neutral")},
				Fields: []ObjectField{
					ofReq(ofLookup("bookingId", "Booking", "vehicle_bookings")), ofLookup("contactId", "Customer", "contacts"),
					ofReq(ofLookup("lenderId", "Lender", "accounts")),
					of("loanAmount", "Loan amount", "currency"), of("downPayment", "Down payment", "currency"),
					of("interestRate", "Interest rate (% a year)", "number"), of("tenureMonths", "Tenure (months)", "number"),
					of("emi", "EMI", "currency"), of("processingFee", "Processing fee", "currency"),
					of("lenderReference", "Lender's reference", "text"), of("documentsPending", "Documents still needed", "text"),
					of("decidedOn", "Decided on", "date"), of("disbursedOn", "Disbursed on", "date"), of("disbursedAmount", "Disbursed amount", "currency"),
					of("rejectionReason", "Why it was rejected", "text"), of("payoutExpected", "Our payout expected", "currency"),
					of("payoutReceived", "Our payout received", "currency"), of("notes", "Notes", "textarea")}}},
		{Key: "stock_transfers", Module: "dealership", Singular: "Stock transfer", Plural: "Stock transfers", Icon: "truck", Prefix: "TRF",
			Description: "A vehicle moving from one location or branch to another, until the other side confirms it arrived.",
			ObjectBody: ObjectBody{NameLabel: "Transfer", StatusLabel: "Status", Statuses: []StatusOption{
				st("in_transit", "On the way", "warning"), st("received", "Received", "success"), st("cancelled", "Cancelled", "neutral")},
				Fields: []ObjectField{
					ofReq(ofLookup("vehicleId", "Vehicle", "vehicle_stock")), of("fromLocation", "From", "text"), ofReq(of("toLocation", "To", "text")),
					of("sentOn", "Sent on", "date"), of("receivedOn", "Received on", "date"), of("challanNumber", "Challan / e-way bill number", "text"),
					ofLookup("receivedBy", "Received by", "users"), of("notes", "Notes", "textarea")}}},
	}
}

func dealershipMoreUpgrades() map[string]objectUpgrade {
	return map[string]objectUpgrade{
		"vehicle_bookings": {fields: []ObjectField{
			of("exShowroom", "Ex-showroom price", "currency"), of("roadTax", "Road tax and registration", "currency"),
			of("insurancePremium", "Insurance", "currency"), of("handlingCharges", "Handling charges", "currency"),
			of("hsrpCharges", "Number plate (HSRP)", "currency"), of("tcsAmount", "TCS", "currency"),
			of("accessoriesAmount", "Accessories", "currency"), of("extendedWarranty", "Extended warranty / service pack", "currency"),
			of("discountAmount", "Discount", "currency"), of("exchangeValue", "Exchange value", "currency"),
			of("balanceToPay", "Balance to pay", "currency")}},
	}
}

// bookingPrice works out the on-road price from its parts whenever any part is given:
// the parts added up, less the discount and the exchange value. Balance to pay is that
// less the advance. A booking with no parts keeps whatever on-road price was typed.
func bookingPrice(after *Row, set map[string]any) {
	given := false
	total := 0.0
	for _, k := range onRoadParts {
		if after.has(k) {
			given = true
			total += after.num(k)
		}
	}
	price := after.num("onRoadPrice")
	if given {
		price = math.Round((total-after.num("discountAmount")-after.num("exchangeValue"))*100) / 100
		if price != after.num("onRoadPrice") || !after.has("onRoadPrice") {
			set["onRoadPrice"] = price
		}
	}
	if after.has("onRoadPrice") || given {
		if bal := math.Round((price-after.num("bookingAmount"))*100) / 100; bal != after.num("balanceToPay") || !after.has("balanceToPay") {
			set["balanceToPay"] = bal
		}
	}
}

// exchangeSaved: an evaluated vehicle has an offer with an end date; an accepted offer
// comes off the booking's price.
func (h *Handler) exchangeSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, after *Row) (*Row, error) {
	status := after.text("status")
	if !after.has("contactId") && !after.has("bookingId") {
		return nil, refuse("contactId", "Pick the customer or the booking this vehicle is offered against.")
	}
	if (status == "evaluated" || status == "accepted" || status == "taken_in") && !after.has("offeredPrice") {
		return nil, refuse("offeredPrice", "Enter the price you are offering for this vehicle.")
	}
	if status == "taken_in" && after.flag("loanOutstanding") {
		return nil, refuse("loanOutstanding", "A loan is still running on this vehicle. Take it in only after the lender's no-objection — then untick this.")
	}
	set := map[string]any{}
	if after.has("offeredPrice") && !after.has("validUntil") {
		set["validUntil"] = time.Now().AddDate(0, 0, 7).Format("2006-01-02")
	}
	if status == "accepted" && (op == "create" || before.text("status") != "accepted") {
		if until := after.text("validUntil"); until != "" && until < today() {
			return nil, refuse("validUntil", "This offer has run out. Evaluate the vehicle again or extend the date.")
		}
	}
	// The booking carries the agreed exchange value while the offer stands.
	if b := after.id("bookingId"); b != uuid.Nil && specFor("vehicle_bookings") != nil {
		row, _, err := h.getRow(ctx, tx, ws, specFor("vehicle_bookings"), b, nil)
		if err == nil && row.text("status") != "delivered" && row.text("status") != "cancelled" {
			var want any
			switch status {
			case "accepted", "taken_in":
				want = after.num("offeredPrice")
			case "declined":
				want = nil
			default:
				want = row.Values["exchangeValue"]
			}
			if fmt.Sprint(want) != fmt.Sprint(row.Values["exchangeValue"]) {
				// Not the dealership source: the booking's own rules recompute the price.
				if _, err := h.updateValues(ctx, tx, ws, specFor("vehicle_bookings"), b, actorInfo{Kind: "system", Source: "exchange"},
					map[string]any{"exchangeValue": want, "exchangeVehicle": joinNonEmpty(" ", after.text("makeModel"), after.text("registrationNumber"))}, nil, nil); err != nil {
					return nil, err
				}
			}
		}
	}
	if len(set) == 0 {
		return after, nil
	}
	return h.updateValues(ctx, tx, ws, specFor("vehicle_exchanges"), after.uuid(), systemActor(sourceDealer), set, nil, nil)
}

// emiOf is the equal monthly instalment of a loan.
func emiOf(principal, yearlyRate, months float64) float64 {
	if principal <= 0 || months <= 0 {
		return 0
	}
	r := yearlyRate / 12 / 100
	if r == 0 {
		return math.Round(principal/months*100) / 100
	}
	f := math.Pow(1+r, months)
	return math.Round(principal*r*f/(f-1)*100) / 100
}

// financeSaved: the EMI is worked out, a rejection says why, and when one lender is
// approved the customer's other applications for that booking are closed.
func (h *Handler) financeSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, after *Row) (*Row, error) {
	spec := specFor("finance_applications")
	status := after.text("status")
	if status == "rejected" && !after.has("rejectionReason") {
		return nil, refuse("rejectionReason", "Say why the lender rejected it.")
	}
	if (status == "submitted" || status == "approved" || status == "disbursed") && after.num("loanAmount") <= 0 {
		return nil, refuse("loanAmount", "Enter the loan amount.")
	}
	if status == "disbursed" && op == "update" && before.text("status") != "approved" && before.text("status") != "disbursed" {
		return nil, refuse("status", "A loan is disbursed only after the lender approved it.")
	}
	set := map[string]any{}
	if after.has("loanAmount") && after.has("tenureMonths") {
		if e := emiOf(after.num("loanAmount"), after.num("interestRate"), after.num("tenureMonths")); e != after.num("emi") {
			set["emi"] = e
		}
	}
	changed := op == "create" || before.text("status") != status
	if changed && (status == "approved" || status == "rejected") && !after.has("decidedOn") {
		set["decidedOn"] = today()
	}
	if changed && status == "disbursed" && !after.has("disbursedOn") {
		set["disbursedOn"] = today()
	}
	if changed && status == "approved" {
		rows, err := tx.Query(ctx, `SELECT id FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'finance_applications' AND deleted_at IS NULL
			AND custom->>'bookingId' = $2 AND id <> $3 AND status IN ('draft', 'submitted', 'approved')`, ws, after.text("bookingId"), after.uuid())
		if err != nil {
			return nil, err
		}
		var others []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if rows.Scan(&id) == nil {
				others = append(others, id)
			}
		}
		rows.Close()
		for _, id := range others {
			if _, err := h.updateValues(ctx, tx, ws, spec, id, systemActor(sourceDealer), map[string]any{"status": "closed"}, nil, nil); err != nil {
				return nil, err
			}
		}
		if b := after.id("bookingId"); b != uuid.Nil {
			row, _, err := h.getRow(ctx, tx, ws, specFor("vehicle_bookings"), b, nil)
			if err == nil && row.text("status") != "delivered" && row.text("status") != "cancelled" {
				if _, err := h.updateValues(ctx, tx, ws, specFor("vehicle_bookings"), b, systemActor(sourceDealer),
					map[string]any{"paymentPlan": "loan", "financier": h.labelOf(ctx, tx, ws, "accounts", after.text("lenderId"))}, nil, nil); err != nil {
					return nil, err
				}
			}
		}
	}
	if len(set) == 0 {
		return after, nil
	}
	return h.updateValues(ctx, tx, ws, spec, after.uuid(), systemActor(sourceDealer), set, nil, nil)
}

// transferSaved: only a free vehicle travels; while it does it can't be allocated; the
// receiving side confirms and the vehicle's location changes.
func (h *Handler) transferSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, after *Row) (*Row, error) {
	spec := specFor("stock_transfers")
	status := after.text("status")
	vid := after.id("vehicleId")
	if op == "update" && before.id("vehicleId") != vid {
		return nil, refuse("vehicleId", "A transfer is for one vehicle. Cancel this one and make another.")
	}
	if op == "update" && before.text("status") != "in_transit" && before.text("status") != status {
		return nil, refuse("status", "This transfer is already finished.")
	}
	set := map[string]any{}
	switch {
	case op == "create":
		if status != "in_transit" {
			return nil, refuse("status", "A new transfer starts On the way; mark it Received when it arrives.")
		}
		var vStatus, loc, name string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(status, ''), COALESCE(custom->>'location', ''), name FROM crm.object_records
			WHERE id = $1 AND workspace_id = $2 AND object_key = 'vehicle_stock' AND deleted_at IS NULL FOR UPDATE`, vid, ws).Scan(&vStatus, &loc, &name); err != nil {
			return nil, refuse("vehicleId", "That vehicle isn't in your stock.")
		}
		if vStatus != "in_stock" && vStatus != "demo" {
			return nil, refuse("vehicleId", fmt.Sprintf("%s can't be moved: it is %s.", name, vStatus))
		}
		if loc != "" && loc == after.text("toLocation") {
			return nil, refuse("toLocation", "The vehicle is already there.")
		}
		if !after.has("fromLocation") {
			set["fromLocation"] = loc
		}
		if !after.has("sentOn") {
			set["sentOn"] = today()
		}
		if err := h.setVehicle(ctx, tx, ws, vid, map[string]any{"status": "in_transit"}); err != nil {
			return nil, err
		}
	case status == "received" && before.text("status") == "in_transit":
		if !after.has("receivedOn") {
			set["receivedOn"] = today()
		}
		if err := h.setVehicle(ctx, tx, ws, vid, map[string]any{"status": "in_stock", "location": after.text("toLocation")}); err != nil {
			return nil, err
		}
	case status == "cancelled" && before.text("status") == "in_transit":
		if err := h.setVehicle(ctx, tx, ws, vid, map[string]any{"status": "in_stock"}); err != nil {
			return nil, err
		}
	}
	clean(set)
	if len(set) == 0 {
		return after, nil
	}
	return h.updateValues(ctx, tx, ws, spec, after.uuid(), systemActor(sourceDealer), set, nil, nil)
}
