package crm

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"cardflow-backend/internal/crm/store"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The app's profile row (public.users) hangs off the identity (D-93). public.users keeps
// its id, so cards, businesses and tickets are untouched; identity_id says who it is.

const (
	legacyDefaultName = "CardFlow User" // what the app called a person before they typed a name
	placeholderPhone  = "+910000000000" // a stand-in row older code inserted; never a real person
)

type appProfile struct {
	hasUsers bool
	hasClaim bool // public.businesses has the card-claim columns (app migration 014)
}

func newAppProfile(ctx context.Context, st *store.Store) *appProfile {
	p := &appProfile{}
	_ = st.Pool.QueryRow(ctx, `SELECT to_regclass('public.users') IS NOT NULL`).Scan(&p.hasUsers)
	_ = st.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_schema = 'public' AND table_name = 'businesses' AND column_name = 'contact_phone')`).Scan(&p.hasClaim)
	return p
}

func realName(n string) string {
	n = strings.TrimSpace(n)
	if n == legacyDefaultName || n == "New user" {
		return ""
	}
	return n
}

// SignedIn links the profile row for this phone to the identity (creating it for a new
// person), records the sign-in, and hands over businesses made from cards carrying this
// number that nobody owns yet.
func (p *appProfile) SignedIn(ctx context.Context, tx pgx.Tx, identityID uuid.UUID, phone, name string, created bool) (map[string]any, error) {
	if !p.hasUsers {
		return nil, nil
	}
	var userID uuid.UUID
	var linked *uuid.UUID
	var userName string
	err := tx.QueryRow(ctx, `SELECT id, identity_id, COALESCE(name, '') FROM public.users WHERE phone = $1 AND deleted_at IS NULL FOR UPDATE`, phone).
		Scan(&userID, &linked, &userName)
	isNewProfile := false
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		// The identity may already have a profile under an older number.
		err = tx.QueryRow(ctx, `SELECT id, COALESCE(name, '') FROM public.users WHERE identity_id = $1 AND deleted_at IS NULL`, identityID).Scan(&userID, &userName)
		if errors.Is(err, pgx.ErrNoRows) {
			profileName := realName(name)
			if profileName == "" {
				profileName = legacyDefaultName
			}
			if err := tx.QueryRow(ctx, `
				INSERT INTO public.users (phone, name, role, plan, status, identity_id, last_login_at)
				VALUES ($1, $2, 'user', 'free', 'active', $3, now()) RETURNING id`, phone, profileName, identityID).Scan(&userID); err != nil {
				return nil, err
			}
			userName, isNewProfile = profileName, true
		} else if err != nil {
			return nil, err
		}
	case err != nil:
		return nil, err
	case linked == nil:
		if _, err := tx.Exec(ctx, `UPDATE public.users SET identity_id = $2 WHERE id = $1`, userID, identityID); err != nil {
			return nil, err
		}
	case *linked != identityID:
		return nil, shared.NewError(409, "account_conflict", "This number is linked to another account. Contact support.")
	}
	if _, err := tx.Exec(ctx, `UPDATE public.users SET last_login_at = now() WHERE id = $1`, userID); err != nil {
		return nil, err
	}
	out := map[string]any{"userId": userID.String(), "isNewProfile": isNewProfile}
	// A person the app already knows by name keeps that name on their identity.
	if n := realName(userName); n != "" {
		out["displayName"] = n
		if _, err := tx.Exec(ctx, `UPDATE crm.identities SET display_name = $2, updated_at = now() WHERE id = $1 AND display_name = 'New user'`, identityID, n); err != nil {
			return nil, err
		}
	}
	if p.hasClaim {
		rows, err := tx.Query(ctx, `
			UPDATE public.businesses SET owner_user_id = $1, claimed_at = now(), updated_at = now()
			WHERE owner_user_id IS NULL AND deleted_at IS NULL AND contact_phone = $2
			RETURNING id::text, name, COALESCE(contact_name, '')`, userID, phone)
		if err != nil {
			return nil, err
		}
		claimed := []map[string]string{}
		for rows.Next() {
			var id, bname, contact string
			if err := rows.Scan(&id, &bname, &contact); err != nil {
				rows.Close()
				return nil, err
			}
			claimed = append(claimed, map[string]string{"id": id, "name": bname, "contact_name": contact})
			if contact != "" && out["suggestedName"] == nil {
				out["suggestedName"] = contact
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		out["claimedBusinesses"] = claimed
	}
	return out, nil
}

func (p *appProfile) PhoneChanged(ctx context.Context, tx pgx.Tx, identityID uuid.UUID, phone string) error {
	if !p.hasUsers {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE public.users SET phone = $2, updated_at = now() WHERE identity_id = $1 AND deleted_at IS NULL`, identityID, phone)
	return err
}

