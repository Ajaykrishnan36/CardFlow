package cardflow

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/records"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ---- support tickets (workspace members with ticket read / update) ----

type LookupValue struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Object string `json:"object"`
}

type Ticket struct {
	ID        string     `json:"id"`
	Subject   string     `json:"subject"`
	Message   string     `json:"message"`
	Category  string     `json:"category"`
	Status    string     `json:"status"`
	Reply     string     `json:"reply,omitempty"`
	RepliedAt *time.Time `json:"repliedAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
	UpdatedAt time.Time  `json:"updatedAt"`
	User      struct {
		Name       string `json:"name"`
		Phone      string `json:"phone"`
		Role       string `json:"role"`
		ExternalID string `json:"externalId,omitempty"`
	} `json:"user"`
	Account *LookupValue `json:"account,omitempty"`
	Contact *LookupValue `json:"contact,omitempty"`
	Source  string       `json:"source"`
}

const ticketSelect = `
	SELECT t.id, t.subject, t.message, t.category, t.status, COALESCE(t.admin_reply, ''), t.replied_at, t.created_at, t.updated_at,
	       t.user_name, t.user_phone, t.user_role, COALESCE(t.user_id::text, ''),
	       a.id::text, a.name || ' · ' || a.code, c.id::text,
	       COALESCE(NULLIF(trim(concat_ws(' ', c.first_name, c.last_name)), ''), c.code) || ' · ' || c.code
	FROM public.support_tickets t
	LEFT JOIN crm.external_links l ON l.system = 'cardflow' AND l.external_type = 'user' AND l.external_id = t.user_id::text
	LEFT JOIN crm.accounts a ON a.id = l.account_id AND a.deleted_at IS NULL
	LEFT JOIN crm.contacts c ON c.id = l.contact_id AND c.deleted_at IS NULL`

func scanTicket(row pgx.Row) (Ticket, error) {
	var t Ticket
	var accID, accLabel, conID, conLabel *string
	err := row.Scan(&t.ID, &t.Subject, &t.Message, &t.Category, &t.Status, &t.Reply, &t.RepliedAt, &t.CreatedAt, &t.UpdatedAt,
		&t.User.Name, &t.User.Phone, &t.User.Role, &t.User.ExternalID, &accID, &accLabel, &conID, &conLabel)
	if err != nil {
		return t, err
	}
	if accID != nil {
		t.Account = &LookupValue{ID: *accID, Label: *accLabel, Object: "accounts"}
	}
	if conID != nil {
		t.Contact = &LookupValue{ID: *conID, Label: *conLabel, Object: "contacts"}
	}
	t.Source = AppName
	return t, nil
}

// supportScope checks the request is for the connected workspace with the ticket action.
func (c *Connector) supportScope(w http.ResponseWriter, r *http.Request, action string) (*records.Scope, bool) {
	sc := records.ScopeFrom(r.Context())
	if sc == nil || sc.WS != c.WorkspaceID() || !c.hasTickets {
		shared.WriteError(w, r, shared.NotFound("support_not_connected"))
		return nil, false
	}
	if !sc.Owner && !sc.Eff.Can("ticket", action) {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "You don't have permission to do that."))
		return nil, false
	}
	return sc, true
}

func (c *Connector) handleListTickets(w http.ResponseWriter, r *http.Request) {
	if _, ok := c.supportScope(w, r, "read"); !ok {
		return
	}
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	status := r.URL.Query().Get("status")
	if status != "open" && status != "in_progress" && status != "resolved" {
		status = ""
	}
	like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
	rows, err := c.store.Pool.Query(ctx, ticketSelect+`
		WHERE ($1 = '' OR t.subject ILIKE $2 OR t.message ILIKE $2 OR t.user_name ILIKE $2 OR t.user_phone ILIKE $2 OR t.id ILIKE $2)
		  AND ($3 = '' OR t.status = $3)
		ORDER BY (t.status = 'resolved'), t.updated_at DESC LIMIT 200`, q, like, status)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	list := []Ticket{}
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			rows.Close()
			shared.WriteError(w, r, err)
			return
		}
		list = append(list, t)
	}
	rows.Close()
	var counts struct {
		All        int `json:"all"`
		Open       int `json:"open"`
		InProgress int `json:"in_progress"`
		Resolved   int `json:"resolved"`
	}
	if err := c.store.Pool.QueryRow(ctx, `
		SELECT count(*), count(*) FILTER (WHERE status = 'open'), count(*) FILTER (WHERE status = 'in_progress'),
		       count(*) FILTER (WHERE status = 'resolved') FROM public.support_tickets`).
		Scan(&counts.All, &counts.Open, &counts.InProgress, &counts.Resolved); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": list, "counts": counts})
}

func (c *Connector) handleGetTicket(w http.ResponseWriter, r *http.Request) {
	if _, ok := c.supportScope(w, r, "read"); !ok {
		return
	}
	t, err := scanTicket(c.store.Pool.QueryRow(r.Context(), ticketSelect+` WHERE t.id = $1`, chi.URLParam(r, "id")))
	if errors.Is(err, pgx.ErrNoRows) {
		shared.WriteError(w, r, shared.NotFound("ticket_not_found"))
		return
	}
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, t)
}

func (c *Connector) handleUpdateTicket(w http.ResponseWriter, r *http.Request) {
	sc, ok := c.supportScope(w, r, "update")
	if !ok {
		return
	}
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	var in struct {
		Status *string `json:"status"`
		Reply  *string `json:"reply"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	status, reply := "", ""
	if in.Status != nil {
		status = *in.Status
		if status != "open" && status != "in_progress" && status != "resolved" {
			shared.WriteError(w, r, shared.Validation(map[string]string{"status": "Pick open, in progress or resolved."}))
			return
		}
	}
	if in.Reply != nil {
		reply = strings.TrimSpace(*in.Reply)
		if len(reply) > 4000 {
			shared.WriteError(w, r, shared.Validation(map[string]string{"reply": "Use at most 4000 characters."}))
			return
		}
	}
	if status == "" && reply == "" {
		shared.WriteError(w, r, shared.Validation(map[string]string{"reply": "Write a reply or change the status."}))
		return
	}
	sess := identity.SessionFrom(ctx)
	actor := sess.IdentityID
	err := c.store.WithTx(ctx, func(tx pgx.Tx) error {
		var userID *string
		var subject string
		err := tx.QueryRow(ctx, `
			UPDATE public.support_tickets
			SET status = COALESCE(NULLIF($2, ''), CASE WHEN $3 <> '' AND status = 'open' THEN 'in_progress' ELSE status END),
			    admin_reply = COALESCE(NULLIF($3, ''), admin_reply),
			    replied_at = CASE WHEN $3 <> '' THEN now() ELSE replied_at END,
			    replied_by = CASE WHEN $3 <> '' THEN $4 ELSE replied_by END,
			    updated_at = now()
			WHERE id = $1 RETURNING user_id::text, subject`, id, status, reply, sess.DisplayName+" (CRM)").Scan(&userID, &subject)
		if errors.Is(err, pgx.ErrNoRows) {
			return shared.NotFound("ticket_not_found")
		}
		if err != nil {
			return err
		}
		ws := sc.WS
		if userID != nil && reply != "" {
			var l link
			if err := tx.QueryRow(ctx, `SELECT lead_id, account_id, contact_id FROM crm.external_links
				WHERE system = $1 AND external_type = 'user' AND external_id = $2`, System, *userID).Scan(&l.leadID, &l.accountID, &l.contactID); err == nil {
				if err := addActivity(ctx, tx, ws, l, "ticket.replied", "Support replied: "+subject, time.Now(),
					"cardflow:reply:"+id+":"+time.Now().Format(time.RFC3339Nano), map[string]any{"ticketId": id, "by": sess.DisplayName}); err != nil {
					return err
				}
			}
		}
		meta := identity.Meta(r)
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &ws, ActorID: &actor, Action: "ticket.updated", EntityType: "ticket",
			After: map[string]any{"ticketId": id, "status": status, "replied": reply != ""}, IP: meta.IP, RequestID: meta.RequestID})
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	t, err := scanTicket(c.store.Pool.QueryRow(ctx, ticketSelect+` WHERE t.id = $1`, id))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, t)
}

