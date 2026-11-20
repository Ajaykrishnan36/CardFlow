package records

import (
	"context"
	"net/http"
	"sort"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
)

// GET /insurance/report — the agent's money in one place, from the same records the policies
// and commissions are kept in (catalogue G12, G13, G15, J14, J15, J22, G18):
//
//	cash      today: collected for insurers, remitted, still held, and own income received
//	months    the financial year month by month: commission, fees, direct costs, profit
//	insurers  the same by insurer and by class
//	years     five policy years from the year tables: premium and commission, firm and
//	          Estimated kept apart, commission % by year, against the active plan
//	draft     true while anything is unreconciled (short-paid commission, unmatched lines)
type reportMonth struct {
	Month      string  `json:"month"`
	Commission float64 `json:"commission"`
	Fees       float64 `json:"fees"`
	Costs      float64 `json:"costs"`
	Profit     float64 `json:"profit"`
	Policies   float64 `json:"policies"`
}

type reportYear struct {
	Year               int     `json:"year"`
	Label              string  `json:"label"`
	NewPolicies        float64 `json:"newPolicies"`
	Renewals           float64 `json:"renewals"`
	PremiumFirm        float64 `json:"premiumFirm"`
	PremiumEstimated   float64 `json:"premiumEstimated"`
	CommissionFirm     float64 `json:"commissionFirm"`
	CommissionEstimate float64 `json:"commissionEstimated"`
	CommissionActual   float64 `json:"commissionActual"`
	CommissionPercent  float64 `json:"commissionPercent"`
	PlanCommission     float64 `json:"planCommission,omitempty"`
}

