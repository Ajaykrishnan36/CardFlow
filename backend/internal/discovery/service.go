package discovery

import (
	"context"
	"fmt"
	"strings"

	"cardflow-backend/internal/database"
	"cardflow-backend/internal/domain"
	"github.com/google/uuid"
)

type DiscoveryService struct {
	db *database.DB
}

func NewDiscoveryService(db *database.DB) *DiscoveryService {
	return &DiscoveryService{db: db}
}

type SearchParams struct {
	Query      string
	CategoryID string
	Latitude   float64
	Longitude  float64
	RadiusKm   float64
	City       string
	Pincode    string
	Limit      int
	Offset     int
}

func (s *DiscoveryService) GetCategories(ctx context.Context) ([]domain.Category, error) {
	if s.db == nil || s.db.Pool == nil {
		return s.getFallbackCategories(), nil
	}

	rows, err := s.db.Pool.Query(ctx, `
		SELECT c.id, c.parent_id, c.name, c.slug, COALESCE(c.icon, 'Briefcase'), c.sort_order, c.is_active,
		       COUNT(b.id) as biz_count
		FROM categories c
		LEFT JOIN businesses b ON b.primary_category_id = c.id AND b.status = 'live' AND b.listing = 'listed'
		WHERE c.is_active = TRUE
		GROUP BY c.id, c.parent_id, c.name, c.slug, c.icon, c.sort_order, c.is_active
		ORDER BY c.sort_order ASC
	`)
	if err != nil {
		return s.getFallbackCategories(), nil
	}
	defer rows.Close()

	var categories []domain.Category
	for rows.Next() {
		var cat domain.Category
		if err := rows.Scan(&cat.ID, &cat.ParentID, &cat.Name, &cat.Slug, &cat.Icon, &cat.SortOrder, &cat.IsActive, &cat.Count); err == nil {
			categories = append(categories, cat)
		}
	}

	if len(categories) == 0 {
		return s.getFallbackCategories(), nil
	}

	return categories, nil
}

func (s *DiscoveryService) SearchBusinesses(ctx context.Context, p SearchParams) ([]domain.Business, error) {
	if s.db == nil || s.db.Pool == nil {
		return s.getFallbackBusinesses(p.Query, p.CategoryID), nil
	}

	if p.Limit <= 0 {
		p.Limit = 20
	}
	if p.RadiusKm <= 0 {
		p.RadiusKm = 25.0
	}

	// PostGIS spatial query with FTS rank and distance
	query := `
		SELECT b.id, b.owner_user_id, b.name, b.slug, COALESCE(b.description, ''), b.primary_category_id,
		       c.name as category_name, b.logo_url, b.website, b.email, b.address_line1, b.locality,
		       b.city, b.district, b.state, b.pincode, b.country,
		       ST_Y(b.location::geometry) as lat, ST_X(b.location::geometry) as lng,
		       COALESCE(ST_Distance(b.location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography) / 1000.0, 0.0) as distance_km,
		       b.service_area_km, b.year_established, b.gstin, b.trade_name, b.status::text, b.verification::text,
		       b.listing::text, b.phone_verified, b.completeness, b.created_at, b.updated_at
		FROM businesses b
		JOIN categories c ON c.id = b.primary_category_id
		WHERE b.status = 'live' AND b.listing = 'listed'
	`

	var args []interface{}
	args = append(args, p.Longitude, p.Latitude) // $1, $2

	argIndex := 3
	if p.CategoryID != "" {
		query += fmt.Sprintf(" AND (b.primary_category_id = $%d OR c.slug = $%d)", argIndex, argIndex)
		args = append(args, p.CategoryID)
		argIndex++
	}

	if p.Query != "" {
		query += fmt.Sprintf(" AND (b.search_tsv @@ plainto_tsquery('english', $%d) OR b.name ILIKE $%d OR b.city ILIKE $%d)", argIndex, argIndex+1, argIndex+1)
		args = append(args, p.Query, "%"+p.Query+"%")
		argIndex += 2
	}

	if p.Latitude != 0 && p.Longitude != 0 {
		query += fmt.Sprintf(" AND ST_DWithin(b.location, ST_SetSRID(ST_MakePoint($1, $2), 4326)::geography, $%d * 1000.0)", argIndex)
		args = append(args, p.RadiusKm)
		argIndex++
		query += " ORDER BY distance_km ASC"
	} else {
		query += " ORDER BY b.created_at DESC"
	}

	query += fmt.Sprintf(" LIMIT %d OFFSET %d", p.Limit, p.Offset)

	rows, err := s.db.Pool.Query(ctx, query, args...)
	if err != nil {
		return s.getFallbackBusinesses(p.Query, p.CategoryID), nil
	}
	defer rows.Close()

	var businesses []domain.Business
	for rows.Next() {
		var b domain.Business
		var desc, catName string
		var logo, web, email, loc, dist, yr, gstin, trade *string
		var status, verif, list string
		err := rows.Scan(
			&b.ID, &b.OwnerUserID, &b.Name, &b.Slug, &desc, &b.PrimaryCategoryID,
			&catName, &logo, &web, &email, &b.AddressLine1, &loc,
			&b.City, &dist, &b.State, &b.Pincode, &b.Country,
			&b.Latitude, &b.Longitude, &b.DistanceKm,
			&b.ServiceAreaKm, &yr, &gstin, &trade, &status, &verif,
			&list, &b.PhoneVerified, &b.Completeness, &b.CreatedAt, &b.UpdatedAt,
		)
		if err == nil {
			b.Description = desc
			b.PrimaryCategory = catName
			b.LogoURL = logo
			b.Website = web
			b.Email = email
			b.Locality = loc
			b.District = dist
			b.GSTIN = gstin
			b.TradeName = trade
			b.Status = status
			b.Verification = verif
			b.Listing = list

			// Load services and phones
			b.Services = s.getBusinessServices(ctx, b.ID)
			b.Phones = s.getBusinessPhones(ctx, b.ID)

			businesses = append(businesses, b)
		}
	}

	if len(businesses) == 0 {
		return s.getFallbackBusinesses(p.Query, p.CategoryID), nil
	}

	return businesses, nil
}

