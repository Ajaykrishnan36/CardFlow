package records

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/access"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Workflow struct {
	ID          uuid.UUID    `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Status      string       `json:"status"`
	Draft       WorkflowDef  `json:"draft"`
	Published   *WorkflowDef `json:"published,omitempty"`
	Version     int          `json:"version"`
	HasChanges  bool         `json:"hasChanges"`
	WebhookURL  string       `json:"webhookUrl,omitempty"`
	NextRunAt   *time.Time   `json:"nextRunAt,omitempty"`
	LastRunAt   *time.Time   `json:"lastRunAt,omitempty"`
	CreatedBy   string       `json:"createdBy"`
	CreatedAt   time.Time    `json:"createdAt"`
	UpdatedAt   time.Time    `json:"updatedAt"`
	Runs        struct {
		Total  int `json:"total"`
		Failed int `json:"failed"`
	} `json:"runs"`
}

type WorkflowRun struct {
	ID         uuid.UUID      `json:"id"`
	WorkflowID uuid.UUID      `json:"workflowId"`
	Version    int            `json:"version"`
	Status     string         `json:"status"`
	Trigger    map[string]any `json:"trigger"`
	Steps      []StepLog      `json:"steps"`
	Error      string         `json:"error,omitempty"`
	ResumeAt   *time.Time     `json:"resumeAt,omitempty"`
	StartedBy  string         `json:"startedBy,omitempty"`
	StartedAt  time.Time      `json:"startedAt"`
	FinishedAt *time.Time     `json:"finishedAt,omitempty"`
}

func (h *Handler) canManageWorkflows(sc *Scope) bool {
	return sc.Owner || sc.Eff.HasCapability(access.CapWorkflows)
}

func (h *Handler) requireWorkflows(w http.ResponseWriter, r *http.Request) (*Scope, bool) {
	sc := scopeFrom(r.Context())
	if apiKeyFrom(r.Context()) != nil || !h.canManageWorkflows(sc) {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "You need the “Manage workflows” permission."))
		return nil, false
	}
	return sc, true
}

const workflowSelect = `SELECT w.id, w.name, w.description, w.status, w.draft, w.published, w.version, COALESCE(w.webhook_token, ''), w.next_run_at, w.last_run_at,
	COALESCE(i.display_name, ''), w.created_at, w.updated_at,
	(SELECT count(*) FROM crm.workflow_runs r WHERE r.workflow_id = w.id),
	(SELECT count(*) FROM crm.workflow_runs r WHERE r.workflow_id = w.id AND r.status = 'failed')
	FROM crm.workflows w LEFT JOIN crm.identities i ON i.id = w.created_by`

func (h *Handler) scanWorkflow(row pgx.Row) (*Workflow, error) {
	wf := &Workflow{}
	var draft, pub []byte
	var token string
	if err := row.Scan(&wf.ID, &wf.Name, &wf.Description, &wf.Status, &draft, &pub, &wf.Version, &token, &wf.NextRunAt, &wf.LastRunAt,
		&wf.CreatedBy, &wf.CreatedAt, &wf.UpdatedAt, &wf.Runs.Total, &wf.Runs.Failed); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(draft, &wf.Draft)
	if wf.Draft.Steps == nil {
		wf.Draft.Steps = []WorkflowStep{}
	}
	if len(pub) > 0 && string(pub) != "null" {
		var p WorkflowDef
		_ = json.Unmarshal(pub, &p)
		wf.Published = &p
	}
	a, _ := json.Marshal(wf.Draft)
	b, _ := json.Marshal(wf.Published)
	wf.HasChanges = wf.Published == nil || string(a) != string(b)
	if token != "" {
		wf.WebhookURL = strings.TrimRight(h.cfg.BaseURL, "/") + "/api/crm/v1/hooks/workflows/" + token
	}
	return wf, nil
}

func (h *Handler) handleListWorkflows(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	manage := h.canManageWorkflows(sc)
	rows, err := h.store.Pool.Query(r.Context(), workflowSelect+` WHERE w.workspace_id = $1 ORDER BY w.updated_at DESC`, sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []*Workflow{}
	for rows.Next() {
		wf, err := h.scanWorkflow(rows)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		// People who can't manage workflows only see the ones they may start by hand.
		if !manage {
			t := wf.Published
			if wf.Status != "active" || t == nil || t.Trigger.Type != "manual" || t.Trigger.Manual == nil || !t.Trigger.Manual.Everyone {
				continue
			}
			if t.Trigger.Object != "" && !sc.Can(t.Trigger.Object, "read") {
				continue
			}
			wf.Draft = WorkflowDef{Trigger: t.Trigger, Steps: []WorkflowStep{}}
			wf.Published = &WorkflowDef{Trigger: t.Trigger, Steps: []WorkflowStep{}}
			wf.WebhookURL = ""
		}
		out = append(out, wf)
	}
	respond(w, r, http.StatusOK, map[string]any{"data": out, "canManage": manage}, rows.Err())
}

func (h *Handler) loadWorkflow(ctx context.Context, ws, id uuid.UUID) (*Workflow, error) {
	wf, err := h.scanWorkflow(h.store.Pool.QueryRow(ctx, workflowSelect+` WHERE w.id = $1 AND w.workspace_id = $2`, id, ws))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, shared.NotFound("workflow_not_found")
	}
	return wf, err
}

func workflowID(r *http.Request) uuid.UUID {
	id, _ := uuid.Parse(chi.URLParam(r, "wfId"))
	return id
}

func (h *Handler) handleGetWorkflow(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireWorkflows(w, r)
	if !ok {
		return
	}
	wf, err := h.loadWorkflow(r.Context(), sc.WS, workflowID(r))
	respond(w, r, http.StatusOK, wf, err)
}

type workflowInput struct {
	Name        *string      `json:"name"`
	Description *string      `json:"description"`
	Draft       *WorkflowDef `json:"draft"`
}

func (h *Handler) handleCreateWorkflow(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireWorkflows(w, r)
	if !ok {
		return
	}
	var in workflowInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	name := ""
	if in.Name != nil {
		name = strings.TrimSpace(*in.Name)
	}
	if name == "" || len(name) > 100 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"name": "Name the workflow (up to 100 characters)."}))
		return
	}
	def := WorkflowDef{Trigger: WorkflowTrigger{Type: "record.created", Object: "leads"}, Steps: []WorkflowStep{}}
	if in.Draft != nil {
		def = *in.Draft
	}
	raw, _ := json.Marshal(def)
	desc := ""
	if in.Description != nil {
		desc = strings.TrimSpace(*in.Description)
	}
	var id uuid.UUID
	if err := h.store.Pool.QueryRow(r.Context(), `INSERT INTO crm.workflows (workspace_id, name, description, draft, created_by, updated_by)
		VALUES ($1, $2, $3, $4, $5, $5) RETURNING id`, sc.WS, name, desc, raw, actor(r)).Scan(&id); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	wf, err := h.loadWorkflow(r.Context(), sc.WS, id)
	respond(w, r, http.StatusCreated, wf, err)
}

func (h *Handler) handleUpdateWorkflow(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireWorkflows(w, r)
	if !ok {
		return
	}
	wf, err := h.loadWorkflow(r.Context(), sc.WS, workflowID(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in workflowInput
	if err := shared.DecodeJSONLimit(w, r, &in, 2<<20); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if in.Name != nil {
		wf.Name = strings.TrimSpace(*in.Name)
		if wf.Name == "" || len(wf.Name) > 100 {
			shared.WriteError(w, r, shared.Validation(map[string]string{"name": "Name the workflow (up to 100 characters)."}))
			return
		}
	}
	if in.Description != nil {
		wf.Description = strings.TrimSpace(*in.Description)
	}
	if in.Draft != nil {
		wf.Draft = *in.Draft
	}
	raw, _ := json.Marshal(wf.Draft)
	if _, err := h.store.Pool.Exec(r.Context(), `UPDATE crm.workflows SET name = $2, description = $3, draft = $4, updated_by = $5, updated_at = now() WHERE id = $1`,
		wf.ID, wf.Name, wf.Description, raw, actor(r)); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out, err := h.loadWorkflow(r.Context(), sc.WS, wf.ID)
	respond(w, r, http.StatusOK, out, err)
}

// POST /workflows/{wfId}/publish — the draft becomes the active version.
func (h *Handler) handlePublishWorkflow(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireWorkflows(w, r)
	if !ok {
		return
	}
	wf, err := h.loadWorkflow(r.Context(), sc.WS, workflowID(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	def := wf.Draft
	if fe := h.validateWorkflowDef(sc, &def); len(fe) > 0 {
		e := shared.Validation(fe)
		e.Message = "Fix these before publishing."
		shared.WriteError(w, r, e)
		return
	}
	var tz string
	_ = h.store.Pool.QueryRow(r.Context(), `SELECT timezone FROM crm.workspaces WHERE id = $1`, sc.WS).Scan(&tz)
	var next *time.Time
	if def.Trigger.Type == "schedule" {
		next = nextScheduleTime(def.Trigger.Schedule, tz, time.Now())
	}
	token := ""
	if def.Trigger.Type == "webhook" {
		_ = h.store.Pool.QueryRow(r.Context(), `SELECT COALESCE(webhook_token, '') FROM crm.workflows WHERE id = $1`, wf.ID).Scan(&token)
		if token == "" {
			t, _ := shared.RandomToken(24)
			token = "wh_" + t
		}
	}
	raw, _ := json.Marshal(def)
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var version int
		if err := tx.QueryRow(r.Context(), `UPDATE crm.workflows SET draft = $2, published = $2, version = version + 1, status = 'active', next_run_at = $3,
			webhook_token = COALESCE(webhook_token, NULLIF($4, '')), updated_by = $5, updated_at = now() WHERE id = $1 RETURNING version`,
			wf.ID, raw, next, token, actor(r)).Scan(&version); err != nil {
			return err
		}
		if _, err := tx.Exec(r.Context(), `INSERT INTO crm.workflow_versions (workflow_id, version, definition, published_by) VALUES ($1, $2, $3, $4)`,
			wf.ID, version, raw, actor(r)); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, a.audit(sc.WS, "workflow.published", "workflow", &wf.ID, nil, map[string]any{"name": wf.Name, "version": version}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.bus.Kick()
	out, err := h.loadWorkflow(r.Context(), sc.WS, wf.ID)
	respond(w, r, http.StatusOK, out, err)
}

// POST /workflows/{wfId}/status {"status":"active|inactive"}
func (h *Handler) handleWorkflowStatus(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireWorkflows(w, r)
	if !ok {
		return
	}
	var in struct {
		Status string `json:"status"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	wf, err := h.loadWorkflow(r.Context(), sc.WS, workflowID(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	switch in.Status {
	case "inactive":
		_, err = h.store.Pool.Exec(r.Context(), `UPDATE crm.workflows SET status = 'inactive', updated_at = now() WHERE id = $1`, wf.ID)
	case "active":
		if wf.Published == nil {
			shared.WriteError(w, r, shared.NewError(http.StatusConflict, "not_published", "Publish the workflow first."))
			return
		}
		var tz string
		_ = h.store.Pool.QueryRow(r.Context(), `SELECT timezone FROM crm.workspaces WHERE id = $1`, sc.WS).Scan(&tz)
		var next *time.Time
		if wf.Published.Trigger.Type == "schedule" {
			next = nextScheduleTime(wf.Published.Trigger.Schedule, tz, time.Now())
		}
		_, err = h.store.Pool.Exec(r.Context(), `UPDATE crm.workflows SET status = 'active', next_run_at = $2, updated_at = now() WHERE id = $1`, wf.ID, next)
	default:
		shared.WriteError(w, r, shared.Validation(map[string]string{"status": "Pick on or off."}))
		return
	}
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	_ = shared.WriteAudit(r.Context(), h.store.Pool, actorFromRequest(r, "ui").audit(sc.WS, "workflow."+in.Status, "workflow", &wf.ID, nil, nil))
	h.bus.Kick()
	out, err := h.loadWorkflow(r.Context(), sc.WS, wf.ID)
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) handleDeleteWorkflow(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireWorkflows(w, r)
	if !ok {
		return
	}
	id := workflowID(r)
	tag, err := h.store.Pool.Exec(r.Context(), `DELETE FROM crm.workflows WHERE id = $1 AND workspace_id = $2`, id, sc.WS)
	if err != nil || tag.RowsAffected() == 0 {
		shared.WriteError(w, r, shared.NotFound("workflow_not_found"))
		return
	}
	_ = shared.WriteAudit(r.Context(), h.store.Pool, actorFromRequest(r, "ui").audit(sc.WS, "workflow.deleted", "workflow", &id, nil, nil))
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) handleWorkflowVersions(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireWorkflows(w, r)
	if !ok {
		return
	}
	rows, err := h.store.Pool.Query(r.Context(), `SELECT v.version, v.definition, COALESCE(i.display_name, ''), v.published_at FROM crm.workflow_versions v
		JOIN crm.workflows w ON w.id = v.workflow_id LEFT JOIN crm.identities i ON i.id = v.published_by
		WHERE v.workflow_id = $1 AND w.workspace_id = $2 ORDER BY v.version DESC`, workflowID(r), sc.WS)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	type version struct {
		Version     int         `json:"version"`
		Definition  WorkflowDef `json:"definition"`
		PublishedBy string      `json:"publishedBy"`
		PublishedAt time.Time   `json:"publishedAt"`
	}
	out := []version{}
	for rows.Next() {
		var v version
		var raw []byte
		if err := rows.Scan(&v.Version, &raw, &v.PublishedBy, &v.PublishedAt); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		_ = json.Unmarshal(raw, &v.Definition)
		out = append(out, v)
	}
	respond(w, r, http.StatusOK, map[string]any{"data": out}, rows.Err())
}

// POST /workflows/{wfId}/versions/{version}/restore — copy an old version into the draft.
func (h *Handler) handleRestoreWorkflowVersion(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireWorkflows(w, r)
	if !ok {
		return
	}
	var raw []byte
	err := h.store.Pool.QueryRow(r.Context(), `SELECT v.definition FROM crm.workflow_versions v JOIN crm.workflows w ON w.id = v.workflow_id
		WHERE v.workflow_id = $1 AND w.workspace_id = $2 AND v.version = $3`, workflowID(r), sc.WS, chi.URLParam(r, "version")).Scan(&raw)
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("version_not_found"))
		return
	}
	if _, err := h.store.Pool.Exec(r.Context(), `UPDATE crm.workflows SET draft = $2, updated_at = now() WHERE id = $1`, workflowID(r), raw); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	out, err := h.loadWorkflow(r.Context(), sc.WS, workflowID(r))
	respond(w, r, http.StatusOK, out, err)
}

