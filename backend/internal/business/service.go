package business

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"

	"cardflow-backend/internal/database"
	"cardflow-backend/internal/domain"
	"github.com/google/uuid"
)

type BusinessService struct {
	db         *database.DB
	latLngOnce sync.Once
	latLngMode bool
}

func NewBusinessService(db *database.DB) *BusinessService {
	return &BusinessService{db: db}
}

func (s *BusinessService) usesLatLngColumns(ctx context.Context) bool {
	s.latLngOnce.Do(func() {
		if s.db == nil || s.db.Pool == nil {
			return
		}
		var ok bool
		_ = s.db.Pool.QueryRow(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.columns
				WHERE table_schema = 'public' AND table_name = 'businesses' AND column_name = 'latitude'
			)
		`).Scan(&ok)
		s.latLngMode = ok
	})
	return s.latLngMode
}

func (s *BusinessService) resolveCategoryID(ctx context.Context, raw string) uuid.UUID {
	if id, err := uuid.Parse(raw); err == nil {
		return id
	}
	if s.db != nil && s.db.Pool != nil {
		var id uuid.UUID
		slug := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(raw), " ", "-"))
		err := s.db.Pool.QueryRow(ctx, `
			SELECT id FROM categories
			WHERE LOWER(name) = LOWER($1) OR LOWER(slug) = LOWER($2)
			LIMIT 1
		`, raw, slug).Scan(&id)
		if err == nil {
			return id
		}
	}
	return uuid.MustParse("c0000000-0000-0000-0000-000000000001")
}

func (s *BusinessService) businessCoordSelectSQL(ctx context.Context) string {
	if s.usesLatLngColumns(ctx) {
		return "b.latitude as lat, b.longitude as lng"
	}
	return "ST_Y(b.location::geometry) as lat, ST_X(b.location::geometry) as lng"
}

func (s *BusinessService) getBusinessServices(ctx context.Context, bizID uuid.UUID) []string {
	if s.db == nil || s.db.Pool == nil {
		return nil
	}
	rows, err := s.db.Pool.Query(ctx, `SELECT name FROM business_services WHERE business_id = $1 ORDER BY created_at ASC`, bizID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var services []string
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err == nil {
			services = append(services, name)
		}
	}
	return services
}

// getBusinessPhoneInfo returns the primary phone, the WhatsApp number (if any),
// and the full list of numbers on file for a business.
func (s *BusinessService) getBusinessPhoneInfo(ctx context.Context, bizID uuid.UUID) (phone *string, whatsapp *string, phones []string) {
	if s.db == nil || s.db.Pool == nil {
		return nil, nil, nil
	}
	rows, err := s.db.Pool.Query(ctx, `
		SELECT phone, is_whatsapp FROM business_phones
		WHERE business_id = $1 ORDER BY created_at ASC
	`, bizID)
	if err != nil {
		return nil, nil, nil
	}
	defer rows.Close()

	for rows.Next() {
		var p string
		var isWhatsApp bool
		if err := rows.Scan(&p, &isWhatsApp); err != nil {
			continue
		}
		phones = append(phones, p)
		if phone == nil {
			phoneCopy := p
			phone = &phoneCopy
		}
		if isWhatsApp && whatsapp == nil {
			waCopy := p
			whatsapp = &waCopy
		}
	}
	return phone, whatsapp, phones
}

// syncBusinessServices replaces the stored service/product tags for a business.
func (s *BusinessService) syncBusinessServices(ctx context.Context, bizID uuid.UUID, services []string) {
	if s.db == nil || s.db.Pool == nil {
		return
	}
	_, _ = s.db.Pool.Exec(ctx, `DELETE FROM business_services WHERE business_id = $1`, bizID)
	for _, svc := range services {
		if strings.TrimSpace(svc) == "" {
			continue
		}
		_, _ = s.db.Pool.Exec(ctx, `
			INSERT INTO business_services (id, business_id, name) VALUES ($1, $2, $3)
		`, uuid.New(), bizID, svc)
	}
}

type CreateBusinessInput struct {
	Name            string   `json:"name"`
	Description     string   `json:"description"`
	CategoryID      string   `json:"category_id"`
	AddressLine1    string   `json:"address_line1"`
	Locality        string   `json:"locality"`
	City            string   `json:"city"`
	State           string   `json:"state"`
	Pincode         string   `json:"pincode"`
	Latitude        float64  `json:"latitude"`
	Longitude       float64  `json:"longitude"`
	Phone           string   `json:"phone"`
	WhatsApp        string   `json:"whatsapp"`
	Email           string   `json:"email"`
	Website         string   `json:"website"`
	GSTIN           string   `json:"gstin"`
	Services        []string `json:"services"`
	YearEstablished int      `json:"year_established"`
	FrontImageData  string   `json:"front_image_data"`
	BackImageData   string   `json:"back_image_data"`
}

func (s *BusinessService) GetOwnerBusinesses(ctx context.Context, ownerUserID uuid.UUID) ([]domain.Business, error) {
	if s.db == nil || s.db.Pool == nil {
		// Fallback for DEV mode
		return []domain.Business{
			{
				ID:              uuid.MustParse("00000000-0000-0000-0000-0000000000b1"),
				OwnerUserID:     ownerUserID,
				Name:            "Kovai Precision Tools",
				Slug:            "kovai-precision-tools",
				Description:     "Leading manufacturers of CNC machined precision components.",
				PrimaryCategory: "Manufacturing",
				AddressLine1:    "42, SIDCO Industrial Estate",
				City:            "Coimbatore",
				State:           "Tamil Nadu",
				Pincode:         "641004",
				Status:          "live",
				Verification:    "gst",
				Listing:         "listed",
				ViewsCount:      248,
				EnquiriesCount:  14,
				Services:        []string{"CNC Milling", "Hydraulic Valves"},
				Phones:          []string{"+919443012345"},
			},
			{
				ID:              uuid.MustParse("00000000-0000-0000-0000-0000000000b2"),
				OwnerUserID:     ownerUserID,
				Name:            "Apex Infotech Solutions",
				Slug:            "apex-infotech-solutions",
				Description:     "Enterprise ERP, Cloud Migration, and Custom Apps.",
				PrimaryCategory: "IT & Software",
				AddressLine1:    "105, Cross Cut Road",
				City:            "Coimbatore",
				State:           "Tamil Nadu",
				Pincode:         "641018",
				Status:          "live",
				Verification:    "gst",
				Listing:         "listed",
				ViewsCount:      512,
				EnquiriesCount:  28,
				Services:        []string{"Cloud ERP", "React Native Apps"},
				Phones:          []string{"+919842155678"},
			},
		}, nil
	}

	rows, err := s.db.Pool.Query(ctx, `
		SELECT b.id, b.owner_user_id, b.name, b.slug, COALESCE(b.description, ''), b.primary_category_id,
		       COALESCE(c.name, ''), b.address_line1, b.city, b.state, b.pincode,
		       b.website, b.email, b.gstin,
		       `+s.businessCoordSelectSQL(ctx)+`,
		       b.status::text, b.verification::text, b.listing::text, b.completeness, b.created_at, b.updated_at,
		       (SELECT count(DISTINCT sc.user_id) FROM saved_cards sc WHERE sc.linked_business_id = b.id AND sc.deleted_at IS NULL),
		       COALESCE(b.source, 'owner')
		FROM businesses b
		LEFT JOIN categories c ON c.id = b.primary_category_id
		WHERE b.owner_user_id = $1 AND b.status != 'removed'
		ORDER BY b.created_at DESC
	`, ownerUserID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var businesses []domain.Business
	for rows.Next() {
		var b domain.Business
		var catName, status, verif, list string
		var website, email, gstin *string
		err := rows.Scan(
			&b.ID, &b.OwnerUserID, &b.Name, &b.Slug, &b.Description, &b.PrimaryCategoryID,
			&catName, &b.AddressLine1, &b.City, &b.State, &b.Pincode,
			&website, &email, &gstin,
			&b.Latitude, &b.Longitude,
			&status, &verif, &list, &b.Completeness, &b.CreatedAt, &b.UpdatedAt,
			&b.SavedCount, &b.Source,
		)
		if err == nil {
			b.PrimaryCategory = catName
			b.Status = status
			b.Verification = verif
			b.Listing = list
			b.Website = website
			b.Email = email
			b.GSTIN = gstin
			b.Services = s.getBusinessServices(ctx, b.ID)
			b.Phone, b.WhatsApp, b.Phones = s.getBusinessPhoneInfo(ctx, b.ID)
			b.CardImageURL = "/api/v1/owner/businesses/" + b.ID.String() + "/card-image?side=front"
			b.CardBackImageURL = "/api/v1/owner/businesses/" + b.ID.String() + "/card-image?side=back"
			b.ViewsCount = 120
			b.EnquiriesCount = 8
			businesses = append(businesses, b)
		}
	}

	return businesses, nil
}

func (s *BusinessService) CreateBusiness(ctx context.Context, ownerUserID uuid.UUID, in CreateBusinessInput) (*domain.Business, error) {
	// A business created earlier from someone's scanned card (same GSTIN, no
	// owner yet) becomes this owner's instead of a second record for it.
	if gst := strings.ToUpper(strings.TrimSpace(in.GSTIN)); gst != "" && s.db != nil && s.db.Pool != nil {
		var existing uuid.UUID
		err := s.db.Pool.QueryRow(ctx, `
			UPDATE businesses SET owner_user_id = $2, claimed_at = now(), status = 'live', listing = 'listed',
			       primary_category_id = $3, updated_at = now()
			WHERE upper(gstin) = $1 AND owner_user_id IS NULL AND deleted_at IS NULL
			RETURNING id`, gst, ownerUserID, s.resolveCategoryID(ctx, in.CategoryID)).Scan(&existing)
		if err == nil {
			return s.UpdateBusiness(ctx, ownerUserID, existing, in)
		}
	}
	slug := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(in.Name), " ", "-")) + "-" + uuid.New().String()[:4]
	newID := uuid.New()

	catUUID := s.resolveCategoryID(ctx, in.CategoryID)

	if in.Latitude == 0 && in.Longitude == 0 {
		in.Latitude = 11.0168
		in.Longitude = 76.9558
	}

	if in.Pincode == "" {
		in.Pincode = "000000"
	}
	if in.City == "" {
		in.City = "Coimbatore"
	}
	if in.State == "" {
		in.State = "Tamil Nadu"
	}
	if in.AddressLine1 == "" {
		in.AddressLine1 = "Address pending"
	}

	if s.db != nil && s.db.Pool != nil {
		_, _ = s.db.Pool.Exec(ctx, `
			INSERT INTO users (id, phone, name, city, state, country, role, plan, status, free_scans_remaining)
			VALUES ($1, '+910000000000', 'CardFlow User', 'Coimbatore', 'Tamil Nadu', 'IN', 'user', 'free', 'active', 30)
			ON CONFLICT (id) DO NOTHING
		`, ownerUserID)

		var err error
		if s.usesLatLngColumns(ctx) {
			_, err = s.db.Pool.Exec(ctx, `
				INSERT INTO businesses (
					id, owner_user_id, name, slug, description, primary_category_id,
					address_line1, locality, city, state, pincode, country,
					latitude, longitude, website, email, gstin, status, verification, listing, completeness
				) VALUES (
					$1, $2, $3, $4, $5, $6,
					$7, $8, $9, $10, $11, 'IN',
					$12, $13, $14, $15, $16, 'live', 'pending', 'listed', 80
				)
			`, newID, ownerUserID, in.Name, slug, in.Description, catUUID,
				in.AddressLine1, in.Locality, in.City, in.State, in.Pincode,
				in.Latitude, in.Longitude,
				in.Website, in.Email, gstinOrNull(in.GSTIN),
			)
		} else {
			_, err = s.db.Pool.Exec(ctx, `
				INSERT INTO businesses (
					id, owner_user_id, name, slug, description, primary_category_id,
					address_line1, locality, city, state, pincode, country,
					location, website, email, gstin, status, verification, listing, completeness
				) VALUES (
					$1, $2, $3, $4, $5, $6,
					$7, $8, $9, $10, $11, 'IN',
					ST_SetSRID(ST_MakePoint($12, $13), 4326)::geography,
					$14, $15, $16, 'live', 'pending', 'listed', 80
				)
			`, newID, ownerUserID, in.Name, slug, in.Description, catUUID,
				in.AddressLine1, in.Locality, in.City, in.State, in.Pincode,
				in.Longitude, in.Latitude,
				in.Website, in.Email, gstinOrNull(in.GSTIN),
			)
		}
		if err != nil {
			return nil, err
		}

		// Insert digital card
		_, _ = s.db.Pool.Exec(ctx, `
			INSERT INTO digital_cards (business_id, template, brand_color, qr_slug)
			VALUES ($1, 'modern', '#32145F', $2)
			ON CONFLICT (business_id) DO NOTHING
		`, newID, slug)

		// Insert phone
		if in.Phone != "" {
			_, _ = s.db.Pool.Exec(ctx, `
				INSERT INTO business_phones (business_id, phone, is_whatsapp, otp_verified)
				VALUES ($1, $2, TRUE, TRUE)
			`, newID, in.Phone)
		}
		if in.WhatsApp != "" && in.WhatsApp != in.Phone {
			_, _ = s.db.Pool.Exec(ctx, `
				INSERT INTO business_phones (business_id, phone, is_whatsapp, otp_verified)
				VALUES ($1, $2, TRUE, FALSE)
			`, newID, in.WhatsApp)
		}
		for _, svc := range in.Services {
			if strings.TrimSpace(svc) == "" {
				continue
			}
			_, _ = s.db.Pool.Exec(ctx, `
				INSERT INTO business_services (id, business_id, name) VALUES ($1, $2, $3)
			`, uuid.New(), newID, svc)
		}
		if in.FrontImageData != "" {
			if err := s.persistCardImage(ctx, newID, "front", in.FrontImageData); err != nil {
				return nil, fmt.Errorf("save front card image: %w", err)
			}
		}
		if in.BackImageData != "" {
			if err := s.persistCardImage(ctx, newID, "back", in.BackImageData); err != nil {
				return nil, fmt.Errorf("save back card image: %w", err)
			}
		}
	}

	biz := &domain.Business{
		ID:                newID,
		OwnerUserID:       ownerUserID,
		Name:              in.Name,
		Slug:              slug,
		Description:       in.Description,
		PrimaryCategoryID: catUUID,
		AddressLine1:      in.AddressLine1,
		City:              in.City,
		State:             in.State,
		Pincode:           in.Pincode,
		Status:            "live",
		Verification:      "pending",
		Listing:           "listed",
		Completeness:      80,
		Services:          in.Services,
	}
	if in.Website != "" {
		biz.Website = &in.Website
	}
	if in.Email != "" {
		biz.Email = &in.Email
	}
	if in.GSTIN != "" {
		biz.GSTIN = &in.GSTIN
	}
	if in.Phone != "" {
		biz.Phone = &in.Phone
		biz.Phones = append(biz.Phones, in.Phone)
	}
	if in.WhatsApp != "" {
		biz.WhatsApp = &in.WhatsApp
		if in.WhatsApp != in.Phone {
			biz.Phones = append(biz.Phones, in.WhatsApp)
		}
	}
	if in.FrontImageData != "" {
		biz.CardImageURL = "/api/v1/owner/businesses/" + newID.String() + "/card-image?side=front"
	}
	if in.BackImageData != "" {
		biz.CardBackImageURL = "/api/v1/owner/businesses/" + newID.String() + "/card-image?side=back"
	}
	return biz, nil
}

func decodeBizDataURL(dataURL string) ([]byte, string, error) {
	if dataURL == "" {
		return nil, "", errors.New("empty")
	}
	payload := dataURL
	contentType := "image/jpeg"
	if strings.HasPrefix(dataURL, "data:") {
		parts := strings.SplitN(dataURL, ",", 2)
		if len(parts) != 2 {
			return nil, "", errors.New("invalid data url")
		}
		if strings.Contains(parts[0], "image/png") {
			contentType = "image/png"
		}
		payload = parts[1]
	}
	raw, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		return nil, "", err
	}
	return raw, contentType, nil
}

func (s *BusinessService) persistCardImage(ctx context.Context, businessID uuid.UUID, side, dataURL string) error {
	if dataURL == "" || s.db == nil || s.db.Pool == nil {
		return nil
	}
	raw, contentType, err := decodeBizDataURL(dataURL)
	if err != nil || len(raw) == 0 {
		return err
	}
	_, _ = s.db.Pool.Exec(ctx, `DELETE FROM business_card_images WHERE business_id = $1 AND side = $2`, businessID, side)
	_, err = s.db.Pool.Exec(ctx, `
		INSERT INTO business_card_images (id, business_id, side, image_data, content_type)
		VALUES ($1, $2, $3, $4, $5)
	`, uuid.New(), businessID, side, raw, contentType)
	return err
}

func (s *BusinessService) GetCardImage(ctx context.Context, userID, businessID uuid.UUID, side string) ([]byte, string, error) {
	if s.db == nil || s.db.Pool == nil {
		return nil, "", errors.New("image not found")
	}
	var owner uuid.UUID
	var listing string
	err := s.db.Pool.QueryRow(ctx, `SELECT owner_user_id, listing::text FROM businesses WHERE id = $1 AND deleted_at IS NULL`, businessID).Scan(&owner, &listing)
	if err != nil {
		return nil, "", errors.New("image not found")
	}
	if owner != userID && listing != "listed" {
		return nil, "", errors.New("image not found")
	}
	if side != "back" {
		side = "front"
	}
	var data []byte
	var ct string
	err = s.db.Pool.QueryRow(ctx, `
		SELECT image_data, COALESCE(NULLIF(content_type, ''), 'image/jpeg')
		FROM business_card_images WHERE business_id = $1 AND side = $2
		ORDER BY created_at DESC LIMIT 1
	`, businessID, side).Scan(&data, &ct)
	if err != nil || len(data) == 0 {
		return nil, "", errors.New("image not found")
	}
	return data, ct, nil
}

func (s *BusinessService) VerifyOwnerAccess(ctx context.Context, ownerUserID, businessID uuid.UUID) error {
	if s.db == nil || s.db.Pool == nil {
		return nil
	}

	var count int
	err := s.db.Pool.QueryRow(ctx, `
		SELECT COUNT(1) FROM businesses WHERE id = $1 AND owner_user_id = $2
	`, businessID, ownerUserID).Scan(&count)
	if err != nil || count == 0 {
		return errors.New("access denied: you do not own this business")
	}
	return nil
}

// UpdateBusiness lets the owner edit listing fields and optionally replace card images.
func (s *BusinessService) UpdateBusiness(ctx context.Context, ownerUserID, businessID uuid.UUID, in CreateBusinessInput) (*domain.Business, error) {
	if err := s.VerifyOwnerAccess(ctx, ownerUserID, businessID); err != nil {
		return nil, err
	}

	if s.db != nil && s.db.Pool != nil {
		_, err := s.db.Pool.Exec(ctx, `
			UPDATE businesses SET
				name = COALESCE(NULLIF($2, ''), name),
				description = COALESCE(NULLIF($3, ''), description),
				address_line1 = COALESCE(NULLIF($4, ''), address_line1),
				locality = COALESCE(NULLIF($5, ''), locality),
				city = COALESCE(NULLIF($6, ''), city),
				state = COALESCE(NULLIF($7, ''), state),
				pincode = COALESCE(NULLIF($8, ''), pincode),
				website = COALESCE(NULLIF($9, ''), website),
				email = COALESCE(NULLIF($10, ''), email),
				gstin = COALESCE(NULLIF($11, ''), gstin),
				updated_at = NOW()
			WHERE id = $1 AND owner_user_id = $12
		`, businessID, in.Name, in.Description, in.AddressLine1, in.Locality,
			in.City, in.State, in.Pincode, in.Website, in.Email, in.GSTIN, ownerUserID)
		if err != nil {
			return nil, err
		}

		if in.Phone != "" {
			_, _ = s.db.Pool.Exec(ctx, `DELETE FROM business_phones WHERE business_id = $1`, businessID)
			_, _ = s.db.Pool.Exec(ctx, `
				INSERT INTO business_phones (business_id, phone, is_whatsapp, otp_verified)
				VALUES ($1, $2, TRUE, TRUE)
			`, businessID, in.Phone)
			if in.WhatsApp != "" && in.WhatsApp != in.Phone {
				_, _ = s.db.Pool.Exec(ctx, `
					INSERT INTO business_phones (business_id, phone, is_whatsapp, otp_verified)
					VALUES ($1, $2, TRUE, FALSE)
				`, businessID, in.WhatsApp)
			}
		}

		if in.Services != nil {
			s.syncBusinessServices(ctx, businessID, in.Services)
		}

		_ = s.persistCardImage(ctx, businessID, "front", in.FrontImageData)
		_ = s.persistCardImage(ctx, businessID, "back", in.BackImageData)
	}

	// Re-read the row instead of echoing the input: fields the caller left
	// blank were kept as-is by the UPDATE's COALESCE, so the input alone
	// can't tell us the true current value.
	biz := &domain.Business{
		ID:          businessID,
		OwnerUserID: ownerUserID,
		Status:      "live",
	}
	if s.db != nil && s.db.Pool != nil {
		var catName, status, verif, list string
		var website, email, gstin *string
		err := s.db.Pool.QueryRow(ctx, `
			SELECT b.name, b.slug, COALESCE(b.description, ''), b.primary_category_id,
			       COALESCE(c.name, ''), b.address_line1, b.city, b.state, b.pincode,
			       b.website, b.email, b.gstin,
			       b.status::text, b.verification::text, b.listing::text, b.completeness
			FROM businesses b
			LEFT JOIN categories c ON c.id = b.primary_category_id
			WHERE b.id = $1
		`, businessID).Scan(
			&biz.Name, &biz.Slug, &biz.Description, &biz.PrimaryCategoryID,
			&catName, &biz.AddressLine1, &biz.City, &biz.State, &biz.Pincode,
			&website, &email, &gstin,
			&status, &verif, &list, &biz.Completeness,
		)
		if err == nil {
			biz.PrimaryCategory = catName
			biz.Status = status
			biz.Verification = verif
			biz.Listing = list
			biz.Website = website
			biz.Email = email
			biz.GSTIN = gstin
		}
		biz.Services = s.getBusinessServices(ctx, businessID)
		biz.Phone, biz.WhatsApp, biz.Phones = s.getBusinessPhoneInfo(ctx, businessID)
	}
	biz.CardImageURL = "/api/v1/owner/businesses/" + businessID.String() + "/card-image?side=front"
	biz.CardBackImageURL = "/api/v1/owner/businesses/" + businessID.String() + "/card-image?side=back"
	return biz, nil
}

// UploadCardImage replaces one side of the owner's original business card image.
func (s *BusinessService) UploadCardImage(ctx context.Context, ownerUserID, businessID uuid.UUID, side, dataURL string) error {
	if err := s.VerifyOwnerAccess(ctx, ownerUserID, businessID); err != nil {
		return err
	}
	if side != "back" {
		side = "front"
	}
	if dataURL == "" {
		return errors.New("image data required")
	}
	return s.persistCardImage(ctx, businessID, side, dataURL)
}

// gstinOrNull stores "no GSTIN" as NULL: the one-listing-per-GSTIN rule must not treat
// two businesses without a GSTIN as the same business.
func gstinOrNull(g string) any {
	if g = strings.ToUpper(strings.TrimSpace(g)); g == "" {
		return nil
	}
	return g
}
