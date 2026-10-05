// Package records is the metadata-driven record engine for leads, accounts and
// contacts (PRD §5 data model engine, §6 CRM features). Standard fields are declared
// here once; custom fields and page layouts live in the database, so the same code
// renders, validates and stores every object.
package records

type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

type StatusOption struct {
	Option
	Tone string `json:"tone"`
}

// Field is both the API field definition and, for standard fields, the column mapping.
type Field struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	ReadOnly bool     `json:"readOnly"`
	Standard bool     `json:"standard"`
	Options  []Option `json:"options,omitempty"`
	Lookup   string   `json:"lookup,omitempty"` // lookup / relations: the object linked to
	HelpText string   `json:"helpText,omitempty"`
	Unique   bool     `json:"unique,omitempty"` // no two records may share a value

	column string // standard fields stored in a column; "" = stored in custom jsonb
	isInt  bool   // int column (numbers are rounded)
	system bool   // maintained by the engine (never written from input)
}

type Section struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Columns int      `json:"columns"`
	Fields  []string `json:"fields"`
}

type Layout struct {
	Highlights []string  `json:"highlights"`
	Sections   []Section `json:"sections"`
}

type objectSpec struct {
	Key         string
	Table       string
	Singular    string
	Plural      string
	Prefix      string
	TitleSQL    string // SQL expression (table alias t) for the record title
	SearchSQL   []string
	Fields      []Field
	ListColumns []string
	StatusField string
	Statuses    []StatusOption
	Layout      Layout
	Icon        string // objects defined as data (D-45)
	Custom      bool
	order       int
}

