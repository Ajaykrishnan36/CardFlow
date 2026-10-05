package e2e

import (
	"context"
	"fmt"
	"testing"
	"time"
)

func today() string { return time.Now().Format("2006-01-02") }

func staffOf(t *testing.T, boss, ws, name string) (token, identity string) {
	t.Helper()
	phone := freshPhone()
	want(t, call(t, "POST", crmAPI+"/w/"+ws+"/admin/members", boss, map[string]any{"displayName": name, "phone": phone, "roleKey": "STAFF"}), 201, "add "+name)
	token = signIn(t, phone, name)
	return token, call(t, "GET", crmAPI+"/me", token, nil).str("identity", "id")
}

func entry(t *testing.T, token, ws, book, item string, price float64, more map[string]any) resp {
	t.Helper()
	body := map[string]any{"priceBookId": book, "itemId": item, "unitPrice": price, "listPrice": price, "validFrom": "2026-01-01"}
	for k, v := range more {
		body[k] = v
	}
	return call(t, "POST", crmAPI+"/w/"+ws+"/price-book-entries", token, body)
}

// Price book entries, bundles, pricing and discount rules, approval, and the chain
// quote → order → invoice with prices that stay as agreed (D-119…D-121).
func TestPricingFromPriceBookToInvoice(t *testing.T) {
	boss := signIn(t, freshPhone(), "Sales Ops")
	ws := newBusiness(t, boss, "Pricing Co")
	base := crmAPI + "/w/" + ws
	a := create(t, boss, ws, "catalog_items", map[string]any{"name": "Product A", "unitPrice": 1111, "taxRate": 18, "family": "Core"})
	b := create(t, boss, ws, "catalog_items", map[string]any{"name": "Product B", "unitPrice": 2222})
	book := create(t, boss, ws, "price_books", map[string]any{"name": "Standard 2026", "isDefault": true, "status": "active"})
	acc := create(t, boss, ws, "accounts", map[string]any{"name": "Buyer", "type": "customer"})

	// Entries: the selling price lives in the book, not on the product.
	want(t, entry(t, boss, ws, book, a, 1000, map[string]any{"maxDiscountPercent": 30}), 200, "entry A")
	eb := entry(t, boss, ws, book, b, 2000, nil)
	want(t, eb, 200, "entry B")
	want(t, entry(t, boss, ws, book, a, 900, map[string]any{"minQuantity": 10}), 200, "volume price for A")
	want(t, entry(t, boss, ws, book, a, 950, nil), 422, "a second price for the same item, quantity and date")
	list := call(t, "GET", base+"/price-book-entries?priceBookId="+book, boss, nil)
	if len(list.list("data")) != 3 {
		t.Fatalf("entries of the book: %s", truncate(list.Raw, 300))
	}

	price := func(token string, body map[string]any) resp {
		return call(t, "POST", base+"/pricing/preview", token, body)
	}
	p := price(boss, map[string]any{"accountId": acc, "lines": []map[string]any{{"itemId": a, "quantity": 1}, {"itemId": b, "quantity": 1}}})
	want(t, p, 200, "price two items")
	// A: 1,000 + 18% tax = 1,180; B: 2,000. The default price book was found without being named.
	if num(t, p, "subtotal") != 3000 || num(t, p, "tax") != 180 || num(t, p, "total") != 3180 || p.str("priceBookId") != book {
		t.Fatalf("list pricing: %s", truncate(p.Raw, 500))
	}
	// Quantity break: 10 of A at 900.
	p = price(boss, map[string]any{"lines": []map[string]any{{"itemId": a, "quantity": 10}}})
	if l := p.list("lines")[0].(map[string]any); l["unitPrice"] != float64(900) || l["total"] != float64(9000) {
		t.Fatalf("volume price: %v", l)
	}
	// A discount rule: A and B together → 10% off both. 900 + 1,800 = 2,700 exactly.
	rule := call(t, "POST", base+"/pricing/rules", boss, map[string]any{"name": "A+B bundle discount", "kind": "discount", "priority": 10,
		"condition": map[string]any{"itemIds": []string{a, b}, "withItemIds": []string{a, b}}, "action": map[string]any{"type": "percent", "value": 10}})
	want(t, rule, 200, "discount rule")
	p = price(boss, map[string]any{"lines": []map[string]any{{"itemId": a, "quantity": 1}, {"itemId": b, "quantity": 1}}})
	if num(t, p, "subtotal") != 3000 || num(t, p, "discount") != 300 || num(t, p, "total") != 2700+162 {
		t.Fatalf("bundle discount must be exactly 300: %s", truncate(p.Raw, 500))
	}
	// A alone is not discounted.
	if p = price(boss, map[string]any{"lines": []map[string]any{{"itemId": a, "quantity": 1}}}); num(t, p, "discount") != 0 {
		t.Fatalf("the rule needs both items: %s", truncate(p.Raw, 300))
	}
	want(t, price(boss, map[string]any{"lines": []map[string]any{{"itemId": a, "quantity": 1, "discountPercent": 31}}}), 422, "more than the entry's largest discount")
	want(t, price(boss, map[string]any{"lines": []map[string]any{{"itemId": a, "quantity": 0}}}), 422, "zero quantity")

	// Rounding: 3 × 333.33 less 7.5% is done in whole paise.
	c := create(t, boss, ws, "catalog_items", map[string]any{"name": "Odd price"})
	want(t, entry(t, boss, ws, book, c, 333.33, nil), 200, "entry C")
	p = price(boss, map[string]any{"lines": []map[string]any{{"itemId": c, "quantity": 3, "discountPercent": 7.5}}})
	if l := p.list("lines")[0].(map[string]any); l["total"] != 924.99 || l["discountAmount"] != 75.00 {
		t.Fatalf("999.99 less 7.5%% = 924.99 (75.00 off): %v", l)
	}

	// Configuration and eligibility rules.
	want(t, call(t, "POST", base+"/pricing/rules", boss, map[string]any{"name": "C needs A", "kind": "configuration",
		"condition": map[string]any{"itemIds": []string{c}}, "action": map[string]any{"requires": []string{a}}}), 200, "configuration rule")
	want(t, price(boss, map[string]any{"lines": []map[string]any{{"itemId": c, "quantity": 1}}}), 422, "C without A")
	want(t, price(boss, map[string]any{"lines": []map[string]any{{"itemId": c, "quantity": 1}, {"itemId": a, "quantity": 1}}}), 200, "C with A")
	want(t, call(t, "POST", base+"/pricing/rules", boss, map[string]any{"name": "B for partners only", "kind": "eligibility",
		"condition": map[string]any{"itemIds": []string{b}}, "action": map[string]any{"accountTypes": []string{"partner"}}}), 200, "eligibility rule")
	want(t, price(boss, map[string]any{"accountId": acc, "lines": []map[string]any{{"itemId": b, "quantity": 1}}}), 422, "B for a customer that isn't a partner")
	rules := call(t, "GET", base+"/pricing/rules", boss, nil)
	for _, x := range rules.list("data") {
		if m := x.(map[string]any); m["kind"] == "eligibility" {
			want(t, call(t, "DELETE", base+"/pricing/rules/"+m["id"].(string), boss, nil), 204, "remove the eligibility rule")
		}
	}

	// A bundle: A is part of it at no extra price, B is an option at its own price.
	bundle := create(t, boss, ws, "catalog_items", map[string]any{"name": "Starter bundle"})
	want(t, entry(t, boss, ws, book, bundle, 5000, nil), 200, "bundle price")
	want(t, call(t, "PUT", base+"/catalog_items/"+bundle+"/bundle", boss, map[string]any{"components": []map[string]any{
		{"itemId": a, "required": true, "quantity": 2, "priceMode": "included"}, {"itemId": b, "required": false, "quantity": 1, "priceMode": "additional", "maxQuantity": "3"}}}), 200, "bundle components")
	want(t, call(t, "PUT", base+"/catalog_items/"+a+"/bundle", boss, map[string]any{"components": []map[string]any{{"itemId": bundle, "quantity": 1}}}), 422, "a bundle inside its own component")
	p = price(boss, map[string]any{"lines": []map[string]any{{"itemId": bundle, "quantity": 2}}})
	want(t, p, 200, "bundle without options")
	if len(p.list("lines")) != 2 || num(t, p, "subtotal") != 10000 || p.list("lines")[1].(map[string]any)["quantity"] != float64(4) {
		t.Fatalf("2 bundles = 10,000 with 4 of A included: %s", truncate(p.Raw, 500))
	}
	p = price(boss, map[string]any{"lines": []map[string]any{{"itemId": bundle, "quantity": 1, "options": []map[string]any{{"itemId": b, "quantity": 2}}}}})
	if len(p.list("lines")) != 3 || num(t, p, "subtotal") != 9000 {
		t.Fatalf("bundle 5,000 + 2 × B at 2,000: %s", truncate(p.Raw, 500))
	}
	want(t, price(boss, map[string]any{"lines": []map[string]any{{"itemId": bundle, "quantity": 1, "options": []map[string]any{{"itemId": b, "quantity": 4}}}}}), 422, "more of an option than allowed")
	want(t, price(boss, map[string]any{"lines": []map[string]any{{"itemId": bundle, "quantity": 1, "options": []map[string]any{{"itemId": c}}}}}), 422, "an option that isn't in the bundle")

	// ---- a quote ----
	want(t, call(t, "PUT", base+"/approvals/rules", boss, map[string]any{"kind": "discount", "rules": []map[string]any{{"minValue": 15, "approverRole": "ADMIN", "label": "Sales manager"}}}), 200, "approval limit")
	quote := create(t, boss, ws, "quotes", map[string]any{"name": "Quote for Buyer", "accountId": acc, "status": "draft"})
	put := call(t, "PUT", base+"/quotes/"+quote+"/lines", boss, map[string]any{"lines": []map[string]any{{"itemId": a, "quantity": 2}, {"itemId": b, "quantity": 1}}})
	want(t, put, 200, "price the quote")
	qv := get(t, boss, ws, "quotes", quote)
	// A 2 × 1,000 and B 2,000, each 10% off: 3,600; tax 18% on A's 1,800 = 324.
	if qv["subtotal"] != float64(4000) || qv["discount"] != float64(400) || qv["tax"] != float64(324) || qv["total"] != float64(3924) || qv["approvalStatus"] != "not_required" || qv["priceBookId"] != book {
		t.Fatalf("quote totals: %v", qv)
	}
	if n := total(t, boss, ws, "line_items", "?filter="+fmt.Sprintf(`{"op":"and","filters":[{"field":"quoteId","op":"eq","value":"%s"}]}`, quote)); n != 2 {
		t.Fatalf("the quote should have 2 line items, has %d", n)
	}
	// A 20% line discount needs approval; the quote can't go out until it is given.
	put = call(t, "PUT", base+"/quotes/"+quote+"/lines", boss, map[string]any{"lines": []map[string]any{{"itemId": a, "quantity": 2, "discountPercent": 20}, {"itemId": b, "quantity": 1}}})
	want(t, put, 200, "discount the quote")
	if put.str("approvalRequest", "status") != "pending" || get(t, boss, ws, "quotes", quote)["approvalStatus"] != "pending" {
		t.Fatalf("a 28%% discount should wait for approval: %s", truncate(put.Raw, 400))
	}
	want(t, edit(t, boss, ws, "quotes", quote, map[string]any{"status": "sent"}), 422, "send a quote that waits for approval")
	want(t, call(t, "POST", base+"/quotes/"+quote+"/convert", boss, nil), 422, "convert a quote that waits for approval")
	rep, _ := staffOf(t, boss, ws, "Rep")
	inbox := call(t, "GET", base+"/approvals", boss, nil)
	want(t, inbox, 200, "approval inbox")
	reqID := inbox.list("data")[0].(map[string]any)["id"].(string)
	want(t, call(t, "POST", base+"/approvals/"+reqID+"/decide", rep, map[string]any{"status": "approved"}), 403, "staff approving a discount")
	if len(call(t, "GET", base+"/approvals", rep, nil).list("data")) != 0 {
		t.Fatalf("staff must not see requests they neither made nor can decide")
	}
	want(t, call(t, "POST", base+"/approvals/"+reqID+"/decide", boss, map[string]any{"status": "approved", "note": "Strategic account"}), 200, "approve")
	want(t, call(t, "POST", base+"/approvals/"+reqID+"/decide", boss, map[string]any{"status": "rejected"}), 409, "decide twice")
	if get(t, boss, ws, "quotes", quote)["approvalStatus"] != "approved" {
		t.Fatalf("the quote should be approved")
	}
	agreed := get(t, boss, ws, "quotes", quote)["total"]

	// ---- the price book changes; what was quoted does not ----
	want(t, call(t, "PATCH", base+"/price-book-entries/"+eb.str("id"), boss, map[string]any{"unitPrice": 2500, "listPrice": 2500}), 200, "raise B's price")
	order := call(t, "POST", base+"/quotes/"+quote+"/convert", boss, nil)
	want(t, order, 201, "quote → order")
	want(t, call(t, "POST", base+"/quotes/"+quote+"/convert", boss, nil), 409, "convert twice")
	ov := get(t, boss, ws, "sales_orders", order.str("id"))
	if ov["total"] != agreed || ov["quoteId"] != quote || get(t, boss, ws, "quotes", quote)["status"] != "accepted" {
		t.Fatalf("the order must carry the quote's agreed total %v: %v", agreed, ov)
	}
	ol := call(t, "GET", base+"/sales_orders/"+order.str("id")+"/lines", boss, nil)
	if len(ol.list("pricing", "lines")) != 2 || ol.list("pricing", "lines")[1].(map[string]any)["unitPrice"] != float64(1800) {
		t.Fatalf("order lines must be the quoted prices, not today's: %s", truncate(ol.Raw, 500))
	}
	want(t, call(t, "PUT", base+"/quotes/"+quote+"/lines", boss, map[string]any{"lines": []map[string]any{{"itemId": a, "quantity": 1}}}), 409, "reprice an accepted quote")
	inv := call(t, "POST", base+"/sales_orders/"+order.str("id")+"/invoice", boss, nil)
	want(t, inv, 201, "order → invoice")
	invID := inv.str("id")
	iv := get(t, boss, ws, "invoices", invID)
	if iv["total"] != agreed || iv["orderId"] != order.str("id") || iv["quoteId"] != quote || iv["balanceDue"] != agreed {
		t.Fatalf("the invoice must carry the agreed total: %v", iv)
	}
	// Once issued, the invoice's lines are frozen — through the pricing API and through the records API.
	want(t, edit(t, boss, ws, "invoices", invID, map[string]any{"status": "sent"}), 200, "issue the invoice")
	if get(t, boss, ws, "invoices", invID)["finalizedAt"] == nil {
		t.Fatalf("an issued invoice records when it was finalized")
	}
	want(t, call(t, "PUT", base+"/invoices/"+invID+"/lines", boss, map[string]any{"lines": []map[string]any{{"itemId": a, "quantity": 1}}}), 409, "reprice an issued invoice")
	il := call(t, "GET", crmAPI+"/w/"+ws+"/crm/line_items?filter="+fmt.Sprintf(`{"op":"and","filters":[{"field":"invoiceId","op":"eq","value":"%s"}]}`, invID), boss, nil)
	line := il.list("data")[0].(map[string]any)
	want(t, call(t, "PATCH", base+"/crm/line_items/"+line["id"].(string), boss, map[string]any{"values": map[string]any{"unitPrice": 1}, "expectedVersion": line["version"]}), 409, "edit a line of an issued invoice")
	want(t, call(t, "DELETE", base+"/crm/line_items/"+line["id"].(string), boss, nil), 409, "delete a line of an issued invoice")
	// A new quote today gets today's price for B.
	p = price(boss, map[string]any{"lines": []map[string]any{{"itemId": b, "quantity": 1}}})
	if num(t, p, "total") != 2500 {
		t.Fatalf("a new quote uses the new price: %s", truncate(p.Raw, 300))
	}
	// Revising makes version 2 and keeps version 1 as it was.
	q2 := create(t, boss, ws, "quotes", map[string]any{"name": "Second quote", "accountId": acc, "status": "draft"})
	want(t, call(t, "PUT", base+"/quotes/"+q2+"/lines", boss, map[string]any{"lines": []map[string]any{{"itemId": a, "quantity": 1}}}), 200, "price")
	rev := call(t, "POST", base+"/quotes/"+q2+"/revise", boss, nil)
	want(t, rev, 201, "revise")
	if v := get(t, boss, ws, "quotes", rev.str("id")); v["quoteVersion"] != float64(2) || v["previousQuoteId"] != q2 || v["total"] != float64(1180) {
		t.Fatalf("revision: %v", v)
	}
	if get(t, boss, ws, "quotes", q2)["status"] != "expired" {
		t.Fatalf("the revised quote is superseded")
	}

	// ---- permissions and tenant isolation ----
	want(t, entry(t, rep, ws, book, c, 1, map[string]any{"minQuantity": 99}), 403, "staff changing a price")
	want(t, call(t, "POST", base+"/pricing/rules", rep, map[string]any{"name": "x", "kind": "discount", "action": map[string]any{"type": "percent", "value": 50}}), 403, "staff adding a rule")
	// Staff typing their own price don't get it: the worked-out price stands.
	if p = price(rep, map[string]any{"lines": []map[string]any{{"itemId": c, "quantity": 1, "unitPrice": 1}, {"itemId": a, "quantity": 1}}}); p.list("lines")[0].(map[string]any)["unitPrice"] != 333.33 {
		t.Fatalf("a price typed without the pricing permission must be ignored: %s", truncate(p.Raw, 300))
	}
	other := signIn(t, freshPhone(), "Rival")
	otherWS := newBusiness(t, other, "Rival Pricing")
	ob := crmAPI + "/w/" + otherWS
	otherItem := create(t, other, otherWS, "catalog_items", map[string]any{"name": "Theirs", "unitPrice": 5})
	otherBook := create(t, other, otherWS, "price_books", map[string]any{"name": "Their book"})
	want(t, call(t, "GET", base+"/price-book-entries", other, nil), 403, "outsider listing prices")
	if len(call(t, "GET", ob+"/price-book-entries", other, nil).list("data")) != 0 {
		t.Fatalf("price book entries leaked across businesses")
	}
	want(t, entry(t, other, otherWS, book, otherItem, 1, nil), 422, "an entry in another business's price book")
	want(t, entry(t, other, otherWS, otherBook, a, 1, nil), 422, "an entry for another business's item")
	want(t, call(t, "POST", ob+"/pricing/preview", other, map[string]any{"priceBookId": book, "lines": []map[string]any{{"itemId": otherItem, "quantity": 1}}}), 422, "pricing from another business's book")
	want(t, call(t, "POST", ob+"/pricing/preview", other, map[string]any{"lines": []map[string]any{{"itemId": a, "quantity": 1}}}), 422, "pricing another business's item")
	want(t, call(t, "PUT", ob+"/quotes/"+quote+"/lines", other, map[string]any{"lines": []map[string]any{}}), 404, "another business's quote")
	want(t, call(t, "POST", ob+"/approvals/"+reqID+"/decide", other, map[string]any{"status": "approved"}), 404, "another business's approval")
	want(t, call(t, "POST", ob+"/pricing/rules", other, map[string]any{"name": "x", "kind": "discount", "condition": map[string]any{"itemIds": []string{a}}, "action": map[string]any{"type": "percent", "value": 5}}), 422, "a rule naming another business's item")
	if len(call(t, "GET", ob+"/pricing/rules", other, nil).list("data")) != 0 {
		t.Fatalf("pricing rules leaked across businesses")
	}
	// Every price change is in the audit log.
	var audits int
	_ = testDB.Pool.QueryRow(context.Background(), `SELECT count(*) FROM crm.audit_events a JOIN crm.workspaces w ON w.id = a.workspace_id
		WHERE w.code = $1 AND a.action IN ('price_book_entry.saved', 'document.priced', 'approval.approved', 'document.converted', 'pricing_rule.saved')`, ws).Scan(&audits)
	if audits < 12 {
		t.Fatalf("pricing changes must be audited, found %d events", audits)
	}
}

