package records

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Field service and scheduling (D-127).
//
//	case → work order (what to do, lines priced like any document) → appointment
//	     → a service resource (technician, crew, outside provider) → done → invoice
//
// A service resource has working hours, days off, skills and a capacity. An appointment
// with a resource is only saved if it fits: inside the resource's working hours, not on a
// day off or during an absence, the resource has the skills the service needs, and it
// isn't already booked (more bookings than its capacity). Free slots are worked out from
// the same rules, so what the slot list offers is what saving accepts.
//
//	GET  /w/{code}/scheduling/slots       ?serviceId=&date=YYYY-MM-DD&days=&resourceId=&durationMinutes=
//	POST /w/{code}/scheduling/book        {serviceId, resourceId?, start, accountId, contactId, workOrderId, caseId, name, bookingSource}
//	POST /w/{code}/appointments/{id}/reschedule   {start, resourceId?}
//	POST /w/{code}/cases/{id}/work-order
//	POST /w/{code}/work_orders/{id}/invoice
//	GET  /w/{code}/entitlements/{id}/usage

func (h *Handler) fieldServiceRoutes(r chi.Router) {
	r.Get("/scheduling/slots", h.handleSlots)
	r.Post("/scheduling/book", h.handleBook)
	r.Post("/appointments/{id}/reschedule", h.handleReschedule)
	r.Post("/cases/{id}/work-order", h.handleCaseWorkOrder)
	r.Post("/work_orders/{id}/invoice", h.handleWorkOrderInvoice)
	r.Get("/entitlements/{id}/usage", h.handleEntitlementUsage)
}

const sourceScheduling = "scheduling"

// apptBlocks: the appointment statuses that take up a resource's time.
const apptBlocks = `('scheduled', 'confirmed', 'in_progress')`

type resource struct {
	ID       uuid.UUID
	Name     string
	Active   bool
	Skills   map[string]bool
	Capacity int
	Hours    Hours
}

func skillSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == '\n' }) {
		if p := strings.ToLower(strings.TrimSpace(part)); p != "" {
			out[p] = true
		}
	}
	return out
}

func resourceFrom(id uuid.UUID, name, status, tz string, raw []byte) resource {
	c := map[string]any{}
	_ = json.Unmarshal(raw, &c)
	str := func(k string) string { s, _ := c[k].(string); return s }
	r := resource{ID: id, Name: name, Active: status != "inactive", Skills: skillSet(str("skills")), Capacity: intOf(c["capacity"])}
	if r.Capacity < 1 {
		r.Capacity = 1
	}
	r.Hours = Hours{BusinessOnly: true, Start: str("workStart"), End: str("workEnd"), TZ: tz}
	if r.Hours.Start == "" {
		r.Hours.Start = "09:00"
	}
	if r.Hours.End == "" {
		r.Hours.End = "18:00"
	}
	for d := range skillSet(str("workDays")) {
		if len(d) >= 3 {
			r.Hours.Days = append(r.Hours.Days, d[:3])
		}
	}
	if len(r.Hours.Days) == 0 {
		r.Hours.Days = []string{"mon", "tue", "wed", "thu", "fri", "sat"}
	}
	for _, part := range strings.FieldsFunc(str("holidays"), func(r rune) bool { return r == ',' || r == '\n' || r == ' ' || r == ';' }) {
		if _, err := time.Parse("2006-01-02", part); err == nil {
			r.Hours.Holidays = append(r.Hours.Holidays, part)
		}
	}
	return r
}

func (h *Handler) loadResources(ctx context.Context, q querier, ws uuid.UUID, only uuid.UUID, lock bool) ([]resource, error) {
	sql := `SELECT r.id, r.name, COALESCE(r.status, ''), w.timezone, r.custom FROM crm.object_records r JOIN crm.workspaces w ON w.id = r.workspace_id
		WHERE r.workspace_id = $1 AND r.object_key = 'service_resources' AND r.deleted_at IS NULL AND ($2::uuid IS NULL OR r.id = $2) ORDER BY r.name`
	if lock {
		sql += ` FOR UPDATE OF r`
	}
	rows, err := q.Query(ctx, sql, ws, nullUUID(only))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []resource
	for rows.Next() {
		var id uuid.UUID
		var name, status, tz string
		var raw []byte
		if err := rows.Scan(&id, &name, &status, &tz, &raw); err != nil {
			return nil, err
		}
		out = append(out, resourceFrom(id, name, status, tz, raw))
	}
	return out, rows.Err()
}

