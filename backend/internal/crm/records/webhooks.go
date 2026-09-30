package records

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"cardflow-backend/internal/crm/platform"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Webhooks (D-62): when the product's setup has webhooks on, record events are POSTed
// to the workspace's endpoints as JSON, signed with HMAC-SHA256 so the receiver can
// check they came from the CRM. Failed deliveries are retried (1m, 5m, 30m, 2h, 6h, 24h)
// and every attempt is kept in a delivery log that can be replayed.

var webhookEvents = map[string]bool{"record.created": true, "record.updated": true, "record.deleted": true, "record.restored": true, "record.destroyed": true}

var retryDelays = []time.Duration{time.Minute, 5 * time.Minute, 30 * time.Minute, 2 * time.Hour, 6 * time.Hour, 24 * time.Hour}

// outboundClient refuses private, loopback and link-local addresses outside local
// development, so webhooks and workflow HTTP steps can't reach internal services.
func (h *Handler) outboundClient(timeout time.Duration) *http.Client {
	allowPrivate := h.cfg.AppEnv == "local"
	dialer := &net.Dialer{Timeout: 5 * time.Second, Control: func(network, address string, c syscall.RawConn) error {
		if allowPrivate {
			return nil
		}
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
			return errors.New("that address isn't reachable from the CRM")
		}
		return nil
	}}
	return &http.Client{Timeout: timeout, Transport: &http.Transport{DialContext: dialer.DialContext, TLSHandshakeTimeout: 5 * time.Second,
		ResponseHeaderTimeout: timeout, MaxIdleConns: 10, IdleConnTimeout: 30 * time.Second},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			return nil
		}}
}

func validOutboundURL(raw string, local bool) (string, string) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", "Enter a full URL starting with https://."
	}
	if u.Scheme != "https" && !local {
		return "", "Use an https:// URL."
	}
	if len(raw) > 2000 {
		return "", "The URL is too long."
	}
	return u.String(), ""
}

func signPayload(secret []byte, ts string, body []byte) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte(ts + "."))
	m.Write(body)
	return "sha256=" + hex.EncodeToString(m.Sum(nil))
}

