package platform

import (
	"net/http"
	"time"

	"cardflow-backend/internal/crm/billing"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// POST /platform/workspaces/{id}/renew — the owner renews members of a business with a
// payment taken in hand (cash, UPI, bank, cheque): no Razorpay. Several members in one go;
// each member's paid date moves on by the plan from today or from the date they already have.
func (h *Handler) handleRenewMembers(w http.ResponseWriter, r *http.Request) {
	wsID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("business_not_found"))
		return
	}
	var in struct {
		Plan          string      `json:"plan"`
		Method        string      `json:"method"`
		Amount        float64     `json:"amount"` // rupees received in all (0 = plan price × members)
		Reference     string      `json:"reference"`
		MembershipIDs []uuid.UUID `json:"membershipIds"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	fe := map[string]string{}
	if !Plans[in.Plan] {
		fe["plan"] = "Choose a plan."
	}
	switch in.Method {
	case "cash", "upi", "bank", "cheque":
	default:
		fe["method"] = "Say how they paid: cash, UPI, bank transfer or cheque."
	}
	if in.Amount < 0 {
		fe["amount"] = "Enter the amount received."
	}
	if len(in.MembershipIDs) == 0 {
		fe["membershipIds"] = "Choose at least one member."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	if h.billing == nil {
		shared.WriteError(w, r, shared.NewError(http.StatusConflict, "billing_off", "Payments aren't set up on this server."))
		return
	}
	ctx := r.Context()
	var platformWS bool
	if err := h.store.Pool.QueryRow(ctx, `SELECT is_platform FROM crm.workspaces WHERE id = $1 AND status <> 'deleted'`, wsID).Scan(&platformWS); err != nil || platformWS {
		shared.WriteError(w, r, shared.NotFound("business_not_found"))
		return
	}
	me := actorID(r)
	if _, err := h.billing.RecordOffline(ctx, billing.Offline{Workspace: wsID, Memberships: in.MembershipIDs, Period: in.Plan,
		AmountPaise: int64(in.Amount*100 + 0.5), Method: in.Method, Reference: in.Reference, By: me}); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	type paid struct {
		MembershipID uuid.UUID  `json:"membershipId"`
		PaidUntil    *time.Time `json:"paidUntil"`
	}
	out := []paid{}
	rows, err := h.store.Pool.Query(ctx, `SELECT id, paid_until FROM crm.memberships WHERE workspace_id = $1 AND id = ANY($2)`, wsID, in.MembershipIDs)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var p paid
		if err := rows.Scan(&p.MembershipID, &p.PaidUntil); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out = append(out, p)
	}
	_ = shared.WriteAudit(ctx, h.store.Pool, auditEvent(r, "billing.renewed_offline", "workspace", &wsID, &wsID, nil,
		map[string]any{"plan": in.Plan, "method": in.Method, "amount": in.Amount, "members": len(out)}))
	shared.WriteJSON(w, http.StatusOK, map[string]any{"members": out})
}
