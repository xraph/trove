package scan

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove"
	"github.com/xraph/trove/middleware"
)

// mockProvider is a test Provider.
type mockProvider struct {
	result *Result
	err    error
}

func (m *mockProvider) Scan(_ context.Context, _ io.Reader) (*Result, error) {
	if m.err != nil {
		return nil, m.err
	}
	return m.result, nil
}

// bufCloser wraps a bytes.Buffer as an io.WriteCloser.
type bufCloser struct {
	bytes.Buffer
	closed bool
}

func (b *bufCloser) Close() error {
	b.closed = true
	return nil
}

func TestName(t *testing.T) {
	s := New()
	assert.Equal(t, "scan", s.Name())
}

func TestDirection(t *testing.T) {
	s := New()
	assert.Equal(t, middleware.DirectionWrite, s.Direction())
}

func TestInterfaceCompliance(_ *testing.T) {
	var _ middleware.Middleware = New()
	var _ middleware.WriteMiddleware = New()
}

func TestCleanContentPassthrough(t *testing.T) {
	provider := &mockProvider{
		result: &Result{Clean: true},
	}
	s := New(WithProvider(provider))

	var dst bufCloser
	ctx := context.Background()

	w, err := s.WrapWriter(ctx, &dst, "file.txt")
	require.NoError(t, err)

	_, err = w.Write([]byte("hello world"))
	require.NoError(t, err)

	err = w.Close()
	require.NoError(t, err)

	assert.Equal(t, "hello world", dst.String())
	assert.True(t, dst.closed)
}

func TestThreatBlocked(t *testing.T) {
	provider := &mockProvider{
		result: &Result{Clean: false, Threat: "EICAR-Test-Signature"},
	}
	s := New(WithProvider(provider))

	var dst bufCloser
	ctx := context.Background()

	w, err := s.WrapWriter(ctx, &dst, "malware.exe")
	require.NoError(t, err)

	_, err = w.Write([]byte("fake malware content"))
	require.NoError(t, err)

	err = w.Close()
	assert.Error(t, err)
	assert.ErrorIs(t, err, trove.ErrContentBlocked)

	// Destination should not have received data.
	assert.Empty(t, dst.String())
	assert.False(t, dst.closed)
}

func TestOnDetectCallback(t *testing.T) {
	provider := &mockProvider{
		result: &Result{Clean: false, Threat: "Win.Trojan.Test"},
	}

	var detectedKey string
	var detectedResult *Result

	s := New(
		WithProvider(provider),
		WithOnDetect(func(_ context.Context, key string, result *Result) {
			detectedKey = key
			detectedResult = result
		}),
	)

	var dst bufCloser
	ctx := context.Background()

	w, err := s.WrapWriter(ctx, &dst, "virus.exe")
	require.NoError(t, err)

	w.Write([]byte("infected content"))
	w.Close()

	assert.Equal(t, "virus.exe", detectedKey)
	assert.NotNil(t, detectedResult)
	assert.Equal(t, "Win.Trojan.Test", detectedResult.Threat)
}

func TestMaxSizeSkip(t *testing.T) {
	scanCalled := false
	provider := &mockProvider{
		result: &Result{Clean: true},
	}
	// Override to track calls.
	trackingProvider := &trackProvider{inner: provider, called: &scanCalled}

	s := New(
		WithProvider(trackingProvider),
		WithMaxSize(10),
	)

	var dst bufCloser
	ctx := context.Background()

	w, err := s.WrapWriter(ctx, &dst, "big.bin")
	require.NoError(t, err)

	// Write data larger than maxSize.
	data := make([]byte, 20)
	_, err = w.Write(data)
	require.NoError(t, err)

	err = w.Close()
	require.NoError(t, err)

	// Should pass through without scanning.
	assert.False(t, scanCalled)
	assert.Equal(t, 20, dst.Len())
}

func TestExtensionSkip(t *testing.T) {
	s := New(
		WithProvider(&mockProvider{result: &Result{Clean: true}}),
		WithSkipExtensions(".log", "tmp"),
	)

	var dst bufCloser
	ctx := context.Background()

	// Skip extension should passthrough without wrapping.
	w, err := s.WrapWriter(ctx, &dst, "debug.log")
	require.NoError(t, err)

	_, err = w.Write([]byte("log data"))
	require.NoError(t, err)

	err = w.Close()
	require.NoError(t, err)

	assert.Equal(t, "log data", dst.String())
}

func TestProviderError(t *testing.T) {
	provider := &mockProvider{
		err: errors.New("scan service unavailable"),
	}
	s := New(WithProvider(provider))

	var dst bufCloser
	ctx := context.Background()

	w, err := s.WrapWriter(ctx, &dst, "file.txt")
	require.NoError(t, err)

	w.Write([]byte("content"))

	err = w.Close()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "scan service unavailable")
}

func TestNoProviderPassthrough(t *testing.T) {
	// No provider set — should passthrough.
	s := New()

	var dst bufCloser
	ctx := context.Background()

	w, err := s.WrapWriter(ctx, &dst, "file.txt")
	require.NoError(t, err)

	_, err = w.Write([]byte("content"))
	require.NoError(t, err)

	err = w.Close()
	require.NoError(t, err)

	assert.Equal(t, "content", dst.String())
}

func TestParseResponse(t *testing.T) {
	tests := []struct {
		response string
		clean    bool
		threat   string
	}{
		{"stream: OK", true, ""},
		{"stream: EICAR-Test-Signature FOUND", false, "EICAR-Test-Signature"},
		{"stream: Win.Trojan.Agent-123 FOUND", false, "Win.Trojan.Agent-123"},
		{"ERROR: some error", false, "unknown"},
	}

	for _, tt := range tests {
		result := parseResponse(tt.response)
		assert.Equal(t, tt.clean, result.Clean, "response: %s", tt.response)
		if !tt.clean {
			assert.Equal(t, tt.threat, result.Threat, "response: %s", tt.response)
		}
	}
}

// trackProvider wraps a provider and tracks whether Scan was called.
type trackProvider struct {
	inner  Provider
	called *bool
}

func (p *trackProvider) Scan(ctx context.Context, r io.Reader) (*Result, error) {
	*p.called = true
	return p.inner.Scan(ctx, r)
}
