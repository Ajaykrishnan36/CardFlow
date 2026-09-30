package records

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/platform"
	"cardflow-backend/internal/crm/shared"
	"cardflow-backend/internal/crm/store"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Handler struct {
	store    *store.Store
	cfg      shared.Config
	platform *platform.Handler

	wsOnce sync.Mutex
	wsID   uuid.UUID

	extensions []Extension
	bus        *Bus
	mailer     Mailer
}

// Mailer sends the CRM's own emails (record emails, campaigns, workflow emails).
type Mailer interface {
	SendHTML(ctx context.Context, to, subject, html, text string, opts MailOptions) error
	From() string
}

// MailOptions are optional headers of a CRM email.
type MailOptions struct {
	FromName string
	ReplyTo  string
	Headers  map[string]string
}

func NewHandler(st *store.Store, cfg shared.Config, p *platform.Handler, m Mailer) *Handler {
	h := &Handler{store: st, cfg: cfg, platform: p, bus: newBus(), mailer: m}
	eventHandler = h
	return h
}

// Bus is the live-update and event hub (for connectors that change records).
func (h *Handler) Bus() *Bus { return h.bus }

// Routes mounts the record engine twice: for the owner's Platform CRM (/platform/…,
// everything allowed) and for workspace members (/w/{code}/…, limited by role +
// permission sets + products, checked on every request — D-30, D-31).
func (h *Handler) Routes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(identity.RequireOwner, h.ownerScope)
		h.mountRecords(r, "/platform")
		r.Post("/platform/crm/{object}/{id}/login", h.handleGiveLogin)
		h.objectRoutes(r)
	})
	r.Route("/w/{code}", func(r chi.Router) {
		r.Use(identity.RequireReady, h.memberScope)
		r.Get("/context", h.handleContext)
		r.Get("/dashboard", h.handleDashboard)
		r.Get("/access/fields", h.handleWorkspaceFieldCatalog)
		h.reportRoutes(r)
		h.mountRecords(r, "")
		for _, ext := range h.extensions {
			if ext.MemberRoutes != nil {
				ext.MemberRoutes(r)
			}
		}
		h.workspaceRoutes(r)
		h.platform.AdminRoutes(r, func(r *http.Request) *platform.AdminScope {
			sc := scopeFrom(r.Context())
			return &platform.AdminScope{WS: sc.WS, Code: sc.Code, IsPlatform: sc.IsPlatformWS, MembershipID: sc.MembershipID, Limit: sc.Eff}
		})
	})
}

func (h *Handler) mountRecords(r chi.Router, prefix string) {
	r.Get(prefix+"/crm/meta/{object}", h.handleMeta)
	r.Put(prefix+"/crm/meta/{object}/layout", h.handleSaveLayout)
	r.Post(prefix+"/crm/meta/{object}/layout/reset", h.handleResetLayout)
	r.Post(prefix+"/crm/meta/{object}/fields", h.handleCreateField)
	r.Patch(prefix+"/crm/meta/{object}/fields/{key}", h.handleUpdateField)
	r.Delete(prefix+"/crm/meta/{object}/fields/{key}", h.handleDeleteField)

	r.Get(prefix+"/crm/{object}", h.handleList)
	r.Post(prefix+"/crm/{object}", h.handleCreate)
	r.Get(prefix+"/crm/{object}/groups", h.handleGroups)
	r.Post(prefix+"/crm/{object}/bulk", h.handleBulk)
	r.Get(prefix+"/crm/{object}/export", h.handleExport)
	r.Post(prefix+"/crm/{object}/import", h.handleImport)
	r.Post(prefix+"/crm/{object}/merge", h.handleMerge)
	r.Get(prefix+"/crm/{object}/views", h.handleListViews)
	r.Post(prefix+"/crm/{object}/views", h.handleCreateView)
	r.Patch(prefix+"/crm/{object}/views/{viewId}", h.handleUpdateView)
	r.Delete(prefix+"/crm/{object}/views/{viewId}", h.handleDeleteView)
	r.Get(prefix+"/crm/{object}/{id}", h.handleGet)
	r.Patch(prefix+"/crm/{object}/{id}", h.handleUpdate)
	r.Delete(prefix+"/crm/{object}/{id}", h.handleDelete)
	r.Post(prefix+"/crm/{object}/{id}/restore", h.handleRestore)
	r.Get(prefix+"/crm/{object}/{id}/timeline", h.handleTimeline)
	r.Post(prefix+"/crm/{object}/{id}/notes", h.handleAddNote)
	r.Get(prefix+"/crm/{object}/{id}/files", h.handleListFiles)
	r.Post(prefix+"/crm/{object}/{id}/files", h.handleUploadFile)
	r.Get(prefix+"/crm/{object}/{id}/duplicates", h.handleDuplicates)
	r.Post(prefix+"/crm/{object}/{id}/email", h.handleSendRecordEmail)
	r.Get(prefix+"/crm/{object}/{id}/emails", h.handleEmailThreads)
	r.Post(prefix+"/crm/leads/{id}/convert", h.handleConvert)
	r.Post(prefix+"/crm/communications/{id}/send", h.handleSendCommunication)

	r.Get(prefix+"/lookup/{target}", h.handleLookup)
	r.Get(prefix+"/search", h.handleSearch)
	r.Get(prefix+"/files/{fileId}", h.handleDownloadFile)
	r.Delete(prefix+"/files/{fileId}", h.handleDeleteFile)
	r.Delete(prefix+"/timeline/{itemId}", h.handleDeleteTimelineItem)
	r.Get(prefix+"/favorites", h.handleListFavorites)
	r.Post(prefix+"/favorites", h.handleAddFavorite)
	r.Put(prefix+"/favorites/order", h.handleReorderFavorites)
	r.Delete(prefix+"/favorites/{favId}", h.handleRemoveFavorite)
	r.Get(prefix+"/notifications", h.handleListNotifications)
	r.Post(prefix+"/notifications/read", h.handleReadNotifications)
	r.Get(prefix+"/stream", h.handleStream)
}

