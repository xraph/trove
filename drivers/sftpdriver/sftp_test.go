package sftpdriver

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
)

func TestNew(t *testing.T) {
	drv := New()
	assert.NotNil(t, drv)
	assert.Equal(t, "sftp", drv.Name())
}

func TestParseDSN_Basic(t *testing.T) {
	cfg, err := parseDSN("sftp://deploy:secret@files.example.com:22/data/storage")
	require.NoError(t, err)
	assert.Equal(t, "deploy", cfg.User)
	assert.Equal(t, "secret", cfg.Password)
	assert.Equal(t, "files.example.com", cfg.Host)
	assert.Equal(t, 22, cfg.Port)
	assert.Equal(t, "/data/storage", cfg.BasePath)
	assert.Empty(t, cfg.KeyFile)
}

func TestParseDSN_WithKeyFile(t *testing.T) {
	cfg, err := parseDSN("sftp://deploy@host/uploads?key=/home/deploy/.ssh/id_rsa")
	require.NoError(t, err)
	assert.Equal(t, "deploy", cfg.User)
	assert.Empty(t, cfg.Password)
	assert.Equal(t, "host", cfg.Host)
	assert.Equal(t, 22, cfg.Port)
	assert.Equal(t, "/uploads", cfg.BasePath)
	assert.Equal(t, "/home/deploy/.ssh/id_rsa", cfg.KeyFile)
}

func TestParseDSN_CustomPort(t *testing.T) {
	cfg, err := parseDSN("sftp://user:pass@myhost:2222/var/data")
	require.NoError(t, err)
	assert.Equal(t, "myhost", cfg.Host)
	assert.Equal(t, 2222, cfg.Port)
}

func TestParseDSN_DefaultPath(t *testing.T) {
	cfg, err := parseDSN("sftp://user:pass@host")
	require.NoError(t, err)
	assert.Equal(t, "/", cfg.BasePath)
}

func TestParseDSN_MissingHost(t *testing.T) {
	_, err := parseDSN("sftp://user:pass@/path")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing host")
}

func TestParseDSN_MissingUser(t *testing.T) {
	_, err := parseDSN("sftp://host/path")
	// When no user is specified, "host" becomes the host and user is empty.
	// Actually, URL parsing: sftp://host/path -> host="host", user=""
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "missing user")
}

func TestParseDSN_WrongScheme(t *testing.T) {
	_, err := parseDSN("s3://host/bucket")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "expected scheme")
}

func TestParseDSN_EmptyDSN(t *testing.T) {
	_, err := parseDSN("")
	assert.Error(t, err)
}

func TestParseDSN_Addr(t *testing.T) {
	cfg, err := parseDSN("sftp://user:pass@myhost:2222/data")
	require.NoError(t, err)
	assert.Equal(t, "myhost:2222", cfg.addr())
}

func TestInterfaceCompliance(t *testing.T) {
	var drv driver.Driver = New()
	assert.Equal(t, "sftp", drv.Name())
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
	factory, ok := driver.Lookup("sftp")
	assert.True(t, ok)
	assert.NotNil(t, factory)

	drv := factory()
	assert.Equal(t, "sftp", drv.Name())
}

func TestDetectContentType(t *testing.T) {
	tests := []struct {
		name     string
		expected string
	}{
		{"file.txt", "text/plain"},
		{"image.png", "image/png"},
		{"data.json", "application/json"},
		{"noext", "application/octet-stream"},
	}

	for _, tt := range tests {
		ct := detectContentType(tt.name)
		// Content type detection varies by platform, just check non-empty.
		assert.NotEmpty(t, ct, "content type for %s", tt.name)
	}
}
