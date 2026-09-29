package card

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"cardflow-backend/internal/database"
	"cardflow-backend/internal/domain"
	"cardflow-backend/internal/extractor"
	"cardflow-backend/internal/storage"
	"github.com/google/uuid"
)

type CardService struct {
	db           *database.DB
	s3           *storage.S3Service
	extractor    *extractor.GeminiService
	vaultMutex   sync.RWMutex
	memoryVault  map[uuid.UUID][]domain.SavedCard
	imageMutex   sync.RWMutex
	memoryImages map[string][]byte
}

func NewCardService(db *database.DB, s3 *storage.S3Service, ext *extractor.GeminiService) *CardService {
	return &CardService{
		db:           db,
		s3:           s3,
		extractor:    ext,
		memoryVault:  make(map[uuid.UUID][]domain.SavedCard),
		memoryImages: make(map[string][]byte),
	}
}

func (s *CardService) dbReady() bool {
	return s.db != nil && s.db.Pool != nil
}

func (s *CardService) requireDB() error {
	if !s.dbReady() {
		return fmt.Errorf("server database not configured — admin must set DATABASE_URL on Render and redeploy")
	}
	return nil
}

func (s *CardService) GetSavedCards(ctx context.Context, userID uuid.UUID) ([]domain.SavedCard, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}

	cards := make([]domain.SavedCard, 0)

	rows, err := s.db.Pool.Query(ctx, `
			SELECT id, user_id, COALESCE(person_name, ''), COALESCE(designation, ''), COALESCE(company, ''),
			       COALESCE(website, ''), COALESCE(notes, ''), COALESCE(met_context, ''), COALESCE(private_rating, 5),
			       COALESCE(contact_type::text, 'business'), COALESCE(extract_status::text, 'extracted'),
			       COALESCE(gstin, ''), COALESCE(source, 'SCANNED'), linked_business_id, created_at, updated_at
			FROM saved_cards
			WHERE user_id = $1 AND deleted_at IS NULL
			ORDER BY created_at DESC
		`, userID)
	if err != nil {
		return nil, fmt.Errorf("load saved cards: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c domain.SavedCard
		var contactType, extractStatus, website, gstinVal, sourceVal string
		var rating int16
		var linkedBusinessID *uuid.UUID

		scanErr := rows.Scan(
			&c.ID, &c.UserID, &c.PersonName, &c.Designation, &c.Company,
			&website, &c.Notes, &c.MetContext, &rating, &contactType,
			&extractStatus, &gstinVal, &sourceVal, &linkedBusinessID, &c.CreatedAt, &c.UpdatedAt,
		)
		if scanErr == nil {
			if website != "" {
				c.Website = &website
			}
			c.GSTIN = gstinVal
			c.Source = sourceVal
			c.LinkedBusinessID = linkedBusinessID
			rInt := int(rating)
			c.PrivateRating = &rInt
			c.ContactType = contactType
			c.ExtractStatus = extractStatus

			// Fetch phones
			pRows, pErr := s.db.Pool.Query(ctx, `SELECT raw_phone, phone_e164, phone_type, is_whatsapp FROM saved_card_phones WHERE saved_card_id = $1`, c.ID)
			if pErr == nil {
				for pRows.Next() {
					var raw, e164, pType string
					var isWA bool
					if pRows.Scan(&raw, &e164, &pType, &isWA) == nil {
						c.Phones = append(c.Phones, domain.CardPhone{Raw: raw, E164: e164, Type: pType, IsWhatsApp: isWA})
					}
				}
				pRows.Close()
			}

			// Fetch emails
			eRows, eErr := s.db.Pool.Query(ctx, `SELECT email FROM saved_card_emails WHERE saved_card_id = $1`, c.ID)
			if eErr == nil {
				for eRows.Next() {
					var em string
					if eRows.Scan(&em) == nil {
						c.Emails = append(c.Emails, em)
					}
				}
				eRows.Close()
			}

			// Fetch address
			var rawAddr string
			_ = s.db.Pool.QueryRow(ctx, `SELECT raw_address FROM saved_card_addresses WHERE saved_card_id = $1`, c.ID).Scan(&rawAddr)
			c.RawAddress = rawAddr

			// Fetch tags
			tRows, tErr := s.db.Pool.Query(ctx, `
						SELECT t.name FROM tags t
						JOIN saved_card_tags sct ON sct.tag_id = t.id
						WHERE sct.saved_card_id = $1
					`, c.ID)
			if tErr == nil {
				for tRows.Next() {
					var tn string
					if tRows.Scan(&tn) == nil {
						c.Tags = append(c.Tags, tn)
					}
				}
				tRows.Close()
			}

			// Fetch original card image metadata
			var imgKey string
			var hasImageData bool
			_ = s.db.Pool.QueryRow(ctx, `
						SELECT object_key,
						       (image_data IS NOT NULL AND length(image_data) > 0)
						FROM saved_card_images
						WHERE saved_card_id = $1 AND side = 'front'
						ORDER BY created_at DESC LIMIT 1
					`, c.ID).Scan(&imgKey, &hasImageData)
			if imgKey != "" || hasImageData {
				c.OriginalCardImageURL = originalImageAPIPath(c.ID.String(), "front")
				if imgKey != "" {
					c.FrontImageKey = &imgKey
				}
			} else if linkedBusinessID != nil && s.businessHasImage(ctx, *linkedBusinessID, "front") {
				// No image of its own — this card was GSTIN-linked to an
				// existing business, so fall back to that business's image
				// instead of asking the user to upload a duplicate.
				c.OriginalCardImageURL = originalImageAPIPath(c.ID.String(), "front")
			}

			var backKey string
			var hasBack bool
			_ = s.db.Pool.QueryRow(ctx, `
						SELECT object_key,
						       (image_data IS NOT NULL AND length(image_data) > 0)
						FROM saved_card_images
						WHERE saved_card_id = $1 AND side = 'back'
						ORDER BY created_at DESC LIMIT 1
					`, c.ID).Scan(&backKey, &hasBack)
			if backKey != "" || hasBack {
				c.OriginalBackImageURL = originalImageAPIPath(c.ID.String(), "back")
				if backKey != "" {
					c.BackImageKey = &backKey
				}
			} else if linkedBusinessID != nil && s.businessHasImage(ctx, *linkedBusinessID, "back") {
				c.OriginalBackImageURL = originalImageAPIPath(c.ID.String(), "back")
			}

			if linkedBusinessID != nil {
				c.SavedCount = s.savedByCount(ctx, *linkedBusinessID)
			}

			// Parse GSTIN from notes if embedded
			if c.Notes != "" && len(c.Notes) > 7 && c.Notes[:7] == "__GST__" {
				parts := strings.SplitN(c.Notes, "\n", 2)
				c.GSTIN = strings.TrimPrefix(parts[0], "__GST__:")
				if len(parts) > 1 {
					c.Notes = parts[1]
				} else {
					c.Notes = ""
				}
			}

			cards = append(cards, c)
		}
	}

	return cards, nil
}

