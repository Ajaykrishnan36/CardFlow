package database

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
)

//go:embed migrations/001_initial_schema.sql
var schemaSQL string

//go:embed migrations/002_seed_data.sql
var seedSQL string

//go:embed migrations/003_saved_card_gstin_image.sql
var migration003SQL string

//go:embed migrations/004_saved_card_image_data.sql
var migration004SQL string

//go:embed migrations/005_business_card_images.sql
var migration005SQL string

//go:embed migrations/006_dev_minimal_bootstrap.sql
var migration006BootstrapSQL string

//go:embed migrations/007_dev_business_bootstrap.sql
var migration007BusinessBootstrapSQL string

//go:embed migrations/008_user_subscription.sql
var migration008UserSubscriptionSQL string

//go:embed migrations/009_subscription_payments.sql
var migration009SubscriptionPaymentsSQL string

//go:embed migrations/010_contact_backups.sql
var migration010ContactBackupsSQL string

//go:embed migrations/011_saved_card_linked_business.sql
var migration011SavedCardLinkedBusinessSQL string

//go:embed migrations/012_support_tickets.sql
var migration012SupportTicketsSQL string

//go:embed migrations/013_revenuecat.sql
var migration013RevenueCatSQL string

//go:embed migrations/014_card_businesses.sql
var migration014CardBusinessesSQL string

func RunMigrations(ctx context.Context, db *DB) error {
	if db == nil || db.Pool == nil {
		return fmt.Errorf("database pool is not initialized")
	}

	slog.Info("Running database schema migrations...")
	if _, err := db.Pool.Exec(ctx, schemaSQL); err != nil {
		slog.Warn("Full schema migration failed (PostGIS may be missing); applying minimal card vault bootstrap", "error", err)
		if _, err2 := db.Pool.Exec(ctx, migration006BootstrapSQL); err2 != nil {
			return fmt.Errorf("failed executing schema migration and minimal bootstrap: %w", err2)
		}
		slog.Info("Minimal card vault bootstrap applied successfully")
	} else {
		slog.Info("Schema migration applied successfully")
	}

	// Applied in order (a map would run them in random order, so a later
	// migration could run before the one that creates what it alters).
	for _, m := range []struct{ name, sql string }{
		{"003_saved_card_gstin_image", migration003SQL},
		{"004_saved_card_image_data", migration004SQL},
		{"005_business_card_images", migration005SQL},
		{"007_dev_business_bootstrap", migration007BusinessBootstrapSQL},
		{"008_user_subscription", migration008UserSubscriptionSQL},
		{"009_subscription_payments", migration009SubscriptionPaymentsSQL},
		{"010_contact_backups", migration010ContactBackupsSQL},
		{"011_saved_card_linked_business", migration011SavedCardLinkedBusinessSQL},
		{"012_support_tickets", migration012SupportTicketsSQL},
		{"013_revenuecat", migration013RevenueCatSQL},
		{"014_card_businesses", migration014CardBusinessesSQL},
	} {
		name, sql := m.name, m.sql
		if _, err := db.Pool.Exec(ctx, sql); err != nil {
			slog.Warn("Incremental migration note", "migration", name, "error", err)
		} else {
			slog.Info("Incremental migration applied", "migration", name)
		}
	}

	slog.Info("Applying initial seed data (Categories & DEV Test Users)...")
	if _, err := db.Pool.Exec(ctx, seedSQL); err != nil {
		slog.Warn("Seed migration execution note", "error", err)
	} else {
		slog.Info("Seed data applied successfully")
	}

	return nil
}
