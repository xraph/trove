# Dashboard migration: templ to React shell

Trove's dashboard used to render server-side with templ and ForgeUI, from
`extension/dashboard/`. It now lives in the Forge dashboard's React shell as
`@forge-go/dashboard-plugin-trove`, reading the `trove` contract contributor in
`extension/contract`. The templ package is gone.

This file is the record of that move. We wrote it by walking every templ source
before deleting it: 25 `.templ` files (11 pages, 11 components and 3 widgets),
plus `contributor.go`, `data.go`, `manifest.go` and `plugin_iface.go`. Once the
directory is gone there is nothing left to check against, so anything missing
from here is a feature that went missing by accident. Every page, column,
action, form field, filter, badge, empty state, widget and nav item is listed
below, and each one says whether it moved, changed or was dropped, and why.

The cut happened in two steps. Commit 0ad27be stopped the extension from
registering the templ dashboard, because forge main had already removed
`DashboardAware` and the contributor package. Commit 2c2c22f deleted the
`extension/dashboard` package, once this file was in.

## What you need to do

If you ran the templ dashboard through `DashboardAware`, you don't need to
change anything in Trove. The extension no longer implements
`DashboardAware`, and `Extension.DashboardContributor()` is gone. It registers
the contract contributor through `ContractContributorAware` and the shell finds
it. If your own code imports `github.com/xraph/trove/extension/dashboard`, that
import has to go: the package no longer exists.

Add the plugin to your shell:

```tsx
import trovePlugin from "@forge-go/dashboard-plugin-trove"

const plugins = [corePlugin, trovePlugin]
```

The pages mount at `/@trove`, under a nav group called Storage. They use
Tailwind classes of their own, so the shell's stylesheet has to scan the
package. If your shell declares its sources with `@source`, add
`@source "<path to>/packages/plugin-trove/src";` next to the others.

Trove's extension needs forge v1.12.0, and `extension/go.mod` pins it. It's
the first forge release whose dashboard packages import neither templ nor
forgeui, so neither is in the extension's module graph any more. Anything
older than v1.11.2 also never passed a manifest's `invalidates` to the client,
and Trove declares its cache hints in `extension/contract/manifest.yaml`, so on
an older forge no upload, copy or delete would refresh the page you're looking
at. If a workspace `replace` holds forge back, lift it.

There are three new config keys:

| key | default | what it does |
|---|---|---|
| `dashboard_content_path` | `/dashboard/trove/content` | where the content route mounts, under the dashboard's own base path so the proxy that carries the dashboard carries this too |
| `dashboard_max_upload_bytes` | 67108864 (64 MiB) | the largest file a dashboard upload accepts |
| `dashboard_content_secret` | empty | the HMAC key that signs content tickets; empty means a random key per process. Set it and it must be at least 32 bytes, or the app refuses to start |

The 64 MiB cap is deliberate. Every Trove middleware buffers the whole object
in memory, so raising it raises what one upload can cost you in RAM.

Run more than one instance behind a load balancer and you have to set
`dashboard_content_secret`, to at least 32 bytes. A shorter one fails config
validation and the app won't start. With it empty, each process makes its own key at
start, and a ticket minted on one replica fails with a 403 on the next. The
Overview tells you which mode you're in under "Content links".

Object bytes never travel in the contract envelope. They go through the
content route, authorised by short-lived tickets that contract intents mint.
Download and preview tickets live 60 seconds and ride in the `?t=` query
parameter, because a plain `<a download>` can't send a header. Upload tickets
live 15 minutes and go in an `X-Trove-Ticket` header, and a PUT carrying `?t=`
is refused. The split exists because forge's dashboard tracing records the
query string. That means a download ticket can show up in the Traces view for
its 60 seconds of life. It's read-only and short-lived, so we accepted that,
but forge should redact `t` (see Still open). `disable_routes` does not turn the content route
off.

The content route does not work behind the Next.js proxy, which is
`@forge-go/dashboard-next` (`packages/next` in the forge-dashboard repo).
It forwards only GET and POST, caps a request body at 1 MiB, and reads every
response as text. Uploads never reach Trove, and a binary download comes back
mangled. Use the Vite shell, or serve the dashboard from the Go server.

If you call Trove from Go, a few things grew, and one thing behaves
differently:

- With a `Delimiter` set, `List` now folds the keys under a common prefix into
  that prefix on every driver, and returns the prefixes through
  `ObjectIterator.CommonPrefixes()`. mem, local and SFTP used to ignore the
  delimiter and return every key. Objects and prefixes form one sorted
  sequence, and `MaxKeys` counts both.
- `ListResult.NextToken` is opaque on every driver. Pass it back unchanged as
  `Cursor`, and stop when it is empty, never when a page is empty: a cloud
  driver can return an empty page with more to come.
- New, all additive: `driver.NewObjectIteratorWithPrefixes`,
  `driver.PageKeys`, `CAS.Bucket()`, `CAS.Stat(ctx, hash)`,
  `Stream.TotalSize()`, `Trove.Backends()` and `Trove.DriverFor(bucket, key)`.

There is no schema change. The metadata store in `extension/store` is
untouched, and nothing in the new dashboard reads it.

## Bugs found on the way

Building the React pages meant reading Trove closely, and that turned up a lot
the templ pages had been hiding. The biggest is the reason the dashboard looks
so different now. The templ pages read a metadata store that normal operation
never writes, because `trove.Put`, `Delete` and `Copy` go straight to the
driver and the root module can't import the extension, so the Objects,
Uploads, CAS and Quotas pages showed you tables nobody fills and you could
store a thousand objects without one of them appearing. The new pages read the
drivers.

Slice 1 of this migration fixed what the browser needed in Trove core:

- S3 sent your cursor as `StartAfter` and returned S3's continuation token as
  `NextToken`, so feeding the token back gave you the wrong page. It now pages
  with `ContinuationToken`.
