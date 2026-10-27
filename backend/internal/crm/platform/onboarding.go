package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Onboarding (D-155). Creating a business asks, in order: the person (name, date of birth,
// personal email; their mobile is the one they signed in with), their address, the
// business (brand name, industry, phone and WhatsApp — "same as mine" by default — email,
// website, GSTIN) and its address. An address is picked, not typed: country and state from
// lists, the rest filled from the pincode or from the device's location, then house /
// building, landmark and area. The person's details are kept once and offered again for
// their next business. Each business gets a login address, /<code>/login, and can add its
// own domain.

// Address is a postal address, as the forms send it.
type Address struct {
	Line       string   `json:"addressLine"` // house number / building name
	Landmark   string   `json:"landmark"`
	Area       string   `json:"area"`
	City       string   `json:"city"`
	District   string   `json:"district"`
	State      string   `json:"state"`
	PostalCode string   `json:"postalCode"`
	Country    string   `json:"country"`
	Latitude   *float64 `json:"latitude"`
	Longitude  *float64 `json:"longitude"`
}

var pincodeRe = regexp.MustCompile(`^[1-9][0-9]{5}$`)

// normalize trims the address and reports what is wrong, keyed by prefix+field.
func (a *Address) normalize(prefix string, fe map[string]string) {
	for _, p := range []*string{&a.Line, &a.Landmark, &a.Area, &a.City, &a.District, &a.State, &a.PostalCode, &a.Country} {
		*p = strings.Join(strings.Fields(*p), " ")
		if len(*p) > 200 {
			*p = (*p)[:200]
		}
	}
	if a.empty() {
		return
	}
	if a.Country == "" {
		a.Country = "India"
	}
	if strings.EqualFold(a.Country, "india") {
		a.Country = "India"
		if a.PostalCode != "" && !pincodeRe.MatchString(a.PostalCode) {
			fe[prefix+"postalCode"] = "Enter the 6-digit pincode."
		}
		if a.State != "" {
			if s, ok := shared.IndianStateNamed(a.State); ok {
				a.State = s.Name
			} else {
				fe[prefix+"state"] = "Choose the state from the list."
			}
		}
	}
	if a.Latitude != nil && (*a.Latitude < -90 || *a.Latitude > 90) || a.Longitude != nil && (*a.Longitude < -180 || *a.Longitude > 180) {
		fe[prefix+"latitude"] = "That location isn't valid."
	}
}

func (a *Address) empty() bool {
	return a == nil || a.Line == "" && a.Landmark == "" && a.Area == "" && a.City == "" && a.District == "" && a.State == "" && a.PostalCode == "" && a.Latitude == nil
}

// OneLine is the address written out on one line.
func (a *Address) OneLine() string {
	parts := []string{}
	for _, s := range []string{a.Line, a.Area, a.Landmark, a.City, a.State, a.PostalCode, a.Country} {
		if s != "" {
			parts = append(parts, s)
		}
	}
	if a.Landmark != "" {
		// "near <landmark>" reads better in a printed address
		for i, p := range parts {
			if p == a.Landmark {
				parts[i] = "Near " + a.Landmark
			}
		}
	}
	return strings.Join(parts, ", ")
}

// TeamSizes are the choices for "Size of team".
var TeamSizes = map[string]bool{"Just me": true, "2-10": true, "11-50": true, "51-200": true, "201-500": true, "500+": true}

// normalizeOnboarding checks and tidies everything the create form sends.
func (in *CreateBusinessInput) normalizeOnboarding() map[string]string {
	fe := map[string]string{}
	in.DisplayName = strings.Join(strings.Fields(in.DisplayName), " ")
	if in.DisplayName != "" && (len(in.DisplayName) < 2 || len(in.DisplayName) > 120) {
		fe["displayName"] = "Enter your name (2–120 characters)."
	}
	if s := strings.TrimSpace(in.DateOfBirth); s != "" {
		d, err := time.Parse("2006-01-02", s)
		age := time.Since(d).Hours() / 24 / 365.25
		switch {
		case err != nil:
			fe["dateOfBirth"] = "Enter the date as YYYY-MM-DD."
		case age < 13:
			fe["dateOfBirth"] = "You need to be at least 13."
		case age > 120:
			fe["dateOfBirth"] = "Check the year."
		}
		in.DateOfBirth = s
	}
	checkEmail := func(key string, v *string) {
		*v = strings.ToLower(strings.TrimSpace(*v))
		if *v == "" {
			return
		}
		if a, err := mail.ParseAddress(*v); err != nil || a.Address != *v || !strings.Contains((*v)[strings.LastIndex(*v, "@"):], ".") || len(*v) > 200 {
			fe[key] = "Enter a valid email address."
		}
	}
	checkEmail("personalEmail", &in.PersonalEmail)
	checkEmail("email", &in.Email)
	checkPhone := func(key string, v *string) {
		*v = strings.TrimSpace(*v)
		if *v == "" {
			return
		}
		if p, ok := identity.NormalizePhone(*v); ok {
			*v = p
		} else {
			fe[key] = "Enter a valid phone number."
		}
	}
	checkPhone("phone", &in.Phone)
	checkPhone("whatsapp", &in.Whatsapp)
	if w := strings.TrimSpace(in.Website); w != "" {
		if !strings.Contains(w, "://") {
			w = "https://" + w
		}
		u, err := url.Parse(w)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || !strings.Contains(u.Host, ".") || len(w) > 200 {
			fe["website"] = "Enter a website address, like www.example.com."
		}
		in.Website = w
	}
	if in.Personal != nil {
		in.Personal.normalize("personal.", fe)
	}
	if in.Address != nil {
		in.Address.normalize("address.", fe)
		if !in.Address.empty() {
			in.City, in.State, in.Country = in.Address.City, in.Address.State, in.Address.Country
		}
	}
	if strings.TrimSpace(in.TaxID) == "" && strings.TrimSpace(in.GSTIN) != "" { // clients from before D-158
		in.TaxIDType, in.TaxID = "gstin", in.GSTIN
	}
	if strings.TrimSpace(in.TaxID) != "" {
		in.TaxIDType = strings.ToLower(strings.TrimSpace(in.TaxIDType))
		if in.TaxIDType == "" {
			in.TaxIDType = "gstin"
		}
		id, msg := shared.CheckTaxID(in.TaxIDType, in.TaxID, in.State)
		in.TaxID = id
		if msg != "" {
			fe["taxId"] = msg
			if in.TaxIDType == "gstin" && strings.TrimSpace(in.GSTIN) != "" {
				fe["gstin"] = msg
			}
		} else if in.TaxIDType == "gstin" {
			in.GSTIN = id
		}
	} else {
		in.TaxIDType = ""
	}
	in.TeamSize = strings.TrimSpace(in.TeamSize)
	if in.TeamSize != "" && !TeamSizes[in.TeamSize] {
		fe["teamSize"] = "Choose the size of your team."
	}
	return fe
}

// businessProfileFrom is what the create form puts in the business's profile.
func businessProfileFrom(in CreateBusinessInput) map[string]string {
	profile := map[string]string{}
	add := func(k, v string) {
		if v = strings.TrimSpace(v); v != "" {
			if len(v) > 500 {
				v = v[:500]
			}
			profile[k] = v
		}
	}
	for k, v := range map[string]string{"industry": in.Industry, "phone": in.Phone, "whatsapp": in.Whatsapp, "email": in.Email, "website": in.Website,
		"gstin": in.GSTIN, "city": in.City, "state": in.State, "country": in.Country, "teamSize": in.TeamSize, "taxIdType": in.TaxIDType, "taxId": in.TaxID} {
		add(k, v)
	}
	if in.TaxIDType == "pan" || in.TaxIDType == "tan" {
		add(in.TaxIDType, in.TaxID)
	}
	if a := in.Address; !a.empty() {
		for k, v := range map[string]string{"addressLine": a.Line, "landmark": a.Landmark, "area": a.Area, "district": a.District, "postalCode": a.PostalCode, "address": a.OneLine()} {
			add(k, v)
		}
		if a.Latitude != nil && a.Longitude != nil {
			add("latitude", strconv.FormatFloat(*a.Latitude, 'f', 6, 64))
			add("longitude", strconv.FormatFloat(*a.Longitude, 'f', 6, 64))
		}
	}
	return profile
}

// savePersonDetails keeps the person's own details (name, date of birth, personal email, address).
func savePersonDetails(ctx context.Context, tx pgx.Tx, who uuid.UUID, name, dob, email string, a *Address) error {
	if name != "" {
		if _, err := tx.Exec(ctx, `UPDATE crm.identities SET display_name = $2, updated_at = now() WHERE id = $1`, who, name); err != nil {
			return err
		}
	}
	if dob == "" && email == "" && a.empty() {
		return nil
	}
	if a == nil {
		a = &Address{}
	}
	_, err := tx.Exec(ctx, `
		INSERT INTO crm.person_details (identity_id, date_of_birth, personal_email, address_line, landmark, area, city, district, state, postal_code, country, latitude, longitude, updated_at)
		VALUES ($1, NULLIF($2, '')::date, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), NULLIF($9, ''), NULLIF($10, ''), NULLIF($11, ''), $12, $13, now())
		ON CONFLICT (identity_id) DO UPDATE SET date_of_birth = COALESCE(EXCLUDED.date_of_birth, crm.person_details.date_of_birth),
			personal_email = COALESCE(EXCLUDED.personal_email, crm.person_details.personal_email),
			address_line = EXCLUDED.address_line, landmark = EXCLUDED.landmark, area = EXCLUDED.area, city = EXCLUDED.city,
			district = EXCLUDED.district, state = EXCLUDED.state, postal_code = EXCLUDED.postal_code, country = EXCLUDED.country,
			latitude = EXCLUDED.latitude, longitude = EXCLUDED.longitude, updated_at = now()`,
		who, dob, email, a.Line, a.Landmark, a.Area, a.City, a.District, a.State, a.PostalCode, a.Country, a.Latitude, a.Longitude)
	return err
}

// PersonDetails is what the person told us about themselves.
type PersonDetails struct {
	DisplayName   string  `json:"displayName"`
	Phone         string  `json:"phone"`
	DateOfBirth   string  `json:"dateOfBirth"`
	PersonalEmail string  `json:"personalEmail"`
	Address       Address `json:"address"`
}

func loadPersonDetails(ctx context.Context, q querier, who uuid.UUID) (*PersonDetails, error) {
	p := &PersonDetails{}
	var dob *time.Time
	var lat, lng *float64
	err := q.QueryRow(ctx, `
		SELECT i.display_name,
		       COALESCE((SELECT value_normalized FROM crm.verified_identifiers WHERE identity_id = i.id AND kind = 'phone' ORDER BY verified_at DESC NULLS LAST LIMIT 1), ''),
		       d.date_of_birth, COALESCE(d.personal_email, ''), COALESCE(d.address_line, ''), COALESCE(d.landmark, ''), COALESCE(d.area, ''),
		       COALESCE(d.city, ''), COALESCE(d.district, ''), COALESCE(d.state, ''), COALESCE(d.postal_code, ''), COALESCE(d.country, ''),
		       d.latitude::float8, d.longitude::float8
		FROM crm.identities i LEFT JOIN crm.person_details d ON d.identity_id = i.id WHERE i.id = $1`, who).
		Scan(&p.DisplayName, &p.Phone, &dob, &p.PersonalEmail, &p.Address.Line, &p.Address.Landmark, &p.Address.Area, &p.Address.City,
			&p.Address.District, &p.Address.State, &p.Address.PostalCode, &p.Address.Country, &lat, &lng)
	if err != nil {
		return nil, err
	}
	if dob != nil {
		p.DateOfBirth = dob.Format("2006-01-02")
	}
	p.Address.Latitude, p.Address.Longitude = lat, lng
	if p.DisplayName == "New user" {
		p.DisplayName = ""
	}
	return p, nil
}

// ---------------------------------------------------------------- routes

// OnboardingRoutes: the signed-in person's details and the address lookups.
func (h *Handler) onboardingRoutes(r chi.Router) {
	r.Get("/me/details", func(w http.ResponseWriter, r *http.Request) {
		p, err := loadPersonDetails(r.Context(), h.store.Pool, actorID(r))
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, p)
	})
	r.Put("/me/details", func(w http.ResponseWriter, r *http.Request) {
		var in CreateBusinessInput
		if err := shared.DecodeJSON(w, r, &in); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		fe := in.normalizeOnboarding()
		for k := range fe {
			if !strings.HasPrefix(k, "personal.") && k != "displayName" && k != "dateOfBirth" && k != "personalEmail" {
				delete(fe, k)
			}
		}
		if len(fe) > 0 {
			shared.WriteError(w, r, shared.Validation(fe))
			return
		}
		err := h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
			return savePersonDetails(r.Context(), tx, actorID(r), in.DisplayName, in.DateOfBirth, in.PersonalEmail, in.Personal)
		})
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		p, err := loadPersonDetails(r.Context(), h.store.Pool, actorID(r))
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, p)
	})
	r.Get("/geo/states", func(w http.ResponseWriter, r *http.Request) {
		shared.WriteJSON(w, http.StatusOK, map[string]any{"country": "India", "data": shared.IndianStates})
	})
	r.Get("/geo/pincode/{pin}", h.handlePincode)
	r.Get("/geo/reverse", h.handleReverseGeocode)
}

