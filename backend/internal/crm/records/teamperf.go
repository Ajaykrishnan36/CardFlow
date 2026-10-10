package records

import (
	"context"
	"net/http"
	"sort"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
)

// Team performance (D-137): who added which leads, contacts and accounts, who converted
// leads and who won deals, for the people who may see everyone's records.
//
//	GET /w/{code}/dashboard/team   ?range=week|month|year|all|custom&from=&to=

type teamMember struct {
	ID             string  `json:"id"`
	Name           string  `json:"name"`
	Role           string  `json:"role"`
	Active         bool    `json:"active"`
	LeadsAdded     int     `json:"leadsAdded"`
	LeadsConverted int     `json:"leadsConverted"`
	ContactsAdded  int     `json:"contactsAdded"`
	AccountsAdded  int     `json:"accountsAdded"`
	DealsWon       int     `json:"dealsWon"`
	WonValue       float64 `json:"wonValue"`
	// Leads holds the leads this person added in each bucket of the report (same order as buckets).
	Leads []int `json:"leads"`
}

type teamBucket struct {
	Key      string `json:"key"`   // 2026-10-10 or 2026-10
	Label    string `json:"label"` // 10 Oct or Oct 2026
	Leads    int    `json:"leads"`
	Contacts int    `json:"contacts"`
}

type teamPerformance struct {
	Range    summaryRange `json:"range"`
	Bucket   string       `json:"bucket"` // day | month
	Buckets  []teamBucket `json:"buckets"`
	Members  []teamMember `json:"members"`
	Currency string       `json:"currency"`
}