- Azure had the mirror bug, passing a blob name as the opaque `Marker`. It now
  pages with the marker.
- S3 dropped `CommonPrefixes`, Azure ignored the delimiter, and mem, local and
  SFTP ignored it too. All six drivers now report common prefixes.
- GCS turned each common prefix into an `ObjectInfo` with an empty key and
  every field zero, which counted against `MaxKeys` and vanished on later
  pages. It now reports prefixes, and pages with the iterator's page token
  instead of rescanning from the start.
- CAS and streams gained read-only accessors (`CAS.Bucket`, `CAS.Stat`,
  `Stream.TotalSize`), and a stream's total size is now safe to read while
  the transfer runs.

The conformance cases for delimiters and paging run in CI on mem and local.
The S3, GCS, Azure and SFTP changes compile, and are exercised only under the
`integration` build tag, which CI does not set, so nothing has run them
against a real backend in CI.

Everything below is recorded, not fixed. Each one sits outside the dashboard
code, in Trove core, a driver or the extension.

- `EnableEncryption` is display-only, in single-store and multi-store mode.
  If you set `enable_encryption: true`, nothing registers the encrypt
  middleware from it, the per-store `enableEncrypt` is collected and then
  ignored, and your objects are stored exactly as they arrive. The Vault
  key-provider hook exists and nothing constructs it. The templ Settings page
  showed "Encryption" switched on all the same. The Overview now says
  "Configured, not applied".
- The read pipeline is not reversed. Register compress and encrypt together
  and you get compressed bytes back from a download, with no error. The docs say
  the opposite, and give a default priority of 100 that the code doesn't use.
- `ScopeContentType` matches the key's extension, not the content type, and
  only for `x/*` entries.
- The resolver's cache ignores `ctx`, and a stale entry can keep an old
  pipeline for one direction after `Register` or `Remove`.
- compress sniffs the zstd magic on read, so a `.zst` file you upload yourself
  is silently decompressed on download.
- encrypt has no default key provider and panics without one. Reading
  plaintext through it fails, or panics inside the standard library for some
  inputs.
- Every middleware buffers the whole object in memory, which is why you can't
  raise `dashboard_max_upload_bytes` for free.
- The REST handler sets `Content-Length` to the stored size, which is wrong
  under compress, encrypt or watermark.
- CAS: the index is in memory and lost on restart. There is no API that
  releases a reference, so GC, which collects only refcount zero, can never
  collect anything, however long you run it. The extension never creates the `cas` bucket, so on
  memdriver the first `Store` fails. CAS also bypasses middleware.
- Streams: `Drop` backpressure discards upload data, `MaxBandwidth` and
  `IdleTimeout` do nothing, and `Stream.Close` leaks the pool slot.
- Multipart: GCS parts are visible objects, not namespaced per upload. Upload
  ids repeat after a restart and all state is lost with it. Azure's abort does
  nothing on the backend, and its block ids collide across uploads. Nothing in
  Trove or the extension calls these APIs, and the "resumable" stream option
  is a flag only its own getter reads.
- ETags: on mem the ETag is the length in hex, so equal-length objects share
  one and you can't use it to tell two of them apart. On local, SFTP and Azure, `Put` returns a different ETag from what
  `Head` returns.
- Azure's `Put` echoes a storage class it never applied. `CreatedAt` is really
  last-modified on Azure, local and SFTP, which is why the Buckets page labels
  that column "Last modified" there.
- Unclassified errors: bucket exists on GCS, bucket not empty on S3 and GCS,
  and most GCS and Azure multipart errors come back as plain errors with no
  sentinel.
- `DeleteBucket` on a non-empty bucket is recursive on local, mem, SFTP and
  Azure, and an error on S3 and GCS. The dashboard lists one key first and
  refuses a non-empty bucket on every driver.
- VFS: `ReadDir` sees at most 1000 keys and ignores `NextToken`, `RemoveAll`
  deletes one page, `Mkdir` on local stores the marker as a plain file,
  `SetMetadata` is a no-op on S3, GCS and Azure, and IOFS `ReadDir(n)` returns
  the wrong error.
- Copy: S3 does not URL-encode `CopySource`, Azure's copy is asynchronous and
  never polled, and S3, GCS and Azure ignore `CopyOption`. Trove's `Copy`
  never calls `ServerCopy`, even where the driver has it.
- The metadata store: no writers for objects, upload sessions, CAS entries or
  quota usage. `SetQuota` and `PutCASEntry` on Postgres and SQLite emit
  `ON CONFLICT (...) DO UPDATE` with no SET clause, which both databases
  reject. Not-found sentinels differ by backend, a quota save zeroes its
  usage, no backend has tests, and the memory store and the `migrate` package
  are imported nowhere.
- All five hooks (chronicle, dispatch, metrics, vault and warden) are never
  constructed, so if you were counting on Chronicle or metrics seeing storage
  events, they don't. The warden hook trusts a client-supplied `X-Subject-ID` header.
- The REST handler package (`extension/handler`) is never mounted and has no
  auth. `registerRoutes` is an empty placeholder and its upload handlers are
  stubs. Since slice 1 it also drops prefixes on mem and local when
  `?delimiter` is set. Nothing breaks, because nothing serves it.
- The conformance suite runs on mem and local only, and the integration setups
  for the other four drivers probably can't pass as written.
- The Next.js proxy can't carry the content route (see What you need to do).

The templ pages had bugs of their own, and those go with them:

- Every upload and download button pointed at the REST handler, which is never
  mounted. None of them ever worked.
- Deleting a bucket removed its metadata row and left the bucket and its
  objects on the driver, and deleting an object soft-deleted its row and left
  the bytes, so anything you deleted from the templ pages is still on the
  driver.
- The file browser's Download and Copy buttons used the metadata row's ID where
  they meant the key, so Download asked for the wrong path and Copy put an ID
  on your clipboard.
- Saving object metadata wiped it. The form named its fields `metadata_key_N`
  and the handler read `meta_key_N`, so it always saved an empty map.
