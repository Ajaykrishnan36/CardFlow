package records

import (
	"context"
	"errors"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/plans"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Business cards in the CRM (D-98). A card is scanned by a member inside one business.
// The server looks for the person among that business's own leads, contacts and accounts
// — never anywhere else — and saves the card as a new lead, a new contact, an attachment
// to an existing record, or just a card. Record, link, timeline entry, follow-up and note
// are written in one transaction.
//
//	POST   /w/{code}/cards/match            who is this, in this business?
//	POST   /w/{code}/cards                  save (new card or an existing cardId)
//	GET    /w/{code}/cards                  my cards in this business (+ ?object=&recordId=)
//	GET    /w/{code}/cards/{id}             one card with what it's linked to
//	GET    /w/{code}/cards/{id}/image       the scanned picture (?side=front|back)
//	DELETE /w/{code}/cards/{id}/links/{linkId}

func (h *Handler) cardRoutes(r chi.Router) {
	r.Post("/cards/match", h.handleCardMatch)
	r.Post("/cards", h.handleCardSave)
	r.Get("/cards", h.handleCardList)
	r.Get("/cards/{id}", h.handleCardGet)
	r.Get("/cards/{id}/image", h.handleCardImage)
	r.Delete("/cards/{id}/links/{linkId}", h.handleCardUnlink)
}

type cardFields struct {
	PersonName  string   `json:"personName"`
	Designation string   `json:"designation"`
	Company     string   `json:"company"`
	Website     string   `json:"website"`
	Phones      []string `json:"phones"`
	Emails      []string `json:"emails"`
	Address     string   `json:"address"`
	GSTIN       string   `json:"gstin"`
	Notes       string   `json:"notes"`
	MetContext  string   `json:"metContext"`
	EventTag    string   `json:"eventTag"`
}

// clean trims the card and drops values a CRM field would refuse (cards come from OCR).
func (c *cardFields) clean() {
	c.PersonName, c.Designation, c.Company = clipText(c.PersonName, 150), clipText(c.Designation, 150), clipText(c.Company, 200)
	c.Notes, c.MetContext, c.EventTag, c.Address = clipText(c.Notes, 4000), clipText(c.MetContext, 2000), clipText(c.EventTag, 100), clipText(c.Address, 1000)
	c.GSTIN = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(c.GSTIN), " ", ""))
	if len(c.GSTIN) != 15 {
		c.GSTIN = ""
	}
	c.Website = strings.TrimSpace(c.Website)
	if c.Website != "" {
		s := c.Website
		if !strings.Contains(s, "://") {
			s = "https://" + s
		}
		u, err := url.Parse(s)
		if err != nil || u.Host == "" || !strings.Contains(u.Host, ".") || len(s) > 255 {
			c.Website = ""
		} else {
			c.Website = s
		}
	}
	phones := []string{}
	seen := map[string]bool{}
	for _, p := range c.Phones {
		p = cleanPhone(p)
		k := phoneKey(p)
		if p == "" || k == "" || seen[k] {
			continue
		}
		seen[k] = true
		phones = append(phones, p)
	}
	c.Phones = phones
	emails := []string{}
	for _, e := range c.Emails {
		e = strings.ToLower(strings.TrimSpace(e))
		addr, err := mail.ParseAddress(e)
		if err != nil || addr.Address != e || !strings.Contains(e[strings.LastIndex(e, "@"):], ".") || seen[e] || len(e) > 255 {
			continue
		}
		seen[e] = true
		emails = append(emails, e)
	}
	c.Emails = emails
}

func clipText(s string, max int) string {
	s = strings.TrimSpace(s)
	if len(s) > max {
		s = strings.ToValidUTF8(s[:max], "")
	}
	return s
}

// cleanPhone keeps what a phone field accepts: digits and "+-() .".
func cleanPhone(s string) string {
	var b strings.Builder
	digits := 0
	for _, r := range strings.TrimSpace(s) {
		switch {
		case r >= '0' && r <= '9':
			digits++
			b.WriteRune(r)
		case strings.ContainsRune("+-() .", r):
			b.WriteRune(r)
		}
	}
	if digits < 7 || digits > 15 {
		return ""
	}
	return strings.TrimSpace(b.String())
}

// phoneKey is what two spellings of one number share: the last 10 digits.
func phoneKey(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	d := b.String()
	if len(d) < 7 {
		return ""
	}
	if len(d) > 10 {
		d = d[len(d)-10:]
	}
	return d
}

const phoneKeySQL = `right(regexp_replace(COALESCE(%s, ''), '\D', '', 'g'), 10)`

func phoneExpr(col string) string { return strings.Replace(phoneKeySQL, "%s", col, 1) }

// ---- matching ----

type CardMatch struct {
	Object    string   `json:"object"`
	ID        string   `json:"id"`
	Code      string   `json:"code"`
	Title     string   `json:"title"`
	Subtitle  string   `json:"subtitle,omitempty"`
	Status    string   `json:"status,omitempty"`
	MatchedOn []string `json:"matchedOn"`
}

