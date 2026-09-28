package platform

import (
	"context"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/mail"
	"cardflow-backend/internal/crm/shared"
)

// Email status and a test send for the owner (so a live SMTP setup can be checked
// without inviting anyone).

func (h *Handler) handleEmailStatus(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"mode": h.mailer.Mode(), "from": h.mailer.From(), "host": h.cfg.SMTPHost, "port": h.cfg.SMTPPort}
	if h.mailer.Mode() == "smtp" {
		if err := mail.CheckSMTP(h.cfg); err != nil {
			out["reachable"] = false
			out["problem"] = "Can't reach " + h.cfg.SMTPHost + " (" + err.Error() + "). If this runs on Render's free plan, SMTP ports are blocked: set CRM_BREVO_API_KEY."
		} else {
			out["reachable"] = true
		}
	}
	shared.WriteJSON(w, http.StatusOK, out)
}

func (h *Handler) handleEmailTest(w http.ResponseWriter, r *http.Request) {
	var in struct {
		To string `json:"to"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	to, ok := identity.NormalizeEmail(strings.TrimSpace(in.To))
	if !ok {
		shared.WriteError(w, r, shared.Validation(map[string]string{"to": "Enter a valid email address."}))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Second)
	defer cancel()
	err := h.mailer.Send(ctx, mail.Message{
		To:      to,
		Subject: "Test email from " + h.cfg.AppName,
		Heading: "Email is working",
		Lines:   []string{"This is a test from " + h.cfg.AppName + ". Invitations, password resets, sign-in codes and security alerts will arrive like this."},
		Button:  &mail.Button{Label: "Open " + h.cfg.AppName, URL: h.cfg.BaseURL + "/crm/owner/dashboard"},
		Footer:  "Sent with " + h.mailer.From() + " via " + h.mailer.Mode() + ".",
	})
	if err != nil {
		// Provider errors don't contain secrets; show them so the owner can fix the setup.
		shared.WriteError(w, r, shared.NewError(http.StatusBadGateway, "email_failed", "Couldn't send: "+err.Error()))
		return
	}
	_ = shared.WriteAudit(r.Context(), h.store.Pool, auditEvent(r, "email.test_sent", "", nil, nil, nil, map[string]any{"to": to}))
	shared.WriteJSON(w, http.StatusOK, map[string]any{"sent": true, "mode": h.mailer.Mode()})
}
