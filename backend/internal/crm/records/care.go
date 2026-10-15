package records

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Customer care (D-151): the "what is coming up → call → book → done → next time" cycle
// that every one of the 110 business PRDs asks for (their section 5, U01–U15), built once
// for every trade.
//
//	care_schedules  something that recurs for a customer: a service every 3 months, a
//	                renewal every year, a follow-up every 2 weeks — any interval, any anchor
//	due_items       one occurrence: due on a date, called, booked, done (or skipped)
//	call_logs       every call or visit: outcome, notes and the next action
//
// Rules: a call never marks the work done; finishing (or skipping) a due item creates
// exactly one next one; an item someone is calling is claimed for 30 minutes so two
// people don't call the same customer; counts on the Upcoming page are the same query as
// the list they open.

const sourceCare = "care"

func careObjects() []ObjectDefinition {
	kinds := opts("service", "Service / maintenance", "renewal", "Renewal", "follow_up", "Follow-up", "visit", "Visit", "payment", "Payment due",
		"review", "Review", "delivery", "Delivery / pickup", "recall", "Recall / check-up", "other", "Other")
	return []ObjectDefinition{
		{Key: "care_schedules", Module: "care", Singular: "Recurring schedule", Plural: "Recurring schedules", Icon: "repeat", Prefix: "RSC",
			Description: "Anything that comes round again for a customer — a service, a renewal, a check-up — with its interval and the next due date.",
			ObjectBody: ObjectBody{NameLabel: "Schedule", StatusLabel: "Status", Statuses: []StatusOption{
				st("active", "Active", "success"), st("paused", "Paused", "warning"), st("ended", "Ended", "neutral")},
				Fields: []ObjectField{
					ofLookup("contactId", "Customer", "contacts"), ofLookup("accountId", "Account", "accounts"),
					ofReq(ofSelect("kind", "What comes up", kinds)), ofLookup("itemId", "Service or product", "catalog_items"),
					ofLookup("assetId", "For the customer's", "assets"), ofLookup("contractId", "Contract", "contracts"),
					ofReq(of("intervalValue", "Every", "number")),
					ofReq(ofSelect("intervalUnit", "Unit", opts("days", "Days", "weeks", "Weeks", "months", "Months", "years", "Years"))),
					ofSelect("anchor", "Next date counts from", opts("completion", "When the last one was done", "fixed", "The planned date (fixed calendar)")),
					ofReq(of("startDate", "First due date", "date")), of("endDate", "Stop after", "date"),
					of("nextDueDate", "Next due", "date"), of("lastDoneOn", "Last done on", "date"), of("cycles", "Times done", "number"),
					of("notes", "Notes", "textarea")}}},
		{Key: "due_items", Module: "care", Singular: "Due item", Plural: "Due items", Icon: "calendar", Prefix: "DUE",
			Description: "One occurrence of a schedule: when it is due, who is calling, what the customer said, and whether it was booked and done.",
			ObjectBody: ObjectBody{NameLabel: "Due item", StatusLabel: "Status", Statuses: []StatusOption{
				st("open", "To contact", "primary"), st("contacted", "Contacted — follow up", "warning"), st("booked", "Booked", "primary"),
				st("done", "Done", "success"), st("skipped", "Skipped", "neutral")},
				Fields: []ObjectField{
					ofLookup("scheduleId", "Schedule", "care_schedules"), ofLookup("contactId", "Customer", "contacts"), ofLookup("accountId", "Account", "accounts"),
					ofSelect("kind", "What is due", kinds), ofLookup("assetId", "For the customer's", "assets"), ofLookup("contractId", "Contract", "contracts"),
					ofReq(of("dueDate", "Due date", "date")), of("cycle", "Occurrence", "number"),
					ofLookup("claimedBy", "Being called by", "users"), of("claimedUntil", "Claimed until", "datetime"),
					ofSelect("lastOutcome", "Last call", callOutcomes), of("lastContactAt", "Last contacted", "datetime"),
					of("nextActionAt", "Next action on", "date"), ofLookup("appointmentId", "Appointment", "appointments"),
					of("doneOn", "Done on", "date"), ofSelect("skipReason", "Why skipped", opts("declined", "Customer declined", "done_elsewhere", "Done elsewhere",
						"not_needed", "Not needed this time", "unreachable", "Could not reach", "moved", "Customer moved", "other", "Other")),
					of("notes", "Notes", "textarea")}}},
		{Key: "call_logs", Module: "care", Singular: "Call", Plural: "Call log", Icon: "message-square", Prefix: "CAL",
			Description: "Every call, message or visit to a customer: the outcome, what was said and what happens next.",
			ObjectBody: ObjectBody{NameLabel: "Call", Statuses: []StatusOption{},
				Fields: []ObjectField{
					ofLookup("contactId", "Customer", "contacts"), ofLookup("leadId", "Lead", "leads"), ofLookup("dueItemId", "About", "due_items"),
					ofSelect("channel", "By", opts("call", "Phone call", "whatsapp", "WhatsApp", "sms", "SMS", "visit", "Visit", "email", "Email")),
					ofReq(ofSelect("outcome", "Outcome", callOutcomes)), of("calledAt", "When", "datetime"),
					of("nextActionAt", "Next action on", "date"), of("notes", "What was said", "textarea")}}},
	}
}

