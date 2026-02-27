package hooks

import (
	"context"
	"log/slog"
	"time"

	"github.com/xraph/dispatch/engine"
	"github.com/xraph/dispatch/job"
	"github.com/xraph/forge"
	"github.com/xraph/vessel"

	"github.com/xraph/trove"
)

// DispatchHook registers background jobs with Dispatch.
type DispatchHook struct {
	eng    *engine.Engine
	trove  *trove.Trove
	logger *slog.Logger
}

// NewDispatchHook creates a Dispatch hook, auto-discovering the engine from DI.
// Returns nil if Dispatch is not available.
func NewDispatchHook(fapp forge.App, t *trove.Trove, logger *slog.Logger) *DispatchHook {
	eng, err := vessel.Inject[*engine.Engine](fapp.Container())
	if err != nil {
		if logger != nil {
			logger.Debug("dispatch not available, skipping job registration")
		}
		return nil
	}

	return &DispatchHook{
		eng:    eng,
		trove:  t,
		logger: logger,
	}
}

// RegisterJobs registers all Trove background jobs.
func (h *DispatchHook) RegisterJobs(ctx context.Context) {
	if h == nil {
		return
	}

	// Cleanup expired upload sessions.
	engine.Register(h.eng, &job.Definition[struct{}]{
		Name: "trove.cleanup-expired-uploads",
		Handler: func(ctx context.Context, _ struct{}) error {
			return h.cleanupExpiredUploads(ctx)
		},
	})

	// CAS garbage collection.
	engine.Register(h.eng, &job.Definition[struct{}]{
		Name: "trove.cas-gc",
		Handler: func(ctx context.Context, _ struct{}) error {
			return h.casGC(ctx)
		},
	})

	if h.logger != nil {
		h.logger.Info("registered trove dispatch jobs",
			"jobs", []string{"trove.cleanup-expired-uploads", "trove.cas-gc"},
		)
	}
}

func (h *DispatchHook) cleanupExpiredUploads(ctx context.Context) error {
	// This would use the store to list and clean up expired uploads.
	// Placeholder for now — requires store access.
	if h.logger != nil {
		h.logger.Info("running expired upload cleanup",
			"time", time.Now().UTC(),
		)
	}
	return nil
}

func (h *DispatchHook) casGC(ctx context.Context) error {
	casEngine := h.trove.CAS()
	if casEngine == nil {
		return nil
	}

	result, err := casEngine.GC(ctx)
	if err != nil {
		return err
	}

	if h.logger != nil {
		h.logger.Info("CAS garbage collection completed",
			"scanned", result.Scanned,
			"deleted", result.Deleted,
			"freed_bytes", result.FreedBytes,
			"errors", result.Errors,
		)
	}
	return nil
}