// workspaceRoutes are the product-level tools inside /w/{code}.
func (h *Handler) workspaceRoutes(r chi.Router) {
	r.Get("/developer/api-keys", h.handleListAPIKeys)
	r.Post("/developer/api-keys", h.handleCreateAPIKey)
	r.Delete("/developer/api-keys/{keyId}", h.handleRevokeAPIKey)
	r.Get("/developer/webhooks", h.handleListWebhooks)
	r.Post("/developer/webhooks", h.handleCreateWebhook)
	r.Patch("/developer/webhooks/{hookId}", h.handleUpdateWebhook)
	r.Delete("/developer/webhooks/{hookId}", h.handleDeleteWebhook)
	r.Post("/developer/webhooks/{hookId}/rotate", h.handleRotateWebhookSecret)
	r.Post("/developer/webhooks/{hookId}/test", h.handleTestWebhook)
	r.Get("/developer/webhooks/{hookId}/deliveries", h.handleListDeliveries)
	r.Post("/developer/webhooks/{hookId}/deliveries/{deliveryId}/retry", h.handleRetryDelivery)
	r.Get("/developer/openapi.json", h.handleOpenAPI)
	r.Post("/graphql", h.handleGraphQL)
	r.Get("/graphql/schema", h.handleGraphQLSchema)

	r.Get("/workflows", h.handleListWorkflows)
	r.Post("/workflows", h.handleCreateWorkflow)
	r.Get("/workflows/{wfId}", h.handleGetWorkflow)
	r.Patch("/workflows/{wfId}", h.handleUpdateWorkflow)
	r.Delete("/workflows/{wfId}", h.handleDeleteWorkflow)
	r.Post("/workflows/{wfId}/publish", h.handlePublishWorkflow)
	r.Post("/workflows/{wfId}/status", h.handleWorkflowStatus)
	r.Post("/workflows/{wfId}/run", h.handleRunWorkflow)
	r.Get("/workflows/{wfId}/versions", h.handleWorkflowVersions)
	r.Post("/workflows/{wfId}/versions/{version}/restore", h.handleRestoreWorkflowVersion)
	r.Get("/workflows/{wfId}/runs", h.handleListRuns)
	r.Post("/workflows/{wfId}/runs/{runId}/stop", h.runAction("stop"))
	r.Post("/workflows/{wfId}/runs/{runId}/retry", h.runAction("retry"))

	r.Get("/mailboxes", h.handleListMailboxes)
	r.Get("/mailboxes/connect/{provider}", h.handleConnectMailbox)
	r.Post("/mailboxes/imap", h.handleConnectIMAP)
	r.Post("/mailboxes/blocklist", h.handleBlocklist)
	r.Patch("/mailboxes/{mailboxId}", h.handleUpdateMailbox)
	r.Delete("/mailboxes/{mailboxId}", h.handleDeleteMailbox)
	r.Post("/mailboxes/{mailboxId}/sync", h.handleSyncMailbox)

	r.Get("/campaigns", h.handleListCampaigns)
	r.Post("/campaigns", h.handleCreateCampaign)
	r.Get("/campaigns/{campaignId}", h.handleGetCampaign)
	r.Patch("/campaigns/{campaignId}", h.handleUpdateCampaign)
	r.Delete("/campaigns/{campaignId}", h.handleDeleteCampaign)
	r.Get("/campaigns/{campaignId}/audience", h.handleCampaignAudience)
	r.Get("/campaigns/{campaignId}/recipients", h.handleCampaignRecipients)
	r.Post("/campaigns/{campaignId}/test", h.handleTestCampaign)
	r.Post("/campaigns/{campaignId}/send", h.handleSendCampaign)
	r.Post("/campaigns/{campaignId}/cancel", h.handleCancelCampaign)

	r.Get("/sso", h.handleGetSSO)
	r.Put("/sso", h.handleSaveSSO)
	r.Delete("/sso", h.handleDeleteSSO)
	r.Get("/invite-link", h.handleGetInviteLink)
	r.Put("/invite-link", h.handleSaveInviteLink)

	h.workspaceObjectRoutes(r)
	r.Get("/teams", h.handleListTeams)
	r.Post("/teams", h.handleCreateTeam)
	r.Patch("/teams/{teamId}", h.handleUpdateTeam)
	r.Delete("/teams/{teamId}", h.handleDeleteTeam)
}

