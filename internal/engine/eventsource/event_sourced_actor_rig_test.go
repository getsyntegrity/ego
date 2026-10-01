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

package eventsource

import (
	"context"
	"fmt"
	"time"

	specmock "github.com/getsyntegrity/go-specs/mock"
	"github.com/getsyntegrity/go-specs/specs"
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/extension"
	"google.golang.org/protobuf/proto"

	"github.com/getsyntegrity/ego/egopb"
	"github.com/getsyntegrity/ego/eventstream"
	"github.com/getsyntegrity/ego/internal/engine/enginetest"
	"github.com/getsyntegrity/ego/internal/extensions"
	"github.com/getsyntegrity/ego/internal/goaktlog"
	"github.com/getsyntegrity/ego/persistence"
	behaviorport "github.com/getsyntegrity/ego/port/behavior"
	"github.com/getsyntegrity/ego/tenancy"
	testpb "github.com/getsyntegrity/ego/test/data/testpb"
	"github.com/getsyntegrity/ego/testkit"
)

// newRecoveringActor builds the Actor that recover needs and nothing else: the
// behavior, the unscoped persistence scope and the id the stores are keyed by.
// A unit case sets the stores, the encryptor and the event adapters it wants on
// the returned value. No actor system is involved.
func newRecoveringActor(persistenceID string, behavior behaviorport.EventSourced) *Actor {
	return &Actor{
		persistenceID: persistenceID,
		behavior:      behavior,
		scope:         persistence.Unscoped(),
	}
}

// expectRecoveryFailure runs recover on entity and requires it to fail with a
// message that starts with prefix and to leave the actor with no recovered
// state. When cause is not nil the error must also wrap it.
func expectRecoveryFailure(ctx *specs.Context, entity *Actor, prefix string, cause error) {
	err := entity.recover(context.Background())
	ctx.Expect(err).To(specs.Not(specs.BeNil()))
	ctx.Expect(err.Error()).To(specs.StartWith(prefix))
	if cause != nil {
		ctx.Expect(err).To(specs.MatchError(cause))
	}
	ctx.Expect(entity.currentState).To(specs.BeNil())
	ctx.Expect(entity.eventsCounter).ToEqual(uint64(0))
}

// The component cases in this package start a real goakt actor system,
// so they cannot be unit tests. They still must not sleep: every wait polls an
// observable condition with ctx.Eventually, bounded by these limits.
const (
	pollTimeout  = 5 * time.Second
	pollInterval = 10 * time.Millisecond
	askTimeout   = 5 * time.Second

	// quietPeriod is how long a case watches that something does NOT happen.
	quietPeriod = 200 * time.Millisecond
)

// eventSourcedBehavior is an event sourced behavior that can also travel as a
// spawn dependency, which every behavior the test suite builds does.
type eventSourcedBehavior interface {
	behaviorport.EventSourced
	extension.Dependency
}

// mistypedExtensionCase is a case whose actor system registers an extension
// under extensionID with a type PreStart does not expect.
type mistypedExtensionCase struct {
	name        string
	system      string
	extensionID string
}

// actorRig is a goakt actor system wired with an events stream and the
// extensions a case passes, started for one go-specs case. Its teardown is
// registered with ctx.Cleanup, so a case never stops the system by hand. Create
// the mock controllers of a case before its rig: cleanups run last registered
// first, so the system stops before the controllers verify their expectations.
type actorRig struct {
	system  goakt.ActorSystem
	stream  eventstream.Stream
	stopped bool
}

// startActorRig starts an actor system whose actors retry PreStart three times,
// the setting most cases use. The events stream extension is always added to
// exts.
func startActorRig(ctx *specs.Context, exts ...extension.Extension) *actorRig {
	return startActorRigWith(ctx, "TestActorSystem", 3, exts...)
}