// A record in another currency keeps the rate it was made at (D-118).
func TestMultiCurrencyKeepsHistoricalRates(t *testing.T) {
	boss := signIn(t, freshPhone(), "Exporter")
	ws := newBusiness(t, boss, "Export House")
	base := crmAPI + "/w/" + ws
	cur := call(t, "GET", base+"/currencies", boss, nil)
	want(t, cur, 200, "currencies")
	if cur.str("base") != "INR" || len(cur.list("data")) < 10 {
		t.Fatalf("currency list: %s", truncate(cur.Raw, 200))
	}
	want(t, call(t, "PUT", base+"/exchange-rates", boss, map[string]any{"fromCurrency": "USD", "rate": 83, "effectiveFrom": "2026-01-01"}), 200, "USD rate")
	want(t, call(t, "PUT", base+"/exchange-rates", boss, map[string]any{"fromCurrency": "INR", "rate": 83}), 422, "a rate from the base currency to itself")
	want(t, call(t, "PUT", base+"/exchange-rates", boss, map[string]any{"fromCurrency": "XXX", "rate": 2}), 422, "an unknown currency")

	deal := create(t, boss, ws, "opportunities", map[string]any{"name": "US deal", "amount": 1000, "currency": "USD", "closeDate": today(), "status": "closed_won"})
	v := get(t, boss, ws, "opportunities", deal)
	if v["exchangeRate"] != float64(83) || v["baseAmount"] != float64(83000) {
		t.Fatalf("1,000 USD at 83 = 83,000 INR: %v", v)
	}
	local := create(t, boss, ws, "opportunities", map[string]any{"name": "Local deal", "amount": 5000, "closeDate": today(), "status": "closed_won"})
	if lv := get(t, boss, ws, "opportunities", local); lv["currency"] != "INR" || lv["baseAmount"] != float64(5000) || lv["exchangeRate"] != float64(1) {
		t.Fatalf("a deal without a currency is in the base currency: %v", lv)
	}
	// The rate changes from today: the old deal keeps 83, even when its amount is edited.
	want(t, call(t, "PUT", base+"/exchange-rates", boss, map[string]any{"fromCurrency": "USD", "rate": 90, "effectiveFrom": today()}), 200, "new USD rate")
	want(t, edit(t, boss, ws, "opportunities", deal, map[string]any{"amount": 2000}), 200, "change the amount")
	if v = get(t, boss, ws, "opportunities", deal); v["exchangeRate"] != float64(83) || v["baseAmount"] != float64(166000) {
		t.Fatalf("a record keeps the rate it was made at: %v", v)
	}
	fresh := create(t, boss, ws, "opportunities", map[string]any{"name": "New US deal", "amount": 100, "currency": "usd", "closeDate": today(), "status": "closed_won"})
	if nv := get(t, boss, ws, "opportunities", fresh); nv["exchangeRate"] != float64(90) || nv["baseAmount"] != float64(9000) || nv["currency"] != "USD" {
		t.Fatalf("a new record takes today's rate: %v", nv)
	}
	r := call(t, "POST", base+"/crm/opportunities", boss, map[string]any{"values": map[string]any{"name": "Euro deal", "amount": 10, "currency": "EUR", "closeDate": today()}})
	want(t, r, 422, "a currency without a rate")
	// A rate typed on the record wins over the table.
	typed := create(t, boss, ws, "invoices", map[string]any{"name": "Euro invoice", "total": 100, "currency": "EUR", "exchangeRate": 91.5, "status": "sent"})
	if tv := get(t, boss, ws, "invoices", typed); tv["baseTotal"] != float64(9150) {
		t.Fatalf("typed rate: %v", tv)
	}
	// The forecast adds everything up in the base currency: 166,000 + 5,000 + 9,000.
	periods := call(t, "GET", base+"/forecast/periods", boss, nil)
	f := call(t, "GET", base+"/forecast?period="+periods.str("current", "month"), boss, nil)
	if num(t, f, "totals", "closed") != 180000 {
		t.Fatalf("forecast in base currency = %v, want 180000", num(t, f, "totals", "closed"))
	}
	conv := call(t, "GET", base+"/exchange-rates/convert?from=USD&amount=10&date=2026-03-01", boss, nil)
	if num(t, conv, "converted") != 830 {
		t.Fatalf("10 USD on 1 March at the March rate = 830: %s", conv.Raw)
	}
	// Rates belong to one business.
	other := signIn(t, freshPhone(), "Other Exporter")
	otherWS := newBusiness(t, other, "Other Export")
	if len(call(t, "GET", crmAPI+"/w/"+otherWS+"/exchange-rates", other, nil).list("data")) != 0 {
		t.Fatalf("exchange rates leaked across businesses")
	}
	want(t, call(t, "POST", crmAPI+"/w/"+otherWS+"/crm/opportunities", other, map[string]any{"values": map[string]any{"name": "x", "amount": 1, "currency": "USD", "closeDate": today()}}), 422, "another business's rate is not used")
	rate := call(t, "GET", base+"/exchange-rates", boss, nil).list("data")[0].(map[string]any)["id"].(string)
	want(t, call(t, "DELETE", crmAPI+"/w/"+otherWS+"/exchange-rates/"+rate, other, nil), 404, "deleting another business's rate")
	rep, _ := staffOf(t, boss, ws, "Clerk")
	want(t, call(t, "PUT", base+"/exchange-rates", rep, map[string]any{"fromCurrency": "GBP", "rate": 100}), 403, "staff setting a rate")
}