// PublicRoutes need no sign-in: incoming workflow webhooks and unsubscribe links.
func (h *Handler) PublicRoutes(r chi.Router) {
	r.Post("/hooks/workflows/{token}", h.HandleWorkflowWebhook)
	r.Get("/hooks/workflows/{token}", h.HandleWorkflowWebhook)
	r.Get("/public/unsubscribe/{token}", h.HandleUnsubscribe)
	r.Post("/public/unsubscribe/{token}", h.HandleUnsubscribe)
}

func (h *Handler) workspace(ctx context.Context) (uuid.UUID, error) {
	h.wsOnce.Lock()
	defer h.wsOnce.Unlock()
	if h.wsID != uuid.Nil {
		return h.wsID, nil
	}
	id, err := platform.PlatformWorkspaceID(ctx, h.store.Pool)
	if err != nil {
		return uuid.Nil, err
	}
	h.wsID = id
	return id, nil
}

// scope resolves the object and workspace for a request and checks the permission
// needed: a record action (read/create/update/delete), "meta" (read or customize) or
// "customize" (page layouts and custom fields).
func (h *Handler) scope(r *http.Request, need string) (*objectSpec, uuid.UUID, error) {
	spec := specFor(chi.URLParam(r, "object"))
	if spec == nil {
		return nil, uuid.Nil, shared.NotFound("object_not_found")
	}
	sc := scopeFrom(r.Context())
	if !sc.Enabled(spec.Key) {
		return nil, uuid.Nil, shared.NotFound("object_not_found")
	}
	ok := false
	switch need {
	case "customize":
		ok = sc.CanCustomize()
	case "meta":
		ok = sc.Can(spec.Key, "read") || sc.CanCustomize()
	default:
		ok = sc.Can(spec.Key, need)
	}
	if !ok {
		return nil, uuid.Nil, errForbidden
	}
	return spec, sc.WS, nil
}

// ownerFilter limits queries to the owners whose records the actor may see when their
// access is "own": themselves and everyone in roles below theirs (D-48). nil = no limit.
func ownerFilter(r *http.Request, spec *objectSpec) []uuid.UUID {
	return scopeFrom(r.Context()).OwnersFor(spec.Key, actor(r))
}

func actor(r *http.Request) uuid.UUID { return identity.SessionFrom(r.Context()).IdentityID }

func audit(r *http.Request, wsID uuid.UUID, action, entityType string, entityID *uuid.UUID, before, after any) shared.AuditEvent {
	a := actor(r)
	meta := identity.Meta(r)
	return shared.AuditEvent{WorkspaceID: &wsID, ActorID: &a, Action: action, EntityType: entityType, EntityID: entityID,
		Before: before, After: after, IP: meta.IP, RequestID: meta.RequestID}
}