type busy struct{ from, to time.Time }

// busyTimes: when a resource can't take another job — its appointments and its absences.
func (h *Handler) busyTimes(ctx context.Context, q querier, ws, res uuid.UUID, from, to time.Time, except uuid.UUID) (appts, away []busy, err error) {
	rows, err := q.Query(ctx, `
		SELECT 'a', (custom->>'startsAt')::timestamptz, COALESCE(NULLIF(custom->>'endsAt', '')::timestamptz, (custom->>'startsAt')::timestamptz + interval '1 hour')
		FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'appointments' AND deleted_at IS NULL AND custom->>'resourceId' = $2
		  AND status IN `+apptBlocks+` AND id <> $5 AND COALESCE(custom->>'startsAt', '') <> ''
		  AND (custom->>'startsAt')::timestamptz < $4 AND COALESCE(NULLIF(custom->>'endsAt', '')::timestamptz, (custom->>'startsAt')::timestamptz + interval '1 hour') > $3
		UNION ALL
		SELECT 'x', (custom->>'startsAt')::timestamptz, (custom->>'endsAt')::timestamptz
		FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'resource_absences' AND deleted_at IS NULL AND custom->>'resourceId' = $2
		  AND COALESCE(custom->>'startsAt', '') <> '' AND COALESCE(custom->>'endsAt', '') <> ''
		  AND (custom->>'startsAt')::timestamptz < $4 AND (custom->>'endsAt')::timestamptz > $3`, ws, res.String(), from, to, except)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var b busy
		if err := rows.Scan(&kind, &b.from, &b.to); err != nil {
			return nil, nil, err
		}
		if kind == "a" {
			appts = append(appts, b)
		} else {
			away = append(away, b)
		}
	}
	return appts, away, rows.Err()
}

// fits reports why a resource can't take [from, to), or "" when it can.
func (r resource) fits(from, to time.Time, appts, away []busy) string {
	if !r.Active {
		return r.Name + " is inactive."
	}
	wFrom, wTo, ok := r.Hours.window(from)
	if !ok || from.Before(wFrom) || to.After(wTo) {
		return r.Name + " doesn't work at that time (" + r.Hours.Start + "–" + r.Hours.End + ", " + strings.Join(r.Hours.Days, " ") + ")."
	}
	for _, b := range away {
		if from.Before(b.to) && to.After(b.from) {
			return r.Name + " is away at that time."
		}
	}
	n := 0
	for _, b := range appts {
		if from.Before(b.to) && to.After(b.from) {
			n++
		}
	}
	if n >= r.Capacity {
		return r.Name + " is already booked at that time."
	}
	return ""
}

// serviceNeeds reads what a service asks of an appointment: how long, which skills.
func (h *Handler) serviceNeeds(ctx context.Context, q querier, ws, item uuid.UUID) (minutes int, skills map[string]bool, name string, err error) {
	if item == uuid.Nil {
		return 0, nil, "", nil
	}
	var raw []byte
	err = q.QueryRow(ctx, `SELECT name, custom FROM crm.object_records WHERE id = $1 AND workspace_id = $2 AND object_key = 'catalog_items' AND deleted_at IS NULL`, item, ws).Scan(&name, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil, "", shared.Validation(map[string]string{"itemId": "Choose a service of this business."})
	}
	c := map[string]any{}
	_ = json.Unmarshal(raw, &c)
	s, _ := c["skillsRequired"].(string)
	return intOf(c["durationMinutes"]), skillSet(s), name, err
}

