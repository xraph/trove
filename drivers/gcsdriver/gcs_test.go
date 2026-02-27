package gcsdriver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
)

func TestNew(t *testing.T) {
	drv := New()
	assert.NotNil(t, drv)
	assert.Equal(t, "gcs", drv.Name())
}

func TestParseDSN_Basic(t *testing.T) {
	cfg, err := parseDSN("gcs://my-project/my-bucket")
	require.NoError(t, err)
	assert.Equal(t, "my-project", cfg.ProjectID)
	assert.Equal(t, "my-bucket", cfg.Bucket)
	assert.Empty(t, cfg.CredentialsFile)
	assert.Empty(t, cfg.Endpoint)
}

func TestParseDSN_WithCredentials(t *testing.T) {
	cfg, err := parseDSN("gcs://project/bucket?credentials=/path/to/key.json")
	require.NoError(t, err)
	assert.Equal(t, "project", cfg.ProjectID)
	assert.Equal(t, "bucket", cfg.Bucket)
	assert.Equal(t, "/path/to/key.json", cfg.CredentialsFile)
}

func TestParseDSN_WithEndpoint(t *testing.T) {
	cfg, err := parseDSN("gcs://project/bucket?endpoint=http://localhost:4443")
	require.NoError(t, err)
	assert.Equal(t, "http://localhost:4443", cfg.Endpoint)
}

func TestParseDSN_MissingBucket(t *testing.T) {
	_, err := parseDSN("gcs://project/")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing bucket")
}

func TestParseDSN_MissingProjectID(t *testing.T) {
	_, err := parseDSN("gcs:///bucket")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing project ID")
}

func TestParseDSN_WrongScheme(t *testing.T) {
	_, err := parseDSN("s3://project/bucket")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected scheme")
}

func TestParseDSN_EmptyDSN(t *testing.T) {
	_, err := parseDSN("")
	assert.Error(t, err)
}

func TestInterfaceCompliance(t *testing.T) {
	var drv driver.Driver = New()
	assert.Equal(t, "gcs", drv.Name())

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
	factory, ok := driver.Lookup("gcs")
	assert.True(t, ok)
	assert.NotNil(t, factory)

	drv := factory()
	assert.Equal(t, "gcs", drv.Name())
}
