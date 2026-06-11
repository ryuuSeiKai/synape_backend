// Synape Server — Go implementation replacing the previous Express + Firebase Functions backend.
//
// Routes:
//   GET  /                          — Homepage (this page)
//   GET  /auth/callback              — GitHub OAuth callback (deep-link redirect)
//   GET  /auth/google-callback.html  — Google OAuth callback (deep-link redirect)
//   POST /api/auth/github/exchange   — Exchange GitHub code for token
//   POST /api/auth/google/exchange   — Exchange Google code for token
//   POST /api/auth/google/refresh    — Refresh Google tokens
//   GET  /api/auth/me                — Get current user + entitlements
//   PATCH /api/auth/me               — Update profile
//   DELETE /api/auth/me              — Delete account
//   POST /api/auth/link              — Link provider to account
//   POST /api/auth/unlink            — Unlink provider from account
//   GET  /api/sync/state             — Get sync state
//   GET  /api/sync/pull/{kind}       — Pull sync blob
//   PUT  /api/sync/push/{kind}       — Push sync blob
//   DELETE /api/sync/wipe            — Wipe all sync data
//   GET  /health                     — Health check
//   GET  /changelog                  — Changelog page
//   GET  /terms                      — Terms of service
//   GET  /privacy                    — Privacy policy
//
// All other /api/* paths are proxied to UPSTREAM_API if configured.
// Non-matched paths return 404.
package main

import (
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/ryuuvu/synape-server/internal/auth"
	"github.com/ryuuvu/synape-server/internal/config"
	"github.com/ryuuvu/synape-server/internal/db"
	"github.com/ryuuvu/synape-server/internal/handler"
	"github.com/ryuuvu/synape-server/internal/marketplace"
	"github.com/ryuuvu/synape-server/internal/proxy"
	"github.com/ryuuvu/synape-server/internal/sync"
)

func main() {
	cfg := config.Load()

	// ─── Database ───────────────────────────────────────────────────────
	store, err := db.New(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer store.DB.Close()
	log.Printf("Database: %s", cfg.DatabaseURL)

	// ─── Handlers ───────────────────────────────────────────────────────
	authH := auth.NewHandler(store, cfg)
	syncH := sync.NewHandler(store)
	staticH := handler.NewHandler(cfg)

	// ─── Router ────────────────────────────────────────────────────────
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(middleware.RealIP)
	r.Use(securityHeaders)

	// Homepage
	r.Get("/", staticH.Home)

	// Health check
	r.Get("/health", staticH.Health)

	// OAuth callback redirects (no auth required)
	r.Get("/auth/callback", authH.CallbackRedirect)
	r.Get("/auth/google-callback.html", authH.GoogleCallbackRedirect)

	// Ticket polling routes (no deep-link needed)
	r.Post("/api/auth/ticket", authH.CreateTicket)
	r.Get("/api/auth/ticket/{id}", authH.PollTicket)

	// Auth API routes
	r.Post("/api/auth/github/exchange", authH.GitHubExchange)
	r.Post("/api/auth/google/exchange", authH.GoogleExchange)
	r.Post("/api/auth/google/refresh", authH.GoogleRefresh)
	r.Get("/api/auth/me", authH.Me)
	r.Patch("/api/auth/me", authH.UpdateProfile)
	r.Delete("/api/auth/me", authH.DeleteAccount)
	r.Post("/api/auth/link", authH.Link)
	r.Post("/api/auth/unlink", authH.Unlink)

	// Sync API routes
	r.Get("/api/sync/state", syncH.State)
	r.Get("/api/sync/pull/{kind}", syncH.Pull)
	r.Put("/api/sync/push/{kind}", syncH.Push)
	r.Delete("/api/sync/wipe", syncH.Wipe)

	// Marketplace API routes
	r.Get("/api/marketplace/skills", marketplace.HandleSkills)
	r.Get("/api/marketplace/skill", marketplace.HandleSkill)

	// Static pages
	r.Get("/changelog", staticH.StaticPage)
	r.Get("/changelog.html", staticH.StaticPage)
	r.Get("/terms", staticH.StaticPage)
	r.Get("/privacy", staticH.StaticPage)

	// Reverse proxy for remaining /api/* paths
	if cfg.UpstreamAPI != "" {
		log.Printf("Upstream API: %s", cfg.UpstreamAPI)
		r.HandleFunc("/api/*", func(w http.ResponseWriter, r *http.Request) {
			proxy.ReverseProxy(cfg.UpstreamAPI).ServeHTTP(w, r)
		})
	} else {
		log.Println("API proxy: disabled")
	}

	// Catch-all 404
	r.NotFound(handler.NotFound)

	// ─── Start ──────────────────────────────────────────────────────────
	addr := fmt.Sprintf(":%d", cfg.Port)
	log.Printf("Synape server running on %s", addr)
	log.Printf("  Deep-link scheme: %s", cfg.DeepLinkScheme)
	if err := http.ListenAndServe(addr, r); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Prevent MIME-type sniffing
		w.Header().Set("X-Content-Type-Options", "nosniff")
		// Prevent clickjacking
		w.Header().Set("X-Frame-Options", "DENY")
		// CORS for the desktop app deep-link origin (allow all for now)
		origin := r.Header.Get("Origin")
		if origin != "" && !strings.HasPrefix(origin, "synape-editor://") {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Provider, X-Confirm, X-Forwarded-Proto")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}
