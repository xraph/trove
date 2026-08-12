# Changelog

All notable changes to Trove are documented in this file.

## [Unreleased]

### Path Traversal

#### Fixed
- **`localdriver` applied its containment guard to one operation out of eight.** `safeJoin` was added to reject buckets and keys containing `..`, but only `Put` called it. `Get`, `Head`, `Delete`, `Copy` (source and destination), `List`, `CreateBucket`, and `DeleteBucket` each built their path with a bare `filepath.Join(rootDir, bucket, key)`, so a key of `../../etc/passwd` reached the filesystem unchecked on every one of them — readable via `Get`, removable via `Delete`. All of them now route through `safeJoin`, as does the bucket probe inside `classifyObjectErr`.
- **A key could cross bucket boundaries even where the guard did run.** `safeJoin` checked containment once, against the root, after joining every segment, so a key could spend the bucket segment on the way out and still land inside the root. `Put("tenant-a", "../tenant-b/secret.txt")` wrote into another bucket and passed the check. Containment is now enforced once per segment, against the directory the preceding segments produced: a key stays inside its bucket and a bucket stays inside the root. Buckets are Trove's tenancy boundary, so root-only containment was the wrong invariant.
- **`DeleteBucket(".")` deleted the entire storage root.** `.` resolved to `rootDir` itself, which was then handed to `os.RemoveAll`. The facade rejects an empty bucket name but not `.`. A segment that resolves to its own parent is now refused, which also stops `Delete(bucket, ".")` from removing a bucket directory through the object path.
- **`sftpdriver` had no containment check at all.** `objectPath` joined `BasePath`, bucket, and key with `path.Join` and returned the result, so the same traversals applied to the remote server. It now uses an equivalent `safeJoin` in remote path space — slash-separated regardless of the client's OS, with prefix-based containment since `path` has no `Rel` — and every operation handles its error.

#### Added
- **Rooted segments are rejected rather than silently reinterpreted.** `filepath.Join` treats a key of `/etc/passwd` as relative and stores the object at `<root>/<bucket>/etc/passwd`, handing the caller back an object at a key it never asked for. Leading separators, backslashes, and volume names are now refused on every platform, since `filepath.IsAbs` alone answers `false` for `/etc/passwd` on Windows.
- **Traversal test suites for both drivers.** `drivers/localdriver/traversal_test.go` runs every operation against traversal keys and bucket names, asserting both the rejection and that a planted out-of-root file and a sibling bucket's object are byte-identical afterward — an error alone does not prove containment. `drivers/sftpdriver/traversal_test.go` covers `safeJoin` and `objectPath` directly, since the SFTP operations need a live server, and adds a source assertion that no remote path is built with a bare `path.Join`. Both include control cases so that a guard rejecting everything would fail.

#### Changed
- **The gosec `G703` finding on `localdriver`'s `os.Rename` is suppressed with a documented justification.** `objPath` is containment-checked by `safeJoin` before it reaches the rename, but gosec's taint analysis tracks the flow from the bucket and key parameters to the filesystem sink and has no way to recognize a `filepath.Rel` check as a sanitizer — the check is already in the flow and the finding persists. Silencing it structurally would mean laundering the path through something the analyzer cannot follow, which would hide real traversal bugs there in future.

### Error Classification

#### Added
- **Canonical sentinels in `driver`** (`driver/errors.go`): `ErrNotFound`, `ErrObjectNotFound`, `ErrBucketNotFound`, `ErrBucketExists`, `ErrPermissionDenied`, `ErrQuotaExceeded`. Out-of-tree drivers in their own modules can now wrap them without importing the root package. The root package re-exports all of them, so `trove.ErrObjectNotFound` and `driver.ErrObjectNotFound` are the same value.
- **`ErrPermissionDenied`**: new sentinel for operations the backend refuses as unauthorized.
- **Not-found hierarchy**: `ErrObjectNotFound` and `ErrBucketNotFound` now unwrap to `ErrNotFound`, so callers can match either the specific resource or any missing resource.
- **Error contract documented** in the `driver` package: implementations MUST wrap the sentinels, using typed errors or status codes rather than message matching.
- **`trove.Permanent(err) bool`** (and `driver.Permanent`): reports whether retrying can ever change the outcome, so consumers share one retry taxonomy instead of each writing their own. Permanent: `ErrNotFound` (and both specific forms), `ErrPermissionDenied`, `ErrBucketExists`, `ErrInvalidPath`. Retryable: `ErrQuotaExceeded` — refused now, may be granted later — and any unclassified error, since dead-lettering a transient failure discards work that would have succeeded.
- **`ErrInvalidPath`**: new sentinel for a bucket name or key that is not addressable — empty, containing a NUL byte, rooted, or resolving outside the container it was addressed to. `localdriver` and `sftpdriver` wrap it from their `safeJoin` containment guards. It deliberately does **not** unwrap to `ErrNotFound`: nothing was looked up, so the request is malformed rather than the resource missing, and an HTTP surface answers `400` rather than `404`. Object-store drivers do not return it — their keys are opaque strings, so `../` is an ordinary character sequence with nothing to reject. `trovetest.RunDriverSuite` does not require it for that reason.

