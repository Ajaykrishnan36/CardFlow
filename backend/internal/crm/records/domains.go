package records

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
)

// A tenant's own domain, connected for it (D-157). When the business saves a domain the
// server adds it to the site's Vercel project (VERCEL_API_TOKEN, VERCEL_PROJECT_ID,
// optional VERCEL_TEAM_ID), so the only thing left is the one DNS record the domain's
// owner adds at their registrar; the page shows exactly which record and whether it is
// live yet. Without those settings the page shows the same record and says the platform
// owner connects the domain.
//
//	GET  /w/{code}/business/domain        status of the tenant's domain
//	POST /w/{code}/business/domain/check  ask again (after adding the record)

type dnsRecord struct {
	Type  string `json:"type"`  // A | CNAME | TXT
	Name  string `json:"name"`  // what to type in the registrar's "name / host" box
	Value string `json:"value"` // what it points to
	Why   string `json:"why,omitempty"`
}

type domainStatus struct {
	Domain    string      `json:"domain"`
	Automatic bool        `json:"automatic"` // this server adds the domain to the hosting by itself
	Added     bool        `json:"added"`     // the hosting knows the domain
	Live      bool        `json:"live"`      // DNS points here and the domain is verified
	Records   []dnsRecord `json:"records"`
	Message   string      `json:"message"`
}

type vercel struct {
	token, project, team, base string
}

func vercelFromEnv() *vercel {
	v := &vercel{token: strings.TrimSpace(os.Getenv("VERCEL_API_TOKEN")), project: strings.TrimSpace(os.Getenv("VERCEL_PROJECT_ID")),
		team: strings.TrimSpace(os.Getenv("VERCEL_TEAM_ID")), base: strings.TrimSpace(os.Getenv("VERCEL_API_URL"))}
	if v.base == "" {
		v.base = "https://api.vercel.com"
	}
	if v.token == "" || v.project == "" {
		return nil
	}
	return v
}

var vercelHTTP = &http.Client{Timeout: 10 * time.Second}

func (v *vercel) call(ctx context.Context, method, path string, body any) (int, map[string]any, error) {
	u := v.base + path
	if v.team != "" {
		sep := "?"
		if strings.Contains(u, "?") {
			sep = "&"
		}
		u += sep + "teamId=" + url.QueryEscape(v.team)
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+v.token)
	req.Header.Set("Content-Type", "application/json")
	res, err := vercelHTTP.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	out := map[string]any{}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&out)
	return res.StatusCode, out, nil
}

// add puts the domain on the project; "already there" counts as done.
func (v *vercel) add(ctx context.Context, domain string) error {
	st, out, err := v.call(ctx, http.MethodPost, "/v10/projects/"+url.PathEscape(v.project)+"/domains", map[string]string{"name": domain})
	if err != nil {
		return err
	}
	if st == 200 || st == 201 {
		return nil
	}
	if e, ok := out["error"].(map[string]any); ok {
		if code, _ := e["code"].(string); code == "domain_already_in_use" || code == "domain_already_exists" {
			return nil
		}
		return fmt.Errorf("%v", e["message"])
	}
	return fmt.Errorf("the hosting answered %d", st)
}

func (v *vercel) remove(ctx context.Context, domain string) {
	_, _, _ = v.call(ctx, http.MethodDelete, "/v9/projects/"+url.PathEscape(v.project)+"/domains/"+url.PathEscape(domain), nil)
}

// neededRecords: an apex domain (ajaytraders.in) points with an A record, a subdomain
// (crm.ajaytraders.in) with a CNAME; both go to Vercel.
func neededRecords(domain string) []dnsRecord {
	labels := strings.Split(domain, ".")
	apex := len(labels) == 2 || (len(labels) == 3 && len(labels[len(labels)-2]) <= 3 && len(labels[len(labels)-1]) == 2) // example.co.in
	if apex {
		return []dnsRecord{{Type: "A", Name: "@", Value: "76.76.21.21", Why: "Points your domain at the CRM."}}
	}
	host := strings.Join(labels[:len(labels)-2], ".")
	if len(labels) >= 4 && len(labels[len(labels)-2]) <= 3 && len(labels[len(labels)-1]) == 2 {
		host = strings.Join(labels[:len(labels)-3], ".")
	}
	return []dnsRecord{{Type: "CNAME", Name: host, Value: "cname.vercel-dns.com", Why: "Points this address at the CRM."}}
}

