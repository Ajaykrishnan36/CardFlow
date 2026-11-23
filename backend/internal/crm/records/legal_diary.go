package records

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The advocate's home (D-185): the court diary, what a service needs, asking the client for
// missing documents, and the reminders. All of it respects who may see each matter.

type diaryRow struct {
	ID          string `json:"id"`
	At          string `json:"at"`
	Status      string `json:"status"`
	Court       string `json:"court"`
	Hall        string `json:"hall"`
	MatterID    string `json:"matterId"`
	MatterCode  string `json:"matterCode"`
	Matter      string `json:"matter"`
	CaseNumber  string `json:"caseNumber"`
	Client      string `json:"client"`
	CaseType    string `json:"caseType"`
	Purpose     string `json:"purpose"`
	AdvocateID  string `json:"advocateId"`
	Advocate    string `json:"advocate"`
	LastOutcome string `json:"lastOutcome"`
	NotUpdated  bool   `json:"notUpdated"`
}

func visibleSQL(key string) string {
	return `($2::uuid[] IS NULL OR t.owner_id = ANY($2) OR ` + teamMemberSQL(specFor(key), "$2") + `)`
}

func (h *Handler) handleLegalDiary(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	spec := specFor("court_hearings")
	if spec == nil || !sc.Enabled("court_hearings") || !sc.Can("court_hearings", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	ctx := r.Context()
	now := time.Now().In(ist)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, ist)
	from, to := day, day.AddDate(0, 0, 8)
	qs := r.URL.Query()
	if t, err := time.ParseInLocation("2006-01-02", qs.Get("from"), ist); err == nil {
		from = t
	}
	if t, err := time.ParseInLocation("2006-01-02", qs.Get("to"), ist); err == nil {
		to = t.AddDate(0, 0, 1)
	}
	if !to.After(from) {
		shared.WriteError(w, r, shared.Validation(map[string]string{"to": "The end date is before the start date."}))
		return
	}
	owners := ownerFilter(r, spec)
	count := func(key, cond string, args ...any) int {
		var n int
		_ = h.store.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.object_records t WHERE t.workspace_id = $1 AND t.object_key = '`+key+`' AND t.deleted_at IS NULL AND `+visibleSQL(key)+` AND `+cond,
			append([]any{sc.WS, owners}, args...)...).Scan(&n)
		return n
	}
	at := `NULLIF(t.custom->>'hearingAt', '')::timestamptz`
	tiles := map[string]int{
		"today":      count("court_hearings", at+` >= $3 AND `+at+` < $4 AND t.status = 'scheduled'`, day, day.AddDate(0, 0, 1)),
		"next7":      count("court_hearings", at+` >= $3 AND `+at+` < $4 AND t.status = 'scheduled'`, day, day.AddDate(0, 0, 8)),
		"notUpdated": count("court_hearings", at+` < $3 AND t.status = 'scheduled'`, now),
		"overdue":    count("tasks", `COALESCE(t.custom->>'matterId', '') <> '' AND left(COALESCE(t.custom->>'dueDate', ''), 10) BETWEEN '0001-01-01' AND $3 AND COALESCE(t.status, '') NOT IN ('completed', 'cancelled')`, day.AddDate(0, 0, -1).Format("2006-01-02")),
		"missingDocs": count("legal_matters", `COALESCE(t.status, '') <> 'closed' AND EXISTS (SELECT 1 FROM crm.object_records q WHERE q.workspace_id = t.workspace_id AND q.object_key = 'client_requirements'
			AND q.deleted_at IS NULL AND q.custom->>'matterId' = t.id::text AND q.custom->>'need' = 'required' AND COALESCE(q.status, '') <> 'verified')`),
		"deadlines": count("legal_deadlines", `t.status = 'open' AND left(COALESCE(t.custom->>'dueDate', ''), 10) BETWEEN $3 AND $4`, day.Format("2006-01-02"), day.AddDate(0, 0, 30).Format("2006-01-02")),
	}
	if sc.Can("leads", "read") && ownerFilter(r, specFor("leads")) == nil {
		var n int
		_ = h.store.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.leads WHERE workspace_id = $1 AND deleted_at IS NULL AND owner_id IS NULL AND COALESCE(status, '') NOT IN ('converted', 'lost', 'not_interested')`, sc.WS).Scan(&n)
		tiles["unassigned"] = n
	}
	args := []any{sc.WS, owners, from, to}
	where := ""
	add := func(sql, v string) {
		if v != "" {
			args = append(args, v)
			where += " AND " + strings.ReplaceAll(sql, "?", fmt.Sprintf("$%d", len(args)))
		}
	}
	add(`t.custom->>'assignedAdvocateId' = ?`, qs.Get("advocate"))
	add(`(t.custom->>'courtId' = ? OR t.custom->>'courtName' = ?)`, qs.Get("court"))
	add(`m.custom->>'caseKind' = ?`, qs.Get("caseType"))
	add(`t.status = ?`, qs.Get("status"))
	range_ := `(` + at + ` >= $3 AND ` + at + ` < $4)`
	if qs.Get("notUpdated") == "1" {
		range_ = `(` + at + ` < now() AND t.status = 'scheduled' AND $3::timestamptz IS NOT NULL AND $4::timestamptz IS NOT NULL)`
	}
	rows, err := h.store.Pool.Query(ctx, `
		SELECT t.id::text, t.custom->>'hearingAt', COALESCE(t.status, ''), COALESCE(NULLIF(c.name, ''), t.custom->>'courtName', m.custom->>'courtName', ''), COALESCE(t.custom->>'courtRoom', ''),
		       COALESCE(m.id::text, ''), COALESCE(m.code, ''), COALESCE(m.name, ''), COALESCE(m.custom->>'courtCaseNumber', ''),
		       COALESCE(NULLIF(a.name, ''), trim(COALESCE(ct.first_name, '') || ' ' || COALESCE(ct.last_name, '')), ''), COALESCE(m.custom->>'caseKind', ''), COALESCE(t.custom->>'purpose', ''),
		       COALESCE(i.id::text, ''), COALESCE(i.display_name, ''),
		       COALESCE((SELECT COALESCE(NULLIF(p.custom->>'hearingOutcome', ''), p.custom->>'proceedings', '') FROM crm.object_records p WHERE p.workspace_id = t.workspace_id AND p.object_key = 'court_hearings'
		                 AND p.deleted_at IS NULL AND p.custom->>'matterId' = t.custom->>'matterId' AND p.status <> 'scheduled' AND p.id <> t.id ORDER BY p.custom->>'hearingAt' DESC LIMIT 1), '')
		FROM crm.object_records t
		LEFT JOIN crm.object_records m ON m.id::text = t.custom->>'matterId' AND m.workspace_id = t.workspace_id
		LEFT JOIN crm.object_records c ON c.id::text = t.custom->>'courtId' AND c.workspace_id = t.workspace_id
		LEFT JOIN crm.accounts a ON a.id::text = m.custom->>'accountId' AND a.workspace_id = t.workspace_id
		LEFT JOIN crm.contacts ct ON ct.id::text = m.custom->>'contactId' AND ct.workspace_id = t.workspace_id
		LEFT JOIN crm.identities i ON i.id::text = COALESCE(NULLIF(t.custom->>'assignedAdvocateId', ''), m.custom->>'primaryAdvocateId')
		WHERE t.workspace_id = $1 AND t.object_key = 'court_hearings' AND t.deleted_at IS NULL AND `+visibleSQL("court_hearings")+` AND `+range_+where+`
		ORDER BY `+at+` LIMIT 500`, args...)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []diaryRow{}
	for rows.Next() {
		var d diaryRow
		if err := rows.Scan(&d.ID, &d.At, &d.Status, &d.Court, &d.Hall, &d.MatterID, &d.MatterCode, &d.Matter, &d.CaseNumber, &d.Client, &d.CaseType, &d.Purpose, &d.AdvocateID, &d.Advocate, &d.LastOutcome); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		if t, ok := parseWhen(d.At); ok && t.Before(now) && d.Status == "scheduled" {
			d.NotUpdated = true
		}
		out = append(out, d)
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"tiles": tiles, "rows": out, "from": from.Format("2006-01-02"), "to": to.AddDate(0, 0, -1).Format("2006-01-02"), "timezone": "Asia/Kolkata"})
}

