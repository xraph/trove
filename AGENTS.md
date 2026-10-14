# AGENTS.md

Trove is a multi-backend object storage engine for Go: one API over local disk, memory, S3, GCS, Azure Blob and SFTP, with streaming, a middleware pipeline, content-addressable storage and a virtual filesystem. A separate module wraps it as a Forge extension.

The repo holds seven modules. The release workflow tags six of them at the same commit.

| Module | Directory | Release tag |
|---|---|---|
| `github.com/xraph/trove` | `.` | `vX.Y.Z` |
| `github.com/xraph/trove/drivers/azuredriver` | `drivers/azuredriver` | `drivers/azuredriver/vX.Y.Z` |
| `github.com/xraph/trove/drivers/gcsdriver` | `drivers/gcsdriver` | `drivers/gcsdriver/vX.Y.Z` |
| `github.com/xraph/trove/drivers/s3driver` | `drivers/s3driver` | `drivers/s3driver/vX.Y.Z` |
| `github.com/xraph/trove/drivers/sftpdriver` | `drivers/sftpdriver` | `drivers/sftpdriver/vX.Y.Z` |
| `github.com/xraph/trove/extension` | `extension` | `extension/vX.Y.Z` |
| `github.com/xraph/trove/bench` | `bench` | `bench/vX.Y.Z` |

`drivers/localdriver` and `drivers/memdriver` have no `go.mod`. They are packages inside the root module.

Each sub-module requires the root at a released version and carries `replace github.com/xraph/trove => ../..` (or `=> ..`). Those replaces are intentional, so leave them. Never set that require to `v0.0.0`, because a consumer's build ignores your replace, reads the require as written, and stops with `unknown revision v0.0.0` while every local build keeps passing.

## Commands

The Go version is per module. The root, `azuredriver`, `s3driver` and `bench` declare 1.25.7, while `extension`, `gcsdriver` and `sftpdriver` declare 1.26.0, so you want a 1.26 toolchain locally. Always build with `GOWORK=off`.

Root module:

```bash
GOWORK=off go build ./...
make test          # go test -v ./...
make test-race     # go test -race -v ./...
make vet
make lint          # golangci-lint run ./...
make fmt           # gofmt -s -w . then goimports -local github.com/xraph/trove
```

You build a sub-module from its own directory, the way CI does:

```bash
cd extension && GOWORK=off go build ./... && GOWORK=off go test -race -count=1 ./...
cd drivers/s3driver && GOWORK=off go build ./... && GOWORK=off go test -race -count=1 ./...
```

Several Makefile targets are broken. The module lists at the top of the Makefile name grove's database drivers and a `kv/` tree that do not exist here, so `build-all`, `test-all`, `vet-all`, `lint-all`, `fmt-all`, `tidy-all`, `mod-download-all`, `check-all` and `all` all fail on the first missing directory. `build`, `run`, `install` and `dev` point at `./cmd/trove`, which doesn't exist either, and `bench-report` and `bench-update` want a `bench/cmd/benchreport` that isn't there.

`make bench` runs but finds nothing, since `bench/bench_test.go` is behind the `bench` build tag. Pass the tag yourself:

```bash
cd bench && GOWORK=off go test -tags bench -bench=. -benchmem -count=1 -timeout=10m ./...
```

`./updatemod.sh` runs `go mod tidy` in every directory that has a `go.mod`. It is the one tool here that covers all seven modules.

### What CI runs

`.github/workflows/ci.yml` runs on pushes and pull requests to `main`.

| Job | Scope | What it runs |
|---|---|---|
| `ci` (shared `xraph/workflows` `go-ci.yml@v1`, Go 1.25) | root | `make test`, race coverage, golangci-lint, `gofmt -l .`, `make vet`, a `go mod tidy` diff, gosec (gating), govulncheck (reporting only), and a check that nested modules require the root at a version the proxy can serve |
| `vulncheck` | root, extension, the four drivers | `.github/actions/govulncheck-gate` per module |
| `drivers` | each driver module | tidy diff, `go build ./...`, `go test -race -count=1 ./...` |
| `extension` | `extension` | tidy diff, build, `go test -race -count=1 ./...` |
| `bench` | `bench` | tidy diff, build |
| `lint` | root | golangci-lint `latest`, `--timeout 10m` |
| `format` | whole tree | `goimports -l -local github.com/xraph/trove .` with goimports pinned in the workflow |
| `docs` | `docs/` | `pnpm install --frozen-lockfile`, `pnpm types:check`, `pnpm lint`, `pnpm build` |

golangci-lint only ever runs on the root module. Nothing in CI lints `extension` or the drivers, so if you change one, run `golangci-lint run ./...` from its directory yourself (it finds the root `.golangci.yml` by walking up).

The vulncheck gate fails on a reachable vulnerability in a third-party module unless it is listed in `.github/govulncheck-allowlist.txt` as `<module-name> <OSV-ID>`. Standard-library findings are reported and never gate. That file is a backlog: you fix an entry by bumping the dependency and deleting the line, and a new line needs a reason.

### Tests that need services