// Refunds, credit notes, debit notes and write-offs are records; the invoice's balance is
// worked out from them and always adds up (D-126).
func TestFinanceDocumentsKeepTheInvoiceBalanced(t *testing.T) {
	ctx := context.Background()
	boss := signIn(t, freshPhone(), "Controller")
	ws := newBusiness(t, boss, "Books Ltd")
	base := crmAPI + "/w/" + ws
	acc := create(t, boss, ws, "accounts", map[string]any{"name": "Debtor"})
	inv := create(t, boss, ws, "invoices", map[string]any{"name": "Invoice", "total": 100000, "status": "sent", "accountId": acc})
	pay := func(amount float64, invoice string) string {
		v := map[string]any{"name": "Payment", "amount": amount, "paymentDate": today(), "status": "paid"}
		if invoice != "" {
			v["invoiceId"] = invoice
		}
		return create(t, boss, ws, "payments", v)
	}
	p1 := pay(60000, inv)
	pay(40000, inv)
	if v := get(t, boss, ws, "invoices", inv); v["status"] != "paid" || v["balanceDue"] != float64(0) {
		t.Fatalf("60,000 + 40,000 pays 100,000: %v", v)
	}
	// Refund 20,000 of the first payment.
	refund := create(t, boss, ws, "refunds", map[string]any{"name": "Refund", "paymentId": p1, "amount": 20000, "refundDate": today(), "reason": "Returned goods"})
	if rv := get(t, boss, ws, "refunds", refund); rv["status"] != "succeeded" || rv["accountId"] != acc {
		t.Fatalf("refund: %v", rv)
	}
	if pv := get(t, boss, ws, "payments", p1); pv["refundedAmount"] != float64(20000) || pv["status"] != "partially_refunded" || pv["unappliedAmount"] != float64(0) {
		t.Fatalf("payment after a 20,000 refund: %v", pv)
	}
	v := get(t, boss, ws, "invoices", inv)
	if v["amountPaid"] != float64(80000) || v["balanceDue"] != float64(20000) || v["status"] != "partially_paid" {
		t.Fatalf("invoice after the refund: %v", v)
	}
	var off string
	_ = testDB.Pool.QueryRow(ctx, `SELECT amount::text FROM crm.refund_allocations WHERE refund_id = $1 AND invoice_id = $2`, refund, inv).Scan(&off)
	if off != "20000.00" {
		t.Fatalf("the refund should come off the invoice in crm.refund_allocations, got %q", off)
	}
	// A second refund fits only into what is left; the old endpoint makes refund records too.
	want(t, call(t, "POST", base+"/crm/refunds", boss, map[string]any{"values": map[string]any{"name": "Too much", "paymentId": p1, "amount": 40001, "refundDate": today()}}), 422, "refund more than is left")
	want(t, call(t, "POST", base+"/payments/"+p1+"/refund", boss, map[string]any{"amount": 5000, "reason": "Goodwill"}), 200, "second refund")
	if n := total(t, boss, ws, "refunds", ""); n != 2 {
		t.Fatalf("two refund records, got %d", n)
	}
	// Cancelling a refund gives the money back to the invoice.
	want(t, edit(t, boss, ws, "refunds", refund, map[string]any{"status": "cancelled"}), 200, "cancel the first refund")
	if v = get(t, boss, ws, "invoices", inv); v["amountPaid"] != float64(95000) || v["balanceDue"] != float64(5000) {
		t.Fatalf("after cancelling the 20,000 refund: %v", v)
	}

	// Credit note 3,000 against the invoice: applied when issued.
	cn := create(t, boss, ws, "credit_notes", map[string]any{"name": "Credit", "invoiceId": inv, "total": 3000, "reason": "Short delivery"})
	if cv := get(t, boss, ws, "credit_notes", cn); cv["status"] != "draft" || cv["accountId"] != acc {
		t.Fatalf("a credit note starts as a draft on the invoice's account: %v", cv)
	}
	if v = get(t, boss, ws, "invoices", inv); v["balanceDue"] != float64(5000) {
		t.Fatalf("a draft credit note changes nothing: %v", v)
	}
	want(t, edit(t, boss, ws, "credit_notes", cn, map[string]any{"status": "issued"}), 200, "issue the credit note")
	if cv := get(t, boss, ws, "credit_notes", cn); cv["status"] != "applied" || cv["appliedAmount"] != float64(3000) || cv["remainingAmount"] != float64(0) {
		t.Fatalf("issued credit note should be fully applied: %v", cv)
	}
	if v = get(t, boss, ws, "invoices", inv); v["creditedAmount"] != float64(3000) || v["balanceDue"] != float64(2000) || v["amountPaid"] != float64(95000) {
		t.Fatalf("invoice after the credit note: %v", v)
	}
	// Debit note 500: the invoice owes more.
	dn := create(t, boss, ws, "debit_notes", map[string]any{"name": "Freight", "invoiceId": inv, "total": 500, "status": "issued", "reason": "Freight"})
	if v = get(t, boss, ws, "invoices", inv); v["debitedAmount"] != float64(500) || v["balanceDue"] != float64(2500) {
		t.Fatalf("invoice after the debit note: %v", v)
	}
	// Write-offs above 1,000 need approval.
	want(t, call(t, "PUT", base+"/approvals/rules", boss, map[string]any{"kind": "write_off", "rules": []map[string]any{{"minValue": 1000, "approverRole": "SUPER_ADMIN"}}}), 200, "write-off limit")
	want(t, call(t, "POST", base+"/crm/adjustments", boss, map[string]any{"values": map[string]any{"name": "Too big", "invoiceId": inv, "amount": 9999, "status": "approved", "reason": "x", "adjustmentType": "write_off"}}), 201, "a write-off above the limit is created…")
	big := call(t, "GET", base+"/crm/adjustments?q=Too", boss, nil).list("data")[0].(map[string]any)
	if big["values"].(map[string]any)["status"] != "pending_approval" {
		t.Fatalf("…but waits for approval: %v", big["values"])
	}
	if v = get(t, boss, ws, "invoices", inv); v["balanceDue"] != float64(2500) {
		t.Fatalf("a write-off waiting for approval changes nothing: %v", v)
	}
	inbox := call(t, "GET", base+"/approvals", boss, nil)
	req := inbox.list("data")[0].(map[string]any)
	// Approving 9,999 against a balance of 2,500 is refused: nothing is written off beyond what is owed.
	want(t, call(t, "POST", base+"/approvals/"+req["id"].(string)+"/decide", boss, map[string]any{"status": "approved"}), 422, "write off more than is owed")
	want(t, call(t, "POST", base+"/approvals/"+req["id"].(string)+"/decide", boss, map[string]any{"status": "rejected", "note": "Too much"}), 200, "reject")
	small := create(t, boss, ws, "adjustments", map[string]any{"name": "Rounding", "invoiceId": inv, "amount": 500, "status": "approved", "reason": "Small balance", "adjustmentType": "write_off"})
	if get(t, boss, ws, "adjustments", small)["status"] != "approved" {
		t.Fatalf("a write-off under the limit is approved at once")
	}
	if v = get(t, boss, ws, "invoices", inv); v["writtenOffAmount"] != float64(500) || v["balanceDue"] != float64(2000) || v["status"] != "partially_paid" {
		t.Fatalf("invoice after the write-off: %v", v)
	}
	// 100,000 + 500 − 95,000 − 3,000 − 500 = 2,000. A last payment settles it.
	pay(2000, inv)
	if v = get(t, boss, ws, "invoices", inv); v["balanceDue"] != float64(0) || v["status"] != "paid" {
		t.Fatalf("settled: %v", v)
	}
	ledger := call(t, "GET", base+"/invoices/"+inv+"/payments", boss, nil)
	if num(t, ledger, "credited") != 3000 || num(t, ledger, "debited") != 500 || num(t, ledger, "writtenOff") != 500 || num(t, ledger, "balance") != 0 || len(ledger.list("adjustments")) != 3 {
		t.Fatalf("ledger: %s", truncate(ledger.Raw, 600))
	}
	// Cancelling the debit note: the invoice was over-settled by 500 — never a negative balance.
	want(t, edit(t, boss, ws, "debit_notes", dn, map[string]any{"status": "cancelled"}), 200, "cancel the debit note")
	if v = get(t, boss, ws, "invoices", inv); v["balanceDue"] != float64(0) || v["debitedAmount"] != float64(0) {
		t.Fatalf("no negative balance: %v", v)
	}

	// Credit kept on account: a 30,000 note against a 10,000 invoice leaves 20,000 for the next one.
	inv2 := create(t, boss, ws, "invoices", map[string]any{"name": "Small invoice", "total": 10000, "status": "sent", "accountId": acc})
	inv3 := create(t, boss, ws, "invoices", map[string]any{"name": "Next invoice", "total": 50000, "status": "sent", "accountId": acc})
	cn2 := create(t, boss, ws, "credit_notes", map[string]any{"name": "Big credit", "accountId": acc, "invoiceId": inv2, "total": 30000, "status": "issued"})
	if cv := get(t, boss, ws, "credit_notes", cn2); cv["status"] != "partially_applied" || cv["remainingAmount"] != float64(20000) {
		t.Fatalf("credit left on account: %v", cv)
	}
	if get(t, boss, ws, "invoices", inv2)["status"] != "paid" {
		t.Fatalf("a credit note that covers the invoice settles it")
	}
	want(t, call(t, "POST", base+"/credit_notes/"+cn2+"/apply", boss, map[string]any{"invoiceId": inv3, "amount": 20001}), 422, "apply more credit than is left")
	ap := call(t, "POST", base+"/credit_notes/"+cn2+"/apply", boss, map[string]any{"invoiceId": inv3, "amount": 20000})
	want(t, ap, 200, "apply the rest")
	if get(t, boss, ws, "credit_notes", cn2)["status"] != "applied" || get(t, boss, ws, "invoices", inv3)["balanceDue"] != float64(30000) {
		t.Fatalf("credit applied to the next invoice")
	}
	stranger := create(t, boss, ws, "accounts", map[string]any{"name": "Someone else"})
	inv4 := create(t, boss, ws, "invoices", map[string]any{"name": "Theirs", "total": 100, "status": "sent", "accountId": stranger})
	cn3 := create(t, boss, ws, "credit_notes", map[string]any{"name": "Loose credit", "accountId": acc, "total": 50, "status": "issued"})
	want(t, call(t, "POST", base+"/credit_notes/"+cn3+"/apply", boss, map[string]any{"invoiceId": inv4, "amount": 50}), 422, "credit of one customer on another's invoice")

	st := call(t, "GET", base+"/accounts/"+acc+"/statement", boss, nil)
	want(t, st, 200, "statement")
	if num(t, st, "outstanding") != 30000 || num(t, st, "unusedCredit") != 50 || num(t, st, "netReceivable") != 29950 {
		t.Fatalf("statement: %s", truncate(st.Raw, 400))
	}
	// Nothing was applied twice, anywhere.
	var bad int
	_ = testDB.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.object_records i JOIN crm.workspaces w ON w.id = i.workspace_id
		WHERE w.code = $1 AND i.object_key = 'invoices' AND (i.custom->>'balanceDue')::numeric < 0`, ws).Scan(&bad)
	if bad != 0 {
		t.Fatalf("%d invoices have a negative balance", bad)
	}

	// Another business.
	other := signIn(t, freshPhone(), "Outsider")
	otherWS := newBusiness(t, other, "Other Books")
	ob := crmAPI + "/w/" + otherWS
	want(t, call(t, "POST", ob+"/crm/refunds", other, map[string]any{"values": map[string]any{"name": "x", "paymentId": p1, "amount": 1, "refundDate": today()}}), 422, "refunding another business's payment")
	want(t, call(t, "POST", ob+"/crm/credit_notes", other, map[string]any{"values": map[string]any{"name": "x", "invoiceId": inv3, "total": 1}}), 422, "a credit note on another business's invoice")
	want(t, call(t, "POST", ob+"/crm/adjustments", other, map[string]any{"values": map[string]any{"name": "x", "invoiceId": inv3, "amount": 1, "reason": "x"}}), 422, "writing off another business's invoice")
	want(t, call(t, "GET", ob+"/credit_notes/"+cn2+"/allocations", other, nil), 404, "another business's credit note")
	want(t, call(t, "GET", ob+"/accounts/"+acc+"/statement", other, nil), 404, "another business's statement")
	want(t, call(t, "GET", base+"/accounts/"+acc+"/statement", other, nil), 403, "outsider reading a statement")
	if get(t, boss, ws, "invoices", inv3)["balanceDue"] != float64(30000) {
		t.Fatalf("another business must not change this invoice")
	}
}

// Contact roles are semantic; a record team opens a record to colleagues (D-122, D-123).
func TestContactRolesAndRecordTeams(t *testing.T) {
	boss := signIn(t, freshPhone(), "Sales Lead")
	ws := newBusiness(t, boss, "Roles Co")
	base := crmAPI + "/w/" + ws
	acc := create(t, boss, ws, "accounts", map[string]any{"name": "Key Account"})
	john := create(t, boss, ws, "contacts", map[string]any{"lastName": "John", "accountId": acc})
	sarah := create(t, boss, ws, "contacts", map[string]any{"lastName": "Sarah", "accountId": acc})
	opp := create(t, boss, ws, "opportunities", map[string]any{"name": "ERP deal", "closeDate": today(), "accountId": acc})

	roles := base + "/crm/opportunities/" + opp + "/contact-roles"
	r := call(t, "POST", roles, boss, map[string]any{"contactId": john, "role": "Decision maker", "isPrimary": true})
	want(t, r, 201, "decision maker")
	r = call(t, "POST", roles, boss, map[string]any{"contactId": sarah, "role": "Evaluator", "isPrimary": true, "startDate": "2026-09-01"})
	want(t, r, 201, "evaluator, now primary")
	primaries := 0
	for _, x := range r.list("data") {
		if x.(map[string]any)["isPrimary"] == true {
			primaries++
		}
	}
	if len(r.list("data")) != 2 || primaries != 1 {
		t.Fatalf("two roles, one primary: %s", truncate(r.Raw, 400))
	}
	want(t, call(t, "POST", roles, boss, map[string]any{"contactId": john, "role": "Champion"}), 422, "a second role for the same contact")
	want(t, call(t, "POST", roles, boss, map[string]any{"contactId": john}), 422, "a role without a name")
	list := call(t, "GET", roles, boss, nil)
	if len(list.list("roles")) < 5 {
		t.Fatalf("role suggestions for a deal: %s", truncate(list.Raw, 200))
	}
	var johnRole string
	for _, x := range list.list("data") {
		if m := x.(map[string]any); m["contactId"] == john {
			johnRole = m["id"].(string)
		}
	}
	// A role that ended stays in the history, inactive.
	up := call(t, "PATCH", base+"/contact-roles/"+johnRole, boss, map[string]any{"isActive": false, "endDate": today(), "role": "Former decision maker"})
	want(t, up, 200, "end a role")
	// Seen from the contact: the records it has a role on.
	mine := call(t, "GET", base+"/crm/contacts/"+john+"/contact-roles", boss, nil)
	if len(mine.list("data")) != 1 || mine.list("data")[0].(map[string]any)["record"] != "ERP deal" || mine.list("data")[0].(map[string]any)["isActive"] != false {
		t.Fatalf("the contact's own roles: %s", truncate(mine.Raw, 400))
	}
	// Roles on accounts, cases and contracts use the same API.
	want(t, call(t, "POST", base+"/crm/accounts/"+acc+"/contact-roles", boss, map[string]any{"contactId": john, "role": "Billing contact"}), 201, "account contact role")
	// The generic relationship API doesn't hand out contact roles, and doesn't list them.
	want(t, call(t, "POST", base+"/crm/contacts/"+john+"/relationships", boss, map[string]any{"type": "contact_role", "targetObject": "opportunities", "targetId": opp}), 422, "contact_role through the generic API")
	if n := len(call(t, "GET", base+"/crm/opportunities/"+opp+"/relationships", boss, nil).list("data")); n != 0 {
		t.Fatalf("contact roles must not show as generic relationships, got %d", n)
	}

	// ---- record team ----
	rep, repID := staffOf(t, boss, ws, "Engineer")
	recordURL := base + "/crm/opportunities/" + opp
	want(t, call(t, "GET", recordURL, rep, nil), 404, "a colleague's deal before joining its team")
	if total(t, rep, ws, "opportunities", "") != 0 {
		t.Fatalf("the rep sees no deals yet")
	}
	team := recordURL + "/team"
	want(t, call(t, "PUT", team, rep, map[string]any{"identityId": repID, "accessLevel": "full"}), 404, "adding yourself to a team of a record you can't see")
	tr := call(t, "PUT", team, boss, map[string]any{"identityId": repID, "teamRole": "Sales engineer", "accessLevel": "read"})
	want(t, tr, 200, "add to the opportunity team")
	want(t, call(t, "GET", recordURL, rep, nil), 200, "a team member opens the deal")
	if total(t, rep, ws, "opportunities", "") != 1 {
		t.Fatalf("the deal shows in the team member's list")
	}
	want(t, edit(t, rep, ws, "opportunities", opp, map[string]any{"nextStep": "Demo"}), 403, "read access can't change the deal")
	want(t, call(t, "PUT", team, rep, map[string]any{"identityId": repID, "accessLevel": "full"}), 403, "raising your own access")
	want(t, call(t, "PUT", team, boss, map[string]any{"identityId": repID, "teamRole": "Sales engineer", "accessLevel": "write"}), 200, "write access")
	want(t, edit(t, rep, ws, "opportunities", opp, map[string]any{"nextStep": "Demo"}), 200, "write access changes the deal")
	want(t, call(t, "DELETE", recordURL, rep, nil), 403, "write access can't delete")
	// The account is a different record with its own team.
	want(t, call(t, "GET", base+"/crm/accounts/"+acc, rep, nil), 404, "the deal's team doesn't open the account")
	want(t, call(t, "PUT", base+"/crm/accounts/"+acc+"/team", boss, map[string]any{"identityId": repID, "teamRole": "Account manager", "accessLevel": "read", "isPrimary": true}), 200, "account team")
	want(t, call(t, "GET", base+"/crm/accounts/"+acc, rep, nil), 200, "account team member opens the account")
	// Only people of this business can be on a team.
	outsider := signIn(t, freshPhone(), "Outsider")
	outsiderID := call(t, "GET", crmAPI+"/me", outsider, nil).str("identity", "id")
	otherWS := newBusiness(t, outsider, "Outside Co")
	want(t, call(t, "PUT", team, boss, map[string]any{"identityId": outsiderID, "accessLevel": "read"}), 422, "someone from another business on the team")
	want(t, call(t, "GET", team, outsider, nil), 403, "outsider reading a team")
	want(t, call(t, "GET", crmAPI+"/w/"+otherWS+"/crm/opportunities/"+opp+"/team", outsider, nil), 404, "a record id from another business")
	want(t, call(t, "POST", crmAPI+"/w/"+otherWS+"/crm/accounts/"+create(t, outsider, otherWS, "accounts", map[string]any{"name": "Theirs"})+"/contact-roles", outsider,
		map[string]any{"contactId": john, "role": "Spy"}), 404, "a contact of another business in a role")
	want(t, call(t, "PATCH", crmAPI+"/w/"+otherWS+"/contact-roles/"+johnRole, outsider, map[string]any{"role": "x"}), 404, "another business's contact role")
	// Leaving the team closes the door again.
	want(t, call(t, "DELETE", team+"/"+repID, rep, nil), 204, "leave the team")
	want(t, call(t, "GET", recordURL, rep, nil), 404, "after leaving the team")
}

// Territories form a tree; accounts and people are assigned with dates; deals follow (D-124).
func TestTerritoriesResolveAndRollUp(t *testing.T) {
	ctx := context.Background()
	boss := signIn(t, freshPhone(), "Sales Director")
	ws := newBusiness(t, boss, "Territory Co")
	base := crmAPI + "/w/" + ws
	india := create(t, boss, ws, "territories", map[string]any{"name": "India", "territoryType": "country"})
	tn := create(t, boss, ws, "territories", map[string]any{"name": "Tamil Nadu", "territoryType": "state", "parentTerritoryId": india})
	cbe := create(t, boss, ws, "territories", map[string]any{"name": "Coimbatore", "territoryType": "area", "parentTerritoryId": tn})
	chn := create(t, boss, ws, "territories", map[string]any{"name": "Chennai", "territoryType": "area", "parentTerritoryId": tn})
	want(t, edit(t, boss, ws, "territories", india, map[string]any{"parentTerritoryId": cbe}), 422, "a territory under its own sub-territory")
	tree := call(t, "GET", base+"/territories/tree", boss, nil)
	want(t, tree, 200, "tree")
	if len(tree.list("data")) != 4 || tree.list("data")[0].(map[string]any)["name"] != "India" || tree.list("data")[2].(map[string]any)["depth"] != float64(2) {
		t.Fatalf("tree order: %s", truncate(tree.Raw, 500))
	}
	acc := create(t, boss, ws, "accounts", map[string]any{"name": "Mill in Coimbatore"})
	as := call(t, "POST", base+"/territories/"+cbe+"/assignments", boss, map[string]any{"kind": "account", "accountId": acc, "effectiveFrom": "2026-01-01"})
	want(t, as, 200, "account → Coimbatore")
	want(t, call(t, "POST", base+"/territories/"+cbe+"/assignments", boss, map[string]any{"kind": "account", "accountId": acc}), 422, "assign twice")
	// A deal for the account lands in the account's territory; a deal with no account in its owner's.
	open := create(t, boss, ws, "opportunities", map[string]any{"name": "Open deal", "amount": 100000, "closeDate": today(), "accountId": acc, "status": "negotiation", "forecastCategory": "commit"})
	won := create(t, boss, ws, "opportunities", map[string]any{"name": "Won deal", "amount": 50000, "closeDate": today(), "accountId": acc, "status": "closed_won"})
	if get(t, boss, ws, "opportunities", open)["territoryId"] != cbe {
		t.Fatalf("a deal takes its account's territory")
	}
	me := call(t, "GET", crmAPI+"/me", boss, nil).str("identity", "id")
	want(t, call(t, "POST", base+"/territories/"+chn+"/assignments", boss, map[string]any{"kind": "user", "identityId": me}), 200, "user → Chennai")
	loose := create(t, boss, ws, "opportunities", map[string]any{"name": "No account", "amount": 7000, "closeDate": today(), "status": "closed_won"})
	if get(t, boss, ws, "opportunities", loose)["territoryId"] != chn {
		t.Fatalf("a deal without an account takes its owner's territory")
	}
	periods := call(t, "GET", base+"/forecast/periods", boss, nil)
	month := periods.str("current", "month")
	byTerritory := func() map[string]map[string]any {
		f := call(t, "GET", base+"/forecast?period="+month+"&groupBy=territory", boss, nil)
		want(t, f, 200, "forecast by territory")
		if num(t, f, "totals", "closed") != 57000 || num(t, f, "totals", "commit") != 100000 {
			t.Fatalf("the grand total counts each deal once: %s", truncate(f.Raw, 400))
		}
		out := map[string]map[string]any{}
		for _, x := range f.list("rows") {
			m := x.(map[string]any)
			out[m["id"].(string)] = m
		}
		return out
	}
	rows := byTerritory()
	// Coimbatore 50,000 won + 100,000 commit; Chennai 7,000; Tamil Nadu and India roll both up.
	if rows[cbe]["closed"] != float64(50000) || rows[cbe]["commit"] != float64(100000) || rows[chn]["closed"] != float64(7000) ||
		rows[tn]["closed"] != float64(57000) || rows[india]["closed"] != float64(57000) || rows[india]["commit"] != float64(100000) {
		t.Fatalf("territory roll-up: cbe=%v chn=%v tn=%v india=%v", rows[cbe]["closed"], rows[chn]["closed"], rows[tn]["closed"], rows[india]["closed"])
	}
	// The account moves to Chennai: the open deal follows, the won deal stays where it closed.
	want(t, call(t, "POST", base+"/territories/"+chn+"/assignments", boss, map[string]any{"kind": "account", "accountId": acc}), 200, "account → Chennai")
	if get(t, boss, ws, "opportunities", open)["territoryId"] != chn || get(t, boss, ws, "opportunities", won)["territoryId"] != cbe {
		t.Fatalf("open deals follow the account; closed deals keep their territory")
	}
	rows = byTerritory()
	if rows[cbe]["closed"] != float64(50000) || rows[cbe]["commit"] != float64(0) || rows[chn]["commit"] != float64(100000) {
		t.Fatalf("after the move: cbe=%v chn=%v", rows[cbe], rows[chn])
	}
	hist := call(t, "GET", base+"/crm/accounts/"+acc+"/territories", boss, nil)
	if len(hist.list("data")) != 2 || hist.list("data")[0].(map[string]any)["territoryId"] != chn || hist.list("data")[1].(map[string]any)["effectiveTo"] == nil {
		t.Fatalf("the old assignment is kept, ended: %s", truncate(hist.Raw, 500))
	}
	// By role, the same total.
	fr := call(t, "GET", base+"/forecast?period="+month+"&groupBy=role", boss, nil)
	if len(fr.list("rows")) != 1 || fr.list("rows")[0].(map[string]any)["closed"] != float64(57000) {
		t.Fatalf("forecast by role: %s", truncate(fr.Raw, 400))
	}
	// Submitting keeps history.
	want(t, call(t, "POST", base+"/forecast/submit", boss, map[string]any{"periodKey": month, "forecastAmount": 60000}), 200, "submit")
	want(t, call(t, "POST", base+"/forecast/submit", boss, map[string]any{"periodKey": month, "forecastAmount": 57000}), 200, "submit again")
	fh := call(t, "GET", base+"/forecast/history?period="+month, boss, nil)
	want(t, fh, 200, "forecast history")
	if len(fh.list("events")) != 2 || num2(fh.list("accuracy")[0].(map[string]any)["accuracyPercent"]) != 100 {
		t.Fatalf("history keeps both submissions; 57,000 submitted against 57,000 closed is 100%%: %s", truncate(fh.Raw, 500))
	}
	// Permissions and isolation.
	rep, _ := staffOf(t, boss, ws, "Rep")
	want(t, call(t, "POST", base+"/territories/"+cbe+"/assignments", rep, map[string]any{"kind": "account", "accountId": acc}), 403, "staff assigning a territory")
	other := signIn(t, freshPhone(), "Rival")
	otherWS := newBusiness(t, other, "Rival Territories")
	ob := crmAPI + "/w/" + otherWS
	otherAcc := create(t, other, otherWS, "accounts", map[string]any{"name": "Theirs"})
	otherT := create(t, other, otherWS, "territories", map[string]any{"name": "Their patch"})
	want(t, call(t, "POST", ob+"/territories/"+cbe+"/assignments", other, map[string]any{"kind": "account", "accountId": otherAcc}), 404, "another business's territory")
	want(t, call(t, "POST", ob+"/territories/"+otherT+"/assignments", other, map[string]any{"kind": "account", "accountId": acc}), 422, "another business's account in a territory")
	want(t, call(t, "POST", ob+"/territories/"+otherT+"/assignments", other, map[string]any{"kind": "user", "identityId": me}), 422, "a person of another business in a territory")
	want(t, call(t, "POST", ob+"/crm/territories", other, map[string]any{"values": map[string]any{"name": "Under theirs", "parentTerritoryId": india}}), 422, "a parent territory of another business")
	if len(call(t, "GET", ob+"/territories/tree", other, nil).list("data")) != 1 {
		t.Fatalf("territories leaked across businesses")
	}
	var crossed int
	_ = testDB.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.territory_assignments a JOIN crm.object_records t ON t.id = a.territory_id WHERE t.workspace_id <> a.workspace_id`).Scan(&crossed)
	if crossed != 0 {
		t.Fatalf("a territory assignment crosses businesses")
	}
}