// startActorRigWith starts an actor system called name whose actors retry
// PreStart initRetries times. The events stream extension is always added to
// exts.
func startActorRigWith(ctx *specs.Context, name string, initRetries int, exts ...extension.Extension) *actorRig {
	bg := context.Background()
	stream := eventstream.New()

	all := append([]extension.Extension{extensions.NewEventsStream(stream)}, exts...)

	system, err := goakt.NewActorSystem(name,
		goakt.WithLogger(goaktlog.New(enginetest.DiscardLogger)),
		goakt.WithExtensions(all...),
		goakt.WithActorInitMaxRetries(initRetries))
	ctx.Expect(err).To(specs.BeNil())
	ctx.Expect(system.Start(bg)).To(specs.BeNil())

	rig := &actorRig{system: system, stream: stream}
	ctx.Cleanup(func() {
		if !rig.stopped {
			rig.shutdown(func(err error) { ctx.Errorf("stopping the actor system: %v", err) })
		}
	})
	return rig
}

// shutdown closes the stream and stops the system once, reporting a stop error
// through report.
func (r *actorRig) shutdown(report func(error)) {
	r.stopped = true
	r.stream.Close()
	if err := r.system.Stop(context.Background()); err != nil {
		report(err)
	}
}

// stop stops the system in the middle of a case, for the cases that start a
// second system on the same stores. The cleanup then has nothing left to do.
func (r *actorRig) stop(ctx *specs.Context) {
	r.shutdown(func(err error) { ctx.Expect(err).To(specs.BeNil()) })
}

// connectedEventsStore returns an in-memory testkit events store that is
// connected now and disconnected when the case ends, after the actor system
// stops.
func connectedEventsStore(ctx *specs.Context) *testkit.EventStore {
	bg := context.Background()
	store := testkit.NewEventsStore()
	ctx.Expect(store.Connect(bg)).To(specs.BeNil())
	ctx.Cleanup(func() {
		if err := store.Disconnect(bg); err != nil {
			ctx.Errorf("disconnecting the events store: %v", err)
		}
	})
	return store
}

// connectedSnapshotStore is connectedEventsStore for the snapshot store.
func connectedSnapshotStore(ctx *specs.Context) *testkit.SnapshotStore {
	bg := context.Background()
	store := testkit.NewSnapshotStore()
	ctx.Expect(store.Connect(bg)).To(specs.BeNil())
	ctx.Cleanup(func() {
		if err := store.Disconnect(bg); err != nil {
			ctx.Errorf("disconnecting the snapshot store: %v", err)
		}
	})
	return store
}

// trySpawn spawns an actor called name with opts and hands back the spawn
// error, for the cases that expect PreStart to refuse the actor.
func (r *actorRig) trySpawn(name string, opts ...goakt.SpawnOption) (*goakt.PID, error) {
	return r.system.Spawn(context.Background(), name, New(), opts...)
}

// expectRefused requires the spawn of name with opts to fail and to return no
// actor. When cause is not nil the spawn error must also wrap it.
func (r *actorRig) expectRefused(ctx *specs.Context, cause error, name string, opts ...goakt.SpawnOption) {
	pid, err := r.trySpawn(name, opts...)
	ctx.Expect(err).To(specs.Not(specs.BeNil()))
	if cause != nil {
		ctx.Expect(err).To(specs.MatchError(cause))
	}
	ctx.Expect(pid).To(specs.BeNil())
}

// expectRefusedFor is expectRefused for a long lived, stashing actor of behavior
// with the given extra dependencies.
func (r *actorRig) expectRefusedFor(ctx *specs.Context, cause error, behavior eventSourcedBehavior, deps ...extension.Dependency) {
	r.expectRefused(ctx, cause, behavior.ID(),
		goakt.WithDependencies(append([]extension.Dependency{behavior}, deps...)...),
		goakt.WithLongLived(), goakt.WithStashing())
}

