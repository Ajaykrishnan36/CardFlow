package records

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/dop251/goja"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Workflows (D-63). A workflow is a trigger and a list of steps. Triggers: a record is
// created / updated (optionally only when some fields change) / created or updated /
// deleted, someone starts it by hand (on records, in bulk or on its own, with an optional
// form), a schedule, or an incoming webhook. Steps: create, update, upsert, delete and
// find records, send an email, call a URL, notify people, assign a record (round robin,
// least busy or random), run a little JavaScript, wait, branch with if / else, and loop
// over a list. Values can use {{placeholders}} from the trigger and earlier steps.
//
// A workflow has a draft and published versions; publishing makes the draft the active
// version. Runs keep a log of every step, can wait (delays) and resume after a restart,
// can be stopped, and a failed run can be retried from the step that failed. Workflows
// act with full access to the records they touch, so building them needs the
// "Manage workflows" permission.

type WorkflowTrigger struct {
	Type     string         `json:"type"` // record.created | record.updated | record.upserted | record.deleted | manual | schedule | webhook
	Object   string         `json:"object,omitempty"`
	Fields   []string       `json:"fields,omitempty"` // record.updated: only when one of these changes
	Filter   *FilterNode    `json:"filter,omitempty"` // the record must match
	Schedule *WorkflowSched `json:"schedule,omitempty"`
	Manual   *struct {
		Mode     string          `json:"mode,omitempty"`     // single | bulk | global
		Everyone bool            `json:"everyone,omitempty"` // anyone who can read the object may run it
		Form     []WorkflowInput `json:"form,omitempty"`
	} `json:"manual,omitempty"`
}

type WorkflowSched struct {
	Every int    `json:"every,omitempty"` // with Unit
	Unit  string `json:"unit,omitempty"`  // minutes | hours | days | weeks
	Cron  string `json:"cron,omitempty"`  // "m h dom mon dow" in the workspace's time zone
}

type WorkflowInput struct {
	Key      string   `json:"key"`
	Label    string   `json:"label"`
	Type     string   `json:"type"` // text | number | date | boolean | select
	Required bool     `json:"required,omitempty"`
	Options  []string `json:"options,omitempty"`
}

type WorkflowStep struct {
	ID     string         `json:"id"`
	Type   string         `json:"type"`
	Name   string         `json:"name,omitempty"`
	Config map[string]any `json:"config,omitempty"`
	Then   []WorkflowStep `json:"then,omitempty"` // if / loop body
	Else   []WorkflowStep `json:"else,omitempty"`
}

type WorkflowDef struct {
	Trigger WorkflowTrigger `json:"trigger"`
	Steps   []WorkflowStep  `json:"steps"`
}

var stepTypes = map[string]bool{"create_record": true, "update_record": true, "upsert_record": true, "delete_record": true, "find_records": true,
	"send_email": true, "http_request": true, "notify": true, "assign": true, "code": true, "delay": true, "if": true, "loop": true, "stop": true}

// ---- validation ----