func num2(v any) float64 { f, _ := v.(float64); return f }

// Campaign members and what each campaign brought in, without counting a deal twice (D-125).
func TestCampaignAttributionAddsUp(t *testing.T) {
	boss := signIn(t, freshPhone(), "Marketer")
	ws := newBusiness(t, boss, "Campaign Co")
	base := crmAPI + "/w/" + ws
	mk := func(name string) string {
		r := call(t, "POST", base+"/campaigns", boss, map[string]any{"name": name, "subject": "Hello", "bodyHtml": "<p>Hi</p>", "object": "contacts"})
		if r.Status != 200 && r.Status != 201 {
			t.Fatalf("create campaign: %d %s", r.Status, truncate(r.Raw, 300))
		}
		return r.str("id")
	}
	expo, mail := mk("Trade expo"), mk("Follow-up mailer")
	acc := create(t, boss, ws, "accounts", map[string]any{"name": "Prospect Ltd"})
	con := create(t, boss, ws, "contacts", map[string]any{"lastName": "Buyer", "accountId": acc})
	con2 := create(t, boss, ws, "contacts", map[string]any{"lastName": "Evaluator", "accountId": acc})
	lead := create(t, boss, ws, "leads", map[string]any{"lastName": "Walk-in"})
	want(t, call(t, "POST", base+"/campaigns/"+expo+"/members", boss, map[string]any{"contactId": con, "status": "responded", "source": "event"}), 200, "member 1")
	want(t, call(t, "POST", base+"/campaigns/"+expo+"/members", boss, map[string]any{"contactId": con}), 422, "the same member twice")
	want(t, call(t, "POST", base+"/campaigns/"+expo+"/members", boss, map[string]any{"leadId": lead}), 200, "a lead member")
	want(t, call(t, "POST", base+"/campaigns/"+expo+"/members", boss, map[string]any{"leadId": lead, "contactId": con}), 422, "a member is a lead or a contact, not both")
	time.Sleep(15 * time.Millisecond) // the mailer touches later than the expo
	mm := call(t, "POST", base+"/campaigns/"+mail+"/members", boss, map[string]any{"contactId": con2})
	want(t, mm, 200, "member of the second campaign")
	memberID := mm.list("data")[0].(map[string]any)["id"].(string)
	want(t, call(t, "PATCH", base+"/campaign-members/"+memberID, boss, map[string]any{"status": "responded"}), 200, "responded")
	want(t, call(t, "PATCH", base+"/campaigns/"+expo+"/costs", boss, map[string]any{"campaignType": "event", "actualCost": 20000, "budgetedCost": 25000}), 200, "what the expo cost")
	// One won deal of 100,001: its primary contact came from the expo, a contact role from the mailer.
	opp := create(t, boss, ws, "opportunities", map[string]any{"name": "Big order", "amount": 100001, "closeDate": today(), "status": "closed_won", "accountId": acc, "contactId": con})
	want(t, call(t, "POST", base+"/crm/opportunities/"+opp+"/contact-roles", boss, map[string]any{"contactId": con2, "role": "Evaluator"}), 201, "contact role")
	create(t, boss, ws, "opportunities", map[string]any{"name": "Untouched", "amount": 999, "closeDate": today(), "status": "closed_won"})

	att := func(model string) map[string]map[string]any {
		r := call(t, "GET", base+"/campaigns/attribution?model="+model, boss, nil)
		want(t, r, 200, "attribution "+model)
		out := map[string]map[string]any{}
		for _, x := range r.list("data") {
			m := x.(map[string]any)
			out[m["id"].(string)] = m
		}
		if sum := num2(out[expo]["wonRevenue"]) + num2(out[mail]["wonRevenue"]); sum != 100001 {
			t.Fatalf("%s touch: the campaigns' shares must add up to the deal exactly, got %v", model, sum)
		}
		return out
	}
	first := att("first")
	if first[expo]["wonRevenue"] != float64(100001) || first[mail]["wonRevenue"] != float64(0) {
		t.Fatalf("first touch gives everything to the expo: %v / %v", first[expo]["wonRevenue"], first[mail]["wonRevenue"])
	}
	// Spent 20,000, brought in 100,001: ROI 400%.
	if first[expo]["members"] != float64(2) || first[expo]["responded"] != float64(1) || first[expo]["leads"] != float64(1) || num2(first[expo]["roiPercent"]) != 400 || first[expo]["costPerLead"] != float64(20000) {
		t.Fatalf("expo numbers: %v", first[expo])
	}
	if last := att("last"); last[mail]["wonRevenue"] != float64(100001) {
		t.Fatalf("last touch gives everything to the mailer: %v", last[mail])
	}
	if even := att("even"); even[expo]["wonRevenue"] != 50000.5 || even[mail]["wonRevenue"] != 50000.5 {
		t.Fatalf("shared evenly: %v / %v", even[expo]["wonRevenue"], even[mail]["wonRevenue"])
	}
	mine := call(t, "GET", base+"/crm/contacts/"+con+"/campaigns", boss, nil)
	if len(mine.list("data")) != 1 || mine.list("data")[0].(map[string]any)["campaign"] != "Trade expo" {
		t.Fatalf("a contact's campaigns: %s", truncate(mine.Raw, 300))
	}
	// Another business.
	other := signIn(t, freshPhone(), "Other Marketer")
	otherWS := newBusiness(t, other, "Other Campaigns")
	ob := crmAPI + "/w/" + otherWS
	oc := call(t, "POST", ob+"/campaigns", other, map[string]any{"name": "Theirs", "subject": "x", "bodyHtml": "<p>x</p>", "object": "contacts"})
	want(t, call(t, "POST", ob+"/campaigns/"+oc.str("id")+"/members", other, map[string]any{"contactId": con}), 404, "another business's contact as a member")
	want(t, call(t, "POST", ob+"/campaigns/"+expo+"/members", other, map[string]any{"contactId": con}), 404, "a member of another business's campaign")
	want(t, call(t, "PATCH", ob+"/campaign-members/"+memberID, other, map[string]any{"status": "bounced"}), 404, "another business's member")
	want(t, call(t, "GET", base+"/campaigns/attribution", other, nil), 403, "outsider reading attribution")
	if oa := call(t, "GET", ob+"/campaigns/attribution", other, nil); len(oa.list("data")) != 1 || num(t, oa, "totals", "wonRevenue") != 0 {
		t.Fatalf("attribution leaked across businesses: %s", truncate(oa.Raw, 300))
	}
}

