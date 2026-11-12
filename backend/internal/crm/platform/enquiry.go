package platform

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// The website (D-181): "Book a free call" and "Send an enquiry" each file a lead in the
// owner's Platform CRM — never in a customer's business — marked with where it came from
// (source) and what was asked for (requestType). No sign-in. A visitor who already has an
// open lead is not filed twice: the new request is added to that lead and noted on it.

// Lead sources the website writes; the owner's lead form lists them (records/platformlead.go).
const (
	SourceWebsiteCall    = "website_call"    // "CRM Website - Book Free Call"
	SourceWebsiteEnquiry = "website_enquiry" // "CRM Website - General Enquiry"
)

// The website's choices, so a request can only carry values the owner's lead form knows.
var (
	enquiryCategories = map[string]bool{"demo": true, "pricing": true, "product": true, "feature": true, "customization": true, "technical": true, "partnership": true, "other": true}
	leadMethods       = map[string]bool{"paper": true, "excel": true, "whatsapp": true, "other_crm": true, "memory": true, "other": true}
	timeSlots         = map[string]bool{"morning": true, "afternoon": true, "evening": true, "any": true}
	categoryNames     = map[string]string{"demo": "CRM demo", "pricing": "Pricing", "product": "Product information", "feature": "Feature request", "customization": "Customization", "technical": "Technical question", "partnership": "Partnership", "other": "Other"}
	slotNames         = map[string]string{"morning": "Morning (9 AM – 12 PM)", "afternoon": "Afternoon (12 – 4 PM)", "evening": "Evening (4 – 7 PM)", "any": "Any time"}
	emailShape        = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

type websiteRequest struct {
	Kind          string `json:"kind"` // call | enquiry
	Name          string `json:"name"`
	Business      string `json:"business"`
	Phone         string `json:"phone"`
	Email         string `json:"email"`
	Industry      string `json:"industry"`
	TeamSize      string `json:"teamSize"`
	CurrentMethod string `json:"currentMethod"`
	Challenge     string `json:"challenge"`
	PreferredDate string `json:"preferredDate"` // YYYY-MM-DD
	PreferredTime string `json:"preferredTime"` // morning | afternoon | evening | any
	Subject       string `json:"subject"`
	Category      string `json:"category"`
	Message       string `json:"message"`
	Website       string `json:"website"` // a trap: people never see this box
}

// POST /public/enquiries
func (h *Handler) handleWebsiteEnquiry(w http.ResponseWriter, r *http.Request) {
	var in websiteRequest
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if strings.TrimSpace(in.Website) != "" { // a robot filled the hidden box: thank it, keep nothing
		shared.WriteJSON(w, http.StatusOK, map[string]any{"received": true})
		return
	}
	phone, fe := in.normalize()
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	ctx := r.Context()
	var platformWS uuid.UUID
	if err := h.store.Pool.QueryRow(ctx, `SELECT id FROM crm.workspaces WHERE is_platform ORDER BY created_at LIMIT 1`).Scan(&platformWS); err != nil {
		slog.Error("crm website: no platform CRM to file the request in", "error", err)
		shared.WriteError(w, r, shared.ServiceUnavailable("not_ready", "We couldn't take your request right now. Please try again in a little while."))
		return
	}
	// Kept in the database so it holds on any number of servers: a ceiling on the forms per
	// visitor address and in all, every ten minutes.
	ip := shared.ClientIP(r)
	var fromIP, recent int
	if err := h.store.Pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE custom->>'requestIp' = $2), count(*)
		FROM crm.leads WHERE workspace_id = $1 AND source IN ('website_call', 'website_enquiry') AND updated_at > now() - interval '10 minutes'`,
		platformWS, ip).Scan(&fromIP, &recent); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if fromIP >= 5 || recent >= 120 {
		shared.WriteError(w, r, shared.TooManyAttempts(600))
		return
	}
	res, err := h.fileWebsiteRequest(ctx, platformWS, phone, ip, in)
	if err != nil {
		slog.Error("crm website: request not saved", "kind", in.Kind, "error", err)
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusCreated, map[string]any{"received": true, "reference": res})
}

// normalize tidies the request and reports what is missing or wrong.
func (in *websiteRequest) normalize() (string, map[string]string) {
	fe := map[string]string{}
	tidy := func(s string, max int) string { return clip(strings.Join(strings.Fields(s), " "), max) }
	if in.Kind != "call" {
		in.Kind = "enquiry"
	}
	in.Name, in.Business, in.Subject = tidy(in.Name, 120), tidy(in.Business, 160), tidy(in.Subject, 160)
	in.Challenge, in.Message = clip(strings.TrimSpace(in.Challenge), 1000), clip(strings.TrimSpace(in.Message), 2000)
	if len(in.Name) < 2 {
		fe["name"] = "Enter your name."
	}
	if len(in.Business) < 2 {
		fe["business"] = "Enter your business name."
	}
	phone, ok := identity.NormalizePhone(in.Phone)
	if !ok {
		fe["phone"] = "Enter a valid mobile number."
	}
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	if in.Email != "" {
		if e, ok := identity.NormalizeEmail(in.Email); ok && emailShape.MatchString(e) {
			in.Email = e
		} else {
			fe["email"] = "Enter a valid email address."
		}
	}
	if ind := access.FindIndustry(strings.TrimSpace(in.Industry)); ind != nil {
		in.Industry = ind.Label
	} else if strings.TrimSpace(in.Industry) != "" || in.Kind == "call" {
		fe["industry"] = "Choose your industry."
	}
	if in.TeamSize = strings.TrimSpace(in.TeamSize); in.TeamSize != "" && !TeamSizes[in.TeamSize] {
		fe["teamSize"] = "Choose the size of your team."
	}
	if in.CurrentMethod = strings.TrimSpace(in.CurrentMethod); in.CurrentMethod != "" && !leadMethods[in.CurrentMethod] {
		fe["currentMethod"] = "Choose how you manage leads today."
	}
	switch in.Kind {
	case "call":
		in.Subject, in.Category = "", ""
		if in.Email == "" {
			fe["email"] = "Enter your email address."
		}
		if in.TeamSize == "" {
			fe["teamSize"] = "Choose the size of your team."
		}
		if d, err := time.Parse("2006-01-02", strings.TrimSpace(in.PreferredDate)); err != nil {
			fe["preferredDate"] = "Choose a date for the call."
		} else if today := time.Now().In(ist()).Format("2006-01-02"); d.Format("2006-01-02") < today || d.After(time.Now().AddDate(0, 3, 0)) {
			fe["preferredDate"] = "Choose a date from today up to three months ahead."
		} else {
			in.PreferredDate = d.Format("2006-01-02")
		}
		if in.PreferredTime = strings.TrimSpace(in.PreferredTime); !timeSlots[in.PreferredTime] {
			fe["preferredTime"] = "Choose a time for the call."
		}
	default:
		in.PreferredDate, in.PreferredTime, in.TeamSize, in.CurrentMethod, in.Challenge = "", "", "", "", ""
		if len(in.Subject) < 3 {
			fe["subject"] = "Enter a subject."
		}
		if in.Category = strings.TrimSpace(in.Category); !enquiryCategories[in.Category] {
			fe["category"] = "Choose what your enquiry is about."
		}
		if len(in.Message) < 5 {
			fe["message"] = "Write your message."
		}
	}
	return phone, fe
}

// fileWebsiteRequest saves the request as a lead (or onto the visitor's open lead) and tells
// the owner. Returns the lead's code.
func (h *Handler) fileWebsiteRequest(ctx context.Context, platformWS uuid.UUID, phone, ip string, in websiteRequest) (string, error) {
	source := SourceWebsiteEnquiry
	if in.Kind == "call" {
		source = SourceWebsiteCall
	}
	fields := map[string]any{"requestType": in.Kind, "requestIp": ip}
	set := func(k, v string) {
		if v != "" {
			fields[k] = v
		}
	}
	set("teamSize", in.TeamSize)
	set("currentMethod", in.CurrentMethod)
	set("challenge", in.Challenge)
	set("preferredDate", in.PreferredDate)
	set("preferredTime", in.PreferredTime)
	set("subject", in.Subject)
	set("enquiryCategory", in.Category)
	if in.Kind == "call" {
		fields["callStatus"] = "requested"
	}
	summary := in.summary()
	var code, title string
	var leadID uuid.UUID
	var owner *uuid.UUID
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, "website:"+phone); err != nil {
			return err
		}
		if err := tx.QueryRow(ctx, `SELECT id FROM crm.identities WHERE is_platform_owner AND status = 'active' ORDER BY created_at LIMIT 1`).Scan(&owner); err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		// The visitor's open lead, if any: the request is added to it, not filed twice.
		var existingType string
		err := tx.QueryRow(ctx, `
			SELECT id, code, COALESCE(custom->>'requestType', '') FROM crm.leads
			WHERE workspace_id = $1 AND mobile = $2 AND deleted_at IS NULL AND status NOT IN ('converted', 'lost')
			ORDER BY updated_at DESC LIMIT 1 FOR UPDATE`, platformWS, phone).Scan(&leadID, &code, &existingType)
		switch {
		case err == nil:
			// A call request is the stronger ask: it becomes a call request. An enquiry on a
			// call request keeps it one, and adds its subject and message.
			if existingType == "call" && in.Kind != "call" {
				delete(fields, "requestType")
				source = ""
			}
			raw, _ := json.Marshal(fields)
			if _, err := tx.Exec(ctx, `
				UPDATE crm.leads SET custom = COALESCE(custom, '{}') || $2::jsonb, source = COALESCE(NULLIF($3, ''), source),
				       email = COALESCE(email, NULLIF($4, '')), organization = COALESCE(organization, NULLIF($5, '')), industry = COALESCE(industry, NULLIF($6, '')),
				       version = version + 1, updated_at = now()
				WHERE id = $1`, leadID, raw, source, in.Email, in.Business, in.Industry); err != nil {
				return err
			}
			title = "Another website request from " + in.Name
		case errors.Is(err, pgx.ErrNoRows):
			var n int64
			if err := tx.QueryRow(ctx, `
				INSERT INTO crm.code_counters (workspace_id, prefix, next_value) VALUES ($1, 'L', 2)
				ON CONFLICT (workspace_id, prefix) DO UPDATE SET next_value = crm.code_counters.next_value + 1
				RETURNING next_value - 1`, platformWS).Scan(&n); err != nil {
				return err
			}
			code = fmt.Sprintf("L-%06d", n)
			first, last := "", in.Name
			if i := strings.LastIndex(in.Name, " "); i > 0 {
				first, last = in.Name[:i], in.Name[i+1:]
			}
			raw, _ := json.Marshal(fields)
			if err := tx.QueryRow(ctx, `
				INSERT INTO crm.leads (workspace_id, code, first_name, last_name, organization, email, mobile, industry, source, status, description,
				                       user_type, owner_id, created_by, updated_by, custom)
				VALUES ($1, $2, NULLIF($3, ''), $4, $5, NULLIF($6, ''), $7, NULLIF($8, ''), $9, 'new', NULLIF($10, ''), 'business', $11, $11, $11, $12)
				RETURNING id`, platformWS, code, first, last, in.Business, in.Email, phone, in.Industry, source, in.Message, owner, raw).Scan(&leadID); err != nil {
				return err
			}
			title = map[string]string{"call": "New call request from ", "enquiry": "New website enquiry from "}[in.Kind] + in.Name
		default:
			return err
		}
		// What was asked, on the lead's timeline.
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.activities (workspace_id, object_key, record_id, lead_id, kind, title, detail, source)
			VALUES ($1, 'leads', $2, $2, 'website.request', $3, $4, 'website')`,
			platformWS, leadID, map[string]string{"call": "Asked for a free call on the website", "enquiry": "Sent an enquiry on the website"}[in.Kind],
			mustJSON(map[string]any{"kind": in.Kind, "summary": summary, "fields": fields})); err != nil {
			return err
		}
		if owner != nil {
			if _, err := tx.Exec(ctx, `INSERT INTO crm.notifications (workspace_id, identity_id, kind, title, body, link) VALUES ($1, $2, 'website_request', $3, $4, $5)`,
				platformWS, *owner, title, clip(in.Business+" · "+phone+" — "+summary, 500), "/crm/owner/leads/"+leadID.String()); err != nil {
				return err
			}
		}
		return nil
	})
	return code, err
}

// summary is the request in a few lines, for the timeline and the owner's notification.
func (in websiteRequest) summary() string {
	parts := []string{}
	add := func(label, v string) {
		if v != "" {
			parts = append(parts, label+": "+v)
		}
	}
	if in.Kind == "call" {
		when := in.PreferredDate
		if d, err := time.Parse("2006-01-02", in.PreferredDate); err == nil {
			when = d.Format("Mon, 2 Jan 2006")
		}
		add("Preferred call", when+", "+slotNames[in.PreferredTime])
		add("Team size", in.TeamSize)
		add("Leads managed with", in.CurrentMethod)
		add("Main challenge", in.Challenge)
	} else {
		add("Subject", in.Subject)
		add("About", categoryNames[in.Category])
	}
	add("Industry", in.Industry)
	add("Message", in.Message)
	return strings.Join(parts, "\n")
}

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

func ist() *time.Location {
	if l, err := time.LoadLocation("Asia/Kolkata"); err == nil {
		return l
	}
	return time.FixedZone("IST", 5*3600+1800)
}

func clip(s string, max int) string {
	if len(s) > max {
		return s[:max]
	}
	return s
}