func parseTime(v any) (time.Time, bool) {
	s, _ := v.(string)
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05", "2006-01-02T15:04", "2006-01-02 15:04"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// appointmentSaved fills in when an appointment ends and refuses one that doesn't fit its resource.
func (h *Handler) appointmentSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, op string, before, after *Row) (*Row, error) {
	if op != "create" && op != "update" && op != "restore" {
		return after, nil
	}
	start, ok := parseTime(after.Values["startsAt"])
	if !ok {
		return after, nil
	}
	set := map[string]any{}
	status := after.text("status")
	if status == "" {
		status = "scheduled"
		set["status"] = status
	}
	need, skills, _, err := h.serviceNeeds(ctx, tx, ws, after.id("itemId"))
	if err != nil {
		return nil, err
	}
	end, hasEnd := parseTime(after.Values["endsAt"])
	minutes := intOf(after.Values["durationMinutes"])
	moved := before != nil && before.text("startsAt") != after.text("startsAt") && before.text("endsAt") == after.text("endsAt")
	if !hasEnd || moved {
		// No end typed (or the start was moved and the end left behind): start + duration.
		if moved && hasEnd {
			if oldStart, ok := parseTime(before.Values["startsAt"]); ok && minutes <= 0 {
				minutes = int(end.Sub(oldStart).Minutes())
			}
		}
		if minutes <= 0 {
			minutes = need
		}
		if minutes <= 0 {
			minutes = 60
		}
		end = start.Add(time.Duration(minutes) * time.Minute)
		set["endsAt"] = end.UTC().Format(time.RFC3339)
	}
	if !end.After(start) {
		return nil, shared.Validation(map[string]string{"endsAt": "The end must be after the start."})
	}
	if d := int(end.Sub(start).Minutes()); intOf(after.Values["durationMinutes"]) != d {
		set["durationMinutes"] = float64(d)
	}
	blocks := status == "scheduled" || status == "confirmed" || status == "in_progress"
	timeChanged := before == nil || before.text("startsAt") != after.text("startsAt") || before.text("endsAt") != after.text("endsAt") ||
		before.text("resourceId") != after.text("resourceId") || before.text("assignedTo") != after.text("assignedTo") || before.text("status") != status || op == "restore"
	if blocks && timeChanged {
		if res := after.id("resourceId"); res != uuid.Nil {
			// Lock the resource row: two bookings for the same slot are checked one after the other.
			list, err := h.loadResources(ctx, tx, ws, res, true)
			if err != nil {
				return nil, err
			}
			if len(list) == 0 {
				return nil, shared.Validation(map[string]string{"resourceId": "Choose a service resource of this business."})
			}
			r := list[0]
			for s := range skills {
				if !r.Skills[s] {
					return nil, shared.Validation(map[string]string{"resourceId": r.Name + " doesn't have a skill this service needs: " + s + "."})
				}
			}
			appts, away, err := h.busyTimes(ctx, tx, ws, res, start, end, after.uuid())
			if err != nil {
				return nil, err
			}
			if why := r.fits(start, end, appts, away); why != "" {
				return nil, shared.NewError(http.StatusConflict, "schedule_conflict", why)
			}
		} else if person := after.text("assignedTo"); person != "" {
			// No resource record: at least the same person isn't booked twice.
			if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "appt:"+ws.String()+":"+person); err != nil {
				return nil, err
			}
			var clash string
			_ = tx.QueryRow(ctx, `SELECT code FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'appointments' AND deleted_at IS NULL AND id <> $2
				AND custom->>'assignedTo' = $3 AND status IN `+apptBlocks+` AND COALESCE(custom->>'startsAt', '') <> ''
				AND (custom->>'startsAt')::timestamptz < $5 AND COALESCE(NULLIF(custom->>'endsAt', '')::timestamptz, (custom->>'startsAt')::timestamptz + interval '1 hour') > $4 LIMIT 1`,
				ws, after.uuid(), person, start, end).Scan(&clash)
			if clash != "" {
				return nil, shared.NewError(http.StatusConflict, "schedule_conflict", "That person already has an appointment at that time ("+clash+").")
			}
		}
	}
	row := after
	if len(set) > 0 {
		if row, err = h.updateValues(ctx, tx, ws, specFor("appointments"), after.uuid(), systemActor(sourceScheduling), set, nil, nil); err != nil {
			return nil, err
		}
	}
	// An appointment for a work order schedules the work order.
	if wo := row.id("workOrderId"); wo != uuid.Nil && blocks && specFor("work_orders") != nil && (op == "create" || timeChanged) {
		var st string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(status, '') FROM crm.object_records WHERE id = $1 AND workspace_id = $2 AND object_key = 'work_orders' AND deleted_at IS NULL`, wo, ws).Scan(&st); err != nil {
			return nil, shared.Validation(map[string]string{"workOrderId": "Choose a work order of this business."})
		}
		v := map[string]any{"scheduledStart": start.UTC().Format(time.RFC3339), "scheduledEnd": end.UTC().Format(time.RFC3339)}
		if r := row.text("resourceId"); r != "" {
			v["resourceId"] = r
		}
		if st == "" || st == "new" || st == "planned" {
			v["status"] = "scheduled"
		}
		if _, err := h.updateValues(ctx, tx, ws, specFor("work_orders"), wo, systemActor(sourceScheduling), v, nil, nil); err != nil {
			return nil, err
		}
	}
	return row, nil
}

type Slot struct {
	ResourceID string    `json:"resourceId"`
	Resource   string    `json:"resource"`
	Start      time.Time `json:"start"`
	End        time.Time `json:"end"`
}

// freeSlots lists the times a service can be booked on the given days.
func (h *Handler) freeSlots(ctx context.Context, q querier, ws uuid.UUID, service, only uuid.UUID, day time.Time, days, minutes int) ([]Slot, int, error) {
	need, skills, _, err := h.serviceNeeds(ctx, q, ws, service)
	if err != nil {
		return nil, 0, err
	}
	if minutes <= 0 {
		minutes = need
	}
	if minutes <= 0 {
		minutes = 60
	}
	resources, err := h.loadResources(ctx, q, ws, only, false)
	if err != nil {
		return nil, 0, err
	}
	out := []Slot{}
	now := time.Now()
	length := time.Duration(minutes) * time.Minute
	for _, r := range resources {
		able := r.Active
		for s := range skills {
			able = able && r.Skills[s]
		}
		if !able {
			continue
		}
		loc, err := time.LoadLocation(r.Hours.TZ)
		if err != nil {
			loc = time.UTC
		}
		first := time.Date(day.Year(), day.Month(), day.Day(), 12, 0, 0, 0, loc)
		appts, away, err := h.busyTimes(ctx, q, ws, r.ID, first.Add(-24*time.Hour), first.AddDate(0, 0, days+1), uuid.Nil)
		if err != nil {
			return nil, 0, err
		}
		for d := 0; d < days; d++ {
			from, to, ok := r.Hours.window(first.AddDate(0, 0, d))
			if !ok {
				continue
			}
			// Slots start on the half hour.
			for t := from; !t.Add(length).After(to); t = t.Add(30 * time.Minute) {
				if t.Before(now) {
					continue
				}
				if r.fits(t, t.Add(length), appts, away) == "" {
					out = append(out, Slot{ResourceID: r.ID.String(), Resource: r.Name, Start: t.UTC(), End: t.Add(length).UTC()})
				}
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Start.Before(out[j].Start) })
	return out, minutes, nil
}

func (h *Handler) handleSlots(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if specFor("appointments") == nil || !sc.Can("appointments", "read") || !sc.Can("service_resources", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	q := r.URL.Query()
	service, _ := uuid.Parse(q.Get("serviceId"))
	only, _ := uuid.Parse(q.Get("resourceId"))
	day, err := time.Parse("2006-01-02", q.Get("date"))
	if err != nil {
		day = time.Now()
	}
	days, _ := strconv.Atoi(q.Get("days"))
	if days < 1 {
		days = 1
	}
	if days > 14 {
		days = 14
	}
	minutes, _ := strconv.Atoi(q.Get("durationMinutes"))
	slots, used, err := h.freeSlots(r.Context(), h.store.Pool, sc.WS, service, only, day, days, minutes)
	if len(slots) > 500 {
		slots = slots[:500]
	}
	respond(w, r, http.StatusOK, map[string]any{"data": slots, "durationMinutes": used}, err)
}

func (h *Handler) handleBook(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if specFor("appointments") == nil || !sc.Can("appointments", "create") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	var in struct {
		ServiceID     string `json:"serviceId"`
		ResourceID    string `json:"resourceId"`
		Start         string `json:"start"`
		Minutes       int    `json:"durationMinutes"`
		AccountID     string `json:"accountId"`
		ContactID     string `json:"contactId"`
		WorkOrderID   string `json:"workOrderId"`
		CaseID        string `json:"caseId"`
		Name          string `json:"name"`
		Location      string `json:"location"`
		BookingSource string `json:"bookingSource"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	me := actor(r)
	start, ok := parseTime(in.Start)
	if !ok {
		shared.WriteError(w, r, shared.Validation(map[string]string{"start": "Choose a time."}))
		return
	}
	// Every record named must be one of this business the caller may open.
	for object, raw := range map[string]string{"accounts": in.AccountID, "contacts": in.ContactID, "work_orders": in.WorkOrderID, "cases": in.CaseID} {
		if raw == "" {
			continue
		}
		id, err := uuid.Parse(raw)
		if err != nil || specFor(object) == nil || !sc.Can(object, "read") {
			shared.WriteError(w, r, shared.NotFound("record_not_found"))
			return
		}
		if _, _, err := h.getRow(ctx, h.store.Pool, sc.WS, specFor(object), id, sc.OwnersFor(object, me)); err != nil {
			shared.WriteError(w, r, shared.NotFound("record_not_found"))
			return
		}
	}
	service, _ := uuid.Parse(in.ServiceID)
	res, _ := uuid.Parse(in.ResourceID)
	a := actorFromRequest(r, "ui")
	var out *Row
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		need, _, serviceName, err := h.serviceNeeds(ctx, tx, sc.WS, service)
		if err != nil {
			return err
		}
		minutes := in.Minutes
		if minutes <= 0 {
			minutes = need
		}
		if minutes <= 0 {
			minutes = 60
		}
		if res == uuid.Nil {
			// No resource chosen: the first one that is free at that time.
			slots, _, err := h.freeSlots(ctx, tx, sc.WS, service, uuid.Nil, start, 1, minutes)
			if err != nil {
				return err
			}
			for _, s := range slots {
				if s.Start.Equal(start) {
					res, _ = uuid.Parse(s.ResourceID)
					break
				}
			}
			if res == uuid.Nil {
				return shared.NewError(http.StatusConflict, "schedule_conflict", "Nobody is free at that time. Pick another slot.")
			}
		}
		name := strings.TrimSpace(in.Name)
		if name == "" {
			name = serviceName
		}
		if name == "" {
			name = "Appointment"
		}
		v := map[string]any{"name": name, "startsAt": start.UTC().Format(time.RFC3339), "durationMinutes": float64(minutes), "resourceId": res.String(), "status": "scheduled"}
		for k, s := range map[string]string{"itemId": in.ServiceID, "accountId": in.AccountID, "contactId": in.ContactID, "workOrderId": in.WorkOrderID, "caseId": in.CaseID,
			"location": strings.TrimSpace(in.Location), "bookingSource": in.BookingSource} {
			if s != "" {
				v[k] = s
			}
		}
		out, err = h.createRecord(ctx, tx, sc.WS, specFor("appointments"), a, v)
		return err
	})
	if err == nil {
		h.bus.Kick()
	}
	respond(w, r, http.StatusCreated, out, err)
}