// Case → work order → appointment with a resource → completed → invoiced; a resource is
// never booked twice, outside its hours or while away; entitlements count usage (D-127).
func TestFieldServiceSchedulingAndEntitlements(t *testing.T) {
	boss := signIn(t, freshPhone(), "Dispatcher")
	ws := newBusiness(t, boss, "Service Co")
	base := crmAPI + "/w/" + ws
	acc := create(t, boss, ws, "accounts", map[string]any{"name": "Factory"})
	service := create(t, boss, ws, "catalog_items", map[string]any{"name": "Generator service", "itemType": "service", "unitPrice": 3000, "durationMinutes": 90, "skillsRequired": "electrical"})
	part := create(t, boss, ws, "catalog_items", map[string]any{"name": "Filter", "unitPrice": 500})
	days := "mon,tue,wed,thu,fri,sat,sun"
	ravi := create(t, boss, ws, "service_resources", map[string]any{"name": "Ravi", "resourceType": "technician", "skills": "electrical, hvac", "workStart": "09:00", "workEnd": "17:00", "workDays": days})
	create(t, boss, ws, "service_resources", map[string]any{"name": "Plumber Pat", "skills": "plumbing", "workStart": "09:00", "workEnd": "17:00", "workDays": days})

	// Tomorrow, in the business's time zone.
	var tz string
	_ = testDB.Pool.QueryRow(context.Background(), `SELECT timezone FROM crm.workspaces WHERE code = $1`, ws).Scan(&tz)
	loc, _ := time.LoadLocation(tz)
	d := time.Now().In(loc).AddDate(0, 0, 1)
	at := func(h, m int) string {
		return time.Date(d.Year(), d.Month(), d.Day(), h, m, 0, 0, loc).UTC().Format(time.RFC3339)
	}
	date := d.Format("2006-01-02")

	slots := call(t, "GET", base+"/scheduling/slots?serviceId="+service+"&date="+date, boss, nil)
	want(t, slots, 200, "slots")
	// 09:00 … 15:30 start times for a 90-minute job in a 09:00–17:00 day = 14; only Ravi has the skill.
	if len(slots.list("data")) != 14 || slots.list("data")[0].(map[string]any)["resource"] != "Ravi" || num(t, slots, "durationMinutes") != 90 {
		t.Fatalf("slots: %d %s", len(slots.list("data")), truncate(slots.Raw, 300))
	}
	// A case under an entitlement of 1 case, blocking when used up.
	ent := create(t, boss, ws, "entitlements", map[string]any{"name": "Basic support", "accountId": acc, "casesIncluded": 1, "hoursIncluded": 10, "overagePolicy": "block", "status": "active"})
	cs := create(t, boss, ws, "cases", map[string]any{"name": "Generator won't start", "accountId": acc, "entitlementId": ent})
	if get(t, boss, ws, "entitlements", ent)["casesUsed"] != float64(1) {
		t.Fatalf("a case counts against its entitlement")
	}
	r := call(t, "POST", base+"/crm/cases", boss, map[string]any{"values": map[string]any{"name": "Second case", "accountId": acc, "entitlementId": ent}})
	want(t, r, 422, "a case beyond a blocking entitlement")
	want(t, edit(t, boss, ws, "entitlements", ent, map[string]any{"overagePolicy": "allow"}), 200, "allow overage")
	extra := create(t, boss, ws, "cases", map[string]any{"name": "Third case", "accountId": acc, "entitlementId": ent})
	if get(t, boss, ws, "cases", extra)["entitlementExceeded"] != true || get(t, boss, ws, "entitlements", ent)["casesUsed"] != float64(2) {
		t.Fatalf("an allowed case beyond the allowance is flagged")
	}

	// Case → work order.
	wo := call(t, "POST", base+"/cases/"+cs+"/work-order", boss, nil)
	want(t, wo, 201, "work order from the case")
	woID := wo.str("id")
	if v := get(t, boss, ws, "work_orders", woID); v["caseId"] != cs || v["accountId"] != acc || v["entitlementId"] != ent || v["status"] != "new" {
		t.Fatalf("work order: %v", v)
	}
	want(t, call(t, "PUT", base+"/work_orders/"+woID+"/lines", boss, map[string]any{"lines": []map[string]any{{"itemId": service, "quantity": 1}, {"itemId": part, "quantity": 2}}}), 200, "work order lines")
	if get(t, boss, ws, "work_orders", woID)["total"] != float64(4000) {
		t.Fatalf("work order total = 3,000 + 2 × 500")
	}
	// Book it.
	book := call(t, "POST", base+"/scheduling/book", boss, map[string]any{"serviceId": service, "start": at(10, 0), "accountId": acc, "workOrderId": woID, "caseId": cs})
	want(t, book, 201, "book")
	appt := book.str("id")
	av := get(t, boss, ws, "appointments", appt)
	if av["resourceId"] != ravi || av["durationMinutes"] != float64(90) || av["status"] != "scheduled" {
		t.Fatalf("booked with the only skilled resource for 90 minutes: %v", av)
	}
	if v := get(t, boss, ws, "work_orders", woID); v["status"] != "scheduled" || v["resourceId"] != ravi || v["scheduledStart"] == nil {
		t.Fatalf("booking schedules the work order: %v", v)
	}
	// The same slot again: nobody is free. Overlapping by hand: refused.
	want(t, call(t, "POST", base+"/scheduling/book", boss, map[string]any{"serviceId": service, "start": at(10, 0)}), 409, "double booking")
	clash := func(values map[string]any) resp {
		return call(t, "POST", base+"/crm/appointments", boss, map[string]any{"values": values})
	}
	want(t, clash(map[string]any{"name": "Overlap", "startsAt": at(11, 0), "resourceId": ravi, "itemId": service}), 409, "overlapping appointment")
	want(t, clash(map[string]any{"name": "Too early", "startsAt": at(7, 0), "resourceId": ravi}), 409, "outside working hours")
	want(t, clash(map[string]any{"name": "Runs late", "startsAt": at(16, 30), "resourceId": ravi, "durationMinutes": 60}), 409, "ends after working hours")
	pat := call(t, "GET", base+"/crm/service_resources?q=Pat", boss, nil).list("data")[0].(map[string]any)["id"].(string)
	want(t, clash(map[string]any{"name": "Wrong skill", "startsAt": at(13, 0), "resourceId": pat, "itemId": service}), 422, "a resource without the skill")
	// Right after is fine; an absence blocks.
	want(t, clash(map[string]any{"name": "Right after", "startsAt": at(11, 30), "resourceId": ravi, "durationMinutes": 30}), 201, "back-to-back")
	create(t, boss, ws, "resource_absences", map[string]any{"name": "Training", "resourceId": ravi, "startsAt": at(14, 0), "endsAt": at(17, 0)})
	want(t, clash(map[string]any{"name": "During training", "startsAt": at(14, 30), "resourceId": ravi, "durationMinutes": 30}), 409, "during an absence")
	after := call(t, "GET", base+"/scheduling/slots?serviceId="+service+"&date="+date, boss, nil)
	for _, s := range after.list("data") {
		start := s.(map[string]any)["start"].(string)
		if start == at(10, 0) || start == at(13, 0) || start == at(9, 0) {
			t.Fatalf("slot %s should be gone (booked, or running into the absence or the 10:00 job): %s", start, truncate(after.Raw, 400))
		}
	}
	if len(after.list("data")) != 2 { // 12:00 and 12:30 are the only 90-minute gaps left
		t.Fatalf("free slots after booking = %d: %s", len(after.list("data")), truncate(after.Raw, 500))
	}
	// Reschedule: the new time is a new appointment; the old one is kept as Rescheduled and frees its slot.
	re := call(t, "POST", base+"/appointments/"+appt+"/reschedule", boss, map[string]any{"start": at(12, 0)})
	want(t, re, 201, "reschedule")
	if get(t, boss, ws, "appointments", appt)["status"] != "rescheduled" || get(t, boss, ws, "appointments", re.str("id"))["rescheduledFrom"] != appt {
		t.Fatalf("rescheduling keeps the old appointment")
	}
	want(t, clash(map[string]any{"name": "Old slot", "startsAt": at(10, 0), "resourceId": ravi, "durationMinutes": 60}), 201, "the old slot is free again")
	want(t, edit(t, boss, ws, "appointments", re.str("id"), map[string]any{"status": "no_show"}), 200, "no-show")
	// Do the work.
	want(t, call(t, "POST", base+"/work_orders/"+woID+"/invoice", boss, nil), 422, "invoice before the work is done")
	want(t, edit(t, boss, ws, "work_orders", woID, map[string]any{"status": "in_progress"}), 200, "start")
	if _, err := testDB.Pool.Exec(context.Background(), `UPDATE crm.object_records SET custom = custom || jsonb_build_object('actualStart', to_char((now() - interval '2 hours') AT TIME ZONE 'UTC', 'YYYY-MM-DD"T"HH24:MI:SS"Z"')) WHERE id = $1`, woID); err != nil {
		t.Fatal(err)
	}
	want(t, edit(t, boss, ws, "work_orders", woID, map[string]any{"status": "completed", "completionNotes": "Replaced the filter"}), 200, "complete")
	if v := get(t, boss, ws, "work_orders", woID); v["actualEnd"] == nil {
		t.Fatalf("completion records when the work ended: %v", v)
	}
	usage := call(t, "GET", base+"/entitlements/"+ent+"/usage", boss, nil)
	want(t, usage, 200, "entitlement usage")
	if num(t, usage, "hours", "used") < 1.9 || num(t, usage, "hours", "used") > 2.1 || num(t, usage, "hours", "remaining") > 8.1 || num(t, usage, "cases", "used") != 2 {
		t.Fatalf("2 hours of work count against the entitlement: %s", truncate(usage.Raw, 400))
	}
	inv := call(t, "POST", base+"/work_orders/"+woID+"/invoice", boss, nil)
	want(t, inv, 201, "invoice the work order")
	if iv := get(t, boss, ws, "invoices", inv.str("id")); iv["total"] != float64(4000) || iv["workOrderId"] != woID || iv["accountId"] != acc || iv["status"] != "draft" {
		t.Fatalf("work order invoice: %v", iv)
	}
	want(t, call(t, "POST", base+"/work_orders/"+woID+"/invoice", boss, nil), 409, "invoice twice")

	// The assignment milestone of an SLA.
	create(t, boss, ws, "sla_policies", map[string]any{"name": "With assignment", "priority": "any", "firstResponseMinutes": 60, "resolutionMinutes": 240, "assignmentMinutes": 15, "warnPercent": 50, "isDefault": true})
	timed := create(t, boss, ws, "cases", map[string]any{"name": "Timed case"})
	sla := call(t, "GET", base+"/cases/"+timed+"/sla", boss, nil)
	if len(sla.list("timers")) != 3 {
		t.Fatalf("three clocks with an assignment milestone: %s", truncate(sla.Raw, 400))
	}
	want(t, edit(t, boss, ws, "cases", timed, map[string]any{"status": "assigned"}), 200, "assign")
	if get(t, boss, ws, "cases", timed)["assignedAt"] == nil {
		t.Fatalf("assigning completes the assignment milestone")
	}

	// Another business can't see or use any of it.
	other := signIn(t, freshPhone(), "Other Dispatcher")
	otherWS := newBusiness(t, other, "Other Service")
	ob := crmAPI + "/w/" + otherWS
	if n := len(call(t, "GET", ob+"/scheduling/slots?date="+date, other, nil).list("data")); n != 0 {
		t.Fatalf("another business sees %d slots of this one's resources", n)
	}
	want(t, call(t, "POST", ob+"/scheduling/book", other, map[string]any{"resourceId": ravi, "start": at(15, 0)}), 422, "booking another business's resource")
	want(t, call(t, "POST", ob+"/crm/appointments", other, map[string]any{"values": map[string]any{"name": "x", "startsAt": at(15, 0), "resourceId": ravi}}), 422, "an appointment with another business's resource")
	want(t, call(t, "POST", ob+"/cases/"+cs+"/work-order", other, nil), 404, "a work order from another business's case")
	want(t, call(t, "POST", ob+"/work_orders/"+woID+"/invoice", other, nil), 404, "invoicing another business's work order")
	want(t, call(t, "GET", ob+"/entitlements/"+ent+"/usage", other, nil), 404, "another business's entitlement")
	want(t, call(t, "POST", ob+"/crm/cases", other, map[string]any{"values": map[string]any{"name": "x", "entitlementId": ent}}), 422, "a case under another business's entitlement")
	want(t, call(t, "GET", base+"/scheduling/slots?date="+date, other, nil), 403, "outsider reading slots")
}

