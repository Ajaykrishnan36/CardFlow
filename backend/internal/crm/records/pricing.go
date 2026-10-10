package records

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Pricing (D-119, D-120). What an item costs a customer is decided in one place and in a
// fixed order:
//
//	price book (the one named → the customer's own → the default)
//	  → its entry for the item, currency, date and quantity      (else the catalog price)
//	  → price rules      (first that matches, by priority)
//	  → discount rules   (first that matches, by priority)
//	  → the discount typed on the line                            (within the entry's limit)
//	  → tax
//
// The result is written onto the document's line items as a snapshot — list price, unit
// price, discount, tax, which entry and why — so a later price change never alters a quote,
// order or invoice that already exists. All arithmetic is in whole hundredths.
//
//	GET/POST   /w/{code}/price-book-entries        ?priceBookId=&itemId=
//	PATCH/DELETE /w/{code}/price-book-entries/{id}
//	GET/PUT    /w/{code}/catalog_items/{id}/bundle
//	GET/POST   /w/{code}/pricing/rules ; PATCH/DELETE /w/{code}/pricing/rules/{id}
//	POST       /w/{code}/pricing/preview
//	GET/PUT    /w/{code}/{quotes|sales_orders|invoices|contracts|work_orders|opportunities|credit_notes}/{id}/lines
//	POST       /w/{code}/quotes/{id}/revise | /quotes/{id}/convert | /sales_orders/{id}/invoice

func (h *Handler) pricingRoutes(r chi.Router) {
	r.Get("/price-book-entries", h.handleListEntries)
	r.Post("/price-book-entries", h.handleSaveEntry)
	r.Patch("/price-book-entries/{id}", h.handleSaveEntry)
	r.Delete("/price-book-entries/{id}", h.handleDeleteEntry)
	r.Get("/catalog_items/{id}/bundle", h.handleGetBundle)
	r.Put("/catalog_items/{id}/bundle", h.handleSaveBundle)
	r.Get("/pricing/rules", h.handleListRules)
	r.Post("/pricing/rules", h.handleSaveRule)
	r.Patch("/pricing/rules/{id}", h.handleSaveRule)
	r.Delete("/pricing/rules/{id}", h.handleDeleteRule)
	r.Post("/pricing/preview", h.handlePricePreview)
	for object := range lineParents {
		r.Get("/"+object+"/{id}/lines", h.handleGetLines(object))
		r.Put("/"+object+"/{id}/lines", h.handlePutLines(object))
	}
	r.Post("/quotes/{id}/revise", h.handleReviseQuote)
	r.Post("/quotes/{id}/convert", h.handleConvertDocument("quotes", "sales_orders"))
	r.Post("/sales_orders/{id}/invoice", h.handleConvertDocument("sales_orders", "invoices"))
}

// lineParent describes a document that has line items.
type lineParent struct {
	field    string          // the line item field that points at the document
	editable map[string]bool // statuses in which its lines may be changed ("" = no status yet)
	totals   bool            // has subtotal / discount / tax / total
	amount   string          // otherwise: the one field that takes the total
	date     string
}

var lineParents = map[string]lineParent{
	"quotes":        {field: "quoteId", editable: map[string]bool{"": true, "draft": true}, totals: true, date: "quoteDate"},
	"sales_orders":  {field: "orderId", editable: map[string]bool{"": true, "draft": true}, totals: true, date: "orderDate"},
	"invoices":      {field: "invoiceId", editable: map[string]bool{"": true, "draft": true}, totals: true, date: "invoiceDate"},
	"credit_notes":  {field: "creditNoteId", editable: map[string]bool{"": true, "draft": true}, totals: true, date: "issueDate"},
	"contracts":     {field: "contractId", editable: map[string]bool{"": true, "draft": true, "active": true, "expiring": true}, amount: "contractValue", date: "startDate"},
	"work_orders":   {field: "workOrderId", editable: map[string]bool{"": true, "new": true, "planned": true, "scheduled": true, "in_progress": true, "on_hold": true}, amount: "total", date: ""},
	"opportunities": {field: "opportunityId", editable: map[string]bool{"": true, "prospecting": true, "qualification": true, "proposal": true, "negotiation": true}, amount: "amount", date: "closeDate"},
}

// ---------------------------------------------------------------- price book entries

type PriceBookEntry struct {
	ID             string  `json:"id"`
	PriceBookID    string  `json:"priceBookId"`
	PriceBook      string  `json:"priceBook,omitempty"`
	ItemID         string  `json:"itemId"`
	Item           string  `json:"item,omitempty"`
	SKU            string  `json:"sku,omitempty"`
	Currency       string  `json:"currency"`
	ListPrice      Cents   `json:"listPrice"`
	UnitPrice      Cents   `json:"unitPrice"`
	Cost           *Cents  `json:"cost,omitempty"`
	MaxDiscountPct *string `json:"maxDiscountPercent,omitempty"`
	MinQuantity    string  `json:"minQuantity"`
	MaxQuantity    *string `json:"maxQuantity,omitempty"`
	ValidFrom      string  `json:"validFrom"`
	ValidTo        *string `json:"validTo,omitempty"`
	Active         bool    `json:"isActive"`
	Version        int     `json:"version"`
}

const entrySelect = `
	SELECT e.id::text, e.price_book_id::text, COALESCE(b.name, ''), e.catalog_item_id::text, COALESCE(i.name, ''), COALESCE(i.custom->>'sku', ''),
	       e.currency, e.list_price::text, e.unit_price::text, e.cost::text, e.max_discount_pct::text,
	       trim(trailing '.' from trim(trailing '0' from e.min_quantity::text)), trim(trailing '.' from trim(trailing '0' from e.max_quantity::text)),
	       e.valid_from::text, e.valid_to::text, e.is_active, e.version
	FROM crm.price_book_entries e
	LEFT JOIN crm.object_records b ON b.id = e.price_book_id
	LEFT JOIN crm.object_records i ON i.id = e.catalog_item_id`

func scanEntry(row pgx.Row) (PriceBookEntry, error) {
	var e PriceBookEntry
	var list, unit string
	var cost *string
	err := row.Scan(&e.ID, &e.PriceBookID, &e.PriceBook, &e.ItemID, &e.Item, &e.SKU, &e.Currency, &list, &unit, &cost, &e.MaxDiscountPct,
		&e.MinQuantity, &e.MaxQuantity, &e.ValidFrom, &e.ValidTo, &e.Active, &e.Version)
	e.ListPrice, e.UnitPrice = mustCents(list), mustCents(unit)
	if cost != nil {
		c := mustCents(*cost)
		e.Cost = &c
	}
	return e, err
}

