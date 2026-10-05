package crm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"cardflow-backend/internal/crm/connectors/cardflow"
	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/platform"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Fresh start (D-106, D-108): erase every customer's data except the one account the
// platform starts with.
//
// It runs only when the host sets CRM_FRESH_START to a value that starts with
// "erase-everything-" (for example erase-everything-2026-10-05). Each value runs once: the
// run is recorded, so leaving the setting in place — or a restart — never erases again.
// Nobody can trigger it through the API.
//
// Kept:
//   - the platform owner's login;
//   - one person, by mobile number (default +91 98765 43211; CRM_FRESH_START_PHONE), with
//     their app profile and their own saved cards;
//   - one business of that person, by name (default "Ajay traders";
//     CRM_FRESH_START_BUSINESS), with its records, its setup and its public listing;
//   - what holds no customer data: the platform and app-connector workspaces' setup,
//     setups, plans, platform settings, directory categories.
//
// Erased: every other person, business, record, card, listing, ticket, session, file,
// message and audit entry — including that person's other businesses.
//
// Afterwards whatever of the kept account is missing is created (the person, the
// business, its listing), so every environment ends in the same state.

const (
	freshStartPrefix     = "erase-everything-"
	defaultKeepPhone     = "+919876543211"
	defaultKeepBusiness  = "Ajay traders"
	defaultKeepPersonNew = "Ajay"
)

// keptConfig are tables whose rows for the kept workspaces are setup, not customer data.
var keptConfig = map[string]bool{
	"workspace_products": true, "roles": true, "permission_sets": true, "role_assignments": true, "memberships": true,
	"membership_permission_sets": true, "object_definitions": true, "field_definitions": true, "layouts": true, "views": true,
	"reports": true, "dashboards": true, "workflows": true, "mail_accounts": true, "sso_providers": true, "webhooks": true,
	"api_keys": true, "invite_links": true, "assignment_state": true,
	// The built-in relationship types (no workspace) are part of the product (D-111).
	"relationship_types": true,
}

// keptWhole are tables this pass never deletes from directly (no customer data, or rows
// that go with their identity or workflow through ON DELETE CASCADE).
var keptWhole = map[string]bool{
	"schema_migrations": true, "platform_settings": true, "products": true, "product_versions": true, "plans": true,
	"connector_state": true, "workspaces": true, "identities": true, "verified_identifiers": true, "password_credentials": true,
	"mfa_methods": true, "workflow_versions": true,
}

func keepPhone() string {
	if p, ok := identity.NormalizePhone(os.Getenv("CRM_FRESH_START_PHONE")); ok {
		return p
	}
	return defaultKeepPhone
}

func keepBusinessName() string {
	if n := strings.TrimSpace(os.Getenv("CRM_FRESH_START_BUSINESS")); n != "" {
		return n
	}
	return defaultKeepBusiness
}

// FreshStart erases when the host asked for it. Call after the server is wired.
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
	slog.Warn("crm: FRESH START — erasing customer data", "run", run, "keepBusiness", keepBusinessName())
	counts, err := m.eraseEverything(ctx, marker)
	if err != nil {
		slog.Error("crm: fresh start failed — nothing was erased", "error", err)
		return
	}
	slog.Warn("crm: fresh start erased customer data", "steps", len(counts), "rows", sum(counts))
	if m.records != nil {
		// Objects that no longer exist must leave the running server's registry too.
		if err := m.records.LoadObjects(ctx); err != nil {
			slog.Warn("crm: fresh start — object registry not reloaded (a restart fixes it)", "error", err)
		}
	}
	if err := m.ensureKeptAccount(ctx); err != nil {
		slog.Error("crm: fresh start — the kept account is incomplete", "error", err)
		return
	}
	slog.Warn("crm: fresh start complete")
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

func texts(ids []uuid.UUID) []string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.String()
	}
	return out
}

