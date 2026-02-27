# Changelog

All notable changes to Trove are documented in this file.

## [Unreleased]

### Phase 8: Cloud Drivers, Middleware, and Benchmarks

#### Added — Storage Drivers
- **GCS Driver** (`drivers/gcsdriver/`): Google Cloud Storage with multipart (compose), pre-signed URLs, and byte-range reads. Separate module: `github.com/xraph/trove/drivers/gcsdriver`.
- **Azure Driver** (`drivers/azuredriver/`): Azure Blob Storage with block blob staging, SAS URL generation, and range reads. Separate module: `github.com/xraph/trove/drivers/azuredriver`.
- **SFTP Driver** (`drivers/sftpdriver/`): SSH/SFTP remote file storage. Buckets as directories, metadata sidecars. Separate module: `github.com/xraph/trove/drivers/sftpdriver`.

#### Added — Middleware
- **Content Scanning** (`middleware/scan/`): Write-only middleware that scans uploads for threats via pluggable `ScanProvider` interface. Blocks malicious content with `ErrContentBlocked`. Includes built-in ClamAV INSTREAM provider.
- **Watermark** (`middleware/watermark/`): Read-only middleware that embeds invisible metadata in PNG (tEXt chunk) and JPEG (COM marker) images. Supports static and dynamic text via `WithTextFunc()`.

#### Added — Benchmarks
- **Benchmark suite** (`bench/`): Comprehensive benchmarks for memdriver and localdriver covering Put, Get, Head, Delete, Copy, and List operations at multiple sizes (1KB/1MB/10MB).

#### Changed
- **Makefile**: Added `STORAGE_DRIVER_MODULES` variable; updated `build-all`, `test-all`, and `lint-all` targets to include storage driver sub-modules.

---

## Phase 7: Forge Extension

#### Added
- **Extension module** (`extension/`): Full Forge integration with lifecycle management, config, DI, and HTTP route registration.
- **Grove models** (`extension/model/`): Bucket, Object, UploadSession, CASEntry, Quota models with Grove ORM.
- **Store layer** (`extension/store/`): CRUD operations for all models with list options and driver detection.
- **Migrations** (`extension/migrate/`): 5 tables under "trove" namespace with Grove migration system.
- **HTTP handlers** (`extension/handler/`): REST endpoints for objects, buckets, uploads, CAS, and admin.
- **Ecosystem hooks** (`extension/hooks/`): Chronicle audit logging, Dispatch event publishing, Warden policy checks, Vault key management, Metrics reporting.

## Phase 6: Streaming Engine

#### Added
- **Streaming engine** (`stream/`): Stream pool with backpressure, chunk management, concurrent upload/download, and progress callbacks.

## Phase 5: Virtual Filesystem

#### Added
- **VFS** (`vfs/`): Virtual filesystem layer implementing `io/fs.FS` with Stat, ReadDir, Walk, Open, and Create.

## Phase 4: Content-Addressable Storage

#### Added
- **CAS engine** (`cas/`): Content-addressable storage with Store, Retrieve, Pin, GC, and pluggable hash algorithms.

## Phase 3: Middleware Pipeline

#### Added
- **Middleware system** (`middleware/`): Direction-aware, scope-aware pipeline with resolver, caching, and runtime registration.
- **Encryption** (`middleware/encrypt/`): AES-256-GCM with KeyProvider interface.
- **Compression** (`middleware/compress/`): Zstd with auto-detect, skip list, and size guard.
- **Deduplication** (`middleware/dedup/`): BLAKE3 content hashing with duplicate detection callbacks.

## Phase 2: Driver Framework

#### Added
- **Driver interface** (`driver/`): 13-method Driver interface with capability interfaces (MultipartDriver, PresignDriver, RangeDriver).
- **DSN parser** (`driver/dsn.go`): Generic DSN parsing for all drivers.
- **Registry** (`driver/registry.go`): Global driver registration with Lookup/Register.
- **Local driver** (`drivers/localdriver/`): Filesystem storage with metadata sidecars.
- **Memory driver** (`drivers/memdriver/`): In-memory storage for testing.
- **S3 driver** (`drivers/s3driver/`): AWS S3 and S3-compatible services.
- **Conformance suite** (`trovetest/`): 22-subtest conformance suite with RunDriverSuite().

## Phase 1: Core Module

#### Added
- **Root module** (`github.com/xraph/trove`): Core Trove type with Put, Get, Delete, Head, List, Copy, bucket operations.
- **TypeIDs** (`id/`): Type-safe identifiers for objects, buckets, uploads, downloads, streams, policies, versions, chunks.
- **Checksums** (`internal/`): SHA-256, BLAKE3, XXHash checksum support.
- **Sentinel errors**: ErrNotFound, ErrBucketNotFound, ErrObjectNotFound, ErrContentBlocked, and more.
