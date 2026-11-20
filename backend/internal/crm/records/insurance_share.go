package records

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---- B7: share a quote with the customer by a link ----

// POST /quotes/{id}/share — a link the customer can open for 30 days. It shows the plan,
// premium and features, never the agent's commission; each open is logged on the quote.
func (h *Handler) handleShareQuote(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	spec := specFor("quotes")
	if spec == nil || !sc.Can("quotes", "update") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	token, err := shared.RandomToken(24)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var expires time.Time
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		if _, _, err := h.getRow(r.Context(), tx, sc.WS, spec, id, ownerFilter(r, spec)); err != nil {
			return err
		}
		if err := tx.QueryRow(r.Context(), `INSERT INTO crm.doc_shares (workspace_id, object_key, record_id, token_hash, created_by, expires_at)
			VALUES ($1, 'quotes', $2, $3, $4, now() + interval '30 days') RETURNING expires_at`, sc.WS, id, shared.HashToken(token), actor(r)).Scan(&expires); err != nil {
			return err
		}
		me := actor(r)
		return insertActivity(r.Context(), tx, sc.WS, "quotes", id, "quote.shared", "Shared with the customer by a link", map[string]any{"expires": expires.Format("2006-01-02")}, &me)
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusCreated, map[string]any{"token": token, "url": strings.TrimRight(h.cfg.BaseURL, "/") + "/q/" + token, "expiresAt": expires})
}

// GET /public/shared/{token} — the customer's copy: no sign-in, no commission.
func (h *Handler) HandleSharedDocument(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	ctx := r.Context()
	var ws, rec uuid.UUID
	var owner *uuid.UUID
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `UPDATE crm.doc_shares SET opens = opens + 1, last_opened_at = now()
			WHERE token_hash = $1 AND expires_at > now() RETURNING workspace_id, record_id`, shared.HashToken(token)).Scan(&ws, &rec); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return shared.NotFound("link_expired")
			}
			return err
		}
		_ = tx.QueryRow(ctx, `SELECT owner_id FROM crm.object_records WHERE id = $1`, rec).Scan(&owner)
		return insertActivity(ctx, tx, ws, "quotes", rec, "quote.opened", "The customer opened the shared quote", map[string]any{"ip": r.Header.Get("X-Forwarded-For")}, nil)
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var name, status, business string
	var custom []byte
	if err := h.store.Pool.QueryRow(ctx, `SELECT q.name, COALESCE(q.status, ''), q.custom, w.name FROM crm.object_records q JOIN crm.workspaces w ON w.id = q.workspace_id
		WHERE q.id = $1 AND q.workspace_id = $2`, rec, ws).Scan(&name, &status, &custom, &business); err != nil {
		shared.WriteError(w, r, shared.NotFound("link_expired"))
		return
	}
	var v map[string]any
	_ = json.Unmarshal(custom, &v)
	// Only what the customer should see: the agent's commission never leaves the CRM.
	out := map[string]any{"business": business, "name": name, "status": status}
	for _, k := range []string{"quoteDate", "validUntil", "subtotal", "discount", "tax", "total", "terms", "description", "currency"} {
		if x, ok := v[k]; ok {
			out[k] = x
		}
	}
	lines := []map[string]any{}
	rows, err := h.store.Pool.Query(ctx, `SELECT l.name, l.custom FROM crm.object_records l WHERE l.workspace_id = $1 AND l.object_key = 'line_items' AND l.deleted_at IS NULL
		AND l.custom->>'quoteId' = $2 ORDER BY COALESCE(NULLIF(l.custom->>'sortOrder', '')::numeric, 0), l.created_at`, ws, rec.String())
	if err == nil {
		for rows.Next() {
			var n string
			var raw []byte
			if rows.Scan(&n, &raw) == nil {
				var lv map[string]any
				_ = json.Unmarshal(raw, &lv)
				lines = append(lines, map[string]any{"name": n, "quantity": lv["quantity"], "unitPrice": lv["unitPrice"], "taxAmount": lv["taxAmount"], "total": lv["total"]})
			}
		}
		rows.Close()
	}
	out["lines"] = lines
	if owner != nil {
		h.notify(ctx, h.store.Pool, ws, *owner, "quote_opened", "Quote opened: "+name, "The customer opened the quote you shared.", "/quotes/"+rec.String(), nil)
	}
	shared.WriteJSON(w, http.StatusOK, out)
}
