package records

import (
	"context"
	"math"
	"net/http"
	"strconv"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
)

// GET /w/{code}/customers/health[?account=<id>] (D-176): every account the viewer can see with
// its owner, open pipeline, a health score, its engagement week by week (12 weeks) and the
// last touch; with ?account= also that account's day-by-day activity (18 weeks) and its key
// contacts. Engagement is what happened on the account, its contacts and every record that
// names it (deals, quotes, invoices, cases…), from the timeline.
//
// The score (0-100) is worked out, not typed: recency of the last touch (up to 40), how
// much happened in 12 weeks (up to 25), deals won (up to 20), a base of 15, less 15 when a
// deal is past its close date and 15 when an invoice is overdue.

type accountHealth struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Kind        string     `json:"kind,omitempty"`
	Industry    string     `json:"industry,omitempty"`
	Owner       string     `json:"owner"`
	OpenValue   float64    `json:"openValue"`
	OpenDeals   int        `json:"openDeals"`
	WonValue    float64    `json:"wonValue"`
	Score       int        `json:"score"`
	Engagement  []int      `json:"engagement"`
	Touches     int        `json:"touches"`
	LastTouch   *time.Time `json:"lastTouch,omitempty"`
	LastKind    string     `json:"lastKind,omitempty"`
	Overdue     bool       `json:"overdueDeal,omitempty"`
	OverdueBill bool       `json:"overdueInvoice,omitempty"`
}

const healthWeeks = 12

