package records

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Campaign members and attribution (D-125). A campaign member is a lead or a contact the
// campaign touched, with a status and whether they responded. Everyone an email campaign
// is sent to becomes a member; members can also be added by hand (an event, a call list).
//
// Attribution answers "what did this campaign bring in?" without storing a second copy of
// any amount: for each opportunity, the campaigns that touched its people are found —
// its primary contact, its contact roles, and leads that were converted into its contact
// or account — and the deal's amount is shared between them by the chosen model:
// first touch, last touch, or evenly. The shares of one deal always add up to the deal.
//
//	GET    /w/{code}/campaigns/{campaignId}/members
//	POST   /w/{code}/campaigns/{campaignId}/members      {leadId | contactId, status, source}
//	PATCH  /w/{code}/campaign-members/{id}               {status, responded}
//	DELETE /w/{code}/campaign-members/{id}
//	PATCH  /w/{code}/campaigns/{campaignId}/costs        {campaignType, budgetedCost, actualCost, expectedRevenue, startDate, endDate}
//	GET    /w/{code}/campaigns/attribution               ?model=first|last|even
//	GET    /w/{code}/crm/{leads|contacts}/{id}/campaigns

func (h *Handler) campaignMemberRoutes(r chi.Router) {
	r.Get("/campaigns/attribution", h.handleAttribution)
	r.Get("/campaigns/{campaignId}/members", h.handleListCampaignMembers)
	r.Post("/campaigns/{campaignId}/members", h.handleAddCampaignMember)
	r.Patch("/campaigns/{campaignId}/costs", h.handleCampaignCosts)
	r.Patch("/campaign-members/{id}", h.handleUpdateCampaignMember)
	r.Delete("/campaign-members/{id}", h.handleDeleteCampaignMember)
	r.Get("/crm/{object}/{id}/campaigns", h.handleRecordCampaigns)
}

func canManageCampaigns(sc *Scope) bool {
	return sc.Owner || sc.Eff.HasCapability(access.CapCampaigns)
}

var memberStatuses = map[string]bool{"planned": true, "sent": true, "opened": true, "clicked": true, "responded": true, "converted": true, "bounced": true, "unsubscribed": true}

// touchCampaignMember records that a campaign reached a lead or contact.
func (h *Handler) touchCampaignMember(ctx context.Context, ws, campaign uuid.UUID, object string, record uuid.UUID) {
	col := map[string]string{"leads": "lead_id", "contacts": "contact_id"}[object]
	if col == "" {
		return
	}
	_, _ = h.store.Pool.Exec(ctx, `INSERT INTO crm.campaign_members (workspace_id, campaign_id, `+col+`, status, source) VALUES ($1, $2, $3, 'sent', 'email')
		ON CONFLICT (campaign_id, `+col+`) WHERE `+col+` IS NOT NULL DO UPDATE SET last_touch_at = now(), updated_at = now()`, ws, campaign, record)
}

type CampaignMember struct {
	ID           string     `json:"id"`
	CampaignID   string     `json:"campaignId"`
	Campaign     string     `json:"campaign,omitempty"`
	Object       string     `json:"object"` // leads | contacts
	RecordID     string     `json:"recordId"`
	Name         string     `json:"name"`
	Email        string     `json:"email,omitempty"`
	Status       string     `json:"status"`
	Source       string     `json:"source"`
	Responded    bool       `json:"responded"`
	ResponseDate *time.Time `json:"responseDate,omitempty"`
	FirstTouch   time.Time  `json:"firstTouchAt"`
	LastTouch    time.Time  `json:"lastTouchAt"`
}

const memberSelect = `
	SELECT m.id::text, m.campaign_id::text, c.name, CASE WHEN m.lead_id IS NOT NULL THEN 'leads' ELSE 'contacts' END, COALESCE(m.lead_id, m.contact_id)::text,
	       trim(COALESCE(l.first_name, k.first_name, '') || ' ' || COALESCE(l.last_name, k.last_name, '')), COALESCE(l.email, k.email, ''),
	       m.status, m.source, m.responded, m.response_date, m.first_touch_at, m.last_touch_at
	FROM crm.campaign_members m JOIN crm.campaigns c ON c.id = m.campaign_id
	LEFT JOIN crm.leads l ON l.id = m.lead_id LEFT JOIN crm.contacts k ON k.id = m.contact_id`