// PublicTenantRoutes need no sign-in: a business's login page finds its name (D-156, D-158).
func (h *Handler) PublicTenantRoutes(r chi.Router) {
	r.Get("/public/tenants/by-host", func(w http.ResponseWriter, r *http.Request) {
		host := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("host")))
		if i := strings.IndexByte(host, ':'); i >= 0 {
			host = host[:i]
		}
		t := &Tenant{}
		err := h.store.Pool.QueryRow(r.Context(), `SELECT id, slug, name, COALESCE(custom_domain, '') FROM crm.tenants WHERE lower(custom_domain) = $1 AND status = 'active'`, host).
			Scan(&t.ID, &t.Slug, &t.Name, &t.CustomDomain)
		if errors.Is(err, pgx.ErrNoRows) || host == "" {
			err = identity.BusinessNotFound()
		}
		h.writeTenant(w, r, t, err)
	})
	r.Get("/public/tenants/{slug}", func(w http.ResponseWriter, r *http.Request) {
		t, err := TenantBySlug(r.Context(), h.store.Pool, chi.URLParam(r, "slug"))
		h.writeTenant(w, r, t, err)
	})
}

func (h *Handler) writeTenant(w http.ResponseWriter, r *http.Request, t *Tenant, err error) {
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=60")
	shared.WriteJSON(w, http.StatusOK, map[string]any{"slug": t.Slug, "code": t.Slug, "name": t.Name, "loginPath": "/" + t.Slug + "/login",
		"customDomain": t.CustomDomain, "home": "/" + t.Slug + "/home"})
}

