package records

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Record timeline (D-59): everything that happened to a record in one place — field
// changes, notes, the tasks / events / notes / communications linked to it, emails and
// files — newest first. Notes can @mention people, who are notified.

type TimelineItem struct {
	ID        string         `json:"id"`
	Kind      string         `json:"kind"`
	Title     string         `json:"title"`
	Body      string         `json:"body,omitempty"`
	Detail    map[string]any `json:"detail,omitempty"`
	Actor     *LookupValue   `json:"actor,omitempty"`
	Link      *LookupValue   `json:"link,omitempty"`
	Status    string         `json:"status,omitempty"`
	At        time.Time      `json:"at"`
	CanDelete bool           `json:"canDelete,omitempty"`
}

var legacyColumn = map[string]string{"leads": "lead_id", "accounts": "account_id", "contacts": "contact_id"}

// timelineObjects are the objects whose linked records appear on a timeline.
var timelineObjects = []string{"notes", "tasks", "events", "communications"}

func (h *Handler) recordIDParam(w http.ResponseWriter, r *http.Request, need string) (*objectSpec, uuid.UUID, uuid.UUID, bool) {
	spec, ws, err := h.scope(r, need)
	if err != nil {
		shared.WriteError(w, r, err)
		return nil, uuid.Nil, uuid.Nil, false
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return nil, uuid.Nil, uuid.Nil, false
	}
	// The record must be one the requester can see.
	if _, _, err := h.getRow(r.Context(), h.store.Pool, ws, spec, id, ownerFilter(r, spec)); err != nil {
		shared.WriteError(w, r, err)
		return nil, uuid.Nil, uuid.Nil, false
	}
	return spec, ws, id, true
}

