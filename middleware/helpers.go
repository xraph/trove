package middleware

import "context"

// ForBuckets creates a scope matching exact bucket names.
func ForBuckets(buckets ...string) Scope {
	return &ScopeBucket{Buckets: buckets}
}

// ForKeys creates a scope matching glob patterns on object keys.
func ForKeys(patterns ...string) Scope {
	return &ScopeKeyPattern{Patterns: patterns}
}

// ForContentTypes creates a scope matching MIME type prefixes.
func ForContentTypes(types ...string) Scope {
	return &ScopeContentType{Types: types}
}

// When creates a scope from an arbitrary predicate function.
// Use this for tenant scoping, feature flags, or any runtime condition.
func When(fn func(ctx context.Context, bucket, key string) bool) Scope {
	return &ScopeFunc{Fn: fn, Desc: "custom"}
}

// WhenDesc creates a scope from a predicate with a description for debugging.
func WhenDesc(desc string, fn func(ctx context.Context, bucket, key string) bool) Scope {
	return &ScopeFunc{Fn: fn, Desc: desc}
}

// And combines scopes — all must match.
func And(scopes ...Scope) Scope {
	return &ScopeAnd{Scopes: scopes}
}

// Or combines scopes — any must match.
func Or(scopes ...Scope) Scope {
	return &ScopeOr{Scopes: scopes}
}

// Not inverts a scope.
func Not(scope Scope) Scope {
	return &ScopeNot{Inner: scope}
}
