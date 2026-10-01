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

func waitRunning(ctx *specs.Context, pid *goakt.PID) {
	ctx.Eventually(func() any { return pid.IsRunning() }, specs.BeTrue(),
		specs.WithTimeout(pollTimeout), specs.WithInterval(pollInterval))
}

// callCount reports how many calls method has received so far. A poll on it
// waits for a call that a child actor makes asynchronously.
func callCount(method *specmock.Method) func() any {
	return func() any { return len(method.Calls()) }
}

// ask sends msg to pid and returns the CommandReply. The Ask itself must
// succeed: a rejected command is carried inside the reply.
func ask(ctx *specs.Context, pid *goakt.PID, msg proto.Message) *egopb.CommandReply {
	reply, err := goakt.Ask(context.Background(), pid, msg, askTimeout)
	ctx.Expect(err).To(specs.BeNil())
	commandReply, ok := reply.(*egopb.CommandReply)
	ctx.Expect(ok).To(specs.BeTrue())
	return commandReply
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
