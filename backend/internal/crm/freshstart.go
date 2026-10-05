package crm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"cardflow-backend/internal/crm/connectors/cardflow"
	"cardflow-backend/internal/crm/platform"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Fresh start (D-106): erase every customer's data and begin again from a known baseline.
//
// It runs only when the host sets CRM_FRESH_START to a value that starts with
// "erase-everything-" (for example erase-everything-2026-10-05). Each value runs once: the
// run is recorded, so leaving the setting in place — or a restart — never erases again.
// Nobody can trigger it through the API.
//
// Kept: the platform owner's login, the platform workspace and the app-connector
// workspace with their setup (roles, permission sets, objects, layouts), setups, plans,
// platform settings, categories. Erased: every other person, business, record, card,
// listing, ticket, session, file, message and audit entry.
//
// Afterwards the baseline is created: +91 98765 43211 "Ajay krishnan s" as Super Admin of
// two businesses, "Ajay tech" and "Ajay finace", each with its own sample records.

const freshStartPrefix = "erase-everything-"

// keptConfig are tables whose rows for the kept workspaces are setup, not customer data.
var keptConfig = map[string]bool{
	"workspace_products": true, "roles": true, "permission_sets": true, "role_assignments": true, "memberships": true,
	"membership_permission_sets": true, "object_definitions": true, "field_definitions": true, "layouts": true, "views": true,
	"reports": true, "dashboards": true, "workflows": true, "mail_accounts": true, "sso_providers": true, "webhooks": true,
	"api_keys": true, "invite_links": true, "assignment_state": true,
}

// keptWhole are tables that hold no customer data at all.
var keptWhole = map[string]bool{
	"schema_migrations": true, "platform_settings": true, "products": true, "product_versions": true, "plans": true,
	"connector_state": true, "workspaces": true, "identities": true, "verified_identifiers": true, "password_credentials": true,
	"mfa_methods": true, "workflow_versions": true,
}