// eraseEverything deletes in one transaction: either all of it happens or none.
func (m *Module) eraseEverything(ctx context.Context, marker string) (map[string]int64, error) {
	counts := map[string]int64{}
	err := m.store.WithTx(ctx, func(tx pgx.Tx) error {
		// ---- who and what stays ----
		var owners []uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(id), '{}') FROM crm.identities WHERE is_platform_owner`).Scan(&owners); err != nil {
			return err
		}
		if len(owners) == 0 {
			return errors.New("there is no platform owner to keep")
		}
		people := append([]uuid.UUID{}, owners...)
		var person uuid.UUID
		err := tx.QueryRow(ctx, `SELECT identity_id FROM crm.verified_identifiers WHERE kind = 'phone' AND namespace = 'global' AND value_normalized = $1`,
			keepPhone()).Scan(&person)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		hasPerson := err == nil
		if hasPerson {
			people = append(people, person)
		}
		// The kept person's business, by name (the oldest when there are two of that name).
		keptBusiness := []uuid.UUID{}
		if hasPerson {
			var ws uuid.UUID
			err := tx.QueryRow(ctx, `SELECT id FROM crm.workspaces WHERE created_by_identity = $1 AND NOT is_platform AND lower(trim(name)) = lower($2)
				ORDER BY created_at LIMIT 1`, person, keepBusinessName()).Scan(&ws)
			if err != nil && !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
			if err == nil {
				keptBusiness = append(keptBusiness, ws)
			}
		}
		var system []uuid.UUID
		if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(id), '{}') FROM crm.workspaces WHERE is_platform OR code = $1`, cardflow.WorkspaceCode).Scan(&system); err != nil {
			return err
		}
		keepSetup := append(append([]uuid.UUID{}, system...), keptBusiness...) // workspaces whose setup stays

		steps := []eraseStep{}

		// ---- 1. the app's own tables (cards, listings, profiles, tickets, payments) ----
		var hasUsers bool
		if err := tx.QueryRow(ctx, `SELECT to_regclass('public.users') IS NOT NULL`).Scan(&hasUsers); err != nil {
			return err
		}
		if hasUsers {
			var keptUsers, keptListings []uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(id), '{}') FROM public.users WHERE identity_id = ANY($1)`, people).Scan(&keptUsers); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(id), '{}') FROM public.businesses WHERE workspace_id = ANY($1)`, keptBusiness).Scan(&keptListings); err != nil {
				return err
			}
			users, listings, ids := texts(keptUsers), texts(keptListings), texts(people)
			rows, err := tx.Query(ctx, `
				SELECT t.tablename,
				       EXISTS (SELECT 1 FROM information_schema.columns c WHERE c.table_schema = 'public' AND c.table_name = t.tablename AND c.column_name = 'user_id'),
				       EXISTS (SELECT 1 FROM information_schema.columns c WHERE c.table_schema = 'public' AND c.table_name = t.tablename AND c.column_name = 'business_id'),
				       EXISTS (SELECT 1 FROM information_schema.columns c WHERE c.table_schema = 'public' AND c.table_name = t.tablename AND c.column_name = 'saved_card_id')
				FROM pg_tables t WHERE t.schemaname = 'public'
				  AND t.tablename NOT IN ('categories', 'schema_migrations', 'spatial_ref_sys', 'users', 'businesses', 'saved_cards') ORDER BY 1`)
			if err != nil {
				return err
			}
			for rows.Next() {
				var name string
				var byUser, byBusiness, byCard bool
				if err := rows.Scan(&name, &byUser, &byBusiness, &byCard); err != nil {
					rows.Close()
					return err
				}
				table := pgx.Identifier{"public", name}.Sanitize()
				conds := []string{}
				if byUser {
					conds = append(conds, `(user_id IS NULL OR user_id::text <> ALL($1::text[]))`)
				}
				if byBusiness {
					conds = append(conds, `(business_id IS NULL OR business_id::text <> ALL($2::text[]))`)
				}
				switch {
				case len(conds) > 0:
					steps = append(steps, eraseStep{"app: " + name, `DELETE FROM ` + table + ` WHERE ($1::text[] IS NOT NULL AND $2::text[] IS NOT NULL) AND (` + strings.Join(conds, " OR ") + `)`, []any{users, listings}})
				case byCard:
					// Parts of a saved card go with the card (ON DELETE CASCADE).
				default:
					steps = append(steps, eraseStep{"app: " + name, `DELETE FROM ` + table, nil})
				}
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
			var cardLinksListing bool
			_ = tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns WHERE table_schema = 'public' AND table_name = 'saved_cards' AND column_name = 'linked_business_id')`).Scan(&cardLinksListing)
			if cardLinksListing {
				steps = append(steps, eraseStep{"app: saved_cards → listing", `UPDATE public.saved_cards SET linked_business_id = NULL
					WHERE linked_business_id IS NOT NULL AND linked_business_id::text <> ALL($1::text[])`, []any{listings}})
			}
			steps = append(steps,
				eraseStep{"app: saved_cards", `DELETE FROM public.saved_cards
					WHERE NOT (COALESCE(identity_id::text = ANY($1::text[]), false) OR COALESCE(user_id::text = ANY($2::text[]), false))`, []any{ids, users}},
				eraseStep{"app: businesses", `DELETE FROM public.businesses WHERE workspace_id IS NULL OR workspace_id::text <> ALL($1::text[])`, []any{texts(keptBusiness)}},
				eraseStep{"app: users", `DELETE FROM public.users WHERE id::text <> ALL($1::text[])`, []any{users}},
			)
		}

		// ---- 2. CRM tables ----
		rows, err := tx.Query(ctx, `
			SELECT t.tablename,
			       EXISTS (SELECT 1 FROM information_schema.columns c WHERE c.table_schema = 'crm' AND c.table_name = t.tablename AND c.column_name = 'workspace_id'),
			       EXISTS (SELECT 1 FROM information_schema.columns c WHERE c.table_schema = 'crm' AND c.table_name = t.tablename AND c.column_name = 'identity_id')
			FROM pg_tables t WHERE t.schemaname = 'crm' ORDER BY 1`)
		if err != nil {
			return err
		}
		withWorkspace := map[string]bool{}
		for rows.Next() {
			var name string
			var hasWS, hasIdentity bool
			if err := rows.Scan(&name, &hasWS, &hasIdentity); err != nil {
				rows.Close()
				return err
			}
			withWorkspace[name] = hasWS
			table := pgx.Identifier{"crm", name}.Sanitize()
			switch {
			case keptWhole[name]:
			case keptConfig[name] && hasWS:
				steps = append(steps, eraseStep{name, `DELETE FROM ` + table + ` WHERE workspace_id IS NOT NULL AND workspace_id <> ALL($1)`, []any{keepSetup}})
			case hasWS:
				// Customer data: only the kept business's rows stay.
				steps = append(steps, eraseStep{name, `DELETE FROM ` + table + ` WHERE workspace_id IS NULL OR workspace_id <> ALL($1)`, []any{keptBusiness}})
			case hasIdentity:
				steps = append(steps, eraseStep{name, `DELETE FROM ` + table + ` WHERE identity_id IS NULL OR identity_id <> ALL($1)`, []any{people}})
			default:
				steps = append(steps, eraseStep{name, `DELETE FROM ` + table, nil})
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		// Setups ("apps") nobody uses any more and objects the owner made for testing go too:
		// only the standard setup, the app connector's setup and the built-in objects stay.
		unusedSetups := `SELECT id FROM crm.products WHERE key NOT IN ($1, $2) AND id NOT IN (SELECT product_id FROM crm.workspace_products)`
		setupKeys := []any{platform.StandardSetupKey, cardflow.ProductKey}
		var customObjects []string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(key), '{}') FROM crm.object_definitions
			WHERE NOT is_standard AND (workspace_id IS NULL OR workspace_id <> ALL($1))`, keepSetup).Scan(&customObjects); err != nil {
			return err
		}
		steps = append(steps,
			eraseStep{"workspaces", `DELETE FROM crm.workspaces WHERE id <> ALL($1)`, []any{keepSetup}},
			eraseStep{"connector cursor", `DELETE FROM crm.connector_state WHERE key = 'cardflow'`, nil},
			eraseStep{"leads → unused setup", `UPDATE crm.leads SET product_id = NULL WHERE product_id IN (` + unusedSetups + `)`, setupKeys},
			eraseStep{"unused setup versions", `DELETE FROM crm.product_versions WHERE product_id IN (` + unusedSetups + `)`, setupKeys},
			eraseStep{"unused setups", `DELETE FROM crm.products WHERE id IN (` + unusedSetups + `)`, setupKeys},
			eraseStep{"test objects", `DELETE FROM crm.object_definitions WHERE key = ANY($1)`, []any{customObjects}},
		)
		for _, key := range customObjects {
			steps = append(steps, eraseStep{"view of " + key, `DROP VIEW IF EXISTS ` + pgx.Identifier{"crm", "obj_" + key}.Sanitize(), nil})
		}
		// Rows that stay may name a person who doesn't ("created by", "owner"): inside the
		// kept business they pass to its owner, elsewhere to the platform owner.
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
			if column == "identity_id" {
				// The row is about that person (a membership, a mailbox): it goes with them.
				steps = append(steps, eraseStep{table + " (people)", `DELETE FROM ` + t + ` WHERE identity_id IS NOT NULL AND identity_id <> ALL($1)`, []any{people}})
				continue
			}
			if hasPerson && withWorkspace[table] && len(keptBusiness) > 0 {
				steps = append(steps, eraseStep{table + "." + column + " (kept business)", `UPDATE ` + t + ` SET ` + col + ` = $2
					WHERE ` + col + ` IS NOT NULL AND ` + col + ` <> ALL($1) AND workspace_id = ANY($3)`, []any{people, person, keptBusiness}})
			}
			steps = append(steps, eraseStep{table + "." + column, `UPDATE ` + t + ` SET ` + col + ` = $2 WHERE ` + col + ` IS NOT NULL AND ` + col + ` <> ALL($1)`, []any{people, owners[0]}})
		}
		refs.Close()
		if err := refs.Err(); err != nil {
			return err
		}
		steps = append(steps, eraseStep{"identities", `DELETE FROM crm.identities WHERE id <> ALL($1)`, []any{people}})

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