// queueWebhooks creates a delivery for every endpoint listening to the event.
func (h *Handler) queueWebhooks(ctx context.Context, tx pgx.Tx, ev Event) error {
	if !webhookEvents[ev.Type] {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT id FROM crm.webhooks WHERE workspace_id = $1 AND status = 'active' AND $2 = ANY(events)
		AND (cardinality(objects) = 0 OR $3 = ANY(objects))`, ev.WorkspaceID, ev.Type, ev.Object)
	if err != nil {
		return err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if len(ids) == 0 {
		return nil
	}
	setup, err := platform.WorkspaceSetup(ctx, tx, ev.WorkspaceID)
	if err != nil || !setup.Integrations.Webhooks {
		return err
	}
	var code string
	_ = tx.QueryRow(ctx, `SELECT code FROM crm.workspaces WHERE id = $1`, ev.WorkspaceID).Scan(&code)
	payload, _ := json.Marshal(map[string]any{
		"id": ev.ID, "event": ev.Type, "workspace": code, "object": ev.Object, "recordId": ev.RecordID,
		"record": ev.Record, "previous": ev.Previous, "changedFields": ev.Changed, "occurredAt": ev.At, "source": ev.Source,
	})
	for _, id := range ids {
		if _, err := tx.Exec(ctx, `INSERT INTO crm.webhook_deliveries (webhook_id, workspace_id, event_id, event_type, payload, next_attempt_at)
			VALUES ($1, $2, $3, $4, $5, now()) ON CONFLICT (webhook_id, event_id) DO NOTHING`, id, ev.WorkspaceID, ev.ID, ev.Type, payload); err != nil {
			return err
		}
	}
	return nil
}

type dueDelivery struct {
	id       int64
	url      string
	secret   []byte
	eventID  uuid.UUID
	event    string
	payload  []byte
	attempts int
}

// deliverDueWebhooks sends deliveries whose time has come (a two-minute lease keeps two
// workers from sending the same one).
func (h *Handler) deliverDueWebhooks(ctx context.Context) {
	for round := 0; round < 10; round++ {
		rows, err := h.store.Pool.Query(ctx, `
			UPDATE crm.webhook_deliveries d SET next_attempt_at = now() + interval '2 minutes'
			FROM crm.webhooks w
			WHERE d.id IN (SELECT id FROM crm.webhook_deliveries WHERE status = 'pending' AND next_attempt_at <= now() ORDER BY next_attempt_at LIMIT 25 FOR UPDATE SKIP LOCKED)
			  AND w.id = d.webhook_id
			RETURNING d.id, w.url, w.secret_enc, d.event_id, d.event_type, d.payload, d.attempts`)
		if err != nil {
			slog.Error("CRM webhooks", "error", err)
			return
		}
		var due []dueDelivery
		for rows.Next() {
			var d dueDelivery
			var enc []byte
			if err := rows.Scan(&d.id, &d.url, &enc, &d.eventID, &d.event, &d.payload, &d.attempts); err != nil {
				rows.Close()
				return
			}
			d.secret, _ = shared.Decrypt(h.cfg.EncryptionKey, enc)
			due = append(due, d)
		}
		rows.Close()
		if len(due) == 0 {
			return
		}
		for _, d := range due {
			h.sendDelivery(ctx, d)
		}
	}
}

func (h *Handler) sendDelivery(ctx context.Context, d dueDelivery) (int, string) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, d.url, bytes.NewReader(d.payload))
	status, body := 0, ""
	if err == nil {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "CRM-Webhooks/1.0")
		req.Header.Set("X-CRM-Event", d.event)
		req.Header.Set("X-CRM-Delivery", d.eventID.String())
		req.Header.Set("X-CRM-Timestamp", ts)
		req.Header.Set("X-CRM-Signature", signPayload(d.secret, ts, d.payload))
		res, err2 := h.outboundClient(10 * time.Second).Do(req)
		if err2 != nil {
			body = err2.Error()
		} else {
			status = res.StatusCode
			raw, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
			res.Body.Close()
			body = string(raw)
		}
	} else {
		body = err.Error()
	}
	attempts := d.attempts + 1
	ok := status >= 200 && status < 300
	var next *time.Time
	state := "delivered"
	if !ok {
		state = "failed"
		if attempts <= len(retryDelays) {
			t := time.Now().Add(retryDelays[attempts-1])
			next, state = &t, "pending"
		}
	}
	_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.webhook_deliveries SET status = $2, attempts = $3, response_status = NULLIF($4, 0), response_body = $5,
		next_attempt_at = $6, delivered_at = CASE WHEN $2 = 'delivered' THEN now() ELSE delivered_at END WHERE id = $1`, d.id, state, attempts, status, body, next)
	_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.webhooks SET last_delivery_at = now(), last_status = NULLIF($2, 0)
		WHERE id = (SELECT webhook_id FROM crm.webhook_deliveries WHERE id = $1)`, d.id, status)
	return status, body
}

// ---- management: /w/{code}/developer/webhooks ----

type Webhook struct {
	ID             uuid.UUID  `json:"id"`
	URL            string     `json:"url"`
	Description    string     `json:"description"`
	Events         []string   `json:"events"`
	Objects        []string   `json:"objects"`
	Status         string     `json:"status"`
	LastDeliveryAt *time.Time `json:"lastDeliveryAt,omitempty"`
	LastStatus     *int       `json:"lastStatus,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	Secret         string     `json:"secret,omitempty"` // only when created or rotated
	Failing        int        `json:"failing"`
}

type webhookInput struct {
	URL         *string   `json:"url"`
	Description *string   `json:"description"`
	Events      *[]string `json:"events"`
	Objects     *[]string `json:"objects"`
	Status      *string   `json:"status"`
}

