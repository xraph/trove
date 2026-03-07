package handler

import (
	"encoding/json"
	"net/http"

	log "github.com/xraph/go-utils/log"

	"github.com/xraph/trove/extension/model"
	troveid "github.com/xraph/trove/id"
)

type createBucketRequest struct {
	Name       string `json:"name"`
	Versioning bool   `json:"versioning,omitempty"`
	CASEnabled bool   `json:"cas_enabled,omitempty"`
}

func (h *Handler) createBucket(w http.ResponseWriter, r *http.Request) {
	var req createBucketRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	// Create in storage driver.
	if err := h.trove.CreateBucket(r.Context(), req.Name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Record in metadata store.
	if h.store != nil {
		bucket := &model.Bucket{
			ID:         troveid.NewBucketID().String(),
			Name:       req.Name,
			Driver:     h.trove.Driver().Name(),
			Versioning: req.Versioning,
			CASEnabled: req.CASEnabled,
		}
		if err := h.store.CreateBucket(r.Context(), bucket); err != nil {
			h.logger.Error("failed to record bucket in store", log.Error(err))
		}
	}

	writeJSON(w, http.StatusCreated, map[string]string{"name": req.Name})
}

func (h *Handler) listBuckets(w http.ResponseWriter, r *http.Request) {
	if h.store != nil {
		buckets, err := h.store.ListBuckets(r.Context(), r.URL.Query().Get("tenant"))
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, buckets)
		return
	}

	// Fallback to driver listing.
	buckets, err := h.trove.ListBuckets(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, buckets)
}

func (h *Handler) getBucket(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("bucket")

	if h.store != nil {
		bucket, err := h.store.GetBucketByName(r.Context(), name)
		if err != nil {
			writeError(w, http.StatusNotFound, "bucket not found")
			return
		}
		writeJSON(w, http.StatusOK, bucket)
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"name": name})
}

func (h *Handler) deleteBucket(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("bucket")

	if err := h.trove.DeleteBucket(r.Context(), name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