func (s *CardService) CreateSavedCard(ctx context.Context, userID uuid.UUID, card domain.SavedCard) (*domain.SavedCard, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}

	card.ID = uuid.New()
	card.UserID = userID
	card.CreatedAt = time.Now()
	card.UpdatedAt = time.Now()
	if card.ExtractStatus == "" {
		card.ExtractStatus = "extracted"
	}
	if card.ContactType == "" {
		card.ContactType = "business"
	}
	if card.Source == "" {
		card.Source = "SCANNED"
	}

	notesToStore := card.Notes
	if card.GSTIN != "" {
		notesToStore = "__GST__:" + card.GSTIN + "\n" + notesToStore
	}

	var pendingImage []byte
	var pendingContentType string
	if strings.HasPrefix(card.OriginalCardImageURL, "data:") {
		if raw, ct, err := decodeDataURL(card.OriginalCardImageURL); err == nil {
			pendingImage = raw
			pendingContentType = ct
		}
		card.OriginalCardImageURL = ""
	}

	// One business record per GSTIN: link to it (creating it from this card
	// the first time). The business keeps the canonical card images, so the
	// saver's copy is only used to fill a side the business doesn't have yet.
	if gst := NormalizeGSTIN(card.GSTIN); gst != "" {
		card.GSTIN = gst
		bizID, err := s.linkCardBusiness(ctx, userID, card)
		if err != nil {
			return nil, fmt.Errorf("link business: %w", err)
		}
		card.LinkedBusinessID = bizID
	}

	// Ensure user exists in users table to satisfy foreign key
	_, _ = s.db.Pool.Exec(ctx, `
			INSERT INTO users (id, phone, name, city, state, country, role, plan, status, free_scans_remaining)
			VALUES ($1, '+910000000000', 'CardFlow User', 'Coimbatore', 'Tamil Nadu', 'IN', 'user', 'free', 'active', 30)
			ON CONFLICT (id) DO NOTHING
		`, userID)

	_, err := s.db.Pool.Exec(ctx, `
			INSERT INTO saved_cards (
				id, user_id, person_name, designation, company, website,
				notes, met_context, contact_type, extract_status, gstin, source, linked_business_id
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'business', 'extracted', NULLIF($9, ''), $10, $11)
			ON CONFLICT (id) DO UPDATE SET
				person_name = EXCLUDED.person_name,
				company = EXCLUDED.company,
				designation = EXCLUDED.designation,
				website = EXCLUDED.website,
				notes = EXCLUDED.notes,
				gstin = EXCLUDED.gstin
		`, card.ID, userID, card.PersonName, card.Designation, card.Company, card.Website, notesToStore, card.MetContext, card.GSTIN, card.Source, card.LinkedBusinessID)
	if err != nil {
		return nil, fmt.Errorf("save card: %w", err)
	}

	// Insert phone numbers
	for _, p := range card.Phones {
		_, _ = s.db.Pool.Exec(ctx, `
				INSERT INTO saved_card_phones (id, saved_card_id, raw_phone, phone_e164, phone_type, is_whatsapp)
				VALUES ($1, $2, $3, $4, $5, $6)
			`, uuid.New(), card.ID, p.Raw, p.E164, p.Type, p.IsWhatsApp)
	}

	// Insert emails
	for _, em := range card.Emails {
		_, _ = s.db.Pool.Exec(ctx, `
				INSERT INTO saved_card_emails (id, saved_card_id, email)
				VALUES ($1, $2, $3)
			`, uuid.New(), card.ID, em)
	}

	// Insert address
	if card.RawAddress != "" {
		_, _ = s.db.Pool.Exec(ctx, `
				INSERT INTO saved_card_addresses (saved_card_id, raw_address)
				VALUES ($1, $2)
				ON CONFLICT (saved_card_id) DO UPDATE SET raw_address = EXCLUDED.raw_address
			`, card.ID, card.RawAddress)
	}

	// Insert tags
	for _, t := range card.Tags {
		tagID := uuid.New()
		_ = s.db.Pool.QueryRow(ctx, `
				INSERT INTO tags (id, user_id, name, kind) VALUES ($1, $2, $3, 'custom')
				ON CONFLICT (user_id, name) DO UPDATE SET name = EXCLUDED.name RETURNING id
			`, tagID, userID, t).Scan(&tagID)

		_, _ = s.db.Pool.Exec(ctx, `
				INSERT INTO saved_card_tags (saved_card_id, tag_id) VALUES ($1, $2)
				ON CONFLICT DO NOTHING
			`, card.ID, tagID)
	}

	if len(pendingImage) > 0 {
		if err := s.SaveCardImage(ctx, userID, card.ID, pendingImage, pendingContentType, "front"); err != nil {
			return nil, err
		}
	}
	if card.LinkedBusinessID != nil {
		card.SavedCount = s.savedByCount(ctx, *card.LinkedBusinessID)
	}

	if s.hasOriginalImage(card.ID, "front") {
		card.OriginalCardImageURL = originalImageAPIPath(card.ID.String(), "front")
	} else if card.LinkedBusinessID != nil && s.businessHasImage(ctx, *card.LinkedBusinessID, "front") {
		card.OriginalCardImageURL = originalImageAPIPath(card.ID.String(), "front")
	}

	return &card, nil
}

