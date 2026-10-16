package keysmith_test

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	gometrics "github.com/xraph/go-utils/metrics"

	"github.com/xraph/keysmith"
	"github.com/xraph/keysmith/id"
	"github.com/xraph/keysmith/key"
	"github.com/xraph/keysmith/policy"
	"github.com/xraph/keysmith/rotation"
	"github.com/xraph/keysmith/store/memory"
)

// ── Spans ────────────────────────────────────────

// recordSpans makes a SpanRecorder the global tracer provider for the rest
// of the test and puts the old provider back afterwards.
func recordSpans(t *testing.T) *tracetest.SpanRecorder {
	t.Helper()
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})
	return rec
}

// ksSpans returns the ended spans keysmith started, in the order they ended.
func ksSpans(rec *tracetest.SpanRecorder) []sdktrace.ReadOnlySpan {
	var out []sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.InstrumentationScope().Name == keysmith.InstrumentationName {
			out = append(out, s)
		}
	}
	return out
}

func spanNames(spans []sdktrace.ReadOnlySpan) []string {
	names := make([]string, len(spans))
	for i, s := range spans {
		names[i] = s.Name()
	}
	return names
}

// onlySpan returns the one keysmith span called name.
func onlySpan(t *testing.T, rec *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	var found []sdktrace.ReadOnlySpan
	for _, s := range ksSpans(rec) {
		if s.Name() == name {
			found = append(found, s)
		}
	}
	require.Len(t, found, 1, "spans called %q among %v", name, spanNames(ksSpans(rec)))
	return found[0]
}

func attrsOf(kvs []attribute.KeyValue) map[string]any {
	m := make(map[string]any, len(kvs))
	for _, kv := range kvs {
		m[string(kv.Key)] = kv.Value.AsInterface()
	}
	return m
}

func spanAttrs(s sdktrace.ReadOnlySpan) map[string]any { return attrsOf(s.Attributes()) }

// ── Metrics ──────────────────────────────────────

// fakeMetrics is a go-utils metrics sink that keeps every counter and
// histogram keysmith declares, keyed by name and labels, so a test can read
// them back. Every Inc is also sent on incs.
type fakeMetrics struct {
	*gometrics.MockMetrics

	mu         sync.Mutex
	counters   map[string]*gometrics.MockCounter
	histograms map[string]*fakeHistogram
	incs       chan string
}

type fakeHistogram struct {
	*gometrics.MockHistogram
	opts gometrics.MetricOptions
}

type fakeCounter struct {
	*gometrics.MockCounter
	key string
	f   *fakeMetrics
}

func (c fakeCounter) Inc() {
	c.MockCounter.Inc()
	select {
	case c.f.incs <- c.key:
	default:
	}
}

func seriesKey(name string, labels map[string]string) string {
	parts := make([]string, 0, len(labels))
	for _, k := range slices.Sorted(maps.Keys(labels)) {
		parts = append(parts, fmt.Sprintf("%s=%s", k, labels[k]))
	}
	return name + "{" + strings.Join(parts, ",") + "}"
}

func newFakeMetrics() *fakeMetrics {
	f := &fakeMetrics{
		MockMetrics: gometrics.NewMockMetrics(),
		counters:    map[string]*gometrics.MockCounter{},
		histograms:  map[string]*fakeHistogram{},
		incs:        make(chan string, 256),
	}
	f.CounterFunc = func(name string, opts ...gometrics.MetricOption) gometrics.Counter {
		var o gometrics.MetricOptions
		for _, opt := range opts {
			opt(&o)
		}
		k := seriesKey(name, o.Labels)
		f.mu.Lock()
		defer f.mu.Unlock()
		c, ok := f.counters[k]
		if !ok {
			c = gometrics.NewMockCounter()
			f.counters[k] = c
		}
		return fakeCounter{MockCounter: c, key: k, f: f}
	}
	f.HistogramFunc = func(name string, opts ...gometrics.MetricOption) gometrics.Histogram {
		var o gometrics.MetricOptions
		for _, opt := range opts {
			opt(&o)
		}
		k := seriesKey(name, o.Labels)
		f.mu.Lock()
		defer f.mu.Unlock()
		h, ok := f.histograms[k]
		if !ok {
			h = &fakeHistogram{MockHistogram: gometrics.NewMockHistogram(), opts: o}
			f.histograms[k] = h
		}
		return h
	}
	return f
}

