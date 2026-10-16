package extension

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/xraph/forge"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/store/memory"
)

func scrape(t *testing.T, app forge.App) string {
	t.Helper()
	p, ok := app.Metrics().(interface{ PrometheusHandler() http.Handler })
	require.True(t, ok, "app metrics %T have no Prometheus handler", app.Metrics())
	rr := httptest.NewRecorder()
	p.PrometheusHandler().ServeHTTP(rr, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/_/metrics", nil))
	body, err := io.ReadAll(rr.Body)
	require.NoError(t, err)
	return string(body)
}

// The extension hands the engine the app's metrics, so keysmith's series
// land on /_/metrics under forge's namespace, and its spans go to forge's
// tracer provider.
func TestExtensionWiresTelemetryIntoForge(t *testing.T) {
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})

	app := forge.New(forge.WithAppName("keysmith-telemetry-test"), forge.WithAppLogger(forge.NewNoopLogger()))
	rec := tracetest.NewSpanRecorder()
	require.NoError(t, forge.RegisterSpanProcessor(app, rec))

	ext := New(WithEngineOptions(keysmith.WithStore(memory.New())), WithDisableRoutes())
	require.NoError(t, ext.Register(app))

	ctx := keysmith.WithTenant(context.Background(), "app", "t1")
	created, err := ext.Engine().CreateKey(ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvTest})
	require.NoError(t, err)
	_, err = ext.Engine().ValidateKey(ctx, created.RawKey)
	require.NoError(t, err)

	var names []string
	for _, s := range rec.Ended() {
		if s.InstrumentationScope().Name == keysmith.InstrumentationName {
			names = append(names, s.Name())
		}
	}
	assert.Equal(t, []string{"create key", "validate key"}, names)

	body := scrape(t, app)
	for _, want := range []string{
		`forge_keysmith_key_operations_total{operation="create",result="ok"} 1`,
		`forge_keysmith_key_validations_total{result="ok",via_previous_key="false"} 1`,
		`forge_keysmith_key_validation_duration_seconds_count{result="ok"} 1`,
		`forge_keysmith_key_validation_duration_seconds_bucket{result="ok",le="0.0005"}`,
		`forge_keysmith_key_validation_duration_seconds_bucket{result="ok",le="2.5"}`,
	} {
		assert.Contains(t, body, want)
	}

	// Nothing else under keysmith's names: no series for outcomes that
	// didn't happen, and no stray name.
	var series []string
	for line := range strings.SplitSeq(body, "\n") {
		if strings.HasPrefix(line, "forge_keysmith_") && !strings.Contains(line, "_bucket{") {
			series = append(series, line[:strings.LastIndex(line, " ")])
		}
	}
	assert.ElementsMatch(t, []string{
		`forge_keysmith_key_operations_total{operation="create",result="ok"}`,
		`forge_keysmith_key_validations_total{result="ok",via_previous_key="false"}`,
		`forge_keysmith_key_validation_duration_seconds_sum{result="ok"}`,
		`forge_keysmith_key_validation_duration_seconds_count{result="ok"}`,
		`forge_keysmith_last_used_updates_failed_total`,
	}, series)
}
