package dashboard

import (
	"context"
	"fmt"
	"io"
	"strconv"

	"github.com/a-h/templ"

	"github.com/xraph/forge/extensions/dashboard/contributor"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/dashboard/components"
	"github.com/xraph/keysmith/dashboard/pages"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/plugin"
	"github.com/xraph/keysmith/rotation"
)

// Ensure Contributor implements the required interfaces at compile time.
var _ contributor.LocalContributor = (*Contributor)(nil)

// Contributor implements the dashboard LocalContributor interface for the
// keysmith extension. It renders pages, widgets, and settings using templ
// components and ForgeUI.
type Contributor struct {
	manifest *contributor.Manifest
	engine   *keysmith.Engine
	plugins  []plugin.Plugin
}

// New creates a new keysmith dashboard contributor.
func New(manifest *contributor.Manifest, engine *keysmith.Engine, plugins []plugin.Plugin) *Contributor {
	return &Contributor{
		manifest: manifest,
		engine:   engine,
		plugins:  plugins,
	}
}

// Manifest returns the contributor manifest.
func (c *Contributor) Manifest() *contributor.Manifest { return c.manifest }

// RenderPage renders a page for the given route.
func (c *Contributor) RenderPage(ctx context.Context, route string, params contributor.Params) (templ.Component, error) {
	// Check plugin-contributed pages first (PageContributor).
	for _, p := range c.plugins {
		if dpc, ok := p.(PageContributor); ok {
			if comp, err := dpc.DashboardRenderPage(ctx, route, params); err == nil && comp != nil {
				return comp, nil
			}
		}
	}

	// Check plugin-contributed pages (Plugin).
	for _, dp := range c.dashboardPlugins() {
		for _, pp := range dp.DashboardPages() {
			if pp.Route == route {
				return pp.Render(ctx), nil
			}
		}
	}

	switch route {
	case "/", "":
		return c.renderOverview(ctx)
	case "/keys":
		return c.renderKeys(ctx, params)
	case "/keys/detail":
		return c.renderKeyDetail(ctx, params)
	case "/policies":
		return c.renderPolicies(ctx)
	case "/policies/detail":
		return c.renderPolicyDetail(ctx, params)
	case "/scopes":
		return c.renderScopes(ctx)
	case "/usage":
		return c.renderUsage(ctx)
	case "/usage/detail":
		return c.renderUsageDetail(ctx, params)
	case "/rotations":
		return c.renderRotations(ctx, params)
	case "/settings":
		return c.renderSettings(ctx)
	default:
		return nil, contributor.ErrPageNotFound
	}
}

// RenderWidget renders a widget by ID.
func (c *Contributor) RenderWidget(ctx context.Context, widgetID string) (templ.Component, error) {
	// Check plugin-contributed widgets first.
	for _, dp := range c.dashboardPlugins() {
		for _, w := range dp.DashboardWidgets(ctx) {
			if w.ID == widgetID {
				return w.Render(ctx), nil
			}
		}
	}

	switch widgetID {
	case "keysmith-stats":
		return c.renderStatsWidget(ctx)
	case "keysmith-recent-keys":
		return c.renderRecentKeysWidget(ctx)
	case "keysmith-usage-summary":
		return c.renderUsageSummaryWidget(ctx)
	default:
		return nil, contributor.ErrWidgetNotFound
	}
}

// RenderSettings renders a settings panel by ID.
func (c *Contributor) RenderSettings(ctx context.Context, settingID string) (templ.Component, error) {
	switch settingID {
	case "keysmith-config":
		return c.renderSettingsPanel(ctx)
	default:
		return nil, contributor.ErrSettingNotFound
	}
}

// ─── Private Render Helpers ──────────────────────────────────────────────────

func (c *Contributor) renderOverview(ctx context.Context) (templ.Component, error) {
	stats := fetchKeyStats(ctx, c.engine)
	recentKeys, _ := fetchRecentKeys(ctx, c.engine, 5)

	overviewStats := pages.OverviewStats{
		TotalKeys:     int(stats.Total),
		ActiveKeys:    int(stats.Active),
		TotalPolicies: int(fetchPolicyCount(ctx, c.engine)),
		TotalScopes:   fetchScopeCount(ctx, c.engine),
	}

	pluginSections := c.collectPluginSections(ctx)

	return templ.ComponentFunc(func(tCtx context.Context, w io.Writer) error {
		childCtx := templ.WithChildren(tCtx, components.PluginSections(pluginSections))
		return pages.OverviewPage(overviewStats, recentKeys).Render(childCtx, w)
	}), nil
}