#### Fixed
- **Drivers now wrap the sentinels for not-found conditions.** `memdriver`, `localdriver`, `s3driver`, `gcsdriver`, `azuredriver`, and `sftpdriver` previously returned descriptive-only errors (e.g. `memdriver: object %q not found in bucket %q`), so `errors.Is(err, trove.ErrNotFound)` returned `false` and callers could not tell a permanently missing object from a transient backend failure. Descriptive messages are preserved; the sentinel is added with `%w`. Covers Get, Head, Delete, Copy, List, GetRange, bucket operations, and multipart.
- **`azuredriver` no longer classifies deletes by substring.** `Delete` matched `"BlobNotFound"` and `"404"` against the rendered message; it now reads the typed service error code via `bloberror.HasCode`.
- **Cloud drivers map provider errors properly**: S3 `NoSuchKey`/`NoSuchBucket`/`NotFound`/`NoSuchUpload` plus API error codes and HTTP status; GCS `storage.ErrObjectNotExist`/`ErrBucketNotExist` and typed `googleapi.Error` (including the reason code that separates a quota 403 from a permission 403); Azure `BlobNotFound`/`ContainerNotFound` and `azcore.ResponseError` status.
- **`cas.ErrNotFound` now wraps `driver.ErrNotFound`.** Content missing from the content-addressable store had the same classification gap as the drivers, so a consumer had to special-case the CAS to recognize a permanently missing blob. `errors.Is(err, cas.ErrNotFound)` is unaffected.
- **Permission and quota failures are classified** where the backend reports them: `ErrPermissionDenied` and `ErrQuotaExceeded` across the cloud drivers, and `ErrPermissionDenied` for `localdriver` and `sftpdriver`.
- **Path-traversal rejections are classified.** `localdriver` and `sftpdriver` `safeJoin` returned bare descriptive errors, so every rejection reached the HTTP extension unclassified and became a `500`. They now wrap `ErrInvalidPath`.
- **The HTTP extension no longer echoes raw error text into 5xx responses.** `extension/handler` put `err.Error()` straight into the body on every driver failure. Because driver errors wrap os-level ones, a failed write answered with e.g. `mkdir /srv/trove/data/nested: permission denied` — disclosing the absolute storage root and the directory layout under it to any client that could provoke a failure. An unclassified error is now logged server-side with its operation and request path, and the client receives `{"error":"internal error"}`.
- **Malformed keys and bucket names answer `400`, not `500` or `404`.** A percent-encoded traversal (`%2e%2e%2f…`, which `net/http`'s mux passes through to the handler intact, unlike a literal `../`) previously returned `500` on `PUT`/`DELETE`/list and `404` on `GET`/`HEAD`. `GET` and `HEAD` also reported *every* failure as `404`, so a backend outage was indistinguishable from a missing object.

#### Changed
- **`trovetest.RunDriverSuite`** asserts the sentinels on not-found paths instead of merely asserting that an error occurred, so every driver — including the gated cloud integration runs — is held to the contract.
- **`extension/handler` maps classified errors to their documented status codes** across the object, bucket, and CAS routes: `ErrInvalidPath`/`ErrKeyEmpty`/`ErrBucketEmpty` → `400`, `ErrNotFound` → `404`, `ErrBucketExists` → `409`, `ErrPermissionDenied` → `403`, `ErrQuotaExceeded` → `507`. Classifying an error is now also a statement that its message is safe to publish, since classified messages are returned to clients and unclassified ones are not; the driver contract documents the rule.

---

### Dependencies and Supply Chain

#### Fixed
- **`sftpdriver`: `golang.org/x/crypto` v0.37.0 → v0.52.0.** Five advisories in `x/crypto/ssh` were reachable, not merely present — `Open` calls `ssh.Dial`, `ssh.ParsePrivateKey`, and `sftp.NewClient` directly: infinite loop on large channel writes (GO-2026-5020), FIDO/U2F physical-interaction bypass (GO-2026-5019), DoS from pathological RSA/DSA parameters (GO-2026-5018), client-triggered server deadlock (GO-2026-5017), and a byte-arithmetic underflow panic (GO-2026-5013). The module's `go` directive is unchanged; x/crypto v0.52.0 requires only Go 1.25.0.

#### Added
- **govulncheck now runs against each driver sub-module** in CI's `drivers` matrix job. It resolves imports per module, so the shared `go-ci.yml` `security` job — which runs at the repository root — never scanned `azuredriver`, `gcsdriver`, `s3driver`, or `sftpdriver` at all. Every third-party dependency the project ships lives in those modules, which is why the x/crypto findings above were invisible to CI while Dependabot reported them.
- **The `drivers`, `extension`, `bench` and `lint` jobs no longer skip when the shared root-module workflow fails.** A reusable workflow reports a single conclusion for all of its jobs, so a `ci / Security` failure — currently two standard-library advisories about the runner's Go patch release — marked the whole `ci` job failed and skipped every job that declared `needs: ci`. The driver sub-modules and the extension module were therefore not being built or tested at all, and a skipped job reports as neither pass nor fail, so the coverage vanished without a red check anywhere. They now run unless the workflow is cancelled; `needs: ci` is kept for ordering.
- **`.github/govulncheck-allowlist.txt`** records the reachable findings that remain in `gcsdriver`, `s3driver`, and `azuredriver`, all of them indirect. It is a backlog rather than an exemption: the gate fails on anything not listed, so removing a line is how a fix gets enforced. Standard-library findings are reported but never gate, since they track the runner's Go patch release rather than this repository, and gating on them would red every branch whenever a new Go version lands.

---

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
