package middleware

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
)

// Scope determines whether a middleware should be active for a given operation.
// Scopes are evaluated at runtime against the request context, bucket, and key.
type Scope interface {
	// Match returns true if the middleware should be active.
	Match(ctx context.Context, bucket, key string) bool

	// String returns a human-readable description for debugging.
	String() string
}

// --- Built-in Scopes ---

// ScopeGlobal matches every operation. This is the default when no scope is set.
type ScopeGlobal struct{}

// Match always returns true.
func (ScopeGlobal) Match(_ context.Context, _, _ string) bool { return true }

// String returns the scope description.
func (ScopeGlobal) String() string { return "global" }

// ScopeBucket matches operations on specific bucket(s).
type ScopeBucket struct {
	Buckets []string
}

// Match returns true if the bucket matches any of the configured buckets.
func (s *ScopeBucket) Match(_ context.Context, bucket, _ string) bool {
	for _, b := range s.Buckets {
		if b == bucket {
			return true
		}
	}
	return false
}

// String returns the scope description.
func (s *ScopeBucket) String() string {
	return fmt.Sprintf("bucket(%s)", strings.Join(s.Buckets, ","))
}

// ScopeKeyPattern matches operations on keys matching glob patterns.
type ScopeKeyPattern struct {
	Patterns []string
}

// Match returns true if the key matches any of the configured patterns.
func (s *ScopeKeyPattern) Match(_ context.Context, _, key string) bool {
	for _, p := range s.Patterns {
		matched, err := filepath.Match(p, key)
		if err == nil && matched {
			return true
		}
	}
	return false
}

// String returns the scope description.
func (s *ScopeKeyPattern) String() string {
	return fmt.Sprintf("key(%s)", strings.Join(s.Patterns, ","))
}

// ScopeContentType matches by the object's content type prefix.
type ScopeContentType struct {
	Types []string
}

// Match checks the key extension against configured content types.
// For full content-type matching, use ScopeFunc with context-based lookup.
func (s *ScopeContentType) Match(_ context.Context, _, key string) bool {
	ext := filepath.Ext(key)
	for _, t := range s.Types {
		// Support wildcard prefix matching like "image/*"
		if strings.HasSuffix(t, "/*") {
			prefix := strings.TrimSuffix(t, "/*")
			if strings.HasPrefix(ext, "."+prefix) {
				return true
			}
		}
	}
	return false
}

// String returns the scope description.
func (s *ScopeContentType) String() string {
	return fmt.Sprintf("content-type(%s)", strings.Join(s.Types, ","))
}

// ScopeFunc is an arbitrary predicate for custom runtime evaluation.
// Use this for tenant scoping, feature flags, or any runtime condition.
type ScopeFunc struct {
	Fn   func(ctx context.Context, bucket, key string) bool
	Desc string
}

// Match delegates to the predicate function.
func (s *ScopeFunc) Match(ctx context.Context, bucket, key string) bool {
	return s.Fn(ctx, bucket, key)
}

// String returns the scope description.
func (s *ScopeFunc) String() string {
	if s.Desc != "" {
		return s.Desc
	}
	return "func"
}

// --- Boolean Combinators ---

// ScopeAnd requires ALL child scopes to match.
type ScopeAnd struct {
	Scopes []Scope
}

// Match returns true only if all child scopes match.
func (s *ScopeAnd) Match(ctx context.Context, bucket, key string) bool {
	for _, child := range s.Scopes {
		if !child.Match(ctx, bucket, key) {
			return false
		}
	}
	return true
}

// String returns the scope description.
func (s *ScopeAnd) String() string {
	parts := make([]string, len(s.Scopes))
	for i, child := range s.Scopes {
		parts[i] = child.String()
	}
	return fmt.Sprintf("and(%s)", strings.Join(parts, ","))
}

// ScopeOr requires ANY child scope to match.
type ScopeOr struct {
	Scopes []Scope
}

// Match returns true if any child scope matches.
func (s *ScopeOr) Match(ctx context.Context, bucket, key string) bool {
	for _, child := range s.Scopes {
		if child.Match(ctx, bucket, key) {
			return true
		}
	}
	return false
}

// String returns the scope description.
func (s *ScopeOr) String() string {
	parts := make([]string, len(s.Scopes))
	for i, child := range s.Scopes {
		parts[i] = child.String()
	}
	return fmt.Sprintf("or(%s)", strings.Join(parts, ","))
}

// ScopeNot inverts a scope.
type ScopeNot struct {
	Inner Scope
}

// Match returns the inverse of the inner scope.
func (s *ScopeNot) Match(ctx context.Context, bucket, key string) bool {
	return !s.Inner.Match(ctx, bucket, key)
}

// String returns the scope description.
func (s *ScopeNot) String() string {
	return fmt.Sprintf("not(%s)", s.Inner.String())
}