// spawn spawns a long lived, stashing actor for behavior with the given extra
// dependencies, and waits until it reports itself running.
func (r *actorRig) spawn(ctx *specs.Context, behavior eventSourcedBehavior, deps ...extension.Dependency) *goakt.PID {
	pid, err := r.trySpawn(behavior.ID(),
		goakt.WithDependencies(append([]extension.Dependency{behavior}, deps...)...),
		goakt.WithLongLived(), goakt.WithStashing())
	ctx.Expect(err).To(specs.BeNil())
	ctx.Expect(pid).To(specs.Not(specs.BeNil()))
	waitRunning(ctx, pid)
	return pid
}

// spawnWithoutStash is spawn for an actor that does not stash, the way the cases
// that restart the system on the same stores spawn their second actor.
func (r *actorRig) spawnWithoutStash(ctx *specs.Context, behavior eventSourcedBehavior, deps ...extension.Dependency) *goakt.PID {
	pid, err := r.trySpawn(behavior.ID(),
		goakt.WithDependencies(append([]extension.Dependency{behavior}, deps...)...),
		goakt.WithLongLived())
	ctx.Expect(err).To(specs.BeNil())
	ctx.Expect(pid).To(specs.Not(specs.BeNil()))
	waitRunning(ctx, pid)
	return pid
}

func waitRunning(ctx *specs.Context, pid *goakt.PID) {
	ctx.Eventually(func() any { return pid.IsRunning() }, specs.BeTrue(),
		specs.WithTimeout(pollTimeout), specs.WithInterval(pollInterval))
}

func waitStopped(ctx *specs.Context, pid *goakt.PID) {
	ctx.Eventually(func() any { return pid.IsRunning() }, specs.BeFalse(),
		specs.WithTimeout(pollTimeout), specs.WithInterval(pollInterval))
}

// latestSnapshot reads the newest snapshot the store holds for id. A store error
// is returned as the observed value so a failing poll reports it.
func latestSnapshot(store persistence.SnapshotStore, id string) func() any {
	return func() any {
		snapshot, err := store.GetLatestSnapshot(context.Background(), persistence.Unscoped(), id)
		if err != nil {
			return err
		}
		return snapshot
	}
}

// latestEvent reads the newest event the store holds for id, or the store error.
func latestEvent(store persistence.EventsStore, id string) func() any {
	return func() any {
		event, err := store.GetLatestEvent(context.Background(), persistence.Unscoped(), id)
		if err != nil {
			return err
		}
		return event
	}
}

// beSnapshotAt matches the value latestSnapshot observes once the store holds a
// snapshot at sequence number seq. A missing snapshot or a store error does not
// match, and a failing poll reports the last value it saw.
func beSnapshotAt(seq uint64) specs.Matcher {
	return specs.Satisfy(fmt.Sprintf("is a snapshot at sequence %d", seq), func(v any) bool {
		snapshot, ok := v.(*egopb.Snapshot)
		return ok && snapshot != nil && snapshot.GetSequenceNumber() == seq
	})
}

// beProto matches a message that proto.Equal considers equal to want.
func beProto(want proto.Message) specs.Matcher {
	return specs.Satisfy(fmt.Sprintf("is proto-equal to %v", want), func(v any) bool {
		got, ok := v.(proto.Message)
		return ok && proto.Equal(want, got)
	})
}

// expectAccountState requires state to be at sequence number seq and to carry
// the account want.
func expectAccountState(ctx *specs.Context, state *egopb.StateReply, seq uint64, want *testpb.Account) {
	ctx.Expect(state.GetSequenceNumber()).ToEqual(seq)
	got := new(testpb.Account)
	ctx.Expect(state.GetState().UnmarshalTo(got)).To(specs.BeNil())
	ctx.Expect(got).To(beProto(want))
}

// callCount reports how many calls method has received so far. A poll on it
// waits for a call that a child actor makes asynchronously.
func callCount(method *specmock.Method) func() any {
	return func() any { return len(method.Calls()) }
}