func (h *Handler) scanRun(row pgx.Row) (*WorkflowRun, error) {
	run := &WorkflowRun{}
	var trig, steps []byte
	var errText *string
	if err := row.Scan(&run.ID, &run.WorkflowID, &run.Version, &run.Status, &trig, &steps, &errText, &run.ResumeAt, &run.StartedBy, &run.StartedAt, &run.FinishedAt); err != nil {
		return nil, err
	}
	_ = json.Unmarshal(trig, &run.Trigger)
	_ = json.Unmarshal(steps, &run.Steps)
	if run.Steps == nil {
		run.Steps = []StepLog{}
	}
	if errText != nil {
		run.Error = *errText
	}
	return run, nil
}

const runSelect = `SELECT r.id, r.workflow_id, r.version, r.status, r.trigger, r.steps, r.error, r.resume_at, COALESCE(i.display_name, ''), r.started_at, r.finished_at
	FROM crm.workflow_runs r LEFT JOIN crm.identities i ON i.id = r.started_by`

func (h *Handler) handleListRuns(w http.ResponseWriter, r *http.Request) {
	sc, ok := h.requireWorkflows(w, r)
	if !ok {
		return
	}
	status := r.URL.Query().Get("status")
	rows, err := h.store.Pool.Query(r.Context(), runSelect+` WHERE r.workflow_id = $1 AND r.workspace_id = $2 AND ($3 = '' OR r.status = $3)
		ORDER BY r.started_at DESC LIMIT 100`, workflowID(r), sc.WS, status)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []*WorkflowRun{}
	for rows.Next() {
		run, err := h.scanRun(rows)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out = append(out, run)
	}
	respond(w, r, http.StatusOK, map[string]any{"data": out}, rows.Err())
}

