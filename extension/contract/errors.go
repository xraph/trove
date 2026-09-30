package contract

import (
	"context"
	"errors"
	"strings"

	"github.com/xraph/forge"
	"github.com/xraph/forge/extensions/dashboard/contract"

	"github.com/xraph/trove"
	"github.com/xraph/trove/cas"
	"github.com/xraph/trove/driver"
)

// mapError turns a Trove or driver error into a *contract.Error the client
// can branch on. Anything unrecognised becomes CodeInternal with a generic
// message: a driver error can carry a host, a bucket URL or a credential,
// so its text never reaches the client. Handlers call it through
// Deps.mapError, which logs the CodeInternal case.
func mapError(err error) error {
	if err == nil {
		return nil
	}
	var ce *contract.Error
	switch {
	case errors.As(err, &ce):
		return ce
	case errors.Is(err, driver.ErrBucketNotFound):
		return notFound("bucket not found")
	case errors.Is(err, driver.ErrObjectNotFound):
		return notFound("object not found")
	case errors.Is(err, cas.ErrNotFound):
		return notFound("this hash is not in the CAS index. The index is kept in memory and forgets every entry on restart.")
	case errors.Is(err, driver.ErrNotFound):
		return notFound("not found")
	case errors.Is(err, driver.ErrBucketExists):
		return conflict("a bucket with this name already exists")
	case errors.Is(err, trove.ErrContentBlocked):
		return badRequest("a content scan blocked this upload")
	case errors.Is(err, driver.ErrPermissionDenied):
		return &contract.Error{Code: contract.CodePermissionDenied, Message: "the storage backend refused this operation"}
	case errors.Is(err, driver.ErrQuotaExceeded):
		return &contract.Error{Code: contract.CodeUnavailable, Message: "the storage backend is rate limiting or out of quota", Retryable: true}
	case errors.Is(err, driver.ErrInvalidPath):
		return badRequest("the key is not a valid path for this driver")
	case errors.Is(err, trove.ErrKeyEmpty):
		return badRequest("key is required")
	case errors.Is(err, trove.ErrBucketEmpty):
		return badRequest("bucket is required")
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		// A page that navigates away cancels its requests. That is
		// routine, so it is retryable and never logged as an error.
		return &contract.Error{Code: contract.CodeUnavailable, Message: "the request was cancelled or timed out", Retryable: true}
	default:
		return &contract.Error{Code: contract.CodeInternal, Message: "an internal error occurred"}
	}
}

// mapError maps err and, for CodeInternal with a logger set, logs the real
// error with the intent that hit it. That is the one case an operator
// cannot diagnose from what the client sees.
func (d Deps) mapError(intent string, err error) error {
	mapped := mapError(err)
	if d.Logger == nil || mapped == nil {
		return mapped
	}
	var ce *contract.Error
	if errors.As(mapped, &ce) && ce.Code == contract.CodeInternal {
		d.Logger.Error("trove/contract: internal error answering intent",
			forge.F("intent", intent),
			forge.F("error", err),
		)
	}
	return mapped
}

func badRequest(msg string) error {
	return &contract.Error{Code: contract.CodeBadRequest, Message: msg}
}

func conflict(msg string) error {
	return &contract.Error{Code: contract.CodeConflict, Message: msg}
}

func notFound(msg string) error {
	return &contract.Error{Code: contract.CodeNotFound, Message: msg}
}

func unavailable(msg string) error {
	return &contract.Error{Code: contract.CodeUnavailable, Message: msg}
}

// requireName checks a bucket or store name is present. It does not trim
// what it returns: the caller uses the value as sent. Object keys do not go
// through here, because a key made of spaces is a valid key.
func requireName(field, value string) error {
	if strings.TrimSpace(value) == "" {
		return badRequest(field + " is required")
	}
	return nil
}
