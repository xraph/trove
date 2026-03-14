package dashboard

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/xraph/forge/extensions/dashboard/contributor"

	"github.com/xraph/trove"
	"github.com/xraph/trove/driver"
	"github.com/xraph/trove/extension/dashboard/components"
	"github.com/xraph/trove/extension/dashboard/pages"
	"github.com/xraph/trove/extension/dashboard/widgets"
	"github.com/xraph/trove/extension/model"
	"github.com/xraph/trove/extension/store"
	"github.com/xraph/trove/id"
)

// Ensure Contributor implements the required interfaces at compile time.
var _ contributor.LocalContributor = (*Contributor)(nil)

// Contributor implements the dashboard LocalContributor interface for the
// trove extension. It renders pages, widgets, and settings using templ
// components and ForgeUI.
type Contributor struct {
	manifest *contributor.Manifest
	store    store.Store
	trove    *trove.Trove
	config   ContributorConfig
}

// ContributorConfig holds configuration details surfaced on the settings page.
type ContributorConfig struct {
	StorageDriver    string
	BasePath         string
	DefaultBucket    string
	CASEnabled       bool
	Encryption       bool
	Compression      bool
	PresignSupported bool
}

// New creates a new trove dashboard contributor.
func New(manifest *contributor.Manifest, s store.Store, t *trove.Trove, cfg ContributorConfig) *Contributor {
	return &Contributor{
		manifest: manifest,
		store:    s,
		trove:    t,
		config:   cfg,
	}
}

// Manifest returns the contributor manifest.
func (c *Contributor) Manifest() *contributor.Manifest { return c.manifest }

// RenderPage renders a page for the given route.
func (c *Contributor) RenderPage(ctx context.Context, route string, params contributor.Params) (templ.Component, error) {
	switch route {
	case "/", "":
		return c.renderOverview(ctx)
	case "/buckets":
		return c.renderBuckets(ctx, params)
	case "/buckets/detail":
		return c.renderBucketDetail(ctx, params)
	case "/objects":
		return c.renderObjects(ctx, params)
	case "/objects/detail":
		return c.renderObjectDetail(ctx, params)
	case "/browser":
		return c.renderFileBrowser(ctx, params)
	case "/uploads":
		return c.renderUploads(ctx, params)
	case "/uploads/detail":
		return c.renderUploadDetail(ctx, params)
	case "/cas":
		return c.renderCAS(ctx, params)
	case "/quotas":
		return c.renderQuotas(ctx, params)
	case "/settings":
		return c.renderSettings(ctx)
	default:
		return nil, contributor.ErrPageNotFound
	}
}

// RenderWidget renders a widget by ID.
func (c *Contributor) RenderWidget(ctx context.Context, widgetID string) (templ.Component, error) {
	switch widgetID {
	case "trove-stats":
		return c.renderStatsWidget(ctx)
	case "trove-recent-objects":
		return c.renderRecentObjectsWidget(ctx)
	case "trove-health":
		return c.renderHealthWidget(ctx)
	default:
		return nil, contributor.ErrWidgetNotFound
	}
}

// RenderSettings renders a settings panel by ID.
func (c *Contributor) RenderSettings(ctx context.Context, settingID string) (templ.Component, error) {
	switch settingID {
	case "trove-config":
		return c.renderSettings(ctx)
	default:
		return nil, contributor.ErrSettingNotFound
	}
}

// ─── Private Render Helpers ──────────────────────────────────────────────────

func (c *Contributor) renderOverview(ctx context.Context) (templ.Component, error) {
	stats := fetchStorageStats(ctx, c.store)
	recentObjects, _ := fetchRecentObjects(ctx, c.store, 5)

	return pages.OverviewPage(pages.OverviewData{
		Stats: pages.OverviewStats{
			TotalBuckets:  stats.TotalBuckets,
			TotalObjects:  stats.TotalObjects,
			TotalSize:     stats.TotalSize,
			ActiveUploads: stats.ActiveUploads,
			CASEntries:    stats.CASEntries,
		},
		RecentObjects: recentObjects,
	}), nil
}

