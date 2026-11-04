package records

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
)

// GET /w/{code}/dashboard/overview?range=month|quarter|year (D-176): the owner's whole
// business on one screen — eight numbers against the period before (each with its own
// small trend), the pipeline as a funnel by stage, win rate, the sales cycle, revenue over
// time, top products, the sales leaderboard, deals at risk, what is coming up and what just
// happened. Counted from the records the flow makes; nothing is typed in. A section the
// viewer can't read is left out, and someone who sees only their own records gets their own
// numbers.

type kpiTrend struct {
	Key    string    `json:"key"`
	Label  string    `json:"label"`
	Value  float64   `json:"value"`
	Before *float64  `json:"before,omitempty"` // the same number for the period before
	Money  bool      `json:"money,omitempty"`
	Pct    bool      `json:"percent,omitempty"`
	Sub    string    `json:"sub,omitempty"`
	Trend  []float64 `json:"trend"`
	Path   string    `json:"path,omitempty"`
	Invert bool      `json:"invert,omitempty"` // lower is better (pending payments)
}

type stageRow struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Count float64 `json:"count"`
	Value float64 `json:"value"`
	Tone  string  `json:"tone,omitempty"`
}

type nameValue struct {
	Name   string  `json:"name"`
	Value  float64 `json:"value"`
	Count  float64 `json:"count"`
	ID     string  `json:"id,omitempty"`
	Avatar string  `json:"initials,omitempty"`
}

type riskRow struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Amount float64 `json:"amount"`
	Reason string  `json:"reason"`
}

type comingRow struct {
	Object string `json:"object"`
	ID     string `json:"id"`
	Title  string `json:"title"`
	When   string `json:"when"`
	Who    string `json:"who,omitempty"`
}

type activityRow struct {
	Kind     string    `json:"kind"`
	Category string    `json:"category"`
	Object   string    `json:"object"`
	ID       string    `json:"id"`
	Title    string    `json:"title"`
	Actor    string    `json:"actor"`
	At       time.Time `json:"at"`
}

// activityCategory: what an entry on the feed is about, for its filter.
var activityCategory = map[string]string{
	"leads": "leads", "opportunities": "deals", "quotes": "quotes", "sales_orders": "orders", "invoices": "invoices",
	"payments": "payments", "tasks": "follow-ups", "appointments": "meetings", "events": "meetings", "call_logs": "calls",
	"communications": "calls", "contacts": "customers", "accounts": "customers", "cases": "service",
}