var callOutcomes = opts("connected", "Connected", "no_answer", "No answer", "busy", "Busy", "switched_off", "Switched off",
	"callback", "Asked to call back", "interested", "Interested — will decide", "booked", "Booked", "declined", "Declined",
	"done_elsewhere", "Done elsewhere", "wrong_number", "Wrong number")

// addInterval moves a date by a schedule's interval; months are calendar months, and a
// day that doesn't exist in the target month (31st, 29 Feb) falls back to its last day.
func addInterval(from time.Time, n int, unit string) time.Time {
	switch unit {
	case "days":
		return from.AddDate(0, 0, n)
	case "weeks":
		return from.AddDate(0, 0, 7*n)
	case "years":
		n *= 12
		fallthrough
	default: // months
		y, m, d := from.Date()
		first := time.Date(y, m+time.Month(n), 1, 0, 0, 0, 0, time.UTC)
		last := first.AddDate(0, 1, -1).Day()
		if d > last {
			d = last
		}
		return time.Date(first.Year(), first.Month(), d, 0, 0, 0, 0, time.UTC)
	}
}

func parseDay(s string) (time.Time, bool) {
	if len(s) >= 10 {
		s = s[:10]
	}
	t, err := time.Parse("2006-01-02", s)
	return t, err == nil
}

func (h *Handler) careSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, a actorInfo, op string, before, after *Row) (*Row, error) {
	if a.Source == sourceCare {
		return after, nil
	}
	switch spec.Key {
	case "care_schedules":
		return h.scheduleSaved(ctx, tx, ws, op, before, after)
	case "due_items":
		return h.dueItemSaved(ctx, tx, ws, a, op, before, after)
	case "call_logs":
		return h.callLogged(ctx, tx, ws, a, after)
	}
	return after, nil
}

