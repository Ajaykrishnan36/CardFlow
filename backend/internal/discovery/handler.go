package discovery

import (
	"fmt"
	"net/http"
	"strconv"

	"cogniz-crm-backend/pkg/response"
	"github.com/go-chi/chi/v5"
)

type DiscoveryHandler struct {
	svc *DiscoveryService
}

func NewDiscoveryHandler(svc *DiscoveryService) *DiscoveryHandler {
	return &DiscoveryHandler{svc: svc}
}

func (h *DiscoveryHandler) GetCategories(w http.ResponseWriter, r *http.Request) {
	categories, err := h.svc.GetCategories(r.Context())
	if err != nil {
		response.InternalServerError(w, "failed to load categories")
		return
	}
	response.JSON(w, http.StatusOK, categories)
}

func (h *DiscoveryHandler) SearchBusinesses(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query().Get("q")
	catID := r.URL.Query().Get("category_id")
	lat, _ := strconv.ParseFloat(r.URL.Query().Get("lat"), 64)
	lng, _ := strconv.ParseFloat(r.URL.Query().Get("lng"), 64)
	radius, _ := strconv.ParseFloat(r.URL.Query().Get("radius_km"), 64)
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	params := SearchParams{
		Query:      q,
		CategoryID: catID,
		Latitude:   lat,
		Longitude:  lng,
		RadiusKm:   radius,
		Limit:      limit,
		Offset:     offset,
	}

	results, err := h.svc.SearchBusinesses(r.Context(), params)
	if err != nil {
		response.InternalServerError(w, "search query failed")
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"businesses": results,
		"count":      len(results),
	})
}

func (h *DiscoveryHandler) GetBusiness(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	biz, err := h.svc.GetBusinessByIDOrSlug(r.Context(), id)
	if err != nil {
		response.NotFound(w, "business listing not found")
		return
	}
	response.JSON(w, http.StatusOK, biz)
}

func (h *DiscoveryHandler) GetBusinessBySlug(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	biz, err := h.svc.GetBusinessByIDOrSlug(r.Context(), slug)
	if err != nil {
		response.NotFound(w, "business profile not found")
		return
	}
	response.JSON(w, http.StatusOK, biz)
}

// RenderPublicHTML renders a high-performance, mobile-responsive, SEO-friendly HTML profile
func (h *DiscoveryHandler) RenderPublicHTML(w http.ResponseWriter, r *http.Request) {
	slug := chi.URLParam(r, "slug")
	biz, err := h.svc.GetBusinessByIDOrSlug(r.Context(), slug)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	servicesHTML := ""
	for _, s := range biz.Services {
		servicesHTML += fmt.Sprintf(`<span class="tag">%s</span>`, s)
	}

	phoneStr := "Not available"
	if len(biz.Phones) > 0 {
		phoneStr = biz.Phones[0]
	}

	html := fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>%s — CardFlow Digital Business Profile</title>
  <meta name="description" content="%s">
  <style>
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; background: #0F172A; color: #F8FAFC; margin: 0; padding: 20px; display: flex; justify-content: center; }
    .card { background: #1E293B; border: 1px solid #334155; border-radius: 20px; padding: 32px; max-width: 520px; width: 100%%; box-shadow: 0 25px 50px -12px rgba(0, 0, 0, 0.5); }
    .badge { background: #1E3A8A; color: #60A5FA; font-size: 12px; font-weight: 700; padding: 4px 10px; border-radius: 999px; text-transform: uppercase; display: inline-block; margin-bottom: 12px; }
    h1 { font-size: 24px; margin: 0 0 8px 0; color: #FFFFFF; }
    .location { color: #94A3B8; font-size: 14px; margin-bottom: 20px; }
    .desc { color: #CBD5E1; font-size: 15px; line-height: 1.6; margin-bottom: 24px; }
    .section-title { font-size: 13px; font-weight: 700; color: #94A3B8; text-transform: uppercase; margin-bottom: 10px; }
    .tags { display: flex; flex-wrap: wrap; gap: 8px; margin-bottom: 24px; }
    .tag { background: #334155; color: #E2E8F0; padding: 6px 12px; border-radius: 8px; font-size: 13px; font-weight: 500; }
    .btn { display: block; text-align: center; background: #2563EB; color: #FFF; padding: 14px; border-radius: 12px; text-decoration: none; font-weight: 600; font-size: 16px; margin-top: 10px; }
    .btn-secondary { background: #334155; color: #F8FAFC; }
    .footer { text-align: center; margin-top: 24px; font-size: 12px; color: #64748B; }
  </style>
</head>
<body>
  <div class="card">
    <span class="badge">%s • %s VERIFIED</span>
    <h1>%s</h1>
    <div class="location">📍 %s, %s, %s</div>
    <div class="desc">%s</div>
    <div class="section-title">Key Services</div>
    <div class="tags">%s</div>
    <a class="btn" href="tel:%s">📞 Call Business (%s)</a>
    <a class="btn btn-secondary" href="https://wa.me/%s?text=Hi, I found %s on CardFlow" target="_blank">💬 WhatsApp Enquiry</a>
    <div class="footer">Powered by CardFlow Business Discovery</div>
  </div>
</body>
</html>`,
		biz.Name, biz.Description,
		biz.PrimaryCategory, biz.Verification,
		biz.Name,
		biz.AddressLine1, biz.City, biz.State,
		biz.Description,
		servicesHTML,
		phoneStr, phoneStr,
		phoneStr, biz.Name,
	)

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(html))
}