func (h *Handler) handleInsuranceReport(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if specFor("commissions") == nil || !sc.Enabled("commissions") || !sc.Can("commissions", "read") || !sc.Can("insurance_policies", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	out, err := h.insuranceReport(r.Context(), sc.WS, time.Now())
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) insuranceReport(ctx context.Context, ws uuid.UUID, now time.Time) (map[string]any, error) {
	q := h.store.Pool
	today := now.Format("2006-01-02")
	// Financial year: April to March.
	fy := now.Year()
	if now.Month() < time.April {
		fy--
	}
	from := time.Date(fy, time.April, 1, 0, 0, 0, 0, time.UTC)
	cash := map[string]float64{}
	var collected, remitted, held, earned float64
	_ = q.QueryRow(ctx, `SELECT
		COALESCE(sum(`+numeric(`d.custom->>'paidAmount'`)+`) FILTER (WHERE d.custom->>'paidOn' = $2 AND p.custom->>'collectionMode' = 'agent'), 0),
		COALESCE(sum(`+numeric(`d.custom->>'paidAmount'`)+`) FILTER (WHERE d.custom->>'remittedOn' = $2), 0),
		COALESCE(sum(`+numeric(`d.custom->>'paidAmount'`)+`) FILTER (WHERE d.status = 'paid' AND COALESCE(d.custom->>'remittedOn', '') = '' AND p.custom->>'collectionMode' = 'agent'), 0)
		FROM crm.object_records d JOIN crm.object_records p ON p.id::text = d.custom->>'policyId'
		WHERE d.workspace_id = $1 AND d.object_key = 'premium_dues' AND d.deleted_at IS NULL`, ws, today).Scan(&collected, &remitted, &held)
	_ = q.QueryRow(ctx, `SELECT COALESCE(sum(`+numeric(`custom->>'receivedAmount'`)+`), 0) FROM crm.object_records
		WHERE workspace_id = $1 AND object_key = 'commissions' AND deleted_at IS NULL AND custom->>'receivedOn' = $2`, ws, today).Scan(&earned)
	cash["collectedToday"], cash["remittedToday"], cash["heldForInsurers"], cash["incomeToday"] = round2(collected), round2(remitted), round2(held), round2(earned)

	// Months: commission received by the month it arrived; fees and costs by the month the term started.
	months := make([]reportMonth, 12)
	idx := map[string]int{}
	for i := 0; i < 12; i++ {
		m := from.AddDate(0, i, 0).Format("2006-01")
		months[i].Month, idx[m] = m, i
	}
	rows, err := q.Query(ctx, `SELECT left(custom->>'receivedOn', 7), COALESCE(sum(`+numeric(`custom->>'receivedAmount'`)+`), 0) FROM crm.object_records
		WHERE workspace_id = $1 AND object_key = 'commissions' AND deleted_at IS NULL AND COALESCE(custom->>'receivedOn', '') <> '' GROUP BY 1`, ws)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m string
		var v float64
		if rows.Scan(&m, &v) == nil {
			if i, ok := idx[m]; ok {
				months[i].Commission = round2(v)
			}
		}
	}
	rows.Close()
	rows, err = q.Query(ctx, `SELECT left(custom->>'startDate', 7), count(*), COALESCE(sum(`+numeric(`custom->>'agentFee'`)+`), 0), COALESCE(sum(`+numeric(`custom->>'directCosts'`)+`), 0)
		FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'insurance_policies' AND deleted_at IS NULL
		AND status IN ('in_force', 'renewed', 'lapsed', 'matured') GROUP BY 1`, ws)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m string
		var n, fee, cost float64
		if rows.Scan(&m, &n, &fee, &cost) == nil {
			if i, ok := idx[m]; ok {
				months[i].Policies, months[i].Fees, months[i].Costs = n, round2(fee), round2(cost)
			}
		}
	}
	rows.Close()
	var total reportMonth
	total.Month = "Total"
	for i := range months {
		months[i].Profit = round2(months[i].Commission + months[i].Fees - months[i].Costs)
		total.Commission += months[i].Commission
		total.Fees += months[i].Fees
		total.Costs += months[i].Costs
		total.Profit += months[i].Profit
		total.Policies += months[i].Policies
	}

	// By insurer and class: expected and received commission, profit on policies.
	type split struct {
		Name     string  `json:"name"`
		Expected float64 `json:"expected"`
		Received float64 `json:"received"`
		Profit   float64 `json:"profit"`
	}
	insurers := []split{}
	rows, err = q.Query(ctx, `SELECT COALESCE(a.name, 'No insurer'), COALESCE(sum(`+numeric(`c.custom->>'expectedAmount'`)+`) FILTER (WHERE c.status <> 'clawback'), 0),
		COALESCE(sum(`+numeric(`c.custom->>'receivedAmount'`)+`), 0)
		FROM crm.object_records c LEFT JOIN crm.accounts a ON a.id::text = c.custom->>'insurerId'
		WHERE c.workspace_id = $1 AND c.object_key = 'commissions' AND c.deleted_at IS NULL GROUP BY 1 ORDER BY 1`, ws)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s split
		if rows.Scan(&s.Name, &s.Expected, &s.Received) == nil {
			s.Expected, s.Received = round2(s.Expected), round2(s.Received)
			insurers = append(insurers, s)
		}
	}
	rows.Close()
	classes := []split{}
	rows, err = q.Query(ctx, `SELECT COALESCE(NULLIF(custom->>'policyClass', ''), 'other'), COALESCE(sum(`+numeric(`custom->>'commissionAmount'`)+`), 0), COALESCE(sum(`+numeric(`custom->>'profit'`)+`), 0)
		FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'insurance_policies' AND deleted_at IS NULL AND status IN ('in_force', 'renewed', 'lapsed', 'matured') GROUP BY 1 ORDER BY 1`, ws)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var s split
		if rows.Scan(&s.Name, &s.Expected, &s.Profit) == nil {
			s.Expected, s.Profit = round2(s.Expected), round2(s.Profit)
			classes = append(classes, s)
		}
	}
	rows.Close()

	// Five years from the policy-year tables: a year is the financial year its policy year starts in.
	years := make([]reportYear, 5)
	for i := range years {
		years[i].Year = fy + i
		years[i].Label = time.Date(fy+i, time.April, 1, 0, 0, 0, 0, time.UTC).Format("2006") + "-" + time.Date(fy+i+1, 1, 1, 0, 0, 0, 0, time.UTC).Format("06")
	}
	rows, err = q.Query(ctx, `SELECT custom->>'startDate', `+numeric(`custom->>'policyYear'`)+`, COALESCE(custom->>'premiumBasis', ''),
		`+numeric(`custom->>'premium'`)+`, `+numeric(`custom->>'expectedCommission'`)+`, `+numeric(`custom->>'actualCommission'`)+`
		FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'policy_years' AND deleted_at IS NULL AND COALESCE(status, '') <> 'closed'`, ws)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var start, basis string
		var py, premium, expected, actual float64
		if rows.Scan(&start, &py, &basis, &premium, &expected, &actual) != nil || len(start) < 10 {
			continue
		}
		t, err := time.Parse("2006-01-02", start[:10])
		if err != nil {
			continue
		}
		y := t.Year()
		if t.Month() < time.April {
			y--
		}
		i := y - fy
		if i < 0 || i >= len(years) {
			continue
		}
		if py == 1 {
			years[i].NewPolicies++
		} else {
			years[i].Renewals++
		}
		if basis == "estimated" {
			years[i].PremiumEstimated += premium
			years[i].CommissionEstimate += expected
		} else {
			years[i].PremiumFirm += premium
			years[i].CommissionFirm += expected
		}
		years[i].CommissionActual += actual
	}
	rows.Close()
	// The active plan is the base case beside the actuals (J22).
	var planY1, planY2 float64
	_ = q.QueryRow(ctx, `SELECT `+numeric(`custom->>'year1Commission'`)+`, `+numeric(`custom->>'year2Commission'`)+` FROM crm.object_records
		WHERE workspace_id = $1 AND object_key = 'sales_plans' AND deleted_at IS NULL AND status <> 'superseded' ORDER BY created_at DESC LIMIT 1`, ws).Scan(&planY1, &planY2)
	for i := range years {
		y := &years[i]
		y.PremiumFirm, y.PremiumEstimated, y.CommissionFirm, y.CommissionEstimate, y.CommissionActual =
			round2(y.PremiumFirm), round2(y.PremiumEstimated), round2(y.CommissionFirm), round2(y.CommissionEstimate), round2(y.CommissionActual)
		if p := y.PremiumFirm + y.PremiumEstimated; p > 0 {
			y.CommissionPercent = round2((y.CommissionFirm + y.CommissionEstimate) / p * 100)
		}
	}
	years[0].PlanCommission = round2(planY1)
	years[1].PlanCommission = round2(planY2)

	// Commission % earned by policy year, by insurer (J15): expected against actual.
	type pct struct {
		Insurer   string  `json:"insurer"`
		Year      int     `json:"policyYear"`
		Expected  float64 `json:"expectedPercent"`
		Actual    float64 `json:"actualPercent,omitempty"`
		Estimated bool    `json:"estimated"`
	}
	byYear := []pct{}
	rows, err = q.Query(ctx, `SELECT COALESCE(a.name, ''), `+numeric(`y.custom->>'policyYear'`)+`::int,
		sum(`+numeric(`y.custom->>'premium'`)+`), sum(`+numeric(`y.custom->>'expectedCommission'`)+`), sum(`+numeric(`y.custom->>'actualCommission'`)+`),
		bool_or(y.custom->>'premiumBasis' = 'estimated'),
		sum(`+numeric(`y.custom->>'premium'`)+`) FILTER (WHERE COALESCE(y.custom->>'actualCommission', '') <> '')
		FROM crm.object_records y LEFT JOIN crm.accounts a ON a.id::text = y.custom->>'insurerId'
		WHERE y.workspace_id = $1 AND y.object_key = 'policy_years' AND y.deleted_at IS NULL GROUP BY 1, 2 HAVING sum(`+numeric(`y.custom->>'premium'`)+`) > 0 ORDER BY 1, 2`, ws)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p pct
		var premium, expected, actual float64
		var paidPremium *float64
		if rows.Scan(&p.Insurer, &p.Year, &premium, &expected, &actual, &p.Estimated, &paidPremium) != nil {
			continue
		}
		p.Expected = round2(expected / premium * 100)
		if paidPremium != nil && *paidPremium > 0 {
			p.Actual = round2(actual / *paidPremium * 100)
		}
		byYear = append(byYear, p)
	}
	rows.Close()
	sort.SliceStable(byYear, func(i, j int) bool { return byYear[i].Insurer < byYear[j].Insurer })

	// A report with figures not yet reconciled is a draft (G18).
	var short, unmatched int
	_ = q.QueryRow(ctx, `SELECT count(*) FILTER (WHERE object_key = 'commissions' AND status = 'short_paid'), count(*) FILTER (WHERE object_key = 'statement_lines' AND status = 'unmatched')
		FROM crm.object_records WHERE workspace_id = $1 AND deleted_at IS NULL AND object_key IN ('commissions', 'statement_lines')`, ws).Scan(&short, &unmatched)
	return map[string]any{
		"financialYear": years[0].Label, "cash": cash, "months": months, "monthsTotal": total, "insurers": insurers, "classes": classes,
		"years": years, "commissionByYear": byYear,
		"draft": short+unmatched > 0, "unreconciled": map[string]int{"shortPaidCommissions": short, "unmatchedStatementLines": unmatched},
		"assumptions": []string{"Financial year April–March.", "Commission counts in the month it was received; fees and direct costs in the month the policy term started.",
			"Years after the first come from each policy's year table; Variable premiums are Estimated until the renewal is issued.",
			"Plan figures are the active annual plan's (year 2 Estimated); they are never added to actuals."},
	}, nil
}
