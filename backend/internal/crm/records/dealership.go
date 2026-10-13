package records

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The dealership pack (D-145): what a bike or car dealer needs beyond the general CRM.
//
//	vehicle_stock          one physical vehicle in the dealer's stock, known by its VIN
//	vehicle_bookings       a customer's booking: model, colour, advance, promised date
//	test_rides             a test ride or drive with its checks
//	vehicle_registrations  the road from temporary registration to the RC in hand
//	service_dues           the vehicle's service schedule, one row per service
//	odometer_readings      every reading, with its date and where it came from
//
// The rules that hold them together live here, on the server, so they apply to forms,
// imports and the API alike:
//
//   - a vehicle is allocated to at most one live booking, and only a vehicle in stock
//     can be allocated; cancelling or re-allocating frees it;
//   - a booking is delivered only with a vehicle, a passed inspection, the handover done,
//     the customer's sign-off and nothing left to pay on its invoice;
//   - delivery makes the customer's vehicle (an asset), opens its registration, writes the
//     first odometer reading and builds the service schedule — nobody types it again;
//   - closing a service proposes the next one by days and by kilometres, whichever comes first.

const sourceDealer = "dealership"

func dealershipObjects() []ObjectDefinition {
	return []ObjectDefinition{
		{Key: "vehicle_stock", Module: "dealership", Singular: "Vehicle", Plural: "Vehicle stock", Icon: "car", Prefix: "VEH",
			Description: "Every vehicle in stock, known by its VIN: model, colour, where it is and whether it is free, allocated or delivered.",
			ObjectBody: ObjectBody{NameLabel: "Vehicle", StatusLabel: "Status", Statuses: []StatusOption{
				st("in_stock", "In stock", "success"), st("in_transit", "In transit", "neutral"), st("allocated", "Allocated", "primary"),
				st("delivered", "Delivered", "neutral"), st("demo", "Demo vehicle", "warning"), st("on_hold", "On hold", "danger")},
				Fields: []ObjectField{
					ofReq(ofLookup("itemId", "Model / variant", "catalog_items")), of("colour", "Colour", "text"),
					{Key: "vin", Label: "VIN / chassis number", Type: "text", Required: true, Unique: true},
					{Key: "engineNumber", Label: "Engine / motor number", Type: "text", Unique: true},
					of("keyNumber", "Key number", "text"), of("location", "Location / branch", "text"),
					of("receivedDate", "Received on", "date"), of("landedCost", "Landed cost", "currency"),
					ofLookup("bookingId", "Allocated to booking", "vehicle_bookings"), ofLookup("assetId", "Customer's vehicle", "assets"),
					of("notes", "Notes", "textarea")}}},
		{Key: "vehicle_bookings", Module: "dealership", Singular: "Booking", Plural: "Bookings", Icon: "clipboard", Prefix: "BKG",
			Description: "A customer's booking from the advance to the handover: model, colour, promised date, the allocated vehicle and the delivery checks.",
			ObjectBody: ObjectBody{NameLabel: "Booking", StatusLabel: "Status", Statuses: []StatusOption{
				st("open", "Booked", "primary"), st("allocated", "Vehicle allocated", "primary"), st("ready", "Ready for delivery", "warning"),
				st("delivered", "Delivered", "success"), st("cancelled", "Cancelled", "danger")},
				Fields: []ObjectField{
					ofLookup("contactId", "Customer", "contacts"), ofLookup("accountId", "Account", "accounts"),
					ofReq(ofLookup("itemId", "Model / variant", "catalog_items")), of("colour", "Colour", "text"),
					of("bookingDate", "Booked on", "date"), of("promisedDate", "Promised delivery", "date"),
					of("bookingAmount", "Booking advance", "currency"), of("onRoadPrice", "On-road price", "currency"),
					ofSelect("paymentPlan", "Paying by", opts("cash", "Own funds", "loan", "Loan", "exchange", "Exchange + balance")),
					of("financier", "Financier", "text"), of("exchangeVehicle", "Exchange vehicle (make, model, year)", "text"),
					ofLookup("vehicleId", "Allocated vehicle", "vehicle_stock"),
					ofLookup("opportunityId", "Opportunity", "opportunities"), ofLookup("quoteId", "Quote", "quotes"),
					ofLookup("orderId", "Sales order", "sales_orders"), ofLookup("invoiceId", "Invoice", "invoices"),
					of("pdiPassed", "Pre-delivery inspection passed", "boolean"), of("accessoriesFitted", "Accessories fitted", "boolean"),
					of("documentsHandedOver", "Keys, manual, tool kit and warranty card handed over", "boolean"),
					of("customerSignedOff", "Customer signed the delivery note", "boolean"),
					of("deliveryDate", "Delivered on", "date"), of("deliveryOdometer", "Odometer at delivery (km)", "number"),
					of("expectedDailyKm", "Expected daily use (km)", "number"),
					ofSelect("cancellationReason", "Why it was cancelled", opts("changed_mind", "Changed their mind", "finance_rejected", "Finance rejected",
						"bought_elsewhere", "Bought elsewhere", "delivery_delay", "Delivery took too long", "price", "Price", "other", "Other")),
					of("refundAmount", "Refund amount", "currency"), ofLookup("assetId", "Customer's vehicle", "assets"),
					of("notes", "Notes", "textarea")}}},
		{Key: "test_rides", Module: "dealership", Singular: "Test ride", Plural: "Test rides", Icon: "flag", Prefix: "TRD",
			Description: "A test ride or drive: who, which vehicle, the licence and indemnity checks, and what the customer said.",
			ObjectBody: ObjectBody{NameLabel: "Test ride", StatusLabel: "Status", Statuses: []StatusOption{
				st("scheduled", "Scheduled", "primary"), st("completed", "Completed", "success"), st("cancelled", "Cancelled", "neutral"), st("no_show", "No show", "danger")},
				Fields: []ObjectField{
					ofLookup("leadId", "Lead", "leads"), ofLookup("contactId", "Customer", "contacts"),
					ofReq(ofLookup("itemId", "Model / variant", "catalog_items")), ofLookup("vehicleId", "Demo vehicle", "vehicle_stock"),
					ofReq(of("scheduledAt", "When", "datetime")), ofLookup("assignedTo", "Taken by", "users"),
					ofSelect("place", "Where", opts("showroom", "At the showroom", "doorstep", "At the customer's place")),
					of("licenceSeen", "Driving licence checked", "boolean"), of("licenceValidUntil", "Licence valid until", "date"),
					of("indemnitySigned", "Indemnity signed", "boolean"),
					of("startOdometer", "Odometer at start (km)", "number"), of("endOdometer", "Odometer at end (km)", "number"),
					of("rating", "Customer's rating", "rating"), of("feedback", "What the customer said", "textarea"),
					ofLookup("opportunityId", "Opportunity", "opportunities")}}},
		{Key: "vehicle_registrations", Module: "dealership", Singular: "Registration", Plural: "Registrations", Icon: "file-text", Prefix: "RGN",
			Description: "A vehicle's registration from the temporary number to the RC handed to the customer.",
			ObjectBody: ObjectBody{NameLabel: "Registration", StatusLabel: "Stage", Statuses: []StatusOption{
				st("documents_pending", "Documents pending", "danger"), st("temporary", "Temporary registration", "neutral"), st("rto_filed", "Filed at RTO", "primary"),
				st("tax_paid", "Road tax paid", "primary"), st("hsrp_ordered", "Number plate ordered", "primary"),
				st("rc_issued", "RC issued", "warning"), st("rc_handed_over", "RC handed over", "success")},
				Fields: []ObjectField{
					ofLookup("contactId", "Customer", "contacts"), ofLookup("accountId", "Account", "accounts"),
					ofLookup("assetId", "Customer's vehicle", "assets"), ofLookup("bookingId", "Booking", "vehicle_bookings"),
					of("rtoOffice", "RTO office", "text"), of("applicationNumber", "Application number", "text"),
					of("temporaryNumber", "Temporary number", "text"), of("registrationNumber", "Registration number", "text"),
					of("hsrpNumber", "Number plate (HSRP) reference", "text"), of("hypothecation", "Financier on the RC", "text"),
					of("documentsPending", "Documents still needed", "text"),
					of("filedOn", "Filed on", "date"), of("rcIssuedOn", "RC issued on", "date"), of("rcHandedOn", "RC handed over on", "date"),
					of("notes", "Notes", "textarea")}}},
		{Key: "service_dues", Module: "dealership", Singular: "Service due", Plural: "Service schedule", Icon: "wrench", Prefix: "SVD",
			Description: "Each vehicle's services, planned from the delivery date: when and at how many kilometres each is due, and what was done.",
			ObjectBody: ObjectBody{NameLabel: "Service", StatusLabel: "Status", Statuses: []StatusOption{
				st("planned", "Planned", "neutral"), st("due", "Due", "warning"), st("booked", "Booked", "primary"),
				st("done", "Done", "success"), st("skipped", "Skipped", "neutral")},
				Fields: []ObjectField{
					ofReq(ofLookup("assetId", "Vehicle", "assets")), ofLookup("contactId", "Customer", "contacts"), ofLookup("accountId", "Account", "accounts"),
					of("serviceNumber", "Service number", "number"),
					ofSelect("kind", "Kind", opts("free", "Free service", "paid", "Paid service", "annual", "Annual service")),
					of("dueDate", "Due date", "date"), of("dueKm", "Due at (km)", "number"),
					ofSelect("basis", "Set by", opts("days", "Days", "kilometres", "Kilometres", "manual", "Service team")),
					of("overrideReason", "Why the date was changed", "text"),
					ofLookup("workOrderId", "Job card", "work_orders"), ofLookup("appointmentId", "Appointment", "appointments"),
					of("doneOn", "Done on", "date"), of("closingOdometer", "Odometer at service (km)", "number"),
					of("notes", "Notes", "textarea")}}},
		{Key: "odometer_readings", Module: "dealership", Singular: "Odometer reading", Plural: "Odometer readings", Icon: "zap", Prefix: "ODO",
			Description: "Every odometer reading of a customer's vehicle, with its date and source.",
			ObjectBody: ObjectBody{NameLabel: "Reading", Statuses: []StatusOption{},
				Fields: []ObjectField{
					ofReq(ofLookup("assetId", "Vehicle", "assets")), ofReq(of("reading", "Reading (km)", "number")), of("readingDate", "Date", "date"),
					ofSelect("source", "Source", opts("delivery", "Delivery", "service", "Service", "arrival", "Arrival at workshop", "staff", "Staff", "customer", "Customer said"))}}},
	}
}

