package records

import (
	"context"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"cogniz-crm-backend/internal/crm/access"
	"cogniz-crm-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The Lawyer CRM flow (D-185). The system flags and calculates; an advocate decides. Statutory
// periods and tax treatment here are starting rules to be confirmed by the firm.

var legalMoreKeys = map[string]bool{"case_intakes": true, "conflict_checks": true, "accident_details": true, "family_details": true, "client_requirements": true,
	"legal_documents": true, "advocate_assignments": true, "legal_deadlines": true, "legal_notices": true, "case_updates": true, "legal_invoices": true, "legal_payments": true,
	"case_expenses": true, "case_parties": true, "property_details": true, "criminal_details": true, "contract_details": true, "payment_milestones": true}

// Everything that follows its matter's access.
var matterChildren = []string{"court_hearings", "accident_details", "property_details", "family_details", "criminal_details", "contract_details", "case_parties", "client_requirements",
	"legal_documents", "advocate_assignments", "legal_deadlines", "legal_notices", "case_updates", "payment_milestones", "legal_invoices", "legal_payments", "case_expenses", "tasks"}

// legalRetentionYears is the starting retention period of a closed file; the firm confirms it.
const legalRetentionYears = 8

var cnrRe = regexp.MustCompile(`^[A-Za-z0-9]{16}$`)

func lsys() actorInfo { return systemActor(sourceLegal) }

func (h *Handler) legalSet(ctx context.Context, tx pgx.Tx, ws uuid.UUID, key string, r *Row, set map[string]any) (*Row, error) {
	for k, v := range set {
		if fmt.Sprint(r.Values[k]) == fmt.Sprint(v) {
			delete(set, k)
		}
	}
	if len(set) == 0 {
		return r, nil
	}
	return h.updateValues(ctx, tx, ws, specFor(key), r.uuid(), lsys(), set, nil, nil)
}

func (h *Handler) legalMake(ctx context.Context, tx pgx.Tx, ws uuid.UUID, key string, v map[string]any) (*Row, error) {
	clean(v)
	if _, ok := v["name"]; !ok {
		h.autoName(ctx, tx, ws, specFor(key), v)
	}
	return h.createRecord(ctx, tx, ws, specFor(key), lsys(), v)
}

func (h *Handler) legalTrade(ctx context.Context, q querier, ws uuid.UUID) bool {
	mods, err := access.WorkspaceModules(ctx, q, ws)
	return err == nil && mods["legal"] && !workspaceIsPlatform(ctx, q, ws)
}

// legalCoreSaved: what a law firm's leads and deals must do.
func (h *Handler) legalCoreSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, a actorInfo, op string, before, after *Row) (*Row, error) {
	if op == "delete" || a.Source == sourceLegal || !h.legalTrade(ctx, tx, ws) {
		return after, nil
	}
	switch spec.Key {
	case "leads":
		if after.text("source") == "advertisement" && before.text("source") != "advertisement" {
			return nil, refuse("source", "Bar Council rules restrict advocates from advertising. Use Phone, Walk-in, Referral or Website.")
		}
		// Unassigned leads: no owner until someone is chosen; choosing one takes it off the queue.
		if op == "create" && after.flag("sendToQueue") {
			if _, err := tx.Exec(ctx, `UPDATE crm.leads SET owner_id = NULL WHERE id = $1 AND workspace_id = $2`, after.uuid(), ws); err != nil {
				return nil, err
			}
			after.Values["ownerId"] = nil
		} else if op == "update" && after.flag("sendToQueue") && after.has("ownerId") {
			return h.legalSet(ctx, tx, ws, "leads", after, map[string]any{"sendToQueue": false})
		}
	case "quotes":
		if after.text("status") == "accepted" && before.text("status") != "accepted" {
			for _, n := range []struct{ key, msg string }{{"acceptedOn", "Enter the date the client accepted."}, {"acceptanceMethod", "Choose how the client accepted."},
				{"engagementSignedOn", "Enter the date the engagement letter was signed."}, {"paymentTerms", "Choose the payment terms."}} {
				if !after.has(n.key) {
					return nil, refuse(n.key, n.msg)
				}
			}
			if after.Values["advanceAmount"] == nil {
				return nil, refuse("advanceAmount", "Enter the advance agreed (0 if none).")
			}
			if after.num("advanceAmount") < 0 || after.num("advanceAmount") > after.num("total") {
				return nil, refuse("advanceAmount", "The advance must be between 0 and the quote total.")
			}
		}
	case "opportunities":
		if after.text("status") == "closed_won" && before.text("status") != "closed_won" {
			if err := h.matterFromDeal(ctx, tx, ws, after); err != nil {
				return nil, err
			}
		}
	}
	return after, nil
}

// ---- conflict check ----