// handleReschedule books the new time as a new appointment and marks the old one
// "Rescheduled", so the history of what was promised stays.
func (h *Handler) handleReschedule(w http.ResponseWriter, r *http.Request) {
	sc, appt, err := h.documentScope(r, "appointments", "update")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in struct {
		Start      string `json:"start"`
		ResourceID string `json:"resourceId"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	start, ok := parseTime(in.Start)
	if !ok {
		shared.WriteError(w, r, shared.Validation(map[string]string{"start": "Choose a time."}))
		return
	}
	if st := appt.text("status"); st != "scheduled" && st != "confirmed" && st != "no_show" && st != "" {
		shared.WriteError(w, r, shared.NewError(http.StatusUnprocessableEntity, "not_reschedulable", "Only an appointment that hasn't happened can be rescheduled."))
		return
	}
	ctx := r.Context()
	a := actorFromRequest(r, "ui")
	var out *Row
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		// Free the old slot first, so the new time may overlap it.
		if _, err := h.updateValues(ctx, tx, sc.WS, specFor("appointments"), appt.uuid(), a, map[string]any{"status": "rescheduled"}, nil, nil); err != nil {
			return err
		}
		v := map[string]any{"name": appt.Title, "startsAt": start.UTC().Format(time.RFC3339), "rescheduledFrom": appt.ID, "status": "scheduled"}
		for _, k := range []string{"itemId", "accountId", "contactId", "resourceId", "assignedTo", "workOrderId", "caseId", "opportunityId", "location", "meetingLink", "timezone", "durationMinutes", "notes", "bookingSource", "reminderMinutes"} {
			if x := appt.Values[k]; x != nil && x != "" {
				v[k] = x
			}
		}
		if in.ResourceID != "" {
			v["resourceId"] = in.ResourceID
		}
		var err error
		out, err = h.createRecord(ctx, tx, sc.WS, specFor("appointments"), a, v)
		return err
	})
	if err == nil {
		h.bus.Kick()
	}
	respond(w, r, http.StatusCreated, out, err)
}

// ---------------------------------------------------------------- work orders

func (h *Handler) handleCaseWorkOrder(w http.ResponseWriter, r *http.Request) {
	sc, c, err := h.documentScope(r, "cases", "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if specFor("work_orders") == nil || !sc.Can("work_orders", "create") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	ctx := r.Context()
	a := actorFromRequest(r, "ui")
	v := map[string]any{"name": c.Title, "caseId": c.ID, "status": "new"}
	for _, k := range []string{"accountId", "contactId", "assetId", "contractId", "entitlementId", "priority"} {
		if s := c.text(k); s != "" {
			v[k] = s
		}
	}
	var out *Row
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var err error
		if out, err = h.createRecord(ctx, tx, sc.WS, specFor("work_orders"), a, v); err != nil {
			return err
		}
		return insertActivity(ctx, tx, sc.WS, "cases", c.uuid(), "work_order.created", "Work order "+out.Code, map[string]any{"workOrderId": out.ID}, a.ID)
	})
	if err == nil {
		h.bus.Kick()
	}
	respond(w, r, http.StatusCreated, out, err)
}

// workOrderSaved stamps when work really started and ended, and on completion counts the
// hours against the entitlement and notes the job on the asset.
func (h *Handler) workOrderSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, op string, before, after *Row) (*Row, error) {
	if op != "create" && op != "update" {
		return after, nil
	}
	status := after.text("status")
	was := ""
	if before != nil {
		was = before.text("status")
	}
	set := map[string]any{}
	now := time.Now().UTC()
	if status == "" {
		status = "new"
		set["status"] = status
	}
	if status == "in_progress" && was != "in_progress" && after.text("actualStart") == "" {
		set["actualStart"] = now.Format(time.RFC3339)
	}
	if status == "completed" && was != "completed" {
		if after.text("actualEnd") == "" {
			set["actualEnd"] = now.Format(time.RFC3339)
		}
		if after.text("actualStart") == "" {
			set["actualStart"] = now.Format(time.RFC3339)
		}
	}
	if s, ok := parseTime(after.Values["scheduledStart"]); ok {
		if e, ok := parseTime(after.Values["scheduledEnd"]); ok && !e.After(s) {
			return nil, shared.Validation(map[string]string{"scheduledEnd": "The end must be after the start."})
		}
	}
	row := after
	var err error
	if len(set) > 0 {
		if row, err = h.updateValues(ctx, tx, ws, specFor("work_orders"), after.uuid(), systemActor(sourceScheduling), set, nil, nil); err != nil {
			return nil, err
		}
	}
	if status == "completed" && was != "completed" {
		start, ok1 := parseTime(row.Values["actualStart"])
		end, ok2 := parseTime(row.Values["actualEnd"])
		if ent := row.id("entitlementId"); ent != uuid.Nil && ok1 && ok2 && end.After(start) {
			hours := float64(int(end.Sub(start).Minutes()*100/60)) / 100
			if hours > 0 {
				if err := h.useEntitlement(ctx, tx, ws, ent, "hours", hours, "work_orders", row.uuid(), row.Code, a.ID); err != nil {
					return nil, err
				}
			}
		}
		if asset := row.id("assetId"); asset != uuid.Nil && specFor("assets") != nil {
			if _, ok := specFor("assets").field("workOrderId"); ok {
				if _, err := h.updateValues(ctx, tx, ws, specFor("assets"), asset, systemActor(sourceScheduling), map[string]any{"workOrderId": row.ID}, nil, nil); err != nil {
					var ae *shared.Error
					if !errors.As(err, &ae) || ae.Status != http.StatusNotFound {
						return nil, err
					}
				}
			}
			if err := insertActivity(ctx, tx, ws, "assets", asset, "asset.serviced", "Serviced: "+row.Title, map[string]any{"workOrderId": row.ID, "code": row.Code}, a.ID); err != nil {
				return nil, err
			}
		}
	}
	return row, nil
}

// handleWorkOrderInvoice bills a work order: a draft invoice with the work order's lines
// exactly as priced.
func (h *Handler) handleWorkOrderInvoice(w http.ResponseWriter, r *http.Request) {
	sc, wo, err := h.documentScope(r, "work_orders", "update")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if !sc.Can("invoices", "create") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	ctx := r.Context()
	a := actorFromRequest(r, "ui")
	var out *Row
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var status, invoiced string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(status, ''), COALESCE(custom->>'invoiceId', '') FROM crm.object_records WHERE id = $1 AND workspace_id = $2 FOR UPDATE`, wo.uuid(), sc.WS).Scan(&status, &invoiced); err != nil {
			return err
		}
		if invoiced != "" {
			return shared.NewError(http.StatusConflict, "already_converted", "This work order was already invoiced.")
		}
		if status != "completed" {
			return shared.NewError(http.StatusUnprocessableEntity, "not_convertible", "Complete the work order before invoicing it.")
		}
		lines, err := h.storedLines(ctx, tx, sc.WS, "work_orders", wo.uuid())
		if err != nil {
			return err
		}
		if len(lines.Lines) == 0 {
			return shared.NewError(http.StatusUnprocessableEntity, "no_lines", "Add the services and parts used to the work order first.")
		}
		extra := map[string]any{"workOrderId": wo.ID, "status": "draft", "invoiceDate": time.Now().Format("2006-01-02"),
			"subtotal": lines.Subtotal.Float(), "discount": lines.Discount.Float(), "tax": lines.Tax.Float(), "total": lines.Total.Float()}
		if out, err = h.copyDocument(ctx, tx, sc, a, "work_orders", wo, "invoices", extra); err != nil {
			return err
		}
		_, err = h.updateValues(ctx, tx, sc.WS, specFor("work_orders"), wo.uuid(), systemActor(sourceScheduling), map[string]any{"invoiceId": out.ID}, nil, nil)
		return err
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.bus.Kick()
	shared.WriteJSON(w, http.StatusCreated, map[string]any{"object": "invoices", "id": out.ID, "code": out.Code})
}