// scheduleSaved: a schedule is about someone, has a sensible interval, and always has
// exactly one open due item while it is active.
func (h *Handler) scheduleSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, after *Row) (*Row, error) {
	if !after.has("contactId") && !after.has("accountId") {
		return nil, refuse("contactId", "Pick the customer this is for.")
	}
	if n := after.num("intervalValue"); n < 1 || n > 1000 || n != float64(int(n)) {
		return nil, refuse("intervalValue", "Use a whole number from 1 to 1000.")
	}
	start, ok := parseDay(after.text("startDate"))
	if !ok {
		return nil, refuse("startDate", "Enter the first due date.")
	}
	if end, ok := parseDay(after.text("endDate")); ok && end.Before(start) {
		return nil, refuse("endDate", "The schedule can't stop before it starts.")
	}
	set := map[string]any{}
	if !after.has("anchor") {
		set["anchor"] = "completion"
	}
	if op == "create" {
		set["nextDueDate"] = start.Format("2006-01-02")
		set["cycles"] = 0.0
	}
	var err error
	if len(set) > 0 {
		if after, err = h.updateValues(ctx, tx, ws, specFor("care_schedules"), after.uuid(), systemActor(sourceCare), set, nil, nil); err != nil {
			return nil, err
		}
	}
	switch after.text("status") {
	case "active":
		// A changed first date or interval moves the open item (nothing done is rewritten).
		if op == "update" && (before.text("startDate") != after.text("startDate") || before.num("intervalValue") != after.num("intervalValue") ||
			before.text("intervalUnit") != after.text("intervalUnit")) && before.num("cycles") == 0 {
			if _, err := tx.Exec(ctx, `UPDATE crm.object_records SET custom = jsonb_set(custom, '{dueDate}', to_jsonb($3::text)), updated_at = now()
				WHERE workspace_id = $1 AND object_key = 'due_items' AND deleted_at IS NULL AND custom->>'scheduleId' = $2 AND status IN ('open', 'contacted')`,
				ws, after.ID, start.Format("2006-01-02")); err != nil {
				return nil, err
			}
		}
		return after, h.ensureDueItem(ctx, tx, ws, after)
	default:
		// Paused or ended: the waiting item is skipped, so nobody is asked to call.
		_, err := tx.Exec(ctx, `UPDATE crm.object_records SET status = 'skipped', custom = custom || jsonb_build_object('skipReason', 'not_needed'), updated_at = now()
			WHERE workspace_id = $1 AND object_key = 'due_items' AND deleted_at IS NULL AND custom->>'scheduleId' = $2 AND status IN ('open', 'contacted')`, ws, after.ID)
		return after, err
	}
}

