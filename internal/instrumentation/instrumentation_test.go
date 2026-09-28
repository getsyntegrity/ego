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

package instrumentation

import (
	"context"
	"errors"
	"os/exec"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	noopmetric "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/types/known/wrapperspb"
)

// measurement is one recorded value with its attribute set.
type measurement struct {
	value float64
	attrs attribute.Set
}

// fakeMeter records the instruments created on it and every measurement.
type fakeMeter struct {
	noopmetric.Meter

	mu           sync.Mutex
	catalog      map[string]string
	measurements map[string][]measurement
}

func newFakeMeter() *fakeMeter {
	return &fakeMeter{catalog: map[string]string{}, measurements: map[string][]measurement{}}
}

func (m *fakeMeter) record(name string, value float64, set attribute.Set) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.measurements[name] = append(m.measurements[name], measurement{value: value, attrs: set})
}

func (m *fakeMeter) Int64Counter(name string, opts ...metric.Int64CounterOption) (metric.Int64Counter, error) {
	cfg := metric.NewInt64CounterConfig(opts...)
	m.catalog[name] = "Int64Counter|" + cfg.Description() + "|" + cfg.Unit()
	return fakeInt64{meter: m, name: name}, nil
}

func (m *fakeMeter) Int64UpDownCounter(name string, opts ...metric.Int64UpDownCounterOption) (metric.Int64UpDownCounter, error) {
	cfg := metric.NewInt64UpDownCounterConfig(opts...)
	m.catalog[name] = "Int64UpDownCounter|" + cfg.Description() + "|" + cfg.Unit()
	return fakeInt64UpDown{meter: m, name: name}, nil
}

func (m *fakeMeter) Int64Gauge(name string, opts ...metric.Int64GaugeOption) (metric.Int64Gauge, error) {
	cfg := metric.NewInt64GaugeConfig(opts...)
	m.catalog[name] = "Int64Gauge|" + cfg.Description() + "|" + cfg.Unit()
	return fakeGauge{meter: m, name: name}, nil
}

func (m *fakeMeter) Float64Histogram(name string, opts ...metric.Float64HistogramOption) (metric.Float64Histogram, error) {
	cfg := metric.NewFloat64HistogramConfig(opts...)
	m.catalog[name] = "Float64Histogram|" + cfg.Description() + "|" + cfg.Unit()
	return fakeHistogram{meter: m, name: name}, nil
}

type fakeInt64 struct {
	noopmetric.Int64Counter
	meter *fakeMeter
	name  string
}

func (c fakeInt64) Add(_ context.Context, v int64, opts ...metric.AddOption) {
	c.meter.record(c.name, float64(v), metric.NewAddConfig(opts).Attributes())
}

type fakeInt64UpDown struct {
	noopmetric.Int64UpDownCounter
	meter *fakeMeter
	name  string
}

func (c fakeInt64UpDown) Add(_ context.Context, v int64, opts ...metric.AddOption) {
	c.meter.record(c.name, float64(v), metric.NewAddConfig(opts).Attributes())
}

type fakeGauge struct {
	noopmetric.Int64Gauge
	meter *fakeMeter
	name  string
}

func (g fakeGauge) Record(_ context.Context, v int64, opts ...metric.RecordOption) {
	g.meter.record(g.name, float64(v), metric.NewRecordConfig(opts).Attributes())
}

type fakeHistogram struct {
	noopmetric.Float64Histogram
	meter *fakeMeter
	name  string
}

func (h fakeHistogram) Record(_ context.Context, v float64, opts ...metric.RecordOption) {
	h.meter.record(h.name, v, metric.NewRecordConfig(opts).Attributes())
}

func newTracer(t *testing.T) (*tracetest.InMemoryExporter, *sdktrace.TracerProvider) {
	t.Helper()
	exporter := tracetest.NewInMemoryExporter()
	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exporter),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })
	return exporter, provider
}

func TestNewWithoutMeterDisablesMetrics(t *testing.T) {
	assert.Nil(t, New(nil))
}

