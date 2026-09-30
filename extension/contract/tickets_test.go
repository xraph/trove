package contract

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"
)

func newTestSigner(t *testing.T) *Signer {
	t.Helper()
	s, err := NewSigner(bytes.Repeat([]byte("s"), 32))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	return s
}

func TestNewSigner_RejectsShortKey(t *testing.T) {
	if _, err := NewSigner(bytes.Repeat([]byte("s"), 31)); err == nil {
		t.Fatal("a 31-byte key was accepted")
	}
}

func TestNewRandomSigner_SignsAndVerifies(t *testing.T) {
	s, err := NewRandomSigner()
	if err != nil {
		t.Fatalf("NewRandomSigner: %v", err)
	}
	tok, _, err := s.Issue(Ticket{Store: "default", Bucket: "b", Key: "k", Op: OpDownload}, time.Minute)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := s.Verify(tok); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestSigner_RoundTrip(t *testing.T) {
	s := newTestSigner(t)
	in := Ticket{
		Store: "default", Bucket: "reports", Key: "2026/q3 résumé #1.pdf", Op: OpUpload,
		Subject: "user_1", Size: 42, ContentType: "application/pdf", Overwrite: true, Limit: 7,
	}
	tok, expires, err := s.Issue(in, UploadTicketTTL)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	got, err := s.Verify(tok)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	in.Expires = expires.Unix()
	if got != in {
		t.Fatalf("ticket = %+v, want %+v", got, in)
	}
}

func TestSigner_Expired(t *testing.T) {
	s := newTestSigner(t)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	s.now = func() time.Time { return now }
	tok, _, err := s.Issue(Ticket{Store: "default", Bucket: "b", Key: "k", Op: OpDownload}, DownloadTicketTTL)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	now = now.Add(DownloadTicketTTL)
	if _, err := s.Verify(tok); !errors.Is(err, ErrTicketExpired) {
		t.Fatalf("Verify at expiry = %v, want ErrTicketExpired", err)
	}
}

func TestSigner_TamperedPayloadIsInvalid(t *testing.T) {
	s := newTestSigner(t)
	tok, _, err := s.Issue(Ticket{Store: "default", Bucket: "b", Key: "k", Op: OpDownload}, time.Minute)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	_, sig, _ := strings.Cut(tok, ".")
	forged := base64.RawURLEncoding.EncodeToString([]byte(`{"s":"default","b":"b","k":"other","o":"download","e":9999999999}`))
	if _, err := s.Verify(forged + "." + sig); !errors.Is(err, ErrTicketInvalid) {
		t.Fatalf("Verify of a forged payload = %v, want ErrTicketInvalid", err)
	}
}

func TestSigner_TamperedSignatureIsInvalid(t *testing.T) {
	s := newTestSigner(t)
	tok, _, err := s.Issue(Ticket{Store: "default", Bucket: "b", Key: "k", Op: OpDownload}, time.Minute)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	body, _, _ := strings.Cut(tok, ".")
	bad := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0}, 32))
	if _, err := s.Verify(body + "." + bad); !errors.Is(err, ErrTicketInvalid) {
		t.Fatalf("Verify of a bad signature = %v, want ErrTicketInvalid", err)
	}
}

func TestSigner_OtherKeyIsInvalid(t *testing.T) {
	a := newTestSigner(t)
	b, err := NewSigner(bytes.Repeat([]byte("x"), 32))
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	tok, _, err := a.Issue(Ticket{Store: "default", Bucket: "b", Key: "k", Op: OpDownload}, time.Minute)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := b.Verify(tok); !errors.Is(err, ErrTicketInvalid) {
		t.Fatalf("Verify under another key = %v, want ErrTicketInvalid", err)
	}
}

func TestSigner_MalformedTokensAreInvalid(t *testing.T) {
	s := newTestSigner(t)
	for _, tok := range []string{"", "abc", "abc.", ".abc", "!!!.!!!", "a.b.c"} {
		if _, err := s.Verify(tok); !errors.Is(err, ErrTicketInvalid) {
			t.Errorf("Verify(%q) = %v, want ErrTicketInvalid", tok, err)
		}
	}
}

func TestSigner_TicketWithoutOperationIsInvalid(t *testing.T) {
	s := newTestSigner(t)
	tok, _, err := s.Issue(Ticket{Store: "default", Bucket: "b", Key: "k"}, time.Minute)
	if err != nil {
		t.Fatalf("Issue: %v", err)
	}
	if _, err := s.Verify(tok); !errors.Is(err, ErrTicketInvalid) {
		t.Fatalf("Verify of a ticket with no op = %v, want ErrTicketInvalid", err)
	}
}
