package records

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Multi-currency (D-118). A business has one base currency (crm.workspaces.currency).
// A deal, quote, order, invoice, payment, expense, income or contract may be in another
// currency: it then carries the exchange rate that applied when it was made and its
// amount in the base currency. The rate on a record is never changed by a later rate —
// yesterday's invoice keeps yesterday's rate.
//
//	GET    /w/{code}/currencies
//	GET    /w/{code}/exchange-rates
//	PUT    /w/{code}/exchange-rates            {fromCurrency, toCurrency?, rate, effectiveFrom}
//	DELETE /w/{code}/exchange-rates/{id}
//	GET    /w/{code}/exchange-rates/convert?from=USD&amount=100&date=2026-10-05

func (h *Handler) currencyRoutes(r chi.Router) {
	r.Get("/currencies", h.handleCurrencies)
	r.Get("/exchange-rates", h.handleListRates)
	r.Put("/exchange-rates", h.handleSaveRate)
	r.Delete("/exchange-rates/{id}", h.handleDeleteRate)
	r.Get("/exchange-rates/convert", h.handleConvertCurrency)
}

// ---- money as whole minor units: no float ever adds up two amounts ----

// Cents is an amount in hundredths of the currency unit.
type Cents int64

// parseCents reads "1234.5", 1234.5 or json.Number as hundredths, rounding half away from zero.
func parseCents(v any) (Cents, bool) {
	s := numText(v)
	if s == "" {
		return 0, false
	}
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	whole, frac, _ := strings.Cut(s, ".")
	if whole == "" {
		whole = "0"
	}
	w, err := strconv.ParseInt(whole, 10, 64)
	if err != nil || w > 9_000_000_000_000 {
		return 0, false
	}
	frac += "000"
	f, err := strconv.ParseInt(frac[:2], 10, 64)
	if err != nil {
		return 0, false
	}
	c := w*100 + f
	if frac[2] >= '5' {
		c++
	}
	if neg {
		c = -c
	}
	return Cents(c), true
}

// numText renders a JSON number (or numeric string) as plain decimal text; "" when it isn't one.
func numText(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		x = strings.TrimSpace(x)
		if _, err := strconv.ParseFloat(x, 64); err != nil || strings.ContainsAny(x, "eE") {
			return ""
		}
		return x
	case json.Number:
		return numText(x.String())
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return ""
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return numText(float64(x))
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case Cents:
		return x.String()
	}
	return ""
}

func (c Cents) String() string {
	neg := c < 0
	if neg {
		c = -c
	}
	s := fmt.Sprintf("%d.%02d", int64(c)/100, int64(c)%100)
	if neg {
		return "-" + s
	}
	return s
}

// MarshalJSON writes the amount as a JSON number with two decimals.
func (c Cents) MarshalJSON() ([]byte, error) { return []byte(c.String()), nil }

func (c *Cents) UnmarshalJSON(b []byte) error {
	v, ok := parseCents(strings.Trim(string(b), `"`))
	if !ok && string(b) != "null" {
		return errors.New("not an amount")
	}
	*c = v
	return nil
}

// Float is for values handed to the record engine, which stores JSON numbers.
func (c Cents) Float() float64 { return float64(c) / 100 }

// mulRatio returns c × num ÷ den rounded half away from zero, in integers.
func (c Cents) mulRatio(num, den int64) Cents {
	if den == 0 {
		return 0
	}
	p := int64(c) * num
	q := p / den
	r := p % den
	if r < 0 {
		r = -r
	}
	if 2*r >= absInt(den) {
		if (p < 0) != (den < 0) {
			q--
		} else {
			q++
		}
	}
	return Cents(q)
}