func (c *Contributor) renderBuckets(ctx context.Context, params contributor.Params) (templ.Component, error) {
	action := params.QueryParams["action"]

	// Show the create form.
	if action == "show_form" {
		buckets, _ := fetchBuckets(ctx, c.store)
		return pages.BucketsPage(pages.BucketsPageData{
			Buckets:    buckets,
			TotalCount: len(buckets),
			ShowForm:   true,
		}), nil
	}

	// Handle create bucket action.
	if action == "create" {
		name := paramValue(params, "name")
		if name != "" {
			// Create bucket on the storage driver.
			if err := c.trove.CreateBucket(ctx, name); err != nil {
				return c.bucketsPageWithError(ctx, fmt.Sprintf("Failed to create bucket: %v", err), true)
			}

			// Record in metadata store.
			bucket := &model.Bucket{
				ID:         id.NewBucketID().String(),
				Name:       name,
				Driver:     c.config.StorageDriver,
				Region:     paramValue(params, "region"),
				Versioning: paramValue(params, "versioning") == "on",
				CASEnabled: paramValue(params, "cas_enabled") == "on",
			}
			if qb := paramValue(params, "quota_bytes"); qb != "" {
				bucket.QuotaBytes, _ = strconv.ParseInt(qb, 10, 64)
			}
			if qo := paramValue(params, "quota_objects"); qo != "" {
				bucket.QuotaObjects, _ = strconv.ParseInt(qo, 10, 64)
			}
			if err := c.store.CreateBucket(ctx, bucket); err != nil {
				return c.bucketsPageWithError(ctx, fmt.Sprintf("Bucket created on driver but failed to store metadata: %v", err), false)
			}
		} else {
			return c.bucketsPageWithError(ctx, "Bucket name is required.", true)
		}
	}

	buckets, err := fetchBuckets(ctx, c.store)
	if err != nil {
		buckets = nil
	}

	return pages.BucketsPage(pages.BucketsPageData{
		Buckets:    buckets,
		TotalCount: len(buckets),
	}), nil
}

func (c *Contributor) bucketsPageWithError(ctx context.Context, errMsg string, showForm bool) (templ.Component, error) {
	buckets, _ := fetchBuckets(ctx, c.store)
	return pages.BucketsPage(pages.BucketsPageData{
		Buckets:      buckets,
		TotalCount:   len(buckets),
		ErrorMessage: errMsg,
		ShowForm:     showForm,
	}), nil
}

func (c *Contributor) renderBucketDetail(ctx context.Context, params contributor.Params) (templ.Component, error) {
	bucketID := params.QueryParams["bucket_id"]
	if bucketID == "" {
		return nil, contributor.ErrPageNotFound
	}

	// Handle actions.
	if action := params.QueryParams["action"]; action != "" {
		switch action {
		case "delete":
			if err := c.store.DeleteBucket(ctx, bucketID); err != nil {
				return nil, fmt.Errorf("dashboard: delete bucket: %w", err)
			}
			return c.renderBuckets(ctx, params)

		case "edit":
			return c.bucketDetailPage(ctx, bucketID, true, "")

		case "save":
			bucket, err := fetchBucket(ctx, c.store, bucketID)
			if err != nil {
				return nil, fmt.Errorf("dashboard: resolve bucket for save: %w", err)
			}
			bucket.Versioning = paramValue(params, "versioning") == "on"
			bucket.CASEnabled = paramValue(params, "cas_enabled") == "on"
			bucket.Region = paramValue(params, "region")
			if qb := paramValue(params, "quota_bytes"); qb != "" {
				bucket.QuotaBytes, _ = strconv.ParseInt(qb, 10, 64)
			}
			if qo := paramValue(params, "quota_objects"); qo != "" {
				bucket.QuotaObjects, _ = strconv.ParseInt(qo, 10, 64)
			}
			if err := c.store.UpdateBucket(ctx, bucket); err != nil {
				return c.bucketDetailPage(ctx, bucketID, true, fmt.Sprintf("Failed to save: %v", err))
			}
			return c.bucketDetailPage(ctx, bucketID, false, "")

		case "save_lifecycle":
			// Lifecycle rules are saved as JSON on the bucket.
			bucket, err := fetchBucket(ctx, c.store, bucketID)
			if err != nil {
				return nil, fmt.Errorf("dashboard: resolve bucket for lifecycle: %w", err)
			}
			lifecycleJSON := paramValue(params, "lifecycle")
			if lifecycleJSON != "" {
				bucket.Lifecycle = []byte(lifecycleJSON)
			} else {
				bucket.Lifecycle = nil
			}
			if err := c.store.UpdateBucket(ctx, bucket); err != nil {
				return c.bucketDetailPage(ctx, bucketID, false, fmt.Sprintf("Failed to save lifecycle: %v", err))
			}
			return c.bucketDetailPage(ctx, bucketID, false, "")
		}
	}

	return c.bucketDetailPage(ctx, bucketID, false, "")
}

