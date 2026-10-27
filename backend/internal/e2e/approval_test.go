package e2e

import (
	"context"
	"testing"
	"time"

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
	if err := testDB.Pool.QueryRow(context.Background(), `SELECT w.id::text FROM crm.workspaces w LEFT JOIN crm.tenants tn ON tn.id = w.tenant_id WHERE w.code = $1 OR tn.slug = $1 ORDER BY (w.code = $1) DESC LIMIT 1`, slug).Scan(&id); err != nil {
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

// slugOf: the URL code of the newest business of that name created by that number (the page
// never shows it before approval).
func slugOf(t *testing.T, phone, name string) string {
	t.Helper()
	var slug string
	if err := testDB.Pool.QueryRow(context.Background(), `
		SELECT COALESCE(tn.slug, w.code) FROM crm.workspaces w
		JOIN crm.verified_identifiers v ON v.identity_id = w.created_by_identity AND v.kind = 'phone' AND v.value_normalized = '+91' || $1
		LEFT JOIN crm.tenants tn ON tn.id = w.tenant_id
		WHERE w.name = $2 ORDER BY w.created_at DESC LIMIT 1`, phone, name).Scan(&slug); err != nil {
		t.Fatalf("business %s of %s: %v", name, phone, err)
	}
	return slug
}

// ownedBy counts the businesses a number created.
func ownedBy(t *testing.T, phone string) int {
	t.Helper()
	var n int
	if err := testDB.Pool.QueryRow(context.Background(), `
		SELECT count(*) FROM crm.workspaces w
		JOIN crm.verified_identifiers v ON v.identity_id = w.created_by_identity AND v.kind = 'phone' AND v.value_normalized = '+91' || $1
		WHERE w.status <> 'deleted'`, phone).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// waitAudit waits for a background notice to be recorded for that number.
func waitAudit(t *testing.T, action, phone string) {
	t.Helper()
	for i := 0; i < 50; i++ {
		var n int
		_ = testDB.Pool.QueryRow(context.Background(), `
			SELECT count(*) FROM crm.audit_events a JOIN crm.verified_identifiers v ON v.identity_id = a.actor_id AND v.kind = 'phone' AND v.value_normalized = '+91' || $2
			WHERE a.action = $1`, action, phone).Scan(&n)
		if n > 0 {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("no %s recorded for %s", action, phone)
}