func (h *Handler) handleCustomerHealth(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.Can("accounts", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	ctx := r.Context()
	me := actor(r)
	args := []any{sc.WS}
	own := ""
	if sc.OwnOnly("accounts") {
		args = append(args, sc.OwnersFor("accounts", me))
		own = " AND a.owner_id = ANY($2::uuid[])"
	}
	only := r.URL.Query().Get("account")
	if only != "" {
		id, err := uuid.Parse(only)
		if err != nil {
			shared.WriteError(w, r, shared.NotFound("account"))
			return
		}
		args = append(args, id)
		own += " AND a.id = $" + strconv.Itoa(len(args))
	}
	rows, err := h.store.Pool.Query(ctx, `SELECT a.id::text, a.name, COALESCE(a.kind, ''), COALESCE(a.industry, ''), COALESCE(i.display_name, '')
		FROM crm.accounts a LEFT JOIN crm.identities i ON i.id = a.owner_id
		WHERE a.workspace_id = $1 AND a.deleted_at IS NULL`+own+` ORDER BY a.updated_at DESC LIMIT 200`, args...)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list := []*accountHealth{}
	byID := map[string]*accountHealth{}
	ids := []string{}
	for rows.Next() {
		a := &accountHealth{Engagement: make([]int, healthWeeks)}
		if rows.Scan(&a.ID, &a.Name, &a.Kind, &a.Industry, &a.Owner) == nil {
			list = append(list, a)
			byID[a.ID] = a
			ids = append(ids, a.ID)
		}
	}
	rows.Close()
	if only != "" && len(list) == 0 {
		shared.WriteError(w, r, shared.NotFound("account"))
		return
	}
	now := time.Now()
	since := now.AddDate(0, 0, -7*healthWeeks)
	// The records that belong to each account: itself, its contacts, and records naming it.
	const recs = `
		WITH acc AS (SELECT unnest($2::text[])::uuid AS id),
		recs AS (
			SELECT a.id AS acc, a.id AS rid FROM acc a
			UNION ALL SELECT c.account_id, c.id FROM crm.contacts c JOIN acc ON acc.id = c.account_id WHERE c.workspace_id = $1 AND c.deleted_at IS NULL
			UNION ALL SELECT acc.id, o.id FROM crm.object_records o JOIN acc ON o.custom->>'accountId' = acc.id::text WHERE o.workspace_id = $1 AND o.deleted_at IS NULL
		)`
	if len(ids) > 0 {
		erows, err := h.store.Pool.Query(ctx, recs+`
			SELECT r.acc::text, floor(extract(epoch FROM (now() - x.occurred_at)) / 604800)::int, count(*)
			FROM crm.activities x JOIN recs r ON r.rid = x.record_id
			WHERE x.workspace_id = $1 AND x.occurred_at >= $3 GROUP BY 1, 2`, sc.WS, ids, since)
		if err == nil {
			for erows.Next() {
				var id string
				var ago, n int
				if erows.Scan(&id, &ago, &n) == nil && ago >= 0 && ago < healthWeeks {
					if a := byID[id]; a != nil {
						a.Engagement[healthWeeks-1-ago] += n
						a.Touches += n
					}
				}
			}
			erows.Close()
		}
		lrows, err := h.store.Pool.Query(ctx, recs+`
			SELECT DISTINCT ON (r.acc) r.acc::text, x.occurred_at, x.object_key
			FROM crm.activities x JOIN recs r ON r.rid = x.record_id
			WHERE x.workspace_id = $1 AND $3::timestamptz IS NOT NULL ORDER BY r.acc, x.occurred_at DESC`, sc.WS, ids, since)
		if err == nil {
			for lrows.Next() {
				var id, kind string
				var at time.Time
				if lrows.Scan(&id, &at, &kind) == nil {
					if a := byID[id]; a != nil {
						t := at
						a.LastTouch, a.LastKind = &t, kind
					}
				}
			}
			lrows.Close()
		}
		drows, err := h.store.Pool.Query(ctx, `SELECT o.custom->>'accountId', o.object_key, COALESCE(o.status, ''),
			`+numeric(`o.custom->>'amount'`)+`::float8, COALESCE(NULLIF(o.custom->>'closeDate', ''), '') < to_char(current_date, 'YYYY-MM-DD') AND COALESCE(o.custom->>'closeDate', '') <> ''
			FROM crm.object_records o WHERE o.workspace_id = $1 AND o.deleted_at IS NULL AND o.object_key IN ('opportunities', 'invoices')
			  AND o.custom->>'accountId' = ANY($2::text[])`, sc.WS, ids)
		if err == nil {
			for drows.Next() {
				var id, obj, st string
				var amt float64
				var late bool
				if drows.Scan(&id, &obj, &st, &amt, &late) != nil {
					continue
				}
				a := byID[id]
				if a == nil {
					continue
				}
				switch {
				case obj == "opportunities" && st == "closed_won":
					a.WonValue += amt
				case obj == "opportunities" && st != "closed_lost":
					a.OpenDeals++
					a.OpenValue += amt
					a.Overdue = a.Overdue || late
				case obj == "invoices" && st == "overdue":
					a.OverdueBill = true
				}
			}
			drows.Close()
		}
	}
	for _, a := range list {
		a.Score = healthScore(a, now, h.wonCount(ctx, sc.WS, a.ID))
	}
	out := map[string]any{"data": list, "weeks": healthWeeks}
	if only != "" {
		out["detail"] = h.accountDetail(ctx, sc.WS, list[0].ID, recs)
	}
	shared.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) wonCount(ctx context.Context, ws uuid.UUID, account string) int {
	var n int
	_ = h.store.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND deleted_at IS NULL AND object_key = 'opportunities'
		AND status = 'closed_won' AND custom->>'accountId' = $2`, ws, account).Scan(&n)
	return n
}

func healthScore(a *accountHealth, now time.Time, won int) int {
	s := 15.0
	if a.LastTouch != nil {
		d := now.Sub(*a.LastTouch).Hours() / 24
		switch {
		case d <= 7:
			s += 40
		case d <= 30:
			s += 28
		case d <= 90:
			s += 15
		default:
			s += 5
		}
	}
	s += math.Min(25, float64(a.Touches)*2.5)
	s += math.Min(20, float64(won)*10)
	if a.Overdue {
		s -= 15
	}
	if a.OverdueBill {
		s -= 15
	}
	return int(math.Max(0, math.Min(100, math.Round(s))))
}

type keyContact struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Title     string     `json:"title,omitempty"`
	Role      string     `json:"role,omitempty"`
	LastTouch *time.Time `json:"lastTouch,omitempty"`
}

// accountDetail: one account's activity by day (18 weeks, oldest first, Monday to Sunday) with
// what kind it was, and its key contacts.
func (h *Handler) accountDetail(ctx context.Context, ws uuid.UUID, account, recs string) map[string]any {
	const weeks = 18
	end := time.Now()
	start := end.AddDate(0, 0, -7*weeks+1)
	start = time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, end.Location())
	cells := make([]int, weeks*7)
	kinds := map[string]int{}
	rows, err := h.store.Pool.Query(ctx, recs+`
		SELECT x.occurred_at, x.object_key FROM crm.activities x JOIN recs r ON r.rid = x.record_id
		WHERE x.workspace_id = $1 AND x.occurred_at >= $3`, ws, []string{account}, start)
	if err == nil {
		for rows.Next() {
			var at time.Time
			var obj string
			if rows.Scan(&at, &obj) != nil {
				continue
			}
			i := int(at.Sub(start).Hours() / 24)
			if i >= 0 && i < len(cells) {
				cells[i]++
			}
			switch obj {
			case "communications":
				kinds["messages"]++
			case "call_logs":
				kinds["calls"]++
			case "appointments", "events":
				kinds["meetings"]++
			default:
				kinds["updates"]++
			}
		}
		rows.Close()
	}
	contacts := []keyContact{}
	crows, err := h.store.Pool.Query(ctx, `SELECT c.id::text, trim(concat_ws(' ', c.first_name, c.last_name)), COALESCE(c.title, ''),
		COALESCE((SELECT r.role FROM crm.record_relationships r WHERE r.workspace_id = c.workspace_id AND r.type_key = 'contact_role' AND r.source_id = c.id AND r.is_active ORDER BY r.is_primary DESC LIMIT 1), ''),
		(SELECT max(x.occurred_at) FROM crm.activities x WHERE x.workspace_id = c.workspace_id AND x.record_id = c.id)
		FROM crm.contacts c WHERE c.workspace_id = $1 AND c.account_id = $2 AND c.deleted_at IS NULL ORDER BY c.updated_at DESC LIMIT 8`, ws, account)
	if err == nil {
		for crows.Next() {
			var k keyContact
			if crows.Scan(&k.ID, &k.Name, &k.Title, &k.Role, &k.LastTouch) == nil {
				contacts = append(contacts, k)
			}
		}
		crows.Close()
	}
	return map[string]any{"heatmap": cells, "from": start.Format("2006-01-02"), "weeks": weeks, "kinds": kinds, "contacts": contacts}
}
