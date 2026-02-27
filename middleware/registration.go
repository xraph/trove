package middleware

// Registration binds a middleware to a scope and optional direction override.
type Registration struct {
	// Middleware is the middleware instance.
	Middleware Middleware

	// Scope determines when this middleware is active.
	// When nil, defaults to ScopeGlobal.
	Scope Scope

	// Direction overrides the middleware's own Direction() when non-zero.
	// This allows registering a bidirectional middleware for only reads or writes.
	Direction Direction

	// Priority controls execution order. Lower values run first.
	// Default is 0. Use negative values to run before default priority.
	Priority int
}

// effectiveDirection returns the direction, preferring the registration override.
func (r *Registration) effectiveDirection() Direction {
	if r.Direction != 0 {
		return r.Direction
	}
	return r.Middleware.Direction()
}

// effectiveScope returns the scope, defaulting to ScopeGlobal.
func (r *Registration) effectiveScope() Scope {
	if r.Scope != nil {
		return r.Scope
	}
	return ScopeGlobal{}
}
