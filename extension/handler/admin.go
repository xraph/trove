package handler

import (
	"net/http"
)

func (h *Handler) stats(w http.ResponseWriter, r *http.Request) {
	stats := map[string]any{
		"driver":         h.trove.Driver().Name(),
		"active_streams": h.trove.Pool().ActiveCount(),
		"cas_enabled":    h.trove.CAS() != nil,
	}

	writeJSON(w, http.StatusOK, stats)
}
