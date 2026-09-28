package shared

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

// Execer is satisfied by *pgxpool.Pool, *pgxpool.Conn and pgx.Tx, so audits can be
// written inside the same transaction as the change they describe (PRD GO-02).
type Execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

type AuditEvent struct {
	WorkspaceID *uuid.UUID
	ActorID     *uuid.UUID
	ActorKind   string // identity | system | api_key
	Action      string
	EntityType  string
	EntityID    *uuid.UUID
	Before      any
	After       any
	Reason      string
	IP          string
	RequestID   string
}

func WriteAudit(ctx context.Context, q Execer, e AuditEvent) error {
	if e.ActorKind == "" {
		e.ActorKind = "identity"
	}
	before, err := jsonOrNil(e.Before)
	if err != nil {
		return err
	}
	after, err := jsonOrNil(e.After)
	if err != nil {
		return err
	}
	_, err = q.Exec(ctx, `
		INSERT INTO crm.audit_events
			(workspace_id, actor_id, actor_kind, action, entity_type, entity_id, before, after, reason, ip, request_id)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6, $7, $8, NULLIF($9, ''), NULLIF($10, '')::inet, NULLIF($11, ''))`,
		e.WorkspaceID, e.ActorID, e.ActorKind, e.Action, e.EntityType, e.EntityID, before, after, e.Reason, e.IP, e.RequestID)
	return err
}

func jsonOrNil(v any) ([]byte, error) {
	if v == nil {
		return nil, nil
	}
	return json.Marshal(v)
}