func respond(w http.ResponseWriter, r *http.Request, status int, v any, err error) {
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, status, v)
}

// ---- metadata ----

func (h *Handler) handleMeta(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "meta")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	ctx := r.Context()
	// The page-layout editor places every field; field access limits what records show, not the layout.
	if r.URL.Query().Get("purpose") == "layout" && scopeFrom(ctx).CanCustomize() {
		ctx = withAllFields(ctx)
	}
	m, err := h.meta(ctx, ws, spec)
	respond(w, r, http.StatusOK, m, err)
}

func (h *Handler) handleSaveLayout(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "customize")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in Layout
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		fields, err := allFieldsRaw(r.Context(), tx, ws, spec)
		if err != nil {
			return err
		}
		l, err := validateLayout(in, fields)
		if err != nil {
			return err
		}
		if err := saveLayout(r.Context(), tx, ws, spec.Key, l, actor(r)); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, audit(r, ws, "layout.updated", "layout", nil, nil,
			map[string]any{"object": spec.Key, "sections": len(l.Sections)}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	m, err := h.meta(withAllFields(r.Context()), ws, spec) // customizers edit every field
	respond(w, r, http.StatusOK, m, err)
}

func (h *Handler) handleResetLayout(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "customize")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		if _, err := tx.Exec(r.Context(), `DELETE FROM crm.layouts WHERE workspace_id = $1 AND object_key = $2`, ws, spec.Key); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, audit(r, ws, "layout.reset", "layout", nil, nil, map[string]any{"object": spec.Key}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	m, err := h.meta(withAllFields(r.Context()), ws, spec) // customizers edit every field
	respond(w, r, http.StatusOK, m, err)
}

func (h *Handler) handleCreateField(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "customize")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in fieldInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	label := ""
	if in.Label != nil {
		label = strings.TrimSpace(*in.Label)
	}
	key := strings.TrimSpace(in.Key)
	if key == "" {
		key = snakeKey(label)
	}
	fe := map[string]string{}
	if label == "" || len(label) > 60 {
		fe["label"] = "Enter a label (up to 60 characters)."
	}
	if !fieldKeyRe.MatchString(key) {
		fe["key"] = "2–41 characters: start with a letter, then lower-case letters, digits or underscores."
	}
	if looksSecret(label) || looksSecret(key) {
		fe["label"] = "Don't store passwords, PINs or OTPs in fields — anyone who can open the record would see them. Use \"Give login\" to let this person sign in."
	}
	if !customTypes[in.Type] {
		fe["type"] = "Pick a field type."
	}
	lookupTo := ""
	if isLinkType(in.Type) {
		lookupTo = strings.TrimSpace(in.Lookup)
		if !h.isLookupTarget(lookupTo) {
			fe["lookup"] = "Pick the object this field links to."
		}
	}
	unique := in.Unique != nil && *in.Unique
	if unique && !uniqueTypes[in.Type] {
		fe["unique"] = "Only text, email, phone, URL and number fields can require unique values."
	}
	var options []Option
	if in.Options != nil {
		var msg string
		if options, msg = validateOptions(in.Type, *in.Options); msg != "" {
			fe["options"] = msg
		}
	} else if in.Type == "select" || in.Type == "multiselect" {
		fe["options"] = "Add at least one option."
	}
	helpText := ""
	if in.HelpText != nil {
		helpText = strings.TrimSpace(*in.HelpText)
		if len(helpText) > 300 {
			fe["helpText"] = "Use at most 300 characters."
		}
	}
	if len(fe) > 0 {
		shared.WriteError(w, r, shared.Validation(fe))
		return
	}
	optJSON, _ := json.Marshal(map[string]any{"choices": options})
	required := in.Required != nil && *in.Required

	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		fields, err := allFieldsRaw(r.Context(), tx, ws, spec)
		if err != nil {
			return err
		}
		for _, f := range fields {
			if strings.EqualFold(f.Key, key) || snakeKey(f.Key) == key {
				return shared.Validation(map[string]string{"key": "A field with this key already exists."})
			}
		}
		var count int
		if err := tx.QueryRow(r.Context(), `SELECT count(*) FROM crm.field_definitions WHERE workspace_id = $1 AND object_key = $2`, ws, spec.Key).Scan(&count); err != nil {
			return err
		}
		if count >= 200 {
			return shared.Validation(map[string]string{"key": "This object already has 200 custom fields."})
		}
		// An archived field with the same key is revived rather than duplicated.
		tag, err := tx.Exec(r.Context(), `
			UPDATE crm.field_definitions SET label = $4, type = $5, is_required = $6, options = $7, help_text = NULLIF($8, ''),
			       is_unique = $9, lookup_target = NULLIF($10, ''), status = 'published', updated_at = now()
			WHERE workspace_id = $1 AND object_key = $2 AND key = $3 AND status = 'archived'`,
			ws, spec.Key, key, label, in.Type, required, optJSON, helpText, unique, lookupTo)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			if _, err := tx.Exec(r.Context(), `
				INSERT INTO crm.field_definitions (workspace_id, object_key, key, label, type, is_required, options, help_text, position, created_by, is_unique, lookup_target)
				VALUES ($1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), $9, $10, $11, NULLIF($12, ''))`,
				ws, spec.Key, key, label, in.Type, required, optJSON, helpText, count, actor(r), unique, lookupTo); err != nil {
				return err
			}
		}
		// Place the new field on the page layout (chosen section, else the first).
		fields = append(fields, Field{Key: key})
		l, err := loadLayout(r.Context(), tx, ws, spec, fields)
		if err != nil {
			return err
		}
		placed := false
		for i := range l.Sections {
			if l.Sections[i].ID == in.SectionID || (in.SectionID == "" && i == 0) {
				l.Sections[i].Fields = append(l.Sections[i].Fields, key)
				placed = true
				break
			}
		}
		if !placed && len(l.Sections) > 0 {
			l.Sections[0].Fields = append(l.Sections[0].Fields, key)
		}
		if err := saveLayout(r.Context(), tx, ws, spec.Key, l, actor(r)); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, audit(r, ws, "field.created", "field", nil, nil,
			map[string]any{"object": spec.Key, "key": key, "type": in.Type}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	m, err := h.meta(withAllFields(r.Context()), ws, spec) // customizers edit every field
	respond(w, r, http.StatusCreated, m, err)
}