func (c *Contributor) bucketDetailPage(ctx context.Context, bucketID string, editMode bool, errMsg string) (templ.Component, error) {
	bucket, err := fetchBucket(ctx, c.store, bucketID)
	if err != nil {
		return nil, fmt.Errorf("dashboard: resolve bucket: %w", err)
	}

	objects, _ := fetchObjects(ctx, c.store, bucketID)
	objectCount := len(objects)
	var totalSize int64
	for _, o := range objects {
		totalSize += o.Size
	}

	return pages.BucketDetailPage(pages.BucketDetailData{
		Bucket:       bucket,
		ObjectCount:  objectCount,
		TotalSize:    totalSize,
		Objects:      objects,
		EditMode:     editMode,
		ErrorMessage: errMsg,
	}), nil
}

func (c *Contributor) renderObjects(ctx context.Context, params contributor.Params) (templ.Component, error) {
	bucketID := params.QueryParams["bucket_id"]
	prefix := params.QueryParams["prefix"]

	var objects []*model.Object
	var err error

	if bucketID != "" {
		var opts []store.ListOption
		if prefix != "" {
			opts = append(opts, store.WithPrefix(prefix))
		}
		objects, err = fetchObjects(ctx, c.store, bucketID, opts...)
	} else {
		objects, err = fetchAllObjects(ctx, c.store, 100)
	}
	if err != nil {
		objects = nil
	}

	// Fetch buckets for the filter dropdown.
	buckets, _ := fetchBuckets(ctx, c.store)

	return pages.ObjectsPage(pages.ObjectsPageData{
		Objects:    objects,
		Buckets:    buckets,
		BucketID:   bucketID,
		Prefix:     prefix,
		TotalCount: len(objects),
	}), nil
}

func (c *Contributor) renderObjectDetail(ctx context.Context, params contributor.Params) (templ.Component, error) {
	objectID := params.QueryParams["object_id"]
	if objectID == "" {
		return nil, contributor.ErrPageNotFound
	}

	// Handle actions.
	if action := params.QueryParams["action"]; action != "" {
		switch action {
		case "delete":
			if err := c.store.SoftDeleteObject(ctx, objectID); err != nil {
				return nil, fmt.Errorf("dashboard: delete object: %w", err)
			}
			return c.renderObjects(ctx, params)

		case "edit_metadata":
			return c.objectDetailPage(ctx, objectID, true, false, "")

		case "edit_tags":
			return c.objectDetailPage(ctx, objectID, false, true, "")

		case "save_metadata":
			obj, err := fetchObject(ctx, c.store, objectID)
			if err != nil {
				return nil, fmt.Errorf("dashboard: resolve object: %w", err)
			}
			obj.Metadata = parseKeyValueParams(params, "meta")
			if err := c.store.UpdateObject(ctx, obj); err != nil {
				return c.objectDetailPage(ctx, objectID, true, false, fmt.Sprintf("Failed to save: %v", err))
			}
			return c.objectDetailPage(ctx, objectID, false, false, "")

		case "save_tags":
			obj, err := fetchObject(ctx, c.store, objectID)
			if err != nil {
				return nil, fmt.Errorf("dashboard: resolve object: %w", err)
			}
			obj.Tags = parseKeyValueParams(params, "tag")
			if err := c.store.UpdateObject(ctx, obj); err != nil {
				return c.objectDetailPage(ctx, objectID, false, true, fmt.Sprintf("Failed to save: %v", err))
			}
			return c.objectDetailPage(ctx, objectID, false, false, "")

		case "copy":
			obj, err := fetchObject(ctx, c.store, objectID)
			if err != nil {
				return nil, fmt.Errorf("dashboard: resolve object for copy: %w", err)
			}
			srcBucket, err := fetchBucket(ctx, c.store, obj.BucketID)
			if err != nil {
				return c.objectDetailPage(ctx, objectID, false, false, "Could not resolve source bucket.")
			}

			dstBucketID := paramValue(params, "dest_bucket_id")
			dstKey := paramValue(params, "dest_key")
			if dstBucketID == "" || dstKey == "" {
				return c.objectDetailPage(ctx, objectID, false, false, "Destination bucket and key are required.")
			}

			// Resolve destination bucket name from ID.
			dstBucket, err := fetchBucket(ctx, c.store, dstBucketID)
			if err != nil {
				return c.objectDetailPage(ctx, objectID, false, false, "Could not resolve destination bucket.")
			}
			dstBucketName := dstBucket.Name

			info, copyErr := c.trove.Copy(ctx, srcBucket.Name, obj.Key, dstBucketName, dstKey)
			if copyErr != nil {
				return c.objectDetailPage(ctx, objectID, false, false, fmt.Sprintf("Copy failed: %v", copyErr))
			}

			// Record the copied object in metadata store.
			newObj := &model.Object{
				ID:          id.NewObjectID().String(),
				BucketID:    dstBucketID,
				Key:         dstKey,
				Size:        info.Size,
				ContentType: info.ContentType,
				ETag:        info.ETag,
				Driver:      obj.Driver,
			}
			_ = c.store.CreateObject(ctx, newObj)

			return c.objectDetailPage(ctx, objectID, false, false, "")
		}
	}

	return c.objectDetailPage(ctx, objectID, false, false, "")
}