// FreshStart erases and re-seeds when the host asked for it. Call after the server is wired.
func (m *Module) FreshStart(ctx context.Context) {
	run := strings.TrimSpace(os.Getenv("CRM_FRESH_START"))
	if run == "" || m.store == nil {
		return
	}
	if !strings.HasPrefix(run, freshStartPrefix) || len(run) < len(freshStartPrefix)+4 {
		slog.Error("crm: CRM_FRESH_START ignored — it must look like " + freshStartPrefix + "<date or label>")
		return
	}
	marker := "fresh-start:" + run
	var done bool
	if err := m.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.connector_state WHERE key = $1)`, marker).Scan(&done); err != nil {
		slog.Error("crm: fresh start not run", "error", err)
		return
	}
	if done {
		slog.Info("crm: fresh start already done for this value; nothing erased", "run", run)
		return
	}
	slog.Warn("crm: FRESH START — erasing all customer data", "run", run)
	counts, err := m.eraseEverything(ctx, marker)
	if err != nil {
		slog.Error("crm: fresh start failed — nothing was erased", "error", err)
		return
	}
	slog.Warn("crm: fresh start erased customer data", "tables", len(counts), "rows", sum(counts))
	if err := m.seedBaseline(ctx); err != nil {
		slog.Error("crm: fresh start — baseline data not created", "error", err)
		return
	}
	slog.Warn("crm: fresh start complete — baseline created")
}

func sum(m map[string]int64) (n int64) {
	for _, v := range m {
		n += v
	}
	return n
}

type eraseStep struct {
	name string
	sql  string
	args []any
}

// eraseEverything deletes in one transaction: either all of it happens or none.
func (m *Module) eraseEverything(ctx context.Context, marker string) (map[string]int64, error) {
	counts := map[string]int64{}
	err := m.store.WithTx(ctx, func(tx pgx.Tx) error {
		// Who and what stays.
		var owners []uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(id), '{}') FROM crm.identities WHERE is_platform_owner`).Scan(&owners); err != nil {
			return err
		}
		if len(owners) == 0 {
			return errors.New("there is no platform owner to keep")
		}
		var keep []uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(id), '{}') FROM crm.workspaces WHERE is_platform OR code = $1`, cardflow.WorkspaceCode).Scan(&keep); err != nil {
			return err
		}

		// 1. The app's own tables (cards, listings, profiles, tickets, payments). Reference
		//    data (categories) stays.
		var appTables []string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(format('public.%I', tablename)), '{}') FROM pg_tables
			WHERE schemaname = 'public' AND tablename NOT IN ('categories', 'schema_migrations', 'spatial_ref_sys')`).Scan(&appTables); err != nil {
			return err
		}
		var crossRefs int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM pg_constraint c JOIN pg_class s ON s.oid = c.conrelid JOIN pg_namespace sn ON sn.oid = s.relnamespace
			JOIN pg_class t ON t.oid = c.confrelid JOIN pg_namespace tn ON tn.oid = t.relnamespace
			WHERE c.contype = 'f' AND sn.nspname = 'crm' AND tn.nspname = 'public'`).Scan(&crossRefs); err != nil {
			return err
		}
		if crossRefs > 0 {
			return errors.New("a CRM table points at an app table; refusing to cascade into the CRM")
		}
		if len(appTables) > 0 {
			var n int64
			for _, t := range appTables {
				var c int64
				_ = tx.QueryRow(ctx, `SELECT count(*) FROM `+t).Scan(&c)
				n += c
			}
			if _, err := tx.Exec(ctx, `TRUNCATE `+strings.Join(appTables, ", ")+` CASCADE`); err != nil {
				return fmt.Errorf("app tables: %w", err)
			}
			counts["public.*"] = n
		}

		// 2. CRM tables.
		rows, err := tx.Query(ctx, `
			SELECT t.tablename, EXISTS (SELECT 1 FROM information_schema.columns c
			       WHERE c.table_schema = 'crm' AND c.table_name = t.tablename AND c.column_name = 'workspace_id')
			FROM pg_tables t WHERE t.schemaname = 'crm' ORDER BY 1`)
		if err != nil {
			return err
		}
		steps := []eraseStep{}
		for rows.Next() {
			var name string
			var hasWS bool
			if err := rows.Scan(&name, &hasWS); err != nil {
				rows.Close()
				return err
			}
			table := pgx.Identifier{"crm", name}.Sanitize()
			switch {
			case keptWhole[name]:
			case keptConfig[name] && hasWS:
				steps = append(steps, eraseStep{name, `DELETE FROM ` + table + ` WHERE workspace_id IS NOT NULL AND workspace_id <> ALL($1)`, []any{keep}})
			default:
				steps = append(steps, eraseStep{name, `DELETE FROM ` + table, nil})
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		steps = append(steps,
			eraseStep{"memberships (people)", `DELETE FROM crm.memberships WHERE identity_id <> ALL($1)`, []any{owners}},
			eraseStep{"workspaces", `DELETE FROM crm.workspaces WHERE id <> ALL($1)`, []any{keep}},
			eraseStep{"connector cursor", `DELETE FROM crm.connector_state WHERE key = 'cardflow'`, nil},
		)
		// Setup rows that stay may name a person who doesn't ("created by"): hand them to the owner.
		refs, err := tx.Query(ctx, `
			SELECT s.relname, a.attname FROM pg_constraint c
			JOIN pg_class s ON s.oid = c.conrelid JOIN pg_namespace sn ON sn.oid = s.relnamespace
			JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
			WHERE c.contype = 'f' AND c.confrelid = 'crm.identities'::regclass AND sn.nspname = 'crm'
			  AND c.confdeltype NOT IN ('c', 'n') AND array_length(c.conkey, 1) = 1`)
		if err != nil {
			return err
		}
		for refs.Next() {
			var table, column string
			if err := refs.Scan(&table, &column); err != nil {
				refs.Close()
				return err
			}
			if table == "identities" {
				continue
			}
			t, col := pgx.Identifier{"crm", table}.Sanitize(), pgx.Identifier{column}.Sanitize()
			steps = append(steps, eraseStep{table + "." + column, `UPDATE ` + t + ` SET ` + col + ` = $2 WHERE ` + col + ` IS NOT NULL AND ` + col + ` <> ALL($1)`, []any{owners, owners[0]}})
		}
		refs.Close()
		if err := refs.Err(); err != nil {
			return err
		}
		steps = append(steps, eraseStep{"identities", `DELETE FROM crm.identities WHERE NOT is_platform_owner`, nil})

		// Run until everything has gone through: a step blocked by a row another step
		// removes is simply tried again.
		pending := steps
		for len(pending) > 0 {
			var next []eraseStep
			var lastErr error
			progressed := false
			for _, s := range pending {
				sp, err := tx.Begin(ctx)
				if err != nil {
					return err
				}
				tag, err := sp.Exec(ctx, s.sql, s.args...)
				if err != nil {
					_ = sp.Rollback(ctx)
					var pgErr *pgconn.PgError
					if errors.As(err, &pgErr) && pgErr.Code == "23503" { // foreign key: not yet
						next, lastErr = append(next, s), fmt.Errorf("%s: %w", s.name, err)
						continue
					}
					return fmt.Errorf("%s: %w", s.name, err)
				}
				if err := sp.Commit(ctx); err != nil {
					return err
				}
				counts[s.name] += tag.RowsAffected()
				progressed = true
			}
			if !progressed {
				return fmt.Errorf("could not finish erasing: %w", lastErr)
			}
			pending = next
		}

		// The owner's address, when the host names one and the only owner has another.
		if email := strings.ToLower(strings.TrimSpace(m.cfg.OwnerEmail)); email != "" && len(owners) == 1 {
			var has bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.verified_identifiers WHERE kind = 'email' AND namespace = 'global' AND value_normalized = $1)`, email).Scan(&has); err != nil {
				return err
			}
			if !has {
				if _, err := tx.Exec(ctx, `DELETE FROM crm.verified_identifiers WHERE identity_id = $1 AND kind = 'email' AND namespace = 'global'`, owners[0]); err != nil {
					return err
				}
				if _, err := tx.Exec(ctx, `INSERT INTO crm.verified_identifiers (identity_id, kind, value_normalized, namespace, verified_at) VALUES ($1, 'email', $2, 'global', now())`,
					owners[0], email); err != nil {
					return err
				}
			}
		}
		_, err = tx.Exec(ctx, `INSERT INTO crm.connector_state (key, value) VALUES ($1, jsonb_build_object('at', now(), 'rows', $2::bigint))`, marker, sum(counts))
		return err
	})
	return counts, err
}

