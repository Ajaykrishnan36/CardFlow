package contacts

import (
	"encoding/json"
	"net/http"
	"time"

	"cogniz-crm-backend/internal/database"
	"cogniz-crm-backend/internal/domain"
	"cogniz-crm-backend/internal/middleware"
	"cogniz-crm-backend/pkg/response"
)

type ContactsHandler struct {
	db *database.DB
}

func NewContactsHandler(db *database.DB) *ContactsHandler {
	return &ContactsHandler{db: db}
}

type contactInput struct {
	Name   string   `json:"name"`
	Phones []string `json:"phones"`
	Emails []string `json:"emails"`
}

// BackupContacts replaces the caller's stored phone-contact backup wholesale
// with the set just uploaded (a full snapshot, not an incremental merge).
func (h *ContactsHandler) BackupContacts(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}
	if h.db == nil || h.db.Pool == nil {
		response.InternalServerError(w, "database not connected")
		return
	}

	var req struct {
		Contacts []contactInput `json:"contacts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.BadRequest(w, "invalid request body", nil)
		return
	}
	if len(req.Contacts) == 0 {
		response.BadRequest(w, "no contacts provided", nil)
		return
	}

	tx, err := h.db.Pool.Begin(r.Context())
	if err != nil {
		response.InternalServerError(w, "failed to start backup: "+err.Error())
		return
	}
	defer tx.Rollback(r.Context())

	if _, err := tx.Exec(r.Context(), `DELETE FROM contact_backups WHERE user_id = $1`, user.ID); err != nil {
		response.InternalServerError(w, "failed to clear previous backup: "+err.Error())
		return
	}
	for _, c := range req.Contacts {
		phones, _ := json.Marshal(c.Phones)
		emails, _ := json.Marshal(c.Emails)
		if _, err := tx.Exec(r.Context(), `
			INSERT INTO contact_backups (user_id, name, phones, emails)
			VALUES ($1, $2, $3, $4)
		`, user.ID, c.Name, phones, emails); err != nil {
			response.InternalServerError(w, "failed to save contact: "+err.Error())
			return
		}
	}
	if err := tx.Commit(r.Context()); err != nil {
		response.InternalServerError(w, "failed to commit backup: "+err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"count": len(req.Contacts)})
}

// GetBackupStatus reports whether a backup exists yet, without returning the
// actual contact data — used to decide whether "Import Contacts" is enabled.
func (h *ContactsHandler) GetBackupStatus(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}
	if h.db == nil || h.db.Pool == nil {
		response.InternalServerError(w, "database not connected")
		return
	}

	var count int
	var lastAt *time.Time
	err := h.db.Pool.QueryRow(r.Context(), `
		SELECT COUNT(*), MAX(created_at) FROM contact_backups WHERE user_id = $1
	`, user.ID).Scan(&count, &lastAt)
	if err != nil {
		response.InternalServerError(w, "failed to check backup: "+err.Error())
		return
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{
		"has_backup":   count > 0,
		"count":        count,
		"backed_up_at": lastAt,
	})
}

// GetBackup returns the actual backed-up contacts, for restoring onto a device.
func (h *ContactsHandler) GetBackup(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(middleware.UserContextKey).(*domain.User)
	if !ok || user == nil {
		response.Unauthorized(w, "authentication required")
		return
	}
	if h.db == nil || h.db.Pool == nil {
		response.InternalServerError(w, "database not connected")
		return
	}

	rows, err := h.db.Pool.Query(r.Context(), `
		SELECT name, phones, emails FROM contact_backups WHERE user_id = $1 ORDER BY name
	`, user.ID)
	if err != nil {
		response.InternalServerError(w, "failed to load backup: "+err.Error())
		return
	}
	defer rows.Close()

	contacts := []map[string]interface{}{}
	for rows.Next() {
		var name string
		var phonesRaw, emailsRaw []byte
		if err := rows.Scan(&name, &phonesRaw, &emailsRaw); err != nil {
			response.InternalServerError(w, "failed to read contact: "+err.Error())
			return
		}
		var phones, emails []string
		_ = json.Unmarshal(phonesRaw, &phones)
		_ = json.Unmarshal(emailsRaw, &emails)
		contacts = append(contacts, map[string]interface{}{
			"name":   name,
			"phones": phones,
			"emails": emails,
		})
	}

	response.JSON(w, http.StatusOK, map[string]interface{}{"contacts": contacts})
}
