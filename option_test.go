// MIT License
//
// Copyright (c) 2022-2026 Arsene Tochemey Gandote
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package ego

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/pablogore/ego/v4/encryption"
	"github.com/pablogore/ego/v4/eventadapter"
	"github.com/pablogore/ego/v4/internal/extensions"
	"github.com/pablogore/ego/v4/projection"
	"github.com/pablogore/ego/v4/tenancy"
	"github.com/pablogore/ego/v4/testkit"
)

// stubTenantResolver is a minimal tenancy.TenantResolver used across
// option_test.go and engine_test.go to exercise WithTenantResolver and
// NewEngine's tenant-resolver validation without pulling in a mocking
// framework for what is, here, just an identity comparison.
type stubTenantResolver struct {
	id string
}

var _ tenancy.TenantResolver = (*stubTenantResolver)(nil)

func (r *stubTenantResolver) Resolve(context.Context) (tenancy.TenantContext, error) {
	tid, err := tenancy.NewTenantID(r.id)
	if err != nil {
		return tenancy.TenantContext{}, err
	}
	return tenancy.NewTenantContext(tid)
}

// funcTenantResolver is a named function type implementing
// tenancy.TenantResolver, used to exercise the reflect.Func branch of
// isNilResolver: a nil value of this type is a typed-nil that must be
// treated exactly like a nil interface, not a callable resolver.
type funcTenantResolver func(context.Context) (tenancy.TenantContext, error)

var _ tenancy.TenantResolver = funcTenantResolver(nil)

func (f funcTenantResolver) Resolve(ctx context.Context) (tenancy.TenantContext, error) {
	return f(ctx)
}

// countingTenantResolver is an instrumented tenancy.TenantResolver used to
// prove exactly how many times Resolve was invoked, rather than inferring
// the count from downstream behavior. Safe for concurrent use.
type countingTenantResolver struct {
	id    string
	calls atomic.Int64
}

var _ tenancy.TenantResolver = (*countingTenantResolver)(nil)

func (r *countingTenantResolver) Resolve(context.Context) (tenancy.TenantContext, error) {
	r.calls.Add(1)
	tid, err := tenancy.NewTenantID(r.id)
	if err != nil {
		return tenancy.TenantContext{}, err
	}
	return tenancy.NewTenantContext(tid)
}

func (r *countingTenantResolver) callCount() int64 {
	return r.calls.Load()
}

// erroringTenantResolver is a tenancy.TenantResolver stub that always fails
// with a fixed error. Used to prove a resolver failure blocks a command
// before it ever reaches the actor system. It also counts its own
// invocations so tests can assert Resolve was tried exactly once.
type erroringTenantResolver struct {
	err   error
	calls atomic.Int64
}

var _ tenancy.TenantResolver = (*erroringTenantResolver)(nil)

func (r *erroringTenantResolver) Resolve(context.Context) (tenancy.TenantContext, error) {
	r.calls.Add(1)
	return tenancy.TenantContext{}, r.err
}

func (r *erroringTenantResolver) callCount() int64 {
	return r.calls.Load()
}

// perCallerTenantKey is the context key perCallerTenantResolver reads from.
type perCallerTenantKey struct{}

// perCallerTenantResolver resolves whichever tenant ID the caller placed on
// ctx under perCallerTenantKey, simulating a resolver that derives identity
// from request-scoped data (e.g. a header) rather than a fixed value. Used
// to prove concurrent commands for different tenants never cross-contaminate
// the TenantContext each command's handler observes.
type perCallerTenantResolver struct{}

var _ tenancy.TenantResolver = perCallerTenantResolver{}

func (perCallerTenantResolver) Resolve(ctx context.Context) (tenancy.TenantContext, error) {
	id, _ := ctx.Value(perCallerTenantKey{}).(string)
	tid, err := tenancy.NewTenantID(id)
	if err != nil {
		return tenancy.TenantContext{}, err
	}
	return tenancy.NewTenantContext(tid)
}

