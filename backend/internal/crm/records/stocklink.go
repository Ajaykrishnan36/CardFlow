package records

import (
	"context"
	"net/http"
	"time"

	"cardflow-backend/internal/crm/shared"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Vehicles in the common stock (D-178). Each vehicle stays its own record (VIN, colour,
// booking), and the common Stock and Stock movements show it too: one stock line per model
// ("Vehicles") counts the bikes or cars on hand, and every vehicle received, allocated,
// released, delivered or taken back writes the movement — so the stock pages, their chart
// and their history cover vehicles and accessories alike.

const vehicleBatch = "Vehicles"

func stockLinkUpgrades() map[string]objectUpgrade {
	return map[string]objectUpgrade{
		"stock_moves": {fields: []ObjectField{ofLookup("vehicleId", "Vehicle", "vehicle_stock"), ofLookup("bookingId", "Booking", "vehicle_bookings")}},
		"stock_lots":  {fields: []ObjectField{of("vehicles", "Vehicles in this line", "number")}},
	}
}

// vehicleMoves are the movements a vehicle's status change means, in order.
func vehicleMoves(op, was, now string) []string {
	held := func(s string) bool { return s == "in_stock" || s == "in_transit" || s == "demo" || s == "on_hold" }
	if op == "create" {
		if held(now) {
			return []string{"receive"}
		}
		return nil
	}
	if was == now {
		return nil
	}
	switch {
	case held(was) && now == "allocated":
		return []string{"reserve"}
	case was == "allocated" && held(now):
		return []string{"release"}
	case was == "allocated" && now == "delivered":
		return []string{"release", "issue"}
	case held(was) && now == "delivered":
		return []string{"issue"}
	case was == "delivered" && (held(now) || now == "allocated"):
		return []string{"return"}
	}
	return nil
}

// vehicleStockSaved writes the movements for one vehicle.
func (h *Handler) vehicleStockSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, after *Row) error {
	if specFor("stock_lots") == nil || specFor("stock_moves") == nil || !after.has("itemId") {
		return nil
	}
	moves := vehicleMoves(op, before.text("status"), after.text("status"))
	if len(moves) == 0 {
		return nil
	}
	lot, err := h.vehicleLot(ctx, tx, ws, after.id("itemId"), after.text("location"))
	if err != nil || lot == uuid.Nil {
		return err
	}
	var booking, contact string
	if b := after.id("bookingId"); b != uuid.Nil {
		booking = b.String()
		_ = tx.QueryRow(ctx, `SELECT COALESCE(custom->>'contactId', '') FROM crm.object_records WHERE id = $1 AND workspace_id = $2`, b, ws).Scan(&contact)
	}
	reason := map[string]string{"receive": "Vehicle received", "reserve": "Allocated to a booking", "release": "Allocation released",
		"issue": "Delivered to the customer", "return": "Vehicle taken back"}
	for _, kind := range moves {
		m := map[string]any{"lotId": lot.String(), "moveType": kind, "quantity": 1.0, "vehicleId": after.uuid().String(), "movedOn": today(),
			"reason": joinNonEmpty(" · ", reason[kind], after.Title)}
		if booking != "" {
			m["bookingId"] = booking
		}
		if contact != "" && (kind == "issue" || kind == "reserve") {
			m["contactId"] = contact
		}
		if _, err := h.createRecord(ctx, tx, ws, specFor("stock_moves"), systemActor(sourceDealer), m); err != nil {
			return err
		}
	}
	_, err = tx.Exec(ctx, `UPDATE crm.object_records l SET custom = l.custom || jsonb_build_object('vehicles',
		(SELECT count(*) FROM crm.object_records v WHERE v.workspace_id = l.workspace_id AND v.object_key = 'vehicle_stock' AND v.deleted_at IS NULL
		  AND v.custom->>'itemId' = l.custom->>'itemId' AND v.status IN ('in_stock', 'in_transit', 'demo', 'on_hold', 'allocated')))
		WHERE l.id = $1`, lot)
	return err
}

// vehicleLot is the model's "Vehicles" stock line, made the first time.
func (h *Handler) vehicleLot(ctx context.Context, tx pgx.Tx, ws, item uuid.UUID, location string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'stock_lots' AND deleted_at IS NULL
		AND custom->>'itemId' = $2 AND custom->>'batchNumber' = $3 ORDER BY created_at LIMIT 1`, ws, item.String(), vehicleBatch).Scan(&id)
	if err == nil {
		return id, nil
	}
	if err != pgx.ErrNoRows {
		return uuid.Nil, err
	}
	if location == "" {
		location = "Showroom"
	}
	row, err := h.createRecord(ctx, tx, ws, specFor("stock_lots"), systemActor(sourceDealer), map[string]any{
		"itemId": item.String(), "batchNumber": vehicleBatch, "location": location, "unit": "piece", "reorderLevel": 2.0,
		"notes": "Counts this model's vehicles. Each vehicle is in Vehicle stock; its movements are written here."})
	if err != nil {
		return uuid.Nil, err
	}
	return row.uuid(), nil
}

// backfillVehicleStock (once): vehicles added before the link get their stock line and
// movements — received, then reserved or delivered as they stand.
func (h *Handler) backfillVehicleStock(ctx context.Context) error {
	return h.store.WithTx(ctx, func(tx pgx.Tx) error {
		tag, err := tx.Exec(ctx, `INSERT INTO crm.connector_state (key, value) VALUES ('stock:vehicles-v1', '{"done":true}') ON CONFLICT (key) DO NOTHING`)
		if err != nil || tag.RowsAffected() == 0 || specFor("vehicle_stock") == nil || specFor("stock_moves") == nil {
			return err
		}
		rows, err := tx.Query(ctx, `SELECT workspace_id, id FROM crm.object_records WHERE object_key = 'vehicle_stock' AND deleted_at IS NULL
			AND NOT EXISTS (SELECT 1 FROM crm.object_records m WHERE m.object_key = 'stock_moves' AND m.custom->>'vehicleId' = object_records.id::text)
			ORDER BY created_at`)
		if err != nil {
			return err
		}
		type ref struct{ ws, id uuid.UUID }
		var list []ref
		for rows.Next() {
			var r ref
			if rows.Scan(&r.ws, &r.id) == nil {
				list = append(list, r)
			}
		}
		rows.Close()
		for _, r := range list {
			v, _, err := h.getRow(ctx, tx, r.ws, specFor("vehicle_stock"), r.id, nil)
			if err != nil {
				continue
			}
			now := v.text("status")
			start := &Row{Values: map[string]any{"status": "in_stock"}}
			if err := h.vehicleStockSaved(ctx, tx, r.ws, "create", nil, &Row{ID: v.ID, Title: v.Title, Values: mergeValues(v.Values, map[string]any{"status": "in_stock"})}); err != nil {
				return err
			}
			if now != "in_stock" {
				if err := h.vehicleStockSaved(ctx, tx, r.ws, "update", start, v); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

func mergeValues(a, b map[string]any) map[string]any {
	out := make(map[string]any, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}

// GET /w/{code}/stock/summary (D-178): what the stock pages show above their lists — eight
// weeks of stock in against stock out, what is on hand per product (with what is promised
// and whether it is running low), and the totals. Counted from stock lines and movements.
func (h *Handler) handleStockSummary(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.Enabled("stock_lots") || !sc.Can("stock_lots", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	ctx := r.Context()
	type week struct {
		Label string  `json:"label"`
		In    float64 `json:"in"`
		Out   float64 `json:"out"`
	}
	type item struct {
		Name     string  `json:"name"`
		OnHand   float64 `json:"onHand"`
		Reserved float64 `json:"reserved"`
		Low      bool    `json:"low"`
		Vehicles bool    `json:"vehicles,omitempty"`
	}
	out := struct {
		Weeks    []week  `json:"weeks"`
		Items    []item  `json:"items"`
		OnHand   float64 `json:"onHand"`
		Reserved float64 `json:"reserved"`
		Lines    int     `json:"lines"`
		Low      int     `json:"low"`
		Value    float64 `json:"value"`
		InWeek   float64 `json:"inThisWeek"`
		OutWeek  float64 `json:"outThisWeek"`
	}{Weeks: []week{}, Items: []item{}}
	now := time.Now()
	end := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
	start := end.AddDate(0, 0, -56)
	ins := map[int]float64{}
	outs := map[int]float64{}
	if sc.Can("stock_moves", "read") {
		rows, err := h.store.Pool.Query(ctx, `SELECT floor(extract(epoch FROM ($3::timestamptz - COALESCE(NULLIF(custom->>'movedOn', '')::date::timestamptz, created_at))) / 604800)::int,
			custom->>'moveType', COALESCE(sum(`+numeric(`custom->>'quantity'`)+`), 0)::float8
			FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'stock_moves' AND deleted_at IS NULL
			  AND COALESCE(NULLIF(custom->>'movedOn', '')::date::timestamptz, created_at) >= $2 GROUP BY 1, 2`, sc.WS, start, end)
		if err == nil {
			for rows.Next() {
				var ago int
				var kind string
				var q float64
				if rows.Scan(&ago, &kind, &q) != nil || ago < 0 || ago > 7 {
					continue
				}
				switch kind {
				case "receive", "return":
					ins[7-ago] += q
				case "issue", "damage", "transfer":
					outs[7-ago] += q
				}
			}
			rows.Close()
		}
	}
	for i := 0; i < 8; i++ {
		out.Weeks = append(out.Weeks, week{Label: start.AddDate(0, 0, 7*i+6).Format("2 Jan"), In: ins[i], Out: outs[i]})
	}
	out.InWeek, out.OutWeek = ins[7], outs[7]
	rows, err := h.store.Pool.Query(ctx, `SELECT COALESCE(i.name, l.name), COALESCE(sum(`+numeric(`l.custom->>'quantity'`)+`), 0)::float8,
		COALESCE(sum(`+numeric(`l.custom->>'reserved'`)+`), 0)::float8, bool_or(l.status IN ('low', 'out')), bool_or(l.custom->>'batchNumber' = $2),
		COALESCE(sum(`+numeric(`l.custom->>'quantity'`)+` * COALESCE(NULLIF(`+numeric(`l.custom->>'unitCost'`)+`, 0), `+numeric(`i.custom->>'unitPrice'`)+`)), 0)::float8, count(*)
		FROM crm.object_records l LEFT JOIN crm.object_records i ON i.id::text = l.custom->>'itemId'
		WHERE l.workspace_id = $1 AND l.object_key = 'stock_lots' AND l.deleted_at IS NULL GROUP BY 1 ORDER BY 2 DESC LIMIT 12`, sc.WS, vehicleBatch)
	if err == nil {
		for rows.Next() {
			var x item
			var value float64
			var n int
			if rows.Scan(&x.Name, &x.OnHand, &x.Reserved, &x.Low, &x.Vehicles, &value, &n) == nil {
				out.Items = append(out.Items, x)
				out.OnHand += x.OnHand
				out.Reserved += x.Reserved
				out.Value += value
				out.Lines += n
				if x.Low {
					out.Low++
				}
			}
		}
		rows.Close()
	}
	shared.WriteJSON(w, http.StatusOK, out)
}
