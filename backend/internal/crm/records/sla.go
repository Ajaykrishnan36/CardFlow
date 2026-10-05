package records

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SLA (D-115). A case gets two clocks — first response and resolution — from the SLA
// policy that applies to it:
//
//  1. the policy named on the case, else
//  2. the policy of the case's entitlement — or of the active entitlement of its contract
//     or account — else
//  3. the active policy for the case's priority, else the default policy.
//
// A policy can count business hours only (working days, a daily window, holidays, in the
// business's time zone). The clocks pause while the case waits for the customer
// ("Pending") and stop when it is answered / resolved. A sweep marks clocks that ran out
// as breached, flags the case (which the workflow engine sees as a normal record update)
// and escalates it when the policy says so.
//
//	GET /w/{code}/cases/{id}/sla

const sourceSLA = "sla"

// Hours are the times an SLA clock runs.
type Hours struct {
	BusinessOnly bool     `json:"businessOnly"`
	Start        string   `json:"start"` // "09:00"
	End          string   `json:"end"`   // "18:00"
	Days         []string `json:"days"`  // mon … sun
	Holidays     []string `json:"holidays"`
	TZ           string   `json:"tz"`
}

var dayKeys = [...]string{"sun", "mon", "tue", "wed", "thu", "fri", "sat"}

func parseClock(s string, def int) int {
	parts := strings.SplitN(strings.TrimSpace(s), ":", 2)
	if len(parts) != 2 {
		return def
	}
	hh, err1 := strconv.Atoi(parts[0])
	mm, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || hh < 0 || hh > 24 || mm < 0 || mm > 59 {
		return def
	}
	return hh*60 + mm
}

// window returns the working window of the day t falls on (in the policy's time zone), or
// ok=false when that day isn't worked.
func (h Hours) window(t time.Time) (from, to time.Time, ok bool) {
	loc, err := time.LoadLocation(h.TZ)
	if err != nil {
		loc = time.UTC
	}
	t = t.In(loc)
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	works := len(h.Days) == 0
	for _, d := range h.Days {
		if d == dayKeys[day.Weekday()] {
			works = true
		}
	}
	date := day.Format("2006-01-02")
	for _, hd := range h.Holidays {
		if hd == date {
			works = false
		}
	}
	start, end := parseClock(h.Start, 9*60), parseClock(h.End, 18*60)
	if !works || end <= start {
		return day, day, false
	}
	return day.Add(time.Duration(start) * time.Minute), day.Add(time.Duration(end) * time.Minute), true
}

// Add returns the moment `minutes` of clock time after start.
func (h Hours) Add(start time.Time, minutes int) time.Time {
	if !h.BusinessOnly {
		return start.Add(time.Duration(minutes) * time.Minute)
	}
	left := time.Duration(minutes) * time.Minute
	t := start
	for i := 0; i < 3700 && left > 0; i++ { // at most ~10 years of days
		from, to, ok := h.window(t)
		if ok && t.Before(to) {
			if t.Before(from) {
				t = from
			}
			if room := to.Sub(t); room >= left {
				return t.Add(left)
			} else {
				left -= room
			}
		}
		// The start of the next day.
		loc := from.Location()
		next := time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
		t = next
	}
	return t
}

// Between is the clock time from a to b (0 when b is not after a).
func (h Hours) Between(a, b time.Time) time.Duration {
	if !b.After(a) {
		return 0
	}
	if !h.BusinessOnly {
		return b.Sub(a)
	}
	var total time.Duration
	t := a
	for i := 0; i < 3700 && t.Before(b); i++ {
		from, to, ok := h.window(t)
		if ok {
			lo, hi := from, to
			if t.After(lo) {
				lo = t
			}
			if b.Before(hi) {
				hi = b
			}
			if hi.After(lo) {
				total += hi.Sub(lo)
			}
		}
		loc := from.Location()
		t = time.Date(from.Year(), from.Month(), from.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, 1)
	}
	return total
}

type slaPolicy struct {
	ID            uuid.UUID
	Name          string
	FirstResponse int
	Resolution    int
	AutoEscalate  bool
	Hours         Hours
}