func (h *Handler) handleDashboardTeam(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.Owner && !sc.Eff.HasCapability(access.CapDashboard) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	// A report about everyone is for people who see everyone's leads.
	if specFor("leads") == nil || !sc.Enabled("leads") || !sc.Can("leads", "read") || sc.OwnOnly("leads") {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "Team performance is for people who can see everyone's leads."))
		return
	}
	ctx := r.Context()
	env := h.envFor(ctx, sc.WS, actor(r))
	loc, err := time.LoadLocation(env.TZ)
	if err != nil {
		loc = time.UTC
	}
	q := r.URL.Query()
	key := strings.ToLower(q.Get("range"))
	if key == "" {
		key = "month"
	}
	rng, from, to, err := rangeBounds(key, q.Get("from"), q.Get("to"), loc, time.Now())
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	now := time.Now().In(loc)
	if key == "all" {
		// "All time" starts at the first lead (at most three years back) and ends this month.
		var first *time.Time
		_ = h.store.Pool.QueryRow(ctx, `SELECT min(created_at) FROM crm.leads WHERE workspace_id = $1 AND deleted_at IS NULL`, sc.WS).Scan(&first)
		from = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, -11, 0)
		if first != nil && first.In(loc).Before(from) {
			f := first.In(loc)
			from = time.Date(f.Year(), f.Month(), 1, 0, 0, 0, 0, loc)
		}
		if floor := now.AddDate(-3, 0, 0); from.Before(floor) {
			from = time.Date(floor.Year(), floor.Month(), 1, 0, 0, 0, 0, loc)
		}
		to = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc).AddDate(0, 1, 0)
	}
	out := teamPerformance{Range: rng, Bucket: "day", Buckets: []teamBucket{}, Members: []teamMember{}, Currency: "INR"}
	_ = h.store.Pool.QueryRow(ctx, `SELECT currency FROM crm.workspaces WHERE id = $1`, sc.WS).Scan(&out.Currency)
	out.Currency = strings.TrimSpace(out.Currency)

	layout, label, trunc := "2006-01-02", "2 Jan", "day"
	if to.Sub(from) > 93*24*time.Hour {
		out.Bucket, layout, label, trunc = "month", "2006-01", "Jan 2006", "month"
	}
	index := map[string]int{}
	for d := from; d.Before(to); {
		index[d.Format(layout)] = len(out.Buckets)
		out.Buckets = append(out.Buckets, teamBucket{Key: d.Format(layout), Label: d.Format(label)})
		if trunc == "day" {
			d = d.AddDate(0, 0, 1)
		} else {
			d = d.AddDate(0, 1, 0)
		}
	}

	people := map[string]*teamMember{}
	person := func(id string) *teamMember {
		if p := people[id]; p != nil {
			return p
		}
		p := &teamMember{ID: id, Leads: make([]int, len(out.Buckets))}
		people[id] = p
		return p
	}
	// Everyone who works here now, so someone who added nothing still shows.
	rows, err := h.store.Pool.Query(ctx, `
		SELECT i.id::text, i.display_name, COALESCE((SELECT ro.name FROM crm.role_assignments ra JOIN crm.roles ro ON ro.id = ra.role_id
		        WHERE ra.membership_id = m.id ORDER BY ro.rank DESC LIMIT 1), '')
		FROM crm.memberships m JOIN crm.identities i ON i.id = m.identity_id
		WHERE m.workspace_id = $1 AND m.status = 'active' AND NOT i.is_platform_owner`, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	for rows.Next() {
		var id, name, role string
		if err := rows.Scan(&id, &name, &role); err != nil {
			rows.Close()
			shared.WriteError(w, r, err)
			return
		}
		p := person(id)
		p.Name, p.Role, p.Active = name, role, true
	}
	rows.Close()

	tz := loc.String()
	bucketSQL := `to_char(date_trunc('` + trunc + `', t.created_at AT TIME ZONE $4), '` + map[string]string{"day": "YYYY-MM-DD", "month": "YYYY-MM"}[trunc] + `')`
	type hit struct {
		who, bucket string
		n           int
	}
	added := func(table string) ([]hit, error) {
		rs, err := h.store.Pool.Query(ctx, `SELECT COALESCE(t.created_by::text, ''), `+bucketSQL+`, count(*) FROM crm.`+table+` t
			WHERE t.workspace_id = $1 AND t.deleted_at IS NULL AND t.created_at >= $2 AND t.created_at < $3 GROUP BY 1, 2`, sc.WS, from, to, tz)
		if err != nil {
			return nil, err
		}
		defer rs.Close()
		var list []hit
		for rs.Next() {
			var x hit
			if err := rs.Scan(&x.who, &x.bucket, &x.n); err != nil {
				return nil, err
			}
			list = append(list, x)
		}
		return list, rs.Err()
	}
	fail := func(err error) bool {
		if err != nil {
			shared.WriteError(w, r, err)
		}
		return err != nil
	}
	leads, err := added("leads")
	if fail(err) {
		return
	}
	for _, x := range leads {
		i, ok := index[x.bucket]
		if ok {
			out.Buckets[i].Leads += x.n
		}
		if x.who != "" {
			p := person(x.who)
			p.LeadsAdded += x.n
			if ok {
				p.Leads[i] += x.n
			}
		}
	}
	contacts, err := added("contacts")
	if fail(err) {
		return
	}
	for _, x := range contacts {
		if i, ok := index[x.bucket]; ok {
			out.Buckets[i].Contacts += x.n
		}
		if x.who != "" {
			person(x.who).ContactsAdded += x.n
		}
	}
	accounts, err := added("accounts")
	if fail(err) {
		return
	}
	for _, x := range accounts {
		if x.who != "" {
			person(x.who).AccountsAdded += x.n
		}
	}
	if fail(h.teamCounts(ctx, `SELECT c.converted_by::text, count(*), 0 FROM crm.lead_conversions c
		WHERE c.workspace_id = $1 AND c.status = 'converted' AND c.converted_by IS NOT NULL AND c.created_at >= $2 AND c.created_at < $3 GROUP BY 1`,
		[]any{sc.WS, from, to}, func(id string, n int, _ float64) { person(id).LeadsConverted += n })) {
		return
	}
	if specFor("opportunities") != nil && sc.Enabled("opportunities") && sc.Can("opportunities", "read") && !sc.OwnOnly("opportunities") {
		amount := numeric(`COALESCE(NULLIF(o.custom->>'baseAmount', ''), o.custom->>'amount')`)
		if fail(h.teamCounts(ctx, `SELECT o.owner_id::text, count(*), COALESCE(sum(`+amount+`), 0)::float8 FROM crm.object_records o
			WHERE o.workspace_id = $1 AND o.object_key = 'opportunities' AND o.deleted_at IS NULL AND o.status = 'closed_won' AND o.owner_id IS NOT NULL
			  AND left(COALESCE(o.custom->>'closeDate', ''), 10) >= $2 AND left(COALESCE(o.custom->>'closeDate', ''), 10) < $3 GROUP BY 1`,
			[]any{sc.WS, from.Format("2006-01-02"), to.Format("2006-01-02")}, func(id string, n int, v float64) {
				p := person(id)
				p.DealsWon += n
				p.WonValue += v
			})) {
			return
		}
	}
	// People who added something but no longer work here keep their name.
	var missing []string
	for id, p := range people {
		if p.Name == "" {
			missing = append(missing, id)
		}
	}
	if len(missing) > 0 {
		rs, err := h.store.Pool.Query(ctx, `SELECT id::text, display_name FROM crm.identities WHERE id::text = ANY($1)`, missing)
		if fail(err) {
			return
		}
		for rs.Next() {
			var id, name string
			if rs.Scan(&id, &name) == nil {
				people[id].Name, people[id].Role = name, "No longer a member"
			}
		}
		rs.Close()
	}
	for _, p := range people {
		if _, err := uuid.Parse(p.ID); err != nil || p.Name == "" {
			continue
		}
		if !p.Active && p.LeadsAdded+p.LeadsConverted+p.ContactsAdded+p.AccountsAdded+p.DealsWon == 0 {
			continue
		}
		out.Members = append(out.Members, *p)
	}
	sort.Slice(out.Members, func(a, b int) bool {
		if out.Members[a].LeadsAdded != out.Members[b].LeadsAdded {
			return out.Members[a].LeadsAdded > out.Members[b].LeadsAdded
		}
		return out.Members[a].Name < out.Members[b].Name
	})
	shared.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) teamCounts(ctx context.Context, sql string, args []any, each func(id string, n int, value float64)) error {
	rows, err := h.store.Pool.Query(ctx, sql, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		var n int
		var v float64
		if err := rows.Scan(&id, &n, &v); err != nil {
			return err
		}
		each(id, n, v)
	}
	return rows.Err()
}
