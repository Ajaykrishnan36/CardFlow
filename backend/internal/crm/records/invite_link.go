package records

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/jackc/pgx/v5"
)

// A product's shareable invite link (D-83). People with the link and an email on one of
// the listed company domains join with the chosen role; everyone else still needs an
// invitation. The link can be turned off or replaced (the old one stops working).

type InviteLinkSettings struct {
	Configured bool      `json:"configured"`
	Enabled    bool      `json:"enabled"`
	URL        string    `json:"url,omitempty"`
	Domains    []string  `json:"domains"`
	RoleKey    string    `json:"roleKey"`
	Uses       int       `json:"uses"`
	UpdatedAt  time.Time `json:"updatedAt,omitempty"`
}

func inviteURL(base, token string) string {
	return strings.TrimRight(base, "/") + "/crm/join/" + token
}

func cleanDomains(in []string) []string {
	out := []string{}
	for _, d := range in {
		d = strings.ToLower(strings.TrimPrefix(strings.TrimSpace(d), "@"))
		if d != "" && strings.Contains(d, ".") && !strings.ContainsAny(d, " /@") && len(d) <= 200 && !contains(out, d) {
			out = append(out, d)
		}
	}
	return out
}

// Personal mail services can't be a company domain: the link would be open to anyone.
var publicMailDomains = map[string]bool{"gmail.com": true, "googlemail.com": true, "outlook.com": true, "hotmail.com": true, "live.com": true,
	"yahoo.com": true, "icloud.com": true, "me.com": true, "aol.com": true, "proton.me": true, "protonmail.com": true, "gmx.com": true,
	"mail.com": true, "yandex.com": true, "zoho.com": true, "rediffmail.com": true, "yahoo.co.in": true, "msn.com": true}

func (h *Handler) inviteLink(ctx context.Context, sc *Scope) (*InviteLinkSettings, error) {
	s := &InviteLinkSettings{Domains: []string{}, RoleKey: "STAFF"}
	var status string
	var enc []byte
	err := h.store.Pool.QueryRow(ctx, `SELECT status, token_enc, domains, role_key, uses, updated_at FROM crm.invite_links WHERE workspace_id = $1`, sc.WS).
		Scan(&status, &enc, &s.Domains, &s.RoleKey, &s.Uses, &s.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	s.Configured, s.Enabled = true, status == "active"
	if raw, err := shared.Decrypt(h.cfg.EncryptionKey, enc); err == nil {
		s.URL = inviteURL(h.cfg.BaseURL, string(raw))
	}
	return s, nil
}

func (h *Handler) handleGetInviteLink(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireAccessAdmin(w, r)
	if !ok {
		return
	}
	s, err := h.inviteLink(r.Context(), sc)
	respond(w, r, http.StatusOK, s, err)
}

// PUT /invite-link {enabled, domains, roleKey, regenerate}
func (h *Handler) handleSaveInviteLink(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireAccessAdmin(w, r)
	if !ok {
		return
	}
	var in struct {
		Enabled    bool     `json:"enabled"`
		Domains    []string `json:"domains"`
		RoleKey    string   `json:"roleKey"`
		Regenerate bool     `json:"regenerate"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	domains := cleanDomains(in.Domains)
	fe := map[string]string{}
	if len(domains) == 0 {
		fe["domains"] = "List at least one company email domain (e.g. acme.com)."
	}
	for _, d := range domains {
		if publicMailDomains[d] {
			fe["domains"] = d + " is a personal email service — anyone could join. Use your company's own domain."
		}
	}
	if in.RoleKey == "" {
		in.RoleKey = "STAFF"
	}
	var roleOK bool
	_ = h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.roles WHERE workspace_id = $1 AND key = $2)`, sc.WS, in.RoleKey).Scan(&roleOK)
	if in.RoleKey == "SUPER_ADMIN" || !roleOK {
		fe["roleKey"] = "Choose a role other than Super admin."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	status := "disabled"
	if in.Enabled {
		status = "active"
	}
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		var exists bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.invite_links WHERE workspace_id = $1)`, sc.WS).Scan(&exists); err != nil {
			return err
		}
		var hash, enc []byte
		if !exists || in.Regenerate {
			token, err := shared.RandomToken(24)
			if err != nil {
				return err
			}
			if enc, err = shared.Encrypt(h.cfg.EncryptionKey, []byte(token)); err != nil {
				return err
			}
			hash = shared.HashToken(token)
		}
		if !exists {
			if _, err := tx.Exec(ctx, `INSERT INTO crm.invite_links (workspace_id, token_hash, token_enc, domains, role_key, status, created_by) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				sc.WS, hash, enc, domains, in.RoleKey, status, actor(r)); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE crm.invite_links SET domains = $2, role_key = $3, status = $4, token_hash = COALESCE($5, token_hash),
			token_enc = COALESCE($6, token_enc), uses = CASE WHEN $5::bytea IS NULL THEN uses ELSE 0 END, updated_at = now() WHERE workspace_id = $1`,
			sc.WS, domains, in.RoleKey, status, hash, enc); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, actorFromRequest(r, "ui").audit(sc.WS, "invite_link.saved", "invite_link", nil, nil,
			map[string]any{"enabled": in.Enabled, "domains": domains, "role": in.RoleKey, "regenerated": in.Regenerate || !exists}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	s, err := h.inviteLink(ctx, sc)
	respond(w, r, http.StatusOK, s, err)
}
