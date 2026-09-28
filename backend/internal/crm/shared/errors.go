package shared

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"

	chiMiddleware "github.com/go-chi/chi/v5/middleware"
)

// Error is the PRD §12 error envelope: {code, message, fieldErrors?, requestId}.
// Secrets and SQL never go into Message.
type Error struct {
	Status      int
	Code        string
	Message     string
	FieldErrors map[string]string
	RetryAfter  int
}

func (e *Error) Error() string { return fmt.Sprintf("%d %s: %s", e.Status, e.Code, e.Message) }

func NewError(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

func BadRequest(message string) *Error {
	return NewError(http.StatusBadRequest, "bad_request", message)
}

func Unauthenticated() *Error {
	return NewError(http.StatusUnauthorized, "unauthenticated", "Please sign in to continue.")
}

func InvalidCredentials() *Error {
	return NewError(http.StatusUnauthorized, "invalid_credentials", "Invalid email or password.")
}

func Forbidden(code, message string) *Error {
	return NewError(http.StatusForbidden, code, message)
}

func NotFound(code string) *Error {
	return NewError(http.StatusNotFound, code, "Not found.")
}

func Validation(fields map[string]string) *Error {
	return &Error{Status: http.StatusUnprocessableEntity, Code: "validation_failed", Message: "Please fix the highlighted fields.", FieldErrors: fields}
}

func TooManyAttempts(retryAfterSeconds int) *Error {
	return &Error{
		Status:     http.StatusTooManyRequests,
		Code:       "too_many_attempts",
		Message:    "Too many attempts. Please wait a few minutes and try again.",
		RetryAfter: retryAfterSeconds,
	}
}

func ServiceUnavailable(code, message string) *Error {
	return NewError(http.StatusServiceUnavailable, code, message)
}

type errorBody struct {
	Code        string            `json:"code"`
	Message     string            `json:"message"`
	FieldErrors map[string]string `json:"fieldErrors,omitempty"`
	RequestID   string            `json:"requestId,omitempty"`
}

func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// WriteError renders err with the PRD envelope. Unknown errors are logged and
// surface only as a generic 500 so internals never leak.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	reqID := chiMiddleware.GetReqID(r.Context())
	var appErr *Error
	if !errors.As(err, &appErr) {
		slog.Error("crm: unhandled error", "error", err, "path", r.URL.Path, "request_id", reqID)
		appErr = NewError(http.StatusInternalServerError, "internal_error", "Something went wrong. Please try again.")
	}
	if appErr.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(appErr.RetryAfter))
	}
	WriteJSON(w, appErr.Status, errorBody{
		Code:        appErr.Code,
		Message:     appErr.Message,
		FieldErrors: appErr.FieldErrors,
		RequestID:   reqID,
	})
}

const maxJSONBody = 1 << 20

// DecodeJSON reads a bounded JSON body into dst.
func DecodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxJSONBody)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return BadRequest("Request body must be valid JSON.")
	}
	return nil
}

// ClientIP returns the caller IP (chi's RealIP middleware has already applied proxy headers).
func ClientIP(r *http.Request) string {
	addr := strings.TrimSpace(r.RemoteAddr)
	if host, _, err := net.SplitHostPort(addr); err == nil {
		addr = host
	}
	if net.ParseIP(addr) == nil {
		return ""
	}
	return addr
}
