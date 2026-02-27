package scan

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
)

// ClamAVProvider implements Provider using the ClamAV INSTREAM protocol.
// It connects to a ClamAV daemon over TCP and streams content for scanning.
type ClamAVProvider struct {
	addr    string
	timeout time.Duration
}

// ClamAVOption configures the ClamAV provider.
type ClamAVOption func(*ClamAVProvider)

// WithTimeout sets the scan timeout for ClamAV connections.
func WithTimeout(d time.Duration) ClamAVOption {
	return func(p *ClamAVProvider) { p.timeout = d }
}

// NewClamAVProvider creates a ClamAV scan provider.
// The addr is the ClamAV daemon address (e.g., "localhost:3310").
func NewClamAVProvider(addr string, opts ...ClamAVOption) *ClamAVProvider {
	p := &ClamAVProvider{
		addr:    addr,
		timeout: 30 * time.Second,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Scan sends content to ClamAV using the INSTREAM protocol and returns
// the scan result.
//
// INSTREAM protocol:
//  1. Send "zINSTREAM\x00"
//  2. For each chunk: send 4-byte big-endian length + data
//  3. Send 4-byte zero to signal end of stream
//  4. Read response: "stream: OK\x00" or "stream: <THREAT> FOUND\x00"
func (p *ClamAVProvider) Scan(ctx context.Context, r io.Reader) (*Result, error) {
	dialer := &net.Dialer{Timeout: p.timeout}
	conn, err := dialer.DialContext(ctx, "tcp", p.addr)
	if err != nil {
		return nil, fmt.Errorf("clamav: connect to %s: %w", p.addr, err)
	}
	defer conn.Close()

	// Apply timeout from context or default.
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(p.timeout)
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, fmt.Errorf("clamav: set deadline: %w", err)
	}

	// Send INSTREAM command (null-terminated).
	if _, err := conn.Write([]byte("zINSTREAM\x00")); err != nil {
		return nil, fmt.Errorf("clamav: send command: %w", err)
	}

	// Stream content in chunks.
	buf := make([]byte, 8192)
	var lenBuf [4]byte

	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			binary.BigEndian.PutUint32(lenBuf[:], uint32(n)) //nolint:gosec // n is bounded by 8192 buf size
			if _, err := conn.Write(lenBuf[:]); err != nil {
				return nil, fmt.Errorf("clamav: send chunk length: %w", err)
			}
			if _, err := conn.Write(buf[:n]); err != nil {
				return nil, fmt.Errorf("clamav: send chunk data: %w", err)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("clamav: read content: %w", readErr)
		}
	}

	// Send zero-length sentinel.
	binary.BigEndian.PutUint32(lenBuf[:], 0)
	if _, err := conn.Write(lenBuf[:]); err != nil {
		return nil, fmt.Errorf("clamav: send sentinel: %w", err)
	}

	// Read response.
	var respBuf bytes.Buffer
	if _, err := io.Copy(&respBuf, conn); err != nil {
		return nil, fmt.Errorf("clamav: read response: %w", err)
	}

	response := strings.TrimRight(respBuf.String(), "\x00\n\r")
	return parseResponse(response), nil
}

// parseResponse parses a ClamAV response string into a Result.
func parseResponse(response string) *Result {
	if strings.HasSuffix(response, "OK") {
		return &Result{Clean: true}
	}

	if strings.HasSuffix(response, "FOUND") {
		// Extract threat name: "stream: <THREAT> FOUND"
		threat := response
		threat = strings.TrimPrefix(threat, "stream: ")
		threat = strings.TrimSuffix(threat, " FOUND")
		return &Result{
			Clean:  false,
			Threat: threat,
		}
	}

	// Unknown response.
	return &Result{
		Clean:   false,
		Threat:  "unknown",
		Details: response,
	}
}