// dealershipUpgrades adds the vehicle side to objects that already exist.
func dealershipUpgrades() map[string]objectUpgrade {
	return map[string]objectUpgrade{
		"assets": {fields: []ObjectField{
			of("registrationNumber", "Registration number", "text"), of("vin", "VIN / chassis number", "text"), of("engineNumber", "Engine / motor number", "text"),
			of("colour", "Colour", "text"), of("odometer", "Odometer (km)", "number"), of("odometerDate", "Odometer read on", "date"),
			of("avgDailyKm", "Average daily use (km)", "number"), of("nextServiceDate", "Next service due", "date"), of("nextServiceKm", "Next service at (km)", "number"),
			of("insuranceExpiry", "Insurance expires", "date"), of("pucExpiry", "Pollution certificate expires", "date")}},
		"catalog_items": {fields: []ObjectField{
			of("firstServiceDays", "First service after (days)", "number"), of("firstServiceKm", "First service at (km)", "number"),
			of("serviceIntervalDays", "Service every (days)", "number"), of("serviceIntervalKm", "Service every (km)", "number"),
			of("freeServices", "Free services", "number")}},
		"work_orders": {fields: []ObjectField{of("odometer", "Odometer at arrival (km)", "number"), ofLookup("serviceDueId", "Scheduled service", "service_dues")}},
	}
}

// autoNamed objects name themselves from what they are about: nobody should have to
// invent a title for a booking or a test ride.
var autoNamed = map[string]bool{"vehicle_stock": true, "vehicle_bookings": true, "test_rides": true, "vehicle_registrations": true,
	"service_dues": true, "odometer_readings": true, "vehicle_exchanges": true, "finance_applications": true, "stock_transfers": true,
	"insurance_policies": true, "premium_dues": true, "commissions": true}

func (h *Handler) labelOf(ctx context.Context, q querier, ws uuid.UUID, object string, id any) string {
	spec := specFor(object)
	s := asString(id)
	if spec == nil || s == "" {
		return ""
	}
	var label string
	_ = q.QueryRow(ctx, "SELECT "+spec.TitleSQL+" FROM crm."+spec.Table+" t WHERE t.id::text = $1 AND t.workspace_id = $2", s, ws).Scan(&label)
	return label
}

