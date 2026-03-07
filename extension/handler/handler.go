// Package handler provides HTTP handlers for Trove REST API.
package handler

import (
	"encoding/json"
	"net/http"

	log "github.com/xraph/go-utils/log"

	"github.com/xraph/trove"
	"github.com/xraph/trove/extension/store"
)

// Handler provides HTTP handlers for Trove operations.
type Handler struct {
	trove  *trove.Trove
	store  *store.Store
	logger log.Logger
	mux    *http.ServeMux
}

// New creates a new Handler.
func New(t *trove.Trove, s *store.Store, logger log.Logger) *Handler {
	if logger == nil {
		logger = log.NewNoopLogger()
	}
	h := &Handler{
		trove:  t,
		store:  s,
		logger: logger,
		mux:    http.NewServeMux(),
	}
	h.registerRoutes()
	return h
}

func (h *Handler) registerRoutes() {
	// Bucket operations.
	h.mux.HandleFunc("POST /buckets", h.createBucket)
	h.mux.HandleFunc("GET /buckets", h.listBuckets)
	h.mux.HandleFunc("GET /buckets/{bucket}", h.getBucket)
	h.mux.HandleFunc("DELETE /buckets/{bucket}", h.deleteBucket)

	// Object operations.
	h.mux.HandleFunc("PUT /buckets/{bucket}/objects/{key...}", h.putObject)
	h.mux.HandleFunc("GET /buckets/{bucket}/objects/{key...}", h.getObject)
	h.mux.HandleFunc("DELETE /buckets/{bucket}/objects/{key...}", h.deleteObject)
	h.mux.HandleFunc("HEAD /buckets/{bucket}/objects/{key...}", h.headObject)
	h.mux.HandleFunc("GET /buckets/{bucket}/list", h.listObjects)

	// Upload operations.
	h.mux.HandleFunc("POST /uploads", h.initiateUpload)
	h.mux.HandleFunc("PUT /uploads/{id}/chunks/{number}", h.uploadChunk)
	h.mux.HandleFunc("POST /uploads/{id}/complete", h.completeUpload)
	h.mux.HandleFunc("POST /uploads/{id}/abort", h.abortUpload)
	h.mux.HandleFunc("GET /uploads/{id}", h.getUploadStatus)

	// CAS operations.
	h.mux.HandleFunc("POST /cas/store", h.casStore)
	h.mux.HandleFunc("GET /cas/{hash}", h.casRetrieve)
	h.mux.HandleFunc("HEAD /cas/{hash}", h.casExists)

	// Admin.
	h.mux.HandleFunc("GET /admin/stats", h.stats)
}

// ServeHTTP implements http.Handler.
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mux.ServeHTTP(w, r)
}

// --- Response helpers ---

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
