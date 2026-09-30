package contract

import (
	"testing"

	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
)

func TestCursor_RoundTrip(t *testing.T) {
	for _, raw := range []string{"a/", "logs/2026/x.json", "opaque-token-2", "bad\xffutf8"} {
		enc := encodeCursor(raw)
		if enc == nil {
			t.Fatalf("encodeCursor(%q) = nil", raw)
		}
		got, err := decodeCursor(*enc)
		if err != nil || got != raw {
			t.Fatalf("decodeCursor(encodeCursor(%q)) = %q, %v", raw, got, err)
		}
	}
}

func TestCursor_EmptyTokenIsNull(t *testing.T) {
	if encodeCursor("") != nil {
		t.Fatal("an empty token produced a cursor")
	}
	got, err := decodeCursor("")
	if err != nil || got != "" {
		t.Fatalf("decodeCursor(\"\") = %q, %v", got, err)
	}
}

func TestCursor_MalformedIsBadRequest(t *testing.T) {
	for _, c := range []string{"!!!", "a b", "abc=="} {
		if _, err := decodeCursor(c); codeOf(err) != dashcontract.CodeBadRequest {
			t.Errorf("decodeCursor(%q) = %v, want BAD_REQUEST", c, err)
		}
	}
}