func (h *Handler) validateWorkflowDef(sc *Scope, d *WorkflowDef) map[string]string {
	fe := map[string]string{}
	t := &d.Trigger
	switch t.Type {
	case "record.created", "record.updated", "record.upserted", "record.deleted":
		if specFor(t.Object) == nil || !sc.Enabled(t.Object) {
			fe["trigger.object"] = "Pick which records start this workflow."
		}
	case "manual":
		if t.Manual == nil {
			t.Manual = &struct {
				Mode     string          `json:"mode,omitempty"`
				Everyone bool            `json:"everyone,omitempty"`
				Form     []WorkflowInput `json:"form,omitempty"`
			}{Mode: "global"}
		}
		if t.Manual.Mode != "single" && t.Manual.Mode != "bulk" {
			t.Manual.Mode = "global"
		}
		if t.Manual.Mode != "global" && (specFor(t.Object) == nil || !sc.Enabled(t.Object)) {
			fe["trigger.object"] = "Pick which records the workflow runs on."
		}
		for i, in := range t.Manual.Form {
			if !regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_]{0,40}$`).MatchString(in.Key) || strings.TrimSpace(in.Label) == "" {
				fe[fmt.Sprintf("trigger.manual.form.%d", i)] = "Every form field needs a label and a key (letters, digits, _)."
			}
		}
	case "schedule":
		s := t.Schedule
		if s == nil || (s.Cron == "" && (s.Every < 1 || !map[string]bool{"minutes": true, "hours": true, "days": true, "weeks": true}[s.Unit])) {
			fe["trigger.schedule"] = "Say how often it runs."
		} else if s.Cron != "" {
			if _, err := parseCron(s.Cron); err != nil {
				fe["trigger.schedule"] = "That cron expression isn't valid: " + err.Error()
			}
		} else if s.Unit == "minutes" && s.Every < 5 {
			fe["trigger.schedule"] = "Run at most every 5 minutes."
		}
	case "webhook":
	default:
		fe["trigger.type"] = "Pick what starts the workflow."
	}
	if t.Filter != nil && specFor(t.Object) != nil {
		fields, _ := allFieldsRaw(context.Background(), h.store.Pool, sc.WS, specFor(t.Object))
		if _, errs := compileFilter(t.Filter, fields, filterEnv{}, &sqlBuilder{}); len(errs) > 0 {
			fe["trigger.filter"] = "Fix the conditions: " + errMessage(shared.Validation(errs))
		}
	}
	count := 0
	seen := map[string]bool{}
	var walk func(list []WorkflowStep, path string)
	walk = func(list []WorkflowStep, path string) {
		for i := range list {
			s := &list[i]
			count++
			p := fmt.Sprintf("%s.%d", path, i)
			if s.ID == "" || seen[s.ID] {
				s.ID = "s" + strings.ReplaceAll(uuid.NewString()[:6], "-", "")
			}
			seen[s.ID] = true
			if !stepTypes[s.Type] {
				fe[p] = "Pick what this step does."
				continue
			}
			if s.Config == nil {
				s.Config = map[string]any{}
			}
			if obj, ok := s.Config["object"].(string); ok && obj != "" && (specFor(obj) == nil || !sc.Enabled(obj)) {
				fe[p] = "Pick an object switched on in this workspace."
			}
			switch s.Type {
			case "create_record", "upsert_record", "update_record", "delete_record", "find_records":
				if _, ok := s.Config["object"].(string); !ok {
					fe[p] = "Pick which records this step works on."
				}
			case "send_email":
				if strings.TrimSpace(str(s.Config["to"])) == "" || strings.TrimSpace(str(s.Config["subject"])) == "" {
					fe[p] = "An email needs a recipient and a subject."
				}
			case "notify":
				if len(asStrings(s.Config["to"])) == 0 {
					fe[p] = "Choose who gets the notification."
				}
			case "http_request":
				if strings.TrimSpace(str(s.Config["url"])) == "" {
					fe[p] = "Enter the URL to call."
				}
			case "code":
				if len(str(s.Config["source"])) > 20_000 {
					fe[p] = "Keep code under 20,000 characters."
				}
			case "delay":
				if str(s.Config["until"]) == "" {
					if n, ok := asNumber(s.Config["amount"]); !ok || n < 1 {
						fe[p] = "Say how long to wait."
					}
				}
			}
			walk(s.Then, p+".then")
			walk(s.Else, p+".else")
		}
	}
	walk(d.Steps, "steps")
	if count > 50 {
		fe["steps"] = "A workflow can have at most 50 steps."
	}
	return fe
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// ---- templates: {{trigger.record.email}}, {{steps.s2.record.id}}, {{loop.item.name}} ----

var tmplRe = regexp.MustCompile(`\{\{\s*([a-zA-Z0-9_.\[\]-]+)\s*\}\}`)

func lookupPath(vars map[string]any, path string) any {
	var cur any = vars
	for _, part := range strings.Split(path, ".") {
		switch x := cur.(type) {
		case map[string]any:
			cur = x[part]
		case []any:
			i, err := strconv.Atoi(part)
			if err != nil || i < 0 || i >= len(x) {
				return nil
			}
			cur = x[i]
		default:
			return nil
		}
	}
	return cur
}

// render fills placeholders. A string that is exactly one placeholder keeps the value's type.
func render(v any, vars map[string]any) any {
	switch x := v.(type) {
	case string:
		if m := tmplRe.FindStringSubmatch(x); m != nil && strings.TrimSpace(x) == m[0] {
			return lookupPath(vars, m[1])
		}
		return tmplRe.ReplaceAllStringFunc(x, func(s string) string {
			m := tmplRe.FindStringSubmatch(s)
			val := lookupPath(vars, m[1])
			switch y := val.(type) {
			case nil:
				return ""
			case string:
				return y
			case float64:
				return strconv.FormatFloat(y, 'f', -1, 64)
			case bool:
				return strconv.FormatBool(y)
			}
			raw, _ := json.Marshal(val)
			return string(raw)
		})
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = render(val, vars)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = render(val, vars)
		}
		return out
	}
	return v
}

// ---- compiled program: steps become instructions with jumps, so a run can stop at a
// delay and resume at a program counter ----

type instr struct {
	Op   string        // step | if | jump | loop | next
	Step *WorkflowStep // step / if / loop
	To   int           // jump target; if: where "else" starts; loop: after the loop
	Loop int           // next: the loop instruction
}

func compileSteps(steps []WorkflowStep) []instr {
	var out []instr
	var emit func(list []WorkflowStep)
	emit = func(list []WorkflowStep) {
		for i := range list {
			s := &list[i]
			switch s.Type {
			case "if":
				at := len(out)
				out = append(out, instr{Op: "if", Step: s})
				emit(s.Then)
				jump := len(out)
				out = append(out, instr{Op: "jump"})
				out[at].To = len(out)
				emit(s.Else)
				out[jump].To = len(out)
			case "loop":
				at := len(out)
				out = append(out, instr{Op: "loop", Step: s})
				emit(s.Then)
				out = append(out, instr{Op: "next", Loop: at})
				out[at].To = len(out)
			default:
				out = append(out, instr{Op: "step", Step: s})
			}
		}
	}
	emit(steps)
	return out
}

// ---- runs ----

type runState struct {
	Def   WorkflowDef    `json:"def"`
	Vars  map[string]any `json:"vars"`
	Loops map[string]struct {
		Items []any `json:"items"`
		Index int   `json:"index"`
	} `json:"loops,omitempty"`
	Iterations int    `json:"iterations"`
	Name       string `json:"name"`
	CreatorID  string `json:"creatorId,omitempty"`
	Depth      int    `json:"depth"`
}

type StepLog struct {
	StepID   string    `json:"stepId"`
	Type     string    `json:"type"`
	Name     string    `json:"name,omitempty"`
	Status   string    `json:"status"` // ok | failed | skipped | waiting
	Output   any       `json:"output,omitempty"`
	Error    string    `json:"error,omitempty"`
	At       time.Time `json:"at"`
	Duration int64     `json:"durationMs"`
}

// startRun queues a run of a workflow's published (or, for tests, draft) definition.
func (h *Handler) startRun(ctx context.Context, q interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}, ws, workflowID uuid.UUID, version int, def WorkflowDef, name string, creator *uuid.UUID, trigger map[string]any, startedBy *uuid.UUID, depth int) (uuid.UUID, error) {
	st := runState{Def: def, Vars: map[string]any{"trigger": trigger, "steps": map[string]any{}}, Name: name, Depth: depth}
	if creator != nil {
		st.CreatorID = creator.String()
	}
	raw, _ := json.Marshal(st)
	trig, _ := json.Marshal(trigger)
	var id uuid.UUID
	// Every start (record event, schedule, webhook, manual or test) stamps the workflow's last run.
	err := q.QueryRow(ctx, `WITH touched AS (UPDATE crm.workflows SET last_run_at = now() WHERE id = $2)
		INSERT INTO crm.workflow_runs (workspace_id, workflow_id, version, status, trigger, context, resume_at, started_by)
		VALUES ($1, $2, $3, 'queued', $4, $5, now(), $6) RETURNING id`, ws, workflowID, version, trig, raw, startedBy).Scan(&id)
	return id, err
}

// triggerWorkflows starts the active workflows a record event matches (inside the relay).
func (h *Handler) triggerWorkflows(ctx context.Context, tx pgx.Tx, ev Event) error {
	if ev.Depth >= 5 {
		return nil
	}
	types := map[string][]string{
		"record.created":  {"record.created", "record.upserted"},
		"record.updated":  {"record.updated", "record.upserted"},
		"record.deleted":  {"record.deleted"},
		"record.restored": {"record.created"},
	}[ev.Type]
	if len(types) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `SELECT id, name, version, published, created_by FROM crm.workflows
		WHERE workspace_id = $1 AND status = 'active' AND published->'trigger'->>'type' = ANY($2) AND published->'trigger'->>'object' = $3`,
		ev.WorkspaceID, types, ev.Object)
	if err != nil {
		return err
	}
	type wf struct {
		id      uuid.UUID
		name    string
		version int
		def     WorkflowDef
		creator *uuid.UUID
	}
	var list []wf
	for rows.Next() {
		var w wf
		var raw []byte
		if err := rows.Scan(&w.id, &w.name, &w.version, &raw, &w.creator); err != nil {
			rows.Close()
			return err
		}
		_ = json.Unmarshal(raw, &w.def)
		list = append(list, w)
	}
	rows.Close()
	spec := specFor(ev.Object)
	for _, w := range list {
		if ev.WorkflowID != nil && *ev.WorkflowID == w.id {
			continue // a workflow never triggers itself
		}
		t := w.def.Trigger
		if t.Type == "record.updated" && len(t.Fields) > 0 {
			hit := false
			for _, f := range t.Fields {
				hit = hit || contains(ev.Changed, f)
			}
			if !hit {
				continue
			}
		}
		if t.Filter != nil && spec != nil {
			fields, err := allFieldsRaw(ctx, tx, ev.WorkspaceID, spec)
			if err != nil {
				return err
			}
			b := &sqlBuilder{}
			where, fe := whereFor(spec, fields, ev.WorkspaceID, listParams{IDs: []uuid.UUID{ev.RecordID}, Filter: t.Filter, Deleted: ev.Type == "record.deleted"}, b)
			if len(fe) > 0 {
				continue
			}
			var match bool
			if err := tx.QueryRow(ctx, "SELECT EXISTS (SELECT 1 FROM crm."+spec.Table+" t"+where+")", b.args...).Scan(&match); err != nil || !match {
				continue
			}
		}
		trig := map[string]any{"type": ev.Type, "object": ev.Object, "recordId": ev.RecordID.String(), "record": ev.Record,
			"previous": ev.Previous, "changedFields": ev.Changed, "actorId": ev.ActorID}
		if _, err := h.startRun(ctx, tx, ev.WorkspaceID, w.id, w.version, w.def, w.name, w.creator, trig, nil, ev.Depth+1); err != nil {
			return err
		}
		_, _ = tx.Exec(ctx, `UPDATE crm.workflows SET last_run_at = now() WHERE id = $1`, w.id)
	}
	return nil
}

// runDueWorkflows starts schedules that are due and runs queued / waiting runs.
func (h *Handler) runDueWorkflows(ctx context.Context) {
	// Schedules.
	_ = h.store.WithTx(ctx, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT w.id, w.workspace_id, w.name, w.version, w.published, w.created_by, ws.timezone FROM crm.workflows w
			JOIN crm.workspaces ws ON ws.id = w.workspace_id
			WHERE w.status = 'active' AND w.next_run_at <= now() FOR UPDATE OF w SKIP LOCKED LIMIT 20`)
		if err != nil {
			return err
		}
		type due struct {
			id, ws  uuid.UUID
			name    string
			version int
			def     WorkflowDef
			creator *uuid.UUID
			tz      string
		}
		var list []due
		for rows.Next() {
			var d due
			var raw []byte
			if err := rows.Scan(&d.id, &d.ws, &d.name, &d.version, &raw, &d.creator, &d.tz); err != nil {
				rows.Close()
				return err
			}
			_ = json.Unmarshal(raw, &d.def)
			list = append(list, d)
		}
		rows.Close()
		for _, d := range list {
			if _, err := h.startRun(ctx, tx, d.ws, d.id, d.version, d.def, d.name, d.creator, map[string]any{"type": "schedule", "at": time.Now().UTC()}, nil, 0); err != nil {
				return err
			}
			next := nextScheduleTime(d.def.Trigger.Schedule, d.tz, time.Now())
			if _, err := tx.Exec(ctx, `UPDATE crm.workflows SET next_run_at = $2, last_run_at = now() WHERE id = $1`, d.id, next); err != nil {
				return err
			}
		}
		return nil
	})
	// Runs.
	for round := 0; round < 20; round++ {
		rows, err := h.store.Pool.Query(ctx, `UPDATE crm.workflow_runs SET status = 'running', resume_at = now() + interval '10 minutes'
			WHERE id IN (SELECT id FROM crm.workflow_runs WHERE status IN ('queued', 'waiting') AND resume_at <= now() ORDER BY resume_at LIMIT 10 FOR UPDATE SKIP LOCKED)
			RETURNING id`)
		if err != nil {
			slog.Error("CRM workflows", "error", err)
			return
		}
		var ids []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		if len(ids) == 0 {
			return
		}
		for _, id := range ids {
			h.executeRun(ctx, id)
		}
	}
}

