// Package trovetest provides testing utilities for Trove storage drivers.
//
// The primary entry point is RunDriverSuite, which runs a comprehensive
// conformance test suite against any driver.Driver implementation. All
// Trove drivers must pass this suite to be considered conformant.
package trovetest

import (
	"bytes"
	"io"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/xraph/trove/driver"
)

// TestObject creates a test object key and reader from the given data.
func TestObject(key string, data []byte) (string, io.Reader) {
	return key, bytes.NewReader(data)
}

// RandomData generates random bytes of the specified size.
func RandomData(size int) []byte {
	data := make([]byte, size)
	//nolint:gosec // math/rand is fine for test data
	r := rand.New(rand.NewSource(42))
	r.Read(data)
	return data
}

// AssertObjectEquals asserts that two ObjectInfo values are equal
// on the fields that matter for conformance (ignoring timing fields).
func AssertObjectEquals(t *testing.T, expected, actual *driver.ObjectInfo) {
	t.Helper()
	assert.Equal(t, expected.Key, actual.Key, "key mismatch")
	assert.Equal(t, expected.Size, actual.Size, "size mismatch")
	if expected.ContentType != "" {
		assert.Equal(t, expected.ContentType, actual.ContentType, "content_type mismatch")
	}
}

// ReadAll reads all bytes from an ObjectReader and closes it.
func ReadAll(t *testing.T, reader *driver.ObjectReader) []byte {
	t.Helper()
	data, err := io.ReadAll(reader)
	assert.NoError(t, err, "reading object content")
	assert.NoError(t, reader.Close(), "closing object reader")
	return data
}