// ---------------------------------------------------------------- entitlement usage

// useEntitlement records one use of an entitlement (a case, or hours of work) and keeps
// the entitlement's "used" numbers in step. One source counts once.
func (h *Handler) useEntitlement(ctx context.Context, tx pgx.Tx, ws, ent uuid.UUID, kind string, qty float64, object string, source uuid.UUID, note string, actor *uuid.UUID) error {
	if _, err := tx.Exec(ctx, `INSERT INTO crm.entitlement_usage (workspace_id, entitlement_id, kind, quantity, source_object, source_id, note, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) ON CONFLICT (entitlement_id, kind, source_object, source_id) DO UPDATE SET quantity = EXCLUDED.quantity`,
		ws, ent, kind, qty, object, source, note, actor); err != nil {
		return err
	}
	return h.refreshEntitlement(ctx, tx, ws, ent)
}

func (h *Handler) refreshEntitlement(ctx context.Context, tx pgx.Tx, ws, ent uuid.UUID) error {
	spec := specFor("entitlements")
	if _, ok := spec.field("casesUsed"); !ok {
		return nil
	}
	var cases, hours float64
	if err := tx.QueryRow(ctx, `SELECT COALESCE(sum(quantity) FILTER (WHERE kind = 'case'), 0)::float8, COALESCE(sum(quantity) FILTER (WHERE kind = 'hours'), 0)::float8
		FROM crm.entitlement_usage WHERE entitlement_id = $1 AND workspace_id = $2`, ent, ws).Scan(&cases, &hours); err != nil {
		return err
	}
	_, err := h.updateValues(ctx, tx, ws, spec, ent, systemActor(sourceScheduling), map[string]any{"casesUsed": cases, "hoursUsed": hours}, nil, nil)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	var ae *shared.Error
	if errors.As(err, &ae) && ae.Status == http.StatusNotFound {
		return nil // the entitlement is in the recycle bin
	}
	return err
}

