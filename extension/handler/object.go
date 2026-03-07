package handler

import (
	"fmt"
	"io"
	"net/http"
	"strconv"

	log "github.com/xraph/go-utils/log"

	"github.com/xraph/trove/driver"
)

func (h *Handler) putObject(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	key := r.PathValue("key")

	if bucket == "" || key == "" {
		writeError(w, http.StatusBadRequest, "bucket and key are required")
		return
	}

	info, err := h.trove.Put(r.Context(), bucket, key, r.Body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, info)
}

func (h *Handler) getObject(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	key := r.PathValue("key")

	obj, err := h.trove.Get(r.Context(), bucket, key)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	defer obj.Close()

	if obj.Info != nil {
		if obj.Info.ContentType != "" {
			w.Header().Set("Content-Type", obj.Info.ContentType)
		}
		if obj.Info.ETag != "" {
			w.Header().Set("ETag", obj.Info.ETag)
		}
		if obj.Info.Size > 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(obj.Info.Size, 10))
		}
	}

	w.WriteHeader(http.StatusOK)
	if _, err := io.Copy(w, obj); err != nil {
		h.logger.Error("failed to stream object", log.Error(err))
	}
}

func (h *Handler) deleteObject(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	key := r.PathValue("key")

	if err := h.trove.Delete(r.Context(), bucket, key); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) headObject(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	key := r.PathValue("key")

	info, err := h.trove.Head(r.Context(), bucket, key)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", info.ContentType)
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size, 10))
	if info.ETag != "" {
		w.Header().Set("ETag", info.ETag)
	}
	w.WriteHeader(http.StatusOK)
}

func (h *Handler) listObjects(w http.ResponseWriter, r *http.Request) {
	bucket := r.PathValue("bucket")
	prefix := r.URL.Query().Get("prefix")
	delimiter := r.URL.Query().Get("delimiter")

	var opts []driver.ListOption
	if prefix != "" {
		opts = append(opts, driver.WithPrefix(prefix))
	}
	if delimiter != "" {
		opts = append(opts, driver.WithDelimiter(delimiter))
	}

	iter, err := h.trove.List(r.Context(), bucket, opts...)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	defer iter.Close()

	objects, err := iter.All(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("list objects: %v", err))
		return
	}

	writeJSON(w, http.StatusOK, objects)
}