// Extension wires tickets into the record engine: member routes, the "Support tickets"
// related list on app users' records, and an open-tickets KPI.
func (c *Connector) Extension() records.Extension {
	return records.Extension{
		MemberRoutes: func(r chi.Router) {
			r.Get("/support/tickets", c.handleListTickets)
			r.Get("/support/tickets/{id}", c.handleGetTicket)
			r.Patch("/support/tickets/{id}", c.handleUpdateTicket)
		},
		Related: func(ctx context.Context, sc *records.Scope, object, recordID string) ([]records.RelatedList, error) {
			if sc.WS != c.WorkspaceID() || !c.hasTickets || object == "leads" {
				return nil, nil
			}
			col := map[string]string{"accounts": "account_id", "contacts": "contact_id"}[object]
			rows, err := c.store.Pool.Query(ctx, `
				SELECT t.id, t.id, t.subject, t.category, t.status FROM public.support_tickets t
				JOIN crm.external_links l ON l.system = 'cardflow' AND l.external_type = 'user' AND l.external_id = t.user_id::text
				WHERE l.`+col+` = $1 ORDER BY t.created_at DESC LIMIT 50`, recordID)
			if err != nil {
				return nil, err
			}
			defer rows.Close()
			list := records.RelatedList{Key: "tickets", Label: "Support tickets", Object: "tickets", Rows: []records.RelatedRow{}}
			for rows.Next() {
				var rr records.RelatedRow
				if err := rows.Scan(&rr.ID, &rr.Code, &rr.Title, &rr.Subtitle, &rr.Status); err != nil {
					return nil, err
				}
				list.Rows = append(list.Rows, rr)
			}
			return []records.RelatedList{list}, rows.Err()
		},
		KPIs: func(ctx context.Context, sc *records.Scope) ([]records.KPI, error) {
			if sc.WS != c.WorkspaceID() || !c.hasTickets || (!sc.Owner && !sc.Eff.Can("ticket", "read")) {
				return nil, nil
			}
			var open, today int
			if err := c.store.Pool.QueryRow(ctx, `
				SELECT count(*) FILTER (WHERE status <> 'resolved'), count(*) FILTER (WHERE created_at >= date_trunc('day', now() AT TIME ZONE 'Asia/Kolkata') AT TIME ZONE 'Asia/Kolkata')
				FROM public.support_tickets`).Scan(&open, &today); err != nil {
				return nil, err
			}
			k := records.KPI{Key: "tickets", Label: "Open tickets", Value: open, Path: "/crm/w/" + sc.Code + "/support"}
			if today > 0 {
				k.Hint = strconv.Itoa(today) + " new today"
			}
			return []records.KPI{k}, nil
		},
	}
}