// buildActorSystem constructs and starts a goakt actor system from a Config so
// the optional extension branches in Config.GoaktOptions can be inspected.
func buildActorSystem(t *testing.T, cfg *Config) goakt.ActorSystem {
	t.Helper()
	ctx := context.Background()
	sys, err := goakt.NewActorSystem("OptionTest", cfg.GoaktOptions()...)
	require.NoError(t, err)
	require.NoError(t, sys.Start(ctx))
	t.Cleanup(func() { _ = sys.Stop(ctx) })
	return sys
}

func TestOptionWithLogger(t *testing.T) {
	c := NewConfig(nil, WithLogger(DiscardLogger))
	assert.Equal(t, DiscardLogger, c.logger)
}

func TestOptionWithStateStore(t *testing.T) {
	store := testkit.NewDurableStore()
	c := NewConfig(nil, WithStateStore(store))
	assert.Equal(t, store, c.stateStore)
}

func TestOptionWithOffsetStore(t *testing.T) {
	store := testkit.NewOffsetStore()
	c := NewConfig(nil, WithOffsetStore(store))
	assert.Equal(t, store, c.offsetStore)
}

func TestOptionWithSnapshotStore(t *testing.T) {
	store := testkit.NewSnapshotStore()
	c := NewConfig(nil, WithSnapshotStore(store))
	assert.Equal(t, store, c.snapshotStore)
}

func TestOptionWithEncryptor(t *testing.T) {
	enc := encryption.NewAESEncryptor(testkit.NewKeyStore())
	c := NewConfig(nil, WithEncryptor(enc))
	assert.Equal(t, enc, c.encryptor)
}

func TestOptionWithTenantResolverNil(t *testing.T) {
	// A nil resolver must be inert: no registration, no error, tenant-aware
	// mode not activated (spec.md "Nil option is inert").
	c := NewConfig(nil, WithTenantResolver(nil))
	assert.Nil(t, c.tenantResolver)
	assert.Zero(t, c.tenantResolverCount)
}

func TestOptionWithTenantResolverTypedNil(t *testing.T) {
	// A typed-nil resolver value is non-nil at the interface level but
	// wraps a nil pointer; isNilResolver must detect it the way
	// isNilLogger does, so it is treated exactly like a plain nil.
	var typedNil *stubTenantResolver
	c := NewConfig(nil, WithTenantResolver(typedNil))
	assert.Nil(t, c.tenantResolver)
	assert.Zero(t, c.tenantResolverCount)
}

func TestOptionWithTenantResolverFuncTypedNil(t *testing.T) {
	// A typed-nil value of a named function type implementing
	// tenancy.TenantResolver is non-nil at the interface level but wraps a
	// nil func; isNilResolver must detect it via the reflect.Func branch,
	// not just reflect.Pointer, or it would be registered as an "effective"
	// resolver and panic on the first Resolve call.
	var typedNil funcTenantResolver
	c := NewConfig(nil, WithTenantResolver(typedNil))
	assert.Nil(t, c.tenantResolver)
	assert.Zero(t, c.tenantResolverCount)
}

func TestOptionWithTenantResolver(t *testing.T) {
	// A single non-nil registration becomes the effective resolver and
	// activates tenant-aware mode (spec.md "Non-nil resolver registers as
	// effective").
	resolver := &stubTenantResolver{id: "acme"}
	c := NewConfig(nil, WithTenantResolver(resolver))
	assert.Same(t, resolver, c.tenantResolver)
	assert.Equal(t, 1, c.tenantResolverCount)
}

func TestOptionWithTenantResolverNilAfterNonNil(t *testing.T) {
	// nil after a valid registration must not reset it — nil is not a
	// mechanism to disable tenancy once configured (spec.md "Nil after
	// non-nil does not disable tenancy").
	resolver := &stubTenantResolver{id: "acme"}
	c := NewConfig(nil, WithTenantResolver(resolver), WithTenantResolver(nil))
	assert.Same(t, resolver, c.tenantResolver)
	assert.Equal(t, 1, c.tenantResolverCount)
}

