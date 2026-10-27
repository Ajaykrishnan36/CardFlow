package platform

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"cardflow-backend/internal/crm/mail"
	"cardflow-backend/internal/crm/sms"
	"cardflow-backend/internal/crm/whatsapp"
	"github.com/google/uuid"
)

// Notices to a person on every channel we have (D-161): email, SMS and WhatsApp. Each
// channel reports what happened, so the screen and the owner can say it honestly:
// "sent", "failed", "not_set_up" (no provider on this server) or "no_address".

// SetMessaging gives the handler the SMS and WhatsApp senders (email is always there).
func (h *Handler) SetMessaging(s sms.Sender, wa *whatsapp.Sender) {
	h.sms, h.wa = s, wa
}

type notice struct {
	Subject string
	Heading string
	Lines   []string // email body; the text messages join them
	Footer  string
}

func (h *Handler) notifyPerson(ctx context.Context, who uuid.UUID, n notice) map[string]string {
	out := map[string]string{"email": "no_address", "sms": "no_address", "whatsapp": "no_address"}
	p, err := h.registeredPerson(ctx, who)
	if err != nil {
		slog.Warn("crm: notice not sent, person not found", "identity", who, "error", err)
		return out
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 25*time.Second)
	defer cancel()
	text := strings.Join(n.Lines, " ")
	status := func(channel string, err error) string {
		switch {
		case err == nil:
			return "sent"
		case errors.Is(err, sms.ErrUnavailable), errors.Is(err, whatsapp.ErrUnavailable):
			return "not_set_up"
		default:
			slog.Warn("crm: notice not delivered", "channel", channel, "identity", who, "error", err)
			return "failed"
		}
	}
	if p.Email != "" {
		out["email"] = status("email", h.mailer.Send(ctx, mail.Message{To: p.Email, Subject: n.Subject, Heading: n.Heading, Lines: n.Lines, Footer: n.Footer}))
	}
	if p.Phone != "" {
		if h.sms == nil {
			out["sms"] = "not_set_up"
		} else {
			out["sms"] = status("sms", sms.SendText(ctx, h.sms, p.Phone, text))
		}
		out["whatsapp"] = status("whatsapp", h.wa.Send(ctx, p.Phone, p.Name, text))
	}
	return out
}

// notifyOwner emails the platform owner(s) about something they should know (D-161).
func (h *Handler) notifyOwner(ctx context.Context, subject string, lines []string) {
	rows, err := h.store.Pool.Query(ctx, `
		SELECT v.value_normalized FROM crm.identities i
		JOIN crm.verified_identifiers v ON v.identity_id = i.id AND v.kind = 'email'
		WHERE i.is_platform_owner AND i.status = 'active'`)
	if err != nil {
		return
	}
	var to []string
	for rows.Next() {
		var e string
		if rows.Scan(&e) == nil {
			to = append(to, e)
		}
	}
	rows.Close()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 20*time.Second)
	defer cancel()
	for _, e := range to {
		if err := h.mailer.Send(ctx, mail.Message{To: e, Subject: subject, Heading: subject, Lines: lines}); err != nil {
			slog.Warn("crm: owner notice not sent", "error", err)
		}
	}
}
