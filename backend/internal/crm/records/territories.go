package records

import (
	"context"
	"errors"
	"log/slog"
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

// Territories (D-124). A territory is a record (object "territories") in a tree —
// India → Tamil Nadu → Coimbatore — with a manager. crm.territory_assignments says which
// accounts, people and teams belong to it, with dates: moving an account ends the old
// assignment and starts a new one, so history stays.
//
// A deal's territory (opportunities.territoryId) is resolved when the deal is made: the
// one typed on it → its account's territory → its owner's territory → the territory of a
// team its owner is in. Open deals follow their account when it moves; closed deals keep
// the territory they were closed in.
//
//	GET    /w/{code}/territories/tree
//	GET    /w/{code}/territories/{id}/assignments      ?history=1
//	POST   /w/{code}/territories/{id}/assignments      {kind, accountId | identityId | teamId, isPrimary, effectiveFrom}
//	DELETE /w/{code}/territory-assignments/{id}        (ends it today; the row is kept)
//	GET    /w/{code}/crm/accounts/{id}/territories

func (h *Handler) territoryRoutes(r chi.Router) {
	r.Get("/territories/tree", h.handleTerritoryTree)
	r.Get("/territories/{id}/assignments", h.handleListTerritoryAssignments)
	r.Post("/territories/{id}/assignments", h.handleAssignTerritory)
	r.Delete("/territory-assignments/{id}", h.handleEndTerritoryAssignment)
	r.Get("/crm/accounts/{id}/territories", h.handleAccountTerritories)
}

const sourceTerritory = "territory"

func canManageTerritories(sc *Scope) bool {
	return sc.Owner || sc.Eff.HasCapability(access.CapTerritories)
}

type TerritoryNode struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Name     string `json:"name"`
	ParentID string `json:"parentId,omitempty"`
	Type     string `json:"type,omitempty"`
	Status   string `json:"status,omitempty"`
	Manager  string `json:"manager,omitempty"`
	Depth    int    `json:"depth"`
	Accounts int    `json:"accounts"`
	People   int    `json:"people"`
	Teams    int    `json:"teams"`
}

