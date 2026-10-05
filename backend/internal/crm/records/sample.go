package records

import (
	"context"
	"errors"
	"fmt"
	"time"

	"cardflow-backend/internal/crm/shared"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// SeedSample creates a few connected example records in one business — one or two of each
// main kind — through the same code path as records made by hand, so codes, timeline,
// pricing, SLA clocks and balances are all real. One transaction; once per business.
func (h *Handler) SeedSample(ctx context.Context, code string) (int, error) {
	marker := "sample-data:" + code
	made := 0
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var done bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.connector_state WHERE key = $1)`, marker).Scan(&done); err != nil || done {
			return err
		}
		var ws uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM crm.workspaces WHERE code = $1 AND NOT is_platform`, code).Scan(&ws); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return errors.New("no business with the code " + code)
			}
			return err
		}
		// The records belong to the person who has been in the business longest.
		var owner uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT identity_id FROM crm.memberships WHERE workspace_id = $1 AND status = 'active' ORDER BY created_at LIMIT 1`, ws).Scan(&owner); err != nil {
			return errors.New("the business has no active member to own the sample records")
		}
		a := actorInfo{ID: &owner, Kind: "system", Source: "sample"}
		note := "Sample record — edit or delete it freely."
		day := func(offset int) string { return time.Now().AddDate(0, 0, offset).Format("2006-01-02") }
		at := func(days, hour int) string {
			t := time.Now().AddDate(0, 0, days)
			return time.Date(t.Year(), t.Month(), t.Day(), hour, 0, 0, 0, time.FixedZone("IST", 19800)).UTC().Format(time.RFC3339)
		}
		// mk creates a record when the business has that object; "" otherwise.
		mk := func(object string, v map[string]any) (string, error) {
			spec := specFor(object)
			if spec == nil {
				return "", nil
			}
			v["ownerId"] = owner.String()
			for k, x := range v {
				if s, ok := x.(string); ok && s == "" {
					delete(v, k)
				}
			}
			row, err := h.createRecord(ctx, tx, ws, spec, a, v)
			if err != nil {
				var ae *shared.Error
				if errors.As(err, &ae) && len(ae.FieldErrors) > 0 {
					return "", fmt.Errorf("%s: %v", object, ae.FieldErrors)
				}
				return "", errors.New(object + ": " + err.Error())
			}
			made++
			return row.ID, nil
		}
		var err error
		must := func(id string, e error) string {
			if e != nil && err == nil {
				err = e
			}
			return id
		}

		// ---- people and companies ----
		customer := must(mk("accounts", map[string]any{"name": "Sri Lakshmi Textiles", "type": "customer", "phone": "+91 422 555 0101",
			"website": "https://example.com", "billingCity": "Coimbatore", "billingState": "Tamil Nadu", "billingCountry": "India", "description": note}))
		vendor := must(mk("accounts", map[string]any{"name": "Kovai Packaging Supplies", "type": "vendor", "billingCity": "Coimbatore", "billingCountry": "India", "description": note}))
		buyer := must(mk("contacts", map[string]any{"firstName": "Priya", "lastName": "Raman", "title": "Purchase Manager", "email": "priya.raman@example.com", "phone": "+91 98765 00001", "accountId": customer, "description": note}))
		finance := must(mk("contacts", map[string]any{"firstName": "Karthik", "lastName": "Subramanian", "title": "Accounts Head", "email": "karthik.s@example.com", "accountId": customer, "description": note}))
		must(mk("leads", map[string]any{"firstName": "Anitha", "lastName": "Kumar", "organization": "Green Leaf Exports", "email": "anitha@example.com", "phone": "+91 98765 00002",
			"status": "new", "source": "web", "description": note}))
		must(mk("leads", map[string]any{"firstName": "Mohammed", "lastName": "Ismail", "organization": "Ismail & Sons Hardware", "phone": "+91 98765 00003", "status": "working", "source": "referral", "description": note}))

		// ---- products, services and prices ----
		product := must(mk("catalog_items", map[string]any{"name": "Cotton Fabric Roll (50 m)", "sku": "FAB-050", "itemType": "product", "unitPrice": 12000.0, "cost": 9000.0, "taxRate": 5.0, "unit": "roll", "family": "Fabric", "description": note}))
		service := must(mk("catalog_items", map[string]any{"name": "On-site Machine Service", "sku": "SVC-001", "itemType": "service", "unitPrice": 2500.0, "taxRate": 18.0, "unit": "visit", "durationMinutes": 120.0,
			"billingFrequency": "one_time", "skillsRequired": "mechanical", "workOrderEligible": true, "description": note}))
		book := must(mk("price_books", map[string]any{"name": "Standard Prices", "status": "active", "isDefault": true, "currency": "INR", "validFrom": day(-30), "description": note}))
		if err == nil && book != "" {
			for item, price := range map[string][2]string{product: {"12000", "12500"}, service: {"2500", "2500"}} {
				if item == "" {
					continue
				}
				if _, e := tx.Exec(ctx, `INSERT INTO crm.price_book_entries (workspace_id, price_book_id, catalog_item_id, currency, list_price, unit_price, max_discount_pct, valid_from, created_by, updated_by)
					VALUES ($1, $2, $3, 'INR', $5::numeric, $4::numeric, 20, CURRENT_DATE - 30, $6, $6)`, ws, book, item, price[0], price[1], owner); e != nil {
					return e
				}
			}
		}
		territory := must(mk("territories", map[string]any{"name": "Tamil Nadu", "territoryCode": "TN", "territoryType": "state", "status": "active", "description": note}))
		if err == nil && territory != "" && customer != "" {
			if _, e := tx.Exec(ctx, `INSERT INTO crm.territory_assignments (workspace_id, territory_id, kind, account_id, created_by) VALUES ($1, $2, 'account', $3, $4)`, ws, territory, customer, owner); e != nil {
				return e
			}
		}

		// ---- selling ----
		open := must(mk("opportunities", map[string]any{"name": "Fabric supply for Diwali season", "accountId": customer, "contactId": buyer, "amount": 240000.0, "probability": 60.0,
			"closeDate": day(21), "status": "proposal", "forecastCategory": "best_case", "type": "new_business", "leadSource": "referral", "nextStep": "Send revised quote", "description": note}))
		must(mk("opportunities", map[string]any{"name": "Annual machine service contract", "accountId": customer, "contactId": finance, "amount": 30000.0, "probability": 100.0,
			"closeDate": day(-3), "status": "closed_won", "type": "existing_business", "description": note}))
		if err == nil && open != "" && buyer != "" {
			if _, e := tx.Exec(ctx, `INSERT INTO crm.record_relationships (workspace_id, type_key, source_object, source_id, target_object, target_id, role, is_primary, created_by)
				VALUES ($1, 'contact_role', 'contacts', $2, 'opportunities', $3, 'Decision maker', true, $4) ON CONFLICT DO NOTHING`, ws, buyer, open, owner); e != nil {
				return e
			}
		}
		quote := must(mk("quotes", map[string]any{"name": "Quote — 20 fabric rolls", "accountId": customer, "contactId": buyer, "opportunityId": open, "priceBookId": book,
			"quoteDate": day(-1), "validUntil": day(14), "status": "draft", "description": note}))
		sc := &Scope{WS: ws, Code: code}
		if err == nil && quote != "" && product != "" {
			qid, _ := uuid.Parse(quote)
			if _, e := h.writeLines(ctx, tx, sc, "quotes", qid, PriceRequest{Lines: []PriceLineIn{{ItemID: product, Quantity: 20, DiscountPercent: 5}, {ItemID: service, Quantity: 1}}}, a, true); e != nil {
				return errors.New("quote lines: " + e.Error())
			}
		}
		invoice := must(mk("invoices", map[string]any{"name": "Invoice — machine service contract", "accountId": customer, "contactId": finance, "invoiceDate": day(-3), "dueDate": day(12),
			"status": "draft", "description": note}))
		if err == nil && invoice != "" && service != "" {
			iid, _ := uuid.Parse(invoice)
			if _, e := h.writeLines(ctx, tx, sc, "invoices", iid, PriceRequest{PriceBookID: book, Lines: []PriceLineIn{{ItemID: service, Quantity: 12}}}, a, true); e != nil {
				return errors.New("invoice lines: " + e.Error())
			}
			if _, e := h.updateValues(ctx, tx, ws, specFor("invoices"), iid, a, map[string]any{"status": "sent"}, nil, nil); e != nil {
				return errors.New("invoice: " + e.Error())
			}
		}
		// Part of the invoice is already paid.
		must(mk("payments", map[string]any{"name": "Advance for service contract", "amount": 15000.0, "paymentDate": day(-2), "paymentMethod": "bank_transfer", "reference": "UTR-SAMPLE-001",
			"invoiceId": invoice, "status": "paid", "notes": note}))
		must(mk("purchase_orders", map[string]any{"name": "Packing material — October", "accountId": vendor, "orderDate": day(-5), "expectedDate": day(5), "total": 18000.0, "status": "ordered", "description": note}))
		must(mk("expenses", map[string]any{"name": "Courier charges", "amount": 1450.0, "date": day(-4), "accountId": vendor, "description": note}))

		// ---- service ----
		contract := must(mk("contracts", map[string]any{"name": "Annual machine service contract 2026–27", "accountId": customer, "contactId": finance, "startDate": day(-3), "endDate": day(362),
			"contractValue": 30000.0, "renewalNoticeDays": 30.0, "status": "active", "description": note}))
		policy := must(mk("sla_policies", map[string]any{"name": "Standard support", "priority": "any", "firstResponseMinutes": 120.0, "resolutionMinutes": 1440.0, "isDefault": true, "status": "active", "description": note}))
		must(mk("entitlements", map[string]any{"name": "Service contract support", "accountId": customer, "contractId": contract, "slaPolicyId": policy,
			"startDate": day(-3), "endDate": day(362), "casesIncluded": 12.0, "hoursIncluded": 24.0, "overagePolicy": "allow", "status": "active", "description": note}))
		asset := must(mk("assets", map[string]any{"name": "Power Loom #3", "accountId": customer, "serialNumber": "PL-2024-0003", "installDate": day(-400), "warrantyEnd": day(330),
			"location": "Unit 2, Coimbatore", "contractId": contract, "status": "installed", "description": note}))
		must(mk("cases", map[string]any{"name": "Loom stops after 20 minutes", "accountId": customer, "contactId": buyer, "assetId": asset, "contractId": contract, "priority": "high", "origin": "phone",
			"status": "new", "description": "<p>" + note + "</p>"}))
		resource := must(mk("service_resources", map[string]any{"name": "Senthil (Technician)", "resourceType": "technician", "skills": "mechanical, electrical", "workStart": "09:00", "workEnd": "18:00",
			"workDays": "mon,tue,wed,thu,fri,sat", "capacity": 1.0, "status": "active", "description": note}))
		_ = resource

		// ---- to do ----
		must(mk("tasks", map[string]any{"name": "Call Priya about the revised quote", "dueDate": day(1), "priority": "high", "status": "not_started", "opportunityId": open, "accountId": customer, "description": note}))
		must(mk("events", map[string]any{"name": "Factory visit — Sri Lakshmi Textiles", "startsAt": at(3, 11), "endsAt": at(3, 12), "accountId": customer, "location": "Coimbatore", "description": note}))
		if err != nil {
			return err
		}
		_, e := tx.Exec(ctx, `INSERT INTO crm.connector_state (key, value) VALUES ($1, jsonb_build_object('records', $2::int, 'at', now())) ON CONFLICT (key) DO NOTHING`, marker, made)
		return e
	})
	if err != nil {
		return 0, err
	}
	if made > 0 {
		h.bus.Kick()
	}
	return made, nil
}