func scanMembers(rows pgx.Rows) ([]CampaignMember, error) {
	defer rows.Close()
	list := []CampaignMember{}
	for rows.Next() {
		var x CampaignMember
		if err := rows.Scan(&x.ID, &x.CampaignID, &x.Campaign, &x.Object, &x.RecordID, &x.Name, &x.Email, &x.Status, &x.Source, &x.Responded, &x.ResponseDate, &x.FirstTouch, &x.LastTouch); err != nil {
			return nil, err
		}
		list = append(list, x)
	}
	return list, rows.Err()
}

func (h *Handler) handleListCampaignMembers(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireCampaigns(w, r)
	if !ok {
		return
	}
	me := actor(r)
	// Row scope applies: someone who sees only their own leads sees only those members.
	rows, err := h.store.Pool.Query(r.Context(), memberSelect+`
		WHERE m.workspace_id = $1 AND m.campaign_id = $2
		  AND (m.lead_id IS NULL OR ($3::uuid[] IS NULL OR l.owner_id = ANY($3)))
		  AND (m.contact_id IS NULL OR ($4::uuid[] IS NULL OR k.owner_id = ANY($4)))
		ORDER BY m.last_touch_at DESC LIMIT 1000`, sc.WS, campaignID(r), sc.OwnersFor("leads", me), sc.OwnersFor("contacts", me))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := scanMembers(rows)
	respond(w, r, http.StatusOK, map[string]any{"data": list, "statuses": sortedKeys(map[string]string{"planned": "", "sent": "", "opened": "", "clicked": "", "responded": "", "converted": "", "bounced": "", "unsubscribed": ""})}, err)
}

func (h *Handler) handleAddCampaignMember(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireCampaigns(w, r)
	if !ok {
		return
	}
	var in struct {
		LeadID    string `json:"leadId"`
		ContactID string `json:"contactId"`
		Status    string `json:"status"`
		Source    string `json:"source"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	me := actor(r)
	campaign := campaignID(r)
	var exists bool
	_ = h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.campaigns WHERE id = $1 AND workspace_id = $2)`, campaign, sc.WS).Scan(&exists)
	if !exists {
		shared.WriteError(w, r, shared.NotFound("campaign_not_found"))
		return
	}
	if in.Status == "" {
		in.Status = "planned"
	}
	if in.Source == "" {
		in.Source = "manual"
	}
	fe := map[string]string{}
	if !memberStatuses[in.Status] {
		fe["status"] = "Choose a status."
	}
	object, raw, col := "leads", in.LeadID, "lead_id"
	if in.ContactID != "" {
		object, raw, col = "contacts", in.ContactID, "contact_id"
	}
	if (in.LeadID != "") == (in.ContactID != "") {
		fe["contactId"] = "Choose a lead or a contact."
	}
	id, perr := uuid.Parse(raw)
	if len(fe) == 0 {
		// A lead or contact of this business that the caller may open; another business's is not found.
		if perr != nil || !sc.Can(object, "read") {
			fe["contactId"] = "Choose a lead or a contact."
		} else if _, _, err := h.getRow(ctx, h.store.Pool, sc.WS, specFor(object), id, sc.OwnersFor(object, me)); err != nil {
			shared.WriteError(w, r, shared.NotFound("record_not_found"))
			return
		}
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	a := actorFromRequest(r, "ui")
	responded := in.Status == "responded" || in.Status == "converted"
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var mid uuid.UUID
		err := tx.QueryRow(ctx, `INSERT INTO crm.campaign_members (workspace_id, campaign_id, `+col+`, status, source, responded, response_date, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, CASE WHEN $6 THEN now() END, $7) ON CONFLICT DO NOTHING RETURNING id`, sc.WS, campaign, id, in.Status, clipText(in.Source, 40), responded, me).Scan(&mid)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.Validation(map[string]string{"contactId": "Already a member of this campaign."})
		}
		if err != nil {
			return err
		}
		if err := insertActivity(ctx, tx, sc.WS, object, id, "campaign.member_added", "Added to a campaign", map[string]any{"campaignId": campaign.String()}, a.ID); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "campaign_member.created", "campaign_member", &mid, nil, map[string]any{"campaignId": campaign.String(), "object": object, "recordId": id.String(), "status": in.Status}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.handleListCampaignMembers(w, r)
}

