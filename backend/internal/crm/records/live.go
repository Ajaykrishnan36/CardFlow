package records

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Live updates and notifications (D-60). A page keeps one server-sent-events stream per
// workspace open; the worker pushes "this record changed" (pages refetch what they show,
// so permissions are applied by the normal APIs) and new notifications for the viewer.
// Notifications: someone assigned you a record, mentioned you, an import finished, or a
// workflow sent you one.

type Notification struct {
	ID        int64      `json:"id"`
	Kind      string     `json:"kind"`
	Title     string     `json:"title"`
	Body      string     `json:"body,omitempty"`
	Link      string     `json:"link,omitempty"`
	Actor     string     `json:"actor,omitempty"`
	ReadAt    *time.Time `json:"readAt,omitempty"`
	CreatedAt time.Time  `json:"createdAt"`
}

// notify stores a notification and pushes it to the person's open pages.
func (h *Handler) notify(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, ws, to uuid.UUID, kind, title, body, link string, actorID *uuid.UUID) {
	if to == uuid.Nil {
		return
	}
	if len(body) > 500 {
		body = body[:500] + "…"
	}
	var n Notification
	err := q.QueryRow(ctx, `INSERT INTO crm.notifications (workspace_id, identity_id, kind, title, body, link, actor_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id, created_at`, ws, to, kind, title, body, link, actorID).Scan(&n.ID, &n.CreatedAt)
	if err != nil {
		return
	}
	n.Kind, n.Title, n.Body, n.Link = kind, title, body, link
	h.bus.publish(uuid.Nil, to, liveMessage{Kind: "notification", Data: n})
}

// notifyForEvent: a record assigned to someone else tells its new owner.
func (h *Handler) notifyForEvent(ctx context.Context, tx pgx.Tx, ev Event) error {
	if ev.Type != "record.updated" && ev.Type != "record.created" {
		return nil
	}
	assigned := ev.Type == "record.created"
	for _, c := range ev.Changed {
		if c == "ownerId" {
			assigned = true
		}
	}
	if !assigned {
		return nil
	}
	ownerStr, _ := ev.Record["ownerId"].(string)
	owner, err := uuid.Parse(ownerStr)
	if err != nil || (ev.ActorID != nil && *ev.ActorID == owner) {
		return nil
	}
	spec := specFor(ev.Object)
	if spec == nil {
		return nil
	}
	var code string
	var isPlatform bool
	if err := tx.QueryRow(ctx, `SELECT code, is_platform FROM crm.workspaces WHERE id = $1`, ev.WorkspaceID).Scan(&code, &isPlatform); err != nil {
		return nil
	}
	link := "/crm/w/" + code + "/" + ev.Object + "/" + ev.RecordID.String()
	if isPlatform {
		link = "/crm/owner/" + ev.Object + "/" + ev.RecordID.String()
	}
	by := "Someone"
	if ev.ActorID != nil {
		_ = tx.QueryRow(ctx, `SELECT display_name FROM crm.identities WHERE id = $1`, *ev.ActorID).Scan(&by)
	} else if ev.Source == "workflow" {
		by = "A workflow"
	}
	h.notify(ctx, tx, ev.WorkspaceID, owner, "assigned", fmt.Sprintf("%s assigned you %s %s", by, articleFor(spec.Singular), spec.Singular), ev.Title, link, ev.ActorID)
	return nil
}

func articleFor(word string) string {
	if word == "" {
		return "a"
	}
	switch word[0] {
	case 'A', 'E', 'I', 'O', 'U', 'a', 'e', 'i', 'o', 'u':
		return "an"
	}
	return "a"
}

// GET /notifications?unread=1&before=<id>
func (h *Handler) handleListNotifications(w http.ResponseWriter, r *http.Request) {
	me := actor(r)
	before := int64(1 << 62)
	if b, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64); err == nil {
		before = b
	}
	onlyUnread := r.URL.Query().Get("unread") == "1"
	rows, err := h.store.Pool.Query(r.Context(), `SELECT n.id, n.kind, n.title, n.body, n.link, COALESCE(i.display_name, ''), n.read_at, n.created_at
		FROM crm.notifications n LEFT JOIN crm.identities i ON i.id = n.actor_id
		WHERE n.identity_id = $1 AND n.id < $2 AND ($3 = false OR n.read_at IS NULL) ORDER BY n.id DESC LIMIT 30`, me, before, onlyUnread)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		var n Notification
		if err := rows.Scan(&n.ID, &n.Kind, &n.Title, &n.Body, &n.Link, &n.Actor, &n.ReadAt, &n.CreatedAt); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out = append(out, n)
	}
	var unread int
	_ = h.store.Pool.QueryRow(r.Context(), `SELECT count(*) FROM crm.notifications WHERE identity_id = $1 AND read_at IS NULL`, me).Scan(&unread)
	respond(w, r, http.StatusOK, map[string]any{"data": out, "unread": unread}, rows.Err())
}

// POST /notifications/read {"ids":[…]} or {"all":true}
func (h *Handler) handleReadNotifications(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
		All bool    `json:"all"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	me := actor(r)
	var err error
	if in.All {
		_, err = h.store.Pool.Exec(r.Context(), `UPDATE crm.notifications SET read_at = now() WHERE identity_id = $1 AND read_at IS NULL`, me)
	} else {
		_, err = h.store.Pool.Exec(r.Context(), `UPDATE crm.notifications SET read_at = now() WHERE identity_id = $1 AND id = ANY($2) AND read_at IS NULL`, me, in.IDs)
	}
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.handleListNotifications(w, r)
}

// GET /stream — server-sent events for the workspace and the viewer.
func (h *Handler) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		shared.WriteError(w, r, shared.NewError(http.StatusNotImplemented, "no_streaming", "Live updates aren't available."))
		return
	}
	sc := scopeFrom(r.Context())
	sub := h.bus.subscribe(sc.WS, actor(r))
	defer h.bus.unsubscribe(sub)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache, no-transform")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprint(w, "retry: 5000\n: connected\n\n")
	flusher.Flush()
	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()
	// Streams end after a while so a page never holds an old session forever; the browser reconnects.
	deadline := time.NewTimer(30 * time.Minute)
	defer deadline.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-deadline.C:
			return
		case <-ping.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case m := <-sub.ch:
			raw, _ := json.Marshal(m.Data)
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", m.Kind, raw); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
