package cardflow

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"cardflow-backend/internal/crm/records"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// App support tickets are Cases (D-72). Every ticket from the app is mirrored as a Case
// in the product, so the product uses the same Cases list, board, record page, reports
// and workflows as every other product. Changing a case's status or its "Reply in app"
// field updates the ticket, and the person sees it in the app.

const caseTicketField = "app_ticket_id"

// caseFields are the product's custom fields on Cases for app tickets.
var caseFields = []fieldDef{
	{"cases", caseTicketField, "App ticket ID", "text", nil, "The ticket's ID in " + AppName + "."},
	{"cases", "app_category", "App category", "text", nil, "The topic the person picked in the app."},
	{"cases", "app_reply", "Reply in app", "textarea", nil, "What the person sees in the app. Save the case to send it."},
}

// ticket ↔ case status.
func caseStatus(ticket string) string {
	switch ticket {
	case "in_progress":
		return "working"
	case "resolved":
		return "resolved"
	}
	return "new"
}

func ticketStatus(caseStatus string) string {
	switch caseStatus {
	case "working", "escalated":
		return "in_progress"
	case "resolved", "closed":
		return "resolved"
	}
	return "open"
}

// syncTicketCases mirrors tickets changed since the last run into Cases.
func (c *Connector) syncTicketCases(ctx context.Context, wsID uuid.UUID, st *syncState) error {
	var owner uuid.UUID
	if err := c.store.Pool.QueryRow(ctx, `SELECT id FROM crm.identities WHERE is_platform_owner ORDER BY created_at LIMIT 1`).Scan(&owner); err != nil {
		return err
	}
	for {
		rows, err := c.store.Pool.Query(ctx, `
			SELECT t.id, t.subject, t.message, t.category, t.status, COALESCE(t.admin_reply, ''), t.updated_at,
			       l.account_id, l.contact_id,
			       (SELECT r.status FROM crm.object_records r WHERE r.workspace_id = $3 AND r.object_key = 'cases'
			         AND r.custom->>'`+caseTicketField+`' = t.id LIMIT 1)
			FROM public.support_tickets t
			LEFT JOIN crm.external_links l ON l.system = $1 AND l.external_type = 'user' AND l.external_id = t.user_id::text
			WHERE t.updated_at > $2 ORDER BY t.updated_at, t.id LIMIT 200`, System, st.CasesSince, wsID)
		if err != nil {
			return err
		}
		type tk struct {
			id, subject, message, category, status, reply string
			at                                            time.Time
			accountID, contactID                          *uuid.UUID
			current                                       *string
		}
		var list []tk
		for rows.Next() {
			var t tk
			if err := rows.Scan(&t.id, &t.subject, &t.message, &t.category, &t.status, &t.reply, &t.at, &t.accountID, &t.contactID, &t.current); err != nil {
				rows.Close()
				return err
			}
			list = append(list, t)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, t := range list {
			values := map[string]any{
				"name": strings.TrimSpace(t.subject), "description": t.message, "origin": "app",
				"app_category": t.category, "app_reply": t.reply,
			}
			if t.current == nil {
				values["status"] = caseStatus(t.status)
				values["priority"] = "medium"
			} else if ticketStatus(*t.current) != t.status {
				// The app moved the ticket (or the case is new to us): follow it. A case that is
				// e.g. Escalated stays so while the ticket is "in progress".
				values["status"] = caseStatus(t.status)
			}
			if t.contactID != nil && *t.contactID != uuid.Nil {
				values["contactId"] = t.contactID.String()
			}
			if t.accountID != nil && *t.accountID != uuid.Nil {
				values["accountId"] = t.accountID.String()
			}
			if values["name"] == "" {
				values["name"] = "Support request"
			}
			err := c.store.WithTx(ctx, func(tx pgx.Tx) error {
				_, _, err := records.SyncRecord(ctx, tx, wsID, "cases", caseTicketField, t.id, values, owner, "app")
				return err
			})
			if err != nil {
				slog.Warn("cardflow: ticket → case", "ticket", t.id, "error", err)
			}
			st.CasesSince = t.at
		}
		if len(list) < 200 {
			records.KickEvents()
			return nil
		}
	}
}

// onCaseEvent sends a case's status and "Reply in app" back to the app's ticket.
// It never fails the relay: a problem is logged and the next change tries again.
func (c *Connector) onCaseEvent(ctx context.Context, tx pgx.Tx, ev records.Event) error {
	if ev.Object != "cases" || ev.WorkspaceID != c.WorkspaceID() || ev.Source == "app" || !c.hasTickets {
		return nil
	}
	if ev.Type != "record.updated" && ev.Type != "record.created" {
		return nil
	}
	id, _ := ev.Record[caseTicketField].(string)
	if id == "" {
		return nil
	}
	statusChanged, replyChanged := ev.Type == "record.created", false
	for _, k := range ev.Changed {
		switch k {
		case "status":
			statusChanged = true
		case "app_reply":
			replyChanged = true
		}
	}
	if !statusChanged && !replyChanged {
		return nil
	}
	status := ""
	if statusChanged {
		s, _ := ev.Record["status"].(string)
		status = ticketStatus(s)
	}
	reply := ""
	if replyChanged {
		reply, _ = ev.Record["app_reply"].(string)
		reply = strings.TrimSpace(reply)
	}
	by := "CRM"
	if ev.ActorID != nil {
		var name string
		if tx.QueryRow(ctx, `SELECT display_name FROM crm.identities WHERE id = $1`, *ev.ActorID).Scan(&name) == nil && name != "" {
			by = name + " (CRM)"
		}
	}
	_, err := tx.Exec(ctx, `SAVEPOINT cardflow_case`)
	if err != nil {
		return nil
	}
	_, err = tx.Exec(ctx, `
		UPDATE public.support_tickets
		SET status = COALESCE(NULLIF($2, ''), CASE WHEN $3 <> '' AND status = 'open' THEN 'in_progress' ELSE status END),
		    admin_reply = CASE WHEN $4 THEN NULLIF($3, '') ELSE admin_reply END,
		    replied_at = CASE WHEN $3 <> '' THEN now() ELSE replied_at END,
		    replied_by = CASE WHEN $3 <> '' THEN $5 ELSE replied_by END,
		    updated_at = now()
		WHERE id = $1`, id, status, reply, replyChanged, by)
	if err != nil {
		slog.Warn("cardflow: case → ticket", "ticket", id, "error", err)
		_, _ = tx.Exec(ctx, `ROLLBACK TO SAVEPOINT cardflow_case`)
		return nil
	}
	_, _ = tx.Exec(ctx, `RELEASE SAVEPOINT cardflow_case`)
	return nil
}

// standardModules turns on, once, the objects every product has, so this product
// looks like the others (the owner can switch any of them off afterwards).
func upgradeStandardModules(ctx context.Context, tx pgx.Tx, productID uuid.UUID) error {
	const marker = "cardflow:standard-modules-v1"
	var done bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.connector_state WHERE key = $1)`, marker).Scan(&done); err != nil || done {
		return err
	}
	mods := []string{"opportunities", "tasks", "calendar", "notes", "communications", "catalog", "files"}
	if _, err := tx.Exec(ctx, `
		UPDATE crm.product_versions SET config = jsonb_set(config, '{modules}',
		       COALESCE(config->'modules', '[]'::jsonb) || to_jsonb(ARRAY(SELECT m FROM unnest($2::text[]) m WHERE NOT COALESCE(config->'modules', '[]'::jsonb) ? m)))
		WHERE product_id = $1`, productID, mods); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE crm.products SET draft_config = jsonb_set(draft_config, '{modules}',
		       COALESCE(draft_config->'modules', '[]'::jsonb) || to_jsonb(ARRAY(SELECT m FROM unnest($2::text[]) m WHERE NOT COALESCE(draft_config->'modules', '[]'::jsonb) ? m)))
		WHERE id = $1 AND draft_config ? 'modules'`, productID, mods); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `INSERT INTO crm.connector_state (key, value) VALUES ($1, '{"done":true}') ON CONFLICT (key) DO NOTHING`, marker)
	return err
}
