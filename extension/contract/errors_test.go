package contract

import (
	"context"
	"errors"
	"fmt"
	"testing"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/go-utils/log"

	"github.com/xraph/trove"
	"github.com/xraph/trove/cas"
	"github.com/xraph/trove/driver"
)

func TestMapError_Sentinels(t *testing.T) {
	wrap := func(err error) error { return fmt.Errorf("somedriver: thing: %w", err) }
	cases := []struct {
		err  error
		want dashcontract.ErrorCode
	}{
		{wrap(driver.ErrObjectNotFound), dashcontract.CodeNotFound},
		{wrap(driver.ErrBucketNotFound), dashcontract.CodeNotFound},
		{wrap(cas.ErrNotFound), dashcontract.CodeNotFound},
		{wrap(driver.ErrNotFound), dashcontract.CodeNotFound},
		{wrap(driver.ErrBucketExists), dashcontract.CodeConflict},
		{wrap(trove.ErrContentBlocked), dashcontract.CodeBadRequest},
		{wrap(driver.ErrPermissionDenied), dashcontract.CodePermissionDenied},
		{wrap(driver.ErrQuotaExceeded), dashcontract.CodeUnavailable},
		{wrap(driver.ErrInvalidPath), dashcontract.CodeBadRequest},
		{trove.ErrKeyEmpty, dashcontract.CodeBadRequest},
		{trove.ErrBucketEmpty, dashcontract.CodeBadRequest},
		{errors.New("dial tcp 10.0.0.5:9000: connection refused"), dashcontract.CodeInternal},
	}
	for _, c := range cases {
		if got := codeOf(mapError(c.err)); got != c.want {
			t.Errorf("mapError(%v) = %s, want %s", c.err, got, c.want)
		}
	}
}

func TestMapError_InternalHidesTheCause(t *testing.T) {
	err := mapError(errors.New("s3: secret=abc123 refused"))
	var ce *dashcontract.Error
	if !errors.As(err, &ce) || ce.Message != "an internal error occurred" {
		t.Fatalf("mapError leaked the cause: %v", err)
	}
}

func TestMapError_PassesContractErrorsThrough(t *testing.T) {
	in := conflict("bucket still holds objects")
	var inCE, gotCE *dashcontract.Error
	if !errors.As(in, &inCE) || !errors.As(mapError(in), &gotCE) || gotCE != inCE {
		t.Fatalf("mapError replaced a contract error: %v", mapError(in))
	}
	if mapError(nil) != nil {
		t.Fatal("mapError(nil) is not nil")
	}
}

func TestMapError_BucketBeforeObject(t *testing.T) {
	var ce *dashcontract.Error
	if !errors.As(mapError(driver.ErrBucketNotFound), &ce) || ce.Message != "bucket not found" {
		t.Fatalf("bucket not found mapped to %v", ce)
	}
	if !errors.As(mapError(driver.ErrObjectNotFound), &ce) || ce.Message != "object not found" {
		t.Fatalf("object not found mapped to %v", ce)
	}
}

func TestRequireName(t *testing.T) {
	if err := requireName("bucket", "reports"); err != nil {
		t.Fatalf("requireName(reports) = %v", err)
	}
	for _, v := range []string{"", " ", "\t"} {
		if codeOf(requireName("bucket", v)) != dashcontract.CodeBadRequest {
			t.Errorf("requireName(%q) accepted it", v)
		}
	}
}

// TestMapError_CancelledRequestIsRetryableAndQuiet covers a page that
// navigates away mid-request. That is routine, so it maps to a retryable
// UNAVAILABLE and never reaches the error log.
func TestMapError_CancelledRequestIsRetryableAndQuiet(t *testing.T) {
	logger := log.NewTestLogger()
	deps := Deps{Logger: logger}
	for _, cause := range []error{context.Canceled, context.DeadlineExceeded} {
		err := deps.mapError("objects.list", fmt.Errorf("memdriver: list: %w", cause))
		var ce *dashcontract.Error
		if !errors.As(err, &ce) || ce.Code != dashcontract.CodeUnavailable || !ce.Retryable {
			t.Errorf("mapError(%v) = %+v, want a retryable UNAVAILABLE", cause, ce)
		}
	}
	if entries := logger.(*log.TestLogger).GetLogsByLevel("ERROR"); len(entries) != 0 {
		t.Fatalf("error entries = %d, want none for a cancelled request", len(entries))
	}
}
