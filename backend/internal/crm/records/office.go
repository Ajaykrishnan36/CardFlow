package records

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
)

// GET /w/{code}/dashboard/office (D-170): what the owner needs to know without asking
// anyone — counted from the records the flow makes, never typed in. Sections for objects
// the viewer can't read (or the business doesn't use) are left out; someone who sees only
// their own records gets their own numbers.

type officeRow struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Value float64 `json:"value"`
	Money bool    `json:"money,omitempty"`
	Alert bool    `json:"alert,omitempty"` // worth a red number
	Path  string  `json:"path,omitempty"`  // the list behind it
	Note  string  `json:"note,omitempty"`
}

type rideByModel struct {
	Model  string `json:"model"`
	Rides  int    `json:"rides"`
	Bought int    `json:"bought"`
}

type stockByModel struct {
	Model     string `json:"model"`
	InStock   int    `json:"inStock"`
	Allocated int    `json:"allocated"`
	Low       bool   `json:"low"`
}

func (h *Handler) handleOffice(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.Owner && !sc.Eff.HasCapability(access.CapDashboard) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	ctx := r.Context()
	me := actor(r)
	loc := ist
	if l, err := time.LoadLocation(h.envFor(ctx, sc.WS, me).TZ); err == nil {
		loc = l
	}
	now := time.Now().In(loc)
	today := now.Format("2006-01-02")
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	can := func(key string) bool { return specFor(key) != nil && sc.Enabled(key) && sc.Can(key, "read") }
	// where: this business, not deleted, and for "own records" only the viewer's.
	where := func(key string, args *[]any) string {
		*args = append(*args, sc.WS)
		s := "t.workspace_id = $" + strconv.Itoa(len(*args)) + " AND t.deleted_at IS NULL AND t.object_key = '" + key + "'"
		if sc.OwnOnly(key) {
			*args = append(*args, sc.OwnersFor(key, me))
			s += " AND t.owner_id = ANY($" + strconv.Itoa(len(*args)) + "::uuid[])"
		}
		return s
	}
	count := func(key, cond string, extra ...any) float64 {
		args := append([]any{}, extra...)
		w := where(key, &args)
		var n float64
		_ = h.store.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.object_records t WHERE `+w+` AND `+cond, args...).Scan(&n)
		return n
	}
	base := "/crm/w/" + sc.Code + "/"
	// The business's own word for a test ride (a car dealer's are test drives, D-174).
	rides, ride := "Test rides", "Test ride"
	if t, ok := access.WorkspaceTerms(ctx, h.store.Pool, sc.WS)["test_rides"]; ok {
		rides, ride = t.Plural, t.Singular
	}
	out := struct {
		Tiles   []officeRow    `json:"tiles"`
		Today   []officeRow    `json:"today"`
		Rides   []rideByModel  `json:"testRidesByModel"`
		Stock   []stockByModel `json:"stockByModel"`
		From    string         `json:"from"`
		Funnel  []officeRow    `json:"funnel"`
		Ratio   *float64       `json:"testRideToPurchase,omitempty"`
		Monthly string         `json:"period"`
		Ride    string         `json:"rideWord"`
	}{Ride: ride, Tiles: []officeRow{}, Today: []officeRow{}, Rides: []rideByModel{}, Stock: []stockByModel{}, Funnel: []officeRow{}, From: monthStart.Format("2006-01-02"), Monthly: now.Format("January 2006")}

	// The month at a glance.
	if can("leads") {
		args := []any{sc.WS, monthStart}
		s := ""
		if sc.OwnOnly("leads") {
			args = append(args, sc.OwnersFor("leads", me))
			s = " AND t.owner_id = ANY($3::uuid[])"
		}
		var n float64
		_ = h.store.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.leads t WHERE t.workspace_id = $1 AND t.deleted_at IS NULL AND t.created_at >= $2`+s, args...).Scan(&n)
		out.Tiles = append(out.Tiles, officeRow{Key: "newLeads", Label: "New leads this month", Value: n, Path: base + "leads"})
		out.Funnel = append(out.Funnel, officeRow{Key: "leads", Label: "Leads", Value: n})
	}
	monthDay := monthStart.Format("2006-01-02")
	if can("test_rides") {
		args := []any{monthDay}
		w := where("test_rides", &args)
		rows, err := h.store.Pool.Query(ctx, `
			SELECT COALESCE(i.name, 'Other'), count(*),
			       count(*) FILTER (WHERE EXISTS (SELECT 1 FROM crm.object_records b WHERE b.workspace_id = t.workspace_id AND b.object_key = 'vehicle_bookings'
			              AND b.deleted_at IS NULL AND COALESCE(b.status, '') <> 'cancelled' AND b.custom->>'contactId' = t.custom->>'contactId'
			              AND COALESCE(t.custom->>'contactId', '') <> ''))
			FROM crm.object_records t LEFT JOIN crm.object_records i ON i.id::text = t.custom->>'itemId'
			WHERE `+w+` AND t.status = 'completed' AND left(COALESCE(t.custom->>'scheduledAt', to_char(t.created_at, 'YYYY-MM-DD')), 10) >= $1
			GROUP BY 1 ORDER BY 2 DESC LIMIT 12`, args...)
		if err == nil {
			for rows.Next() {
				var x rideByModel
				if rows.Scan(&x.Model, &x.Rides, &x.Bought) == nil {
					out.Rides = append(out.Rides, x)
				}
			}
			rows.Close()
		}
		var ridden, bought float64
		for _, x := range out.Rides {
			ridden += float64(x.Rides)
			bought += float64(x.Bought)
		}
		out.Tiles = append(out.Tiles, officeRow{Key: "testRides", Label: rides + " done", Value: ridden, Path: base + "test_rides"},
			officeRow{Key: "boughtAfterRide", Label: "Bought after a " + strings.ToLower(ride), Value: bought})
		if ridden > 0 {
			ratio := bought * 100 / ridden
			out.Ratio = &ratio
		}
		out.Funnel = append(out.Funnel, officeRow{Key: "testRides", Label: rides, Value: ridden})
	}
	if can("quotes") {
		n := count("quotes", "t.created_at >= $1", monthStart)
		out.Funnel = append(out.Funnel, officeRow{Key: "quotes", Label: "Quotes", Value: n})
	}
	if can("invoices") {
		n := count("invoices", "t.status = 'paid' AND COALESCE(NULLIF(t.custom->>'invoiceDate', ''), to_char(t.created_at, 'YYYY-MM-DD')) >= $1", monthDay)
		out.Funnel = append(out.Funnel, officeRow{Key: "paid", Label: "Paid / invoiced", Value: n})
	}
	if can("vehicle_bookings") {
		n := count("vehicle_bookings", "t.status = 'delivered' AND left(COALESCE(t.custom->>'deliveryDate', to_char(t.updated_at, 'YYYY-MM-DD')), 10) >= $1", monthDay)
		out.Funnel = append(out.Funnel, officeRow{Key: "delivered", Label: "Delivered", Value: n})
	}
	if can("opportunities") {
		args := []any{monthDay}
		w := where("opportunities", &args)
		var v float64
		_ = h.store.Pool.QueryRow(ctx, `SELECT COALESCE(sum(`+numeric(`t.custom->>'amount'`)+`), 0)::float8 FROM crm.object_records t WHERE `+w+`
			AND t.status = 'closed_won' AND COALESCE(NULLIF(t.custom->>'closeDate', ''), to_char(t.updated_at, 'YYYY-MM-DD')) >= $1`, args...).Scan(&v)
		out.Tiles = append(out.Tiles, officeRow{Key: "revenueWon", Label: "Revenue won", Value: v, Money: true, Path: base + "opportunities"})
	}
	if can("finance_applications") {
		n := count("finance_applications", "COALESCE(t.status, '') IN ('draft', 'submitted')")
		out.Tiles = append(out.Tiles, officeRow{Key: "loansPending", Label: "Loans waiting for sanction", Value: n, Alert: n > 0, Path: base + "finance_applications"})
	}

	// Today in the office.
	if can("tasks") {
		due := count("tasks", "COALESCE(t.status, '') NOT IN ('completed', 'deferred') AND t.custom->>'dueDate' = $1", today)
		late := count("tasks", "COALESCE(t.status, '') NOT IN ('completed', 'deferred') AND NULLIF(t.custom->>'dueDate', '') < $1", today)
		out.Today = append(out.Today, officeRow{Key: "followUps", Label: "Follow-ups due today", Value: due, Path: base + "tasks"},
			officeRow{Key: "overdue", Label: "Follow-ups overdue", Value: late, Alert: late > 0, Path: base + "tasks"})
	}
	if can("test_rides") {
		n := count("test_rides", "COALESCE(t.status, '') = 'scheduled' AND left(t.custom->>'scheduledAt', 10) = $1", today)
		out.Today = append(out.Today, officeRow{Key: "ridesToday", Label: rides + " booked today", Value: n, Path: base + "test_rides"})
	}
	if can("quotes") {
		n := count("quotes", "t.custom->>'approvalStatus' = 'pending'")
		out.Today = append(out.Today, officeRow{Key: "approvals", Label: "Quotes waiting for discount approval", Value: n, Alert: n > 0, Path: base + "approvals"})
	}
	if can("payments") {
		args := []any{today}
		w := where("payments", &args)
		var v float64
		_ = h.store.Pool.QueryRow(ctx, `SELECT COALESCE(sum(`+numeric(`t.custom->>'amount'`)+`), 0)::float8 FROM crm.object_records t WHERE `+w+`
			AND t.status IN `+paymentCounts+` AND left(COALESCE(t.custom->>'paymentDate', ''), 10) = $1`, args...).Scan(&v)
		out.Today = append(out.Today, officeRow{Key: "paidToday", Label: "Payments received today", Value: v, Money: true, Path: base + "payments"})
	}
	if can("quotes") {
		n := count("quotes", "t.status = 'accepted' AND "+numeric(`t.custom->>'balanceDue'`)+" > 0")
		out.Today = append(out.Today, officeRow{Key: "balances", Label: "Quotes part paid (balance to collect)", Value: n, Path: base + "quotes"})
	}
	if can("vehicle_bookings") {
		n := count("vehicle_bookings", "COALESCE(t.status, '') IN ('allocated', 'ready')")
		out.Today = append(out.Today, officeRow{Key: "deliveries", Label: "Deliveries to make", Value: n, Path: base + "vehicle_bookings"})
	}
	{
		var n float64
		_ = h.store.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.files f WHERE f.workspace_id = $1 AND f.deleted_at IS NULL AND f.doc_type IS NOT NULL AND f.verified_at IS NULL`, sc.WS).Scan(&n)
		out.Today = append(out.Today, officeRow{Key: "docs", Label: "Documents to verify", Value: n, Alert: n > 0})
	}
	if can("cases") {
		n := count("cases", "t.custom->>'category' = 'service' AND COALESCE(t.status, '') NOT IN ('resolved', 'closed')")
		late := count("cases", "COALESCE(t.status, '') NOT IN ('resolved', 'closed') AND (t.custom->>'slaBreached')::text = 'true'")
		out.Today = append(out.Today, officeRow{Key: "serviceTickets", Label: "Service tickets open", Value: n, Path: base + "cases"},
			officeRow{Key: "slaBreached", Label: "Tickets past their SLA", Value: late, Alert: late > 0, Path: base + "cases"})
	}
	if can("service_dues") {
		n := count("service_dues", "COALESCE(t.status, '') IN ('planned', 'due', 'booked') AND t.custom->>'dueDate' >= $1 AND t.custom->>'dueDate' <= $2", today, now.AddDate(0, 0, 7).Format("2006-01-02"))
		out.Today = append(out.Today, officeRow{Key: "servicesDue", Label: "Services due this week", Value: n, Path: base + "service_dues"})
	}
	if can("vehicle_stock") {
		args := []any{}
		w := where("vehicle_stock", &args)
		rows, err := h.store.Pool.Query(ctx, `SELECT COALESCE(i.name, 'Other'), count(*) FILTER (WHERE t.status = 'in_stock'), count(*) FILTER (WHERE t.status = 'allocated')
			FROM crm.object_records t LEFT JOIN crm.object_records i ON i.id::text = t.custom->>'itemId'
			WHERE `+w+` AND COALESCE(t.status, '') IN ('in_stock', 'allocated', 'in_transit', 'demo', 'on_hold')
			GROUP BY 1 ORDER BY 2, 1 LIMIT 20`, args...)
		if err == nil {
			for rows.Next() {
				var x stockByModel
				if rows.Scan(&x.Model, &x.InStock, &x.Allocated) == nil {
					x.Low = x.InStock <= 2
					out.Stock = append(out.Stock, x)
				}
			}
			rows.Close()
		}
	}
	h.officeByTrade(ctx, can, count, where, base, today, monthStart, now, &out.Tiles, &out.Today)
	if can("opportunities") {
		// Sales by person this month.
		args := []any{monthDay}
		w := where("opportunities", &args)
		rows, err := h.store.Pool.Query(ctx, `SELECT COALESCE(i.display_name, 'Nobody'), count(*) FROM crm.object_records t LEFT JOIN crm.identities i ON i.id = t.owner_id
			WHERE `+w+` AND t.status = 'closed_won' AND COALESCE(NULLIF(t.custom->>'closeDate', ''), to_char(t.updated_at, 'YYYY-MM-DD')) >= $1
			GROUP BY 1 ORDER BY 2 DESC LIMIT 6`, args...)
		if err == nil {
			note := ""
			for rows.Next() {
				var who string
				var n int
				if rows.Scan(&who, &n) == nil {
					if note != "" {
						note += " · "
					}
					note += who + " " + strconv.Itoa(n)
				}
			}
			rows.Close()
			if note != "" {
				out.Today = append(out.Today, officeRow{Key: "byPerson", Label: "Sales by person this month", Note: note})
			}
		}
	}
	shared.WriteJSON(w, http.StatusOK, out)
}

// officeByTrade adds the numbers of the industry packs a business uses (D-174): an
// insurer's premiums and renewals, a law firm's matters and hearings, a developer's units.
// Each is counted only where the pack is on and the viewer can read it.
func (h *Handler) officeByTrade(ctx context.Context, can func(string) bool, count func(string, string, ...any) float64, where func(string, *[]any) string,
	base, today string, monthStart, now time.Time, tiles, todayRows *[]officeRow) {
	monthDay := monthStart.Format("2006-01-02")
	week := now.AddDate(0, 0, 7).Format("2006-01-02")
	sum := func(key, field, cond string, extra ...any) float64 {
		args := append([]any{}, extra...)
		w := where(key, &args)
		var v float64
		_ = h.store.Pool.QueryRow(ctx, `SELECT COALESCE(sum(`+numeric(`t.custom->>'`+field+`'`)+`), 0)::float8 FROM crm.object_records t WHERE `+w+` AND `+cond, args...).Scan(&v)
		return v
	}
	// Insurance.
	if can("insurance_policies") {
		sold := count("insurance_policies", "t.status IN ('in_force', 'renewed') AND COALESCE(NULLIF(t.custom->>'startDate', ''), to_char(t.created_at, 'YYYY-MM-DD')) >= $1", monthDay)
		proposals := count("insurance_policies", "t.status = 'proposal'")
		renewals := count("insurance_policies", "t.status = 'in_force' AND t.custom->>'endDate' >= $1 AND t.custom->>'endDate' <= $2", today, now.AddDate(0, 0, 30).Format("2006-01-02"))
		*tiles = append(*tiles, officeRow{Key: "policiesSold", Label: "Policies sold this month", Value: sold, Path: base + "insurance_policies"},
			officeRow{Key: "pendingApplications", Label: "Proposals waiting", Value: proposals, Path: base + "insurance_policies"})
		*todayRows = append(*todayRows, officeRow{Key: "renewals", Label: "Renewals in the next 30 days", Value: renewals, Alert: renewals > 0, Path: base + "insurance_policies"})
	}
	if can("premium_dues") {
		got := sum("premium_dues", "paidAmount", "t.status = 'paid' AND t.custom->>'paidOn' >= $1", monthDay)
		late := count("premium_dues", "t.status IN ('due', 'overdue') AND t.custom->>'dueDate' < $1", today)
		*tiles = append(*tiles, officeRow{Key: "premiumCollected", Label: "Premium collected this month", Value: got, Money: true, Path: base + "premium_dues"})
		*todayRows = append(*todayRows, officeRow{Key: "premiumsLate", Label: "Premiums past their due date", Value: late, Alert: late > 0, Path: base + "premium_dues"})
	}
	if can("insurance_claims") {
		open := count("insurance_claims", "COALESCE(t.status, '') NOT IN ('settled', 'rejected', 'closed', 'withdrawn')")
		*todayRows = append(*todayRows, officeRow{Key: "claims", Label: "Claims open", Value: open, Path: base + "insurance_claims"})
	}
	// Legal.
	if can("legal_matters") {
		active := count("legal_matters", "COALESCE(t.status, '') IN ('new', 'active', 'on_hold')")
		opened := count("legal_matters", "t.created_at >= $1", monthStart)
		done := count("legal_matters", "COALESCE(t.status, '') IN ('won', 'lost', 'settled', 'withdrawn', 'closed') AND t.custom->>'closedOn' >= $1", monthDay)
		*tiles = append(*tiles, officeRow{Key: "activeMatters", Label: "Active matters", Value: active, Path: base + "legal_matters"},
			officeRow{Key: "newMatters", Label: "New matters this month", Value: opened, Path: base + "legal_matters"},
			officeRow{Key: "completedMatters", Label: "Matters completed this month", Value: done, Path: base + "legal_matters"})
	}
	if can("court_hearings") {
		day := count("court_hearings", "t.status = 'scheduled' AND left(t.custom->>'hearingAt', 10) = $1", today)
		next := count("court_hearings", "t.status = 'scheduled' AND left(t.custom->>'hearingAt', 10) > $1 AND left(t.custom->>'hearingAt', 10) <= $2", today, week)
		*todayRows = append(*todayRows, officeRow{Key: "hearingsToday", Label: "Hearings today", Value: day, Alert: day > 0, Path: base + "court_hearings"},
			officeRow{Key: "hearingsWeek", Label: "Hearings in the next 7 days", Value: next, Path: base + "court_hearings"})
	}
	if can("appointments") && can("legal_matters") {
		n := count("appointments", "left(COALESCE(t.custom->>'startsAt', ''), 10) >= $1 AND left(COALESCE(t.custom->>'startsAt', ''), 10) <= $2", today, week)
		*todayRows = append(*todayRows, officeRow{Key: "consultations", Label: "Consultations in the next 7 days", Value: n, Path: base + "appointments"})
	}
	// Real estate.
	if can("property_units") {
		free := count("property_units", "t.status = 'available'")
		*tiles = append(*tiles, officeRow{Key: "unitsAvailable", Label: "Units available", Value: free, Path: base + "property_units"})
	}
	if can("property_visits") {
		n := count("property_visits", "t.status = 'completed' AND left(t.custom->>'scheduledAt', 10) >= $1", monthDay)
		up := count("property_visits", "t.status = 'scheduled' AND left(t.custom->>'scheduledAt', 10) >= $1 AND left(t.custom->>'scheduledAt', 10) <= $2", today, week)
		*tiles = append(*tiles, officeRow{Key: "siteVisits", Label: "Site visits this month", Value: n, Path: base + "property_visits"})
		*todayRows = append(*todayRows, officeRow{Key: "visitsWeek", Label: "Site visits in the next 7 days", Value: up, Path: base + "property_visits"})
	}
	if can("property_bookings") {
		n := count("property_bookings", "t.status IN ('booked', 'agreement', 'completed') AND t.created_at >= $1", monthStart)
		*tiles = append(*tiles, officeRow{Key: "unitBookings", Label: "Bookings this month", Value: n, Path: base + "property_bookings"})
	}
	// Every business that sells: orders, and invoices still to be paid.
	if can("sales_orders") {
		n := count("sales_orders", "t.created_at >= $1", monthStart)
		*tiles = append(*tiles, officeRow{Key: "orders", Label: "Orders this month", Value: n, Path: base + "sales_orders"})
	}
	if can("invoices") {
		n := count("invoices", "COALESCE(t.status, '') IN ('sent', 'partially_paid', 'overdue')")
		*todayRows = append(*todayRows, officeRow{Key: "unpaidInvoices", Label: "Invoices waiting for payment", Value: n, Alert: n > 0, Path: base + "invoices"})
	}
}
