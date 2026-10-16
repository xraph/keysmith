package keysmith

import (
	"context"
	"errors"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/xraph/forge"
	gometrics "github.com/xraph/go-utils/metrics"

	"github.com/xraph/keysmith/rotation"
)

// InstrumentationName is the tracer name on every span keysmith starts.
const InstrumentationName = "github.com/xraph/keysmith"

// Span names. Every span keysmith starts is SpanKindInternal, the kind a
// span gets when none is given.
const (
	spanValidateKey    = "validate key"
	spanCheckRateLimit = "check rate limit"
	spanCreateKey      = "create key"
	spanRotateKey      = "rotate key"
	spanRevokeKey      = "revoke key"
	spanSuspendKey     = "suspend key"
	spanReactivateKey  = "reactivate key"
	spanExpireKeys     = "expire keys"
)

// Span attributes. None of them carries a key, a hash, a hint, an ID, a
// tenant or any text a caller supplied.
const (
	attrResult             = attribute.Key("keysmith.result")
	attrViaPreviousKey     = attribute.Key("keysmith.validation.via_previous_key")
	attrRotationReason     = attribute.Key("keysmith.rotation.reason")
	attrGraceSeconds       = attribute.Key("keysmith.rotation.grace_seconds")
	attrGraceWindowsClosed = attribute.Key("keysmith.rotation.grace_windows_closed")
	attrKeysExpired        = attribute.Key("keysmith.keys.expired")
	attrKeysFailed         = attribute.Key("keysmith.keys.failed")
	attrHookEvent          = attribute.Key("keysmith.hook.event")
)

// eventHookFailed is the span event a failed lifecycle hook leaves on the
// span of the operation that fired it.
const eventHookFailed = "hook failed"

// Metric names as keysmith records them. Forge's Prometheus export puts its
// namespace in front, so keysmith_key_validations_total scrapes as
// forge_keysmith_key_validations_total.
const (
	metricKeyValidations      = "keysmith_key_validations_total"
	metricValidationDuration  = "keysmith_key_validation_duration_seconds"
	metricKeyOperations       = "keysmith_key_operations_total"
	metricRateLimitChecks     = "keysmith_rate_limit_checks_total"
	metricHookErrors          = "keysmith_hook_errors_total"
	metricLastUsedWriteFailed = "keysmith_last_used_updates_failed_total"
)

// validationDurationBuckets cover a hash, one to three indexed reads and a
// rate limiter call: well under a millisecond in memory, a few milliseconds
// against a nearby database, and a long tail for a slow limiter.
var validationDurationBuckets = []float64{
	0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5,
}

// Values of the result label on keysmith_key_validations_total and
// keysmith_key_validation_duration_seconds, and of keysmith.result on the
// validate key span.
const (
	validationOK          = "ok"
	validationInvalid     = "invalid"
	validationInactive    = "inactive"
	validationExpired     = "expired"
	validationRateLimited = "rate_limited"
	validationCancelled   = "cancelled"
	validationError       = "error"
)

// Values of the operation label on keysmith_key_operations_total.
const (
	opCreate     = "create"
	opRotate     = "rotate"
	opRevoke     = "revoke"
	opSuspend    = "suspend"
	opReactivate = "reactivate"
	opExpire     = "expire"
)

// Values of the result label on keysmith_key_operations_total, and of
// keysmith.result on the lifecycle spans.
const (
	resultOK        = "ok"
	resultNotFound  = "not_found"
	resultRejected  = "rejected"
	resultConflict  = "conflict"
	resultCancelled = "cancelled"
	resultError     = "error"
)

// Values of the result label on keysmith_rate_limit_checks_total, and of
// keysmith.result on the check rate limit span.
const (
	rateLimitAllowed   = "allowed"
	rateLimitDenied    = "denied"
	rateLimitCancelled = "cancelled"
	rateLimitError     = "error"
)

// Values of the event label on keysmith_hook_errors_total, and of
// keysmith.hook.event on the hook failed span event.
const (
	hookKeyCreated          = "key_created"
	hookKeyCreateFailed     = "key_create_failed"
	hookKeyValidated        = "key_validated"
	hookKeyValidationFailed = "key_validation_failed"
	hookKeyRotated          = "key_rotated"
	hookKeyRevoked          = "key_revoked"
	hookKeySuspended        = "key_suspended"
	hookKeyReactivated      = "key_reactivated"
	hookKeyExpired          = "key_expired"
	hookKeyRateLimited      = "key_rate_limited"
	hookPolicyCreated       = "policy_created"
	hookPolicyUpdated       = "policy_updated"
	hookPolicyDeleted       = "policy_deleted"
)

// startSpan starts an internal span on the global tracer provider, which
// forge.New installs. The tracer is looked up on every call, as forge and
// grove do, so a provider installed after the engine was built still gets
// the spans.
func startSpan(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return otel.Tracer(InstrumentationName).Start(ctx, name, opts...)
}

// markError puts a failure on span: error.type, an exception event that
// carries only the error's type, and an error status described by that
// type. The error's text never goes on the span, because a store's message
// can quote the values it was given.
func markError(span trace.Span, err error) {
	if !span.IsRecording() {
		return
	}
	typ := errorType(err)
	span.SetAttributes(semconv.ErrorTypeKey.String(typ))
	span.AddEvent(semconv.ExceptionEventName, trace.WithAttributes(semconv.ExceptionType(typ)))
	span.SetStatus(codes.Error, typ)
}

