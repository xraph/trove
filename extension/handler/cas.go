package handler

import (
	"io"
	"net/http"
)

func (h *Handler) casStore(w http.ResponseWriter, r *http.Request) {
	casEngine := h.trove.CAS()
	if casEngine == nil {
		writeError(w, http.StatusNotFound, "CAS is not enabled")
		return
	}

	hash, info, err := casEngine.Store(r.Context(), r.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"hash": hash,
		"info": info,
	})
}

func (h *Handler) casRetrieve(w http.ResponseWriter, r *http.Request) {
	casEngine := h.trove.CAS()
	if casEngine == nil {
		writeError(w, http.StatusNotFound, "CAS is not enabled")
		return
	}

	hash := r.PathValue("hash")
	reader, err := casEngine.Retrieve(r.Context(), hash)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", "application/octet-stream")
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, reader)
}

func (h *Handler) casExists(w http.ResponseWriter, r *http.Request) {
	casEngine := h.trove.CAS()
	if casEngine == nil {
		writeError(w, http.StatusNotFound, "CAS is not enabled")
		return
	}

	hash := r.PathValue("hash")
	exists, err := casEngine.Exists(r.Context(), hash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if !exists {
		w.WriteHeader(http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusOK)
}