func (h *Handler) handleTimeline(w http.ResponseWriter, r *http.Request) {
	spec, ws, id, ok := h.recordIDParam(w, r, "read")
	if !ok {
		return
	}
	ctx := r.Context()
	sc := scopeFrom(ctx)
	me := actor(r)
	before := time.Now().Add(time.Minute)
	if b := r.URL.Query().Get("before"); b != "" {
		if t, err := time.Parse(time.RFC3339Nano, b); err == nil {
			before = t
		}
	}
	limit := queryInt(r, "limit", 30, 5, 100)
	kind := r.URL.Query().Get("kind") // all | notes | emails | history | files | tasks | events
	items := []TimelineItem{}
	names := map[uuid.UUID]string{}
	actors := []uuid.UUID{}

	want := func(k string) bool { return kind == "" || kind == "all" || kind == k }

	// 1. Activities: history, notes, calls, app events.
	if want("history") || want("notes") || want("app") {
		legacy := "false"
		if col := legacyColumn[spec.Key]; col != "" {
			legacy = "a." + col + " = $3"
		}
		kindFilter := ""
		switch kind {
		case "history":
			kindFilter = " AND a.kind LIKE 'record.%'"
		case "notes":
			kindFilter = " AND a.kind IN ('note', 'call')"
		}
		rows, err := h.store.Pool.Query(ctx, `SELECT a.id, a.kind, a.title, a.detail, a.actor_id, a.occurred_at FROM crm.activities a
			WHERE a.workspace_id = $1 AND ((a.object_key = $2 AND a.record_id = $3) OR `+legacy+`) AND a.occurred_at < $4`+kindFilter+`
			ORDER BY a.occurred_at DESC LIMIT $5`, ws, spec.Key, id, before, limit)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		for rows.Next() {
			var aid int64
			var it TimelineItem
			var raw []byte
			var actorID *uuid.UUID
			if err := rows.Scan(&aid, &it.Kind, &it.Title, &raw, &actorID, &it.At); err != nil {
				rows.Close()
				shared.WriteError(w, r, err)
				return
			}
			it.ID = "a:" + strconv.FormatInt(aid, 10)
			_ = json.Unmarshal(raw, &it.Detail)
			if b, ok := it.Detail["body"].(string); ok {
				it.Body = b
				delete(it.Detail, "body")
			}
			if actorID != nil {
				it.Actor = &LookupValue{ID: actorID.String(), Object: "users"}
				actors = append(actors, *actorID)
				it.CanDelete = (it.Kind == "note" || it.Kind == "call") && (*actorID == me || sc.CanCustomize())
			}
			items = append(items, it)
		}
		rows.Close()
	}
	// 2. Linked notes, tasks, events and communications.
	for _, obj := range timelineObjects {
		if !want(obj) && !(kind == "notes" && obj == "notes") {
			continue
		}
		ls := specFor(obj)
		if ls == nil || !sc.Enabled(obj) || !sc.Can(obj, "read") {
			continue
		}
		var keys []string
		for _, f := range ls.Fields {
			if f.Type == "lookup" && f.Lookup == spec.Key && !f.inColumn() {
				keys = append(keys, f.Key)
			}
		}
		if len(keys) == 0 {
			continue
		}
		conds := make([]string, len(keys))
		for i, k := range keys {
			conds[i] = "t.custom->>'" + k + "' = $2"
		}
		bodyKey := map[string]string{"notes": "body", "tasks": "description", "events": "description", "communications": "body"}[obj]
		dateExpr := map[string]string{
			"tasks":          "COALESCE((NULLIF(t.custom->>'dueDate', ''))::timestamp AT TIME ZONE 'Asia/Kolkata', t.created_at)",
			"events":         "COALESCE((NULLIF(t.custom->>'startsAt', ''))::timestamptz, t.created_at)",
			"communications": "COALESCE((NULLIF(t.custom->>'occurredAt', ''))::timestamptz, t.created_at)",
		}[obj]
		if dateExpr == "" {
			dateExpr = "t.created_at"
		}
		own := sc.OwnersFor(obj, me)
		rows, err := h.store.Pool.Query(ctx, `SELECT t.id::text, t.code, t.name, COALESCE(t.status, ''), COALESCE(t.custom->>'`+bodyKey+`', ''), t.created_by, `+dateExpr+`,
			COALESCE(t.custom->>'channel', ''), COALESCE(t.custom->>'direction', '')
			FROM crm.`+ls.Table+` t WHERE t.workspace_id = $1 AND t.deleted_at IS NULL AND (`+strings.Join(conds, " OR ")+`)
			AND ($3::uuid[] IS NULL OR t.owner_id = ANY($3)) AND `+dateExpr+` < $4 ORDER BY 7 DESC LIMIT $5`, ws, id.String(), own, before, limit)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		for rows.Next() {
			var rid, code, name, status, body, channel, direction string
			var by *uuid.UUID
			var at time.Time
			if err := rows.Scan(&rid, &code, &name, &status, &body, &by, &at, &channel, &direction); err != nil {
				rows.Close()
				shared.WriteError(w, r, err)
				return
			}
			it := TimelineItem{ID: obj + ":" + rid, Kind: strings.TrimSuffix(obj, "s"), Title: name, Body: body, Status: status, At: at,
				Link: &LookupValue{ID: rid, Label: name + " · " + code, Object: obj}}
			if obj == "communications" {
				it.Detail = map[string]any{"channel": channel, "direction": direction}
			}
			if by != nil {
				it.Actor = &LookupValue{ID: by.String(), Object: "users"}
				actors = append(actors, *by)
			}
			items = append(items, it)
		}
		rows.Close()
	}
	// 3. Emails linked to the record.
	if want("emails") {
		rows, err := h.store.Pool.Query(ctx, `SELECT m.id::text, m.direction, m.from_addr, m.from_name, m.to_addrs, m.subject, m.snippet, m.body_text, m.status,
			COALESCE(m.error, ''), m.sent_by, m.sent_at, COALESCE(ma.visibility, 'share_everything'), COALESCE(ma.identity_id, '00000000-0000-0000-0000-000000000000')
			FROM crm.message_links l JOIN crm.messages m ON m.id = l.message_id LEFT JOIN crm.mail_accounts ma ON ma.id = m.mail_account_id
			WHERE l.workspace_id = $1 AND l.object_key = $2 AND l.record_id = $3 AND m.sent_at < $4 ORDER BY m.sent_at DESC LIMIT $5`, ws, spec.Key, id, before, limit)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		for rows.Next() {
			var mid, dir, from, fromName, subject, snippet, body, status, errText, visibility string
			var to []string
			var by *uuid.UUID
			var at time.Time
			var owner uuid.UUID
			if err := rows.Scan(&mid, &dir, &from, &fromName, &to, &subject, &snippet, &body, &status, &errText, &by, &at, &visibility, &owner); err != nil {
				rows.Close()
				shared.WriteError(w, r, err)
				return
			}
			it := TimelineItem{ID: "m:" + mid, Kind: "email", Title: subject, At: at, Status: status,
				Detail: map[string]any{"direction": dir, "from": from, "fromName": fromName, "to": to, "error": errText, "messageId": mid}}
			// Mailbox owners choose how much others see of their synced emails.
			mine := owner == me
			switch {
			case mine || visibility == "share_everything":
				it.Body = body
				if it.Body == "" {
					it.Body = snippet
				}
			case visibility == "subject":
				it.Body = ""
			default:
				it.Title = "Email"
				it.Detail = map[string]any{"direction": dir, "from": from, "to": to}
			}
			if by != nil {
				it.Actor = &LookupValue{ID: by.String(), Object: "users"}
				actors = append(actors, *by)
			}
			items = append(items, it)
		}
		rows.Close()
	}
	// 4. Files.
	if want("files") {
		rows, err := h.store.Pool.Query(ctx, `SELECT id::text, name, content_type, size_bytes, created_by, created_at FROM crm.files
			WHERE workspace_id = $1 AND object_key = $2 AND record_id = $3 AND deleted_at IS NULL AND created_at < $4 ORDER BY created_at DESC LIMIT $5`,
			ws, spec.Key, id, before, limit)
		if err != nil {
			shared.WriteError(w, r, err)
			return
		}
		for rows.Next() {
			var fid, name, ct string
			var size int
			var by *uuid.UUID
			var at time.Time
			if err := rows.Scan(&fid, &name, &ct, &size, &by, &at); err != nil {
				rows.Close()
				shared.WriteError(w, r, err)
				return
			}
			it := TimelineItem{ID: "f:" + fid, Kind: "file", Title: name, At: at, Detail: map[string]any{"fileId": fid, "contentType": ct, "size": size}}
			if by != nil {
				it.Actor = &LookupValue{ID: by.String(), Object: "users"}
				actors = append(actors, *by)
				it.CanDelete = *by == me || sc.CanCustomize()
			}
			items = append(items, it)
		}
		rows.Close()
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].At.After(items[j].At) })
	more := len(items) > limit
	if more {
		items = items[:limit]
	}
	if len(actors) > 0 {
		rows, err := h.store.Pool.Query(ctx, `SELECT id, display_name FROM crm.identities WHERE id = ANY($1)`, actors)
		if err == nil {
			for rows.Next() {
				var aid uuid.UUID
				var n string
				if rows.Scan(&aid, &n) == nil {
					names[aid] = n
				}
			}
			rows.Close()
		}
		for i := range items {
			if items[i].Actor != nil {
				if aid, err := uuid.Parse(items[i].Actor.ID); err == nil {
					items[i].Actor.Label = names[aid]
				}
			}
		}
	}
	var next string
	if more && len(items) > 0 {
		next = items[len(items)-1].At.Format(time.RFC3339Nano)
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": items, "next": next})
}