func TestOptionWithTenantResolverCountsOnlyNonNilRegistrations(t *testing.T) {
	// A single non-nil registration surrounded by nil registrations still
	// counts as exactly one (spec.md "Nil registrations do not count").
	resolver := &stubTenantResolver{id: "acme"}
	c := NewConfig(nil,
		WithTenantResolver(nil),
		WithTenantResolver(resolver),
		WithTenantResolver(nil),
	)
	assert.Same(t, resolver, c.tenantResolver)
	assert.Equal(t, 1, c.tenantResolverCount)
}

func TestOptionWithTenantResolverTypedNilThenValid(t *testing.T) {
	// A typed-nil registration followed by a valid non-nil registration
	// must produce exactly one effective registration: the typed-nil is
	// inert and must not be mistaken for an already-registered resolver.
	var typedNil *stubTenantResolver
	resolver := &stubTenantResolver{id: "acme"}
	c := NewConfig(nil, WithTenantResolver(typedNil), WithTenantResolver(resolver))
	assert.Same(t, resolver, c.tenantResolver)
	assert.Equal(t, 1, c.tenantResolverCount)
}

func TestOptionWithTenantResolverValidThenTypedNil(t *testing.T) {
	// The reverse order must produce the same result: a typed-nil
	// registration after a valid one must not count and must not disturb
	// the already-registered effective resolver.
	var typedNil *stubTenantResolver
	resolver := &stubTenantResolver{id: "acme"}
	c := NewConfig(nil, WithTenantResolver(resolver), WithTenantResolver(typedNil))
	assert.Same(t, resolver, c.tenantResolver)
	assert.Equal(t, 1, c.tenantResolverCount)
}

func TestOptionWithTenantResolverAmbiguousCount(t *testing.T) {
	// Two distinct non-nil registrations are both counted; NewEngine (not
	// the Option itself, which cannot return an error) rejects
	// tenantResolverCount > 1.
	c := NewConfig(nil,
		WithTenantResolver(&stubTenantResolver{id: "acme"}),
		WithTenantResolver(&stubTenantResolver{id: "globex"}),
	)
	assert.Equal(t, 2, c.tenantResolverCount)
}

func TestOptionWithProjection(t *testing.T) {
	handler := projection.NewDiscardHandler()
	recovery := projection.NewRecovery(projection.WithRetries(10))
	o := &projection.Options{
		Handler:      handler,
		BufferSize:   500,
		PullInterval: time.Second,
		Recovery:     recovery,
	}
	c := NewConfig(nil, WithProjection("accounts", o))
	require.NotNil(t, c.projections)
	assert.Same(t, o, c.projections["accounts"])
}

func TestOptionWithProjectionMultiple(t *testing.T) {
	accounts := &projection.Options{Handler: projection.NewDiscardHandler()}
	audit := &projection.Options{Handler: projection.NewDiscardHandler()}
	c := NewConfig(nil,
		WithProjection("accounts", accounts),
		WithProjection("audit", audit),
	)
	require.Len(t, c.projections, 2)
	assert.Same(t, accounts, c.projections["accounts"])
	assert.Same(t, audit, c.projections["audit"])
}

func TestOptionWithProjectionNil(t *testing.T) {
	c := NewConfig(nil, WithProjection("accounts", nil))
	assert.Nil(t, c.projections)
}

func TestOptionWithTelemetry(t *testing.T) {
	tracer := nooptrace.NewTracerProvider().Tracer("test")
	tel := &Telemetry{Tracer: tracer}
	c := NewConfig(nil, WithTelemetry(tel))
	require.NotNil(t, c.telemetry)
	assert.Equal(t, tracer, c.telemetry.Tracer)
}

func TestOptionWithTelemetryNil(t *testing.T) {
	c := NewConfig(nil, WithTelemetry(nil))
	assert.Nil(t, c.telemetry)
}

func TestOptionWithEventAdapters(t *testing.T) {
	adapter := &testEventAdapter{}
	c := NewConfig(nil, WithEventAdapters(adapter))
	require.Len(t, c.eventAdapters, 1)
	assert.Equal(t, adapter, c.eventAdapters[0])
}

func TestOptionWithEventAdaptersMultiple(t *testing.T) {
	c := NewConfig(nil,
		WithEventAdapters(&testEventAdapter{}),
		WithEventAdapters(&testEventAdapter{}),
	)
	assert.Len(t, c.eventAdapters, 2)
}