func (h *Handler) runConflict(ctx context.Context, tx pgx.Tx, ws uuid.UUID, c *Row) (*Row, error) {
	matches := []string{}
	seen := map[string]bool{}
	for _, raw := range strings.Split(c.text("searchedNames"), ",") {
		name := strings.TrimSpace(raw)
		if len(name) < 3 {
			continue
		}
		like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(name) + "%"
		rows, err := tx.Query(ctx, `
			SELECT 'Client contact', trim(COALESCE(first_name, '') || ' ' || last_name) FROM crm.contacts
			 WHERE workspace_id = $1 AND deleted_at IS NULL AND id::text <> $3 AND trim(COALESCE(first_name, '') || ' ' || last_name) ILIKE $2
			UNION ALL SELECT 'Client', name FROM crm.accounts WHERE workspace_id = $1 AND deleted_at IS NULL AND id::text <> $4 AND name ILIKE $2 AND COALESCE(type, '') <> 'partner'
			UNION ALL SELECT 'Party (' || COALESCE(custom->>'partyRole', 'other') || ')', name FROM crm.object_records
			 WHERE workspace_id = $1 AND object_key = 'case_parties' AND deleted_at IS NULL AND name ILIKE $2 AND COALESCE(custom->>'contactId', '') <> $3
			LIMIT 25`, ws, like, c.text("contactId"), c.text("accountId"))
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var kind, who string
			if rows.Scan(&kind, &who) == nil && !seen[kind+who] {
				seen[kind+who] = true
				matches = append(matches, name+" → "+kind+": "+who)
			}
		}
		rows.Close()
	}
	status := "clear"
	if len(matches) > 0 {
		status = "review_required"
	}
	c, err := h.legalSet(ctx, tx, ws, "conflict_checks", c, map[string]any{"status": status, "matchCount": float64(len(matches)), "matches": strings.Join(matches, "\n")})
	if err != nil {
		return nil, err
	}
	return c, h.pushConflict(ctx, tx, ws, c)
}

func (h *Handler) pushConflict(ctx context.Context, tx pgx.Tx, ws uuid.UUID, c *Row) error {
	if id := c.id("intakeId"); id != uuid.Nil {
		if i, _, err := h.getRow(ctx, tx, ws, specFor("case_intakes"), id, nil); err == nil {
			if _, err := h.legalSet(ctx, tx, ws, "case_intakes", i, map[string]any{"conflictStatus": c.text("status")}); err != nil {
				return err
			}
		}
	}
	return nil
}

func (h *Handler) conflictSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, op string, before, c *Row) (*Row, error) {
	if op == "create" {
		return h.runConflict(ctx, tx, ws, c)
	}
	if st := c.text("status"); st != before.text("status") {
		if !c.has("decisionReason") {
			return nil, refuse("decisionReason", "Write why you made this decision.")
		}
		set := map[string]any{"reviewedAt": time.Now().Format(time.RFC3339)}
		if a.ID != nil {
			set["reviewedBy"] = a.ID.String()
		}
		c, err := h.legalSet(ctx, tx, ws, "conflict_checks", c, set)
		if err != nil {
			return nil, err
		}
		return c, h.pushConflict(ctx, tx, ws, c)
	}
	return c, nil
}

// legalConvertGate: a possible conflict needs an advocate's decision before conversion.
func (h *Handler) legalConvertGate(ctx context.Context, tx pgx.Tx, ws, lead uuid.UUID) error {
	if !h.legalTrade(ctx, tx, ws) {
		return nil
	}
	var open int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'case_intakes' AND deleted_at IS NULL
		AND custom->>'leadId' = $2 AND custom->>'conflictStatus' IN ('review_required', 'conflict_found')`, ws, lead.String()).Scan(&open)
	if open > 0 {
		return refuse("lead", "A possible conflict of interest is waiting for an advocate's decision. Open the conflict check first.")
	}
	return nil
}

// legalAfterConvert moves the lead's intakes to the client; the first one's service becomes
// the deal's line at the Price Book price.
func (h *Handler) legalAfterConvert(ctx context.Context, tx pgx.Tx, r actorInfo, sc *Scope, ws, lead uuid.UUID, res *ConvertResult, clientName string) error {
	if !h.legalTrade(ctx, tx, ws) {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT id FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'case_intakes' AND deleted_at IS NULL AND custom->>'leadId' = $2 ORDER BY created_at`, ws, lead.String())
	if err != nil {
		return err
	}
	ids := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for n, id := range ids {
		i, _, err := h.getRow(ctx, tx, ws, specFor("case_intakes"), id, nil)
		if err != nil {
			return err
		}
		set := map[string]any{}
		if res.AccountID != nil {
			set["accountId"] = res.AccountID.String()
		}
		if res.ContactID != nil {
			set["contactId"] = res.ContactID.String()
		}
		if n == 0 && res.OpportunityID != nil {
			set["opportunityId"] = res.OpportunityID.String()
			if i.has("serviceId") {
				if _, err := h.writeLines(ctx, tx, sc, "opportunities", *res.OpportunityID, PriceRequest{Lines: []PriceLineIn{{ItemID: i.text("serviceId"), Quantity: 1.0}}}, r, false); err != nil {
					return err
				}
				if o, _, err := h.getRow(ctx, tx, ws, specFor("opportunities"), *res.OpportunityID, nil); err == nil {
					if _, err := h.legalSet(ctx, tx, ws, "opportunities", o, map[string]any{"name": clientName + " – " + h.labelOf(ctx, tx, ws, "catalog_items", i.text("serviceId"))}); err != nil {
						return err
					}
				}
			}
		}
		if _, err := h.legalSet(ctx, tx, ws, "case_intakes", i, set); err != nil {
			return err
		}
	}
	return nil
}

