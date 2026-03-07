package dashboard

import (
	"context"
	"fmt"

	"github.com/a-h/templ"

	"github.com/xraph/forge/extensions/dashboard/contributor"

	"github.com/xraph/trove/extension/dashboard/components"
	"github.com/xraph/trove/extension/dashboard/pages"
	"github.com/xraph/trove/extension/dashboard/widgets"
	"github.com/xraph/trove/extension/model"
	"github.com/xraph/trove/extension/store"
)

// Ensure Contributor implements the required interfaces at compile time.
var _ contributor.LocalContributor = (*Contributor)(nil)

// Contributor implements the dashboard LocalContributor interface for the
// trove extension. It renders pages, widgets, and settings using templ
// components and ForgeUI.
type Contributor struct {
	manifest *contributor.Manifest
	store    *store.Store
	config   ContributorConfig
}

// ContributorConfig holds configuration details surfaced on the settings page.
type ContributorConfig struct {
	StorageDriver string
	BasePath      string
	DefaultBucket string
	CASEnabled    bool
	Encryption    bool
	Compression   bool
}

// New creates a new trove dashboard contributor.
func New(manifest *contributor.Manifest, s *store.Store, cfg ContributorConfig) *Contributor {
	return &Contributor{
		manifest: manifest,
		store:    s,
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
		return c.renderQuotas(ctx)
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
	buckets, err := fetchBuckets(ctx, c.store)
	if err != nil {
		buckets = nil
	}

	return pages.BucketsPage(pages.BucketsPageData{
		Buckets:    buckets,
		TotalCount: len(buckets),
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
		}
	}

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
		Bucket:      bucket,
		ObjectCount: objectCount,
		TotalSize:   totalSize,
		Objects:     objects,
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
		}
	}

	obj, err := fetchObject(ctx, c.store, objectID)
	if err != nil {
		return nil, fmt.Errorf("dashboard: resolve object: %w", err)
	}

	return pages.ObjectDetailPage(pages.ObjectDetailData{
		Object: obj,
	}), nil
}

func (c *Contributor) renderFileBrowser(ctx context.Context, params contributor.Params) (templ.Component, error) {
	bucketID := params.QueryParams["bucket_id"]
	prefix := params.QueryParams["prefix"]

	// Fetch buckets for selector.
	buckets, _ := fetchBuckets(ctx, c.store)

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
		Prefix:      prefix,
		Entries:     entries,
		Breadcrumbs: breadcrumbs,
		Buckets:     buckets,
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
	}), nil
}

func (c *Contributor) renderQuotas(ctx context.Context) (templ.Component, error) {
	quotas, err := fetchQuotas(ctx, c.store)
	if err != nil {
		quotas = nil
	}

	return pages.QuotasPage(pages.QuotasPageData{
		Quotas:     quotas,
		TotalCount: len(quotas),
	}), nil
}

func (c *Contributor) renderSettings(ctx context.Context) (templ.Component, error) {
	return pages.SettingsPage(pages.SettingsPageData{
		StorageDriver: c.config.StorageDriver,
		BasePath:      c.config.BasePath,
		DefaultBucket: c.config.DefaultBucket,
		CASEnabled:    c.config.CASEnabled,
		Encryption:    c.config.Encryption,
		Compression:   c.config.Compression,
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
