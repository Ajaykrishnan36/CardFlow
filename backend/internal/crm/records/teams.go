package records

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Teams (D-67): groups of people in a workspace. Workflows assign records round robin
// (or to the least busy) within a team, and lists filter on "owner is on my team".

type TeamMember struct {
	IdentityID uuid.UUID `json:"identityId"`
	Name       string    `json:"name"`
	Email      string    `json:"email"`
}

type Team struct {
	ID          uuid.UUID    `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Members     []TeamMember `json:"members"`
	CreatedAt   time.Time    `json:"createdAt"`
}

func (h *Handler) canManageTeams(sc *Scope) bool {
	return sc.Owner || sc.Eff.HasCapability(access.CapMembersManage) || sc.Eff.HasCapability(access.CapAccessManage)
}

func (h *Handler) handleListTeams(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	rows, err := h.store.Pool.Query(r.Context(), `SELECT id, name, description, created_at FROM crm.teams WHERE workspace_id = $1 ORDER BY name`, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	teams := []*Team{}
	byID := map[uuid.UUID]*Team{}
	for rows.Next() {
		t := &Team{Members: []TeamMember{}}
		if err := rows.Scan(&t.ID, &t.Name, &t.Description, &t.CreatedAt); err != nil {
			rows.Close()
			shared.WriteError(w, r, err)
			return
		}
		teams = append(teams, t)
		byID[t.ID] = t
	}
	rows.Close()
	mrows, err := h.store.Pool.Query(r.Context(), `SELECT tm.team_id, i.id, i.display_name, COALESCE(e.value_normalized, '') FROM crm.team_members tm
		JOIN crm.memberships m ON m.id = tm.membership_id JOIN crm.identities i ON i.id = m.identity_id
		LEFT JOIN crm.verified_identifiers e ON e.identity_id = i.id AND e.kind = 'email' AND e.namespace = 'global'
		WHERE tm.workspace_id = $1 AND m.status = 'active' ORDER BY i.display_name`, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer mrows.Close()
	for mrows.Next() {
		var tid uuid.UUID
		var m TeamMember
		if err := mrows.Scan(&tid, &m.IdentityID, &m.Name, &m.Email); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		if t := byID[tid]; t != nil {
			t.Members = append(t.Members, m)
		}
	}
	respond(w, r, http.StatusOK, map[string]any{"data": teams, "canManage": h.canManageTeams(sc)}, mrows.Err())
}

type teamInput struct {
	Name        *string      `json:"name"`
	Description *string      `json:"description"`
	Members     *[]uuid.UUID `json:"members"` // identity ids
}

func (h *Handler) saveTeam(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	sc := scopeFrom(r.Context())
	if !h.canManageTeams(sc) {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "You need the “Manage users” permission to change teams."))
		return
	}
	var in teamInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	err := h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		if id == uuid.Nil {
			name := ""
			if in.Name != nil {
				name = strings.TrimSpace(*in.Name)
			}
			if name == "" || len(name) > 80 {
				return shared.Validation(map[string]string{"name": "Name the team (up to 80 characters)."})
			}
			desc := ""
			if in.Description != nil {
				desc = strings.TrimSpace(*in.Description)
			}
			if err := tx.QueryRow(r.Context(), `INSERT INTO crm.teams (workspace_id, name, description) VALUES ($1, $2, $3) RETURNING id`, sc.WS, name, desc).Scan(&id); err != nil {
				var pe *pgconn.PgError
				if errors.As(err, &pe) && pe.Code == "23505" {
					return shared.Validation(map[string]string{"name": "A team with this name already exists."})
				}
				return err
			}
		} else {
			if in.Name != nil {
				name := strings.TrimSpace(*in.Name)
				if name == "" || len(name) > 80 {
					return shared.Validation(map[string]string{"name": "Name the team (up to 80 characters)."})
				}
				if _, err := tx.Exec(r.Context(), `UPDATE crm.teams SET name = $3 WHERE id = $1 AND workspace_id = $2`, id, sc.WS, name); err != nil {
					return shared.Validation(map[string]string{"name": "A team with this name already exists."})
				}
			}
			if in.Description != nil {
				if _, err := tx.Exec(r.Context(), `UPDATE crm.teams SET description = $3 WHERE id = $1 AND workspace_id = $2`, id, sc.WS, strings.TrimSpace(*in.Description)); err != nil {
					return err
				}
			}
		}
		if in.Members != nil {
			if _, err := tx.Exec(r.Context(), `DELETE FROM crm.team_members WHERE team_id = $1`, id); err != nil {
				return err
			}
			if _, err := tx.Exec(r.Context(), `INSERT INTO crm.team_members (workspace_id, team_id, membership_id)
				SELECT $1, $2, m.id FROM crm.memberships m WHERE m.workspace_id = $1 AND m.identity_id = ANY($3) AND m.status = 'active'
				ON CONFLICT DO NOTHING`, sc.WS, id, *in.Members); err != nil {
				return err
			}
		}
		return shared.WriteAudit(r.Context(), tx, actorFromRequest(r, "ui").audit(sc.WS, "team.saved", "team", &id, nil, nil))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.handleListTeams(w, r)
}

func (h *Handler) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	h.saveTeam(w, r, uuid.Nil)
}

func (h *Handler) handleUpdateTeam(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "teamId"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("team_not_found"))
		return
	}
	var exists bool
	_ = h.store.Pool.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM crm.teams WHERE id = $1 AND workspace_id = $2)`, id, scopeFrom(r.Context()).WS).Scan(&exists)
	if !exists {
		shared.WriteError(w, r, shared.NotFound("team_not_found"))
		return
	}
	h.saveTeam(w, r, id)
}

func (h *Handler) handleDeleteTeam(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	if !h.canManageTeams(sc) {
		shared.WriteError(w, r, errForbidden)
		return
	}
	id, _ := uuid.Parse(chi.URLParam(r, "teamId"))
	tag, err := h.store.Pool.Exec(r.Context(), `DELETE FROM crm.teams WHERE id = $1 AND workspace_id = $2`, id, sc.WS)
	if err != nil || tag.RowsAffected() == 0 {
		shared.WriteError(w, r, shared.NotFound("team_not_found"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