func TestOptionWithLoggerNilFallback(t *testing.T) {
	// Passing a nil Logger via WithLogger should round-trip through
	// NewConfig and land on the default logger (ResolveLogger fallback).
	c := NewConfig(nil, WithLogger(nil))
	require.NotNil(t, c.logger)
	assert.Same(t, DefaultLogger(), c.logger)
}

func TestConfigGoaktOptionsEncryptor(t *testing.T) {
	// WithEncryptor must register the Encryptor extension via GoaktOptions.
	enc := encryption.NewAESEncryptor(testkit.NewKeyStore())
	cfg := NewConfig(testkit.NewEventsStore(), WithEncryptor(enc))

	sys := buildActorSystem(t, cfg)
	require.NotNil(t, sys.Extension(extensions.EncryptorExtensionID))
}

func TestConfigGoaktOptionsTenancyMarker(t *testing.T) {
	// WithTenantResolver must register the tenancy marker extension via
	// GoaktOptions when a non-nil resolver is configured.
	cfg := NewConfig(testkit.NewEventsStore(), WithTenantResolver(&stubTenantResolver{id: "acme"}))

	sys := buildActorSystem(t, cfg)
	require.NotNil(t, sys.Extension(extensions.TenancyExtensionID))
}

func TestConfigGoaktOptionsNoTenancyMarkerWithoutResolver(t *testing.T) {
	// Backward compatibility (T3/D7): an engine that never registers a
	// resolver must not carry the tenancy marker extension at all.
	cfg := NewConfig(testkit.NewEventsStore())

	sys := buildActorSystem(t, cfg)
	require.Nil(t, sys.Extension(extensions.TenancyExtensionID))
}

func TestConfigGoaktOptionsNoTenancyMarkerWithTypedNilResolver(t *testing.T) {
	// A typed-nil resolver (pointer or func) must not activate the tenancy
	// marker extension: it is inert, not an effective registration.
	var typedNilPointer *stubTenantResolver
	var typedNilFunc funcTenantResolver
	cfg := NewConfig(testkit.NewEventsStore(),
		WithTenantResolver(typedNilPointer),
		WithTenantResolver(typedNilFunc),
	)

	sys := buildActorSystem(t, cfg)
	require.Nil(t, sys.Extension(extensions.TenancyExtensionID))
}

func TestConfigGoaktOptionsTelemetry(t *testing.T) {
	// WithTelemetry must register the Telemetry extension via GoaktOptions.
	tel := &Telemetry{
		Tracer: nooptrace.NewTracerProvider().Tracer("test"),
		Meter:  noopmetric.NewMeterProvider().Meter("test"),
	}
	cfg := NewConfig(testkit.NewEventsStore(), WithTelemetry(tel))

	sys := buildActorSystem(t, cfg)
	require.NotNil(t, sys.Extension(extensions.TelemetryExtensionID))
}

func TestConfigGoaktOptionsProjectionDefaultsRecovery(t *testing.T) {
	// When a projection is configured without a Recovery, GoaktOptions
	// should still register the extension by falling back to a default
	// recovery strategy.
	cfg := NewConfig(testkit.NewEventsStore(),
		WithProjection("accounts", &projection.Options{
			Handler:      projection.NewDiscardHandler(),
			BufferSize:   10,
			PullInterval: time.Second,
		}),
	)

	sys := buildActorSystem(t, cfg)
	require.NotNil(t, sys.Extension(extensions.ProjectionExtensionID))
}

func TestClusterKindsExposesEgoActors(t *testing.T) {
	kinds := ClusterKinds()
	require.Len(t, kinds, 4)
}

// testEventAdapter is a no-op EventAdapter for testing.
type testEventAdapter struct{}

var _ eventadapter.EventAdapter = (*testEventAdapter)(nil)

func (a *testEventAdapter) Adapt(event *anypb.Any, _ uint64) (*anypb.Any, error) {
	return event, nil
}

// keep the trace and nooptrace packages referenced even when no test uses
// them directly.
var _ trace.Tracer = nooptrace.NewTracerProvider().Tracer("compile-check")
