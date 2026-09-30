package contract

import (
	"context"

	"github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/trove/middleware"
)

type middlewareListInput struct {
	Store  string `json:"store"`
	Bucket string `json:"bucket"`
	Key    string `json:"key"`
}

type middlewareRegistration struct {
	middlewareRow
	MatchesWrite *bool `json:"matchesWrite"`
	MatchesRead  *bool `json:"matchesRead"`
}

type middlewareWarning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type middlewareListOutput struct {
	Registrations []middlewareRegistration `json:"registrations"`
	Warnings      []middlewareWarning      `json:"warnings"`
}

func middlewareListHandler(deps Deps) func(context.Context, middlewareListInput, contract.Principal) (middlewareListOutput, error) {
	return func(ctx context.Context, in middlewareListInput, _ contract.Principal) (middlewareListOutput, error) {
		st, err := deps.Stores.Resolve(in.Store)
		if err != nil {
			return middlewareListOutput{}, err
		}
		test := in.Bucket != "" && in.Key != ""

		out := middlewareListOutput{Registrations: []middlewareRegistration{}, Warnings: []middlewareWarning{}}
		names := map[string]bool{}
		contentTypeScope, customScope := false, false
		for _, r := range ordered(st.Trove) {
			row := middlewareRegistration{middlewareRow: rowOf(r)}
			names[row.Name] = true
			if scopeHas(scopeOf(r), func(s middleware.Scope) bool {
				_, ok := s.(*middleware.ScopeContentType)
				return ok
			}) {
				contentTypeScope = true
			}
			if scopeHas(scopeOf(r), func(s middleware.Scope) bool {
				_, ok := s.(*middleware.ScopeFunc)
				return ok
			}) {
				customScope = true
			}
			if test {
				match := scopeOf(r).Match(ctx, in.Bucket, in.Key)
				w := match && runs(r, middleware.DirectionWrite)
				rd := match && runs(r, middleware.DirectionRead)
				row.MatchesWrite, row.MatchesRead = &w, &rd
			}
			out.Registrations = append(out.Registrations, row)
		}

		if names["compress"] && names["encrypt"] {
			out.Warnings = append(out.Warnings, middlewareWarning{
				Code:    "read-order",
				Message: "Reads run middleware in the same order as writes instead of reversed. With compress and encrypt both registered, a download can return compressed bytes without an error.",
			})
		}
		if contentTypeScope {
			out.Warnings = append(out.Warnings, middlewareWarning{
				Code:    "content-type-scope",
				Message: "A content-type scope matches the key's file extension, not the object's content type, and only for type/* entries.",
			})
		}
		if customScope {
			out.Warnings = append(out.Warnings, middlewareWarning{
				Code:    "cached-custom-scope",
				Message: "A custom scope is evaluated once per bucket and key and then cached, so it cannot depend on who is asking.",
			})
		}
		if len(out.Registrations) > 0 {
			out.Warnings = append(out.Warnings, middlewareWarning{
				Code:    "bypass",
				Message: "CAS, copy and streams move stored bytes without running any middleware.",
			})
		}
		return out, nil
	}
}
