package s3driver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
)

func TestNew(t *testing.T) {
	drv := New()
	assert.NotNil(t, drv)
	assert.Equal(t, "s3", drv.Name())
}

func TestParseDSN_Basic(t *testing.T) {
	cfg, err := parseDSN("s3://us-west-2/my-bucket")
	require.NoError(t, err)
	assert.Equal(t, "us-west-2", cfg.Region)
	assert.Equal(t, "my-bucket", cfg.Bucket)
	assert.Empty(t, cfg.AccessKey)
	assert.Empty(t, cfg.SecretKey)
	assert.Empty(t, cfg.Endpoint)
	assert.False(t, cfg.ForcePathStyle)
}

func TestParseDSN_WithCredentials(t *testing.T) {
	cfg, err := parseDSN("s3://AKID:SECRET@us-east-1/data")
	require.NoError(t, err)
	assert.Equal(t, "AKID", cfg.AccessKey)
	assert.Equal(t, "SECRET", cfg.SecretKey)
	assert.Equal(t, "us-east-1", cfg.Region)
	assert.Equal(t, "data", cfg.Bucket)
}

func TestParseDSN_WithEndpoint(t *testing.T) {
	cfg, err := parseDSN("s3://minioadmin:minioadmin@us-east-1/test?endpoint=http://localhost:9000&path_style=true")
	require.NoError(t, err)
	assert.Equal(t, "minioadmin", cfg.AccessKey)
	assert.Equal(t, "minioadmin", cfg.SecretKey)
	assert.Equal(t, "us-east-1", cfg.Region)
	assert.Equal(t, "test", cfg.Bucket)
	assert.Equal(t, "http://localhost:9000", cfg.Endpoint)
	assert.True(t, cfg.ForcePathStyle)
}

func TestParseDSN_DefaultRegion(t *testing.T) {
	// Host-less DSN defaults to us-east-1.
	cfg, err := parseDSN("s3:///my-bucket")
	require.NoError(t, err)
	assert.Equal(t, "us-east-1", cfg.Region)
	assert.Equal(t, "my-bucket", cfg.Bucket)
}

func TestParseDSN_MissingBucket(t *testing.T) {
	_, err := parseDSN("s3://us-east-1/")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing bucket")
}

func TestParseDSN_WrongScheme(t *testing.T) {
	_, err := parseDSN("gcs://us-east-1/bucket")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected scheme")
}

func TestParseDSN_EmptyDSN(t *testing.T) {
	_, err := parseDSN("")
	assert.Error(t, err)
}

func TestParseDSN_PathStyle_1(t *testing.T) {
	cfg, err := parseDSN("s3://us-east-1/bucket?path_style=1")
	require.NoError(t, err)
	assert.True(t, cfg.ForcePathStyle)
}

func TestParseDSN_PathStyle_False(t *testing.T) {
	cfg, err := parseDSN("s3://us-east-1/bucket?path_style=false")
	require.NoError(t, err)
	assert.False(t, cfg.ForcePathStyle)
}

func TestInterfaceCompliance(t *testing.T) {
	var drv driver.Driver = New()
	assert.Equal(t, "s3", drv.Name())

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
	// Unwrap with a type that doesn't hold an S3Driver returns nil.
	type fakeHandle struct{}
	mock := &struct {
		fakeHandle
	}{}
	_ = mock
	// Can't test Unwrap without a Trove handle, but we can test the nil path.
	result := Unwrap(fakeDriverHolder{})
	assert.Nil(t, result)
}

type fakeDriverHolder struct{}

func (fakeDriverHolder) Driver() driver.Driver {
	return nil
}

func TestRegistered(t *testing.T) {
	factory, ok := driver.Lookup("s3")
	assert.True(t, ok)
	assert.NotNil(t, factory)

	drv := factory()
	assert.Equal(t, "s3", drv.Name())
}
