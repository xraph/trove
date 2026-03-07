package hooks

import (
	"net/http"

	log "github.com/xraph/go-utils/log"

	"github.com/xraph/forge"
	"github.com/xraph/vessel"
	"github.com/xraph/warden"
)

// WardenHook provides authorization middleware using Warden.
type WardenHook struct {
	engine *warden.Engine
	logger log.Logger
}

// NewWardenHook creates a Warden hook, auto-discovering the engine from DI.
// Returns nil if Warden is not available.
func NewWardenHook(fapp forge.App, logger log.Logger) *WardenHook {
	eng, err := vessel.Inject[*warden.Engine](fapp.Container())
	if err != nil {
		if logger != nil {
			logger.Debug("warden not available, skipping access control")
		}
		return nil
	}

	return &WardenHook{
		engine: eng,
		logger: logger,
	}
}

// Resource types for Trove operations.
const (
	ResourceBucket = "trove:bucket"
	ResourceObject = "trove:object"
	ResourceUpload = "trove:upload"
	ResourceStream = "trove:stream"
	ResourceCAS    = "trove:cas"
)

// Actions for Trove operations.
const (
	ActionCreate  = "create"
	ActionRead    = "read"
	ActionUpdate  = "update"
	ActionDelete  = "delete"
	ActionList    = "list"
	ActionPresign = "presign"
	ActionStream  = "stream"
)

// Middleware returns an HTTP middleware that checks authorization.
func (h *WardenHook) Middleware() func(http.Handler) http.Handler {
	if h == nil {
		return func(next http.Handler) http.Handler { return next }
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resource := resourceFromRequest(r)
			action := actionFromMethod(r.Method)

			result, err := h.engine.Check(r.Context(), &warden.CheckRequest{
				Subject:  warden.Subject{ID: subjectFromContext(r)},
				Action:   warden.Action{Name: action},
				Resource: warden.Resource{Type: resource},
			})
			if err != nil {
				if h.logger != nil {
					h.logger.Warn("warden check failed", log.Any("error", err))
				}
				http.Error(w, "authorization check failed", http.StatusInternalServerError)
				return
			}

			if !result.Allowed {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func actionFromMethod(method string) string {
	switch method {
	case http.MethodGet, http.MethodHead:
		return ActionRead
	case http.MethodPut, http.MethodPost:
		return ActionCreate
	case http.MethodDelete:
		return ActionDelete
	default:
		return ActionRead
	}
}

func resourceFromRequest(r *http.Request) string {
	// Simplified resource detection from URL path.
	path := r.URL.Path
	switch {
	case contains(path, "/cas/"):
		return ResourceCAS
	case contains(path, "/uploads/"):
		return ResourceUpload
	case contains(path, "/objects/"):
		return ResourceObject
	case contains(path, "/buckets/"):
		return ResourceBucket
	default:
		return ResourceObject
	}
}

func subjectFromContext(r *http.Request) string {
	// Extract subject from context (set by auth middleware).
	if sub := r.Header.Get("X-Subject-ID"); sub != "" {
		return sub
	}
	return "anonymous"
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