func joinNonEmpty(sep string, parts ...string) string {
	out := []string{}
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, sep)
}

// autoName fills the name of a new record that was saved without one.
func (h *Handler) autoName(ctx context.Context, q querier, ws uuid.UUID, spec *objectSpec, values map[string]any) {
	if !autoNamed[spec.Key] {
		return
	}
	// These records also start at their first status (In stock, Booked, Scheduled…).
	if spec.StatusField != "" && len(spec.Statuses) > 0 && isEmpty(values["status"]) {
		values["status"] = spec.Statuses[0].Value
	}
	if !isEmpty(values["name"]) {
		return
	}
	item := h.labelOf(ctx, q, ws, "catalog_items", values["itemId"])
	who := h.labelOf(ctx, q, ws, "contacts", values["contactId"])
	if who == "" {
		who = h.labelOf(ctx, q, ws, "accounts", values["accountId"])
	}
	if who == "" {
		who = h.labelOf(ctx, q, ws, "leads", values["leadId"])
	}
	asset := h.labelOf(ctx, q, ws, "assets", values["assetId"])
	var name string
	switch spec.Key {
	case "vehicle_stock":
		vin := asString(values["vin"])
		if len(vin) > 6 {
			vin = "…" + vin[len(vin)-6:]
		}
		name = joinNonEmpty(" · ", item, asString(values["colour"]), vin)
	case "vehicle_bookings":
		name = joinNonEmpty(" — ", who, joinNonEmpty(" ", item, asString(values["colour"])))
	case "test_rides":
		name = joinNonEmpty(" — ", who, item)
	case "vehicle_registrations":
		name = joinNonEmpty(" — ", who, asset)
	case "service_dues":
		n, _ := asNumber(values["serviceNumber"])
		name = joinNonEmpty(" — ", fmt.Sprintf("Service %d", int(n)), asset)
		if n == 0 {
			name = joinNonEmpty(" — ", "Service", asset)
		}
	case "odometer_readings":
		km, _ := asNumber(values["reading"])
		name = joinNonEmpty(" — ", fmt.Sprintf("%d km", int(km)), asset)
	case "vehicle_exchanges":
		name = joinNonEmpty(" — ", joinNonEmpty(" ", asString(values["makeModel"]), asString(values["registrationNumber"])), who)
	case "finance_applications":
		name = joinNonEmpty(" — ", h.labelOf(ctx, q, ws, "accounts", values["lenderId"]), h.labelOf(ctx, q, ws, "vehicle_bookings", values["bookingId"]))
	case "stock_transfers":
		name = joinNonEmpty(" → ", h.labelOf(ctx, q, ws, "vehicle_stock", values["vehicleId"]), asString(values["toLocation"]))
	case "insurance_policies":
		name = joinNonEmpty(" — ", who, joinNonEmpty(" ", item, asString(values["policyNumber"])))
	case "premium_dues":
		n, _ := asNumber(values["instalmentNumber"])
		name = joinNonEmpty(" — ", fmt.Sprintf("Premium %d", int(n)), h.labelOf(ctx, q, ws, "insurance_policies", values["policyId"]))
	case "commissions":
		y, _ := asNumber(values["policyYear"])
		name = joinNonEmpty(" — ", fmt.Sprintf("Commission year %d", int(y)), h.labelOf(ctx, q, ws, "insurance_policies", values["policyId"]))
	}
	if name == "" {
		name = spec.Singular
	}
	if len(name) > 200 {
		name = name[:200]
	}
	values["name"] = name
}

func (r *Row) num(key string) float64 {
	if r == nil {
		return 0
	}
	n, _ := asNumber(r.Values[key])
	return n
}

func (r *Row) has(key string) bool { return r != nil && !isEmpty(r.Values[key]) }

func today() string { return time.Now().Format("2006-01-02") }

func refuse(field, msg string) error { return shared.Validation(map[string]string{field: msg}) }

// dealerSaved routes a save of a dealership record to its rules.
func (h *Handler) dealerSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, a actorInfo, op string, before, after *Row) (*Row, error) {
	if a.Source == sourceDealer {
		return after, nil
	}
	switch spec.Key {
	case "vehicle_stock":
		return after, h.vehicleSaved(op, before, after)
	case "vehicle_bookings":
		return h.bookingSaved(ctx, tx, ws, a, op, before, after)
	case "test_rides":
		return after, h.testRideSaved(ctx, tx, ws, a, op, before, after)
	case "vehicle_registrations":
		return h.registrationSaved(ctx, tx, ws, op, before, after)
	case "service_dues":
		return h.serviceDueSaved(ctx, tx, ws, a, op, before, after)
	case "odometer_readings":
		return after, h.odometerSaved(ctx, tx, ws, op, after)
	case "vehicle_exchanges":
		return h.exchangeSaved(ctx, tx, ws, op, before, after)
	case "finance_applications":
		return h.financeSaved(ctx, tx, ws, op, before, after)
	case "stock_transfers":
		return h.transferSaved(ctx, tx, ws, op, before, after)
	}
	return after, nil
}

// vehicleSaved: only a booking allocates or delivers a vehicle.
func (h *Handler) vehicleSaved(op string, before, after *Row) error {
	was := before.text("status")
	now := after.text("status")
	if op == "create" && (now == "allocated" || now == "delivered" || after.has("bookingId")) {
		return refuse("status", "Add the vehicle as In stock, then allocate it from the customer's booking.")
	}
	if op != "update" {
		return nil
	}
	if before.text("bookingId") != after.text("bookingId") {
		return refuse("bookingId", "Allocate or release a vehicle from the booking, not from here.")
	}
	if was != now && (now == "allocated" || now == "delivered" || was == "allocated" || was == "delivered") {
		return refuse("status", "This status follows the booking: allocate, cancel or deliver the booking instead.")
	}
	return nil
}

func (h *Handler) setVehicle(ctx context.Context, tx pgx.Tx, ws, vehicle uuid.UUID, values map[string]any) error {
	_, err := h.updateValues(ctx, tx, ws, specFor("vehicle_stock"), vehicle, systemActor(sourceDealer), values, nil, nil)
	return err
}