// counts returns every counter series with a non-zero value.
func (f *fakeMetrics) counts() map[string]float64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := map[string]float64{}
	for k, c := range f.counters {
		if v := c.Value(); v != 0 {
			out[k] = v
		}
	}
	return out
}

func (f *fakeMetrics) histogram(k string) *fakeHistogram {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.histograms[k]
}

// every series name and label set the sink has seen.
func (f *fakeMetrics) seriesKeys() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Concat(slices.Collect(maps.Keys(f.counters)), slices.Collect(maps.Keys(f.histograms)))
}

// ── Fakes ────────────────────────────────────────

// limiter answers every Allow with allowed and err.
type limiter struct {
	allowed bool
	err     error
}

func (l limiter) Allow(context.Context, string, int, time.Duration) (bool, error) {
	return l.allowed, l.err
}

func (limiter) Remaining(context.Context, string, int, time.Duration) (int, error) { return 0, nil }

// brokenKeys fails the key reads and writes it is told to, the way a store
// that lost its connection would. By default it fails with errLeaky, whose
// text quotes a value, as driver errors do, so a test can check that text
// never reaches telemetry. Set err to fail with something else.
type brokenKeys struct {
	key.Store
	get, getByHash, listExpired, updateLastUsed bool
	updateStateOf                               map[string]bool // key IDs whose UpdateState fails
	err                                         error
}

var errLeaky = errors.New("injected: connection lost while reading alice@example.com")

func (b brokenKeys) fail() error {
	if b.err != nil {
		return b.err
	}
	return errLeaky
}

func (b brokenKeys) Get(ctx context.Context, keyID id.KeyID) (*key.Key, error) {
	if b.get {
		return nil, b.fail()
	}
	return b.Store.Get(ctx, keyID)
}

func (b brokenKeys) GetByHash(ctx context.Context, hash string) (*key.Key, error) {
	if b.getByHash {
		return nil, b.fail()
	}
	return b.Store.GetByHash(ctx, hash)
}

func (b brokenKeys) ListExpired(ctx context.Context, before time.Time) ([]*key.Key, error) {
	if b.listExpired {
		return nil, b.fail()
	}
	return b.Store.ListExpired(ctx, before)
}

func (b brokenKeys) UpdateState(ctx context.Context, keyID id.KeyID, state key.State) error {
	if b.updateStateOf[keyID.String()] {
		return b.fail()
	}
	return b.Store.UpdateState(ctx, keyID, state)
}

func (b brokenKeys) UpdateLastUsed(ctx context.Context, keyID id.KeyID, at time.Time) error {
	if b.updateLastUsed {
		return b.fail()
	}
	return b.Store.UpdateLastUsed(ctx, keyID, at)
}

// errCancelled is a cancellation as a store wraps it.
var errCancelled = fmt.Errorf("keysmith/postgres: get key: %w", context.Canceled)

// failingHook fails every key-created hook.
type failingHook struct{}

func (failingHook) Name() string { return "failing-hook" }

func (failingHook) OnKeyCreated(context.Context, *key.Key) error { return errLeaky }

func limitedKey(t *testing.T, eng *keysmith.Engine, ctx context.Context) *key.CreateResult { //nolint:revive // matches mustCreate
	t.Helper()
	pol := &policy.Policy{Name: "limited", RateLimit: 10, RateLimitWindow: time.Minute}
	require.NoError(t, eng.CreatePolicy(ctx, pol))
	return mustCreate(t, eng, ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive, PolicyID: &pol.ID})
}

// ── Tests ────────────────────────────────────────