Plain `go test` needs no outside services. The cloud and SFTP conformance runs live in `drivers/*/integration_test.go` behind the `integration` build tag, and CI never sets it, so a change to one of those four drivers is only checked against a real backend when you run it yourself. Without a service listening, the tagged tests fail. They do not skip.

| Driver | Service | Variables (defaults in the test file) |
|---|---|---|
| `s3driver` | MinIO on `localhost:9000` | `S3_ENDPOINT`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, `S3_REGION` |
| `azuredriver` | Azurite on `127.0.0.1:10000` | `AZURE_ENDPOINT`, `AZURE_ACCOUNT_NAME`, `AZURE_ACCOUNT_KEY` |
| `gcsdriver` | fake-gcs-server on `localhost:4443` | `GCS_ENDPOINT`, `GCS_PROJECT` |
| `sftpdriver` | atmoz/sftp on `localhost:2222` | `SFTP_HOST`, `SFTP_PORT`, `SFTP_USER`, `SFTP_PASSWORD` |

The `docker run` line for each service is in the comment at the top of its `integration_test.go`. Then:

```bash
cd drivers/s3driver && GOWORK=off go test -tags integration -count=1 ./...
```

There are no database-backed tests. The extension's Postgres, SQLite and Mongo metadata stores have no tests at all, and the extension's own tests use `memdriver`.

## Layout

| Path | What lives there |
|---|---|
| `trove.go`, `options.go`, `options_middleware.go`, `config.go`, `errors.go` | the `Trove` facade: `Open`, object and bucket operations, multi-backend routing, options |
| `driver/` | the `Driver` interface, capability interfaces, error sentinels, DSN parsing, the driver registry, paging |
| `drivers/localdriver`, `drivers/memdriver` | the in-module drivers |
| `drivers/{s3,gcs,azure,sftp}driver` | the cloud and SFTP drivers, one module each |
| `middleware/` | the read/write interceptor pipeline, plus `compress`, `dedup`, `encrypt`, `scan` (ClamAV) and `watermark` |
| `cas/` | content-addressable storage, its index and GC |
| `vfs/` | a directory-shaped filesystem view over object keys |
| `stream/` | the streaming engine: chunks, pools, backpressure |
| `id/` | TypeID identifiers |
| `internal/` | checksums and the key router |
| `trovetest/` | `RunDriverSuite` (driver conformance), a mock driver, fixtures |
| `extension/` | the Forge extension module: `model/`, `store/{memory,postgres,sqlite,mongo}`, `migrate/`, `handler/` (REST), `hooks/`, `contract/` (dashboard contract with `manifest.yaml`), `manager.go` (multi-store) |
| `bench/` | benchmarks behind the `bench` tag |
| `docs/` | the Fumadocs site (`make docs`, `make docs-build`) |
| `CHANGELOG.md` | an `[Unreleased]` section; the release workflow neither reads nor rotates it |
| `MIGRATION.md` | where each piece of the old templ dashboard went |

## Conventions

### Errors and drivers

Classification sentinels live in `driver/errors.go`, so a driver in its own module can wrap them without importing the facade, and the root re-exports every one of them under the same identity (`trove.ErrObjectNotFound` is `driver.ErrObjectNotFound`, so `errors.Is` matches either way). `ErrObjectNotFound` and `ErrBucketNotFound` unwrap to `ErrNotFound`. `ErrInvalidPath` does not, on purpose: a malformed key should answer 400, not 404. `trove.Permanent(err)` tells a retrying caller whether to give up.

When you write a driver, wrap the sentinel with `%w` and keep your own message, for example `fmt.Errorf("mydriver: object %q not found in bucket %q: %w", key, bucket, driver.ErrObjectNotFound)`. Classify from the backend's typed error or HTTP status, never from message text. `Delete` returns nil for a missing object.

Watch out for `extension/store.ErrNotFound`. It has the same `trove: not found` text as `driver.ErrNotFound`, but it is a separate value, and `errors.Is` won't match one against the other.

A new driver implements `driver.Driver`, adds whichever capability interfaces the backend supports (`MultipartDriver`, `PresignDriver`, `ServerCopyDriver`, `VersioningDriver`, `NotificationDriver`, `LifecycleDriver`, `RangeDriver`), registers itself in `init` with `driver.Register("<scheme>", ...)`, and exposes an `Unwrap` helper for the typed driver. Every driver runs `trovetest.RunDriverSuite`, and you should add yours to it.

### IDs, options, logging

IDs are TypeIDs from `id/`, with prefixes `obj`, `bkt`, `upl`, `dwn`, `str`, `pol`, `ver` and `chk`.

The facade takes `trove.Option`, which is `func(*Trove) error`. The extension takes `ExtOption`, which is `func(*Extension)`.

The root module has no logger. The extension logs through `github.com/xraph/go-utils/log` and `e.Logger()` with `forge.F` fields.

### The Forge extension