func (c *Contributor) renderKeys(ctx context.Context, params contributor.Params) (templ.Component, error) {
	filter := &key.ListFilter{Limit: 50}

	if env := params.QueryParams["environment"]; env != "" {
		filter.Environment = key.Environment(env)
	}
	if state := params.QueryParams["state"]; state != "" {
		filter.State = key.State(state)
	}

	keys, count, err := fetchKeys(ctx, c.engine, filter)
	if err != nil {
		return nil, fmt.Errorf("dashboard: render keys: %w", err)
	}

	return pages.KeysPage(pages.KeysPageData{
		Keys:        keys,
		TotalCount:  count,
		Environment: params.QueryParams["environment"],
		State:       params.QueryParams["state"],
	}), nil
}

func (c *Contributor) renderKeyDetail(ctx context.Context, params contributor.Params) (templ.Component, error) {
	keyIDStr := params.QueryParams["key_id"]
	if keyIDStr == "" {
		keyIDStr = params.PathParams["id"]
	}
	if keyIDStr == "" {
		return nil, contributor.ErrPageNotFound
	}

	keyID, err := id.ParseKeyID(keyIDStr)
	if err != nil {
		return nil, contributor.ErrPageNotFound
	}

	// Handle actions.
	if action := params.QueryParams["action"]; action != "" {
		switch action {
		case "rotate":
			reason := rotation.Reason(params.QueryParams["reason"])
			if reason == "" {
				reason = rotation.ReasonManual
			}
			if _, actionErr := c.engine.RotateKey(ctx, keyID, reason); actionErr != nil {
				return nil, fmt.Errorf("dashboard: rotate key: %w", actionErr)
			}
		case "revoke":
			if actionErr := c.engine.RevokeKey(ctx, keyID, "Revoked via dashboard"); actionErr != nil {
				return nil, fmt.Errorf("dashboard: revoke key: %w", actionErr)
			}
		case "suspend":
			if actionErr := c.engine.SuspendKey(ctx, keyID); actionErr != nil {
				return nil, fmt.Errorf("dashboard: suspend key: %w", actionErr)
			}
		case "reactivate":
			if actionErr := c.engine.ReactivateKey(ctx, keyID); actionErr != nil {
				return nil, fmt.Errorf("dashboard: reactivate key: %w", actionErr)
			}
		}
	}

	k, err := c.engine.GetKey(ctx, keyID)
	if err != nil {
		return nil, fmt.Errorf("dashboard: resolve key: %w", err)
	}

	data := pages.KeyDetailData{Key: k}

	// Fetch policy if assigned.
	if k.PolicyID != nil {
		pol, polErr := c.engine.GetPolicy(ctx, *k.PolicyID)
		if polErr == nil {
			data.Policy = pol
		}
	}

	// Fetch scopes.
	scopes, _ := fetchKeyScopes(ctx, c.engine, keyID)
	data.Scopes = scopes

	// Fetch rotations.
	rotations, _ := fetchKeyRotations(ctx, c.engine, keyID)
	data.Rotations = rotations

	// Fetch usage.
	usageRecords, usageCount, _ := fetchKeyUsage(ctx, c.engine, keyID, 20)
	data.Usage = usageRecords
	data.UsageCount = usageCount

	// Collect plugin-contributed key detail sections.
	pluginSections := c.collectKeyDetailSections(ctx, keyID)

	return templ.ComponentFunc(func(tCtx context.Context, w io.Writer) error {
		childCtx := templ.WithChildren(tCtx, components.PluginSections(pluginSections))
		return pages.KeyDetailPage(data).Render(childCtx, w)
	}), nil
}

func (c *Contributor) renderPolicies(ctx context.Context) (templ.Component, error) {
	policies, count, err := fetchPolicies(ctx, c.engine)
	if err != nil {
		return nil, fmt.Errorf("dashboard: render policies: %w", err)
	}

	return pages.PoliciesPage(pages.PoliciesPageData{
		Policies:   policies,
		TotalCount: count,
	}), nil
}