// bookingSaved keeps the booking and its vehicle in step, and runs the delivery.
func (h *Handler) bookingSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, op string, before, after *Row) (*Row, error) {
	spec := specFor("vehicle_bookings")
	id := after.uuid()
	status, was := after.text("status"), before.text("status")
	oldV, newV := before.id("vehicleId"), after.id("vehicleId")
	set := map[string]any{}
	if op == "update" && was == "delivered" && status != "delivered" {
		return nil, refuse("status", "A delivered booking stays delivered. Raise a case if something went wrong with the delivery.")
	}
	if op == "update" && was == "delivered" && oldV != newV {
		return nil, refuse("vehicleId", "The vehicle of a delivered booking can't be changed.")
	}
	if op == "update" && was == "cancelled" && status != "cancelled" {
		return nil, refuse("status", "A cancelled booking stays cancelled. Make a new booking for the customer.")
	}
	if !after.has("contactId") && !after.has("accountId") {
		return nil, refuse("contactId", "Pick the customer who is booking.")
	}
	if op == "create" && !after.has("bookingDate") {
		set["bookingDate"] = today()
	}
	if status != "delivered" || was != "delivered" {
		bookingPrice(after, set)
	}

	if status == "cancelled" {
		if !after.has("cancellationReason") {
			return nil, refuse("cancellationReason", "Say why the booking was cancelled.")
		}
		// The vehicle goes back to stock, exactly once.
		for _, v := range []uuid.UUID{oldV, newV} {
			if v == uuid.Nil {
				continue
			}
			if err := h.releaseVehicle(ctx, tx, ws, v, id); err != nil {
				return nil, err
			}
		}
		if newV != uuid.Nil {
			set["vehicleId"] = nil
		}
		return h.applyBooking(ctx, tx, ws, spec, id, after, set)
	}

	if newV != oldV || (op == "create" && newV != uuid.Nil) {
		if oldV != uuid.Nil && oldV != newV {
			if err := h.releaseVehicle(ctx, tx, ws, oldV, id); err != nil {
				return nil, err
			}
		}
		if newV != uuid.Nil {
			if err := h.allocateVehicle(ctx, tx, ws, newV, id, after); err != nil {
				return nil, err
			}
			if status == "open" || status == "" {
				set["status"], status = "allocated", "allocated"
			}
		} else if status == "allocated" || status == "ready" {
			set["status"], status = "open", "open"
		}
	}
	if newV == uuid.Nil && (status == "allocated" || status == "ready" || status == "delivered") {
		return nil, refuse("vehicleId", "Allocate a vehicle from stock first.")
	}
	if status == "ready" && !after.flag("pdiPassed") {
		return nil, refuse("pdiPassed", "The vehicle is ready only after its pre-delivery inspection has passed.")
	}

	if status == "delivered" && was != "delivered" {
		fe := map[string]string{}
		if !after.flag("pdiPassed") {
			fe["pdiPassed"] = "The pre-delivery inspection must pass before delivery."
		}
		if !after.flag("documentsHandedOver") {
			fe["documentsHandedOver"] = "Tick this once the keys, manual, tool kit and warranty card are handed over."
		}
		if !after.flag("customerSignedOff") {
			fe["customerSignedOff"] = "The customer signs the delivery note before the vehicle leaves."
		}
		if inv := after.id("invoiceId"); inv != uuid.Nil && specFor("invoices") != nil {
			if row, _, err := h.getRow(ctx, tx, ws, specFor("invoices"), inv, nil); err == nil && row.num("balanceDue") > 0.005 {
				fe["invoiceId"] = fmt.Sprintf("%s still has %.2f to pay. Record the payment before delivery.", row.Code, row.num("balanceDue"))
			}
		}
		if len(fe) > 0 {
			return nil, shared.Validation(fe)
		}
		if !after.has("deliveryDate") {
			set["deliveryDate"] = today()
		}
		row, err := h.applyBooking(ctx, tx, ws, spec, id, after, set)
		if err != nil {
			return nil, err
		}
		return h.deliverBooking(ctx, tx, ws, a, row)
	}
	return h.applyBooking(ctx, tx, ws, spec, id, after, set)
}

func (h *Handler) applyBooking(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, id uuid.UUID, after *Row, set map[string]any) (*Row, error) {
	if len(set) == 0 {
		return after, nil
	}
	return h.updateValues(ctx, tx, ws, spec, id, systemActor(sourceDealer), set, nil, nil)
}

// allocateVehicle gives a vehicle in stock to a booking. The row is locked, so two
// people allocating the same vehicle at the same moment can't both succeed.
func (h *Handler) allocateVehicle(ctx context.Context, tx pgx.Tx, ws, vehicle, booking uuid.UUID, b *Row) error {
	var status, held, item, name string
	err := tx.QueryRow(ctx, `SELECT COALESCE(status, ''), COALESCE(custom->>'bookingId', ''), COALESCE(custom->>'itemId', ''), name
		FROM crm.object_records WHERE id = $1 AND workspace_id = $2 AND object_key = 'vehicle_stock' AND deleted_at IS NULL FOR UPDATE`, vehicle, ws).
		Scan(&status, &held, &item, &name)
	if errors.Is(err, pgx.ErrNoRows) {
		return refuse("vehicleId", "That vehicle isn't in your stock.")
	}
	if err != nil {
		return err
	}
	if held == booking.String() {
		return nil
	}
	if held != "" || status != "in_stock" {
		why := "it is " + strings.ReplaceAll(status, "_", " ")
		if held != "" {
			why = "it is allocated to " + joinNonEmpty(" ", h.codeOf(ctx, tx, ws, held), "already")
		}
		return refuse("vehicleId", fmt.Sprintf("%s can't be allocated: %s. Pick a vehicle that is In stock.", name, why))
	}
	if want := b.text("itemId"); want != "" && item != "" && want != item {
		return refuse("vehicleId", fmt.Sprintf("%s is a different model from the one booked. Change the booking's model first if the customer agreed.", name))
	}
	return h.setVehicle(ctx, tx, ws, vehicle, map[string]any{"status": "allocated", "bookingId": booking.String()})
}

func (h *Handler) codeOf(ctx context.Context, q querier, ws uuid.UUID, id string) string {
	var code string
	_ = q.QueryRow(ctx, `SELECT code FROM crm.object_records WHERE id::text = $1 AND workspace_id = $2`, id, ws).Scan(&code)
	return code
}

