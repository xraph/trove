package dashboard

import (
	"github.com/xraph/forge/extensions/dashboard/contributor"
	"github.com/xraph/trove/extension/dashboard/components"
)

// NewManifest builds a contributor.Manifest for the trove dashboard.
func NewManifest() *contributor.Manifest {
	return &contributor.Manifest{
		Name:        "trove",
		DisplayName: "Trove",
		Icon:        "hard-drive",
		Version:     "1.0.0",
		Layout:      "extension",
		ShowSidebar: boolPtr(true),
		TopbarConfig: &contributor.TopbarConfig{
			Title:       "Trove",
			LogoIcon:    "hard-drive",
			AccentColor: "#3b82f6",
			ShowSearch:  true,
			Actions: []contributor.TopbarAction{
				{Label: "API Docs", Icon: "file-text", Href: "/docs", Variant: "ghost"},
			},
		},
		Nav:                  baseNav(),
		Widgets:              baseWidgets(),
		Settings:             baseSettings(),
		SidebarFooterContent: components.FooterAPIDocsLink("/docs"),
	}
}

// baseNav returns the core navigation items for the trove dashboard.
func baseNav() []contributor.NavItem {
	return []contributor.NavItem{
		// Overview
		{Label: "Overview", Path: "/", Icon: "layout-dashboard", Group: "Overview", Priority: 0},

		// Storage
		{Label: "Buckets", Path: "/buckets", Icon: "database", Group: "Storage", Priority: 10},
		{Label: "Objects", Path: "/objects", Icon: "file", Group: "Storage", Priority: 20},
		{Label: "File Browser", Path: "/browser", Icon: "folder-open", Group: "Storage", Priority: 25},

		// Operations
		{Label: "Uploads", Path: "/uploads", Icon: "upload", Group: "Operations", Priority: 30},
		{Label: "CAS Index", Path: "/cas", Icon: "fingerprint", Group: "Operations", Priority: 40},

		// Administration
		{Label: "Quotas", Path: "/quotas", Icon: "gauge", Group: "Administration", Priority: 50},
		{Label: "Settings", Path: "/settings", Icon: "settings", Group: "Administration", Priority: 60},
	}
}

// baseWidgets returns the core widget descriptors for the trove dashboard.
func baseWidgets() []contributor.WidgetDescriptor {
	return []contributor.WidgetDescriptor{
		{
			ID:          "trove-stats",
			Title:       "Storage Stats",
			Description: "Bucket and object counts",
			Size:        "md",
			RefreshSec:  60,
			Group:       "Trove",
		},
		{
			ID:          "trove-recent-objects",
			Title:       "Recent Objects",
			Description: "Recently stored objects",
			Size:        "lg",
			RefreshSec:  30,
			Group:       "Trove",
		},
		{
			ID:          "trove-health",
			Title:       "System Health",
			Description: "Driver status and system overview",
			Size:        "sm",
			RefreshSec:  30,
			Group:       "Trove",
		},
	}
}

// baseSettings returns the core settings descriptors for the trove dashboard.
func baseSettings() []contributor.SettingsDescriptor {
	return []contributor.SettingsDescriptor{
		{
			ID:          "trove-config",
			Title:       "Trove Settings",
			Description: "Storage configuration and feature flags",
			Group:       "Trove",
			Icon:        "hard-drive",
		},
	}
}

func boolPtr(b bool) *bool { return &b }
