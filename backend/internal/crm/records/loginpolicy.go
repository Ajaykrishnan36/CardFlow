package records

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Login security a business sets for its own people (D-142).
//
//   - Allowed networks: when the list isn't empty, members can only work from those
//     addresses. API keys are held to the same list.
//   - Sign in again after N hours: a session older than that is refused in this
//     business, however active it has been.
//
// The platform owner entering a business from the console is not held to either — the
// owner has to be able to get in to help a business that locked itself out.

type LoginPolicy struct {
	IPRanges     []string   `json:"ipRanges"`
	SessionHours *int       `json:"sessionHours"`
	UpdatedAt    *time.Time `json:"updatedAt,omitempty"`
	// YourIP is the address this request came from, so the screen can offer it.
	YourIP string `json:"yourIp,omitempty"`
}

func loadLoginPolicy(ctx context.Context, q querier, ws uuid.UUID) (*LoginPolicy, error) {
	p := &LoginPolicy{IPRanges: []string{}}
	err := q.QueryRow(ctx, `SELECT ip_ranges, session_hours, updated_at FROM crm.login_policies WHERE workspace_id = $1`, ws).
		Scan(&p.IPRanges, &p.SessionHours, &p.UpdatedAt)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	if p.IPRanges == nil {
		p.IPRanges = []string{}
	}
	return p, nil
}

// normalRange turns "203.0.113.7" or "203.0.113.0/24" into a network.
func normalRange(s string) (*net.IPNet, string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, "", false
	}
	if !strings.Contains(s, "/") {
		ip := net.ParseIP(s)
		if ip == nil {
			return nil, "", false
		}
		if ip.To4() != nil {
			s += "/32"
		} else {
			s += "/128"
		}
	}
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		return nil, "", false
	}
	return n, n.String(), true
}

func ipAllowed(ranges []string, ip string) bool {
	if len(ranges) == 0 {
		return true
	}
	addr := net.ParseIP(ip)
	if addr == nil {
		return false
	}
	for _, r := range ranges {
		if n, _, ok := normalRange(r); ok && n.Contains(addr) {
			return true
		}
	}
	return false
}

// checkLoginPolicy is called for every member and API-key request in a business.
func (h *Handler) checkLoginPolicy(r *http.Request, ws uuid.UUID, sess *identity.Session) error {
	p, err := loadLoginPolicy(r.Context(), h.store.Pool, ws)
	if err != nil {
		return err
	}
	if !ipAllowed(p.IPRanges, shared.ClientIP(r)) {
		return shared.Forbidden("network_not_allowed", "This business can only be used from its approved networks. Connect from the office network or ask your administrator.")
	}
	if p.SessionHours != nil && sess != nil {
		var started time.Time
		if err := h.store.Pool.QueryRow(r.Context(), `SELECT created_at FROM crm.sessions WHERE id = $1`, sess.ID).Scan(&started); err == nil &&
			time.Since(started) > time.Duration(*p.SessionHours)*time.Hour {
			return shared.NewError(http.StatusUnauthorized, "session_expired", "This business asks you to sign in again. Please sign in.")
		}
	}
	return nil
}

func (h *Handler) handleGetLoginPolicy(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireAccessAdmin(w, r)
	if !ok {
		return
	}
	p, err := loadLoginPolicy(r.Context(), h.store.Pool, sc.WS)
	if p != nil {
		p.YourIP = shared.ClientIP(r)
	}
	respond(w, r, http.StatusOK, p, err)
}

func (h *Handler) handleSaveLoginPolicy(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireAccessAdmin(w, r)
	if !ok {
		return
	}
	var in struct {
		IPRanges     []string `json:"ipRanges"`
		SessionHours *int     `json:"sessionHours"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	fe := map[string]string{}
	ranges := []string{}
	seen := map[string]bool{}
	for _, raw := range in.IPRanges {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		_, norm, ok := normalRange(raw)
		if !ok {
			fe["ipRanges"] = fmt.Sprintf("%q isn't an address or a range. Use 203.0.113.7 or 203.0.113.0/24.", strings.TrimSpace(raw))
			break
		}
		if !seen[norm] {
			seen[norm] = true
			ranges = append(ranges, norm)
		}
	}
	if len(ranges) > 50 {
		fe["ipRanges"] = "Use at most 50 ranges."
	}
	// Saving a list that leaves out where you are now would lock you out on the spot.
	if fe["ipRanges"] == "" && !sc.Owner && !ipAllowed(ranges, shared.ClientIP(r)) {
		fe["ipRanges"] = fmt.Sprintf("Your own address (%s) isn't in the list — you would be locked out. Add it first.", shared.ClientIP(r))
	}
	if in.SessionHours != nil && (*in.SessionHours < 1 || *in.SessionHours > 720) {
		fe["sessionHours"] = "Use between 1 hour and 720 hours (30 days)."
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	ctx := r.Context()
	err := h.store.WithTx(ctx, func(tx pgx.Tx) error {
		before, err := loadLoginPolicy(ctx, tx, sc.WS)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO crm.login_policies (workspace_id, ip_ranges, session_hours, updated_by) VALUES ($1, $2, $3, $4)
			ON CONFLICT (workspace_id) DO UPDATE SET ip_ranges = EXCLUDED.ip_ranges, session_hours = EXCLUDED.session_hours,
				updated_by = EXCLUDED.updated_by, updated_at = now()`, sc.WS, ranges, in.SessionHours, actor(r)); err != nil {
			return err
		}
		return shared.WriteAudit(ctx, tx, audit(r, sc.WS, "login_policy.updated", "login_policy", nil,
			map[string]any{"ipRanges": before.IPRanges, "sessionHours": before.SessionHours},
			map[string]any{"ipRanges": ranges, "sessionHours": in.SessionHours}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	p, err := loadLoginPolicy(ctx, h.store.Pool, sc.WS)
	if p != nil {
		p.YourIP = shared.ClientIP(r)
	}
	respond(w, r, http.StatusOK, p, err)
}