// Several refunds sent at the same moment can't take more than the payment has (§45).
func TestConcurrentRefundsNeverExceedThePayment(t *testing.T) {
	boss := signIn(t, freshPhone(), "Cashier")
	ws := newBusiness(t, boss, "Race Co")
	base := crmAPI + "/w/" + ws
	inv := create(t, boss, ws, "invoices", map[string]any{"name": "Invoice", "total": 60000, "status": "sent"})
	pay := create(t, boss, ws, "payments", map[string]any{"name": "Payment", "amount": 60000, "paymentDate": today(), "status": "paid", "invoiceId": inv})
	results := make(chan int, 6)
	for i := 0; i < 6; i++ {
		go func() {
			r := call(t, "POST", base+"/payments/"+pay+"/refund", boss, map[string]any{"amount": 25000})
			results <- r.Status
		}()
	}
	ok := 0
	for i := 0; i < 6; i++ {
		if s := <-results; s == 200 {
			ok++
		} else if s != 422 {
			t.Errorf("a refund that doesn't fit should be refused with 422, got %d", s)
		}
	}
	if ok != 2 {
		t.Fatalf("25,000 fits into 60,000 exactly twice; %d refunds went through", ok)
	}
	if v := get(t, boss, ws, "payments", pay); v["refundedAmount"] != float64(50000) || v["status"] != "partially_refunded" {
		t.Fatalf("payment after the race: %v", v)
	}
	if v := get(t, boss, ws, "invoices", inv); v["amountPaid"] != float64(10000) || v["balanceDue"] != float64(50000) {
		t.Fatalf("invoice after the race: %v", v)
	}
	// Two people pricing the same quote at once: both finish, and the quote has one set of lines.
	item := create(t, boss, ws, "catalog_items", map[string]any{"name": "Thing", "unitPrice": 100})
	quote := create(t, boss, ws, "quotes", map[string]any{"name": "Quote", "status": "draft"})
	done := make(chan int, 4)
	for i := 0; i < 4; i++ {
		go func() {
			done <- call(t, "PUT", base+"/quotes/"+quote+"/lines", boss, map[string]any{"lines": []map[string]any{{"itemId": item, "quantity": 3}}}).Status
		}()
	}
	for i := 0; i < 4; i++ {
		if s := <-done; s != 200 {
			t.Errorf("pricing a quote concurrently: got %d", s)
		}
	}
	lines := call(t, "GET", base+"/quotes/"+quote+"/lines", boss, nil)
	if len(lines.list("pricing", "lines")) != 1 || get(t, boss, ws, "quotes", quote)["total"] != float64(300) {
		t.Fatalf("one set of lines after concurrent pricing: %s", truncate(lines.Raw, 300))
	}
}