func (h *Handler) handleUpdateCampaignMember(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireCampaigns(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("member_not_found"))
		return
	}
	var in struct {
		Status    *string `json:"status"`
		Responded *bool   `json:"responded"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if in.Status != nil && !memberStatuses[*in.Status] {
		shared.WriteError(w, r, shared.Validation(map[string]string{"status": "Choose a status."}))
		return
	}
	if in.Status != nil && in.Responded == nil && (*in.Status == "responded" || *in.Status == "converted") {
		t := true
		in.Responded = &t
	}
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `UPDATE crm.campaign_members SET status = COALESCE($3, status), responded = COALESCE($4, responded),
			response_date = CASE WHEN COALESCE($4, responded) AND response_date IS NULL THEN now() WHEN NOT COALESCE($4, responded) THEN NULL ELSE response_date END,
			last_touch_at = now(), updated_at = now() WHERE id = $1 AND workspace_id = $2`, id, sc.WS, in.Status, in.Responded)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return shared.NotFound("member_not_found")
		}
		return shared.WriteAudit(r.Context(), tx, a.audit(sc.WS, "campaign_member.updated", "campaign_member", &id, nil, in))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (h *Handler) handleDeleteCampaignMember(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireCampaigns(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("member_not_found"))
		return
	}
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `DELETE FROM crm.campaign_members WHERE id = $1 AND workspace_id = $2`, id, sc.WS)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return shared.NotFound("member_not_found")
		}
		return shared.WriteAudit(r.Context(), tx, a.audit(sc.WS, "campaign_member.deleted", "campaign_member", &id, nil, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleCampaignCosts(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireCampaigns(w, r)
	if !ok {
		return
	}
	var in struct {
		Type            *string `json:"campaignType"`
		BudgetedCost    any     `json:"budgetedCost"`
		ActualCost      any     `json:"actualCost"`
		ExpectedRevenue any     `json:"expectedRevenue"`
		StartDate       *string `json:"startDate"`
		EndDate         *string `json:"endDate"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	fe := map[string]string{}
	amount := func(v any, field string) *string {
		if v == nil {
			return nil
		}
		c, ok := parseCents(v)
		if !ok || c < 0 {
			fe[field] = "Enter an amount of 0 or more."
			return nil
		}
		s := c.String()
		return &s
	}
	budget, actual, expected := amount(in.BudgetedCost, "budgetedCost"), amount(in.ActualCost, "actualCost"), amount(in.ExpectedRevenue, "expectedRevenue")
	start, end := cleanDate(in.StartDate, fe, "startDate"), cleanDate(in.EndDate, fe, "endDate")
	if in.Type != nil && !map[string]bool{"email": true, "event": true, "webinar": true, "advertising": true, "social": true, "referral": true, "telemarketing": true, "other": true}[*in.Type] {
		fe["campaignType"] = "Choose a type."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	a := actorFromRequest(r, "ui")
	id := campaignID(r)
	err := h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `UPDATE crm.campaigns SET campaign_type = COALESCE($3, campaign_type), budgeted_cost = COALESCE($4::numeric, budgeted_cost),
			actual_cost = COALESCE($5::numeric, actual_cost), expected_revenue = COALESCE($6::numeric, expected_revenue),
			start_date = CASE WHEN $7 THEN $8::date ELSE start_date END, end_date = CASE WHEN $9 THEN $10::date ELSE end_date END, updated_at = now()
			WHERE id = $1 AND workspace_id = $2`, id, sc.WS, in.Type, budget, actual, expected, in.StartDate != nil, start, in.EndDate != nil, end)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return shared.NotFound("campaign_not_found")
		}
		return shared.WriteAudit(r.Context(), tx, a.audit(sc.WS, "campaign.costs_updated", "campaign", &id, nil, in))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleRecordCampaigns: the campaigns a lead or contact is a member of.
