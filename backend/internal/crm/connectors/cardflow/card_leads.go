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

// App businesses → CRM leads and accounts (D-51, D-52).
//
//   - A business created from a scanned card whose owner isn't on the app yet
//     is a Lead (source card_scan): GSTIN, business name and the phone on the card.
//   - A business with an owner — registered by them in the app, or claimed
//     when the person whose phone is on the card signed up — is an Account
//     (kind business). The owner's Contact (created at sign-up) is linked to it,
//     and a card lead is converted into that Account + Contact.
//   - People without a business stay contacts only; nothing is deleted when a
//     business is removed in the app.

type bizRow struct {
	ID, Name, GSTIN, ContactName, Designation, Phone, Email, Website string
	Address, City, State, Pincode, OwnerID, Source                   string
	CreatedAt, UpdatedAt                                             time.Time
	ClaimedAt                                                        *time.Time
	Deleted                                                          bool
}

func (b bizRow) nameParts() (first, last string) {
	n := strings.TrimSpace(b.ContactName)
	if n == "" {
		return "", strings.TrimSpace(b.Name)
	}
	if i := strings.LastIndex(n, " "); i > 0 {
		return n[:i], n[i+1:]
	}
	return "", n
}

const bizSelect = `
	SELECT b.id::text, b.name, COALESCE(trim(b.gstin), ''), COALESCE(b.contact_name, ''), COALESCE(b.contact_designation, ''),
	       COALESCE(b.contact_phone, ''), COALESCE(b.email, ''), COALESCE(b.website, ''), COALESCE(b.address_line1, ''),
	       COALESCE(b.city, ''), COALESCE(b.state, ''), COALESCE(b.pincode, ''), COALESCE(b.owner_user_id::text, ''),
	       COALESCE(b.source, 'owner'), b.created_at, b.updated_at, b.claimed_at, b.deleted_at IS NOT NULL
	FROM public.businesses b`

func scanBiz(rows pgx.Rows) (bizRow, error) {
	var b bizRow
	err := rows.Scan(&b.ID, &b.Name, &b.GSTIN, &b.ContactName, &b.Designation, &b.Phone, &b.Email, &b.Website,
		&b.Address, &b.City, &b.State, &b.Pincode, &b.OwnerID, &b.Source, &b.CreatedAt, &b.UpdatedAt, &b.ClaimedAt, &b.Deleted)
	return b, err
}

// syncBusinesses handles businesses changed since the last run, then retries
// owned businesses still waiting for their owner's contact (the user sync
// creates it; it can land a moment later).
func (c *Connector) syncBusinesses(ctx context.Context, wsID uuid.UUID, st *syncState) error {
	if !c.hasBiz || !c.hasCardSource(ctx) {
		return nil
	}
	since := st.BusinessesSince.Add(-2 * time.Second)
	for {
		batch, err := c.queryBiz(ctx, bizSelect+` WHERE b.updated_at > $1 ORDER BY b.updated_at LIMIT 200`, since)
		if err != nil {
			return err
		}
		for _, b := range batch {
			if err := c.upsertBusiness(ctx, wsID, b); err != nil {
				return fmt.Errorf("business %s: %w", b.ID, err)
			}
			if b.UpdatedAt.After(st.BusinessesSince) {
				st.BusinessesSince = b.UpdatedAt
			}
		}
		if len(batch) < 200 {
			break
		}
		since = st.BusinessesSince
	}
	pending, err := c.queryBiz(ctx, bizSelect+`
		JOIN crm.external_links el ON el.system = 'cardflow' AND el.external_type = 'business' AND el.external_id = b.id::text
		LEFT JOIN crm.leads l ON l.id = el.lead_id
		WHERE b.owner_user_id IS NOT NULL AND b.deleted_at IS NULL
		  AND (el.contact_id IS NULL OR el.account_id IS NULL OR (el.lead_id IS NOT NULL AND l.converted_at IS NULL))
		  AND EXISTS (SELECT 1 FROM crm.external_links u WHERE u.system = 'cardflow' AND u.external_type = 'user'
		              AND u.external_id = b.owner_user_id::text AND u.contact_id IS NOT NULL)
		LIMIT 200`)
	if err != nil {
		return err
	}
	for _, b := range pending {
		if err := c.upsertBusiness(ctx, wsID, b); err != nil {
			return fmt.Errorf("business %s: %w", b.ID, err)
		}
	}
	return nil
}

