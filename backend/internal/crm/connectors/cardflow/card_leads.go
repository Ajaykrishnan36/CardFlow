package cardflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"cardflow-backend/internal/crm/records"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Card businesses → CRM leads.
//
// A business created from a scanned card (source 'card') whose owner isn't on
// the app yet becomes a Lead in the workspace. When the person whose phone is
// on the card signs up (the app claims the business for them), the lead is
// converted into that app user's account + contact. Owner-registered
// businesses don't create leads — their owner is already an account.

type cardBusinessRow struct {
	ID, Name, GSTIN, ContactName, Designation, Phone, Email, Website string
	Address, City, State, Pincode, OwnerID                           string
	CreatedAt, UpdatedAt                                             time.Time
	Deleted                                                          bool
}

func (b cardBusinessRow) nameParts() (first, last string) {
	n := strings.TrimSpace(b.ContactName)
	if n == "" {
		return "", strings.TrimSpace(b.Name)
	}
	if i := strings.LastIndex(n, " "); i > 0 {
		return n[:i], n[i+1:]
	}
	return "", n
}

// syncCardBusinesses creates/updates leads for card businesses changed since
// the last run, then converts leads whose business has been claimed.
func (c *Connector) syncCardBusinesses(ctx context.Context, wsID uuid.UUID, st *syncState) error {
	if !c.hasBiz || !c.hasCardSource(ctx) {
		return nil
	}
	since := st.CardBusinessesSince.Add(-2 * time.Second)
	for {
		rows, err := c.store.Pool.Query(ctx, `
			SELECT b.id::text, b.name, COALESCE(trim(b.gstin), ''), COALESCE(b.contact_name, ''), COALESCE(b.contact_designation, ''),
			       COALESCE(b.contact_phone, ''), COALESCE(b.email, ''), COALESCE(b.website, ''), COALESCE(b.address_line1, ''),
			       COALESCE(b.city, ''), COALESCE(b.state, ''), COALESCE(b.pincode, ''), COALESCE(b.owner_user_id::text, ''),
			       b.created_at, b.updated_at, b.deleted_at IS NOT NULL
			FROM public.businesses b
			WHERE COALESCE(b.source, 'owner') = 'card' AND b.updated_at > $1
			ORDER BY b.updated_at LIMIT 200`, since)
		if err != nil {
			return err
		}
		var batch []cardBusinessRow
		for rows.Next() {
			var b cardBusinessRow
			if err := rows.Scan(&b.ID, &b.Name, &b.GSTIN, &b.ContactName, &b.Designation, &b.Phone, &b.Email, &b.Website,
				&b.Address, &b.City, &b.State, &b.Pincode, &b.OwnerID, &b.CreatedAt, &b.UpdatedAt, &b.Deleted); err != nil {
				rows.Close()
				return err
			}
			batch = append(batch, b)
		}
		rows.Close()
		for _, b := range batch {
			if err := c.upsertCardLead(ctx, wsID, b); err != nil {
				return fmt.Errorf("card business %s: %w", b.ID, err)
			}
			if b.UpdatedAt.After(st.CardBusinessesSince) {
				st.CardBusinessesSince = b.UpdatedAt
			}
		}
		if len(batch) < 200 {
			break
		}
		since = st.CardBusinessesSince
	}
	return c.convertClaimedCardLeads(ctx, wsID)
}