func intOf(v any) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	}
	return 0
}

func (h *Handler) loadPolicy(ctx context.Context, q querier, ws, id uuid.UUID) (*slaPolicy, error) {
	var name, tz string
	var raw []byte
	err := q.QueryRow(ctx, `SELECT p.name, p.custom, w.timezone FROM crm.object_records p JOIN crm.workspaces w ON w.id = p.workspace_id
		WHERE p.id = $1 AND p.workspace_id = $2 AND p.object_key = 'sla_policies' AND p.deleted_at IS NULL AND COALESCE(p.status, 'active') = 'active'`, id, ws).
		Scan(&name, &raw, &tz)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	c := map[string]any{}
	_ = json.Unmarshal(raw, &c)
	p := &slaPolicy{ID: id, Name: name, FirstResponse: intOf(c["firstResponseMinutes"]), Resolution: intOf(c["resolutionMinutes"])}
	p.AutoEscalate, _ = c["autoEscalate"].(bool)
	p.Hours.TZ = tz
	p.Hours.BusinessOnly, _ = c["businessHoursOnly"].(bool)
	p.Hours.Start, _ = c["businessStart"].(string)
	p.Hours.End, _ = c["businessEnd"].(string)
	if days, ok := c["workDays"].([]any); ok {
		for _, d := range days {
			if s, ok := d.(string); ok {
				p.Hours.Days = append(p.Hours.Days, s)
			}
		}
	}
	if p.Hours.BusinessOnly && len(p.Hours.Days) == 0 {
		p.Hours.Days = []string{"mon", "tue", "wed", "thu", "fri"}
	}
	if hs, ok := c["holidays"].(string); ok {
		for _, part := range strings.FieldsFunc(hs, func(r rune) bool { return r == ',' || r == '\n' || r == ' ' || r == ';' }) {
			if _, err := time.Parse("2006-01-02", part); err == nil {
				p.Hours.Holidays = append(p.Hours.Holidays, part)
			}
		}
	}
	if p.FirstResponse <= 0 && p.Resolution <= 0 {
		return nil, nil
	}
	return p, nil
}

// resolvePolicy picks the policy for a case and, on the way, the entitlement it came from.
func (h *Handler) resolvePolicy(ctx context.Context, tx pgx.Tx, ws uuid.UUID, c *Row) (*slaPolicy, uuid.UUID, error) {
	if id := c.id("slaPolicyId"); id != uuid.Nil {
		p, err := h.loadPolicy(ctx, tx, ws, id)
		if err != nil || p != nil {
			return p, c.id("entitlementId"), err
		}
	}
	// The entitlement on the case, or the active one of its contract / account today.
	entitlement := c.id("entitlementId")
	var policyID *uuid.UUID
	today := time.Now().Format("2006-01-02")
	err := tx.QueryRow(ctx, `
		SELECT e.id, NULLIF(e.custom->>'slaPolicyId', '')::uuid FROM crm.object_records e
		WHERE e.workspace_id = $1 AND e.object_key = 'entitlements' AND e.deleted_at IS NULL
		  AND (e.id = $2 OR ($2 = '00000000-0000-0000-0000-000000000000'::uuid AND COALESCE(e.status, 'active') = 'active'
		       AND (COALESCE(e.custom->>'startDate', '') = '' OR e.custom->>'startDate' <= $5)
		       AND (COALESCE(e.custom->>'endDate', '') = '' OR e.custom->>'endDate' >= $5)
		       AND (($3 <> '' AND e.custom->>'contractId' = $3) OR ($4 <> '' AND e.custom->>'accountId' = $4))))
		ORDER BY (e.id = $2) DESC, ($3 <> '' AND e.custom->>'contractId' = $3) DESC, e.created_at DESC LIMIT 1`,
		ws, entitlement, c.text("contractId"), c.text("accountId"), today).Scan(&entitlement, &policyID)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, uuid.Nil, err
	}
	if err == nil && policyID != nil {
		p, err := h.loadPolicy(ctx, tx, ws, *policyID)
		if err != nil || p != nil {
			return p, entitlement, err
		}
	}
	if errors.Is(err, pgx.ErrNoRows) {
		entitlement = uuid.Nil
	}
	// By priority; else the default; else a policy for any priority.
	var id uuid.UUID
	err = tx.QueryRow(ctx, `
		SELECT p.id FROM crm.object_records p
		WHERE p.workspace_id = $1 AND p.object_key = 'sla_policies' AND p.deleted_at IS NULL AND COALESCE(p.status, 'active') = 'active'
		  AND (($2 <> '' AND p.custom->>'priority' = $2) OR COALESCE(p.custom->>'isDefault', 'false') = 'true'
		       OR COALESCE(p.custom->>'priority', 'any') IN ('any', ''))
		ORDER BY ($2 <> '' AND p.custom->>'priority' = $2) DESC, (COALESCE(p.custom->>'isDefault', 'false') = 'true') DESC, p.created_at LIMIT 1`,
		ws, c.text("priority")).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, entitlement, nil
	}
	if err != nil {
		return nil, uuid.Nil, err
	}
	p, err := h.loadPolicy(ctx, tx, ws, id)
	return p, entitlement, err
}

