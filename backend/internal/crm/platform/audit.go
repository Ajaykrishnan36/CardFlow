package platform

import (
	"context"
	"net/http"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
)

type AuditEntry struct {
	ID            int64      `json:"id"`
	CreatedAt     time.Time  `json:"createdAt"`
	ActorName     string     `json:"actorName,omitempty"`
	ActorKind     string     `json:"actorKind"`
	Action        string     `json:"action"`
	EntityType    string     `json:"entityType,omitempty"`
	EntityID      *uuid.UUID `json:"entityId,omitempty"`
	WorkspaceName string     `json:"workspaceName,omitempty"`
	IP            string     `json:"ip,omitempty"`
}

// listAudit returns entries newest first. With includeActor, entries where the entity
// acted (not only where it was the target) are included too.
func (h *Handler) listAudit(ctx context.Context, limit int, before int64, entityID *uuid.UUID, includeActor bool) ([]AuditEntry, int64, error) {
	rows, err := h.store.Pool.Query(ctx, `
		SELECT a.id, a.created_at, COALESCE(i.display_name, ''), a.actor_kind, a.action, COALESCE(a.entity_type, ''),
		       a.entity_id, COALESCE(w.name, ''), COALESCE(host(a.ip), '')
		FROM crm.audit_events a
		LEFT JOIN crm.identities i ON i.id = a.actor_id
		LEFT JOIN crm.workspaces w ON w.id = a.workspace_id
		WHERE ($1 = 0 OR a.id < $1)
		  AND ($2::uuid IS NULL OR a.entity_id = $2 OR ($3 AND a.actor_id = $2))
		ORDER BY a.id DESC LIMIT $4`, before, entityID, includeActor, limit+1)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []AuditEntry{}
	for rows.Next() {
		var e AuditEntry
		if err := rows.Scan(&e.ID, &e.CreatedAt, &e.ActorName, &e.ActorKind, &e.Action, &e.EntityType, &e.EntityID, &e.WorkspaceName, &e.IP); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	var next int64
	if len(out) > limit {
		out = out[:limit]
		next = out[len(out)-1].ID
	}
	return out, next, rows.Err()
}

func (h *Handler) handleAudit(w http.ResponseWriter, r *http.Request) {
	limit := queryInt(r, "limit", 50, 1, 200)
	before := int64(queryInt(r, "before", 0, 0, 1<<31-1))
	var entityID *uuid.UUID
	if raw := r.URL.Query().Get("entityId"); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil {
			shared.WriteError(w, r, shared.Validation(map[string]string{"entityId": "Not a valid id."}))
			return
		}
		entityID = &id
	}
	list, next, err := h.listAudit(r.Context(), limit, before, entityID, false)
	resp := map[string]any{"data": list}
	if next > 0 {
		resp["nextBefore"] = next
	}
	writeResult(w, r, http.StatusOK, resp, err)
}