func nextScheduleTime(s *WorkflowSched, tz string, from time.Time) *time.Time {
	if s == nil {
		return nil
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		loc = istLocation
	}
	if s.Cron != "" {
		c, err := parseCron(s.Cron)
		if err != nil {
			return nil
		}
		t := c.next(from.In(loc))
		return &t
	}
	var d time.Duration
	switch s.Unit {
	case "minutes":
		d = time.Duration(s.Every) * time.Minute
	case "hours":
		d = time.Duration(s.Every) * time.Hour
	case "days":
		d = time.Duration(s.Every) * 24 * time.Hour
	case "weeks":
		d = time.Duration(s.Every) * 7 * 24 * time.Hour
	default:
		return nil
	}
	t := from.Add(d)
	return &t
}

var errRunStopped = errors.New("stopped")

// executeRun runs instructions from the run's program counter until it finishes,
// waits (delay) or fails.
func (h *Handler) executeRun(ctx context.Context, runID uuid.UUID) {
	var ws, workflowID uuid.UUID
	var raw, stepsRaw []byte
	var pc int
	err := h.store.Pool.QueryRow(ctx, `SELECT workspace_id, workflow_id, context, step_index, steps FROM crm.workflow_runs WHERE id = $1`, runID).
		Scan(&ws, &workflowID, &raw, &pc, &stepsRaw)
	if err != nil {
		return
	}
	var st runState
	_ = json.Unmarshal(raw, &st)
	var logs []StepLog
	_ = json.Unmarshal(stepsRaw, &logs)
	if st.Vars == nil {
		st.Vars = map[string]any{}
	}
	if st.Vars["steps"] == nil {
		st.Vars["steps"] = map[string]any{}
	}
	prog := compileSteps(st.Def.Steps)
	var creator *uuid.UUID
	if id, err := uuid.Parse(st.CreatorID); err == nil {
		creator = &id
	}
	wfID := workflowID
	a := actorInfo{Kind: "system", Source: "workflow", WorkflowID: &wfID, Depth: st.Depth}
	status, errText := "completed", ""
	var resume *time.Time
	save := func() {
		ctxRaw, _ := json.Marshal(st)
		logRaw, _ := json.Marshal(logs)
		finished := "now()"
		if status == "waiting" || status == "running" {
			finished = "NULL"
		}
		_, _ = h.store.Pool.Exec(ctx, `UPDATE crm.workflow_runs SET status = $2, context = $3, steps = $4, step_index = $5, error = NULLIF($6, ''),
			resume_at = $7, finished_at = `+finished+` WHERE id = $1`, runID, status, ctxRaw, logRaw, pc, errText, resume)
	}
	for pc < len(prog) {
		// A stop request wins.
		var cur string
		if err := h.store.Pool.QueryRow(ctx, `SELECT status FROM crm.workflow_runs WHERE id = $1`, runID).Scan(&cur); err == nil && cur == "stopped" {
			return
		}
		st.Iterations++
		if st.Iterations > 1000 {
			status, errText = "failed", "The workflow ran more than 1,000 steps and was stopped (check its loops)."
			break
		}
		in := prog[pc]
		switch in.Op {
		case "jump":
			pc = in.To
			continue
		case "if":
			ok := evalConditions(in.Step.Config, st.Vars)
			logs = append(logs, StepLog{StepID: in.Step.ID, Type: "if", Name: in.Step.Name, Status: "ok", Output: map[string]any{"result": ok}, At: time.Now()})
			if ok {
				pc++
			} else {
				pc = in.To
			}
			continue
		case "loop":
			if st.Loops == nil {
				st.Loops = map[string]struct {
					Items []any `json:"items"`
					Index int   `json:"index"`
				}{}
			}
			key := strconv.Itoa(pc)
			l, started := st.Loops[key]
			if !started {
				items, _ := render(in.Step.Config["items"], st.Vars).([]any)
				if len(items) > 200 {
					items = items[:200]
				}
				l.Items, l.Index = items, 0
				logs = append(logs, StepLog{StepID: in.Step.ID, Type: "loop", Name: in.Step.Name, Status: "ok", Output: map[string]any{"items": len(items)}, At: time.Now()})
			}
			if l.Index >= len(l.Items) {
				delete(st.Loops, key)
				delete(st.Vars, "loop")
				pc = in.To
				continue
			}
			st.Loops[key] = l
			st.Vars["loop"] = map[string]any{"item": l.Items[l.Index], "index": l.Index}
			pc++
			continue
		case "next":
			key := strconv.Itoa(in.Loop)
			l := st.Loops[key]
			l.Index++
			st.Loops[key] = l
			pc = in.Loop
			continue
		}
		s := in.Step
		started := time.Now()
		if s.Type == "delay" {
			until := time.Now()
			if u := render(s.Config["until"], st.Vars); u != nil && str(u) != "" {
				if t, err := time.Parse(time.RFC3339, str(u)); err == nil {
					until = t
				} else if t, err := time.ParseInLocation("2006-01-02", str(u), istLocation); err == nil {
					until = t
				}
			} else {
				n, _ := asNumber(s.Config["amount"])
				unit := map[string]time.Duration{"minutes": time.Minute, "hours": time.Hour, "days": 24 * time.Hour}[str(s.Config["unit"])]
				if unit == 0 {
					unit = time.Minute
				}
				until = time.Now().Add(time.Duration(n) * unit)
			}
			pc++
			logs = append(logs, StepLog{StepID: s.ID, Type: s.Type, Name: s.Name, Status: "waiting", Output: map[string]any{"until": until}, At: started})
			if until.After(time.Now().Add(time.Second)) {
				status, resume = "waiting", &until
				save()
				h.bus.wakeAt(until)
				return
			}
			continue
		}
		if s.Type == "stop" {
			logs = append(logs, StepLog{StepID: s.ID, Type: s.Type, Name: s.Name, Status: "ok", At: started})
			pc = len(prog)
			break
		}
		out, err := h.runStep(ctx, ws, s, st.Vars, a, creator)
		lg := StepLog{StepID: s.ID, Type: s.Type, Name: s.Name, Status: "ok", Output: out, At: started, Duration: time.Since(started).Milliseconds()}
		if err != nil {
			lg.Status, lg.Error = "failed", errMessageOrText(err)
			logs = append(logs, lg)
			status, errText = "failed", fmt.Sprintf("Step “%s” failed: %s", stepLabel(s), lg.Error)
			break
		}
		logs = append(logs, lg)
		st.Vars["steps"].(map[string]any)[s.ID] = out
		pc++
	}
	if len(logs) > 300 {
		logs = logs[len(logs)-300:]
	}
	save()
	h.bus.Kick()
	if status == "failed" && creator != nil {
		var code string
		_ = h.store.Pool.QueryRow(ctx, `SELECT code FROM crm.workspaces WHERE id = $1`, ws).Scan(&code)
		h.notify(ctx, h.store.Pool, ws, *creator, "workflow.failed", "Workflow “"+st.Name+"” failed", errText, "/crm/w/"+code+"/workflows/"+workflowID.String()+"?tab=runs", nil)
	}
}