func (h *Handler) domainStatus(ctx context.Context, domain string) domainStatus {
	out := domainStatus{Domain: domain, Records: []dnsRecord{}}
	if domain == "" {
		out.Message = "No domain yet. Your team signs in at the address above."
		return out
	}
	out.Records = neededRecords(domain)
	v := vercelFromEnv()
	if v == nil {
		out.Message = "Add the record below at the company where you bought the domain. The platform owner then connects the domain to the CRM."
		return out
	}
	out.Automatic = true
	st, d, err := v.call(ctx, http.MethodGet, "/v9/projects/"+url.PathEscape(v.project)+"/domains/"+url.PathEscape(domain), nil)
	if err != nil || st == 404 {
		if err == nil {
			if aerr := v.add(ctx, domain); aerr == nil {
				st, d, err = v.call(ctx, http.MethodGet, "/v9/projects/"+url.PathEscape(v.project)+"/domains/"+url.PathEscape(domain), nil)
			}
		}
		if err != nil || st != 200 {
			out.Message = "Couldn't reach the hosting to connect the domain. Try “Check again” in a minute."
			return out
		}
	}
	out.Added = st == 200
	verified, _ := d["verified"].(bool)
	// A domain used on another Vercel account first has to prove it is ours (TXT record).
	if vs, ok := d["verification"].([]any); ok {
		for _, x := range vs {
			m, _ := x.(map[string]any)
			typ, _ := m["type"].(string)
			name, _ := m["domain"].(string)
			val, _ := m["value"].(string)
			if typ != "" && val != "" {
				out.Records = append(out.Records, dnsRecord{Type: typ, Name: strings.TrimSuffix(name, "."+domain), Value: val, Why: "Proves the domain is yours."})
			}
		}
	}
	_, cfg, err := v.call(ctx, http.MethodGet, "/v6/domains/"+url.PathEscape(domain)+"/config", nil)
	misconfigured := true
	if err == nil {
		if m, ok := cfg["misconfigured"].(bool); ok {
			misconfigured = m
		}
	}
	out.Live = verified && !misconfigured
	switch {
	case out.Live:
		out.Message = "Connected. Opening " + domain + " shows your sign-in page."
	case !verified:
		out.Message = "Waiting for the records below. They usually work within minutes; some registrars take up to a day."
	default:
		out.Message = "Waiting for DNS: add the record below at your domain's registrar, then press “Check again”."
	}
	return out
}

func (h *Handler) tenantDomain(r *http.Request) (string, error) {
	var d string
	err := h.store.Pool.QueryRow(r.Context(), `SELECT COALESCE(t.custom_domain, '') FROM crm.workspaces w LEFT JOIN crm.tenants t ON t.id = w.tenant_id WHERE w.id = $1`,
		scopeFrom(r.Context()).WS).Scan(&d)
	return d, err
}

func (h *Handler) domainRoutes(r chi.Router) {
	show := func(w http.ResponseWriter, r *http.Request) {
		d, err := h.tenantDomain(r)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		shared.WriteJSON(w, http.StatusOK, h.domainStatus(r.Context(), d))
	}
	r.Get("/business/domain", show)
	r.Post("/business/domain/check", func(w http.ResponseWriter, r *http.Request) {
		if !canEditBusiness(scopeFrom(r.Context())) {
			shared.WriteError(w, r, errForbidden)
			return
		}
		show(w, r)
	})
}

// domainChanged connects a newly saved domain and disconnects the old one (best effort:
// the status call retries, and saving the profile never fails because of the hosting).
func (h *Handler) domainChanged(ctx context.Context, before, after string) {
	v := vercelFromEnv()
	if v == nil || before == after {
		return
	}
	if before != "" {
		v.remove(ctx, before)
	}
	if after != "" {
		_ = v.add(ctx, after)
	}
}