func TestValidateKeyTracesUnderTheCallersSpan(t *testing.T) {
	rec := recordSpans(t)
	f := newFakeMetrics()
	eng, ctx := newEngine(t, memory.New(), keysmith.WithMetrics(f), keysmith.WithRateLimiter(limiter{allowed: true}))
	created := limitedKey(t, eng, ctx)

	reqCtx, req := otel.Tracer("test").Start(ctx, "handle request")
	_, err := eng.ValidateKey(reqCtx, created.RawKey)
	req.End()
	require.NoError(t, err)

	validate := onlySpan(t, rec, "validate key")
	assert.Equal(t, trace.SpanKindInternal, validate.SpanKind())
	assert.Equal(t, req.SpanContext().SpanID(), validate.Parent().SpanID(), "validate key is a child of the caller's span")
	assert.Equal(t, map[string]any{
		"keysmith.result":                      "ok",
		"keysmith.validation.via_previous_key": false,
	}, spanAttrs(validate))
	assert.Equal(t, codes.Unset, validate.Status().Code)

	limit := onlySpan(t, rec, "check rate limit")
	assert.Equal(t, trace.SpanKindInternal, limit.SpanKind())
	assert.Equal(t, validate.SpanContext().SpanID(), limit.Parent().SpanID(), "check rate limit is a child of validate key")
	assert.Equal(t, map[string]any{"keysmith.result": "allowed"}, spanAttrs(limit))

	counts := f.counts()
	assert.InDelta(t, 1, counts["keysmith_key_validations_total{result=ok,via_previous_key=false}"], 0)
	assert.InDelta(t, 1, counts["keysmith_rate_limit_checks_total{result=allowed}"], 0)
	assert.InDelta(t, 1, counts["keysmith_key_operations_total{operation=create,result=ok}"], 0)

	h := f.histogram("keysmith_key_validation_duration_seconds{result=ok}")
	require.NotNil(t, h)
	assert.Equal(t, uint64(1), h.Count())
	assert.Equal(t, "s", h.opts.Unit)
	assert.Equal(t, []float64{0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5}, h.opts.Buckets)
}

