package records

import (
	"encoding/json"
	"net/http"

	"cardflow-backend/internal/crm/shared"
	"github.com/jackc/pgx/v5"
)

// Dashboard and menu layouts (D-130). A business — and the owner console — can put the
// items of its dashboard and of its side menu in its own order and hide the ones it
// doesn't use, separately for the desktop layout and the phone layout. Only the
// arrangement is stored (keys in order, keys hidden); what a person may see is still
// decided by their permissions, and an item added to the product later simply appears at
// the end.
//
//	GET    {prefix}/ui-layout
//	PUT    {prefix}/ui-layout   {surface: dashboard|nav, device: desktop|mobile, order: [...], hidden: [...]}
//	DELETE {prefix}/ui-layout?surface=&device=    (back to the standard arrangement)

type UILayout struct {
	Order  []string `json:"order"`
	Hidden []string `json:"hidden"`
}

func (h *Handler) handleGetUILayout(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	out := map[string]map[string]UILayout{"dashboard": {}, "nav": {}}
	rows, err := h.store.Pool.Query(r.Context(), `SELECT surface, device, definition FROM crm.ui_layouts WHERE workspace_id = $1`, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var surface, device string
		var raw []byte
		if err := rows.Scan(&surface, &device, &raw); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		var l UILayout
		_ = json.Unmarshal(raw, &l)
		if out[surface] != nil {
			out[surface][device] = l
		}
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"layouts": out, "canEdit": sc.CanCustomize()})
}

func cleanKeys(in []string) ([]string, bool) {
	seen := map[string]bool{}
	out := []string{}
	for _, k := range in {
		if k == "" || len(k) > 120 || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	return out, len(out) <= 400
}

func (h *Handler) handleSaveUILayout(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.CanCustomize() {
		shared.WriteError(w, r, errForbidden)
		return
	}
	var in struct {
		Surface string   `json:"surface"`
		Device  string   `json:"device"`
		Order   []string `json:"order"`
		Hidden  []string `json:"hidden"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	order, ok1 := cleanKeys(in.Order)
	hidden, ok2 := cleanKeys(in.Hidden)
	fe := map[string]string{}
	if in.Surface != "dashboard" && in.Surface != "nav" {
		fe["surface"] = "Choose the dashboard or the menu."
	}
	if in.Device != "desktop" && in.Device != "mobile" {
		fe["device"] = "Choose desktop or mobile."
	}
	if !ok1 || !ok2 {
		fe["order"] = "Too many items."
	}
	// The way back to the dashboard and to the settings can't be hidden.
	for _, k := range hidden {
		if in.Surface == "nav" && lockedNav[k] {
			fe["hidden"] = "This item can't be hidden."
		}
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	def, _ := json.Marshal(UILayout{Order: order, Hidden: hidden})
	a := actorFromRequest(r, "ui")
	err := h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `INSERT INTO crm.ui_layouts (workspace_id, surface, device, definition, updated_by) VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (workspace_id, surface, device) DO UPDATE SET definition = EXCLUDED.definition, updated_by = EXCLUDED.updated_by, updated_at = now()`,
			sc.WS, in.Surface, in.Device, def, a.ID); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, a.audit(sc.WS, "ui_layout.saved", "ui_layout", nil, nil,
			map[string]any{"surface": in.Surface, "device": in.Device, "items": len(order), "hidden": len(hidden)}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.handleGetUILayout(w, r)
}

// lockedNav: menu entries nobody can hide, so a business can't lock itself out.
var lockedNav = map[string]bool{"home": true, "dashboard": true, "overview": true, "admin": true, "access": true, "settings": true}

func (h *Handler) handleResetUILayout(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.CanCustomize() {
		shared.WriteError(w, r, errForbidden)
		return
	}
	q := r.URL.Query()
	a := actorFromRequest(r, "ui")
	err := h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `DELETE FROM crm.ui_layouts WHERE workspace_id = $1 AND ($2 = '' OR surface = $2) AND ($3 = '' OR device = $3)`,
			sc.WS, q.Get("surface"), q.Get("device")); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, a.audit(sc.WS, "ui_layout.reset", "ui_layout", nil, nil, map[string]any{"surface": q.Get("surface"), "device": q.Get("device")}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.handleGetUILayout(w, r)
}