// ensureKeptAccount creates whatever of the kept account doesn't exist yet: the person,
// their business, and the business's public listing. Nothing else is added — no sample
// records.
func (m *Module) ensureKeptAccount(ctx context.Context) error {
	phone, businessName := keepPhone(), keepBusinessName()
	setupID, err := platform.EnsureStandardSetup(ctx, m.store.Pool)
	if err != nil {
		return err
	}
	var person, ws uuid.UUID
	createdBusiness := false
	in := platform.CreateBusinessInput{Name: businessName, Phone: strings.TrimPrefix(phone, "+91")}
	err = m.store.WithTx(ctx, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `SELECT identity_id FROM crm.verified_identifiers WHERE kind = 'phone' AND namespace = 'global' AND value_normalized = $1`, phone).Scan(&person)
		if errors.Is(err, pgx.ErrNoRows) {
			if err := tx.QueryRow(ctx, `INSERT INTO crm.identities (display_name, source) VALUES ($1, 'baseline') RETURNING id`, defaultKeepPersonNew).Scan(&person); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `INSERT INTO crm.verified_identifiers (identity_id, kind, value_normalized, namespace, verified_at) VALUES ($1, 'phone', $2, 'global', now())`,
				person, phone); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if m.profile != nil {
			if _, err := m.profile.SignedIn(ctx, tx, person, phone, defaultKeepPersonNew, false); err != nil {
				return fmt.Errorf("app profile: %w", err)
			}
		}
		err = tx.QueryRow(ctx, `SELECT id FROM crm.workspaces WHERE created_by_identity = $1 AND NOT is_platform AND lower(trim(name)) = lower($2)
			ORDER BY created_at LIMIT 1`, person, businessName).Scan(&ws)
		if errors.Is(err, pgx.ErrNoRows) {
			id, _, err := platform.CreateBusinessTx(ctx, tx, person, setupID, in)
			if err != nil {
				return fmt.Errorf("business %s: %w", businessName, err)
			}
			ws, createdBusiness = id, true
			return nil
		}
		return err
	})
	if err != nil {
		return err
	}
	// The business's public listing (directory, digital card, QR), if it has none.
	var hasListing bool
	var listings bool
	_ = m.store.Pool.QueryRow(ctx, `SELECT to_regclass('public.businesses') IS NOT NULL`).Scan(&listings)
	if listings {
		_ = m.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM public.businesses WHERE workspace_id = $1 AND deleted_at IS NULL)`, ws).Scan(&hasListing)
		if !hasListing && platform.AfterBusinessCreated != nil {
			platform.AfterBusinessCreated(ctx, person, ws, in)
		}
	}
	slog.Info("crm: kept account ready", "business", businessName, "createdBusiness", createdBusiness, "createdListing", listings && !hasListing)
	return nil
}
