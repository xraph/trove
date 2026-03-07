// Package hooks provides ecosystem integration hooks for Trove.
package hooks

import (
	"context"
	"fmt"

	log "github.com/xraph/go-utils/log"

	"github.com/xraph/chronicle"
	"github.com/xraph/forge"
	"github.com/xraph/vessel"
)

// ChronicleHook emits audit events for storage operations via Chronicle.
type ChronicleHook struct {
	emitter chronicle.Emitter
	logger  log.Logger
}

// NewChronicleHook creates a Chronicle hook, auto-discovering the emitter from DI.
// Returns nil if Chronicle is not available.
func NewChronicleHook(fapp forge.App, logger log.Logger) *ChronicleHook {
	emitter, err := vessel.Inject[chronicle.Emitter](fapp.Container())
	if err != nil {
		if logger != nil {
			logger.Debug("chronicle not available, skipping audit hook")
		}
		return nil
	}

	return &ChronicleHook{
		emitter: emitter,
		logger:  logger,
	}
}

// Event names for storage operations.
const (
	EventObjectCreated  = "trove.object.created"
	EventObjectRead     = "trove.object.read"
	EventObjectDeleted  = "trove.object.deleted"
	EventObjectCopied   = "trove.object.copied"
	EventBucketCreated  = "trove.bucket.created"
	EventBucketDeleted  = "trove.bucket.deleted"
	EventUploadStarted  = "trove.upload.initiated"
	EventUploadComplete = "trove.upload.completed"
	EventUploadAborted  = "trove.upload.aborted"
	EventCASStored      = "trove.cas.stored"
	EventCASGC          = "trove.cas.gc"
)

// RecordObjectCreated emits an object creation event.
func (h *ChronicleHook) RecordObjectCreated(ctx context.Context, bucket, key string, size int64) {
	if h == nil {
		return
	}
	h.record(ctx, EventObjectCreated, "object", fmt.Sprintf("%s/%s", bucket, key), map[string]any{
		"bucket": bucket,
		"key":    key,
		"size":   size,
	})
}

// RecordObjectRead emits an object read event.
func (h *ChronicleHook) RecordObjectRead(ctx context.Context, bucket, key string) {
	if h == nil {
		return
	}
	h.record(ctx, EventObjectRead, "object", fmt.Sprintf("%s/%s", bucket, key), map[string]any{
		"bucket": bucket,
		"key":    key,
	})
}

// RecordObjectDeleted emits an object deletion event.
func (h *ChronicleHook) RecordObjectDeleted(ctx context.Context, bucket, key string) {
	if h == nil {
		return
	}
	h.record(ctx, EventObjectDeleted, "object", fmt.Sprintf("%s/%s", bucket, key), map[string]any{
		"bucket": bucket,
		"key":    key,
	})
}

// RecordBucketCreated emits a bucket creation event.
func (h *ChronicleHook) RecordBucketCreated(ctx context.Context, name string) {
	if h == nil {
		return
	}
	h.record(ctx, EventBucketCreated, "bucket", name, nil)
}

// RecordBucketDeleted emits a bucket deletion event.
func (h *ChronicleHook) RecordBucketDeleted(ctx context.Context, name string) {
	if h == nil {
		return
	}
	h.record(ctx, EventBucketDeleted, "bucket", name, nil)
}

// RecordCASStored emits a CAS store event.
func (h *ChronicleHook) RecordCASStored(ctx context.Context, hash string, size int64, deduplicated bool) {
	if h == nil {
		return
	}
	h.record(ctx, EventCASStored, "cas", hash, map[string]any{
		"size":         size,
		"deduplicated": deduplicated,
	})
}

// RecordCASGC emits a CAS garbage collection event.
func (h *ChronicleHook) RecordCASGC(ctx context.Context, scanned, deleted int, freedBytes int64) {
	if h == nil {
		return
	}
	h.record(ctx, EventCASGC, "cas", "gc", map[string]any{
		"scanned":     scanned,
		"deleted":     deleted,
		"freed_bytes": freedBytes,
	})
}

func (h *ChronicleHook) record(ctx context.Context, action, resource, resourceID string, meta map[string]any) {
	builder := h.emitter.Info(ctx, action, resource, resourceID)
	if builder == nil {
		return
	}
	if err := builder.Record(); err != nil && h.logger != nil {
		h.logger.Warn("failed to record chronicle event",
			log.String("action", action),
			log.Any("error", err),
		)
	}
}
