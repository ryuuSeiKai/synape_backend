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

// Home handles GET / — Synape server homepage.
func (h *Handler) Home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write([]byte(homepageHTML(h.Scheme)))
}

// Health handles GET /health.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status": "ok",
		"scheme": h.Scheme,
	})
}

func homepageHTML(scheme string) string {
	return `<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>Synape Server</title>
<style>
  *, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, 'Helvetica Neue', Arial, sans-serif;
    background: #0d1117; color: #e6edf3; min-height: 100vh; display: flex; flex-direction: column;
  }
  .container { max-width: 720px; margin: 0 auto; padding: 2rem 1.5rem; flex: 1; }
  header { text-align: center; padding: 4rem 0 2rem; }
  .logo {
    display: inline-flex; align-items: center; justify-content: center;
    width: 72px; height: 72px; border-radius: 16px;
    background: linear-gradient(135deg, #6b8cff, #8b5cf6); margin-bottom: 1rem;
  }
  h1 { font-size: 2rem; font-weight: 700; margin-bottom: .5rem; }
  p.subtitle { color: #8b949e; font-size: 1.05rem; }
  .status-card {
    background: #161b22; border: 1px solid #30363d; border-radius: 12px;
    padding: 1.5rem; margin: 2rem 0; display: flex; align-items: center; gap: .75rem;
  }
  .dot { width: 10px; height: 10px; border-radius: 50%; background: #3fb950; flex-shrink: 0; }
  .status-card span { font-size: .9rem; color: #8b949e; }
  .status-card strong { color: #e6edf3; }
  h2 { font-size: 1.25rem; margin: 2rem 0 1rem; }
  .endpoints { list-style: none; }
  .endpoints li {
    display: flex; align-items: center; justify-content: space-between;
    padding: .75rem 1rem; border-bottom: 1px solid #21262d;
  }
  .endpoints li:last-child { border-bottom: none; }
  .method {
    display: inline-block; font-size: .7rem; font-weight: 600; padding: 2px 8px;
    border-radius: 4px; margin-right: .75rem; min-width: 44px; text-align: center;
  }
  .get { background: #1f6feb22; color: #58a6ff; }
  .post { background: #23863622; color: #3fb950; }
  .put { background: #d2992222; color: #d29922; }
  .patch { background: #b0880022; color: #d29922; }
  .delete { background: #da363322; color: #f85149; }
  .path { font-family: 'SF Mono', 'Fira Code', monospace; font-size: .85rem; color: #e6edf3; }
  .desc { font-size: .8rem; color: #8b949e; }
  footer { text-align: center; padding: 2rem; color: #484f58; font-size: .8rem; border-top: 1px solid #21262d; }
  footer a { color: #58a6ff; text-decoration: none; }
</style>
</head>
<body>
<div class="container">
  <header>
    <div class="logo">
      <svg viewBox="0 0 24 24" width="36" height="36" fill="white"><path d="M12 2C6.48 2 2 6.48 2 12s4.48 10 10 10 10-4.48 10-10S17.52 2 12 2zm-1 17.93c-3.95-.49-7-3.85-7-7.93 0-.62.08-1.21.21-1.79L9 15v1c0 1.1.9 2 2 2v1.93zm6.9-2.54c-.26-.81-1-1.39-1.9-1.39h-1v-3c0-.55-.45-1-1-1H8v-2h2c.55 0 1-.45 1-1V7h2c1.1 0 2-.9 2-2v-.41c2.93 1.19 5 4.06 5 7.41 0 2.08-.8 3.97-2.1 5.39z"/></svg>
    </div>
    <h1>Synape Server</h1>
    <p class="subtitle">Self-hosted API server for the Synape desktop app</p>
  </header>

  <div class="status-card">
    <div class="dot"></div>
    <div>
      <strong>Server is running</strong>
      <span>&middot; Deep-link scheme: ` + scheme + `</span>
    </div>
  </div>

  <h2>Endpoints</h2>
  <ul class="endpoints">
    <li><span><span class="method get">GET</span><span class="path">/health</span></span><span class="desc">Health check</span></li>
    <li><span><span class="method get">GET</span><span class="path">/auth/callback</span></span><span class="desc">GitHub OAuth callback</span></li>
    <li><span><span class="method get">GET</span><span class="path">/auth/google-callback.html</span></span><span class="desc">Google OAuth callback</span></li>
    <li><span><span class="method post">POST</span><span class="path">/api/auth/github/exchange</span></span><span class="desc">GitHub token exchange</span></li>
    <li><span><span class="method post">POST</span><span class="path">/api/auth/google/exchange</span></span><span class="desc">Google token exchange</span></li>
    <li><span><span class="method post">POST</span><span class="path">/api/auth/google/refresh</span></span><span class="desc">Google token refresh</span></li>
    <li><span><span class="method get">GET</span><span class="path">/api/auth/me</span></span><span class="desc">Current user info</span></li>
    <li><span><span class="method get">GET</span><span class="path">/changelog</span></span><span class="desc">Changelog</span></li>
    <li><span><span class="method get">GET</span><span class="path">/terms</span></span><span class="desc">Terms of Service</span></li>
    <li><span><span class="method get">GET</span><span class="path">/privacy</span></span><span class="desc">Privacy Policy</span></li>
  </ul>
</div>
<footer>
  <p><a href="https://github.com/ryuuvu/synape">Synape</a> &middot; Self-hosted edition</p>
</footer>
</body>
</html>`
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
