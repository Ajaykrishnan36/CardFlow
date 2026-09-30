package records

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Events (D-55). Every record change writes an event to crm.outbox_events in the same
// transaction, then kicks the in-process worker. The worker relays events to live
// pages (server-sent events), webhooks, workflows and notifications, and runs delayed
// work (webhook retries, waiting workflow runs, schedules, campaigns).
//
// Nothing polls: the worker sleeps until it is kicked or until the earliest time some
// stored work is due, so an idle CRM sends no queries and the serverless database can
// suspend. On start it catches up with anything left from before a restart.

type Event struct {
	ID          uuid.UUID      `json:"id"`
	Type        string         `json:"type"` // record.created | record.updated | record.deleted | record.restored | record.destroyed
	WorkspaceID uuid.UUID      `json:"workspaceId"`
	Object      string         `json:"object"`
	RecordID    uuid.UUID      `json:"recordId"`
	ActorID     *uuid.UUID     `json:"actorId,omitempty"`
	Source      string         `json:"source,omitempty"` // ui | api | import | bulk | workflow | app
	Record      map[string]any `json:"record,omitempty"`
	Previous    map[string]any `json:"previous,omitempty"`
	Changed     []string       `json:"changed,omitempty"`
	Title       string         `json:"title,omitempty"`
	At          time.Time      `json:"occurredAt"`
	// Set when a workflow made the change: it never triggers itself, and chains stop at 5.
	WorkflowID *uuid.UUID `json:"workflowId,omitempty"`
	Depth      int        `json:"depth,omitempty"`
}

// emitEvent stores an event with the change that caused it.
func emitEvent(ctx context.Context, tx pgx.Tx, ev Event) error {
	if ev.ID == uuid.Nil {
		ev.ID = uuid.New()
	}
	if ev.At.IsZero() {
		ev.At = time.Now().UTC()
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO crm.outbox_events (event_id, workspace_id, event_type, actor_id, entity_type, entity_id, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		ev.ID, ev.WorkspaceID, ev.Type, ev.ActorID, ev.Object, ev.RecordID, payload, ev.At)
	return err
}

// ---- live updates (server-sent events) ----

type liveMessage struct {
	Kind string `json:"kind"` // record | notification
	Data any    `json:"data"`
}

type subscriber struct {
	ws       uuid.UUID
	identity uuid.UUID
	ch       chan liveMessage
}

type Bus struct {
	kick chan struct{}

	mu   sync.Mutex
	subs map[*subscriber]struct{}

	timerMu sync.Mutex
	timer   *time.Timer
	due     time.Time
}

func newBus() *Bus {
	return &Bus{kick: make(chan struct{}, 1), subs: map[*subscriber]struct{}{}}
}

// Kick wakes the worker (never blocks).
func (b *Bus) Kick() {
	select {
	case b.kick <- struct{}{}:
	default:
	}
}

// wakeAt makes sure the worker runs again by t (the earliest wins).
func (b *Bus) wakeAt(t time.Time) {
	b.timerMu.Lock()
	defer b.timerMu.Unlock()
	if !b.due.IsZero() && !t.Before(b.due) {
		return
	}
	d := time.Until(t)
	if d < 0 {
		d = 0
	}
	if b.timer != nil {
		b.timer.Stop()
	}
	b.due = t
	b.timer = time.AfterFunc(d, func() {
		b.timerMu.Lock()
		b.due = time.Time{}
		b.timerMu.Unlock()
		b.Kick()
	})
}

func (b *Bus) subscribe(ws, identity uuid.UUID) *subscriber {
	s := &subscriber{ws: ws, identity: identity, ch: make(chan liveMessage, 64)}
	b.mu.Lock()
	b.subs[s] = struct{}{}
	b.mu.Unlock()
	return s
}

func (b *Bus) unsubscribe(s *subscriber) {
	b.mu.Lock()
	delete(b.subs, s)
	b.mu.Unlock()
}

// publish sends a message to live pages of a workspace (identity = uuid.Nil: everyone there).
func (b *Bus) publish(ws, identity uuid.UUID, m liveMessage) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for s := range b.subs {
		if (ws != uuid.Nil && s.ws != ws) || (identity != uuid.Nil && s.identity != identity) {
			continue
		}
		select {
		case s.ch <- m:
		default: // a slow page catches up on its next refetch
		}
	}
}

// ---- the worker ----