func (h *Handler) handleListEntries(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.Can("price_books", "read") && !sc.Can("catalog_items", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	q := r.URL.Query()
	book, _ := uuid.Parse(q.Get("priceBookId"))
	item, _ := uuid.Parse(q.Get("itemId"))
	rows, err := h.store.Pool.Query(r.Context(), entrySelect+`
		WHERE e.workspace_id = $1 AND e.deleted_at IS NULL AND ($2::uuid IS NULL OR e.price_book_id = $2) AND ($3::uuid IS NULL OR e.catalog_item_id = $3)
		ORDER BY i.name, e.currency, e.min_quantity, e.valid_from DESC LIMIT 1000`, sc.WS, nullUUID(book), nullUUID(item))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	list := []PriceBookEntry{}
	for rows.Next() {
		e, err := scanEntry(rows)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		list = append(list, e)
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": list, "canManage": canManagePricing(sc), "base": h.baseCurrency(r.Context(), h.store.Pool, sc.WS)})
}

func nullUUID(id uuid.UUID) *uuid.UUID {
	if id == uuid.Nil {
		return nil
	}
	return &id
}

func (h *Handler) handleSaveEntry(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !canManagePricing(sc) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	var in struct {
		PriceBookID    string  `json:"priceBookId"`
		ItemID         string  `json:"itemId"`
		Currency       string  `json:"currency"`
		ListPrice      any     `json:"listPrice"`
		UnitPrice      any     `json:"unitPrice"`
		Cost           any     `json:"cost"`
		MaxDiscountPct any     `json:"maxDiscountPercent"`
		MinQuantity    any     `json:"minQuantity"`
		MaxQuantity    any     `json:"maxQuantity"`
		ValidFrom      string  `json:"validFrom"`
		ValidTo        *string `json:"validTo"`
		Active         *bool   `json:"isActive"`
		Version        *int    `json:"expectedVersion"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	var id uuid.UUID
	if raw := chi.URLParam(r, "id"); raw != "" {
		var err error
		if id, err = uuid.Parse(raw); err != nil {
			shared.WriteError(w, r, shared.NotFound("entry_not_found"))
			return
		}
		// Editing: the book and item stay; missing fields keep their value.
		cur, err := scanEntry(h.store.Pool.QueryRow(ctx, entrySelect+` WHERE e.id = $1 AND e.workspace_id = $2 AND e.deleted_at IS NULL`, id, sc.WS))
		if err != nil {
			shared.WriteError(w, r, shared.NotFound("entry_not_found"))
			return
		}
		if in.Version != nil && *in.Version != cur.Version {
			shared.WriteError(w, r, shared.NewError(http.StatusConflict, "version_conflict", "Someone else changed this price. Reload and try again."))
			return
		}
		in.PriceBookID, in.ItemID = cur.PriceBookID, cur.ItemID
		if in.Currency == "" {
			in.Currency = cur.Currency
		}
		if in.ListPrice == nil {
			in.ListPrice = cur.ListPrice.String()
		}
		if in.UnitPrice == nil {
			in.UnitPrice = cur.UnitPrice.String()
		}
		if in.Cost == nil && cur.Cost != nil {
			in.Cost = cur.Cost.String()
		}
		if in.MaxDiscountPct == nil && cur.MaxDiscountPct != nil {
			in.MaxDiscountPct = *cur.MaxDiscountPct
		}
		if in.MinQuantity == nil {
			in.MinQuantity = cur.MinQuantity
		}
		if in.MaxQuantity == nil && cur.MaxQuantity != nil {
			in.MaxQuantity = *cur.MaxQuantity
		}
		if in.ValidFrom == "" {
			in.ValidFrom = cur.ValidFrom
		}
		if in.ValidTo == nil {
			in.ValidTo = cur.ValidTo
		}
		if in.Active == nil {
			in.Active = &cur.Active
		}
	}
	fe := map[string]string{}
	book, _ := uuid.Parse(in.PriceBookID)
	item, _ := uuid.Parse(in.ItemID)
	// Both ends must be records of this business.
	var okBook, okItem bool
	_ = h.store.Pool.QueryRow(ctx, `SELECT
		EXISTS (SELECT 1 FROM crm.object_records WHERE id = $2 AND workspace_id = $1 AND object_key = 'price_books' AND deleted_at IS NULL),
		EXISTS (SELECT 1 FROM crm.object_records WHERE id = $3 AND workspace_id = $1 AND object_key = 'catalog_items' AND deleted_at IS NULL)`,
		sc.WS, book, item).Scan(&okBook, &okItem)
	if !okBook {
		fe["priceBookId"] = "Choose a price book."
	}
	if !okItem {
		fe["itemId"] = "Choose a product or service."
	}
	in.Currency = strings.ToUpper(strings.TrimSpace(in.Currency))
	if in.Currency == "" {
		in.Currency = h.baseCurrency(ctx, h.store.Pool, sc.WS)
	}
	var known bool
	_ = h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.currencies WHERE code = $1 AND is_active)`, in.Currency).Scan(&known)
	if !known {
		fe["currency"] = "Choose a currency from the list."
	}
	unit, ok := parseCents(in.UnitPrice)
	if !ok || unit < 0 {
		fe["unitPrice"] = "Enter the price."
	}
	list, ok := parseCents(in.ListPrice)
	if !ok {
		list = unit
	}
	if list < unit {
		fe["listPrice"] = "The list price can't be below the selling price."
	}
	var cost *string
	if c, ok := parseCents(in.Cost); ok {
		if c < 0 {
			fe["cost"] = "Enter a cost of 0 or more."
		}
		s := c.String()
		cost = &s
	}
	var maxDisc *string
	if p, ok := bps(in.MaxDiscountPct); ok {
		if p < 0 || p > 10000 {
			fe["maxDiscountPercent"] = "Enter a percentage from 0 to 100."
		}
		s := Cents(p).String()
		maxDisc = &s
	}
	minQ := numText(in.MinQuantity)
	if minQ == "" {
		minQ = "1"
	}
	if f, _ := strconv.ParseFloat(minQ, 64); f <= 0 {
		fe["minQuantity"] = "Enter a quantity above 0."
	}
	var maxQ *string
	if s := numText(in.MaxQuantity); s != "" {
		a, _ := strconv.ParseFloat(s, 64)
		b, _ := strconv.ParseFloat(minQ, 64)
		if a < b {
			fe["maxQuantity"] = "This can't be below the minimum quantity."
		}
		maxQ = &s
	}
	if in.ValidFrom == "" {
		in.ValidFrom = time.Now().Format("2006-01-02")
	}
	if _, err := time.Parse("2006-01-02", in.ValidFrom); err != nil {
		fe["validFrom"] = "Enter a date."
	}
	if in.ValidTo != nil && *in.ValidTo == "" {
		in.ValidTo = nil
	}
	if in.ValidTo != nil {
		if _, err := time.Parse("2006-01-02", *in.ValidTo); err != nil || *in.ValidTo < in.ValidFrom {
			fe["validTo"] = "Enter a date on or after the start date."
		}
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	active := in.Active == nil || *in.Active
	a := actorFromRequest(r, "ui")
	var out PriceBookEntry
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var before any
		if id == uuid.Nil {
			if err := tx.QueryRow(ctx, `
				INSERT INTO crm.price_book_entries (workspace_id, price_book_id, catalog_item_id, currency, list_price, unit_price, cost, max_discount_pct,
				  min_quantity, max_quantity, valid_from, valid_to, is_active, created_by, updated_by)
				VALUES ($1, $2, $3, $4, $5::numeric, $6::numeric, $7::numeric, $8::numeric, $9::numeric, $10::numeric, $11::date, $12::date, $13, $14, $14) RETURNING id`,
				sc.WS, book, item, in.Currency, list.String(), unit.String(), cost, maxDisc, minQ, maxQ, in.ValidFrom, in.ValidTo, active, a.ID).Scan(&id); err != nil {
				return entryConflict(err)
			}
		} else {
			old, err := scanEntry(tx.QueryRow(ctx, entrySelect+` WHERE e.id = $1 AND e.workspace_id = $2 FOR UPDATE OF e`, id, sc.WS))
			if err != nil {
				return err
			}
			before = old
			if _, err := tx.Exec(ctx, `
				UPDATE crm.price_book_entries SET currency = $3, list_price = $4::numeric, unit_price = $5::numeric, cost = $6::numeric, max_discount_pct = $7::numeric,
				  min_quantity = $8::numeric, max_quantity = $9::numeric, valid_from = $10::date, valid_to = $11::date, is_active = $12,
				  updated_by = $13, updated_at = now(), version = version + 1
				WHERE id = $1 AND workspace_id = $2`, id, sc.WS, in.Currency, list.String(), unit.String(), cost, maxDisc, minQ, maxQ, in.ValidFrom, in.ValidTo, active, a.ID); err != nil {
				return entryConflict(err)
			}
		}
		var err error
		if out, err = scanEntry(tx.QueryRow(ctx, entrySelect+` WHERE e.id = $1`, id)); err != nil {
			return err
		}
		// A price change is a financial event: who, what, old and new.
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "price_book_entry.saved", "price_book_entry", &id, before, out))
	})
	respond(w, r, http.StatusOK, out, err)
}

func entryConflict(err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23505" {
		return shared.Validation(map[string]string{"validFrom": "This price book already has a price for this item, currency and quantity from that date. Change that one, or pick another start date."})
	}
	return err
}

