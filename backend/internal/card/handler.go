package card

import (
	"encoding/json"
	"net/http"
	"strings"

	"cardflow-backend/internal/domain"
	"cardflow-backend/internal/middleware"
	"cardflow-backend/internal/storage"
	"cardflow-backend/pkg/response"
	"cardflow-backend/pkg/validator"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// FreeSavedCardLimit is how many cards a non-premium user may save to their vault.
const FreeSavedCardLimit = 5

type CardHandler struct {
	svc *CardService
	s3  *storage.S3Service
}

func NewCardHandler(svc *CardService, s3 *storage.S3Service) *CardHandler {
	return &CardHandler{svc: svc, s3: s3}
}

func (h *CardHandler) ListCards(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}

	cards, err := h.svc.GetSavedCards(r.Context(), user.ID)
	if err != nil {
		response.InternalServerError(w, "failed to load cards: "+err.Error())
		return
	}
	if cards == nil {
		cards = []domain.SavedCard{}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"cards": cards,
		"count": len(cards),
	})
}

func (h *CardHandler) CreateCard(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}

	var card domain.SavedCard
	if err := json.NewDecoder(r.Body).Decode(&card); err != nil {
		response.BadRequest(w, "invalid request body", err.Error())
		return
	}

	if msg := validateCard(card); msg != "" {
		response.BadRequest(w, msg, nil)
		return
	}

	if !user.IsPremiumActive() {
		existing, err := h.svc.GetSavedCards(r.Context(), user.ID)
		if err != nil {
			response.InternalServerError(w, "failed to check card count: "+err.Error())
			return
		}
		if len(existing) >= FreeSavedCardLimit {
			response.Error(w, http.StatusPaymentRequired, "UPGRADE_REQUIRED",
				"Free plan allows up to 5 saved cards. Upgrade to CardFlow Premium to save unlimited cards.", nil)
			return
		}
	}

	created, err := h.svc.CreateSavedCard(r.Context(), user.ID, card)
	if err != nil {
		response.InternalServerError(w, "failed to save card: "+err.Error())
		return
	}

	response.JSON(w, http.StatusCreated, created)
}

func (h *CardHandler) GetUploadURL(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}

	var req struct {
		Kind string `json:"kind"` // 'card_image', 'business_logo', etc.
		Ext  string `json:"ext"`  // 'jpg', 'png', 'webp'
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.Ext == "" {
		req.Ext = "webp"
	}
	if req.Kind == "" {
		req.Kind = "card_image"
	}

	if h.s3 == nil {
		// Fallback dev response
		response.JSON(w, http.StatusOK, storage.PresignedUploadResponse{
			UploadURL:        "https://s3.local/upload-mock/" + uuid.New().String(),
			ObjectKey:        "cards/" + user.ID.String() + "/original/" + uuid.New().String() + "." + req.Ext,
			ExpiresInSeconds: 300,
		})
		return
	}

	presigned, err := h.s3.GeneratePresignedUpload(r.Context(), user.ID.String(), req.Kind, req.Ext, 15*1024*1024)
	if err != nil {
		response.InternalServerError(w, "failed to generate upload URL: "+err.Error())
		return
	}

	response.JSON(w, http.StatusOK, presigned)
}

