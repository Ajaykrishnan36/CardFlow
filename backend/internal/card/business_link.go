package card

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"cardflow-backend/internal/domain"
	"cardflow-backend/pkg/validator"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// One business record per GSTIN (migration 014).
//
// The first time anyone saves a card whose GSTIN isn't on CardFlow yet, an
// unclaimed business (owner_user_id NULL, source 'card') is created from the
// card. Every later save of the same GSTIN links to that record — the saver
// gets their own vault entry, but the business data and card images are
// stored once. Later saves only fill what's still empty (and missing images);
// the name, phone and GSTIN that created the record never change. When the
// person whose phone is on the card signs up, the business becomes theirs
// (see auth.ClaimCardBusinesses).

// DefaultCategoryID is the "Other" category every card-created business starts in.
var defaultCategoryID = uuid.MustParse("c0000000-0000-0000-0000-000000000001")

var looseGSTIN = regexp.MustCompile(`^[0-9]{2}[A-Z0-9]{13}$`)

// NormalizeGSTIN upper-cases and strips separators; "" if it can't be a GSTIN.
func NormalizeGSTIN(raw string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(raw) {
		if (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	g := b.String()
	if !looseGSTIN.MatchString(g) {
		return ""
	}
	return g
}

// cardBusiness is what a saved card contributes to its business record.
type cardBusiness struct {
	GSTIN, Name, ContactName, Designation, Phone, WhatsApp, Email, Website string
	Address, City, State, Pincode                                          string
}

func businessFromCard(card domain.SavedCard) cardBusiness {
	f := cardBusiness{
		GSTIN:       NormalizeGSTIN(card.GSTIN),
		ContactName: strings.TrimSpace(card.PersonName),
		Designation: strings.TrimSpace(card.Designation),
		Name:        strings.TrimSpace(card.Company),
		Address:     strings.TrimSpace(card.AddressLine),
		City:        strings.TrimSpace(card.City),
		State:       strings.TrimSpace(card.State),
		Pincode:     strings.TrimSpace(card.Pincode),
	}
	if f.Address == "" {
		f.Address = strings.TrimSpace(card.RawAddress)
	}
	if f.Name == "" {
		f.Name = f.ContactName
	}
	if card.Website != nil {
		f.Website = strings.TrimSpace(*card.Website)
	}
	if len(card.Emails) > 0 {
		f.Email = strings.TrimSpace(card.Emails[0])
	}
	for _, p := range card.Phones {
		raw := p.E164
		if raw == "" {
			raw = p.Raw
		}
		n, ok := validator.NormalizePhone(raw)
		if !ok {
			continue
		}
		if p.IsWhatsApp {
			if f.WhatsApp == "" {
				f.WhatsApp = n
			}
		} else if f.Phone == "" {
			f.Phone = n
		}
	}
	if f.Phone == "" {
		f.Phone = f.WhatsApp
	}
	if len(f.Pincode) != 6 {
		f.Pincode = ""
	}
	return f
}

var slugClean = regexp.MustCompile(`[^a-z0-9]+`)

func businessSlug(name string) string {
	s := strings.Trim(slugClean.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(s) > 60 {
		s = s[:60]
	}
	if s == "" {
		s = "business"
	}
	return s + "-" + uuid.New().String()[:6]
}

var (
	latLngOnce sync.Once
	latLngMode bool
)

// usesLatLng reports whether businesses has plain latitude/longitude columns
// (dev bootstrap) instead of a PostGIS location (full schema).
func (s *CardService) usesLatLng(ctx context.Context) bool {
	latLngOnce.Do(func() {
		_ = s.db.Pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM information_schema.columns
			WHERE table_schema = 'public' AND table_name = 'businesses' AND column_name = 'latitude')`).Scan(&latLngMode)
	})
	return latLngMode
}

// linkCardBusiness finds (or creates) the business for this card's GSTIN.
// Returns nil when the card has no usable GSTIN.
func (s *CardService) linkCardBusiness(ctx context.Context, userID uuid.UUID, card domain.SavedCard) (*uuid.UUID, error) {
	f := businessFromCard(card)
	if f.GSTIN == "" {
		return nil, nil
	}
	for attempt := 0; attempt < 2; attempt++ {
		var id uuid.UUID
		var owner *uuid.UUID
		err := s.db.Pool.QueryRow(ctx, `
			SELECT id, owner_user_id FROM businesses
			WHERE upper(gstin) = $1 AND deleted_at IS NULL
			ORDER BY (owner_user_id IS NULL), created_at LIMIT 1`, f.GSTIN).Scan(&id, &owner)
		if err == nil {
			if owner == nil {
				s.fillCardBusiness(ctx, id, f)
			}
			return &id, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return nil, err
		}
		id, err = s.createCardBusiness(ctx, userID, f)
		if err == nil {
			return &id, nil
		}
		var pgErr *pgconn.PgError
		if !(errors.As(err, &pgErr) && pgErr.Code == "23505") {
			return nil, err
		}
		// Someone else created it a moment ago — link to theirs.
	}
	return nil, fmt.Errorf("could not link business for GSTIN %s", f.GSTIN)
}

func (s *CardService) createCardBusiness(ctx context.Context, userID uuid.UUID, f cardBusiness) (uuid.UUID, error) {
	id := uuid.New()
	name := f.Name
	if name == "" {
		name = "Business " + f.GSTIN
	}
	coords := `location`
	coordVals := `ST_SetSRID(ST_MakePoint(76.9558, 11.0168), 4326)::geography`
	if s.usesLatLng(ctx) {
		coords = `latitude, longitude`
		coordVals = `11.0168, 76.9558`
	}
	_, err := s.db.Pool.Exec(ctx, `
		INSERT INTO businesses (
			id, owner_user_id, name, slug, primary_category_id, address_line1, city, state, pincode, country, `+coords+`,
			website, email, gstin, status, verification, listing, completeness,
			source, contact_name, contact_designation, contact_phone, created_by_user_id
		) VALUES (
			$1, NULL, $2, $3, $4, $5, $6, $7, $8, 'IN', `+coordVals+`,
			NULLIF($9, ''), NULLIF($10, ''), $11, 'draft', 'pending', 'unlisted', 30,
			'card', NULLIF($12, ''), NULLIF($13, ''), NULLIF($14, ''), $15
		)`, id, name, businessSlug(name), defaultCategoryID, f.Address, f.City, f.State, f.Pincode,
		f.Website, f.Email, f.GSTIN, f.ContactName, f.Designation, f.Phone, userID)
	if err != nil {
		return uuid.Nil, err
	}
	s.addBusinessPhones(ctx, id, f)
	slog.Info("Card business created", "business_id", id, "gstin", f.GSTIN, "by", userID)
	return id, nil
}

// fillCardBusiness fills fields that are still empty on an unclaimed business.
// Name, contact phone and GSTIN are never overwritten.
func (s *CardService) fillCardBusiness(ctx context.Context, id uuid.UUID, f cardBusiness) {
	_, err := s.db.Pool.Exec(ctx, `
		UPDATE businesses SET
			email = COALESCE(NULLIF(email, ''), NULLIF($2, '')),
			website = COALESCE(NULLIF(website, ''), NULLIF($3, '')),
			address_line1 = CASE WHEN COALESCE(address_line1, '') IN ('', 'Address pending') AND $4 <> '' THEN $4 ELSE address_line1 END,
			city = CASE WHEN COALESCE(city, '') = '' AND $5 <> '' THEN $5 ELSE city END,
			state = CASE WHEN COALESCE(state, '') = '' AND $6 <> '' THEN $6 ELSE state END,
			pincode = CASE WHEN COALESCE(pincode, '') IN ('', '000000') AND $7 <> '' THEN $7 ELSE pincode END,
			contact_name = COALESCE(NULLIF(contact_name, ''), NULLIF($8, '')),
			contact_designation = COALESCE(NULLIF(contact_designation, ''), NULLIF($9, '')),
			contact_phone = COALESCE(NULLIF(contact_phone, ''), NULLIF($10, '')),
			updated_at = now()
		WHERE id = $1 AND owner_user_id IS NULL AND (
			(COALESCE(email, '') = '' AND $2 <> '') OR (COALESCE(website, '') = '' AND $3 <> '') OR
			(COALESCE(address_line1, '') IN ('', 'Address pending') AND $4 <> '') OR (COALESCE(city, '') = '' AND $5 <> '') OR
			(COALESCE(state, '') = '' AND $6 <> '') OR (COALESCE(pincode, '') IN ('', '000000') AND $7 <> '') OR
			(COALESCE(contact_name, '') = '' AND $8 <> '') OR (COALESCE(contact_designation, '') = '' AND $9 <> '') OR
			(COALESCE(contact_phone, '') = '' AND $10 <> ''))`,
		id, f.Email, f.Website, f.Address, f.City, f.State, f.Pincode, f.ContactName, f.Designation, f.Phone)
	if err != nil {
		slog.Warn("Card business fill failed", "business_id", id, "error", err)
	}
	s.addBusinessPhones(ctx, id, f)
}

// addBusinessPhones adds the card's phone / WhatsApp if the business doesn't list them yet.
func (s *CardService) addBusinessPhones(ctx context.Context, id uuid.UUID, f cardBusiness) {
	for _, p := range []struct {
		phone string
		wa    bool
	}{{f.Phone, f.WhatsApp == "" || f.WhatsApp == f.Phone}, {f.WhatsApp, true}} {
		if p.phone == "" {
			continue
		}
		_, _ = s.db.Pool.Exec(ctx, `
			INSERT INTO business_phones (business_id, phone, is_whatsapp, otp_verified)
			SELECT $1, $2, $3, FALSE
			WHERE NOT EXISTS (SELECT 1 FROM business_phones WHERE business_id = $1 AND phone = $2)`, id, p.phone, p.wa)
	}
}

// attachBusinessImage stores a card image on the business when it doesn't
// have that side yet. Returns true if it was stored.
func (s *CardService) attachBusinessImage(ctx context.Context, businessID uuid.UUID, side string, data []byte, contentType string) (bool, error) {
	if len(data) == 0 {
		return false, nil
	}
	if contentType == "" {
		contentType = "image/jpeg"
	}
	tag, err := s.db.Pool.Exec(ctx, `
		INSERT INTO business_card_images (business_id, side, image_data, content_type)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (business_id, side) DO UPDATE SET image_data = EXCLUDED.image_data, content_type = EXCLUDED.content_type
		WHERE business_card_images.image_data IS NULL OR length(business_card_images.image_data) = 0`,
		businessID, normalizeSide(side), data, contentType)
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() > 0 {
		_, _ = s.db.Pool.Exec(ctx, `UPDATE businesses SET updated_at = now() WHERE id = $1`, businessID)
	}
	return tag.RowsAffected() > 0, nil
}

// SaveCardImage stores an uploaded card image. For a card linked to a
// business the image belongs to the business (stored once for everyone who
// saved it); it's only kept if the business doesn't have that side yet.
func (s *CardService) SaveCardImage(ctx context.Context, userID, cardID uuid.UUID, data []byte, contentType, side string) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	var linked *uuid.UUID
	_ = s.db.Pool.QueryRow(ctx, `SELECT linked_business_id FROM saved_cards WHERE id = $1 AND user_id = $2`, cardID, userID).Scan(&linked)
	if linked != nil {
		if _, err := s.attachBusinessImage(ctx, *linked, side, data, contentType); err != nil {
			return err
		}
		return nil
	}
	return s.persistOriginalImage(ctx, userID, cardID, data, contentType, side)
}

// savedByCount is how many people have this business in their vault.
func (s *CardService) savedByCount(ctx context.Context, businessID uuid.UUID) int {
	var n int
	_ = s.db.Pool.QueryRow(ctx, `
		SELECT count(DISTINCT user_id) FROM saved_cards WHERE linked_business_id = $1 AND deleted_at IS NULL`, businessID).Scan(&n)
	return n
}

// BackfillCardBusinesses links cards saved before migration 014 (GSTIN but
// no business) to their business, creating it when needed. Idempotent.
func (s *CardService) BackfillCardBusinesses(ctx context.Context) {
	if !s.dbReady() {
		return
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT id, user_id FROM saved_cards
		WHERE linked_business_id IS NULL AND deleted_at IS NULL AND COALESCE(gstin, '') <> ''
		ORDER BY created_at LIMIT 5000`)
	if err != nil {
		slog.Warn("Card business backfill skipped", "error", err)
		return
	}
	type pending struct{ card, user uuid.UUID }
	var list []pending
	for rows.Next() {
		var p pending
		if rows.Scan(&p.card, &p.user) == nil {
			list = append(list, p)
		}
	}
	rows.Close()
	linked := 0
	for _, p := range list {
		card, err := s.loadCardForLink(ctx, p.card)
		if err != nil {
			continue
		}
		bizID, err := s.linkCardBusiness(ctx, p.user, card)
		if err != nil || bizID == nil {
			continue
		}
		if _, err := s.db.Pool.Exec(ctx, `UPDATE saved_cards SET linked_business_id = $2 WHERE id = $1`, p.card, *bizID); err != nil {
			continue
		}
		// Share the card's images with the business if it has none (the
		// card's own copies are kept).
		for _, side := range []string{"front", "back"} {
			var data []byte
			var ct string
			if s.db.Pool.QueryRow(ctx, `
				SELECT image_data, COALESCE(NULLIF(content_type, ''), 'image/jpeg') FROM saved_card_images
				WHERE saved_card_id = $1 AND side = $2 AND image_data IS NOT NULL ORDER BY created_at DESC LIMIT 1`,
				p.card, side).Scan(&data, &ct) == nil {
				_, _ = s.attachBusinessImage(ctx, *bizID, side, data, ct)
			}
		}
		linked++
	}
	if linked > 0 {
		slog.Info("Card business backfill", "linked_cards", linked)
	}
}

// loadCardForLink reads the fields of one saved card that feed its business.
func (s *CardService) loadCardForLink(ctx context.Context, cardID uuid.UUID) (domain.SavedCard, error) {
	var c domain.SavedCard
	var website string
	err := s.db.Pool.QueryRow(ctx, `
		SELECT id, user_id, COALESCE(person_name, ''), COALESCE(designation, ''), COALESCE(company, ''),
		       COALESCE(website, ''), COALESCE(gstin, '')
		FROM saved_cards WHERE id = $1`, cardID).Scan(&c.ID, &c.UserID, &c.PersonName, &c.Designation, &c.Company, &website, &c.GSTIN)
	if err != nil {
		return c, err
	}
	if website != "" {
		c.Website = &website
	}
	rows, err := s.db.Pool.Query(ctx, `SELECT raw_phone, phone_e164, is_whatsapp FROM saved_card_phones WHERE saved_card_id = $1`, cardID)
	if err == nil {
		for rows.Next() {
			var p domain.CardPhone
			if rows.Scan(&p.Raw, &p.E164, &p.IsWhatsApp) == nil {
				c.Phones = append(c.Phones, p)
			}
		}
		rows.Close()
	}
	var email string
	if s.db.Pool.QueryRow(ctx, `SELECT email FROM saved_card_emails WHERE saved_card_id = $1 LIMIT 1`, cardID).Scan(&email) == nil {
		c.Emails = []string{email}
	}
	_ = s.db.Pool.QueryRow(ctx, `SELECT raw_address FROM saved_card_addresses WHERE saved_card_id = $1`, cardID).Scan(&c.RawAddress)
	return c, nil
}
