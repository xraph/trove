package trove

import (
	"github.com/xraph/trove/cas"
	"github.com/xraph/trove/middleware"
)

// WithMiddleware registers global middleware (read + write, all operations).
func WithMiddleware(mws ...middleware.Middleware) Option {
	return func(t *Trove) error {
		for _, mw := range mws {
			t.resolver.Register(middleware.Registration{
				Middleware: mw,
				Scope:      middleware.ScopeGlobal{},
			})
		}
		return nil
	}
}

// WithReadMiddleware registers middleware that only runs on the download/read path.
func WithReadMiddleware(mws ...middleware.Middleware) Option {
	return func(t *Trove) error {
		for _, mw := range mws {
			t.resolver.Register(middleware.Registration{
				Middleware: mw,
				Scope:      middleware.ScopeGlobal{},
				Direction:  middleware.DirectionRead,
			})
		}
		return nil
	}
}

// WithWriteMiddleware registers middleware that only runs on the upload/write path.
func WithWriteMiddleware(mws ...middleware.Middleware) Option {
	return func(t *Trove) error {
		for _, mw := range mws {
			t.resolver.Register(middleware.Registration{
				Middleware: mw,
				Scope:      middleware.ScopeGlobal{},
				Direction:  middleware.DirectionWrite,
			})
		}
		return nil
	}
}

// WithScopedMiddleware registers middleware with a composed scope (read + write).
func WithScopedMiddleware(scope middleware.Scope, mws ...middleware.Middleware) Option {
	return func(t *Trove) error {
		for _, mw := range mws {
			t.resolver.Register(middleware.Registration{
				Middleware: mw,
				Scope:      scope,
			})
		}
		return nil
	}
}

// WithScopedReadMiddleware registers read-only middleware with a composed scope.
func WithScopedReadMiddleware(scope middleware.Scope, mws ...middleware.Middleware) Option {
	return func(t *Trove) error {
		for _, mw := range mws {
			t.resolver.Register(middleware.Registration{
				Middleware: mw,
				Scope:      scope,
				Direction:  middleware.DirectionRead,
			})
		}
		return nil
	}
}

// WithScopedWriteMiddleware registers write-only middleware with a composed scope.
func WithScopedWriteMiddleware(scope middleware.Scope, mws ...middleware.Middleware) Option {
	return func(t *Trove) error {
		for _, mw := range mws {
			t.resolver.Register(middleware.Registration{
				Middleware: mw,
				Scope:      scope,
				Direction:  middleware.DirectionWrite,
			})
		}
		return nil
	}
}

// WithMiddlewareAt registers middleware with explicit priority (lower = runs first).
func WithMiddlewareAt(priority int, mws ...middleware.Middleware) Option {
	return func(t *Trove) error {
		for _, mw := range mws {
			t.resolver.Register(middleware.Registration{
				Middleware: mw,
				Scope:      middleware.ScopeGlobal{},
				Priority:   priority,
			})
		}
		return nil
	}
}

// WithCAS enables content-addressable storage with the given hash algorithm.
func WithCAS(alg cas.HashAlgorithm) Option {
	return func(t *Trove) error {
		t.cas = cas.New(t.driver,
			cas.WithAlgorithm(alg),
		)
		return nil
	}
}