func TestValidateKeyOutcomes(t *testing.T) {
	type setup struct {
		eng *keysmith.Engine
		ctx context.Context
		raw string
	}
	cases := []struct {
		name      string
		prepare   func(t *testing.T, f *fakeMetrics) setup
		wantErr   error
		result    string
		failed    bool     // validate key carries an error status
		limit     string   // result on check rate limit, "" for no such span
		limitErr  bool     // check rate limit carries an error status
		alsoCount []string // counter series that must read 1
	}{
		{
			name: "unknown key",
			prepare: func(t *testing.T, f *fakeMetrics) setup {
				eng, ctx := newEngine(t, memory.New(), keysmith.WithMetrics(f))
				return setup{eng, ctx, "sk_" + "live_" + "nosuchkeynosuchkeynosuchkey00"}
			},
			wantErr: keysmith.ErrInvalidKey,
			result:  "invalid",
		},
		{
			name: "suspended key",
			prepare: func(t *testing.T, f *fakeMetrics) setup {
				eng, ctx := newEngine(t, memory.New(), keysmith.WithMetrics(f))
				r := mustCreate(t, eng, ctx, nil)
				require.NoError(t, eng.SuspendKey(ctx, r.Key.ID))
				return setup{eng, ctx, r.RawKey}
			},
			wantErr: keysmith.ErrKeyInactive,
			result:  "inactive",
		},
		{
			name: "expired key",
			prepare: func(t *testing.T, f *fakeMetrics) setup {
				eng, ctx := newEngine(t, memory.New(), keysmith.WithMetrics(f))
				past := time.Now().Add(-time.Minute)
				r := mustCreate(t, eng, ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive, ExpiresAt: &past})
				return setup{eng, ctx, r.RawKey}
			},
			wantErr:   keysmith.ErrKeyExpired,
			result:    "expired",
			alsoCount: []string{"keysmith_key_operations_total{operation=expire,result=ok}"},
		},
		{
			name: "rate limited",
			prepare: func(t *testing.T, f *fakeMetrics) setup {
				eng, ctx := newEngine(t, memory.New(), keysmith.WithMetrics(f), keysmith.WithRateLimiter(limiter{allowed: false}))
				return setup{eng, ctx, limitedKey(t, eng, ctx).RawKey}
			},
			wantErr:   keysmith.ErrRateLimited,
			result:    "rate_limited",
			limit:     "denied",
			alsoCount: []string{"keysmith_rate_limit_checks_total{result=denied}"},
		},
		{
			name: "rate limiter fails",
			prepare: func(t *testing.T, f *fakeMetrics) setup {
				eng, ctx := newEngine(t, memory.New(), keysmith.WithMetrics(f), keysmith.WithRateLimiter(limiter{err: errLeaky}))
				return setup{eng, ctx, limitedKey(t, eng, ctx).RawKey}
			},
			wantErr:   keysmith.ErrRateLimited,
			result:    "rate_limited",
			limit:     "error",
			limitErr:  true,
			alsoCount: []string{"keysmith_rate_limit_checks_total{result=error}"},
		},
		{
			// The caller still gets ErrInvalidKey, as before. The telemetry
			// is where an outage stops looking like a run of bad keys.
			name: "store fails",
			prepare: func(t *testing.T, f *fakeMetrics) setup {
				s := memory.New()
				plain, ctx := newEngine(t, s)
				r := mustCreate(t, plain, ctx, nil)
				eng, _ := newEngine(t, failingStore{Store: s, keys: brokenKeys{Store: s.Keys(), getByHash: true}}, keysmith.WithMetrics(f))
				return setup{eng, ctx, r.RawKey}
			},
			wantErr: keysmith.ErrInvalidKey,
			result:  "error",
			failed:  true,
		},
		{
			// A client that hung up is not an outage, so it stays out of
			// result="error".
			name: "caller cancels during lookup",
			prepare: func(t *testing.T, f *fakeMetrics) setup {
				s := memory.New()
				plain, ctx := newEngine(t, s)
				r := mustCreate(t, plain, ctx, nil)
				eng, _ := newEngine(t, failingStore{Store: s, keys: brokenKeys{Store: s.Keys(), getByHash: true, err: errCancelled}}, keysmith.WithMetrics(f))
				return setup{eng, ctx, r.RawKey}
			},
			wantErr: keysmith.ErrInvalidKey,
			result:  "cancelled",
		},
		{
			name: "caller cancels during the rate limit check",
			prepare: func(t *testing.T, f *fakeMetrics) setup {
				eng, ctx := newEngine(t, memory.New(), keysmith.WithMetrics(f), keysmith.WithRateLimiter(limiter{err: errCancelled}))
				return setup{eng, ctx, limitedKey(t, eng, ctx).RawKey}
			},
			wantErr:   keysmith.ErrRateLimited,
			result:    "cancelled",
			limit:     "cancelled",
			alsoCount: []string{"keysmith_rate_limit_checks_total{result=cancelled}"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeMetrics()
			s := tc.prepare(t, f)
			rec := recordSpans(t)

			_, err := s.eng.ValidateKey(s.ctx, s.raw)
			require.ErrorIs(t, err, tc.wantErr)

			validate := onlySpan(t, rec, "validate key")
			assert.True(t, !validate.Parent().IsValid(), "no caller span, so validate key is a root")
			assert.Equal(t, tc.result, spanAttrs(validate)["keysmith.result"])
			if tc.failed {
				assert.Equal(t, codes.Error, validate.Status().Code)
				assert.Equal(t, "*errors.errorString", validate.Status().Description)
				assert.Equal(t, "*errors.errorString", spanAttrs(validate)["error.type"])
				require.Len(t, validate.Events(), 1)
				assert.Equal(t, "exception", validate.Events()[0].Name)
				assert.Equal(t, map[string]any{"exception.type": "*errors.errorString"}, attrsOf(validate.Events()[0].Attributes))
			} else {
				assert.Equal(t, codes.Unset, validate.Status().Code, "a refused key is an answer, not a failure")
			}

			if tc.limit == "" {
				assert.NotContains(t, spanNames(ksSpans(rec)), "check rate limit")
			} else {
				limit := onlySpan(t, rec, "check rate limit")
				assert.Equal(t, tc.limit, spanAttrs(limit)["keysmith.result"])
				assert.Equal(t, tc.limitErr, limit.Status().Code == codes.Error)
			}

			counts := f.counts()
			assert.InDelta(t, 1, counts["keysmith_key_validations_total{result="+tc.result+",via_previous_key=false}"], 0)
			for _, k := range tc.alsoCount {
				assert.InDelta(t, 1, counts[k], 0, k)
			}
			h := f.histogram("keysmith_key_validation_duration_seconds{result=" + tc.result + "}")
			require.NotNil(t, h)
			assert.Equal(t, uint64(1), h.Count())
		})
	}
}

