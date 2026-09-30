package records

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"regexp"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Email campaigns (D-66): one email to many contacts or leads, picked with the same
// filters as a list. {{firstName}}-style merge fields, a test send, send now or at a
// time, a status per recipient, an unsubscribe link in every email (people who
// unsubscribe are skipped from then on) and at most 1,000 emails a day per workspace.
// Campaigns go out through the CRM's own sender (Brevo or SMTP), a few at a time.

const campaignDailyLimit = 1000

type Campaign struct {
	ID          uuid.UUID      `json:"id"`
	Name        string         `json:"name"`
	Subject     string         `json:"subject"`
	BodyHTML    string         `json:"bodyHtml"`
	FromName    string         `json:"fromName"`
	ReplyTo     string         `json:"replyTo"`
	Object      string         `json:"object"`
	EmailField  string         `json:"emailField"`
	Filter      *FilterNode    `json:"filter,omitempty"`
	Status      string         `json:"status"`
	ScheduledAt *time.Time     `json:"scheduledAt,omitempty"`
	Stats       map[string]int `json:"stats"`
	CreatedBy   string         `json:"createdBy"`
	CreatedAt   time.Time      `json:"createdAt"`
	UpdatedAt   time.Time      `json:"updatedAt"`
	SentAt      *time.Time     `json:"sentAt,omitempty"`
}

func (h *Handler) requireCampaigns(w http.ResponseWriter, r *http.Request) (*Scope, bool) {
	sc := scopeFrom(r.Context())
	if apiKeyFrom(r.Context()) != nil || (!sc.Owner && !sc.Eff.HasCapability(access.CapCampaigns)) {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "You need the “Manage email campaigns” permission."))
		return nil, false
	}
	return sc, true
}

const campaignSelect = `SELECT c.id, c.name, c.subject, c.body_html, c.from_name, c.reply_to, c.object_key, c.email_field, c.filter, c.status, c.scheduled_at, c.stats,
	COALESCE(i.display_name, ''), c.created_at, c.updated_at, c.sent_at FROM crm.campaigns c LEFT JOIN crm.identities i ON i.id = c.created_by`

func scanCampaign(row pgx.Row) (*Campaign, error) {
	c := &Campaign{}
	var filter, stats []byte
	if err := row.Scan(&c.ID, &c.Name, &c.Subject, &c.BodyHTML, &c.FromName, &c.ReplyTo, &c.Object, &c.EmailField, &filter, &c.Status, &c.ScheduledAt, &stats,
		&c.CreatedBy, &c.CreatedAt, &c.UpdatedAt, &c.SentAt); err != nil {
		return nil, err
	}
	if len(filter) > 2 {
		var f FilterNode
		if json.Unmarshal(filter, &f) == nil {
			c.Filter = &f
		}
	}
	_ = json.Unmarshal(stats, &c.Stats)
	if c.Stats == nil {
		c.Stats = map[string]int{}
	}
	return c, nil
}

func (h *Handler) handleListCampaigns(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireCampaigns(w, r)
	if !ok {
		return
	}
	rows, err := h.store.Pool.Query(r.Context(), campaignSelect+` WHERE c.workspace_id = $1 ORDER BY c.created_at DESC`, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []*Campaign{}
	for rows.Next() {
		c, err := scanCampaign(rows)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out = append(out, c)
	}
	var sentToday int
	_ = h.store.Pool.QueryRow(r.Context(), `SELECT count(*) FROM crm.campaign_recipients WHERE workspace_id = $1 AND status = 'sent' AND sent_at > now() - interval '1 day'`, sc.WS).Scan(&sentToday)
	respond(w, r, http.StatusOK, map[string]any{"data": out, "sentToday": sentToday, "dailyLimit": campaignDailyLimit, "senderReady": h.mailer != nil && h.mailer.From() != ""}, rows.Err())
}

type campaignInput struct {
	Name        *string     `json:"name"`
	Subject     *string     `json:"subject"`
	BodyHTML    *string     `json:"bodyHtml"`
	FromName    *string     `json:"fromName"`
	ReplyTo     *string     `json:"replyTo"`
	Object      *string     `json:"object"`
	EmailField  *string     `json:"emailField"`
	Filter      *FilterNode `json:"filter"`
	ClearFilter bool        `json:"clearFilter"`
}

func (h *Handler) loadCampaign(ctx context.Context, ws, id uuid.UUID) (*Campaign, error) {
	c, err := scanCampaign(h.store.Pool.QueryRow(ctx, campaignSelect+` WHERE c.id = $1 AND c.workspace_id = $2`, id, ws))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NotFound("campaign_not_found")
	}
	return c, err
}

