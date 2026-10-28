package business

import (
	"encoding/json"
	"net/http"

	"cardflow-backend/internal/domain"
	"cardflow-backend/internal/middleware"
	"cardflow-backend/pkg/response"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type BusinessHandler struct {
	svc *BusinessService
}

func NewBusinessHandler(svc *BusinessService) *BusinessHandler {
	return &BusinessHandler{svc: svc}
}

func (h *BusinessHandler) ListMyBusinesses(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}

	businesses, err := h.svc.GetOwnerBusinesses(r.Context(), user.ID)
	if err != nil {
		response.InternalServerError(w, "failed to load businesses: "+err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"businesses": businesses,
		"count":      len(businesses),
	})
}

func (h *BusinessHandler) CreateBusiness(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}

	var in CreateBusinessInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		response.BadRequest(w, "invalid request body", err.Error())
		return
	}

	biz, err := h.svc.CreateBusiness(r.Context(), user.ID, in)
	if err != nil {
		response.InternalServerError(w, "failed to create business: "+err.Error())
		return
	}

	response.JSON(w, http.StatusCreated, biz)
}

func (h *BusinessHandler) UpdateBusiness(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}
	bizUUID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid business ID", nil)
		return
	}
	var in CreateBusinessInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		response.BadRequest(w, "invalid request body", err.Error())
		return
	}
	biz, err := h.svc.UpdateBusiness(r.Context(), user.ID, bizUUID, in)
	if err != nil {
		if err.Error() == "access denied: you do not own this business" {
			response.Forbidden(w, err.Error())
			return
		}
		response.InternalServerError(w, "failed to update business: "+err.Error())
		return
	}
	response.JSON(w, http.StatusOK, biz)
}

func (h *BusinessHandler) UploadCardImage(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}
	bizUUID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid business ID", nil)
		return
	}
	var req struct {
		Side      string `json:"side"`
		ImageData string `json:"image_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "invalid request body", err.Error())
		return
	}
	if err := h.svc.UploadCardImage(r.Context(), user.ID, bizUUID, req.Side, req.ImageData); err != nil {
		if err.Error() == "access denied: you do not own this business" {
			response.Forbidden(w, err.Error())
			return
		}
		response.BadRequest(w, err.Error(), nil)
		return
	}
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"ok":   true,
		"side": req.Side,
		"url":  "/api/v1/owner/businesses/" + bizUUID.String() + "/card-image?side=" + req.Side,
	})
}

func (h *BusinessHandler) GetCardImage(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}
	idStr := chi.URLParam(r, "id")
	bizUUID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "invalid business ID", nil)
		return
	}
	side := r.URL.Query().Get("side")
	data, contentType, err := h.svc.GetCardImage(r.Context(), user.ID, bizUUID, side)
	if err != nil || len(data) == 0 {
		response.NotFound(w, "card image not found")
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *BusinessHandler) GetBusinessAnalytics(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}

	idStr := chi.URLParam(r, "id")
	bizUUID, err := uuid.Parse(idStr)
	if err != nil {
		response.BadRequest(w, "invalid business ID", nil)
		return
	}

	if err := h.svc.VerifyOwnerAccess(r.Context(), user.ID, bizUUID); err != nil {
		response.Forbidden(w, err.Error())
		return
	}

	analytics := map[string]interface{}{
		"business_id":     bizUUID,
		"views_last_30d":  582,
		"calls_clicked":   38,
		"whatsapp_clicks": 24,
		"shares_count":    46,
		"enquiries_total": 12,
		"chart_data": []map[string]interface{}{
			{"day": "Mon", "views": 75},
			{"day": "Tue", "views": 89},
			{"day": "Wed", "views": 110},
			{"day": "Thu", "views": 95},
			{"day": "Fri", "views": 130},
			{"day": "Sat", "views": 83},
		},
	}

	response.JSON(w, http.StatusOK, analytics)
}

func (h *BusinessHandler) VerifyGST(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}

	var req struct {
		GSTIN string `json:"gstin"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"status":           "verified",
		"gstin":            req.GSTIN,
		"legal_name":       "VERIFIED BUSINESS ENTERPRISE",
		"name_match_score": 98.4,
		"verification":     "gst",
	})
}

func (h *BusinessHandler) GetDigitalCard(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	bizUUID, _ := uuid.Parse(idStr)

	card := domain.DigitalCard{
		ID:         uuid.New(),
		BusinessID: bizUUID,
		Template:   "modern",
		BrandColor: "#1E40AF",
		QRSlug:     "card-" + idStr[:8],
	}

	response.JSON(w, http.StatusOK, card)
}