func (h *Handler) handleUpdateField(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "customize")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	key := chi.URLParam(r, "key")
	var in fieldInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var label, typ, helpText string
		var required, unique bool
		var raw []byte
		err := tx.QueryRow(r.Context(), `
			SELECT label, type, is_required, options, COALESCE(help_text, ''), is_unique FROM crm.field_definitions
			WHERE workspace_id = $1 AND object_key = $2 AND key = $3 AND status = 'published' FOR UPDATE`, ws, spec.Key, key).
			Scan(&label, &typ, &required, &raw, &helpText, &unique)
		if errors.Is(err, pgx.ErrNoRows) {
			if _, std := spec.field(key); std {
				return shared.Forbidden("standard_field", "Standard fields can't be changed; hide them from the layout instead.")
			}
			return shared.NotFound("field_not_found")
		}
		if err != nil {
			return err
		}
		fe := map[string]string{}
		if in.Label != nil {
			label = strings.TrimSpace(*in.Label)
			if label == "" || len(label) > 60 {
				fe["label"] = "Enter a label (up to 60 characters)."
			}
		}
		if in.Required != nil {
			required = *in.Required
		}
		if in.HelpText != nil {
			helpText = strings.TrimSpace(*in.HelpText)
			if len(helpText) > 300 {
				fe["helpText"] = "Use at most 300 characters."
			}
		}
		if in.Unique != nil && *in.Unique != unique {
			unique = *in.Unique
			if unique && !uniqueTypes[typ] {
				fe["unique"] = "Only text, email, phone, URL and number fields can require unique values."
			} else if unique {
				var dupes int
				if err := tx.QueryRow(r.Context(), fmt.Sprintf(`
					SELECT count(*) FROM (SELECT lower(custom->>$2::text) FROM crm.%s WHERE workspace_id = $1 AND deleted_at IS NULL
					AND COALESCE(custom->>$2::text, '') <> '' GROUP BY 1 HAVING count(*) > 1) d`, spec.Table), ws, key).Scan(&dupes); err != nil {
					return err
				}
				if dupes > 0 {
					fe["unique"] = fmt.Sprintf("%d values are already used by more than one record — fix or merge those first.", dupes)
				}
			}
		}
		if in.Options != nil {
			opts, msg := validateOptions(typ, *in.Options)
			if msg != "" {
				fe["options"] = msg
			}
			raw, _ = json.Marshal(map[string]any{"choices": opts})
		}
		if len(fe) > 0 {
			return shared.Validation(fe)
		}
		if _, err := tx.Exec(r.Context(), `
			UPDATE crm.field_definitions SET label = $4, is_required = $5, options = $6, help_text = NULLIF($7, ''), is_unique = $8, updated_at = now()
			WHERE workspace_id = $1 AND object_key = $2 AND key = $3`, ws, spec.Key, key, label, required, raw, helpText, unique); err != nil {
			return err
		}
		return shared.WriteAudit(r.Context(), tx, audit(r, ws, "field.updated", "field", nil, nil,
			map[string]any{"object": spec.Key, "key": key, "label": label, "required": required}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	m, err := h.meta(withAllFields(r.Context()), ws, spec) // customizers edit every field
	respond(w, r, http.StatusOK, m, err)
}

func (h *Handler) handleDeleteField(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "customize")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	key := chi.URLParam(r, "key")
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		tag, err := tx.Exec(r.Context(), `
			UPDATE crm.field_definitions SET status = 'archived', updated_at = now()
			WHERE workspace_id = $1 AND object_key = $2 AND key = $3 AND status = 'published'`, ws, spec.Key, key)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			if _, std := spec.field(key); std {
				return shared.Forbidden("standard_field", "Standard fields can't be removed; hide them from the layout instead.")
			}
			return shared.NotFound("field_not_found")
		}
		// Values stay in custom jsonb, so reviving the field restores them.
		return shared.WriteAudit(r.Context(), tx, audit(r, ws, "field.archived", "field", nil, nil,
			map[string]any{"object": spec.Key, "key": key}))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	m, err := h.meta(withAllFields(r.Context()), ws, spec) // customizers edit every field
	respond(w, r, http.StatusOK, m, err)
}