- The Copy Key button on the object detail page built JavaScript by pasting
  the key into a quoted string, so a key with a quote in it broke the button,
  and a crafted key could run script in the operator's session. The file
  browser's copy button used the same pattern, but with the row ID (see the
  Download and Copy item above), which Trove generates, so only the object
  detail button could be broken or abused through a key.
- The bucket search box sent a `query` parameter nothing read, and the quota
  table's Edit button sent `action=edit`, which no handler matched.
- The buckets table's Driver column and the Settings page printed the
  `storage_driver` setting, which is a DSN and can carry credentials.
- Pin and Unpin on the CAS page flipped a table the GC never reads, and
  Generate URL had no handler at all.
- Every count was capped at 100 rows a bucket, and a failed read showed as an
  empty table or a zero.

## Deliberately dropped

- The overview's stat cards (Buckets, Objects, Storage Used, Active Uploads)
  and the Storage Stats widget. Objects and bytes were counted from a table
  nothing writes, capped at 100 a bucket, and no driver can count or sum
  without reading every key. Active uploads came from upload sessions, which
  nothing writes either.
- Recent objects, on the overview and as a widget. It read the same unwritten
  table, and no driver lists by recency.
- The bucket search box. The handler never read it, so whatever you typed,
  you got every bucket back.
- Bucket detail, the bucket edit form and the lifecycle JSON. Region, quotas,
  versioning, the CAS checkbox and lifecycle rules were labels in the
  metadata row. Nothing sent them to a driver or enforced them, and no driver
  implements `LifecycleDriver` or `VersioningDriver`.
- Object metadata and tag editing. Both wrote the metadata store only, and the
  metadata save wiped everything. No driver stores tags. The inspector shows
  the user metadata the driver actually stored, read-only.
- New Folder. It had no handler, and a folder is only a prefix. Upload a file
  into a path and you have the folder.
- The Uploads list, its status filter, the upload detail page and Abort.
  Nothing writes upload sessions, and Abort only changed a row. Transfers
  shows the streams open in this process instead.
- Quotas. Nothing enforces them, usage was never measured, and creating one
  failed on Postgres and SQLite.
- The topbar search and the API Docs link. No search provider existed, and a
  docs link is not a dashboard feature.
- The plugin section interfaces in `plugin_iface.go`. Nothing in forgery
  implements them, and the contributor never called them.
- CAS lookup by hash. No intent answers it, and filtering the page you already
  loaded would pass a partial search off as a complete one. It needs a
  `cas.lookup` intent first. The Pinned and Unpinned filters are dropped for
  the same reason.

Everything else that was dropped says why where it appears below.

## Blocked

Nothing. Every templ surface either has a React replacement or is dropped with
a reason, and none of them waits on work outside Trove.

## Page by page

Status is one of **migrated** (same thing, same place), **changed** (the
behaviour is different on purpose, with the reason) or **dropped** (gone, with
the reason).

A few things hold across every React page. Every list reads the driver through
the contract, never the metadata store. No list shows a total, because no
driver can produce one: captions count what's on screen, such as "214 shown,
more under this prefix". Keys, ETags, hashes and byte sizes are mono. A byte
size is the exact number ("4,812 B") with the readable size on hover, where the
templ pages printed "4.7 KB". A timestamp uses the kit's `Timestamp`, and an
absent value is a "none" cell. The templ pages printed "Not set", "Never",
"N/A" or a dash.

In multi-store mode every page except the browser has a store picker in its
header (`StorePicker`). The browser takes its store from `?store=` in the URL,
so a copied link opens the same store. The templ contributor could only see
the default store.

### Route map

templ routes are relative to the contributor, and the actions rode in the
query string over `GET`.

| templ route | React route | status |
|---|---|---|
| `/` | `/@trove/` | migrated |
| `/buckets`, `?action=show_form`, `?action=create` | `/@trove/buckets`, with a create dialog | migrated |
| `/buckets/detail?bucket_id=`, `&action=edit`, `save`, `save_lifecycle`, `delete` | none | dropped: see Bucket detail |
| `/objects?bucket_id=&prefix=` | `/@trove/buckets/:bucket?prefix=` | changed: objects are listed one bucket at a time |
| `/objects/detail?object_id=`, `&action=edit_metadata`, `edit_tags`, `save_metadata`, `save_tags`, `copy`, `delete` | `/@trove/buckets/:bucket?key=`, the inspector beside the listing | changed: see Object detail |
| `/browser?bucket_id=&prefix=` | `/@trove/buckets/:bucket?store=&prefix=&key=`, loaded lazily | changed |
| `/uploads?status=` | none | dropped: see Uploads |
| `/uploads/detail?upload_id=`, `&action=abort` | none | dropped: see Upload detail |
| `/cas?filter=&action=&hash=` | `/@trove/cas` | changed |
| `/quotas?action=` | none | dropped: see Quotas |
| `/settings` | none: its content is on `/@trove/` | changed |
| none | `/@trove/middleware` | new: registered middleware, and a test of what applies to a key |
| none | `/@trove/transfers` | new: streams open in this process, polled every 5 seconds |

The templ links joined IDs, prefixes and keys into URLs with no escaping, so a
prefix holding `&`, `#` or `+` broke navigation. `browserHref()` in
`browser-location.ts` encodes the bucket into the path and the rest with
`URLSearchParams`, and builds absolute `/@trove/...` paths so the host never
appends the current query string to them.

### Overview

`pages/overview.templ`, and `components/stat_card.templ` for its cards.

