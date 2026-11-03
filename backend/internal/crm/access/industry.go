package access

import (
	"context"
	"strings"

	"github.com/google/uuid"
)

// One CRM for every trade (D-174). A business picks its industry and its business type when
// it is created; that choice is data, not code: it switches on the industry pack that fits
// (vehicles, policies, legal matters, units…), shows the fields that trade asks a lead for,
// and changes a few words — a law firm's contacts are Clients, a car dealer books Test
// drives. Everything underneath is the same leads → opportunity → quote → payment →
// invoice → case flow. Adding a trade or a business type is a line here.

// BusinessType is one kind of business inside an industry.
type BusinessType struct {
	Key   string `json:"key"`
	Label string `json:"label"`
}

// Industry is one choice of "Industry type" on the sign-up form.
type Industry struct {
	Key      string         `json:"key"`
	Label    string         `json:"label"`
	Keywords string         `json:"keywords,omitempty"` // more words the search box matches
	Types    []BusinessType `json:"types"`
	TypeName string         `json:"typeLabel"` // what the business-type box is called
	// Packs: the industry packs switched on for this industry. A pack is matched to older
	// businesses by the industry words in IndustryPacks as well.
	Packs []string `json:"packs,omitempty"`
}

func types(pairs ...string) []BusinessType {
	out := make([]BusinessType, 0, len(pairs)/2+1)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, BusinessType{Key: pairs[i], Label: pairs[i+1]})
	}
	return append(out, BusinessType{Key: "other", Label: "Other"})
}

// OtherIndustry is the choice for a trade that isn't listed: the business types its own and
// gets the common CRM, without any industry pack or wording.
const OtherIndustry = "Other"

