package handler

import (
	"encoding/json"
	"net/http"
)

type initiateUploadRequest struct {
	Bucket      string `json:"bucket"`
	Key         string `json:"key"`
	ContentType string `json:"content_type,omitempty"`
}

func (h *Handler) initiateUpload(w http.ResponseWriter, r *http.Request) {
	var req initiateUploadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Placeholder — full multipart upload requires upload session management.
	writeJSON(w, http.StatusCreated, map[string]string{
		"status":  "initiated",
		"bucket":  req.Bucket,
		"key":     req.Key,
		"message": "multipart upload sessions require the store layer",
	})
}

func (h *Handler) uploadChunk(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusAccepted, map[string]string{
		"status":  "accepted",
		"id":      r.PathValue("id"),
		"chunk":   r.PathValue("number"),
		"message": "chunk upload accepted",
	})
}

func (h *Handler) completeUpload(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "completed",
		"id":     r.PathValue("id"),
	})
}

func (h *Handler) abortUpload(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "aborted",
		"id":     r.PathValue("id"),
	})
}

func (h *Handler) getUploadStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "unknown",
		"id":     r.PathValue("id"),
	})
}
