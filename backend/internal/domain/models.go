package domain

import (
	"time"

	"github.com/google/uuid"
)

// User Roles & Statuses
type UserRole string

const (
	RoleUser  UserRole = "user"
	RoleAdmin UserRole = "admin"
)

type SubscriptionPlan string

const (
	PlanFree    SubscriptionPlan = "free"
	PlanPlus    SubscriptionPlan = "plus"
	PlanPremium SubscriptionPlan = "premium"
)

type User struct {
	ID                    uuid.UUID        `json:"id"`
	Phone                 string           `json:"phone"`
	Name                  string           `json:"name"`
	Email                 *string          `json:"email,omitempty"`
	PhotoURL              *string          `json:"photo_url,omitempty"`
	City                  string           `json:"city"`
	State                 string           `json:"state"`
	Country               string           `json:"country"`
	Role                  UserRole         `json:"role"`
	Plan                  SubscriptionPlan `json:"plan"`
	FreeScansRemaining    int              `json:"free_scans_remaining"`
	FreeScansResetAt      time.Time        `json:"free_scans_reset_at"`
	Status                string           `json:"status"`
	IsIDVerified          bool             `json:"is_id_verified"`
	CreditBalance         int              `json:"credit_balance"`
	IsSubscribed          bool             `json:"is_subscribed"`
	SubscriptionPlanID    *string          `json:"subscription_plan_id,omitempty"`
	SubscriptionExpiresAt *time.Time       `json:"subscription_expires_at,omitempty"`
	CreatedAt             time.Time        `json:"created_at"`
	UpdatedAt             time.Time        `json:"updated_at"`
	LastLoginAt           *time.Time       `json:"last_login_at,omitempty"`
	DeletedAt             *time.Time       `json:"deleted_at,omitempty"`
}

// IsPremiumActive reports whether the user currently has an unexpired premium
// subscription — a nil expiry means a lifetime plan, which never expires.
func (u *User) IsPremiumActive() bool {
	if u == nil || !u.IsSubscribed {
		return false
	}
	return u.SubscriptionExpiresAt == nil || u.SubscriptionExpiresAt.After(time.Now())
}

type UserKYC struct {
	UserID             uuid.UUID  `json:"user_id"`
	AadhaarStatus      string     `json:"aadhaar_status"`
	AadhaarLast4       *string    `json:"aadhaar_last4,omitempty"`
	AadhaarProviderRef *string    `json:"aadhaar_provider_ref,omitempty"`
	PANMasked          *string    `json:"pan_masked,omitempty"`
	PANStatus          string     `json:"pan_status"`
	RegistryName       *string    `json:"registry_name,omitempty"`
	NameMatchScore     *float64   `json:"name_match_score,omitempty"`
	Provider           string     `json:"provider"`
	VerifiedAt         *time.Time `json:"verified_at,omitempty"`
}

type Category struct {
	ID        uuid.UUID  `json:"id"`
	ParentID  *uuid.UUID `json:"parent_id,omitempty"`
	Name      string     `json:"name"`
	Slug      string     `json:"slug"`
	Icon      string     `json:"icon"`
	SortOrder int        `json:"sort_order"`
	IsActive  bool       `json:"is_active"`
	Count     int        `json:"count,omitempty"`
}