// Industries is the list on the sign-up form, in its order. Labels are what is stored in the
// business profile, so a label is never changed once used (older ones stay matched below).
var Industries = []Industry{
	{Key: "retail", Label: "Retail", Keywords: "shop store", TypeName: "Type of shop", Types: types(
		"mobile_shop", "Mobile shop", "electronics", "Electronics", "clothing", "Clothing", "grocery", "Grocery",
		"furniture", "Furniture", "hardware", "Hardware", "medical_shop", "Medical shop", "general_retail", "General retail")},
	{Key: "wholesale", Label: "Wholesale & distribution", Keywords: "distributor stockist dealer", TypeName: "Type of distribution", Types: types(
		"wholesale_distributor", "Wholesale distributor", "fmcg", "FMCG distributor", "electronics", "Electronics distributor",
		"auto_parts", "Automobile parts distributor", "food", "Food distributor", "industrial", "Industrial supplies")},
	{Key: "automotive", Label: "Automotive", Keywords: "bike car vehicle dealer showroom two wheeler motorcycle scooter ev", TypeName: "Vehicle business type",
		Packs: []string{"dealership"}, Types: types(
			"bike", "Bike", "car", "Car", "bike_car", "Both bike & car", "scooter", "Scooter", "motorcycle", "Motorcycle",
			"commercial", "Commercial vehicles", "ev", "Electric vehicles")},
	{Key: "manufacturing", Label: "Manufacturing", Keywords: "factory production", TypeName: "What you make", Types: types(
		"food", "Food processing", "textiles", "Textiles & garments", "engineering", "Engineering & machinery", "chemicals", "Chemicals",
		"plastics", "Plastics & packaging", "furniture", "Furniture", "electronics", "Electronics")},
	{Key: "agriculture", Label: "Agriculture", Keywords: "farm agri dairy", TypeName: "Type of business", Types: types(
		"farm", "Farm / grower", "inputs", "Seeds & fertilisers", "equipment", "Farm equipment", "dairy", "Dairy", "poultry", "Poultry",
		"agro_trading", "Agro trading")},
	{Key: "transport", Label: "Transport & logistics", Keywords: "courier fleet truck travel logistics", TypeName: "Type of business", Types: types(
		"courier", "Courier & parcel", "fleet", "Truck / fleet operator", "freight", "Freight forwarding", "warehousing", "Warehousing",
		"movers", "Packers & movers", "travel", "Travel & taxi")},
	{Key: "services", Label: "Services", Keywords: "consulting agency", TypeName: "Type of service", Types: types(
		"consulting", "Consulting", "accounting", "Accounting & tax", "marketing", "Marketing & advertising", "facility", "Cleaning & facility",
		"repair", "Repair & maintenance", "events", "Event management", "printing", "Printing")},
	{Key: "real_estate", Label: "Real estate", Keywords: "property builder broker rental", TypeName: "Type of business",
		Packs: []string{"real_estate"}, Types: types(
			"agent", "Real estate agent", "consultant", "Property consultant", "builder", "Builder", "developer", "Property developer",
			"rental", "Rental management", "commercial", "Commercial real estate")},
	{Key: "construction", Label: "Construction", Keywords: "contractor interior building", TypeName: "Type of business", Types: types(
		"contractor", "Contractor", "builder", "Builder", "interiors", "Interior design", "architecture", "Architecture",
		"materials", "Building materials", "electrical", "Electrical & plumbing")},
	{Key: "education", Label: "Education", Keywords: "school college coaching training", TypeName: "Type of institution", Types: types(
		"school", "School", "college", "College", "coaching", "Coaching / tuition", "training", "Training institute",
		"online", "Online courses", "preschool", "Preschool")},
	{Key: "healthcare", Label: "Healthcare", Keywords: "clinic hospital doctor lab dental pharmacy", TypeName: "Type of practice", Types: types(
		"clinic", "Clinic", "hospital", "Hospital", "lab", "Diagnostic lab", "dental", "Dental", "pharmacy", "Pharmacy",
		"physio", "Physiotherapy", "ayush", "Ayurveda & wellness")},
	{Key: "hospitality", Label: "Hospitality", Keywords: "hotel restaurant cafe resort catering", TypeName: "Type of business", Types: types(
		"hotel", "Hotel", "restaurant", "Restaurant", "cafe", "Cafe", "resort", "Resort", "homestay", "Homestay", "catering", "Catering",
		"travel_agency", "Travel agency")},
	{Key: "technology", Label: "Technology", Keywords: "software it saas digital agency", TypeName: "Type of business", Types: types(
		"software", "Software development", "it_consulting", "IT consulting", "saas", "SaaS", "hardware", "Hardware sales",
		"it_services", "IT services", "digital_agency", "Digital agency")},
	{Key: "finance", Label: "Finance", Keywords: "loan nbfc broker advisor", TypeName: "Type of business", Types: types(
		"loans", "Loans & NBFC", "chit", "Chit fund", "advisor", "Financial advisor", "accounting", "Accounting firm",
		"broking", "Stock broking", "microfinance", "Microfinance")},
	{Key: "insurance", Label: "Insurance", Keywords: "policy agent broker lic", TypeName: "Type of insurance business",
		Packs: []string{"insurance"}, Types: types(
			"agent", "Insurance agent", "agency", "Insurance agency", "broker", "Insurance broker", "life", "Life insurance",
			"health", "Health insurance", "motor", "Motor insurance", "general", "General insurance", "corporate", "Corporate insurance")},
	{Key: "legal", Label: "Legal services", Keywords: "advocate lawyer law firm attorney legal court", TypeName: "Type of legal practice",
		Packs: []string{"legal"}, Types: types(
			"advocate", "Advocate", "lawyer", "Individual lawyer", "law_firm", "Law firm", "consultancy", "Legal consultancy",
			"corporate", "Corporate legal services", "property", "Property legal services", "family", "Family law",
			"criminal", "Criminal law", "civil", "Civil law")},
	{Key: "beauty", Label: "Beauty & salon", Keywords: "parlour salon spa makeup bridal barber", TypeName: "Type of salon",
		Packs: []string{"salon"}, Types: types(
			"parlour", "Beauty parlour", "unisex", "Unisex salon", "bridal", "Bridal makeup artist", "spa", "Spa",
			"barber", "Barber shop", "nails", "Nail studio")},
	{Key: "other", Label: OtherIndustry, TypeName: "Type of business"},
}

// FindIndustry finds an industry by its key or label, ignoring case.
func FindIndustry(v string) *Industry {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return nil
	}
	for i := range Industries {
		if Industries[i].Key == v || strings.ToLower(Industries[i].Label) == v {
			return &Industries[i]
		}
	}
	return nil
}

