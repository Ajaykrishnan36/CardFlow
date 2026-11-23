package enquiry

import (
	"encoding/json"
	"net/http"
	"time"

	"cogniz-crm-backend/internal/database"
	"cogniz-crm-backend/internal/domain"
	"cogniz-crm-backend/internal/middleware"
	"cogniz-crm-backend/pkg/response"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type EnquiryHandler struct {
	db *database.DB
}

func NewEnquiryHandler(db *database.DB) *EnquiryHandler {
	return &EnquiryHandler{db: db}
}

type CreateEnquiryRequest struct {
	BusinessID string `json:"business_id"`
	Message    string `json:"message"`
	SharePhone bool   `json:"share_phone"`
}

func (h *EnquiryHandler) CreateEnquiry(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}

	var req CreateEnquiryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "invalid request body", err.Error())
		return
	}

	bizUUID, err := uuid.Parse(req.BusinessID)
	if err != nil {
		response.BadRequest(w, "invalid business ID", nil)
		return
	}

	enquiry := domain.Enquiry{
		ID:            uuid.New(),
		BusinessID:    bizUUID,
		UserID:        user.ID,
		CustomerName:  user.Name,
		CustomerPhone: user.Phone,
		Message:       req.Message,
		SharePhone:    req.SharePhone,
		Status:        "new",
		CreatedAt:     time.Now(),
	}

	response.JSON(w, http.StatusCreated, enquiry)
}

func (h *EnquiryHandler) ListBusinessEnquiries(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	bizUUID, _ := uuid.Parse(idStr)

	// Mock enquiries
	enquiries := []domain.Enquiry{
		{
			ID:            uuid.New(),
			BusinessID:    bizUUID,
			UserID:        uuid.New(),
			CustomerName:  "Ramesh Patel",
			CustomerPhone: "+91 98450 11223",
			Message:       "Need quotation for 5,000 units of custom CNC milled brass bushings. Delivery required in 3 weeks.",
			SharePhone:    true,
			Status:        "new",
			CreatedAt:     time.Now().Add(-3 * time.Hour),
		},
		{
			ID:            uuid.New(),
			BusinessID:    bizUUID,
			UserID:        uuid.New(),
			CustomerName:  "Karthik S.",
			CustomerPhone: "+91 97890 44556",
			Message:       "Are you available for urgent hydraulic valve maintenance and inspection on Monday?",
			SharePhone:    true,
			Status:        "viewed",
			CreatedAt:     time.Now().Add(-24 * time.Hour),
		},
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"enquiries": enquiries,
		"count":     len(enquiries),
	})
}