// ---- notes & calls on the timeline ----

var mentionRe = regexp.MustCompile(`@\[([^\]]{1,80})\]\(([0-9a-fA-F-]{36})\)`)

func (h *Handler) handleAddNote(w http.ResponseWriter, r *http.Request) {
	spec, ws, id, ok := h.recordIDParam(w, r, "read")
	if !ok {
		return
	}
	var in struct {
		Body string `json:"body"`
		Kind string `json:"kind"` // note | call
	}
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	in.Body = sanitizeRich(in.Body)
	if in.Body == "" || len(in.Body) > 200_000 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"body": "Write a note (it can't be empty or huge)."}))
		return
	}
	if in.Kind != "call" {
		in.Kind = "note"
	}
	a := actorFromRequest(r, "ui")
	sc := scopeFrom(r.Context())
	var title string
	_ = h.store.Pool.QueryRow(r.Context(), `SELECT `+spec.TitleSQL+` FROM crm.`+spec.Table+` t WHERE t.id = $1`, id).Scan(&title)
	err := h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		label := map[string]string{"note": "Note", "call": "Call logged"}[in.Kind]
		if err := insertActivity(r.Context(), tx, ws, spec.Key, id, in.Kind, label, map[string]any{"body": in.Body}, a.ID); err != nil {
			return err
		}
		// Mentions: @[Name](identity-id) → a notification for each person in the workspace.
		preview := mentionRe.ReplaceAllString(richPlain(in.Body), "@$1")
		if len(preview) > 300 {
			preview = preview[:300] + "…"
		}
		for _, mid := range mentionIDs(in.Body) {
			target, err := uuid.Parse(mid)
			if err != nil || target == a.uuidOrNil() {
				continue
			}
			var member bool
			_ = tx.QueryRow(r.Context(), `SELECT EXISTS (SELECT 1 FROM crm.memberships WHERE identity_id = $1 AND workspace_id = $2 AND status = 'active')
				OR EXISTS (SELECT 1 FROM crm.identities WHERE id = $1 AND is_platform_owner)`, target, ws).Scan(&member)
			if member {
				h.notify(r.Context(), tx, ws, target, "mention", "You were mentioned on "+title, preview,
					recordPath(sc, spec.Key, id), a.ID)
			}
		}
		return nil
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.bus.Kick()
	w.WriteHeader(http.StatusCreated)
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func recordPath(sc *Scope, object string, id uuid.UUID) string {
	if sc.OwnerConsole {
		return "/crm/owner/" + object + "/" + id.String()
	}
	return "/crm/w/" + sc.Code + "/" + object + "/" + id.String()
}