func stepLabel(s *WorkflowStep) string {
	if s.Name != "" {
		return s.Name
	}
	return strings.ReplaceAll(s.Type, "_", " ")
}

func errMessageOrText(err error) string {
	var se *shared.Error
	if errors.As(err, &se) {
		return errMessage(se)
	}
	return err.Error()
}

// evalConditions: {"match":"all|any","conditions":[{"left":"{{…}}","op":"eq","right":"x"}]}
func evalConditions(cfg map[string]any, vars map[string]any) bool {
	list, _ := cfg["conditions"].([]any)
	matchAny := str(cfg["match"]) == "any"
	if len(list) == 0 {
		return true
	}
	for _, raw := range list {
		c, _ := raw.(map[string]any)
		left := render(c["left"], vars)
		right := render(c["right"], vars)
		ok := compareValues(left, str(c["op"]), right)
		if matchAny && ok {
			return true
		}
		if !matchAny && !ok {
			return false
		}
	}
	return !matchAny
}

func compareValues(left any, op string, right any) bool {
	ls, rs := strings.TrimSpace(asString(left)), strings.TrimSpace(asString(right))
	if arr, ok := left.([]any); ok {
		switch op {
		case "empty":
			return len(arr) == 0
		case "notEmpty":
			return len(arr) > 0
		case "contains":
			for _, x := range arr {
				if strings.EqualFold(asString(x), rs) {
					return true
				}
			}
			return false
		}
		raw, _ := json.Marshal(arr)
		ls = string(raw)
	}
	ln, lok := asNumber(left)
	rn, rok := asNumber(right)
	switch op {
	case "eq":
		if lok && rok && ls != "" && rs != "" {
			return ln == rn
		}
		return strings.EqualFold(ls, rs)
	case "neq":
		if lok && rok && ls != "" && rs != "" {
			return ln != rn
		}
		return !strings.EqualFold(ls, rs)
	case "contains":
		return strings.Contains(strings.ToLower(ls), strings.ToLower(rs))
	case "notContains":
		return !strings.Contains(strings.ToLower(ls), strings.ToLower(rs))
	case "startsWith":
		return strings.HasPrefix(strings.ToLower(ls), strings.ToLower(rs))
	case "empty":
		return ls == "" || ls == "null"
	case "notEmpty":
		return ls != "" && ls != "null"
	case "gt", "gte", "lt", "lte":
		if !lok || !rok {
			// Dates compare as text in ISO form.
			switch op {
			case "gt":
				return ls > rs
			case "gte":
				return ls >= rs
			case "lt":
				return ls < rs
			default:
				return ls <= rs
			}
		}
		switch op {
		case "gt":
			return ln > rn
		case "gte":
			return ln >= rn
		case "lt":
			return ln < rn
		default:
			return ln <= rn
		}
	case "in":
		for _, part := range strings.Split(rs, ",") {
			if strings.EqualFold(strings.TrimSpace(part), ls) {
				return true
			}
		}
		return false
	case "isTrue":
		return ls == "true"
	case "isFalse":
		return ls != "true"
	}
	return false
}

