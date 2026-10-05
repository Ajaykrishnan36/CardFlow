package records

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Forecasting (D-112). The forecast of a period is worked out from the opportunities
// whose close date falls in it — nothing is stored twice. Every opportunity lands in
// exactly one bucket, so nothing is counted twice:
//
//	closed    stage Closed won
//	lost      stage Closed lost
//	commit    open, forecast category Commit (or Closed)
//	bestCase  open, forecast category Best case
//	pipeline  open, forecast category Pipeline or none
//	omitted   open, forecast category Omitted
//
// forecast = closed + commit. bestCaseTotal = forecast + bestCase. openPipeline = commit +
// bestCase + pipeline. Rolled up by owner, then by team (a person in two teams appears in
// both team rows; the company total is always summed from people, never from teams).
//
// A member whose access is "own records" gets their own numbers and those of the roles
// below theirs — the same rule as every list. Targets (quotas) and approving need the
// forecast.manage capability.
//
//	GET  /w/{code}/forecast?period=2026-Q3&groupBy=owner|team&pipeline=
//	GET  /w/{code}/forecast/periods
//	PUT  /w/{code}/forecast/quotas                  {periodKey, scope, ownerId|teamId, pipeline, amount}
//	POST /w/{code}/forecast/submit                  {periodKey, pipeline, forecastAmount, comment}
//	POST /w/{code}/forecast/submissions/{id}/review {status, overrideAmount, comment}
//	GET  /w/{code}/forecast/settings   PUT …        {fiscalStartMonth}

func (h *Handler) forecastRoutes(r chi.Router) {
	r.Get("/forecast/history", h.handleForecastHistory)
	r.Get("/forecast", h.handleForecast)
	r.Get("/forecast/periods", h.handleForecastPeriods)
	r.Put("/forecast/quotas", h.handleSetQuota)
	r.Post("/forecast/submit", h.handleSubmitForecast)
	r.Post("/forecast/submissions/{id}/review", h.handleReviewForecast)
	r.Get("/forecast/settings", h.handleForecastSettings)
	r.Put("/forecast/settings", h.handleSaveForecastSettings)
}

type ForecastPeriod struct {
	Key   string `json:"key"`
	Kind  string `json:"kind"` // month | quarter | year
	Label string `json:"label"`
	From  string `json:"from"` // first day, inclusive
	To    string `json:"to"`   // last day, inclusive
}

var (
	monthKeyRe   = regexp.MustCompile(`^(\d{4})-(0[1-9]|1[0-2])$`)
	quarterKeyRe = regexp.MustCompile(`^(\d{4})-Q([1-4])$`)
	yearKeyRe    = regexp.MustCompile(`^FY(\d{4})$`)
)

// fiscalStart is the month a business's financial year starts in (1–12). April by default
// (the Indian financial year); a business changes it in the forecast settings.
func (h *Handler) fiscalStart(ctx context.Context, ws uuid.UUID) int {
	var s string
	_ = h.store.Pool.QueryRow(ctx, `SELECT COALESCE(profile->>'fiscalStartMonth', '') FROM crm.workspaces WHERE id = $1`, ws).Scan(&s)
	if n, err := strconv.Atoi(s); err == nil && n >= 1 && n <= 12 {
		return n
	}
	return 4
}

// fiscalYearOf is the fiscal year a date belongs to: the calendar year its fiscal year starts in.
func fiscalYearOf(t time.Time, start int) int {
	if int(t.Month()) < start {
		return t.Year() - 1
	}
	return t.Year()
}

func fyLabel(year, start int) string {
	if start == 1 {
		return fmt.Sprintf("FY %d", year)
	}
	return fmt.Sprintf("FY %d–%02d", year, (year+1)%100)
}