func TestRotationTraceAndPreviousKeyValidation(t *testing.T) {
	f := newFakeMetrics()
	eng, ctx := newEngine(t, memory.New(), keysmith.WithMetrics(f))
	orig := mustCreate(t, eng, ctx, nil)
	rec := recordSpans(t)

	_, err := eng.RotateKey(ctx, orig.Key.ID, rotation.ReasonCompromise, keysmith.WithGrace(time.Hour))
	require.NoError(t, err)
	_, err = eng.RotateKey(ctx, orig.Key.ID, rotation.Reason("because alice@example.com said so"), keysmith.WithGrace(time.Hour))
	require.NoError(t, err)
	vr, err := eng.ValidateKey(ctx, orig.RawKey)
	require.NoError(t, err)
	require.True(t, vr.ViaPreviousKey)

	var reasons []any
	for _, s := range ksSpans(rec) {
		if s.Name() != "rotate key" {
			continue
		}
		a := spanAttrs(s)
		reasons = append(reasons, a["keysmith.rotation.reason"])
		assert.Equal(t, "ok", a["keysmith.result"])
		assert.Equal(t, int64(3600), a["keysmith.rotation.grace_seconds"])
	}
	assert.Equal(t, []any{"compromise", "other"}, reasons, "a reason outside the four is clamped")

	assert.Equal(t, true, spanAttrs(onlySpan(t, rec, "validate key"))["keysmith.validation.via_previous_key"])
	counts := f.counts()
	assert.InDelta(t, 2, counts["keysmith_key_operations_total{operation=rotate,result=ok}"], 0)
	assert.InDelta(t, 1, counts["keysmith_key_validations_total{result=ok,via_previous_key=true}"], 0)
}

func TestLifecycleOperationResults(t *testing.T) {
	f := newFakeMetrics()
	s := memory.New()
	eng, ctx := newEngine(t, s, keysmith.WithMetrics(f))
	r := mustCreate(t, eng, ctx, nil)
	rec := recordSpans(t)

	require.NoError(t, eng.SuspendKey(ctx, r.Key.ID))
	require.ErrorIs(t, eng.SuspendKey(ctx, r.Key.ID), keysmith.ErrInvalidStateTransition)
	require.NoError(t, eng.ReactivateKey(ctx, r.Key.ID))
	require.ErrorIs(t, eng.ReactivateKey(ctx, id.NewKeyID()), keysmith.ErrKeyNotFound)
	_, err := eng.RotateKey(ctx, r.Key.ID, rotation.ReasonManual, keysmith.WithGrace(time.Hour))
	require.NoError(t, err)
	require.NoError(t, eng.RevokeKey(ctx, r.Key.ID, "no longer needed"))
	require.ErrorIs(t, eng.RevokeKey(ctx, r.Key.ID, "again"), keysmith.ErrInvalidStateTransition)

	// A suspend that lost a race to a revoke.
	other := mustCreate(t, eng, ctx, nil)
	raced := racedEngine(t, s, func(o *keysmith.Engine, ctx context.Context) error {
		return o.RevokeKey(ctx, other.Key.ID, "raced")
	})
	require.ErrorIs(t, raced.SuspendKey(ctx, other.Key.ID), keysmith.ErrKeyConflict)

	// A rotation whose record can't be written is a real failure.
	broken, _ := newEngine(t, failRotationCreate{s}, keysmith.WithMetrics(f))
	third := mustCreate(t, eng, ctx, nil)
	_, err = broken.RotateKey(ctx, third.Key.ID, rotation.ReasonManual)
	require.Error(t, err)

	// A caller that cancelled is neither.
	gone, _ := newEngine(t, failingStore{Store: s, keys: brokenKeys{Store: s.Keys(), get: true, err: errCancelled}}, keysmith.WithMetrics(f))
	require.ErrorIs(t, gone.SuspendKey(ctx, third.Key.ID), context.Canceled)

	type got struct{ name, result string }
	var spans []got
	for _, sp := range ksSpans(rec) {
		if sp.Name() == "validate key" {
			continue
		}
		res, _ := spanAttrs(sp)["keysmith.result"].(string)
		spans = append(spans, got{sp.Name(), res})
		assert.Equal(t, res == "error", sp.Status().Code == codes.Error, "%s %s: only a real failure marks the span", sp.Name(), res)
	}
	assert.Equal(t, []got{
		{"suspend key", "ok"},
		{"suspend key", "rejected"},
		{"reactivate key", "ok"},
		{"reactivate key", "not_found"},
		{"rotate key", "ok"},
		{"revoke key", "ok"},
		{"revoke key", "rejected"},
		{"create key", "ok"},
		{"revoke key", "ok"},        // the racing revoke
		{"suspend key", "conflict"}, // the one that lost
		{"create key", "ok"},
		{"rotate key", "error"},
		{"suspend key", "cancelled"},
	}, spans)

	var revoked sdktrace.ReadOnlySpan
	for _, sp := range ksSpans(rec) {
		if sp.Name() == "revoke key" {
			revoked = sp
			break
		}
	}
	require.NotNil(t, revoked)
	assert.Equal(t, int64(1), spanAttrs(revoked)["keysmith.rotation.grace_windows_closed"], "revoking ends the open grace window")

	counts := f.counts()
	for k, want := range map[string]float64{
		"keysmith_key_operations_total{operation=create,result=ok}":            3,
		"keysmith_key_operations_total{operation=suspend,result=ok}":           1,
		"keysmith_key_operations_total{operation=suspend,result=rejected}":     1,
		"keysmith_key_operations_total{operation=reactivate,result=ok}":        1,
		"keysmith_key_operations_total{operation=reactivate,result=not_found}": 1,
		"keysmith_key_operations_total{operation=rotate,result=ok}":            1,
		"keysmith_key_operations_total{operation=rotate,result=error}":         1,
		"keysmith_key_operations_total{operation=revoke,result=ok}":            1,
		"keysmith_key_operations_total{operation=revoke,result=rejected}":      1,
		"keysmith_key_operations_total{operation=suspend,result=cancelled}":    1,
	} {
		assert.InDelta(t, want, counts[k], 0, k)
	}
}