var caseOpen = map[string]bool{"new": true, "assigned": true, "working": true, "pending": true, "escalated": true}

// caseSaved starts, pauses, resumes and stops a case's SLA clocks as the case changes.
func (h *Handler) caseSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, op string, before, after *Row) (*Row, error) {
	id := after.uuid()
	now := time.Now()
	set := map[string]any{}
	status := after.text("status")
	switch op {
	case "delete", "restore":
		return after, nil
	case "create":
		// A case always has a status: the clocks are driven by it.
		if status == "" {
			set["status"] = "new"
		}
		policy, entitlement, err := h.resolvePolicy(ctx, tx, ws, after)
		if err != nil {
			return nil, err
		}
		if entitlement != uuid.Nil && after.id("entitlementId") == uuid.Nil {
			set["entitlementId"] = entitlement.String()
		}
		if policy != nil {
			if err := h.startTimers(ctx, tx, ws, id, policy, now, set); err != nil {
				return nil, err
			}
		}
	case "update":
		was := before.text("status")
		if was == "" {
			was = "new"
		}
		// A new priority (or a policy picked by hand) retargets clocks that are still running.
		if before.text("priority") != after.text("priority") || before.text("slaPolicyId") != after.text("slaPolicyId") ||
			before.text("entitlementId") != after.text("entitlementId") {
			if before.text("slaPolicyId") == after.text("slaPolicyId") && before.text("priority") != after.text("priority") && before.text("entitlementId") == after.text("entitlementId") {
				// The policy on the case came from its old priority: look again.
				after.Values["slaPolicyId"] = nil
			}
			policy, _, err := h.resolvePolicy(ctx, tx, ws, after)
			if err != nil {
				return nil, err
			}
			if policy != nil {
				if err := h.retargetTimers(ctx, tx, ws, id, policy, set); err != nil {
					return nil, err
				}
			}
		}
		if was != status {
			// First response: the case left "New".
			if was == "new" && status != "new" && after.text("firstRespondedAt") == "" {
				if _, err := tx.Exec(ctx, `UPDATE crm.sla_timers SET completed_at = $3 WHERE case_id = $1 AND workspace_id = $2 AND milestone = 'first_response' AND completed_at IS NULL`,
					id, ws, now); err != nil {
					return nil, err
				}
				set["firstRespondedAt"] = now.UTC().Format(time.RFC3339)
			}
			switch {
			case status == "pending":
				if _, err := tx.Exec(ctx, `UPDATE crm.sla_timers SET paused_at = $3 WHERE case_id = $1 AND workspace_id = $2 AND completed_at IS NULL AND paused_at IS NULL`,
					id, ws, now); err != nil {
					return nil, err
				}
			case was == "pending":
				if err := h.resumeTimers(ctx, tx, ws, id, now, set); err != nil {
					return nil, err
				}
			}
			switch {
			case status == "resolved" || status == "closed":
				if _, err := tx.Exec(ctx, `UPDATE crm.sla_timers SET completed_at = $3, paused_at = NULL WHERE case_id = $1 AND workspace_id = $2 AND completed_at IS NULL`,
					id, ws, now); err != nil {
					return nil, err
				}
				if status == "closed" && after.text("closedAt") == "" {
					set["closedAt"] = now.UTC().Format(time.RFC3339)
				}
			case caseOpen[status] && (was == "resolved" || was == "closed"):
				// Reopened: the resolution clock runs again from where it stopped.
				if _, err := tx.Exec(ctx, `UPDATE crm.sla_timers SET completed_at = NULL WHERE case_id = $1 AND workspace_id = $2 AND milestone = 'resolution'`, id, ws); err != nil {
					return nil, err
				}
			}
			if status == "escalated" && after.text("escalatedAt") == "" {
				set["escalatedAt"] = now.UTC().Format(time.RFC3339)
			}
		}
	}
	h.wakeSweep()
	if len(set) == 0 {
		return after, nil
	}
	return h.updateValues(ctx, tx, ws, specFor("cases"), id, systemActor(sourceSLA), set, nil, nil)
}