func TestNewCreatesTheCatalog(t *testing.T) {
	meter := newFakeMeter()
	require.NotNil(t, New(meter))

	assert.Equal(t, map[string]string{
		"ego.commands.total":                    "Int64Counter|Total number of commands processed|",
		"ego.commands.duration":                 "Float64Histogram|Duration of command processing in milliseconds|",
		"ego.events.persisted.total":            "Int64Counter|Total number of events persisted|",
		"ego.projection.events.processed.total": "Int64Counter|Total number of events processed by projections|",
		"ego.entities.active":                   "Int64UpDownCounter|Number of currently active entities|",
		"ego.projections.active":                "Int64UpDownCounter|Number of currently active projections|",
		"ego.projection.lag_ms":                 "Int64Gauge|Projection lag in milliseconds per shard|",
		"ego.projection.latest_offset":          "Int64Gauge|Current projection offset timestamp per shard|",
		"ego.projection.events_behind":          "Int64Gauge|Approximate number of unprocessed events per shard|",
	}, meter.catalog)
}

func TestNilInstrumentsRecordNothing(t *testing.T) {
	ctx := context.Background()
	var instruments *Instruments

	assert.NotPanics(t, func() {
		instruments.CommandReceived(ctx)
		instruments.CommandCompleted(ctx, time.Now())
		instruments.EventsPersisted(ctx, 3)
		instruments.EntityStarted(ctx)
		instruments.EntityStopped(ctx)
		instruments.ProjectionStarted(ctx)
		instruments.ProjectionStopped(ctx)
		instruments.ProjectionEventHandled(ctx)
		instruments.Shard("orders", 7).Record(ctx, 1, 2, 3)
	})
}

func TestRecordingMethods(t *testing.T) {
	ctx := context.Background()
	meter := newFakeMeter()
	instruments := New(meter)

	instruments.CommandReceived(ctx)
	instruments.CommandCompleted(ctx, time.Now().Add(-1500*time.Millisecond))
	instruments.EventsPersisted(ctx, 3)
	instruments.EntityStarted(ctx)
	instruments.EntityStopped(ctx)
	instruments.ProjectionStarted(ctx)
	instruments.ProjectionStopped(ctx)
	instruments.ProjectionEventHandled(ctx)

	values := func(name string) []float64 {
		var out []float64
		for _, m := range meter.measurements[name] {
			assert.Equal(t, 0, m.attrs.Len(), "%s carries no attributes", name)
			out = append(out, m.value)
		}
		return out
	}

	assert.Equal(t, []float64{1}, values("ego.commands.total"))
	duration := values("ego.commands.duration")
	require.Len(t, duration, 1)
	assert.GreaterOrEqual(t, duration[0], float64(1500), "duration is recorded in whole milliseconds")
	assert.Equal(t, duration[0], float64(int64(duration[0])), "duration is truncated to whole milliseconds")
	assert.Equal(t, []float64{3}, values("ego.events.persisted.total"))
	assert.Equal(t, []float64{1, -1}, values("ego.entities.active"))
	assert.Equal(t, []float64{1, -1}, values("ego.projections.active"))
	assert.Equal(t, []float64{1}, values("ego.projection.events.processed.total"))
}

func TestShardRecordsTheGaugesWithProjectionAttributes(t *testing.T) {
	ctx := context.Background()
	meter := newFakeMeter()
	shard := New(meter).Shard("orders", 7)

	shard.Record(ctx, 11, 22, 33)

	want := attribute.NewSet(
		attribute.String("projection_name", "orders"),
		attribute.Int64("shard", 7),
	)
	for name, value := range map[string]float64{
		"ego.projection.lag_ms":        11,
		"ego.projection.latest_offset": 22,
		"ego.projection.events_behind": 33,
	} {
		require.Len(t, meter.measurements[name], 1, name)
		assert.Equal(t, value, meter.measurements[name][0].value, name)
		assert.True(t, want.Equals(&meter.measurements[name][0].attrs), "%s attributes", name)
	}
}

