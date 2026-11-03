package records

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Fields for one trade only (D-174). The vehicle side the dealership pack added to shared
// objects — a VIN on an asset, service every N km on a product, top speed in the catalog —
// is for dealers: a law firm's asset or an insurer's catalog never shows it. The field
// stays in the definition (and in any record that has a value); it is just not offered.
var tradeOnlyFields = map[string]map[string]string{
	"assets": {"registrationNumber": "dealership", "vin": "dealership", "engineNumber": "dealership", "odometer": "dealership",
		"odometerDate": "dealership", "avgDailyKm": "dealership", "nextServiceKm": "dealership", "insuranceExpiry": "dealership", "pucExpiry": "dealership"},
	"catalog_items": {"firstServiceDays": "dealership", "firstServiceKm": "dealership", "serviceIntervalDays": "dealership",
		"serviceIntervalKm": "dealership", "freeServices": "dealership"},
	"work_orders":     {"odometer": "dealership", "serviceDueId": "dealership"},
	"catalog_entries": {"mileage": "dealership", "topSpeed": "dealership", "tankCapacity": "dealership", "seatHeight": "dealership"},
}

// tradeUpgrades: what a bike or a car is described by in the catalog.
func tradeUpgrades() map[string]objectUpgrade {
	return map[string]objectUpgrade{
		"catalog_entries": {fields: []ObjectField{
			ofOnly(ofSelect("bikeCategory", "Bike category", opts("commuter", "Commuter", "sports", "Sports", "cruiser", "Cruiser", "adventure", "Adventure / touring",
				"scooter", "Scooter", "moped", "Moped", "electric", "Electric")), "dealership:bike"),
			ofOnly(ofSelect("carBody", "Car body type", opts("hatchback", "Hatchback", "sedan", "Sedan", "suv", "SUV", "compact_suv", "Compact SUV", "muv", "MUV / MPV",
				"pickup", "Pickup", "van", "Van", "coupe", "Coupe / convertible")), "dealership:car"),
			ofOnly(ofSelect("fuelType", "Fuel", opts("petrol", "Petrol", "diesel", "Diesel", "cng", "CNG", "electric", "Electric", "hybrid", "Hybrid")), "dealership"),
			ofOnly(ofSelect("transmission", "Transmission", opts("manual", "Manual", "automatic", "Automatic", "amt", "AMT", "cvt", "CVT", "dct", "DCT")), "dealership:car"),
			ofOnly(of("seating", "Seats", "number"), "dealership:car")}},
	}
}

func ofOnly(f ObjectField, trades string) ObjectField { f.Only = trades; return f }

// markTradeOnly (once) tags the stored definitions with tradeOnlyFields.
func markTradeOnly(ctx context.Context, tx pgx.Tx) error {
	const marker = "objects:trade-only-v1"
	tag, err := tx.Exec(ctx, `INSERT INTO crm.connector_state (key, value) VALUES ($1, '{"done":true}') ON CONFLICT (key) DO NOTHING`, marker)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	for key, only := range tradeOnlyFields {
		var raw []byte
		err := tx.QueryRow(ctx, `SELECT definition FROM crm.object_definitions WHERE key = $1 AND is_standard`, key).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		var stored ObjectBody
		if json.Unmarshal(raw, &stored) != nil {
			continue
		}
		changed := false
		for i, f := range stored.Fields {
			if o, ok := only[f.Key]; ok && f.Only == "" {
				stored.Fields[i].Only, changed = o, true
			}
		}
		if changed {
			out, _ := json.Marshal(stored)
			if _, err := tx.Exec(ctx, `UPDATE crm.object_definitions SET definition = $2, updated_at = now() WHERE key = $1 AND is_standard`, key, out); err != nil {
				return err
			}
		}
	}
	return nil
}
