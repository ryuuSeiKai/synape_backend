// Package handler provides simple HTTP handlers for static pages, health checks, and 404s.
package handler

import (
	"encoding/json"
	"net/http"

	"github.com/ryuuvu/synape-server/internal/config"
)

// Handler holds dependencies for simple HTTP handlers.
type Handler struct {
	Config config.Config
	Scheme string
}

// NewHandler creates a new Handler.
func NewHandler(cfg config.Config) *Handler {
	return &Handler{Config: cfg, Scheme: cfg.DeepLinkScheme}
}

// Health handles GET /health.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"scheme": h.Scheme,
	})
}

// StaticPage handles static content pages.
func (h *Handler) StaticPage(w http.ResponseWriter, r *http.Request) {
	pages := map[string]string{
		"/changelog":      "<h1>Changelog</h1><p>Self-hosted Synape instance.</p>",
		"/changelog.html": "<h1>Changelog</h1><p>Self-hosted Synape instance.</p>",
		"/terms":          "<h1>Terms of Service</h1><p>Your terms here.</p>",
		"/privacy":        "<h1>Privacy Policy</h1><p>Your privacy policy here.</p>",
	}

	html, ok := pages[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(html))
}

// NotFound handles 404 for all unmatched routes.
func NotFound(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("Not found"))
}