func TestCleanupExpiredKeysTrace(t *testing.T) {
	t.Run("nothing to do starts no span", func(t *testing.T) {
		eng, ctx := newEngine(t, memory.New())
		mustCreate(t, eng, ctx, nil)
		rec := recordSpans(t)
		require.NoError(t, eng.CleanupExpiredKeys(ctx))
		assert.Empty(t, ksSpans(rec))
	})

	t.Run("a run with work is one root span linked to the caller", func(t *testing.T) {
		f := newFakeMetrics()
		eng, ctx := newEngine(t, memory.New(), keysmith.WithMetrics(f))
		past := time.Now().Add(-time.Minute)
		for range 2 {
			mustCreate(t, eng, ctx, &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive, ExpiresAt: &past})
		}
		rec := recordSpans(t)

		jobCtx, job := otel.Tracer("test").Start(ctx, "scheduled job")
		require.NoError(t, eng.CleanupExpiredKeys(jobCtx))
		job.End()

		sweep := onlySpan(t, rec, "expire keys")
		assert.False(t, sweep.Parent().IsValid(), "expire keys is a root span")
		require.Len(t, sweep.Links(), 1)
		assert.Equal(t, job.SpanContext().SpanID(), sweep.Links()[0].SpanContext.SpanID())
		assert.Equal(t, map[string]any{"keysmith.keys.expired": int64(2), "keysmith.keys.failed": int64(0)}, spanAttrs(sweep))
		assert.Equal(t, codes.Unset, sweep.Status().Code)
		assert.InDelta(t, 2, f.counts()["keysmith_key_operations_total{operation=expire,result=ok}"], 0)
	})

	t.Run("a key that fails to expire marks the run", func(t *testing.T) {
		f := newFakeMetrics()
		s := memory.New()
		plain, ctx := newEngine(t, s)
		past := time.Now().Add(-time.Minute)
		in := &keysmith.CreateKeyInput{Name: "k", Prefix: "sk", Environment: key.EnvLive, ExpiresAt: &past}
		bad := mustCreate(t, plain, ctx, in)
		mustCreate(t, plain, ctx, in)
		eng, _ := newEngine(t, failingStore{Store: s, keys: brokenKeys{Store: s.Keys(), updateStateOf: map[string]bool{bad.Key.ID.String(): true}}}, keysmith.WithMetrics(f))
		rec := recordSpans(t)

		require.NoError(t, eng.CleanupExpiredKeys(ctx), "one key failing doesn't fail the run")

		sweep := onlySpan(t, rec, "expire keys")
		assert.Equal(t, map[string]any{
			"keysmith.keys.expired": int64(1),
			"keysmith.keys.failed":  int64(1),
			"error.type":            "*errors.errorString",
		}, spanAttrs(sweep))
		assert.Equal(t, codes.Error, sweep.Status().Code)
		assert.Equal(t, "*errors.errorString", sweep.Status().Description)
		require.Len(t, sweep.Events(), 1)
		assert.Equal(t, map[string]any{"exception.type": "*errors.errorString"}, attrsOf(sweep.Events()[0].Attributes))
		counts := f.counts()
		assert.InDelta(t, 1, counts["keysmith_key_operations_total{operation=expire,result=ok}"], 0)
		assert.InDelta(t, 1, counts["keysmith_key_operations_total{operation=expire,result=error}"], 0)
	})

	t.Run("a cancelled listing starts no span", func(t *testing.T) {
		s := memory.New()
		eng, ctx := newEngine(t, failingStore{Store: s, keys: brokenKeys{Store: s.Keys(), listExpired: true, err: errCancelled}})
		rec := recordSpans(t)
		require.ErrorIs(t, eng.CleanupExpiredKeys(ctx), context.Canceled)
		assert.Empty(t, ksSpans(rec))
	})

	t.Run("a failed listing is recorded", func(t *testing.T) {
		s := memory.New()
		eng, ctx := newEngine(t, failingStore{Store: s, keys: brokenKeys{Store: s.Keys(), listExpired: true}})
		rec := recordSpans(t)
		require.Error(t, eng.CleanupExpiredKeys(ctx))
		sweep := onlySpan(t, rec, "expire keys")
		assert.Equal(t, codes.Error, sweep.Status().Code)
		assert.Equal(t, "*errors.errorString", sweep.Status().Description)
	})
}