func (c *Contributor) objectDetailPage(ctx context.Context, objectID string, editMeta, editTags bool, errMsg string) (templ.Component, error) {
	obj, err := fetchObject(ctx, c.store, objectID)
	if err != nil {
		return nil, fmt.Errorf("dashboard: resolve object: %w", err)
	}

	buckets, _ := fetchBuckets(ctx, c.store)

	return pages.ObjectDetailPage(pages.ObjectDetailData{
		Object:           obj,
		EditingMetadata:  editMeta,
		EditingTags:      editTags,
		ErrorMessage:     errMsg,
		Buckets:          buckets,
		APIBasePath:      c.config.BasePath,
		PresignSupported: c.config.PresignSupported,
	}), nil
}

func (c *Contributor) renderFileBrowser(ctx context.Context, params contributor.Params) (templ.Component, error) {
	bucketID := params.QueryParams["bucket_id"]
	prefix := params.QueryParams["prefix"]

	// Fetch buckets for selector.
	buckets, _ := fetchBuckets(ctx, c.store)

	// Resolve bucket name for API calls.
	var bucketName string
	if bucketID != "" {
		for _, b := range buckets {
			if b.ID == bucketID {
				bucketName = b.Name
				break
			}
		}
	}

	var entries []components.BrowserEntry
	if bucketID != "" {
		var opts []store.ListOption
		if prefix != "" {
			opts = append(opts, store.WithPrefix(prefix))
		}
		objects, err := fetchObjects(ctx, c.store, bucketID, opts...)
		if err == nil {
			entries = components.BuildBrowserEntries(objects, prefix)
		}
	}

	breadcrumbs := components.BuildBreadcrumbs(prefix)

	return pages.BrowserPage(pages.BrowserPageData{
		BucketID:    bucketID,
		BucketName:  bucketName,
		Prefix:      prefix,
		Entries:     entries,
		Breadcrumbs: breadcrumbs,
		Buckets:     buckets,
		APIBasePath: c.config.BasePath,
	}), nil
}

func (c *Contributor) renderUploads(ctx context.Context, params contributor.Params) (templ.Component, error) {
	uploads, err := fetchUploads(ctx, c.store)
	if err != nil {
		uploads = nil
	}

	statusFilter := params.QueryParams["status"]

	// Filter by status if specified.
	if statusFilter != "" {
		var filtered []*model.UploadSession
		for _, u := range uploads {
			if string(u.Status) == statusFilter {
				filtered = append(filtered, u)
			}
		}
		uploads = filtered
	}

	return pages.UploadsPage(pages.UploadsPageData{
		Uploads:      uploads,
		TotalCount:   len(uploads),
		StatusFilter: statusFilter,
	}), nil
}