func (h *Handler) handleListWebhooks(w http.ResponseWriter, r *http.Request) {
	sc, setup, ok := h.requireDeveloper(w, r, true)
	if !ok {
		return
	}
	rows, err := h.store.Pool.Query(r.Context(), `SELECT w.id, w.url, w.description, w.events, w.objects, w.status, w.last_delivery_at, w.last_status, w.created_at,
		(SELECT count(*) FROM crm.webhook_deliveries d WHERE d.webhook_id = w.id AND d.status <> 'delivered' AND d.attempts > 0)
		FROM crm.webhooks w WHERE w.workspace_id = $1 ORDER BY w.created_at`, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []Webhook{}
	for rows.Next() {
		var wh Webhook
		if err := rows.Scan(&wh.ID, &wh.URL, &wh.Description, &wh.Events, &wh.Objects, &wh.Status, &wh.LastDeliveryAt, &wh.LastStatus, &wh.CreatedAt, &wh.Failing); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out = append(out, wh)
	}
	respond(w, r, http.StatusOK, map[string]any{"data": out, "enabled": setup.Integrations.Webhooks, "events": []string{"record.created", "record.updated", "record.deleted", "record.restored", "record.destroyed"}}, rows.Err())
}

func (h *Handler) validateWebhook(sc *Scope, in webhookInput, wh *Webhook, creating bool) map[string]string {
	fe := map[string]string{}
	if in.URL != nil || creating {
		u := ""
		if in.URL != nil {
			u = *in.URL
		}
		clean, msg := validOutboundURL(u, h.cfg.AppEnv == "local")
		if msg != "" {
			fe["url"] = msg
		}
		wh.URL = clean
	}
	if in.Description != nil {
		wh.Description = strings.TrimSpace(*in.Description)
		if len(wh.Description) > 200 {
			fe["description"] = "Use at most 200 characters."
		}
	}
	if in.Events != nil || creating {
		wh.Events = []string{}
		if in.Events != nil {
			for _, e := range *in.Events {
				if webhookEvents[e] && !contains(wh.Events, e) {
					wh.Events = append(wh.Events, e)
				}
			}
		}
		if len(wh.Events) == 0 {
			fe["events"] = "Pick at least one event."
		}
	}
	if in.Objects != nil {
		wh.Objects = []string{}
		for _, o := range *in.Objects {
			if specFor(o) != nil && sc.Enabled(o) && !contains(wh.Objects, o) {
				wh.Objects = append(wh.Objects, o)
			}
		}
	}
	if wh.Objects == nil {
		wh.Objects = []string{}
	}
	if in.Status != nil {
		if *in.Status != "active" && *in.Status != "paused" {
			fe["status"] = "Pick active or paused."
		}
		wh.Status = *in.Status
	}
	if wh.Status == "" {
		wh.Status = "active"
	}
	return fe
}

func (h *Handler) handleCreateWebhook(w http.ResponseWriter, r *http.Request) {
	sc, setup, ok := h.requireDeveloper(w, r, true)
	if !ok {
		return
	}
	if !setup.Integrations.Webhooks {
		shared.WriteError(w, r, shared.NewError(http.StatusConflict, "webhooks_off", "Turn on webhooks in the app's setup first (Products → Apps → Edit app setup → Sign-in & integrations)."))
		return
	}
	var in webhookInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	wh := &Webhook{}
	if fe := h.validateWebhook(sc, in, wh, true); len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	var n int
	_ = h.store.Pool.QueryRow(r.Context(), `SELECT count(*) FROM crm.webhooks WHERE workspace_id = $1`, sc.WS).Scan(&n)
	if n >= 20 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"url": "A workspace can have 20 webhooks."}))
		return
	}
	secret, _ := shared.RandomToken(32)
	secret = "whsec_" + secret
	enc, err := shared.Encrypt(h.cfg.EncryptionKey, []byte(secret))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	err = h.store.Pool.QueryRow(r.Context(), `INSERT INTO crm.webhooks (workspace_id, url, description, secret_enc, events, objects, status, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8) RETURNING id, created_at`, sc.WS, wh.URL, wh.Description, enc, wh.Events, wh.Objects, wh.Status, actor(r)).Scan(&wh.ID, &wh.CreatedAt)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	_ = shared.WriteAudit(r.Context(), h.store.Pool, actorFromRequest(r, "ui").audit(sc.WS, "webhook.created", "webhook", &wh.ID, nil, map[string]any{"url": wh.URL, "events": wh.Events}))
	wh.Secret = secret
	shared.WriteJSON(w, http.StatusCreated, wh)
}

