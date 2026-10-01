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

package engine

import (
	"context"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"github.com/stretchr/testify/require"
	goakt "github.com/tochemey/goakt/v4/actor"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	nooptrace "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/getsyntegrity/ego/encryption"
	"github.com/getsyntegrity/ego/eventadapter"
	"github.com/getsyntegrity/ego/internal/extensions"
	"github.com/getsyntegrity/ego/projection"
	"github.com/getsyntegrity/ego/tenancy"
	"github.com/getsyntegrity/ego/testkit"
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
//
// TENANT-003 T4 (corrected): Engine.Entity/DurableStateEntity/Saga no
// longer call Resolve at spawn at all (Resolve-Once, Propagate-After
// reserves Resolve for the command trust boundary alone) — a spawn under
// this resolver instead declares its tenant explicitly via engine.WithTenant,
// so this resolver can stay a simple always-fail stub with no escape hatch.
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

// zeroValueTenantResolver is a tenancy.TenantResolver stub that returns the
// zero-value tenancy.TenantContext{} with a nil error — the exact malformed
// value an external TenantResolver implementation can produce, since every
// TenantContext field is unexported and a bare struct literal is the only
// construction path available outside the tenancy package. Used to prove
// Engine.SendCommand's trust boundary rejects it (design.md Decision D8)
// before dispatch, the domain handler, or persistence, rather than treating
// "no error" as "a valid identity was resolved".
//
// TENANT-003 T4 (corrected): as with erroringTenantResolver above, spawn no
// longer calls Resolve, so a spawn under this resolver declares its tenant
// via engine.WithTenant and this stub needs no escape hatch either.
type zeroValueTenantResolver struct {
	calls atomic.Int64
}

var _ tenancy.TenantResolver = (*zeroValueTenantResolver)(nil)

func (r *zeroValueTenantResolver) Resolve(context.Context) (tenancy.TenantContext, error) {
	r.calls.Add(1)
	return tenancy.TenantContext{}, nil
}

func (r *zeroValueTenantResolver) callCount() int64 {
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
	specs.Describe(t, "WithLogger stores the logger on the config", func(s *specs.Spec) {
		s.It("keeps the given logger", func(ctx *specs.Context) {
			c := NewConfig(nil, WithLogger(DiscardLogger))
			ctx.Expect(c.logger).To(specs.Equal(DiscardLogger))
		})
	})
}

func TestOptionWithStateStore(t *testing.T) {
	specs.Describe(t, "WithStateStore stores the state store on the config", func(s *specs.Spec) {
		s.It("keeps the given store", func(ctx *specs.Context) {
			store := testkit.NewDurableStore()
			c := NewConfig(nil, WithStateStore(store))
			ctx.Expect(c.stateStore).To(specs.Equal(store))
		})
	})
}

func TestOptionWithOffsetStore(t *testing.T) {
	specs.Describe(t, "WithOffsetStore stores the offset store on the config", func(s *specs.Spec) {
		s.It("keeps the given store", func(ctx *specs.Context) {
			store := testkit.NewOffsetStore()
			c := NewConfig(nil, WithOffsetStore(store))
			ctx.Expect(c.offsetStore).To(specs.Equal(store))
		})
	})
}

func TestOptionWithSnapshotStore(t *testing.T) {
	specs.Describe(t, "WithSnapshotStore stores the snapshot store on the config", func(s *specs.Spec) {
		s.It("keeps the given store", func(ctx *specs.Context) {
			store := testkit.NewSnapshotStore()
			c := NewConfig(nil, WithSnapshotStore(store))
			ctx.Expect(c.snapshotStore).To(specs.Equal(store))
		})
	})
}

func TestOptionWithEncryptor(t *testing.T) {
	specs.Describe(t, "WithEncryptor stores the encryptor on the config", func(s *specs.Spec) {
		s.It("keeps the given encryptor", func(ctx *specs.Context) {
			enc := encryption.NewAESEncryptor(testkit.NewKeyStore())
			c := NewConfig(nil, WithEncryptor(enc))
			ctx.Expect(c.encryptor).To(specs.Equal(enc))
		})
	})
}

func TestOptionWithTenantResolverNil(t *testing.T) {
	specs.Describe(t, "WithTenantResolver(nil) is inert", func(s *specs.Spec) {
		s.It("registers nothing and does not activate tenant-aware mode", func(ctx *specs.Context) {
			// A nil resolver must be inert: no registration, no error, tenant-aware
			// mode not activated (spec.md "Nil option is inert").
			c := NewConfig(nil, WithTenantResolver(nil))
			ctx.Expect(c.tenantResolver).To(specs.BeNil())
			ctx.Expect(c.tenantResolverCount).To(specs.BeZero())
		})
	})
}

func TestOptionWithTenantResolverTypedNil(t *testing.T) {
	specs.Describe(t, "WithTenantResolver ignores a typed-nil pointer resolver", func(s *specs.Spec) {
		s.It("treats it exactly like a plain nil", func(ctx *specs.Context) {
			// A typed-nil resolver value is non-nil at the interface level but
			// wraps a nil pointer; isNilResolver must detect it the way
			// isNilLogger does, so it is treated exactly like a plain nil.
			var typedNil *stubTenantResolver
			c := NewConfig(nil, WithTenantResolver(typedNil))
			ctx.Expect(c.tenantResolver).To(specs.BeNil())
			ctx.Expect(c.tenantResolverCount).To(specs.BeZero())
		})
	})
}

func TestOptionWithTenantResolverFuncTypedNil(t *testing.T) {
	specs.Describe(t, "WithTenantResolver ignores a typed-nil func resolver", func(s *specs.Spec) {
		s.It("detects it through the reflect.Func branch", func(ctx *specs.Context) {
			// A typed-nil value of a named function type implementing
			// tenancy.TenantResolver is non-nil at the interface level but wraps a
			// nil func; isNilResolver must detect it via the reflect.Func branch,
			// not just reflect.Pointer, or it would be registered as an "effective"
			// resolver and panic on the first Resolve call.
			var typedNil funcTenantResolver
			c := NewConfig(nil, WithTenantResolver(typedNil))
			ctx.Expect(c.tenantResolver).To(specs.BeNil())
			ctx.Expect(c.tenantResolverCount).To(specs.BeZero())
		})
	})
}

func TestOptionWithTenantResolver(t *testing.T) {
	specs.Describe(t, "WithTenantResolver registers a non-nil resolver", func(s *specs.Spec) {
		s.It("makes it the effective resolver and activates tenant-aware mode", func(ctx *specs.Context) {
			// A single non-nil registration becomes the effective resolver and
			// activates tenant-aware mode (spec.md "Non-nil resolver registers as
			// effective").
			resolver := &stubTenantResolver{id: "acme"}
			c := NewConfig(nil, WithTenantResolver(resolver))
			ctx.Expect(c.tenantResolver).To(beTheSamePointer(resolver))
			ctx.Expect(c.tenantResolverCount).To(specs.Equal(1))
		})
	})
}

func TestOptionWithTenantResolverNilAfterNonNil(t *testing.T) {
	specs.Describe(t, "a nil registration after a valid one", func(s *specs.Spec) {
		s.It("does not reset the effective resolver", func(ctx *specs.Context) {
			// nil after a valid registration must not reset it: nil is not a
			// mechanism to disable tenancy once configured (spec.md "Nil after
			// non-nil does not disable tenancy").
			resolver := &stubTenantResolver{id: "acme"}
			c := NewConfig(nil, WithTenantResolver(resolver), WithTenantResolver(nil))
			ctx.Expect(c.tenantResolver).To(beTheSamePointer(resolver))
			ctx.Expect(c.tenantResolverCount).To(specs.Equal(1))
		})
	})
}

func TestOptionWithTenantResolverCountsOnlyNonNilRegistrations(t *testing.T) {
	specs.Describe(t, "the registration count", func(s *specs.Spec) {
		s.It("counts a single non-nil registration surrounded by nils as one", func(ctx *specs.Context) {
			// A single non-nil registration surrounded by nil registrations still
			// counts as exactly one (spec.md "Nil registrations do not count").
			resolver := &stubTenantResolver{id: "acme"}
			c := NewConfig(nil,
				WithTenantResolver(nil),
				WithTenantResolver(resolver),
				WithTenantResolver(nil),
			)
			ctx.Expect(c.tenantResolver).To(beTheSamePointer(resolver))
			ctx.Expect(c.tenantResolverCount).To(specs.Equal(1))
		})
	})
}

func TestOptionWithTenantResolverTypedNilThenValid(t *testing.T) {
	specs.Describe(t, "a typed-nil registration before a valid one", func(s *specs.Spec) {
		s.It("leaves exactly one effective registration", func(ctx *specs.Context) {
			// The typed-nil is inert and must not be mistaken for an
			// already-registered resolver.
			var typedNil *stubTenantResolver
			resolver := &stubTenantResolver{id: "acme"}
			c := NewConfig(nil, WithTenantResolver(typedNil), WithTenantResolver(resolver))
			ctx.Expect(c.tenantResolver).To(beTheSamePointer(resolver))
			ctx.Expect(c.tenantResolverCount).To(specs.Equal(1))
		})
	})
}

func TestOptionWithTenantResolverValidThenTypedNil(t *testing.T) {
	specs.Describe(t, "a typed-nil registration after a valid one", func(s *specs.Spec) {
		s.It("does not count and does not disturb the effective resolver", func(ctx *specs.Context) {
			// The reverse order must produce the same result as the one above.
			var typedNil *stubTenantResolver
			resolver := &stubTenantResolver{id: "acme"}
			c := NewConfig(nil, WithTenantResolver(resolver), WithTenantResolver(typedNil))
			ctx.Expect(c.tenantResolver).To(beTheSamePointer(resolver))
			ctx.Expect(c.tenantResolverCount).To(specs.Equal(1))
		})
	})
}

func TestOptionWithTenantResolverAmbiguousCount(t *testing.T) {
	specs.Describe(t, "two distinct non-nil registrations", func(s *specs.Spec) {
		s.It("are both counted, and NewEngine rejects the ambiguity", func(ctx *specs.Context) {
			// The Option itself cannot return an error; NewEngine rejects
			// tenantResolverCount > 1.
			c := NewConfig(nil,
				WithTenantResolver(&stubTenantResolver{id: "acme"}),
				WithTenantResolver(&stubTenantResolver{id: "globex"}),
			)
			ctx.Expect(c.tenantResolverCount).To(specs.Equal(2))
		})
	})
}

func TestOptionWithProjection(t *testing.T) {
	specs.Describe(t, "WithProjection registers the projection options by name", func(s *specs.Spec) {
		s.It("keeps the given options under the projection name", func(ctx *specs.Context) {
			handler := projection.NewDiscardHandler()
			recovery := projection.NewRecovery(projection.WithRetries(10))
			o := &projection.Options{
				Handler:      handler,
				BufferSize:   500,
				PullInterval: time.Second,
				Recovery:     recovery,
			}
			c := NewConfig(nil, WithProjection("accounts", o))
			ctx.Expect(c.projections).To(specs.HaveLen(1))
			ctx.Expect(c.projections["accounts"]).To(beTheSamePointer(o))
		})
	})
}

func TestOptionWithProjectionMultiple(t *testing.T) {
	specs.Describe(t, "WithProjection called twice", func(s *specs.Spec) {
		s.It("registers both projections", func(ctx *specs.Context) {
			accounts := &projection.Options{Handler: projection.NewDiscardHandler()}
			audit := &projection.Options{Handler: projection.NewDiscardHandler()}
			c := NewConfig(nil,
				WithProjection("accounts", accounts),
				WithProjection("audit", audit),
			)
			ctx.Expect(c.projections).To(specs.HaveLen(2))
			ctx.Expect(c.projections["accounts"]).To(beTheSamePointer(accounts))
			ctx.Expect(c.projections["audit"]).To(beTheSamePointer(audit))
		})
	})
}

func TestOptionWithProjectionNil(t *testing.T) {
	specs.Describe(t, "WithProjection with nil options", func(s *specs.Spec) {
		s.It("registers nothing", func(ctx *specs.Context) {
			c := NewConfig(nil, WithProjection("accounts", nil))
			ctx.Expect(c.projections).To(specs.BeNil())
		})
	})
}

func TestOptionWithTelemetry(t *testing.T) {
	specs.Describe(t, "WithTelemetry stores the telemetry on the config", func(s *specs.Spec) {
		s.It("keeps the given tracer", func(ctx *specs.Context) {
			tracer := nooptrace.NewTracerProvider().Tracer("test")
			tel := &Telemetry{Tracer: tracer}
			c := NewConfig(nil, WithTelemetry(tel))
			ctx.Expect(c.telemetry).To(specs.Not(specs.BeNil()))
			ctx.Expect(c.telemetry.Tracer).To(specs.Equal(tracer))
		})
	})
}

func TestOptionWithTelemetryNil(t *testing.T) {
	specs.Describe(t, "WithTelemetry(nil)", func(s *specs.Spec) {
		s.It("leaves the config without telemetry", func(ctx *specs.Context) {
			c := NewConfig(nil, WithTelemetry(nil))
			ctx.Expect(c.telemetry).To(specs.BeNil())
		})
	})
}

func TestOptionWithEventAdapters(t *testing.T) {
	specs.Describe(t, "WithEventAdapters appends adapters to the config", func(s *specs.Spec) {
		s.It("keeps the given adapter", func(ctx *specs.Context) {
			adapter := &testEventAdapter{}
			c := NewConfig(nil, WithEventAdapters(adapter))
			ctx.Expect(c.eventAdapters).To(specs.HaveLen(1))
			ctx.Expect(c.eventAdapters[0]).To(specs.Equal(adapter))
		})
	})
}

func TestOptionWithEventAdaptersMultiple(t *testing.T) {
	specs.Describe(t, "WithEventAdapters called twice", func(s *specs.Spec) {
		s.It("keeps both adapters", func(ctx *specs.Context) {
			c := NewConfig(nil,
				WithEventAdapters(&testEventAdapter{}),
				WithEventAdapters(&testEventAdapter{}),
			)
			ctx.Expect(c.eventAdapters).To(specs.HaveLen(2))
		})
	})
}

func TestOptionWithLoggerNilFallback(t *testing.T) {
	specs.Describe(t, "WithLogger(nil)", func(s *specs.Spec) {
		s.It("lands on the default logger", func(ctx *specs.Context) {
			// Passing a nil Logger via WithLogger should round-trip through
			// NewConfig and land on the default logger (ResolveLogger fallback).
			c := NewConfig(nil, WithLogger(nil))
			ctx.Expect(c.logger).To(specs.Not(specs.BeNil()))
			ctx.Expect(c.logger).To(beTheSamePointer(DefaultLogger()))
		})
	})
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

// TestClusterKindsExposesEgoActors pins the GoAkt kind names of the cluster
// actors. GoAkt names a kind lower(reflect.Type.String()) and ships that name
// in spawn, relocation and singleton records, so a node running another
// version only understands these exact names: the types must stay declared in
// package engine.
func TestClusterKindsExposesEgoActors(t *testing.T) {
	specs.Describe(t, "ClusterKinds lists the GoAkt kind names of the ego actors", func(s *specs.Spec) {
		s.It("keeps the exact names other nodes understand, in order", func(ctx *specs.Context) {
			var got []string
			for _, kind := range ClusterKinds() {
				got = append(got, strings.ToLower(reflect.TypeOf(kind).Elem().String()))
			}

			ctx.Expect(got).To(specs.HaveElementsInOrder(
				specs.Equal("engine.eventsourcedactor"),
				specs.Equal("engine.durablestateactor"),
				specs.Equal("engine.sagaactor"),
				specs.Equal("engine.projectionactor"),
			))
		})
	})
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
