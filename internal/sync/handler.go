// Package sync implements cloud sync blob management (state, pull, push, wipe).
package sync

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/ryuuvu/synape-server/internal/db"
)

// Handler holds dependencies for sync HTTP handlers.
type Handler struct {
	Store *db.Store
}

// NewHandler creates a new sync Handler.
func NewHandler(store *db.Store) *Handler {
	return &Handler{Store: store}
}

// SyncStateRow is the API response for a sync state row.
type SyncStateRow struct {
	Kind        string `json:"kind"`
	ContentHash string `json:"contentHash"`
	UpdatedAt   string `json:"updatedAt"`
}

// SyncPullResponse is the API response for sync pull.
type SyncPullResponse struct {
	Kind        string `json:"kind"`
	ContentHash string `json:"contentHash"`
	UpdatedAt   string `json:"updatedAt"`
	Payload     string `json:"payload"`
}

// SyncPushResponse is the API response for sync push.
type SyncPushResponse struct {
	Kind        string `json:"kind"`
	ContentHash string `json:"contentHash"`
	UpdatedAt   string `json:"updatedAt"`
}

// SyncPushRequest is the API request for sync push.
type SyncPushRequest struct {
	ContentHash string `json:"contentHash"`
	Payload     string `json:"payload"`
	PrevHash    *string `json:"prevHash,omitempty"`
}

// State handles GET /api/sync/state.
func (h *Handler) State(w http.ResponseWriter, r *http.Request) {
	blobs, err := h.Store.GetSyncBlobs()
	if err != nil {
		log.Printf("[sync] state error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	rows := make([]SyncStateRow, len(blobs))
	for i, b := range blobs {
		rows[i] = SyncStateRow{
			Kind:        b.Kind,
			ContentHash: b.ContentHash,
			UpdatedAt:   b.UpdatedAt,
		}
	}

	writeJSON(w, http.StatusOK, rows)
}

// Pull handles GET /api/sync/pull/{kind}.
func (h *Handler) Pull(w http.ResponseWriter, r *http.Request) {
	kind := extractKind(r.URL.Path, "/api/sync/pull/")
	if kind == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing kind"})
		return
	}

	blob, err := h.Store.GetSyncBlob(kind)
	if err != nil {
		log.Printf("[sync] pull error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if blob == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}

	resp := SyncPullResponse{
		Kind:        blob.Kind,
		ContentHash: blob.ContentHash,
		UpdatedAt:   blob.UpdatedAt,
		Payload:     blob.Payload,
	}

	writeJSON(w, http.StatusOK, resp)
}

// Push handles PUT /api/sync/push/{kind}.
func (h *Handler) Push(w http.ResponseWriter, r *http.Request) {
	kind := extractKind(r.URL.Path, "/api/sync/push/")
	if kind == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing kind"})
		return
	}

	var req SyncPushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	// Optimistic concurrency check
	if req.PrevHash != nil && *req.PrevHash != "*" {
		existing, err := h.Store.GetSyncBlob(kind)
		if err != nil {
			log.Printf("[sync] push conflict check error: %v", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}
		if existing != nil && existing.ContentHash != *req.PrevHash {
			writeJSON(w, http.StatusPreconditionFailed, map[string]interface{}{
				"error":              "conflict",
				"currentHash":        existing.ContentHash,
				"currentUpdatedAt":   existing.UpdatedAt,
			})
			return
		}
	}

	if err := h.Store.UpsertSyncBlob(kind, req.ContentHash, req.Payload); err != nil {
		log.Printf("[sync] push error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	blob, _ := h.Store.GetSyncBlob(kind)

	resp := SyncPushResponse{
		Kind:        kind,
		ContentHash: req.ContentHash,
	}
	if blob != nil {
		resp.UpdatedAt = blob.UpdatedAt
	}

	writeJSON(w, http.StatusOK, resp)
}

// Wipe handles DELETE /api/sync/wipe.
func (h *Handler) Wipe(w http.ResponseWriter, r *http.Request) {
	confirm := r.Header.Get("X-Confirm")
	if confirm != "yes" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "requires X-Confirm: yes"})
		return
	}

	if err := h.Store.WipeSyncBlobs(); err != nil {
		log.Printf("[sync] wipe error: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// ─── Helpers ───────────────────────────────────────────────────────────────

func extractKind(path, prefix string) string {
	if !strings.HasPrefix(path, prefix) {
		return ""
	}
	kind := strings.TrimPrefix(path, prefix)
	// Remove trailing slash
	kind = strings.TrimSuffix(kind, "/")
	return kind
}

func contentHash(payload string) string {
	h := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("%x", h)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