func (h *Handler) applyCampaign(sc *Scope, c *Campaign, in campaignInput) map[string]string {
	fe := map[string]string{}
	set := func(dst *string, v *string, max int, key, msg string) {
		if v != nil {
			*dst = strings.TrimSpace(*v)
			if len(*dst) > max {
				fe[key] = msg
			}
		}
	}
	set(&c.Name, in.Name, 120, "name", "Use at most 120 characters.")
	set(&c.Subject, in.Subject, 250, "subject", "Use at most 250 characters.")
	set(&c.FromName, in.FromName, 80, "fromName", "Use at most 80 characters.")
	set(&c.ReplyTo, in.ReplyTo, 200, "replyTo", "Use at most 200 characters.")
	if in.BodyHTML != nil {
		c.BodyHTML = *in.BodyHTML
		if len(c.BodyHTML) > 300_000 {
			fe["bodyHtml"] = "The email is too long."
		}
	}
	if in.Object != nil {
		c.Object = *in.Object
	}
	if in.EmailField != nil {
		c.EmailField = *in.EmailField
	}
	if in.Filter != nil {
		c.Filter = in.Filter
	}
	if in.ClearFilter {
		c.Filter = nil
	}
	if c.Name == "" {
		fe["name"] = "Name the campaign."
	}
	if c.ReplyTo != "" && !strings.Contains(c.ReplyTo, "@") {
		fe["replyTo"] = "Enter an email address."
	}
	spec := specFor(c.Object)
	if spec == nil || !sc.Enabled(c.Object) {
		fe["object"] = "Pick who the campaign goes to."
	}
	return fe
}

func (h *Handler) saveCampaign(ctx context.Context, c *Campaign) error {
	filter, _ := json.Marshal(c.Filter)
	if c.Filter == nil {
		filter = []byte("{}")
	}
	_, err := h.store.Pool.Exec(ctx, `UPDATE crm.campaigns SET name = $2, subject = $3, body_html = $4, from_name = $5, reply_to = $6, object_key = $7, email_field = $8,
		filter = $9, updated_at = now() WHERE id = $1`, c.ID, c.Name, c.Subject, c.BodyHTML, c.FromName, c.ReplyTo, c.Object, c.EmailField, filter)
	return err
}

func (h *Handler) handleCreateCampaign(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireCampaigns(w, r)
	if !ok {
		return
	}
	var in campaignInput
	if err := shared.DecodeJSONLimit(w, r, &in, 1<<20); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	c := &Campaign{Object: "contacts", EmailField: "email", Status: "draft"}
	if fe := h.applyCampaign(sc, c, in); len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	if err := h.store.Pool.QueryRow(r.Context(), `INSERT INTO crm.campaigns (workspace_id, name, created_by) VALUES ($1, $2, $3) RETURNING id`, sc.WS, c.Name, actor(r)).
		Scan(&c.ID); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if err := h.saveCampaign(r.Context(), c); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out, err := h.loadCampaign(r.Context(), sc.WS, c.ID)
	respond(w, r, http.StatusCreated, out, err)
}

func campaignID(r *http.Request) uuid.UUID {
	id, _ := uuid.Parse(chi.URLParam(r, "campaignId"))
	return id
}

func (h *Handler) handleGetCampaign(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireCampaigns(w, r)
	if !ok {
		return
	}
	c, err := h.loadCampaign(r.Context(), sc.WS, campaignID(r))
	respond(w, r, http.StatusOK, c, err)
}