func (c *Connector) queryBiz(ctx context.Context, sql string, args ...any) ([]bizRow, error) {
	rows, err := c.store.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []bizRow
	for rows.Next() {
		b, err := scanBiz(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (c *Connector) hasCardSource(ctx context.Context) bool {
	var ok bool
	_ = c.store.Pool.QueryRow(ctx, `
		SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'businesses' AND column_name = 'source')`).Scan(&ok)
	return ok
}

func (c *Connector) upsertBusiness(ctx context.Context, wsID uuid.UUID, b bizRow) error {
	return c.store.WithTx(ctx, func(tx pgx.Tx) error {
		var owner uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT id FROM crm.identities WHERE is_platform_owner ORDER BY created_at LIMIT 1`).Scan(&owner); err != nil {
			return err
		}
		var leadID, accountID, contactID *uuid.UUID
		err := tx.QueryRow(ctx, `
			SELECT lead_id, account_id, contact_id FROM crm.external_links
			WHERE system = $1 AND external_type = 'business' AND external_id = $2 FOR UPDATE`, System, b.ID).Scan(&leadID, &accountID, &contactID)
		linked := err == nil
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// Before D-52 a claimed card business pointed at the owner's personal
		// ("individual") account; the business gets its own account instead.
		if accountID != nil {
			var kind string
			if err := tx.QueryRow(ctx, `SELECT kind FROM crm.accounts WHERE id = $1`, *accountID).Scan(&kind); err != nil || kind != "business" {
				accountID = nil
			}
		}
		if !linked && b.Deleted {
			return nil
		}
		if !linked {
			if _, err := tx.Exec(ctx, `
				INSERT INTO crm.external_links (workspace_id, system, external_type, external_id) VALUES ($1, $2, 'business', $3)`,
				wsID, System, b.ID); err != nil {
				return err
			}
		}

		// No owner yet: a card business is a lead (owner-registered ones always have an owner).
		if b.OwnerID == "" {
			if b.Source != "card" || b.Deleted {
				return nil
			}
			if leadID == nil {
				id, err := createCardLead(ctx, tx, wsID, owner, b)
				if err != nil {
					return err
				}
				leadID = &id
				_, err = tx.Exec(ctx, `UPDATE crm.external_links SET lead_id = $3, synced_at = now() WHERE system = $1 AND external_type = 'business' AND external_id = $2`,
					System, b.ID, id)
				return err
			}
			return refreshCardLead(ctx, tx, *leadID, b)
		}

		// Owned: the business is an account; the owner's contact belongs to it.
		if accountID == nil {
			id, err := createBusinessAccount(ctx, tx, wsID, owner, b)
			if err != nil {
				return err
			}
			accountID = &id
		} else if !b.Deleted {
			if _, err := tx.Exec(ctx, `
				UPDATE crm.accounts SET name = $2, phone = COALESCE(NULLIF($3, ''), phone), email = COALESCE(NULLIF($4, ''), email),
				       website = COALESCE(NULLIF($5, ''), website), billing_street = COALESCE(NULLIF($6, ''), billing_street),
				       billing_city = COALESCE(NULLIF($7, ''), billing_city), billing_state = COALESCE(NULLIF($8, ''), billing_state),
				       billing_postal_code = COALESCE(NULLIF($9, ''), billing_postal_code), updated_at = now()
				WHERE id = $1 AND (name, COALESCE(phone, ''), COALESCE(email, ''), COALESCE(website, ''), COALESCE(billing_city, ''))
				      IS DISTINCT FROM ($2, COALESCE(NULLIF($3, ''), phone, ''), COALESCE(NULLIF($4, ''), email, ''),
				                        COALESCE(NULLIF($5, ''), website, ''), COALESCE(NULLIF($7, ''), billing_city, ''))`,
				*accountID, b.Name, b.Phone, strings.ToLower(b.Email), b.Website, b.Address, b.City, b.State, b.Pincode); err != nil {
				return err
			}
		}
		// The owner's contact comes from the user sync; wait for it if it isn't there yet.
		var userContact *uuid.UUID
		_ = tx.QueryRow(ctx, `SELECT contact_id FROM crm.external_links WHERE system = $1 AND external_type = 'user' AND external_id = $2`,
			System, b.OwnerID).Scan(&userContact)
		if userContact != nil {
			contactID = userContact
			// First business becomes the contact's account (a person keeps one primary account).
			// (A contact still under a personal account from before D-52 moves to the business.)
			if _, err := tx.Exec(ctx, `
				UPDATE crm.contacts c SET account_id = $2, updated_at = now()
				WHERE c.id = $1 AND (c.account_id IS NULL OR EXISTS (
					SELECT 1 FROM crm.accounts a WHERE a.id = c.account_id AND a.kind = 'individual'))`,
				*contactID, *accountID); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `
				UPDATE crm.external_links el SET account_id = $3
				WHERE el.system = $1 AND el.external_type = 'user' AND el.external_id = $2 AND (el.account_id IS NULL OR EXISTS (
					SELECT 1 FROM crm.accounts a WHERE a.id = el.account_id AND a.kind = 'individual'))`,
				System, b.OwnerID, *accountID); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE crm.external_links SET account_id = $3, contact_id = COALESCE($4, contact_id), synced_at = now()
			WHERE system = $1 AND external_type = 'business' AND external_id = $2`, System, b.ID, *accountID, contactID); err != nil {
			return err
		}
		// A card lead converts into the business account + the owner's contact.
		if leadID != nil && contactID != nil {
			if err := convertCardLead(ctx, tx, wsID, owner, *leadID, *accountID, *contactID, b); err != nil {
				return err
			}
		}
		return nil
	})
}

func createCardLead(ctx context.Context, tx pgx.Tx, wsID, owner uuid.UUID, b bizRow) (uuid.UUID, error) {
	code, err := records.NextCode(ctx, tx, wsID, "L")
	if err != nil {
		return uuid.Nil, err
	}
	first, last := b.nameParts()
	custom, _ := json.Marshal(map[string]any{"app_business_id": b.ID, "gstin": b.GSTIN})
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO crm.leads (workspace_id, code, first_name, last_name, title, organization, email, mobile, phone, website,
		                       street, city, state, postal_code, country, source, status, description, user_type,
		                       owner_id, created_by, updated_by, custom, created_at)
		VALUES ($1, $2, NULLIF($3, ''), $4, NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), NULLIF($8, ''), NULLIF($9, ''),
		        NULLIF($10, ''), NULLIF($11, ''), NULLIF($12, ''), NULLIF($13, ''), 'India', 'card_scan', 'new', $14, 'business',
		        $15, $15, $15, $16, $17)
		RETURNING id`, wsID, code, first, last, b.Designation, b.Name, strings.ToLower(b.Email), b.Phone, b.Website,
		b.Address, b.City, b.State, b.Pincode,
		"Business card scanned in "+AppName+" (GSTIN "+b.GSTIN+"). The owner isn't on the app yet — the lead converts into an account and contact when they sign up with "+
			orDash(b.Phone)+".",
		owner, custom, b.CreatedAt).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	if err := leadActivity(ctx, tx, wsID, id, "app.card_scanned", "Business card scanned in "+AppName, b.CreatedAt,
		"cardflow:cardbiz:"+b.ID, map[string]any{"business": b.Name, "gstin": b.GSTIN}); err != nil {
		return uuid.Nil, err
	}
	if err := records.EmitRecordEvent(ctx, tx, wsID, "leads", id, "record.created", "app"); err != nil {
		return uuid.Nil, err
	}
	return id, shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &wsID, ActorKind: "system", Action: "connector.card_lead_created",
		EntityType: "lead", EntityID: &id, After: map[string]any{"app": AppName, "businessId": b.ID, "gstin": b.GSTIN}})
}

// refreshCardLead keeps an open card lead in step with its business (later scans fill blanks).
func refreshCardLead(ctx context.Context, tx pgx.Tx, leadID uuid.UUID, b bizRow) error {
	_, err := tx.Exec(ctx, `
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
}

func createBusinessAccount(ctx context.Context, tx pgx.Tx, wsID, owner uuid.UUID, b bizRow) (uuid.UUID, error) {
	code, err := records.NextCode(ctx, tx, wsID, "A")
	if err != nil {
		return uuid.Nil, err
	}
	custom, _ := json.Marshal(map[string]any{"app_business_id": b.ID, "app_gstin": b.GSTIN})
	desc := "Business in " + AppName + "."
	if b.Source == "card" {
		desc = "Business in " + AppName + ", first added from a scanned card and claimed by its owner."
	}
	at := b.CreatedAt
	if b.ClaimedAt != nil {
		at = *b.ClaimedAt
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO crm.accounts (workspace_id, code, kind, name, type, lifecycle, email, phone, website,
		                          billing_street, billing_city, billing_state, billing_postal_code, billing_country,
		                          description, owner_id, created_by, updated_by, custom, created_at)
		VALUES ($1, $2, 'business', $3, 'customer', 'active', NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''),
		        NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''), 'India', $11, $12, $12, $12, $13, $14)
		RETURNING id`, wsID, code, b.Name, strings.ToLower(b.Email), b.Phone, b.Website, b.Address, b.City, b.State, b.Pincode,
		desc, owner, custom, at).Scan(&id); err != nil {
		return uuid.Nil, err
	}
	if err := records.EmitRecordEvent(ctx, tx, wsID, "accounts", id, "record.created", "app"); err != nil {
		return uuid.Nil, err
	}
	return id, shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &wsID, ActorKind: "system", Action: "connector.business_account_created",
		EntityType: "account", EntityID: &id, After: map[string]any{"app": AppName, "businessId": b.ID, "gstin": b.GSTIN}})
}

func convertCardLead(ctx context.Context, tx pgx.Tx, wsID, owner, leadID, accountID, contactID uuid.UUID, b bizRow) error {
	at := time.Now()
	if b.ClaimedAt != nil {
		at = *b.ClaimedAt
	}
	tag, err := tx.Exec(ctx, `
		UPDATE crm.leads SET status = 'converted', converted_at = $2, converted_account_id = $3, converted_contact_id = $4, updated_at = now()
		WHERE id = $1 AND converted_at IS NULL`, leadID, at, accountID, contactID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	if err := records.EmitRecordEvent(ctx, tx, wsID, "leads", leadID, "record.updated", "app"); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO crm.lead_conversions (workspace_id, lead_id, account_id, contact_id, trigger, converted_by)
		VALUES ($1, $2, $3, $4, 'app_claim', $5)`, wsID, leadID, accountID, contactID, owner); err != nil {
		return err
	}
	if err := leadActivity(ctx, tx, wsID, leadID, "app.business_claimed", "Owner signed up and claimed "+b.Name, at,
		"cardflow:claim:"+b.ID, map[string]any{"businessId": b.ID}); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO crm.activities (workspace_id, account_id, contact_id, kind, title, detail, source, dedupe_key, occurred_at)
		VALUES ($1, $2, $3, 'app.business_claimed', $4, '{}'::jsonb, $5, $6, $7)
		ON CONFLICT (dedupe_key) DO NOTHING`, wsID, accountID, contactID, "Claimed "+b.Name+" in "+AppName,
		System, "cardflow:claim-account:"+b.ID, at); err != nil {
		return err
	}
	return shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &wsID, ActorKind: "system", Action: "connector.card_lead_converted",
		EntityType: "lead", EntityID: &leadID, After: map[string]any{"accountId": accountID, "contactId": contactID, "businessId": b.ID}})
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