func (h *Handler) handleRecordCampaigns(w http.ResponseWriter, r *http.Request) {
	sc, spec, row, err := h.recordScope(r, "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	// A contact keeps the campaigns of the lead it was converted from. A deal shows the
	// campaigns that reached its people: its primary contact, its contact roles, and the
	// lead its account or contact came from (the same people the results page credits).
	fromLead := `SELECT lc.lead_id FROM crm.lead_conversions lc WHERE lc.workspace_id = $1 AND lc.status = 'converted'`
	cond := map[string]string{
		"leads":    "m.lead_id = $2",
		"contacts": "(m.contact_id = $2 OR m.lead_id IN (" + fromLead + " AND lc.contact_id = $2))",
		"opportunities": `(m.contact_id::text IN (
			   SELECT o.custom->>'contactId' FROM crm.object_records o WHERE o.id = $2 AND o.workspace_id = $1
			   UNION SELECT rr.source_id::text FROM crm.record_relationships rr WHERE rr.workspace_id = $1 AND rr.type_key = 'contact_role' AND rr.target_object = 'opportunities' AND rr.target_id = $2)
			 OR m.lead_id IN (` + fromLead + ` AND (lc.account_id::text = (SELECT o.custom->>'accountId' FROM crm.object_records o WHERE o.id = $2 AND o.workspace_id = $1)
			   OR lc.contact_id::text = (SELECT o.custom->>'contactId' FROM crm.object_records o WHERE o.id = $2 AND o.workspace_id = $1))))`,
	}[spec.Key]
	if cond == "" {
		shared.WriteJSON(w, http.StatusOK, map[string]any{"data": []CampaignMember{}})
		return
	}
	rows, err := h.store.Pool.Query(r.Context(), memberSelect+` WHERE m.workspace_id = $1 AND `+cond+` ORDER BY m.first_touch_at DESC LIMIT 200`, sc.WS, row.uuid())
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := scanMembers(rows)
	respond(w, r, http.StatusOK, map[string]any{"data": list, "canManage": canManageCampaigns(sc)}, err)
}

type CampaignAttribution struct {
	ID              string   `json:"id"`
	Name            string   `json:"name"`
	Type            string   `json:"type"`
	Status          string   `json:"status"`
	Members         int      `json:"members"`
	Responded       int      `json:"responded"`
	Leads           int      `json:"leads"`
	Contacts        int      `json:"contacts"`
	ConvertedLeads  int      `json:"convertedLeads"`
	Opportunities   int      `json:"opportunities"`
	WonDeals        int      `json:"wonDeals"`
	Pipeline        Cents    `json:"pipeline"`   // share of open deals
	WonRevenue      Cents    `json:"wonRevenue"` // share of won deals
	ActualCost      *Cents   `json:"actualCost,omitempty"`
	BudgetedCost    *Cents   `json:"budgetedCost,omitempty"`
	ExpectedRevenue *Cents   `json:"expectedRevenue,omitempty"`
	ConversionRate  float64  `json:"conversionRate"` // converted leads ÷ leads, %
	ROI             *float64 `json:"roiPercent,omitempty"`
	CostPerLead     *Cents   `json:"costPerLead,omitempty"`
}

func (h *Handler) handleAttribution(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !canManageCampaigns(sc) && !sc.Can("opportunities", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	ctx := r.Context()
	me := actor(r)
	model := r.URL.Query().Get("model")
	if model != "last" && model != "even" {
		model = "first"
	}
	out := map[uuid.UUID]*CampaignAttribution{}
	order := []uuid.UUID{}
	rows, err := h.store.Pool.Query(ctx, `
		SELECT c.id, c.name, c.campaign_type, c.status, c.actual_cost::text, c.budgeted_cost::text, c.expected_revenue::text,
		       count(m.id), count(m.id) FILTER (WHERE m.responded), count(m.lead_id), count(m.contact_id),
		       count(m.lead_id) FILTER (WHERE EXISTS (SELECT 1 FROM crm.lead_conversions lc WHERE lc.lead_id = m.lead_id))
		FROM crm.campaigns c LEFT JOIN crm.campaign_members m ON m.campaign_id = c.id
		WHERE c.workspace_id = $1 GROUP BY c.id ORDER BY c.created_at DESC LIMIT 500`, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	for rows.Next() {
		var id uuid.UUID
		x := &CampaignAttribution{}
		var actual, budget, expected *string
		if err := rows.Scan(&id, &x.Name, &x.Type, &x.Status, &actual, &budget, &expected, &x.Members, &x.Responded, &x.Leads, &x.Contacts, &x.ConvertedLeads); err != nil {
			rows.Close()
			shared.WriteError(w, r, err)
			return
		}
		x.ID = id.String()
		for src, dst := range map[*string]**Cents{actual: &x.ActualCost, budget: &x.BudgetedCost, expected: &x.ExpectedRevenue} {
			if src != nil {
				c := mustCents(*src)
				*dst = &c
			}
		}
		if x.Leads > 0 {
			x.ConversionRate = float64(x.ConvertedLeads*10000/x.Leads) / 100
		}
		out[id] = x
		order = append(order, id)
	}
	rows.Close()

	// Every (deal, campaign) pair with the first time that campaign touched one of the deal's people.
	type touch struct {
		campaign uuid.UUID
		at       time.Time
	}
	type deal struct {
		amount  Cents
		won     bool
		open    bool
		touches []touch
	}
	deals := map[uuid.UUID]*deal{}
	if specFor("opportunities") != nil && sc.Can("opportunities", "read") {
		trows, err := h.store.Pool.Query(ctx, `
			WITH touches AS (
			  SELECT cm.campaign_id, cm.contact_id::text AS contact, NULL::text AS account, cm.first_touch_at FROM crm.campaign_members cm
			   WHERE cm.workspace_id = $1 AND cm.contact_id IS NOT NULL
			  UNION ALL
			  SELECT cm.campaign_id, lc.contact_id::text, lc.account_id::text, cm.first_touch_at FROM crm.campaign_members cm
			   JOIN crm.lead_conversions lc ON lc.lead_id = cm.lead_id AND lc.workspace_id = cm.workspace_id WHERE cm.workspace_id = $1),
			opp_contacts AS (
			  SELECT o.id AS opp, o.custom->>'contactId' AS contact FROM crm.object_records o
			   WHERE o.workspace_id = $1 AND o.object_key = 'opportunities' AND o.deleted_at IS NULL AND COALESCE(o.custom->>'contactId', '') <> ''
			  UNION
			  SELECT r.target_id, r.source_id::text FROM crm.record_relationships r
			   WHERE r.workspace_id = $1 AND r.type_key = 'contact_role' AND r.target_object = 'opportunities')
			SELECT o.id, `+money(`COALESCE(NULLIF(o.custom->>'baseAmount', ''), o.custom->>'amount')`)+`::text, COALESCE(o.status, ''), t.campaign_id, min(t.first_touch_at)
			FROM crm.object_records o
			JOIN touches t ON t.contact IN (SELECT oc.contact FROM opp_contacts oc WHERE oc.opp = o.id)
			               OR (t.account IS NOT NULL AND t.account = o.custom->>'accountId')
			WHERE o.workspace_id = $1 AND o.object_key = 'opportunities' AND o.deleted_at IS NULL AND COALESCE(o.status, '') <> 'closed_lost'
			  AND ($2::uuid[] IS NULL OR o.owner_id = ANY($2))
			GROUP BY o.id, t.campaign_id`, sc.WS, sc.OwnersFor("opportunities", me))
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		for trows.Next() {
			var opp, campaign uuid.UUID
			var amount, status string
			var at time.Time
			if err := trows.Scan(&opp, &amount, &status, &campaign, &at); err != nil {
				trows.Close()
				shared.WriteError(w, r, err)
				return
			}
			d := deals[opp]
			if d == nil {
				d = &deal{amount: mustCents(amount), won: status == "closed_won", open: status != "closed_won"}
				deals[opp] = d
			}
			d.touches = append(d.touches, touch{campaign, at})
		}
		trows.Close()
	}
	var totalWon, totalPipeline Cents
	for _, d := range deals {
		sort.Slice(d.touches, func(i, j int) bool {
			if !d.touches[i].at.Equal(d.touches[j].at) {
				return d.touches[i].at.Before(d.touches[j].at)
			}
			return d.touches[i].campaign.String() < d.touches[j].campaign.String()
		})
		// The shares of one deal add up to exactly its amount: the last one takes the rounding.
		shares := make([]Cents, len(d.touches))
		switch model {
		case "first":
			shares[0] = d.amount
		case "last":
			shares[len(shares)-1] = d.amount
		default:
			each := d.amount.mulRatio(1, int64(len(shares)))
			var given Cents
			for i := range shares {
				shares[i] = each
				given += each
			}
			shares[len(shares)-1] += d.amount - given
		}
		for i, t := range d.touches {
			c := out[t.campaign]
			if c == nil {
				continue
			}
			c.Opportunities++
			if d.won {
				if shares[i] > 0 || model == "even" {
					c.WonDeals++
				}
				c.WonRevenue += shares[i]
			} else {
				c.Pipeline += shares[i]
			}
		}
		if d.won {
			totalWon += d.amount
		} else {
			totalPipeline += d.amount
		}
	}
	list := []CampaignAttribution{}
	for _, id := range order {
		c := out[id]
		if c.ActualCost != nil && *c.ActualCost > 0 {
			roi := float64(int64(c.WonRevenue-*c.ActualCost)*10000/int64(*c.ActualCost)) / 100
			c.ROI = &roi
			if c.Leads > 0 {
				per := c.ActualCost.mulRatio(1, int64(c.Leads))
				c.CostPerLead = &per
			}
		}
		list = append(list, *c)
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"model": model, "data": list, "currency": h.baseCurrency(ctx, h.store.Pool, sc.WS),
		"totals": map[string]any{"wonRevenue": totalWon, "pipeline": totalPipeline, "deals": len(deals)}, "canManage": canManageCampaigns(sc),
		"models": []map[string]string{{"key": "first", "label": "First touch"}, {"key": "last", "label": "Last touch"}, {"key": "even", "label": "Shared evenly"}}})
}

var _ = strings.TrimSpace