// ---- Closed Won → matter, details and checklist ----

func legalKind(text string) string {
	t := strings.ToLower(text)
	for _, k := range []struct{ word, kind string }{{"accident", "accident"}, {"motor", "accident"}, {"mact", "accident"}, {"property", "property"}, {"land", "property"},
		{"divorce", "family"}, {"custody", "family"}, {"family", "family"}, {"bail", "criminal"}, {"criminal", "criminal"}, {"contract", "contract"}, {"agreement", "contract"}, {"corporate", "contract"}} {
		if strings.Contains(t, k.word) {
			return k.kind
		}
	}
	return "other"
}

func (h *Handler) matterFromDeal(ctx context.Context, tx pgx.Tx, ws uuid.UUID, o *Row) error {
	var have int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'legal_matters' AND deleted_at IS NULL AND custom->>'opportunityId' = $2`, ws, o.ID).Scan(&have); err != nil || have > 0 {
		return err
	}
	contact := o.text("contactId")
	if contact == "" && o.has("accountId") {
		_ = tx.QueryRow(ctx, `SELECT id::text FROM crm.contacts WHERE workspace_id = $1 AND account_id::text = $2 AND deleted_at IS NULL ORDER BY created_at LIMIT 1`, ws, o.text("accountId")).Scan(&contact)
	}
	if contact == "" {
		return refuse("contactId", "Add the client's contact before closing this deal as won, so the matter can be created.")
	}
	var intake, service, category, quote, acceptedOn, method, engaged, terms string
	var advance float64
	fee := o.num("amount")
	_ = tx.QueryRow(ctx, `SELECT id::text, COALESCE(custom->>'serviceId', ''), COALESCE(custom->>'matterCategory', '') FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'case_intakes'
		AND deleted_at IS NULL AND custom->>'opportunityId' = $2 ORDER BY created_at LIMIT 1`, ws, o.ID).Scan(&intake, &service, &category)
	_ = tx.QueryRow(ctx, `SELECT id::text, `+numeric(`custom->>'total'`)+`, COALESCE(custom->>'acceptedOn', ''), COALESCE(custom->>'acceptanceMethod', ''), COALESCE(custom->>'engagementSignedOn', ''), COALESCE(custom->>'paymentTerms', ''), `+numeric(`custom->>'advanceAmount'`)+`
		FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'quotes' AND deleted_at IS NULL
		AND custom->>'opportunityId' = $2 AND status = 'accepted' ORDER BY updated_at DESC LIMIT 1`, ws, o.ID).Scan(&quote, &fee, &acceptedOn, &method, &engaged, &terms, &advance)
	if service == "" {
		_ = tx.QueryRow(ctx, `SELECT COALESCE(custom->>'itemId', '') FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'line_items' AND deleted_at IS NULL AND custom->>'opportunityId' = $2 ORDER BY created_at LIMIT 1`, ws, o.ID).Scan(&service)
	}
	kindText := o.Title
	if service != "" {
		var family, cat string
		_ = tx.QueryRow(ctx, `SELECT COALESCE(custom->>'family', ''), COALESCE(custom->>'category', '') FROM crm.object_records WHERE id::text = $1 AND workspace_id = $2`, service, ws).Scan(&family, &cat)
		kindText = h.labelOf(ctx, tx, ws, "catalog_items", service) + " " + family + " " + cat
	}
	kind := legalKind(kindText)
	v := map[string]any{"name": o.Title, "contactId": contact, "accountId": o.text("accountId"), "opportunityId": o.ID, "quoteId": quote, "intakeId": intake, "serviceId": service,
		"matterCategory": category, "caseKind": kind, "feeAgreed": fee, "outstanding": fee, "status": "documentation", "ownerId": o.text("ownerId"), "acceptedOn": firstNonEmpty(acceptedOn, today()), "acceptanceMethod": method, "engagementSignedOn": engaged}
	m, err := h.legalMake(ctx, tx, ws, "legal_matters", v)
	if err != nil {
		return err
	}
	if key := map[string]string{"accident": "accident_details", "property": "property_details", "family": "family_details", "criminal": "criminal_details", "contract": "contract_details"}[kind]; key != "" {
		if _, err := h.legalMake(ctx, tx, ws, key, map[string]any{"name": specFor(key).Singular + " — " + m.Title, "matterId": m.ID, "ownerId": m.text("ownerId")}); err != nil {
			return err
		}
	}
	if service != "" {
		rows, err := tx.Query(ctx, `SELECT name, COALESCE(custom->>'need', 'required'), COALESCE(custom->>'instructions', '') FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'service_requirements'
			AND deleted_at IS NULL AND COALESCE(status, 'active') = 'active' AND custom->>'serviceId' = $2 ORDER BY created_at`, ws, service)
		if err != nil {
			return err
		}
		type item struct{ name, need, ins string }
		list := []item{}
		for rows.Next() {
			var i item
			if rows.Scan(&i.name, &i.need, &i.ins) == nil {
				list = append(list, i)
			}
		}
		rows.Close()
		for _, i := range list {
			if _, err := h.legalMake(ctx, tx, ws, "client_requirements", map[string]any{"name": i.name, "matterId": m.ID, "need": i.need, "instructions": i.ins, "status": "not_requested", "ownerId": m.text("ownerId")}); err != nil {
				return err
			}
		}
	}
	if quote != "" {
		if _, err := tx.Exec(ctx, `UPDATE crm.object_records SET custom = custom || jsonb_build_object('matterId', $3::text), updated_at = now() WHERE workspace_id = $1 AND object_key = 'payment_milestones' AND custom->>'quoteId' = $2`, ws, quote, m.ID); err != nil {
			return err
		}
	}
	if advance > 0 {
		var planned int
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'payment_milestones' AND deleted_at IS NULL AND custom->>'matterId' = $2`, ws, m.ID).Scan(&planned)
		if planned == 0 {
			if _, err := h.legalMake(ctx, tx, ws, "payment_milestones", map[string]any{"name": "Advance — " + m.Title, "matterId": m.ID, "quoteId": quote, "amount": advance, "dueDate": firstNonEmpty(acceptedOn, today()),
				"trigger": "On acceptance (" + strings.ReplaceAll(terms, "_", " ") + ")", "status": "due", "ownerId": m.text("ownerId")}); err != nil {
				return err
			}
		}
	}
	if intake != "" {
		if _, err := tx.Exec(ctx, `UPDATE crm.object_records SET status = 'converted', updated_at = now() WHERE id::text = $1 AND workspace_id = $2`, intake, ws); err != nil {
			return err
		}
	}
	return nil
}