// territoryTree returns every territory of a business in tree order (parents before children).
func (h *Handler) territoryTree(ctx context.Context, q querier, ws uuid.UUID) ([]TerritoryNode, error) {
	rows, err := q.Query(ctx, `
		SELECT t.id::text, t.code, t.name, COALESCE(t.custom->>'parentTerritoryId', ''), COALESCE(t.custom->>'territoryType', ''), COALESCE(t.status, ''),
		       COALESCE(m.display_name, ''),
		       (SELECT count(*) FROM crm.territory_assignments a WHERE a.territory_id = t.id AND a.kind = 'account' AND a.effective_to IS NULL),
		       (SELECT count(*) FROM crm.territory_assignments a WHERE a.territory_id = t.id AND a.kind = 'user' AND a.effective_to IS NULL),
		       (SELECT count(*) FROM crm.territory_assignments a WHERE a.territory_id = t.id AND a.kind = 'team' AND a.effective_to IS NULL)
		FROM crm.object_records t
		LEFT JOIN crm.identities m ON m.id::text = t.custom->>'managerId'
		WHERE t.workspace_id = $1 AND t.object_key = 'territories' AND t.deleted_at IS NULL ORDER BY t.name`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byParent := map[string][]TerritoryNode{}
	known := map[string]bool{}
	var all []TerritoryNode
	for rows.Next() {
		var n TerritoryNode
		if err := rows.Scan(&n.ID, &n.Code, &n.Name, &n.ParentID, &n.Type, &n.Status, &n.Manager, &n.Accounts, &n.People, &n.Teams); err != nil {
			return nil, err
		}
		known[n.ID] = true
		all = append(all, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, n := range all {
		parent := n.ParentID
		if !known[parent] {
			parent = "" // its parent was deleted: it shows at the top
		}
		byParent[parent] = append(byParent[parent], n)
	}
	out := []TerritoryNode{}
	var walk func(parent string, depth int)
	walk = func(parent string, depth int) {
		for _, n := range byParent[parent] {
			if depth > 20 {
				return
			}
			n.Depth = depth
			out = append(out, n)
			walk(n.ID, depth+1)
		}
	}
	walk("", 0)
	return out, nil
}

func (h *Handler) handleTerritoryTree(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if specFor("territories") == nil || !sc.Can("territories", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	list, err := h.territoryTree(r.Context(), h.store.Pool, sc.WS)
	respond(w, r, http.StatusOK, map[string]any{"data": list, "canManage": canManageTerritories(sc)}, err)
}

// territorySaved keeps the tree a tree: a territory can't sit under itself or under one of
// its own descendants.
func (h *Handler) territorySaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, after *Row) error {
	parent := after.id("parentTerritoryId")
	if parent == uuid.Nil {
		return nil
	}
	var loops bool
	err := tx.QueryRow(ctx, `
		WITH RECURSIVE up AS (
		  SELECT id, custom->>'parentTerritoryId' AS parent, 1 AS depth FROM crm.object_records WHERE id = $2 AND workspace_id = $1 AND object_key = 'territories'
		  UNION ALL
		  SELECT t.id, t.custom->>'parentTerritoryId', up.depth + 1 FROM crm.object_records t JOIN up ON t.id::text = up.parent
		  WHERE t.workspace_id = $1 AND t.object_key = 'territories' AND up.depth < 50)
		SELECT EXISTS (SELECT 1 FROM up WHERE id = $3)`, ws, parent, after.uuid()).Scan(&loops)
	if err != nil {
		return err
	}
	if loops {
		return shared.Validation(map[string]string{"parentTerritoryId": "A territory can't be placed under itself or under one of its own sub-territories."})
	}
	return nil
}

type TerritoryAssignment struct {
	ID            string  `json:"id"`
	TerritoryID   string  `json:"territoryId"`
	Territory     string  `json:"territory,omitempty"`
	Kind          string  `json:"kind"`
	TargetID      string  `json:"targetId"`
	Target        string  `json:"target"`
	Primary       bool    `json:"isPrimary"`
	EffectiveFrom string  `json:"effectiveFrom"`
	EffectiveTo   *string `json:"effectiveTo,omitempty"`
}

const assignmentSelect = `
	SELECT a.id::text, a.territory_id::text, COALESCE(t.name, ''), a.kind, COALESCE(a.account_id, a.identity_id, a.team_id)::text,
	       COALESCE(acc.name, i.display_name, tm.name, ''), a.is_primary, a.effective_from::text, a.effective_to::text
	FROM crm.territory_assignments a
	LEFT JOIN crm.object_records t ON t.id = a.territory_id
	LEFT JOIN crm.accounts acc ON acc.id = a.account_id
	LEFT JOIN crm.identities i ON i.id = a.identity_id
	LEFT JOIN crm.teams tm ON tm.id = a.team_id`

func scanAssignments(rows pgx.Rows) ([]TerritoryAssignment, error) {
	defer rows.Close()
	list := []TerritoryAssignment{}
	for rows.Next() {
		var x TerritoryAssignment
		if err := rows.Scan(&x.ID, &x.TerritoryID, &x.Territory, &x.Kind, &x.TargetID, &x.Target, &x.Primary, &x.EffectiveFrom, &x.EffectiveTo); err != nil {
			return nil, err
		}
		list = append(list, x)
	}
	return list, rows.Err()
}

func (h *Handler) territoryScope(r *http.Request) (*Scope, *Row, error) {
	sc := scopeFrom(r.Context())
	spec := specFor("territories")
	if spec == nil || !sc.Can("territories", "read") {
		return nil, nil, errForbidden
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return nil, nil, shared.NotFound("record_not_found")
	}
	// The territory structure is shared by the whole business: anyone who may read territories sees all of it.
	row, _, err := h.getRow(r.Context(), h.store.Pool, sc.WS, spec, id, nil)
	return sc, row, err
}

func (h *Handler) handleListTerritoryAssignments(w http.ResponseWriter, r *http.Request) {
	sc, t, err := h.territoryScope(r)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	history := r.URL.Query().Get("history") != ""
	rows, err := h.store.Pool.Query(r.Context(), assignmentSelect+` WHERE a.workspace_id = $1 AND a.territory_id = $2 AND ($3 OR a.effective_to IS NULL)
		ORDER BY (a.effective_to IS NULL) DESC, a.kind, 6 LIMIT 1000`, sc.WS, t.uuid(), history)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := scanAssignments(rows)
	respond(w, r, http.StatusOK, map[string]any{"data": list, "canManage": canManageTerritories(sc)}, err)
}

func (h *Handler) handleAccountTerritories(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || !sc.Can("accounts", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	if _, _, err := h.getRow(r.Context(), h.store.Pool, sc.WS, specFor("accounts"), id, sc.OwnersFor("accounts", actor(r))); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	rows, err := h.store.Pool.Query(r.Context(), assignmentSelect+` WHERE a.workspace_id = $1 AND a.account_id = $2 ORDER BY (a.effective_to IS NULL) DESC, a.is_primary DESC, a.effective_from DESC LIMIT 100`, sc.WS, id)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := scanAssignments(rows)
	respond(w, r, http.StatusOK, map[string]any{"data": list, "canManage": canManageTerritories(sc) && specFor("territories") != nil}, err)
}

func (h *Handler) handleAssignTerritory(w http.ResponseWriter, r *http.Request) {
	sc, t, err := h.territoryScope(r)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if !canManageTerritories(sc) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	var in struct {
		Kind          string `json:"kind"`
		AccountID     string `json:"accountId"`
		IdentityID    string `json:"identityId"`
		TeamID        string `json:"teamId"`
		Primary       *bool  `json:"isPrimary"`
		EffectiveFrom string `json:"effectiveFrom"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	if in.EffectiveFrom == "" {
		in.EffectiveFrom = time.Now().Format("2006-01-02")
	}
	primary := in.Primary == nil || *in.Primary
	fe := map[string]string{}
	if _, err := time.Parse("2006-01-02", in.EffectiveFrom); err != nil {
		fe["effectiveFrom"] = "Enter a date."
	}
	var account, person, team *uuid.UUID
	var ok bool
	// Whatever is assigned must belong to this business.
	switch in.Kind {
	case "account":
		id, err := uuid.Parse(in.AccountID)
		_ = h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.accounts WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL)`, id, sc.WS).Scan(&ok)
		if err != nil || !ok {
			fe["accountId"] = "Choose an account of this business."
		}
		account = &id
	case "user":
		id, err := uuid.Parse(in.IdentityID)
		_ = h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.memberships WHERE identity_id = $1 AND workspace_id = $2 AND status = 'active')`, id, sc.WS).Scan(&ok)
		if err != nil || !ok {
			fe["identityId"] = "Choose someone who works in this business."
		}
		person = &id
	case "team":
		id, err := uuid.Parse(in.TeamID)
		_ = h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.teams WHERE id = $1 AND workspace_id = $2)`, id, sc.WS).Scan(&ok)
		if err != nil || !ok {
			fe["teamId"] = "Choose a team of this business."
		}
		team = &id
	default:
		fe["kind"] = "Assign an account, a person or a team."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var already bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.territory_assignments WHERE workspace_id = $1 AND territory_id = $2 AND kind = $3
			AND COALESCE(account_id, identity_id, team_id) = COALESCE($4::uuid, $5::uuid, $6::uuid) AND effective_to IS NULL)`, sc.WS, t.uuid(), in.Kind, account, person, team).Scan(&already); err != nil {
			return err
		}
		if already {
			return shared.Validation(map[string]string{"kind": "Already assigned to this territory."})
		}
		var moved []string
		if primary {
			// One primary territory at a time: the previous one ends the day before. History is kept.
			rows, err := tx.Query(ctx, `UPDATE crm.territory_assignments SET effective_to = greatest($7::date - 1, effective_from), is_primary = is_primary
				WHERE workspace_id = $1 AND kind = $2 AND COALESCE(account_id, identity_id, team_id) = COALESCE($3::uuid, $4::uuid, $5::uuid)
				  AND is_primary AND effective_to IS NULL AND territory_id <> $6 RETURNING territory_id::text`, sc.WS, in.Kind, account, person, team, t.uuid(), in.EffectiveFrom)
			if err != nil {
				return err
			}
			for rows.Next() {
				var s string
				if rows.Scan(&s) == nil {
					moved = append(moved, s)
				}
			}
			rows.Close()
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO crm.territory_assignments (workspace_id, territory_id, kind, account_id, identity_id, team_id, is_primary, effective_from, created_by)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8::date, $9) RETURNING id`, sc.WS, t.uuid(), in.Kind, account, person, team, primary, in.EffectiveFrom, a.ID).Scan(&id); err != nil {
			return err
		}
		reassigned := int64(0)
		if account != nil && primary {
			// Open deals of the account follow it; closed deals keep the territory they closed in.
			tag, err := tx.Exec(ctx, `UPDATE crm.object_records SET custom = custom || jsonb_build_object('territoryId', $3::text), updated_at = now(), version = version + 1
				WHERE workspace_id = $1 AND object_key = 'opportunities' AND deleted_at IS NULL AND custom->>'accountId' = $2
				  AND COALESCE(status, '') NOT IN ('closed_won', 'closed_lost') AND COALESCE(custom->>'territoryId', '') <> $3`, sc.WS, account.String(), t.ID)
			if err != nil {
				return err
			}
			reassigned = tag.RowsAffected()
			if err := insertActivity(ctx, tx, sc.WS, "accounts", *account, "territory.assigned", "Territory: "+t.Title, map[string]any{"territoryId": t.ID, "from": moved, "openDealsMoved": reassigned}, a.ID); err != nil {
				return err
			}
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "territory.assigned", "territory_assignment", &id, map[string]any{"previous": moved},
			map[string]any{"territoryId": t.ID, "kind": in.Kind, "accountId": account, "identityId": person, "teamId": team, "primary": primary,
				"effectiveFrom": in.EffectiveFrom, "openDealsMoved": reassigned}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.bus.Kick()
	h.handleListTerritoryAssignments(w, r)
}

func (h *Handler) handleEndTerritoryAssignment(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !canManageTerritories(sc) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("assignment_not_found"))
		return
	}
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `UPDATE crm.territory_assignments SET effective_to = greatest(CURRENT_DATE, effective_from) WHERE id = $1 AND workspace_id = $2 AND effective_to IS NULL`, id, sc.WS)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return shared.NotFound("assignment_not_found")
		}
		return shared.WriteAudit(r.Context(), tx, a.audit(sc.WS, "territory.unassigned", "territory_assignment", &id, nil, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolveTerritory: the territory a deal belongs to, from its account, then its owner, then
// a team its owner is in ("" when none).
func (h *Handler) resolveTerritory(ctx context.Context, q querier, ws uuid.UUID, account, owner uuid.UUID) string {
	var id string
	err := q.QueryRow(ctx, `
		SELECT a.territory_id::text FROM crm.territory_assignments a
		JOIN crm.object_records t ON t.id = a.territory_id AND t.deleted_at IS NULL AND COALESCE(t.status, 'active') <> 'inactive'
		WHERE a.workspace_id = $1 AND a.effective_from <= CURRENT_DATE AND (a.effective_to IS NULL OR a.effective_to >= CURRENT_DATE)
		  AND ((a.kind = 'account' AND a.account_id = $2)
		    OR (a.kind = 'user' AND a.identity_id = $3)
		    OR (a.kind = 'team' AND a.team_id IN (SELECT tm.team_id FROM crm.team_members tm JOIN crm.memberships m ON m.id = tm.membership_id WHERE m.identity_id = $3 AND m.workspace_id = $1)))
		ORDER BY CASE a.kind WHEN 'account' THEN 0 WHEN 'user' THEN 1 ELSE 2 END, a.is_primary DESC, a.effective_from DESC LIMIT 1`,
		ws, nullUUID(account), nullUUID(owner)).Scan(&id)
	if err != nil {
		return ""
	}
	return id
}

// opportunityTerritory gives a new deal its territory, and looks again when its account
// changes while it has none typed in.
func (h *Handler) opportunityTerritory(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, after *Row) (*Row, error) {
	if specFor("territories") == nil || (op != "create" && op != "update") {
		return after, nil
	}
	if _, ok := specFor("opportunities").field("territoryId"); !ok {
		return after, nil
	}
	// A deal that was already closed keeps its territory; one created closed still gets one.
	closed := op == "update" && (after.text("status") == "closed_won" || after.text("status") == "closed_lost")
	accountChanged := op == "update" && before.id("accountId") != after.id("accountId")
	typed := op == "update" && before.text("territoryId") != after.text("territoryId")
	if closed || typed || (op == "update" && !accountChanged && after.text("territoryId") != "") || (op == "create" && after.text("territoryId") != "") {
		return after, nil
	}
	want := h.resolveTerritory(ctx, tx, ws, after.id("accountId"), after.id("ownerId"))
	if want == "" || want == after.text("territoryId") {
		return after, nil
	}
	return h.updateValues(ctx, tx, ws, specFor("opportunities"), after.uuid(), systemActor(sourceTerritory), map[string]any{"territoryId": want}, nil, nil)
}

// forecastDimension is the forecast by territory (each territory with everything below it
// rolled up) or by role. The grand total is computed elsewhere from the deals themselves,
// so a deal that shows in a territory and in its parent is still counted once.
func (h *Handler) forecastDimension(ctx context.Context, sc *Scope, p ForecastPeriod, pipeline string, owners []uuid.UUID, dim string, people map[uuid.UUID]ForecastNumbers) ([]ForecastRow, error) {
	out := []ForecastRow{}
	if dim == "role" {
		rows, err := h.store.Pool.Query(ctx, `
			SELECT DISTINCT ON (m.identity_id) m.identity_id, r.id, r.name FROM crm.memberships m
			JOIN crm.role_assignments ra ON ra.membership_id = m.id JOIN crm.roles r ON r.id = ra.role_id
			WHERE m.workspace_id = $1 ORDER BY m.identity_id, r.rank DESC`, sc.WS)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		byRole := map[uuid.UUID]*ForecastRow{}
		seen := map[uuid.UUID]bool{}
		for rows.Next() {
			var person, role uuid.UUID
			var name string
			if err := rows.Scan(&person, &role, &name); err != nil {
				return nil, err
			}
			n, ok := people[person]
			if !ok {
				continue
			}
			seen[person] = true
			row := byRole[role]
			if row == nil {
				row = &ForecastRow{ID: role.String(), Name: name}
				byRole[role] = row
			}
			row.add(n)
			row.Quota += n.Quota
			row.Members++
		}
		rest := ForecastRow{Name: "No role"}
		for id, n := range people {
			if !seen[id] {
				rest.add(n)
				rest.Quota += n.Quota
				rest.Members++
			}
		}
		for _, row := range byRole {
			row.derive()
			out = append(out, *row)
		}
		rest.derive()
		if rest.OpenDeals > 0 || rest.WonDeals > 0 || rest.Lost > 0 {
			out = append(out, rest)
		}
		return out, rows.Err()
	}
	if specFor("territories") == nil || !sc.Can("territories", "read") {
		return out, nil
	}
	byTerritory, err := h.forecastBy(ctx, sc.WS, p, pipeline, owners, forecastTerritoryExpr)
	if err != nil {
		return nil, err
	}
	tree, err := h.territoryTree(ctx, h.store.Pool, sc.WS)
	if err != nil {
		return nil, err
	}
	// Roll each territory's own deals up into every ancestor.
	total := map[string]ForecastNumbers{}
	parent := map[string]string{}
	for _, n := range tree {
		parent[n.ID] = n.ParentID
	}
	known := map[uuid.UUID]bool{}
	for _, n := range tree {
		id, _ := uuid.Parse(n.ID)
		known[id] = true
		own := byTerritory[id]
		for at, hops := n.ID, 0; at != "" && hops < 30; at, hops = parent[at], hops+1 {
			sum := total[at]
			sum.add(own)
			total[at] = sum
		}
	}
	for _, n := range tree {
		sum := total[n.ID]
		sum.derive()
		row := ForecastRow{ID: n.ID, Name: strings.Repeat("— ", n.Depth) + n.Name, ForecastNumbers: sum, Members: n.People}
		out = append(out, row)
	}
	rest := ForecastRow{Name: "No territory"}
	for id, n := range byTerritory {
		if !known[id] {
			rest.add(n)
		}
	}
	rest.derive()
	if rest.OpenDeals > 0 || rest.WonDeals > 0 || rest.Lost > 0 {
		out = append(out, rest)
	}
	return out, nil
}

// handleForecastHistory: every submission and decision of a period, with how the
// submitted number compares to what actually closed.
func (h *Handler) handleForecastHistory(w http.ResponseWriter, r *http.Request) {
	sc, err := h.forecastAccess(r)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	me := actor(r)
	start := h.fiscalStart(ctx, sc.WS)
	period, err := parsePeriod(r.URL.Query().Get("period"), start)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	owners := sc.OwnersFor("opportunities", me)
	type event struct {
		Owner          string    `json:"owner"`
		OwnerID        string    `json:"ownerId"`
		Event          string    `json:"event"`
		ClosedWon      float64   `json:"closedWon"`
		Commit         float64   `json:"commit"`
		BestCase       float64   `json:"bestCase"`
		Pipeline       float64   `json:"pipeline"`
		ForecastAmount float64   `json:"forecastAmount"`
		OverrideAmount *float64  `json:"overrideAmount,omitempty"`
		Quota          float64   `json:"quota"`
		Comment        string    `json:"comment,omitempty"`
		Actor          string    `json:"actor,omitempty"`
		At             time.Time `json:"at"`
	}
	rows, err := h.store.Pool.Query(ctx, `
		SELECT COALESCE(o.display_name, ''), f.owner_id::text, f.event, f.closed_won::float8, f.commit_amount::float8, f.best_case::float8, f.pipeline_amount::float8,
		       f.forecast_amount::float8, f.override_amount::float8, f.quota::float8, f.comment, COALESCE(a.display_name, ''), f.created_at
		FROM crm.forecast_history f LEFT JOIN crm.identities o ON o.id = f.owner_id LEFT JOIN crm.identities a ON a.id = f.actor_id
		WHERE f.workspace_id = $1 AND f.period_key = $2 AND ($3::uuid[] IS NULL OR f.owner_id = ANY($3)) ORDER BY f.created_at DESC LIMIT 500`, sc.WS, period.Key, owners)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	events := []event{}
	latest := map[string]float64{}
	for rows.Next() {
		var e event
		if err := rows.Scan(&e.Owner, &e.OwnerID, &e.Event, &e.ClosedWon, &e.Commit, &e.BestCase, &e.Pipeline, &e.ForecastAmount, &e.OverrideAmount, &e.Quota, &e.Comment, &e.Actor, &e.At); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		if _, ok := latest[e.OwnerID]; !ok && e.Event != "rejected" {
			latest[e.OwnerID] = e.ForecastAmount
			if e.OverrideAmount != nil {
				latest[e.OwnerID] = *e.OverrideAmount
			}
		}
		events = append(events, e)
	}
	// Accuracy: the latest number each person stood behind against what they closed.
	actual, err := h.forecastByOwner(ctx, sc.WS, period, "", owners)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	type accuracy struct {
		OwnerID   string   `json:"ownerId"`
		Submitted float64  `json:"submitted"`
		Closed    float64  `json:"closedWon"`
		Accuracy  *float64 `json:"accuracyPercent,omitempty"` // 100 = exact; absent until something closed
	}
	acc := []accuracy{}
	for owner, submitted := range latest {
		id, _ := uuid.Parse(owner)
		x := accuracy{OwnerID: owner, Submitted: submitted, Closed: actual[id].Closed}
		if x.Closed > 0 {
			diff := submitted - x.Closed
			if diff < 0 {
				diff = -diff
			}
			v := 100 - diff/x.Closed*100
			if v < 0 {
				v = 0
			}
			x.Accuracy = &v
		}
		acc = append(acc, x)
	}
	sort.Slice(acc, func(i, j int) bool { return acc[i].OwnerID < acc[j].OwnerID })
	shared.WriteJSON(w, http.StatusOK, map[string]any{"period": period, "events": events, "accuracy": acc})
}

// BackfillTerritories (once): the free-text territory typed on deals becomes a territory
// record with that name, and the deal points at it. The text stays where it was.
func (h *Handler) BackfillTerritories(ctx context.Context) {
	const marker = "territories:from-text-v1"
	var done bool
	if err := h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.connector_state WHERE key = $1)`, marker).Scan(&done); err != nil || done || specFor("territories") == nil {
		return
	}
	made, linked := 0, int64(0)
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT DISTINCT workspace_id, trim(custom->>'territory') FROM crm.object_records
			WHERE object_key = 'opportunities' AND deleted_at IS NULL AND trim(COALESCE(custom->>'territory', '')) <> '' AND COALESCE(custom->>'territoryId', '') = ''`)
		if err != nil {
			return err
		}
		type item struct {
			ws   uuid.UUID
			name string
		}
		var list []item
		for rows.Next() {
			var x item
			if err := rows.Scan(&x.ws, &x.name); err != nil {
				rows.Close()
				return err
			}
			list = append(list, x)
		}
		rows.Close()
		for _, x := range list {
			var id string
			err := tx.QueryRow(ctx, `SELECT id::text FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'territories' AND deleted_at IS NULL AND lower(name) = lower($2) LIMIT 1`, x.ws, x.name).Scan(&id)
			if errors.Is(err, pgx.ErrNoRows) {
				row, err := h.createRecord(ctx, tx, x.ws, specFor("territories"), actorInfo{Kind: "system", Source: "backfill"},
					map[string]any{"name": clipText(x.name, 120), "status": "active", "description": "Created from the territory text typed on opportunities."})
				if err != nil {
					return err
				}
				id = row.ID
				made++
			} else if err != nil {
				return err
			}
			tag, err := tx.Exec(ctx, `UPDATE crm.object_records SET custom = custom || jsonb_build_object('territoryId', $3::text)
				WHERE workspace_id = $1 AND object_key = 'opportunities' AND deleted_at IS NULL AND lower(trim(custom->>'territory')) = lower($2) AND COALESCE(custom->>'territoryId', '') = ''`, x.ws, x.name, id)
			if err != nil {
				return err
			}
			linked += tag.RowsAffected()
		}
		_, err = tx.Exec(ctx, `INSERT INTO crm.connector_state (key, value) VALUES ($1, jsonb_build_object('territories', $2::int, 'opportunities', $3::int)) ON CONFLICT (key) DO NOTHING`, marker, made, linked)
		return err
	})
	if err != nil {
		slog.Error("crm: territories not created from opportunity text", "error", err)
	} else if made > 0 {
		slog.Info("crm: territories created from the text on opportunities", "territories", made, "opportunities", linked)
	}
}