// StartWorker runs the event relay and scheduled work until ctx ends.
func (h *Handler) StartWorker(ctx context.Context) {
	go func() {
		// Catch up after a restart (one round of queries), then sleep until kicked.
		h.workOnce(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-h.bus.kick:
				h.workOnce(ctx)
			}
		}
	}()
}

func (h *Handler) workOnce(ctx context.Context) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("CRM worker panic", "error", r)
		}
	}()
	c, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	for i := 0; i < 20; i++ { // drain bursts, but never spin forever
		n, err := h.relayOutbox(c)
		if err != nil {
			slog.Error("CRM event relay", "error", err)
			break
		}
		if n == 0 {
			break
		}
	}
	h.deliverDueWebhooks(c)
	h.runDueWorkflows(c)
	h.sendDueCampaigns(c)
	h.syncDueMailboxes(c)
	h.purgeRecycleBins(c)
	h.scheduleNextWake(c)
}

// scheduleNextWake finds the earliest stored work and sets the wake-up timer for it.
func (h *Handler) scheduleNextWake(ctx context.Context) {
	var next *time.Time
	err := h.store.Pool.QueryRow(ctx, `
		SELECT min(t) FROM (
			SELECT min(next_attempt_at) AS t FROM crm.webhook_deliveries WHERE status = 'pending'
			UNION ALL SELECT min(resume_at) FROM crm.workflow_runs WHERE status IN ('queued', 'waiting')
			UNION ALL SELECT min(next_run_at) FROM crm.workflows WHERE status = 'active' AND next_run_at IS NOT NULL
			UNION ALL SELECT min(scheduled_at) FROM crm.campaigns WHERE status = 'scheduled'
			UNION ALL SELECT min(COALESCE(last_synced_at, now() - interval '2 hours') + interval '1 hour') FROM crm.mail_accounts WHERE status = 'active'
			UNION ALL SELECT CASE WHEN EXISTS (SELECT 1 FROM crm.workspaces WHERE bin_retention_days IS NOT NULL) THEN $1::timestamptz END
		) x`, nextBinPurge()).Scan(&next)
	if err != nil {
		slog.Error("CRM worker schedule", "error", err)
		return
	}
	if next != nil {
		t := *next
		if time.Until(t) < 2*time.Second {
			t = time.Now().Add(2 * time.Second)
		}
		h.bus.wakeAt(t)
	}
}

// relayOutbox hands pending events to live pages, webhooks, workflows and notifications.
func (h *Handler) relayOutbox(ctx context.Context) (int, error) {
	var events []Event
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, payload FROM crm.outbox_events WHERE relayed_at IS NULL ORDER BY id LIMIT 200 FOR UPDATE SKIP LOCKED`)
		if err != nil {
			return err
		}
		var ids []int64
		for rows.Next() {
			var id int64
			var raw []byte
			if err := rows.Scan(&id, &raw); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
			var ev Event
			if json.Unmarshal(raw, &ev) == nil && ev.WorkspaceID != uuid.Nil {
				events = append(events, ev)
			}
		}
		rows.Close()
		if len(ids) == 0 {
			return nil
		}
		for _, ev := range events {
			if err := h.queueWebhooks(ctx, tx, ev); err != nil {
				return err
			}
			if err := h.triggerWorkflows(ctx, tx, ev); err != nil {
				return err
			}
			if err := h.notifyForEvent(ctx, tx, ev); err != nil {
				return err
			}
			for _, x := range h.extensions {
				if x.OnEvent != nil {
					if err := x.OnEvent(ctx, tx, ev); err != nil {
						return err
					}
				}
			}
		}
		_, err = tx.Exec(ctx, `UPDATE crm.outbox_events SET relayed_at = now(), attempts = attempts + 1 WHERE id = ANY($1)`, ids)
		return err
	})
	if err != nil {
		return 0, err
	}
	for _, ev := range events {
		h.bus.publish(ev.WorkspaceID, uuid.Nil, liveMessage{Kind: "record", Data: map[string]any{
			"type": ev.Type, "object": ev.Object, "id": ev.RecordID, "actorId": ev.ActorID,
		}})
		if ev.Object == "events" && ev.Type == "record.created" {
			go func(ev Event) {
				c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				h.pushEventToCalendar(c, ev.WorkspaceID, ev)
			}(ev)
		}
	}
	return len(events), nil
}