// ---- access follows the assignment ----

// syncMatterTeam gives a matter's active advocates its records (read on the client) and takes
// them away when an assignment ends. Roles above them see it through the hierarchy.
func (h *Handler) syncMatterTeam(ctx context.Context, tx pgx.Tx, ws uuid.UUID, matter string) error {
	mid, err := uuid.Parse(matter)
	if err != nil {
		return nil
	}
	var people []uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(DISTINCT (custom->>'advocateId')::uuid), '{}') FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'advocate_assignments'
		AND deleted_at IS NULL AND status = 'active' AND custom->>'matterId' = $2`, ws, matter).Scan(&people); err != nil {
		return err
	}
	sync := func(object string, id uuid.UUID, level string, keep []uuid.UUID) error {
		if _, err := tx.Exec(ctx, `DELETE FROM crm.record_team_members WHERE workspace_id = $1 AND object_key = $2 AND record_id = $3 AND team_role = 'matter_assignment' AND NOT identity_id = ANY($4::uuid[])`, ws, object, id, keep); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO crm.record_team_members (workspace_id, object_key, record_id, identity_id, team_role, access_level)
			SELECT $1, $2, $3, p, 'matter_assignment', $5 FROM unnest($4::uuid[]) p ON CONFLICT (workspace_id, object_key, record_id, identity_id) DO NOTHING`, ws, object, id, keep, level)
		return err
	}
	if err := sync("legal_matters", mid, "write", people); err != nil {
		return err
	}
	for _, key := range matterChildren {
		rows, err := tx.Query(ctx, `SELECT id FROM crm.object_records WHERE workspace_id = $1 AND object_key = $2 AND deleted_at IS NULL AND custom->>'matterId' = $3`, ws, key, matter)
		if err != nil {
			return err
		}
		ids := []uuid.UUID{}
		for rows.Next() {
			var id uuid.UUID
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			if err := sync(key, id, "write", people); err != nil {
				return err
			}
		}
	}
	// The client: read for anyone active on any of this client's matters.
	var contact, account string
	_ = tx.QueryRow(ctx, `SELECT COALESCE(custom->>'contactId', ''), COALESCE(custom->>'accountId', '') FROM crm.object_records WHERE id = $1`, mid).Scan(&contact, &account)
	for _, c := range []struct{ object, field, id string }{{"contacts", "contactId", contact}, {"accounts", "accountId", account}} {
		id, err := uuid.Parse(c.id)
		if err != nil {
			continue
		}
		var all []uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(DISTINCT (a.custom->>'advocateId')::uuid), '{}') FROM crm.object_records a JOIN crm.object_records m ON m.id::text = a.custom->>'matterId'
			WHERE a.workspace_id = $1 AND a.object_key = 'advocate_assignments' AND a.deleted_at IS NULL AND a.status = 'active' AND m.deleted_at IS NULL AND m.custom->>'`+c.field+`' = $2`, ws, c.id).Scan(&all); err != nil {
			return err
		}
		if err := sync(c.object, id, "read", all); err != nil {
			return err
		}
	}
	return nil
}

// mayDirect: the matter's owner or someone above them (or with every record) decides.
func mayDirect(ctx context.Context, a actorInfo, m *Row) bool {
	sc := scopeFrom(ctx)
	if a.Kind == "system" || a.ID == nil || sc == nil {
		return true
	}
	owners := sc.OwnersFor("legal_matters", *a.ID)
	if owners == nil {
		return true
	}
	for _, o := range owners {
		if o == m.id("ownerId") {
			return true
		}
	}
	return false
}

func (h *Handler) assignmentSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, op string, before, r *Row, m *Row) (*Row, error) {
	if !mayDirect(ctx, a, m) {
		return nil, refuse("matterId", "Only the main or supervising advocate can change who works on this matter.")
	}
	set := map[string]any{}
	if op == "create" {
		if !r.has("status") {
			set["status"] = "active"
		}
		if !r.has("startDate") {
			set["startDate"] = today()
		}
		if !r.has("assignmentRole") {
			set["assignmentRole"] = "primary"
		}
		if a.ID != nil {
			set["assignedBy"] = a.ID.String()
		}
		if firstNonEmpty(r.text("assignmentRole"), "primary") == "primary" {
			var old uuid.UUID
			err := tx.QueryRow(ctx, `SELECT id FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'advocate_assignments' AND deleted_at IS NULL AND status = 'active'
				AND custom->>'matterId' = $2 AND custom->>'assignmentRole' = 'primary' AND id <> $3 LIMIT 1`, ws, m.ID, r.uuid()).Scan(&old)
			if err == nil {
				if !r.has("reassignmentReason") {
					return nil, refuse("reassignmentReason", "This matter has a primary advocate. Say why it is being reassigned.")
				}
				if _, err := h.updateValues(ctx, tx, ws, specFor("advocate_assignments"), old, lsys(), map[string]any{"status": "ended", "endDate": today(), "reassignmentReason": r.text("reassignmentReason")}, nil, nil); err != nil {
					return nil, err
				}
			}
			if _, err := h.legalSet(ctx, tx, ws, "legal_matters", m, map[string]any{"primaryAdvocateId": r.text("advocateId")}); err != nil {
				return nil, err
			}
		}
	} else if r.text("status") == "ended" && before.text("status") != "ended" {
		if !r.has("reassignmentReason") {
			return nil, refuse("reassignmentReason", "Say why this assignment is ending.")
		}
		if !r.has("endDate") {
			set["endDate"] = today()
		}
	}
	r, err := h.legalSet(ctx, tx, ws, "advocate_assignments", r, set)
	if err != nil {
		return nil, err
	}
	return r, h.syncMatterTeam(ctx, tx, ws, m.ID)
}

// ---- deadlines ----

func deadlineDates(rule string, t time.Time) (earliest, due time.Time) {
	switch rule {
	case "ni138_notice":
		return time.Time{}, t.AddDate(0, 0, 30)
	case "ni138_complaint": // trigger = the day the drawer received the notice
		day16 := t.AddDate(0, 0, 16)
		return day16, day16.AddDate(0, 1, 0)
	case "caveat":
		return time.Time{}, t.AddDate(0, 0, 90)
	case "section80":
		return t.AddDate(0, 2, 0), t.AddDate(0, 2, 0)
	case "commercial_ws":
		return time.Time{}, t.AddDate(0, 0, 120)
	case "commercial_appeal":
		return time.Time{}, t.AddDate(0, 0, 60)
	case "civil_appeal":
		return time.Time{}, t.AddDate(0, 0, 90)
	}
	return time.Time{}, time.Time{}
}

func (h *Handler) deadlineSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, r, m *Row) (*Row, error) {
	rule := firstNonEmpty(r.text("rule"), "custom")
	if rule == "caveat" && (m.text("matterCategory") == "criminal" || m.text("caseKind") == "criminal") {
		return nil, refuse("rule", "A CPC caveat doesn't apply to a criminal matter.")
	}
	set := map[string]any{}
	if !r.has("status") {
		set["status"] = "open"
	}
	trigger := r.text("triggerDate")
	if len(trigger) >= 10 {
		if t, err := time.Parse("2006-01-02", trigger[:10]); err == nil {
			e, d := deadlineDates(rule, t)
			if !d.IsZero() {
				set["dueDate"] = d.Format("2006-01-02")
			} else if !r.has("dueDate") {
				return nil, refuse("dueDate", "Enter the due date.")
			}
			if !e.IsZero() {
				set["earliestDate"] = e.Format("2006-01-02")
				if done := r.text("doneOn"); done != "" && done[:10] < e.Format("2006-01-02") {
					return nil, refuse("doneOn", "Too early: this can't be filed before "+e.Format("2 Jan 2006")+".")
				}
			}
		}
	}
	if r.has("doneOn") && r.text("status") == "open" {
		set["status"] = "done"
	}
	return h.legalSet(ctx, tx, ws, "legal_deadlines", r, set)
}

// ---- money ----

func (h *Handler) legalInvoiceSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, r, m *Row) (*Row, error) {
	var category string
	if m.has("accountId") {
		_ = tx.QueryRow(ctx, `SELECT COALESCE(custom->>'clientTaxCategory', '') FROM crm.accounts WHERE id::text = $1 AND workspace_id = $2`, m.text("accountId"), ws).Scan(&category)
	}
	regime := r.text("taxRegime")
	if regime == "" {
		switch {
		case r.text("serviceNature") == "non_legal":
			regime = "forward"
		case category == "gst_business":
			regime = "rcm"
		default:
			regime = "exempt"
		}
	}
	gst := 0.0
	if regime == "forward" {
		gst = round2(r.num("grossFee") * 18 / 100)
	}
	total := round2(r.num("grossFee") + r.num("reimbursable") + gst)
	var cash, tds float64
	_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(CASE WHEN custom->>'isRefund' = 'true' THEN -1 ELSE 1 END * `+numeric(`custom->>'amount'`)+`), 0), COALESCE(sum(`+numeric(`custom->>'tds'`)+`), 0)
		FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'legal_payments' AND deleted_at IS NULL AND custom->>'invoiceId' = $2`, ws, r.ID).Scan(&cash, &tds)
	balance := round2(total - cash - tds)
	set := map[string]any{"taxRegime": regime, "sacCode": firstNonEmpty(r.text("sacCode"), "9982"), "gstAmount": gst, "total": total, "cashReceived": round2(cash), "tdsDeducted": round2(tds), "balance": balance}
	if s := r.text("status"); s != "void" {
		switch {
		case balance <= 0.005:
			set["status"] = "paid"
		case cash+tds > 0:
			set["status"] = "partial"
		case s == "":
			set["status"] = "draft"
		}
	}
	return h.legalSet(ctx, tx, ws, "legal_invoices", r, set)
}

