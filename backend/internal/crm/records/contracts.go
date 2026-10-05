package records

import (
	"net/http"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Contracts (D-116) are records like any other; this adds the one thing a form can't do:
// renewing. A renewal is a new contract for the next term, linked back to the one it
// renews ("Renewal of"), and the old one is marked Renewed — in one transaction.
//
//	POST /w/{code}/contracts/{id}/renew   {startDate, endDate, contractValue}
//	GET  /w/{code}/cases/{id}/sla         (see sla.go)

func (h *Handler) serviceRoutes(r chi.Router) {
	r.Post("/contracts/{id}/renew", h.handleRenewContract)
	r.Get("/cases/{id}/sla", h.handleCaseSLA)
}

func (h *Handler) handleRenewContract(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	ctx := r.Context()
	me := actor(r)
	spec := specFor("contracts")
	if spec == nil || !sc.Enabled("contracts") {
		shared.WriteError(w, r, shared.NotFound("object_not_found"))
		return
	}
	// Renewing changes the old contract and makes a new one.
	if !sc.Can("contracts", "update") || !sc.Can("contracts", "create") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	var in struct {
		StartDate     string   `json:"startDate"`
		EndDate       string   `json:"endDate"`
		ContractValue *float64 `json:"contractValue"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	a := actorFromRequest(r, "ui")
	var renewed *Row
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		old, _, err := h.getRow(ctx, tx, sc.WS, spec, id, sc.OwnersFor("contracts", me))
		if err != nil {
			return err
		}
		switch old.text("status") {
		case "renewed":
			return shared.NewError(http.StatusUnprocessableEntity, "already_renewed", "This contract has already been renewed.")
		case "draft", "cancelled":
			return shared.NewError(http.StatusUnprocessableEntity, "not_renewable", "Only a contract that has started can be renewed.")
		}
		// The next term: by default it starts the day after this one ends and lasts as long.
		oldStart, err1 := time.Parse("2006-01-02", old.text("startDate"))
		oldEnd, err2 := time.Parse("2006-01-02", old.text("endDate"))
		start, end := in.StartDate, in.EndDate
		if start == "" && err2 == nil {
			start = oldEnd.AddDate(0, 0, 1).Format("2006-01-02")
		}
		if end == "" && err1 == nil && err2 == nil {
			if s, err := time.Parse("2006-01-02", start); err == nil {
				end = s.Add(oldEnd.Sub(oldStart)).Format("2006-01-02")
			}
		}
		s, errS := time.Parse("2006-01-02", start)
		e, errE := time.Parse("2006-01-02", end)
		if errS != nil || errE != nil || !e.After(s) {
			return shared.Validation(map[string]string{"endDate": "Choose a start and an end date for the new term (the end after the start)."})
		}
		values := map[string]any{"name": old.Title, "startDate": start, "endDate": end, "status": "active"}
		for _, k := range []string{"accountId", "contactId", "contractValue", "currency", "autoRenew", "renewalNoticeDays", "terms", "description",
			"opportunityId", "quoteId", "orderId", "subscriptionId", "ownerId"} {
			if v, ok := old.Values[k]; ok && v != nil && v != "" {
				values[k] = v
			}
		}
		if in.ContractValue != nil {
			values["contractValue"] = *in.ContractValue
		}
		renewed, err = h.createRecord(ctx, tx, sc.WS, spec, a, values)
		if err != nil {
			return err
		}
		if _, err := h.updateValues(ctx, tx, sc.WS, spec, id, a, map[string]any{"status": "renewed", "renewalDate": start}, nil, nil); err != nil {
			return err
		}
		newID := renewed.uuid()
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.record_relationships (workspace_id, type_key, source_object, source_id, target_object, target_id, created_by)
			VALUES ($1, 'renewal_of', 'contracts', $2, 'contracts', $3, $4) ON CONFLICT DO NOTHING`, sc.WS, newID, id, me); err != nil {
			return err
		}
		// What the old contract covered (assets, services, cases) is covered by the new one.
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.record_relationships (workspace_id, type_key, source_object, source_id, target_object, target_id, note, created_by)
			SELECT workspace_id, type_key, 'contracts', $2, target_object, target_id, note, $4 FROM crm.record_relationships
			WHERE workspace_id = $1 AND source_object = 'contracts' AND source_id = $3 AND type_key <> 'renewal_of'
			ON CONFLICT DO NOTHING`, sc.WS, newID, id, me); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "contract.renewed", "contract", &id, map[string]any{"endDate": old.text("endDate")},
			map[string]any{"renewedBy": newID.String(), "startDate": start, "endDate": end}))
	})
	if err == nil {
		h.bus.Kick()
	}
	respond(w, r, http.StatusCreated, renewed, err)
}