func (h *Handler) handleDeleteEntry(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !canManagePricing(sc) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("entry_not_found"))
		return
	}
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		// Kept (soft delete): documents priced from it still name it.
		tag, err := tx.Exec(r.Context(), `UPDATE crm.price_book_entries SET deleted_at = now(), is_active = false, updated_by = $3 WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL`, id, sc.WS, a.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return shared.NotFound("entry_not_found")
		}
		return shared.WriteAudit(r.Context(), tx, a.audit(sc.WS, "price_book_entry.deleted", "price_book_entry", &id, nil, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- bundles

type BundleComponent struct {
	ItemID      string  `json:"itemId"`
	Item        string  `json:"item,omitempty"`
	Required    bool    `json:"required"`
	Quantity    float64 `json:"quantity"`
	MinQuantity *string `json:"minQuantity,omitempty"`
	MaxQuantity *string `json:"maxQuantity,omitempty"`
	Sequence    int     `json:"sequence"`
	PriceMode   string  `json:"priceMode"` // included | additional
}

func (h *Handler) bundleOf(ctx context.Context, q querier, ws uuid.UUID, bundles []uuid.UUID) (map[uuid.UUID][]BundleComponent, error) {
	out := map[uuid.UUID][]BundleComponent{}
	if len(bundles) == 0 {
		return out, nil
	}
	rows, err := q.Query(ctx, `
		SELECT b.bundle_item_id, b.component_item_id::text, COALESCE(i.name, ''), b.is_required, b.quantity::float8, b.min_quantity::text, b.max_quantity::text, b.sequence, b.price_mode
		FROM crm.product_bundle_items b JOIN crm.object_records i ON i.id = b.component_item_id AND i.deleted_at IS NULL
		WHERE b.workspace_id = $1 AND b.bundle_item_id = ANY($2) ORDER BY b.sequence, i.name`, ws, bundles)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id uuid.UUID
		var c BundleComponent
		if err := rows.Scan(&id, &c.ItemID, &c.Item, &c.Required, &c.Quantity, &c.MinQuantity, &c.MaxQuantity, &c.Sequence, &c.PriceMode); err != nil {
			return nil, err
		}
		out[id] = append(out[id], c)
	}
	return out, rows.Err()
}

func (h *Handler) catalogItem(r *http.Request, need string) (*Scope, *Row, error) {
	sc := scopeFrom(r.Context())
	if !sc.Can("catalog_items", need) {
		return nil, nil, errForbidden
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return nil, nil, shared.NotFound("record_not_found")
	}
	row, _, err := h.getRow(r.Context(), h.store.Pool, sc.WS, specFor("catalog_items"), id, sc.OwnersFor("catalog_items", actor(r)))
	return sc, row, err
}

func (h *Handler) handleGetBundle(w http.ResponseWriter, r *http.Request) {
	sc, item, err := h.catalogItem(r, "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	m, err := h.bundleOf(r.Context(), h.store.Pool, sc.WS, []uuid.UUID{item.uuid()})
	list := m[item.uuid()]
	if list == nil {
		list = []BundleComponent{}
	}
	respond(w, r, http.StatusOK, map[string]any{"data": list, "canManage": canManagePricing(sc)}, err)
}

func (h *Handler) handleSaveBundle(w http.ResponseWriter, r *http.Request) {
	sc, item, err := h.catalogItem(r, "update")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if !canManagePricing(sc) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	var in struct {
		Components []BundleComponent `json:"components"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	fe := map[string]string{}
	ids := []uuid.UUID{}
	seen := map[uuid.UUID]bool{}
	for i, c := range in.Components {
		id, err := uuid.Parse(c.ItemID)
		key := "components." + strconv.Itoa(i)
		switch {
		case err != nil:
			fe[key] = "Choose a product or service."
		case id == item.uuid():
			fe[key] = "A bundle can't contain itself."
		case seen[id]:
			fe[key] = "This item is already in the bundle."
		case c.Quantity <= 0:
			fe[key] = "Enter a quantity above 0."
		case c.PriceMode != "" && c.PriceMode != "included" && c.PriceMode != "additional":
			fe[key] = "Choose whether the price is included in the bundle or added to it."
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(in.Components) > 100 {
		fe["components"] = "A bundle can have up to 100 components."
	}
	// Components are items of this business; and none of them may be a bundle that leads back here.
	var found int
	_ = h.store.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'catalog_items' AND deleted_at IS NULL AND id = ANY($2)`, sc.WS, ids).Scan(&found)
	if found != len(seen) && len(fe) == 0 {
		fe["components"] = "Choose products and services of this business."
	}
	var loops bool
	_ = h.store.Pool.QueryRow(ctx, `
		WITH RECURSIVE down AS (
		  SELECT component_item_id AS id FROM crm.product_bundle_items WHERE workspace_id = $1 AND bundle_item_id = ANY($2)
		  UNION SELECT b.component_item_id FROM crm.product_bundle_items b JOIN down d ON b.bundle_item_id = d.id WHERE b.workspace_id = $1)
		SELECT EXISTS (SELECT 1 FROM down WHERE id = $3)`, sc.WS, ids, item.uuid()).Scan(&loops)
	if loops {
		fe["components"] = "One of these is a bundle that already contains this item."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM crm.product_bundle_items WHERE workspace_id = $1 AND bundle_item_id = $2`, sc.WS, item.uuid()); err != nil {
			return err
		}
		for i, c := range in.Components {
			mode := c.PriceMode
			if mode == "" {
				mode = "additional"
			}
			seq := c.Sequence
			if seq == 0 {
				seq = i + 1
			}
			if _, err := tx.Exec(ctx, `INSERT INTO crm.product_bundle_items (workspace_id, bundle_item_id, component_item_id, is_required, quantity, min_quantity, max_quantity, sequence, price_mode, created_by)
				VALUES ($1, $2, $3, $4, $5, $6::numeric, $7::numeric, $8, $9, $10)`, sc.WS, item.uuid(), ids[i], c.Required, c.Quantity, c.MinQuantity, c.MaxQuantity, seq, mode, a.ID); err != nil {
				return err
			}
		}
		if item.flag("isBundle") != (len(in.Components) > 0) {
			if _, err := h.updateValues(ctx, tx, sc.WS, specFor("catalog_items"), item.uuid(), a, map[string]any{"isBundle": len(in.Components) > 0}, nil, nil); err != nil {
				return err
			}
		}
		id := item.uuid()
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "bundle.saved", "catalog_item", &id, nil, in.Components))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.handleGetBundle(w, r)
}

// ---------------------------------------------------------------- rules

type ruleCondition struct {
	ItemIDs      []string `json:"itemIds,omitempty"`     // the items the rule is about (empty = every item)
	Family       string   `json:"family,omitempty"`      // product family
	ItemType     string   `json:"itemType,omitempty"`    // product | service
	MinQuantity  *float64 `json:"minQuantity,omitempty"` // of the line
	MaxQuantity  *float64 `json:"maxQuantity,omitempty"`
	AccountIDs   []string `json:"accountIds,omitempty"`   // the customer is one of these
	AccountTypes []string `json:"accountTypes,omitempty"` // the customer's type is one of these
	TerritoryIDs []string `json:"territoryIds,omitempty"` // the customer is in one of these territories
	PriceBookIDs []string `json:"priceBookIds,omitempty"`
	WithItemIDs  []string `json:"withItemIds,omitempty"` // every one of these is also on the document
	Currency     string   `json:"currency,omitempty"`
}

type ruleAction struct {
	// price: fixed | percent_of_list.  discount: percent | amount (per unit).
	Type  string `json:"type,omitempty"`
	Value any    `json:"value,omitempty"`
	// configuration: the items in the condition need / can't go with these.
	Requires []string `json:"requires,omitempty"`
	Excludes []string `json:"excludes,omitempty"`
	// eligibility: the items in the condition are only for these customers.
	AccountTypes []string `json:"accountTypes,omitempty"`
	TerritoryIDs []string `json:"territoryIds,omitempty"`
	Message      string   `json:"message,omitempty"`
}

type PricingRule struct {
	ID            string        `json:"id"`
	Name          string        `json:"name"`
	Kind          string        `json:"kind"`
	Priority      int           `json:"priority"`
	Condition     ruleCondition `json:"condition"`
	Action        ruleAction    `json:"action"`
	EffectiveFrom *string       `json:"effectiveFrom,omitempty"`
	EffectiveTo   *string       `json:"effectiveTo,omitempty"`
	Active        bool          `json:"isActive"`
}

func (h *Handler) pricingRules(ctx context.Context, q querier, ws uuid.UUID, onlyLiveOn string) ([]PricingRule, error) {
	rows, err := q.Query(ctx, `
		SELECT id::text, name, kind, priority, condition, action, effective_from::text, effective_to::text, is_active
		FROM crm.pricing_rules WHERE workspace_id = $1
		  AND ($2 = '' OR (is_active AND (effective_from IS NULL OR effective_from <= $2::date) AND (effective_to IS NULL OR effective_to >= $2::date)))
		ORDER BY priority, created_at, id`, ws, onlyLiveOn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []PricingRule{}
	for rows.Next() {
		var x PricingRule
		var cond, act []byte
		if err := rows.Scan(&x.ID, &x.Name, &x.Kind, &x.Priority, &cond, &act, &x.EffectiveFrom, &x.EffectiveTo, &x.Active); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(cond, &x.Condition)
		_ = json.Unmarshal(act, &x.Action)
		list = append(list, x)
	}
	return list, rows.Err()
}

func (h *Handler) handleListRules(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.Can("catalog_items", "read") && !canManagePricing(sc) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	list, err := h.pricingRules(r.Context(), h.store.Pool, sc.WS, "")
	respond(w, r, http.StatusOK, map[string]any{"data": list, "canManage": canManagePricing(sc)}, err)
}

func (h *Handler) handleSaveRule(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !canManagePricing(sc) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	var in PricingRule
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	var id uuid.UUID
	if raw := chi.URLParam(r, "id"); raw != "" {
		var err error
		if id, err = uuid.Parse(raw); err != nil {
			shared.WriteError(w, r, shared.NotFound("rule_not_found"))
			return
		}
	}
	in.Name = strings.TrimSpace(in.Name)
	fe := map[string]string{}
	if in.Name == "" || len(in.Name) > 120 {
		fe["name"] = "Name the rule (up to 120 characters)."
	}
	if in.Priority == 0 {
		in.Priority = 100
	}
	val, hasVal := parseCents(in.Action.Value)
	switch in.Kind {
	case "price":
		if (in.Action.Type != "fixed" && in.Action.Type != "percent_of_list") || !hasVal || val < 0 {
			fe["action"] = "Set a fixed price, or a percentage of the list price."
		}
	case "discount":
		if (in.Action.Type != "percent" && in.Action.Type != "amount") || !hasVal || val <= 0 || (in.Action.Type == "percent" && val > 10000) {
			fe["action"] = "Set a discount: a percentage up to 100, or an amount per unit."
		}
	case "configuration":
		if len(in.Condition.ItemIDs) == 0 || len(in.Action.Requires)+len(in.Action.Excludes) == 0 {
			fe["action"] = "Choose the items the rule is about, and what they need or can't be sold with."
		}
	case "eligibility":
		if len(in.Condition.ItemIDs) == 0 || len(in.Action.AccountTypes)+len(in.Action.TerritoryIDs) == 0 {
			fe["action"] = "Choose the items, and the customer types or territories that may buy them."
		}
	default:
		fe["kind"] = "Choose what the rule does."
	}
	// Every record a rule names must be one of this business.
	refs := map[string][]string{
		"catalog_items": append(append(append(append([]string{}, in.Condition.ItemIDs...), in.Condition.WithItemIDs...), in.Action.Requires...), in.Action.Excludes...),
		"price_books":   in.Condition.PriceBookIDs,
		"territories":   append(append([]string{}, in.Condition.TerritoryIDs...), in.Action.TerritoryIDs...),
	}
	for object, list := range refs {
		ids, bad := parseUUIDs(list)
		var n int
		_ = h.store.Pool.QueryRow(ctx, `SELECT count(DISTINCT id) FROM crm.object_records WHERE workspace_id = $1 AND object_key = $2 AND deleted_at IS NULL AND id = ANY($3)`, sc.WS, object, ids).Scan(&n)
		if bad || n != len(uniqueUUIDs(ids)) {
			fe["condition"] = "The rule names a record that isn't in this business."
		}
	}
	if ids, bad := parseUUIDs(in.Condition.AccountIDs); bad {
		fe["condition"] = "The rule names a record that isn't in this business."
	} else if len(ids) > 0 {
		var n int
		_ = h.store.Pool.QueryRow(ctx, `SELECT count(DISTINCT id) FROM crm.accounts WHERE workspace_id = $1 AND deleted_at IS NULL AND id = ANY($2)`, sc.WS, ids).Scan(&n)
		if n != len(uniqueUUIDs(ids)) {
			fe["condition"] = "The rule names a record that isn't in this business."
		}
	}
	for _, d := range []*string{in.EffectiveFrom, in.EffectiveTo} {
		if d != nil && *d != "" {
			if _, err := time.Parse("2006-01-02", *d); err != nil {
				fe["effectiveFrom"] = "Enter dates as YYYY-MM-DD."
			}
		}
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	cond, _ := json.Marshal(in.Condition)
	act, _ := json.Marshal(in.Action)
	a := actorFromRequest(r, "ui")
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		if id == uuid.Nil {
			if err := tx.QueryRow(ctx, `INSERT INTO crm.pricing_rules (workspace_id, name, kind, priority, condition, action, effective_from, effective_to, is_active, created_by, updated_by)
				VALUES ($1, $2, $3, $4, $5, $6, NULLIF($7, '')::date, NULLIF($8, '')::date, true, $9, $9) RETURNING id`,
				sc.WS, in.Name, in.Kind, in.Priority, cond, act, strOr(in.EffectiveFrom), strOr(in.EffectiveTo), a.ID).Scan(&id); err != nil {
				return err
			}
		} else {
			tag, err := tx.Exec(ctx, `UPDATE crm.pricing_rules SET name = $3, kind = $4, priority = $5, condition = $6, action = $7,
				effective_from = NULLIF($8, '')::date, effective_to = NULLIF($9, '')::date, is_active = $10, updated_by = $11, updated_at = now()
				WHERE id = $1 AND workspace_id = $2`, id, sc.WS, in.Name, in.Kind, in.Priority, cond, act, strOr(in.EffectiveFrom), strOr(in.EffectiveTo), in.Active, a.ID)
			if err != nil {
				return err
			}
			if tag.RowsAffected() == 0 {
				return shared.NotFound("rule_not_found")
			}
		}
		in.ID = id.String()
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "pricing_rule.saved", "pricing_rule", &id, nil, in))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.handleListRules(w, r)
}

func strOr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

func parseUUIDs(list []string) ([]uuid.UUID, bool) {
	out := make([]uuid.UUID, 0, len(list))
	bad := false
	for _, s := range list {
		id, err := uuid.Parse(s)
		if err != nil {
			bad = true
			continue
		}
		out = append(out, id)
	}
	return out, bad
}

func uniqueUUIDs(list []uuid.UUID) []uuid.UUID {
	seen := map[uuid.UUID]bool{}
	out := []uuid.UUID{}
	for _, id := range list {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}

func (h *Handler) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !canManagePricing(sc) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("rule_not_found"))
		return
	}
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `DELETE FROM crm.pricing_rules WHERE id = $1 AND workspace_id = $2`, id, sc.WS)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return shared.NotFound("rule_not_found")
		}
		return shared.WriteAudit(r.Context(), tx, a.audit(sc.WS, "pricing_rule.deleted", "pricing_rule", &id, nil, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---------------------------------------------------------------- the engine

type PriceOption struct {
	ItemID   string `json:"itemId"`
	Quantity any    `json:"quantity"`
}

type PriceLineIn struct {
	ItemID           string        `json:"itemId"`
	Name             string        `json:"name"`
	Quantity         any           `json:"quantity"`
	DiscountPercent  any           `json:"discountPercent"`
	UnitPrice        any           `json:"unitPrice"` // a typed-in price: needs "Manage pricing", or an item with no price anywhere
	Options          []PriceOption `json:"options"`   // optional bundle components to include
	StartDate        string        `json:"startDate"`
	EndDate          string        `json:"endDate"`
	BillingFrequency string        `json:"billingFrequency"`
}

type PriceRequest struct {
	PriceBookID string        `json:"priceBookId"`
	Currency    string        `json:"currency"`
	AccountID   string        `json:"accountId"`
	Date        string        `json:"date"`
	Lines       []PriceLineIn `json:"lines"`
}

type PricedLine struct {
	ItemID           string  `json:"itemId,omitempty"`
	Name             string  `json:"name"`
	SKU              string  `json:"sku,omitempty"`
	Quantity         float64 `json:"quantity"`
	ListPrice        Cents   `json:"listPrice"`
	UnitPrice        Cents   `json:"unitPrice"`       // after price and discount rules, before the line's own discount
	DiscountPercent  float64 `json:"discountPercent"` // the line's own discount
	DiscountAmount   Cents   `json:"discountAmount"`  // everything off the list price, for the whole line
	TaxRate          float64 `json:"taxRate"`
	TaxAmount        Cents   `json:"taxAmount"`
	Total            Cents   `json:"total"` // before tax
	UnitCost         *Cents  `json:"unitCost,omitempty"`
	EntryID          string  `json:"priceBookEntryId,omitempty"`
	Note             string  `json:"pricingNote"`
	BundleOf         *int    `json:"bundleOf,omitempty"` // index of the bundle line this is a component of
	StartDate        string  `json:"startDate,omitempty"`
	EndDate          string  `json:"endDate,omitempty"`
	BillingFrequency string  `json:"billingFrequency,omitempty"`
	offListBps       int64
}

type ApprovalNeed struct {
	Required     bool    `json:"required"`
	Kind         string  `json:"kind"`
	Value        float64 `json:"value"`
	ApproverRole string  `json:"approverRole,omitempty"`
	Label        string  `json:"label,omitempty"`
}

type PriceResult struct {
	PriceBookID        string       `json:"priceBookId,omitempty"`
	PriceBook          string       `json:"priceBook,omitempty"`
	Currency           string       `json:"currency"`
	Lines              []PricedLine `json:"lines"`
	Subtotal           Cents        `json:"subtotal"` // at list price
	Discount           Cents        `json:"discount"`
	Tax                Cents        `json:"tax"`
	Total              Cents        `json:"total"`
	MaxDiscountPercent float64      `json:"maxDiscountPercent"`
	Approval           ApprovalNeed `json:"approval"`
}

type catalogRow struct {
	id       uuid.UUID
	name     string
	sku      string
	family   string
	itemType string
	status   string
	price    Cents
	hasPrice bool
	cost     *Cents
	taxBps   int64
}

type entryRow struct {
	id      string
	list    Cents
	unit    Cents
	cost    *Cents
	maxDisc *int64
	minQ    int64
	maxQ    *int64
}

// priceLines works out the price of every line. canOverride: the caller may type a price.
func (h *Handler) priceLines(ctx context.Context, q querier, ws uuid.UUID, req PriceRequest, canOverride bool) (*PriceResult, error) {
	base := h.baseCurrency(ctx, q, ws)
	day := req.Date
	if len(day) >= 10 {
		day = day[:10]
	}
	if _, err := time.Parse("2006-01-02", day); err != nil {
		day = time.Now().Format("2006-01-02")
	}
	fe := map[string]string{}
	if len(req.Lines) > 300 {
		return nil, shared.Validation(map[string]string{"lines": "A document can have up to 300 lines."})
	}

	// ---- the customer ----
	account, _ := uuid.Parse(req.AccountID)
	var accountType string
	accountTerritories := map[string]bool{}
	if account != uuid.Nil {
		if err := q.QueryRow(ctx, `SELECT COALESCE(type, '') FROM crm.accounts WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL`, account, ws).Scan(&accountType); err != nil {
			return nil, shared.Validation(map[string]string{"accountId": "Choose an account of this business."})
		}
		rows, err := q.Query(ctx, `SELECT territory_id::text FROM crm.territory_assignments
			WHERE workspace_id = $1 AND account_id = $2 AND effective_from <= $3::date AND (effective_to IS NULL OR effective_to >= $3::date)`, ws, account, day)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var t string
			if err := rows.Scan(&t); err != nil {
				rows.Close()
				return nil, err
			}
			accountTerritories[t] = true
		}
		rows.Close()
	}

	// ---- the price book: named → the customer's own → the default ----
	out := &PriceResult{Lines: []PricedLine{}}
	book, _ := uuid.Parse(req.PriceBookID)
	var bookCurrency string
	err := q.QueryRow(ctx, `
		SELECT id, name, COALESCE(custom->>'currency', '') FROM crm.object_records
		WHERE workspace_id = $1 AND object_key = 'price_books' AND deleted_at IS NULL
		  AND (($2::uuid IS NOT NULL AND id = $2)
		    OR ($2::uuid IS NULL AND COALESCE(status, 'active') = 'active'
		        AND (COALESCE(custom->>'validFrom', '') = '' OR custom->>'validFrom' <= $4) AND (COALESCE(custom->>'validTo', '') = '' OR custom->>'validTo' >= $4)
		        AND (($3::uuid IS NOT NULL AND custom->>'accountId' = $3::text) OR (custom->>'isDefault' = 'true' AND COALESCE(custom->>'accountId', '') = ''))))
		ORDER BY (custom->>'accountId' = $3::text) DESC NULLS LAST, created_at LIMIT 1`, ws, nullUUID(book), nullUUID(account), day).Scan(&book, &out.PriceBook, &bookCurrency)
	if errors.Is(err, pgx.ErrNoRows) {
		if req.PriceBookID != "" {
			return nil, shared.Validation(map[string]string{"priceBookId": "Choose a price book of this business."})
		}
		book = uuid.Nil
	} else if err != nil {
		return nil, err
	}
	if book != uuid.Nil {
		out.PriceBookID = book.String()
	}
	out.Currency = strings.ToUpper(strings.TrimSpace(req.Currency))
	if out.Currency == "" {
		out.Currency = bookCurrency
	}
	if out.Currency == "" {
		out.Currency = base
	}

	// ---- expand bundles into lines ----
	type work struct {
		in       PriceLineIn
		item     uuid.UUID
		qty      int64
		bundleOf *int
		included bool // a component whose price is part of the bundle's
		index    int  // of the request line, for error messages
	}
	var lines []work
	top := []uuid.UUID{}
	for i, l := range req.Lines {
		id, err := uuid.Parse(l.ItemID)
		if l.ItemID != "" && err != nil {
			fe["lines."+strconv.Itoa(i)] = "Choose a product or service."
			continue
		}
		qty, ok := milli(l.Quantity)
		if l.Quantity == nil {
			qty, ok = 1000, true
		}
		if !ok || qty <= 0 {
			fe["lines."+strconv.Itoa(i)] = "Enter a quantity above 0."
			continue
		}
		if id == uuid.Nil && strings.TrimSpace(l.Name) == "" {
			fe["lines."+strconv.Itoa(i)] = "Choose an item, or describe the line."
			continue
		}
		lines = append(lines, work{in: l, item: id, qty: qty, index: i})
		if id != uuid.Nil {
			top = append(top, id)
		}
	}
	bundles, err := h.bundleOf(ctx, q, ws, top)
	if err != nil {
		return nil, err
	}
	var expanded []work
	for _, l := range lines {
		parent := len(expanded)
		expanded = append(expanded, l)
		comps := bundles[l.item]
		if len(comps) == 0 {
			if len(l.in.Options) > 0 {
				fe["lines."+strconv.Itoa(l.index)] = "This item is not a bundle, so it has no options."
			}
			continue
		}
		chosen := map[string]PriceOption{}
		for _, o := range l.in.Options {
			chosen[o.ItemID] = o
		}
		for _, c := range comps {
			o, picked := chosen[c.ItemID]
			delete(chosen, c.ItemID)
			if !c.Required && !picked {
				continue
			}
			per := int64(c.Quantity*1000 + 0.5)
			if picked && o.Quantity != nil {
				v, ok := milli(o.Quantity)
				if !ok || v <= 0 {
					fe["lines."+strconv.Itoa(l.index)] = "Enter a quantity above 0 for " + c.Item + "."
					continue
				}
				per = v
			}
			if c.MinQuantity != nil {
				if m, _ := milli(*c.MinQuantity); per < m {
					fe["lines."+strconv.Itoa(l.index)] = c.Item + ": at least " + *c.MinQuantity + " per bundle."
				}
			}
			if c.MaxQuantity != nil {
				if m, _ := milli(*c.MaxQuantity); per > m {
					fe["lines."+strconv.Itoa(l.index)] = c.Item + ": at most " + *c.MaxQuantity + " per bundle."
				}
			}
			cid, _ := uuid.Parse(c.ItemID)
			p := parent
			expanded = append(expanded, work{item: cid, qty: per * l.qty / 1000, bundleOf: &p, included: c.PriceMode == "included", index: l.index})
		}
		for id := range chosen {
			_ = id
			fe["lines."+strconv.Itoa(l.index)] = "One of the chosen options is not part of this bundle."
		}
	}

	// ---- the items ----
	ids := []uuid.UUID{}
	for _, l := range expanded {
		if l.item != uuid.Nil {
			ids = append(ids, l.item)
		}
	}
	items := map[uuid.UUID]catalogRow{}
	rows, err := q.Query(ctx, `SELECT id, name, COALESCE(status, ''), custom FROM crm.object_records
		WHERE workspace_id = $1 AND object_key = 'catalog_items' AND deleted_at IS NULL AND id = ANY($2)`, ws, ids)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var c catalogRow
		var raw []byte
		if err := rows.Scan(&c.id, &c.name, &c.status, &raw); err != nil {
			rows.Close()
			return nil, err
		}
		var v map[string]any
		_ = json.Unmarshal(raw, &v)
		c.sku, _ = v["sku"].(string)
		c.family, _ = v["family"].(string)
		c.itemType, _ = v["itemType"].(string)
		c.price, c.hasPrice = parseCents(v["unitPrice"])
		if x, ok := parseCents(v["cost"]); ok {
			c.cost = &x
		}
		c.taxBps, _ = bps(v["taxRate"])
		items[c.id] = c
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	onDoc := map[string]bool{}
	for _, l := range expanded {
		if l.item != uuid.Nil {
			onDoc[l.item.String()] = true
		}
	}

	// ---- entries of the book for these items ----
	entries := map[uuid.UUID][]entryRow{}
	if book != uuid.Nil {
		rows, err := q.Query(ctx, `
			SELECT catalog_item_id, id::text, list_price::text, unit_price::text, cost::text, (max_discount_pct * 100)::bigint,
			       (min_quantity * 1000)::bigint, (max_quantity * 1000)::bigint
			FROM crm.price_book_entries
			WHERE workspace_id = $1 AND price_book_id = $2 AND catalog_item_id = ANY($3) AND currency = $4 AND deleted_at IS NULL AND is_active
			  AND valid_from <= $5::date AND (valid_to IS NULL OR valid_to >= $5::date)
			ORDER BY min_quantity DESC, valid_from DESC`, ws, book, ids, out.Currency, day)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var item uuid.UUID
			var e entryRow
			var list, unit string
			var cost *string
			if err := rows.Scan(&item, &e.id, &list, &unit, &cost, &e.maxDisc, &e.minQ, &e.maxQ); err != nil {
				rows.Close()
				return nil, err
			}
			e.list, e.unit = mustCents(list), mustCents(unit)
			if cost != nil {
				c := mustCents(*cost)
				e.cost = &c
			}
			entries[item] = append(entries[item], e)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	rules, err := h.pricingRules(ctx, q, ws, day)
	if err != nil {
		return nil, err
	}
	in := func(list []string, v string) bool {
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	}
	anyIn := func(list []string, set map[string]bool) bool {
		for _, x := range list {
			if set[x] {
				return true
			}
		}
		return false
	}
	matches := func(c ruleCondition, item catalogRow, qty int64) bool {
		if len(c.ItemIDs) > 0 && !in(c.ItemIDs, item.id.String()) {
			return false
		}
		if c.Family != "" && !strings.EqualFold(c.Family, item.family) {
			return false
		}
		if c.ItemType != "" && c.ItemType != item.itemType && !(c.ItemType == "product" && item.itemType == "") {
			return false
		}
		if c.MinQuantity != nil && qty < int64(*c.MinQuantity*1000+0.5) {
			return false
		}
		if c.MaxQuantity != nil && qty > int64(*c.MaxQuantity*1000+0.5) {
			return false
		}
		if len(c.AccountIDs) > 0 && (account == uuid.Nil || !in(c.AccountIDs, account.String())) {
			return false
		}
		if len(c.AccountTypes) > 0 && !in(c.AccountTypes, accountType) {
			return false
		}
		if len(c.TerritoryIDs) > 0 && !anyIn(c.TerritoryIDs, accountTerritories) {
			return false
		}
		if len(c.PriceBookIDs) > 0 && (book == uuid.Nil || !in(c.PriceBookIDs, book.String())) {
			return false
		}
		if c.Currency != "" && !strings.EqualFold(c.Currency, out.Currency) {
			return false
		}
		for _, w := range c.WithItemIDs {
			if !onDoc[w] {
				return false
			}
		}
		return true
	}

	// ---- eligibility and configuration: is this a valid basket? ----
	for _, rule := range rules {
		if rule.Kind != "eligibility" && rule.Kind != "configuration" {
			continue
		}
		for _, l := range expanded {
			item, ok := items[l.item]
			if !ok || !matches(rule.Condition, item, l.qty) {
				continue
			}
			key := "lines." + strconv.Itoa(l.index)
			msg := rule.Action.Message
			if rule.Kind == "eligibility" {
				okType := len(rule.Action.AccountTypes) == 0 || in(rule.Action.AccountTypes, accountType)
				okTerr := len(rule.Action.TerritoryIDs) == 0 || anyIn(rule.Action.TerritoryIDs, accountTerritories)
				if !okType || !okTerr {
					if msg == "" {
						msg = item.name + " is not available for this customer (" + rule.Name + ")."
					}
					fe[key] = msg
				}
				continue
			}
			for _, need := range rule.Action.Requires {
				if !onDoc[need] {
					if msg == "" {
						msg = item.name + " needs another item that is not on this document (" + rule.Name + ")."
					}
					fe[key] = msg
				}
			}
			for _, not := range rule.Action.Excludes {
				if onDoc[not] && not != item.id.String() {
					if msg == "" {
						msg = item.name + " can't be sold together with another item on this document (" + rule.Name + ")."
					}
					fe[key] = msg
				}
			}
		}
	}

	// ---- price every line ----
	var maxOff int64
	for _, l := range expanded {
		key := "lines." + strconv.Itoa(l.index)
		pl := PricedLine{Quantity: float64(l.qty) / 1000, BundleOf: l.bundleOf, StartDate: l.in.StartDate, EndDate: l.in.EndDate, BillingFrequency: l.in.BillingFrequency}
		var item catalogRow
		var list, unit Cents
		var maxDisc *int64
		priced := false
		if l.item != uuid.Nil {
			var ok bool
			if item, ok = items[l.item]; !ok {
				fe[key] = "Choose a product or service of this business."
				continue
			}
			if item.status == "inactive" {
				fe[key] = item.name + " is inactive and can't be sold."
				continue
			}
			pl.ItemID, pl.Name, pl.SKU, pl.UnitCost = item.id.String(), item.name, item.sku, item.cost
			pl.TaxRate = float64(item.taxBps) / 100
			// The entry with the highest quantity break the line reaches.
			for _, e := range entries[l.item] {
				if l.qty >= e.minQ && (e.maxQ == nil || l.qty <= *e.maxQ) {
					list, unit, maxDisc, priced = e.list, e.unit, e.maxDisc, true
					pl.EntryID, pl.Note = e.id, "Price book: "+out.PriceBook
					if e.cost != nil {
						pl.UnitCost = e.cost
					}
					break
				}
			}
			if !priced && item.hasPrice && out.Currency == base {
				list, unit, priced = item.price, item.price, true
				pl.Note = "Catalog price"
			}
		}
		if strings.TrimSpace(l.in.Name) != "" && l.bundleOf == nil {
			pl.Name = strings.TrimSpace(l.in.Name)
		}
		if l.included {
			list, unit, priced = 0, 0, true
			pl.Note = "Included in the bundle"
		}
		// Price rules, then discount rules: the first that matches in priority order.
		if l.item != uuid.Nil && priced && !l.included {
			for _, kind := range []string{"price", "discount"} {
				for _, rule := range rules {
					if rule.Kind != kind || !matches(rule.Condition, item, l.qty) {
						continue
					}
					v, _ := parseCents(rule.Action.Value)
					switch rule.Action.Type {
					case "fixed":
						unit = v
					case "percent_of_list":
						unit = list.percentOf(int64(v))
					case "percent":
						unit -= unit.percentOf(int64(v))
					case "amount":
						unit -= v
					}
					if unit < 0 {
						unit = 0
					}
					pl.Note += " · " + rule.Name
					break
				}
			}
		}
		// A typed-in price.
		if typed, ok := parseCents(l.in.UnitPrice); ok && l.bundleOf == nil {
			switch {
			case typed < 0:
				fe[key] = "Enter a price of 0 or more."
			case !priced:
				list, unit, priced = typed, typed, true
				pl.Note = "Price typed in"
			case typed != unit && canOverride:
				unit = typed
				pl.Note += " · price overridden"
			case typed != unit:
				// Someone without the pricing permission sent a different price: the worked-out price stands.
			}
		}
		if !priced {
			fe[key] = pl.Name + " has no price in " + out.Currency + ". Add it to the price book, or type a price."
			continue
		}
		if list < unit {
			list = unit
		}
		// The line's own discount.
		manual, _ := bps(l.in.DiscountPercent)
		if l.bundleOf != nil {
			manual = 0
		}
		if manual < 0 || manual > 10000 {
			fe[key] = "Enter a discount from 0 to 100%."
			continue
		}
		if maxDisc != nil && manual > *maxDisc {
			fe[key] = pl.Name + ": the largest discount this price book allows is " + Cents(*maxDisc).String() + "%."
			continue
		}
		gross := list.timesQty(l.qty)
		net := unit.timesQty(l.qty)
		net -= net.percentOf(manual)
		pl.ListPrice, pl.UnitPrice, pl.DiscountPercent = list, unit, float64(manual)/100
		pl.Total, pl.DiscountAmount = net, gross-net
		pl.TaxAmount = net.percentOf(item.taxBps)
		if gross > 0 {
			pl.offListBps = int64(Cents(int64(pl.DiscountAmount)).mulRatio(10000, int64(gross)))
			if pl.offListBps > maxOff && !l.included {
				maxOff = pl.offListBps
			}
		}
		out.Lines = append(out.Lines, pl)
		out.Subtotal += gross
		out.Discount += pl.DiscountAmount
		out.Tax += pl.TaxAmount
		out.Total += net + pl.TaxAmount
	}
	if len(fe) > 0 {
		return nil, shared.Validation(fe)
	}
	out.MaxDiscountPercent = float64(maxOff) / 100
	out.Approval, err = h.approvalNeeded(ctx, q, ws, "discount", Cents(maxOff))
	return out, err
}

func (h *Handler) handlePricePreview(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !sc.Can("catalog_items", "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	var in PriceRequest
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	// The customer must be one the caller may see.
	if id, err := uuid.Parse(in.AccountID); err == nil {
		if _, _, err := h.getRow(r.Context(), h.store.Pool, sc.WS, specFor("accounts"), id, sc.OwnersFor("accounts", actor(r))); err != nil {
			shared.WriteError(w, r, shared.Validation(map[string]string{"accountId": "Choose an account of this business."}))
			return
		}
	}
	out, err := h.priceLines(r.Context(), h.store.Pool, sc.WS, in, canManagePricing(sc))
	respond(w, r, http.StatusOK, out, err)
}

// ---------------------------------------------------------------- lines of a document

type DocumentLines struct {
	Object   string       `json:"object"`
	ID       string       `json:"id"`
	Status   string       `json:"status"`
	Editable bool         `json:"editable"`
	Result   PriceResult  `json:"pricing"`
	Approval *ApprovalRow `json:"approvalRequest,omitempty"`
}

func (h *Handler) documentScope(r *http.Request, object, need string) (*Scope, *Row, error) {
	sc := scopeFrom(r.Context())
	spec := specFor(object)
	if spec == nil || !sc.Enabled(object) {
		return nil, nil, shared.NotFound("object_not_found")
	}
	if !sc.Can(object, need) {
		return nil, nil, errForbidden
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		return nil, nil, shared.NotFound("record_not_found")
	}
	row, _, err := h.getRow(r.Context(), h.store.Pool, sc.WS, spec, id, sc.OwnersFor(object, actor(r)))
	return sc, row, err
}

// storedLines reads the line items of a document as they were priced.
func (h *Handler) storedLines(ctx context.Context, q querier, ws uuid.UUID, object string, id uuid.UUID) (PriceResult, error) {
	p := lineParents[object]
	out := PriceResult{Lines: []PricedLine{}}
	rows, err := q.Query(ctx, `SELECT id::text, name, custom FROM crm.object_records
		WHERE workspace_id = $1 AND object_key = 'line_items' AND deleted_at IS NULL AND custom->>'`+p.field+`' = $2
		ORDER BY COALESCE(NULLIF(custom->>'sortOrder', ''), '0')::numeric, created_at, id`, ws, id.String())
	if err != nil {
		return out, err
	}
	defer rows.Close()
	index := map[string]int{}
	var parents []string
	for rows.Next() {
		var lid, name string
		var raw []byte
		if err := rows.Scan(&lid, &name, &raw); err != nil {
			return out, err
		}
		var v map[string]any
		_ = json.Unmarshal(raw, &v)
		str := func(k string) string { s, _ := v[k].(string); return s }
		qty, _ := milli(v["quantity"])
		disc, _ := bps(v["discountPercent"])
		tax, _ := bps(v["taxRate"])
		l := PricedLine{ItemID: str("itemId"), Name: name, Quantity: float64(qty) / 1000, ListPrice: mustCents(v["listPrice"]), UnitPrice: mustCents(v["unitPrice"]),
			DiscountPercent: float64(disc) / 100, DiscountAmount: mustCents(v["discountAmount"]), TaxRate: float64(tax) / 100, TaxAmount: mustCents(v["taxAmount"]),
			Total: mustCents(v["total"]), EntryID: str("priceBookEntryId"), Note: str("pricingNote"), StartDate: str("startDate"), EndDate: str("endDate"),
			BillingFrequency: str("billingFrequency")}
		if l.ListPrice == 0 {
			l.ListPrice = l.UnitPrice // lines made before list prices were kept
		}
		if c, ok := parseCents(v["unitCost"]); ok {
			l.UnitCost = &c
		}
		index[lid] = len(out.Lines)
		parents = append(parents, str("bundleLineId"))
		out.Lines = append(out.Lines, l)
		out.Subtotal += l.ListPrice.timesQty(qty)
		out.Discount += l.DiscountAmount
		out.Tax += l.TaxAmount
		out.Total += l.Total + l.TaxAmount
	}
	for i, parent := range parents {
		if j, ok := index[parent]; ok && parent != "" {
			k := j
			out.Lines[i].BundleOf = &k
		}
	}
	return out, rows.Err()
}

func (h *Handler) handleGetLines(object string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sc, doc, err := h.documentScope(r, object, "read")
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		ctx := r.Context()
		res, err := h.storedLines(ctx, h.store.Pool, sc.WS, object, doc.uuid())
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		res.PriceBookID, res.Currency = doc.text("priceBookId"), doc.text("currency")
		if res.Currency == "" {
			res.Currency = h.baseCurrency(ctx, h.store.Pool, sc.WS)
		}
		out := DocumentLines{Object: object, ID: doc.ID, Status: doc.text("status"), Result: res,
			Editable: lineParents[object].editable[doc.text("status")] && sc.Can(object, "update") && sc.Can("line_items", "create")}
		out.Approval, _ = h.latestApproval(ctx, h.store.Pool, sc.WS, object, doc.uuid(), "discount")
		shared.WriteJSON(w, http.StatusOK, out)
	}
}

// handlePutLines replaces the lines of a document with freshly priced ones and writes the
// totals onto the document — one transaction, the document row locked.
func (h *Handler) handlePutLines(object string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sc, doc, err := h.documentScope(r, object, "update")
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		if !sc.Can("line_items", "create") {
			shared.WriteError(w, r, errForbidden)
			return
		}
		var in PriceRequest
		if err := shared.DecodeJSON(w, r, &in); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		ctx := r.Context()
		a := actorFromRequest(r, "ui")
		err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
			_, err := h.writeLines(ctx, tx, sc, object, doc.uuid(), in, a, canManagePricing(sc))
			return err
		})
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		h.bus.Kick()
		h.handleGetLines(object)(w, r)
	}
}

// writeLines is the one place a document's lines and totals are written.
func (h *Handler) writeLines(ctx context.Context, tx pgx.Tx, sc *Scope, object string, id uuid.UUID, in PriceRequest, a actorInfo, canOverride bool) (*PriceResult, error) {
	p := lineParents[object]
	spec := specFor(object)
	// Lock the document: two people pricing it at once would otherwise both delete and both insert.
	var status string
	if err := tx.QueryRow(ctx, `SELECT COALESCE(status, '') FROM crm.object_records WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL FOR UPDATE`, id, sc.WS).Scan(&status); err != nil {
		return nil, shared.NotFound("record_not_found")
	}
	if !p.editable[status] {
		return nil, shared.NewError(http.StatusConflict, "document_locked", "The lines of this "+strings.ToLower(spec.Singular)+" can't be changed any more: it is no longer a draft. Its prices stay as they were agreed.")
	}
	doc, _, err := h.getRow(ctx, tx, sc.WS, spec, id, nil)
	if err != nil {
		return nil, err
	}
	if in.AccountID == "" {
		in.AccountID = doc.text("accountId")
	}
	if in.PriceBookID == "" {
		in.PriceBookID = doc.text("priceBookId")
	}
	if in.Currency == "" {
		in.Currency = doc.text("currency")
	}
	if in.Date == "" && p.date != "" {
		in.Date = doc.text(p.date)
	}
	res, err := h.priceLines(ctx, tx, sc.WS, in, canOverride)
	if err != nil {
		return nil, err
	}
	before, err := h.storedLines(ctx, tx, sc.WS, object, id)
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM crm.object_records WHERE workspace_id = $1 AND object_key = 'line_items' AND custom->>'`+p.field+`' = $2`, sc.WS, id.String()); err != nil {
		return nil, err
	}
	lineSpec := specFor("line_items")
	made := make([]string, len(res.Lines))
	for i, l := range res.Lines {
		v := map[string]any{"name": l.Name, p.field: id.String(), "quantity": l.Quantity, "listPrice": l.ListPrice.Float(), "unitPrice": l.UnitPrice.Float(),
			"discountPercent": l.DiscountPercent, "discountAmount": l.DiscountAmount.Float(), "taxRate": l.TaxRate, "taxAmount": l.TaxAmount.Float(),
			"total": l.Total.Float(), "pricingNote": l.Note, "sortOrder": float64(i + 1)}
		if l.ItemID != "" {
			v["itemId"] = l.ItemID
		}
		if res.PriceBookID != "" {
			v["priceBookId"] = res.PriceBookID
		}
		if l.EntryID != "" {
			v["priceBookEntryId"] = l.EntryID
		}
		if l.UnitCost != nil {
			v["unitCost"] = l.UnitCost.Float()
		}
		if l.BundleOf != nil {
			v["bundleLineId"] = made[*l.BundleOf]
		}
		for k, s := range map[string]string{"startDate": l.StartDate, "endDate": l.EndDate, "billingFrequency": l.BillingFrequency} {
			if s != "" {
				v[k] = s
			}
		}
		row, err := h.createRecord(ctx, tx, sc.WS, lineSpec, systemActor(sourcePricing), v)
		if err != nil {
			return nil, err
		}
		made[i] = row.ID
	}
	// The document's own numbers.
	set := map[string]any{"currency": res.Currency}
	if res.PriceBookID != "" {
		set["priceBookId"] = res.PriceBookID
	}
	if p.totals {
		set["subtotal"], set["discount"], set["tax"], set["total"] = res.Subtotal.Float(), res.Discount.Float(), res.Tax.Float(), res.Total.Float()
	} else {
		set[p.amount] = res.Total.Float()
	}
	if object == "quotes" {
		set["discountPercent"] = res.MaxDiscountPercent
		st, err := h.requestApproval(ctx, tx, sc, a, "discount", object, id, Cents(int64(res.MaxDiscountPercent*100+0.5)), res.Approval,
			"Discount of "+strconv.FormatFloat(res.MaxDiscountPercent, 'f', -1, 64)+"% on "+doc.Title)
		if err != nil {
			return nil, err
		}
		set["approvalStatus"] = st
	}
	// Only fields the object has.
	for k := range set {
		if _, ok := spec.field(k); !ok {
			delete(set, k)
		}
	}
	if _, err := h.updateValues(ctx, tx, sc.WS, spec, id, a, set, nil, nil); err != nil {
		return nil, err
	}
	if err := insertActivity(ctx, tx, sc.WS, object, id, "pricing.updated", "Priced: "+res.Total.String()+" "+res.Currency,
		map[string]any{"lines": len(res.Lines), "total": res.Total.String(), "discount": res.Discount.String(), "priceBook": res.PriceBook}, a.ID); err != nil {
		return nil, err
	}
	err = shared.WriteAudit(ctx, tx, a.audit(sc.WS, "document.priced", entityName(spec), &id,
		map[string]any{"total": before.Total.String(), "discount": before.Discount.String(), "lines": len(before.Lines)},
		map[string]any{"total": res.Total.String(), "discount": res.Discount.String(), "lines": len(res.Lines), "priceBook": res.PriceBookID, "currency": res.Currency,
			"maxDiscountPercent": res.MaxDiscountPercent}))
	return res, err
}

const sourcePricing = "pricing"

// lineItemSaved keeps the lines of a document that is no longer a draft exactly as agreed.
func (h *Handler) lineItemSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, op string, before, after *Row) error {
	rows := []*Row{after}
	if before != nil {
		rows = append(rows, before)
	}
	for _, row := range rows {
		for object, p := range lineParents {
			if object == "opportunities" || object == "work_orders" || object == "contracts" {
				continue // working documents: their lines are edited freely
			}
			id := row.id(p.field)
			if id == uuid.Nil {
				continue
			}
			var status string
			if err := tx.QueryRow(ctx, `SELECT COALESCE(status, '') FROM crm.object_records WHERE id = $1 AND workspace_id = $2 AND object_key = $3 AND deleted_at IS NULL`, id, ws, object).Scan(&status); err != nil {
				if errors.Is(err, pgx.ErrNoRows) {
					continue
				}
				return err
			}
			if !p.editable[status] {
				return shared.NewError(http.StatusConflict, "document_locked", "This line belongs to a "+strings.ToLower(specFor(object).Singular)+" that is no longer a draft. Its prices stay as they were agreed.")
			}
		}
	}
	return nil
}

// ---------------------------------------------------------------- quote → order → invoice

// documentName names the next document after its source: "Quote — Acme" becomes
// "Order — Acme", then "Invoice — Acme". A name that doesn't start with the kind is kept.
func documentName(title, from, toSingular string) string {
	words := map[string][]string{"quotes": {"quotation", "quote"}, "sales_orders": {"sales order", "order"}, "work_orders": {"work order"}}[from]
	lower := strings.ToLower(title)
	for _, w := range words {
		if strings.HasPrefix(lower, w) && (len(title) == len(w) || !isLetter(title[len(w)])) {
			to := map[string]string{"Sales order": "Order"}[toSingular]
			if to == "" {
				to = toSingular
			}
			return to + title[len(w):]
		}
	}
	return title
}

func isLetter(b byte) bool { return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') }

// copyDocument makes the next document of the chain from a source: header fields the two
// share, and the lines exactly as priced (no price is looked up again).
func (h *Handler) copyDocument(ctx context.Context, tx pgx.Tx, sc *Scope, a actorInfo, from string, src *Row, to string, extra map[string]any) (*Row, error) {
	toSpec := specFor(to)
	v := map[string]any{}
	for _, k := range []string{"accountId", "contactId", "opportunityId", "priceBookId", "currency", "exchangeRate", "subtotal", "discount", "tax", "total", "contractId", "description", "terms"} {
		if _, ok := toSpec.field(k); ok && src.Values[k] != nil && src.Values[k] != "" {
			v[k] = src.Values[k]
		}
	}
	v["name"] = documentName(src.Title, from, toSpec.Singular)
	for k, x := range extra {
		v[k] = x
	}
	row, err := h.createRecord(ctx, tx, sc.WS, toSpec, a, v)
	if err != nil {
		return nil, err
	}
	if err := h.copyLines(ctx, tx, sc.WS, lineParents[from].field, src.ID, lineParents[to].field, row.ID); err != nil {
		return nil, err
	}
	return row, nil
}

// copyLines copies the line items of one document onto another exactly as priced.
func (h *Handler) copyLines(ctx context.Context, tx pgx.Tx, ws uuid.UUID, fromField, srcID, toField, dstID string) error {
	rows, err := tx.Query(ctx, `SELECT id::text, name, custom FROM crm.object_records
		WHERE workspace_id = $1 AND object_key = 'line_items' AND deleted_at IS NULL AND custom->>'`+fromField+`' = $2
		ORDER BY COALESCE(NULLIF(custom->>'sortOrder', ''), '0')::numeric, created_at, id`, ws, srcID)
	if err != nil {
		return err
	}
	type line struct {
		id, name string
		v        map[string]any
	}
	var lines []line
	for rows.Next() {
		var l line
		var raw []byte
		if err := rows.Scan(&l.id, &l.name, &raw); err != nil {
			rows.Close()
			return err
		}
		_ = json.Unmarshal(raw, &l.v)
		lines = append(lines, l)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	newID := map[string]string{}
	for _, l := range lines {
		nv := map[string]any{"name": l.name, toField: dstID}
		for k, x := range l.v {
			switch k {
			case "quoteId", "orderId", "invoiceId", "contractId", "workOrderId", "opportunityId", "creditNoteId":
			case "bundleLineId":
				if s, _ := x.(string); newID[s] != "" {
					nv[k] = newID[s]
				}
			default:
				nv[k] = x
			}
		}
		made, err := h.createRecord(ctx, tx, ws, specFor("line_items"), systemActor(sourcePricing), nv)
		if err != nil {
			return err
		}
		newID[l.id] = made.ID
	}
	return nil
}

func (h *Handler) handleConvertDocument(from, to string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sc, doc, err := h.documentScope(r, from, "update")
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		if !sc.Can(to, "create") {
			shared.WriteError(w, r, errForbidden)
			return
		}
		ctx := r.Context()
		a := actorFromRequest(r, "ui")
		today := time.Now().Format("2006-01-02")
		var out *Row
		err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
			var status string
			if err := tx.QueryRow(ctx, `SELECT COALESCE(status, '') FROM crm.object_records WHERE id = $1 AND workspace_id = $2 FOR UPDATE`, doc.uuid(), sc.WS).Scan(&status); err != nil {
				return err
			}
			link := lineParents[from].field // quoteId on the order, orderId on the invoice
			var already string
			_ = tx.QueryRow(ctx, `SELECT code FROM crm.object_records WHERE workspace_id = $1 AND object_key = $2 AND deleted_at IS NULL AND custom->>'`+link+`' = $3 AND COALESCE(status, '') NOT IN ('cancelled', 'void') LIMIT 1`,
				sc.WS, to, doc.ID).Scan(&already)
			if already != "" {
				return shared.NewError(http.StatusConflict, "already_converted", "This was already turned into "+already+".")
			}
			extra := map[string]any{link: doc.ID, "status": "draft"}
			if from == "quotes" {
				if status == "declined" || status == "expired" {
					return shared.NewError(http.StatusUnprocessableEntity, "not_convertible", "A declined or expired quote can't become an order. Revise it first.")
				}
				if st := doc.text("approvalStatus"); st == "pending" || st == "rejected" {
					return shared.NewError(http.StatusUnprocessableEntity, "approval_required", "The discount on this quote has not been approved.")
				}
				extra["orderDate"] = today
			} else {
				if status == "cancelled" {
					return shared.NewError(http.StatusUnprocessableEntity, "not_convertible", "A cancelled order can't be invoiced.")
				}
				extra["invoiceDate"] = today
				if q := doc.text("quoteId"); q != "" {
					extra["quoteId"] = q
				}
			}
			var err error
			if out, err = h.copyDocument(ctx, tx, sc, a, from, doc, to, extra); err != nil {
				return err
			}
			next := "confirmed"
			if from == "quotes" {
				next = "accepted"
			}
			if status == "" || status == "draft" || status == "sent" {
				if _, err := h.updateValues(ctx, tx, sc.WS, specFor(from), doc.uuid(), a, map[string]any{"status": next}, nil, nil); err != nil {
					return err
				}
			}
			id := doc.uuid()
			return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "document.converted", entityName(specFor(from)), &id, nil, map[string]any{"to": to, "id": out.ID, "code": out.Code}))
		})
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		h.bus.Kick()
		shared.WriteJSON(w, http.StatusCreated, map[string]any{"object": to, "id": out.ID, "code": out.Code})
	}
}