func (h *Handler) rollupMatter(ctx context.Context, tx pgx.Tx, ws uuid.UUID, id string) error {
	mid, err := uuid.Parse(id)
	if err != nil {
		return nil
	}
	m, _, err := h.getRow(ctx, tx, ws, specFor("legal_matters"), mid, nil)
	if err != nil {
		return nil
	}
	var got float64
	_ = tx.QueryRow(ctx, `SELECT COALESCE(sum(CASE WHEN custom->>'isRefund' = 'true' THEN -1 ELSE 1 END * (`+numeric(`custom->>'amount'`)+` + `+numeric(`custom->>'tds'`)+`)), 0)
		FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'legal_payments' AND deleted_at IS NULL AND custom->>'matterId' = $2 AND COALESCE(custom->>'appliesTo', 'fee') = 'fee'`, ws, id).Scan(&got)
	_, err = h.legalSet(ctx, tx, ws, "legal_matters", m, map[string]any{"feeReceived": round2(got), "outstanding": round2(m.num("feeAgreed") - got)})
	return err
}

// ---- the router ----

func (h *Handler) legalMoreSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, a actorInfo, op string, before, after *Row) (*Row, error) {
	if a.Source == sourceLegal {
		return after, nil
	}
	if spec.Key == "conflict_checks" {
		return h.conflictSaved(ctx, tx, ws, a, op, before, after)
	}
	if spec.Key == "case_intakes" {
		if op == "create" {
			names := []string{}
			if after.has("opponents") {
				names = append(names, after.text("opponents"))
			}
			if id := after.id("leadId"); id != uuid.Nil {
				var who string
				_ = tx.QueryRow(ctx, `SELECT trim(COALESCE(first_name, '') || ' ' || last_name) FROM crm.leads WHERE id = $1`, id).Scan(&who)
				names = append(names, who)
			}
			set := map[string]any{"enquiryDate": firstNonEmpty(after.text("enquiryDate"), today())}
			if !after.has("status") {
				set["status"] = "new"
			}
			var err error
			if after, err = h.legalSet(ctx, tx, ws, "case_intakes", after, set); err != nil {
				return nil, err
			}
			if len(names) > 0 {
				_, err := h.legalMake(ctx, tx, ws, "conflict_checks", map[string]any{"name": "Conflict check — " + after.Title, "intakeId": after.ID, "leadId": after.text("leadId"),
					"contactId": after.text("contactId"), "accountId": after.text("accountId"), "searchedNames": strings.Join(names, ", "), "ownerId": after.text("ownerId")})
				if err != nil {
					return nil, err
				}
				c, _, err := h.lastConflict(ctx, tx, ws, after.uuid())
				if err == nil {
					if _, err := h.runConflict(ctx, tx, ws, c); err != nil {
						return nil, err
					}
				}
				after, _, _ = h.getRow(ctx, tx, ws, spec, after.uuid(), nil)
			}
		}
		return after, nil
	}
	if !after.has("matterId") {
		return after, nil
	}
	m, _, err := h.getRow(ctx, tx, ws, specFor("legal_matters"), after.id("matterId"), nil)
	if err != nil {
		return nil, refuse("matterId", "Pick the matter.")
	}
	switch spec.Key {
	case "advocate_assignments":
		return h.assignmentSaved(ctx, tx, ws, a, op, before, after, m)
	case "accident_details", "family_details":
		if isTrue(after.Values["involvesMinor"]) && (!after.has("guardianContactId") || !isTrue(after.Values["guardianConsent"])) {
			return nil, refuse("guardianConsent", "A minor is involved: add the guardian and confirm their consent.")
		}
	case "legal_deadlines":
		if after, err = h.deadlineSaved(ctx, tx, ws, after, m); err != nil {
			return nil, err
		}
	case "legal_notices":
		set := map[string]any{}
		if s, ok := parseWhen(after.text("servedOn")); ok && after.num("cureDays") > 0 {
			set["cureExpiry"] = s.AddDate(0, 0, int(after.num("cureDays"))).Format("2006-01-02")
		}
		if after, err = h.legalSet(ctx, tx, ws, "legal_notices", after, set); err != nil {
			return nil, err
		}
	case "case_updates":
		if isTrue(after.Values["approvedForClient"]) && (isTrue(after.Values["privileged"]) || !after.has("clientSummary")) {
			return nil, refuse("approvedForClient", "Only a non-privileged update with a client-safe summary can go to the client.")
		}
	case "case_expenses":
		st := after.text("status")
		if op == "create" {
			target := "pending"
			if mayDirect(ctx, a, m) {
				target = "approved"
			}
			if after, err = h.legalSet(ctx, tx, ws, "case_expenses", after, map[string]any{"status": target}); err != nil {
				return nil, err
			}
		} else if st != before.text("status") {
			if !mayDirect(ctx, a, m) {
				return nil, refuse("status", "Only the main or supervising advocate can approve expenses.")
			}
			if a.ID != nil {
				if after, err = h.legalSet(ctx, tx, ws, "case_expenses", after, map[string]any{"approvedBy": a.ID.String()}); err != nil {
					return nil, err
				}
			}
		}
	case "legal_documents":
		if isTrue(after.Values["electronicEvidence"]) && after.text("section63") == "" {
			if after, err = h.legalSet(ctx, tx, ws, "legal_documents", after, map[string]any{"section63": "pending"}); err != nil {
				return nil, err
			}
		}
		if st := after.text("status"); st == "" {
			if after, err = h.legalSet(ctx, tx, ws, "legal_documents", after, map[string]any{"status": "received"}); err != nil {
				return nil, err
			}
		} else if st != before.text("status") && st == "verified" && a.ID != nil {
			if after, err = h.legalSet(ctx, tx, ws, "legal_documents", after, map[string]any{"verifiedBy": a.ID.String(), "verifiedOn": today()}); err != nil {
				return nil, err
			}
		}
		// A linked checklist item follows its document.
		if _, err := tx.Exec(ctx, `UPDATE crm.object_records SET status = $3, updated_at = now() WHERE workspace_id = $1 AND object_key = 'client_requirements' AND deleted_at IS NULL AND custom->>'documentId' = $2`, ws, after.ID, after.text("status")); err != nil {
			return nil, err
		}
	case "client_requirements":
		if after.has("documentId") {
			var ds string
			_ = tx.QueryRow(ctx, `SELECT COALESCE(status, '') FROM crm.object_records WHERE id::text = $1 AND workspace_id = $2`, after.text("documentId"), ws).Scan(&ds)
			if after, err = h.legalSet(ctx, tx, ws, "client_requirements", after, map[string]any{"status": ds, "receivedOn": firstNonEmpty(after.text("receivedOn"), today())}); err != nil {
				return nil, err
			}
		}
	case "legal_invoices":
		if after, err = h.legalInvoiceSaved(ctx, tx, ws, after, m); err != nil {
			return nil, err
		}
	case "legal_payments":
		if id := after.id("invoiceId"); id != uuid.Nil {
			inv, _, err := h.getRow(ctx, tx, ws, specFor("legal_invoices"), id, nil)
			if err != nil {
				return nil, refuse("invoiceId", "Pick the invoice.")
			}
			if inv.text("matterId") != after.text("matterId") {
				return nil, refuse("invoiceId", "This invoice belongs to another matter.")
			}
			if _, err := h.legalInvoiceSaved(ctx, tx, ws, inv, m); err != nil {
				return nil, err
			}
		}
		if err := h.rollupMatter(ctx, tx, ws, m.ID); err != nil {
			return nil, err
		}
	}
	if op == "create" {
		if err := h.syncMatterTeam(ctx, tx, ws, m.ID); err != nil {
			return nil, err
		}
	}
	return after, nil
}

