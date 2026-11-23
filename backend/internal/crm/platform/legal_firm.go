package platform

import (
	"context"
	"encoding/json"

	"cardflow-backend/internal/crm/access"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// legalFirmDefaults gives a new law firm its stages and team (D-185), once, when it is
// created. Main Advocate is the business's Admin; juniors report to a senior; a clerk never
// sees fees; accounts never sees case notes. The firm can change all of it in Settings.
func legalFirmDefaults(ctx context.Context, tx pgx.Tx, ws uuid.UUID) error {
	if _, err := tx.Exec(ctx, `UPDATE crm.workspaces SET stage_settings = COALESCE(stage_settings, '{}'::jsonb) || $2::jsonb WHERE id = $1`, ws, legalStages()); err != nil {
		return err
	}
	return legalFirmRoles(ctx, tx, ws)
}

func legalStages() []byte {
	st, _ := json.Marshal(StageSettings{
		LeadStatuses: []Option{{Value: "new", Label: "New"}, {Value: "contacted", Label: "Contacted"}, {Value: "consultation_done", Label: "Consultation done"},
			{Value: "information_pending", Label: "Information pending"}, {Value: "qualified", Label: "Qualified"}, {Value: "proposal", Label: "Proposal in progress"},
			{Value: "converted", Label: "Converted"}, {Value: "not_interested", Label: "Not interested"}, {Value: "lost", Label: "Lost"}},
		PipelineStages: []Stage{{"qualification", "Qualification", 10}, {"consultation", "Consultation", 20}, {"legal_assessment", "Legal assessment", 35}, {"quote_preparation", "Quote preparation", 50},
			{"quote_sent", "Quote sent", 60}, {"negotiation", "Negotiation", 75}, {"client_acceptance", "Client acceptance", 90}, {"closed_won", "Closed won", 100}, {"closed_lost", "Closed lost", 0}},
	})
	return st
}

// MigrateLegalFirms (start-up, once) gives law firms made before D-185 the same roles. They
// get the stages too, unless they set their own or have a record on a step the new list lacks.
func MigrateLegalFirms(ctx context.Context, pool *pgxpool.Pool) error {
	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		var done bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.connector_state WHERE key = 'access:legal-firms-v1')`).Scan(&done); err != nil || done {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT id FROM crm.workspaces WHERE NOT is_platform AND (lower(COALESCE(profile->>'industry', '')) LIKE '%legal%' OR COALESCE(profile->>'packs', '') ILIKE '%legal%')`)
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
		for _, ws := range ids {
			var has bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.permission_sets WHERE workspace_id = $1 AND system_key = 'STAFF')`, ws).Scan(&has); err != nil {
				return err
			}
			if !has {
				continue
			}
			if err := legalFirmRoles(ctx, tx, ws); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE crm.workspaces w SET stage_settings = COALESCE(w.stage_settings, '{}'::jsonb) || $2::jsonb
				WHERE w.id = $1 AND NOT COALESCE(w.stage_settings, '{}'::jsonb) ?| array['leadStatuses', 'pipelineStages']
				  AND NOT EXISTS (SELECT 1 FROM crm.leads l WHERE l.workspace_id = w.id AND l.deleted_at IS NULL AND COALESCE(l.status, 'new') NOT IN ('new', 'contacted', 'consultation_done', 'information_pending', 'qualified', 'proposal', 'converted', 'not_interested', 'lost'))
				  AND NOT EXISTS (SELECT 1 FROM crm.object_records o WHERE o.workspace_id = w.id AND o.object_key = 'opportunities' AND o.deleted_at IS NULL AND COALESCE(o.status, 'qualification') NOT IN ('qualification', 'consultation', 'legal_assessment', 'quote_preparation', 'quote_sent', 'negotiation', 'client_acceptance', 'closed_won', 'closed_lost'))`, ws, legalStages()); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO crm.connector_state (key, value) VALUES ('access:legal-firms-v1', '{"done":true}') ON CONFLICT (key) DO NOTHING`)
		return err
	})
}

func legalFirmRoles(ctx context.Context, tx pgx.Tx, ws uuid.UUID) error {
	var raw []byte
	if err := tx.QueryRow(ctx, `SELECT rules FROM crm.permission_sets WHERE workspace_id = $1 AND system_key = 'STAFF'`, ws).Scan(&raw); err != nil {
		return err
	}
	work := []string{"read", "create", "update"}
	read := []string{"read"}
	caseKeys := []string{"legal_matters", "court_hearings", "case_intakes", "conflict_checks", "accident_details", "property_details", "family_details", "criminal_details", "contract_details",
		"case_parties", "client_requirements", "legal_documents", "advocate_assignments", "legal_deadlines", "legal_notices", "case_updates"}
	moneyKeys := []string{"legal_invoices", "legal_payments", "payment_milestones", "case_expenses"}
	refKeys := []string{"service_scopes", "service_requirements", "courts", "advocate_profiles"}
	staff := func() access.Rules {
		var r access.Rules
		_ = json.Unmarshal(raw, &r)
		if r.Objects == nil {
			r.Objects = map[string][]string{}
		}
		if r.Rows == nil {
			r.Rows = map[string]map[string]string{}
		}
		if r.Fields == nil {
			r.Fields = map[string]map[string]string{}
		}
		for _, k := range caseKeys {
			r.Objects[k], r.Rows[k] = work, map[string]string{"scope": "team"}
		}
		for _, k := range refKeys {
			r.Objects[k], r.Rows[k] = read, map[string]string{"scope": "workspace"}
		}
		return r
	}
	advocate := func() access.Rules {
		r := staff()
		for _, k := range moneyKeys {
			r.Objects[k], r.Rows[k] = work, map[string]string{"scope": "team"}
		}
		return r
	}
	clerk := staff()
	for _, k := range append([]string{"quotes", "opportunities", "invoices", "payments", "income", "expenses"}, moneyKeys...) {
		delete(clerk.Objects, k)
		delete(clerk.Rows, k)
	}
	clerk.Fields["legal_matters"] = map[string]string{"feeAgreed": "hidden", "feeReceived": "hidden", "outstanding": "hidden"}
	books := access.Rules{Objects: map[string][]string{"contact": read, "account": read, "legal_matters": read}, Rows: map[string]map[string]string{
		"contact": {"scope": "workspace"}, "account": {"scope": "workspace"}, "legal_matters": {"scope": "workspace"}},
		Capabilities: []string{access.CapDashboard},
		Fields:       map[string]map[string]string{"legal_matters": {"description": "hidden", "outcome": "hidden", "opposingCounsel": "hidden"}}}
	for _, k := range moneyKeys {
		books.Objects[k], books.Rows[k] = work, map[string]string{"scope": "workspace"}
	}
	for _, x := range []struct {
		key, name, parent, desc string
		rules                   access.Rules
	}{
		{"SENIOR_ADVOCATE", "Senior Advocate", "ADMIN", "Their matters and their juniors' matters.", advocate()},
		{"JUNIOR_ADVOCATE", "Junior Advocate", "SENIOR_ADVOCATE", "Only the matters they are assigned to.", advocate()},
		{"LEGAL_ASSISTANT", "Legal Assistant", "SENIOR_ADVOCATE", "Documents and hearings of assigned matters. No fees.", clerk},
		{"LEGAL_ACCOUNTS", "Accounts", "ADMIN", "Invoices, payments and expenses. No case notes or documents.", books},
	} {
		base, _ := json.Marshal(x.rules)
		if _, err := tx.Exec(ctx, `INSERT INTO crm.roles (workspace_id, key, name, is_system, rank, base_rules, parent_role_id)
			SELECT $1, $2, $3, false, $4, $5, (SELECT id FROM crm.roles WHERE workspace_id = $1 AND key = $6)
			WHERE NOT EXISTS (SELECT 1 FROM crm.roles WHERE workspace_id = $1 AND key = $2)`, ws, x.key, x.name, access.RankStaff, base, x.parent); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO crm.permission_sets (workspace_id, name, description, rules, system_key)
			SELECT $1, $2, $3, $4, $5 WHERE NOT EXISTS (SELECT 1 FROM crm.permission_sets WHERE workspace_id = $1 AND (system_key = $5 OR lower(name) = lower($2)))`,
			ws, x.name+" access", x.desc, base, x.key); err != nil {
			return err
		}
	}
	return nil
}
