package memdriver_test

import (
	"testing"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/drivers/memdriver"
	"github.com/xraph/trove/trovetest"
)

func TestMemDriverConformance(t *testing.T) {
	trovetest.RunDriverSuite(t, func(t *testing.T) driver.Driver {
		t.Helper()
		return memdriver.New()
	})
}