// handleReviseQuote makes the next version of a quote: a new draft with the same lines; the
// old one stays as it was sent and is marked expired.
func (h *Handler) handleReviseQuote(w http.ResponseWriter, r *http.Request) {
	sc, doc, err := h.documentScope(r, "quotes", "update")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if !sc.Can("quotes", "create") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	ctx := r.Context()
	a := actorFromRequest(r, "ui")
	var out *Row
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		version := 1
		if f, err := strconv.ParseFloat(numText(doc.Values["quoteVersion"]), 64); err == nil && f >= 1 {
			version = int(f)
		}
		extra := map[string]any{"status": "draft", "quoteVersion": float64(version + 1), "previousQuoteId": doc.ID, "quoteDate": time.Now().Format("2006-01-02"), "approvalStatus": "not_required"}
		if v := doc.text("validUntil"); v != "" {
			extra["validUntil"] = v
		}
		var err error
		if out, err = h.copyDocument(ctx, tx, sc, a, "quotes", doc, "quotes", extra); err != nil {
			return err
		}
		set := map[string]any{}
		if doc.Values["quoteVersion"] == nil {
			set["quoteVersion"] = float64(version)
		}
		if st := doc.text("status"); st == "draft" || st == "sent" || st == "" {
			set["status"] = "expired"
		}
		if len(set) > 0 {
			if _, err := h.updateValues(ctx, tx, sc.WS, specFor("quotes"), doc.uuid(), systemActor(sourcePricing), set, nil, nil); err != nil {
				return err
			}
		}
		// The new version is priced as the old one was; if that discount needs approval, it is asked for again.
		lines, err := h.storedLines(ctx, tx, sc.WS, "quotes", out.uuid())
		if err != nil {
			return err
		}
		var maxOff int64
		for _, l := range lines.Lines {
			if g := l.ListPrice.timesQty(int64(l.Quantity*1000 + 0.5)); g > 0 {
				if off := int64(l.DiscountAmount.mulRatio(10000, int64(g))); off > maxOff {
					maxOff = off
				}
			}
		}
		need, err := h.approvalNeeded(ctx, tx, sc.WS, "discount", Cents(maxOff))
		if err != nil {
			return err
		}
		st, err := h.requestApproval(ctx, tx, sc, a, "discount", "quotes", out.uuid(), Cents(maxOff), need, "Discount on "+out.Title)
		if err != nil {
			return err
		}
		_, err = h.updateValues(ctx, tx, sc.WS, specFor("quotes"), out.uuid(), systemActor(sourcePricing), map[string]any{"approvalStatus": st, "discountPercent": float64(maxOff) / 100}, nil, nil)
		return err
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.bus.Kick()
	shared.WriteJSON(w, http.StatusCreated, map[string]any{"object": "quotes", "id": out.ID, "code": out.Code})
}

// quoteSaved: a quote whose discount is waiting for approval (or was refused) can't go out.
func (h *Handler) quoteSaved(before, after *Row) error {
	if before == nil || before.text("status") == after.text("status") {
		return nil
	}
	if s := after.text("status"); s == "sent" || s == "accepted" {
		switch after.text("approvalStatus") {
		case "pending":
			return shared.NewError(http.StatusUnprocessableEntity, "approval_required", "The discount on this quote is waiting for approval. It can be sent once it is approved.")
		case "rejected":
			return shared.NewError(http.StatusUnprocessableEntity, "approval_required", "The discount on this quote was not approved. Lower it, then send the quote.")
		}
	}
	return nil
}

func sortedKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