// looksSecret blocks custom fields meant to hold credentials in plain text (D-33).
func looksSecret(s string) bool {
	k := snakeKey(s)
	for _, part := range strings.Split(k, "_") {
		switch part {
		case "password", "passwd", "pwd", "passcode", "pin", "otp", "cvv", "secret", "token":
			return true
		}
	}
	return strings.Contains(k, "password")
}

// ---- records ----

func queryInt(r *http.Request, name string, def, min, max int) int {
	v, err := strconv.Atoi(r.URL.Query().Get(name))
	if err != nil {
		return def
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

type workspaceFacet struct {
	WorkspaceRef
	Count int `json:"count"`
}

func (h *Handler) workspaceFacets(ctx context.Context, spec *objectSpec) ([]workspaceFacet, error) {
	rows, err := h.store.Pool.Query(ctx, `
		SELECT w.id::text, w.code, CASE WHEN w.is_platform THEN 'Platform CRM' ELSE w.name END, w.is_platform, count(t.id)
		FROM crm.workspaces w LEFT JOIN crm.`+spec.Table+` t ON t.workspace_id = w.id AND t.deleted_at IS NULL
		GROUP BY w.id ORDER BY w.is_platform DESC, count(t.id) DESC, w.name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []workspaceFacet{}
	for rows.Next() {
		var f workspaceFacet
		if err := rows.Scan(&f.ID, &f.Code, &f.Name, &f.IsPlatform, &f.Count); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

type recordInput struct {
	Values          map[string]any `json:"values"`
	ExpectedVersion *int           `json:"expectedVersion"`
}

// objectRules applies per-object business rules on top of field validation.
func objectRules(spec *objectSpec, values map[string]any, current map[string]any, creating bool) map[string]string {
	fe := map[string]string{}
	if spec.Key != "leads" {
		return fe
	}
	status, statusGiven := values["status"].(string)
	curStatus, _ := current["status"].(string)
	if statusGiven && status == "converted" && curStatus != "converted" {
		fe["status"] = "Use Convert to convert a lead."
	}
	if curStatus == "converted" && statusGiven && status != "converted" {
		fe["status"] = "A converted lead's status can't be changed."
	}
	effective := curStatus
	if statusGiven {
		effective = status
	}
	if effective == "lost" {
		reason, given := values["lostReason"]
		if !given {
			reason = current["lostReason"]
		}
		if s, _ := reason.(string); strings.TrimSpace(s) == "" {
			fe["lostReason"] = "Tell us why the lead was lost."
		}
	}
	_ = creating
	return fe
}

func (h *Handler) handleCreate(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "create")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var in recordInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	var row *Row
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var err error
		row, err = h.createRecord(r.Context(), tx, ws, spec, actorFromRequest(r, "ui"), in.Values)
		return err
	})
	if err == nil {
		h.bus.Kick()
	}
	respond(w, r, http.StatusCreated, row, err)
}

func (h *Handler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "update")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	var in recordInput
	if err := shared.DecodeJSON(w, r, &in); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	if in.ExpectedVersion == nil {
		shared.WriteError(w, r, shared.Validation(map[string]string{"expectedVersion": "expectedVersion is required."}))
		return
	}
	var row *Row
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		var err error
		row, err = h.updateValues(r.Context(), tx, ws, spec, id, actorFromRequest(r, "ui"), in.Values, in.ExpectedVersion, ownerFilter(r, spec))
		return err
	})
	if err == nil {
		h.bus.Kick()
	}
	respond(w, r, http.StatusOK, row, err)
}

func (h *Handler) handleDelete(w http.ResponseWriter, r *http.Request) {
	spec, ws, err := h.scope(r, "delete")
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("record_not_found"))
		return
	}
	err = h.store.WithTx(r.Context(), func(tx pgx.Tx) error {
		return h.deleteRecord(r.Context(), tx, ws, spec, id, actorFromRequest(r, "ui"), ownerFilter(r, spec))
	})
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	h.bus.Kick()
	w.WriteHeader(http.StatusNoContent)
}

// ---- lookup (combobox search) ----

func (h *Handler) handleLookup(w http.ResponseWriter, r *http.Request) {
	sc := scopeFrom(r.Context())
	ws := sc.WS
	target := chi.URLParam(r, "target")
	if !sc.Owner {
		switch {
		case target == "users":
		case specFor(target) != nil && sc.Can(target, "read"):
		default:
			shared.WriteError(w, r, errForbidden)
			return
		}
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	like := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q) + "%"
	var sql string
	args := []any{like}
	switch target {
	case "users":
		sql = `SELECT i.id::text, i.display_name || COALESCE(' · ' || e.value_normalized, '') FROM crm.identities i
			LEFT JOIN crm.verified_identifiers e ON e.identity_id = i.id AND e.kind = 'email' AND e.namespace = 'global'
			WHERE i.status <> 'deleted' AND (i.display_name ILIKE $1 OR e.value_normalized ILIKE $1)
			  AND ($2::boolean OR EXISTS (SELECT 1 FROM crm.memberships m WHERE m.identity_id = i.id AND m.workspace_id = $3 AND m.status = 'active'))
			ORDER BY i.display_name LIMIT 20`
		args = append(args, sc.Owner, ws)
	case "products":
		sql = `SELECT id::text, name FROM crm.products WHERE status <> 'archived' AND (name ILIKE $1 OR key ILIKE $1) ORDER BY name LIMIT 20`
	case "workspaces":
		sql = `SELECT id::text, name FROM crm.workspaces WHERE NOT is_platform AND (name ILIKE $1 OR code ILIKE $1) ORDER BY name LIMIT 20`
	default:
		spec := specFor(target)
		if spec == nil {
			shared.WriteError(w, r, shared.NotFound("object_not_found"))
			return
		}
		parts := make([]string, len(spec.SearchSQL))
		for i, c := range spec.SearchSQL {
			parts[i] = c + " ILIKE $1"
		}
		own := sc.OwnersFor(target, actor(r))
		sql = `SELECT t.id::text, ` + spec.TitleSQL + ` || ' · ' || t.code FROM crm.` + spec.Table + ` t
			WHERE t.workspace_id = $2 AND t.deleted_at IS NULL AND ($3::uuid[] IS NULL OR t.owner_id = ANY($3)) AND (` + strings.Join(parts, " OR ") + `)
			ORDER BY t.updated_at DESC LIMIT 20`
		args = append(args, ws, own)
	}
	rows, err := h.store.Pool.Query(r.Context(), sql, args...)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []LookupValue{}
	for rows.Next() {
		var v LookupValue
		if err := rows.Scan(&v.ID, &v.Label); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		v.Object = target
		out = append(out, v)
	}
	respond(w, r, http.StatusOK, map[string]any{"data": out}, rows.Err())
}
