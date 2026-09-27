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

package goakt

import (
	"errors"
	"regexp"
	"slices"

	kitlog "github.com/pablogore/kit-logger/pkg/logger"
	actor "github.com/tochemey/goakt/v4/actor"

	"github.com/pablogore/ego/v4"
	"github.com/pablogore/ego/v4/compose"
)

var (
	// ErrClusterConfigRequired is reported by New when WithCluster is given
	// a nil cluster configuration (rule G1).
	ErrClusterConfigRequired = errors.New("compose/goakt: WithCluster needs a non-nil cluster configuration (G1)")
	// ErrClusterKindsRequired is reported by New when WithCluster is given
	// no entity kinds (rule G1): a node could not rebuild a behavior a peer
	// places on it, so the first remote spawn would fail.
	ErrClusterKindsRequired = errors.New("compose/goakt: WithCluster needs at least one entity kind (G1)")
)

// Option configures the GoAkt-specific settings of an App: the parts a
// runtime-neutral compose.Spec deliberately does not carry (design §D2).
type Option func(*options)

type options struct {
	logger       kitlog.Logger
	telemetry    *ego.Telemetry
	clusterSet   bool
	cluster      *actor.ClusterConfig
	kinds        []ego.BehaviorKind
	actorOptions []actor.Option
}

// WithLogger sets the logger the engine and the actor system log through.
// Without it, Ego's default logger is used (see ego.WithLogger).
func WithLogger(logger kitlog.Logger) Option {
	return func(o *options) { o.logger = logger }
}

// WithTelemetry enables OpenTelemetry instrumentation (see ego.WithTelemetry).
// The consumer keeps owning the telemetry providers. Starting the engine
// sets the process-wide OpenTelemetry propagator, as ego.Engine.Start does.
func WithTelemetry(telemetry *ego.Telemetry) Option {
	return func(o *options) { o.telemetry = telemetry }
}

// WithCluster runs the actor system in cluster mode with cfg. It registers
// ego.ClusterKinds() on cfg and the given behavior kinds with
// ego.WithBehaviorKinds, the two registrations a cluster node needs
// (design §5.1). cfg is modified when App.Start runs, not by New.
//
// kinds are behavior prototypes, one pointer per behavior type this node
// may host, for example new(AccountBehavior); at least one is required, or
// New fails with ErrClusterKindsRequired (rule G1). A value of the
// deprecated ego.EntityKind type is assignable to ego.BehaviorKind; a
// []ego.EntityKind slice has to be converted element by element.
//
// Cluster mode also needs remoting, which is passed through
// WithActorSystemOptions, for example
// WithActorSystemOptions(actor.WithRemote(remote.NewConfig(host, port))).
func WithCluster(cfg *actor.ClusterConfig, kinds ...ego.BehaviorKind) Option {
	return func(o *options) {
		o.clusterSet = true
		o.cluster = cfg
		o.kinds = append(o.kinds, kinds...)
	}
}

// WithActorSystemOptions appends GoAkt options to the ones the App derives
// from the Spec, for settings it does not model itself (remoting, TLS,
// custom extensions, supervision). They are applied after Ego's own
// options, so an option that GoAkt applies last-wins overrides Ego's value.
func WithActorSystemOptions(opts ...actor.Option) Option {
	return func(o *options) { o.actorOptions = append(o.actorOptions, opts...) }
}

// actorSystemName is GoAkt's own naming rule, checked by
// actor.NewActorSystem (goakt v4.5.4, actor/actor_system.go). It is
// repeated here so New can reject a bad name without constructing
// anything; a test compares both on the same names so a drift fails.
var actorSystemName = regexp.MustCompile("^[a-zA-Z0-9][a-zA-Z0-9-_]*$")

// validate returns the GoAkt-specific problems (design §D4a, G1 and G2).
func (o *options) validate(spec compose.Spec) []error {
	var errs []error
	if o.clusterSet {
		if o.cluster == nil {
			errs = append(errs, ErrClusterConfigRequired)
		}
		if len(o.kinds) == 0 {
			errs = append(errs, ErrClusterKindsRequired)
		}
	}
	switch {
	case spec.Name == "":
		errs = append(errs, &compose.ValidationError{Rule: "G2", Field: "Name", Problem: "required: it names the GoAkt actor system"})
	case !actorSystemName.MatchString(spec.Name):
		errs = append(errs, &compose.ValidationError{Rule: "G2", Field: "Name", Problem: "not a valid GoAkt actor-system name: letters, digits, '-' and '_' only, starting with a letter or digit"})
	}
	return errs
}

// egoOptions translates the Spec and these options into the ego.Config
// options step 2 builds the engine's configuration from.
func (o *options) egoOptions(spec compose.Spec) []ego.Option {
	var opts []ego.Option
	if o.logger != nil {
		opts = append(opts, ego.WithLogger(o.logger))
	}
	if o.telemetry != nil {
		opts = append(opts, ego.WithTelemetry(o.telemetry))
	}
	if len(o.kinds) > 0 {
		opts = append(opts, ego.WithBehaviorKinds(o.kinds...))
	}
	opts = append(opts, ego.WithEntityFamilies(entityFamilies(spec.Families)))
	if spec.StateStore != nil {
		opts = append(opts, ego.WithStateStore(spec.StateStore))
	}
	if spec.SnapshotStore != nil {
		opts = append(opts, ego.WithSnapshotStore(spec.SnapshotStore))
	}
	if spec.OffsetStore != nil {
		opts = append(opts, ego.WithOffsetStore(spec.OffsetStore))
	}
	for _, name := range projectionNames(spec) {
		opts = append(opts, ego.WithProjection(name, spec.Projections[name]))
	}
	if len(spec.EventAdapters) > 0 {
		opts = append(opts, ego.WithEventAdapters(spec.EventAdapters...))
	}
	if spec.Encryptor != nil {
		opts = append(opts, ego.WithEncryptor(spec.Encryptor))
	}
	if spec.TenantResolver != nil {
		opts = append(opts, ego.WithTenantResolver(spec.TenantResolver))
	}
	return opts
}

// entityFamilies maps compose's runtime-neutral families onto the engine's.
func entityFamilies(families compose.Family) ego.EntityFamily {
	var out ego.EntityFamily
	if families&compose.EventSourced != 0 {
		out |= ego.EventSourcedFamily
	}
	if families&compose.DurableState != 0 {
		out |= ego.DurableStateFamily
	}
	if families&compose.Saga != 0 {
		out |= ego.SagaFamily
	}
	return out
}

// projectionNames returns the Spec's projection names in sorted order, so
// projections are registered and started deterministically.
func projectionNames(spec compose.Spec) []string {
	names := make([]string, 0, len(spec.Projections))
	for name := range spec.Projections {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}
