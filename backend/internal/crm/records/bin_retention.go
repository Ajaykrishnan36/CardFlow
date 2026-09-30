package records

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Recycle bin retention (D-84). Off by default: deleted records stay in the bin until
// someone restores or deletes them. A product can choose to delete bin items for good a
// number of days after they were deleted; the worker does it at most once an hour, in
// small batches, through the same path as "Delete permanently".

var binRetentionChoices = map[int]bool{30: true, 60: true, 90: true, 180: true, 365: true}

// GET /recycle-bin
func (h *Handler) handleGetBinSettings(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	var days *int
	if err := h.store.Pool.QueryRow(r.Context(), `SELECT bin_retention_days FROM crm.workspaces WHERE id = $1`, sc.WS).Scan(&days); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"retentionDays": days})
}

// PUT /recycle-bin {retentionDays: null | 30 | 60 | 90 | 180 | 365}
func (h *Handler) handleSaveBinSettings(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireAccessAdmin(w, r)
	if !ok {
		return
	}
	var in struct {
		RetentionDays *int `json:"retentionDays"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if in.RetentionDays != nil && !binRetentionChoices[*in.RetentionDays] {
		shared.WriteError(w, r, shared.Validation(map[string]string{"retentionDays": "Choose 30, 60, 90, 180 or 365 days, or keep records until they're deleted by hand."}))
		return
	}
	err := h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var before *int
		if err := tx.QueryRow(r.Context(), `SELECT bin_retention_days FROM crm.workspaces WHERE id = $1 FOR UPDATE`, sc.WS).Scan(&before); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `UPDATE crm.workspaces SET bin_retention_days = $2 WHERE id = $1`, sc.WS, in.RetentionDays); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, actorFromRequest(r, "ui").audit(sc.WS, "recycle_bin.retention_changed", "workspace", &sc.WS,
			map[string]any{"retentionDays": before}, map[string]any{"retentionDays": in.RetentionDays}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	binPurgeMu.Lock()
	binPurgeLast = time.Time{} // check again now
	binPurgeMu.Unlock()
	h.bus.Kick()
	shared.WriteJSON(w, http.StatusOK, map[string]any{"retentionDays": in.RetentionDays})
}

var (
	binPurgeMu   sync.Mutex
	binPurgeLast time.Time
)

func nextBinPurge() time.Time {
	binPurgeMu.Lock()
	defer binPurgeMu.Unlock()
	return binPurgeLast.Add(time.Hour)
}

// purgeRecycleBins deletes expired bin items in products that opted in (hourly at most).
func (h *Handler) purgeRecycleBins(ctx context.Context) {
	binPurgeMu.Lock()
	if time.Since(binPurgeLast) < time.Hour {
		binPurgeMu.Unlock()
		return
	}
	binPurgeLast = time.Now()
	binPurgeMu.Unlock()

	type ws struct {
		id   uuid.UUID
		days int
	}
	rows, err := h.store.Pool.Query(ctx, `SELECT id, bin_retention_days FROM crm.workspaces WHERE bin_retention_days IS NOT NULL AND status = 'active'`)
	if err != nil {
		slog.Error("CRM recycle bin purge", "error", err)
		return
	}
	list := []ws{}
	for rows.Next() {
		var w ws
		if rows.Scan(&w.id, &w.days) == nil && w.days >= 30 {
			list = append(list, w)
		}
	}
	rows.Close()
	if len(list) == 0 {
		return
	}
	all := append([]*objectSpec{specs["leads"], specs["accounts"], specs["contacts"]}, dynamicSpecs()...)
	budget := 500 // records per run, across products
	for _, w := range list {
		cutoff := time.Now().AddDate(0, 0, -w.days)
		for _, spec := range all {
			if budget <= 0 || ctx.Err() != nil {
				return
			}
			ids := []uuid.UUID{}
			r2, err := h.store.Pool.Query(ctx, "SELECT id FROM crm."+spec.Table+" WHERE workspace_id = $1 AND deleted_at IS NOT NULL AND deleted_at < $2 ORDER BY deleted_at LIMIT $3",
				w.id, cutoff, budget)
			if err != nil {
				continue // an object without a bin
			}
			for r2.Next() {
				var id uuid.UUID
				if r2.Scan(&id) == nil {
					ids = append(ids, id)
				}
			}
			r2.Close()
			for _, id := range ids {
				budget--
				err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
					return h.destroyRecord(ctx, tx, w.id, spec, id, systemActor("retention"), nil)
				})
				if err != nil {
					slog.Warn("CRM recycle bin purge: kept a record", "object", spec.Key, "id", id, "error", err)
				}
			}
		}
	}
	h.bus.Kick()
}
