package records

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The legal pack (D-174), for advocates, lawyers and law firms:
//
//	legal_matters   a client's matter: what it is about, the court and case number, the
//	                other side, the advocate in charge (the owner) and the next hearing
//	court_hearings  each date in court: before whom, for what, what happened and the next date
//
// A matter is not a support case: the business's Cases stay its support tickets. The
// client comes from the matter on every hearing (one customer per record, D-173). Saving a
// hearing as attended or adjourned with a next date books that next hearing, and the
// matter always shows its nearest hearing; the advocate is reminded the day before.

const sourceLegal = "legal"

var legalCategories = opts("civil", "Civil", "criminal", "Criminal", "family", "Family", "property", "Property", "corporate", "Corporate / commercial",
	"consumer", "Consumer", "labour", "Labour & employment", "tax", "Tax", "ip", "Intellectual property", "cheque_bounce", "Cheque bounce (NI Act)",
	"writ", "Writ / constitutional", "arbitration", "Arbitration", "other", "Other")

// Matter statuses that mean it is over.
var matterClosed = map[string]bool{"won": true, "lost": true, "settled": true, "withdrawn": true, "closed": true}

func legalObjects() []ObjectDefinition {
	return []ObjectDefinition{
		{Key: "legal_matters", Module: "legal", Singular: "Client matter", Plural: "Client matters", Icon: "scale", Prefix: "MAT",
			Description: "A client's legal matter: what it is about, the court and case number, the other side, the advocate in charge and the next hearing.",
			ObjectBody: ObjectBody{NameLabel: "Matter", StatusLabel: "Status", Statuses: []StatusOption{
				st("new", "New", "neutral"), st("active", "Active", "primary"), st("on_hold", "On hold", "warning"),
				st("won", "Decided in favour", "success"), st("lost", "Decided against", "danger"), st("settled", "Settled", "success"),
				st("withdrawn", "Withdrawn", "neutral"), st("closed", "Closed", "neutral")},
				Fields: []ObjectField{
					ofReq(ofLookup("contactId", "Contact", "contacts")), ofLookup("accountId", "Account", "accounts"),
					ofSelect("matterCategory", "Matter category", legalCategories), ofLookup("serviceId", "Legal service", "catalog_items"),
					ofSelect("role", "Client is the", opts("petitioner", "Petitioner / plaintiff", "respondent", "Respondent / defendant", "complainant", "Complainant",
						"accused", "Accused", "applicant", "Applicant", "advisory", "Not in court (advice / documents)")),
					of("courtName", "Court / tribunal", "text"), of("courtCaseNumber", "Court case number", "text"),
					of("opposingParty", "Opposite party", "text"), of("opposingCounsel", "Opposite counsel", "text"),
					of("filedOn", "Filed on", "date"), of("nextHearingAt", "Next hearing", "datetime"),
					ofLookup("associateId", "Assisting advocate", "users"), of("feeAgreed", "Fee agreed", "currency"),
					ofLookup("opportunityId", "Opportunity", "opportunities"), ofLookup("contractId", "Engagement agreement", "contracts"),
					of("closedOn", "Closed on", "date"), of("outcome", "Outcome", "textarea"), of("description", "About the matter", "textarea")}}},
		{Key: "court_hearings", Module: "legal", Singular: "Hearing", Plural: "Hearings", Icon: "calendar", Prefix: "HRG",
			Description: "One date in court for a matter: before whom, for what, what happened and the next date.",
			ObjectBody: ObjectBody{NameLabel: "Hearing", StatusLabel: "Status", Statuses: []StatusOption{
				st("scheduled", "Scheduled", "primary"), st("attended", "Attended", "success"), st("adjourned", "Adjourned", "warning"),
				st("disposed", "Disposed / final order", "neutral"), st("cancelled", "Cancelled", "neutral")},
				Fields: []ObjectField{
					ofReq(ofLookup("matterId", "Matter", "legal_matters")), ofLookup("contactId", "Contact", "contacts"), ofLookup("accountId", "Account", "accounts"),
					ofReq(of("hearingAt", "Date & time", "datetime")),
					ofSelect("purpose", "Stage / purpose", opts("filing", "Filing", "admission", "Admission", "notice", "Notice / summons", "evidence", "Evidence",
						"cross", "Cross-examination", "arguments", "Arguments", "orders", "Orders", "judgment", "Judgment", "mention", "Mention", "mediation", "Mediation", "other", "Other")),
					of("courtName", "Court / tribunal", "text"), of("courtRoom", "Court hall / room", "text"), of("judge", "Before (judge / bench)", "text"),
					of("outcome", "What happened", "textarea"), of("nextHearingAt", "Next date", "datetime")}}},
	}
}