func (p *appProfile) Renamed(ctx context.Context, tx pgx.Tx, identityID uuid.UUID, name string) error {
	if !p.hasUsers {
		return nil
	}
	_, err := tx.Exec(ctx, `UPDATE public.users SET name = $2, updated_at = now() WHERE identity_id = $1 AND deleted_at IS NULL`, identityID, name)
	return err
}

// backfillAppIdentities gives every existing app user an identity (once each, D-93). It
// only adds: an identity, its phone identifier and the link. A number that already
// belongs to an identity (an invited CRM user, say) is linked, never duplicated. The
// phone counts as proved only if the person has signed in before; otherwise it is proved
// at their next sign-in. Identities made here carry source = 'app_backfill'.
func backfillAppIdentities(ctx context.Context, st *store.Store) {
	var has bool
	if err := st.Pool.QueryRow(ctx, `SELECT to_regclass('public.users') IS NOT NULL`).Scan(&has); err != nil || !has {
		return
	}
	conn, err := st.Pool.Acquire(ctx)
	if err != nil {
		return
	}
	defer conn.Release()
	var locked bool
	if err := conn.QueryRow(ctx, `SELECT pg_try_advisory_lock(7203117)`).Scan(&locked); err != nil || !locked {
		return // another instance is doing it
	}
	defer func() { _, _ = conn.Exec(context.Background(), `SELECT pg_advisory_unlock(7203117)`) }()

	linked, created, skipped := 0, 0, 0
	for {
		rows, err := st.Pool.Query(ctx, `
			SELECT id, phone, COALESCE(name, ''), last_login_at, status::text FROM public.users
			WHERE identity_id IS NULL AND deleted_at IS NULL AND phone IS NOT NULL AND phone <> $1
			ORDER BY created_at LIMIT 200`, placeholderPhone)
		if err != nil {
			slog.Error("crm: identity backfill stopped", "error", err)
			return
		}
		type appUser struct {
			id        uuid.UUID
			phone     string
			name      string
			lastLogin *time.Time
			status    string
		}
		var batch []appUser
		for rows.Next() {
			var u appUser
			if err := rows.Scan(&u.id, &u.phone, &u.name, &u.lastLogin, &u.status); err == nil {
				batch = append(batch, u)
			}
		}
		rows.Close()
		if len(batch) == 0 {
			break
		}
		progressed := false
		for _, u := range batch {
			phone, ok := identity.NormalizePhone(u.phone)
			if !ok {
				skipped++
				continue
			}
			err := st.WithTx(ctx, func(tx pgx.Tx) error {
				var identityID uuid.UUID
				err := tx.QueryRow(ctx, `SELECT identity_id FROM crm.verified_identifiers WHERE kind = 'phone' AND namespace = 'global' AND value_normalized = $1`, phone).Scan(&identityID)
				if errors.Is(err, pgx.ErrNoRows) {
					name := realName(u.name)
					if name == "" {
						name = "New user"
					}
					status := "active"
					if u.status == "suspended" {
						status = "suspended"
					}
					if err := tx.QueryRow(ctx, `INSERT INTO crm.identities (display_name, status, source, last_login_at) VALUES ($1, $2, 'app_backfill', $3) RETURNING id`,
						name, status, u.lastLogin).Scan(&identityID); err != nil {
						return err
					}
					if _, err := tx.Exec(ctx, `INSERT INTO crm.verified_identifiers (identity_id, kind, value_normalized, namespace, verified_at) VALUES ($1, 'phone', $2, 'global', $3)`,
						identityID, phone, u.lastLogin); err != nil {
						return err
					}
					created++
				} else if err != nil {
					return err
				} else {
					// One identity can carry one app profile; a second profile for the same person is left alone.
					var taken bool
					if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM public.users WHERE identity_id = $1)`, identityID).Scan(&taken); err != nil {
						return err
					}
					if taken {
						return errSkip
					}
					linked++
				}
				_, err = tx.Exec(ctx, `UPDATE public.users SET identity_id = $2 WHERE id = $1 AND identity_id IS NULL`, u.id, identityID)
				return err
			})
			switch {
			case err == nil:
				progressed = true
			case errors.Is(err, errSkip):
				skipped++
			default:
				skipped++
				slog.Warn("crm: identity backfill skipped a user", "user", u.id, "error", err)
			}
		}
		if !progressed {
			break // everything left needs a person to look at it
		}
	}
	if created+linked+skipped > 0 {
		slog.Info("crm: app users linked to identities", "created", created, "linkedToExisting", linked, "skipped", skipped)
	}
}

var errSkip = errors.New("skip")