// releaseVehicle puts a vehicle back in stock if this booking holds it.
func (h *Handler) releaseVehicle(ctx context.Context, tx pgx.Tx, ws, vehicle, booking uuid.UUID) error {
	var status, held string
	err := tx.QueryRow(ctx, `SELECT COALESCE(status, ''), COALESCE(custom->>'bookingId', '') FROM crm.object_records
		WHERE id = $1 AND workspace_id = $2 AND object_key = 'vehicle_stock' FOR UPDATE`, vehicle, ws).Scan(&status, &held)
	if err != nil || held != booking.String() || status == "delivered" {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		return err
	}
	return h.setVehicle(ctx, tx, ws, vehicle, map[string]any{"status": "in_stock", "bookingId": nil})
}

// deliverBooking does everything a delivery sets in motion.
func (h *Handler) deliverBooking(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, b *Row) (*Row, error) {
	sys := systemActor(sourceDealer)
	vehicle, _, err := h.getRow(ctx, tx, ws, specFor("vehicle_stock"), b.id("vehicleId"), nil)
	if err != nil {
		return nil, err
	}
	owner := b.text("ownerId")
	delivered := b.text("deliveryDate")
	if len(delivered) > 10 {
		delivered = delivered[:10]
	}
	model := h.labelOf(ctx, tx, ws, "catalog_items", b.text("itemId"))
	asset := map[string]any{
		"name": joinNonEmpty(" · ", model, vehicle.text("colour")), "status": "active", "ownerId": owner,
		"accountId": b.text("accountId"), "contactId": b.text("contactId"), "itemId": b.text("itemId"),
		"serialNumber": vehicle.text("vin"), "vin": vehicle.text("vin"), "engineNumber": vehicle.text("engineNumber"), "colour": vehicle.text("colour"),
		"purchaseDate": delivered, "installDate": delivered, "warrantyStart": delivered,
	}
	if b.has("onRoadPrice") {
		asset["price"] = b.Values["onRoadPrice"]
	}
	if b.has("expectedDailyKm") {
		asset["avgDailyKm"] = b.Values["expectedDailyKm"]
	}
	clean(asset)
	assetRow, err := h.createRecord(ctx, tx, ws, specFor("assets"), sys, asset)
	if err != nil {
		return nil, err
	}
	assetID := assetRow.ID
	if err := h.setVehicle(ctx, tx, ws, vehicle.uuid(), map[string]any{"status": "delivered", "assetId": assetID}); err != nil {
		return nil, err
	}
	// Registration starts here; the number is written back to the vehicle when it comes.
	if specFor("vehicle_registrations") != nil {
		reg := map[string]any{"status": "temporary", "ownerId": owner, "assetId": assetID, "bookingId": b.ID,
			"accountId": b.text("accountId"), "contactId": b.text("contactId"), "hypothecation": b.text("financier")}
		clean(reg)
		h.autoName(ctx, tx, ws, specFor("vehicle_registrations"), reg)
		if _, err := h.createRecord(ctx, tx, ws, specFor("vehicle_registrations"), sys, reg); err != nil {
			return nil, err
		}
	}
	// The first reading, and the schedule that counts from it.
	odo := b.num("deliveryOdometer")
	if err := h.addReading(ctx, tx, ws, assetID, odo, delivered, "delivery", owner); err != nil {
		return nil, err
	}
	if err := h.buildServicePlan(ctx, tx, ws, assetRow, b, delivered, odo); err != nil {
		return nil, err
	}
	// A thank-you and feedback call three days on.
	if specFor("tasks") != nil {
		due, _ := time.Parse("2006-01-02", delivered)
		task := map[string]any{"name": "Thank-you and feedback call — " + joinNonEmpty(" ", model), "status": "not_started", "ownerId": owner,
			"dueDate": due.AddDate(0, 0, 3).Format("2006-01-02"), "accountId": b.text("accountId"), "contactId": b.text("contactId")}
		clean(task)
		if _, err := h.createRecord(ctx, tx, ws, specFor("tasks"), sys, task); err != nil {
			return nil, err
		}
	}
	_ = a
	return h.updateValues(ctx, tx, ws, specFor("vehicle_bookings"), b.uuid(), sys, map[string]any{"assetId": assetID}, nil, nil)
}

// clean drops empty values so they aren't validated as given.
func clean(m map[string]any) {
	for k, v := range m {
		if isEmpty(v) {
			delete(m, k)
		}
	}
}

// servicePlan is a model's service rule. Zero values fall back to the common
// two-wheeler pattern; every number is a field on the model (catalog item).
type servicePlan struct {
	firstDays, firstKm, everyDays, everyKm float64
	free                                   int
}

func (h *Handler) planFor(ctx context.Context, q querier, ws uuid.UUID, item uuid.UUID) servicePlan {
	p := servicePlan{firstDays: 30, firstKm: 750, everyDays: 120, everyKm: 3000, free: 3}
	if item == uuid.Nil || specFor("catalog_items") == nil {
		return p
	}
	row, _, err := h.getRow(ctx, q, ws, specFor("catalog_items"), item, nil)
	if err != nil {
		return p
	}
	pick := func(key string, into *float64) {
		if n := row.num(key); n > 0 {
			*into = n
		}
	}
	pick("firstServiceDays", &p.firstDays)
	pick("firstServiceKm", &p.firstKm)
	pick("serviceIntervalDays", &p.everyDays)
	pick("serviceIntervalKm", &p.everyKm)
	if row.has("freeServices") {
		p.free = int(row.num("freeServices"))
	}
	return p
}

// buildServicePlan writes the free services of a delivered vehicle: the first one at the
// model's first-service mark, the rest one interval apart.
func (h *Handler) buildServicePlan(ctx context.Context, tx pgx.Tx, ws uuid.UUID, asset, b *Row, delivered string, odo float64) error {
	spec := specFor("service_dues")
	if spec == nil {
		return nil
	}
	p := h.planFor(ctx, tx, ws, b.id("itemId"))
	n := p.free
	if n < 1 {
		n = 1 // even with no free services, the first paid one is planned
	}
	start, _ := time.Parse("2006-01-02", delivered)
	var first map[string]any
	for i := 1; i <= n; i++ {
		days := p.firstDays + float64(i-1)*p.everyDays
		km := odo + p.firstKm + float64(i-1)*p.everyKm
		kind := "free"
		if i > p.free {
			kind = "paid"
		}
		due := map[string]any{"status": "planned", "ownerId": b.text("ownerId"), "assetId": asset.ID, "accountId": b.text("accountId"), "contactId": b.text("contactId"),
			"serviceNumber": float64(i), "kind": kind, "dueDate": start.AddDate(0, 0, int(days)).Format("2006-01-02"), "dueKm": km, "basis": "days"}
		clean(due)
		h.autoName(ctx, tx, ws, spec, due)
		if _, err := h.createRecord(ctx, tx, ws, spec, systemActor(sourceDealer), due); err != nil {
			return err
		}
		if i == 1 {
			first = due
		}
	}
	_, err := h.updateValues(ctx, tx, ws, specFor("assets"), asset.uuid(), systemActor(sourceDealer),
		map[string]any{"nextServiceDate": first["dueDate"], "nextServiceKm": first["dueKm"]}, nil, nil)
	return err
}

