package contract

import "encoding/base64"

// encodeCursor wraps a driver's NextToken for the wire. On mem, local and
// sftp the token is a raw key, and JSON would mangle a key that is not
// valid UTF-8; base64url survives. It also keeps anyone from hand-editing a
// key into a URL. An empty token means the listing is complete: null.
func encodeCursor(token string) *string {
	if token == "" {
		return nil
	}
	s := base64.RawURLEncoding.EncodeToString([]byte(token))
	return &s
}

// decodeCursor unwraps a cursor the client passed back. A cursor that does
// not decode was not one this package issued, and is refused here, before
// it reaches a backend that would answer with an untyped error.
func decodeCursor(cursor string) (string, error) {
	if cursor == "" {
		return "", nil
	}
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", badRequest("cursor is malformed. Pass back nextCursor exactly as you received it.")
	}
	return string(raw), nil
}
