package dashboard

import (
	"context"

	"github.com/xraph/forge/extensions/dashboard/contributor"

	"github.com/xraph/keysmith/dashboard/components"
	"github.com/xraph/keysmith/plugin"
)

// NewManifest builds a contributor.Manifest for the keysmith dashboard.
// It starts with the base nav items, widgets, and settings, then merges
// any additional contributions from plugins implementing dashboard.Plugin.
func NewManifest(plugins []plugin.Plugin) *contributor.Manifest {
	m := &contributor.Manifest{
		Name:        "keysmith",
		DisplayName: "Keysmith",
		Icon:        "key-round",
		Version:     "1.0.0",
		Layout:      "extension",
		ShowSidebar: boolPtr(true),
		TopbarConfig: &contributor.TopbarConfig{
			Title:       "Keysmith",
			LogoIcon:    "key-round",
			AccentColor: "#f59e0b",
			ShowSearch:  true,
			Actions: []contributor.TopbarAction{
				{Label: "API Docs", Icon: "file-text", Href: "/docs", Variant: "ghost"},
			},
		},
		Nav:      baseNav(),
		Widgets:  baseWidgets(),
		Settings: baseSettings(),
		Capabilities: []string{
			"searchable",
		},
		SidebarFooterContent: components.FooterAPIDocsLink("/docs"),
	}

	// Merge plugin-contributed nav items and widgets.
	for _, p := range plugins {
		// PageContributor provides nav items for pages with route params.
		if dpc, ok := p.(PageContributor); ok {
			m.Nav = append(m.Nav, dpc.DashboardNavItems()...)
		}

		dp, ok := p.(Plugin)
		if !ok {
			continue
		}

		for _, pp := range dp.DashboardPages() {
			m.Nav = append(m.Nav, contributor.NavItem{
				Label:    pp.Label,
				Path:     pp.Route,
				Icon:     pp.Icon,
				Group:    "Plugins",
				Priority: 10,
			})
		}

		for _, pw := range dp.DashboardWidgets(context.Background()) {
			m.Widgets = append(m.Widgets, contributor.WidgetDescriptor{
				ID:         pw.ID,
				Title:      pw.Title,
				Size:       pw.Size,
				RefreshSec: pw.RefreshSec,
				Group:      "Keysmith",
			})
		}
	}

	return m
}

// baseNav returns the core navigation items for the keysmith dashboard.
func baseNav() []contributor.NavItem {
	return []contributor.NavItem{
		// Overview group
		{Label: "Overview", Path: "/", Icon: "layout-dashboard", Group: "Overview", Priority: 0},

		// Key Management group
		{Label: "API Keys", Path: "/keys", Icon: "key-round", Group: "Key Management", Priority: 0},
		{Label: "Policies", Path: "/policies", Icon: "shield-check", Group: "Key Management", Priority: 1},
		{Label: "Scopes", Path: "/scopes", Icon: "lock", Group: "Key Management", Priority: 2},

		// Analytics group
		{Label: "Usage", Path: "/usage", Icon: "bar-chart-3", Group: "Analytics", Priority: 0},
		{Label: "Rotations", Path: "/rotations", Icon: "refresh-cw", Group: "Analytics", Priority: 1},

		// Configuration group
		{Label: "Settings", Path: "/settings", Icon: "settings", Group: "Configuration", Priority: 0},
	}
}

// baseWidgets returns the core widget descriptors for the keysmith dashboard.
func baseWidgets() []contributor.WidgetDescriptor {
	return []contributor.WidgetDescriptor{
		{
			ID:          "keysmith-stats",
			Title:       "Key Stats",
			Description: "API key counts and status",
			Size:        "md",
			RefreshSec:  60,
			Group:       "Keysmith",
		},
		{
			ID:          "keysmith-recent-keys",
			Title:       "Recent Keys",
			Description: "Recently created API keys",
			Size:        "md",
			RefreshSec:  30,
			Group:       "Keysmith",
		},
		{
			ID:          "keysmith-usage-summary",
			Title:       "Usage Summary",
			Description: "API usage overview",
			Size:        "lg",
			RefreshSec:  15,
			Group:       "Keysmith",
		},
	}
}

// baseSettings returns the core settings descriptors for the keysmith dashboard.
func baseSettings() []contributor.SettingsDescriptor {
	return []contributor.SettingsDescriptor{
		{
			ID:          "keysmith-config",
			Title:       "Keysmith Settings",
			Description: "Engine configuration and plugin information",
			Group:       "Keysmith",
			Icon:        "key-round",
		},
	}
}

func boolPtr(b bool) *bool { return &b }