// ensureDueItem creates the schedule's next occurrence unless one is already waiting.
// The schedule row is locked, so two saves at the same moment create one item.
func (h *Handler) ensureDueItem(ctx context.Context, tx pgx.Tx, ws uuid.UUID, s *Row) error {
	if _, err := tx.Exec(ctx, `SELECT 1 FROM crm.object_records WHERE id = $1 FOR UPDATE`, s.uuid()); err != nil {
		return err
	}
	var open int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'due_items' AND deleted_at IS NULL
		AND custom->>'scheduleId' = $2 AND status IN ('open', 'contacted', 'booked')`, ws, s.ID).Scan(&open); err != nil || open > 0 {
		return err
	}
	due, ok := parseDay(s.text("nextDueDate"))
	if !ok {
		return nil
	}
	if end, ok := parseDay(s.text("endDate")); ok && due.After(end) {
		_, err := h.updateValues(ctx, tx, ws, specFor("care_schedules"), s.uuid(), systemActor(sourceCare), map[string]any{"status": "ended"}, nil, nil)
		return err
	}
	cycle := s.num("cycles") + 1
	v := map[string]any{"name": joinNonEmpty(" — ", labelOf(careKindLabels, s.text("kind")), h.labelOf(ctx, tx, ws, "contacts", s.text("contactId")),
		h.labelOf(ctx, tx, ws, "assets", s.text("assetId"))), "status": "open", "ownerId": s.text("ownerId"), "scheduleId": s.ID,
		"contactId": s.text("contactId"), "accountId": s.text("accountId"), "assetId": s.text("assetId"), "contractId": s.text("contractId"),
		"kind": s.text("kind"), "dueDate": due.Format("2006-01-02"), "cycle": cycle}
	clean(v)
	_, err := h.createRecord(ctx, tx, ws, specFor("due_items"), systemActor(sourceCare), v)
	return err
}

var careKindLabels = func() map[string]string {
	out := map[string]string{}
	for _, d := range careObjects() {
		for _, f := range d.Fields {
			if f.Key == "kind" {
				for _, o := range f.Options {
					out[o.Value] = o.Label
				}
			}
		}
	}
	return out
}()

func labelOf(m map[string]string, k string) string {
	if l, ok := m[k]; ok {
		return l
	}
	return k
}

// dueItemSaved: done needs a date and plans the next one; skipped needs a reason and
// plans the next one too (unless the schedule has ended); a finished item stays finished.
func (h *Handler) dueItemSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, op string, before, after *Row) (*Row, error) {
	status, was := after.text("status"), before.text("status")
	if op == "update" && (was == "done" || was == "skipped") && status != was {
		return nil, refuse("status", "This one is finished. The next occurrence is already planned on the schedule.")
	}
	if !after.has("contactId") && !after.has("accountId") {
		return nil, refuse("contactId", "Pick the customer this is for.")
	}
	set := map[string]any{}
	finishing := (status == "done" || status == "skipped") && (op == "create" || was != status)
	if status == "skipped" && !after.has("skipReason") {
		return nil, refuse("skipReason", "Say why it was skipped.")
	}
	if status == "booked" && !after.has("appointmentId") && !after.has("nextActionAt") {
		return nil, refuse("appointmentId", "Pick the appointment, or set the date it is booked for (Next action on).")
	}
	if status == "done" && !after.has("doneOn") {
		set["doneOn"] = today()
	}
	if finishing {
		set["claimedBy"], set["claimedUntil"] = nil, nil
	}
	var err error
	if len(set) > 0 {
		if after, err = h.updateValues(ctx, tx, ws, specFor("due_items"), after.uuid(), systemActor(sourceCare), set, nil, nil); err != nil {
			return nil, err
		}
	}
	if !finishing || !after.has("scheduleId") {
		return after, nil
	}
	s, _, err := h.getRow(ctx, tx, ws, specFor("care_schedules"), after.id("scheduleId"), nil)
	if err != nil || s.text("status") != "active" {
		return after, nil
	}
	// The next date: from when it was actually done, or from the planned date.
	base, _ := parseDay(after.text("dueDate"))
	if s.text("anchor") != "fixed" {
		if d, ok := parseDay(after.text("doneOn")); ok && status == "done" {
			base = d
		} else if status == "skipped" {
			base, _ = parseDay(today())
		}
	}
	next := addInterval(base, int(s.num("intervalValue")), s.text("intervalUnit"))
	sv := map[string]any{"nextDueDate": next.Format("2006-01-02"), "cycles": s.num("cycles") + 1}
	if status == "done" {
		sv["lastDoneOn"] = after.text("doneOn")
	}
	if s, err = h.updateValues(ctx, tx, ws, specFor("care_schedules"), s.uuid(), systemActor(sourceCare), sv, nil, nil); err != nil {
		return nil, err
	}
	return after, h.ensureDueItem(ctx, tx, ws, s)
}

// callLogged: what a call means for the due item it was about. No outcome of a call ever
// marks the work itself done.
func (h *Handler) callLogged(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, c *Row) (*Row, error) {
	if !c.has("contactId") && !c.has("leadId") && !c.has("dueItemId") {
		return nil, refuse("contactId", "Pick who was called.")
	}
	outcome := c.text("outcome")
	needsDate := outcome == "connected" || outcome == "callback" || outcome == "interested"
	if needsDate && !c.has("nextActionAt") {
		return nil, refuse("nextActionAt", "Set when to follow up.")
	}
	set := map[string]any{}
	if !c.has("calledAt") {
		set["calledAt"] = time.Now().UTC().Format(time.RFC3339)
	}
	if outcome == "no_answer" || outcome == "busy" || outcome == "switched_off" {
		if !c.has("nextActionAt") {
			set["nextActionAt"] = time.Now().AddDate(0, 0, 1).Format("2006-01-02") // try again tomorrow
		}
	}
	due := c.id("dueItemId")
	if due != uuid.Nil {
		d, _, err := h.getRow(ctx, tx, ws, specFor("due_items"), due, nil)
		if err != nil {
			return nil, refuse("dueItemId", "That due item doesn't exist.")
		}
		if !c.has("contactId") && d.has("contactId") {
			set["contactId"] = d.text("contactId")
		}
		if d.text("status") == "done" || d.text("status") == "skipped" {
			return nil, refuse("dueItemId", "That one is already finished.")
		}
		next := c.text("nextActionAt")
		if v, ok := set["nextActionAt"].(string); ok {
			next = v
		}
		dv := map[string]any{"lastOutcome": outcome, "lastContactAt": time.Now().UTC().Format(time.RFC3339), "nextActionAt": next,
			"claimedBy": nil, "claimedUntil": nil}
		switch outcome {
		case "booked":
			dv["status"] = "booked"
		case "declined":
			dv["status"], dv["skipReason"] = "skipped", "declined"
		case "done_elsewhere":
			dv["status"], dv["skipReason"] = "skipped", "done_elsewhere"
		case "wrong_number":
			// The number is wrong: the item stays to contact, flagged, until someone fixes the number.
		default:
			if d.text("status") == "open" {
				dv["status"] = "contacted"
			}
		}
		if dv["status"] == "booked" && !d.has("appointmentId") && next == "" {
			return nil, refuse("nextActionAt", "Set the date it is booked for.")
		}
		clean(dv)
		for _, k := range []string{"claimedBy", "claimedUntil"} {
			dv[k] = nil
		}
		// Goes through the due item's own rules: a skip plans the next occurrence.
		if _, err := h.updateValues(ctx, tx, ws, specFor("due_items"), due, actorInfo{Kind: "system", Source: "call"}, dv, nil, nil); err != nil {
			return nil, err
		}
	}
	if len(set) == 0 {
		return c, nil
	}
	return h.updateValues(ctx, tx, ws, specFor("call_logs"), c.uuid(), systemActor(sourceCare), set, nil, nil)
}

// ---- the Upcoming page ----

func (h *Handler) careRoutes(r chi.Router) {
	r.Get("/upcoming/summary", h.handleUpcomingSummary)
	r.Get("/upcoming/list", h.handleUpcomingList)
	r.Post("/upcoming/{id}/claim", h.handleClaimDue)
}

type window struct {
	Key, Label string
	From, To   int // days from local today; To exclusive; From -100000 = no start
}

var windows = []window{{"overdue", "Overdue", -100000, 0}, {"today", "Today", 0, 1}, {"next3", "Next 3 days", 0, 3},
	{"next7", "Next 7 days", 0, 7}, {"next10", "Next 10 days", 0, 10}, {"next30", "Next 30 days", 0, 30}}

// upcomingWhere is the one predicate both the tiles and the list use.
func (h *Handler) upcomingWhere(r *http.Request, b *sqlBuilder) (string, error) {
	sc := scopeFrom(r.Context())
	if !sc.Can("due_items", "read") {
		return "", errForbidden
	}
	env := h.envFor(r.Context(), sc.WS, actor(r))
	loc, _ := time.LoadLocation(env.TZ)
	day := time.Now().In(loc).Format("2006-01-02")
	where := ` WHERE t.workspace_id = ` + b.arg(sc.WS) + ` AND t.object_key = 'due_items' AND t.deleted_at IS NULL`
	if own := sc.OwnersFor("due_items", actor(r)); own != nil {
		where += ` AND t.owner_id = ANY(` + b.arg(own) + `::uuid[])`
	}
	q := r.URL.Query()
	switch q.Get("scope") {
	case "mine":
		where += ` AND t.owner_id = ` + b.arg(actor(r))
	case "team":
		where += ` AND t.owner_id = ANY(` + b.arg(append([]uuid.UUID{actor(r)}, env.Team...)) + `::uuid[])`
	}
	if k := q.Get("kind"); k != "" {
		where += ` AND t.custom->>'kind' = ` + b.arg(k)
	}
	d := `NULLIF(t.custom->>'dueDate', '')::date`
	// The day is bound only when the window uses it (an unused argument is an error).
	todayArg := ""
	todaySQL := func() string {
		if todayArg == "" {
			todayArg = b.arg(day) + `::date`
		}
		return todayArg
	}
	switch w := q.Get("window"); w {
	case "uncontacted":
		where += ` AND t.status = 'open' AND COALESCE(t.custom->>'lastContactAt', '') = '' AND ` + d + ` < ` + todaySQL() + ` + 30`
	case "callback":
		where += ` AND t.status IN ('open', 'contacted') AND NULLIF(t.custom->>'nextActionAt', '')::date <= ` + todaySQL()
	case "booked":
		where += ` AND t.status = 'booked'`
	case "done":
		where += ` AND t.status = 'done' AND NULLIF(t.custom->>'doneOn', '')::date > ` + todaySQL() + ` - 30`
	default:
		found := false
		for _, x := range windows {
			if x.Key == w {
				found = true
				where += ` AND t.status IN ('open', 'contacted', 'booked')`
				if x.From > -100000 {
					where += ` AND ` + d + ` >= ` + todaySQL() + ` + ` + strconv.Itoa(x.From)
				}
				where += ` AND ` + d + ` < ` + todaySQL() + ` + ` + strconv.Itoa(x.To)
			}
		}
		if !found {
			where += ` AND t.status IN ('open', 'contacted', 'booked')`
		}
	}
	return where, nil
}

type upcomingTile struct {
	Key       string `json:"key"`
	Label     string `json:"label"`
	Items     int    `json:"items"`
	Customers int    `json:"customers"`
	Range     string `json:"range"`
}

func (h *Handler) handleUpcomingSummary(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	env := h.envFor(r.Context(), sc.WS, actor(r))
	loc, _ := time.LoadLocation(env.TZ)
	now := time.Now().In(loc)
	out := []upcomingTile{}
	keys := []window{}
	keys = append(keys, windows...)
	keys = append(keys, window{"uncontacted", "Not contacted yet", 0, 0}, window{"callback", "Call back today", 0, 0}, window{"booked", "Booked", 0, 0},
		window{"done", "Done in the last 30 days", 0, 0})
	for _, x := range keys {
		q := r.URL.Query()
		q.Set("window", x.Key)
		r2 := r.Clone(r.Context())
		r2.URL.RawQuery = q.Encode()
		b := &sqlBuilder{}
		where, err := h.upcomingWhere(r2, b)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		t := upcomingTile{Key: x.Key, Label: x.Label}
		if err := h.store.Pool.QueryRow(r.Context(), `SELECT count(*), count(DISTINCT COALESCE(NULLIF(t.custom->>'contactId', ''), t.custom->>'accountId', t.id::text))
			FROM crm.object_records t`+where, b.args...).Scan(&t.Items, &t.Customers); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		switch {
		case x.Key == "overdue":
			t.Range = "before " + now.Format("2 Jan")
		case x.To > 0:
			t.Range = now.Format("2 Jan") + " – " + now.AddDate(0, 0, x.To-1).Format("2 Jan")
		}
		out = append(out, t)
	}
	var missing int
	_ = h.store.Pool.QueryRow(r.Context(), `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'care_schedules'
		AND deleted_at IS NULL AND status = 'active' AND COALESCE(custom->>'nextDueDate', '') = ''`, sc.WS).Scan(&missing)
	respond(w, r, http.StatusOK, map[string]any{"tiles": out, "missingDueDate": missing, "timezone": env.TZ, "today": now.Format("2006-01-02"),
		"note": "Windows overlap (Today is inside Next 3 days), so don't add them up. Items counts each due item; Customers counts each person once."}, nil)
}

type upcomingRow struct {
	ID           string     `json:"id"`
	Code         string     `json:"code"`
	Title        string     `json:"title"`
	Status       string     `json:"status"`
	Kind         string     `json:"kind"`
	DueDate      string     `json:"dueDate"`
	DaysLeft     int        `json:"daysLeft"`
	ContactID    string     `json:"contactId,omitempty"`
	Customer     string     `json:"customer"`
	Phone        string     `json:"phone,omitempty"`
	AccountID    string     `json:"accountId,omitempty"`
	AssetID      string     `json:"assetId,omitempty"`
	Asset        string     `json:"asset,omitempty"`
	Owner        string     `json:"owner,omitempty"`
	LastOutcome  string     `json:"lastOutcome,omitempty"`
	NextActionAt string     `json:"nextActionAt,omitempty"`
	ClaimedBy    string     `json:"claimedBy,omitempty"`
	ClaimedUntil *time.Time `json:"claimedUntil,omitempty"`
	Version      int        `json:"version"`
}

func (h *Handler) handleUpcomingList(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	b := &sqlBuilder{}
	where, err := h.upcomingWhere(r, b)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	env := h.envFor(r.Context(), sc.WS, actor(r))
	loc, _ := time.LoadLocation(env.TZ)
	day := time.Now().In(loc).Format("2006-01-02")
	var total int
	if err := h.store.Pool.QueryRow(r.Context(), `SELECT count(*) FROM crm.object_records t`+where, b.args...).Scan(&total); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("pageSize"))
	if limit < 1 || limit > 200 {
		limit = 50
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}
	dayArg := b.arg(day)
	rows, err := h.store.Pool.Query(r.Context(), `
		SELECT t.id::text, t.code, t.name, t.status, COALESCE(t.custom->>'kind', ''), COALESCE(t.custom->>'dueDate', ''),
		       COALESCE(NULLIF(t.custom->>'dueDate', '')::date - `+dayArg+`::date, 0),
		       COALESCE(t.custom->>'contactId', ''), COALESCE(NULLIF(trim(concat_ws(' ', c.first_name, c.last_name)), ''), a.name, ''),
		       COALESCE(NULLIF(c.mobile, ''), NULLIF(c.phone, ''), a.phone, ''), COALESCE(t.custom->>'accountId', ''),
		       COALESCE(t.custom->>'assetId', ''), COALESCE(ast.name, ''), COALESCE(o.display_name, ''),
		       COALESCE(t.custom->>'lastOutcome', ''), COALESCE(t.custom->>'nextActionAt', ''),
		       COALESCE(cl.display_name, ''), NULLIF(t.custom->>'claimedUntil', '')::timestamptz, t.version
		FROM crm.object_records t
		LEFT JOIN crm.contacts c ON c.id::text = t.custom->>'contactId' AND c.workspace_id = t.workspace_id
		LEFT JOIN crm.accounts a ON a.id::text = t.custom->>'accountId' AND a.workspace_id = t.workspace_id
		LEFT JOIN crm.object_records ast ON ast.id::text = t.custom->>'assetId' AND ast.workspace_id = t.workspace_id
		LEFT JOIN crm.identities o ON o.id = t.owner_id
		LEFT JOIN crm.identities cl ON cl.id::text = t.custom->>'claimedBy'
		`+where+` ORDER BY NULLIF(t.custom->>'dueDate', '')::date NULLS LAST, t.id
		LIMIT `+strconv.Itoa(limit)+` OFFSET `+strconv.Itoa((page-1)*limit), b.args...)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []upcomingRow{}
	for rows.Next() {
		var x upcomingRow
		if err := rows.Scan(&x.ID, &x.Code, &x.Title, &x.Status, &x.Kind, &x.DueDate, &x.DaysLeft, &x.ContactID, &x.Customer, &x.Phone, &x.AccountID,
			&x.AssetID, &x.Asset, &x.Owner, &x.LastOutcome, &x.NextActionAt, &x.ClaimedBy, &x.ClaimedUntil, &x.Version); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		if x.ClaimedUntil != nil && x.ClaimedUntil.Before(time.Now()) {
			x.ClaimedBy, x.ClaimedUntil = "", nil
		}
		out = append(out, x)
	}
	respond(w, r, http.StatusOK, map[string]any{"data": out, "total": total, "page": page, "pageSize": limit}, rows.Err())
}

// POST /upcoming/{id}/claim — "I'm calling this customer now" for 30 minutes. A second
// person gets 409 with who is calling.
func (h *Handler) handleClaimDue(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.Can("due_items", "update") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	me := actor(r)
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var by, until, status string
		var owner *uuid.UUID
		err := tx.QueryRow(r.Context(), `SELECT COALESCE(custom->>'claimedBy', ''), COALESCE(custom->>'claimedUntil', ''), status, owner_id
			FROM crm.object_records WHERE id = $1 AND workspace_id = $2 AND object_key = 'due_items' AND deleted_at IS NULL FOR UPDATE`, id, sc.WS).Scan(&by, &until, &status, &owner)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("record_not_found")
		}
		if err != nil {
			return err
		}
		if own := sc.OwnersFor("due_items", me); own != nil && (owner == nil || !contains(uuidStrings(own), owner.String())) {
			return shared.NotFound("record_not_found")
		}
		if status == "done" || status == "skipped" {
			return shared.NewError(http.StatusConflict, "finished", "This one is already finished.")
		}
		if t, err := time.Parse(time.RFC3339, until); err == nil && t.After(time.Now()) && by != "" && by != me.String() {
			var who string
			_ = tx.QueryRow(r.Context(), `SELECT display_name FROM crm.identities WHERE id::text = $1`, by).Scan(&who)
			return shared.NewError(http.StatusConflict, "claimed", fmt.Sprintf("%s is calling this customer right now (until %s).", who, t.In(time.Local).Format("15:04")))
		}
		_, err = tx.Exec(r.Context(), `UPDATE crm.object_records SET custom = custom || jsonb_build_object('claimedBy', $2::text, 'claimedUntil', $3::text), updated_at = now()
			WHERE id = $1`, id, me.String(), time.Now().Add(30*time.Minute).UTC().Format(time.RFC3339))
		return err
	})
	respond(w, r, http.StatusOK, map[string]any{"ok": true}, err)
}

func uuidStrings(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// sweepCare repairs schedules that are active but have nothing waiting (for example
// after an import), so every active schedule always has its next item.
func (h *Handler) sweepCare(ctx context.Context) {
	if specFor("care_schedules") == nil {
		return
	}
	rows, err := h.store.Pool.Query(ctx, `SELECT s.id, s.workspace_id FROM crm.object_records s WHERE s.object_key = 'care_schedules' AND s.deleted_at IS NULL
		AND s.status = 'active' AND COALESCE(s.custom->>'nextDueDate', '') <> ''
		AND NOT EXISTS (SELECT 1 FROM crm.object_records d WHERE d.object_key = 'due_items' AND d.deleted_at IS NULL AND d.custom->>'scheduleId' = s.id::text
		                AND d.status IN ('open', 'contacted', 'booked')) LIMIT 500`)
	if err != nil {
		return
	}
	type ref struct{ id, ws uuid.UUID }
	var list []ref
	for rows.Next() {
		var x ref
		if rows.Scan(&x.id, &x.ws) == nil {
			list = append(list, x)
		}
	}
	rows.Close()
	for _, x := range list {
		_ = h.store.WithTx(ctx, func(tx pgx.Tx) error {
			s, _, err := h.getRow(ctx, tx, x.ws, specFor("care_schedules"), x.id, nil)
			if err != nil {
				return err
			}
			return h.ensureDueItem(ctx, tx, x.ws, s)
		})
	}
}

var _ = strings.TrimSpace