| templ | React | status |
|---|---|---|
| Title "Overview", subtitle "Monitor your object storage at a glance." | `pages/overview.tsx`: "Overview", "What this store's driver can do, and which protections are actually switched on." | changed |
| Stat card Buckets, "Storage containers" | none on the overview. The Buckets page caption counts the driver's buckets | dropped: it counted metadata rows, which only the templ create form wrote |
| Stat card Objects, "Stored objects" | none | dropped: see Deliberately dropped |
| Stat card Storage Used, "Total bytes" | none | dropped: see Deliberately dropped |
| Stat card Active Uploads, "In progress" | none. Transfers lists open streams | dropped: nothing writes upload sessions |
| A CAS entry count, fetched and never rendered | none | dropped: dead code |
| Stat card icons (database, file, hard-drive, upload) | none | dropped |
| Card "Quick Actions" with View Buckets, File Browser and View Objects | the Storage nav group, and a bucket name opens its browser | changed: the nav already does it |
| Card "Recent Objects" with a View All button | none | dropped: see Deliberately dropped |
| Recent objects columns Key (cut to its last 37 characters), Size, Content Type, Created, a row opening object detail | none | dropped: with the card |
| Empty state "No objects yet", "Store your first object to see it here." | none | dropped: with the card |

The React overview shows what used to sit on Settings and the health widget,
then more. In order: the driver name and a Healthy or Unhealthy badge with the
ping error; a routing alert, "Some keys go to other backends", when the store
routes keys away from its default backend; a Protection table (Encryption,
Compression, Content scanning, CAS), each Configured against Applied with a
note; "What this driver can do", eight capability lines that say yes or no and
what it means; and a Configuration block. See Settings for how each templ field
landed.

### Buckets

`pages/buckets.templ`, `components/bucket_table.templ`.

| templ | React | status |
|---|---|---|
| Title "Buckets", a count badge of metadata rows | `pages/buckets.tsx`: "Buckets", "Buckets as the driver reports them.", caption "N buckets" | changed: the driver's list, so the default bucket and buckets made outside the dashboard show up |
| Button "Create Bucket", opening an inline card | "Create bucket", opening a dialog | changed |
| Card "Create New Bucket" with a close X | Dialog "Create a bucket", "The driver decides which names it accepts." | changed |
| Field Name, required, placeholder "my-bucket" | Name, mono; Create is disabled while it's blank | migrated |
| Field Region, "us-east-1" | none | dropped: a label in the metadata row, never sent to the driver |
| Fields Quota (Bytes) and Quota (Objects), "0 = no limit" | none | dropped: nothing enforces them |
| Checkbox Versioning | none | dropped: a label; no driver implements versioning |
| Checkbox CAS Enabled | none | dropped: CAS is set per store in config, and the box only set a label |
| Buttons Cancel and Create | Cancel and Create ("Creating…" while it runs) | migrated |
| Error banners "Failed to create bucket: ...", "Bucket name is required." | "Could not create the bucket" (`CommandAlert`) inside the dialog | changed |
| Error "Bucket created on driver but failed to store metadata" | none | dropped: there is no metadata row to write |
| Search box "Search buckets by name..." | none | dropped: see Deliberately dropped |
| Empty state "No buckets found", "Create your first bucket to get started." | "No buckets in this store yet. Create one to start storing objects." | migrated |
| A failed read shown as that empty state | the query fails and says so | changed |
| Column Name, medium weight, the whole row opening bucket detail | Name, mono, a link to the browser | changed: there is no bucket detail page |
| Column Driver, a badge | none | dropped: one store has one driver, and the Overview names it. The badge printed the `storage_driver` DSN |
| Column Region, a dash when empty | none | dropped: see the form |
| Column Versioning, a badge | none | dropped |
| Column CAS, a badge | none | dropped |
| Column Quota, bytes or "No limit" | none | dropped |
| Column Created, "Jan 02, 2006" | "Created" or "Last modified", following `createdAtMeaning` | changed: local, SFTP and Azure only know a last-modified time, and the header says so |
| No delete on the list | Delete on every row, confirmed | changed: see Bucket detail |

### Bucket detail

`pages/bucket_detail.templ`. The page is dropped. Everything on it came from the
metadata row, except the objects card, which came from the unwritten objects
table.

| templ | React | status |
|---|---|---|
| Button "Back to Buckets" | the browser's "Buckets" breadcrumb | changed |
| Header: database tile, bucket name, bucket ID, driver badge | the browser's page title is the bucket name | changed |
| Error banner | `CommandAlert` inside each dialog | changed |
| Field Name | the browser's title | changed |
| Fields ID, Driver, Region, Tenant Key, Created, Updated | none | dropped: metadata row fields. Nothing sets a tenant key, and the driver's time is on the Buckets list |
| Button "Browse Files" | the bucket name on the Buckets list | changed |
| Button "Edit" | none | dropped: see Deliberately dropped |
| Button "Delete Bucket", confirmed in the browser, which removed the metadata row and left the bucket on the driver | Delete on the Buckets list: a `ConfirmDialog` saying up front that only an empty bucket can go, then `buckets.delete` on the driver | changed: it deletes the real bucket now, and refuses a non-empty one and the CAS bucket |
| Stat cards Objects and Total Size, from at most 100 rows | none | dropped: no driver can count |
| Stat cards Quota (Bytes) and Quota (Objects), "No limit" | none | dropped |
| Card Configuration: Versioning and CAS Enabled badges, Default Metadata pairs | none | dropped: labels nothing reads |
| Edit form: Versioning and CAS Enabled checkboxes, Quota Bytes, Quota Objects, Region, Save, Cancel | none | dropped |
| Card "Lifecycle Rules": a JSON textarea (placeholder `[]`) and "Save Lifecycle" | none | dropped: stored as bytes, never parsed or sent to a driver |
| Card Objects with View All, the first 100 rows in the object table | the browser's listing | changed |
| Empty state "No objects", "This bucket is empty." | "This bucket is empty", "Drop files here, or use Upload files, to add the first object." | migrated |

### Objects

`pages/objects.templ`, `components/object_table.templ`. The object table also
rendered the Objects card on Bucket detail.