func (h *Handler) startTimers(ctx context.Context, tx pgx.Tx, ws, caseID uuid.UUID, p *slaPolicy, start time.Time, set map[string]any) error {
	hours, _ := json.Marshal(p.Hours)
	set["slaPolicyId"] = p.ID.String()
	for milestone, minutes := range map[string]int{"first_response": p.FirstResponse, "resolution": p.Resolution} {
		if minutes <= 0 {
			continue
		}
		due := p.Hours.Add(start, minutes)
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.sla_timers (workspace_id, case_id, policy_id, milestone, target_minutes, hours, started_at, due_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (case_id, milestone) DO NOTHING`, ws, caseID, p.ID, milestone, minutes, hours, start, due); err != nil {
			return err
		}
		if milestone == "first_response" {
			set["firstResponseDueAt"] = due.UTC().Format(time.RFC3339)
		} else {
			set["slaDueAt"] = due.UTC().Format(time.RFC3339)
		}
	}
	return nil
}

// retargetTimers applies another policy to the clocks that haven't stopped. A clock keeps
// the time already used: its new due moment is counted from when it started.
func (h *Handler) retargetTimers(ctx context.Context, tx pgx.Tx, ws, caseID uuid.UUID, p *slaPolicy, set map[string]any) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.sla_timers WHERE case_id = $1 AND workspace_id = $2)`, caseID, ws).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		var created time.Time
		if err := tx.QueryRow(ctx, `SELECT created_at FROM crm.object_records WHERE id = $1 AND workspace_id = $2`, caseID, ws).Scan(&created); err != nil {
			return err
		}
		return h.startTimers(ctx, tx, ws, caseID, p, created, set)
	}
	hours, _ := json.Marshal(p.Hours)
	set["slaPolicyId"] = p.ID.String()
	rows, err := tx.Query(ctx, `SELECT milestone, started_at, paused_seconds FROM crm.sla_timers WHERE case_id = $1 AND workspace_id = $2 AND completed_at IS NULL`, caseID, ws)
	if err != nil {
		return err
	}
	type timer struct {
		milestone string
		started   time.Time
		paused    int
	}
	var list []timer
	for rows.Next() {
		var t timer
		if err := rows.Scan(&t.milestone, &t.started, &t.paused); err != nil {
			rows.Close()
			return err
		}
		list = append(list, t)
	}
	rows.Close()
	for _, t := range list {
		minutes := p.Resolution
		if t.milestone == "first_response" {
			minutes = p.FirstResponse
		}
		if minutes <= 0 {
			continue
		}
		due := p.Hours.Add(t.started, minutes).Add(time.Duration(t.paused) * time.Second)
		if _, err := tx.Exec(ctx, `UPDATE crm.sla_timers SET policy_id = $3, target_minutes = $4, hours = $5, due_at = $6,
			breached_at = CASE WHEN $6 > now() THEN NULL ELSE breached_at END, warned_at = NULL
			WHERE case_id = $1 AND workspace_id = $2 AND milestone = $7`, caseID, ws, p.ID, minutes, hours, due, t.milestone); err != nil {
			return err
		}
		if t.milestone == "first_response" {
			set["firstResponseDueAt"] = due.UTC().Format(time.RFC3339)
		} else {
			set["slaDueAt"] = due.UTC().Format(time.RFC3339)
		}
	}
	return nil
}