func (c *Contributor) renderUploadDetail(ctx context.Context, params contributor.Params) (templ.Component, error) {
	uploadID := params.QueryParams["upload_id"]
	if uploadID == "" {
		return nil, contributor.ErrPageNotFound
	}

	// Handle actions.
	if action := params.QueryParams["action"]; action != "" {
		switch action {
		case "abort":
			upload, err := c.store.GetUploadSession(ctx, uploadID)
			if err != nil {
				return nil, fmt.Errorf("dashboard: get upload for abort: %w", err)
			}
			upload.Status = model.UploadStatusAborted
			if err := c.store.UpdateUploadSession(ctx, upload); err != nil {
				return nil, fmt.Errorf("dashboard: abort upload: %w", err)
			}
			return c.renderUploads(ctx, params)
		}
	}

	upload, err := fetchUpload(ctx, c.store, uploadID)
	if err != nil {
		return nil, fmt.Errorf("dashboard: resolve upload: %w", err)
	}

	return pages.UploadDetailPage(pages.UploadDetailData{
		Upload: upload,
	}), nil
}

func (c *Contributor) renderCAS(ctx context.Context, params contributor.Params) (templ.Component, error) {
	filter := params.QueryParams["filter"]
	var gcResult *pages.GCResultData

	// Handle CAS actions.
	if action := params.QueryParams["action"]; action != "" {
		hash := params.QueryParams["hash"]
		switch action {
		case "pin":
			if hash != "" {
				_ = c.store.SetCASPinned(ctx, hash, true)
			}
		case "unpin":
			if hash != "" {
				_ = c.store.SetCASPinned(ctx, hash, false)
			}
		case "gc":
			if c.trove.CAS() != nil {
				result, err := c.trove.CAS().GC(ctx)
				if err != nil {
					gcResult = &pages.GCResultData{ErrorMessage: err.Error()}
				} else {
					gcResult = &pages.GCResultData{
						Scanned:    result.Scanned,
						Deleted:    result.Deleted,
						FreedBytes: result.FreedBytes,
					}
				}
			} else {
				gcResult = &pages.GCResultData{ErrorMessage: "CAS is not enabled."}
			}
		}
	}

	var entries []*model.CASEntry
	var err error

	switch filter {
	case "unpinned":
		entries, err = c.store.ListUnpinnedCAS(ctx)
	default:
		entries, err = fetchCASEntries(ctx, c.store)
	}
	if err != nil {
		entries = nil
	}

	// Filter pinned-only in memory if requested.
	if filter == "pinned" {
		var pinned []*model.CASEntry
		for _, e := range entries {
			if e.Pinned {
				pinned = append(pinned, e)
			}
		}
		entries = pinned
	}

	return pages.CASPage(pages.CASPageData{
		Entries:    entries,
		TotalCount: len(entries),
		Filter:     filter,
		GCResult:   gcResult,
		CASEnabled: c.config.CASEnabled,
	}), nil
}

func (c *Contributor) renderQuotas(ctx context.Context, params contributor.Params) (templ.Component, error) {
	var errMsg string
	var showForm bool

	// Handle quota actions.
	if action := params.QueryParams["action"]; action != "" {
		switch action {
		case "show_form":
			showForm = true
		case "create", "save":
			tenantKey := paramValue(params, "tenant_key")
			if tenantKey == "" {
				errMsg = "Tenant key is required."
				showForm = true
			} else {
				q := &model.Quota{TenantKey: tenantKey}
				if lb := paramValue(params, "limit_bytes"); lb != "" {
					q.LimitBytes, _ = strconv.ParseInt(lb, 10, 64)
				}
				if lo := paramValue(params, "limit_objects"); lo != "" {
					q.LimitObjects, _ = strconv.ParseInt(lo, 10, 64)
				}
				if err := c.store.SetQuota(ctx, q); err != nil {
					errMsg = fmt.Sprintf("Failed to save quota: %v", err)
					showForm = true
				}
			}
		case "delete":
			tenantKey := params.QueryParams["tenant_key"]
			if tenantKey != "" {
				_ = c.store.DeleteQuota(ctx, tenantKey)
			}
		}
	}

	quotas, err := fetchQuotas(ctx, c.store)
	if err != nil {
		quotas = nil
	}

	return pages.QuotasPage(pages.QuotasPageData{
		Quotas:       quotas,
		TotalCount:   len(quotas),
		ErrorMessage: errMsg,
		ShowForm:     showForm,
	}), nil
}

