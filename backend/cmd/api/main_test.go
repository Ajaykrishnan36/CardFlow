package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"cogniz-crm-backend/internal/discovery"
	"github.com/go-chi/chi/v5"
)

func TestRoutes(t *testing.T) {
	r := chi.NewRouter()

	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("CardFlow API"))
	})

	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"healthy"}`))
	})

	discSvc := discovery.NewDiscoveryService(nil)
	discHandler := discovery.NewDiscoveryHandler(discSvc)
	r.Get("/b/{slug}", discHandler.RenderPublicHTML)

	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/categories", discHandler.GetCategories)
	})

	// Test /
	reqRoot := httptest.NewRequest("GET", "/", nil)
	wRoot := httptest.NewRecorder()
	r.ServeHTTP(wRoot, reqRoot)
	if wRoot.Code != http.StatusOK {
		t.Errorf("Expected 200 for /, got %d", wRoot.Code)
	}

	// Test /health
	reqHealth := httptest.NewRequest("GET", "/health", nil)
	wHealth := httptest.NewRecorder()
	r.ServeHTTP(wHealth, reqHealth)
	if wHealth.Code != http.StatusOK {
		t.Errorf("Expected 200 for /health, got %d", wHealth.Code)
	}

	// Test /b/kovai-precision-tools
	reqSlug := httptest.NewRequest("GET", "/b/kovai-precision-tools", nil)
	wSlug := httptest.NewRecorder()
	r.ServeHTTP(wSlug, reqSlug)
	if wSlug.Code != http.StatusOK {
		t.Errorf("Expected 200 for /b/slug, got %d", wSlug.Code)
	}
}