`extension.New(opts...)` embeds `*forge.BaseExtension`. `Register` loads config from `extensions.trove`, then `trove`. It resolves a store from the named grove database (`WithGroveDatabase`), or from any `*grove.DB` in the vessel container, and runs migrations right there in `Register` unless `DisableMigrate` is set. It mounts no REST routes: `registerRoutes` is an empty placeholder, so `base_path` (default `/trove`) and `WithBasePath` are parsed but nothing reads them. The handlers in `extension/handler` are a plain `http.Handler` you build with `handler.New` and mount yourself. The one route the extension serves is the dashboard content route, at `dashboard_content_path` (default `/dashboard/trove/content`). With one store it provides `*trove.Trove`. Configure several stores and it also provides a `*TroveManager` and each store by name.

`extension/hooks` holds the Chronicle, Dispatch, Vault and Warden integrations, plus a metrics hook that registers through the Forge app's metrics registry. Each of the four service hooks looks its service up in the vessel container, and its constructor returns nil (with a debug log) when the service isn't registered, so an app without Warden, say, still starts.

### Lint and tests

`.golangci.yml` is a golangci-lint v2 config. The rules you'll trip over:

- `errcheck` runs with `check-blank` and `check-type-assertions`, so dropping an error with `_ = f()` fails outside tests, and so does `v, _ := x.(T)`.
- `govet` enables every analyzer except `fieldalignment`, shadowing included.
- revive's `exported` rule wants a doc comment on every exported identifier.
- `nolintlint` reports a `//nolint` that suppresses nothing.
- Test files are exempt from `gosec`, `errcheck` and `gocritic`.

One trap: `.golangci.yml` sets goimports' local prefix to `github.com/xraph/grove`. The `format` job and `make fmt` both use `github.com/xraph/trove`, so group your imports as standard library, third party, then `github.com/xraph/trove/...`.

Tests in the root module and the drivers use testify (`require`, `assert`). The extension's tests use plain `testing`.

Commits use conventional commits with a scope, for example `fix(extension): ...` or `chore(gcsdriver): ...`.

## Dependencies

The root module has no xraph dependencies, and the only xraph module the drivers depend on is the root. `extension` depends on `github.com/xraph/chronicle`, `dispatch`, `vault`, `warden`, `grove` (with `mongodriver`, `pgdriver` and `sqlitedriver`), `forge`, `go-utils` and `vessel`. See each `go.mod` for versions.

Dispatch and fabriq depend on the trove root, and `cortex/inttest` pulls it in indirectly.

That makes a loop at the repo level: dispatch needs trove's root, and `trove/extension` needs dispatch. A dependency round goes in this order:

1. Dispatch pins the trove root at its latest existing release.
2. Dispatch is released.
3. Trove bumps `extension` to the new dispatch and cuts a release, which tags the root and every sub-module again.

To bump, run `go get` in the module that needs it, once per xraph module at its own release, then tidy everything:

```bash
cd extension
GOWORK=off go get github.com/xraph/dispatch@vX.Y.Z
GOWORK=off go get github.com/xraph/forge@vX.Y.Z
cd ..
GOWORK=off ./updatemod.sh
```

Grove tags its drivers at the same version as its root, so move `grove` and its three drivers together. Then build and test each module you touched.

Never commit a `go.work`, or a `replace` pointing at a sibling repo, and check `git status` before you commit, since `.gitignore` doesn't cover `go.work`. Dependabot only watches the root `go.mod`. The sub-modules move when you move them.

## Releasing

You cut a release by dispatching the workflow. Never push a tag yourself.

1. Release anything trove needs first (for `extension`, that can be dispatch, chronicle, vault, warden, grove or forge), and pin it.
2. Wait for CI on `main` to pass: `gh run list --workflow ci.yml --branch main --limit 1`.
3. `gh workflow run release.yml --ref main -f tag=vX.Y.Z`
4. If the root changed, re-pin dispatch and fabriq to the new tag.

`.github/workflows/release.yml` does this:

1. It normalises the input to a leading `v` and rejects anything that isn't `vX.Y.Z` (with an optional pre-release suffix). Then it tags `main` as `github-actions[bot]` and pushes the tag.
2. It sets up Go from `extension/go.mod`, the highest version in the repo.
3. It verifies the root: `go mod download`, a `go mod tidy` diff on `go.mod` and `go.sum`, `go build ./...` and `go test -race -count=1 ./...`.
4. It does the same for `drivers/azuredriver`, `gcsdriver`, `s3driver` and `sftpdriver`.
5. It verifies `extension` and `bench` with download, the tidy diff and `go build ./...`. Neither module's tests run here.
6. It tags and pushes `drivers/<name>/vX.Y.Z` for each driver, then `extension/vX.Y.Z` and `bench/vX.Y.Z`, all at the commit the root tag points to.
7. It publishes a GitHub release with notes from `git log` since the previous tag.

Nothing rewrites the sub-modules' requires of the root during a release. A sub-module tag resolves the root at whatever version its `go.mod` names at that commit. The root tag goes out before any verification, so a failed run leaves `vX.Y.Z` published without its sub-module tags. Fix forward with the next patch version.

## Branch rules

`main` has no rules or protection. Direct pushes go through, and CI runs after them.
