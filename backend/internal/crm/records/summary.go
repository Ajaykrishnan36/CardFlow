package records

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
)

// Dashboard summary (D-96): the numbers on the mobile Home screen and the web dashboard,
// for one business and one date range. Everything is computed in SQL for the caller: an
// object they can't read is left out (not shown as zero), and with "own records" scope the
// counts cover their records and their team's. Nothing here is sample data.

// GET /w/{code}/dashboard/summary?range=today|week|month|year|all|custom&from=YYYY-MM-DD&to=YYYY-MM-DD

type summaryRange struct {
	Key  string `json:"key"`
	From string `json:"from,omitempty"` // inclusive, business time zone
	To   string `json:"to,omitempty"`   // inclusive
	TZ   string `json:"timezone"`
}

type moneySummary struct {
	Currency string  `json:"currency"`
	Income   float64 `json:"income"`
	Expenses float64 `json:"expenses"`
	Net      float64 `json:"net"`
}

type dashboardSummary struct {
	Range    summaryRange       `json:"range"`
	Metrics  map[string]float64 `json:"metrics"`
	Finance  *moneySummary      `json:"finance,omitempty"`
	Lists    []recentList       `json:"lists"`
	Currency string             `json:"currency"`
	// Can tells the client which quick actions to offer (the server still checks each one).
	Can map[string]bool `json:"can"`
}

