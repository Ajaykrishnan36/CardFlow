package records

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strings"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/platform"
	"cardflow-backend/internal/crm/shared"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// A business's own lead statuses and deal stages (D-182): whoever may customise the
// business sets them in Settings › Stages. They replace the product setup's lists for that
// business only. The steps the CRM itself relies on keep their keys: a lead is always
// "converted" or "lost" at the end, a deal "closed_won" (100%) or "closed_lost" (0%).

var stageKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

type stageItem struct {
	Value       string `json:"value"`
	Label       string `json:"label"`
	Probability *int   `json:"probability,omitempty"`
	Tone        string `json:"tone,omitempty"`
	Fixed       bool   `json:"fixed,omitempty"`
	InUse       int    `json:"inUse"`
}

var fixedStages = map[string]map[string]bool{
	"leads":         {"converted": true, "lost": true},
	"opportunities": {"closed_won": true, "closed_lost": true},
}

func (h *Handler) stageCounts(r *http.Request, spec *objectSpec, ws uuid.UUID) map[string]int {
	out := map[string]int{}
	rows, err := h.store.Pool.Query(r.Context(), `SELECT COALESCE(status, ''), count(*) FROM crm.`+spec.Table+`
		WHERE workspace_id = $1 AND deleted_at IS NULL GROUP BY 1`, ws)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var k string
		var n int
		if rows.Scan(&k, &n) == nil {
			out[k] = n
		}
	}
	return out
}

func (h *Handler) handleGetStages(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "meta")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if fixedStages[spec.Key] == nil {
		shared.WriteError(w, r, shared.NotFound("object_not_found"))
		return
	}
	cfg, _ := cachedSetup(r.Context(), h.store.Pool, ws)
	prob := map[string]int{}
	for _, s := range cfg.PipelineStages {
		prob[s.Key] = s.Probability
	}
	counts := h.stageCounts(r, spec, ws)
	items := []stageItem{}
	for _, s := range statusesFor(r.Context(), h.store.Pool, ws, spec) {
		it := stageItem{Value: s.Value, Label: s.Label, Tone: s.Tone, Fixed: fixedStages[spec.Key][s.Value], InUse: counts[s.Value]}
		if spec.Key == "opportunities" {
			p := prob[s.Value]
			it.Probability = &p
		}
		items = append(items, it)
	}
	var custom bool
	_ = h.store.Pool.QueryRow(r.Context(), `SELECT COALESCE(stage_settings ? $2, false) FROM crm.workspaces WHERE id = $1`, ws,
		map[string]string{"leads": "leadStatuses", "opportunities": "pipelineStages"}[spec.Key]).Scan(&custom)
	shared.WriteJSON(w, http.StatusOK, map[string]any{"object": spec.Key, "stages": items, "custom": custom,
		"canEdit": scopeFrom(r.Context()).CanCustomize()})
}

// handleSaveStages replaces the business's statuses or stages. Sending an empty list goes
// back to the product setup's own (when no record uses a stage that would disappear).
func (h *Handler) handleSaveStages(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "customize")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	fixed := fixedStages[spec.Key]
	if fixed == nil {
		shared.WriteError(w, r, shared.NotFound("object_not_found"))
		return
	}
	var in struct {
		Stages []stageItem `json:"stages"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	fe := map[string]string{}
	seen := map[string]bool{}
	var leads []platform.Option
	var stages []platform.Stage
	open := 0
	for i, s := range in.Stages {
		s.Value, s.Label = strings.TrimSpace(s.Value), strings.TrimSpace(s.Label)
		if s.Value == "" {
			s.Value = snakeKey(s.Label)
		}
		key := fmt.Sprintf("stages.%d", i)
		switch {
		case !stageKeyRe.MatchString(s.Value):
			fe[key] = "Use a key of lower-case letters, digits or underscores."
		case s.Label == "" || len(s.Label) > 40:
			fe[key] = "Give each stage a name (up to 40 characters)."
		case seen[s.Value]:
			fe[key] = "Each stage can be in the list once."
		}
		seen[s.Value] = true
		if spec.Key == "leads" {
			leads = append(leads, platform.Option{Value: s.Value, Label: s.Label})
			continue
		}
		p := 0
		if s.Probability != nil {
			p = *s.Probability
		}
		switch s.Value {
		case "closed_won":
			p = 100
		case "closed_lost":
			p = 0
		default:
			open++
			if p < 0 || p > 99 {
				fe[key] = "An open stage's probability is 0–99%."
			}
		}
		stages = append(stages, platform.Stage{Key: s.Value, Label: s.Label, Probability: p})
	}
	if len(in.Stages) > 0 {
		for k := range fixed {
			if !seen[k] {
				fe["stages"] = "Keep the closing steps (" + strings.Join(fixedKeyList(fixed), ", ") + "); you can rename them."
			}
		}
		if len(in.Stages) > 30 {
			fe["stages"] = "Up to 30 stages."
		}
		if spec.Key == "opportunities" && open == 0 {
			fe["stages"] = "Add at least one open stage before Closed won and Closed lost."
		}
		if spec.Key == "leads" && len(in.Stages)-len(fixed) < 1 {
			fe["stages"] = "Add at least one open status before Converted and Closed lost."
		}
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	// A stage that records are still at can't disappear: move them first.
	counts := h.stageCounts(r, spec, ws)
	keep := seen
	if len(in.Stages) == 0 {
		var d platform.ProductConfig
		d, _ = platform.WorkspaceSetupWithoutStages(ctx, h.store.Pool, ws)
		keep = map[string]bool{"converted": true, "lost": true}
		for _, o := range d.LeadStatuses {
			keep[o.Value] = true
		}
		for _, s := range d.PipelineStages {
			keep[s.Key] = true
		}
	}
	for k, n := range counts {
		if k != "" && n > 0 && !keep[k] {
			fe["stages"] = fmt.Sprintf("%d %s still at \"%s\". Move them to another stage first.", n, strings.ToLower(spec.Plural), k)
		}
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	path := map[string]string{"leads": "leadStatuses", "opportunities": "pipelineStages"}[spec.Key]
	var raw []byte
	if spec.Key == "leads" {
		raw, _ = json.Marshal(leads)
	} else {
		raw, _ = json.Marshal(stages)
	}
	actor := identity.SessionFrom(ctx).IdentityID
	err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		if len(in.Stages) == 0 {
			if _, err := tx.Exec(ctx, `UPDATE crm.workspaces SET stage_settings = COALESCE(stage_settings, '{}'::jsonb) - $2, updated_at = now() WHERE id = $1`, ws, path); err != nil {
				return err
			}
		} else if _, err := tx.Exec(ctx, `UPDATE crm.workspaces SET stage_settings = jsonb_set(COALESCE(stage_settings, '{}'::jsonb), ARRAY[$2::text], $3::jsonb, true), updated_at = now() WHERE id = $1`,
			ws, path, raw); err != nil {
			return err
		}
		meta := identity.Meta(r)
		return shared.WriteAudit(ctx, tx, shared.AuditEvent{WorkspaceID: &ws, ActorID: &actor, Action: "stages.updated", EntityType: "object",
			After: map[string]any{"object": spec.Key, "stages": json.RawMessage(raw)}, IP: meta.IP, RequestID: meta.RequestID})
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ForgetSetup()
	h.handleGetStages(w, r)
}

func fixedKeyList(m map[string]bool) []string {
	out := []string{}
	for _, k := range []string{"converted", "lost", "closed_won", "closed_lost"} {
		if m[k] {
			out = append(out, k)
		}
	}
	return out
}
