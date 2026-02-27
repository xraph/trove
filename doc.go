// Package trove provides a multi-backend object storage engine with
// bi-directional streaming, composable middleware, and first-class
// Forge transport integration.
//
// Trove is to object/file storage what Grove is to databases — a polyglot,
// driver-based abstraction that generates native operations per storage
// backend while delivering a unified, composable API surface.
//
// # Quick Start
//
//	drv := localdriver.New()
//	drv.Open(ctx, "file:///tmp/storage")
//
//	t, err := trove.Open(drv)
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer t.Close(ctx)
//
//	// Upload an object.
//	err = t.Put(ctx, "my-bucket", "hello.txt", strings.NewReader("hello world"))
//
//	// Download an object.
//	reader, err := t.Get(ctx, "my-bucket", "hello.txt")
//	defer reader.Close()
//
// # Drivers
//
// Every storage backend registers as a driver — the same pattern Grove uses
// for pgdriver, mongodriver, etc. Available drivers:
//
//   - localdriver: Local filesystem storage
//   - memdriver: In-memory storage (testing)
//   - s3driver: AWS S3, MinIO, R2, DigitalOcean Spaces (Phase 3)
//   - gcsdriver: Google Cloud Storage (Phase 3)
//   - azuredriver: Azure Blob Storage (Phase 3)
//
// # Multi-Backend Routing
//
// Trove supports multiple named backends simultaneously:
//
//	t, err := trove.Open(primaryDriver,
//	    trove.WithBackend("archive", archiveDriver),
//	    trove.WithRoute("*.log", "archive"),
//	)
package trove
