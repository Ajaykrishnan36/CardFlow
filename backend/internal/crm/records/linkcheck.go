package records

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// One customer per record (D-173). A record about a customer's vehicle, case, invoice, quote
// or deal belongs to that record's customer: the contact and account are filled from it and
// can't be someone else. A contact brings their own account. So a case can't be for Balaji's
// account, Senthil's contact and Revathi's bike at once.

// customerAnchors are the fields that decide the customer, strongest first.
var customerAnchors = []struct{ field, object string }{
	{"assetId", "assets"}, {"caseId", "cases"}, {"workOrderId", "work_orders"}, {"invoiceId", "invoices"},
	{"orderId", "sales_orders"}, {"salesOrderId", "sales_orders"}, {"quoteId", "quotes"}, {"bookingId", "vehicle_bookings"},
	{"opportunityId", "opportunities"}, {"contractId", "contracts"}, {"matterId", "legal_matters"},
}

// linkCustomer fills contactId / accountId from what decides them and reports a mismatch.
// current is the record before the change (nil when creating).
func (h *Handler) linkCustomer(ctx context.Context, q querier, ws uuid.UUID, spec *objectSpec, fields []Field, values, current map[string]any) map[string]string {
	if spec == nil || spec.Key == "contacts" || spec.Key == "accounts" || spec.Key == "leads" {
		return nil
	}
	lookups := map[string]string{}
	for _, f := range fields {
		if f.Type == "lookup" && !f.ReadOnly {
			lookups[f.Key] = f.Lookup
		}
	}
	hasContact, hasAccount := lookups["contactId"] == "contacts", lookups["accountId"] == "accounts"
	if !hasContact && !hasAccount {
		return nil
	}
	touched := false
	for k := range values {
		if k == "contactId" || k == "accountId" {
			touched = true
		}
		for _, a := range customerAnchors {
			if k == a.field {
				touched = true
			}
		}
	}
	if !touched {
		return nil
	}
	get := func(k string) string {
		if v, ok := values[k]; ok {
			return strings.TrimSpace(asString(v))
		}
		if current != nil {
			return strings.TrimSpace(asString(current[k]))
		}
		return ""
	}
	fe := map[string]string{}
	label := func(object, id string) string { return h.labelOf(ctx, q, ws, object, id) }

	// The strongest anchor that has a customer decides.
	for _, a := range customerAnchors {
		id := get(a.field)
		if lookups[a.field] != a.object || id == "" {
			continue
		}
		var contact, account string
		if err := q.QueryRow(ctx, `SELECT COALESCE(custom->>'contactId', ''), COALESCE(custom->>'accountId', '') FROM crm.object_records
			WHERE id::text = $1 AND workspace_id = $2 AND object_key = $3`, id, ws, a.object).Scan(&contact, &account); err != nil {
			continue
		}
		if contact == "" && account == "" {
			continue
		}
		what := strings.ToLower(singularOf(a.object))
		if hasContact && contact != "" {
			switch get("contactId") {
			case "":
				values["contactId"] = contact
			case contact:
			default:
				fe["contactId"] = "This " + what + " is " + label("contacts", contact) + "’s. Pick " + label("contacts", contact) + ", or another " + what + "."
			}
		}
		if hasAccount && account != "" {
			switch get("accountId") {
			case "":
				values["accountId"] = account
			case account:
			default:
				fe["accountId"] = "This " + what + " belongs to " + label("accounts", account) + "."
			}
		}
		if hasAccount && account == "" && contact != "" && get("accountId") != "" {
			// The anchor is a person's, with no account: the record can't name some other account.
			if mine := h.contactAccount(ctx, q, ws, contact); mine != get("accountId") {
				fe["accountId"] = "This " + what + " is " + label("contacts", contact) + "’s, who isn't part of " + label("accounts", get("accountId")) + ". Leave Account empty."
			}
		}
		return fe
	}

	// No anchor: the contact brings their account.
	if hasContact && hasAccount && get("contactId") != "" {
		mine := h.contactAccount(ctx, q, ws, get("contactId"))
		switch {
		case mine == "":
			if get("accountId") != "" {
				fe["accountId"] = label("contacts", get("contactId")) + " isn't part of " + label("accounts", get("accountId")) + ". Leave Account empty, or add them to that account first."
			}
		case get("accountId") == "":
			values["accountId"] = mine
		case get("accountId") != mine:
			fe["contactId"] = label("contacts", get("contactId")) + " is a contact of " + label("accounts", mine) + ", not " + label("accounts", get("accountId")) + "."
		}
	}
	return fe
}

// contactAccount is the account a contact belongs to ("" for a person on their own).
func (h *Handler) contactAccount(ctx context.Context, q querier, ws uuid.UUID, contact string) string {
	var account string
	_ = q.QueryRow(ctx, `SELECT COALESCE(account_id::text, '') FROM crm.contacts WHERE id::text = $1 AND workspace_id = $2`, contact, ws).Scan(&account)
	return account
}

func singularOf(object string) string {
	if s := specFor(object); s != nil {
		return s.Singular
	}
	return object
}