func (h *Handler) lastConflict(ctx context.Context, tx pgx.Tx, ws, intake uuid.UUID) (*Row, []Field, error) {
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT id FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'conflict_checks' AND custom->>'intakeId' = $2 ORDER BY created_at DESC LIMIT 1`, ws, intake.String()).Scan(&id); err != nil {
		return nil, nil, err
	}
	return h.getRow(ctx, tx, ws, specFor("conflict_checks"), id, nil)
}

// matterGate: the matter's own rules (CNR format; what closing needs).
func (h *Handler) matterGate(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, m *Row) error {
	if c := m.text("cnrNumber"); c != "" && !cnrRe.MatchString(c) {
		return refuse("cnrNumber", "A CNR number has 16 letters and digits.")
	}
	if m.text("status") != "closed" || before.text("status") == "closed" {
		return nil
	}
	if !m.has("closureReason") {
		return refuse("closureReason", "Choose why the matter is closing.")
	}
	if !m.has("outcome") {
		return refuse("outcome", "Write the final outcome.")
	}
	if m.num("outstanding") > 0.005 && !isTrue(m.Values["balanceWrittenOff"]) {
		return refuse("outstanding", fmt.Sprintf("₹%.2f is still outstanding. Record the payment or mark the balance written off.", m.num("outstanding")))
	}
	var scheduled, orders int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'court_hearings' AND deleted_at IS NULL AND status = 'scheduled' AND custom->>'matterId' = $2`, ws, m.ID).Scan(&scheduled)
	if scheduled > 0 {
		return refuse("status", "This matter still has a scheduled hearing. Update or cancel it first.")
	}
	var inCourt int
	_ = tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'court_hearings' AND deleted_at IS NULL AND custom->>'matterId' = $2`, ws, m.ID).Scan(&inCourt)
	if inCourt > 0 || m.has("courtCaseNumber") {
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'legal_documents' AND deleted_at IS NULL AND custom->>'matterId' = $2 AND custom->>'category' IN ('court_order', 'settlement')`, ws, m.ID).Scan(&orders)
		if orders == 0 {
			return refuse("status", "Add the final court order or settlement document before closing.")
		}
	}
	return nil
}