// valuesFrom renders a step's "values" map.
func valuesFrom(cfg map[string]any, vars map[string]any) map[string]any {
	vals, _ := render(cfg["values"], vars).(map[string]any)
	if vals == nil {
		vals = map[string]any{}
	}
	return vals
}

func (h *Handler) runStep(ctx context.Context, ws uuid.UUID, s *WorkflowStep, vars map[string]any, a actorInfo, creator *uuid.UUID) (any, error) {
	cfg := s.Config
	objSpec := func() (*objectSpec, error) {
		spec := specFor(str(cfg["object"]))
		if spec == nil {
			return nil, errors.New("the object of this step no longer exists")
		}
		return spec, nil
	}
	recordID := func(key string) (uuid.UUID, error) {
		v := render(cfg[key], vars)
		if v == nil || asString(v) == "" {
			v = lookupPath(vars, "trigger.recordId")
		}
		id, err := uuid.Parse(asString(v))
		if err != nil {
			return uuid.Nil, errors.New("no record to work on (the record id is empty)")
		}
		return id, nil
	}
	switch s.Type {
	case "create_record":
		spec, err := objSpec()
		if err != nil {
			return nil, err
		}
		vals := valuesFrom(cfg, vars)
		if _, ok := vals["ownerId"]; !ok && creator != nil {
			vals["ownerId"] = creator.String()
		}
		var row *Row
		err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
			var err error
			row, err = h.createRecord(ctx, tx, ws, spec, a, vals)
			return err
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"record": rowSnapshot(row), "id": row.ID}, nil
	case "update_record":
		spec, err := objSpec()
		if err != nil {
			return nil, err
		}
		id, err := recordID("recordId")
		if err != nil {
			return nil, err
		}
		var row *Row
		err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
			var err error
			row, err = h.updateValues(ctx, tx, ws, spec, id, a, valuesFrom(cfg, vars), nil, nil)
			return err
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"record": rowSnapshot(row), "id": row.ID}, nil
	case "upsert_record":
		spec, err := objSpec()
		if err != nil {
			return nil, err
		}
		matchField := str(cfg["matchField"])
		matchValue := asString(render(cfg["matchValue"], vars))
		vals := valuesFrom(cfg, vars)
		var row *Row
		created := false
		err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
			fields, err := allFieldsRaw(ctx, tx, ws, spec)
			if err != nil {
				return err
			}
			var existing uuid.UUID
			if f, ok := findField(fields, matchField); ok && matchValue != "" {
				b := &sqlBuilder{}
				where, fe := whereFor(spec, fields, ws, listParams{Filter: &FilterNode{Field: f.Key, Op: "eq", Value: matchValue}}, b)
				if len(fe) == 0 {
					_ = tx.QueryRow(ctx, "SELECT t.id FROM crm."+spec.Table+" t"+where+" ORDER BY t.created_at LIMIT 1", b.args...).Scan(&existing)
				}
			}
			if existing != uuid.Nil {
				row, err = h.updateValues(ctx, tx, ws, spec, existing, a, vals, nil, nil)
				return err
			}
			if _, ok := vals["ownerId"]; !ok && creator != nil {
				vals["ownerId"] = creator.String()
			}
			created = true
			row, err = h.createRecord(ctx, tx, ws, spec, a, vals)
			return err
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"record": rowSnapshot(row), "id": row.ID, "created": created}, nil
	case "delete_record":
		spec, err := objSpec()
		if err != nil {
			return nil, err
		}
		id, err := recordID("recordId")
		if err != nil {
			return nil, err
		}
		err = h.store.WithTx(ctx, func(tx pgx.Tx) error { return h.deleteRecord(ctx, tx, ws, spec, id, a, nil) })
		if err != nil {
			return nil, err
		}
		return map[string]any{"id": id.String(), "deleted": true}, nil
	case "find_records":
		spec, err := objSpec()
		if err != nil {
			return nil, err
		}
		p := listParams{Limit: 50}
		if n, ok := asNumber(cfg["limit"]); ok && n >= 1 && n <= 200 {
			p.Limit = int(n)
		}
		if raw, ok := cfg["filter"]; ok && raw != nil {
			var node FilterNode
			b, _ := json.Marshal(render(raw, vars))
			if json.Unmarshal(b, &node) == nil {
				p.Filter = &node
			}
		}
		if raw, ok := cfg["sorts"]; ok {
			b, _ := json.Marshal(raw)
			_ = json.Unmarshal(b, &p.Sorts)
		}
		rows, total, err := h.list(ctx, ws, spec, p)
		if err != nil {
			return nil, err
		}
		list := make([]any, len(rows))
		for i := range rows {
			list[i] = rowSnapshot(&rows[i])
		}
		var first any
		if len(list) > 0 {
			first = list[0]
		}
		return map[string]any{"records": list, "count": len(list), "total": total, "first": first}, nil
	case "send_email":
		to := []string{}
		for _, part := range strings.FieldsFunc(asString(render(cfg["to"], vars)), func(r rune) bool { return r == ',' || r == ';' }) {
			if e := strings.TrimSpace(part); e != "" {
				to = append(to, e)
			}
		}
		if len(to) == 0 {
			return nil, errors.New("there's no email address to send to")
		}
		subject := asString(render(cfg["subject"], vars))
		body := asString(render(cfg["body"], vars))
		var link *recordLink
		if obj := str(cfg["linkObject"]); obj != "" {
			if id, err := uuid.Parse(asString(render(cfg["linkRecordId"], vars))); err == nil {
				link = &recordLink{Object: obj, ID: id}
			}
		}
		sent := []string{}
		for _, addr := range to {
			if len(sent) >= 20 {
				break
			}
			if _, err := h.sendEmail(ctx, ws, emailInput{To: []string{addr}, Subject: subject, Body: body}, creatorOrNil(creator), link, "workflow"); err != nil {
				return map[string]any{"sent": sent}, err
			}
			sent = append(sent, addr)
		}
		return map[string]any{"sent": sent}, nil
	case "http_request":
		method := strings.ToUpper(str(cfg["method"]))
		if method == "" {
			method = http.MethodPost
		}
		u, msg := validOutboundURL(asString(render(cfg["url"], vars)), h.cfg.AppEnv == "local")
		if msg != "" {
			return nil, errors.New(msg)
		}
		var body io.Reader
		if b := render(cfg["body"], vars); b != nil && method != http.MethodGet {
			raw, _ := json.Marshal(b)
			if s, ok := b.(string); ok {
				raw = []byte(s)
			}
			body = bytes.NewReader(raw)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, body)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", "CRM-Workflows/1.0")
		if hs, ok := render(cfg["headers"], vars).(map[string]any); ok {
			for k, v := range hs {
				req.Header.Set(k, asString(v))
			}
		}
		res, err := h.outboundClient(15 * time.Second).Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(res.Body, 256<<10))
		var parsed any
		if json.Unmarshal(raw, &parsed) != nil {
			parsed = string(raw)
		}
		out := map[string]any{"status": res.StatusCode, "body": parsed}
		if res.StatusCode >= 400 {
			return out, fmt.Errorf("the server answered %d", res.StatusCode)
		}
		return out, nil
	case "notify":
		title := asString(render(cfg["title"], vars))
		if title == "" {
			title = "Workflow update"
		}
		body := asString(render(cfg["body"], vars))
		link := asString(render(cfg["link"], vars))
		targets := []uuid.UUID{}
		for _, t := range asStrings(render(cfg["to"], vars)) {
			switch t {
			case "owner":
				if id, err := uuid.Parse(asString(lookupPath(vars, "trigger.record.ownerId"))); err == nil {
					targets = append(targets, id)
				}
			case "creator":
				if creator != nil {
					targets = append(targets, *creator)
				}
			default:
				if id, err := uuid.Parse(t); err == nil {
					targets = append(targets, id)
				}
			}
		}
		for _, id := range targets {
			h.notify(ctx, h.store.Pool, ws, id, "workflow", title, body, link, nil)
		}
		return map[string]any{"notified": len(targets)}, nil
	case "assign":
		spec, err := objSpec()
		if err != nil {
			return nil, err
		}
		id, err := recordID("recordId")
		if err != nil {
			return nil, err
		}
		pool, err := h.assignmentPool(ctx, ws, cfg)
		if err != nil {
			return nil, err
		}
		if len(pool) == 0 {
			return nil, errors.New("nobody to assign to (the team or list is empty)")
		}
		var pick uuid.UUID
		switch str(cfg["strategy"]) {
		case "random":
			pick = pool[rand.Intn(len(pool))]
		case "least_loaded":
			best := -1
			for _, p := range pool {
				var n int
				_ = h.store.Pool.QueryRow(ctx, "SELECT count(*) FROM crm."+spec.Table+" WHERE workspace_id = $1 AND owner_id = $2 AND deleted_at IS NULL", ws, p).Scan(&n)
				if best < 0 || n < best {
					best, pick = n, p
				}
			}
		default: // round robin
			key := s.ID + ":" + spec.Key
			var idx int
			if err := h.store.Pool.QueryRow(ctx, `INSERT INTO crm.assignment_state (workspace_id, key, last_index) VALUES ($1, $2, 0)
				ON CONFLICT (workspace_id, key) DO UPDATE SET last_index = crm.assignment_state.last_index + 1 RETURNING last_index`, ws, key).Scan(&idx); err != nil {
				return nil, err
			}
			pick = pool[idx%len(pool)]
		}
		err = h.store.WithTx(ctx, func(tx pgx.Tx) error {
			_, err := h.updateValues(ctx, tx, ws, spec, id, a, map[string]any{"ownerId": pick.String()}, nil, nil)
			return err
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"ownerId": pick.String()}, nil
	case "code":
		return runCode(str(cfg["source"]), vars)
	}
	return nil, fmt.Errorf("unknown step %q", s.Type)
}