func (h *Handler) handleOverview(w http.ResponseWriter, r *http.Request) {
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
	// The period and the one before it, each cut into eight buckets for the small trends.
	var from time.Time
	rng := r.URL.Query().Get("range")
	switch rng {
	case "quarter":
		q := (int(now.Month()) - 1) / 3
		from = time.Date(now.Year(), time.Month(q*3+1), 1, 0, 0, 0, 0, loc)
	case "year":
		from = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc)
	default:
		rng = "month"
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	}
	var prevFrom time.Time
	switch rng {
	case "quarter":
		prevFrom = from.AddDate(0, -3, 0)
	case "year":
		prevFrom = from.AddDate(-1, 0, 0)
	default:
		prevFrom = from.AddDate(0, -1, 0)
	}
	// The period runs to the end of today; the one before is compared up to the same point
	// (day 12 of the month against day 12 of last month).
	end := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, loc)
	prevTo := prevFrom.Add(end.Sub(from))
	// The small trends look back over equal steps ending today: 8 weeks, 8 fortnights or 12 months.
	buckets, step := 8, 7*24*time.Hour
	switch rng {
	case "quarter":
		step = 14 * 24 * time.Hour
	case "year":
		buckets = 12
	}
	edges := make([]time.Time, buckets+1)
	for i := range edges {
		if rng == "year" {
			edges[i] = time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, loc).AddDate(0, i-buckets, 0)
		} else {
			edges[i] = end.Add(-step * time.Duration(buckets-i))
		}
	}

	can := func(key string) bool { return specFor(key) != nil && sc.Enabled(key) && sc.Can(key, "read") }
	base := "/crm/w/" + sc.Code + "/"
	// own: the "only my records" condition for a table aliased t, appended to args.
	own := func(key string, args *[]any) string {
		if !sc.OwnOnly(key) {
			return ""
		}
		*args = append(*args, sc.OwnersFor(key, me))
		return " AND t.owner_id = ANY($" + strconv.Itoa(len(*args)) + "::uuid[])"
	}
	rec := func(key string, args *[]any) string {
		*args = append(*args, sc.WS)
		return "t.workspace_id = $" + strconv.Itoa(len(*args)) + " AND t.deleted_at IS NULL AND t.object_key = '" + key + "'" + own(key, args)
	}
	core := func(table, key string, args *[]any) string {
		*args = append(*args, sc.WS)
		return "crm." + table + " t WHERE t.workspace_id = $" + strconv.Itoa(len(*args)) + " AND t.deleted_at IS NULL" + own(key, args)
	}
	one := func(sql string, args ...any) float64 {
		var v float64
		_ = h.store.Pool.QueryRow(ctx, sql, args...).Scan(&v)
		return v
	}
	// series runs a sum/count over [a, b) for each bucket and the two periods.
	type measure func(a, b time.Time) float64
	trend := func(m measure) (cur, prev float64, tr []float64) {
		tr = make([]float64, buckets)
		for i := 0; i < buckets; i++ {
			tr[i] = m(edges[i], edges[i+1])
		}
		return m(from, end), m(prevFrom, prevTo), tr
	}
	day := func(t time.Time) string { return t.Format("2006-01-02") }
	amount := numeric(`t.custom->>'amount'`)

	out := struct {
		Range      string        `json:"range"`
		From       string        `json:"from"`
		Period     string        `json:"period"`
		Currency   string        `json:"currency"`
		KPIs       []kpiTrend    `json:"kpis"`
		Funnel     []stageRow    `json:"funnel"`
		Lost       *stageRow     `json:"lost,omitempty"`
		Open       *nameValue    `json:"openPipeline,omitempty"`
		WinRate    *float64      `json:"winRate,omitempty"`
		WinBefore  *float64      `json:"winRateBefore,omitempty"`
		WinTrend   []float64     `json:"winRateTrend,omitempty"`
		Cycle      *float64      `json:"avgCycleDays,omitempty"`
		CycleBef   *float64      `json:"avgCycleDaysBefore,omitempty"`
		Revenue    []float64     `json:"revenueTrend"`
		Won        []float64     `json:"wonTrend"`
		Labels     []string      `json:"trendLabels"`
		Products   []nameValue   `json:"topProducts"`
		Leaders    []nameValue   `json:"leaderboard"`
		Risk       []riskRow     `json:"atRisk"`
		Coming     []comingRow   `json:"upcoming"`
		Activity   []activityRow `json:"activity"`
		Expected   float64       `json:"expectedRevenue"`
		WonValue   float64       `json:"wonValue"`
		Pending    float64       `json:"pendingPayments"`
		RevenueSum float64       `json:"revenue"`
	}{Range: rng, From: day(from), KPIs: []kpiTrend{}, Funnel: []stageRow{}, Revenue: []float64{}, Won: []float64{}, Labels: []string{},
		Products: []nameValue{}, Leaders: []nameValue{}, Risk: []riskRow{}, Coming: []comingRow{}, Activity: []activityRow{}}
	_ = h.store.Pool.QueryRow(ctx, `SELECT COALESCE(currency, 'INR') FROM crm.workspaces WHERE id = $1`, sc.WS).Scan(&out.Currency)
	switch rng {
	case "quarter":
		out.Period = "Q" + strconv.Itoa((int(now.Month())-1)/3+1) + " " + strconv.Itoa(now.Year())
	case "year":
		out.Period = strconv.Itoa(now.Year())
	default:
		out.Period = now.Format("January 2006")
	}
	for i := 0; i < buckets; i++ {
		if rng == "year" {
			out.Labels = append(out.Labels, edges[i].Format("Jan"))
		} else {
			out.Labels = append(out.Labels, edges[i+1].AddDate(0, 0, -1).Format("2 Jan"))
		}
	}
	add := func(k kpiTrend, cur, prev float64, tr []float64) {
		k.Value, k.Trend = cur, tr
		p := prev
		k.Before = &p
		out.KPIs = append(out.KPIs, k)
	}

	// 1. Leads created.
	if can("leads") {
		cur, prev, tr := trend(func(a, b time.Time) float64 {
			args := []any{}
			w := core("leads", "leads", &args)
			args = append(args, a, b)
			return one(`SELECT count(*) FROM `+w+` AND t.created_at >= $`+strconv.Itoa(len(args)-1)+` AND t.created_at < $`+strconv.Itoa(len(args)), args...)
		})
		add(kpiTrend{Key: "leads", Label: "Total leads", Sub: "new this period", Path: base + "leads"}, cur, prev, tr)
	}
	wonIn := func(a, b time.Time, what string) float64 {
		args := []any{}
		w := rec("opportunities", &args)
		args = append(args, day(a), day(b))
		return one(`SELECT `+what+` FROM crm.object_records t WHERE `+w+` AND t.status = 'closed_won'
			AND COALESCE(NULLIF(t.custom->>'closeDate', ''), to_char(t.updated_at, 'YYYY-MM-DD')) >= $`+strconv.Itoa(len(args)-1)+`
			AND COALESCE(NULLIF(t.custom->>'closeDate', ''), to_char(t.updated_at, 'YYYY-MM-DD')) < $`+strconv.Itoa(len(args)), args...)
	}
	lostIn := func(a, b time.Time) float64 {
		args := []any{}
		w := rec("opportunities", &args)
		args = append(args, day(a), day(b))
		return one(`SELECT count(*) FROM crm.object_records t WHERE `+w+` AND t.status = 'closed_lost'
			AND COALESCE(NULLIF(t.custom->>'closeDate', ''), to_char(t.updated_at, 'YYYY-MM-DD')) >= $`+strconv.Itoa(len(args)-1)+`
			AND COALESCE(NULLIF(t.custom->>'closeDate', ''), to_char(t.updated_at, 'YYYY-MM-DD')) < $`+strconv.Itoa(len(args)), args...)
	}
	if can("opportunities") {
		// 2. Open deals (now), against deals opened in each period.
		args := []any{}
		w := rec("opportunities", &args)
		var n, v float64
		_ = h.store.Pool.QueryRow(ctx, `SELECT count(*), COALESCE(sum(`+amount+`), 0)::float8 FROM crm.object_records t WHERE `+w+`
			AND COALESCE(t.status, '') NOT IN ('closed_won', 'closed_lost')`, args...).Scan(&n, &v)
		_, _, tr := trend(func(a, b time.Time) float64 {
			args := []any{}
			w := rec("opportunities", &args)
			args = append(args, a, b)
			return one(`SELECT count(*) FROM crm.object_records t WHERE `+w+` AND t.created_at >= $`+strconv.Itoa(len(args)-1)+` AND t.created_at < $`+strconv.Itoa(len(args)), args...)
		})
		k := kpiTrend{Key: "openOpportunities", Label: "Open opportunities", Sub: shortRupees(v) + " in the pipeline", Path: base + "opportunities", Trend: tr, Value: n}
		// Open at the same point of the period before: opened by then and not yet closed then.
		args = []any{}
		w = rec("opportunities", &args)
		args = append(args, prevTo, day(prevTo))
		before := one(`SELECT count(*) FROM crm.object_records t WHERE `+w+` AND t.created_at < $`+strconv.Itoa(len(args)-1)+`
			AND (COALESCE(t.status, '') NOT IN ('closed_won', 'closed_lost') OR COALESCE(NULLIF(t.custom->>'closeDate', ''), to_char(t.updated_at, 'YYYY-MM-DD')) >= $`+strconv.Itoa(len(args))+`)`, args...)
		k.Before = &before
		out.KPIs = append(out.KPIs, k)
		out.Open = &nameValue{Name: "Open pipeline", Value: v, Count: n}
		// 3. Won.
		cur, prev, wt := trend(func(a, b time.Time) float64 { return wonIn(a, b, "count(*)") })
		out.WonValue = wonIn(from, end, "COALESCE(sum("+amount+"), 0)::float8")
		add(kpiTrend{Key: "won", Label: "Won opportunities", Sub: shortRupees(out.WonValue) + " won", Path: base + "opportunities"}, cur, prev, wt)
		// Win rate and sales cycle.
		wonN, lostN := cur, lostIn(from, end)
		if wonN+lostN > 0 {
			wr := wonN * 100 / (wonN + lostN)
			out.WinRate = &wr
		}
		pw, pl := prev, lostIn(prevFrom, prevTo)
		if pw+pl > 0 {
			wr := pw * 100 / (pw + pl)
			out.WinBefore = &wr
		}
		for i := 0; i < buckets; i++ {
			ww, ll := wonIn(edges[i], edges[i+1], "count(*)"), lostIn(edges[i], edges[i+1])
			if ww+ll > 0 {
				out.WinTrend = append(out.WinTrend, ww*100/(ww+ll))
			} else {
				out.WinTrend = append(out.WinTrend, 0)
			}
		}
		cycle := func(a, b time.Time) *float64 {
			args := []any{}
			w := rec("opportunities", &args)
			args = append(args, day(a), day(b))
			var v *float64
			_ = h.store.Pool.QueryRow(ctx, `SELECT avg(GREATEST(0, COALESCE(NULLIF(t.custom->>'closeDate', ''), to_char(t.updated_at, 'YYYY-MM-DD'))::date - t.created_at::date))::float8
				FROM crm.object_records t WHERE `+w+` AND t.status = 'closed_won'
				AND COALESCE(NULLIF(t.custom->>'closeDate', ''), to_char(t.updated_at, 'YYYY-MM-DD')) >= $`+strconv.Itoa(len(args)-1)+`
				AND COALESCE(NULLIF(t.custom->>'closeDate', ''), to_char(t.updated_at, 'YYYY-MM-DD')) < $`+strconv.Itoa(len(args)), args...).Scan(&v)
			return v
		}
		out.Cycle, out.CycleBef = cycle(from, end), cycle(prevFrom, prevTo)
		// The funnel: leads of the period, then every open stage, then won in the period.
		spec := specFor("opportunities")
		stages := statusesFor(ctx, h.store.Pool, sc.WS, spec)
		if len(out.KPIs) > 0 && out.KPIs[0].Key == "leads" {
			out.Funnel = append(out.Funnel, stageRow{Key: "leads", Label: "New leads", Count: out.KPIs[0].Value})
		}
		counts := map[string][2]float64{}
		args = []any{}
		w = rec("opportunities", &args)
		rows, err := h.store.Pool.Query(ctx, `SELECT COALESCE(t.status, ''), count(*), COALESCE(sum(`+amount+`), 0)::float8 FROM crm.object_records t WHERE `+w+` GROUP BY 1`, args...)
		if err == nil {
			for rows.Next() {
				var s string
				var c, v float64
				if rows.Scan(&s, &c, &v) == nil {
					counts[s] = [2]float64{c, v}
				}
			}
			rows.Close()
		}
		for _, s := range stages {
			if s.Value == "closed_won" || s.Value == "closed_lost" {
				continue
			}
			c := counts[s.Value]
			out.Funnel = append(out.Funnel, stageRow{Key: s.Value, Label: s.Label, Count: c[0], Value: c[1], Tone: s.Tone})
			out.Expected += c[1]
		}
		out.Funnel = append(out.Funnel, stageRow{Key: "closed_won", Label: "Won", Count: cur, Value: out.WonValue, Tone: "success"})
		out.Lost = &stageRow{Key: "closed_lost", Label: "Lost", Count: lostN, Tone: "danger"}
		// The sales leaderboard: won this period, by owner.
		args = []any{}
		w = rec("opportunities", &args)
		args = append(args, day(from), day(end))
		lrows, err := h.store.Pool.Query(ctx, `SELECT COALESCE(t.owner_id::text, ''), COALESCE(i.display_name, 'Nobody'), count(*), COALESCE(sum(`+amount+`), 0)::float8
			FROM crm.object_records t LEFT JOIN crm.identities i ON i.id = t.owner_id
			WHERE `+w+` AND t.status = 'closed_won' AND COALESCE(NULLIF(t.custom->>'closeDate', ''), to_char(t.updated_at, 'YYYY-MM-DD')) >= $`+strconv.Itoa(len(args)-1)+`
			  AND COALESCE(NULLIF(t.custom->>'closeDate', ''), to_char(t.updated_at, 'YYYY-MM-DD')) < $`+strconv.Itoa(len(args))+`
			GROUP BY 1, 2 ORDER BY 4 DESC LIMIT 6`, args...)
		if err == nil {
			for lrows.Next() {
				var x nameValue
				if lrows.Scan(&x.ID, &x.Name, &x.Count, &x.Value) == nil {
					x.Avatar = initialsOf(x.Name)
					out.Leaders = append(out.Leaders, x)
				}
			}
			lrows.Close()
		}
		// Deals at risk: the close date has passed, or nothing has happened for 14 days.
		args = []any{}
		w = rec("opportunities", &args)
		args = append(args, day(now))
		rrows, err := h.store.Pool.Query(ctx, `SELECT t.id::text, t.name, (`+amount+`)::float8,
			CASE WHEN NULLIF(t.custom->>'closeDate', '') < $`+strconv.Itoa(len(args))+` THEN 'Close date passed ' || to_char(NULLIF(t.custom->>'closeDate', '')::date, 'DD Mon')
			     ELSE 'No update in ' || (current_date - t.updated_at::date) || ' days' END
			FROM crm.object_records t WHERE `+w+` AND COALESCE(t.status, '') NOT IN ('closed_won', 'closed_lost')
			  AND (NULLIF(t.custom->>'closeDate', '') < $`+strconv.Itoa(len(args))+` OR t.updated_at < now() - interval '14 days')
			ORDER BY 3 DESC LIMIT 6`, args...)
		if err == nil {
			for rrows.Next() {
				var x riskRow
				if rrows.Scan(&x.ID, &x.Name, &x.Amount, &x.Reason) == nil {
					out.Risk = append(out.Risk, x)
				}
			}
			rrows.Close()
		}
	}
	// 4. Revenue: money received.
	if can("payments") {
		cur, prev, tr := trend(func(a, b time.Time) float64 {
			args := []any{}
			w := rec("payments", &args)
			args = append(args, day(a), day(b))
			return one(`SELECT COALESCE(sum(`+amount+`), 0)::float8 FROM crm.object_records t WHERE `+w+` AND t.status IN `+paymentCounts+`
				AND left(COALESCE(NULLIF(t.custom->>'paymentDate', ''), to_char(t.created_at, 'YYYY-MM-DD')), 10) >= $`+strconv.Itoa(len(args)-1)+`
				AND left(COALESCE(NULLIF(t.custom->>'paymentDate', ''), to_char(t.created_at, 'YYYY-MM-DD')), 10) < $`+strconv.Itoa(len(args)), args...)
		})
		out.RevenueSum, out.Revenue = cur, tr
		add(kpiTrend{Key: "revenue", Label: "Total revenue", Money: true, Sub: "money received", Path: base + "payments"}, cur, prev, tr)
	}
	if can("opportunities") {
		for i := 0; i < buckets; i++ {
			out.Won = append(out.Won, wonIn(edges[i], edges[i+1], "COALESCE(sum("+amount+"), 0)::float8"))
		}
	}
	// 5. New customers: contacts added.
	if can("contacts") {
		cur, prev, tr := trend(func(a, b time.Time) float64 {
			args := []any{}
			w := core("contacts", "contacts", &args)
			args = append(args, a, b)
			return one(`SELECT count(*) FROM `+w+` AND t.created_at >= $`+strconv.Itoa(len(args)-1)+` AND t.created_at < $`+strconv.Itoa(len(args)), args...)
		})
		add(kpiTrend{Key: "newCustomers", Label: "New customers", Sub: "contacts added", Path: base + "contacts"}, cur, prev, tr)
	}
	// 6. Orders.
	if can("sales_orders") {
		cur, prev, tr := trend(func(a, b time.Time) float64 {
			args := []any{}
			w := rec("sales_orders", &args)
			args = append(args, a, b)
			return one(`SELECT count(*) FROM crm.object_records t WHERE `+w+` AND t.created_at >= $`+strconv.Itoa(len(args)-1)+` AND t.created_at < $`+strconv.Itoa(len(args)), args...)
		})
		add(kpiTrend{Key: "orders", Label: "Total orders", Path: base + "sales_orders"}, cur, prev, tr)
	}
	// 7. Pending payments: what invoices still wait for (now); the trend is invoices raised.
	if can("invoices") {
		args := []any{}
		w := rec("invoices", &args)
		var pend, n float64
		_ = h.store.Pool.QueryRow(ctx, `SELECT COALESCE(sum(GREATEST(0, CASE WHEN COALESCE(t.custom->>'balanceDue', '') <> '' THEN `+numeric(`t.custom->>'balanceDue'`)+` ELSE `+numeric(`t.custom->>'total'`)+` END)), 0)::float8, count(*)
			FROM crm.object_records t WHERE `+w+` AND COALESCE(t.status, '') IN ('sent', 'partially_paid', 'overdue')`, args...).Scan(&pend, &n)
		out.Pending = pend
		_, _, tr := trend(func(a, b time.Time) float64 {
			args := []any{}
			w := rec("invoices", &args)
			args = append(args, a, b)
			return one(`SELECT count(*) FROM crm.object_records t WHERE `+w+` AND COALESCE(t.status, '') IN ('sent', 'partially_paid', 'overdue')
				AND t.created_at >= $`+strconv.Itoa(len(args)-1)+` AND t.created_at < $`+strconv.Itoa(len(args)), args...)
		})
		out.KPIs = append(out.KPIs, kpiTrend{Key: "pendingPayments", Label: "Pending payments", Value: pend, Money: true, Invert: true,
			Sub: strconv.Itoa(int(n)) + " invoices waiting", Trend: tr, Path: base + "invoices"})
		// Top products: what sold on invoices this period.
		args = []any{}
		w = rec("line_items", &args)
		args = append(args, from)
		prows, err := h.store.Pool.Query(ctx, `SELECT COALESCE(i.name, t.name, 'Other'), COALESCE(sum(`+numeric(`t.custom->>'total'`)+`), 0)::float8, COALESCE(sum(`+numeric(`t.custom->>'quantity'`)+`), 0)::float8
			FROM crm.object_records t JOIN crm.object_records inv ON inv.id::text = t.custom->>'invoiceId' AND inv.deleted_at IS NULL
			LEFT JOIN crm.object_records i ON i.id::text = t.custom->>'itemId'
			WHERE `+w+` AND COALESCE(inv.status, '') IN ('paid', 'partially_paid', 'sent', 'overdue') AND inv.created_at >= $`+strconv.Itoa(len(args))+`
			GROUP BY 1 ORDER BY 2 DESC LIMIT 6`, args...)
		if err == nil {
			for prows.Next() {
				var x nameValue
				if prows.Scan(&x.Name, &x.Value, &x.Count) == nil {
					out.Products = append(out.Products, x)
				}
			}
			prows.Close()
		}
	}
	// 8. Conversion rate: leads converted out of leads created.
	if can("leads") {
		conv := func(a, b time.Time) (float64, float64) {
			args := []any{}
			w := core("leads", "leads", &args)
			args = append(args, a, b)
			var made, converted float64
			_ = h.store.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE t.created_at >= $`+strconv.Itoa(len(args)-1)+` AND t.created_at < $`+strconv.Itoa(len(args))+`),
				count(*) FILTER (WHERE t.converted_at >= $`+strconv.Itoa(len(args)-1)+` AND t.converted_at < $`+strconv.Itoa(len(args))+`) FROM `+w, args...).Scan(&made, &converted)
			return made, converted
		}
		rate := func(a, b time.Time) float64 {
			m, c := conv(a, b)
			if m == 0 {
				return 0
			}
			return c * 100 / m
		}
		cur, prev, tr := trend(rate)
		add(kpiTrend{Key: "conversion", Label: "Conversion rate", Pct: true, Sub: "leads converted", Path: base + "leads"}, cur, prev, tr)
	}
	// Coming up in the next seven days: my tasks and appointments.
	week := now.AddDate(0, 0, 7)
	if can("tasks") {
		args := []any{}
		w := rec("tasks", &args)
		args = append(args, day(now), day(week))
		rows, err := h.store.Pool.Query(ctx, `SELECT t.id::text, t.name, COALESCE(t.custom->>'dueDate', ''), COALESCE(i.display_name, '')
			FROM crm.object_records t LEFT JOIN crm.identities i ON i.id = t.owner_id
			WHERE `+w+` AND COALESCE(t.status, '') NOT IN ('completed', 'deferred') AND t.custom->>'dueDate' >= $`+strconv.Itoa(len(args)-1)+`
			  AND t.custom->>'dueDate' <= $`+strconv.Itoa(len(args))+`
			ORDER BY t.custom->>'dueDate' LIMIT 6`, args...)
		if err == nil {
			for rows.Next() {
				x := comingRow{Object: "tasks"}
				if rows.Scan(&x.ID, &x.Title, &x.When, &x.Who) == nil {
					out.Coming = append(out.Coming, x)
				}
			}
			rows.Close()
		}
	}
	if can("appointments") {
		args := []any{}
		w := rec("appointments", &args)
		args = append(args, now.UTC().Format(time.RFC3339), week.UTC().Format(time.RFC3339))
		rows, err := h.store.Pool.Query(ctx, `SELECT t.id::text, t.name, COALESCE(t.custom->>'startsAt', ''), COALESCE(i.display_name, '')
			FROM crm.object_records t LEFT JOIN crm.identities i ON i.id = t.owner_id
			WHERE `+w+` AND NULLIF(t.custom->>'startsAt', '')::timestamptz >= $`+strconv.Itoa(len(args)-1)+`::timestamptz
			  AND NULLIF(t.custom->>'startsAt', '')::timestamptz <= $`+strconv.Itoa(len(args))+`::timestamptz
			ORDER BY 3 LIMIT 6`, args...)
		if err == nil {
			for rows.Next() {
				x := comingRow{Object: "appointments"}
				if rows.Scan(&x.ID, &x.Title, &x.When, &x.Who) == nil {
					out.Coming = append(out.Coming, x)
				}
			}
			rows.Close()
		}
	}
	out.Activity = h.recentActivity(ctx, sc, me, 30)
	shared.WriteJSON(w, http.StatusOK, out)
}

// recentActivity is what just happened in the business, newest first: only objects the
// viewer can read, and on objects they see only their own of, only what they did.
func (h *Handler) recentActivity(ctx context.Context, sc *Scope, me uuid.UUID, limit int) []activityRow {
	out := []activityRow{}
	rows, err := h.store.Pool.Query(ctx, `
		SELECT a.kind, a.object_key, a.record_id::text, a.occurred_at, COALESCE(i.display_name, 'The CRM'), a.actor_id = $2,
		       COALESCE(o.name, NULLIF(trim(concat_ws(' ', l.first_name, l.last_name)), ''), NULLIF(trim(concat_ws(' ', c.first_name, c.last_name)), ''), ac.name, '')
		FROM crm.activities a
		LEFT JOIN crm.identities i ON i.id = a.actor_id
		LEFT JOIN crm.object_records o ON o.id = a.record_id
		LEFT JOIN crm.leads l ON l.id = a.record_id
		LEFT JOIN crm.contacts c ON c.id = a.record_id
		LEFT JOIN crm.accounts ac ON ac.id = a.record_id
		WHERE a.workspace_id = $1 AND a.object_key = ANY($3) AND a.kind IN ('record.created', 'record.converted', 'file.uploaded', 'asset.serviced', 'work_order.created')
		ORDER BY a.occurred_at DESC LIMIT 200`, sc.WS, me, activityObjects())
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var x activityRow
		var mine bool
		if rows.Scan(&x.Kind, &x.Object, &x.ID, &x.At, &x.Actor, &mine, &x.Title) != nil {
			continue
		}
		if !sc.Enabled(x.Object) || !sc.Can(x.Object, "read") || (sc.OwnOnly(x.Object) && !mine) {
			continue
		}
		x.Category = activityCategory[x.Object]
		out = append(out, x)
		if len(out) >= limit {
			break
		}
	}
	return out
}

func activityObjects() []string {
	keys := make([]string, 0, len(activityCategory))
	for k := range activityCategory {
		keys = append(keys, k)
	}
	return keys
}

func initialsOf(name string) string {
	parts := strings.Fields(name)
	s := ""
	for _, p := range parts {
		if len(s) < 2 && p != "" {
			s += strings.ToUpper(p[:1])
		}
	}
	return s
}

// shortRupees: ₹12.4L, ₹1.2Cr — for one line of text.
func shortRupees(v float64) string {
	switch {
	case v >= 1e7:
		return "₹" + strconv.FormatFloat(v/1e7, 'f', 2, 64) + "Cr"
	case v >= 1e5:
		return "₹" + strconv.FormatFloat(v/1e5, 'f', 1, 64) + "L"
	case v >= 1e3:
		return "₹" + strconv.FormatFloat(v/1e3, 'f', 1, 64) + "K"
	}
	return "₹" + strconv.FormatFloat(v, 'f', 0, 64)
}

// GET /w/{code}/forecast/trend?from=YYYY-MM-DD&to=YYYY-MM-DD (D-176): closed won added up
// week by week through a forecast period, up to today — the actual line of the forecast
// chart. Nothing is projected here; the page draws the pace from these points.
func (h *Handler) handleForecastTrend(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.Enabled("opportunities") || !sc.Can("opportunities", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	ctx := r.Context()
	me := actor(r)
	from, err1 := time.Parse("2006-01-02", r.URL.Query().Get("from"))
	to, err2 := time.Parse("2006-01-02", r.URL.Query().Get("to"))
	if err1 != nil || err2 != nil || to.Before(from) || to.Sub(from) > 400*24*time.Hour {
		shared.WriteError(w, r, shared.Validation(map[string]string{"from": "Give the period as from and to dates."}))
		return
	}
	args := []any{sc.WS, from.Format("2006-01-02"), to.Format("2006-01-02")}
	own := ""
	if sc.OwnOnly("opportunities") {
		args = append(args, sc.OwnersFor("opportunities", me))
		own = " AND t.owner_id = ANY($4::uuid[])"
	}
	rows, err := h.store.Pool.Query(ctx, `SELECT COALESCE(NULLIF(t.custom->>'closeDate', ''), to_char(t.updated_at, 'YYYY-MM-DD')), COALESCE(sum(`+numeric(`t.custom->>'amount'`)+`), 0)::float8
		FROM crm.object_records t WHERE t.workspace_id = $1 AND t.deleted_at IS NULL AND t.object_key = 'opportunities' AND t.status = 'closed_won'
		  AND COALESCE(NULLIF(t.custom->>'closeDate', ''), to_char(t.updated_at, 'YYYY-MM-DD')) BETWEEN $2 AND $3`+own+` GROUP BY 1`, args...)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	byDay := map[string]float64{}
	for rows.Next() {
		var d string
		var v float64
		if rows.Scan(&d, &v) == nil {
			byDay[d] += v
		}
	}
	rows.Close()
	type week struct {
		Label  string   `json:"label"`
		End    string   `json:"end"`
		Closed *float64 `json:"closed"` // running total; null for weeks still to come
	}
	out := struct {
		Weeks []week `json:"weeks"`
		Today string `json:"today"`
	}{Weeks: []week{}, Today: time.Now().Format("2006-01-02")}
	total := 0.0
	n := 0
	for start := from; !start.After(to); start = start.AddDate(0, 0, 7) {
		n++
		end := start.AddDate(0, 0, 6)
		if end.After(to) {
			end = to
		}
		wk := week{Label: "W" + strconv.Itoa(n), End: end.Format("2006-01-02")}
		if !start.After(time.Now()) {
			for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
				total += byDay[d.Format("2006-01-02")]
			}
			v := total
			wk.Closed = &v
		}
		out.Weeks = append(out.Weeks, wk)
	}
	shared.WriteJSON(w, http.StatusOK, out)
}