func (o *objectSpec) field(key string) (Field, bool) {
	for _, f := range o.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

// ---- shared option sets ----

func opts(pairs ...string) []Option {
	out := make([]Option, 0, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		out = append(out, Option{Value: pairs[i], Label: pairs[i+1]})
	}
	return out
}

var (
	salutations = opts("Mr.", "Mr.", "Ms.", "Ms.", "Mrs.", "Mrs.", "Dr.", "Dr.", "Prof.", "Prof.")
	sources     = opts("manual", "Manual entry", "web", "Website", "phone", "Phone inquiry", "email", "Email", "referral", "Referral",
		"partner", "Partner", "event", "Event", "social", "Social media", "advertisement", "Advertisement", "business_card", "Business card", "directory", "Business directory", "app_signup", "App sign-up", "other", "Other")
	ratings    = opts("hot", "Hot", "warm", "Warm", "cold", "Cold")
	industries = opts("agriculture", "Agriculture", "automotive", "Automotive", "banking", "Banking & finance", "construction", "Construction",
		"consulting", "Consulting", "education", "Education", "energy", "Energy & utilities", "entertainment", "Media & entertainment",
		"government", "Government", "healthcare", "Healthcare", "hospitality", "Hospitality", "insurance", "Insurance",
		"manufacturing", "Manufacturing", "nonprofit", "Non-profit", "real_estate", "Real estate", "retail", "Retail & e-commerce",
		"technology", "Technology & software", "telecom", "Telecommunications", "transport", "Transport & logistics", "other", "Other")
	roleOptions = opts("SUPER_ADMIN", "Super Admin", "ADMIN", "Admin", "STAFF", "Staff", "END_USER", "End user")
	leadStatus  = []StatusOption{
		{Option{"new", "New"}, "primary"}, {Option{"working", "Working"}, "warning"}, {Option{"qualified", "Qualified"}, "success"},
		{Option{"converted", "Converted"}, "success"}, {Option{"lost", "Closed lost"}, "danger"},
	}
	accountLifecycle = []StatusOption{
		{Option{"prospect", "Prospect"}, "neutral"}, {Option{"onboarding", "Onboarding"}, "primary"},
		{Option{"active", "Active customer"}, "success"}, {Option{"churned", "Churned"}, "danger"},
	}
)

func statusOptions(s []StatusOption) []Option {
	out := make([]Option, len(s))
	for i, o := range s {
		out[i] = o.Option
	}
	return out
}

func text(key, label, column string) Field {
	return Field{Key: key, Label: label, Type: "text", column: column}
}

func typed(key, label, typ, column string) Field {
	return Field{Key: key, Label: label, Type: typ, column: column}
}

func sel(key, label, column string, o []Option) Field {
	return Field{Key: key, Label: label, Type: "select", column: column, Options: o}
}

func lookup(key, label, column, target string) Field {
	return Field{Key: key, Label: label, Type: "lookup", column: column, Lookup: target}
}

func req(f Field) Field  { f.Required = true; return f }
func ro(f Field) Field   { f.ReadOnly = true; f.system = true; return f }
func intf(f Field) Field { f.isInt = true; return f }
func help(f Field, h string) Field {
	f.HelpText = h
	return f
}

// systemFields are present on every object.
func systemFields() []Field {
	return []Field{
		ro(text("code", "Record ID", "code")),
		// The owner can be changed (reassign / "change owner"); it must be a member of the workspace.
		help(lookup("ownerId", "Owner", "owner_id", "users"), "Who this record belongs to. Records are shared by owner and role."),
		ro(typed("createdAt", "Created", "datetime", "created_at")),
		ro(lookup("createdBy", "Created by", "created_by", "users")),
		ro(typed("updatedAt", "Last modified", "datetime", "updated_at")),
		ro(lookup("updatedBy", "Last modified by", "updated_by", "users")),
	}
}

func withSystem(fields ...Field) []Field {
	out := make([]Field, 0, len(fields)+6)
	for _, f := range fields {
		f.Standard = true
		out = append(out, f)
	}
	for _, f := range systemFields() {
		f.Standard = true
		out = append(out, f)
	}
	return out
}

var systemSection = Section{ID: "system", Title: "System information", Columns: 2,
	Fields: []string{"code", "ownerId", "createdBy", "createdAt", "updatedBy", "updatedAt"}}

// ---- objects (Salesforce-style default layouts: "maximum" fields placed, owners hide what they don't use) ----

var leadSpec = objectSpec{
	Key: "leads", Table: "leads", Singular: "Lead", Plural: "Leads", Prefix: "L",
	TitleSQL:  `COALESCE(NULLIF(trim(concat_ws(' ', t.first_name, t.last_name)), ''), t.organization, t.code)`,
	SearchSQL: []string{"t.first_name", "t.last_name", "t.organization", "t.email", "t.phone", "t.mobile", "t.code", "concat_ws(' ', t.first_name, t.last_name)"},
	Fields: withSystem(
		sel("salutation", "Salutation", "salutation", salutations),
		text("firstName", "First name", "first_name"),
		req(text("lastName", "Last name", "last_name")),
		text("title", "Job title", "title"),
		help(text("organization", "Company", "organization"), "Becomes the account name when the lead is converted."),
		typed("email", "Email", "email", "email"),
		typed("phone", "Phone", "phone", "phone"),
		typed("mobile", "Mobile", "phone", "mobile"),
		typed("website", "Website", "url", "website"),
		sel("status", "Lead status", "status", statusOptions(leadStatus)),
		help(text("lostReason", "Lost reason", "lost_reason"), "Required when the status is Closed lost."),
		sel("source", "Lead source", "source", sources),
		sel("rating", "Rating", "rating", ratings),
		sel("industry", "Industry", "industry", industries),
		lookup("productId", "Interested product", "product_id", "products"),
		typed("annualRevenue", "Annual revenue", "currency", "annual_revenue"),
		intf(typed("employees", "No. of employees", "number", "employees")),
		text("userType", "User type", "user_type"),
		help(sel("intendedRoleKey", "Access role after invitation", "intended_role_key", roleOptions), "Used when this lead is given a login on conversion."),
		intf(typed("score", "Lead score", "number", "score")),
		typed("nextFollowUpAt", "Next follow-up", "datetime", "next_follow_up_at"),
		typed("tags", "Tags", "multiselect", "tags"),
		text("street", "Street", "street"),
		text("city", "City", "city"),
		text("state", "State / province", "state"),
		text("postalCode", "Postal code", "postal_code"),
		text("country", "Country", "country"),
		typed("description", "Description", "textarea", "description"),
		ro(typed("convertedAt", "Converted on", "datetime", "converted_at")),
		ro(lookup("convertedAccountId", "Converted account", "converted_account_id", "accounts")),
		ro(lookup("convertedContactId", "Converted contact", "converted_contact_id", "contacts")),
		ro(lookup("identityId", "User login", "identity_id", "users")),
	),
	ListColumns: []string{"organization", "email", "phone", "status", "source", "ownerId", "createdAt"},
	StatusField: "status",
	Statuses:    leadStatus,
	Layout: Layout{
		Highlights: []string{"organization", "status", "email", "phone", "source", "ownerId"},
		Sections: []Section{
			{ID: "lead_info", Title: "Lead information", Columns: 2, Fields: []string{
				"salutation", "status", "firstName", "source", "lastName", "rating", "title", "productId",
				"organization", "industry", "email", "website", "phone", "mobile", "lostReason"}},
			{ID: "qualification", Title: "Qualification", Columns: 2, Fields: []string{
				"annualRevenue", "employees", "score", "nextFollowUpAt", "userType", "intendedRoleKey", "tags"}},
			{ID: "address", Title: "Address information", Columns: 2, Fields: []string{"street", "city", "state", "postalCode", "country"}},
			{ID: "description", Title: "Description", Columns: 1, Fields: []string{"description"}},
			{ID: "conversion", Title: "Conversion", Columns: 2, Fields: []string{"convertedAt", "convertedAccountId", "convertedContactId", "identityId"}},
			systemSection,
		},
	},
}

var accountSpec = objectSpec{
	Key: "accounts", Table: "accounts", Singular: "Account", Plural: "Accounts", Prefix: "A",
	TitleSQL:  `t.name`,
	SearchSQL: []string{"t.name", "t.email", "t.phone", "t.website", "t.code"},
	Fields: withSystem(
		req(text("name", "Account name", "name")),
		sel("kind", "Account kind", "kind", opts("business", "Business", "individual", "Individual")),
		sel("type", "Type", "type", opts("prospect", "Prospect", "customer", "Customer", "vendor", "Vendor", "supplier", "Supplier", "partner", "Partner", "reseller", "Reseller", "distributor", "Distributor", "competitor", "Competitor", "other", "Other")),
		sel("lifecycle", "Lifecycle stage", "lifecycle", statusOptions(accountLifecycle)),
		sel("industry", "Industry", "industry", industries),
		sel("rating", "Rating", "rating", ratings),
		sel("ownership", "Ownership", "ownership", opts("public", "Public", "private", "Private", "subsidiary", "Subsidiary", "government", "Government", "other", "Other")),
		lookup("parentAccountId", "Parent account", "parent_account_id", "accounts"),
		typed("website", "Website", "url", "website"),
		typed("email", "Email", "email", "email"),
		typed("phone", "Phone", "phone", "phone"),
		typed("annualRevenue", "Annual revenue", "currency", "annual_revenue"),
		intf(typed("employees", "Employees", "number", "employees")),
		text("billingStreet", "Billing street", "billing_street"),
		text("billingCity", "Billing city", "billing_city"),
		text("billingState", "Billing state / province", "billing_state"),
		text("billingPostalCode", "Billing postal code", "billing_postal_code"),
		text("billingCountry", "Billing country", "billing_country"),
		text("shippingStreet", "Shipping street", "shipping_street"),
		text("shippingCity", "Shipping city", "shipping_city"),
		text("shippingState", "Shipping state / province", "shipping_state"),
		text("shippingPostalCode", "Shipping postal code", "shipping_postal_code"),
		text("shippingCountry", "Shipping country", "shipping_country"),
		typed("description", "Description", "textarea", "description"),
		ro(lookup("customerWorkspaceId", "Customer product", "customer_workspace_id", "workspaces")),
		ro(lookup("identityId", "Primary login", "identity_id", "users")),
	),
	ListColumns: []string{"type", "lifecycle", "industry", "phone", "ownerId", "createdAt"},
	StatusField: "lifecycle",
	Statuses:    accountLifecycle,
	Layout: Layout{
		Highlights: []string{"type", "lifecycle", "industry", "phone", "website", "ownerId"},
		Sections: []Section{
			{ID: "account_info", Title: "Account information", Columns: 2, Fields: []string{
				"name", "rating", "kind", "phone", "type", "email", "lifecycle", "website", "industry", "parentAccountId",
				"ownership", "employees", "annualRevenue"}},
			{ID: "address", Title: "Address information", Columns: 2, Fields: []string{
				"billingStreet", "shippingStreet", "billingCity", "shippingCity", "billingState", "shippingState",
				"billingPostalCode", "shippingPostalCode", "billingCountry", "shippingCountry"}},
			{ID: "description", Title: "Description", Columns: 1, Fields: []string{"description"}},
			{ID: "access", Title: "CRM access", Columns: 2, Fields: []string{"customerWorkspaceId", "identityId"}},
			systemSection,
		},
	},
}

var contactSpec = objectSpec{
	Key: "contacts", Table: "contacts", Singular: "Contact", Plural: "Contacts", Prefix: "C",
	TitleSQL:  `COALESCE(NULLIF(trim(concat_ws(' ', t.first_name, t.last_name)), ''), t.email, t.code)`,
	SearchSQL: []string{"t.first_name", "t.last_name", "t.email", "t.phone", "t.mobile", "t.code", "concat_ws(' ', t.first_name, t.last_name)"},
	Fields: withSystem(
		sel("salutation", "Salutation", "salutation", salutations),
		text("firstName", "First name", "first_name"),
		req(text("lastName", "Last name", "last_name")),
		lookup("accountId", "Account", "account_id", "accounts"),
		text("title", "Title", "title"),
		text("department", "Department", "department"),
		typed("email", "Email", "email", "email"),
		typed("phone", "Phone", "phone", "phone"),
		typed("mobile", "Mobile", "phone", "mobile"),
		sel("leadSource", "Lead source", "lead_source", sources),
		typed("birthdate", "Birthdate", "date", "birthdate"),
		text("mailingStreet", "Mailing street", "mailing_street"),
		text("mailingCity", "Mailing city", "mailing_city"),
		text("mailingState", "Mailing state / province", "mailing_state"),
		text("mailingPostalCode", "Mailing postal code", "mailing_postal_code"),
		text("mailingCountry", "Mailing country", "mailing_country"),
		typed("description", "Description", "textarea", "description"),
		ro(lookup("identityId", "User login", "identity_id", "users")),
	),
	ListColumns: []string{"accountId", "title", "email", "phone", "ownerId", "createdAt"},
	Layout: Layout{
		Highlights: []string{"accountId", "title", "email", "phone", "ownerId"},
		Sections: []Section{
			{ID: "contact_info", Title: "Contact information", Columns: 2, Fields: []string{
				"salutation", "phone", "firstName", "mobile", "lastName", "email", "accountId", "leadSource", "title", "birthdate", "department"}},
			{ID: "address", Title: "Address information", Columns: 2, Fields: []string{
				"mailingStreet", "mailingCity", "mailingState", "mailingPostalCode", "mailingCountry"}},
			{ID: "description", Title: "Description", Columns: 1, Fields: []string{"description"}},
			{ID: "access", Title: "CRM access", Columns: 2, Fields: []string{"identityId"}},
			systemSection,
		},
	},
}

var specs = map[string]*objectSpec{"leads": &leadSpec, "accounts": &accountSpec, "contacts": &contactSpec}

// inColumn reports whether a field's value lives in its own column (built-in standard
// fields) rather than the custom jsonb (custom fields and fields of objects defined as data).
func (f Field) inColumn() bool { return f.Standard && f.column != "" }
