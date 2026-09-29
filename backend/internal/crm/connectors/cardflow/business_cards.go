package cardflow

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"cardflow-backend/internal/crm/identity"
	"cardflow-backend/internal/crm/shared"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// BusinessSaver is an app user who saved this business's card to their vault.
type BusinessSaver struct {
	UserID  string    `json:"userId"`
	Name    string    `json:"name"`
	Phone   string    `json:"phone"`
	City    string    `json:"city"`
	SavedAt time.Time `json:"savedAt"`
}

// GET /w/{code}/app/businesses/{id}/savers — who saved this card (one row per person).
func (c *Connector) handleBusinessSavers(w http.ResponseWriter, r *http.Request) {
	if _, ok := c.appScope(w, r, "app_business", "read"); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil || !c.hasCards {
		shared.WriteJSON(w, http.StatusOK, map[string]any{"data": []BusinessSaver{}})
		return
	}
	rows, err := c.store.Pool.Query(r.Context(), `
		SELECT u.id::text, COALESCE(NULLIF(u.name, 'CardFlow User'), ''), u.phone, COALESCE(u.city, ''), min(sc.created_at)
		FROM public.saved_cards sc
		JOIN public.users u ON u.id = sc.user_id
		WHERE sc.linked_business_id = $1 AND sc.deleted_at IS NULL
		GROUP BY u.id, u.name, u.phone, u.city
		ORDER BY min(sc.created_at) DESC
		LIMIT 500`, id)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	defer rows.Close()
	out := []BusinessSaver{}
	for rows.Next() {
		var s BusinessSaver
		if err := rows.Scan(&s.UserID, &s.Name, &s.Phone, &s.City, &s.SavedAt); err != nil {
			shared.WriteError(w, r, err)
			return
		}
		out = append(out, s)
	}
	shared.WriteJSON(w, http.StatusOK, map[string]any{"data": out})
}

func cardSide(v string) string {
	if strings.EqualFold(v, "back") {
		return "back"
	}
	return "front"
}

// GET /w/{code}/app/businesses/{id}/card-image?side=front|back
func (c *Connector) handleBusinessCardImage(w http.ResponseWriter, r *http.Request) {
	if _, ok := c.appScope(w, r, "app_business", "read"); !ok {
		return
	}
	id := chi.URLParam(r, "id")
	if _, err := uuid.Parse(id); err != nil {
		shared.WriteError(w, r, shared.NotFound("image_not_found"))
		return
	}
	var data []byte
	var ct string
	err := c.store.Pool.QueryRow(r.Context(), `
		SELECT image_data, COALESCE(NULLIF(content_type, ''), 'image/jpeg') FROM public.business_card_images
		WHERE business_id = $1 AND side = $2 AND length(image_data) > 0`, id, cardSide(r.URL.Query().Get("side"))).Scan(&data, &ct)
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("image_not_found"))
		return
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=60")
	_, _ = w.Write(data)
}

const maxCardImageBytes = 8 << 20

// PUT /w/{code}/app/businesses/{id}/card-image  {"side":"front","imageData":"data:image/jpeg;base64,..."}
func (c *Connector) handleUploadBusinessCardImage(w http.ResponseWriter, r *http.Request) {
	sc, ok := c.appScope(w, r, "app_business", "update")
	if !ok {
		return
	}
	ctx := r.Context()
	id := chi.URLParam(r, "id")
	bizID, err := uuid.Parse(id)
	if err != nil {
		shared.WriteError(w, r, shared.NotFound("business_not_found"))
		return
	}
	var in struct {
		Side      string `json:"side"`
		ImageData string `json:"imageData"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 12<<20)).Decode(&in); err != nil {
		shared.WriteError(w, r, shared.Validation(map[string]string{"imageData": "Choose an image file (JPG or PNG, up to 8 MB)."}))
		return
	}
	data, ct, ok := decodeImageDataURL(in.ImageData)
	if !ok || len(data) == 0 || len(data) > maxCardImageBytes {
		shared.WriteError(w, r, shared.Validation(map[string]string{"imageData": "Choose an image file (JPG or PNG, up to 8 MB)."}))
		return
	}
	var exists bool
	if err := c.store.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM public.businesses WHERE id = $1 AND deleted_at IS NULL)`, bizID).Scan(&exists); err != nil || !exists {
		shared.WriteError(w, r, shared.NotFound("business_not_found"))
		return
	}
	side := cardSide(in.Side)
	if _, err := c.store.Pool.Exec(ctx, `
		INSERT INTO public.business_card_images (business_id, side, image_data, content_type)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (business_id, side) DO UPDATE SET image_data = EXCLUDED.image_data, content_type = EXCLUDED.content_type`,
		bizID, side, data, ct); err != nil {
		shared.WriteError(w, r, err)
		return
	}
	_, _ = c.store.Pool.Exec(ctx, `UPDATE public.businesses SET updated_at = now() WHERE id = $1`, bizID)
	actor := identity.SessionFrom(ctx).IdentityID
	_ = shared.WriteAudit(ctx, c.store.Pool, shared.AuditEvent{WorkspaceID: &sc.WS, ActorKind: "identity", ActorID: &actor,
		Action: "app.business_card_image_updated", EntityType: "app_business", EntityID: &bizID,
		After: map[string]any{"side": side, "bytes": len(data)}})
	b, err := c.loadBusiness(ctx, id)
	if err != nil {
		shared.WriteError(w, r, err)
		return
	}
	shared.WriteJSON(w, http.StatusOK, b)
}

func decodeImageDataURL(v string) ([]byte, string, bool) {
	if !strings.HasPrefix(v, "data:") {
		return nil, "", false
	}
	comma := strings.Index(v, ",")
	if comma < 0 {
		return nil, "", false
	}
	meta := v[5:comma]
	ct := strings.TrimSuffix(meta, ";base64")
	if !strings.HasPrefix(ct, "image/") || !strings.HasSuffix(meta, ";base64") {
		return nil, "", false
	}
	data, err := base64.StdEncoding.DecodeString(v[comma+1:])
	if err != nil {
		return nil, "", false
	}
	return data, ct, true
}
