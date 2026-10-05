package crm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"

	"cardflow-backend/internal/crm/connectors/cardflow"
	"cardflow-backend/internal/crm/platform"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// One-time clean-up the owner asked for on 5 Oct 2026 (D-129): remove the test businesses
// "Gova test" and "surya work space", and with them the app connector's "Business Card
// Snap" workspace, with everything that belongs to them; then give "Ajay traders" a few
// sample records. It runs by itself at start-up, once, and only in a database that
// actually has one of the two test businesses — so it does nothing anywhere else.
//
// Before anything is deleted, every row is copied to crm.deleted_workspace_archive.
// Other businesses, people, saved cards and listings of other businesses are not touched.

const (
	retireMarker     = "cleanup:test-workspaces-2026-10-05"
	connectorRetired = "connector:cardflow-retired"
	sampleBusiness   = "ajay-traders"
)

var retireCodes = []string{"gova-test", "surya-work-space"}

// connectorEnabled: the app connector runs unless it is switched off by the host, or was
// retired by the clean-up (CRM_CARDFLOW_SYNC=force brings it back).
func (m *Module) connectorEnabled(ctx context.Context) bool {
	switch os.Getenv("CRM_CARDFLOW_SYNC") {
	case "false":
		return false
	case "force":
		return true
	}
	var retired bool
	_ = m.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.connector_state WHERE key = $1)`, connectorRetired).Scan(&retired)
	return !retired
}

// RetireTestWorkspaces does the clean-up described above. It reports whether it ran.
func (m *Module) RetireTestWorkspaces(ctx context.Context) bool {
	if m.store == nil {
		return false
	}
	var done bool
	if err := m.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.connector_state WHERE key = $1)`, retireMarker).Scan(&done); err != nil || done {
		return false
	}
	var found int
	if err := m.store.Pool.QueryRow(ctx, `SELECT count(*) FROM crm.workspaces WHERE code = ANY($1) AND NOT is_platform`, retireCodes).Scan(&found); err != nil || found == 0 {
		return false
	}
	codes := append(append([]string{}, retireCodes...), cardflow.WorkspaceCode)
	counts := map[string]int64{}
	err := m.store.WithTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT id, code FROM crm.workspaces WHERE code = ANY($1) AND NOT is_platform FOR UPDATE`, codes)
		if err != nil {
			return err
		}
		var ids []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			var code string
			if err := rows.Scan(&id, &code); err != nil {
				rows.Close()
				return err
			}
			ids = append(ids, id)
		}
		rows.Close()

		// Every foreign key that points at a business: rows that belong to it (workspace_id)
		// are archived and deleted; other references (an account's provisioned tenant) are cleared.
		refs, err := tx.Query(ctx, `
			SELECT sn.nspname, s.relname, a.attname, a.attnotnull FROM pg_constraint c
			JOIN pg_class s ON s.oid = c.conrelid JOIN pg_namespace sn ON sn.oid = s.relnamespace
			JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
			WHERE c.contype = 'f' AND c.confrelid = 'crm.workspaces'::regclass AND array_length(c.conkey, 1) = 1
			UNION
			-- tables that carry the business's id without a foreign key (the audit log)
			SELECT 'crm', col.table_name, 'workspace_id', false FROM information_schema.columns col
			JOIN pg_tables t ON t.schemaname = 'crm' AND t.tablename = col.table_name
			WHERE col.table_schema = 'crm' AND col.column_name = 'workspace_id'
			ORDER BY 1, 2, 3`)
		if err != nil {
			return err
		}
		type ref struct {
			schema, table, column string
			notNull               bool
		}
		var list []ref
		seen := map[string]bool{}
		for refs.Next() {
			var r ref
			if err := refs.Scan(&r.schema, &r.table, &r.column, &r.notNull); err != nil {
				refs.Close()
				return err
			}
			if key := r.schema + "." + r.table + "." + r.column; !seen[key] {
				seen[key] = true
				list = append(list, r)
			}
		}
		refs.Close()
		type step struct {
			name, sql string
		}
		var steps []step
		for _, r := range list {
			t := pgx.Identifier{r.schema, r.table}.Sanitize()
			col := pgx.Identifier{r.column}.Sanitize()
			name := r.schema + "." + r.table
			switch {
			case r.schema == "crm" && r.table == "workspaces":
			case r.schema == "crm" && r.column == "workspace_id":
				tag, err := tx.Exec(ctx, `INSERT INTO crm.deleted_workspace_archive (workspace_code, table_name, row_data, reason)
					SELECT w.code, $2, to_jsonb(t), $3 FROM `+t+` t JOIN crm.workspaces w ON w.id = t.workspace_id WHERE t.workspace_id = ANY($1)`, ids, name, retireMarker)
				if err != nil {
					return fmt.Errorf("archive %s: %w", name, err)
				}
				counts["archived"] += tag.RowsAffected()
				steps = append(steps, step{name, `DELETE FROM ` + t + ` WHERE workspace_id = ANY($1)`})
			case r.schema == "public" && r.table == "businesses":
				// The public listing of a removed business leaves the directory (kept, hidden).
				steps = append(steps, step{name, `UPDATE public.businesses SET deleted_at = COALESCE(deleted_at, now()), ` + col + ` = NULL WHERE ` + col + ` = ANY($1)`})
			case !r.notNull:
				steps = append(steps, step{name + "." + r.column, `UPDATE ` + t + ` SET ` + col + ` = NULL WHERE ` + col + ` = ANY($1)`})
			default:
				steps = append(steps, step{name + "." + r.column, `DELETE FROM ` + t + ` WHERE ` + col + ` = ANY($1)`})
			}
		}
		// Objects these businesses defined for themselves have a reporting view each.
		var views []string
		if err := tx.QueryRow(ctx, `SELECT COALESCE(array_agg(key), '{}') FROM crm.object_definitions WHERE workspace_id = ANY($1)`, ids).Scan(&views); err != nil {
			return err
		}
		for _, key := range views {
			if _, err := tx.Exec(ctx, `DROP VIEW IF EXISTS `+pgx.Identifier{"crm", "obj_" + key}.Sanitize()); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `INSERT INTO crm.deleted_workspace_archive (workspace_code, table_name, row_data, reason)
			SELECT code, 'crm.workspaces', to_jsonb(w), $2 FROM crm.workspaces w WHERE id = ANY($1)`, ids, retireMarker); err != nil {
			return err
		}
		steps = append(steps, step{"crm.workspaces", `DELETE FROM crm.workspaces WHERE id = ANY($1)`})

		// A step blocked by rows another step removes is tried again.
		pending := steps
		for len(pending) > 0 {
			var next []step
			var lastErr error
			progressed := false
			for _, s := range pending {
				sp, err := tx.Begin(ctx)
				if err != nil {
					return err
				}
				tag, err := sp.Exec(ctx, s.sql, ids)
				if err != nil {
					_ = sp.Rollback(ctx)
					var pgErr *pgconn.PgError
					if errors.As(err, &pgErr) && pgErr.Code == "23503" {
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
				return fmt.Errorf("could not finish: %w", lastErr)
			}
			pending = next
		}
		// Setups ("apps") that no business uses any more, except the standard one.
		if _, err := tx.Exec(ctx, `INSERT INTO crm.deleted_workspace_archive (workspace_code, table_name, row_data, reason)
			SELECT '', 'crm.products', to_jsonb(p), $2 FROM crm.products p WHERE p.key <> $1 AND p.id NOT IN (SELECT product_id FROM crm.workspace_products)`,
			platform.StandardSetupKey, retireMarker); err != nil {
			return err
		}
		unused := `SELECT id FROM crm.products WHERE key <> $1 AND id NOT IN (SELECT product_id FROM crm.workspace_products)`
		if _, err := tx.Exec(ctx, `DELETE FROM crm.product_versions WHERE product_id IN (`+unused+`)`, platform.StandardSetupKey); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM crm.products WHERE id IN (`+unused+`)`, platform.StandardSetupKey)
		if err != nil {
			return err
		}
		counts["unused setups"] = tag.RowsAffected()
		for _, key := range []string{retireMarker, connectorRetired, "cardflow"} {
			if key == "cardflow" {
				// The connector's sync position: it has nothing to sync into any more.
				if _, err := tx.Exec(ctx, `DELETE FROM crm.connector_state WHERE key = $1`, key); err != nil {
					return err
				}
				continue
			}
			if _, err := tx.Exec(ctx, `INSERT INTO crm.connector_state (key, value) VALUES ($1, jsonb_build_object('at', now(), 'businesses', $2::text[])) ON CONFLICT (key) DO NOTHING`, key, codes); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		slog.Error("crm: test businesses not removed — nothing was changed", "error", err)
		return false
	}
	slog.Warn("crm: test businesses removed (rows copied to crm.deleted_workspace_archive)", "businesses", codes, "counts", counts)
	// The business that stays gets a few example records (once).
	if made, err := m.records.SeedSample(ctx, sampleBusiness); err != nil {
		slog.Error("crm: sample data not created", "business", sampleBusiness, "error", err)
	} else if made > 0 {
		slog.Info("crm: sample data created", "business", sampleBusiness, "records", made)
	}
	return true
}