// parsePeriod turns a key into dates. The fiscal quarter and year depend on the business's start month.
func parsePeriod(key string, start int) (ForecastPeriod, error) {
	day := func(y, m int) time.Time { return time.Date(y, time.Month(m), 1, 0, 0, 0, 0, time.UTC) }
	out := func(kind, label string, from time.Time, months int) ForecastPeriod {
		return ForecastPeriod{Key: key, Kind: kind, Label: label, From: from.Format("2006-01-02"), To: from.AddDate(0, months, -1).Format("2006-01-02")}
	}
	if m := monthKeyRe.FindStringSubmatch(key); m != nil {
		y, _ := strconv.Atoi(m[1])
		mo, _ := strconv.Atoi(m[2])
		return out("month", day(y, mo).Format("January 2006"), day(y, mo), 1), nil
	}
	if m := quarterKeyRe.FindStringSubmatch(key); m != nil {
		y, _ := strconv.Atoi(m[1])
		q, _ := strconv.Atoi(m[2])
		from := day(y, start).AddDate(0, 3*(q-1), 0)
		return out("quarter", fmt.Sprintf("Q%d %s (%s – %s)", q, fyLabel(y, start), from.Format("Jan"), from.AddDate(0, 2, 0).Format("Jan 2006")), from, 3), nil
	}
	if m := yearKeyRe.FindStringSubmatch(key); m != nil {
		y, _ := strconv.Atoi(m[1])
		from := day(y, start)
		return out("year", fmt.Sprintf("%s (%s – %s)", fyLabel(y, start), from.Format("Jan 2006"), from.AddDate(0, 11, 0).Format("Jan 2006")), from, 12), nil
	}
	return ForecastPeriod{}, shared.Validation(map[string]string{"period": "Use a month (2026-10), a fiscal quarter (2026-Q3) or a fiscal year (FY2026)."})
}

// currentPeriods are the month, quarter and year that contain a date.
func currentPeriods(now time.Time, start int) (month, quarter, year string) {
	fy := fiscalYearOf(now, start)
	offset := (int(now.Month()) - start + 12) % 12
	return now.Format("2006-01"), fmt.Sprintf("%d-Q%d", fy, offset/3+1), fmt.Sprintf("FY%d", fy)
}