func (c *Contributor) renderPolicyDetail(ctx context.Context, params contributor.Params) (templ.Component, error) {
	policyIDStr := params.QueryParams["policy_id"]
	if policyIDStr == "" {
		policyIDStr = params.PathParams["id"]
	}
	if policyIDStr == "" {
		return nil, contributor.ErrPageNotFound
	}

	policyID, err := id.ParsePolicyID(policyIDStr)
	if err != nil {
		return nil, contributor.ErrPageNotFound
	}

	pol, err := c.engine.GetPolicy(ctx, policyID)
	if err != nil {
		return nil, fmt.Errorf("dashboard: resolve policy: %w", err)
	}

	// Fetch keys using this policy.
	keys, _ := c.engine.Store().Keys().ListByPolicy(ctx, policyID)

	return pages.PolicyDetailPage(pages.PolicyDetailData{
		Policy: pol,
		Keys:   keys,
	}), nil
}

func (c *Contributor) renderScopes(ctx context.Context) (templ.Component, error) {
	scopes, err := fetchScopes(ctx, c.engine)
	if err != nil {
		scopes = nil
	}

	return pages.ScopesPage(pages.ScopesPageData{
		Scopes:     scopes,
		TotalCount: len(scopes),
	}), nil
}

func (c *Contributor) renderUsage(ctx context.Context) (templ.Component, error) {
	records, count, _ := fetchRecentUsage(ctx, c.engine, 50)
	aggs, _ := fetchUsageAggregates(ctx, c.engine)

	return pages.UsagePage(pages.UsagePageData{
		Records:      records,
		Aggregations: aggs,
		TotalCount:   count,
	}), nil
}

func (c *Contributor) renderUsageDetail(ctx context.Context, params contributor.Params) (templ.Component, error) {
	keyIDStr := params.QueryParams["key_id"]
	if keyIDStr == "" {
		return nil, contributor.ErrPageNotFound
	}

	keyID, err := id.ParseKeyID(keyIDStr)
	if err != nil {
		return nil, contributor.ErrPageNotFound
	}

	k, err := c.engine.GetKey(ctx, keyID)
	if err != nil {
		return nil, fmt.Errorf("dashboard: resolve key for usage: %w", err)
	}

	records, count, _ := fetchKeyUsage(ctx, c.engine, keyID, 100)
	aggs, _ := fetchKeyAggregates(ctx, c.engine, keyID)

	return pages.UsageDetailPage(pages.UsageDetailData{
		Key:          k,
		Records:      records,
		Aggregations: aggs,
		TotalCount:   count,
	}), nil
}

func (c *Contributor) renderRotations(ctx context.Context, params contributor.Params) (templ.Component, error) {
	filter := &rotation.ListFilter{Limit: 50}
	reasonStr := params.QueryParams["reason"]
	if reasonStr != "" {
		filter.Reason = rotation.Reason(reasonStr)
	}

	rotations, err := fetchRotations(ctx, c.engine, filter)
	if err != nil {
		rotations = nil
	}

	// Build key map for display.
	keyMap := make(map[string]*key.Key)
	for _, r := range rotations {
		kid := r.KeyID.String()
		if _, ok := keyMap[kid]; ok {
			continue
		}
		k, err := c.engine.GetKey(ctx, r.KeyID)
		if err == nil {
			keyMap[kid] = k
		}
	}

	return pages.RotationsPage(pages.RotationsPageData{
		Rotations:  rotations,
		KeyMap:     keyMap,
		TotalCount: len(rotations),
		Reason:     reasonStr,
	}), nil
}

func (c *Contributor) renderSettings(ctx context.Context) (templ.Component, error) {
	pluginNames := make([]string, 0, len(c.plugins))
	for _, p := range c.plugins {
		pluginNames = append(pluginNames, p.Name())
	}

	// Determine store driver.
	storeDriver := "unknown"
	if c.engine.Store() != nil {
		if err := c.engine.Store().Ping(ctx); err == nil {
			storeDriver = "connected"
		}
	}

	data := pages.SettingsPageData{
		PluginNames: pluginNames,
		StoreDriver: storeDriver,
	}

	pluginSettings := c.collectPluginSettings(ctx)

	return templ.ComponentFunc(func(tCtx context.Context, w io.Writer) error {
		childCtx := templ.WithChildren(tCtx, components.PluginSections(pluginSettings))
		return pages.SettingsPage(data).Render(childCtx, w)
	}), nil
}