// ask sends msg to pid and returns the CommandReply. The Ask itself must
// succeed: a rejected command is carried inside the reply.
func ask(ctx *specs.Context, pid *goakt.PID, msg proto.Message) *egopb.CommandReply {
	return askWith(ctx, context.Background(), pid, msg)
}

// askWith is ask with the context the command travels in, for the cases that
// attach a TenantContext to it.
func askWith(ctx *specs.Context, callCtx context.Context, pid *goakt.PID, msg proto.Message) *egopb.CommandReply {
	reply, err := goakt.Ask(callCtx, pid, msg, askTimeout)
	ctx.Expect(err).To(specs.BeNil())
	commandReply, ok := reply.(*egopb.CommandReply)
	ctx.Expect(ok).To(specs.BeTrue())
	return commandReply
}

// backgroundAsk is a command whose reply is deferred until its batch cycle
// flushes, sent from its own goroutine so the case can send another command
// into the same open cycle.
type backgroundAsk struct {
	done  chan struct{}
	reply any
	err   error
}

// askInBackground sends msg to pid with callCtx and returns at once.
func askInBackground(callCtx context.Context, pid *goakt.PID, msg proto.Message) *backgroundAsk {
	pending := &backgroundAsk{done: make(chan struct{})}
	go func() {
		defer close(pending.done)
		pending.reply, pending.err = goakt.Ask(callCtx, pid, msg, askTimeout)
	}()
	return pending
}

// await waits for the reply and returns it. The Ask itself must have
// succeeded.
func (b *backgroundAsk) await(ctx *specs.Context) *egopb.CommandReply {
	<-b.done
	ctx.Expect(b.err).To(specs.BeNil())
	commandReply, ok := b.reply.(*egopb.CommandReply)
	ctx.Expect(ok).To(specs.BeTrue())
	return commandReply
}

// attachTenant returns a context that carries tenant, the way Engine.SendCommand
// attaches it before it reaches the actor.
func attachTenant(ctx *specs.Context, tenant tenancy.TenantContext) context.Context {
	attached, err := tenancy.Attach(context.Background(), tenant)
	ctx.Expect(err).To(specs.BeNil())
	return attached
}

// tenantScopeOf builds the persistence scope of the named tenant.
func tenantScopeOf(ctx *specs.Context, name tenancy.TenantID) persistence.Scope {
	scope, err := persistence.NewTenantScope(name)
	ctx.Expect(err).To(specs.BeNil())
	return scope
}

// latestTenantEvent reads the newest event the store holds for id in scope, or
// the store error.
func latestTenantEvent(store persistence.EventsStore, scope persistence.Scope, id string) func() any {
	return func() any {
		event, err := store.GetLatestEvent(context.Background(), scope, id)
		if err != nil {
			return err
		}
		return event
	}
}

// invocations reports how many times the probe behavior has run HandleCommand.
// A poll on it waits until a command that a background Ask sent has reached the
// actor.
func invocations(behavior interface{ InvocationCount() int }) func() any {
	return func() any { return behavior.InvocationCount() }
}

// stateReplyOf requires reply to be a state reply, the success shape, and
// returns it.
func stateReplyOf(ctx *specs.Context, reply *egopb.CommandReply) *egopb.StateReply {
	ctx.Expect(reply.GetReply()).To(specs.Satisfy("is a state reply", func(v any) bool {
		_, ok := v.(*egopb.CommandReply_StateReply)
		return ok
	}))
	return reply.GetStateReply()
}

// errorReplyMessage requires reply to be an error reply and returns its
// message.
func errorReplyMessage(ctx *specs.Context, reply *egopb.CommandReply) string {
	ctx.Expect(reply.GetReply()).To(specs.Satisfy("is an error reply", func(v any) bool {
		_, ok := v.(*egopb.CommandReply_ErrorReply)
		return ok
	}))
	return reply.GetErrorReply().GetMessage()
}