// rangeBounds turns a range key into [from, to) in the business time zone.
func rangeBounds(key, fromStr, toStr string, loc *time.Location, now time.Time) (summaryRange, time.Time, time.Time, error) {
	now = now.In(loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	out := summaryRange{Key: key, TZ: loc.String()}
	var from, to time.Time
	switch key {
	case "", "today":
		out.Key = "today"
		from, to = day, day.AddDate(0, 0, 1)
	case "week":
		wd := (int(day.Weekday()) + 6) % 7 // Monday starts the week
		from = day.AddDate(0, 0, -wd)
		to = from.AddDate(0, 0, 7)
	case "month":
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
		to = from.AddDate(0, 1, 0)
	case "year":
		from = time.Date(now.Year(), 1, 1, 0, 0, 0, 0, loc)
		to = from.AddDate(1, 0, 0)
	case "all":
		from = time.Date(1970, 1, 1, 0, 0, 0, 0, loc)
		to = time.Date(2200, 1, 1, 0, 0, 0, 0, loc)
		return out, from, to, nil
	case "custom":
		f, err1 := time.ParseInLocation("2006-01-02", fromStr, loc)
		t, err2 := time.ParseInLocation("2006-01-02", toStr, loc)
		if err1 != nil || err2 != nil || t.Before(f) {
			return out, from, to, shared.Validation(map[string]string{"from": "Choose a start and an end date (the end can't be before the start)."})
		}
		from, to = f, t.AddDate(0, 0, 1)
	default:
		return out, from, to, shared.Validation(map[string]string{"range": "Choose today, week, month, year, all or custom."})
	}
	out.From, out.To = from.Format("2006-01-02"), to.AddDate(0, 0, -1).Format("2006-01-02")
	return out, from, to, nil
}

// numeric reads a JSON text value as a number, treating anything that isn't one as 0.
func numeric(expr string) string {
	return `CASE WHEN ` + expr + ` ~ '^-?[0-9]+(\.[0-9]+)?$' THEN (` + expr + `)::numeric ELSE 0 END`
}

func (h *Handler) handleDashboardSummary(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.Owner && !sc.Eff.HasCapability(access.CapDashboard) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	ctx := r.Context()
	me := actor(r)
	env := h.envFor(ctx, sc.WS, me)
	loc, err := time.LoadLocation(env.TZ)
	if err != nil {
		loc = time.UTC
	}
	q := r.URL.Query()
	rng, from, to, err := rangeBounds(strings.ToLower(q.Get("range")), q.Get("from"), q.Get("to"), loc, time.Now())
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out := dashboardSummary{Range: rng, Metrics: map[string]float64{}, Lists: []recentList{}, Can: map[string]bool{}, Currency: "INR"}
	_ = h.store.Pool.QueryRow(ctx, `SELECT currency FROM crm.workspaces WHERE id = $1`, sc.WS).Scan(&out.Currency)
	out.Currency = strings.TrimSpace(out.Currency)

	now := time.Now().In(loc)
	today := now.Format("2006-01-02")
	weekAhead := now.AddDate(0, 0, 7).Format("2006-01-02")
	fromDate, toDate := from.Format("2006-01-02"), to.Format("2006-01-02") // [fromDate, toDate)

	can := func(key string) bool { return specFor(key) != nil && sc.Enabled(key) && sc.Can(key, "read") }
	for _, k := range []string{"leads", "contacts", "accounts", "opportunities", "tasks", "events", "cases", "income", "expenses", "notes", "communications"} {
		out.Can[k+".read"] = can(k)
		out.Can[k+".create"] = specFor(k) != nil && sc.Enabled(k) && sc.Can(k, "create")
	}

	// scope adds the tenant filter and, for "own records", the owner filter.
	scope := func(key, alias string, args *[]any) string {
		*args = append(*args, sc.WS)
		s := " " + alias + ".workspace_id = $" + strconv.Itoa(len(*args)) + " AND " + alias + ".deleted_at IS NULL"
		if sc.OwnOnly(key) {
			*args = append(*args, sc.OwnersFor(key, me))
			s += " AND " + alias + ".owner_id = ANY($" + strconv.Itoa(len(*args)) + "::uuid[])"
		}
		return s
	}
	arg := func(args *[]any, v any) string {
		*args = append(*args, v)
		return "$" + strconv.Itoa(len(*args))
	}
	scan := func(sql string, args []any, dst ...any) bool {
		if err := h.store.Pool.QueryRow(ctx, sql, args...).Scan(dst...); err != nil {
			shared.WriteError(w, r, err)
			return false
		}
		return true
	}

	if can("leads") {
		args := []any{}
		where := scope("leads", "t", &args)
		f, t := arg(&args, from), arg(&args, to)
		var active, fresh, converted, lost, followUps float64
		if !scan(`SELECT
			count(*) FILTER (WHERE t.status NOT IN ('converted', 'lost')),
			count(*) FILTER (WHERE t.created_at >= `+f+` AND t.created_at < `+t+`),
			count(*) FILTER (WHERE t.converted_at >= `+f+` AND t.converted_at < `+t+`),
			count(*) FILTER (WHERE t.status = 'lost' AND t.updated_at >= `+f+` AND t.updated_at < `+t+`),
			count(*) FILTER (WHERE t.status NOT IN ('converted', 'lost') AND (t.next_follow_up_at AT TIME ZONE `+arg(&args, loc.String())+`)::date = `+arg(&args, today)+`::date)
			FROM crm.leads t WHERE`+where, args, &active, &fresh, &converted, &lost, &followUps) {
			return
		}
		out.Metrics["activeLeads"], out.Metrics["newLeads"] = active, fresh
		out.Metrics["convertedLeads"], out.Metrics["lostLeads"], out.Metrics["closedLeads"] = converted, lost, converted+lost
		out.Metrics["followUpsToday"] = followUps
	}
	for _, o := range []struct{ key, table, active, fresh string }{
		{"contacts", "contacts", "activeContacts", "newContacts"}, {"accounts", "accounts", "activeAccounts", "newAccounts"},
	} {
		if !can(o.key) {
			continue
		}
		args := []any{}
		where := scope(o.key, "t", &args)
		var total, fresh float64
		if !scan(`SELECT count(*), count(*) FILTER (WHERE t.created_at >= `+arg(&args, from)+` AND t.created_at < `+arg(&args, to)+`)
			FROM crm.`+o.table+` t WHERE`+where, args, &total, &fresh) {
			return
		}
		out.Metrics[o.active], out.Metrics[o.fresh] = total, fresh
	}
	amount := numeric(`t.custom->>'amount'`)
	if can("opportunities") {
		args := []any{}
		where := scope("opportunities", "t", &args)
		f, t := arg(&args, fromDate), arg(&args, toDate)
		closedIn := `(COALESCE(NULLIF(t.custom->>'closeDate', ''), to_char(t.updated_at, 'YYYY-MM-DD')) >= ` + f + ` AND COALESCE(NULLIF(t.custom->>'closeDate', ''), to_char(t.updated_at, 'YYYY-MM-DD')) < ` + t + `)`
		var open, pipeline, weighted, won, wonValue, lost float64
		if !scan(`SELECT
			count(*) FILTER (WHERE COALESCE(t.status, '') NOT IN ('closed_won', 'closed_lost')),
			COALESCE(sum(`+amount+`) FILTER (WHERE COALESCE(t.status, '') NOT IN ('closed_won', 'closed_lost')), 0),
			COALESCE(sum(`+amount+` * `+numeric(`t.custom->>'probability'`)+` / 100) FILTER (WHERE COALESCE(t.status, '') NOT IN ('closed_won', 'closed_lost')), 0),
			count(*) FILTER (WHERE t.status = 'closed_won' AND `+closedIn+`),
			COALESCE(sum(`+amount+`) FILTER (WHERE t.status = 'closed_won' AND `+closedIn+`), 0),
			count(*) FILTER (WHERE t.status = 'closed_lost' AND `+closedIn+`)
			FROM crm.object_records t WHERE t.object_key = 'opportunities' AND`+where, args, &open, &pipeline, &weighted, &won, &wonValue, &lost) {
			return
		}
		out.Metrics["openOpportunities"], out.Metrics["pipelineValue"], out.Metrics["weightedPipeline"] = open, pipeline, weighted
		out.Metrics["wonDeals"], out.Metrics["wonValue"], out.Metrics["lostDeals"] = won, wonValue, lost
		if won+lost > 0 {
			out.Metrics["winRate"] = won * 100 / (won + lost)
		}
	}
	if can("tasks") {
		args := []any{}
		where := scope("tasks", "t", &args)
		td := arg(&args, today)
		due := `NULLIF(t.custom->>'dueDate', '')`
		openTask := `COALESCE(t.status, '') NOT IN ('completed', 'deferred')`
		var dueToday, overdue, upcoming, open float64
		if !scan(`SELECT
			count(*) FILTER (WHERE `+openTask+` AND `+due+` = `+td+`),
			count(*) FILTER (WHERE `+openTask+` AND `+due+` < `+td+`),
			count(*) FILTER (WHERE `+openTask+` AND `+due+` > `+td+` AND `+due+` <= `+arg(&args, weekAhead)+`),
			count(*) FILTER (WHERE `+openTask+`)
			FROM crm.object_records t WHERE t.object_key = 'tasks' AND`+where, args, &dueToday, &overdue, &upcoming, &open) {
			return
		}
		out.Metrics["tasksDueToday"], out.Metrics["overdueTasks"], out.Metrics["upcomingTasks"], out.Metrics["openTasks"] = dueToday, overdue, upcoming, open
	}
	if can("events") {
		args := []any{}
		where := scope("events", "t", &args)
		var upcoming float64
		if !scan(`SELECT count(*) FROM crm.object_records t WHERE t.object_key = 'events' AND COALESCE(t.status, '') <> 'cancelled'
			AND left(COALESCE(t.custom->>'startsAt', ''), 10) >= `+arg(&args, today)+` AND left(COALESCE(t.custom->>'startsAt', ''), 10) <= `+arg(&args, weekAhead)+` AND`+where,
			args, &upcoming) {
			return
		}
		out.Metrics["upcomingMeetings"] = upcoming
	}
	if can("cases") {
		args := []any{}
		where := scope("cases", "t", &args)
		var open, fresh float64
		if !scan(`SELECT count(*) FILTER (WHERE COALESCE(t.status, '') NOT IN ('resolved', 'closed')),
			count(*) FILTER (WHERE t.created_at >= `+arg(&args, from)+` AND t.created_at < `+arg(&args, to)+`)
			FROM crm.object_records t WHERE t.object_key = 'cases' AND`+where, args, &open, &fresh) {
			return
		}
		out.Metrics["openCases"], out.Metrics["newCases"] = open, fresh
	}
	// Money: income and expenses dated inside the range (cancelled ones don't count).
	sum := func(key string) (float64, bool) {
		args := []any{}
		where := scope(key, "t", &args)
		var total float64
		ok := scan(`SELECT COALESCE(sum(`+amount+`), 0) FROM crm.object_records t WHERE t.object_key = `+arg(&args, key)+`
			AND COALESCE(t.status, '') <> 'cancelled'
			AND COALESCE(NULLIF(t.custom->>'date', ''), to_char(t.created_at, 'YYYY-MM-DD')) >= `+arg(&args, fromDate)+`
			AND COALESCE(NULLIF(t.custom->>'date', ''), to_char(t.created_at, 'YYYY-MM-DD')) < `+arg(&args, toDate)+` AND`+where, args, &total)
		return total, ok
	}
	if can("income") || can("expenses") {
		m := &moneySummary{Currency: out.Currency}
		if can("income") {
			v, ok := sum("income")
			if !ok {
				return
			}
			m.Income = v
			// Money received against invoices and orders is income too (less what was refunded).
			if can("payments") {
				args := []any{}
				where := scope("payments", "t", &args)
				paid := `COALESCE(NULLIF(t.custom->>'baseAmount', ''), t.custom->>'amount')`
				var got float64
				if !scan(`SELECT COALESCE(sum(GREATEST(`+numeric(paid)+` - `+numeric(`t.custom->>'refundedAmount'`)+` * COALESCE(NULLIF(t.custom->>'exchangeRate', '')::numeric, 1), 0)), 0)
					FROM crm.object_records t WHERE t.object_key = 'payments' AND COALESCE(t.status, '') IN ('paid', 'partially_refunded')
					AND COALESCE(NULLIF(left(t.custom->>'paymentDate', 10), ''), to_char(t.created_at, 'YYYY-MM-DD')) >= `+arg(&args, fromDate)+`
					AND COALESCE(NULLIF(left(t.custom->>'paymentDate', 10), ''), to_char(t.created_at, 'YYYY-MM-DD')) < `+arg(&args, toDate)+` AND`+where, args, &got) {
					return
				}
				m.Income += got
			}
		}
		if can("expenses") {
			v, ok := sum("expenses")
			if !ok {
				return
			}
			m.Expenses = v
		}
		if can("income") && can("expenses") {
			m.Net = m.Income - m.Expenses
		}
		out.Finance = m
	}

	// Short lists for the Home screen.
	if can("leads") {
		if l, ok := h.summaryList(w, r, sc, me, "followUps", "Today's follow-ups", "leads",
			`SELECT t.id::text, t.code, `+leadSpec.TitleSQL+`, COALESCE(t.organization, ''), COALESCE(t.status, '') FROM crm.leads t WHERE`,
			` AND t.status NOT IN ('converted', 'lost') AND (t.next_follow_up_at AT TIME ZONE '`+strings.ReplaceAll(loc.String(), "'", "")+`')::date <= '`+today+`'::date ORDER BY t.next_follow_up_at LIMIT 5`); ok {
			out.Lists = append(out.Lists, l)
		} else {
			return
		}
		if l, ok := h.summaryList(w, r, sc, me, "recentLeads", "Recent leads", "leads",
			`SELECT t.id::text, t.code, `+leadSpec.TitleSQL+`, COALESCE(t.organization, ''), COALESCE(t.status, '') FROM crm.leads t WHERE`,
			` ORDER BY t.created_at DESC LIMIT 5`); ok {
			out.Lists = append(out.Lists, l)
		} else {
			return
		}
	}
	dyn := func(listKey, label, key, extra, order string) bool {
		if !can(key) {
			return true
		}
		l, ok := h.summaryList(w, r, sc, me, listKey, label, key,
			`SELECT t.id::text, t.code, t.name, `+extra+`, COALESCE(t.status, '') FROM crm.object_records t WHERE t.object_key = '`+key+`' AND`, order)
		if ok {
			out.Lists = append(out.Lists, l)
		}
		return ok
	}
	if !dyn("upcomingTasks", "Upcoming tasks", "tasks", `COALESCE(t.custom->>'dueDate', '')`,
		` AND COALESCE(t.status, '') NOT IN ('completed', 'deferred') ORDER BY NULLIF(t.custom->>'dueDate', '') NULLS LAST, t.created_at LIMIT 5`) {
		return
	}
	if !dyn("upcomingMeetings", "Upcoming meetings", "events", `COALESCE(t.custom->>'startsAt', '')`,
		` AND COALESCE(t.status, '') <> 'cancelled' AND left(COALESCE(t.custom->>'startsAt', ''), 10) >= '`+today+`' ORDER BY t.custom->>'startsAt' LIMIT 5`) {
		return
	}
	if !dyn("recentOpportunities", "Recent opportunities", "opportunities", `COALESCE(t.custom->>'amount', '')`, ` ORDER BY t.updated_at DESC LIMIT 5`) {
		return
	}
	if can("contacts") {
		if l, ok := h.summaryList(w, r, sc, me, "recentContacts", "Recent contacts", "contacts",
			`SELECT t.id::text, t.code, `+contactSpec.TitleSQL+`, COALESCE(t.title, ''), '' FROM crm.contacts t WHERE`, ` ORDER BY t.created_at DESC LIMIT 5`); ok {
			out.Lists = append(out.Lists, l)
		} else {
			return
		}
	}
	shared.WriteJSON(w, http.StatusOK, out)
}

// summaryList runs one short list query with the tenant and owner filters applied.
func (h *Handler) summaryList(w http.ResponseWriter, r *http.Request, sc *Scope, me uuid.UUID, listKey, label, object, selectSQL, tail string) (recentList, bool) {
	args := []any{sc.WS}
	where := " t.workspace_id = $1 AND t.deleted_at IS NULL"
	if sc.OwnOnly(object) {
		args = append(args, sc.OwnersFor(object, me))
		where += " AND t.owner_id = ANY($2::uuid[])"
	}
	l := recentList{Object: object, Label: label, Rows: []RelatedRow{}}
	rows, err := h.store.Pool.Query(r.Context(), selectSQL+where+tail, args...)
	if err != nil {
		shared.WriteError(w, r, err)
		return l, false
	}
	defer rows.Close()
	for rows.Next() {
		var rr RelatedRow
		if err := rows.Scan(&rr.ID, &rr.Code, &rr.Title, &rr.Subtitle, &rr.Status); err != nil {
			shared.WriteError(w, r, err)
			return l, false
		}
		l.Rows = append(l.Rows, rr)
	}
	// The client keys lists by listKey; Object says where a row opens.
	l.Label = label
	l.Key = listKey
	return l, rows.Err() == nil
}