// FindType finds a business type of the industry by its key or label.
func (in *Industry) FindType(v string) *BusinessType {
	v = strings.ToLower(strings.TrimSpace(v))
	for i := range in.Types {
		if in.Types[i].Key == v || strings.ToLower(in.Types[i].Label) == v {
			return &in.Types[i]
		}
	}
	return nil
}

// Trade is a business's industry and business type as it is stored in its profile.
type Trade struct {
	Industry     string `json:"industry"`
	IndustryKey  string `json:"industryKey"`
	BusinessType string `json:"businessType"`
	TypeKey      string `json:"businessTypeKey"`
}

// WorkspaceTrade reads a business's industry and business type.
func WorkspaceTrade(ctx context.Context, q Querier, wsID uuid.UUID) Trade {
	var t Trade
	_ = q.QueryRow(ctx, `SELECT COALESCE(profile->>'industry', ''), COALESCE(profile->>'businessType', '') FROM crm.workspaces WHERE id = $1`, wsID).
		Scan(&t.Industry, &t.BusinessType)
	if in := FindIndustry(t.Industry); in != nil {
		t.IndustryKey = in.Key
		if bt := in.FindType(t.BusinessType); bt != nil {
			t.TypeKey = bt.Key
		}
	} else if strings.Contains(strings.ToLower(t.Industry), "legal") {
		t.IndustryKey = "legal"
	}
	return t
}

// Vehicles is what an automotive business sells: "bike", "car" or both. Anything not
// clearly one of them (commercial, EV, other, or no type picked yet) is both.
func (t Trade) Vehicles() (bike, car bool) {
	switch t.TypeKey {
	case "bike", "scooter", "motorcycle":
		return true, false
	case "car":
		return false, true
	}
	return true, true
}

// Term is a word an industry uses for one object, singular and plural.
type Term struct {
	Singular string `json:"singular"`
	Plural   string `json:"plural"`
}

// Terms is what this trade calls the objects whose usual name doesn't fit it. Only the
// labels change; the records, fields and API keys are the same for every business.
func (t Trade) Terms() map[string]Term {
	out := map[string]Term{}
	switch t.IndustryKey {
	case "automotive":
		// A dealer from before business types (no type picked) keeps the words it had.
		switch bike, car := t.Vehicles(); {
		case t.TypeKey == "":
		case car && !bike:
			out["test_rides"] = Term{"Test drive", "Test drives"}
		case car && bike:
			out["test_rides"] = Term{"Test ride / drive", "Test rides & drives"}
		}
	case "legal":
		out["contacts"] = Term{"Client", "Clients"}
		out["catalog_items"] = Term{"Legal service", "Legal services"}
		out["contracts"] = Term{"Engagement agreement", "Engagement agreements"}
		out["appointments"] = Term{"Consultation", "Consultations"}
		// A support case is not a legal matter: matters are their own records.
		out["cases"] = Term{"Support ticket", "Support tickets"}
	case "insurance":
		out["catalog_items"] = Term{"Insurance plan", "Insurance plans"}
	case "healthcare":
		out["contacts"] = Term{"Patient", "Patients"}
		out["catalog_items"] = Term{"Service / treatment", "Services & treatments"}
	case "education":
		out["catalog_items"] = Term{"Course", "Courses"}
	case "services", "technology":
		out["catalog_items"] = Term{"Product / service", "Products & services"}
	case "real_estate":
		out["contracts"] = Term{"Agreement", "Agreements"}
	}
	return out
}

// WorkspaceTerms is the wording of one business.
func WorkspaceTerms(ctx context.Context, q Querier, wsID uuid.UUID) map[string]Term {
	return WorkspaceTrade(ctx, q, wsID).Terms()
}

// navObjectKeys: the sidebar keys of the core objects, which use their module key.
var navObjectKeys = map[string]string{"leads": "leads", "accounts": "accounts", "contacts": "contacts"}

// WithTerms puts a business's own words on its sidebar.
func WithTerms(nav []NavItem, terms map[string]Term) []NavItem {
	if len(terms) == 0 {
		return nav
	}
	for i := range nav {
		key := nav[i].Key
		if k, ok := navObjectKeys[key]; ok {
			key = k
		}
		if t, ok := terms[key]; ok {
			nav[i].Label = t.Plural
		}
	}
	return nav
}