func TestStartCommandSpan(t *testing.T) {
	t.Run("without a tracer the context is unchanged and there is no span", func(t *testing.T) {
		ctx := context.Background()
		got, span := StartCommandSpan(ctx, nil, "pid", nil)
		assert.Equal(t, ctx, got)
		assert.Nil(t, span)
	})

	t.Run("with a tracer the span is a child carrying the command attributes", func(t *testing.T) {
		exporter, provider := newTracer(t)
		tracer := provider.Tracer("test")
		parentCtx, parent := tracer.Start(context.Background(), "parent")

		ctx, span := StartCommandSpan(parentCtx, tracer, "pid-1", wrapperspb.String("x"))
		require.NotNil(t, span)
		assert.Equal(t, span.SpanContext(), trace.SpanContextFromContext(ctx))
		span.End()
		parent.End()

		stub := findSpan(t, exporter, "ego.command")
		assert.Equal(t, parent.SpanContext().SpanID(), stub.Parent.SpanID())
		assert.Equal(t, []attribute.KeyValue{
			attribute.String("ego.persistence_id", "pid-1"),
			attribute.String("ego.command_type", "google.protobuf.StringValue"),
		}, stub.Attributes)
	})
}

func TestSendCommandSpan(t *testing.T) {
	exporter, provider := newTracer(t)
	tracer := provider.Tracer("test")

	_, ok := StartSendCommandSpan(context.Background(), tracer, "entity-1", wrapperspb.String("x"))
	EndSendCommandSpan(ok, nil)

	_, failed := StartSendCommandSpan(context.Background(), tracer, "entity-2", wrapperspb.String("x"))
	EndSendCommandSpan(failed, errors.New("boom"))

	spans := exporter.GetSpans()
	require.Len(t, spans, 2)
	sort.Slice(spans, func(i, j int) bool { return spans[i].Status.Code < spans[j].Status.Code })

	assert.Equal(t, "ego.send_command", spans[0].Name)
	assert.Equal(t, codes.Unset, spans[0].Status.Code)
	assert.Empty(t, spans[0].Events)
	assert.Equal(t, []attribute.KeyValue{
		attribute.String("ego.entity_id", "entity-1"),
		attribute.String("ego.command_type", "google.protobuf.StringValue"),
	}, spans[0].Attributes)

	assert.Equal(t, codes.Error, spans[1].Status.Code)
	assert.Equal(t, "boom", spans[1].Status.Description)
	require.Len(t, spans[1].Events, 1)
	assert.Equal(t, "exception", spans[1].Events[0].Name)
}

func TestInstallPropagator(t *testing.T) {
	previous := otel.GetTextMapPropagator()
	t.Cleanup(func() { otel.SetTextMapPropagator(previous) })
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator())

	InstallPropagator()

	fields := otel.GetTextMapPropagator().Fields()
	sort.Strings(fields)
	assert.Equal(t, []string{"baggage", "traceparent", "tracestate"}, fields)
}

// TestInstrumentationStaysRuntimeNeutral guards the package boundary: the
// telemetry contract must never pull in the engine, the GoAkt adapter's
// internals or GoAkt itself. archcheck only sees direct imports, so this
// asserts the transitive closure via go list -deps.
func TestInstrumentationStaysRuntimeNeutral(t *testing.T) {
	goBin, err := exec.LookPath("go")
	if err != nil {
		t.Skip("the go tool is not on PATH")
	}

	out, err := exec.Command(goBin, "list", "-deps", ".").CombinedOutput()
	require.NoError(t, err, "go list -deps failed: %s", out)

	deps := strings.Fields(string(out))
	require.NotEmpty(t, deps)
	for _, dep := range deps {
		assert.Falsef(t, strings.HasPrefix(dep, "github.com/tochemey/goakt"),
			"internal/instrumentation must not depend on GoAkt; found %s", dep)
		assert.Falsef(t, strings.HasSuffix(dep, "/v4/engine"),
			"internal/instrumentation must not depend on the engine package; found %s", dep)
		assert.Falsef(t, strings.HasSuffix(dep, "/internal/extensions"),
			"internal/instrumentation must not depend on the GoAkt adapter's internals; found %s", dep)
	}
}

func findSpan(t *testing.T, exporter *tracetest.InMemoryExporter, name string) tracetest.SpanStub {
	t.Helper()
	for _, span := range exporter.GetSpans() {
		if span.Name == name {
			return span
		}
	}
	t.Fatalf("span %q was not exported", name)
	return tracetest.SpanStub{}
}