// resumeTimers restarts paused clocks: what was left when the case started waiting is
// still left now.
func (h *Handler) resumeTimers(ctx context.Context, tx pgx.Tx, ws, caseID uuid.UUID, now time.Time, set map[string]any) error {
	rows, err := tx.Query(ctx, `SELECT id, milestone, hours, due_at, paused_at FROM crm.sla_timers
		WHERE case_id = $1 AND workspace_id = $2 AND paused_at IS NOT NULL AND completed_at IS NULL`, caseID, ws)
	if err != nil {
		return err
	}
	type timer struct {
		id        uuid.UUID
		milestone string
		hours     Hours
		due       time.Time
		paused    time.Time
	}
	var list []timer
	for rows.Next() {
		var t timer
		var raw []byte
		if err := rows.Scan(&t.id, &t.milestone, &raw, &t.due, &t.paused); err != nil {
			rows.Close()
			return err
		}
		_ = json.Unmarshal(raw, &t.hours)
		list = append(list, t)
	}
	rows.Close()
	for _, t := range list {
		left := t.hours.Between(t.paused, t.due)
		due := t.hours.Add(now, int(left.Minutes()+0.5))
		if _, err := tx.Exec(ctx, `UPDATE crm.sla_timers SET paused_at = NULL, due_at = $2, paused_seconds = paused_seconds + $3 WHERE id = $1`,
			t.id, due, int(now.Sub(t.paused).Seconds())); err != nil {
			return err
		}
		if t.milestone == "first_response" {
			set["firstResponseDueAt"] = due.UTC().Format(time.RFC3339)
		} else {
			set["slaDueAt"] = due.UTC().Format(time.RFC3339)
		}
	}
	return nil
}

// ---- reading the clocks ----

type SLATimer struct {
	Milestone        string     `json:"milestone"`
	TargetMinutes    int        `json:"targetMinutes"`
	StartedAt        time.Time  `json:"startedAt"`
	DueAt            time.Time  `json:"dueAt"`
	PausedAt         *time.Time `json:"pausedAt,omitempty"`
	CompletedAt      *time.Time `json:"completedAt,omitempty"`
	BreachedAt       *time.Time `json:"breachedAt,omitempty"`
	State            string     `json:"state"` // running | paused | met | breached | missed
	ElapsedMinutes   int        `json:"elapsedMinutes"`
	RemainingMinutes int        `json:"remainingMinutes"` // negative = overdue by
	BusinessHours    bool       `json:"businessHours"`
}