// legalDeleteGate: a file on legal hold, or still inside its retention period, can't be deleted;
// nor can anything inside it, or the client it belongs to while on hold.
func (h *Handler) legalDeleteGate(ctx context.Context, tx pgx.Tx, ws uuid.UUID, key string, r *Row) error {
	var held, keep int
	switch {
	case key == "legal_matters":
		if isTrue(r.Values["legalHold"]) {
			held = 1
		}
		if u := r.text("retentionUntil"); u != "" && r.text("status") == "closed" && u >= today() {
			keep = 1
		}
	case key == "contacts" || key == "accounts":
		if !h.legalTrade(ctx, tx, ws) {
			return nil
		}
		field := map[string]string{"contacts": "contactId", "accounts": "accountId"}[key]
		_ = tx.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'legal_matters' AND deleted_at IS NULL AND custom->>'legalHold' = 'true' AND custom->>'`+field+`' = $2`, ws, r.ID).Scan(&held)
	case legalMoreKeys[key] || key == "court_hearings":
		if !r.has("matterId") {
			return nil
		}
		_ = tx.QueryRow(ctx, `SELECT count(*) FILTER (WHERE custom->>'legalHold' = 'true'), count(*) FILTER (WHERE status = 'closed' AND COALESCE(custom->>'retentionUntil', '') >= $3)
			FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'legal_matters' AND id::text = $2`, ws, r.text("matterId"), today()).Scan(&held, &keep)
	}
	if held > 0 {
		return shared.NewError(http.StatusUnprocessableEntity, "legal_hold", "This file is on legal hold and can't be deleted. Lift the hold first.")
	}
	if keep > 0 {
		return shared.NewError(http.StatusUnprocessableEntity, "retention_period", "This closed file is inside its retention period and can't be deleted yet.")
	}
	return nil
}
