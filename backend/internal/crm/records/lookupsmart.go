package records

import (
	"context"
	"net/http"
	"regexp"
	"strings"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Pickers that find people the way a counter clerk does (D-143): by any part of the
// name, by any run of digits of the phone number — the last four are enough — or by
// what the customer owns (a serial or registration number). Each match shows a second
// line (phone, email, who it belongs to) so two customers with the same name can be
// told apart, and choosing one fills the related fields of the form.

// identifierKey matches fields that identify a thing a customer owns.
var identifierKey = regexp.MustCompile(`(?i)serial|regist|vin|chassis|engine|plate|policy`)

func rawExpr(f Field) string {
	if f.inColumn() {
		return "t." + f.column
	}
	return "t.custom->>'" + f.Key + "'"
}

func digitsOf(expr string) string {
	return "regexp_replace(COALESCE((" + expr + ")::text, ''), '\\D', '', 'g')"
}

// personMatch is "the linked contact / account matches the search"; personHint is how
// that person shows on the second line. expr is the link's value.
func personMatch(target, expr string) string {
	switch target {
	case "contacts", "leads":
		return `EXISTS (SELECT 1 FROM crm.` + target + ` p WHERE p.id::text = (` + expr + `)::text AND p.workspace_id = $2 AND p.deleted_at IS NULL
			AND (concat_ws(' ', p.first_name, p.last_name) ILIKE $1 OR p.phone ILIKE $1 OR p.mobile ILIKE $1
			  OR ($4::text <> '' AND (` + digitsOf("p.phone") + ` LIKE $4::text OR ` + digitsOf("p.mobile") + ` LIKE $4::text))))`
	case "accounts":
		return `EXISTS (SELECT 1 FROM crm.accounts p WHERE p.id::text = (` + expr + `)::text AND p.workspace_id = $2 AND p.deleted_at IS NULL
			AND (p.name ILIKE $1 OR p.phone ILIKE $1 OR ($4::text <> '' AND ` + digitsOf("p.phone") + ` LIKE $4::text)))`
	}
	return ""
}

func personHint(target, expr string) string {
	switch target {
	case "contacts":
		return `(SELECT concat_ws(' ', NULLIF(trim(concat_ws(' ', p.first_name, p.last_name)), ''), COALESCE(NULLIF(p.mobile, ''), NULLIF(p.phone, '')))
			FROM crm.contacts p WHERE p.id::text = (` + expr + `)::text AND p.workspace_id = $2)`
	case "accounts":
		return `(SELECT p.name FROM crm.accounts p WHERE p.id::text = (` + expr + `)::text AND p.workspace_id = $2)`
	}
	return ""
}

// recordLookupSQL builds the picker query for a record object. Arguments: $1 the text
// pattern, $2 the business, $3 the owners the viewer is limited to, $4 the digits
// pattern (” when the search isn't a number), $5 / $6 the account and contact already
// chosen on the form (” when none) — their records come first.
func recordLookupSQL(spec *objectSpec, fields []Field) string {
	match := make([]string, 0, len(spec.SearchSQL)+8)
	for _, c := range spec.SearchSQL {
		match = append(match, c+" ILIKE $1")
	}
	hint := []string{}
	order := []string{}
	seenPerson := map[string]bool{}
	for _, f := range fields {
		if f.system {
			continue
		}
		expr := rawExpr(f)
		switch f.Type {
		case "phone":
			match = append(match, "($4::text <> '' AND "+digitsOf(expr)+" LIKE $4::text)")
			if !f.inColumn() {
				match = append(match, expr+" ILIKE $1")
			}
			hint = append(hint, "NULLIF(("+expr+")::text, '')")
		case "email":
			if !f.inColumn() {
				match = append(match, expr+" ILIKE $1")
			}
			hint = append(hint, "NULLIF(("+expr+")::text, '')")
		case "text":
			if !f.inColumn() {
				match = append(match, expr+" ILIKE $1")
				if identifierKey.MatchString(f.Key) {
					hint = append(hint, "NULLIF(("+expr+")::text, '')")
				}
			}
		case "lookup":
			if f.Lookup == spec.Key {
				continue // a parent of the same kind says nothing about whose record this is
			}
			if m := personMatch(f.Lookup, expr); m != "" {
				match = append(match, m)
			}
			if h := personHint(f.Lookup, expr); h != "" && !seenPerson[f.Lookup] {
				seenPerson[f.Lookup] = true
				hint = append(hint, h)
			}
			switch f.Key {
			case "accountId":
				order = append(order, "($5::text <> '' AND ("+expr+")::text = $5::text) DESC")
			case "contactId":
				order = append(order, "($6::text <> '' AND ("+expr+")::text = $6::text) DESC")
			}
		}
	}
	hintSQL := "''"
	if len(hint) > 0 {
		hintSQL = "concat_ws(' · ', " + strings.Join(hint, ", ") + ")"
	}
	order = append(order, "t.updated_at DESC")
	return `SELECT t.id::text, ` + spec.TitleSQL + ` || ' · ' || t.code, ` + hintSQL + ` FROM crm.` + spec.Table + ` t
		WHERE t.workspace_id = $2 AND t.deleted_at IS NULL AND ($3::uuid[] IS NULL OR t.owner_id = ANY($3)) AND ($4::text IS NOT NULL AND $5::text IS NOT NULL AND $6::text IS NOT NULL)
		  AND (` + strings.Join(match, " OR ") + `)
		ORDER BY ` + strings.Join(order, ", ") + ` LIMIT 20`
}

// lookupDigits is the digits pattern of a search that is a number ("98765 43210",
// "+91-98765", "3210"): at least three digits and nothing but digits and separators.
func lookupDigits(q string) string {
	d := digitsOnly(q)
	if len(d) < 3 || strings.Trim(q, "0123456789 +-()") != "" {
		return ""
	}
	return "%" + d + "%"
}

func uuidOrEmpty(s string) string {
	if _, err := uuid.Parse(strings.TrimSpace(s)); err != nil {
		return ""
	}
	return strings.TrimSpace(s)
}

// GET /lookup/{target}/{id}/related — what choosing this record tells us about the
// rest of the form: the records it links to (a contact's account, an asset's owner,
// product and contract…). The form fills its empty fields of the same name and kind.
func (h *Handler) handleLookupRelated(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	target := chi.URLParam(r, "target")
	spec := specFor(target)
	if spec == nil {
		shared.WriteError(w, r, shared.NotFound("object_not_found"))
		return
	}
	if !sc.Owner && !sc.Can(target, "read") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	ctx := r.Context()
	row, fields, err := h.getRow(ctx, h.store.Pool, sc.WS, spec, id, sc.OwnersFor(target, actor(r)))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out := map[string]LookupValue{}
	for _, f := range fields {
		if f.Type != "lookup" || f.system || f.Lookup == "users" {
			continue
		}
		if v, ok := row.Lookups[f.Key]; ok && v.ID != "" && h.mayPick(sc, f.Lookup) {
			v.Object = f.Lookup
			out[f.Key] = v
		}
	}
	// One step further: a vehicle that names its owner but not the owner's account
	// still tells the form the account.
	if _, has := out["accountId"]; !has && target != "contacts" && h.mayPick(sc, "accounts") {
		if c, ok := out["contactId"]; ok {
			var v LookupValue
			if err := h.store.Pool.QueryRow(ctx, `SELECT a.id::text, a.name || ' · ' || a.code FROM crm.contacts c JOIN crm.accounts a ON a.id = c.account_id AND a.deleted_at IS NULL
				WHERE c.id::text = $1 AND c.workspace_id = $2 AND ($3::uuid[] IS NULL OR a.owner_id = ANY($3))`, c.ID, sc.WS, sc.OwnersFor("accounts", actor(r))).Scan(&v.ID, &v.Label); err == nil {
				v.Object = "accounts"
				out["accountId"] = v
			}
		}
	}
	// The other way round: an account with exactly one contact (an individual customer)
	// names that contact.
	if target == "accounts" && h.mayPick(sc, "contacts") {
		if v := h.onlyContact(ctx, sc, id, actor(r)); v != nil {
			out["contactId"] = *v
		}
	}
	respond(w, r, http.StatusOK, map[string]any{"lookups": out}, nil)
}

func (h *Handler) mayPick(sc *Scope, target string) bool {
	return specFor(target) != nil && (sc.Owner || sc.Can(target, "read"))
}

func (h *Handler) onlyContact(ctx context.Context, sc *Scope, account uuid.UUID, me uuid.UUID) *LookupValue {
	own := sc.OwnersFor("contacts", me)
	rows, err := h.store.Pool.Query(ctx, `SELECT t.id::text, `+contactSpec.TitleSQL+` || ' · ' || t.code FROM crm.contacts t
		WHERE t.workspace_id = $1 AND t.account_id = $2 AND t.deleted_at IS NULL AND ($3::uuid[] IS NULL OR t.owner_id = ANY($3)) LIMIT 2`, sc.WS, account, own)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var found []LookupValue
	for rows.Next() {
		v := LookupValue{Object: "contacts"}
		if rows.Scan(&v.ID, &v.Label) == nil {
			found = append(found, v)
		}
	}
	if len(found) != 1 {
		return nil
	}
	return &found[0]
}

// laterFields are filled in by the CRM, or only make sense once the record has moved on
// (when a case was closed, why an appointment was cancelled). They stay on the record
// page; the "New …" form leaves them out so it asks only what is known at the start.
var laterFields = map[string][]string{
	"cases": {"slaDueAt", "closedAt", "firstResponseDueAt", "firstRespondedAt", "slaBreached", "escalatedAt", "assignedAt",
		"lastCustomerUpdateAt", "entitlementExceeded", "resolution", "solutionId", "slaPolicyId"},
	"appointments":    {"cancellationReason", "rescheduledFrom", "timezone"},
	"opportunities":   {"lostReason"},
	"leads":           {"lostReason"},
	"assets":          {"workOrderId", "replacedOn"},
	"entitlements":    {"casesUsed", "hoursUsed"},
	"calendar_events": {},
}