func (h *Handler) loadWebhook(ctx context.Context, ws uuid.UUID, id uuid.UUID) (*Webhook, error) {
	wh := &Webhook{}
	err := h.store.Pool.QueryRow(ctx, `SELECT id, url, description, events, objects, status, last_delivery_at, last_status, created_at FROM crm.webhooks
		WHERE id = $1 AND workspace_id = $2`, id, ws).Scan(&wh.ID, &wh.URL, &wh.Description, &wh.Events, &wh.Objects, &wh.Status, &wh.LastDeliveryAt, &wh.LastStatus, &wh.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NotFound("webhook_not_found")
	}
	return wh, err
}

func (h *Handler) handleUpdateWebhook(w http.ResponseWriter, r *http.Request) {
	sc, _, ok := h.requireDeveloper(w, r, true)
	if !ok {
		return
	}
	id, _ := uuid.Parse(chi.URLParam(r, "hookId"))
	wh, err := h.loadWebhook(r.Context(), sc.WS, id)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in webhookInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if fe := h.validateWebhook(sc, in, wh, false); len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	if _, err := h.store.Pool.Exec(r.Context(), `UPDATE crm.webhooks SET url = $2, description = $3, events = $4, objects = $5, status = $6, updated_at = now() WHERE id = $1`,
		id, wh.URL, wh.Description, wh.Events, wh.Objects, wh.Status); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, wh)
}

func (h *Handler) handleRotateWebhookSecret(w http.ResponseWriter, r *http.Request) {
	sc, _, ok := h.requireDeveloper(w, r, true)
	if !ok {
		return
	}
	id, _ := uuid.Parse(chi.URLParam(r, "hookId"))
	wh, err := h.loadWebhook(r.Context(), sc.WS, id)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	secret, _ := shared.RandomToken(32)
	secret = "whsec_" + secret
	enc, _ := shared.Encrypt(h.cfg.EncryptionKey, []byte(secret))
	if _, err := h.store.Pool.Exec(r.Context(), `UPDATE crm.webhooks SET secret_enc = $2, updated_at = now() WHERE id = $1`, id, enc); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	wh.Secret = secret
	shared.WriteJSON(w, http.StatusOK, wh)
}

func (h *Handler) handleDeleteWebhook(w http.ResponseWriter, r *http.Request) {
	sc, _, ok := h.requireDeveloper(w, r, true)
	if !ok {
		return
	}
	id, _ := uuid.Parse(chi.URLParam(r, "hookId"))
	tag, err := h.store.Pool.Exec(r.Context(), `DELETE FROM crm.webhooks WHERE id = $1 AND workspace_id = $2`, id, sc.WS)
	if err != nil || tag.RowsAffected() == 0 {
		shared.WriteError(w, r, shared.NotFound("webhook_not_found"))
		return
	}
	_ = shared.WriteAudit(r.Context(), h.store.Pool, actorFromRequest(r, "ui").audit(sc.WS, "webhook.deleted", "webhook", &id, nil, nil))
	w.WriteHeader(http.StatusNoContent)
}

