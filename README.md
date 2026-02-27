# Trove

A multi-backend object storage engine for Go with composable middleware, streaming, content-addressable storage, and virtual filesystem support.

## Features

- **Polyglot Backends** -- Unified API across local, in-memory, S3, GCS, Azure, and SFTP storage
- **Composable Middleware** -- Stackable read/write interceptors for encryption, compression, scanning, and watermarking with fine-grained scoping
- **Multi-Backend Routing** -- Route objects to different backends via glob patterns or custom functions
- **Streaming** -- Chunked transfers with backpressure, resumability, pause/resume, and lifecycle hooks
- **Content-Addressable Storage** -- Deduplication via SHA-256, BLAKE3, or XXHash with reference counting and garbage collection
- **Virtual Filesystem** -- `io/fs.FS`-compatible hierarchical view over flat object storage
- **Capability Interfaces** -- Opt-in driver features: multipart uploads, pre-signed URLs, versioning, server-side copy, range reads, lifecycle rules, and change notifications

## Supported Backends

| Driver | Description |
|--------|-------------|
| **localdriver** | Local filesystem with sidecar `.meta.json` metadata |
| **memdriver** | In-memory storage for testing |
| **s3driver** | AWS S3, MinIO, R2, DigitalOcean Spaces |
| **gcsdriver** | Google Cloud Storage |
| **azuredriver** | Azure Blob Storage |
| **sftpdriver** | SFTP remote storage |

## Quick Start

```go
// Create a driver and open it
drv := localdriver.New()
drv.Open(ctx, "file:///storage")

// Create a Trove instance
t, _ := trove.Open(drv,
    trove.WithDefaultBucket("uploads"),
    trove.WithChunkSize(8 * 1024 * 1024),
)
defer t.Close(ctx)

// Store an object
t.Put(ctx, "uploads", "photo.jpg", reader)

// Retrieve an object
r, _ := t.Get(ctx, "uploads", "photo.jpg")

// List objects
iter, _ := t.List(ctx, "uploads")
```

## Multi-Backend Routing

```go
t, _ := trove.Open(primaryDriver,
    trove.WithBackend("archive", archiveDriver),
    trove.WithRoute("*.log", "archive"),
    trove.WithRouteFunc(func(bucket, key string) string {
        if strings.HasPrefix(key, "temp/") {
            return "ephemeral"
        }
        return "" // default backend
    }),
)
```

## Middleware

Middleware intercepts read and write paths with fine-grained scoping:

```go
t, _ := trove.Open(drv,
    // Encrypt everything
    trove.WithMiddleware(encrypt.New(keyProvider)),
    // Compress only large files in specific buckets
    trove.WithScopedMiddleware(
        middleware.ScopeAnd(
            middleware.ScopeBucket("assets"),
            middleware.ScopeContentType("application/*"),
        ),
        compress.New(),
    ),
)
```

**Built-in middleware:** compress (zstd), encrypt (AES-256-GCM), scan (ClamAV), watermark (PNG/JPEG/PDF), cache, dedup.

## Content-Addressable Storage

```go
cas := t.CAS()
hash, _ := cas.Store(ctx, reader) // returns content hash, deduplicates automatically
```

## Virtual Filesystem

```go
vfs := t.VFS("my-bucket")
entries, _ := vfs.ReadDir("images/")
file, _ := vfs.Open("images/photo.jpg")
```

## Forge Extension

Trove ships with a Forge extension for DI integration, REST API handlers, and YAML configuration:

```go
import "github.com/xraph/trove/extension"

app := forge.New(
    extension.New(),
)
```

## Benchmarks

Run locally: `cd bench && go test -bench=. -benchmem`

## License

MIT