type cardMatches struct {
	Matches []CardMatch `json:"matches"`
	// Hidden: records of this business that match but that the caller may not open
	// (someone else's records, or an object they can't read). Only a count is given.
	Hidden int `json:"hidden"`
	// Suggested: what the save screen should offer first.
	Suggested string          `json:"suggested"`
	Can       map[string]bool `json:"can"`
}

// matchCard looks for the card's person and company among one business's records.
func (h *Handler) matchCard(ctx context.Context, q querier, sc *Scope, me uuid.UUID, c cardFields) (*cardMatches, error) {
	out := &cardMatches{Matches: []CardMatch{}, Suggested: "lead", Can: map[string]bool{
		"lead":    sc.Enabled("leads") && sc.Can("leads", "create"),
		"contact": sc.Enabled("contacts") && sc.Can("contacts", "create"),
		"account": sc.Enabled("accounts") && sc.Can("accounts", "create"),
	}}
	keys := []string{}
	for _, p := range c.Phones {
		if k := phoneKey(p); k != "" {
			keys = append(keys, k)
		}
	}
	emails := append([]string{}, c.Emails...)
	company := strings.ToLower(strings.TrimSpace(c.Company))

	type src struct {
		object, title, subtitle, status, byPhone, byEmail, byName string
	}
	people := func(object, table, subtitle, status string) src {
		_ = table
		return src{object: object,
			title:    `COALESCE(NULLIF(trim(concat_ws(' ', t.first_name, t.last_name)), ''), t.email, t.code)`,
			subtitle: subtitle, status: status,
			byPhone: `(` + phoneExpr("t.phone") + ` = ANY($2::text[]) OR ` + phoneExpr("t.mobile") + ` = ANY($2::text[]))`,
			byEmail: `(lower(t.email) = ANY($3::text[]))`, byName: `($4::text IS NULL)`}
	}
	sources := []src{
		people("leads", "leads", `COALESCE(t.organization, '')`, `t.status`),
		people("contacts", "contacts", `COALESCE((SELECT a.name FROM crm.accounts a WHERE a.id = t.account_id), '')`, `''`),
		{object: "accounts", title: `t.name`, subtitle: `COALESCE(t.industry, '')`, status: `t.lifecycle`,
			byPhone: `(` + phoneExpr("t.phone") + ` = ANY($2::text[]))`, byEmail: `(lower(t.email) = ANY($3::text[]))`,
			byName: `($4::text <> '' AND lower(trim(t.name)) = $4::text)`},
	}
	for _, s := range sources {
		if !sc.Enabled(s.object) {
			continue
		}
		if len(keys) == 0 && len(emails) == 0 && (s.object != "accounts" || company == "") {
			continue
		}
		readable := sc.Can(s.object, "read")
		owners := sc.OwnersFor(s.object, me)
		rows, err := q.Query(ctx, `
			SELECT t.id::text, t.code, `+s.title+`, `+s.subtitle+`, `+s.status+`,
			       `+s.byPhone+`, `+s.byEmail+`, `+s.byName+`,
			       ($5::uuid[] IS NULL OR t.owner_id = ANY($5::uuid[]))
			FROM crm.`+s.object+` t
			WHERE t.workspace_id = $1 AND t.deleted_at IS NULL AND (`+s.byPhone+` OR `+s.byEmail+` OR `+s.byName+`)
			ORDER BY t.created_at DESC LIMIT 20`, sc.WS, keys, emails, company, owners)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			m := CardMatch{Object: s.object, MatchedOn: []string{}}
			var byPhone, byEmail, byName, visible bool
			if err := rows.Scan(&m.ID, &m.Code, &m.Title, &m.Subtitle, &m.Status, &byPhone, &byEmail, &byName, &visible); err != nil {
				rows.Close()
				return nil, err
			}
			if !readable || !visible {
				out.Hidden++
				continue
			}
			if byPhone {
				m.MatchedOn = append(m.MatchedOn, "phone")
			}
			if byEmail {
				m.MatchedOn = append(m.MatchedOn, "email")
			}
			if byName {
				m.MatchedOn = append(m.MatchedOn, "company")
			}
			out.Matches = append(out.Matches, m)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	for _, m := range out.Matches {
		if m.Object != "accounts" || len(m.MatchedOn) > 1 || (len(m.MatchedOn) == 1 && m.MatchedOn[0] != "company") {
			out.Suggested = "attach"
			break
		}
		// Only the company is known: the person is new, and belongs to that account.
		out.Suggested = "contact"
	}
	return out, nil
}

func (h *Handler) handleCardMatch(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	var in cardFields
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	in.clean()
	out, err := h.matchCard(r.Context(), h.store.Pool, sc, actor(r), in)
	respond(w, r, http.StatusOK, out, err)
}

// ---- saving ----

type cardTarget struct {
	Object string `json:"object"`
	ID     string `json:"id"`
}

type saveCardInput struct {
	// CardID: a card already in the caller's vault. Empty = a new card from Card.
	CardID string     `json:"cardId"`
	Card   cardFields `json:"card"`
	// Action: lead | contact | attach | card_only.
	Action string      `json:"action"`
	Target *cardTarget `json:"target"`
	// Contact: the account to put the contact under, or create one from the card's company.
	AccountID     string `json:"accountId"`
	CreateAccount bool   `json:"createAccount"`
	// AllowDuplicate: create a new lead/contact even though one with this phone or email exists.
	AllowDuplicate bool `json:"allowDuplicate"`
	// FollowUpAt (RFC 3339): a lead's next follow-up; for other records a task due that day.
	FollowUpAt string `json:"followUpAt"`
	Note       string `json:"note"`
	// Values: what the person changed on the review screen (field key → value).
	Values map[string]any `json:"values"`
}

type CardLink struct {
	ID     string `json:"id"`
	Object string `json:"object"`
	Record string `json:"recordId"`
	Code   string `json:"code"`
	Title  string `json:"title"`
}

type Card struct {
	ID          string     `json:"id"`
	PersonName  string     `json:"personName"`
	Designation string     `json:"designation"`
	Company     string     `json:"company"`
	Website     string     `json:"website"`
	Phones      []string   `json:"phones"`
	Emails      []string   `json:"emails"`
	Address     string     `json:"address"`
	GSTIN       string     `json:"gstin"`
	Notes       string     `json:"notes"`
	MetContext  string     `json:"metContext"`
	EventTag    string     `json:"eventTag"`
	HasImage    bool       `json:"hasImage"`
	HasBack     bool       `json:"hasBack"`
	Mine        bool       `json:"mine"`
	SavedBy     string     `json:"savedBy,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	Links       []CardLink `json:"links"`
}

type saveCardResult struct {
	Card    *Card      `json:"card"`
	Record  *CardLink  `json:"record,omitempty"`
	Account *CardLink  `json:"account,omitempty"`
	Created bool       `json:"created"`
	Task    *RecordRef `json:"task,omitempty"`
}

type RecordRef struct {
	ID    string `json:"id"`
	Code  string `json:"code"`
	Title string `json:"title"`
}

var errNoCards = shared.NewError(http.StatusNotFound, "cards_unavailable", "Business cards aren't available on this server.")

func (h *Handler) cardsReady(ctx context.Context) bool {
	var ok bool
	_ = h.store.Pool.QueryRow(ctx, `SELECT to_regclass('public.saved_cards') IS NOT NULL`).Scan(&ok)
	return ok
}

func splitName(full string) (first, last string) {
	parts := strings.Fields(full)
	switch len(parts) {
	case 0:
		return "", ""
	case 1:
		return "", parts[0]
	}
	return strings.Join(parts[:len(parts)-1], " "), parts[len(parts)-1]
}

func cardDescription(c cardFields) string {
	lines := []string{}
	if c.GSTIN != "" {
		lines = append(lines, "GSTIN: "+c.GSTIN)
	}
	if c.MetContext != "" {
		lines = append(lines, "Met: "+c.MetContext)
	}
	if c.EventTag != "" {
		lines = append(lines, "Event: "+c.EventTag)
	}
	if c.Notes != "" {
		lines = append(lines, c.Notes)
	}
	return strings.Join(lines, "\n")
}

func at(list []string, i int) string {
	if i < len(list) {
		return list[i]
	}
	return ""
}

func (h *Handler) handleCardSave(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !h.cardsReady(r.Context()) {
		shared.WriteError(w, r, errNoCards)
		return
	}
	var in saveCardInput
	if err := shared.DecodeJSONLimit(w, r, &in, 1<<20); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	in.Card.clean()
	in.Action = strings.ToLower(strings.TrimSpace(in.Action))
	fe := map[string]string{}
	need := map[string][2]string{"lead": {"leads", "create"}, "contact": {"contacts", "create"}}
	switch in.Action {
	case "lead", "contact":
		p := need[in.Action]
		if !sc.Enabled(p[0]) || !sc.Can(p[0], p[1]) {
			shared.WriteError(w, r, errForbidden)
			return
		}
	case "attach":
		if in.Target == nil || !map[string]bool{"leads": true, "contacts": true, "accounts": true}[in.Target.Object] {
			fe["target"] = "Choose the lead, contact or account to attach this card to."
		} else if !sc.Enabled(in.Target.Object) || !sc.Can(in.Target.Object, "update") {
			shared.WriteError(w, r, errForbidden)
			return
		}
	case "card_only":
	default:
		fe["action"] = "Choose lead, contact, attach or card_only."
	}
	var followUp *time.Time
	if s := strings.TrimSpace(in.FollowUpAt); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			fe["followUpAt"] = "Enter a date and time."
		} else {
			followUp = &t
		}
	}
	if in.CardID == "" && in.Card.PersonName == "" && in.Card.Company == "" && len(in.Card.Phones) == 0 && len(in.Card.Emails) == 0 {
		fe["card"] = "The card needs a name, a company, a phone number or an email."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	status, resp, err := shared.Idempotent(r.Context(), h.store.Pool, actor(r), r.Header.Get("Idempotency-Key"),
		map[string]any{"ws": sc.WS, "body": in}, func() (int, any, error) {
			res, err := h.saveCard(r, sc, in, followUp)
			if err != nil {
				return 0, nil, err
			}
			return http.StatusCreated, res, nil
		})
	if err == nil {
		h.bus.Kick()
	}
	respond(w, r, status, resp, err)
}

func (h *Handler) saveCard(r *http.Request, sc *Scope, in saveCardInput, followUp *time.Time) (*saveCardResult, error) {
	ctx := r.Context()
	me := actor(r)
	a := actorFromRequest(r, "card_scan")
	res := &saveCardResult{}
	var cardID uuid.UUID

	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		c := in.Card
		if in.CardID != "" {
			id, err := uuid.Parse(in.CardID)
			if err != nil {
				return shared.NotFound("card_not_found")
			}
			loaded, err := loadOwnCard(ctx, tx, id, me)
			if err != nil {
				return err
			}
			cardID = id
			// The card's own details are the base; anything sent with the request wins.
			c = mergeCard(*loaded, in.Card)
			if _, err := tx.Exec(ctx, `UPDATE public.saved_cards SET workspace_id = COALESCE(workspace_id, $2), identity_id = COALESCE(identity_id, $3) WHERE id = $1`,
				id, sc.WS, me); err != nil {
				return err
			}
		} else {
			if err := plans.CountCardScan(ctx, tx, sc.WS); err != nil {
				return err
			}
			cardID = uuid.New()
			var userID *uuid.UUID
			var uid uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT id FROM public.users WHERE identity_id = $1 LIMIT 1`, me).Scan(&uid); err == nil {
				userID = &uid
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO public.saved_cards (id, user_id, identity_id, workspace_id, person_name, designation, company, website,
				                                notes, met_context, event_tag, contact_type, extract_status, gstin, source)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NULLIF($11, ''), 'business', 'extracted', NULLIF($12, ''), 'SCANNED')`,
				cardID, userID, me, sc.WS, c.PersonName, c.Designation, c.Company, c.Website, c.Notes, c.MetContext, c.EventTag, c.GSTIN); err != nil {
				return err
			}
			for _, p := range c.Phones {
				e164 := ""
				if k := phoneKey(p); len(k) == 10 {
					e164 = "+91" + k
					if strings.HasPrefix(strings.TrimSpace(p), "+") {
						e164 = "+" + allDigits(p)
					}
				}
				if _, err := tx.Exec(ctx, `INSERT INTO public.saved_card_phones (id, saved_card_id, raw_phone, phone_e164, phone_type, is_whatsapp)
					VALUES ($1, $2, $3, NULLIF($4, ''), 'work', false)`, uuid.New(), cardID, p, e164); err != nil {
					return err
				}
			}
			for _, e := range c.Emails {
				if _, err := tx.Exec(ctx, `INSERT INTO public.saved_card_emails (id, saved_card_id, email) VALUES ($1, $2, $3)`, uuid.New(), cardID, e); err != nil {
					return err
				}
			}
			if c.Address != "" {
				if _, err := tx.Exec(ctx, `INSERT INTO public.saved_card_addresses (saved_card_id, raw_address) VALUES ($1, $2)
					ON CONFLICT (saved_card_id) DO UPDATE SET raw_address = EXCLUDED.raw_address`, cardID, c.Address); err != nil {
					return err
				}
			}
		}

		if in.Action == "lead" || in.Action == "contact" {
			if !in.AllowDuplicate {
				person := c
				person.Company = "" // a company name alone doesn't make a duplicate person
				m, err := h.matchCard(ctx, tx, sc, me, person)
				if err != nil {
					return err
				}
				dup := false
				for _, x := range m.Matches {
					if x.Object != "accounts" {
						dup = true
					}
				}
				if dup || m.Hidden > 0 {
					return &shared.Error{Status: http.StatusConflict, Code: "possible_duplicate",
						Message: "Someone with this phone number or email is already in this business.",
						Details: map[string]any{"matches": m.Matches, "hidden": m.Hidden}}
				}
			}
		}

		first, last := splitName(c.PersonName)
		if last == "" {
			last = c.Company
		}
		if last == "" {
			last = at(c.Phones, 0)
		}
		if last == "" {
			last = at(c.Emails, 0)
		}
		link := func(object string, row *Row) (*CardLink, error) {
			l := &CardLink{Object: object, Record: row.ID, Code: row.Code, Title: row.Title}
			id, _ := uuid.Parse(row.ID)
			err := tx.QueryRow(ctx, `
				INSERT INTO crm.card_links (workspace_id, card_id, object_key, record_id, created_by) VALUES ($1, $2, $3, $4, $5)
				ON CONFLICT (workspace_id, card_id, object_key, record_id) DO UPDATE SET created_by = crm.card_links.created_by
				RETURNING id::text`, sc.WS, cardID, object, id, me).Scan(&l.ID)
			if err != nil {
				return nil, err
			}
			detail := map[string]any{"cardId": cardID.String(), "company": c.Company, "person": c.PersonName}
			return l, insertActivity(ctx, tx, sc.WS, object, id, "card.scanned", "Business card scanned", detail, a.ID)
		}
		withOverrides := func(values map[string]any) map[string]any {
			for k, v := range in.Values {
				values[k] = v
			}
			for k, v := range values {
				if s, ok := v.(string); ok && s == "" {
					delete(values, k)
				}
			}
			return values
		}

		var target *Row
		var targetSpec *objectSpec
		switch in.Action {
		case "lead":
			values := withOverrides(map[string]any{
				"firstName": first, "lastName": last, "title": c.Designation, "organization": c.Company,
				"email": at(c.Emails, 0), "phone": at(c.Phones, 0), "mobile": at(c.Phones, 1), "website": c.Website,
				"source": "business_card", "street": clipText(c.Address, 255), "description": cardDescription(c),
			})
			if followUp != nil {
				values["nextFollowUpAt"] = followUp.UTC().Format(time.RFC3339)
			}
			row, err := h.createRecord(ctx, tx, sc.WS, &leadSpec, a, values)
			if err != nil {
				return err
			}
			target, targetSpec, res.Created = row, &leadSpec, true
		case "contact":
			values := withOverrides(map[string]any{
				"firstName": first, "lastName": last, "title": c.Designation,
				"email": at(c.Emails, 0), "phone": at(c.Phones, 0), "mobile": at(c.Phones, 1),
				"leadSource": "business_card", "mailingStreet": clipText(c.Address, 255), "description": cardDescription(c),
			})
			if in.AccountID != "" {
				accID, err := uuid.Parse(in.AccountID)
				if err != nil {
					return shared.Validation(map[string]string{"accountId": "Choose an account."})
				}
				acc, _, err := h.getRow(ctx, tx, sc.WS, &accountSpec, accID, sc.OwnersFor("accounts", me))
				if err != nil {
					return shared.Validation(map[string]string{"accountId": "Choose an account."})
				}
				values["accountId"] = acc.ID
				if res.Account, err = link("accounts", acc); err != nil {
					return err
				}
			} else if in.CreateAccount && c.Company != "" {
				if !sc.Enabled("accounts") || !sc.Can("accounts", "create") {
					return errForbidden
				}
				// An account with this exact name the caller can see is reused, not doubled.
				var existing uuid.UUID
				err := tx.QueryRow(ctx, `SELECT id FROM crm.accounts WHERE workspace_id = $1 AND deleted_at IS NULL AND lower(trim(name)) = lower($2)
					AND ($3::uuid[] IS NULL OR owner_id = ANY($3::uuid[])) ORDER BY created_at LIMIT 1`, sc.WS, c.Company, sc.OwnersFor("accounts", me)).Scan(&existing)
				var acc *Row
				switch {
				case err == nil:
					if acc, _, err = h.getRow(ctx, tx, sc.WS, &accountSpec, existing, nil); err != nil {
						return err
					}
				case errors.Is(err, pgx.ErrNoRows):
					accValues := map[string]any{"name": c.Company, "kind": "business", "type": "prospect", "website": c.Website,
						"billingStreet": clipText(c.Address, 255)}
					if c.GSTIN != "" {
						accValues["description"] = "GSTIN: " + c.GSTIN
					}
					for k, v := range accValues {
						if s, ok := v.(string); ok && s == "" {
							delete(accValues, k)
						}
					}
					if acc, err = h.createRecord(ctx, tx, sc.WS, &accountSpec, a, accValues); err != nil {
						return err
					}
				default:
					return err
				}
				values["accountId"] = acc.ID
				if res.Account, err = link("accounts", acc); err != nil {
					return err
				}
			}
			row, err := h.createRecord(ctx, tx, sc.WS, &contactSpec, a, values)
			if err != nil {
				return err
			}
			target, targetSpec, res.Created = row, &contactSpec, true
		case "attach":
			targetSpec = specs[in.Target.Object]
			id, err := uuid.Parse(in.Target.ID)
			if err != nil {
				return shared.NotFound("record_not_found")
			}
			if target, _, err = h.getRow(ctx, tx, sc.WS, targetSpec, id, sc.OwnersFor(targetSpec.Key, me)); err != nil {
				return err
			}
			// Fill what the record doesn't have yet from the card; never overwrite.
			fill := map[string]any{}
			put := func(key, v string) {
				if v == "" {
					return
				}
				if cur, _ := target.Values[key].(string); cur == "" {
					fill[key] = v
				}
			}
			switch targetSpec.Key {
			case "leads":
				put("email", at(c.Emails, 0))
				put("phone", at(c.Phones, 0))
				put("title", c.Designation)
				put("organization", c.Company)
				put("website", c.Website)
			case "contacts":
				put("email", at(c.Emails, 0))
				put("phone", at(c.Phones, 0))
				put("title", c.Designation)
			case "accounts":
				put("phone", at(c.Phones, 0))
				put("email", at(c.Emails, 0))
				put("website", c.Website)
			}
			if followUp != nil && targetSpec.Key == "leads" {
				fill["nextFollowUpAt"] = followUp.UTC().Format(time.RFC3339)
			}
			if len(fill) > 0 {
				if target, err = h.updateValues(ctx, tx, sc.WS, targetSpec, id, a, fill, nil, sc.OwnersFor(targetSpec.Key, me)); err != nil {
					return err
				}
			}
		}

		if target != nil {
			l, err := link(targetSpec.Key, target)
			if err != nil {
				return err
			}
			res.Record = l
			targetID, _ := uuid.Parse(target.ID)
			if note := clipText(in.Note, 4000); note != "" {
				if err := insertActivity(ctx, tx, sc.WS, targetSpec.Key, targetID, "note", "Note", map[string]any{"body": note}, a.ID); err != nil {
					return err
				}
			}
			// A follow-up on a contact or account is a task (a lead carries its own date).
			if followUp != nil && targetSpec.Key != "leads" {
				if ts := specFor("tasks"); ts != nil && sc.Enabled("tasks") && sc.Can("tasks", "create") {
					tv := map[string]any{"name": "Follow up with " + target.Title, "dueDate": followUp.UTC().Format("2006-01-02"), "status": "not_started"}
					tv[map[string]string{"contacts": "contactId", "accounts": "accountId"}[targetSpec.Key]] = target.ID
					task, err := h.createRecord(ctx, tx, sc.WS, ts, a, tv)
					if err != nil {
						return err
					}
					res.Task = &RecordRef{ID: task.ID, Code: task.Code, Title: task.Title}
				}
			}
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "card.saved", "card", &cardID, nil, map[string]any{"action": in.Action}))
	})
	if err != nil {
		return nil, err
	}
	card, err := h.loadCard(ctx, sc, me, cardID)
	if err != nil {
		return nil, err
	}
	res.Card = card
	return res, nil
}

func allDigits(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func mergeCard(base, over cardFields) cardFields {
	pick := func(a, b string) string {
		if strings.TrimSpace(b) != "" {
			return b
		}
		return a
	}
	out := base
	out.PersonName, out.Designation, out.Company = pick(base.PersonName, over.PersonName), pick(base.Designation, over.Designation), pick(base.Company, over.Company)
	out.Website, out.Address, out.GSTIN = pick(base.Website, over.Website), pick(base.Address, over.Address), pick(base.GSTIN, over.GSTIN)
	out.Notes, out.MetContext, out.EventTag = pick(base.Notes, over.Notes), pick(base.MetContext, over.MetContext), pick(base.EventTag, over.EventTag)
	if len(over.Phones) > 0 {
		out.Phones = over.Phones
	}
	if len(over.Emails) > 0 {
		out.Emails = over.Emails
	}
	out.clean()
	return out
}

// ownCardSQL: a card is the caller's when they saved it (by identity, or by the app
// profile that belongs to their identity).
const ownCardSQL = `(c.identity_id = $2 OR c.user_id IN (SELECT u.id FROM public.users u WHERE u.identity_id = $2))`

func loadOwnCard(ctx context.Context, q querier, id, me uuid.UUID) (*cardFields, error) {
	var c cardFields
	err := q.QueryRow(ctx, `
		SELECT COALESCE(c.person_name, ''), COALESCE(c.designation, ''), COALESCE(c.company, ''), COALESCE(c.website, ''),
		       COALESCE(c.notes, ''), COALESCE(c.met_context, ''), COALESCE(c.event_tag, ''), COALESCE(trim(c.gstin), ''),
		       COALESCE((SELECT a.raw_address FROM public.saved_card_addresses a WHERE a.saved_card_id = c.id), ''),
		       COALESCE((SELECT array_agg(p.raw_phone ORDER BY p.created_at) FROM public.saved_card_phones p WHERE p.saved_card_id = c.id), '{}'),
		       COALESCE((SELECT array_agg(e.email ORDER BY e.created_at) FROM public.saved_card_emails e WHERE e.saved_card_id = c.id), '{}')
		FROM public.saved_cards c WHERE c.id = $1 AND c.deleted_at IS NULL AND `+ownCardSQL, id, me).
		Scan(&c.PersonName, &c.Designation, &c.Company, &c.Website, &c.Notes, &c.MetContext, &c.EventTag, &c.GSTIN, &c.Address, &c.Phones, &c.Emails)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NotFound("card_not_found")
	}
	if err != nil {
		return nil, err
	}
	c.Notes = stripGSTNote(c.Notes)
	c.clean()
	return &c, nil
}

// stripGSTNote removes the "__GST__:<gstin>" line the app keeps at the top of a card's notes.
func stripGSTNote(s string) string {
	if strings.HasPrefix(s, "__GST__:") {
		if i := strings.Index(s, "\n"); i >= 0 {
			return s[i+1:]
		}
		return ""
	}
	return s
}

// ---- reading ----

const cardSelect = `
	SELECT c.id::text, COALESCE(c.person_name, ''), COALESCE(c.designation, ''), COALESCE(c.company, ''), COALESCE(c.website, ''),
	       COALESCE(c.notes, ''), COALESCE(c.met_context, ''), COALESCE(c.event_tag, ''), COALESCE(trim(c.gstin), ''),
	       COALESCE((SELECT a.raw_address FROM public.saved_card_addresses a WHERE a.saved_card_id = c.id), ''),
	       COALESCE((SELECT array_agg(p.raw_phone ORDER BY p.created_at) FROM public.saved_card_phones p WHERE p.saved_card_id = c.id), '{}'),
	       COALESCE((SELECT array_agg(e.email ORDER BY e.created_at) FROM public.saved_card_emails e WHERE e.saved_card_id = c.id), '{}'),
	       EXISTS (SELECT 1 FROM public.saved_card_images i WHERE i.saved_card_id = c.id AND i.side::text = 'front'),
	       EXISTS (SELECT 1 FROM public.saved_card_images i WHERE i.saved_card_id = c.id AND i.side::text = 'back'),
	       ` + ownCardSQL + `,
	       COALESCE((SELECT i.display_name FROM crm.identities i WHERE i.id = COALESCE(c.identity_id, (SELECT u.identity_id FROM public.users u WHERE u.id = c.user_id))), ''),
	       c.created_at
	FROM public.saved_cards c`

func scanCard(row pgx.Row) (*Card, error) {
	c := &Card{Links: []CardLink{}}
	err := row.Scan(&c.ID, &c.PersonName, &c.Designation, &c.Company, &c.Website, &c.Notes, &c.MetContext, &c.EventTag, &c.GSTIN,
		&c.Address, &c.Phones, &c.Emails, &c.HasImage, &c.HasBack, &c.Mine, &c.SavedBy, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	c.Notes = stripGSTNote(c.Notes)
	return c, nil
}

// cardLinks loads what cards are linked to in this business, keeping only records the
// caller may read.
func (h *Handler) cardLinks(ctx context.Context, sc *Scope, me uuid.UUID, cards []*Card) error {
	if len(cards) == 0 {
		return nil
	}
	byID := map[string]*Card{}
	ids := make([]string, len(cards))
	for i, c := range cards {
		byID[c.ID], ids[i] = c, c.ID
	}
	for _, object := range []string{"leads", "contacts", "accounts"} {
		if !sc.Enabled(object) || !sc.Can(object, "read") {
			continue
		}
		spec := specs[object]
		rows, err := h.store.Pool.Query(ctx, `
			SELECT l.id::text, l.card_id::text, t.id::text, t.code, `+spec.TitleSQL+`
			FROM crm.card_links l JOIN crm.`+spec.Table+` t ON t.id = l.record_id AND t.workspace_id = l.workspace_id AND t.deleted_at IS NULL
			WHERE l.workspace_id = $1 AND l.object_key = $2 AND l.card_id = ANY($3::uuid[])
			  AND ($4::uuid[] IS NULL OR t.owner_id = ANY($4::uuid[]))
			ORDER BY l.created_at`, sc.WS, object, ids, sc.OwnersFor(object, me))
		if err != nil {
			return err
		}
		for rows.Next() {
			l := CardLink{Object: object}
			var cardID string
			if err := rows.Scan(&l.ID, &cardID, &l.Record, &l.Code, &l.Title); err != nil {
				rows.Close()
				return err
			}
			byID[cardID].Links = append(byID[cardID].Links, l)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

// visibleCardSQL: in a business, a member sees the cards they saved there and the cards
// linked to records of that business ($1 = workspace, $2 = caller). Links to records the
// caller can't read are dropped afterwards.
const visibleCardSQL = `c.deleted_at IS NULL AND (
	(c.workspace_id = $1 AND ` + ownCardSQL + `)
	OR EXISTS (SELECT 1 FROM crm.card_links l WHERE l.card_id = c.id AND l.workspace_id = $1))`

func (h *Handler) loadCard(ctx context.Context, sc *Scope, me, id uuid.UUID) (*Card, error) {
	c, err := scanCard(h.store.Pool.QueryRow(ctx, cardSelect+` WHERE c.id = $3 AND `+visibleCardSQL, sc.WS, me, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NotFound("card_not_found")
	}
	if err != nil {
		return nil, err
	}
	if err := h.cardLinks(ctx, sc, me, []*Card{c}); err != nil {
		return nil, err
	}
	// Someone else's card is only visible through a record the caller can read.
	if !c.Mine && len(c.Links) == 0 {
		return nil, shared.NotFound("card_not_found")
	}
	return c, nil
}

func (h *Handler) handleCardGet(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || !h.cardsReady(r.Context()) {
		shared.WriteError(w, r, shared.NotFound("card_not_found"))
		return
	}
	c, err := h.loadCard(r.Context(), sc, actor(r), id)
	respond(w, r, http.StatusOK, c, err)
}

func (h *Handler) handleCardList(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	ctx := r.Context()
	me := actor(r)
	out := struct {
		Items []*Card `json:"items"`
		Total int     `json:"total"`
	}{Items: []*Card{}}
	if !h.cardsReady(ctx) {
		shared.WriteJSON(w, http.StatusOK, out)
		return
	}
	q := r.URL.Query()
	args := []any{sc.WS, me}
	where := `c.deleted_at IS NULL AND c.workspace_id = $1 AND ` + ownCardSQL
	if object, rec := q.Get("object"), q.Get("recordId"); object != "" || rec != "" {
		// Cards on one record: the caller must be able to open that record.
		spec := specs[object]
		recID, err := uuid.Parse(rec)
		if spec == nil || err != nil {
			shared.WriteError(w, r, shared.NotFound("record_not_found"))
			return
		}
		if !sc.Enabled(object) || !sc.Can(object, "read") {
			shared.WriteError(w, r, errForbidden)
			return
		}
		if _, _, err := h.getRow(ctx, h.store.Pool, sc.WS, spec, recID, sc.OwnersFor(object, me)); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		args = append(args, object, recID)
		where = `c.deleted_at IS NULL AND EXISTS (SELECT 1 FROM crm.card_links l WHERE l.card_id = c.id AND l.workspace_id = $1 AND l.object_key = $3 AND l.record_id = $4)`
	}
	if s := strings.TrimSpace(q.Get("q")); s != "" {
		args = append(args, "%"+strings.ToLower(s)+"%")
		n := "$" + strconv.Itoa(len(args))
		where += ` AND (lower(COALESCE(c.person_name, '')) LIKE ` + n + ` OR lower(COALESCE(c.company, '')) LIKE ` + n + `
			OR EXISTS (SELECT 1 FROM public.saved_card_phones p WHERE p.saved_card_id = c.id AND p.raw_phone LIKE ` + n + `)
			OR EXISTS (SELECT 1 FROM public.saved_card_emails e WHERE e.saved_card_id = c.id AND lower(e.email) LIKE ` + n + `))`
	}
	// $2 is used by ownCardSQL inside cardSelect even when the filter doesn't need it.
	if err := h.store.Pool.QueryRow(ctx, `SELECT count(*) FROM public.saved_cards c WHERE ($2::uuid IS NOT NULL) AND `+where, args...).Scan(&out.Total); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	rows, err := h.store.Pool.Query(ctx, cardSelect+` WHERE `+where+` ORDER BY c.created_at DESC LIMIT 200`, args...)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	for rows.Next() {
		c, err := scanCard(rows)
		if err != nil {
			rows.Close()
			shared.WriteError(w, r, err)
			return
		}
		out.Items = append(out.Items, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if err := h.cardLinks(ctx, sc, me, out.Items); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) handleCardImage(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil || !h.cardsReady(r.Context()) {
		shared.WriteError(w, r, shared.NotFound("card_not_found"))
		return
	}
	if _, err := h.loadCard(r.Context(), sc, actor(r), id); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	side := "front"
	if r.URL.Query().Get("side") == "back" {
		side = "back"
	}
	var data []byte
	var contentType string
	err = h.store.Pool.QueryRow(r.Context(), `
		SELECT image_data, COALESCE(NULLIF(content_type, ''), 'image/jpeg') FROM public.saved_card_images
		WHERE saved_card_id = $1 AND side::text = $2 AND image_data IS NOT NULL ORDER BY created_at DESC LIMIT 1`, id, side).Scan(&data, &contentType)
	if err != nil || len(data) == 0 {
		shared.WriteError(w, r, shared.NotFound("image_not_found"))
		return
	}
	if !strings.HasPrefix(contentType, "image/") {
		contentType = "image/jpeg"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(data)
}

// handleCardUnlink removes a card from a record (the card and the record both stay).
func (h *Handler) handleCardUnlink(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	ctx := r.Context()
	me := actor(r)
	cardID, err1 := uuid.Parse(chi.URLParam(r, "id"))
	linkID, err2 := uuid.Parse(chi.URLParam(r, "linkId"))
	if err1 != nil || err2 != nil {
		shared.WriteError(w, r, shared.NotFound("link_not_found"))
		return
	}
	var object string
	var recID uuid.UUID
	err := h.store.Pool.QueryRow(ctx, `SELECT object_key, record_id FROM crm.card_links WHERE id = $1 AND card_id = $2 AND workspace_id = $3`,
		linkID, cardID, sc.WS).Scan(&object, &recID)
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("link_not_found"))
		return
	}
	if !sc.Enabled(object) || !sc.Can(object, "update") {
		shared.WriteError(w, r, errForbidden)
		return
	}
	if _, _, err := h.getRow(ctx, h.store.Pool, sc.WS, specs[object], recID, sc.OwnersFor(object, me)); err != nil {
		shared.WriteError(w, r, shared.NotFound("link_not_found"))
		return
	}
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM crm.card_links WHERE id = $1 AND workspace_id = $2`, linkID, sc.WS); err != nil {
			return err
		}
		if err := insertActivity(ctx, tx, sc.WS, object, recID, "card.removed", "Business card removed", map[string]any{"cardId": cardID.String()}, a.ID); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, a.audit(sc.WS, "card.unlinked", "card", &cardID, map[string]any{"object": object, "recordId": recID.String()}, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// relatedCards is the "Business cards" list on a lead, contact or account.
func (h *Handler) relatedCards(ctx context.Context, ws uuid.UUID, object, recordID string) (RelatedList, bool, error) {
	if specs[object] == nil || !h.cardsReady(ctx) {
		return RelatedList{}, false, nil
	}
	l, err := h.relatedList(ctx, "cards", "Business cards", "cards", `
		SELECT c.id::text, '', COALESCE(NULLIF(c.person_name, ''), NULLIF(c.company, ''), 'Business card'),
		       COALESCE(NULLIF(concat_ws(' · ', NULLIF(c.designation, ''), NULLIF(c.company, '')), ''), ''), ''
		FROM crm.card_links l JOIN public.saved_cards c ON c.id = l.card_id AND c.deleted_at IS NULL
		WHERE l.record_id = $1 AND l.workspace_id = $2 AND l.object_key = '`+object+`' ORDER BY l.created_at DESC LIMIT 50`, recordID, ws)
	return l, err == nil, err
}