func (s *CardService) hasOriginalImage(cardID uuid.UUID, side string) bool {
	side = normalizeSide(side)
	s.imageMutex.RLock()
	_, ok := s.memoryImages[imageMemKey(cardID, side)]
	s.imageMutex.RUnlock()
	if ok {
		return true
	}
	if s.db != nil && s.db.Pool != nil {
		var exists bool
		_ = s.db.Pool.QueryRow(context.Background(), `
			SELECT EXISTS(
				SELECT 1 FROM saved_card_images
				WHERE saved_card_id = $1 AND side = $2
				  AND (
				    (image_data IS NOT NULL AND length(image_data) > 0)
				    OR object_key <> ''
				  )
			)
		`, cardID, side).Scan(&exists)
		if exists {
			return true
		}
	}
	return false
}

func (s *CardService) persistOriginalImage(ctx context.Context, userID, cardID uuid.UUID, data []byte, contentType, side string) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	if len(data) == 0 {
		return fmt.Errorf("empty image")
	}
	if contentType == "" {
		contentType = "image/jpeg"
	}
	side = normalizeSide(side)

	objectKey := imageObjectKey(userID.String(), cardID.String(), side)

	s.imageMutex.Lock()
	s.memoryImages[imageMemKey(cardID, side)] = append([]byte(nil), data...)
	s.imageMutex.Unlock()

	localPath := s.localImagePath(userID.String(), cardID.String(), side)
	if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err == nil {
		_ = os.WriteFile(localPath, data, 0o644)
	}

	if s.db != nil && s.db.Pool != nil {
		_, _ = s.db.Pool.Exec(ctx, `
			DELETE FROM saved_card_images WHERE saved_card_id = $1 AND side = $2
		`, cardID, side)
		_, err := s.db.Pool.Exec(ctx, `
			INSERT INTO saved_card_images (id, saved_card_id, side, object_key, bytes, image_data, content_type)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`, uuid.New(), cardID, side, objectKey, len(data), data, contentType)
		if err != nil {
			_, err2 := s.db.Pool.Exec(ctx, `
				INSERT INTO saved_card_images (id, saved_card_id, side, object_key, bytes)
				VALUES ($1, $2, $3, $4, $5)
			`, uuid.New(), cardID, side, objectKey, len(data))
			if err2 != nil {
				return fmt.Errorf("save image to database: %w", err)
			}
		}
	}

	if s.s3 != nil {
		if err := s.s3.PutObject(ctx, objectKey, data, contentType); err != nil {
			slog.Warn("S3 put skipped; image already stored in database", "error", err)
		}
	}
	return nil
}