func (h *Handler) runAction(action string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sc, ok := h.requireWorkflows(w, r)
		if !ok {
			return
		}
		id, _ := uuid.Parse(chi.URLParam(r, "runId"))
		var sql string
		switch action {
		case "stop":
			sql = `UPDATE crm.workflow_runs SET status = 'stopped', finished_at = now(), error = 'Stopped by hand.' WHERE id = $1 AND workspace_id = $2 AND status IN ('queued', 'running', 'waiting')`
		case "retry":
			// Carry on from the step that failed (its log entry stays for the record).
			sql = `UPDATE crm.workflow_runs SET status = 'queued', resume_at = now(), error = NULL, finished_at = NULL WHERE id = $1 AND workspace_id = $2 AND status IN ('failed', 'stopped')`
		}
		tag, err := h.store.Pool.Exec(r.Context(), sql, id, sc.WS)
		if err != nil || tag.RowsAffected() == 0 {
			shared.WriteError(w, r, shared.NewError(http.StatusConflict, "run_not_changed", "This run can't be "+map[string]string{"stop": "stopped", "retry": "retried"}[action]+" now."))
			return
		}
		h.bus.Kick()
		run, err := h.scanRun(h.store.Pool.QueryRow(r.Context(), runSelect+` WHERE r.id = $1`, id))
		respond(w, r, http.StatusOK, run, err)
	}
}