func (c *Connector) hasCardSource(ctx context.Context) bool {
	var ok bool
	_ = c.store.Pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'businesses' AND column_name = 'source')`).Scan(&ok)
	return ok
}

func (c *Connector) upsertCardLead(ctx context.Context, wsID uuid.UUID, b cardBusinessRow) error {
	return c.store.WithTx(ctx, func(tx pgx.Tx) error {
		var owner uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM crm.identities WHERE is_platform_owner ORDER BY created_at LIMIT 1`).Scan(&owner); err != nil {
			return err
		}
		var leadID uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT lead_id FROM crm.external_links
			WHERE system = $1 AND external_type = 'business' AND external_id = $2 FOR UPDATE`, System, b.ID).Scan(&leadID)
		first, last := b.nameParts()
		custom, _ := json.Marshal(map[string]any{"app_business_id": b.ID, "gstin": b.GSTIN})
		if errors.Is(err, pgx.ErrNoRows) {
			if b.Deleted {
				return nil
			}
			code, err := records.NextCode(ctx, tx, wsID, "L")
			if err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `
				INSERT INTO crm.leads (workspace_id, code, first_name, last_name, title, organization, email, mobile, phone, website,
				                       street, city, state, postal_code, country, source, status, description, user_type,
				                       owner_id, created_by, updated_by, custom, created_at)
				VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), NULLIF($8, ''), NULLIF($9, ''),
				        NULLIF($10, ''), NULLIF($11, ''), NULLIF($12, ''), NULLIF($13, ''), 'India', 'card_scan', 'new', $14, 'business',
				        $15, $15, $15, $16, $17)
				RETURNING id`, wsID, code, first, last, b.Designation, b.Name, strings.ToLower(b.Email), b.Phone, b.Website,
				b.Address, b.City, b.State, b.Pincode,
				"Business card scanned in "+AppName+" (GSTIN "+b.GSTIN+"). The owner isn't on the app yet — the lead converts when they sign up with "+
					orDash(b.Phone)+".",
				owner, custom, b.CreatedAt).Scan(&leadID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO crm.external_links (workspace_id, system, external_type, external_id, lead_id)
				VALUES ($1, $2, 'business', $3, $4)`, wsID, System, b.ID, leadID); err != nil {
				return err
			}
			if err := leadActivity(ctx, tx, wsID, leadID, "app.card_scanned", "Business card scanned in "+AppName, b.CreatedAt,
				"cardflow:cardbiz:"+b.ID, map[string]any{"business": b.Name, "gstin": b.GSTIN}); err != nil {
				return err
			}
			return shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &wsID, ActorKind: "system", Action: "connector.card_lead_created",
				EntityType: "lead", EntityID: &leadID, After: map[string]any{"app": AppName, "businessId": b.ID, "gstin": b.GSTIN}})
		}
		if err != nil {
			return err
		}
		// Keep an open lead in step with the business (later scans fill blanks).
		_, err = tx.Exec(ctx, `
			UPDATE crm.leads SET
				organization = COALESCE(NULLIF($2, ''), organization),
				title = COALESCE(title, NULLIF($3, '')), email = COALESCE(email, NULLIF($4, '')),
				mobile = COALESCE(mobile, NULLIF($5, '')), phone = COALESCE(phone, NULLIF($5, '')), website = COALESCE(website, NULLIF($6, '')),
				street = COALESCE(street, NULLIF($7, '')), city = COALESCE(city, NULLIF($8, '')), state = COALESCE(state, NULLIF($9, '')),
				postal_code = COALESCE(postal_code, NULLIF($10, '')), updated_at = now()
			WHERE id = $1 AND converted_at IS NULL AND deleted_at IS NULL AND
			      (COALESCE(organization, ''), COALESCE(title, ''), COALESCE(email, ''), COALESCE(mobile, ''), COALESCE(website, ''),
			       COALESCE(street, ''), COALESCE(city, ''), COALESCE(state, ''), COALESCE(postal_code, ''))
			      IS DISTINCT FROM
			      (COALESCE(NULLIF($2, ''), organization, ''), COALESCE(title, $3), COALESCE(email, $4), COALESCE(mobile, $5), COALESCE(website, $6),
			       COALESCE(street, $7), COALESCE(city, $8), COALESCE(state, $9), COALESCE(postal_code, $10))`,
			leadID, b.Name, b.Designation, strings.ToLower(b.Email), b.Phone, b.Website, b.Address, b.City, b.State, b.Pincode)
		return err
	})
}