func (s *CardService) GetOriginalImage(ctx context.Context, userID, cardID uuid.UUID, side string) ([]byte, string, error) {
	side = normalizeSide(side)
	s.imageMutex.RLock()
	if data, ok := s.memoryImages[imageMemKey(cardID, side)]; ok {
		s.imageMutex.RUnlock()
		return data, "image/jpeg", nil
	}
	s.imageMutex.RUnlock()

	if s.db != nil && s.db.Pool != nil {
		var imageData []byte
		var contentType string
		var objectKey string
		err := s.db.Pool.QueryRow(ctx, `
			SELECT image_data, COALESCE(NULLIF(content_type, ''), 'image/jpeg'), object_key
			FROM saved_card_images
			WHERE saved_card_id = $1 AND side = $2
			ORDER BY created_at DESC LIMIT 1
		`, cardID, side).Scan(&imageData, &contentType, &objectKey)
		if err == nil && len(imageData) > 0 {
			if contentType == "" {
				contentType = "image/jpeg"
			}
			return imageData, contentType, nil
		}
		if err == nil && objectKey != "" && s.s3 != nil {
			if bytes, ct, s3err := s.s3.GetObject(ctx, objectKey); s3err == nil && len(bytes) > 0 {
				return bytes, ct, nil
			}
		}
	}

	localPath := s.localImagePath(userID.String(), cardID.String(), side)
	if data, err := os.ReadFile(localPath); err == nil && len(data) > 0 {
		return data, "image/jpeg", nil
	}

	// This card has no image of its own — if it was GSTIN-linked to an
	// existing registered business, serve that business's card image
	// instead of storing (and re-storing, per saver) a duplicate copy.
	if s.dbReady() {
		var linkedBusinessID *uuid.UUID
		_ = s.db.Pool.QueryRow(ctx, `SELECT linked_business_id FROM saved_cards WHERE id = $1`, cardID).Scan(&linkedBusinessID)
		if linkedBusinessID != nil {
			var imageData []byte
			var contentType string
			err := s.db.Pool.QueryRow(ctx, `
				SELECT image_data, COALESCE(NULLIF(content_type, ''), 'image/jpeg')
				FROM business_card_images
				WHERE business_id = $1 AND side = $2
			`, *linkedBusinessID, side).Scan(&imageData, &contentType)
			if err == nil && len(imageData) > 0 {
				return imageData, contentType, nil
			}
		}
	}

	return nil, "", fmt.Errorf("image not found")
}