// errorType names an error by class: a context error by its package name,
// anything else by the Go type of the innermost error in its chain.
func errorType(err error) string {
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		return "context.DeadlineExceeded"
	case errors.Is(err, context.Canceled):
		return "context.Canceled"
	}
	for next := errors.Unwrap(err); next != nil; next = errors.Unwrap(err) {
		err = next
	}
	return fmt.Sprintf("%T", err)
}

// cancelled reports whether err comes from a caller that cancelled its
// context, most often a client that went away mid-request. It is counted
// apart from "error" so disconnects don't inflate the failure ratio. A
// deadline is not a cancellation: a timeout usually means something keysmith
// waited on was slow, so it stays an error.
func cancelled(err error) bool { return errors.Is(err, context.Canceled) }

// operationResult sorts the error a lifecycle operation returned into the
// bounded result label. A missing entity, a refused transition, a lost race
// and a cancelled caller are answers, not failures; only "error" marks the
// span.
func operationResult(err error) string {
	switch {
	case err == nil:
		return resultOK
	case cancelled(err):
		return resultCancelled
	case errors.Is(err, ErrKeyConflict):
		return resultConflict
	case errors.Is(err, ErrKeyNotFound), errors.Is(err, ErrPolicyNotFound), errors.Is(err, ErrScopeNotFound):
		return resultNotFound
	case errors.Is(err, ErrInvalidStateTransition), errors.Is(err, ErrKeyLifetimeExceeded),
		errors.Is(err, ErrScopeNotAllowed):
		return resultRejected
	default:
		return resultError
	}
}

// rotationReason clamps a caller's rotation reason to the four the rotation
// package defines. The reason arrives unchecked from the API, so anything
// else is "other".
func rotationReason(r rotation.Reason) string {
	switch r {
	case rotation.ReasonScheduled, rotation.ReasonManual, rotation.ReasonCompromise, rotation.ReasonPolicy:
		return string(r)
	default:
		return "other"
	}
}

// operation is one lifecycle call, from its span's start to its end.
type operation struct {
	tel  *telemetry
	name string
	span trace.Span
}

// startOperation starts the span for a lifecycle operation. End it with end.
func (e *Engine) startOperation(ctx context.Context, op, spanName string, attrs ...attribute.KeyValue) (context.Context, operation) {
	ctx, span := startSpan(ctx, spanName, trace.WithAttributes(attrs...))
	return ctx, operation{tel: e.tel, name: op, span: span}
}

// end records the operation's result on its span and counter and ends the span.
func (o operation) end(err error) {
	result := operationResult(err)
	o.span.SetAttributes(attrResult.String(result))
	if result == resultError {
		markError(o.span, err)
	}
	o.span.End()
	o.tel.operation(o.name, result)
}

// hookDone counts a lifecycle hook that failed and notes it on the span in
// ctx. Hook errors never fail the operation that fired them, so this is the
// only place they show.
func (e *Engine) hookDone(ctx context.Context, event string, err error) {
	if err == nil {
		return
	}
	e.tel.hookFailed(event)
	if span := trace.SpanFromContext(ctx); span.IsRecording() {
		span.AddEvent(eventHookFailed, trace.WithAttributes(
			attrHookEvent.String(event),
			semconv.ExceptionType(errorType(err)),
		))
	}
}

// telemetry holds the engine's metric instruments. They are declared once,
// when the engine is built. A nil *telemetry records nothing, which is what
// an engine built without WithMetrics gets.
type telemetry struct {
	validations        *forge.Vec[forge.Counter]
	validationDuration *forge.Vec[forge.Histogram]
	operations         *forge.Vec[forge.Counter]
	rateLimitChecks    *forge.Vec[forge.Counter]
	hookErrors         *forge.Vec[forge.Counter]
	lastUsedFailed     forge.Counter
}

func newTelemetry(m gometrics.Metrics) *telemetry {
	if m == nil {
		return nil
	}
	return &telemetry{
		validations: forge.CounterVec(m, metricKeyValidations, "result", "via_previous_key"),
		validationDuration: forge.HistogramVec(m, metricValidationDuration, forge.HistogramOpts{
			Buckets: validationDurationBuckets,
			Help:    "Time keysmith took to validate an API key, by result.",
			Unit:    "s",
		}, "result"),
		operations:      forge.CounterVec(m, metricKeyOperations, "operation", "result"),
		rateLimitChecks: forge.CounterVec(m, metricRateLimitChecks, "result"),
		hookErrors:      forge.CounterVec(m, metricHookErrors, "event"),
		lastUsedFailed:  m.Counter(metricLastUsedWriteFailed),
	}
}

func (t *telemetry) validated(result string, viaPrevious bool, took time.Duration) {
	if t == nil {
		return
	}
	via := "false"
	if viaPrevious {
		via = "true"
	}
	t.validations.With(result, via).Inc()
	t.validationDuration.With(result).Observe(took.Seconds())
}

func (t *telemetry) operation(op, result string) {
	if t == nil {
		return
	}
	t.operations.With(op, result).Inc()
}

func (t *telemetry) rateLimitChecked(result string) {
	if t == nil {
		return
	}
	t.rateLimitChecks.With(result).Inc()
}

func (t *telemetry) hookFailed(event string) {
	if t == nil {
		return
	}
	t.hookErrors.With(event).Inc()
}

func (t *telemetry) lastUsedUpdateFailed() {
	if t == nil {
		return
	}
	t.lastUsedFailed.Inc()
}