| templ | React | status |
|---|---|---|
| Title "Objects", a count badge | the browser's caption, `listingCaption()` in `listing.ts` | changed |
| Bucket filter: All Buckets, then one button per bucket | none: you open a bucket from the Buckets page | changed |
| All Buckets: the newest 100 objects across every bucket | none | dropped: no driver lists across buckets or by recency |
| Search "Search objects by key prefix...", applied only with a bucket picked | the path bar's input, "filter, then Enter" (`components/path-bar.tsx`) | changed: the prefix goes to the driver as the literal key prefix |
| Empty state "No objects found", "No objects match the current filters." | "This bucket is empty", "Nothing under this prefix", or "Nothing on this page" with Load more (`components/object-listing.tsx`) | changed: three different situations, three different answers |
| Column Key, mono, cut to the last 37 characters, the whole row opening object detail | Name: the key with the current folder dropped, mono, cut by width with the full key on hover, a link that selects it in the inspector | changed |
| Column Size, "4.7 KB" | Stored size, exact | changed: it is the size as stored, which compress and encrypt change |
| Column Content Type | the inspector's Content type | changed |
| Column Driver, a badge | none | dropped |
| Column Created | Last modified | changed: drivers report a last-modified time |
| No paging, 100 rows at most | Load more, following the driver's cursor; rows virtualise past 200 | changed |
| No folders | folder rows from the driver's common prefixes, each a link | changed |
| No routing warning | "Some keys may live on another backend" when the store routes keys away | changed: new, because a routed store's listing can miss objects |

### Object detail

`pages/object_detail.templ`. In React the object opens in the inspector beside
the listing (`components/inspector.tsx`), with its commands in
`components/object-actions.tsx`.

