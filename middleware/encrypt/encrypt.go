// Package encrypt provides AES-256-GCM encryption middleware for Trove.
// It encrypts data on write and decrypts on read, using a KeyProvider
// interface for key management and rotation.
package encrypt

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"

	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/middleware"
)

// Compile-time interface checks.
var (
	_ middleware.Middleware      = (*Encrypt)(nil)
	_ middleware.ReadMiddleware  = (*Encrypt)(nil)
	_ middleware.WriteMiddleware = (*Encrypt)(nil)
)

// KeyProvider supplies encryption keys. Implementations can fetch keys
// from Vault, environment variables, or any other source.
type KeyProvider interface {
	// Key returns the current encryption key (must be 32 bytes for AES-256).
	Key(ctx context.Context) ([]byte, error)
}

// StaticKeyProvider is a KeyProvider that returns a fixed key.
type StaticKeyProvider struct {
	key []byte
}

// NewStaticKeyProvider creates a key provider from a fixed 32-byte key.
func NewStaticKeyProvider(key []byte) *StaticKeyProvider {
	return &StaticKeyProvider{key: key}
}

// Key returns the static key.
func (p *StaticKeyProvider) Key(_ context.Context) ([]byte, error) {
	return p.key, nil
}

// Option configures the Encrypt middleware.
type Option func(*Encrypt)

// WithKeyProvider sets the key provider.
func WithKeyProvider(kp KeyProvider) Option {
	return func(e *Encrypt) { e.keyProvider = kp }
}

// Encrypt provides AES-256-GCM encryption middleware.
type Encrypt struct {
	keyProvider KeyProvider
}

// New creates a new encryption middleware with the given options.
func New(opts ...Option) *Encrypt {
	e := &Encrypt{}
	for _, opt := range opts {
		opt(e)
	}
	return e
}

// Name returns the middleware identifier.
func (e *Encrypt) Name() string { return "encrypt" }

// Direction returns ReadWrite since encryption participates in both paths.
func (e *Encrypt) Direction() middleware.Direction { return middleware.DirectionReadWrite }

// WrapWriter wraps a writer to encrypt data before it reaches the driver.
// The encrypted format is: [12-byte nonce][encrypted data + GCM tag].
// Since GCM requires knowing the full plaintext, this implementation
// buffers the data, encrypts, then writes. For large files, use chunked
// encryption at the stream level.
func (e *Encrypt) WrapWriter(ctx context.Context, w io.WriteCloser, _ string) (io.WriteCloser, error) {
	key, err := e.keyProvider.Key(ctx)
	if err != nil {
		return nil, fmt.Errorf("encrypt: get key: %w", err)
	}

	return &encryptWriter{
		ctx:   ctx,
		inner: w,
		key:   key,
		buf:   make([]byte, 0, 64*1024),
	}, nil
}

// WrapReader wraps a reader to decrypt data from the driver.
func (e *Encrypt) WrapReader(ctx context.Context, r io.ReadCloser, _ *driver.ObjectInfo) (io.ReadCloser, error) {
	key, err := e.keyProvider.Key(ctx)
	if err != nil {
		return nil, fmt.Errorf("encrypt: get key: %w", err)
	}

	return &decryptReader{
		inner:   r,
		key:     key,
		buf:     nil,
		readPos: 0,
	}, nil
}

// encryptWriter buffers plaintext, then encrypts on Close.
type encryptWriter struct {
	ctx   context.Context
	inner io.WriteCloser
	key   []byte
	buf   []byte
}

func (w *encryptWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	return len(p), nil
}

func (w *encryptWriter) Close() error {
	block, err := aes.NewCipher(w.key)
	if err != nil {
		return fmt.Errorf("encrypt: new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("encrypt: new gcm: %w", err)
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("encrypt: generate nonce: %w", err)
	}

	// Write format: [4-byte nonce size][nonce][ciphertext with GCM tag]
	ciphertext := gcm.Seal(nil, nonce, w.buf, nil)

	// Write nonce size (4 bytes, little-endian).
	var nonceSizeBuf [4]byte
	binary.LittleEndian.PutUint32(nonceSizeBuf[:], uint32(len(nonce))) //nolint:gosec // nonce size is always small
	if _, err := w.inner.Write(nonceSizeBuf[:]); err != nil {
		return fmt.Errorf("encrypt: write nonce size: %w", err)
	}

	// Write nonce.
	if _, err := w.inner.Write(nonce); err != nil {
		return fmt.Errorf("encrypt: write nonce: %w", err)
	}

	// Write ciphertext.
	if _, err := w.inner.Write(ciphertext); err != nil {
		return fmt.Errorf("encrypt: write ciphertext: %w", err)
	}

	return w.inner.Close()
}

// decryptReader reads all encrypted data, decrypts, then serves from buffer.
type decryptReader struct {
	inner   io.ReadCloser
	key     []byte
	buf     []byte
	readPos int
	done    bool
}

func (r *decryptReader) Read(p []byte) (int, error) {
	if !r.done {
		if err := r.decrypt(); err != nil {
			return 0, err
		}
		r.done = true
	}

	if r.readPos >= len(r.buf) {
		return 0, io.EOF
	}

	n := copy(p, r.buf[r.readPos:])
	r.readPos += n
	return n, nil
}

func (r *decryptReader) decrypt() error {
	data, err := io.ReadAll(r.inner)
	if err != nil {
		return fmt.Errorf("encrypt: read ciphertext: %w", err)
	}

	if len(data) < 4 {
		return fmt.Errorf("encrypt: ciphertext too short")
	}

	// Read nonce size.
	nonceSize := int(binary.LittleEndian.Uint32(data[:4]))
	data = data[4:]

	if len(data) < nonceSize {
		return fmt.Errorf("encrypt: ciphertext too short for nonce")
	}

	nonce := data[:nonceSize]
	ciphertext := data[nonceSize:]

	block, err := aes.NewCipher(r.key)
	if err != nil {
		return fmt.Errorf("encrypt: new cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return fmt.Errorf("encrypt: new gcm: %w", err)
	}

	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return fmt.Errorf("encrypt: decrypt: %w", err)
	}

	r.buf = plaintext
	return nil
}

func (r *decryptReader) Close() error {
	return r.inner.Close()
}