func (s *DiscoveryService) GetBusinessByIDOrSlug(ctx context.Context, identifier string) (*domain.Business, error) {
	if s.db == nil || s.db.Pool == nil {
		for _, b := range s.getFallbackBusinesses("", "") {
			if b.ID.String() == identifier || b.Slug == identifier {
				return &b, nil
			}
		}
		return nil, fmt.Errorf("business not found")
	}

	var b domain.Business
	var desc, catName string
	var logo, web, email, loc, dist, gstin, trade *string
	var yearEst *int
	var status, verif, list string

	err := s.db.Pool.QueryRow(ctx, `
		SELECT b.id, b.owner_user_id, b.name, b.slug, COALESCE(b.description, ''), b.primary_category_id,
		       c.name as category_name, b.logo_url, b.website, b.email, b.address_line1, b.locality,
		       b.city, b.district, b.state, b.pincode, b.country,
		       ST_Y(b.location::geometry) as lat, ST_X(b.location::geometry) as lng,
		       b.service_area_km, b.year_established, b.gstin, b.trade_name, b.status::text, b.verification::text,
		       b.listing::text, b.phone_verified, b.completeness, b.created_at, b.updated_at
		FROM businesses b
		JOIN categories c ON c.id = b.primary_category_id
		WHERE (b.id::text = $1 OR b.slug = $1) AND b.status != 'removed' AND b.owner_user_id IS NOT NULL
	`, identifier).Scan(
		&b.ID, &b.OwnerUserID, &b.Name, &b.Slug, &desc, &b.PrimaryCategoryID,
		&catName, &logo, &web, &email, &b.AddressLine1, &loc,
		&b.City, &dist, &b.State, &b.Pincode, &b.Country,
		&b.Latitude, &b.Longitude,
		&b.ServiceAreaKm, &yearEst, &gstin, &trade, &status, &verif,
		&list, &b.PhoneVerified, &b.Completeness, &b.CreatedAt, &b.UpdatedAt,
	)

	if err != nil {
		return nil, err
	}

	b.Description = desc
	b.PrimaryCategory = catName
	b.LogoURL = logo
	b.Website = web
	b.Email = email
	b.Locality = loc
	b.District = dist
	b.YearEstablished = yearEst
	b.GSTIN = gstin
	b.TradeName = trade
	b.Status = status
	b.Verification = verif
	b.Listing = list
	b.Services = s.getBusinessServices(ctx, b.ID)
	b.Phones = s.getBusinessPhones(ctx, b.ID)

	return &b, nil
}