| templ | React | status |
|---|---|---|
| Button "Back to Objects" | the path bar and the "Buckets" breadcrumb | changed |
| Error banner | `CommandAlert` beside the action, or inside its dialog | changed |
| Header: file tile, key, row ID, driver badge, a "Deleted" badge on a soft-deleted row | the full key as the inspector's heading, mono | changed |
| Field Key | the heading | migrated |
| Field ID | none | dropped: a metadata row ID |
| Field Bucket ID | none | dropped: the URL names the bucket |
| Field Content Type | Content type | migrated |
| Field Size | Stored size | changed: named for what it is |
| Field ETag | ETag, mono | migrated |
| Field Driver | none | dropped: the Overview names the driver |
| Field Storage Class | Storage class | migrated |
| Field Version ID | Version | migrated |
| Field Tenant Key | none | dropped: nothing sets it |
| Fields Created and Updated | Last modified | changed: the driver's own time |
| No field for middleware | "Applies now": the middleware matching this key in today's config, or "No middleware matches this key in the current config." | changed: new, worded as config and never as history |
| Button Download, a link to the REST handler, which is never mounted | Download: mints a 60-second ticket on the click (`objects.contentUrl`) and downloads through the content route | changed: it works now |
| Button "Copy Key", JavaScript built from the key | "Copy key", through the clipboard API; "Copied", or a line saying copying needs a secure page | changed: a quote in a key no longer breaks it |
| Button "Copy Object", toggling an inline form | "Copy to", opening a dialog | changed |
| Copy form "Copy Object to Another Bucket": Destination Bucket select, Destination Key prefilled with the key | Destination bucket (the driver's buckets), Destination key prefilled | migrated |
| Copy form buttons Copy and Cancel | Copy and Close | migrated |
| Copy: `trove.Copy`, then a metadata row write whose error was thrown away | `objects.copy`: refused when the destination runs different middleware or a different backend, or either side is the CAS bucket; an existing key asks before replacing; success links to the copy | changed |
| Button "Generate URL" when the driver can presign, with no handler | "Share link" when `presign.available`, with a 1 hour, 1 day or 7 day lifetime; otherwise the reason, such as no signing on this driver | changed |
| Button Delete, confirmed in the browser, which soft-deleted the row and left the bytes | Delete, a `ConfirmDialog`, then `objects.delete` on the driver. In the CAS bucket the dialog says why it refuses and the button stays off | changed |
| Card Checksums (Algorithm, Value), shown when set | none | dropped: nothing ever wrote a checksum |
| Card Metadata, a count badge, a key and value table, "No metadata set." | Metadata, sorted `key=value` tags, "none" when empty | changed: the user metadata the driver stored, from `Head` |
| Button "Edit Metadata", the key and value form, an empty row for a new pair, Save, Cancel | none | dropped: see Deliberately dropped |
| Card Tags, a count badge, "No tags set." | none | dropped: no driver stores tags |
| Button "Edit Tags" and its form | none | dropped |
| No preview | Preview (`components/preview.tsx`): text and JSON in a read-only CodeMirror view, capped at 256 KiB; raster images from a Blob behind an object URL; SVG through a `data:` URL; other image types and other content types say there is no preview | changed: new |

Images over 4 MiB as stored aren't fetched for a preview, and the inspector
says to download them. An SVG never gets a `blob:` URL, because one opened in
its own tab is a document on the dashboard's origin and its script would run
as you.

### File browser

`pages/browser.templ`, `components/file_browser.templ` (`FileBrowser`,
`BrowserToolbar`, `BrowserBreadcrumbs`, `BrowserEntryRow`, `UploadDropZone`).
The React browser is `pages/browser.tsx`, a lazy route.

| templ | React | status |
|---|---|---|
| Title "File Browser", "Browse and manage objects in your storage buckets." | the bucket name, "Objects as the driver lists them, one prefix at a time." | changed |
| Bucket selector: All Buckets, then one button per bucket | none: open a bucket from the Buckets page | changed |
| Empty state "Select a bucket", "Choose a bucket above to browse its contents." | none | dropped: the browser always has a bucket |
| Drop zone: a dashed box, "Drag and drop files here, or click to browse", "Files will be uploaded to" and the prefix | the whole listing is the drop target (`UploadDropZone` in `components/upload-tray.tsx`); while you drag, an overlay names `bucket/folder` in mono and the per-file limit | changed |
| Clicking the drop zone opens the file picker | "Upload files" in the page header opens it | changed |
| Hidden file input, multiple | hidden file input, multiple | migrated |
| Each file PUT at once with `fetch` to the unmounted REST route | `objects.beginUpload`, an XHR `PUT` with the ticket in `X-Trove-Ticket`, then `objects.completeUpload`, two files at a time (`uploads.ts`) | changed: it works now |
| Status line "Uploading N/M files...", then "Upload complete!" or "Upload complete. N file(s) failed." | the upload tray: one row per file with its state, a progress bar while sending, its own error, and Cancel; Replace or Skip when the key exists; Dismiss and "Clear finished" | changed |
| A reload of the browser one second after the last file | `completeUpload` invalidates the listing | changed |
| An upload's state lost on navigation | uploads keep going while you move around the dashboard, and are still in the tray when you come back. A full reload drops them | changed |
| A dropped folder, sent as whatever the browser made of it | refused: "Folders can't be uploaded here. Drop the files inside them instead." | changed |
| A file dropped outside the zone opened in the tab | swallowed while the browser is mounted, so a stray drop can't unload the queue | changed |
| No size check | a file over `dashboard_max_upload_bytes` fails on its row before anything is sent | changed |
| Toolbar button Upload | "Upload files" | migrated |
| Toolbar button "New Folder", with no handler | none | dropped: see Deliberately dropped |
| Toolbar refresh button | none | dropped: dashboard writes refresh the listing, and a reload reads it again |
| Breadcrumbs: a hard-drive icon for the root, then each prefix segment | the path bar: the bucket name for the root, each folder segment a link, and an input to continue the prefix | changed |
| Columns Name, Size, Type, Modified, Actions | Name, Stored size, Last modified | changed: type is in the inspector, and so are the actions |
| Folder rows built from the first 100 metadata rows | folder rows from the driver's common prefixes | changed |
| Folder row: folder icon, name, "Folder", dashes | the folder name as a link, "none" cells | changed |
| File row: content-type icon, name, size, type, modified | the name as a link, stored size, last modified | changed |
| A file row opening object detail | the link selects the key and the inspector opens beside the listing | changed |
| Row action Download, which used the row ID as the key | the inspector's Download | changed |
| Row action Copy, which copied the row ID | the inspector's "Copy key" | changed |
| Row action Delete, confirmed, a soft delete | the inspector's Delete | changed |
| Empty state "Empty directory", "No files or folders here. Upload files to get started." | the three empty states under Objects | changed |
| `BuildBrowserEntries`, `BuildBreadcrumbs`, `browserURL`, `objectDownloadURL` | `mergePage` in `listing.ts`, `PathBar`, `browserHref` | changed |

### Uploads

`pages/uploads.templ`, `components/upload_table.templ`. The page is dropped:
nothing writes upload sessions, so the table was always empty. Transfers
(`pages/transfers.tsx`) is the nearest thing, and its header says what it
shows: "Streams open in this process. They are not saved and are lost on
restart." It shows Object, Direction, State, Transferred and Expected size,
polls every 5 seconds, and says "No streams open." when there are none.

| templ | React | status |
|---|---|---|
| Title "Upload Sessions", a count badge | none | dropped |
| Status filter All, Pending, Active, Completed, Aborted, Expired | none | dropped |
| Empty state "No upload sessions", "No upload sessions match the current filters." | none | dropped |
| Columns Object Key, Status, Parts ("2 / 5"), Size, Expires, Created, a row opening upload detail | none | dropped |
| Status badges: Active default, Pending secondary, Completed outline, Aborted and Expired destructive | none. Transfers has its own stream state badges | dropped |

### Upload detail

`pages/upload_detail.templ`. Dropped with Uploads.

| templ | React | status |
|---|---|---|
| Button "Back to Uploads" | none | dropped |
| Header: upload tile, object key, session ID, status badge | none | dropped |
| Fields ID, Bucket ID, Object Key, Content Type, Status, Total Parts, Uploaded Parts, Total Size, Tenant Key, Expires, Created, Updated | none | dropped |
| Button "Abort Upload" on a pending or active session, confirmed, which only changed the row's status | none | dropped: there was never a backend upload to abort |
| Card "Upload Progress": parts uploaded against total, a bar coloured by status | none | dropped |
| Card Metadata with a count badge and a key and value table | none | dropped |
| Card "Chunk Details", the chunks as indented JSON | none | dropped |

### CAS index

`pages/cas.templ`, `components/cas_table.templ`. The React page is
`pages/cas.tsx`. It reads the CAS engine and the CAS bucket, where the templ
page read `trove_cas_index`, a table unrelated to the index GC runs against.

| templ | React | status |
|---|---|---|
| Title "CAS Index", a count badge | "CAS", "Content-addressable storage: blobs stored under their hash, with a reference count per hash." | changed |
| Subtitle "Content-addressable storage index with deduplication tracking." | the description above | changed |
| No status block | Algorithm, Bucket and Index, then two lines: the index is in memory and a restart forgets every count and pin; nothing lowers a reference count, so GC finds nothing | changed: new, the ceiling stated |
| With CAS off, the table still listed the metadata table | "CAS is not enabled on this store." and nothing else | changed |
| "Run GC", shown when CAS is on, confirmed with "Run garbage collection? This will remove unreferenced CAS entries." | "Run garbage collection", confirmed in a `ConfirmDialog` that says to expect nothing deleted and that unindexed blobs are never touched | migrated |
| Result "GC Complete: Scanned N entries, deleted N, freed X." | "Found N entries with no references and no pin, deleted N, freed X B.", plus how many could not be deleted | changed |
| GC error banner, "CAS is not enabled." | "Garbage collection failed" inside the dialog | changed |
| Filter All, Pinned, Unpinned | none | dropped: see Deliberately dropped |
| Empty state "No CAS entries", "No content-addressable entries found." | "No CAS content stored yet.", or "Nothing on this page, but the driver has more to list.", or "No more entries." | changed |
| Column Hash, cut to 12 characters and "..." | Hash, mono, cut by width with the full hash on hover | changed |
| Column Bucket, a truncated bucket ID | none per row; the Bucket field above | changed |
| Column Key | none | dropped: a CAS blob's key is its hash |
| Column Size | Stored size | migrated |
| Column Refs | References, "none" for a blob the index doesn't know | migrated |
| Column Status: Pinned (default) or Unpinned (outline) | State: Not indexed (destructive), Pinned (secondary), GC candidate (default), Indexed (outline) | changed: a blob that lost its index entry is what you came to find |
| Column Created | Last modified | changed |
| Action Unpin, confirmed with "Unpin this entry? It may be removed during garbage collection." | Unpin, on the engine's index, unconfirmed | changed: it flipped a table GC never reads; an unpin is reversible |
| Action Pin | Pin, on the engine's index | changed |
| Pin and Unpin errors thrown away | "Could not pin", "Could not unpin" | changed |
| No paging | Previous page and Next page, 100 a page | changed |

### Quotas

`pages/quotas.templ`, `components/quota_table.templ`. Dropped: nothing enforces
a quota, nothing measures usage, a quota save zeroes the usage, and creating one
failed on Postgres and SQLite.

| templ | React | status |
|---|---|---|
| Title "Quotas", a count badge, "Storage usage and limits per tenant." | none | dropped |
| Button "Create Quota", the card "Create New Quota" with a close X | none | dropped |
| Fields Tenant Key (required, "tenant-id"), Limit Bytes and Limit Objects ("0 = no limit") | none | dropped |
| Buttons Cancel and Create, error banner, "Tenant key is required." | none | dropped |
| Empty state "No quotas configured", "No tenant quotas have been set up yet." | none | dropped |
| Columns Tenant, Storage Used ("used / limit"), Usage (a bar, amber from 70%, red from 90%, "No limit"), Objects, Updated | none | dropped |
| Action Edit, which sent an action nothing handled | none | dropped |
| Action Delete, confirmed | none | dropped |

### Settings

`pages/settings.templ`, the `renderSettings` helper in `contributor.go`, and the
settings panel `trove-config` declared in `manifest.go`. The page is gone and
its content is on the Overview.

| templ | React | status |
|---|---|---|
| Title "Settings", "Trove storage configuration and feature flags." | the Overview | changed |
| Driver Name, "unknown" when empty | Driver, mono, at the top of the Overview | migrated |
| Health, a green or red dot with Healthy or Unhealthy | `HealthBadge` (Healthy outline, Unhealthy destructive) and the ping's error | changed |
| Capabilities: badges for Multipart Upload, Presigned URLs and Range Reads, only the ones present | "What this driver can do": Folders, Presigned links, Multipart uploads, Range reads, Server-side copy, Versioning, Lifecycle rules, Change notifications, each saying what yes or no means | changed |
| Max Streams | "Stream pool", "N streams at once" | migrated |
| Chunk Size | Chunk size | migrated |
| Presign Support, Supported or Not Supported | the Presigned links line, and on each object the reason a share link is unavailable | changed |
| Storage Driver, the DSN, "memory" when empty | none | dropped: a DSN can carry credentials, and the Overview names the driver |
| Base Path, "/api/storage" when empty (the real default is `/trove`) | none | dropped: it named the REST handler, which is never mounted |
| Default Bucket, "default" when empty | Default bucket, "none" when unset | changed: no invented fallback |
| Feature Flags: CAS Enabled, Encryption and Compression badges, straight from config | Protection: Encryption, Compression, Content scanning and CAS, each Configured against Applied, with a note | changed: the Encryption badge said on while nothing was encrypted |
| No upload settings | Upload limit, ETags and Content links in the Configuration block | changed: new |
| Settings panel `trove-config`, "Trove Settings", "Storage configuration and feature flags", in the Forge dashboard's settings | none | dropped: the shell has no per-plugin settings panel, and the Overview holds what it showed |

### Widgets

`widgets/stats.templ`, `widgets/recent_objects.templ`, `widgets/health.templ`,
and their descriptors in `manifest.go`.

| templ | React | status |
|---|---|---|
| Widget `trove-stats`, "Storage Stats", "Bucket and object counts", size md, every 60 seconds, group Trove: Buckets, Objects, Storage and Uploads | none | dropped: with the stat cards |
| Widget `trove-recent-objects`, "Recent Objects", "Recently stored objects", size lg, every 30 seconds: five keys with their sizes, each opening object detail | none | dropped: with Recent Objects |
| Its empty text "No objects yet." | none | dropped |
| Widget `trove-health`, "System Health", "Driver status and system overview", size sm, every 30 seconds: driver name and a Healthy or Unhealthy dot | the driver and health badge on the Overview | changed |
| Its Buckets and Objects counts | none | dropped: with the stat cards |
| Its CAS "On" or "Off" | the Overview's Protection row for CAS, and the CAS page | changed |

### Navigation and manifest

`manifest.go`, and `components/footer_links.templ` for the sidebar footer.

| templ | React | status |
|---|---|---|
| Name "trove", display name "Trove", icon hard-drive, version "1.0.0", layout "extension", sidebar shown | `index.tsx`: `extension: "trove"`, `namespace: "trove"`, label "Trove", mounted at `/@trove` | changed: the shell decides layout and sidebar |
| Nav Overview, group Overview, priority 0, icon layout-dashboard | Overview, group Storage, priority -10 | changed |
| Nav Buckets, group Storage, priority 10, icon database | Buckets, group Storage, priority 0 | migrated |
| Nav Objects, group Storage, priority 20 | none: the browser is reached from a bucket | changed |
| Nav File Browser, group Storage, priority 25 | none: `/buckets/:bucket` has no nav entry | changed |
| Nav Uploads, group Operations, priority 30 | Transfers, group Storage, priority 30 | changed: see Uploads |
| Nav CAS Index, group Operations, priority 40 | CAS, group Storage, priority 20 | changed |
| Nav Quotas, group Administration, priority 50 | none | dropped: with the page |
| Nav Settings, group Administration, priority 60 | none | dropped: on the Overview |
| No nav for middleware | Middleware, group Storage, priority 10 | changed: new |
| Groups Overview, Storage, Operations, Administration | one group, Storage | changed |
| Nav icons | an icon per item from the kit | migrated |
| Topbar title "Trove", logo icon hard-drive, accent `#3b82f6` | none | dropped: the shell themes every plugin the same way |
| Topbar search (`ShowSearch`) | none | dropped: see Deliberately dropped |
| Topbar action "API Docs" to `/docs` | none | dropped: see Deliberately dropped |
| Sidebar footer "API Docs", tooltip "API Documentation" (`FooterAPIDocsLink`) | none | dropped: same reason |
| Widget and settings descriptors | see Widgets and Settings | dropped |

### Plugin extension points

`plugin_iface.go`, `components/plugin_sections.templ`.

| templ | React | status |
|---|---|---|
| `DashboardPlugin` (`DashboardWidgets`, `DashboardSettingsPanel`, `DashboardPages`), with `PluginWidget` and `PluginPage` | none | dropped: see Deliberately dropped |
| `BucketDetailContributor` and `ObjectDetailContributor` | none | dropped: nothing implements them, and the pages they extended are gone |
| `DashboardPageContributor` | none | dropped |
| `PluginSections`, which rendered a list of contributed sections | none | dropped: never rendered |

### Shared components and helpers

`components/empty_state.templ`, `components/stat_card.templ`,
`components/helpers.templ`, `contributor.go` and `data.go`. Each table
component is covered on the page that rendered it: `BucketTable` on Buckets,
`ObjectTable` on Objects, `UploadTable` on Uploads, `CASTable` on CAS index,
`QuotaTable` on Quotas, and `FileBrowser` on File browser.

| templ | React | status |
|---|---|---|
| `EmptyState`: a 48 pixel muted icon, a title, an optional description | the kit's `EmptyState`, and `ResourceTable`'s `emptyMessage` | changed: no icon |
| `StatCard`: label, icon, value, subtitle | none | dropped: every stat card is |
| `resolveIcon`, mapping 29 icon names, with an info icon for anything else | none | dropped: the nav takes its icons directly |
| `uploadStatusBadge` | none | dropped: with Uploads |
| `driverBadge`: Local, S3, GCS, Azure (default), SFTP and Memory (secondary), anything else as it came | none | dropped: the Overview names the driver in mono |
| `boolBadge`: default when on, outline when off | `FlagStateBadge` for protections: Applied (outline), Not configured (secondary), Configured, not applied (destructive) | changed |
| `pinnedBadge`: Pinned (default), Unpinned (outline) | `CasStateBadge` | changed |
| `fieldRow`, "Not set" for an empty value | the kit's `DescriptionList`, and `NoneCell` | changed |
| `FormatBytes`, "4.7 KB" | `Bytes`: "4,812 B" in mono, "4.7 KiB" on hover | changed |
| `FormatTime` ("Never"), `FormatTimeShort` ("Never"), `FormatDate` ("N/A") | the kit's `Timestamp` | changed |
| `TruncateHash` (12 characters) and `TruncateKey` (the last 37) | truncation by width, with the full value on hover | changed |
| `ContentTypeIcon` | none | dropped |
| The five `...Exported` wrappers. `PinnedBadgeExported` was the only one nothing used. The pages called the other four (`UploadStatusBadgeExported`, `DriverBadgeExported`, `BoolBadgeExported` and `FieldRowExported`) in place of the helpers they wrap, and each of those is covered on the page that used it | none | dropped |
| A whole table row clickable through `hx-get` | the name cell is a link | changed |
| Browser `hx-confirm` on deletes, Unpin and GC | `ConfirmDialog` where a command can't be undone. Errors show inside it, and it can't close while the command runs | changed |
| htmx swaps of `#content` and `hx-push-url` | the shell's router | changed |
| `data.go` reads: every list from the metadata store, 100 rows a bucket by default | every list from the driver through the contract, paged by cursor | changed |
| `contributor.go` actions over `GET` with query parameters | contract commands, one per action | changed |
| `paramValue` and `parseKeyValueParams` (up to 50 pairs) | none | dropped: with the edit forms |

## Still open

These are known gaps. None of them is a regression from the templ pages.

- forge's dashboard tracing records the raw query string, so a download or
  preview ticket in `?t=` is readable in the Traces view for its 60 seconds.
  forge should redact the `t` parameter.
- The content route doesn't work behind the Next.js proxy,
  `@forge-go/dashboard-next` (`packages/next` in the forge-dashboard repo),
  which forwards only GET and POST, caps request bodies at 1 MiB and reads
  responses as text. That's the proxy package's to fix. Until then, use the Vite
  shell or the Go server.
- In a fresh Vite dev server, your first visit to a bucket can hit a
  dependency re-bundle of `react-resizable-panels`, the first plugin
  dependency on it. React logs "Invalid hook call", the plugin's error
  boundary shows, and a reload fixes it. Production builds can't hit this. The
  fix is `optimizeDeps.include` in `apps/shell/vite.config.ts`, which is the
  shell's file, not Trove's.
- golangci-lint reports 21 issues in the extension that predate this
  migration, and we left them alone: 6 in `extension/handler` (two errcheck,
  three noctx in `errors_test.go`, one revive), 10 in `extension/hooks` (one
  gocritic in `warden.go`, nine revive across `chronicle.go`, `dispatch.go`
  and `metrics.go`), 2 errcheck in `extension/model/types.go`, and 3 prealloc
  in `extension/store/memory/store.go`. The 15 that were in
  `extension/dashboard` go with the package.
- Everything listed under Bugs found on the way, apart from the slice 1
  fixes, is still in Trove.
