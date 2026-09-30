package contract

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/middleware"
)

// Handler serves object content for tickets minted by the content intents.
// It authorises by ticket signature alone, which is what lets a plain
// download link work, and it never serves content inline: an uploaded HTML
// or SVG file would otherwise run on the dashboard's origin as the
// operator.
func (c *Content) Handler(stores *Stores, logger forge.Logger) http.Handler {
	return &contentHandler{content: c, stores: stores, logger: logger}
}

type contentHandler struct {
	content *Content
	stores  *Stores
	logger  forge.Logger
}

func (h *contentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		h.serveGet(w, r)
	case http.MethodPut:
		h.servePut(w, r)
	default:
		w.Header().Set("Allow", "GET, PUT")
		h.writeContentError(w, http.StatusMethodNotAllowed, "only GET and PUT are allowed")
	}
}

func (h *contentHandler) ticket(w http.ResponseWriter, r *http.Request, ops ...string) (Ticket, *Store, bool) {
	tk, err := h.content.Signer.Verify(r.URL.Query().Get("t"))
	switch {
	case errors.Is(err, ErrTicketExpired):
		h.writeContentError(w, http.StatusForbidden, "This link has expired. Request a new one.")
		return Ticket{}, nil, false
	case err != nil:
		h.writeContentError(w, http.StatusForbidden, "This link is not valid.")
		return Ticket{}, nil, false
	}
	allowed := false
	for _, op := range ops {
		if tk.Op == op {
			allowed = true
		}
	}
	if !allowed {
		h.writeContentError(w, http.StatusForbidden, "This link is not valid for this request.")
		return Ticket{}, nil, false
	}
	st, err := h.stores.Resolve(tk.Store)
	if err != nil {
		h.writeContentError(w, http.StatusNotFound, "This store no longer exists.")
		return Ticket{}, nil, false
	}
	return tk, st, true
}

func (h *contentHandler) serveGet(w http.ResponseWriter, r *http.Request) {
	tk, st, ok := h.ticket(w, r, OpDownload, OpPreview)
	if !ok {
		return
	}
	fields := h.fields(tk, st.Name)
	obj, err := st.Trove.Get(r.Context(), tk.Bucket, tk.Key)
	if err != nil {
		h.writeMapped(w, err, fields)
		return
	}
	defer obj.Close()

	hdr := w.Header()
	hdr.Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(path.Base(tk.Key)))
	hdr.Set("X-Content-Type-Options", "nosniff")
	hdr.Set("Content-Security-Policy", "sandbox")
	hdr.Set("Cache-Control", "no-store")
	ct := "application/octet-stream"
	if obj.Info != nil && obj.Info.ContentType != "" {
		ct = obj.Info.ContentType
	}
	hdr.Set("Content-Type", ct)

	// src sees every read error before io.Copy does, so a failure on the
	// storage side can be told apart from a client that went away.
	src := &recordingReader{r: obj}
	var body io.Reader = src
	if tk.Op == OpPreview {
		body = io.LimitReader(src, tk.Limit)
	} else if obj.Info != nil && len(matching(r.Context(), st.Trove, tk.Bucket, tk.Key, middleware.DirectionRead)) == 0 {
		// The stored size is what is served only when nothing transforms
		// it on the way out.
		hdr.Set("Content-Length", strconv.FormatInt(obj.Info.Size, 10))
	}
	w.WriteHeader(http.StatusOK)
	_, copyErr := io.Copy(w, body)
	if copyErr == nil {
		return
	}
	if src.err == nil || r.Context().Err() != nil {
		// The client stopped reading. Nothing is wrong on this side.
		h.debug("trove/contract: content download abandoned by the client",
			append(fields, forge.F("error", copyErr))...)
		return
	}
	// The status line and some of the body are already sent. Returning
	// normally would end a chunked response cleanly and the browser would
	// save a truncated file as a whole one, so drop the connection instead.
	h.errorLog("trove/contract: content download failed mid-stream",
		append(fields, forge.F("error", src.err))...)
	panic(http.ErrAbortHandler)
}

// recordingReader remembers the first error its source returned, other than
// the end of the stream.
type recordingReader struct {
	r   io.Reader
	err error
}

func (rr *recordingReader) Read(p []byte) (int, error) {
	n, err := rr.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) && rr.err == nil {
		rr.err = err
	}
	return n, err
}

