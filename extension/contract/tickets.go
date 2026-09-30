package contract

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Ticket operations. A ticket authorises exactly one of these against one
// object, so a download ticket cannot be replayed as an upload.
const (
	OpDownload = "download"
	OpPreview  = "preview"
	OpUpload   = "upload"
)

// Ticket lifetimes. The browser follows a download or preview link at
// once. An upload of the largest allowed file on a slow link takes a while.
const (
	DownloadTicketTTL = 60 * time.Second
	UploadTicketTTL   = 15 * time.Minute
)

// minSecretBytes is the shortest HMAC key a Signer accepts.
const minSecretBytes = 32

var (
	// ErrTicketInvalid reports a ticket that is malformed or whose
	// signature does not match.
	ErrTicketInvalid = errors.New("trove/contract: ticket is invalid")

	// ErrTicketExpired reports a correctly signed ticket past its expiry.
	ErrTicketExpired = errors.New("trove/contract: ticket has expired")
)

// Ticket says what one content request may do. A contract intent signs it
// and the content route checks it, so the route needs nothing from the
// dashboard's session. Field names are short because the ticket travels in
// a URL.
type Ticket struct {
	Store       string `json:"s"`
	Bucket      string `json:"b"`
	Key         string `json:"k"`
	Op          string `json:"o"`
	Expires     int64  `json:"e"`
	Subject     string `json:"sub,omitempty"`
	Size        int64  `json:"n,omitempty"`
	ContentType string `json:"ct,omitempty"`
	Overwrite   bool   `json:"ow,omitempty"`
	Limit       int64  `json:"l,omitempty"`
}

// Signer mints and checks tickets with one HMAC-SHA256 key.
type Signer struct {
	key []byte
	now func() time.Time
}

// NewSigner returns a Signer over key, which must be at least 32 bytes.
func NewSigner(key []byte) (*Signer, error) {
	if len(key) < minSecretBytes {
		return nil, fmt.Errorf("trove/contract: ticket key must be at least %d bytes, got %d", minSecretBytes, len(key))
	}
	k := make([]byte, len(key))
	copy(k, key)
	return &Signer{key: k, now: time.Now}, nil
}

// NewRandomSigner returns a Signer over a fresh random key. Its tickets
// verify only in this process, so a deployment with more than one instance
// needs a configured key instead.
func NewRandomSigner() (*Signer, error) {
	key := make([]byte, minSecretBytes)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("trove/contract: generate ticket key: %w", err)
	}
	return NewSigner(key)
}

// Issue signs t with an expiry ttl from now, and returns the token and that
// expiry. Expiry is kept to the second, because that is what the token
// carries.
func (s *Signer) Issue(t Ticket, ttl time.Duration) (string, time.Time, error) {
	expires := s.now().Add(ttl).Truncate(time.Second)
	t.Expires = expires.Unix()
	payload, err := json.Marshal(t)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("trove/contract: encode ticket: %w", err)
	}
	body := base64.RawURLEncoding.EncodeToString(payload)
	return body + "." + base64.RawURLEncoding.EncodeToString(s.mac(body)), expires, nil
}

// Verify checks token's signature and expiry and returns the ticket it
// carries. A ticket is expired from its expiry second onwards.
func (s *Signer) Verify(token string) (Ticket, error) {
	body, sig, ok := strings.Cut(token, ".")
	if !ok || body == "" || sig == "" {
		return Ticket{}, ErrTicketInvalid
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil || !hmac.Equal(got, s.mac(body)) {
		return Ticket{}, ErrTicketInvalid
	}
	payload, err := base64.RawURLEncoding.DecodeString(body)
	if err != nil {
		return Ticket{}, ErrTicketInvalid
	}
	var t Ticket
	if err := json.Unmarshal(payload, &t); err != nil || t.Op == "" || t.Bucket == "" || t.Key == "" {
		return Ticket{}, ErrTicketInvalid
	}
	if s.now().Unix() >= t.Expires {
		return Ticket{}, ErrTicketExpired
	}
	return t, nil
}

func (s *Signer) mac(body string) []byte {
	h := hmac.New(sha256.New, s.key)
	h.Write([]byte(body))
	return h.Sum(nil)
}