// addReading stores an odometer reading and brings the vehicle's odometer and average
// daily use up to date. A zero reading with nothing known is still a starting point.
func (h *Handler) addReading(ctx context.Context, tx pgx.Tx, ws uuid.UUID, asset string, km float64, date, source, owner string) error {
	spec := specFor("odometer_readings")
	if spec == nil {
		return nil
	}
	v := map[string]any{"assetId": asset, "reading": km, "readingDate": date, "source": source, "ownerId": owner}
	clean(v)
	v["reading"] = km
	h.autoName(ctx, tx, ws, spec, v)
	if _, err := h.createRecord(ctx, tx, ws, spec, systemActor(sourceDealer), v); err != nil {
		return err
	}
	return h.refreshOdometer(ctx, tx, ws, asset)
}

// refreshOdometer recomputes what the vehicle shows: the latest reading and the average
// daily kilometres between the earliest and latest readings at least 30 days apart
// (with less history the estimate given at the sale stays).
func (h *Handler) refreshOdometer(ctx context.Context, tx pgx.Tx, ws uuid.UUID, asset string) error {
	var lastKm, firstKm float64
	var lastDate, firstDate *time.Time
	err := tx.QueryRow(ctx, `
		WITH r AS (SELECT (custom->>'reading')::numeric AS km, COALESCE(NULLIF(custom->>'readingDate', '')::date, created_at::date) AS d
		           FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'odometer_readings' AND deleted_at IS NULL
		             AND custom->>'assetId' = $2 AND COALESCE(custom->>'source', '') <> 'customer')
		SELECT COALESCE((SELECT km FROM r ORDER BY d DESC, km DESC LIMIT 1), 0), (SELECT d FROM r ORDER BY d DESC, km DESC LIMIT 1),
		       COALESCE((SELECT km FROM r ORDER BY d ASC, km ASC LIMIT 1), 0), (SELECT d FROM r ORDER BY d ASC, km ASC LIMIT 1)`, ws, asset).
		Scan(&lastKm, &lastDate, &firstKm, &firstDate)
	if err != nil || lastDate == nil {
		return err
	}
	set := map[string]any{"odometer": lastKm, "odometerDate": lastDate.Format("2006-01-02")}
	if days := lastDate.Sub(*firstDate).Hours() / 24; days >= 30 && lastKm-firstKm >= 100 {
		set["avgDailyKm"] = math.Round((lastKm-firstKm)/days*10) / 10
	}
	id, err := uuid.Parse(asset)
	if err != nil {
		return nil
	}
	_, err = h.updateValues(ctx, tx, ws, specFor("assets"), id, systemActor(sourceDealer), set, nil, nil)
	return err
}

// odometerSaved: a reading typed in by hand. Lower than the last one is refused.
func (h *Handler) odometerSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, after *Row) error {
	if after.num("reading") < 0 {
		return refuse("reading", "A reading can't be below zero.")
	}
	var prev *float64
	if err := tx.QueryRow(ctx, `SELECT max((custom->>'reading')::numeric) FROM crm.object_records
		WHERE workspace_id = $1 AND object_key = 'odometer_readings' AND deleted_at IS NULL AND custom->>'assetId' = $2 AND id <> $3
		  AND COALESCE(custom->>'source', '') <> 'customer'
		  AND COALESCE(NULLIF(custom->>'readingDate', '')::date, created_at::date) <= COALESCE(NULLIF($4, '')::date, now()::date)`,
		ws, after.text("assetId"), after.uuid(), after.text("readingDate")).Scan(&prev); err != nil {
		return err
	}
	if prev != nil && after.num("reading") < *prev && after.text("source") != "customer" {
		return refuse("reading", fmt.Sprintf("The last reading was %.0f km. An odometer doesn't go backwards — check the number.", *prev))
	}
	if !after.has("readingDate") && op == "create" {
		if _, err := h.updateValues(ctx, tx, ws, specFor("odometer_readings"), after.uuid(), systemActor(sourceDealer), map[string]any{"readingDate": today()}, nil, nil); err != nil {
			return err
		}
	}
	return h.refreshOdometer(ctx, tx, ws, after.text("assetId"))
}

// testRideSaved: the checks before a ride, and the follow-up after it.
func (h *Handler) testRideSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, op string, before, after *Row) error {
	status := after.text("status")
	if !after.has("contactId") && !after.has("leadId") {
		return refuse("contactId", "Pick the lead or customer taking the ride.")
	}
	if until := after.text("licenceValidUntil"); until != "" && status != "cancelled" && status != "no_show" {
		ride := after.text("scheduledAt")
		if len(ride) >= 10 && until < ride[:10] {
			return refuse("licenceValidUntil", "The licence expires before the ride. A test ride needs a valid licence.")
		}
	}
	if status != "completed" {
		return nil
	}
	fe := map[string]string{}
	if !after.flag("licenceSeen") {
		fe["licenceSeen"] = "Check the driving licence before the ride."
	}
	if !after.flag("indemnitySigned") {
		fe["indemnitySigned"] = "The customer signs the indemnity before the ride."
	}
	if after.has("startOdometer") && after.has("endOdometer") && after.num("endOdometer") < after.num("startOdometer") {
		fe["endOdometer"] = "The end reading can't be lower than the start."
	}
	if len(fe) > 0 {
		return shared.Validation(fe)
	}
	if (op == "create" || before.text("status") != "completed") && specFor("tasks") != nil {
		task := map[string]any{"name": "Follow up after the test ride — " + after.Title, "status": "not_started", "priority": "high",
			"ownerId": after.text("ownerId"), "dueDate": today(), "contactId": after.text("contactId"), "leadId": after.text("leadId"),
			"opportunityId": after.text("opportunityId")}
		clean(task)
		if _, err := h.createRecord(ctx, tx, ws, specFor("tasks"), systemActor(sourceDealer), task); err != nil {
			return err
		}
	}
	_ = a
	return nil
}