// convertClaimedCardLeads converts open card leads whose business now has an
// owner, into that owner's account + contact (created by the user sync).
func (c *Connector) convertClaimedCardLeads(ctx context.Context, wsID uuid.UUID) error {
	rows, err := c.store.Pool.Query(ctx, `
		SELECT el.lead_id, b.id::text, b.name, u.account_id, u.contact_id, b.claimed_at
		FROM crm.external_links el
		JOIN crm.leads l ON l.id = el.lead_id AND l.converted_at IS NULL AND l.deleted_at IS NULL
		JOIN public.businesses b ON b.id::text = el.external_id AND b.owner_user_id IS NOT NULL
		JOIN crm.external_links u ON u.system = el.system AND u.external_type = 'user' AND u.external_id = b.owner_user_id::text
		WHERE el.system = $1 AND el.external_type = 'business' AND el.workspace_id = $2 AND u.account_id IS NOT NULL
		LIMIT 200`, System, wsID)
	if err != nil {
		return err
	}
	type conv struct {
		leadID, accountID, contactID uuid.UUID
		bizID, bizName               string
		claimedAt                    *time.Time
	}
	var list []conv
	for rows.Next() {
		var v conv
		var contact *uuid.UUID
		if err := rows.Scan(&v.leadID, &v.bizID, &v.bizName, &v.accountID, &contact, &v.claimedAt); err != nil {
			rows.Close()
			return err
		}
		if contact != nil {
			v.contactID = *contact
		}
		list = append(list, v)
	}
	rows.Close()
	for _, v := range list {
		err := c.store.WithTx(ctx, func(tx pgx.Tx) error {
			var owner uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT id FROM crm.identities WHERE is_platform_owner ORDER BY created_at LIMIT 1`).Scan(&owner); err != nil {
				return err
			}
			at := time.Now()
			if v.claimedAt != nil {
				at = *v.claimedAt
			}
			tag, err := tx.Exec(ctx, `
				UPDATE crm.leads SET status = 'converted', converted_at = $2, converted_account_id = $3,
				       converted_contact_id = NULLIF($4, '00000000-0000-0000-0000-000000000000'::uuid), updated_at = now()
				WHERE id = $1 AND converted_at IS NULL`, v.leadID, at, v.accountID, v.contactID)
			if err != nil || tag.RowsAffected() == 0 {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO crm.lead_conversions (workspace_id, lead_id, account_id, contact_id, trigger, converted_by)
				VALUES ($1, $2, $3, NULLIF($4, '00000000-0000-0000-0000-000000000000'::uuid), 'app_claim', $5)`,
				wsID, v.leadID, v.accountID, v.contactID, owner); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE crm.external_links SET account_id = $3, contact_id = NULLIF($4, '00000000-0000-0000-0000-000000000000'::uuid), synced_at = now()
				WHERE system = $1 AND external_type = 'business' AND external_id = $2`, System, v.bizID, v.accountID, v.contactID); err != nil {
				return err
			}
			if err := leadActivity(ctx, tx, wsID, v.leadID, "app.business_claimed", "Owner signed up and claimed "+v.bizName, at,
				"cardflow:claim:"+v.bizID, map[string]any{"businessId": v.bizID}); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO crm.activities (workspace_id, account_id, contact_id, kind, title, detail, source, dedupe_key, occurred_at)
				VALUES ($1, $2, NULLIF($3, '00000000-0000-0000-0000-000000000000'::uuid), 'app.business_claimed', $4, '{}'::jsonb, $5, $6, $7)
				ON CONFLICT (dedupe_key) DO NOTHING`, wsID, v.accountID, v.contactID, "Claimed business "+v.bizName+" in "+AppName,
				System, "cardflow:claim-account:"+v.bizID, at); err != nil {
				return err
			}
			return shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &wsID, ActorKind: "system", Action: "connector.card_lead_converted",
				EntityType: "lead", EntityID: &v.leadID, After: map[string]any{"accountId": v.accountID, "businessId": v.bizID}})
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func leadActivity(ctx context.Context, tx pgx.Tx, wsID, leadID uuid.UUID, kind, title string, at time.Time, dedupe string, detail map[string]any) error {
	raw, _ := json.Marshal(detail)
	_, err := tx.Exec(ctx, `
		INSERT INTO crm.activities (workspace_id, lead_id, kind, title, detail, source, dedupe_key, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (dedupe_key) DO NOTHING`, wsID, leadID, kind, title, raw, System, dedupe, at)
	return err
}

func orDash(v string) string {
	if v == "" {
		return "the phone on the card"
	}
	return v
}