// FreshStarted reports whether a fresh start has ever run here (demo data is not seeded again after one).
func FreshStarted(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}) bool {
	var done bool
	_ = q.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.connector_state WHERE key LIKE 'fresh-start:%')`).Scan(&done)
	return done
}

type seedBusiness struct {
	in      platform.CreateBusinessInput
	account string
	contact [2]string // first, last
	newLead [3]string // first, last, company
	won     [3]string // converted lead: first, last, company
	deal    string
	amount  float64
	task    string
	meeting string
	support string
	income  [2]any // name, amount
	expense [2]any
}

// seedBaseline creates the one customer every environment starts with.
func (m *Module) seedBaseline(ctx context.Context) error {
	const phone, name = "+919876543211", "Ajay krishnan s"
	setupID, err := platform.EnsureStandardSetup(ctx, m.store.Pool)
	if err != nil {
		return err
	}
	businesses := []seedBusiness{
		{
			in:      platform.CreateBusinessInput{Name: "Ajay tech", Industry: "Technology", City: "Coimbatore", State: "Tamil Nadu", Country: "India", Phone: "9876543211"},
			account: "Kovai Software Labs", contact: [2]string{"Priya", "Raman"},
			newLead: [3]string{"Arun", "Mehta", "Mehta Textiles"}, won: [3]string{"Divya", "Nair", "Nair Exports"},
			deal: "Website revamp for Nair Exports", amount: 150000, task: "Call Arun about the product demo", meeting: "Demo with Mehta Textiles",
			support: "Login issue reported by Kovai Software Labs", income: [2]any{"Advance from Nair Exports", 25000.0}, expense: [2]any{"Cloud hosting", 4000.0},
		},
		{
			in:      platform.CreateBusinessInput{Name: "Ajay finace", Industry: "Finance", City: "Coimbatore", State: "Tamil Nadu", Country: "India", Phone: "9876543211"},
			account: "Sundaram Traders", contact: [2]string{"Karthik", "Sundaram"},
			newLead: [3]string{"Meena", "Iyer", "Iyer Jewellers"}, won: [3]string{"Rahul", "Verma", "Verma Motors"},
			deal: "Working capital loan for Verma Motors", amount: 500000, task: "Collect KYC documents from Meena", meeting: "Review meeting with Sundaram Traders",
			support: "Statement not received by Verma Motors", income: [2]any{"Processing fee from Verma Motors", 12000.0}, expense: [2]any{"Office rent", 18000.0},
		},
	}
	type made struct {
		ws uuid.UUID
		in platform.CreateBusinessInput
	}
	var created []made
	var person uuid.UUID
	err = m.store.WithTx(ctx, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO crm.identities (display_name, source) VALUES ($1, 'baseline') RETURNING id`, name).Scan(&person); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO crm.verified_identifiers (identity_id, kind, value_normalized, namespace, verified_at) VALUES ($1, 'phone', $2, 'global', now())`,
			person, phone); err != nil {
			return err
		}
		if m.profile != nil {
			if _, err := m.profile.SignedIn(ctx, tx, person, phone, name, true); err != nil {
				return fmt.Errorf("app profile: %w", err)
			}
		}
		today := time.Now()
		day := func(offset int) string { return today.AddDate(0, 0, offset).Format("2006-01-02") }
		for _, b := range businesses {
			ws, _, err := platform.CreateBusinessTx(ctx, tx, person, setupID, b.in)
			if err != nil {
				return fmt.Errorf("business %s: %w", b.in.Name, err)
			}
			created = append(created, made{ws, b.in})
			add := func(object string, values map[string]any) (uuid.UUID, error) {
				id, err := m.records.SeedRecord(ctx, tx, ws, object, person, values)
				if err != nil {
					return uuid.Nil, fmt.Errorf("%s in %s: %w", object, b.in.Name, err)
				}
				return id, nil
			}
			account, err := add("accounts", map[string]any{"name": b.account, "kind": "business", "type": "customer", "phone": "0422 4000100", "billingCity": "Coimbatore"})
			if err != nil {
				return err
			}
			if _, err := add("contacts", map[string]any{"firstName": b.contact[0], "lastName": b.contact[1], "accountId": account.String(),
				"email": strings.ToLower(b.contact[0]) + "@example.com", "phone": "98400 10001", "title": "Manager"}); err != nil {
				return err
			}
			if _, err := add("leads", map[string]any{"firstName": b.newLead[0], "lastName": b.newLead[1], "organization": b.newLead[2],
				"phone": "98400 20002", "source": "referral", "nextFollowUpAt": today.Add(24 * time.Hour).UTC().Format(time.RFC3339)}); err != nil {
				return err
			}
			// A converted lead: the lead, the account and contact it became, and the deal.
			wonLead, err := add("leads", map[string]any{"firstName": b.won[0], "lastName": b.won[1], "organization": b.won[2], "phone": "98400 30003", "source": "business_card"})
			if err != nil {
				return err
			}
			wonAccount, err := add("accounts", map[string]any{"name": b.won[2], "kind": "business", "type": "customer"})
			if err != nil {
				return err
			}
			wonContact, err := add("contacts", map[string]any{"firstName": b.won[0], "lastName": b.won[1], "accountId": wonAccount.String(), "phone": "98400 30003"})
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `UPDATE crm.leads SET status = 'converted', converted_at = now(), converted_account_id = $2, converted_contact_id = $3 WHERE id = $1`,
				wonLead, wonAccount, wonContact); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO crm.lead_conversions (workspace_id, lead_id, account_id, contact_id, converted_by) VALUES ($1, $2, $3, $4, $5)`,
				ws, wonLead, wonAccount, wonContact, person); err != nil {
				return err
			}
			for _, rec := range []struct {
				object string
				values map[string]any
			}{
				{"opportunities", map[string]any{"name": b.deal, "amount": b.amount, "closeDate": day(21), "accountId": wonAccount.String()}},
				{"tasks", map[string]any{"name": b.task, "dueDate": day(1)}},
				{"events", map[string]any{"name": b.meeting, "startsAt": today.Add(48 * time.Hour).UTC().Format(time.RFC3339)}},
				{"cases", map[string]any{"name": b.support, "accountId": account.String()}},
				{"income", map[string]any{"name": b.income[0], "amount": b.income[1], "date": day(0)}},
				{"expenses", map[string]any{"name": b.expense[0], "amount": b.expense[1], "date": day(0)}},
			} {
				// In a savepoint: an object this setup doesn't carry is skipped, not fatal.
				sp, err := tx.Begin(ctx)
				if err != nil {
					return err
				}
				if _, err := m.records.SeedRecord(ctx, sp, ws, rec.object, person, rec.values); err != nil {
					_ = sp.Rollback(ctx)
					slog.Warn("crm: baseline record skipped", "object", rec.object, "business", b.in.Name, "error", err)
					continue
				}
				if err := sp.Commit(ctx); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	// Each business gets its public listing (directory, digital card, QR).
	if platform.AfterBusinessCreated != nil {
		for _, c := range created {
			platform.AfterBusinessCreated(ctx, person, c.ws, c.in)
		}
	}
	return nil
}
