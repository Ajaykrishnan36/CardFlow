package shared

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Idempotent runs fn at most once per (actor, Idempotency-Key) (PRD §14 conventions,
// OWN-02). A replay with the same body returns the stored response; the same key with
// a different body is 409 idempotency_mismatch. Without a key, fn simply runs.
func Idempotent(ctx context.Context, pool *pgxpool.Pool, actorID uuid.UUID, key string, body any, fn func() (int, any, error)) (int, any, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return fn()
	}
	if len(key) > 200 {
		return 0, nil, BadRequest("Idempotency-Key is too long.")
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return 0, nil, err
	}
	hash := HashToken(string(raw))

	tag, err := pool.Exec(ctx, `
		INSERT INTO crm.idempotency_keys (actor_id, key, request_hash) VALUES ($1, $2, $3)
		ON CONFLICT (actor_id, key) DO NOTHING`, actorID, key, hash)
	if err != nil {
		return 0, nil, err
	}
	if tag.RowsAffected() == 0 {
		var storedHash []byte
		var resp []byte
		var status *int
		err := pool.QueryRow(ctx, `SELECT request_hash, response, status_code FROM crm.idempotency_keys WHERE actor_id = $1 AND key = $2`,
			actorID, key).Scan(&storedHash, &resp, &status)
		if errors.Is(err, pgx.ErrNoRows) {
			return fn()
		}
		if err != nil {
			return 0, nil, err
		}
		if !bytes.Equal(storedHash, hash) {
			return 0, nil, NewError(http.StatusConflict, "idempotency_mismatch", "This request was already sent with different details. Refresh and try again.")
		}
		if status == nil {
			return 0, nil, NewError(http.StatusConflict, "request_in_progress", "This request is still being processed. Please wait a moment.")
		}
		return *status, json.RawMessage(resp), nil
	}

	status, resp, err := fn()
	if err != nil {
		_, _ = pool.Exec(context.WithoutCancel(ctx), `DELETE FROM crm.idempotency_keys WHERE actor_id = $1 AND key = $2`, actorID, key)
		return 0, nil, err
	}
	stored, err := json.Marshal(resp)
	if err == nil {
		_, _ = pool.Exec(ctx, `UPDATE crm.idempotency_keys SET response = $3, status_code = $4 WHERE actor_id = $1 AND key = $2`,
			actorID, key, stored, status)
	}
	return status, resp, nil
}