type Business struct {
	ID                uuid.UUID `json:"id"`
	OwnerUserID       uuid.UUID `json:"owner_user_id"`
	Name              string    `json:"name"`
	Slug              string    `json:"slug"`
	Description       string    `json:"description"`
	PrimaryCategoryID uuid.UUID `json:"primary_category_id"`
	PrimaryCategory   string    `json:"primary_category,omitempty"`
	LogoURL           *string   `json:"logo_url,omitempty"`
	Website           *string   `json:"website,omitempty"`
	Email             *string   `json:"email,omitempty"`
	AddressLine1      string    `json:"address_line1"`
	AddressLine2      *string   `json:"address_line2,omitempty"`
	Locality          *string   `json:"locality,omitempty"`
	City              string    `json:"city"`
	District          *string   `json:"district,omitempty"`
	State             string    `json:"state"`
	Pincode           string    `json:"pincode"`
	Country           string    `json:"country"`
	Latitude          float64   `json:"latitude"`
	Longitude         float64   `json:"longitude"`
	DistanceKm        float64   `json:"distance_km,omitempty"`
	ServiceAreaKm     int       `json:"service_area_km"`
	Hours             string    `json:"hours,omitempty"`
	YearEstablished   *int      `json:"year_established,omitempty"`
	GSTIN             *string   `json:"gstin,omitempty"`
	LegalName         *string   `json:"legal_name,omitempty"`
	TradeName         *string   `json:"trade_name,omitempty"`
	Status            string    `json:"status"`
	Verification      string    `json:"verification"` // 'pending', 'gst', 'pan', 'tan', 'manual', 'failed'
	Listing           string    `json:"listing"`      // 'unlisted', 'listed'
	PhoneVerified     bool      `json:"phone_verified"`
	Completeness      int       `json:"completeness"`
	IsSponsored       bool      `json:"is_sponsored,omitempty"`
	ViewsCount        int       `json:"views_count,omitempty"`
	EnquiriesCount    int       `json:"enquiries_count,omitempty"`
	Services          []string  `json:"services,omitempty"`
	Phone             *string   `json:"phone,omitempty"`
	Phones            []string  `json:"phones,omitempty"`
	WhatsApp          *string   `json:"whatsapp,omitempty"`
	CardImageURL      string    `json:"card_image_url,omitempty"`
	CardBackImageURL  string    `json:"card_back_image_url,omitempty"`
	// SavedCount is how many people have saved this business's card to their vault.
	SavedCount int `json:"saved_count"`
	// Source is 'owner' (registered in the app) or 'card' (created from a scanned card, then claimed).
	Source    string     `json:"source,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
	DeletedAt *time.Time `json:"deleted_at,omitempty"`
}

type DigitalCard struct {
	ID               uuid.UUID `json:"id"`
	BusinessID       uuid.UUID `json:"business_id"`
	Template         string    `json:"template"`
	BrandColor       string    `json:"brand_color"`
	QRSlug           string    `json:"qr_slug"`
	RenderedImageURL *string   `json:"rendered_image_url,omitempty"`
}

type SavedCard struct {
	ID                   uuid.UUID   `json:"id"`
	UserID               uuid.UUID   `json:"user_id"`
	PersonName           string      `json:"person_name"`
	Designation          string      `json:"designation"`
	Company              string      `json:"company"`
	Website              *string     `json:"website,omitempty"`
	Notes                string      `json:"notes"`
	MetContext           string      `json:"met_context"`
	PrivateRating        *int        `json:"private_rating,omitempty"`
	ContactType          string      `json:"contact_type"`
	ExtractStatus        string      `json:"extract_status"`
	RawAddress           string      `json:"raw_address,omitempty"`
	StructuredAddr       *string     `json:"structured_address,omitempty"`
	Tags                 []string    `json:"tags,omitempty"`
	Phones               []CardPhone `json:"phones,omitempty"`
	Emails               []string    `json:"emails,omitempty"`
	FrontImageKey        *string     `json:"front_image_key,omitempty"`
	BackImageKey         *string     `json:"back_image_key,omitempty"`
	OriginalCardImageURL string      `json:"original_card_image_url,omitempty"`
	OriginalBackImageURL string      `json:"original_back_image_url,omitempty"`
	GSTIN                string      `json:"gstin,omitempty"`
	Latitude             *float64    `json:"latitude,omitempty"`
	Longitude            *float64    `json:"longitude,omitempty"`
	// Source distinguishes how this card entered the vault: 'SCANNED' (OCR'd a
	// physical card) or 'BUSINESS_PROFILE' (saved from Browse/Discover).
	Source string `json:"source,omitempty"`
	// LinkedBusinessID is set when this card's GSTIN matches an existing
	// registered business — the card then shares that business's data/images
	// instead of storing its own duplicate copy.
	LinkedBusinessID *uuid.UUID `json:"linked_business_id,omitempty"`
	// Structured address parts (optional; raw_address is still the display form).
	AddressLine string `json:"address_line,omitempty"`
	City        string `json:"city,omitempty"`
	State       string `json:"state,omitempty"`
	Pincode     string `json:"pincode,omitempty"`
	// SavedCount is how many people have saved this business's card (linked cards only).
	SavedCount int       `json:"saved_count,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

type CardPhone struct {
	Raw        string  `json:"raw"`
	E164       string  `json:"e164"`
	Type       string  `json:"type"`
	Usage      string  `json:"usage"`
	IsWhatsApp bool    `json:"is_whatsapp"`
	Confidence float64 `json:"confidence"`
}

type Enquiry struct {
	ID            uuid.UUID  `json:"id"`
	BusinessID    uuid.UUID  `json:"business_id"`
	UserID        uuid.UUID  `json:"user_id"`
	CustomerName  string     `json:"customer_name,omitempty"`
	CustomerPhone string     `json:"customer_phone,omitempty"`
	Message       string     `json:"message"`
	SharePhone    bool       `json:"share_phone"`
	Status        string     `json:"status"` // 'new', 'viewed', 'responded', 'closed'
	CreatedAt     time.Time  `json:"created_at"`
	RespondedAt   *time.Time `json:"responded_at,omitempty"`
}

type AuditLog struct {
	ID         uuid.UUID   `json:"id"`
	AdminID    uuid.UUID   `json:"admin_id"`
	Action     string      `json:"action"`
	TargetType string      `json:"target_type"`
	TargetID   uuid.UUID   `json:"target_id"`
	Before     interface{} `json:"before_state,omitempty"`
	After      interface{} `json:"after_state,omitempty"`
	Notes      string      `json:"notes"`
	IPAddress  string      `json:"ip_address"`
	CreatedAt  time.Time   `json:"created_at"`
}