// businessHasImage reports whether a registered business has a stored card
// image for the given side, used to decide whether a GSTIN-linked saved
// card can fall back to it instead of its own (absent) image.
func (s *CardService) businessHasImage(ctx context.Context, businessID uuid.UUID, side string) bool {
	if !s.dbReady() {
		return false
	}
	var exists bool
	_ = s.db.Pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM business_card_images
			WHERE business_id = $1 AND side = $2 AND image_data IS NOT NULL AND length(image_data) > 0
		)
	`, businessID, side).Scan(&exists)
	return exists
}

func (s *CardService) UpdateSavedCard(ctx context.Context, userID, cardID uuid.UUID, patch domain.SavedCard) (*domain.SavedCard, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}
	if !s.CardBelongsToUser(ctx, userID, cardID) {
		return nil, fmt.Errorf("card not found")
	}

	notesToStore := patch.Notes
	if patch.GSTIN != "" {
		notesToStore = "__GST__:" + patch.GSTIN + "\n" + notesToStore
	}
	_, err := s.db.Pool.Exec(ctx, `
			UPDATE saved_cards SET
				person_name = COALESCE(NULLIF($2, ''), person_name),
				designation = $3,
				company = COALESCE(NULLIF($4, ''), company),
				website = $5,
				notes = $6,
				gstin = NULLIF($7, ''),
				updated_at = NOW()
			WHERE id = $1 AND user_id = $8 AND deleted_at IS NULL
		`, cardID, patch.PersonName, patch.Designation, patch.Company, patch.Website, notesToStore, patch.GSTIN, userID)
	if err != nil {
		return nil, err
	}
	if patch.RawAddress != "" {
		_, _ = s.db.Pool.Exec(ctx, `
				INSERT INTO saved_card_addresses (saved_card_id, raw_address)
				VALUES ($1, $2)
				ON CONFLICT (saved_card_id) DO UPDATE SET raw_address = EXCLUDED.raw_address
			`, cardID, patch.RawAddress)
	}
	if len(patch.Phones) > 0 {
		_, _ = s.db.Pool.Exec(ctx, `DELETE FROM saved_card_phones WHERE saved_card_id = $1`, cardID)
		for _, p := range patch.Phones {
			_, _ = s.db.Pool.Exec(ctx, `
					INSERT INTO saved_card_phones (id, saved_card_id, raw_phone, phone_e164, phone_type, is_whatsapp)
					VALUES ($1, $2, $3, $4, $5, $6)
				`, uuid.New(), cardID, p.Raw, p.E164, p.Type, p.IsWhatsApp)
		}
	}
	if len(patch.Emails) > 0 {
		_, _ = s.db.Pool.Exec(ctx, `DELETE FROM saved_card_emails WHERE saved_card_id = $1`, cardID)
		for _, em := range patch.Emails {
			_, _ = s.db.Pool.Exec(ctx, `
					INSERT INTO saved_card_emails (id, saved_card_id, email) VALUES ($1, $2, $3)
				`, uuid.New(), cardID, em)
		}
	}

	cards, err := s.GetSavedCards(ctx, userID)
	if err != nil {
		return nil, err
	}
	for i := range cards {
		if cards[i].ID == cardID {
			return &cards[i], nil
		}
	}
	patch.ID = cardID
	patch.UserID = userID
	return &patch, nil
}

func (s *CardService) CardBelongsToUser(ctx context.Context, userID, cardID uuid.UUID) bool {
	if !s.dbReady() {
		return false
	}
	var owner uuid.UUID
	err := s.db.Pool.QueryRow(ctx, `
			SELECT user_id FROM saved_cards WHERE id = $1 AND deleted_at IS NULL
		`, cardID).Scan(&owner)
	return err == nil && owner == userID
}

// DeleteSavedCard soft-deletes a card the caller owns — used both for
// removing a scanned card and for "unsaving" a business from the vault.
func (s *CardService) DeleteSavedCard(ctx context.Context, userID, cardID uuid.UUID) error {
	if err := s.requireDB(); err != nil {
		return err
	}
	tag, err := s.db.Pool.Exec(ctx, `
			UPDATE saved_cards SET deleted_at = NOW(), updated_at = NOW()
			WHERE id = $1 AND user_id = $2 AND deleted_at IS NULL
		`, cardID, userID)
	if err != nil {
		return fmt.Errorf("delete card: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("card not found")
	}
	return nil
}

// GetPublicCard loads a single saved card for the unauthenticated share-link
// view — no user_id check, since the whole point is that someone without an
// account (or a different account) can open the link. Only the fields safe
// to show a stranger are populated.
func (s *CardService) GetPublicCard(ctx context.Context, cardID uuid.UUID) (*domain.SavedCard, error) {
	if err := s.requireDB(); err != nil {
		return nil, err
	}

	var c domain.SavedCard
	var website, gstinVal string
	var linkedBusinessID *uuid.UUID
	err := s.db.Pool.QueryRow(ctx, `
			SELECT id, COALESCE(person_name, ''), COALESCE(designation, ''), COALESCE(company, ''),
			       COALESCE(website, ''), COALESCE(gstin, ''), linked_business_id, created_at
			FROM saved_cards
			WHERE id = $1 AND deleted_at IS NULL
		`, cardID).Scan(&c.ID, &c.PersonName, &c.Designation, &c.Company, &website, &gstinVal, &linkedBusinessID, &c.CreatedAt)
	if err != nil {
		return nil, fmt.Errorf("card not found")
	}
	if website != "" {
		c.Website = &website
	}
	c.GSTIN = gstinVal
	c.LinkedBusinessID = linkedBusinessID

	pRows, pErr := s.db.Pool.Query(ctx, `SELECT raw_phone, phone_e164, phone_type, is_whatsapp FROM saved_card_phones WHERE saved_card_id = $1`, c.ID)
	if pErr == nil {
		for pRows.Next() {
			var raw, e164, pType string
			var isWA bool
			if pRows.Scan(&raw, &e164, &pType, &isWA) == nil {
				c.Phones = append(c.Phones, domain.CardPhone{Raw: raw, E164: e164, Type: pType, IsWhatsApp: isWA})
			}
		}
		pRows.Close()
	}

	eRows, eErr := s.db.Pool.Query(ctx, `SELECT email FROM saved_card_emails WHERE saved_card_id = $1`, c.ID)
	if eErr == nil {
		for eRows.Next() {
			var em string
			if eRows.Scan(&em) == nil {
				c.Emails = append(c.Emails, em)
			}
		}
		eRows.Close()
	}

	var rawAddr string
	_ = s.db.Pool.QueryRow(ctx, `SELECT raw_address FROM saved_card_addresses WHERE saved_card_id = $1`, c.ID).Scan(&rawAddr)
	c.RawAddress = rawAddr

	var imgKey string
	var hasImageData bool
	_ = s.db.Pool.QueryRow(ctx, `
			SELECT object_key, (image_data IS NOT NULL AND length(image_data) > 0)
			FROM saved_card_images WHERE saved_card_id = $1 AND side = 'front' ORDER BY created_at DESC LIMIT 1
		`, c.ID).Scan(&imgKey, &hasImageData)
	if imgKey != "" || hasImageData {
		c.OriginalCardImageURL = originalImageAPIPath(c.ID.String(), "front")
	} else if linkedBusinessID != nil && s.businessHasImage(ctx, *linkedBusinessID, "front") {
		c.OriginalCardImageURL = originalImageAPIPath(c.ID.String(), "front")
	}

	var backKey string
	var hasBack bool
	_ = s.db.Pool.QueryRow(ctx, `
			SELECT object_key, (image_data IS NOT NULL AND length(image_data) > 0)
			FROM saved_card_images WHERE saved_card_id = $1 AND side = 'back' ORDER BY created_at DESC LIMIT 1
		`, c.ID).Scan(&backKey, &hasBack)
	if backKey != "" || hasBack {
		c.OriginalBackImageURL = originalImageAPIPath(c.ID.String(), "back")
	} else if linkedBusinessID != nil && s.businessHasImage(ctx, *linkedBusinessID, "back") {
		c.OriginalBackImageURL = originalImageAPIPath(c.ID.String(), "back")
	}

	if c.Notes != "" && len(c.Notes) > 7 && c.Notes[:7] == "__GST__" {
		parts := strings.SplitN(c.Notes, "\n", 2)
		c.GSTIN = strings.TrimPrefix(parts[0], "__GST__:")
	}

	return &c, nil
}

func (s *CardService) ProcessOCR(ctx context.Context, userID uuid.UUID, imageKey string) (*extractor.ExtractedCardData, error) {
	return s.extractor.ExtractCardFromImage(ctx, imageKey)
}