func TestFailedHookIsCountedAndNoted(t *testing.T) {
	f := newFakeMetrics()
	eng, ctx := newEngine(t, memory.New(), keysmith.WithMetrics(f), keysmith.WithExtension(failingHook{}))
	rec := recordSpans(t)

	mustCreate(t, eng, ctx, nil) // the hook's failure doesn't fail the create

	create := onlySpan(t, rec, "create key")
	assert.Equal(t, "ok", spanAttrs(create)["keysmith.result"])
	assert.Equal(t, codes.Unset, create.Status().Code)
	require.Len(t, create.Events(), 1)
	assert.Equal(t, "hook failed", create.Events()[0].Name)
	assert.Equal(t, map[string]any{
		"keysmith.hook.event": "key_created",
		"exception.type":      "*errors.errorString",
	}, attrsOf(create.Events()[0].Attributes))
	assert.InDelta(t, 1, f.counts()["keysmith_hook_errors_total{event=key_created}"], 0)
}

func TestFailedLastUsedWriteIsCounted(t *testing.T) {
	f := newFakeMetrics()
	s := memory.New()
	plain, ctx := newEngine(t, s)
	r := mustCreate(t, plain, ctx, nil)
	eng, _ := newEngine(t, failingStore{Store: s, keys: brokenKeys{Store: s.Keys(), updateLastUsed: true}}, keysmith.WithMetrics(f))

	_, err := eng.ValidateKey(ctx, r.RawKey)
	require.NoError(t, err, "a failed last-used write never fails validation")

	const want = "keysmith_last_used_updates_failed_total{}"
	for {
		select {
		case k := <-f.incs:
			if k == want {
				return
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s never counted; counters: %v", want, f.counts())
		}
	}
}

// An engine built without WithMetrics has no instruments at all. Every
// path that records must cope with that.
func TestEngineWithoutMetricsStillWorks(t *testing.T) {
	eng, ctx := newEngine(t, memory.New(), keysmith.WithRateLimiter(limiter{allowed: true}), keysmith.WithExtension(failingHook{}))
	limitedKey(t, eng, ctx)
	r := mustCreate(t, eng, ctx, nil)
	_, err := eng.ValidateKey(ctx, r.RawKey)
	require.NoError(t, err)
	require.NoError(t, eng.RevokeKey(ctx, r.Key.ID, "done"))
	require.NoError(t, eng.CleanupExpiredKeys(ctx))
}

// Nothing a caller supplies, and no part of a key, may reach a span or a
// metric label. Every value a caller controls is set to an email address,
// the store's errors quote one, and the key's raw value, hash, hint and ID
// are checked too.
func TestTelemetryCarriesNoPersonalDataOrKeyMaterial(t *testing.T) {
	const email = "alice@example.com"
	f := newFakeMetrics()
	s := memory.New()
	eng, err := keysmith.NewEngine(keysmith.WithStore(s), keysmith.WithMetrics(f),
		keysmith.WithRateLimiter(limiter{allowed: true}), keysmith.WithExtension(failingHook{}))
	require.NoError(t, err)
	ctx := keysmith.WithTenant(context.Background(), email, email)
	rec := recordSpans(t)

	pol := &policy.Policy{Name: email, RateLimit: 10, RateLimitWindow: time.Minute}
	require.NoError(t, eng.CreatePolicy(ctx, pol))
	created := mustCreate(t, eng, ctx, &keysmith.CreateKeyInput{
		Name: email, Description: email, Prefix: "sk", Environment: key.EnvLive,
		PolicyID: &pol.ID, CreatedBy: email, Metadata: map[string]any{"owner": email},
	})
	rotated, err := eng.RotateKey(ctx, created.Key.ID, rotation.Reason(email), keysmith.WithRotatedBy(email), keysmith.WithGrace(time.Hour))
	require.NoError(t, err)
	_, err = eng.ValidateKey(ctx, created.RawKey) // the previous key, inside its window
	require.NoError(t, err)
	_, err = eng.ValidateKey(ctx, rotated.RawKey)
	require.NoError(t, err)
	_, err = eng.ValidateKey(ctx, "sk_"+"live_"+email)
	require.ErrorIs(t, err, keysmith.ErrInvalidKey)
	require.NoError(t, eng.RevokeKey(ctx, created.Key.ID, email))

	broken, _ := newEngine(t, failingStore{Store: s, keys: brokenKeys{Store: s.Keys(), getByHash: true, listExpired: true}}, keysmith.WithMetrics(f))
	_, err = broken.ValidateKey(ctx, rotated.RawKey)
	require.ErrorIs(t, err, keysmith.ErrInvalidKey)
	require.Error(t, broken.CleanupExpiredKeys(ctx))

	k, err := eng.GetKey(ctx, created.Key.ID)
	require.NoError(t, err)
	forbidden := []string{
		email, created.RawKey, rotated.RawKey, created.Key.KeyHash, k.KeyHash,
		created.Key.ID.String(), pol.ID.String(), errLeaky.Error(),
	}
	hints := []string{created.Key.Hint, k.Hint}

	var seen []string
	add := func(where string, kvs []attribute.KeyValue) {
		for _, kv := range kvs {
			seen = append(seen, where+" "+string(kv.Key)+"="+kv.Value.String())
			for _, h := range hints {
				assert.NotEqual(t, h, kv.Value.String(), "%s %s carries a key hint", where, kv.Key)
			}
		}
	}
	spans := ksSpans(rec)
	require.NotEmpty(t, spans)
	for _, sp := range spans {
		seen = append(seen, sp.Name(), sp.Status().Description)
		add(sp.Name(), sp.Attributes())
		for _, ev := range sp.Events() {
			seen = append(seen, ev.Name)
			add(sp.Name()+" event "+ev.Name, ev.Attributes)
		}
		for _, l := range sp.Links() {
			add(sp.Name()+" link", l.Attributes)
		}
	}
	seen = append(seen, f.seriesKeys()...)

	for _, s := range seen {
		for _, bad := range forbidden {
			assert.NotContains(t, s, bad)
		}
	}
	assert.Contains(t, f.seriesKeys(), "keysmith_key_validations_total{result=ok,via_previous_key=true}", "the test did reach the metrics")
	assert.Contains(t, spanNames(spans), "expire keys", "the test did reach the failing sweep")
}