func (h *contentHandler) servePut(w http.ResponseWriter, r *http.Request) {
	tk, st, ok := h.ticket(w, r, OpUpload)
	if !ok {
		return
	}
	fields := h.fields(tk, st.Name)
	if r.ContentLength > tk.Size {
		h.writeContentError(w, http.StatusRequestEntityTooLarge, "This body is larger than the upload was declared.", fields...)
		return
	}
	// Always look, even when the ticket allows overwriting: the Head is
	// what fails on a bucket that does not exist or a key the driver
	// refuses, and a driver's Put may create the bucket's directory
	// instead of failing.
	if _, err := st.Trove.Head(r.Context(), tk.Bucket, tk.Key); err == nil {
		if !tk.Overwrite {
			h.writeContentError(w, http.StatusConflict, "An object with this key already exists.", fields...)
			return
		}
	} else if !errors.Is(err, driver.ErrObjectNotFound) {
		h.writeMapped(w, err, fields)
		return
	}
	body := http.MaxBytesReader(w, r.Body, tk.Size)
	var opts []driver.PutOption
	if tk.ContentType != "" {
		opts = append(opts, driver.WithContentType(tk.ContentType))
	}
	info, err := st.Trove.Put(r.Context(), tk.Bucket, tk.Key, body, opts...)
	if err != nil {
		var tooLarge *http.MaxBytesError
		switch {
		case errors.As(err, &tooLarge):
			h.writeContentError(w, http.StatusRequestEntityTooLarge, "This body is larger than the upload was declared.", fields...)
		case errors.Is(err, trove.ErrContentBlocked):
			h.writeContentError(w, http.StatusUnprocessableEntity, "A content scan blocked this upload.", fields...)
		default:
			h.writeMapped(w, err, fields)
		}
		return
	}
	h.info("trove/contract: content upload stored", append(fields, forge.F("storedSize", info.Size))...)
	h.writeJSON(w, http.StatusOK, map[string]any{"key": tk.Key, "storedSize": info.Size, "etag": optString(info.ETag)}, fields...)
}

// writeMapped answers with the HTTP status for err's contract code.
func (h *contentHandler) writeMapped(w http.ResponseWriter, err error, fields []forge.Field) {
	mapped := mapError(err)
	var ce *contract.Error
	status, msg := http.StatusInternalServerError, "an internal error occurred"
	if errors.As(mapped, &ce) {
		msg = ce.Message
		switch ce.Code {
		case contract.CodeNotFound:
			status = http.StatusNotFound
		case contract.CodeBadRequest:
			status = http.StatusBadRequest
		case contract.CodeConflict:
			status = http.StatusConflict
		case contract.CodePermissionDenied:
			status = http.StatusForbidden
		case contract.CodeUnavailable:
			status = http.StatusServiceUnavailable
		}
	}
	if status == http.StatusInternalServerError {
		h.errorLog("trove/contract: content request failed", append(fields, forge.F("error", err))...)
	}
	h.writeContentError(w, status, msg, fields...)
}

func (h *contentHandler) writeContentError(w http.ResponseWriter, status int, msg string, fields ...forge.Field) {
	w.Header().Set("Cache-Control", "no-store")
	h.writeJSON(w, status, map[string]string{"error": msg}, fields...)
}

// writeJSON sends v as the whole response. The status line is already gone
// if the encode fails, so the log is all that is left to do with it.
func (h *contentHandler) writeJSON(w http.ResponseWriter, status int, v any, fields ...forge.Field) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		h.errorLog("trove/contract: content response not written", append(fields, forge.F("error", err))...)
	}
}

// fields is what every log line about a ticketed request carries. The
// subject is there to say who asked, never to authorise anything.
func (h *contentHandler) fields(tk Ticket, store string) []forge.Field {
	return []forge.Field{
		forge.F("subject", tk.Subject),
		forge.F("op", tk.Op),
		forge.F("store", store),
		forge.F("bucket", tk.Bucket),
		forge.F("key", tk.Key),
	}
}

func (h *contentHandler) info(msg string, fields ...forge.Field) {
	if h.logger != nil {
		h.logger.Info(msg, fields...)
	}
}

func (h *contentHandler) debug(msg string, fields ...forge.Field) {
	if h.logger != nil {
		h.logger.Debug(msg, fields...)
	}
}

func (h *contentHandler) errorLog(msg string, fields ...forge.Field) {
	if h.logger != nil {
		h.logger.Error(msg, fields...)
	}
}
