package cardflow

import (
	"context"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// A ticket is a conversation (D-91): the ticket's own message, then every follow-up from the
// person and every support reply (public.support_ticket_messages, app migration 015). The case
// page reads and answers it here; each reply records who answered and their role.

// TicketMessage is one entry in a ticket's conversation.
type TicketMessage struct {
	ID         string    `json:"id"`
	Sender     string    `json:"sender"` // user | support
	AuthorName string    `json:"authorName"`
	AuthorRole string    `json:"authorRole,omitempty"`
	Body       string    `json:"body"`
	CreatedAt  time.Time `json:"createdAt"`
}

// TicketThread is a ticket with its conversation, oldest message first.
type TicketThread struct {
	ID       string          `json:"id"`
	Subject  string          `json:"subject"`
	Status   string          `json:"status"`
	UserName string          `json:"userName"`
	Phone    string          `json:"phone"`
	Messages []TicketMessage `json:"messages"`
}

type pgxQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// supportAuthor is how a CRM person appears on a reply: their name and their role in the product.
func supportAuthor(ctx context.Context, q pgxQuerier, wsID, identityID uuid.UUID) (name, role string) {
	name, role = "Support team", "Support team"
	_ = q.QueryRow(ctx, `
		SELECT i.display_name,
		       CASE WHEN i.is_platform_owner THEN 'Platform owner' ELSE COALESCE((
		         SELECT r.name FROM crm.memberships m
		         JOIN crm.role_assignments ra ON ra.membership_id = m.id
		         JOIN crm.roles r ON r.id = ra.role_id
		         WHERE m.workspace_id = $2 AND m.identity_id = i.id ORDER BY r.rank DESC LIMIT 1), 'Support team') END
		FROM crm.identities i WHERE i.id = $1`, identityID, wsID).Scan(&name, &role)
	return name, role
}

func (c *Connector) loadThread(ctx context.Context, id string) (*TicketThread, error) {
	t := &TicketThread{ID: id}
	var opening string
	var created time.Time
	err := c.store.Pool.QueryRow(ctx, `
		SELECT subject, status, user_name, user_phone, message, created_at FROM public.support_tickets WHERE id = $1`, id).
		Scan(&t.Subject, &t.Status, &t.UserName, &t.Phone, &opening, &created)
	if err == pgx.ErrNoRows {
		return nil, shared.NotFound("ticket_not_found")
	}
	if err != nil {
		return nil, err
	}
	t.Messages = []TicketMessage{{ID: id + "-0", Sender: "user", AuthorName: t.UserName, AuthorRole: "Customer", Body: opening, CreatedAt: created}}
	rows, err := c.store.Pool.Query(ctx, `
		SELECT id::text, sender, author_name, author_role, body, created_at
		FROM public.support_ticket_messages WHERE ticket_id = $1 ORDER BY created_at, id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var m TicketMessage
		if err := rows.Scan(&m.ID, &m.Sender, &m.AuthorName, &m.AuthorRole, &m.Body, &m.CreatedAt); err != nil {
			return nil, err
		}
		if m.Sender == "user" && m.AuthorRole == "" {
			m.AuthorRole = "Customer"
		}
		t.Messages = append(t.Messages, m)
	}
	return t, rows.Err()
}

// GET /w/{code}/app/tickets/{id}
func (c *Connector) handleGetThread(w http.ResponseWriter, r *http.Request) {
	if _, ok := c.appScope(w, r, "cases", "read"); !ok {
		return
	}
	// The person may have written since the last sync; bring the case up to date too.
	c.SyncOnDemand()
	t, err := c.loadThread(r.Context(), chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, t)
}

// POST /w/{code}/app/tickets/{id}/messages — a support reply. The person sees it in the app at
// once; an open ticket moves to "in progress" and the case follows on the next sync.
func (c *Connector) handleReplyThread(w http.ResponseWriter, r *http.Request) {
	sc, ok := c.appScope(w, r, "cases", "update")
	if !ok {
		return
	}
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	var in struct {
		Body string `json:"body"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	body := strings.TrimSpace(in.Body)
	if body == "" || len(body) > 4000 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"body": "Write a reply up to 4000 characters."}))
		return
	}
	actor := identity.SessionFrom(ctx).IdentityID
	err := c.store.WithTx(ctx, func(tx pgx.Tx) error {
		name, role := supportAuthor(ctx, tx, sc.WS, actor)
		tag, err := tx.Exec(ctx, `
			UPDATE public.support_tickets
			SET admin_reply = $2, replied_at = now(), replied_by = $3,
			    status = CASE WHEN status = 'open' THEN 'in_progress' ELSE status END, updated_at = now()
			WHERE id = $1`, id, body, name)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return shared.NotFound("ticket_not_found")
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO public.support_ticket_messages (ticket_id, sender, author_name, author_role, body)
			VALUES ($1, 'support', $2, $3, $4)`, id, name, role, body); err != nil {
			return err
		}
		ws := sc.WS
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &ws, ActorID: &actor, Action: "app_ticket.replied",
			EntityType: "app_ticket", After: map[string]any{"ticketId": id, "app": AppName}})
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	c.TriggerSync()
	t, err := c.loadThread(ctx, id)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusCreated, t)
}
