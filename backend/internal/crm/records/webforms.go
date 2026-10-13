package records

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"
	"sync"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Web-to-lead forms (D-146). A business makes a form, gets a link (and a snippet to
// embed it), and every submission becomes a lead owned by the person the form names.
//
// What the public may send is fixed by the form: name and phone always, plus the
// optional fields the business ticked. Nothing a visitor sends can choose the owner,
// the business or any other field. A hidden field catches simple bots and one address
// can send only a few submissions in ten minutes.
//
// The same mobile sending again does not make a second lead: the enquiry is added to
// the lead already there, so the owner sees the customer came back.

var webFormFields = []struct{ Key, Label, Type string }{
	{"email", "Email", "email"}, {"company", "Company", "text"}, {"city", "City", "text"},
	{"interest", "What are you interested in?", "text"}, {"message", "Message", "textarea"},
}

type WebForm struct {
	ID               uuid.UUID  `json:"id"`
	Name             string     `json:"name"`
	Heading          string     `json:"heading"`
	Intro            string     `json:"intro"`
	Fields           []string   `json:"fields"`
	OwnerID          uuid.UUID  `json:"ownerId"`
	OwnerName        string     `json:"ownerName"`
	ConsentText      string     `json:"consentText"`
	ThankYou         string     `json:"thankYou"`
	Active           bool       `json:"active"`
	Submissions      int        `json:"submissions"`
	LastSubmissionAt *time.Time `json:"lastSubmissionAt,omitempty"`
	URL              string     `json:"url"`
	Embed            string     `json:"embed"`
	token            string
	ws               uuid.UUID
}

func (h *Handler) webFormRoutes(r chi.Router) {
	r.Get("/web-forms", h.handleListWebForms)
	r.Post("/web-forms", h.handleSaveWebForm)
	r.Patch("/web-forms/{formId}", h.handleSaveWebForm)
	r.Delete("/web-forms/{formId}", h.handleDeleteWebForm)
}

func (h *Handler) formURL(token string) string {
	return strings.TrimRight(h.cfg.BaseURL, "/") + "/api/crm/v1/public/forms/" + token
}

const webFormCols = `f.id, f.name, f.heading, f.intro, f.fields, f.owner_id, COALESCE(i.display_name, ''), f.consent_text, f.thank_you,
	f.is_active, f.submissions, f.last_submission_at, f.token, f.workspace_id`

func (h *Handler) scanWebForm(row pgx.Row) (*WebForm, error) {
	f := &WebForm{}
	if err := row.Scan(&f.ID, &f.Name, &f.Heading, &f.Intro, &f.Fields, &f.OwnerID, &f.OwnerName, &f.ConsentText, &f.ThankYou,
		&f.Active, &f.Submissions, &f.LastSubmissionAt, &f.token, &f.ws); err != nil {
		return nil, err
	}
	if f.Fields == nil {
		f.Fields = []string{}
	}
	f.URL = h.formURL(f.token)
	f.Embed = `<iframe src="` + f.URL + `" style="width:100%;max-width:480px;height:640px;border:0" title="` + html.EscapeString(f.Name) + `"></iframe>`
	return f, nil
}

func (h *Handler) listWebForms(ctx context.Context, ws uuid.UUID) ([]*WebForm, error) {
	rows, err := h.store.Pool.Query(ctx, `SELECT `+webFormCols+` FROM crm.web_forms f LEFT JOIN crm.identities i ON i.id = f.owner_id
		WHERE f.workspace_id = $1 ORDER BY f.created_at`, ws)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*WebForm{}
	for rows.Next() {
		f, err := h.scanWebForm(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (h *Handler) webFormAdmin(w http.ResponseWriter, r *http.Request) (*Scope, bool) {
	sc := scopeFrom(r.Context())
	if apiKeyFrom(r.Context()) != nil || !sc.CanCustomize() || !sc.Can("leads", "create") {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "You need the “Customize the product” permission to manage web forms."))
		return nil, false
	}
	return sc, true
}

func (h *Handler) handleListWebForms(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.webFormAdmin(w, r)
	if !ok {
		return
	}
	list, err := h.listWebForms(r.Context(), sc.WS)
	respond(w, r, http.StatusOK, map[string]any{"data": list, "fields": webFormFields}, err)
}

func (h *Handler) handleSaveWebForm(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.webFormAdmin(w, r)
	if !ok {
		return
	}
	var in struct {
		Name        *string   `json:"name"`
		Heading     *string   `json:"heading"`
		Intro       *string   `json:"intro"`
		Fields      *[]string `json:"fields"`
		OwnerID     *string   `json:"ownerId"`
		ConsentText *string   `json:"consentText"`
		ThankYou    *string   `json:"thankYou"`
		Active      *bool     `json:"active"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	cur := &WebForm{Active: true, Fields: []string{"email", "message"}, OwnerID: actor(r)}
	creating := chi.URLParam(r, "formId") == ""
	if !creating {
		id, err := uuid.Parse(chi.URLParam(r, "formId"))
		if err == nil {
			cur, err = h.scanWebForm(h.store.Pool.QueryRow(ctx, `SELECT `+webFormCols+` FROM crm.web_forms f LEFT JOIN crm.identities i ON i.id = f.owner_id
				WHERE f.id = $1 AND f.workspace_id = $2`, id, sc.WS))
		}
		if err != nil {
			shared.WriteError(w, r, shared.NotFound("form_not_found"))
			return
		}
	}
	trim := func(dst *string, src *string) {
		if src != nil {
			*dst = strings.TrimSpace(*src)
		}
	}
	trim(&cur.Name, in.Name)
	trim(&cur.Heading, in.Heading)
	trim(&cur.Intro, in.Intro)
	trim(&cur.ConsentText, in.ConsentText)
	trim(&cur.ThankYou, in.ThankYou)
	if in.Active != nil {
		cur.Active = *in.Active
	}
	fe := map[string]string{}
	if in.Fields != nil {
		known := map[string]bool{}
		for _, f := range webFormFields {
			known[f.Key] = true
		}
		cur.Fields = []string{}
		for _, k := range *in.Fields {
			if !known[k] {
				fe["fields"] = "Pick from the fields offered."
			} else {
				cur.Fields = append(cur.Fields, k)
			}
		}
	}
	if in.OwnerID != nil {
		id, err := uuid.Parse(*in.OwnerID)
		if err != nil {
			fe["ownerId"] = "Pick who gets the leads."
		}
		cur.OwnerID = id
	}
	if cur.Name == "" || len(cur.Name) > 80 {
		fe["name"] = "Give the form a name (up to 80 characters)."
	}
	if len(cur.Heading) > 120 || len(cur.Intro) > 500 || len(cur.ConsentText) > 500 || len(cur.ThankYou) > 300 {
		fe["heading"] = "That text is too long."
	}
	var member bool
	_ = h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.memberships WHERE workspace_id = $1 AND identity_id = $2 AND status = 'active')`, sc.WS, cur.OwnerID).Scan(&member)
	if !member && fe["ownerId"] == "" {
		fe["ownerId"] = "The leads must go to someone who works in this business."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		if creating {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM crm.web_forms WHERE workspace_id = $1`, sc.WS).Scan(&n); err != nil {
				return err
			}
			if n >= 20 {
				return shared.Validation(map[string]string{"name": "A business can have up to 20 web forms."})
			}
			buf := make([]byte, 16)
			if _, err := rand.Read(buf); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `INSERT INTO crm.web_forms (workspace_id, token, name, heading, intro, fields, owner_id, consent_text, thank_you, is_active, created_by)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11) RETURNING id`, sc.WS, "wf_"+hex.EncodeToString(buf), cur.Name, cur.Heading, cur.Intro,
				cur.Fields, cur.OwnerID, cur.ConsentText, cur.ThankYou, cur.Active, actor(r)).Scan(&cur.ID); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE crm.web_forms SET name = $3, heading = $4, intro = $5, fields = $6, owner_id = $7, consent_text = $8,
			thank_you = $9, is_active = $10, updated_at = now() WHERE id = $1 AND workspace_id = $2`, cur.ID, sc.WS, cur.Name, cur.Heading, cur.Intro,
			cur.Fields, cur.OwnerID, cur.ConsentText, cur.ThankYou, cur.Active); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, audit(r, sc.WS, "web_form.saved", "web_form", &cur.ID, nil, map[string]any{"name": cur.Name, "active": cur.Active}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := h.listWebForms(ctx, sc.WS)
	respond(w, r, http.StatusOK, map[string]any{"data": list, "fields": webFormFields}, err)
}

func (h *Handler) handleDeleteWebForm(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.webFormAdmin(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "formId"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("form_not_found"))
		return
	}
	tag, err := h.store.Pool.Exec(r.Context(), `DELETE FROM crm.web_forms WHERE id = $1 AND workspace_id = $2`, id, sc.WS)
	if err == nil && tag.RowsAffected() == 0 {
		err = shared.NotFound("form_not_found")
	}
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list, err := h.listWebForms(r.Context(), sc.WS)
	respond(w, r, http.StatusOK, map[string]any{"data": list, "fields": webFormFields}, err)
}

// ---- the public side ----

var formHits = struct {
	sync.Mutex
	at map[string][]time.Time
}{at: map[string][]time.Time{}}

// formAllowed lets one address send eight submissions in ten minutes.
func formAllowed(ip string) bool {
	formHits.Lock()
	defer formHits.Unlock()
	cut := time.Now().Add(-10 * time.Minute)
	kept := formHits.at[ip][:0]
	for _, t := range formHits.at[ip] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= 8 {
		formHits.at[ip] = kept
		return false
	}
	formHits.at[ip] = append(kept, time.Now())
	if len(formHits.at) > 5000 {
		formHits.at = map[string][]time.Time{ip: formHits.at[ip]}
	}
	return true
}

const formCSS = `*{box-sizing:border-box}body{font:15px/1.5 -apple-system,"Segoe UI",Roboto,Helvetica,Arial,sans-serif;color:#18181b;background:#f4f4f8;margin:0;padding:16px}
.card{background:#fff;border:1px solid #e4e4ec;border-radius:14px;padding:24px;max-width:460px;margin:0 auto}
h1{font-size:20px;margin:0 0 6px}p.intro{color:#52525b;margin:0 0 16px}label{display:block;font-size:13px;font-weight:600;margin:12px 0 4px}
input,textarea{width:100%;padding:10px 12px;border:1px solid #d4d4dc;border-radius:8px;font:inherit}input:focus,textarea:focus{outline:2px solid #4f46e5;border-color:#4f46e5}
.consent{display:flex;gap:8px;align-items:flex-start;margin-top:14px;font-size:13px;color:#3f3f46;font-weight:400}.consent input{width:auto;margin-top:3px}
button{margin-top:18px;width:100%;padding:11px;border:0;border-radius:8px;background:#4f46e5;color:#fff;font:inherit;font-weight:600;cursor:pointer}
.err{background:#fef2f2;border:1px solid #fecaca;color:#b91c1c;border-radius:8px;padding:10px 12px;margin-bottom:12px;font-size:13px}.by{color:#71717a;font-size:12px;text-align:center;margin-top:14px}.hp{position:absolute;left:-9999px}`

func (h *Handler) publicForm(ctx context.Context, token string) (*WebForm, string, error) {
	var business string
	row := h.store.Pool.QueryRow(ctx, `SELECT `+webFormCols+`, w.name FROM crm.web_forms f LEFT JOIN crm.identities i ON i.id = f.owner_id
		JOIN crm.workspaces w ON w.id = f.workspace_id AND w.status = 'active' WHERE f.token = $1 AND f.is_active`, token)
	f := &WebForm{}
	err := row.Scan(&f.ID, &f.Name, &f.Heading, &f.Intro, &f.Fields, &f.OwnerID, &f.OwnerName, &f.ConsentText, &f.ThankYou,
		&f.Active, &f.Submissions, &f.LastSubmissionAt, &f.token, &f.ws, &business)
	return f, business, err
}

func formPage(w http.ResponseWriter, status int, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// A form is meant to be embedded on the business's own site.
	w.Header().Del("X-Frame-Options")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors *")
	w.WriteHeader(status)
	fmt.Fprintf(w, `<!doctype html><html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title><style>%s</style></head><body><div class="card">%s</div></body></html>`,
		html.EscapeString(title), formCSS, body)
}

// GET /public/forms/{token} shows the form; POST takes a submission (a form post from
// the page, or JSON from the business's own site).
func (h *Handler) HandlePublicForm(w http.ResponseWriter, r *http.Request) {
	f, business, err := h.publicForm(r.Context(), chi.URLParam(r, "token"))
	wantsJSON := strings.Contains(r.Header.Get("Content-Type"), "application/json")
	if err != nil {
		if wantsJSON {
			shared.WriteError(w, r, shared.NotFound("form_not_found"))
			return
		}
		formPage(w, http.StatusNotFound, "Form not available", `<h1>This form isn't available</h1><p class="intro">It may have been switched off. Please contact the business directly.</p>`)
		return
	}
	in := map[string]string{}
	problem := ""
	if r.Method == http.MethodPost {
		if wantsJSON {
			raw := map[string]any{}
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16<<10)).Decode(&raw); err != nil {
				shared.WriteError(w, r, shared.BadRequest("Request body must be valid JSON."))
				return
			}
			for k, v := range raw {
				in[k] = strings.TrimSpace(fmt.Sprint(v))
			}
		} else {
			r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
			if err := r.ParseForm(); err == nil {
				for k := range r.PostForm {
					in[k] = strings.TrimSpace(r.PostForm.Get(k))
				}
			}
		}
		switch {
		case in["website_url"] != "":
			// The hidden field only a bot fills in: say thanks, keep nothing.
			problem = ""
		case !formAllowed(shared.ClientIP(r)):
			problem = "Too many submissions from here. Please try again in a few minutes."
		default:
			problem = h.takeSubmission(r.Context(), f, in, shared.ClientIP(r))
		}
		if problem == "" {
			thanks := f.ThankYou
			if thanks == "" {
				thanks = "Thank you. We have your details and will get in touch soon."
			}
			if wantsJSON {
				shared.WriteJSON(w, http.StatusCreated, map[string]any{"ok": true, "message": thanks})
				return
			}
			formPage(w, http.StatusOK, "Thank you", `<h1>Thank you</h1><p class="intro">`+html.EscapeString(thanks)+`</p><div class="by">`+html.EscapeString(business)+`</div>`)
			return
		}
		if wantsJSON {
			shared.WriteError(w, r, shared.Validation(map[string]string{"form": problem}))
			return
		}
	}
	heading := f.Heading
	if heading == "" {
		heading = "Contact " + business
	}
	var b strings.Builder
	b.WriteString(`<h1>` + html.EscapeString(heading) + `</h1>`)
	if f.Intro != "" {
		b.WriteString(`<p class="intro">` + html.EscapeString(f.Intro) + `</p>`)
	}
	if problem != "" {
		b.WriteString(`<div class="err" role="alert">` + html.EscapeString(problem) + `</div>`)
	}
	b.WriteString(`<form method="post" action="` + html.EscapeString(h.formURL(f.token)) + `" novalidate>`)
	field := func(key, label, typ string, required bool) {
		req := ""
		if required {
			req = " required"
		}
		b.WriteString(`<label for="f_` + key + `">` + html.EscapeString(label) + `</label>`)
		if typ == "textarea" {
			b.WriteString(`<textarea id="f_` + key + `" name="` + key + `" rows="3" maxlength="1000">` + html.EscapeString(in[key]) + `</textarea>`)
			return
		}
		b.WriteString(`<input id="f_` + key + `" name="` + key + `" type="` + typ + `" maxlength="200" value="` + html.EscapeString(in[key]) + `"` + req + `>`)
	}
	field("name", "Your name", "text", true)
	field("phone", "Mobile number", "tel", true)
	on := map[string]bool{}
	for _, k := range f.Fields {
		on[k] = true
	}
	for _, x := range webFormFields {
		if on[x.Key] {
			field(x.Key, x.Label, x.Type, false)
		}
	}
	b.WriteString(`<div class="hp" aria-hidden="true"><label>Leave this empty<input name="website_url" tabindex="-1" autocomplete="off"></label></div>`)
	if f.ConsentText != "" {
		b.WriteString(`<label class="consent"><input type="checkbox" name="consent" value="yes" required> <span>` + html.EscapeString(f.ConsentText) + `</span></label>`)
	}
	b.WriteString(`<button type="submit">Send</button></form><div class="by">` + html.EscapeString(business) + `</div>`)
	status := http.StatusOK
	if problem != "" {
		status = http.StatusUnprocessableEntity
	}
	formPage(w, status, heading, b.String())
}