// CRM_SAMPLE_DATA puts a small connected set of example records into one business, once.
func TestSampleDataForOneBusiness(t *testing.T) {
	boss := signIn(t, freshPhone(), "Trader")
	ws := newBusiness(t, boss, "Sample Traders")
	other := signIn(t, freshPhone(), "Neighbour")
	otherWS := newBusiness(t, other, "Untouched Co")
	t.Setenv("CRM_SAMPLE_DATA", ws)
	crmMod.SampleData(context.Background())
	for object, n := range map[string]int{"leads": 2, "accounts": 2, "contacts": 2, "opportunities": 2, "catalog_items": 2, "price_books": 1, "quotes": 1, "invoices": 1,
		"payments": 1, "cases": 1, "contracts": 1, "assets": 1, "territories": 1, "tasks": 1, "line_items": 3} {
		if got := total(t, boss, ws, object, ""); got != n {
			t.Errorf("sample %s = %d, want %d", object, got, n)
		}
	}
	// The records are real: the quote is priced from the price book, the invoice is part paid, the case has its SLA.
	quote := call(t, "GET", crmAPI+"/w/"+ws+"/crm/quotes", boss, nil).list("data")[0].(map[string]any)["values"].(map[string]any)
	// 20 rolls at 12,000 less 5% = 228,000 + 5% tax 11,400; one service 2,500 + 18% tax 450.
	if quote["total"] != float64(242350) {
		t.Errorf("sample quote total = %v, want 242350", quote["total"])
	}
	inv := call(t, "GET", crmAPI+"/w/"+ws+"/crm/invoices", boss, nil).list("data")[0].(map[string]any)["values"].(map[string]any)
	if inv["total"] != float64(35400) || inv["amountPaid"] != float64(15000) || inv["balanceDue"] != float64(20400) || inv["status"] != "partially_paid" {
		t.Errorf("sample invoice: %v", inv)
	}
	cs := call(t, "GET", crmAPI+"/w/"+ws+"/crm/cases", boss, nil).list("data")[0].(map[string]any)
	if len(call(t, "GET", crmAPI+"/w/"+ws+"/cases/"+cs["id"].(string)+"/sla", boss, nil).list("timers")) != 2 {
		t.Errorf("the sample case should have SLA clocks")
	}
	// Once only, and only in the business named.
	crmMod.SampleData(context.Background())
	if got := total(t, boss, ws, "leads", ""); got != 2 {
		t.Errorf("sample data ran twice: %d leads", got)
	}
	if got := total(t, other, otherWS, "leads", "") + total(t, other, otherWS, "accounts", ""); got != 0 {
		t.Errorf("sample data reached another business: %d records", got)
	}
}