// ─── Widget Render Helpers ───────────────────────────────────────────────────

func (c *Contributor) renderStatsWidget(ctx context.Context) (templ.Component, error) {
	stats := fetchKeyStats(ctx, c.engine)
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := fmt.Fprintf(w,
			`<div class="grid grid-cols-2 gap-4 p-4">`+
				`<div class="space-y-1"><span class="text-sm text-muted-foreground">Total Keys</span><p class="text-2xl font-bold">%d</p></div>`+
				`<div class="space-y-1"><span class="text-sm text-muted-foreground">Active</span><p class="text-2xl font-bold">%d</p></div>`+
				`</div>`,
			stats.Total, stats.Active)
		return err
	}), nil
}

func (c *Contributor) renderRecentKeysWidget(ctx context.Context) (templ.Component, error) {
	keys, _ := fetchRecentKeys(ctx, c.engine, 5)
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		if len(keys) == 0 {
			_, err := io.WriteString(w, `<p class="text-sm text-muted-foreground p-4 text-center">No keys yet.</p>`)
			return err
		}
		_, _ = io.WriteString(w, `<ul class="divide-y">`)
		for _, k := range keys {
			_, _ = fmt.Fprintf(w,
				`<li class="flex items-center justify-between px-4 py-2">`+
					`<span class="text-sm font-medium">%s</span>`+
					`<code class="text-xs bg-muted px-1.5 py-0.5 rounded-sm">****%s</code>`+
					`</li>`,
				k.Name, k.Hint)
		}
		_, err := io.WriteString(w, `</ul>`)
		return err
	}), nil
}

func (c *Contributor) renderUsageSummaryWidget(ctx context.Context) (templ.Component, error) {
	_, count, _ := fetchRecentUsage(ctx, c.engine, 1)
	return templ.ComponentFunc(func(_ context.Context, w io.Writer) error {
		_, err := fmt.Fprintf(w,
			`<div class="p-4 space-y-1">`+
				`<span class="text-sm text-muted-foreground">Total Requests</span>`+
				`<p class="text-2xl font-bold">%s</p>`+
				`</div>`,
			strconv.FormatInt(count, 10))
		return err
	}), nil
}

// ─── Settings Render Helper ──────────────────────────────────────────────────

func (c *Contributor) renderSettingsPanel(_ context.Context) (templ.Component, error) {
	pluginNames := make([]string, 0, len(c.plugins))
	for _, p := range c.plugins {
		pluginNames = append(pluginNames, p.Name())
	}
	return pages.SettingsPage(pages.SettingsPageData{
		PluginNames: pluginNames,
		StoreDriver: "connected",
	}), nil
}

// ─── Plugin Helpers ──────────────────────────────────────────────────────────

// dashboardPlugins returns all registered plugins that implement Plugin.
func (c *Contributor) dashboardPlugins() []Plugin {
	var dps []Plugin
	for _, p := range c.plugins {
		if dp, ok := p.(Plugin); ok {
			dps = append(dps, dp)
		}
	}
	return dps
}

// collectPluginSections gathers rendered templ components from all dashboard plugins.
func (c *Contributor) collectPluginSections(ctx context.Context) []templ.Component {
	var sections []templ.Component
	for _, dp := range c.dashboardPlugins() {
		for _, w := range dp.DashboardWidgets(ctx) {
			sections = append(sections, w.Render(ctx))
		}
	}
	return sections
}

// collectPluginSettings gathers settings panels from all dashboard plugins.
func (c *Contributor) collectPluginSettings(ctx context.Context) []templ.Component {
	var panels []templ.Component
	for _, dp := range c.dashboardPlugins() {
		if panel := dp.DashboardSettingsPanel(ctx); panel != nil {
			panels = append(panels, panel)
		}
	}
	return panels
}

// collectKeyDetailSections gathers key detail sections from plugins implementing KeyDetailContributor.
func (c *Contributor) collectKeyDetailSections(ctx context.Context, keyID id.KeyID) []templ.Component {
	var sections []templ.Component
	for _, p := range c.plugins {
		if kdc, ok := p.(KeyDetailContributor); ok {
			if section := kdc.DashboardKeyDetailSection(ctx, keyID); section != nil {
				sections = append(sections, section)
			}
		}
	}
	return sections
}