func (s *DiscoveryService) getBusinessServices(ctx context.Context, bizID uuid.UUID) []string {
	if s.db == nil || s.db.Pool == nil {
		return []string{"Precision Machining", "CNC Milling", "Custom Tooling"}
	}
	rows, err := s.db.Pool.Query(ctx, `SELECT name FROM business_services WHERE business_id = $1`, bizID)
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

func (s *DiscoveryService) getBusinessPhones(ctx context.Context, bizID uuid.UUID) []string {
	if s.db == nil || s.db.Pool == nil {
		return []string{"+91 94430 12345"}
	}
	rows, err := s.db.Pool.Query(ctx, `SELECT phone FROM business_phones WHERE business_id = $1`, bizID)
	if err != nil {
		return nil
	}
	defer rows.Close()

	var phones []string
	for rows.Next() {
		var phone string
		if err := rows.Scan(&phone); err == nil {
			phones = append(phones, phone)
		}
	}
	return phones
}

func (s *DiscoveryService) getFallbackCategories() []domain.Category {
	return []domain.Category{
		{ID: uuid.MustParse("00000000-0000-0000-0000-0000000000c1"), Name: "Manufacturing", Slug: "manufacturing", Icon: "Factory", SortOrder: 1, IsActive: true, Count: 24},
		{ID: uuid.MustParse("00000000-0000-0000-0000-0000000000c2"), Name: "IT & Software", Slug: "it-software", Icon: "Code", SortOrder: 2, IsActive: true, Count: 38},
		{ID: uuid.MustParse("00000000-0000-0000-0000-0000000000c3"), Name: "Textiles & Garments", Slug: "textiles", Icon: "Shirt", SortOrder: 3, IsActive: true, Count: 45},
		{ID: uuid.MustParse("00000000-0000-0000-0000-0000000000c4"), Name: "Hardware & Materials", Slug: "hardware", Icon: "Wrench", SortOrder: 4, IsActive: true, Count: 19},
		{ID: uuid.MustParse("00000000-0000-0000-0000-0000000000c5"), Name: "Electrical & Automation", Slug: "electrical", Icon: "Zap", SortOrder: 5, IsActive: true, Count: 16},
	}
}

func (s *DiscoveryService) getFallbackBusinesses(query, catID string) []domain.Business {
	all := []domain.Business{
		{
			ID:                uuid.MustParse("00000000-0000-0000-0000-0000000000b1"),
			OwnerUserID:       uuid.MustParse("00000000-0000-0000-0000-000000000002"),
			Name:              "Kovai Precision Tools",
			Slug:              "kovai-precision-tools",
			Description:       "Leading manufacturers of CNC machined precision components, hydraulic valves, and automotive fittings.",
			PrimaryCategoryID: uuid.MustParse("00000000-0000-0000-0000-0000000000c1"),
			PrimaryCategory:   "Manufacturing",
			AddressLine1:      "42, SIDCO Industrial Estate",
			Locality:          strPtr("Peelamedu"),
			City:              "Coimbatore",
			State:             "Tamil Nadu",
			Pincode:           "641004",
			Country:           "IN",
			Latitude:          11.0268,
			Longitude:         76.9958,
			DistanceKm:        2.4,
			ServiceAreaKm:     50,
			Website:           strPtr("https://kovaiprecision.com"),
			Email:             strPtr("contact@kovaiprecision.com"),
			GSTIN:             strPtr("33AAAAA0000A1Z5"),
			Status:            "live",
			Verification:      "gst",
			Listing:           "listed",
			PhoneVerified:     true,
			Completeness:      95,
			Services:          []string{"CNC Milling", "Hydraulic Valves", "Lathe Machining", "Custom Tooling"},
			Phones:            []string{"+919443012345"},
		},
		{
			ID:                uuid.MustParse("00000000-0000-0000-0000-0000000000b2"),
			OwnerUserID:       uuid.MustParse("00000000-0000-0000-0000-000000000002"),
			Name:              "Apex Infotech Solutions",
			Slug:              "apex-infotech-solutions",
			Description:       "Enterprise ERP, Cloud Migration, and Custom Web & Mobile Application Development for MSMEs.",
			PrimaryCategoryID: uuid.MustParse("00000000-0000-0000-0000-0000000000c2"),
			PrimaryCategory:   "IT & Software",
			AddressLine1:      "105, Cross Cut Road",
			Locality:          strPtr("Gandhipuram"),
			City:              "Coimbatore",
			State:             "Tamil Nadu",
			Pincode:           "641018",
			Country:           "IN",
			Latitude:          11.0168,
			Longitude:         76.9658,
			DistanceKm:        4.1,
			ServiceAreaKm:     100,
			Website:           strPtr("https://apexinfotech.in"),
			Email:             strPtr("hello@apexinfotech.in"),
			GSTIN:             strPtr("33BBBBB1111B2Z6"),
			Status:            "live",
			Verification:      "gst",
			Listing:           "listed",
			PhoneVerified:     true,
			Completeness:      90,
			Services:          []string{"Cloud ERP", "React Native Apps", "SaaS Development", "AI Integrations"},
			Phones:            []string{"+919842155678"},
		},
	}

	if query == "" && catID == "" {
		return all
	}

	var filtered []domain.Business
	for _, b := range all {
		matchesQ := query == "" || strings.Contains(strings.ToLower(b.Name), strings.ToLower(query)) || strings.Contains(strings.ToLower(b.Description), strings.ToLower(query))
		matchesC := catID == "" || b.PrimaryCategoryID.String() == catID || strings.EqualFold(b.PrimaryCategory, catID)
		if matchesQ && matchesC {
			filtered = append(filtered, b)
		}
	}
	return filtered
}

func strPtr(s string) *string {
	return &s
}
