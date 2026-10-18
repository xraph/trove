package driver_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/trove/driver"
)

func TestParseDSN_Valid(t *testing.T) {
	cfg, err := driver.ParseDSN("s3://AKIDEXAMPLE:wJalrXUtnFEMI@us-east-1/bucket?endpoint=http://localhost:9000")
	require.NoError(t, err)
	assert.Equal(t, "s3", cfg.Scheme)
	assert.Equal(t, "AKIDEXAMPLE", cfg.User)
	assert.Equal(t, "wJalrXUtnFEMI", cfg.Password)
	assert.Equal(t, "us-east-1", cfg.Host)
	assert.Equal(t, "/bucket", cfg.Path)
	assert.Equal(t, "http://localhost:9000", cfg.Params.Get("endpoint"))

	cfg, err = driver.ParseDSN("file:///tmp/storage")
	require.NoError(t, err)
	assert.Equal(t, "file", cfg.Scheme)
	assert.Equal(t, "/tmp/storage", cfg.Path)
}

// TestParseDSN_ErrorsCarryNoCredentials feeds DSNs that url.Parse rejects,
// each holding an access key, a secret or an account key, and checks that
// none of it reaches the error text. forge records a failed Register's
// error on its lifecycle span, and the extension's Register wraps this one.
func TestParseDSN_ErrorsCarryNoCredentials(t *testing.T) {
	const (
		accessKey = "AKIDEXAMPLE"
		secret    = "wJalrXUtnFEMI"
		account   = "acctsecretname"
		accKey    = "c2VjcmV0LWFjY291bnQta2V5"
		connStr   = "DefaultEndpointsProtocol=https;AccountName=" + account + ";AccountKey=" + accKey + ";EndpointSuffix=core.windows.net"
		sasToken  = "sv=2024-11-04&sig=c2FzLXNpZ25hdHVyZQ"
	)

	cases := []struct {
		name string
		dsn  string
		want string
	}{
		{
			name: "bad escape in the password",
			dsn:  "s3://" + accessKey + ":" + secret + "%zz@us-east-1/bucket",
			want: `driver: parse DSN for scheme "s3": invalid percent-escape`,
		},
		{
			name: "slash in the password moves it into the port",
			dsn:  "s3://" + accessKey + ":" + secret + "/x@us-east-1/bucket",
			want: `driver: parse DSN for scheme "s3": invalid port`,
		},
		{
			name: "space in the host",
			dsn:  "sftp://" + accessKey + ":" + secret + "@ho st:22/base",
			want: `driver: parse DSN for scheme "sftp": invalid character in host`,
		},
		{
			name: "control character",
			dsn:  "azure://" + account + "/container?key=" + accKey + "\x7f",
			want: `driver: parse DSN for scheme "azure": net/url: invalid control character in URL`,
		},
		{
			name: "connection string, with a bad port in front of it",
			dsn:  "azure://" + account + ":10o00/container?connection_string=" + connStr,
			want: `driver: parse DSN for scheme "azure": invalid port`,
		},
		{
			name: "connection string with a control character",
			dsn:  "azure://" + account + "/container?connection_string=" + connStr + ";SharedAccessSignature=" + sasToken + "\x00",
			want: `driver: parse DSN for scheme "azure": net/url: invalid control character in URL`,
		},
		{
			name: "missing scheme",
			dsn:  "//" + accessKey + ":" + secret + "@us-east-1/bucket",
			want: "driver: DSN missing scheme",
		},
		{
			name: "scheme left off, so the access key parses as one",
			dsn:  accessKey + ":" + secret + "@us-east-1/bucket",
			want: "driver: DSN must have the form scheme://host/path",
		},
		{
			name: "bracketed host that is not an address",
			dsn:  "s3://" + accessKey + ":" + secret + "@[" + account + "]/bucket",
			want: `driver: parse DSN for scheme "s3": malformed URL`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := driver.ParseDSN(tc.dsn)
			require.Error(t, err)
			assert.Equal(t, tc.want, err.Error())
			for _, s := range []string{accessKey, strings.ToLower(accessKey), secret, account, accKey, "c2FzLXNpZ25hdHVyZQ", "AccountKey"} {
				assert.NotContains(t, err.Error(), s)
			}
		})
	}
}