// GET /legal/services/{id} — what a service covers, what the client must give, and its price.
func (h *Handler) handleLegalService(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.Can("case_intakes", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	ctx := r.Context()
	name := h.labelOf(ctx, h.store.Pool, sc.WS, "catalog_items", id.String())
	if name == "" {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	list := func(key, kind string) []map[string]string {
		out := []map[string]string{}
		rows, err := h.store.Pool.Query(ctx, `SELECT name, COALESCE(custom->>'`+kind+`', ''), COALESCE(custom->>'instructions', custom->>'description', '') FROM crm.object_records
			WHERE workspace_id = $1 AND object_key = '`+key+`' AND deleted_at IS NULL AND COALESCE(status, 'active') = 'active' AND custom->>'serviceId' = $2 ORDER BY created_at`, sc.WS, id.String())
		if err != nil {
			return out
		}
		defer rows.Close()
		for rows.Next() {
			var n, k, d string
			if rows.Scan(&n, &k, &d) == nil {
				out = append(out, map[string]string{"name": n, "kind": k, "note": d})
			}
		}
		return out
	}
	var price *float64
	var book string
	var p float64
	if err := h.store.Pool.QueryRow(ctx, `SELECT e.unit_price::float8, COALESCE(b.name, '') FROM crm.price_book_entries e JOIN crm.object_records b ON b.id = e.price_book_id
		WHERE e.workspace_id = $1 AND e.catalog_item_id = $2 AND e.deleted_at IS NULL AND e.is_active AND e.valid_from <= current_date AND (e.valid_to IS NULL OR e.valid_to >= current_date)
		  AND b.deleted_at IS NULL AND COALESCE(b.status, 'active') = 'active' AND COALESCE(b.custom->>'accountId', '') = ''
		ORDER BY (b.custom->>'isDefault' = 'true') DESC NULLS LAST, e.valid_from DESC LIMIT 1`, sc.WS, id).Scan(&p, &book); err == nil {
		price = &p
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"name": name, "scope": list("service_scopes", "inclusion"), "documents": list("service_requirements", "need"), "price": price, "priceBook": book})
}

// POST /legal_matters/{id}/request-documents — marks what is missing as requested and gives
// the message to send. Nothing is sent from here: no WhatsApp or SMS provider is connected.
func (h *Handler) handleRequestDocuments(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	spec := specFor("legal_matters")
	if spec == nil || !sc.Can("client_requirements", "update") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	ctx := r.Context()
	names := []string{}
	var title string
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		m, _, err := h.getRow(ctx, tx, sc.WS, spec, id, ownerFilter(r, spec))
		if err != nil {
			return err
		}
		title = m.Title
		rows, err := tx.Query(ctx, `SELECT id, name, COALESCE(status, '') FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'client_requirements' AND deleted_at IS NULL
			AND custom->>'matterId' = $2 AND COALESCE(custom->>'need', 'required') <> 'optional' AND COALESCE(status, '') IN ('', 'not_requested', 'requested', 'rejected') ORDER BY created_at`, sc.WS, m.ID)
		if err != nil {
			return err
		}
		ids := []uuid.UUID{}
		for rows.Next() {
			var q uuid.UUID
			var n, s string
			if rows.Scan(&q, &n, &s) == nil {
				names = append(names, n)
				if s != "requested" {
					ids = append(ids, q)
				}
			}
		}
		rows.Close()
		for _, q := range ids {
			if _, err := h.updateValues(ctx, tx, sc.WS, specFor("client_requirements"), q, lsys(), map[string]any{"status": "requested", "requestedOn": today()}, nil, nil); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	msg := ""
	if len(names) > 0 {
		msg = "For your matter \"" + title + "\", please share these documents:\n- " + strings.Join(names, "\n- ")
	}
	h.bus.Kick()
	shared.WriteJSON(w, http.StatusOK, map[string]any{"requested": names, "message": msg})
}

// POST /legal/hearings/{id}/notify — tells the hearing's advocate in the CRM.
func (h *Handler) handleNotifyAdvocate(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	spec := specFor("court_hearings")
	if spec == nil || !sc.Can("court_hearings", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	ctx := r.Context()
	t, _, err := h.getRow(ctx, h.store.Pool, sc.WS, spec, id, ownerFilter(r, spec))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	to := t.id("assignedAdvocateId")
	if to == uuid.Nil {
		to = t.id("ownerId")
	}
	b, ok := businessOf(ctx, h.store.Pool, sc.WS)
	if !ok || to == uuid.Nil {
		shared.WriteError(w, r, shared.Validation(map[string]string{"assignedAdvocateId": "This hearing has no advocate."}))
		return
	}
	me := actor(r)
	h.notify(ctx, h.store.Pool, sc.WS, to, "legal.hearing_notice", "Please check this hearing", t.Title, b.link("court_hearings/"+t.ID), &me)
	h.bus.Kick()
	shared.WriteJSON(w, http.StatusOK, map[string]any{"notified": true})
}

// ---- reminders: each one once ----

func (h *Handler) alertOnce(ctx context.Context, id uuid.UUID, key string) bool {
	tag, err := h.store.Pool.Exec(ctx, `UPDATE crm.object_records SET custom = custom || jsonb_build_object('legalAlerts', COALESCE(custom->'legalAlerts', '[]'::jsonb) || to_jsonb($2::text))
		WHERE id = $1 AND NOT COALESCE(custom->'legalAlerts', '[]'::jsonb) @> to_jsonb($2::text)`, id, key)
	return err == nil && tag.RowsAffected() == 1
}

func (h *Handler) sweepLegalMore(ctx context.Context) {
	if specFor("legal_deadlines") == nil {
		return
	}
	now := time.Now().In(ist)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, ist)
	type item struct {
		id, ws    uuid.UUID
		title, on string
		rule      string
		to1, to2  *uuid.UUID
		object    string
	}
	load := func(sql string) []item {
		out := []item{}
		rows, err := h.store.Pool.Query(ctx, sql)
		if err != nil {
			return out
		}
		defer rows.Close()
		for rows.Next() {
			var i item
			if rows.Scan(&i.id, &i.ws, &i.title, &i.on, &i.rule, &i.to1, &i.to2, &i.object) == nil {
				out = append(out, i)
			}
		}
		return out
	}
	send := func(i item, kind, title string) {
		b, ok := businessOf(ctx, h.store.Pool, i.ws)
		if !ok {
			return
		}
		seen := map[uuid.UUID]bool{}
		for _, to := range []*uuid.UUID{i.to1, i.to2} {
			if to != nil && !seen[*to] {
				seen[*to] = true
				h.notify(ctx, h.store.Pool, i.ws, *to, kind, title, i.title, b.link(i.object+"/"+i.id.String()), nil)
			}
		}
	}
	for _, i := range load(`SELECT id, workspace_id, name, custom->>'hearingAt', '', NULLIF(custom->>'assignedAdvocateId', '')::uuid, owner_id, 'court_hearings' FROM crm.object_records
		WHERE object_key = 'court_hearings' AND deleted_at IS NULL AND status = 'scheduled' AND NULLIF(custom->>'hearingAt', '')::timestamptz < now() + interval '8 days' LIMIT 2000`) {
		t, ok := parseWhen(i.on)
		if !ok {
			continue
		}
		t = t.In(ist)
		d := int(time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, ist).Sub(day).Hours() / 24)
		switch {
		case t.Before(now):
			if h.alertOnce(ctx, i.id, "notupdated:"+i.on) {
				send(i, "legal.hearing_not_updated", "Hearing not updated: "+t.Format("2 Jan"))
			}
		case d == 0:
			if h.alertOnce(ctx, i.id, "today:"+i.on) {
				send(i, "legal.hearing_today", "Hearing today, "+t.Format("3:04 PM"))
			}
		case d == 7:
			if h.alertOnce(ctx, i.id, "7d:"+i.on) {
				send(i, "legal.hearing_week", "Hearing in 7 days: "+t.Format("2 Jan"))
			}
		}
	}
	for _, i := range load(`SELECT id, workspace_id, name, left(custom->>'dueDate', 10), COALESCE(custom->>'rule', ''), NULL::uuid, owner_id, 'legal_deadlines' FROM crm.object_records
		WHERE object_key = 'legal_deadlines' AND deleted_at IS NULL AND status = 'open' AND COALESCE(custom->>'dueDate', '') <> '' LIMIT 2000`) {
		t, err := time.ParseInLocation("2006-01-02", i.on, ist)
		if err != nil {
			continue
		}
		left := int(t.Sub(day).Hours() / 24)
		marks := []int{30, 7, 1, 0}
		if i.rule == "caveat" {
			marks = []int{15, 5, 1, 0} // Day 75 and Day 85 of 90
		}
		for _, mark := range marks {
			if left <= mark {
				if h.alertOnce(ctx, i.id, fmt.Sprintf("d%d:%s", mark, i.on)) && left == mark {
					send(i, "legal.deadline", fmt.Sprintf("Deadline in %d days: %s", left, t.Format("2 Jan 2006")))
				}
			}
		}
		if left < 0 && h.alertOnce(ctx, i.id, "missed:"+i.on) {
			send(i, "legal.deadline", "Deadline passed: "+t.Format("2 Jan 2006"))
		}
	}
	// A file reaching the end of its retention: a person decides; nothing is deleted.
	for _, i := range load(`SELECT id, workspace_id, name, left(custom->>'retentionUntil', 10), '', NULLIF(custom->>'primaryAdvocateId', '')::uuid, owner_id, 'legal_matters' FROM crm.object_records
		WHERE object_key = 'legal_matters' AND deleted_at IS NULL AND status = 'closed' AND COALESCE(custom->>'legalHold', '') <> 'true' AND COALESCE(custom->>'retentionUntil', '') <> ''
		  AND left(custom->>'retentionUntil', 10) <= to_char(now() + interval '30 days', 'YYYY-MM-DD') LIMIT 2000`) {
		if h.alertOnce(ctx, i.id, "retention:"+i.on) {
			send(i, "legal.retention", "File retention ends "+i.on+": review it")
		}
	}
}