func (h *Handler) handleUpdateCampaign(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireCampaigns(w, r)
	if !ok {
		return
	}
	c, err := h.loadCampaign(r.Context(), sc.WS, campaignID(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if c.Status != "draft" && c.Status != "scheduled" {
		shared.WriteError(w, r, shared.NewError(http.StatusConflict, "campaign_sent", "A campaign that has been sent can't be changed. Duplicate it instead."))
		return
	}
	var in campaignInput
	if err := shared.DecodeJSONLimit(w, r, &in, 1<<20); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if fe := h.applyCampaign(sc, c, in); len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	if err := h.saveCampaign(r.Context(), c); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out, err := h.loadCampaign(r.Context(), sc.WS, c.ID)
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) handleDeleteCampaign(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireCampaigns(w, r)
	if !ok {
		return
	}
	tag, err := h.store.Pool.Exec(r.Context(), `DELETE FROM crm.campaigns WHERE id = $1 AND workspace_id = $2 AND status IN ('draft', 'scheduled', 'cancelled', 'sent', 'failed')`, campaignID(r), sc.WS)
	if err != nil || tag.RowsAffected() == 0 {
		shared.WriteError(w, r, shared.NotFound("campaign_not_found"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// audience lists who a campaign would reach: records matching its filter with an email.
func (h *Handler) audience(ctx context.Context, ws uuid.UUID, c *Campaign, limit int) ([]Row, int, error) {
	spec := specFor(c.Object)
	if spec == nil {
		return nil, 0, shared.Validation(map[string]string{"object": "Pick who the campaign goes to."})
	}
	cond := FilterNode{Field: c.EmailField, Op: "notEmpty"}
	filter := &FilterNode{Op: "and", Filters: []FilterNode{cond}}
	if c.Filter != nil {
		filter.Filters = append(filter.Filters, *c.Filter)
	}
	rows, total, err := h.list(ctx, ws, spec, listParams{Filter: filter, Limit: limit, Sorts: []SortSpec{{Field: "createdAt", Dir: "asc"}}})
	return rows, total, err
}

// GET /campaigns/{id}/audience — how many people, and a few examples.
func (h *Handler) handleCampaignAudience(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireCampaigns(w, r)
	if !ok {
		return
	}
	c, err := h.loadCampaign(r.Context(), sc.WS, campaignID(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	rows, total, err := h.audience(r.Context(), sc.WS, c, 10)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var unsubscribed int
	_ = h.store.Pool.QueryRow(r.Context(), `SELECT count(*) FROM crm.unsubscribes WHERE workspace_id = $1`, sc.WS).Scan(&unsubscribed)
	sample := []map[string]any{}
	for _, row := range rows {
		sample = append(sample, map[string]any{"id": row.ID, "title": row.Title, "email": row.Values[c.EmailField]})
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"total": total, "sample": sample, "unsubscribed": unsubscribed})
}

var mergeRe = regexp.MustCompile(`\{\{\s*([a-zA-Z][a-zA-Z0-9_]*)\s*\}\}`)

func mergeFields(s string, row Row, escape bool) string {
	return mergeRe.ReplaceAllStringFunc(s, func(m string) string {
		key := mergeRe.FindStringSubmatch(m)[1]
		var v string
		switch key {
		case "name", "title":
			v = row.Title
		default:
			v = asString(row.Values[key])
			if l, ok := row.Lookups[key]; ok {
				v = l.Label
			}
		}
		if escape {
			return html.EscapeString(v)
		}
		return v
	})
}

func (h *Handler) campaignEmail(c *Campaign, row Row, unsubscribeURL string) (string, string, string) {
	subject := mergeFields(c.Subject, row, false)
	body := mergeFields(c.BodyHTML, row, true)
	if !strings.Contains(strings.ToLower(body), "<html") {
		body = `<div style="font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;font-size:15px;line-height:1.6;color:#18181b;max-width:600px">` + body + `</div>`
	}
	footer := `<p style="margin-top:28px;font-size:12px;color:#71717a;font-family:Arial,sans-serif">Don't want these emails? <a href="` + unsubscribeURL + `" style="color:#71717a">Unsubscribe</a>.</p>`
	if i := strings.LastIndex(strings.ToLower(body), "</body>"); i >= 0 {
		body = body[:i] + footer + body[i:]
	} else {
		body += footer
	}
	text := strings.TrimSpace(regexp.MustCompile(`<[^>]+>`).ReplaceAllString(strings.NewReplacer("<br>", "\n", "</p>", "\n\n").Replace(body), ""))
	return subject, body, html.UnescapeString(text)
}

// POST /campaigns/{id}/test {"to":"me@x.com"}
func (h *Handler) handleTestCampaign(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireCampaigns(w, r)
	if !ok {
		return
	}
	c, err := h.loadCampaign(r.Context(), sc.WS, campaignID(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in struct {
		To string `json:"to"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	to, err := cleanAddresses([]string{in.To})
	if err != nil || len(to) != 1 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"to": "Enter one email address."}))
		return
	}
	if c.Subject == "" || strings.TrimSpace(c.BodyHTML) == "" {
		shared.WriteError(w, r, shared.Validation(map[string]string{"subject": "Write a subject and the email first."}))
		return
	}
	rows, _, _ := h.audience(r.Context(), sc.WS, c, 1)
	sample := Row{Title: "Sample Person", Values: map[string]any{"firstName": "Sample", "lastName": "Person", "email": to[0]}}
	if len(rows) > 0 {
		sample = rows[0]
	}
	subject, body, text := h.campaignEmail(c, sample, strings.TrimRight(h.cfg.BaseURL, "/")+"/api/crm/v1/public/unsubscribe/test")
	if h.mailer == nil {
		shared.WriteError(w, r, shared.NewError(http.StatusConflict, "no_sender", "Email isn't set up on this server."))
		return
	}
	if err := h.mailer.SendHTML(r.Context(), to[0], "[Test] "+subject, body, text, MailOptions{FromName: c.FromName, ReplyTo: c.ReplyTo}); err != nil {
		shared.WriteError(w, r, shared.NewError(http.StatusBadGateway, "email_failed", "The test email couldn't be sent: "+err.Error()))
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// POST /campaigns/{id}/send {"at": "2026-10-01T09:00:00Z"?}
func (h *Handler) handleSendCampaign(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireCampaigns(w, r)
	if !ok {
		return
	}
	c, err := h.loadCampaign(r.Context(), sc.WS, campaignID(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if c.Status != "draft" && c.Status != "scheduled" {
		shared.WriteError(w, r, shared.NewError(http.StatusConflict, "campaign_sent", "This campaign has already been sent."))
		return
	}
	var in struct {
		At *time.Time `json:"at"`
	}
	_ = shared.DecodeJSON(w, r, &in)
	fe := map[string]string{}
	if c.Subject == "" {
		fe["subject"] = "Write a subject."
	}
	if strings.TrimSpace(c.BodyHTML) == "" {
		fe["bodyHtml"] = "Write the email."
	}
	if h.mailer == nil || h.mailer.From() == "" {
		fe["sender"] = "Email isn't set up on this server."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	rows, total, err := h.audience(r.Context(), sc.WS, c, 20_000)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if total == 0 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"filter": "Nobody matches — no one would get this email."}))
		return
	}
	if total > 20_000 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"filter": "Send to at most 20,000 people at once."}))
		return
	}
	when := time.Now()
	status := "sending"
	if in.At != nil && in.At.After(time.Now().Add(time.Minute)) {
		when, status = *in.At, "scheduled"
	}
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `DELETE FROM crm.campaign_recipients WHERE campaign_id = $1`, c.ID); err != nil {
			return err
		}
		seen := map[string]bool{}
		queued := 0
		for _, row := range rows {
			email := strings.ToLower(asString(row.Values[c.EmailField]))
			if email == "" || seen[email] {
				continue
			}
			seen[email] = true
			token, _ := shared.RandomToken(18)
			if _, err := tx.Exec(r.Context(), `INSERT INTO crm.campaign_recipients (campaign_id, workspace_id, record_id, email, name, token)
				VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT DO NOTHING`, c.ID, sc.WS, row.ID, email, row.Title, token); err != nil {
				return err
			}
			queued++
		}
		stats, _ := json.Marshal(map[string]int{"recipients": queued})
		_, err := tx.Exec(r.Context(), `UPDATE crm.campaigns SET status = $2, scheduled_at = $3, stats = $4, updated_at = now() WHERE id = $1`, c.ID, status, when, stats)
		if err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, actorFromRequest(r, "ui").audit(sc.WS, "campaign."+status, "campaign", &c.ID, nil, map[string]any{"recipients": queued}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.bus.Kick()
	out, err := h.loadCampaign(r.Context(), sc.WS, c.ID)
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) handleCancelCampaign(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireCampaigns(w, r)
	if !ok {
		return
	}
	tag, err := h.store.Pool.Exec(r.Context(), `UPDATE crm.campaigns SET status = 'cancelled', updated_at = now() WHERE id = $1 AND workspace_id = $2 AND status IN ('scheduled', 'sending')`,
		campaignID(r), sc.WS)
	if err != nil || tag.RowsAffected() == 0 {
		shared.WriteError(w, r, shared.NewError(http.StatusConflict, "not_sending", "Only a scheduled or sending campaign can be stopped."))
		return
	}
	out, err := h.loadCampaign(r.Context(), sc.WS, campaignID(r))
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) handleCampaignRecipients(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireCampaigns(w, r)
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	rows, err := h.store.Pool.Query(r.Context(), `SELECT record_id, email, name, status, COALESCE(error, ''), sent_at FROM crm.campaign_recipients
		WHERE campaign_id = $1 AND workspace_id = $2 AND ($3 = '' OR status = $3) ORDER BY sent_at DESC NULLS LAST, email LIMIT 500`, campaignID(r), sc.WS, status)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	type rec struct {
		RecordID uuid.UUID  `json:"recordId"`
		Email    string     `json:"email"`
		Name     string     `json:"name"`
		Status   string     `json:"status"`
		Error    string     `json:"error,omitempty"`
		SentAt   *time.Time `json:"sentAt,omitempty"`
	}
	out := []rec{}
	for rows.Next() {
		var x rec
		if err := rows.Scan(&x.RecordID, &x.Email, &x.Name, &x.Status, &x.Error, &x.SentAt); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out = append(out, x)
	}
	respond(w, r, http.StatusOK, map[string]any{"data": out}, rows.Err())
}

// sendDueCampaigns sends the next batch of every campaign that's due.
func (h *Handler) sendDueCampaigns(ctx context.Context) {
	_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.campaigns SET status = 'sending' WHERE status = 'scheduled' AND scheduled_at <= now()`)
	rows, err := h.store.Pool.Query(ctx, campaignSelect+` WHERE c.status = 'sending' LIMIT 10`)
	if err != nil {
		return
	}
	var list []*Campaign
	var wsOf = map[uuid.UUID]uuid.UUID{}
	for rows.Next() {
		c, err := scanCampaign(rows)
		if err == nil {
			list = append(list, c)
		}
	}
	rows.Close()
	for _, c := range list {
		var ws uuid.UUID
		_ = h.store.Pool.QueryRow(ctx, `SELECT workspace_id FROM crm.campaigns WHERE id = $1`, c.ID).Scan(&ws)
		wsOf[c.ID] = ws
		h.sendCampaignBatch(ctx, ws, c)
	}
}

func (h *Handler) sendCampaignBatch(ctx context.Context, ws uuid.UUID, c *Campaign) {
	var sentToday int
	_ = h.store.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.campaign_recipients WHERE workspace_id = $1 AND status = 'sent' AND sent_at > now() - interval '1 day'`, ws).Scan(&sentToday)
	room := campaignDailyLimit - sentToday
	if room <= 0 {
		h.bus.wakeAt(time.Now().Add(time.Hour)) // try again later today / tomorrow
		return
	}
	batch := 25
	if room < batch {
		batch = room
	}
	rows, err := h.store.Pool.Query(ctx, `SELECT record_id, email, token FROM crm.campaign_recipients WHERE campaign_id = $1 AND status = 'pending' ORDER BY email LIMIT $2`, c.ID, batch)
	if err != nil {
		return
	}
	type pending struct {
		id           uuid.UUID
		email, token string
	}
	var list []pending
	for rows.Next() {
		var p pending
		if rows.Scan(&p.id, &p.email, &p.token) == nil {
			list = append(list, p)
		}
	}
	rows.Close()
	spec := specFor(c.Object)
	base := strings.TrimRight(h.cfg.BaseURL, "/")
	for _, p := range list {
		var unsub bool
		_ = h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.unsubscribes WHERE workspace_id = $1 AND email = $2)`, ws, p.email).Scan(&unsub)
		if unsub {
			_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.campaign_recipients SET status = 'unsubscribed' WHERE campaign_id = $1 AND record_id = $2`, c.ID, p.id)
			continue
		}
		row := Row{Values: map[string]any{"email": p.email}, Lookups: map[string]LookupValue{}}
		if spec != nil {
			if full, _, err := h.getRow(ctx, h.store.Pool, ws, spec, p.id, nil); err == nil {
				row = *full
			}
		}
		unsubURL := base + "/api/crm/v1/public/unsubscribe/" + p.token
		subject, body, text := h.campaignEmail(c, row, unsubURL)
		err := h.mailer.SendHTML(ctx, p.email, subject, body, text, MailOptions{FromName: c.FromName, ReplyTo: c.ReplyTo,
			Headers: map[string]string{"List-Unsubscribe": "<" + unsubURL + ">", "List-Unsubscribe-Post": "List-Unsubscribe=One-Click"}})
		if err != nil {
			_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.campaign_recipients SET status = 'failed', error = $3 WHERE campaign_id = $1 AND record_id = $2`, c.ID, p.id, clip(err.Error(), 500))
			continue
		}
		_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.campaign_recipients SET status = 'sent', sent_at = now() WHERE campaign_id = $1 AND record_id = $2`, c.ID, p.id)
	}
	// Stats and completion.
	var pendingLeft int
	stats := map[string]int{}
	srows, err := h.store.Pool.Query(ctx, `SELECT status, count(*) FROM crm.campaign_recipients WHERE campaign_id = $1 GROUP BY status`, c.ID)
	if err == nil {
		total := 0
		for srows.Next() {
			var s string
			var n int
			if srows.Scan(&s, &n) == nil {
				stats[s] = n
				total += n
			}
		}
		srows.Close()
		stats["recipients"] = total
	}
	pendingLeft = stats["pending"]
	raw, _ := json.Marshal(stats)
	if pendingLeft == 0 {
		_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.campaigns SET status = 'sent', stats = $2, sent_at = now(), updated_at = now() WHERE id = $1`, c.ID, raw)
		return
	}
	_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.campaigns SET stats = $2, updated_at = now() WHERE id = $1`, c.ID, raw)
	h.bus.wakeAt(time.Now().Add(3 * time.Second))
}

// GET/POST /public/unsubscribe/{token} — no sign-in; one click stops campaign emails.
func (h *Handler) HandleUnsubscribe(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	var ws uuid.UUID
	var email, name string
	err := h.store.Pool.QueryRow(r.Context(), `SELECT r.workspace_id, r.email, w.name FROM crm.campaign_recipients r JOIN crm.workspaces w ON w.id = r.workspace_id WHERE r.token = $1`,
		token).Scan(&ws, &email, &name)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	page := func(title, msg string) {
		fmt.Fprintf(w, `<!doctype html><meta name="viewport" content="width=device-width,initial-scale=1"><title>%s</title>
<body style="font-family:-apple-system,Segoe UI,Roboto,Arial,sans-serif;background:#f4f4f8;display:grid;place-items:center;min-height:90vh;margin:0">
<div style="background:#fff;border:1px solid #e4e4ec;border-radius:14px;padding:32px;max-width:420px;text-align:center"><h1 style="font-size:20px;margin:0 0 10px">%s</h1>
<p style="color:#52525b;margin:0">%s</p></div></body>`, html.EscapeString(title), html.EscapeString(title), html.EscapeString(msg))
	}
	if err != nil {
		if token == "test" {
			page("Unsubscribe link", "This is how the unsubscribe link will look in the real email.")
			return
		}
		w.WriteHeader(http.StatusNotFound)
		page("Link not found", "This unsubscribe link isn't valid any more.")
		return
	}
	_, _ = h.store.Pool.Exec(r.Context(), `INSERT INTO crm.unsubscribes (workspace_id, email) VALUES ($1, $2) ON CONFLICT DO NOTHING`, ws, email)
	_, _ = h.store.Pool.Exec(r.Context(), `UPDATE crm.campaign_recipients SET status = 'unsubscribed' WHERE token = $1 AND status = 'pending'`, token)
	page("You're unsubscribed", fmt.Sprintf("%s won't send campaign emails to %s any more.", name, email))
}