// registrationSaved: stage dates fill themselves, and the number reaches the vehicle.
func (h *Handler) registrationSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, after *Row) (*Row, error) {
	status := after.text("status")
	reg := strings.ToUpper(strings.TrimSpace(after.text("registrationNumber")))
	if (status == "rc_issued" || status == "rc_handed_over") && reg == "" {
		return nil, refuse("registrationNumber", "Enter the registration number from the RC.")
	}
	set := map[string]any{}
	if reg != after.text("registrationNumber") {
		set["registrationNumber"] = reg
	}
	stamp := func(stage, field string) {
		if status == stage && (op == "create" || before.text("status") != stage) && !after.has(field) {
			set[field] = today()
		}
	}
	stamp("rto_filed", "filedOn")
	stamp("rc_issued", "rcIssuedOn")
	stamp("rc_handed_over", "rcHandedOn")
	if asset := after.id("assetId"); asset != uuid.Nil && reg != "" && (op == "create" || before.text("registrationNumber") != reg) {
		row, _, err := h.getRow(ctx, tx, ws, specFor("assets"), asset, nil)
		if err == nil && row.text("registrationNumber") != reg {
			name := row.Title
			if !strings.Contains(name, reg) {
				name = joinNonEmpty(" ", name, reg)
			}
			if _, err := h.updateValues(ctx, tx, ws, specFor("assets"), asset, systemActor(sourceDealer), map[string]any{"registrationNumber": reg, "name": name}, nil, nil); err != nil {
				return nil, err
			}
		}
	}
	if len(set) == 0 {
		return after, nil
	}
	return h.updateValues(ctx, tx, ws, specFor("vehicle_registrations"), after.uuid(), systemActor(sourceDealer), set, nil, nil)
}