// takeSubmission turns what was sent into a lead (or adds it to the lead that mobile
// already has). It returns "" when taken, or what the visitor should fix.
func (h *Handler) takeSubmission(ctx context.Context, f *WebForm, in map[string]string, ip string) string {
	name := in["name"]
	digits := digitsOnly(in["phone"])
	if name == "" || len(name) > 120 {
		return "Please enter your name."
	}
	if len(digits) < 8 {
		return "Please enter a mobile number we can reach you on."
	}
	if f.ConsentText != "" && in["consent"] == "" {
		return "Please tick the box so we may contact you."
	}
	on := map[string]bool{}
	for _, k := range f.Fields {
		on[k] = true
	}
	pick := func(k string, max int) string {
		v := in[k]
		if !on[k] || v == "" {
			return ""
		}
		if len(v) > max {
			v = v[:max]
		}
		return v
	}
	if e := pick("email", 200); e != "" && !strings.Contains(e, "@") {
		return "That email address doesn't look right."
	}
	note := joinNonEmpty("\n", prefixed("Interested in: ", pick("interest", 200)), pick("message", 1000))
	spec := specFor("leads")
	sys := actorInfo{Kind: "system", Source: "web_form"}
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var leadID uuid.UUID
		err := tx.QueryRow(ctx, `SELECT id FROM crm.leads WHERE workspace_id = $1 AND deleted_at IS NULL AND status <> 'converted'
			AND (right(regexp_replace(COALESCE(phone, ''), '\D', '', 'g'), 10) = $2 OR right(regexp_replace(COALESCE(mobile, ''), '\D', '', 'g'), 10) = $2)
			ORDER BY created_at DESC LIMIT 1`, f.ws, digits).Scan(&leadID)
		switch {
		case err == nil:
			if err := insertActivity(ctx, tx, f.ws, "leads", leadID, "note.added", "Enquired again through “"+f.Name+"”", map[string]any{"body": note, "source": "web_form"}, nil); err != nil {
				return err
			}
		case errors.Is(err, pgx.ErrNoRows):
			first, last := name, ""
			if i := strings.LastIndex(name, " "); i > 0 {
				first, last = name[:i], name[i+1:]
			}
			values := map[string]any{"firstName": first, "lastName": last, "phone": in["phone"], "source": "web", "status": "new",
				"ownerId": f.OwnerID.String(), "email": pick("email", 200), "company": pick("company", 200), "city": pick("city", 100), "description": note}
			if last == "" {
				values["lastName"] = first
				delete(values, "firstName")
			}
			clean(values)
			row, err := h.createRecord(ctx, tx, f.ws, spec, sys, values)
			if err != nil {
				return err
			}
			leadID = row.uuid()
		default:
			return err
		}
		if f.ConsentText != "" && specFor("consents") != nil {
			c := map[string]any{"name": "Consent — " + name, "status": "given", "leadId": leadID.String(), "ownerId": f.OwnerID.String(),
				"purpose": "contact", "channel": "any", "givenOn": time.Now().UTC().Format(time.RFC3339), "source": "web_form",
				"noticeText": f.ConsentText, "evidence": "Ticked on the web form “" + f.Name + "” from " + ip}
			if _, err := h.createRecord(ctx, tx, f.ws, specFor("consents"), sys, c); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE crm.web_forms SET submissions = submissions + 1, last_submission_at = now() WHERE id = $1`, f.ID)
		return err
	})
	if err != nil {
		var se *shared.Error
		if errors.As(err, &se) && se.Status == http.StatusUnprocessableEntity {
			for _, m := range se.FieldErrors {
				return m
			}
		}
		return "We couldn't save your details just now. Please try again."
	}
	return ""
}

func prefixed(prefix, v string) string {
	if v == "" {
		return ""
	}
	return prefix + v
}

// consentObject is the consent log (D-146): who agreed to what, when, how, on which
// wording — and when they took it back.
func consentObject() ObjectDefinition {
	return ObjectDefinition{Key: "consents", Module: "contacts", Singular: "Consent", Plural: "Consents", Icon: "check-square", Prefix: "CNS",
		Description: "A record each time a customer agrees to be contacted, or takes that back: purpose, channel, wording and proof.",
		ObjectBody: ObjectBody{NameLabel: "Consent", StatusLabel: "Status", Statuses: []StatusOption{
			st("given", "Given", "success"), st("withdrawn", "Withdrawn", "danger"), st("expired", "Expired", "neutral")},
			Fields: []ObjectField{
				ofLookup("contactId", "Contact", "contacts"), ofLookup("leadId", "Lead", "leads"),
				ofSelect("purpose", "For", opts("contact", "Contact about their enquiry", "service", "Service reminders", "marketing", "Offers and marketing",
					"sharing", "Sharing with a lender or insurer", "verification", "Identity or document checks")),
				ofSelect("channel", "By", opts("any", "Any channel", "call", "Phone call", "sms", "SMS", "whatsapp", "WhatsApp", "email", "Email")),
				of("givenOn", "Given on", "datetime"), of("withdrawnOn", "Withdrawn on", "datetime"), of("expiresOn", "Valid until", "date"),
				ofSelect("source", "How it was given", opts("web_form", "Web form", "in_person", "In person", "phone", "On a call", "message", "By message", "import", "Imported", "other", "Other")),
				of("noticeText", "What they agreed to", "textarea"), of("evidence", "Proof", "text")}}}
}

// consentSaved: a consent is about someone, is dated when given, and a withdrawal is
// dated and can't be undone — a new consent is a new record.
func (h *Handler) consentSaved(ctx context.Context, tx pgx.Tx, ws uuid.UUID, a actorInfo, op string, before, after *Row) (*Row, error) {
	if a.Source == "consent" {
		return after, nil
	}
	if !after.has("contactId") && !after.has("leadId") {
		return nil, refuse("contactId", "Pick the contact or lead who gave this consent.")
	}
	if op == "update" && before.text("status") == "withdrawn" && after.text("status") != "withdrawn" {
		return nil, refuse("status", "A withdrawn consent stays withdrawn. Record a new consent when the customer agrees again.")
	}
	if op == "update" && before.text("noticeText") != after.text("noticeText") && before.has("noticeText") {
		return nil, refuse("noticeText", "The wording a customer agreed to can't be changed afterwards.")
	}
	set := map[string]any{}
	if after.text("status") == "given" && !after.has("givenOn") {
		set["givenOn"] = time.Now().UTC().Format(time.RFC3339)
	}
	if after.text("status") == "withdrawn" && !after.has("withdrawnOn") {
		set["withdrawnOn"] = time.Now().UTC().Format(time.RFC3339)
	}
	if len(set) == 0 {
		return after, nil
	}
	return h.updateValues(ctx, tx, ws, specFor("consents"), after.uuid(), actorInfo{Kind: "system", Source: "consent"}, set, nil, nil)
}