func (h *CardHandler) ScanCard(w http.ResponseWriter, r *http.Request) {
	var userID uuid.UUID
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if ok && user != nil {
		userID = user.ID
	} else {
		userID = uuid.New()
	}

	var req struct {
		ImageObjectKey string `json:"image_object_key"`
		ImageData      string `json:"image_data"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.ImageObjectKey == "" {
		req.ImageObjectKey = "scanned-card.jpg"
	}

	extracted, err := h.svc.ProcessOCR(r.Context(), userID, req.ImageObjectKey)
	if err != nil {
		response.InternalServerError(w, "AI extraction failed: "+err.Error())
		return
	}

	response.JSON(w, http.StatusOK, extracted)
}

func (h *CardHandler) GetOriginalImage(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}

	cardID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid card id", err.Error())
		return
	}

	if !h.svc.CardBelongsToUser(r.Context(), user.ID, cardID) {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "card not found", nil)
		return
	}

	data, contentType, err := h.svc.GetOriginalImage(r.Context(), user.ID, cardID, r.URL.Query().Get("side"))
	if err != nil || len(data) == 0 {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "original card image not found", nil)
		return
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

func (h *CardHandler) UploadOriginalImage(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}

	cardID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid card id", err.Error())
		return
	}

	if !h.svc.CardBelongsToUser(r.Context(), user.ID, cardID) {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "card not found", nil)
		return
	}

	var req struct {
		ImageData string `json:"image_data"`
		Side      string `json:"side"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ImageData == "" {
		response.BadRequest(w, "image_data required", "provide base64 data URL in image_data")
		return
	}

	raw, contentType, err := decodeDataURL(req.ImageData)
	if err != nil {
		response.BadRequest(w, "invalid image_data", err.Error())
		return
	}

	if err := h.svc.SaveCardImage(r.Context(), user.ID, cardID, raw, contentType, req.Side); err != nil {
		response.InternalServerError(w, "failed to save image: "+err.Error())
		return
	}

	side := normalizeSide(req.Side)
	response.JSON(w, http.StatusOK, map[string]interface{}{
		"original_card_image_url": originalImageAPIPath(cardID.String(), side),
		"side":                    side,
		"message":                 "original card image saved",
	})
}

func (h *CardHandler) UpdateCard(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}
	cardID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid card id", err.Error())
		return
	}
	var patch domain.SavedCard
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		response.BadRequest(w, "invalid request body", err.Error())
		return
	}
	updated, err := h.svc.UpdateSavedCard(r.Context(), user.ID, cardID, patch)
	if err != nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", err.Error(), nil)
		return
	}
	response.JSON(w, http.StatusOK, updated)
}

func (h *CardHandler) DeleteCard(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}
	cardID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid card id", err.Error())
		return
	}
	if err := h.svc.DeleteSavedCard(r.Context(), user.ID, cardID); err != nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", err.Error(), nil)
		return
	}
	response.JSON(w, http.StatusOK, map[string]string{"message": "card deleted"})
}

// PublicGetCard serves a saved card's shareable fields with no auth
// required — this is what a "/share/{id}" link in the app resolves to.
func (h *CardHandler) PublicGetCard(w http.ResponseWriter, r *http.Request) {
	cardID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid card id", err.Error())
		return
	}
	card, err := h.svc.GetPublicCard(r.Context(), cardID)
	if err != nil {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "this shared card link is no longer available", nil)
		return
	}
	response.JSON(w, http.StatusOK, card)
}

// PublicGetOriginalImage serves a shared card's original image with no auth
// required, so the recipient of a share link can see the physical card.
func (h *CardHandler) PublicGetOriginalImage(w http.ResponseWriter, r *http.Request) {
	cardID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		response.BadRequest(w, "invalid card id", err.Error())
		return
	}
	data, contentType, err := h.svc.GetOriginalImage(r.Context(), uuid.Nil, cardID, r.URL.Query().Get("side"))
	if err != nil || len(data) == 0 {
		response.Error(w, http.StatusNotFound, "NOT_FOUND", "original card image not found", nil)
		return
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// validateCard checks a card before saving. Cards typed in by hand
// (source MANUAL) must have a name, phone and valid GSTIN — the GSTIN is what
// links everyone's copy of the card to one business, and the phone is how its
// owner is recognised when they sign up. Scanned cards keep the old rule.
func validateCard(card domain.SavedCard) string {
	hasPhone := false
	for _, p := range card.Phones {
		raw := p.E164
		if raw == "" {
			raw = p.Raw
		}
		if strings.TrimSpace(raw) != "" {
			hasPhone = true
			break
		}
	}
	if strings.EqualFold(card.Source, "MANUAL") {
		switch {
		case strings.TrimSpace(card.PersonName) == "":
			return "Name is required."
		case !hasPhone:
			return "Phone is required."
		case strings.TrimSpace(card.GSTIN) == "":
			return "GSTIN is required."
		case !validator.IsValidGSTIN(NormalizeGSTIN(card.GSTIN)):
			return "GSTIN doesn't look right — it should be 15 characters like 33ABCDE1234F1Z5."
		}
		for _, p := range card.Phones {
			raw := p.E164
			if raw == "" {
				raw = p.Raw
			}
			if _, ok := validator.NormalizePhone(raw); !ok && !p.IsWhatsApp {
				return "Phone number doesn't look right — use a 10-digit mobile number."
			}
		}
		return ""
	}
	if strings.TrimSpace(card.PersonName) == "" && strings.TrimSpace(card.Company) == "" && !hasPhone {
		return "Enter at least a name, company, or phone number."
	}
	return ""
}