// DELETE /timeline/{itemId} — notes and calls by their author (or someone who customizes).
func (h *Handler) handleDeleteTimelineItem(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	raw := strings.TrimPrefix(chi.URLParam(r, "itemId"), "a:")
	aid, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("item_not_found"))
		return
	}
	var author *uuid.UUID
	var kind string
	err = h.store.Pool.QueryRow(r.Context(), `SELECT actor_id, kind FROM crm.activities WHERE id = $1 AND workspace_id = $2`, aid, sc.WS).Scan(&author, &kind)
	if errors.Is(err, pgx.ErrNoRows) || (kind != "note" && kind != "call") {
		shared.WriteError(w, r, shared.NotFound("item_not_found"))
		return
	}
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if !(author != nil && *author == actor(r)) && !sc.CanCustomize() {
		shared.WriteError(w, r, shared.Forbidden("forbidden", "Only the author can delete this note."))
		return
	}
	if _, err := h.store.Pool.Exec(r.Context(), `DELETE FROM crm.activities WHERE id = $1`, aid); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- files ----

const maxFileBytes = 10 << 20
const maxWorkspaceFileBytes = 250 << 20

type FileInfo struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	ContentType string    `json:"contentType"`
	Size        int       `json:"size"`
	Field       string    `json:"field,omitempty"`
	CreatedBy   string    `json:"createdBy,omitempty"`
	CreatedAt   time.Time `json:"createdAt"`
}

var blockedFileTypes = map[string]bool{"application/x-msdownload": true, "application/x-sh": true, "application/x-executable": true}

