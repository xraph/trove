package handler

import (
	"errors"
	"net/http"

	log "github.com/xraph/go-utils/log"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
)

// opaqueMessage is the entire body of an unclassified failure. Driver errors
// wrap os-level ones, whose messages carry absolute server filesystem paths —
// "mkdir /srv/trove/data/nested: permission denied" tells a client where the
// storage root lives and which directories exist under it. Nothing a driver
// has not deliberately classified is fit to publish.
const opaqueMessage = "internal error"

// statusForError maps a classified Trove error onto the HTTP status that
// describes it, returning 0 when the error carries no classification and the
// caller must fall back to an opaque 500.
//
// ErrInvalidPath is checked before the not-found family and reported as 400
// rather than 404 on purpose: the driver's containment guard refused to
// resolve the key at all, so nothing was looked up. Answering 404 would
// describe a malformed request as a missing resource, and would let a client
// use rejected keys to probe which objects exist.
func statusForError(err error) int {
	switch {
	case errors.Is(err, driver.ErrInvalidPath),
		errors.Is(err, trove.ErrKeyEmpty),
		errors.Is(err, trove.ErrBucketEmpty):
		return http.StatusBadRequest
	case errors.Is(err, driver.ErrNotFound):
		// Covers ErrObjectNotFound and ErrBucketNotFound, which unwrap to it.
		return http.StatusNotFound
	case errors.Is(err, driver.ErrBucketExists):
		return http.StatusConflict
	case errors.Is(err, driver.ErrPermissionDenied):
		return http.StatusForbidden
	case errors.Is(err, driver.ErrQuotaExceeded):
		return http.StatusInsufficientStorage
	default:
		return 0
	}
}

// classify returns the status and response message for an error from a Trove
// operation, logging the error itself when it is not fit to publish.
//
// The disclosure rule is the classification: a driver that wraps a sentinel
// has asserted the accompanying message is safe to return, and the drivers
// hold up their end — localdriver's and sftpdriver's containment guards name
// the caller's own segment but never the directory it resolved against.
// Everything else is logged with the request path and answered opaquely, so
// operators keep the diagnostic and clients get nothing.
func (h *Handler) classify(r *http.Request, op string, err error) (status int, msg string) {
	if status = statusForError(err); status != 0 {
		return status, err.Error()
	}

	h.logger.Error("trove operation failed",
		log.String("op", op),
		log.String("path", r.URL.Path),
		log.Error(err),
	)
	return http.StatusInternalServerError, opaqueMessage
}

// writeOpError writes the response for an error returned by a Trove operation.
func (h *Handler) writeOpError(w http.ResponseWriter, r *http.Request, op string, err error) {
	status, msg := h.classify(r, op, err)
	writeError(w, status, msg)
}

// writeOpStatus is writeOpError for responses that carry no body, so the
// classification and the logging still happen for HEAD.
func (h *Handler) writeOpStatus(w http.ResponseWriter, r *http.Request, op string, err error) {
	status, _ := h.classify(r, op, err)
	w.WriteHeader(status)
}
