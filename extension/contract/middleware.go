package contract

import (
	"context"
	"reflect"
	"sort"

	"github.com/xraph/trove"
	"github.com/xraph/trove/middleware"
)

// middlewareRow is one middleware registration as the dashboard sees it.
// Trove keeps each middleware's own settings unexported, so this is all
// there is to show.
type middlewareRow struct {
	Name      string `json:"name"`
	Direction string `json:"direction"`
	Scope     string `json:"scope"`
	Priority  int    `json:"priority"`
}

func effectiveDirection(r middleware.Registration) middleware.Direction {
	if r.Direction != 0 {
		return r.Direction
	}
	return r.Middleware.Direction()
}

func scopeOf(r middleware.Registration) middleware.Scope {
	if r.Scope == nil {
		return middleware.ScopeGlobal{}
	}
	return r.Scope
}

// runs reports whether r takes part in direction dir: its effective
// direction covers it and the middleware implements the matching interface,
// which is what the resolver requires before it puts a middleware in a
// pipeline.
func runs(r middleware.Registration, dir middleware.Direction) bool {
	eff := effectiveDirection(r)
	if dir&middleware.DirectionRead != 0 && eff&middleware.DirectionRead != 0 {
		if _, ok := r.Middleware.(middleware.ReadMiddleware); ok {
			return true
		}
	}
	if dir&middleware.DirectionWrite != 0 && eff&middleware.DirectionWrite != 0 {
		if _, ok := r.Middleware.(middleware.WriteMiddleware); ok {
			return true
		}
	}
	return false
}

func rowOf(r middleware.Registration) middlewareRow {
	return middlewareRow{
		Name:      r.Middleware.Name(),
		Direction: effectiveDirection(r).String(),
		Scope:     scopeOf(r).String(),
		Priority:  r.Priority,
	}
}

// ordered returns the registrations in the order Trove runs them: by
// priority, then in registration order.
func ordered(t *trove.Trove) []middleware.Registration {
	regs := t.Resolver().Registrations()
	sort.SliceStable(regs, func(i, j int) bool { return regs[i].Priority < regs[j].Priority })
	return regs
}

// scopeHas reports whether s, or any scope combined into it, satisfies
// pred. It inspects the scope's type rather than its String() text: a
// ScopeFunc built with WhenDesc prints whatever description its author
// chose, and a bucket can be named anything.
func scopeHas(s middleware.Scope, pred func(middleware.Scope) bool) bool {
	if pred(s) {
		return true
	}
	switch v := s.(type) {
	case *middleware.ScopeAnd:
		for _, c := range v.Scopes {
			if scopeHas(c, pred) {
				return true
			}
		}
	case *middleware.ScopeOr:
		for _, c := range v.Scopes {
			if scopeHas(c, pred) {
				return true
			}
		}
	case *middleware.ScopeNot:
		return scopeHas(v.Inner, pred)
	}
	return false
}

// matchingRegs returns the registrations that run for bucket and key in
// direction dir, in run order. matching builds its rows from it, so the two
// cannot disagree.
func matchingRegs(ctx context.Context, t *trove.Trove, bucket, key string, dir middleware.Direction) []middleware.Registration {
	regs := []middleware.Registration{}
	for _, r := range ordered(t) {
		if runs(r, dir) && scopeOf(r).Match(ctx, bucket, key) {
			regs = append(regs, r)
		}
	}
	return regs
}

// matching returns the registrations that run for bucket and key in
// direction dir, in run order. It evaluates scopes now, so it describes
// the current configuration, never how an existing object was written:
// Trove records nothing per object.
func matching(ctx context.Context, t *trove.Trove, bucket, key string, dir middleware.Direction) []middlewareRow {
	regs := matchingRegs(ctx, t, bucket, key, dir)
	rows := make([]middlewareRow, 0, len(regs))
	for _, r := range regs {
		rows = append(rows, rowOf(r))
	}
	return rows
}

// matchingAny returns every registration that runs for bucket and key in
// either direction, once each, in run order.
func matchingAny(ctx context.Context, t *trove.Trove, bucket, key string) []middlewareRow {
	return matching(ctx, t, bucket, key, middleware.DirectionReadWrite)
}

// sameMiddleware reports whether a and b are the same middleware instance.
// Two instances of one type can be configured differently (encrypt with two
// keys), so only pointer identity counts. A middleware that is not a pointer
// compares as different, which errs toward refusing.
func sameMiddleware(a, b middleware.Middleware) bool {
	va, vb := reflect.ValueOf(a), reflect.ValueOf(b)
	if va.Kind() != reflect.Pointer || vb.Kind() != reflect.Pointer || va.Type() != vb.Type() {
		return false
	}
	return va.Pointer() == vb.Pointer()
}

// samePipeline reports whether the same middleware instances run, in the
// same order, in both directions for the two keys.
func samePipeline(ctx context.Context, t *trove.Trove, srcBucket, srcKey, dstBucket, dstKey string) bool {
	for _, dir := range []middleware.Direction{middleware.DirectionWrite, middleware.DirectionRead} {
		a := matchingRegs(ctx, t, srcBucket, srcKey, dir)
		b := matchingRegs(ctx, t, dstBucket, dstKey, dir)
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if !sameMiddleware(a[i].Middleware, b[i].Middleware) {
				return false
			}
		}
	}
	return true
}
