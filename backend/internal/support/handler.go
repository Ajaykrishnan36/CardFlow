package support

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"cardflow-backend/internal/database"
	"cardflow-backend/internal/domain"
	"cardflow-backend/internal/middleware"
	"cardflow-backend/pkg/response"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Ticket struct {
	ID         string    `json:"id"`
	UserID     string    `json:"user_id"`
	UserName   string    `json:"user_name"`
	UserPhone  string    `json:"user_phone"`
	UserRole   string    `json:"user_role"`
	Category   string    `json:"category"` // 'billing', 'card_scan', 'business_listing', 'general'
	Subject    string    `json:"subject"`
	Message    string    `json:"message"`
	Status     string    `json:"status"` // 'open', 'in_progress', 'resolved'
	AdminReply string    `json:"admin_reply,omitempty"`
	RepliedAt  *time.Time `json:"replied_at,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// SupportHandler stores tickets in the support_tickets table (migration 012) so they
// survive restarts and Ajay's CRM can answer them. Without a database it falls back to
// the in-memory sample tickets below.
type SupportHandler struct {
	db      *database.DB
	mu      sync.RWMutex
	tickets []Ticket
}

func (h *SupportHandler) persistent() bool { return h.db != nil && h.db.Pool != nil }

const ticketColumns = `id, COALESCE(user_id::text, ''), user_name, user_phone, user_role, category, subject, message, status,
	COALESCE(admin_reply, ''), replied_at, created_at, updated_at`

func scanTicket(row pgx.Row) (Ticket, error) {
	var t Ticket
	err := row.Scan(&t.ID, &t.UserID, &t.UserName, &t.UserPhone, &t.UserRole, &t.Category, &t.Subject, &t.Message, &t.Status,
		&t.AdminReply, &t.RepliedAt, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

func (h *SupportHandler) queryTickets(ctx context.Context, where string, args ...any) ([]Ticket, error) {
	rows, err := h.db.Pool.Query(ctx, `SELECT `+ticketColumns+` FROM support_tickets `+where+` ORDER BY created_at DESC LIMIT 500`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Ticket{}
	for rows.Next() {
		t, err := scanTicket(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func NewSupportHandler(db *database.DB) *SupportHandler {
	now := time.Now()
	h := &SupportHandler{
		db: db,
		tickets: []Ticket{
			{
				ID:         "t-1001",
				UserID:     "00000000-0000-0000-0000-0000000000b5",
				UserName:   "Raj",
				UserPhone:  "+917094310122",
				UserRole:   "owner",
				Category:   "business_listing",
				Subject:    "Need help updating GSTIN for Raj Engineering",
				Message:    "Hi admin team, I uploaded new GST certificate, please verify our branch address.",
				Status:     "in_progress",
				AdminReply: "Hello Raj, our team is verifying the documents. Will be approved within 2 hours.",
				CreatedAt:  now.Add(-2 * time.Hour),
				UpdatedAt:  now.Add(-30 * time.Minute),
			},
			{
				ID:        "t-1002",
				UserID:    "00000000-0000-0000-0000-0000000000u4",
				UserName:  "Dharani",
				UserPhone: "+919677840181",
				UserRole:  "user",
				Category:  "card_scan",
				Subject:   "Tamil text recognition inquiry",
				Message:   "How do I scan Tamil visiting cards? Need best lighting guidance.",
				Status:    "resolved",
				AdminReply: "Hi Dharani, select Tamil from the language pill on the scan screen and ensure the card is in landscape inside the frame.",
				CreatedAt: now.Add(-24 * time.Hour),
				UpdatedAt: now.Add(-20 * time.Hour),
			},
		},
	}
	return h
}

// User / Owner creates a support request
func (h *SupportHandler) CreateTicket(w http.ResponseWriter, r *http.Request) {
	user, _ := r.Context().Value(middleware.UserContextKey).(*domain.User)
	userID := "anon"
	userName := "CardFlow User"
	userPhone := "+910000000000"
	userRole := "user"
	if user != nil {
		userID = user.ID.String()
		userName = user.Name
		userPhone = user.Phone
		userRole = string(user.Role)
	}

	var req struct {
		Category string `json:"category"`
		Subject  string `json:"subject"`
		Message  string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "invalid request body", err.Error())
		return
	}

	if req.Subject == "" || req.Message == "" {
		response.BadRequest(w, "subject and message are required", "")
		return
	}
	if req.Category == "" {
		req.Category = "general"
	}

	ticket := Ticket{
		ID:        "t-" + uuid.New().String()[:8],
		UserID:    userID,
		UserName:  userName,
		UserPhone: userPhone,
		UserRole:  userRole,
		Category:  req.Category,
		Subject:   req.Subject,
		Message:   req.Message,
		Status:    "open",
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}

	if h.persistent() {
		var uid *uuid.UUID
		if user != nil {
			uid = &user.ID
		}
		if _, err := h.db.Pool.Exec(r.Context(), `
			INSERT INTO support_tickets (id, user_id, user_name, user_phone, user_role, category, subject, message, status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'open', $9, $9)`,
			ticket.ID, uid, ticket.UserName, ticket.UserPhone, ticket.UserRole, ticket.Category, ticket.Subject, ticket.Message, ticket.CreatedAt); err != nil {
			slog.Error("support: create ticket failed", "error", err)
			response.InternalServerError(w, "could not save your ticket, please try again")
			return
		}
	} else {
		h.mu.Lock()
		h.tickets = append([]Ticket{ticket}, h.tickets...)
		h.mu.Unlock()
	}

	response.JSON(w, http.StatusCreated, map[string]interface{}{
		"success": true,
		"message": "Support ticket created successfully. Our team will resolve it shortly.",
		"ticket":  ticket,
	})
}

// User / Owner fetches their support tickets
func (h *SupportHandler) GetMyTickets(w http.ResponseWriter, r *http.Request) {
	user, _ := r.Context().Value(middleware.UserContextKey).(*domain.User)
	userID := ""
	userPhone := ""
	if user != nil {
		userID = user.ID.String()
		userPhone = user.Phone
	}

	if h.persistent() {
		list, err := h.queryTickets(r.Context(), `WHERE ($1 <> '' AND user_id::text = $1) OR ($2 <> '' AND user_phone = $2)`, userID, userPhone)
		if err != nil {
			slog.Error("support: list my tickets failed", "error", err)
			response.InternalServerError(w, "could not load tickets")
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"tickets": list, "count": len(list)})
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	var userTickets []Ticket
	for _, t := range h.tickets {
		if t.UserID == userID || (userPhone != "" && t.UserPhone == userPhone) {
			userTickets = append(userTickets, t)
		}
	}
	if userTickets == nil {
		userTickets = []Ticket{}
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"tickets": userTickets,
		"count":   len(userTickets),
	})
}

// Admin lists all tickets
func (h *SupportHandler) AdminListTickets(w http.ResponseWriter, r *http.Request) {
	if h.persistent() {
		list, err := h.queryTickets(r.Context(), ``)
		if err != nil {
			slog.Error("support: admin list failed", "error", err)
			response.InternalServerError(w, "could not load tickets")
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"tickets": list, "count": len(list)})
		return
	}
	h.mu.RLock()
	defer h.mu.RUnlock()

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"tickets": h.tickets,
		"count":   len(h.tickets),
	})
}

// Admin updates / resolves ticket
func (h *SupportHandler) AdminUpdateTicket(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")

	var req struct {
		Status     string `json:"status"` // 'open', 'in_progress', 'resolved'
		AdminReply string `json:"admin_reply"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "invalid request", err.Error())
		return
	}

	if h.persistent() {
		if req.Status != "" && req.Status != "open" && req.Status != "in_progress" && req.Status != "resolved" {
			response.BadRequest(w, "invalid status", "")
			return
		}
		t, err := scanTicket(h.db.Pool.QueryRow(r.Context(), `
			UPDATE support_tickets
			SET status = COALESCE(NULLIF($2, ''), status),
			    admin_reply = COALESCE(NULLIF($3, ''), admin_reply),
			    replied_at = CASE WHEN $3 <> '' THEN NOW() ELSE replied_at END,
			    replied_by = CASE WHEN $3 <> '' THEN 'CardFlow admin' ELSE replied_by END,
			    updated_at = NOW()
			WHERE id = $1
			RETURNING `+ticketColumns, id, req.Status, req.AdminReply))
		if errors.Is(err, pgx.ErrNoRows) {
			response.NotFound(w, "ticket not found")
			return
		}
		if err != nil {
			slog.Error("support: update ticket failed", "error", err)
			response.InternalServerError(w, "could not update ticket")
			return
		}
		response.JSON(w, http.StatusOK, map[string]interface{}{"success": true, "message": "Ticket updated successfully", "ticket": t})
		return
	}

	h.mu.Lock()
	defer h.mu.Unlock()

	for i, t := range h.tickets {
		if t.ID == id {
			if req.Status != "" {
				h.tickets[i].Status = req.Status
			}
			if req.AdminReply != "" {
				h.tickets[i].AdminReply = req.AdminReply
				now := time.Now()
				h.tickets[i].RepliedAt = &now
			}
			h.tickets[i].UpdatedAt = time.Now()

			response.JSON(w, http.StatusOK, map[string]interface{}{
				"success": true,
				"message": "Ticket updated successfully",
				"ticket":  h.tickets[i],
			})
			return
		}
	}

	response.NotFound(w, "ticket not found")
}
