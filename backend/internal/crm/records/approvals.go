package records

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Approvals (D-121). A business sets limits: a discount of 10% or more needs a sales
// manager, 20% a director; a write-off above ₹5,000 needs an admin. When a quote, refund,
// credit note, debit note or adjustment crosses a limit, a request is opened and the
// record waits. Someone in the named role — or a role above it — decides; the person who
// asked can't approve their own request unless they manage approvals themselves.
//
//	GET    /w/{code}/approvals/rules
//	PUT    /w/{code}/approvals/rules        {kind, rules: [{minValue, approverRole, label}]}
//	GET    /w/{code}/approvals              ?status=pending|all
//	POST   /w/{code}/approvals/{id}/decide  {status: approved|rejected, note}

func (h *Handler) approvalRoutes(r chi.Router) {
	r.Get("/approvals/rules", h.handleApprovalRules)
	r.Put("/approvals/rules", h.handleSaveApprovalRules)
	r.Get("/approvals", h.handleListApprovals)
	r.Post("/approvals/{id}/decide", h.handleDecideApproval)
}

var approvalKinds = map[string]string{"discount": "Discount (%)", "refund": "Refund (amount)", "credit_note": "Credit note (amount)",
	"debit_note": "Debit note (amount)", "write_off": "Write-off or adjustment (amount)"}

type ApprovalRule struct {
	Kind         string `json:"kind"`
	MinValue     Cents  `json:"minValue"`
	ApproverRole string `json:"approverRole"`
	Label        string `json:"label"`
}

func canManageApprovals(sc *Scope) bool {
	return sc.Owner || sc.Eff.HasCapability(access.CapApprovals)
}

