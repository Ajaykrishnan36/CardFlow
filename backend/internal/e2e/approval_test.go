package e2e

import (
	"context"
	"testing"

	"cardflow-backend/internal/crm/shared"
)

// setReview switches the owner's review of new businesses (D-161) on or off.
func setReview(t *testing.T, on bool) {
	t.Helper()
	v := `{"skipReview": true}`
	if on {
		v = `{"skipReview": false}`
	}
	if _, err := testDB.Pool.Exec(context.Background(), `INSERT INTO crm.platform_settings (key, value) VALUES ('self_serve', '{"enabled": true, "maxBusinesses": 5}'::jsonb || $1::jsonb)
		ON CONFLICT (key) DO UPDATE SET value = crm.platform_settings.value || $1::jsonb`, v); err != nil {
		t.Fatal(err)
	}
}

// ownerToken is a platform-owner session made in the database, so it works on any database
// (a dev one has no demo password to sign in with).
func ownerToken(t *testing.T) string {
	t.Helper()
	raw, err := shared.RandomToken(32)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Pool.Exec(context.Background(), `
		INSERT INTO crm.sessions (identity_id, token_hash, audience, privileged, mfa_required, mfa_passed, recent_auth_at,
		                          idle_expires_at, absolute_expires_at, auth_method, transport)
		SELECT id, $1, 'owner', true, false, true, now(), now() + interval '2 hours', now() + interval '2 hours', 'password', 'bearer'
		FROM crm.identities WHERE is_platform_owner AND status = 'active' ORDER BY created_at LIMIT 1`, shared.HashToken(raw)); err != nil {
		t.Fatal(err)
	}
	return "crms_" + raw
}

// workspaceID of a business by its code.
func workspaceID(t *testing.T, slug string) string {
	t.Helper()
	var id string
	if err := testDB.Pool.QueryRow(context.Background(), `SELECT id::text FROM crm.workspaces WHERE code = $1`, slug).Scan(&id); err != nil {
		t.Fatalf("business %s: %v", slug, err)
	}
	return id
}

// approve: the owner approves a business that waits; its URL is sent on every channel.
func approve(t *testing.T, owner, slug string) resp {
	t.Helper()
	r := call(t, "POST", crmAPI+"/platform/applications/"+workspaceID(t, slug)+"/approve", owner, map[string]any{})
	want(t, r, 200, "owner approves "+slug)
	if r.str("application", "status") != "active" || r.str("notified", "email") == "" {
		t.Fatalf("an approved business is active and its URL is sent: %s", truncate(r.Raw, 400))
	}
	return r
}
