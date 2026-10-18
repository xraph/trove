package extension

import (
	"strings"
	"testing"

	"github.com/xraph/forge"
	"go.opentelemetry.io/otel"
)

// TestRegisterErrorCarriesNoCredentials checks that a store whose DSN does
// not parse fails Register with an error that holds none of the DSN's
// credentials. forge records a failed Register's error text on its
// "register trove" lifecycle span, so whatever this error says is exported.
func TestRegisterErrorCarriesNoCredentials(t *testing.T) {
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	const (
		user   = "storage-admin"
		secret = "hunter2-very-secret"
	)
	dsns := []string{
		"file://" + user + ":" + secret + "%zz@localhost/data",
		user + ":" + secret + "@localhost/data",
		"sftp://" + user + ":" + secret + "/x@host:22/base",
	}

	for _, dsn := range dsns {
		app := forge.New(forge.WithAppName("trove-dsn-test"), forge.WithAppLogger(forge.NewNoopLogger()))
		err := New(WithFileStoreDSN("primary", dsn), WithDisableMigrate()).Register(app)
		if err == nil {
			t.Fatalf("Register accepted a DSN that does not parse")
		}
		for _, s := range []string{user, secret} {
			if strings.Contains(err.Error(), s) {
				t.Errorf("Register error quotes %q: %v", s, err)
			}
		}
	}
}