// ---- integrations (owner) ----

type IntegrationInfo struct {
	Key             string     `json:"key"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	Status          string     `json:"status"`
	Workspace       *wsRef     `json:"workspace,omitempty"`
	LastSyncAt      *time.Time `json:"lastSyncAt,omitempty"`
	LastError       string     `json:"lastError,omitempty"`
	IntervalSeconds int        `json:"intervalSeconds"`
	Stats           struct {
		AppUsers     int `json:"appUsers"`
		Accounts     int `json:"accounts"`
		Contacts     int `json:"contacts"`
		Leads        int `json:"leads"`
		Admins       int `json:"admins"`
		TicketsOpen  int `json:"ticketsOpen"`
		TicketsTotal int `json:"ticketsTotal"`
		SignInsToday int `json:"signInsToday"`
		NewToday     int `json:"newToday"`
	} `json:"stats"`
}

type wsRef struct {
	ID   uuid.UUID `json:"id"`
	Code string    `json:"code"`
	Name string    `json:"name"`
}

func (c *Connector) Info(ctx context.Context) IntegrationInfo {
	c.mu.RLock()
	ready, wsID, lastSync, lastErr := c.ready, c.wsID, c.lastSyncAt, c.lastError
	c.mu.RUnlock()
	info := IntegrationInfo{
		Key: System, Name: AppName, IntervalSeconds: int(syncInterval.Seconds()),
		Description: "The CardFlow app. Every sign-up (phone + OTP) becomes a lead converted into an account and contact; sign-ins are logged; support tickets are answered here.",
		Status:      "active", LastError: lastErr,
	}
	if !lastSync.IsZero() {
		info.LastSyncAt = &lastSync
	}
	if lastErr != "" {
		info.Status = "error"
	}
	if !ready {
		if lastErr == "" {
			info.Status = "disabled"
		}
		return info
	}
	var ws wsRef
	if err := c.store.Pool.QueryRow(ctx, `SELECT id, code, name FROM crm.workspaces WHERE id = $1`, wsID).Scan(&ws.ID, &ws.Code, &ws.Name); err == nil {
		info.Workspace = &ws
	}
	s := &info.Stats
	today := `date_trunc('day', now() AT TIME ZONE 'Asia/Kolkata') AT TIME ZONE 'Asia/Kolkata'`
	_ = c.store.Pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE deleted_at IS NULL), count(*) FILTER (WHERE role::text = 'admin' AND deleted_at IS NULL),
		       count(*) FILTER (WHERE last_login_at >= `+today+`), count(*) FILTER (WHERE created_at >= `+today+`)
		FROM public.users`).Scan(&s.AppUsers, &s.Admins, &s.SignInsToday, &s.NewToday)
	_ = c.store.Pool.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM crm.accounts WHERE workspace_id = $1 AND deleted_at IS NULL),
		       (SELECT count(*) FROM crm.contacts WHERE workspace_id = $1 AND deleted_at IS NULL),
		       (SELECT count(*) FROM crm.leads WHERE workspace_id = $1 AND deleted_at IS NULL)`, wsID).Scan(&s.Accounts, &s.Contacts, &s.Leads)
	if c.hasTickets {
		_ = c.store.Pool.QueryRow(ctx, `SELECT count(*) FILTER (WHERE status <> 'resolved'), count(*) FROM public.support_tickets`).
			Scan(&s.TicketsOpen, &s.TicketsTotal)
	}
	return info
}

// OwnerRoutes mounts /platform/integrations (the caller applies RequireOwner).
func (c *Connector) OwnerRoutes(r chi.Router) {
	r.Get("/platform/integrations", func(w http.ResponseWriter, r *http.Request) {
		shared.WriteJSON(w, http.StatusOK, map[string]any{"data": []IntegrationInfo{c.Info(r.Context())}})
	})
	r.Post("/platform/integrations/cardflow/sync", func(w http.ResponseWriter, r *http.Request) {
		c.setError(c.Sync(r.Context()))
		shared.WriteJSON(w, http.StatusOK, c.Info(r.Context()))
	})
}