// POST /developer/webhooks/{hookId}/test — sends a sample event right away.
func (h *Handler) handleTestWebhook(w http.ResponseWriter, r *http.Request) {
	sc, _, ok := h.requireDeveloper(w, r, true)
	if !ok {
		return
	}
	id, _ := uuid.Parse(chi.URLParam(r, "hookId"))
	var enc []byte
	var u string
	if err := h.store.Pool.QueryRow(r.Context(), `SELECT url, secret_enc FROM crm.webhooks WHERE id = $1 AND workspace_id = $2`, id, sc.WS).Scan(&u, &enc); err != nil {
		shared.WriteError(w, r, shared.NotFound("webhook_not_found"))
		return
	}
	secret, _ := shared.Decrypt(h.cfg.EncryptionKey, enc)
	eventID := uuid.New()
	payload, _ := json.Marshal(map[string]any{"id": eventID, "event": "webhook.test", "workspace": sc.Code, "occurredAt": time.Now().UTC(),
		"record": map[string]any{"id": uuid.New(), "title": "Sample record"}})
	var did int64
	if err := h.store.Pool.QueryRow(r.Context(), `INSERT INTO crm.webhook_deliveries (webhook_id, workspace_id, event_id, event_type, payload, status)
		VALUES ($1, $2, $3, 'webhook.test', $4, 'pending') RETURNING id`, id, sc.WS, eventID, payload).Scan(&did); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	// A test is one attempt only.
	status, body := h.sendDelivery(r.Context(), dueDelivery{id: did, url: u, secret: secret, eventID: eventID, event: "webhook.test", payload: payload, attempts: len(retryDelays)})
	shared.WriteJSON(w, http.StatusOK, map[string]any{"status": status, "ok": status >= 200 && status < 300, "response": body})
}

type Delivery struct {
	ID             int64      `json:"id"`
	EventID        uuid.UUID  `json:"eventId"`
	Event          string     `json:"event"`
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	ResponseStatus *int       `json:"responseStatus,omitempty"`
	ResponseBody   string     `json:"responseBody,omitempty"`
	NextAttemptAt  *time.Time `json:"nextAttemptAt,omitempty"`
	CreatedAt      time.Time  `json:"createdAt"`
	DeliveredAt    *time.Time `json:"deliveredAt,omitempty"`
	Payload        any        `json:"payload,omitempty"`
}

func (h *Handler) handleListDeliveries(w http.ResponseWriter, r *http.Request) {
	sc, _, ok := h.requireDeveloper(w, r, true)
	if !ok {
		return
	}
	id, _ := uuid.Parse(chi.URLParam(r, "hookId"))
	rows, err := h.store.Pool.Query(r.Context(), `SELECT d.id, d.event_id, d.event_type, d.status, d.attempts, d.response_status, COALESCE(d.response_body, ''),
		d.next_attempt_at, d.created_at, d.delivered_at, d.payload
		FROM crm.webhook_deliveries d JOIN crm.webhooks w ON w.id = d.webhook_id
		WHERE d.webhook_id = $1 AND w.workspace_id = $2 ORDER BY d.id DESC LIMIT 100`, id, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []Delivery{}
	for rows.Next() {
		var d Delivery
		var raw []byte
		if err := rows.Scan(&d.ID, &d.EventID, &d.Event, &d.Status, &d.Attempts, &d.ResponseStatus, &d.ResponseBody, &d.NextAttemptAt, &d.CreatedAt, &d.DeliveredAt, &raw); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		_ = json.Unmarshal(raw, &d.Payload)
		out = append(out, d)
	}
	respond(w, r, http.StatusOK, map[string]any{"data": out}, rows.Err())
}

// POST /developer/webhooks/{hookId}/deliveries/{deliveryId}/retry
func (h *Handler) handleRetryDelivery(w http.ResponseWriter, r *http.Request) {
	sc, _, ok := h.requireDeveloper(w, r, true)
	if !ok {
		return
	}
	did, _ := strconv.ParseInt(chi.URLParam(r, "deliveryId"), 10, 64)
	tag, err := h.store.Pool.Exec(r.Context(), `UPDATE crm.webhook_deliveries SET status = 'pending', attempts = 0, next_attempt_at = now()
		WHERE id = $1 AND workspace_id = $2`, did, sc.WS)
	if err != nil || tag.RowsAffected() == 0 {
		shared.WriteError(w, r, shared.NotFound("delivery_not_found"))
		return
	}
	h.bus.Kick()
	w.WriteHeader(http.StatusAccepted)
	_, _ = fmt.Fprint(w, `{"ok":true}`)
}
