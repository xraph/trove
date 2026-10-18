<p align="center">
  <img src="assets/readme-banner.svg" alt="Trove, a Go object storage library for local disk, S3, Google Cloud Storage, Azure Blob Storage and SFTP" width="100%">
</p>

# Trove: object storage for Go over S3, GCS, Azure Blob and local disk

Trove is an open-source Go library for object and file storage. You write against one API (`Put`, `Get`, `List`, `Copy`, buckets) and pick the backend at startup: local disk, memory, Amazon S3 and S3-compatible stores like MinIO, Google Cloud Storage, Azure Blob Storage or SFTP. On top of that you get a middleware pipeline for encryption, compression, virus scanning and deduplication, routing of keys to different backends, content-addressable storage, and an `io/fs` view of a bucket. It runs on its own or as a [Forge](https://github.com/xraph/forge) extension.

[![Go 1.25+](https://img.shields.io/badge/Go-1.25%2B-00ADD8?logo=go&logoColor=white)](https://go.dev/dl/) [![CI](https://github.com/xraph/trove/actions/workflows/ci.yml/badge.svg)](https://github.com/xraph/trove/actions/workflows/ci.yml) [![Go Reference](https://pkg.go.dev/badge/github.com/xraph/trove.svg)](https://pkg.go.dev/github.com/xraph/trove) [![GitHub Stars](https://img.shields.io/github/stars/xraph/trove?style=flat)](https://github.com/xraph/trove/stargazers)

[Documentation](https://www.xraph.com/docs/trove) · [Quick start](#quick-start) · [Drivers](#storage-drivers) · [Middleware](#middleware) · [Routing](#route-keys-to-different-backends) · [CAS and VFS](#content-addressable-storage) · [Forge](#use-it-with-forge) · [FAQ](#faq)

## Quick start

You need [Go 1.25 or later](https://go.dev/dl/). The local disk driver lives in the root module, so one `go get` is enough to try it:

```bash
mkdir hello-trove
cd hello-trove
go mod init example.com/hello-trove
go get github.com/xraph/trove@latest
```

Save this as `main.go`:

```go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/drivers/localdriver"
)

func main() {
	ctx := context.Background()

	local := localdriver.New()
	if err := local.Open(ctx, "local://./storage"); err != nil {
		log.Fatal(err)
	}

	t, err := trove.Open(local, trove.WithDefaultBucket("uploads"))
	if err != nil {
		log.Fatal(err)
	}
	defer t.Close(ctx)

	if err := t.CreateBucket(ctx, "uploads"); err != nil && !errors.Is(err, trove.ErrBucketExists) {
		log.Fatal(err)
	}

	info, err := t.Put(ctx, "uploads", "notes/hello.txt", strings.NewReader("Hello, Trove!"),
		driver.WithContentType("text/plain"))
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("stored", info.Key, info.Size, "bytes")

	obj, err := t.Get(ctx, "uploads", "notes/hello.txt")
	if err != nil {
		log.Fatal(err)
	}
	defer obj.Close()

	body, err := io.ReadAll(obj)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println(string(body))

	it, err := t.List(ctx, "uploads", driver.WithPrefix("notes/"))
	if err != nil {
		log.Fatal(err)
	}
	objects, err := it.All(ctx)
	if err != nil {
		log.Fatal(err)
	}
	for _, o := range objects {
		fmt.Println("listed", o.Key)
	}
}
```

Run it:

```bash
go run .
# stored notes/hello.txt 13 bytes
# Hello, Trove!
# listed notes/hello.txt
```

Look in `./storage/uploads/notes/` and you'll find `hello.txt` next to a `hello.txt.meta.json` sidecar, which is where the local driver keeps the content type and metadata. To move the same code to S3, import `github.com/xraph/trove/drivers/s3driver`, call `s3driver.New()` and open it with an `s3://` DSN. Nothing else changes.

## Why Trove?

If you've written an upload service, you've probably written the same wrapper twice: once over the AWS SDK for production and once over the filesystem for tests and local dev. Trove is that wrapper, with the parts you'd bolt on later (encryption at rest, compression, scanning, routing old files to cheap storage) already built as middleware you can scope to a bucket or a key pattern.

| You need | Trove gives you |
| --- | --- |
| One storage API across clouds | `Put`, `Get`, `Head`, `Delete`, `List`, `Copy` and bucket calls on every driver |
| Fast tests without a cloud account | `memdriver` and `localdriver` in the root module, plus `trovetest.RunDriverSuite` for driver conformance |
| Large uploads and direct browser downloads | Multipart uploads, pre-signed URLs and range reads on the S3, GCS and Azure drivers |
| Encryption at rest | AES-256-GCM middleware with a pluggable key provider |
| Smaller objects | Zstandard compression middleware with a minimum size and extension exclusions |
| Malware checks on upload | A scan middleware with a ClamAV provider |
| Hot and cold storage in one app | Routing rules that send keys to named backends by glob pattern or function |
| Deduplicated content | Content-addressable storage keyed by SHA-256, BLAKE3 or XXHash, with pinning and GC |
| A filesystem view | `Stat`, `ReadDir`, `Walk`, `Open`, `Create` and `Rename` over a bucket, plus an `io/fs.FS` adapter for `http.FileServer` |
| To see what storage is doing | An OpenTelemetry span per operation and six metrics, with no keys, bucket names or error text on either |

The [architecture guide](https://www.xraph.com/docs/trove/architecture) shows how the facade, the router, the middleware resolver and the drivers fit together.

## Storage drivers

`localdriver` and `memdriver` ship inside the root module. The cloud and SFTP drivers are separate Go modules, so you only pull the AWS, Google or Azure SDK into your build when you import that driver, and a service that only ever writes to local disk keeps a short `go.sum`.

| Backend | Package | DSN | Extra capabilities | Docs |
| --- | --- | --- | --- | --- |
| Local disk | `github.com/xraph/trove/drivers/localdriver` | `file:///abs/path`, `local://./rel/path` | | [Local driver](https://www.xraph.com/docs/trove/storage/drivers/local) |
| Memory | `github.com/xraph/trove/drivers/memdriver` | `mem://` | | [Memory driver](https://www.xraph.com/docs/trove/storage/drivers/memory) |
| Amazon S3, MinIO | `github.com/xraph/trove/drivers/s3driver` | `s3://REGION/BUCKET`, with `?endpoint=...&path_style=true` for MinIO | multipart, presign, range | [S3 driver](https://www.xraph.com/docs/trove/storage/drivers/s3) |
| Google Cloud Storage | `github.com/xraph/trove/drivers/gcsdriver` | `gcs://PROJECT_ID/BUCKET` | multipart, presign, range | [GCS driver](https://www.xraph.com/docs/trove/storage/drivers/gcs) |
| Azure Blob Storage | `github.com/xraph/trove/drivers/azuredriver` | `azure://ACCOUNT_NAME/CONTAINER?key=ACCESS_KEY` | multipart, presign, range | [Azure driver](https://www.xraph.com/docs/trove/storage/drivers/azure) |
| SFTP | `github.com/xraph/trove/drivers/sftpdriver` | `sftp://user:pass@host:22/basepath` | | [SFTP driver](https://www.xraph.com/docs/trove/storage/drivers/sftp) |

Not every backend can do everything. Capabilities are separate interfaces in the `driver` package, and you check for one with a type assertion:

```go
if p, ok := t.Driver().(driver.PresignDriver); ok {
	url, err := p.PresignGet(ctx, "uploads", "report.pdf", 15*time.Minute)
	// ...
}
```

Errors are classified the same way across drivers. `errors.Is(err, trove.ErrObjectNotFound)` works whether the object was missing on disk or in S3, and `trove.Permanent(err)` tells you whether a retry has any chance of working.

## Middleware

Middleware wraps the write path, the read path, or both. You can register it for everything, or give it a scope so it only touches the buckets and keys you choose:

```go
t, err := trove.Open(drv,
	trove.WithMiddleware(encrypt.New(
		encrypt.WithKeyProvider(encrypt.NewStaticKeyProvider(key)),
	)),
	trove.WithScopedMiddleware(
		&middleware.ScopeBucket{Buckets: []string{"logs"}},
		compress.New(compress.WithMinSize(4096)),
	),
)
```

| Package | What it does |
| --- | --- |
| `middleware/encrypt` | AES-256-GCM. The key comes from a `KeyProvider`, so you can serve it from a secrets manager |
| `middleware/compress` | Zstandard, skipping objects under `WithMinSize` and extensions in `WithExclude` |
| `middleware/scan` | Scans writes through a `Provider`. `scan.NewClamAVProvider(addr)` talks to clamd |
| `middleware/dedup` | Detects duplicate content by hash, with an in-memory store or your own |
| `middleware/watermark` | Writes a text mark into file metadata: PNG `tEXt`, JPEG `COM` and the PDF info dictionary, without image processing |

Scopes are `ScopeBucket`, `ScopeKeyPattern`, `ScopeContentType` and `ScopeFunc`, and `ScopeAnd`, `ScopeOr` and `ScopeNot` combine them. You can also add and remove middleware at runtime with `UseMiddleware` and `RemoveMiddleware`. One thing to plan for: the middleware buffers the whole object in memory while it works, so if you accept uploads of a few gigabytes through an encrypted or compressed path, size your memory and your upload limits for that. The [middleware guide](https://www.xraph.com/docs/trove/storage/middleware) has the details.

## Route keys to different backends

One `Trove` can hold several named backends. Routing rules decide which one a key lands on, and anything no rule claims goes to the driver you passed to `Open`:

```go
t, err := trove.Open(primary,
	trove.WithBackend("archive", archive),
	trove.WithRoute("*.log", "archive"),
	trove.WithRouteFunc(func(bucket, key string) string {
		if strings.HasPrefix(key, "tmp/") {
			return "archive"
		}
		return ""
	}),
)
```

Patterns use `filepath.Match`, so `*` stops at a `/` and `*.log` matches `app.log` but not `2026/app.log`. `t.Backend("archive")` gives you a `Trove` bound to that backend if you need to reach it directly. See [multi-backend routing](https://www.xraph.com/docs/trove/concepts/multi-backend-routing).

## Content-addressable storage

Turn on CAS with `trove.WithCAS(cas.AlgSHA256)` (or `cas.AlgBlake3`, `cas.AlgXXHash`) and `t.CAS()` stores content under its hash. Writing the same bytes twice keeps one copy:

```go
hash, info, err := t.CAS().Store(ctx, strings.NewReader("same bytes, stored once"))
```

`Retrieve`, `Exists` and `Stat` read it back, `Pin` and `Unpin` protect an entry, and `GC` removes what nothing references. More in the [CAS docs](https://www.xraph.com/docs/trove/storage/cas).

## Virtual filesystem

An object store has no folders, only keys. `t.VFS("uploads")` gives you a directory-shaped view anyway, built from the `/` in your keys, and `vfs.NewIOFS` adapts it to `io/fs.FS` so the standard library can serve it:

```go
fsys := t.VFS("uploads")
entries, err := fsys.ReadDir(ctx, "notes")

http.Handle("/files/", http.StripPrefix("/files/", http.FileServer(http.FS(vfs.NewIOFS(ctx, fsys)))))
```

The `stream` package handles chunked transfers with backpressure, resumable streams and progress hooks. Start with the [VFS docs](https://www.xraph.com/docs/trove/storage/vfs) and the [streaming engine](https://www.xraph.com/docs/trove/storage/streaming-engine).

## Use it with Forge

The `github.com/xraph/trove/extension` module plugs Trove into a [Forge](https://github.com/xraph/forge) app. It opens the storage driver from a DSN during `Register`, creates the default bucket, provides `*trove.Trove` through Forge's DI container, reports health and closes the driver on shutdown. It needs Go 1.26, the same as Forge.

```bash
go get github.com/xraph/trove/extension@latest
go get github.com/xraph/trove/drivers/s3driver@latest
```

```go
import (
	"github.com/xraph/forge"
	"github.com/xraph/trove"
	_ "github.com/xraph/trove/drivers/s3driver"
	troveext "github.com/xraph/trove/extension"
)

app := forge.NewApp(forge.AppConfig{
	Name:        "files",
	HTTPAddress: ":8080",
	Extensions: []forge.Extension{
		troveext.New(troveext.WithConfig(troveext.Config{
			StorageDriver: "s3://us-east-1/my-bucket",
			DefaultBucket: "uploads",
		})),
	},
})

err := app.Router().POST("/notes", func(ctx forge.Context) error {
	t, err := forge.Inject[*trove.Trove](app.Container())
	if err != nil {
		return err
	}

	info, err := t.Put(ctx.Context(), "uploads", "notes/hello.txt", strings.NewReader("hello"))
	if err != nil {
		return err
	}

	return ctx.JSON(http.StatusCreated, info)
})
```

The extension registers the `file`, `local` and `mem` schemes itself. For S3, GCS, Azure or SFTP you blank-import the driver package so its scheme is registered. With no `storage_driver` at all, you get an in-memory store, which is handy in tests and a surprise in production.

If the container holds a `*grove.DB` (from the [Grove](https://github.com/xraph/grove) extension, on Postgres, SQLite or MongoDB), the Trove extension keeps object and bucket metadata there and runs its own migrations during `Register`. It looks the database up while it registers, so list the Grove extension before Trove. It also registers a contributor for the [Forge Dashboard](https://github.com/xraph/forge-dashboard), with a content route at `/dashboard/trove/content` that serves previews, downloads and uploads through short-lived signed tickets.

The REST handlers in `extension/handler` (buckets, objects, multipart uploads, CAS) form a plain `http.Handler` that you build with `handler.New` and mount yourself. Integrations with [Chronicle](https://github.com/xraph/chronicle), [Vault](https://github.com/xraph/vault), [Warden](https://github.com/xraph/warden) and [Dispatch](https://github.com/xraph/dispatch) are in `extension/hooks`, and you wire those yourself too.

Telemetry needs no wiring. Every storage operation records a span under your request's span, and the extension puts `forge_trove_operation_duration_seconds`, `forge_trove_object_bytes_total` and the scan and CAS counters on `/_/metrics`. Outside Forge, pass an `observe.Recorder` to `trove.WithMetrics`. The [observability page](https://www.xraph.com/docs/trove/integration/observability) lists every span and metric.

### Configuration

The extension reads `extensions.trove` from your Forge config, then falls back to a top-level `trove` key. YAML wins for string fields, and a `true` you set in code turns a flag on even when YAML leaves it off.

```yaml
extensions:
  trove:
    storage_driver: s3://us-east-1/my-bucket
    default_bucket: uploads
    enable_compression: true
```

| Key | Option | Default | What it does |
| --- | --- | --- | --- |
| `storage_driver` | `WithConfig` | in-memory | Storage DSN, resolved through the driver registry |
| `default_bucket` | `WithConfig` | `default` | Bucket used when you don't name one, created at startup |
| `grove_database` | `WithGroveDatabase(name)` | unnamed `*grove.DB` | Which named Grove database holds the metadata |
| `disable_migrate` | `WithDisableMigrate()` | `false` | Skips the metadata migrations |
| `enable_compression` | `WithConfig` | `false` | Adds the Zstandard middleware |
| `enable_cas` | `WithConfig` | `false` | Turns on CAS with SHA-256 |
| `stores` (list of `name`, `storage_driver`, `grove_database`, `default_bucket`, `enable_cas`, `enable_compression`) | `WithFileStore`, `WithFileStoreDSN` | | Several named stores, each provided by name, plus a `*TroveManager` |
| `default` | `WithDefaultFileStore(name)` | first store | The store you get as the unnamed `*trove.Trove` |
| `dashboard_content_path` | `WithConfig` | `/dashboard/trove/content` | Where the dashboard content route mounts |
| `dashboard_max_upload_bytes` | `WithConfig` | 64 MiB | Largest upload the dashboard accepts |
| `dashboard_content_secret` | `WithConfig` | random per process | Signs content links, at least 32 bytes. Set it when you run more than one instance |

`WithTroveOption` and `WithTroveOptionFor(store, opt)` pass any `trove.Option`, such as your own middleware or routing rules, through to the instances the extension builds. The [Forge extension guide](https://www.xraph.com/docs/trove/integration/forge-extension) and the [configuration reference](https://www.xraph.com/docs/trove/integration/configuration) go further.

## Documentation

| Topic | Docs |
| --- | --- |
| Install and store a first object | [Getting started](https://www.xraph.com/docs/trove/getting-started), [basic storage](https://www.xraph.com/docs/trove/guides/basic-storage) |
| Pick and configure a backend | [Driver system](https://www.xraph.com/docs/trove/concepts/driver-system), [capabilities](https://www.xraph.com/docs/trove/concepts/capabilities), [driver interface](https://www.xraph.com/docs/trove/storage/driver-interface) |
| Encrypt, compress or scan objects | [Middleware pipeline](https://www.xraph.com/docs/trove/concepts/middleware-pipeline), [middleware setup](https://www.xraph.com/docs/trove/guides/middleware-setup) |
| Split storage across backends | [Multi-backend setup](https://www.xraph.com/docs/trove/guides/multi-backend-setup) |
| Upload large files | [Streaming uploads](https://www.xraph.com/docs/trove/guides/streaming-uploads) |
| Handle errors and tenants | [Errors](https://www.xraph.com/docs/trove/concepts/errors), [multi-tenancy](https://www.xraph.com/docs/trove/concepts/multi-tenancy) |
| Run it inside Forge | [Forge extension](https://www.xraph.com/docs/trove/integration/forge-extension), [Grove store](https://www.xraph.com/docs/trove/integration/grove-store), [dashboard](https://www.xraph.com/docs/trove/integration/dashboard) |
| Trace and measure it | [Observability](https://www.xraph.com/docs/trove/integration/observability) |

The exported Go API is on [pkg.go.dev](https://pkg.go.dev/github.com/xraph/trove), and the docs source is in [`docs/`](docs/).

## FAQ

### How do I use S3 and local storage with the same Go code?

Write your code against `*trove.Trove` and choose the driver when you start up: `localdriver` with a `file://` DSN in development, `s3driver` with an `s3://` DSN in production. The calls and the error values stay the same.

### Does Trove work with MinIO, Cloudflare R2 or other S3-compatible storage?

The S3 driver takes an `endpoint` and `path_style` parameter in its DSN, as in `s3://KEY:SECRET@us-east-1/bucket?endpoint=http://localhost:9000&path_style=true`, and the integration tests run against MinIO. Other S3-compatible services work the same way if they accept the S3 API at a custom endpoint.

### How do I encrypt files at rest in Go?

Add `encrypt.New(encrypt.WithKeyProvider(...))` with `trove.WithMiddleware`. Objects are sealed with AES-256-GCM before they reach the driver and opened again on `Get`, and the key provider can fetch the 32-byte key from wherever you keep secrets.

### Can I use Trove without Forge?

Yes. The root module has no xraph dependencies at all, so you can drop it into any Go service, and you only need Forge if you import `extension`.

## Contributing

Issues and focused pull requests are welcome on [GitHub](https://github.com/xraph/trove/issues). Build with `GOWORK=off`, and test each module from its own directory, since `./...` stops at module boundaries:

```bash
git clone https://github.com/xraph/trove.git
cd trove
GOWORK=off go build ./...
make test-race
make lint
(cd extension && GOWORK=off go test -race -count=1 ./...)
(cd drivers/s3driver && GOWORK=off go test -race -count=1 ./...)
```

The `extension`, `gcsdriver` and `sftpdriver` modules need Go 1.26. The driver integration tests sit behind the `integration` build tag and need a local MinIO, Azurite, fake-gcs-server or SFTP container, which the top of each `integration_test.go` shows how to start. Benchmarks need `-tags bench`. Several `-all` Makefile targets don't work in this repo, and [AGENTS.md](AGENTS.md) lists which ones, along with the CI jobs and the release workflow. Commits follow [Conventional Commits](https://www.conventionalcommits.org/).

## License

See the [Trove repository](https://github.com/xraph/trove) for license terms.

Built by [Rex Raphael](https://github.com/juicycleff) and [contributors](https://github.com/xraph/trove/graphs/contributors), as part of the [Forge](https://github.com/xraph/forge) forgery stack.
