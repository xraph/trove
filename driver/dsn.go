package driver

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// DSNConfig holds parsed DSN components.
type DSNConfig struct {
	// Scheme is the protocol scheme (e.g., "s3", "gcs", "file", "mem").
	Scheme string

	// User is the username or access key.
	User string

	// Password is the password or secret key.
	Password string

	// Host is the hostname and optional port.
	Host string

	// Path is the path component (e.g., bucket, directory).
	Path string

	// Params holds query string parameters.
	Params url.Values
}

// ParseDSN parses a DSN string into its components.
//
// Supported formats:
//
//	scheme://host/path?params
//	scheme://user:password@host/path?params
//	scheme:///path (host-less, e.g., file:///tmp/storage)
//
// Its errors never quote the DSN, or any part of it but the scheme. A DSN
// carries credentials (an access key and secret in the userinfo, an account
// key or a connection string in the query), and an error travels into logs
// and onto trace spans: forge records a failed Register's error text on its
// lifecycle span. An error names the scheme when the DSN starts with a valid
// one followed by "://", and says what kind of problem it found.
func ParseDSN(dsn string) (*DSNConfig, error) {
	if dsn == "" {
		return nil, errors.New("driver: empty DSN")
	}

	u, err := url.Parse(dsn)
	if err != nil {
		return nil, fmt.Errorf("driver: parse DSN%s: %s", schemeHint(dsn), parseProblem(err))
	}

	if u.Scheme == "" {
		return nil, errors.New("driver: DSN missing scheme")
	}

	// A DSN with no "//" after the colon parses as an opaque URL. No driver
	// accepts one, and the text before the colon may not be a scheme at all:
	// "KEY:SECRET@region/bucket" with the s3:// left off parses with the
	// access key as its scheme. Reject it without naming that scheme.
	if u.Opaque != "" {
		return nil, errors.New("driver: DSN must have the form scheme://host/path")
	}

	cfg := &DSNConfig{
		Scheme: u.Scheme,
		Host:   u.Host,
		Path:   u.Path,
		Params: u.Query(),
	}

	if u.User != nil {
		cfg.User = u.User.Username()
		cfg.Password, _ = u.User.Password()
	}

	return cfg, nil
}

// schemeHint returns ` for scheme "s3"` when dsn starts with a valid scheme
// followed by "://", and "" otherwise. The scheme is the one part of a DSN
// that can never hold a credential: userinfo only comes after the "//".
func schemeHint(dsn string) string {
	scheme, _, ok := strings.Cut(dsn, "://")
	if !ok || !validScheme(scheme) {
		return ""
	}
	return fmt.Sprintf(" for scheme %q", scheme)
}

// validScheme reports whether s matches RFC 3986's scheme grammar:
// a letter, then letters, digits, "+", "-" or ".".
func validScheme(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z':
		case i > 0 && ('0' <= c && c <= '9' || c == '+' || c == '-' || c == '.'):
		default:
			return false
		}
	}
	return true
}

// parseProblem names what url.Parse objected to without repeating any of
// the input. A *url.Error quotes the whole URL, and the errors it wraps quote
// the offending fragment (a port, an escape sequence, a host character), any
// of which may be part of a password. Only messages that quote nothing pass
// through as they are.
func parseProblem(err error) string {
	var ue *url.Error
	if errors.As(err, &ue) && ue.Err != nil {
		err = ue.Err
	}

	var escErr url.EscapeError
	if errors.As(err, &escErr) {
		return "invalid percent-escape"
	}
	var hostErr url.InvalidHostError
	if errors.As(err, &hostErr) {
		return "invalid character in host"
	}

	msg := err.Error()
	switch {
	case strings.HasPrefix(msg, "invalid port "):
		return "invalid port"
	case msg == "net/url: invalid userinfo",
		msg == "net/url: invalid control character in URL",
		msg == "missing protocol scheme",
		msg == "first path segment in URL cannot contain colon",
		msg == "invalid IP-literal",
		msg == "missing ']' in host":
		return msg
	default:
		return "malformed URL"
	}
}
