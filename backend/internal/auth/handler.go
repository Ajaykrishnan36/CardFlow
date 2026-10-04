package auth

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"cardflow-backend/internal/domain"
	"cardflow-backend/pkg/response"
	"github.com/google/uuid"
)

type AuthHandler struct {
	authSvc *AuthService
}

// writeAuthError answers with the identity service's own status and message (wrong code,
// too many attempts, SMS unavailable…) instead of flattening everything into "bad request".
func writeAuthError(w http.ResponseWriter, err error) {
	var se *shared.Error
	if errors.As(err, &se) {
		if se.RetryAfter > 0 {
			w.Header().Set("Retry-After", strconv.Itoa(se.RetryAfter))
		}
		msg := se.Message
		for _, m := range se.FieldErrors {
			msg = m // the app shows one message under the field
			break
		}
		response.Error(w, se.Status, strings.ToUpper(se.Code), msg, nil)
		return
	}
	response.BadRequest(w, err.Error(), nil)
}

func NewAuthHandler(authSvc *AuthService) *AuthHandler {
	return &AuthHandler{authSvc: authSvc}
}

type SendOTPRequest struct {
	Phone    string `json:"phone"`
	DeviceID string `json:"device_id"`
	Platform string `json:"platform"`
}

type VerifyOTPRequest struct {
	Phone     string `json:"phone"`
	OTPCode   string `json:"otp_code"`
	OTP       string `json:"otp"`
	DeviceID  string `json:"device_id"`
	Platform  string `json:"platform"`
	PushToken string `json:"push_token"`
}

type UpdateProfileRequest struct {
	Name  *string `json:"name"`
	Email *string `json:"email"`
	City  *string `json:"city"`
	State *string `json:"state"`
}

func (h *AuthHandler) SendOTP(w http.ResponseWriter, r *http.Request) {
	var req SendOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "invalid request body", err.Error())
		return
	}

	res, err := h.authSvc.SendOTP(r.Context(), req.Phone, identity.Meta(r))
	if err != nil {
		writeAuthError(w, err)
		return
	}

	response.JSON(w, http.StatusOK, res)
}

func (h *AuthHandler) VerifyOTP(w http.ResponseWriter, r *http.Request) {
	var req VerifyOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "invalid request body", err.Error())
		return
	}

	code := req.OTPCode
	if code == "" {
		code = req.OTP
	}

	tokenPair, err := h.authSvc.VerifyOTP(r.Context(), req.Phone, code, req.DeviceID, req.Platform, req.PushToken, identity.Meta(r))
	if err != nil {
		writeAuthError(w, err)
		return
	}

	response.JSON(w, http.StatusOK, tokenPair)
}

func (h *AuthHandler) RefreshToken(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RefreshToken string `json:"refresh_token"`
		DeviceID     string `json:"device_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "invalid request body", nil)
		return
	}

	if req.RefreshToken == "" {
		response.Unauthorized(w, "refresh token is required")
		return
	}

	// For dev / mock, generate refreshed tokens
	response.JSON(w, http.StatusOK, map[string]string{
		"access_token":  "cf_jwt_refreshed_" + uuid.New().String(),
		"refresh_token": "cf_refr_new_" + uuid.New().String(),
	})
}

func (h *AuthHandler) LogoutAll(w http.ResponseWriter, r *http.Request) {
	response.Message(w, http.StatusOK, "All sessions terminated successfully")
}

func (h *AuthHandler) GetMe(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value("user").(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}

	response.JSON(w, http.StatusOK, user)
}

func (h *AuthHandler) UpdateMe(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value("user").(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}

	var req UpdateProfileRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "invalid request body", nil)
		return
	}

	updated, err := h.authSvc.UpdateUserProfile(r.Context(), user, req)
	if err != nil {
		response.InternalServerError(w, "failed to update profile: "+err.Error())
		return
	}

	response.JSON(w, http.StatusOK, updated)
}

type ChangePhoneRequest struct {
	Phone   string `json:"phone"`
	OTPCode string `json:"otp_code"`
}

func (h *AuthHandler) ChangePhone(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value("user").(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}

	var req ChangePhoneRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "invalid request body", err.Error())
		return
	}

	updated, err := h.authSvc.ChangePhone(r.Context(), user, req.Phone, req.OTPCode, identity.Meta(r))
	if err != nil {
		writeAuthError(w, err)
		return
	}

	response.JSON(w, http.StatusOK, updated)
}

func (h *AuthHandler) DeleteMe(w http.ResponseWriter, r *http.Request) {
	response.Message(w, http.StatusOK, "Account deletion initiated. All data will be purged following the 30-day DPDP grace period.")
}

func (h *AuthHandler) ExportMe(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value("user").(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}

	exportData := map[string]interface{}{
		"account":        user,
		"export_date":    "2026-08-29",
		"compliance":     "DPDP Act 2023",
		"saved_cards":    []interface{}{},
		"enquiries_sent": []interface{}{},
	}

	response.JSON(w, http.StatusOK, exportData)
}