func creatorOrNil(c *uuid.UUID) *uuid.UUID { return c }

// assignmentPool: a team's active members or a list of people.
func (h *Handler) assignmentPool(ctx context.Context, ws uuid.UUID, cfg map[string]any) ([]uuid.UUID, error) {
	out := []uuid.UUID{}
	if team, err := uuid.Parse(str(cfg["teamId"])); err == nil {
		rows, err := h.store.Pool.Query(ctx, `SELECT m.identity_id FROM crm.team_members tm JOIN crm.memberships m ON m.id = tm.membership_id AND m.status = 'active'
			WHERE tm.team_id = $1 AND tm.workspace_id = $2 ORDER BY m.created_at`, team, ws)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id uuid.UUID
			if rows.Scan(&id) == nil {
				out = append(out, id)
			}
		}
		return out, rows.Err()
	}
	for _, s := range asStrings(cfg["members"]) {
		id, err := uuid.Parse(s)
		if err != nil {
			continue
		}
		var ok bool
		_ = h.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM crm.memberships WHERE identity_id = $1 AND workspace_id = $2 AND status = 'active')`, id, ws).Scan(&ok)
		if ok {
			out = append(out, id)
		}
	}
	return out, nil
}

// runCode runs a small JavaScript function body with `input` (trigger + earlier steps).
// No network, files or timers; one second at most.
func runCode(source string, vars map[string]any) (any, error) {
	vm := goja.New()
	raw, _ := json.Marshal(vars)
	var input any
	_ = json.Unmarshal(raw, &input)
	if err := vm.Set("input", input); err != nil {
		return nil, err
	}
	timer := time.AfterFunc(time.Second, func() { vm.Interrupt("the code ran longer than 1 second") })
	defer timer.Stop()
	v, err := vm.RunString("(function(){\n" + source + "\n})()")
	if err != nil {
		var ex *goja.Exception
		if errors.As(err, &ex) {
			return nil, errors.New(ex.Value().String())
		}
		return nil, err
	}
	out := v.Export()
	b, err := json.Marshal(out)
	if err != nil {
		return nil, errors.New("the code must return plain data (objects, lists, text, numbers)")
	}
	if len(b) > 64<<10 {
		return nil, errors.New("the code returned more than 64 KB")
	}
	var clean any
	_ = json.Unmarshal(b, &clean)
	return clean, nil
}

// ---- cron: "m h dom mon dow" with *, lists, ranges and steps ----

type cronSpec struct {
	min, hour, dom, mon, dow map[int]bool
	domStar, dowStar         bool
}

func parseCronField(f string, lo, hi int) (map[int]bool, error) {
	out := map[int]bool{}
	for _, part := range strings.Split(f, ",") {
		step := 1
		if base, s, ok := strings.Cut(part, "/"); ok {
			n, err := strconv.Atoi(s)
			if err != nil || n < 1 {
				return nil, fmt.Errorf("bad step in %q", part)
			}
			step, part = n, base
		}
		a, b := lo, hi
		if part != "*" {
			if x, y, ok := strings.Cut(part, "-"); ok {
				var e1, e2 error
				a, e1 = strconv.Atoi(x)
				b, e2 = strconv.Atoi(y)
				if e1 != nil || e2 != nil {
					return nil, fmt.Errorf("bad range %q", part)
				}
			} else {
				n, err := strconv.Atoi(part)
				if err != nil {
					return nil, fmt.Errorf("bad value %q", part)
				}
				a, b = n, n
			}
		}
		if a < lo || b > hi || a > b {
			return nil, fmt.Errorf("%q is out of range %d–%d", part, lo, hi)
		}
		for i := a; i <= b; i += step {
			out[i] = true
		}
	}
	return out, nil
}

func parseCron(expr string) (*cronSpec, error) {
	f := strings.Fields(expr)
	if len(f) != 5 {
		return nil, errors.New("use five parts: minute hour day month weekday")
	}
	c := &cronSpec{domStar: f[2] == "*", dowStar: f[4] == "*"}
	var err error
	if c.min, err = parseCronField(f[0], 0, 59); err != nil {
		return nil, err
	}
	if c.hour, err = parseCronField(f[1], 0, 23); err != nil {
		return nil, err
	}
	if c.dom, err = parseCronField(f[2], 1, 31); err != nil {
		return nil, err
	}
	if c.mon, err = parseCronField(f[3], 1, 12); err != nil {
		return nil, err
	}
	if c.dow, err = parseCronField(strings.ReplaceAll(f[4], "7", "0"), 0, 6); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *cronSpec) next(from time.Time) time.Time {
	t := from.Truncate(time.Minute).Add(time.Minute)
	for i := 0; i < 366*24*60; i++ {
		dayOK := true
		switch {
		case !c.domStar && !c.dowStar: // like cron: either the day of month or the weekday
			dayOK = c.dom[t.Day()] || c.dow[int(t.Weekday())]
		case !c.domStar:
			dayOK = c.dom[t.Day()]
		case !c.dowStar:
			dayOK = c.dow[int(t.Weekday())]
		}
		if c.mon[int(t.Month())] && dayOK && c.hour[t.Hour()] && c.min[t.Minute()] {
			return t
		}
		t = t.Add(time.Minute)
	}
	return from.Add(24 * time.Hour)
}
