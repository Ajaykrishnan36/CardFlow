package auth

import (
	"context"
	"fmt"
	"strings"
	"time"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/domain"
	"github.com/google/uuid"
)

// UpdateUserProfile changes only the fields that were sent (a missing field is left as it
// is) and returns the stored profile. A new name is copied to the person's identity, so
// the app and the CRM show the same name.
func (s *AuthService) UpdateUserProfile(ctx context.Context, user *domain.User, req UpdateProfileRequest) (*domain.User, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	if s.db == nil || s.db.Pool == nil {
		return nil, fmt.Errorf("database unavailable")
	}
	trim := func(p *string) *string {
		if p == nil {
			return nil
		}
		v := strings.TrimSpace(*p)
		return &v
	}
	name := trim(req.Name)
	if name != nil && (*name == "" || len(*name) > 100) {
		return nil, fmt.Errorf("enter a name up to 100 characters")
	}
	tx, err := s.db.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var identityID *uuid.UUID
	if err := tx.QueryRow(ctx, `
		UPDATE users
		SET name = COALESCE($2, name), email = COALESCE($3, email), city = COALESCE($4, city), state = COALESCE($5, state), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
		RETURNING identity_id`, user.ID, name, trim(req.Email), trim(req.City), trim(req.State)).Scan(&identityID); err != nil {
		return nil, err
	}
	if name != nil && identityID != nil {
		if _, err := tx.Exec(ctx, `UPDATE crm.identities SET display_name = $2, updated_at = now() WHERE id = $1`, *identityID, *name); err != nil {
			return nil, err
		}
	}
	updated, err := scanUser(tx.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, user.ID))
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return updated, nil
}

// ChangePhone updates the caller's own mobile number after verifying an OTP
// sent to the new number — unlike VerifyOTP (login), this never resolves or
// creates a different account; it only ever touches the caller's own row.
func (s *AuthService) ChangePhone(ctx context.Context, user *domain.User, rawPhone, otpCode string, meta identity.RequestMeta) (*domain.User, error) {
	if user == nil {
		return nil, fmt.Errorf("user required")
	}
	if s.ident == nil || s.db == nil || s.db.Pool == nil {
		return nil, errSignInUnavailable
	}
	var identityID *uuid.UUID
	if err := s.db.Pool.QueryRow(ctx, `SELECT identity_id FROM users WHERE id = $1 AND deleted_at IS NULL`, user.ID).Scan(&identityID); err != nil || identityID == nil {
		return nil, fmt.Errorf("sign in again to change your number")
	}
	// The identity service checks the code sent to the new number, moves the identity to
	// it and updates this profile row in the same transaction.
	phone, err := s.ident.ChangePhone(ctx, *identityID, rawPhone, strings.TrimSpace(otpCode), meta)
	if err != nil {
		return nil, err
	}
	user.Phone = phone
	user.UpdatedAt = time.Now()
	return user, nil
}
