package records

import (
	"context"

	"cardflow-backend/internal/crm/access"
	"github.com/google/uuid"
)

// The owner's Platform CRM leads are people who want a CRM of their own: sign-up requests
// from "Create your own CRM" and, later, enquiries from the landing page. Their form asks
// what the sign-up form asks — the person, the business, its tax ID and the plan — and
// nothing from any trade (no test rides, no policies, no matters).

// platformLeadExtra are the sign-up form's fields; only the platform's own CRM has them.
var platformLeadExtra = []Field{
	only(sel("teamSize", "Size of team", "", opts("Just me", "Just me", "2-10", "2-10", "11-50", "11-50", "51-200", "51-200", "201-500", "201-500", "500+", "500+")), "platform"),
	only(text("businessType", "Business type", ""), "platform"),
	only(typed("businessEmail", "Business email", "email", ""), "platform"),
	only(sel("taxIdType", "Tax ID type", "", opts("gstin", "GSTIN", "pan", "PAN", "tan", "TAN")), "platform"),
	only(text("taxId", "GSTIN, PAN or TAN", ""), "platform"),
	only(sel("requestType", "Enquiry type", "", opts("call", "Call request", "enquiry", "General enquiry", "signup", "Sign-up", "manual", "Added by us")), "platform"),
	// From the website's "Book a free call" form (D-181).
	only(help(sel("callStatus", "Call status", "", opts("requested", "Awaiting confirmation", "scheduled", "Call scheduled", "completed", "Call completed",
		"no_answer", "Not reachable", "cancelled", "Cancelled")), "Requested on the website: confirm the time with the visitor, then mark the call done."), "platform"),
	only(typed("preferredDate", "Preferred call date", "date", ""), "platform"),
	only(sel("preferredTime", "Preferred call time", "", opts("morning", "Morning (9 AM – 12 PM)", "afternoon", "Afternoon (12 – 4 PM)", "evening", "Evening (4 – 7 PM)", "any", "Any time")), "platform"),
	only(sel("currentMethod", "Manages leads today with", "", opts("paper", "Notebook / paper", "excel", "Excel sheets", "whatsapp", "WhatsApp", "memory", "Memory",
		"other_crm", "Another CRM", "other", "Something else")), "platform"),
	only(typed("challenge", "Main business challenge", "textarea", ""), "platform"),
	// From the website's "Send an enquiry" form.
	only(text("subject", "Subject", ""), "platform"),
	only(sel("enquiryCategory", "Enquiry category", "", opts("demo", "CRM demo", "pricing", "Pricing", "product", "Product information", "feature", "Feature request",
		"customization", "Customization", "technical", "Technical question", "partnership", "Partnership", "other", "Other")), "platform"),
	only(text("lookingFor", "Looking for", ""), "platform"),
	only(help(sel("plan", "Plan interested in", "", opts("1m", "1 month", "3m", "3 months", "6m", "6 months", "12m", "12 months")),
		"Price per person. Nothing is paid now: the plan is settled when you approve the business."), "platform"),
}

func init() {
	for _, f := range platformLeadExtra {
		f.Standard = true
		leadSpec.Fields = append(leadSpec.Fields, f)
	}
}

// platformLeadKeep: the lead fields the owner's CRM shows, in no particular order.
var platformLeadKeep = map[string]bool{
	"firstName": true, "lastName": true, "mobile": true, "email": true,
	"organization": true, "teamSize": true, "industry": true, "businessType": true, "phone": true, "businessEmail": true,
	"taxIdType": true, "taxId": true, "plan": true, "requestType": true, "lookingFor": true, "callStatus": true, "preferredDate": true, "preferredTime": true,
	"currentMethod": true, "challenge": true, "subject": true, "enquiryCategory": true,
	"status": true, "lostReason": true, "source": true, "description": true,
	"convertedAt": true, "convertedContactId": true, "identityId": true,
	"code": true, "ownerId": true, "createdBy": true, "createdAt": true, "updatedBy": true, "updatedAt": true,
}

var platformLeadLayout = Layout{
	Highlights: []string{"organization", "status", "requestType", "source", "mobile", "industry"},
	Sections: []Section{
		{ID: "person", Title: "Person", Columns: 2, Fields: []string{"firstName", "lastName", "mobile", "email"}},
		{ID: "business", Title: "Business", Columns: 2, Fields: []string{
			"organization", "teamSize", "industry", "businessType", "phone", "businessEmail", "taxIdType", "taxId"}},
		{ID: "request", Title: "Request", Columns: 2, Fields: []string{"requestType", "source", "callStatus", "preferredDate", "preferredTime",
			"subject", "enquiryCategory", "currentMethod", "challenge", "lookingFor"}},
		{ID: "plan", Title: "Plan & status", Columns: 2, Fields: []string{"plan", "status", "lostReason"}},
		{ID: "description", Title: "Notes", Columns: 1, Fields: []string{"description"}},
		{ID: "conversion", Title: "Approval", Columns: 2, Fields: []string{"convertedAt", "convertedContactId", "identityId"}},
		systemSection,
	},
}

var platformLeadColumns = []string{"organization", "mobile", "requestType", "source", "industry", "status", "ownerId", "createdAt"}

// workspaceIsPlatform: the owner's own CRM.
func workspaceIsPlatform(ctx context.Context, q querier, wsID uuid.UUID) bool {
	var is bool
	_ = q.QueryRow(ctx, `SELECT is_platform FROM crm.workspaces WHERE id = $1`, wsID).Scan(&is)
	return is
}

// platformLeadFields keeps the sign-up form's fields, in its words.
func platformLeadFields(fields []Field) []Field {
	industries := []Option{}
	for _, in := range access.Industries {
		industries = append(industries, Option{Value: in.Label, Label: in.Label})
	}
	out := fields[:0:0]
	for _, f := range fields {
		if !platformLeadKeep[f.Key] {
			continue
		}
		switch f.Key {
		case "organization":
			f.Label, f.Required, f.HelpText = "Business / brand name", true, ""
		case "mobile":
			f.Label, f.Required = "Mobile", true
			f.HelpText = "They sign in to their business with this number."
		case "phone":
			f.Label = "Business phone"
		case "email":
			f.Label = "Email"
		case "industry":
			f.Required, f.Options = true, industries
		case "taxId":
			f.Required = true
			f.HelpText = "One is enough. Its last 4 characters are part of the business URL."
		case "source":
			// Where the owner's leads come from, the website's two forms included (D-181).
			f.Options = append(append([]Option{}, f.Options...),
				Option{Value: "website_call", Label: "CRM Website - Book Free Call"},
				Option{Value: "website_enquiry", Label: "CRM Website - General Enquiry"})
		case "ownerId":
			f.Label = "Assigned to"
		case "convertedAt":
			f.Label = "Approved on"
		case "convertedContactId":
			f.Label = "Contact"
		case "identityId":
			f.Label = "Sign-in"
		}
		out = append(out, f)
	}
	return out
}