// ---------------------------------------------------------------- address lookups

var geoClient = &http.Client{Timeout: 8 * time.Second}

// geoUA names us to the lookup services (India Post refuses Go's default client name).
const geoUA = "AjaysCRM/1.0 (address autofill; contact: support@ajayscrm.app)"

type geoCache struct {
	sync.Mutex
	m map[string]geoEntry
}
type geoEntry struct {
	at  time.Time
	val any
}

func (c *geoCache) get(k string) (any, bool) {
	c.Lock()
	defer c.Unlock()
	e, ok := c.m[k]
	if !ok || time.Since(e.at) > 24*time.Hour {
		return nil, false
	}
	return e.val, true
}
func (c *geoCache) put(k string, v any) {
	c.Lock()
	defer c.Unlock()
	if c.m == nil || len(c.m) > 5000 {
		c.m = map[string]geoEntry{}
	}
	c.m[k] = geoEntry{time.Now(), v}
}

var pinCache, revCache geoCache

func envOr(k, d string) string {
	if v := strings.TrimSpace(os.Getenv(k)); v != "" {
		return v
	}
	return d
}

// GET /geo/pincode/{pin}: India Post's offices for a pincode → state, district, city and areas.
func (h *Handler) handlePincode(w http.ResponseWriter, r *http.Request) {
	pin := chi.URLParam(r, "pin")
	if !pincodeRe.MatchString(pin) {
		shared.WriteError(w, r, shared.Validation(map[string]string{"postalCode": "Enter the 6-digit pincode."}))
		return
	}
	if v, ok := pinCache.get(pin); ok {
		shared.WriteJSON(w, http.StatusOK, v)
		return
	}
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, envOr("CRM_PINCODE_API", "https://api.postalpincode.in")+"/pincode/"+pin, nil)
	req.Header.Set("User-Agent", geoUA)
	req.Header.Set("Accept", "application/json")
	res, err := geoClient.Do(req)
	if err != nil {
		shared.WriteError(w, r, shared.NewError(http.StatusBadGateway, "lookup_unavailable", "Couldn't look up the pincode right now. Choose the state and type the city."))
		return
	}
	defer res.Body.Close()
	var out []struct {
		Status     string `json:"Status"`
		PostOffice []struct {
			Name, District, Block, State, Country string
		} `json:"PostOffice"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out); err != nil || len(out) == 0 {
		shared.WriteError(w, r, shared.NewError(http.StatusBadGateway, "lookup_unavailable", "Couldn't look up the pincode right now. Choose the state and type the city."))
		return
	}
	if out[0].Status != "Success" || len(out[0].PostOffice) == 0 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"postalCode": "No place found for this pincode. Check it."}))
		return
	}
	po := out[0].PostOffice
	areas := []string{}
	seen := map[string]bool{}
	for _, p := range po {
		if !seen[p.Name] {
			seen[p.Name] = true
			areas = append(areas, p.Name)
		}
	}
	state := po[0].State
	if s, ok := shared.IndianStateNamed(state); ok {
		state = s.Name
	}
	val := map[string]any{"postalCode": pin, "country": "India", "state": state, "district": po[0].District, "city": po[0].District, "areas": areas}
	pinCache.put(pin, val)
	shared.WriteJSON(w, http.StatusOK, val)
}

var nominatimGate sync.Mutex
var nominatimLast time.Time

// GET /geo/reverse?lat=&lng=: the address at the device's location (OpenStreetMap).
func (h *Handler) handleReverseGeocode(w http.ResponseWriter, r *http.Request) {
	lat, err1 := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lng, err2 := strconv.ParseFloat(r.URL.Query().Get("lng"), 64)
	if err1 != nil || err2 != nil || lat < -90 || lat > 90 || lng < -180 || lng > 180 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"location": "That location isn't valid."}))
		return
	}
	key := fmt.Sprintf("%.4f,%.4f", lat, lng)
	if v, ok := revCache.get(key); ok {
		shared.WriteJSON(w, http.StatusOK, v)
		return
	}
	// OpenStreetMap's public geocoder allows one request a second, with a named client.
	nominatimGate.Lock()
	if d := time.Second - time.Since(nominatimLast); d > 0 {
		time.Sleep(d)
	}
	nominatimLast = time.Now()
	nominatimGate.Unlock()
	u := envOr("CRM_GEOCODER_URL", "https://nominatim.openstreetmap.org") + "/reverse?format=jsonv2&addressdetails=1&zoom=18&lat=" +
		strconv.FormatFloat(lat, 'f', 6, 64) + "&lon=" + strconv.FormatFloat(lng, 'f', 6, 64)
	req, _ := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	req.Header.Set("User-Agent", geoUA)
	req.Header.Set("Accept-Language", "en")
	res, err := geoClient.Do(req)
	if err != nil {
		shared.WriteError(w, r, shared.NewError(http.StatusBadGateway, "lookup_unavailable", "Couldn't find the address for your location. Enter the pincode instead."))
		return
	}
	defer res.Body.Close()
	var out struct {
		Address map[string]string `json:"address"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out); err != nil || len(out.Address) == 0 {
		shared.WriteError(w, r, shared.NewError(http.StatusBadGateway, "lookup_unavailable", "Couldn't find the address for your location. Enter the pincode instead."))
		return
	}
	a := out.Address
	first := func(keys ...string) string {
		for _, k := range keys {
			if v := strings.TrimSpace(a[k]); v != "" {
				return v
			}
		}
		return ""
	}
	state := first("state", "union_territory")
	if s, ok := shared.IndianStateNamed(state); ok {
		state = s.Name
	}
	line := strings.TrimSpace(strings.Join(strings.Fields(first("house_number")+" "+first("building", "amenity")+" "+first("road")), " "))
	val := map[string]any{"addressLine": line, "area": first("suburb", "neighbourhood", "quarter", "village", "hamlet"),
		"city": first("city", "town", "municipality", "village", "state_district"), "district": first("state_district", "county"),
		"state": state, "postalCode": strings.ReplaceAll(first("postcode"), " ", ""), "country": first("country"), "latitude": lat, "longitude": lng}
	revCache.put(key, val)
	shared.WriteJSON(w, http.StatusOK, val)
}