// caseEntitlement counts a case against the entitlement it is handled under. When the
// allowance is used up the entitlement's policy decides: block the case, or let it
// through marked "over entitlement".
func (h *Handler) caseEntitlement(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, op string, before, after *Row) (*Row, error) {
	if specFor("entitlements") == nil {
		return after, nil
	}
	old := uuid.Nil
	if before != nil {
		old = before.id("entitlementId")
	}
	ent := after.id("entitlementId")
	if op == "delete" {
		ent = uuid.Nil
	}
	if op == "update" && old == ent {
		return after, nil
	}
	if op == "restore" {
		old = uuid.Nil
	}
	if old != uuid.Nil && old != ent {
		if _, err := tx.Exec(ctx, `DELETE FROM crm.entitlement_usage WHERE workspace_id = $1 AND entitlement_id = $2 AND kind = 'case' AND source_object = 'cases' AND source_id = $3`, ws, old, after.uuid()); err != nil {
			return nil, err
		}
		if err := h.refreshEntitlement(ctx, tx, ws, old); err != nil {
			return nil, err
		}
	}
	if ent == uuid.Nil {
		return after, nil
	}
	var included, used float64
	var policy, status, name string
	err := tx.QueryRow(ctx, `
		SELECT COALESCE(NULLIF(e.custom->>'casesIncluded', '')::numeric, 0)::float8, COALESCE(e.custom->>'overagePolicy', ''), COALESCE(e.status, ''), e.name,
		       COALESCE((SELECT sum(quantity) FROM crm.entitlement_usage u WHERE u.entitlement_id = e.id AND u.kind = 'case' AND NOT (u.source_object = 'cases' AND u.source_id = $3)), 0)::float8
		FROM crm.object_records e WHERE e.id = $1 AND e.workspace_id = $2 AND e.object_key = 'entitlements' AND e.deleted_at IS NULL FOR UPDATE`, ent, ws, after.uuid()).
		Scan(&included, &policy, &status, &name, &used)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.Validation(map[string]string{"entitlementId": "Choose an entitlement of this business."})
	}
	if err != nil {
		return nil, err
	}
	over := included > 0 && used >= included
	if over && policy == "block" && a.Source != sourceSLA {
		return nil, shared.Validation(map[string]string{"entitlementId": name + " includes " + strconv.FormatFloat(included, 'f', -1, 64) + " cases and all of them are used. Renew the entitlement, or handle this case without it."})
	}
	if err := h.useEntitlement(ctx, tx, ws, ent, "case", 1, "cases", after.uuid(), after.Code, a.ID); err != nil {
		return nil, err
	}
	if _, ok := specFor("cases").field("entitlementExceeded"); ok && after.flag("entitlementExceeded") != over {
		return h.updateValues(ctx, tx, ws, specFor("cases"), after.uuid(), systemActor(sourceSLA), map[string]any{"entitlementExceeded": over}, nil, nil)
	}
	return after, nil
}

