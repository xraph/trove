package localdriver_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/drivers/localdriver"
	"github.com/xraph/trove/trovetest"
)

func TestLocalDriverConformance(t *testing.T) {
	trovetest.RunDriverSuite(t, func(t *testing.T) driver.Driver {
		t.Helper()
		tmpDir := t.TempDir()
		drv := localdriver.New()
		err := drv.Open(context.Background(), "file://"+tmpDir)
		require.NoError(t, err)
		return drv
	})
}

func TestLocalDriver_OpenInvalidDSN(t *testing.T) {
	drv := localdriver.New()

	t.Run("non-file scheme", func(t *testing.T) {
		err := drv.Open(context.Background(), "s3://bucket")
		require.Error(t, err)
	})

	t.Run("empty DSN", func(t *testing.T) {
		err := drv.Open(context.Background(), "")
		require.Error(t, err)
	})
}

func TestLocalDriver_Unwrap(t *testing.T) {
	drv := localdriver.New()
	drv.SetRootDir(t.TempDir())

	// Create a minimal interface to test Unwrap.
	type driverAccessor interface {
		Driver() driver.Driver
	}
	wrapper := struct{ driverAccessor }{
		driverAccessor: &mockAccessor{drv: drv},
	}

	unwrapped := localdriver.Unwrap(wrapper)
	require.NotNil(t, unwrapped)
	require.Equal(t, drv.RootDir(), unwrapped.RootDir())
}

type mockAccessor struct {
	drv driver.Driver
}

func (m *mockAccessor) Driver() driver.Driver {
	return m.drv
}