func (h *Handler) handleApprovalRules(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	rows, err := h.store.Pool.Query(r.Context(), `SELECT kind, min_value::text, approver_role, label FROM crm.approval_rules WHERE workspace_id = $1 ORDER BY kind, min_value`, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	list := []ApprovalRule{}
	for rows.Next() {
		var x ApprovalRule
		var v string
		if err := rows.Scan(&x.Kind, &v, &x.ApproverRole, &x.Label); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		x.MinValue = mustCents(v)
		list = append(list, x)
	}
	type role struct {
		Key  string `json:"key"`
		Name string `json:"name"`
	}
	roles := []role{}
	rr, err := h.store.Pool.Query(r.Context(), `SELECT key, name FROM crm.roles WHERE workspace_id = $1 ORDER BY rank DESC, name`, sc.WS)
	if err == nil {
		for rr.Next() {
			var x role
			if rr.Scan(&x.Key, &x.Name) == nil {
				roles = append(roles, x)
			}
		}
		rr.Close()
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": list, "kinds": approvalKinds, "roles": roles, "canManage": canManageApprovals(sc)})
}

func (h *Handler) handleSaveApprovalRules(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !canManageApprovals(sc) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	var in struct {
		Kind  string         `json:"kind"`
		Rules []ApprovalRule `json:"rules"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	fe := map[string]string{}
	if approvalKinds[in.Kind] == "" {
		fe["kind"] = "Choose what the limits are for."
	}
	seen := map[Cents]bool{}
	for _, x := range in.Rules {
		var known bool
		_ = h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.roles WHERE workspace_id = $1 AND key = $2)`, sc.WS, x.ApproverRole).Scan(&known)
		switch {
		case !known:
			fe["rules"] = "Choose a role of this business for every limit."
		case x.MinValue < 0 || (in.Kind == "discount" && x.MinValue > 10000):
			fe["rules"] = "Enter a limit of 0 or more (a discount up to 100)."
		case seen[x.MinValue]:
			fe["rules"] = "Each limit can be used once."
		}
		seen[x.MinValue] = true
	}
	if len(in.Rules) > 20 {
		fe["rules"] = "Up to 20 limits."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	a := actorFromRequest(r, "ui")
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM crm.approval_rules WHERE workspace_id = $1 AND kind = $2`, sc.WS, in.Kind); err != nil {
			return err
		}
		for _, x := range in.Rules {
			if _, err := tx.Exec(ctx, `INSERT INTO crm.approval_rules (workspace_id, kind, min_value, approver_role, label, created_by) VALUES ($1, $2, $3::numeric, $4, $5, $6)`,
				sc.WS, in.Kind, x.MinValue.String(), x.ApproverRole, strings.TrimSpace(x.Label), a.ID); err != nil {
				return err
			}
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "approval_rules.saved", "approval_rule", nil, nil, in))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.handleApprovalRules(w, r)
}

// approvalNeeded: the highest limit the value reaches. value is a percentage for discounts
// (12.50 → Cents 1250) and an amount otherwise.
func (h *Handler) approvalNeeded(ctx context.Context, q querier, ws uuid.UUID, kind string, value Cents) (ApprovalNeed, error) {
	need := ApprovalNeed{Kind: kind, Value: value.Float()}
	err := q.QueryRow(ctx, `SELECT approver_role, label FROM crm.approval_rules WHERE workspace_id = $1 AND kind = $2 AND min_value <= $3::numeric AND $3::numeric > 0
		ORDER BY min_value DESC LIMIT 1`, ws, kind, value.String()).Scan(&need.ApproverRole, &need.Label)
	if errors.Is(err, pgx.ErrNoRows) {
		return need, nil
	}
	need.Required = err == nil
	return need, err
}

// mayDecide: the member holds the approver role, a role above it, or manages approvals.
func (h *Handler) mayDecide(ctx context.Context, q querier, sc *Scope, approverRole string) bool {
	if canManageApprovals(sc) {
		return true
	}
	if sc.Eff == nil || sc.Eff.RoleKey == "" {
		return false
	}
	if sc.Eff.RoleKey == approverRole {
		return true
	}
	var above bool
	_ = q.QueryRow(ctx, `
		WITH RECURSIVE up AS (
		  SELECT id, parent_role_id FROM crm.roles WHERE workspace_id = $1 AND key = $2
		  UNION SELECT r.id, r.parent_role_id FROM crm.roles r JOIN up ON r.id = up.parent_role_id WHERE r.workspace_id = $1)
		SELECT EXISTS (SELECT 1 FROM up JOIN crm.roles r ON r.id = up.id WHERE r.key = $3)`, sc.WS, approverRole, sc.Eff.RoleKey).Scan(&above)
	return above
}

// requestApproval opens (or settles) the approval of a record and returns the state the
// record should show: not_required | approved | pending.
func (h *Handler) requestApproval(ctx context.Context, tx pgx.Tx, sc *Scope, a actorInfo, kind, object string, id uuid.UUID, value Cents, need ApprovalNeed, reason string) (string, error) {
	if !need.Required {
		_, err := tx.Exec(ctx, `UPDATE crm.approval_requests SET status = 'cancelled', decided_at = now(), decision_note = 'No longer needed'
			WHERE workspace_id = $1 AND object_key = $2 AND record_id = $3 AND kind = $4 AND status = 'pending'`, sc.WS, object, id, kind)
		return "not_required", err
	}
	// Already approved for at least this much: lowering a discount doesn't ask again.
	var covered bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.approval_requests WHERE workspace_id = $1 AND object_key = $2 AND record_id = $3 AND kind = $4
		AND status = 'approved' AND value >= $5::numeric)`, sc.WS, object, id, kind, value.String()).Scan(&covered); err != nil {
		return "", err
	}
	if covered {
		return "approved", nil
	}
	if _, err := tx.Exec(ctx, `UPDATE crm.approval_requests SET status = 'cancelled', decided_at = now(), decision_note = 'Replaced by a new request'
		WHERE workspace_id = $1 AND object_key = $2 AND record_id = $3 AND kind = $4 AND status = 'pending'`, sc.WS, object, id, kind); err != nil {
		return "", err
	}
	var reqID uuid.UUID
	if err := tx.QueryRow(ctx, `INSERT INTO crm.approval_requests (workspace_id, kind, object_key, record_id, value, approver_role, reason, requested_by)
		VALUES ($1, $2, $3, $4, $5::numeric, $6, $7, $8) RETURNING id`, sc.WS, kind, object, id, value.String(), need.ApproverRole, clipText(reason, 300), a.ID).Scan(&reqID); err != nil {
		return "", err
	}
	if err := insertActivity(ctx, tx, sc.WS, object, id, "approval.requested", "Approval requested: "+approvalKinds[kind],
		map[string]any{"value": value.String(), "approverRole": need.ApproverRole}, a.ID); err != nil {
		return "", err
	}
	if err := shared.WriteAudit(ctx, tx, a.audit(sc.WS, "approval.requested", "approval_request", &reqID, nil,
		map[string]any{"kind": kind, "object": object, "recordId": id.String(), "value": value.String(), "approverRole": need.ApproverRole})); err != nil {
		return "", err
	}
	// Tell the people who can decide.
	rows, err := tx.Query(ctx, `SELECT DISTINCT m.identity_id FROM crm.role_assignments ra JOIN crm.roles r ON r.id = ra.role_id JOIN crm.memberships m ON m.id = ra.membership_id
		WHERE ra.workspace_id = $1 AND r.key = $2 AND m.status = 'active' AND m.identity_id IS DISTINCT FROM $3 LIMIT 25`, sc.WS, need.ApproverRole, a.ID)
	if err != nil {
		return "", err
	}
	var to []uuid.UUID
	for rows.Next() {
		var p uuid.UUID
		if rows.Scan(&p) == nil {
			to = append(to, p)
		}
	}
	rows.Close()
	for _, p := range to {
		h.notify(ctx, tx, sc.WS, p, "approval.requested", "Approval needed", reason, "/crm/w/"+sc.Code+"/approvals", a.ID)
	}
	return "pending", nil
}

type ApprovalRow struct {
	ID           string     `json:"id"`
	Kind         string     `json:"kind"`
	Object       string     `json:"object"`
	RecordID     string     `json:"recordId"`
	Record       string     `json:"record"`
	Value        Cents      `json:"value"`
	ApproverRole string     `json:"approverRole"`
	Reason       string     `json:"reason"`
	Status       string     `json:"status"`
	RequestedBy  string     `json:"requestedBy"`
	DecidedBy    string     `json:"decidedBy,omitempty"`
	Note         string     `json:"note,omitempty"`
	CreatedAt    time.Time  `json:"createdAt"`
	DecidedAt    *time.Time `json:"decidedAt,omitempty"`
	CanDecide    bool       `json:"canDecide"`
	requestedID  *uuid.UUID
}

const approvalSelect = `
	SELECT a.id::text, a.kind, a.object_key, a.record_id::text, COALESCE(o.code || ' · ' || o.name, ''), a.value::text, a.approver_role, a.reason, a.status,
	       COALESCE(rq.display_name, ''), COALESCE(dc.display_name, ''), a.decision_note, a.created_at, a.decided_at, a.requested_by
	FROM crm.approval_requests a
	LEFT JOIN crm.object_records o ON o.id = a.record_id
	LEFT JOIN crm.identities rq ON rq.id = a.requested_by LEFT JOIN crm.identities dc ON dc.id = a.decided_by`

func scanApproval(row pgx.Row) (ApprovalRow, error) {
	var x ApprovalRow
	var v string
	err := row.Scan(&x.ID, &x.Kind, &x.Object, &x.RecordID, &x.Record, &v, &x.ApproverRole, &x.Reason, &x.Status, &x.RequestedBy, &x.DecidedBy, &x.Note, &x.CreatedAt, &x.DecidedAt, &x.requestedID)
	x.Value = mustCents(v)
	return x, err
}

func (h *Handler) latestApproval(ctx context.Context, q querier, ws uuid.UUID, object string, id uuid.UUID, kind string) (*ApprovalRow, error) {
	x, err := scanApproval(q.QueryRow(ctx, approvalSelect+` WHERE a.workspace_id = $1 AND a.object_key = $2 AND a.record_id = $3 AND a.kind = $4 AND a.status <> 'cancelled'
		ORDER BY a.created_at DESC LIMIT 1`, ws, object, id, kind))
	if err != nil {
		return nil, nil
	}
	return &x, nil
}

func (h *Handler) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	ctx := r.Context()
	me := actor(r)
	status := r.URL.Query().Get("status")
	if status == "" {
		status = "pending"
	}
	rows, err := h.store.Pool.Query(ctx, approvalSelect+` WHERE a.workspace_id = $1 AND ($2 = 'all' OR a.status = $2) ORDER BY (a.status = 'pending') DESC, a.created_at DESC LIMIT 200`, sc.WS, status)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var all []ApprovalRow
	for rows.Next() {
		x, err := scanApproval(rows)
		if err != nil {
			rows.Close()
			shared.WriteError(w, r, err)
			return
		}
		all = append(all, x)
	}
	rows.Close()
	// A member sees the requests they made and the ones they may decide.
	list := []ApprovalRow{}
	decide := map[string]bool{}
	for _, x := range all {
		ok, known := decide[x.ApproverRole]
		if !known {
			ok = h.mayDecide(ctx, h.store.Pool, sc, x.ApproverRole)
			decide[x.ApproverRole] = ok
		}
		mine := x.requestedID != nil && *x.requestedID == me
		if !ok && !mine {
			continue
		}
		x.CanDecide = ok && x.Status == "pending" && (!mine || canManageApprovals(sc))
		list = append(list, x)
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": list, "kinds": approvalKinds})
}

func (h *Handler) handleDecideApproval(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	ctx := r.Context()
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("approval_not_found"))
		return
	}
	var in struct {
		Status string `json:"status"`
		Note   string `json:"note"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if in.Status != "approved" && in.Status != "rejected" {
		shared.WriteError(w, r, shared.Validation(map[string]string{"status": "Approve or reject."}))
		return
	}
	a := actorFromRequest(r, "ui")
	me := actor(r)
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		x, err := scanApproval(tx.QueryRow(ctx, approvalSelect+` WHERE a.id = $1 AND a.workspace_id = $2 FOR UPDATE OF a`, id, sc.WS))
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("approval_not_found")
		}
		if err != nil {
			return err
		}
		if !h.mayDecide(ctx, tx, sc, x.ApproverRole) {
			return errForbidden
		}
		if x.requestedID != nil && *x.requestedID == me && !canManageApprovals(sc) {
			return shared.Forbidden("self_approval", "You can't decide a request you made yourself.")
		}
		if x.Status != "pending" {
			return shared.NewError(http.StatusConflict, "already_decided", "This request was already "+x.Status+".")
		}
		if _, err := tx.Exec(ctx, `UPDATE crm.approval_requests SET status = $3, decided_by = $4, decided_at = now(), decision_note = $5 WHERE id = $1 AND workspace_id = $2`,
			id, sc.WS, in.Status, me, clipText(strings.TrimSpace(in.Note), 500)); err != nil {
			return err
		}
		rid, _ := uuid.Parse(x.RecordID)
		if err := h.approvalDecided(ctx, tx, sc, a, x, rid, in.Status == "approved"); err != nil {
			return err
		}
		if err := insertActivity(ctx, tx, sc.WS, x.Object, rid, "approval."+in.Status, "Approval "+in.Status+": "+approvalKinds[x.Kind],
			map[string]any{"value": x.Value.String(), "note": in.Note}, a.ID); err != nil {
			return err
		}
		if x.requestedID != nil {
			h.notify(ctx, tx, sc.WS, *x.requestedID, "approval."+in.Status, "Your request was "+in.Status, x.Reason, "/crm/w/"+sc.Code+"/"+x.Object+"/"+x.RecordID, a.ID)
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "approval."+in.Status, "approval_request", &id,
			map[string]any{"status": "pending"}, map[string]any{"status": in.Status, "kind": x.Kind, "object": x.Object, "recordId": x.RecordID, "value": x.Value.String(), "note": in.Note}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.bus.Kick()
	shared.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

const sourceApproval = "approval"

// approvalDecided moves the record on (or back) once its request is decided.
func (h *Handler) approvalDecided(ctx context.Context, tx pgx.Tx, sc *Scope, a actorInfo, x ApprovalRow, id uuid.UUID, approved bool) error {
	spec := specFor(x.Object)
	if spec == nil {
		return nil
	}
	set := map[string]any{}
	switch x.Object {
	case "quotes":
		set["approvalStatus"] = map[bool]string{true: "approved", false: "rejected"}[approved]
	case "credit_notes", "debit_notes":
		set["status"] = map[bool]string{true: "issued", false: "cancelled"}[approved]
	case "adjustments":
		set["status"] = map[bool]string{true: "approved", false: "rejected"}[approved]
	case "refunds":
		set["status"] = map[bool]string{true: "succeeded", false: "cancelled"}[approved]
	}
	if len(set) == 0 {
		return nil
	}
	// Written as the approval system: the finance hooks see an approved change and apply it.
	_, err := h.updateValues(ctx, tx, sc.WS, spec, id, systemActor(sourceApproval), set, nil, nil)
	return err
}