func (c *Contributor) renderSettings(ctx context.Context) (templ.Component, error) {
	// Detect driver capabilities.
	var capabilities []string
	if _, ok := c.trove.Driver().(driver.MultipartDriver); ok {
		capabilities = append(capabilities, "Multipart Upload")
	}
	if _, ok := c.trove.Driver().(driver.PresignDriver); ok {
		capabilities = append(capabilities, "Presigned URLs")
	}
	if _, ok := c.trove.Driver().(driver.RangeDriver); ok {
		capabilities = append(capabilities, "Range Reads")
	}

	// Check driver health.
	driverHealthy := c.trove.Health(ctx) == nil

	// Pool stats.
	cfg := c.trove.Config()

	return pages.SettingsPage(pages.SettingsPageData{
		StorageDriver:      c.config.StorageDriver,
		BasePath:           c.config.BasePath,
		DefaultBucket:      c.config.DefaultBucket,
		CASEnabled:         c.config.CASEnabled,
		Encryption:         c.config.Encryption,
		Compression:        c.config.Compression,
		DriverName:         c.trove.Driver().Name(),
		DriverHealthy:      driverHealthy,
		DriverCapabilities: capabilities,
		PoolMaxStreams:     cfg.PoolSize,
		ChunkSize:          cfg.ChunkSize,
		PresignSupported:   c.config.PresignSupported,
	}), nil
}

// ─── Widget Render Helpers ───────────────────────────────────────────────────

func (c *Contributor) renderStatsWidget(ctx context.Context) (templ.Component, error) {
	stats := fetchStorageStats(ctx, c.store)
	return widgets.StatsWidget(widgets.StatsWidgetData{
		TotalBuckets:  stats.TotalBuckets,
		TotalObjects:  stats.TotalObjects,
		TotalSize:     components.FormatBytes(stats.TotalSize),
		ActiveUploads: stats.ActiveUploads,
	}), nil
}

func (c *Contributor) renderRecentObjectsWidget(ctx context.Context) (templ.Component, error) {
	objects, _ := fetchRecentObjects(ctx, c.store, 5)
	return widgets.RecentObjectsWidget(widgets.RecentObjectsWidgetData{
		Objects: objects,
	}), nil
}

func (c *Contributor) renderHealthWidget(ctx context.Context) (templ.Component, error) {
	driverHealthy := c.trove.Health(ctx) == nil
	stats := fetchStorageStats(ctx, c.store)
	return widgets.HealthWidget(widgets.HealthWidgetData{
		DriverName:    c.trove.Driver().Name(),
		DriverHealthy: driverHealthy,
		CASEnabled:    c.config.CASEnabled,
		TotalBuckets:  stats.TotalBuckets,
		TotalObjects:  stats.TotalObjects,
	}), nil
}

// ─── Param Helpers ───────────────────────────────────────────────────────────

// paramValue returns a value from FormData first, then QueryParams.
func paramValue(params contributor.Params, key string) string {
	if v, ok := params.FormData[key]; ok && v != "" {
		return v
	}
	return params.QueryParams[key]
}

// parseKeyValueParams extracts key-value pairs from form params with a prefix.
// e.g. prefix="meta" reads meta_key_0, meta_val_0, meta_key_1, meta_val_1, etc.
func parseKeyValueParams(params contributor.Params, prefix string) map[string]string {
	result := make(map[string]string)
	for i := 0; i < 50; i++ {
		k := paramValue(params, prefix+"_key_"+strconv.Itoa(i))
		v := paramValue(params, prefix+"_val_"+strconv.Itoa(i))
		if k != "" {
			result[k] = v
		}
	}
	return result
}

// Suppress unused import warnings during incremental development.
var (
	_ = strings.TrimSpace
	_ = time.Now
)