func absInt(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

// bps reads a percentage ("12.5") as hundredths of a percent (1250).
func bps(v any) (int64, bool) {
	c, ok := parseCents(v)
	return int64(c), ok
}

// percentOf is c × pct% with pct in hundredths of a percent.
func (c Cents) percentOf(pctBps int64) Cents { return c.mulRatio(pctBps, 10000) }

// timesQty is c × quantity, the quantity given to three decimals.
func (c Cents) timesQty(qtyMilli int64) Cents { return c.mulRatio(qtyMilli, 1000) }

// milli reads a quantity ("2", "1.5") as thousandths.
func milli(v any) (int64, bool) {
	s := numText(v)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 || f > 1e9 {
		return 0, false
	}
	return int64(math.Round(f * 1000)), true
}

// ---- currencies and rates ----

func (h *Handler) baseCurrency(ctx context.Context, q querier, ws uuid.UUID) string {
	var c string
	_ = q.QueryRow(ctx, `SELECT COALESCE(NULLIF(currency, ''), 'INR') FROM crm.workspaces WHERE id = $1`, ws).Scan(&c)
	if c == "" {
		c = "INR"
	}
	return c
}

func canManagePricing(sc *Scope) bool {
	return sc.Owner || sc.Eff.HasCapability(access.CapPricing)
}

func (h *Handler) handleCurrencies(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	type cur struct {
		Code     string `json:"code"`
		Name     string `json:"name"`
		Symbol   string `json:"symbol"`
		Decimals int    `json:"decimalPlaces"`
	}
	rows, err := h.store.Pool.Query(r.Context(), `SELECT code, name, symbol, decimal_places FROM crm.currencies WHERE is_active ORDER BY code`)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	list := []cur{}
	for rows.Next() {
		var c cur
		if err := rows.Scan(&c.Code, &c.Name, &c.Symbol, &c.Decimals); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		list = append(list, c)
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": list, "base": h.baseCurrency(r.Context(), h.store.Pool, sc.WS), "canManage": canManagePricing(sc)})
}

type ExchangeRate struct {
	ID            string  `json:"id"`
	From          string  `json:"fromCurrency"`
	To            string  `json:"toCurrency"`
	Rate          string  `json:"rate"`
	EffectiveFrom string  `json:"effectiveFrom"`
	EffectiveTo   *string `json:"effectiveTo,omitempty"`
	Source        string  `json:"source"`
}

func (h *Handler) handleListRates(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	rows, err := h.store.Pool.Query(r.Context(), `
		SELECT id::text, from_currency, to_currency, trim(trailing '.' from trim(trailing '0' from rate::text)), effective_from::text, effective_to::text, source
		FROM crm.exchange_rates WHERE workspace_id = $1 ORDER BY from_currency, to_currency, effective_from DESC LIMIT 500`, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	list := []ExchangeRate{}
	for rows.Next() {
		var x ExchangeRate
		if err := rows.Scan(&x.ID, &x.From, &x.To, &x.Rate, &x.EffectiveFrom, &x.EffectiveTo, &x.Source); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		list = append(list, x)
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": list, "base": h.baseCurrency(r.Context(), h.store.Pool, sc.WS), "canManage": canManagePricing(sc)})
}

func (h *Handler) handleSaveRate(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !canManagePricing(sc) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	var in struct {
		From          string `json:"fromCurrency"`
		To            string `json:"toCurrency"`
		Rate          any    `json:"rate"`
		EffectiveFrom string `json:"effectiveFrom"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	in.From, in.To = strings.ToUpper(strings.TrimSpace(in.From)), strings.ToUpper(strings.TrimSpace(in.To))
	if in.To == "" {
		in.To = h.baseCurrency(ctx, h.store.Pool, sc.WS)
	}
	if in.EffectiveFrom == "" {
		in.EffectiveFrom = time.Now().Format("2006-01-02")
	}
	rate := numText(in.Rate)
	fe := map[string]string{}
	if f, _ := strconv.ParseFloat(rate, 64); rate == "" || f <= 0 || f > 1e9 {
		fe["rate"] = "Enter how many " + in.To + " one " + in.From + " is worth."
	}
	if _, err := time.Parse("2006-01-02", in.EffectiveFrom); err != nil {
		fe["effectiveFrom"] = "Enter a date."
	}
	if in.From == in.To {
		fe["fromCurrency"] = "Choose a currency other than " + in.To + "."
	}
	var known int
	_ = h.store.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.currencies WHERE code IN ($1, $2) AND is_active`, in.From, in.To).Scan(&known)
	if known != 2 && fe["fromCurrency"] == "" {
		fe["fromCurrency"] = "Choose a currency from the list."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	a := actorFromRequest(r, "ui")
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		// The rate before this one ends the day before; history is kept.
		if _, err := tx.Exec(ctx, `UPDATE crm.exchange_rates SET effective_to = $4::date - 1
			WHERE workspace_id = $1 AND from_currency = $2 AND to_currency = $3 AND effective_from < $4::date AND (effective_to IS NULL OR effective_to >= $4::date)`,
			sc.WS, in.From, in.To, in.EffectiveFrom); err != nil {
			return err
		}
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `INSERT INTO crm.exchange_rates (workspace_id, from_currency, to_currency, rate, effective_from, created_by)
			VALUES ($1, $2, $3, $4::numeric, $5::date, $6)
			ON CONFLICT (workspace_id, from_currency, to_currency, effective_from) DO UPDATE SET rate = EXCLUDED.rate RETURNING id`,
			sc.WS, in.From, in.To, rate, in.EffectiveFrom, a.ID).Scan(&id); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "exchange_rate.saved", "exchange_rate", &id, nil, in))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.handleListRates(w, r)
}

func (h *Handler) handleDeleteRate(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !canManagePricing(sc) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("rate_not_found"))
		return
	}
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `DELETE FROM crm.exchange_rates WHERE id = $1 AND workspace_id = $2`, id, sc.WS)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return shared.NotFound("rate_not_found")
		}
		return shared.WriteAudit(r.Context(), tx, a.audit(sc.WS, "exchange_rate.deleted", "exchange_rate", &id, nil, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// rateOn is the rate from a currency into the base currency on a day ("" when none is set).
// A rate entered the other way round (base → currency) is used inverted.
func (h *Handler) rateOn(ctx context.Context, q querier, ws uuid.UUID, from, base, day string) string {
	if from == base {
		return "1"
	}
	var rate string
	err := q.QueryRow(ctx, `
		SELECT r FROM (
		  SELECT rate::text AS r, effective_from, 0 AS inv FROM crm.exchange_rates
		   WHERE workspace_id = $1 AND from_currency = $2 AND to_currency = $3 AND effective_from <= $4::date AND (effective_to IS NULL OR effective_to >= $4::date)
		  UNION ALL
		  SELECT round(1 / rate, 8)::text, effective_from, 1 FROM crm.exchange_rates
		   WHERE workspace_id = $1 AND from_currency = $3 AND to_currency = $2 AND effective_from <= $4::date AND (effective_to IS NULL OR effective_to >= $4::date)
		) x ORDER BY inv, effective_from DESC LIMIT 1`, ws, from, base, day).Scan(&rate)
	if err != nil {
		return ""
	}
	return rate
}

func (h *Handler) handleConvertCurrency(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	ctx := r.Context()
	q := r.URL.Query()
	base := h.baseCurrency(ctx, h.store.Pool, sc.WS)
	from := strings.ToUpper(q.Get("from"))
	day := q.Get("date")
	if _, err := time.Parse("2006-01-02", day); err != nil {
		day = time.Now().Format("2006-01-02")
	}
	amount := numText(q.Get("amount"))
	if amount == "" {
		amount = "1"
	}
	rate := h.rateOn(ctx, h.store.Pool, sc.WS, from, base, day)
	if rate == "" {
		shared.WriteError(w, r, shared.Validation(map[string]string{"from": "No exchange rate from " + from + " to " + base + " on " + day + "."}))
		return
	}
	var out string
	if err := h.store.Pool.QueryRow(ctx, `SELECT round($1::numeric * $2::numeric, 2)::text`, amount, rate).Scan(&out); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"from": from, "to": base, "rate": rate, "date": day, "amount": json.RawMessage(amount), "converted": json.RawMessage(out)})
}

