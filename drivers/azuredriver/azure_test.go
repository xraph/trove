package azuredriver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
)

func TestNew(t *testing.T) {
	drv := New()
	assert.NotNil(t, drv)
	assert.Equal(t, "azure", drv.Name())
}

func TestParseDSN_Basic(t *testing.T) {
	cfg, err := parseDSN("azure://myaccount/mycontainer?key=mykey123")
	require.NoError(t, err)
	assert.Equal(t, "myaccount", cfg.AccountName)
	assert.Equal(t, "mycontainer", cfg.Container)
	assert.Equal(t, "mykey123", cfg.AccountKey)
	assert.Empty(t, cfg.ConnectionString)
	assert.Empty(t, cfg.Endpoint)
}

func TestParseDSN_WithConnectionString(t *testing.T) {
	cfg, err := parseDSN("azure://myaccount/mycontainer?connection_string=DefaultEndpointsProtocol=https")
	require.NoError(t, err)
	assert.Equal(t, "DefaultEndpointsProtocol=https", cfg.ConnectionString)
}

func TestParseDSN_WithEndpoint(t *testing.T) {
	cfg, err := parseDSN("azure://devstoreaccount1/testcontainer?endpoint=http://127.0.0.1:10000/devstoreaccount1")
	require.NoError(t, err)
	assert.Equal(t, "http://127.0.0.1:10000/devstoreaccount1", cfg.Endpoint)
	assert.Equal(t, "devstoreaccount1", cfg.AccountName)
	assert.Equal(t, "testcontainer", cfg.Container)
}

func TestParseDSN_MissingContainer(t *testing.T) {
	_, err := parseDSN("azure://myaccount/")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing container")
}

func TestParseDSN_MissingAccountName(t *testing.T) {
	_, err := parseDSN("azure:///mycontainer")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing account name")
}

func TestParseDSN_WrongScheme(t *testing.T) {
	_, err := parseDSN("s3://account/container")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected scheme")
}

func TestParseDSN_EmptyDSN(t *testing.T) {
	_, err := parseDSN("")
	assert.Error(t, err)
}

func TestInterfaceCompliance(t *testing.T) {
	var drv driver.Driver = New()
	assert.Equal(t, "azure", drv.Name())

	var _ driver.MultipartDriver = New()
	var _ driver.PresignDriver = New()
	var _ driver.RangeDriver = New()
}

func TestDriverClosed(t *testing.T) {
	drv := New()
	drv.closed = true

	_, _, err := drv.getClient()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "closed")
}

func TestDriverNotOpened(t *testing.T) {
	drv := New()

	_, _, err := drv.getClient()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not opened")
}

func TestUnwrap_Nil(t *testing.T) {
	result := Unwrap(fakeDriverHolder{})
	assert.Nil(t, result)
}

type fakeDriverHolder struct{}

func (fakeDriverHolder) Driver() driver.Driver {
	return nil
}

func TestRegistered(t *testing.T) {
	factory, ok := driver.Lookup("azure")
	assert.True(t, ok)
	assert.NotNil(t, factory)

	drv := factory()
	assert.Equal(t, "azure", drv.Name())
}