func (h *Handler) handleForecastPeriods(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	start := h.fiscalStart(r.Context(), sc.WS)
	now := time.Now()
	month, quarter, year := currentPeriods(now, start)
	list := []ForecastPeriod{}
	fy := fiscalYearOf(now, start)
	for _, y := range []int{fy - 1, fy, fy + 1} {
		if p, err := parsePeriod(fmt.Sprintf("FY%d", y), start); err == nil {
			list = append(list, p)
		}
		for q := 1; q <= 4; q++ {
			if p, err := parsePeriod(fmt.Sprintf("%d-Q%d", y, q), start); err == nil {
				list = append(list, p)
			}
		}
	}
	for i := -6; i <= 6; i++ {
		if p, err := parsePeriod(now.AddDate(0, i, 0).Format("2006-01"), start); err == nil {
			list = append(list, p)
		}
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": list, "current": map[string]string{"month": month, "quarter": quarter, "year": year}, "fiscalStartMonth": start})
}

// ForecastNumbers are the buckets and what is derived from them.
type ForecastNumbers struct {
	Quota         float64 `json:"quota"`
	Closed        float64 `json:"closed"`
	Commit        float64 `json:"commit"`
	BestCase      float64 `json:"bestCase"`
	Pipeline      float64 `json:"pipeline"`
	Omitted       float64 `json:"omitted"`
	Lost          float64 `json:"lost"`
	Weighted      float64 `json:"weighted"`      // open amount × probability
	Forecast      float64 `json:"forecast"`      // closed + commit
	BestCaseTotal float64 `json:"bestCaseTotal"` // forecast + best case
	OpenPipeline  float64 `json:"openPipeline"`  // commit + best case + pipeline
	Gap           float64 `json:"gap"`           // quota − forecast (0 without a quota)
	Attainment    float64 `json:"attainment"`    // closed ÷ quota, %
	Coverage      float64 `json:"coverage"`      // open pipeline ÷ what is still to close
	OpenDeals     int     `json:"openDeals"`
	WonDeals      int     `json:"wonDeals"`
}

func (n *ForecastNumbers) derive() {
	n.Forecast = n.Closed + n.Commit
	n.BestCaseTotal = n.Forecast + n.BestCase
	n.OpenPipeline = n.Commit + n.BestCase + n.Pipeline
	n.Gap, n.Attainment, n.Coverage = 0, 0, 0
	if n.Quota > 0 {
		n.Gap = n.Quota - n.Forecast
		n.Attainment = round1(n.Closed / n.Quota * 100)
		if left := n.Quota - n.Closed; left > 0 {
			n.Coverage = round1(n.OpenPipeline / left)
		}
	}
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

func (n *ForecastNumbers) add(o ForecastNumbers) {
	n.Closed += o.Closed
	n.Commit += o.Commit
	n.BestCase += o.BestCase
	n.Pipeline += o.Pipeline
	n.Omitted += o.Omitted
	n.Lost += o.Lost
	n.Weighted += o.Weighted
	n.OpenDeals += o.OpenDeals
	n.WonDeals += o.WonDeals
}

type ForecastSubmission struct {
	ID             string     `json:"id"`
	Status         string     `json:"status"`
	ForecastAmount float64    `json:"forecastAmount"`
	Comment        string     `json:"comment,omitempty"`
	OverrideAmount *float64   `json:"overrideAmount,omitempty"`
	ManagerComment string     `json:"managerComment,omitempty"`
	SubmittedAt    time.Time  `json:"submittedAt"`
	ApprovedAt     *time.Time `json:"approvedAt,omitempty"`
}

type ForecastRow struct {
	ID   string `json:"id"` // identity id or team id ("" = records without an owner)
	Name string `json:"name"`
	ForecastNumbers
	Members    int                 `json:"members,omitempty"` // team rows
	Submission *ForecastSubmission `json:"submission,omitempty"`
}

type ForecastResult struct {
	Period    ForecastPeriod  `json:"period"`
	GroupBy   string          `json:"groupBy"`
	Pipeline  string          `json:"pipeline"`
	Currency  string          `json:"currency"`
	Totals    ForecastNumbers `json:"totals"`
	Rows      []ForecastRow   `json:"rows"`
	CanManage bool            `json:"canManage"` // set targets, approve
	Me        string          `json:"me"`
	Scope     string          `json:"scope"` // company | team: whose deals the caller sees
}

func canManageForecast(sc *Scope) bool {
	return sc.Owner || sc.Eff.HasCapability(access.CapForecast)
}

// forecastByOwner is the one aggregate everything else is built from.
func (h *Handler) forecastByOwner(ctx context.Context, ws uuid.UUID, p ForecastPeriod, pipeline string, owners []uuid.UUID) (map[uuid.UUID]ForecastNumbers, error) {
	return h.forecastBy(ctx, ws, p, pipeline, owners, `o.owner_id`)
}

// forecastTerritoryExpr groups deals by their sales territory (D-124).
const forecastTerritoryExpr = `(CASE WHEN o.custom->>'territoryId' ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$' THEN (o.custom->>'territoryId')::uuid END)`

// forecastBy sums the deals of a period by one dimension. Amounts are in the business's
// base currency: a deal in another currency counts at the rate fixed on it (D-118).
func (h *Handler) forecastBy(ctx context.Context, ws uuid.UUID, p ForecastPeriod, pipeline string, owners []uuid.UUID, group string) (map[uuid.UUID]ForecastNumbers, error) {
	amt := money(`COALESCE(NULLIF(o.custom->>'baseAmount', ''), o.custom->>'amount')`)
	prob := `(CASE WHEN o.custom->>'probability' ~ '^[0-9]+(\.[0-9]+)?$' THEN (o.custom->>'probability')::numeric ELSE 0 END)`
	cat := `COALESCE(NULLIF(o.custom->>'forecastCategory', ''), 'pipeline')`
	open := `COALESCE(o.status, '') NOT IN ('closed_won', 'closed_lost')`
	rows, err := h.store.Pool.Query(ctx, `
		SELECT COALESCE(`+group+`, '00000000-0000-0000-0000-000000000000'::uuid),
		       COALESCE(sum(`+amt+`) FILTER (WHERE o.status = 'closed_won'), 0)::float8,
		       COALESCE(sum(`+amt+`) FILTER (WHERE o.status = 'closed_lost'), 0)::float8,
		       COALESCE(sum(`+amt+`) FILTER (WHERE `+open+` AND `+cat+` IN ('commit', 'closed')), 0)::float8,
		       COALESCE(sum(`+amt+`) FILTER (WHERE `+open+` AND `+cat+` = 'best_case'), 0)::float8,
		       COALESCE(sum(`+amt+`) FILTER (WHERE `+open+` AND `+cat+` NOT IN ('commit', 'closed', 'best_case', 'omitted')), 0)::float8,
		       COALESCE(sum(`+amt+`) FILTER (WHERE `+open+` AND `+cat+` = 'omitted'), 0)::float8,
		       COALESCE(sum(round(`+amt+` * `+prob+` / 100, 2)) FILTER (WHERE `+open+` AND `+cat+` <> 'omitted'), 0)::float8,
		       count(*) FILTER (WHERE `+open+`), count(*) FILTER (WHERE o.status = 'closed_won')
		FROM crm.object_records o
		WHERE o.workspace_id = $1 AND o.object_key = 'opportunities' AND o.deleted_at IS NULL
		  AND o.custom->>'closeDate' >= $2 AND o.custom->>'closeDate' <= $3
		  AND ($4 = '' OR COALESCE(o.custom->>'pipeline', '') = $4)
		  AND ($5::uuid[] IS NULL OR o.owner_id = ANY($5))
		GROUP BY 1`, ws, p.From, p.To, pipeline, owners)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]ForecastNumbers{}
	for rows.Next() {
		var id uuid.UUID
		var n ForecastNumbers
		if err := rows.Scan(&id, &n.Closed, &n.Lost, &n.Commit, &n.BestCase, &n.Pipeline, &n.Omitted, &n.Weighted, &n.OpenDeals, &n.WonDeals); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func (h *Handler) forecastAccess(r *http.Request) (*Scope, error) {
	sc := scopeFrom(r.Context())
	if specFor("opportunities") == nil || !sc.Enabled("opportunities") {
		return nil, shared.NotFound("object_not_found")
	}
	if !sc.Can("opportunities", "read") {
		return nil, errForbidden
	}
	return sc, nil
}

func (h *Handler) handleForecast(w http.ResponseWriter, r *http.Request) {
	sc, err := h.forecastAccess(r)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	me := actor(r)
	q := r.URL.Query()
	start := h.fiscalStart(ctx, sc.WS)
	key := q.Get("period")
	if key == "" {
		_, key, _ = currentPeriods(time.Now(), start)
	}
	period, err := parsePeriod(key, start)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	groupBy := q.Get("groupBy")
	if groupBy != "team" && groupBy != "territory" && groupBy != "role" {
		groupBy = "owner"
	}
	pipeline := strings.TrimSpace(q.Get("pipeline"))
	owners := sc.OwnersFor("opportunities", me)
	out := ForecastResult{Period: period, GroupBy: groupBy, Pipeline: pipeline, Currency: "INR", Rows: []ForecastRow{}, CanManage: canManageForecast(sc), Me: me.String(), Scope: "company"}
	if owners != nil {
		out.Scope = "team"
	}
	_ = h.store.Pool.QueryRow(ctx, `SELECT trim(currency) FROM crm.workspaces WHERE id = $1`, sc.WS).Scan(&out.Currency)

	byOwner, err := h.forecastByOwner(ctx, sc.WS, period, pipeline, owners)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	// Targets and submissions of this period.
	userQuota, teamQuota := map[uuid.UUID]float64{}, map[uuid.UUID]float64{}
	var companyQuota *float64
	qrows, err := h.store.Pool.Query(ctx, `SELECT scope, owner_id, team_id, amount::float8 FROM crm.forecast_quotas
		WHERE workspace_id = $1 AND period_key = $2 AND pipeline = $3`, sc.WS, period.Key, pipeline)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	for qrows.Next() {
		var scope string
		var owner, team *uuid.UUID
		var amount float64
		if err := qrows.Scan(&scope, &owner, &team, &amount); err != nil {
			qrows.Close()
			shared.WriteError(w, r, err)
			return
		}
		switch {
		case scope == "user" && owner != nil:
			userQuota[*owner] = amount
		case scope == "team" && team != nil:
			teamQuota[*team] = amount
		case scope == "company":
			v := amount
			companyQuota = &v
		}
	}
	qrows.Close()
	subs := map[uuid.UUID]*ForecastSubmission{}
	srows, err := h.store.Pool.Query(ctx, `SELECT id::text, owner_id, status, forecast_amount::float8, comment, override_amount::float8, manager_comment, submitted_at, approved_at
		FROM crm.forecast_submissions WHERE workspace_id = $1 AND period_key = $2 AND pipeline = $3 AND ($4::uuid[] IS NULL OR owner_id = ANY($4))`,
		sc.WS, period.Key, pipeline, owners)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	for srows.Next() {
		var s ForecastSubmission
		var owner uuid.UUID
		if err := srows.Scan(&s.ID, &owner, &s.Status, &s.ForecastAmount, &s.Comment, &s.OverrideAmount, &s.ManagerComment, &s.SubmittedAt, &s.ApprovedAt); err != nil {
			srows.Close()
			shared.WriteError(w, r, err)
			return
		}
		subs[owner] = &s
	}
	srows.Close()

	// The people: everyone the caller may see who has deals, a target or a submission —
	// and, for someone who sees the whole business, every active member.
	names := map[uuid.UUID]string{}
	mrows, err := h.store.Pool.Query(ctx, `SELECT i.id, i.display_name FROM crm.memberships m JOIN crm.identities i ON i.id = m.identity_id
		WHERE m.workspace_id = $1 AND m.status = 'active' AND ($2::uuid[] IS NULL OR i.id = ANY($2))`, sc.WS, owners)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	for mrows.Next() {
		var id uuid.UUID
		var name string
		if err := mrows.Scan(&id, &name); err != nil {
			mrows.Close()
			shared.WriteError(w, r, err)
			return
		}
		names[id] = name
	}
	mrows.Close()
	people := map[uuid.UUID]ForecastNumbers{}
	for id := range names {
		people[id] = byOwner[id]
	}
	for id, n := range byOwner {
		people[id] = n // includes deals without an owner (id = nil) and former members
	}
	for id, n := range people {
		n.Quota = userQuota[id]
		n.derive()
		people[id] = n
		out.Totals.add(n)
		out.Totals.Quota += n.Quota
	}
	// The company target, when set, is the target; otherwise the sum of people's targets.
	if companyQuota != nil && owners == nil {
		out.Totals.Quota = *companyQuota
	}
	out.Totals.derive()

	if groupBy == "territory" || groupBy == "role" {
		rows, err := h.forecastDimension(ctx, sc, period, pipeline, owners, groupBy, people)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out.Rows = rows
	} else if groupBy == "owner" {
		for id, n := range people {
			row := ForecastRow{ID: id.String(), Name: names[id], ForecastNumbers: n, Submission: subs[id]}
			if id == uuid.Nil {
				row.ID, row.Name = "", "No owner"
			} else if row.Name == "" {
				row.Name = "Former member"
			}
			out.Rows = append(out.Rows, row)
		}
	} else {
		// Team rows sum their members. Someone in two teams counts in both; the totals above don't.
		trows, err := h.store.Pool.Query(ctx, `
			SELECT t.id, t.name, COALESCE(array_agg(m.identity_id) FILTER (WHERE m.identity_id IS NOT NULL), '{}')
			FROM crm.teams t LEFT JOIN crm.team_members tm ON tm.team_id = t.id
			LEFT JOIN crm.memberships m ON m.id = tm.membership_id AND m.status = 'active'
			WHERE t.workspace_id = $1 GROUP BY t.id, t.name ORDER BY t.name`, sc.WS)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		inTeam := map[uuid.UUID]bool{}
		for trows.Next() {
			var id uuid.UUID
			var name string
			var members []uuid.UUID
			if err := trows.Scan(&id, &name, &members); err != nil {
				trows.Close()
				shared.WriteError(w, r, err)
				return
			}
			row := ForecastRow{ID: id.String(), Name: name}
			peopleQuota := 0.0
			for _, m := range members {
				n, visible := people[m]
				if !visible {
					continue
				}
				row.add(n)
				peopleQuota += n.Quota
				row.Members++
				inTeam[m] = true
			}
			row.Quota = peopleQuota
			if tq, ok := teamQuota[id]; ok {
				row.Quota = tq
			}
			row.derive()
			if row.Members > 0 || owners == nil {
				out.Rows = append(out.Rows, row)
			}
		}
		trows.Close()
		rest := ForecastRow{Name: "Not in a team"}
		for id, n := range people {
			if !inTeam[id] {
				rest.add(n)
				rest.Quota += n.Quota
				rest.Members++
			}
		}
		rest.derive()
		if rest.Members > 0 && (rest.OpenDeals > 0 || rest.WonDeals > 0 || rest.Quota > 0 || rest.Lost > 0) {
			out.Rows = append(out.Rows, rest)
		}
	}
	sort.SliceStable(out.Rows, func(i, j int) bool {
		if out.Rows[i].Forecast != out.Rows[j].Forecast {
			return out.Rows[i].Forecast > out.Rows[j].Forecast
		}
		return out.Rows[i].Name < out.Rows[j].Name
	})
	shared.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) handleSetQuota(w http.ResponseWriter, r *http.Request) {
	sc, err := h.forecastAccess(r)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if !canManageForecast(sc) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	var in struct {
		PeriodKey string  `json:"periodKey"`
		Scope     string  `json:"scope"`
		OwnerID   string  `json:"ownerId"`
		TeamID    string  `json:"teamId"`
		Pipeline  string  `json:"pipeline"`
		Amount    float64 `json:"amount"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	fe := map[string]string{}
	if _, err := parsePeriod(in.PeriodKey, h.fiscalStart(ctx, sc.WS)); err != nil {
		fe["periodKey"] = "Choose a period."
	}
	if in.Amount < 0 {
		fe["amount"] = "A target can't be negative."
	}
	var owner, team *uuid.UUID
	switch in.Scope {
	case "company":
	case "user":
		id, err := uuid.Parse(in.OwnerID)
		var member bool
		if err == nil {
			_ = h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.memberships WHERE workspace_id = $1 AND identity_id = $2 AND status = 'active')`, sc.WS, id).Scan(&member)
		}
		if !member {
			fe["ownerId"] = "Choose a member of this business."
		}
		owner = &id
	case "team":
		id, err := uuid.Parse(in.TeamID)
		var exists bool
		if err == nil {
			_ = h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.teams WHERE workspace_id = $1 AND id = $2)`, sc.WS, id).Scan(&exists)
		}
		if !exists {
			fe["teamId"] = "Choose a team of this business."
		}
		team = &id
	default:
		fe["scope"] = "Choose company, team or user."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	a := actorFromRequest(r, "ui")
	pipeline := strings.TrimSpace(in.Pipeline)
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var before *float64
		_ = tx.QueryRow(ctx, `SELECT amount::float8 FROM crm.forecast_quotas WHERE workspace_id = $1 AND period_key = $2 AND scope = $3
			AND owner_id IS NOT DISTINCT FROM $4 AND team_id IS NOT DISTINCT FROM $5 AND pipeline = $6`, sc.WS, in.PeriodKey, in.Scope, owner, team, pipeline).Scan(&before)
		if _, err := tx.Exec(ctx, `DELETE FROM crm.forecast_quotas WHERE workspace_id = $1 AND period_key = $2 AND scope = $3
			AND owner_id IS NOT DISTINCT FROM $4 AND team_id IS NOT DISTINCT FROM $5 AND pipeline = $6`, sc.WS, in.PeriodKey, in.Scope, owner, team, pipeline); err != nil {
			return err
		}
		if in.Amount > 0 {
			if _, err := tx.Exec(ctx, `INSERT INTO crm.forecast_quotas (workspace_id, period_key, scope, owner_id, team_id, pipeline, amount, created_by, updated_by)
				VALUES ($1, $2, $3, $4, $5, $6, round($7::numeric, 2), $8, $8)`, sc.WS, in.PeriodKey, in.Scope, owner, team, pipeline, in.Amount, a.ID); err != nil {
				return err
			}
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "forecast.quota_set", "forecast_quota", nil, map[string]any{"amount": before},
			map[string]any{"period": in.PeriodKey, "scope": in.Scope, "ownerId": owner, "teamId": team, "pipeline": pipeline, "amount": in.Amount}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleSubmitForecast: a person states what they stand behind for a period. The numbers
// at that moment are kept with it, so a later review can see what changed.
func (h *Handler) handleSubmitForecast(w http.ResponseWriter, r *http.Request) {
	sc, err := h.forecastAccess(r)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in struct {
		PeriodKey      string   `json:"periodKey"`
		Pipeline       string   `json:"pipeline"`
		ForecastAmount *float64 `json:"forecastAmount"`
		Comment        string   `json:"comment"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	me := actor(r)
	period, err := parsePeriod(in.PeriodKey, h.fiscalStart(ctx, sc.WS))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if len(in.Comment) > 1000 || (in.ForecastAmount != nil && *in.ForecastAmount < 0) {
		shared.WriteError(w, r, shared.Validation(map[string]string{"forecastAmount": "Enter an amount of zero or more, and a comment of at most 1,000 characters."}))
		return
	}
	pipeline := strings.TrimSpace(in.Pipeline)
	mine, err := h.forecastByOwner(ctx, sc.WS, period, pipeline, []uuid.UUID{me})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	n := mine[me]
	n.derive()
	amount := n.Forecast
	if in.ForecastAmount != nil {
		amount = *in.ForecastAmount
	}
	a := actorFromRequest(r, "ui")
	var id uuid.UUID
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var quota *float64
		_ = tx.QueryRow(ctx, `SELECT amount::float8 FROM crm.forecast_quotas WHERE workspace_id = $1 AND period_key = $2 AND scope = 'user' AND owner_id = $3 AND pipeline = $4`,
			sc.WS, period.Key, me, pipeline).Scan(&quota)
		if err := tx.QueryRow(ctx, `
			INSERT INTO crm.forecast_submissions (workspace_id, period_key, owner_id, pipeline, closed_won, commit_amount, best_case, pipeline_amount, forecast_amount, quota, comment)
			VALUES ($1, $2, $3, $4, round($5::numeric, 2), round($6::numeric, 2), round($7::numeric, 2), round($8::numeric, 2), round($9::numeric, 2), $10, $11)
			ON CONFLICT (workspace_id, period_key, owner_id, pipeline) DO UPDATE SET closed_won = EXCLUDED.closed_won, commit_amount = EXCLUDED.commit_amount,
			  best_case = EXCLUDED.best_case, pipeline_amount = EXCLUDED.pipeline_amount, forecast_amount = EXCLUDED.forecast_amount, quota = EXCLUDED.quota,
			  comment = EXCLUDED.comment, status = 'submitted', override_amount = NULL, manager_comment = '', submitted_at = now(), approved_at = NULL, approved_by = NULL
			RETURNING id`, sc.WS, period.Key, me, pipeline, n.Closed, n.Commit, n.BestCase, n.Pipeline, amount, quota, strings.TrimSpace(in.Comment)).Scan(&id); err != nil {
			return err
		}
		// Every submission is kept: the row above is the latest, the history is all of them.
		if _, err := tx.Exec(ctx, `INSERT INTO crm.forecast_history (workspace_id, period_key, owner_id, pipeline, event, closed_won, commit_amount, best_case, pipeline_amount, forecast_amount, quota, comment, actor_id)
			VALUES ($1, $2, $3, $4, 'submitted', round($5::numeric, 2), round($6::numeric, 2), round($7::numeric, 2), round($8::numeric, 2), round($9::numeric, 2), COALESCE($10, 0), $11, $3)`,
			sc.WS, period.Key, me, pipeline, n.Closed, n.Commit, n.BestCase, n.Pipeline, amount, quota, strings.TrimSpace(in.Comment)); err != nil {
			return err
		}
		// Tell the people who review forecasts (Super Admins and holders of forecast.manage).
		rows, err := tx.Query(ctx, `SELECT m.id, m.identity_id FROM crm.memberships m WHERE m.workspace_id = $1 AND m.status = 'active' AND m.identity_id <> $2`, sc.WS, me)
		if err != nil {
			return err
		}
		type member struct{ membership, identity uuid.UUID }
		var members []member
		for rows.Next() {
			var x member
			if err := rows.Scan(&x.membership, &x.identity); err != nil {
				rows.Close()
				return err
			}
			members = append(members, x)
		}
		rows.Close()
		var who string
		_ = tx.QueryRow(ctx, `SELECT display_name FROM crm.identities WHERE id = $1`, me).Scan(&who)
		for _, x := range members {
			eff, err := access.ForMembership(ctx, tx, x.membership)
			if err != nil || eff == nil || !eff.HasCapability(access.CapForecast) {
				continue
			}
			h.notify(ctx, tx, sc.WS, x.identity, "forecast.submitted", who+" submitted a forecast", period.Label, "/crm/w/"+sc.Code+"/forecasts?period="+period.Key, &me)
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "forecast.submitted", "forecast", &id, nil,
			map[string]any{"period": period.Key, "pipeline": pipeline, "forecastAmount": amount, "closed": n.Closed, "commit": n.Commit}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"id": id.String(), "forecastAmount": amount})
}

func (h *Handler) handleReviewForecast(w http.ResponseWriter, r *http.Request) {
	sc, err := h.forecastAccess(r)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if !canManageForecast(sc) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("forecast_not_found"))
		return
	}
	var in struct {
		Status         string   `json:"status"`
		OverrideAmount *float64 `json:"overrideAmount"`
		Comment        string   `json:"comment"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if in.Status != "approved" && in.Status != "rejected" {
		shared.WriteError(w, r, shared.Validation(map[string]string{"status": "Choose approved or rejected."}))
		return
	}
	if in.OverrideAmount != nil && *in.OverrideAmount < 0 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"overrideAmount": "An amount can't be negative."}))
		return
	}
	ctx := r.Context()
	me := actor(r)
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var owner uuid.UUID
		var period string
		var was json.RawMessage
		err := tx.QueryRow(ctx, `SELECT owner_id, period_key, jsonb_build_object('status', status, 'forecastAmount', forecast_amount, 'overrideAmount', override_amount)
			FROM crm.forecast_submissions WHERE id = $1 AND workspace_id = $2 FOR UPDATE`, id, sc.WS).Scan(&owner, &period, &was)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("forecast_not_found")
		}
		if err != nil {
			return err
		}
		// A reviewer who sees only their own branch can't review someone outside it.
		if own := sc.OwnersFor("opportunities", me); own != nil {
			inScope := false
			for _, o := range own {
				inScope = inScope || o == owner
			}
			if !inScope {
				return shared.NotFound("forecast_not_found")
			}
		}
		if _, err := tx.Exec(ctx, `UPDATE crm.forecast_submissions SET status = $3, override_amount = $4, manager_comment = $5, approved_at = now(), approved_by = $6
			WHERE id = $1 AND workspace_id = $2`, id, sc.WS, in.Status, in.OverrideAmount, strings.TrimSpace(in.Comment), me); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO crm.forecast_history (workspace_id, period_key, owner_id, pipeline, event, closed_won, commit_amount, best_case, pipeline_amount, forecast_amount, override_amount, quota, comment, actor_id)
			SELECT workspace_id, period_key, owner_id, pipeline, $3, closed_won, commit_amount, best_case, pipeline_amount, forecast_amount, override_amount, COALESCE(quota, 0), manager_comment, $4
			FROM crm.forecast_submissions WHERE id = $1 AND workspace_id = $2`, id, sc.WS, in.Status, me); err != nil {
			return err
		}
		h.notify(ctx, tx, sc.WS, owner, "forecast."+in.Status, "Your forecast was "+in.Status, period, "/crm/w/"+sc.Code+"/forecasts?period="+period, &me)
		var before any
		_ = json.Unmarshal(was, &before)
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "forecast."+in.Status, "forecast", &id, before,
			map[string]any{"status": in.Status, "overrideAmount": in.OverrideAmount, "comment": in.Comment}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleForecastSettings(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	shared.WriteJSON(w, http.StatusOK, map[string]any{"fiscalStartMonth": h.fiscalStart(r.Context(), sc.WS), "canManage": canManageForecast(sc)})
}

func (h *Handler) handleSaveForecastSettings(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !canManageForecast(sc) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	var in struct {
		FiscalStartMonth int `json:"fiscalStartMonth"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if in.FiscalStartMonth < 1 || in.FiscalStartMonth > 12 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"fiscalStartMonth": "Choose a month (1–12)."}))
		return
	}
	a := actorFromRequest(r, "ui")
	err := h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `UPDATE crm.workspaces SET profile = profile || jsonb_build_object('fiscalStartMonth', $2::text) WHERE id = $1`,
			sc.WS, strconv.Itoa(in.FiscalStartMonth)); err != nil {
			return err
		}
		id := sc.WS
		return shared.WriteAudit(r.Context(), tx, a.audit(sc.WS, "forecast.settings_changed", "workspace", &id, nil, in))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"fiscalStartMonth": in.FiscalStartMonth})
}