func (h *Handler) handleCaseSLA(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	ctx := r.Context()
	spec := specFor("cases")
	if spec == nil || !sc.Enabled("cases") {
		shared.WriteError(w, r, shared.NotFound("object_not_found"))
		return
	}
	if !sc.Can("cases", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	c, _, err := h.getRow(ctx, h.store.Pool, sc.WS, spec, id, sc.OwnersFor("cases", actor(r)))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out := struct {
		Policy *LookupValue `json:"policy,omitempty"`
		Timers []SLATimer   `json:"timers"`
	}{Timers: []SLATimer{}}
	if l, ok := c.Lookups["slaPolicyId"]; ok {
		out.Policy = &l
	}
	rows, err := h.store.Pool.Query(ctx, `SELECT milestone, target_minutes, hours, started_at, due_at, paused_at, completed_at, breached_at
		FROM crm.sla_timers WHERE case_id = $1 AND workspace_id = $2 ORDER BY milestone`, id, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	now := time.Now()
	for rows.Next() {
		var t SLATimer
		var raw []byte
		var hours Hours
		if err := rows.Scan(&t.Milestone, &t.TargetMinutes, &raw, &t.StartedAt, &t.DueAt, &t.PausedAt, &t.CompletedAt, &t.BreachedAt); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		_ = json.Unmarshal(raw, &hours)
		t.BusinessHours = hours.BusinessOnly
		// "Now" for the clock: when it stopped, when it paused, or the present.
		at := now
		switch {
		case t.CompletedAt != nil:
			at = *t.CompletedAt
			t.State = "met"
			if at.After(t.DueAt) {
				t.State = "missed"
			}
		case t.PausedAt != nil:
			at = *t.PausedAt
			t.State = "paused"
		case now.After(t.DueAt):
			t.State = "breached"
		default:
			t.State = "running"
		}
		if at.After(t.DueAt) {
			t.RemainingMinutes = -int(hours.Between(t.DueAt, at).Minutes())
		} else {
			t.RemainingMinutes = int(hours.Between(at, t.DueAt).Minutes())
		}
		t.ElapsedMinutes = t.TargetMinutes - t.RemainingMinutes
		out.Timers = append(out.Timers, t)
	}
	shared.WriteJSON(w, http.StatusOK, out)
}

// ---- the sweep: breaches, contracts running out, invoices past due ----

// StartSweeps runs the time-based checks (D-115, D-116) until ctx ends. It doesn't poll on
// a short timer (that would keep a serverless database awake around the clock): after
// each pass it sleeps until the next SLA clock is due — at most an hour, so contracts and
// invoices are looked at hourly — and a change to a case wakes it early.
func (h *Handler) StartSweeps(ctx context.Context) {
	h.sweepWake = make(chan struct{}, 1)
	go func() {
		for {
			h.Sweep(ctx)
			wait := time.Hour
			var next *time.Time
			if err := h.store.Pool.QueryRow(ctx, `
				SELECT min(CASE WHEN warned_at IS NULL THEN started_at + (due_at - started_at) * 0.8 ELSE due_at END)
				FROM crm.sla_timers WHERE completed_at IS NULL AND breached_at IS NULL AND paused_at IS NULL`).Scan(&next); err == nil && next != nil {
				if d := time.Until(*next) + 2*time.Second; d < wait {
					wait = d
				}
			}
			if wait < 20*time.Second {
				wait = 20 * time.Second
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(wait):
			case <-h.sweepWake:
				// Let the transaction that woke us commit.
				select {
				case <-ctx.Done():
					return
				case <-time.After(2 * time.Second):
				}
			}
		}
	}()
}

// wakeSweep tells the sweep that SLA clocks changed.
func (h *Handler) wakeSweep() {
	if h.sweepWake == nil {
		return
	}
	select {
	case h.sweepWake <- struct{}{}:
	default:
	}
}

// Sweep runs one pass of every time-based check.
func (h *Handler) Sweep(ctx context.Context) {
	conn, err := h.store.Pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()
	var got bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(7203121)`).Scan(&got); err != nil || !got {
		return
	}
	defer func() { _, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock(7203121)`) }()
	for name, fn := range map[string]func(context.Context) error{
		"sla": h.sweepSLA, "contracts": h.sweepContracts, "invoices": h.sweepInvoices,
	} {
		if err := fn(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("crm: sweep failed", "check", name, "error", err)
		}
	}
	h.bus.Kick()
}

