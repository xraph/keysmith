package extension

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/xraph/forge/extensions/dashboard"
	dashcontract "github.com/xraph/forge/extensions/dashboard/contract"
	"github.com/xraph/forge/extensions/dashboard/contract/dispatcher"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/store/memory"
)

// The interface assertion lives in a test file on purpose: importing the
// root dashboard package from production code pulls templ into every
// consumer of the extension (forge's dashboard/contract -> dashboard/auth ->
// templ chain is already there; the root package adds the rest).
var _ dashboard.ContractContributorAware = (*Extension)(nil)

func TestRegisterContractContributor(t *testing.T) {
	eng, err := keysmith.NewEngine(keysmith.WithStore(memory.New()))
	require.NoError(t, err)
	e := New(WithDashboardTenant("acme", "app_1"))
	e.eng = eng
	e.config.Dashboard = DashboardConfig{TenantID: "acme", AppID: "app_1"}
	require.NoError(t, e.RegisterContractContributor(dispatcher.New(nil), dashcontract.NewRegistry(), dashcontract.NewWardenRegistry()))
}

func TestRegisterContractContributorSkipsQuietlyWithoutAnEngine(t *testing.T) {
	e := New()
	assert.NoError(t, e.RegisterContractContributor(dispatcher.New(nil), dashcontract.NewRegistry(), dashcontract.NewWardenRegistry()))
}

func TestDashboardTenantMergesFromOptionsWhenYAMLIsSilent(t *testing.T) {
	e := New(WithDashboardTenant("acme", "app_1"))
	merged := e.mergeConfigurations(Config{}, e.config)
	assert.Equal(t, "acme", merged.Dashboard.TenantID)
	assert.Equal(t, "app_1", merged.Dashboard.AppID)
	fromYAML := e.mergeConfigurations(Config{Dashboard: DashboardConfig{TenantID: "yaml"}}, e.config)
	assert.Equal(t, "yaml", fromYAML.Dashboard.TenantID, "YAML wins for strings, as for base_path")
}