func (h *Handler) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	spec, ws, id, ok := h.recordIDParam(w, r, "update")
	if !ok {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFileBytes+(1<<20))
	if err := r.ParseMultipartForm(1 << 20); err != nil {
		shared.WriteError(w, r, shared.NewError(http.StatusRequestEntityTooLarge, "too_large", "Files can be at most 10 MB."))
		return
	}
	file, hdr, err := r.FormFile("file")
	if err != nil {
		shared.WriteError(w, r, shared.Validation(map[string]string{"file": "Choose a file."}))
		return
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil || len(data) == 0 {
		shared.WriteError(w, r, shared.Validation(map[string]string{"file": "That file couldn't be read."}))
		return
	}
	if len(data) > maxFileBytes {
		shared.WriteError(w, r, shared.NewError(http.StatusRequestEntityTooLarge, "too_large", "Files can be at most 10 MB."))
		return
	}
	ct := http.DetectContentType(data)
	if declared := hdr.Header.Get("Content-Type"); declared != "" && ct == "application/octet-stream" {
		ct = declared // e.g. .docx / .xlsx, which sniff as zip or octet-stream
	}
	name := strings.TrimSpace(hdr.Filename)
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	if name == "" || len(name) > 200 {
		name = "file"
	}
	lower := strings.ToLower(name)
	if blockedFileTypes[ct] || strings.HasSuffix(lower, ".exe") || strings.HasSuffix(lower, ".bat") || strings.HasSuffix(lower, ".sh") {
		shared.WriteError(w, r, shared.Validation(map[string]string{"file": "Programs can't be attached."}))
		return
	}
	field := strings.TrimSpace(r.FormValue("field"))
	var used int64
	_ = h.store.Pool.QueryRow(r.Context(), `SELECT COALESCE(sum(size_bytes), 0) FROM crm.files WHERE workspace_id = $1 AND deleted_at IS NULL`, ws).Scan(&used)
	if used+int64(len(data)) > maxWorkspaceFileBytes {
		shared.WriteError(w, r, shared.NewError(http.StatusInsufficientStorage, "storage_full", "This workspace has used its 250 MB of file storage. Delete old files first."))
		return
	}
	a := actorFromRequest(r, "ui")
	var fid uuid.UUID
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		if field != "" {
			fields, err := allFields(r.Context(), tx, ws, spec)
			if err != nil {
				return err
			}
			f, ok := findField(fields, field)
			if !ok || f.Type != "files" || f.ReadOnly {
				return shared.Validation(map[string]string{"field": "That field doesn't take files."})
			}
		}
		if err := tx.QueryRow(r.Context(), `INSERT INTO crm.files (workspace_id, object_key, record_id, field_key, name, content_type, size_bytes, data, created_by)
			VALUES ($1, $2, $3, NULLIF($4, ''), $5, $6, $7, $8, $9) RETURNING id`, ws, spec.Key, id, field, name, ct, len(data), data, a.ID).Scan(&fid); err != nil {
			return err
		}
		if field != "" {
			current, _, err := h.getRow(r.Context(), tx, ws, spec, id, nil)
			if err != nil {
				return err
			}
			list, _ := current.Values[field].([]any)
			list = append(list, map[string]any{"id": fid.String()})
			if _, err := h.updateValues(r.Context(), tx, ws, spec, id, a, map[string]any{field: list}, nil, nil); err != nil {
				return err
			}
		}
		return insertActivity(r.Context(), tx, ws, spec.Key, id, "file.uploaded", "Attached "+name, map[string]any{"fileId": fid.String(), "size": len(data)}, a.ID)
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.bus.Kick()
	shared.WriteJSON(w, http.StatusCreated, FileInfo{ID: fid.String(), Name: name, ContentType: ct, Size: len(data), Field: field, CreatedAt: time.Now()})
}

func (h *Handler) handleListFiles(w http.ResponseWriter, r *http.Request) {
	spec, ws, id, ok := h.recordIDParam(w, r, "read")
	if !ok {
		return
	}
	rows, err := h.store.Pool.Query(r.Context(), `SELECT f.id::text, f.name, f.content_type, f.size_bytes, COALESCE(f.field_key, ''), COALESCE(i.display_name, ''), f.created_at
		FROM crm.files f LEFT JOIN crm.identities i ON i.id = f.created_by
		WHERE f.workspace_id = $1 AND f.object_key = $2 AND f.record_id = $3 AND f.deleted_at IS NULL ORDER BY f.created_at DESC`, ws, spec.Key, id)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []FileInfo{}
	for rows.Next() {
		var f FileInfo
		if err := rows.Scan(&f.ID, &f.Name, &f.ContentType, &f.Size, &f.Field, &f.CreatedBy, &f.CreatedAt); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out = append(out, f)
	}
	respond(w, r, http.StatusOK, map[string]any{"data": out}, rows.Err())
}