// ---- the amount of a record in the base currency ----

const sourceCurrency = "currency"

// moneyFields: per object, the amount that is converted, where the result goes and which
// date picks the rate.
var moneyFields = map[string][3]string{
	"opportunities": {"amount", "baseAmount", "closeDate"},
	"quotes":        {"total", "baseTotal", "quoteDate"},
	"sales_orders":  {"total", "baseTotal", "orderDate"},
	"invoices":      {"total", "baseTotal", "invoiceDate"},
	"payments":      {"amount", "baseAmount", "paymentDate"},
	"expenses":      {"amount", "baseAmount", "date"},
	"income":        {"amount", "baseAmount", "date"},
	"contracts":     {"contractValue", "baseValue", "startDate"},
}

// currencySaved fixes the currency, rate and base amount of a record. The rate is looked
// up once — when the record first gets a foreign currency — and kept from then on.
func (h *Handler) currencySaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, spec *objectSpec, op string, before, after *Row) (*Row, error) {
	mf, ok := moneyFields[spec.Key]
	if !ok || (op != "create" && op != "update") {
		return after, nil
	}
	amountKey, baseKey, dateKey := mf[0], mf[1], mf[2]
	base := h.baseCurrency(ctx, tx, ws)
	cur := strings.ToUpper(strings.TrimSpace(after.text("currency")))
	set := map[string]any{}
	if cur == "" {
		cur = base
		set["currency"] = base
	} else if cur != after.text("currency") {
		set["currency"] = cur
	}
	if cur != base {
		var known bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.currencies WHERE code = $1 AND is_active)`, cur).Scan(&known); err != nil {
			return nil, err
		}
		if !known {
			return nil, shared.Validation(map[string]string{"currency": "Use a three-letter currency code from the currency list, e.g. USD."})
		}
	}
	amount := numText(after.Values[amountKey])
	rate := numText(after.Values["exchangeRate"])
	sameCurrency := before != nil && strings.EqualFold(before.text("currency"), cur)
	if f, _ := strconv.ParseFloat(rate, 64); rate == "" || f <= 0 || !sameCurrency && op == "update" && numText(before.Values["exchangeRate"]) == rate {
		// No rate yet, or the currency just changed and the old rate was left in place.
		day := after.text(dateKey)
		if len(day) >= 10 {
			day = day[:10]
		}
		if _, err := time.Parse("2006-01-02", day); err != nil {
			day = time.Now().Format("2006-01-02")
		}
		rate = h.rateOn(ctx, tx, ws, cur, base, day)
		if rate == "" {
			return nil, shared.Validation(map[string]string{"currency": "There is no exchange rate from " + cur + " to " + base + " for " + day + ". Add one under Settings → Currencies, or type the rate on this record."})
		}
		var asNumber float64
		_ = tx.QueryRow(ctx, `SELECT $1::numeric::float8`, rate).Scan(&asNumber)
		set["exchangeRate"] = asNumber
	}
	if amount != "" {
		var converted string
		if err := tx.QueryRow(ctx, `SELECT round($1::numeric * $2::numeric, 2)::text`, amount, rate).Scan(&converted); err != nil {
			return nil, err
		}
		if want, _ := parseCents(converted); numText(after.Values[baseKey]) == "" || mustCents(after.Values[baseKey]) != want {
			set[baseKey] = want.Float()
		}
	}
	if len(set) == 0 {
		return after, nil
	}
	return h.updateValues(ctx, tx, ws, spec, after.uuid(), systemActor(sourceCurrency), set, nil, nil)
}

func mustCents(v any) Cents {
	c, _ := parseCents(v)
	return c
}