func (h *Handler) handleEntitlementUsage(w http.ResponseWriter, r *http.Request) {
	sc, ent, err := h.documentScope(r, "entitlements", "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	type use struct {
		Kind     string    `json:"kind"`
		Quantity float64   `json:"quantity"`
		Object   string    `json:"object"`
		RecordID string    `json:"recordId"`
		Note     string    `json:"note"`
		At       time.Time `json:"at"`
	}
	rows, err := h.store.Pool.Query(r.Context(), `SELECT kind, quantity::float8, source_object, source_id::text, note, created_at FROM crm.entitlement_usage
		WHERE entitlement_id = $1 AND workspace_id = $2 ORDER BY created_at DESC LIMIT 500`, ent.uuid(), sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	list := []use{}
	var cases, hours float64
	for rows.Next() {
		var u use
		if err := rows.Scan(&u.Kind, &u.Quantity, &u.Object, &u.RecordID, &u.Note, &u.At); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		if u.Kind == "case" {
			cases += u.Quantity
		} else {
			hours += u.Quantity
		}
		list = append(list, u)
	}
	num := func(k string) float64 { f, _ := strconv.ParseFloat(numText(ent.Values[k]), 64); return f }
	remaining := func(included, used float64) *float64 {
		if included <= 0 {
			return nil // unlimited
		}
		v := included - used
		if v < 0 {
			v = 0
		}
		return &v
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": list,
		"cases":         map[string]any{"included": num("casesIncluded"), "used": cases, "remaining": remaining(num("casesIncluded"), cases)},
		"hours":         map[string]any{"included": num("hoursIncluded"), "used": hours, "remaining": remaining(num("hoursIncluded"), hours)},
		"overagePolicy": ent.text("overagePolicy")})
}