// fileAccess loads a file and checks the requester can read its record.
func (h *Handler) fileAccess(r *http.Request, need string) (*FileInfo, []byte, *objectSpec, uuid.UUID, error) {
	sc := scopeFrom(r.Context())
	fid, err := uuid.Parse(chi.URLParam(r, "fileId"))
	if err != nil {
		return nil, nil, nil, uuid.Nil, shared.NotFound("file_not_found")
	}
	var f FileInfo
	var object string
	var recordID uuid.UUID
	var data []byte
	err = h.store.Pool.QueryRow(r.Context(), `SELECT id::text, name, content_type, size_bytes, COALESCE(field_key, ''), created_at, object_key, record_id,
		CASE WHEN $3 THEN data ELSE ''::bytea END
		FROM crm.files WHERE id = $1 AND workspace_id = $2 AND deleted_at IS NULL`, fid, sc.WS, need == "read").
		Scan(&f.ID, &f.Name, &f.ContentType, &f.Size, &f.Field, &f.CreatedAt, &object, &recordID, &data)
	if err != nil {
		return nil, nil, nil, uuid.Nil, shared.NotFound("file_not_found")
	}
	spec := specFor(object)
	if spec == nil || !sc.Enabled(object) || !sc.Can(object, need) {
		return nil, nil, nil, uuid.Nil, shared.NotFound("file_not_found")
	}
	if _, _, err := h.getRow(r.Context(), h.store.Pool, sc.WS, spec, recordID, sc.OwnersFor(object, actor(r))); err != nil {
		return nil, nil, nil, uuid.Nil, shared.NotFound("file_not_found")
	}
	return &f, data, spec, recordID, nil
}

func (h *Handler) handleDownloadFile(w http.ResponseWriter, r *http.Request) {
	f, data, _, _, err := h.fileAccess(r, "read")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	disposition := "attachment"
	if r.URL.Query().Get("inline") == "1" && (strings.HasPrefix(f.ContentType, "image/") || f.ContentType == "application/pdf") && f.ContentType != "image/svg+xml" {
		disposition = "inline"
	}
	w.Header().Set("Content-Type", f.ContentType)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`%s; filename="%s"`, disposition, strings.ReplaceAll(f.Name, `"`, "")))
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Content-Security-Policy", "sandbox")
	_, _ = w.Write(data)
}

func (h *Handler) handleDeleteFile(w http.ResponseWriter, r *http.Request) {
	f, _, spec, recordID, err := h.fileAccess(r, "update")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	sc := scopeFrom(r.Context())
	a := actorFromRequest(r, "ui")
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `UPDATE crm.files SET deleted_at = now() WHERE id = $1`, f.ID); err != nil {
			return err
		}
		if f.Field != "" {
			current, _, err := h.getRow(r.Context(), tx, sc.WS, spec, recordID, nil)
			if err != nil {
				return err
			}
			list, _ := current.Values[f.Field].([]any)
			kept := []any{}
			for _, item := range list {
				if m, ok := item.(map[string]any); ok && m["id"] == f.ID {
					continue
				}
				kept = append(kept, item)
			}
			if _, err := h.updateValues(r.Context(), tx, sc.WS, spec, recordID, a, map[string]any{f.Field: kept}, nil, nil); err != nil {
				return err
			}
		}
		return insertActivity(r.Context(), tx, sc.WS, spec.Key, recordID, "file.deleted", "Removed "+f.Name, nil, a.ID)
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// linkedRecordTitle is used by emails and notifications.
func (h *Handler) recordTitle(ctx context.Context, spec *objectSpec, id uuid.UUID) string {
	var t string
	_ = h.store.Pool.QueryRow(ctx, `SELECT `+spec.TitleSQL+` FROM crm.`+spec.Table+` t WHERE t.id = $1`, id).Scan(&t)
	return t
}
