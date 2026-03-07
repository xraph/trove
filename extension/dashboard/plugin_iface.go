package dashboard

import (
	"context"

	"github.com/a-h/templ"
	"github.com/xraph/forge/extensions/dashboard/contributor"
)

// PluginWidget describes a dashboard widget contributed by a plugin.
type PluginWidget struct {
	ID         string
	Title      string
	Size       string // "sm", "md", "lg"
	RefreshSec int
	Render     func(ctx context.Context) templ.Component
}

// PluginPage describes a dashboard page contributed by a plugin.
type PluginPage struct {
	Route  string // e.g. "/audit-log"
	Label  string // nav label
	Icon   string // lucide icon name
	Render func(ctx context.Context) templ.Component
}

// DashboardPlugin is the interface for plugins that contribute
// dashboard UI elements (widgets, pages, settings panels).
type DashboardPlugin interface {
	DashboardWidgets(ctx context.Context) []PluginWidget
	DashboardSettingsPanel(ctx context.Context) templ.Component
	DashboardPages() []PluginPage
}

// BucketDetailContributor is the interface for plugins that contribute
// additional sections to the bucket detail page.
type BucketDetailContributor interface {
	DashboardBucketDetailSection(ctx context.Context, bucketID string) templ.Component
}

// ObjectDetailContributor is the interface for plugins that contribute
// additional sections to the object detail page.
type ObjectDetailContributor interface {
	DashboardObjectDetailSection(ctx context.Context, objectID string) templ.Component
}

// DashboardPageContributor is the interface for plugins that contribute
// parameterized page routes with full control over rendering.
type DashboardPageContributor interface {
	DashboardNavItems() []contributor.NavItem
	DashboardRenderPage(ctx context.Context, route string, params contributor.Params) (templ.Component, error)
}
