package encrypt

import (
	"bytes"
	"context"
	"crypto/rand"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/middleware"
)

func testKey() []byte {
	key := make([]byte, 32) // AES-256
	_, _ = rand.Read(key)
	return key
}

func TestEncrypt_InterfaceCompliance(t *testing.T) {
	e := New(WithKeyProvider(NewStaticKeyProvider(testKey())))
	var _ middleware.Middleware = e
	var _ middleware.ReadMiddleware = e
	var _ middleware.WriteMiddleware = e
	assert.Equal(t, "encrypt", e.Name())
	assert.Equal(t, middleware.DirectionReadWrite, e.Direction())
}

func TestEncrypt_Roundtrip(t *testing.T) {
	key := testKey()
	e := New(WithKeyProvider(NewStaticKeyProvider(key)))
	ctx := context.Background()

	plaintext := []byte("hello, world! this is a test of AES-256-GCM encryption.")

	// Encrypt: write through the encryption middleware.
	var encrypted bytes.Buffer
	w, err := e.WrapWriter(ctx, &nopWriteCloser{&encrypted}, "test-key")
	require.NoError(t, err)

	_, err = w.Write(plaintext)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	// Encrypted data should be different from plaintext.
	assert.NotEqual(t, plaintext, encrypted.Bytes())
	assert.Greater(t, encrypted.Len(), len(plaintext))

	// Decrypt: read through the decryption middleware.
	r, err := e.WrapReader(ctx, io.NopCloser(bytes.NewReader(encrypted.Bytes())), nil)
	require.NoError(t, err)

	decrypted, err := io.ReadAll(r)
	require.NoError(t, err)
	require.NoError(t, r.Close())

	assert.Equal(t, plaintext, decrypted)
}

func TestEncrypt_EmptyData(t *testing.T) {
	key := testKey()
	e := New(WithKeyProvider(NewStaticKeyProvider(key)))
	ctx := context.Background()

	// Encrypt empty data.
	var encrypted bytes.Buffer
	w, err := e.WrapWriter(ctx, &nopWriteCloser{&encrypted}, "empty")
	require.NoError(t, err)
	require.NoError(t, w.Close())

	// Decrypt should return empty.
	r, err := e.WrapReader(ctx, io.NopCloser(bytes.NewReader(encrypted.Bytes())), nil)
	require.NoError(t, err)

	decrypted, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Empty(t, decrypted)
}

func TestEncrypt_WrongKey(t *testing.T) {
	ctx := context.Background()

	// Encrypt with key1.
	e1 := New(WithKeyProvider(NewStaticKeyProvider(testKey())))
	var encrypted bytes.Buffer
	w, err := e1.WrapWriter(ctx, &nopWriteCloser{&encrypted}, "test")
	require.NoError(t, err)
	_, _ = w.Write([]byte("secret data"))
	require.NoError(t, w.Close())

	// Decrypt with different key should fail.
	e2 := New(WithKeyProvider(NewStaticKeyProvider(testKey())))
	r, err := e2.WrapReader(ctx, io.NopCloser(bytes.NewReader(encrypted.Bytes())), nil)
	require.NoError(t, err)

	_, err = io.ReadAll(r)
	assert.Error(t, err)
}

func TestEncrypt_LargeData(t *testing.T) {
	key := testKey()
	e := New(WithKeyProvider(NewStaticKeyProvider(key)))
	ctx := context.Background()

	// Generate 1MB of random data.
	plaintext := make([]byte, 1024*1024)
	_, _ = rand.Read(plaintext)

	var encrypted bytes.Buffer
	w, err := e.WrapWriter(ctx, &nopWriteCloser{&encrypted}, "large")
	require.NoError(t, err)
	_, err = w.Write(plaintext)
	require.NoError(t, err)
	require.NoError(t, w.Close())

	r, err := e.WrapReader(ctx, io.NopCloser(bytes.NewReader(encrypted.Bytes())), nil)
	require.NoError(t, err)

	decrypted, err := io.ReadAll(r)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

// nopWriteCloser wraps a bytes.Buffer as an io.WriteCloser.
type nopWriteCloser struct {
	buf *bytes.Buffer
}

func (w *nopWriteCloser) Write(p []byte) (int, error) { return w.buf.Write(p) }
func (w *nopWriteCloser) Close() error                { return nil }