// POST /workflows/{wfId}/run {"recordIds":[…],"input":{…},"test":bool}
// Manual runs of the published version; "test" runs the draft (people who manage workflows).
func (h *Handler) handleRunWorkflow(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	wf, err := h.loadWorkflow(r.Context(), sc.WS, workflowID(r))
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in struct {
		RecordIDs []uuid.UUID    `json:"recordIds"`
		Input     map[string]any `json:"input"`
		Test      bool           `json:"test"`
		Record    map[string]any `json:"record"`
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	manage := h.canManageWorkflows(sc)
	def := wf.Published
	version := wf.Version
	if in.Test {
		if !manage {
			shared.WriteError(w, r, shared.Forbidden("forbidden", "You need the “Manage workflows” permission."))
			return
		}
		d := wf.Draft
		if fe := h.validateWorkflowDef(sc, &d); len(fe) > 0 {
			e := shared.Validation(fe)
			e.Message = "Fix these before testing."
			shared.WriteError(w, r, e)
			return
		}
		def, version = &d, 0
	} else {
		if def == nil || wf.Status != "active" {
			shared.WriteError(w, r, shared.NewError(http.StatusConflict, "not_active", "Turn the workflow on first."))
			return
		}
		if def.Trigger.Type != "manual" {
			shared.WriteError(w, r, shared.NewError(http.StatusConflict, "not_manual", "This workflow starts by itself; use Test to try it."))
			return
		}
		if !manage && (def.Trigger.Manual == nil || !def.Trigger.Manual.Everyone) {
			shared.WriteError(w, r, shared.Forbidden("forbidden", "You can't run this workflow."))
			return
		}
	}
	// Check the manual form.
	if def.Trigger.Manual != nil {
		fe := map[string]string{}
		for _, f := range def.Trigger.Manual.Form {
			if f.Required && asString(in.Input[f.Key]) == "" {
				fe["input."+f.Key] = f.Label + " is required."
			}
		}
		if len(fe) > 0 {
			shared.WriteError(w, r, shared.Validation(fe))
			return
		}
	}
	me := actor(r)
	obj := def.Trigger.Object
	var creator *uuid.UUID
	if id, err := uuid.Parse(wf.CreatedBy); err == nil {
		creator = &id
	}
	_ = h.store.Pool.QueryRow(r.Context(), `SELECT created_by FROM crm.workflows WHERE id = $1`, wf.ID).Scan(&creator)
	started := []uuid.UUID{}
	startOne := func(trig map[string]any) error {
		id, err := h.startRun(r.Context(), h.store.Pool, sc.WS, wf.ID, version, *def, wf.Name, creator, trig, &me, 0)
		if err == nil {
			started = append(started, id)
		}
		return err
	}
	if len(in.RecordIDs) > 0 && obj != "" {
		spec := specFor(obj)
		if spec == nil || !sc.Can(obj, "read") {
			shared.WriteError(w, r, errForbidden)
			return
		}
		if len(in.RecordIDs) > 500 {
			shared.WriteError(w, r, shared.Validation(map[string]string{"recordIds": "Run on at most 500 records at once."}))
			return
		}
		for _, rid := range in.RecordIDs {
			row, _, err := h.getRow(r.Context(), h.store.Pool, sc.WS, spec, rid, ownerFilter(r, spec))
			if err != nil {
				continue
			}
			if err := startOne(map[string]any{"type": "manual", "object": obj, "recordId": rid.String(), "record": rowSnapshot(row), "input": in.Input, "actorId": me}); err != nil {
				shared.WriteError(w, r, err)
				return
			}
		}
	} else {
		trig := map[string]any{"type": "manual", "input": in.Input, "actorId": me}
		if in.Test && in.Record != nil {
			trig["record"] = in.Record
			if id, ok := in.Record["id"].(string); ok {
				trig["recordId"] = id
			}
			trig["object"] = obj
		}
		if err := startOne(trig); err != nil {
			shared.WriteError(w, r, err)
			return
		}
	}
	_, _ = h.store.Pool.Exec(r.Context(), `UPDATE crm.workflows SET last_run_at = now() WHERE id = $1`, wf.ID)
	h.bus.Kick()
	shared.WriteJSON(w, http.StatusAccepted, map[string]any{"runs": started})
}

// POST /hooks/workflows/{token} — public: an outside system starts a webhook workflow.
func (h *Handler) HandleWorkflowWebhook(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")
	var id, ws uuid.UUID
	var name string
	var version int
	var raw []byte
	var creator *uuid.UUID
	err := h.store.Pool.QueryRow(r.Context(), `SELECT id, workspace_id, name, version, published, created_by FROM crm.workflows
		WHERE webhook_token = $1 AND status = 'active' AND published->'trigger'->>'type' = 'webhook'`, token).Scan(&id, &ws, &name, &version, &raw, &creator)
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("workflow_not_found"))
		return
	}
	var def WorkflowDef
	_ = json.Unmarshal(raw, &def)
	var body any
	if r.Method == http.MethodPost {
		if err := shared.DecodeJSONLimit(w, r, &body, 256<<10); err != nil {
			shared.WriteError(w, r, err)
			return
		}
	}
	query := map[string]any{}
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			query[k] = v[0]
		}
	}
	runID, err := h.startRun(r.Context(), h.store.Pool, ws, id, version, def, name, creator, map[string]any{"type": "webhook", "body": body, "query": query}, nil, 0)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.bus.Kick()
	shared.WriteJSON(w, http.StatusAccepted, map[string]any{"ok": true, "runId": runID})
}