// serviceDueSaved: closing a service records the odometer and plans the next one.
func (h *Handler) serviceDueSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, op string, before, after *Row) (*Row, error) {
	spec := specFor("service_dues")
	sys := systemActor(sourceDealer)
	set := map[string]any{}
	// A date or kilometre figure changed by hand is the service team's call: say why.
	if op == "update" && before.text("status") != "done" && after.text("status") != "done" &&
		(before.text("dueDate") != after.text("dueDate") || before.num("dueKm") != after.num("dueKm")) {
		if !after.has("overrideReason") {
			return nil, refuse("overrideReason", "Say why the due date or kilometres were changed.")
		}
		set["basis"] = "manual"
	}
	closing := after.text("status") == "done" && (op == "create" || before.text("status") != "done")
	if !closing {
		if len(set) > 0 {
			var err error
			if after, err = h.updateValues(ctx, tx, ws, spec, after.uuid(), sys, set, nil, nil); err != nil {
				return nil, err
			}
		}
		return after, h.syncNextService(ctx, tx, ws, after.text("assetId"))
	}
	if !after.has("closingOdometer") {
		return nil, refuse("closingOdometer", "Enter the odometer reading at this service — the next service is worked out from it.")
	}
	done := after.text("doneOn")
	if done == "" {
		done = today()
		set["doneOn"] = done
	}
	km := after.num("closingOdometer")
	var prev *float64
	if err := tx.QueryRow(ctx, `SELECT max((custom->>'reading')::numeric) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'odometer_readings'
		AND deleted_at IS NULL AND custom->>'assetId' = $2 AND COALESCE(custom->>'source', '') <> 'customer'`, ws, after.text("assetId")).Scan(&prev); err != nil {
		return nil, err
	}
	if prev != nil && km < *prev {
		return nil, refuse("closingOdometer", fmt.Sprintf("The last reading was %.0f km. An odometer doesn't go backwards — check the number.", *prev))
	}
	if len(set) > 0 {
		var err error
		if after, err = h.updateValues(ctx, tx, ws, spec, after.uuid(), sys, set, nil, nil); err != nil {
			return nil, err
		}
	}
	if err := h.addReading(ctx, tx, ws, after.text("assetId"), km, done, "service", after.text("ownerId")); err != nil {
		return nil, err
	}
	// Never two open services for one vehicle: the next is created only when none waits.
	var open int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'service_dues' AND deleted_at IS NULL
		AND custom->>'assetId' = $2 AND status IN ('planned', 'due', 'booked')`, ws, after.text("assetId")).Scan(&open); err != nil {
		return nil, err
	}
	if open == 0 {
		if err := h.planNextService(ctx, tx, ws, after, done, km); err != nil {
			return nil, err
		}
	}
	_ = a
	return after, h.syncNextService(ctx, tx, ws, after.text("assetId"))
}

// planNextService proposes the next service: by days from this one, and by kilometres
// turned into a date with the vehicle's average daily use. The earlier of the two wins,
// and the record says which one set it.
func (h *Handler) planNextService(ctx context.Context, tx pgx.Tx, ws uuid.UUID, last *Row, done string, km float64) error {
	asset, _, err := h.getRow(ctx, tx, ws, specFor("assets"), last.id("assetId"), nil)
	if err != nil {
		return err
	}
	p := h.planFor(ctx, tx, ws, asset.id("itemId"))
	from, _ := time.Parse("2006-01-02", done)
	byDays := from.AddDate(0, 0, int(p.everyDays))
	due, basis := byDays, "days"
	if avg := asset.num("avgDailyKm"); avg > 0 {
		if byKm := from.AddDate(0, 0, int(math.Ceil(p.everyKm/avg))); byKm.Before(byDays) {
			due, basis = byKm, "kilometres"
		}
	}
	n := int(last.num("serviceNumber")) + 1
	kind := "paid"
	if n <= p.free {
		kind = "free"
	}
	v := map[string]any{"status": "planned", "ownerId": last.text("ownerId"), "assetId": last.text("assetId"), "accountId": asset.text("accountId"),
		"contactId": asset.text("contactId"), "serviceNumber": float64(n), "kind": kind, "dueDate": due.Format("2006-01-02"), "dueKm": km + p.everyKm, "basis": basis}
	clean(v)
	h.autoName(ctx, tx, ws, specFor("service_dues"), v)
	_, err = h.createRecord(ctx, tx, ws, specFor("service_dues"), systemActor(sourceDealer), v)
	return err
}

// syncNextService shows the earliest waiting service on the vehicle itself.
func (h *Handler) syncNextService(ctx context.Context, tx pgx.Tx, ws uuid.UUID, asset string) error {
	id, err := uuid.Parse(asset)
	if err != nil {
		return nil
	}
	var date *string
	var km *float64
	err = tx.QueryRow(ctx, `SELECT NULLIF(custom->>'dueDate', ''), NULLIF(custom->>'dueKm', '')::numeric FROM crm.object_records
		WHERE workspace_id = $1 AND object_key = 'service_dues' AND deleted_at IS NULL AND custom->>'assetId' = $2 AND status IN ('planned', 'due', 'booked')
		ORDER BY NULLIF(custom->>'dueDate', '') NULLS LAST LIMIT 1`, ws, asset).Scan(&date, &km)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	set := map[string]any{"nextServiceDate": nil, "nextServiceKm": nil}
	if date != nil {
		set["nextServiceDate"] = *date
	}
	if km != nil {
		set["nextServiceKm"] = *km
	}
	_, err = h.updateValues(ctx, tx, ws, specFor("assets"), id, systemActor(sourceDealer), set, nil, nil)
	return err
}

// workOrderService: a job card for a scheduled service closes that service when the job
// is completed, carrying the odometer noted at arrival.
func (h *Handler) workOrderService(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, after *Row) error {
	due := after.id("serviceDueId")
	if due == uuid.Nil || specFor("service_dues") == nil {
		return nil
	}
	spec := specFor("service_dues")
	row, _, err := h.getRow(ctx, tx, ws, spec, due, nil)
	if err != nil {
		return nil
	}
	a := actorInfo{Kind: "system", Source: "workorder"}
	switch {
	case after.text("status") == "completed" && (op == "create" || before.text("status") != "completed") && row.text("status") != "done":
		if !after.has("odometer") {
			return refuse("odometer", "Enter the odometer reading — this job is a scheduled service and the next one is worked out from it.")
		}
		_, err = h.updateValues(ctx, tx, ws, spec, due, a, map[string]any{"status": "done", "closingOdometer": after.Values["odometer"], "workOrderId": after.ID}, nil, nil)
	case op == "create" && (row.text("status") == "planned" || row.text("status") == "due"):
		_, err = h.updateValues(ctx, tx, ws, spec, due, systemActor(sourceDealer), map[string]any{"status": "booked", "workOrderId": after.ID}, nil, nil)
	}
	return err
}

// sweepServiceDues turns planned services into due ones when their window opens, and
// gives the owner a call task — once. Safe to run any number of times.
func (h *Handler) sweepServiceDues(ctx context.Context) {
	if specFor("service_dues") == nil || specFor("tasks") == nil {
		return
	}
	rows, err := h.store.Pool.Query(ctx, `SELECT id, workspace_id FROM crm.object_records WHERE object_key = 'service_dues' AND deleted_at IS NULL
		AND status = 'planned' AND NULLIF(custom->>'dueDate', '')::date <= now()::date + 7 LIMIT 500`)
	if err != nil {
		return
	}
	type due struct{ id, ws uuid.UUID }
	var list []due
	for rows.Next() {
		var d due
		if rows.Scan(&d.id, &d.ws) == nil {
			list = append(list, d)
		}
	}
	rows.Close()
	for _, d := range list {
		_ = h.store.WithTx(ctx, func(tx pgx.Tx) error {
			var status string
			if err := tx.QueryRow(ctx, `SELECT status FROM crm.object_records WHERE id = $1 FOR UPDATE`, d.id).Scan(&status); err != nil || status != "planned" {
				return err
			}
			sys := systemActor(sourceDealer)
			row, err := h.updateValues(ctx, tx, d.ws, specFor("service_dues"), d.id, sys, map[string]any{"status": "due"}, nil, nil)
			if err != nil {
				return err
			}
			task := map[string]any{"name": "Call to book: " + row.Title, "status": "not_started", "ownerId": row.text("ownerId"),
				"dueDate": today(), "accountId": row.text("accountId"), "contactId": row.text("contactId")}
			clean(task)
			_, err = h.createRecord(ctx, tx, d.ws, specFor("tasks"), sys, task)
			return err
		})
	}
}

// grantPackAccess gives the built-in "Admin access" and "Staff access" sets of the
// businesses that already exist what a new business gets for a pack's objects. It runs
// once per marker and only ever adds: a set an admin has edited keeps its edits.
func (h *Handler) grantPackAccess(ctx context.Context, marker string, objects []string) error {
	return h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var done bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.connector_state WHERE key = $1)`, marker).Scan(&done); err != nil || done {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id, system_key, rules FROM crm.permission_sets WHERE system_key IN ('ADMIN', 'STAFF')`)
		if err != nil {
			return err
		}
		type set struct {
			id  uuid.UUID
			key string
			raw []byte
		}
		var sets []set
		for rows.Next() {
			var s set
			if err := rows.Scan(&s.id, &s.key, &s.raw); err != nil {
				rows.Close()
				return err
			}
			sets = append(sets, s)
		}
		rows.Close()
		for _, s := range sets {
			tpl, ok := access.FindSystemRole(s.key)
			if !ok {
				continue
			}
			var rules access.Rules
			if json.Unmarshal(s.raw, &rules) != nil {
				continue
			}
			if rules.Objects == nil {
				rules.Objects = map[string][]string{}
			}
			if rules.Rows == nil {
				rules.Rows = map[string]map[string]string{}
			}
			changed := false
			for _, o := range objects {
				if _, has := rules.Objects[o]; has || len(tpl.Rules.Objects[o]) == 0 {
					continue
				}
				rules.Objects[o], rules.Rows[o] = tpl.Rules.Objects[o], tpl.Rules.Rows[o]
				changed = true
			}
			if !changed {
				continue
			}
			raw, _ := json.Marshal(rules)
			if _, err := tx.Exec(ctx, `UPDATE crm.permission_sets SET rules = $2, updated_at = now() WHERE id = $1`, s.id, raw); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO crm.connector_state (key, value) VALUES ($1, '{"done":true}') ON CONFLICT (key) DO NOTHING`, marker)
		return err
	})
}
