package platform

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Business codes (D-158). Every business has its own address, base URL / code, where the
// code is
//
//	DD MM YY   the day it was created (India time)
//	WW         ISO week of that day (Monday start, 01–53)
//	TTTT       last four characters of its GSTIN, PAN or TAN (0000 when none)
//	NNNNNN     the next number of crm.business_code_seq, shuffled with a server key (D-165)
//
// for example 27102644234F804317 (27 Oct 2026, week 44, PAN …234F, number 804317). The running
// number makes it unique and the shuffle makes it unguessable: the next business's code can't
// be worked out from one already seen. The code is stored in lower case and accepted in any
// case or with dashes (271026-44-234F-804317). The business name is never part of it.

var india = func() *time.Location {
	if l, err := time.LoadLocation("Asia/Kolkata"); err == nil {
		return l
	}
	return time.FixedZone("IST", 5*3600+1800)
}()

// codeKey keys the shuffle of the running number. SetCodeKey replaces it with one made from
// the server's secret at start; this default is only for tests and tools.
var codeKey = []byte("ajays-crm-business-code")

// SetCodeKey keys the six digits of new business codes with the server's secret. Codes already
// given out never change.
func SetCodeKey(secret []byte) {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("business-code-number"))
	codeKey = mac.Sum(nil)
}

// codeNumber turns the n-th running number into the code's six digits: a keyed shuffle of
// 000000–999999 (a Feistel network on 20 bits, walked back until it lands below a million).
// Every running number gets its own six digits, so codes stay unique, but they come out in no
// order anyone can follow without the key.
func codeNumber(n int64) int {
	x := uint32(n % 1_000_000)
	for {
		if x = shuffle20(x); x < 1_000_000 {
			return int(x)
		}
	}
}

func shuffle20(x uint32) uint32 {
	l, r := x>>10, x&0x3ff
	for round := byte(0); round < 8; round++ {
		mac := hmac.New(sha256.New, codeKey)
		mac.Write([]byte{round, byte(r >> 8), byte(r)})
		s := mac.Sum(nil)
		l, r = r, l^((uint32(s[0])<<8|uint32(s[1]))&0x3ff)
	}
	return l<<10 | r
}

// codeParts builds the code before the running number.
func codeParts(created time.Time, taxID string) string {
	d := created.In(india)
	_, week := d.ISOWeek()
	tail := strings.ToLower(strings.Join(strings.Fields(taxID), ""))
	if len(tail) < 4 {
		tail = strings.Repeat("0", 4-len(tail)) + tail
	}
	return fmt.Sprintf("%s%02d%s", d.Format("020106"), week, tail[len(tail)-4:])
}

// GenerateBusinessCode reserves the next running number and returns a code no tenant or
// business uses yet.
func GenerateBusinessCode(ctx context.Context, tx pgx.Tx, created time.Time, taxID string) (string, error) {
	head := codeParts(created, taxID)
	for i := 0; i < 5; i++ {
		var n int64
		if err := tx.QueryRow(ctx, `SELECT nextval('crm.business_code_seq')`).Scan(&n); err != nil {
			return "", err
		}
		code := fmt.Sprintf("%s%06d", head, codeNumber(n))
		taken, err := slugTaken(ctx, tx, code)
		if err != nil {
			return "", err
		}
		if !taken {
			return code, nil
		}
	}
	return "", fmt.Errorf("could not find a free business code")
}

// NormalizeCode turns what someone typed (any case, dashes, spaces) into the stored form.
func NormalizeCode(raw string) string {
	return strings.ToLower(strings.NewReplacer("-", "", " ", "").Replace(strings.TrimSpace(raw)))
}

// DisplayCode groups a generated code for reading out: 271026-44-234F-804317.
func DisplayCode(code string) string {
	if len(code) != 18 {
		return code
	}
	up := strings.ToUpper(code)
	return up[:6] + "-" + up[6:8] + "-" + up[8:12] + "-" + up[12:]
}

var businessCodeRe = regexp.MustCompile(`^[0-9]{8}[0-9a-z]{4}[0-9]{6}$`)

// IsBusinessCode: the stored form of a generated code.
func IsBusinessCode(s string) bool { return businessCodeRe.MatchString(s) }

// MigrateBusinessAddresses gives every business made before D-158 its own generated address
// (from the day it was created and its tax ID). A business waiting for approval already has
// its own and keeps it: its URL is the one approval sends. A brand address shared by several
// businesses, or a code made from a name, keeps working: it now points to the oldest of
// those businesses, and each business's own new address is shown on its URL card. Nothing
// is removed; it runs at every start and does nothing once every business has its own code.
func MigrateBusinessAddresses(ctx context.Context, pool *pgxpool.Pool) error {
	rows, err := pool.Query(ctx, `
		SELECT w.id FROM crm.workspaces w LEFT JOIN crm.tenants t ON t.id = w.tenant_id
		WHERE NOT w.is_platform AND (t.id IS NULL OR t.status NOT IN ('active', 'pending') OR t.slug !~ '^[0-9]{8}[0-9a-z]{4}[0-9]{6}$'
		   OR EXISTS (SELECT 1 FROM crm.workspaces o WHERE o.tenant_id = w.tenant_id AND o.id <> w.id AND (o.created_at, o.id) < (w.created_at, w.id)))
		ORDER BY w.created_at, w.id`)
	if err != nil {
		return err
	}
	var ids []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error { return giveOwnAddress(ctx, tx, id) }); err != nil {
			return fmt.Errorf("business %s: %w", id, err)
		}
	}
	if len(ids) > 0 {
		slog.Info("CRM businesses now have their own generated addresses", "businesses", len(ids))
	}
	return nil
}

func giveOwnAddress(ctx context.Context, tx pgx.Tx, ws uuid.UUID) error {
	var name, taxID string
	var created time.Time
	var oldTenant, owner *uuid.UUID
	if err := tx.QueryRow(ctx, `
		SELECT w.name, w.created_at, w.tenant_id, COALESCE(t.owner_identity, w.created_by_identity),
		       COALESCE(NULLIF(w.profile->>'taxId', ''), NULLIF(w.profile->>'gstin', ''), NULLIF(w.profile->>'pan', ''), NULLIF(w.profile->>'tan', ''), '')
		FROM crm.workspaces w LEFT JOIN crm.tenants t ON t.id = w.tenant_id WHERE w.id = $1 FOR UPDATE OF w`, ws).
		Scan(&name, &created, &oldTenant, &owner, &taxID); err != nil {
		return err
	}
	code, err := GenerateBusinessCode(ctx, tx, created, taxID)
	if err != nil {
		return err
	}
	newID, err := createTenantTx(ctx, tx, ws, code, name, owner)
	if err != nil {
		return err
	}
	if oldTenant == nil {
		return nil
	}
	// The first business to move takes the old address's place: the old address and its own
	// domain now lead to it.
	var domain *string
	tag, err := tx.Exec(ctx, `UPDATE crm.tenants SET status = 'redirected', redirect_to = $2, updated_at = now() WHERE id = $1 AND redirect_to IS NULL`, *oldTenant, newID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT custom_domain FROM crm.tenants WHERE id = $1`, *oldTenant).Scan(&domain); err != nil || domain == nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE crm.tenants SET custom_domain = NULL WHERE id = $1`, *oldTenant); err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `UPDATE crm.tenants SET custom_domain = $2 WHERE id = $1`, newID, *domain)
	return err
}