func (h *Handler) sweepSLA(ctx context.Context) error {
	if specFor("cases") == nil {
		return nil
	}
	type hit struct {
		timer, ws, caseID uuid.UUID
		policy            *uuid.UUID
		milestone         string
		breached          bool
	}
	rows, err := h.store.Pool.Query(ctx, `
		SELECT t.id, t.workspace_id, t.case_id, t.policy_id, t.milestone, t.due_at <= now()
		FROM crm.sla_timers t
		WHERE t.completed_at IS NULL AND t.breached_at IS NULL AND t.paused_at IS NULL
		  AND (t.due_at <= now() OR (t.warned_at IS NULL AND now() >= t.started_at + (t.due_at - t.started_at) * 0.8))
		ORDER BY t.due_at LIMIT 500`)
	if err != nil {
		return err
	}
	var hits []hit
	for rows.Next() {
		var x hit
		if err := rows.Scan(&x.timer, &x.ws, &x.caseID, &x.policy, &x.milestone, &x.breached); err != nil {
			rows.Close()
			return err
		}
		hits = append(hits, x)
	}
	rows.Close()
	what := map[string]string{"first_response": "first response", "resolution": "resolution"}
	for _, x := range hits {
		err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
			c, _, err := h.getRow(ctx, tx, x.ws, specFor("cases"), x.caseID, nil)
			if err != nil {
				// The case is in the recycle bin or gone: the clock has nothing to time.
				_, err = tx.Exec(ctx, `UPDATE crm.sla_timers SET completed_at = now() WHERE id = $1`, x.timer)
				return err
			}
			owner := c.id("ownerId")
			link := "/crm/w/" + c.workspaceCode(ctx, tx) + "/cases/" + c.ID
			if !x.breached {
				if _, err := tx.Exec(ctx, `UPDATE crm.sla_timers SET warned_at = now() WHERE id = $1`, x.timer); err != nil {
					return err
				}
				if owner != uuid.Nil {
					h.notify(ctx, tx, x.ws, owner, "sla.warning", "SLA nearly due: "+c.Title, "The "+what[x.milestone]+" time for "+c.Code+" is almost up.", link, nil)
				}
				return nil
			}
			if _, err := tx.Exec(ctx, `UPDATE crm.sla_timers SET breached_at = now() WHERE id = $1`, x.timer); err != nil {
				return err
			}
			set := map[string]any{"slaBreached": true}
			escalate := false
			if x.policy != nil {
				if p, _ := h.loadPolicy(ctx, tx, x.ws, *x.policy); p != nil && p.AutoEscalate {
					escalate = true
				}
			}
			if st := c.text("status"); escalate && caseOpen[st] && st != "escalated" {
				set["status"] = "escalated"
				set["escalatedAt"] = time.Now().UTC().Format(time.RFC3339)
				if _, err := tx.Exec(ctx, `UPDATE crm.sla_timers SET escalated_at = now() WHERE id = $1`, x.timer); err != nil {
					return err
				}
			}
			// A normal record update by the system: timeline, audit and the workflow engine all see it.
			if _, err := h.updateValues(ctx, tx, x.ws, specFor("cases"), x.caseID, systemActor(sourceSLA), set, nil, nil); err != nil {
				return err
			}
			if err := insertActivity(ctx, tx, x.ws, "cases", x.caseID, "sla.breached", "SLA breached: "+what[x.milestone], map[string]any{"milestone": x.milestone, "escalated": escalate}, nil); err != nil {
				return err
			}
			if owner != uuid.Nil {
				h.notify(ctx, tx, x.ws, owner, "sla.breached", "SLA breached: "+c.Title, "The "+what[x.milestone]+" time for "+c.Code+" has passed.", link, nil)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// workspaceCode reads the business code of a record's workspace (for links in notifications).
func (r *Row) workspaceCode(ctx context.Context, q querier) string {
	var code string
	_ = q.QueryRow(ctx, `SELECT w.code FROM crm.object_records o JOIN crm.workspaces w ON w.id = o.workspace_id WHERE o.id = $1`, r.uuid()).Scan(&code)
	return code
}

// sweepContracts: an active contract inside its renewal notice becomes "Expiring" and its
// owner gets a renewal task; one past its end date becomes "Expired".
func (h *Handler) sweepContracts(ctx context.Context) error {
	spec := specFor("contracts")
	if spec == nil {
		return nil
	}
	rows, err := h.store.Pool.Query(ctx, `
		SELECT c.id, c.workspace_id, CASE WHEN c.custom->>'endDate' < to_char(now(), 'YYYY-MM-DD') THEN 'expired' ELSE 'expiring' END
		FROM crm.object_records c
		WHERE c.object_key = 'contracts' AND c.deleted_at IS NULL AND COALESCE(c.custom->>'endDate', '') ~ '^\d{4}-\d{2}-\d{2}$'
		  AND ((c.status IN ('active', 'expiring') AND c.custom->>'endDate' < to_char(now(), 'YYYY-MM-DD'))
		    OR (c.status = 'active' AND c.custom->>'endDate' <= to_char(now() + make_interval(days =>
		          CASE WHEN c.custom->>'renewalNoticeDays' ~ '^\d{1,4}$' THEN (c.custom->>'renewalNoticeDays')::int ELSE 30 END), 'YYYY-MM-DD')))
		LIMIT 500`)
	if err != nil {
		return err
	}
	type hit struct {
		id, ws uuid.UUID
		status string
	}
	var hits []hit
	for rows.Next() {
		var x hit
		if err := rows.Scan(&x.id, &x.ws, &x.status); err != nil {
			rows.Close()
			return err
		}
		hits = append(hits, x)
	}
	rows.Close()
	for _, x := range hits {
		err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
			c, err := h.updateValues(ctx, tx, x.ws, spec, x.id, systemActor("contracts"), map[string]any{"status": x.status}, nil, nil)
			if err != nil {
				return err
			}
			owner := c.id("ownerId")
			link := "/crm/w/" + c.workspaceCode(ctx, tx) + "/contracts/" + c.ID
			if x.status == "expired" {
				if owner != uuid.Nil {
					h.notify(ctx, tx, x.ws, owner, "contract.expired", "Contract expired: "+c.Title, c.Code+" ended on "+c.text("endDate")+".", link, nil)
				}
				return nil
			}
			if owner != uuid.Nil {
				h.notify(ctx, tx, x.ws, owner, "contract.expiring", "Contract expiring: "+c.Title, c.Code+" ends on "+c.text("endDate")+".", link, nil)
			}
			if tasks := specFor("tasks"); tasks != nil {
				values := map[string]any{"name": "Renew contract " + c.Title, "dueDate": c.text("endDate"), "status": "not_started", "contractId": c.ID}
				if acc := c.text("accountId"); acc != "" {
					values["accountId"] = acc
				}
				if owner != uuid.Nil {
					values["ownerId"] = owner.String()
				}
				if _, err := h.createRecord(ctx, tx, x.ws, tasks, systemActor("contracts"), values); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// sweepInvoices: a sent or part-paid invoice past its due date becomes "Overdue".
func (h *Handler) sweepInvoices(ctx context.Context) error {
	spec := specFor("invoices")
	if spec == nil {
		return nil
	}
	rows, err := h.store.Pool.Query(ctx, `
		SELECT i.id, i.workspace_id FROM crm.object_records i
		WHERE i.object_key = 'invoices' AND i.deleted_at IS NULL AND i.status IN ('sent', 'partially_paid')
		  AND COALESCE(i.custom->>'dueDate', '') ~ '^\d{4}-\d{2}-\d{2}$' AND i.custom->>'dueDate' < to_char(now(), 'YYYY-MM-DD') LIMIT 500`)
	if err != nil {
		return err
	}
	type hit struct{ id, ws uuid.UUID }
	var hits []hit
	for rows.Next() {
		var x hit
		if err := rows.Scan(&x.id, &x.ws); err != nil {
			rows.Close()
			return err
		}
		hits = append(hits, x)
	}
	rows.Close()
	for _, x := range hits {
		err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
			inv, err := h.updateValues(ctx, tx, x.ws, spec, x.id, systemActor(sourcePayments), map[string]any{"status": "overdue"}, nil, nil)
			if err != nil {
				return err
			}
			if owner := inv.id("ownerId"); owner != uuid.Nil {
				h.notify(ctx, tx, x.ws, owner, "invoice.overdue", "Invoice overdue: "+inv.Title, inv.Code+" was due on "+inv.text("dueDate")+".",
					"/crm/w/"+inv.workspaceCode(ctx, tx)+"/invoices/"+inv.ID, nil)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}