// legalSaved keeps a matter and its hearings in step.
func (h *Handler) legalSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, a actorInfo, op string, before, after *Row) (*Row, error) {
	if a.Source == sourceLegal {
		return after, nil
	}
	sys := systemActor(sourceLegal)
	status, was := after.text("status"), before.text("status")
	changed := op == "create" || status != was

	switch spec.Key {
	case "legal_matters":
		if matterClosed[status] && changed && !after.has("closedOn") {
			return h.updateValues(ctx, tx, ws, spec, after.uuid(), sys, map[string]any{"closedOn": today(), "nextHearingAt": nil}, nil, nil)
		}

	case "court_hearings":
		matters := specFor("legal_matters")
		m, _, err := h.getRow(ctx, tx, ws, matters, after.id("matterId"), nil)
		if err != nil {
			return nil, refuse("matterId", "Pick the matter this hearing is for.")
		}
		if op == "create" && matterClosed[m.text("status")] {
			return nil, refuse("matterId", "This matter is closed. Reopen it before adding a hearing.")
		}
		hearingAt, ok := parseWhen(after.text("hearingAt"))
		next, hasNext := parseWhen(after.text("nextHearingAt"))
		if hasNext && ok && !next.After(hearingAt) {
			return nil, refuse("nextHearingAt", "The next date is after this hearing.")
		}
		if status == "adjourned" && changed && !hasNext {
			return nil, refuse("nextHearingAt", "An adjourned hearing needs the next date.")
		}
		// The next date is the next hearing: book it once.
		if hasNext && (status == "attended" || status == "adjourned") && (changed || after.text("nextHearingAt") != before.text("nextHearingAt")) {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'court_hearings' AND deleted_at IS NULL
				AND custom->>'matterId' = $2 AND NULLIF(custom->>'hearingAt', '')::timestamptz = $3)`, ws, after.text("matterId"), next).Scan(&exists); err != nil {
				return nil, err
			}
			if !exists {
				n := map[string]any{"matterId": after.text("matterId"), "hearingAt": next.Format(time.RFC3339), "status": "scheduled",
					"courtName": after.text("courtName"), "courtRoom": after.text("courtRoom"), "judge": after.text("judge"), "ownerId": after.text("ownerId")}
				clean(n)
				if _, err := h.createRecord(ctx, tx, ws, spec, sys, n); err != nil {
					return nil, err
				}
			}
		}
		set := map[string]any{}
		nearest, found, err := nearestHearing(ctx, tx, ws, m.uuid())
		if err != nil {
			return nil, err
		}
		cur, has := parseWhen(m.text("nextHearingAt"))
		switch {
		case found && (!has || !cur.Equal(nearest)):
			set["nextHearingAt"] = nearest.Format(time.RFC3339)
		case !found && m.has("nextHearingAt"):
			set["nextHearingAt"] = nil
		}
		if m.text("status") == "new" && status == "scheduled" {
			set["status"] = "active"
		}
		if !m.has("courtName") && after.has("courtName") {
			set["courtName"] = after.text("courtName")
		}
		if len(set) > 0 {
			if _, err := h.updateValues(ctx, tx, ws, matters, m.uuid(), sys, set, nil, nil); err != nil {
				return nil, err
			}
		}
	}
	return after, nil
}

// nearestHearing is a matter's next scheduled hearing from now on.
func nearestHearing(ctx context.Context, q querier, ws, matter uuid.UUID) (time.Time, bool, error) {
	var at *time.Time
	err := q.QueryRow(ctx, `SELECT min(NULLIF(custom->>'hearingAt', '')::timestamptz) FROM crm.object_records
		WHERE workspace_id = $1 AND object_key = 'court_hearings' AND deleted_at IS NULL AND status = 'scheduled'
		  AND custom->>'matterId' = $2 AND NULLIF(custom->>'hearingAt', '')::timestamptz >= now() - interval '12 hours'`, ws, matter.String()).Scan(&at)
	if err != nil || at == nil {
		return time.Time{}, false, err
	}
	return *at, true, nil
}

func parseWhen(s string) (time.Time, bool) {
	if s == "" {
		return time.Time{}, false
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04", "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// sweepLegal reminds the advocate (the matter's owner, and whoever owns the hearing) of a
// hearing in the next day, once per date.
func (h *Handler) sweepLegal(ctx context.Context) {
	if specFor("court_hearings") == nil {
		return
	}
	rows, err := h.store.Pool.Query(ctx, `
		SELECT t.id, t.workspace_id, t.name, t.custom->>'hearingAt', t.owner_id, m.owner_id, COALESCE(m.name, '')
		FROM crm.object_records t LEFT JOIN crm.object_records m ON m.id::text = t.custom->>'matterId' AND m.workspace_id = t.workspace_id
		WHERE t.object_key = 'court_hearings' AND t.deleted_at IS NULL AND t.status = 'scheduled'
		  AND NULLIF(t.custom->>'hearingAt', '')::timestamptz BETWEEN now() AND now() + interval '26 hours'
		  AND COALESCE(t.custom->>'remindedFor', '') IS DISTINCT FROM t.custom->>'hearingAt'
		LIMIT 500`)
	if err != nil {
		return
	}
	type due struct {
		id, ws        uuid.UUID
		name, at      string
		owner, matter *uuid.UUID
		matterName    string
	}
	var list []due
	for rows.Next() {
		var d due
		if rows.Scan(&d.id, &d.ws, &d.name, &d.at, &d.owner, &d.matter, &d.matterName) == nil {
			list = append(list, d)
		}
	}
	rows.Close()
	for _, d := range list {
		b, ok := businessOf(ctx, h.store.Pool, d.ws)
		if !ok {
			continue
		}
		when := d.at
		if t, ok := parseWhen(d.at); ok {
			when = t.In(ist).Format("Mon 2 Jan, 3:04 PM")
		}
		sent := map[uuid.UUID]bool{}
		for _, to := range []*uuid.UUID{d.matter, d.owner} {
			if to == nil || sent[*to] {
				continue
			}
			sent[*to] = true
			h.notify(ctx, h.store.Pool, d.ws, *to, "legal.hearing_tomorrow", "Hearing "+when, joinNonEmpty(" — ", d.matterName, d.name), b.link("court_hearings/"+d.id.String()), nil)
		}
		_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.object_records SET custom = custom || jsonb_build_object('remindedFor', custom->>'hearingAt') WHERE id = $1`, d.id)
	}
}

func optionLabels(list []Option) map[string]string {
	out := make(map[string]string, len(list))
	for _, o := range list {
		out[o.Value] = o.Label
	}
	return out
}
