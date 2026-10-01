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

// The self-checks in this file prove that adaptertest fails when it should:
// each deliberately broken fake below breaks one rule, and the test asserts
// that exactly the check for that rule fails. A conformance suite that
// passes a broken adapter is worse than none.
package adaptertest_test

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/persistence"
	"github.com/getsyntegrity/ego/port/adapter"
	"github.com/getsyntegrity/ego/port/adapter/adaptertest"
)

const (
	publisherPort adapter.Port       = "publishing.EventPublisher"
	storePort     adapter.Port       = persistence.PortEventsStore
	flush         adapter.Capability = "test.flush"
)

// ---------------------------------------------------------------------------
// Fakes. owned is a correct owned adapter; every broken variant is built
// from it by flipping one field.
// ---------------------------------------------------------------------------

type owned struct {
	desc     *adapter.Descriptor // nil: undeclared
	unstable bool                // Describe returns a different Name each call
	calls    int

	startErr error
	closed   int
	closeErr error // returned by every Close after the first
}

func (o *owned) Close(ctx context.Context) error {
	o.closed++
	if o.closed > 1 && o.closeErr != nil {
		return o.closeErr
	}
	return nil
}

func (o *owned) describe() adapter.Descriptor {
	o.calls++
	d := *o.desc
	if o.unstable {
		d.Name = fmt.Sprintf("%s-%d", d.Name, o.calls)
	}
	return d
}

// ownedDescribed adds Describe.
type ownedDescribed struct{ *owned }

func (o ownedDescribed) Describe() adapter.Descriptor { return o.describe() }

// starter adds Describe, Start and Ping.
type starter struct{ *owned }

func (s starter) Describe() adapter.Descriptor   { return s.describe() }
func (s starter) Start(context.Context) error    { return s.startErr }
func (s starter) Ping(ctx context.Context) error { return ctx.Err() }

// flusher is an owned, constructor-acquiring adapter with a port-specific
// capability the suite can only check through Target.Capabilities.
type flusher struct {
	ownedDescribed
}

func (flusher) Flush() {}

// store is a correct borrowed adapter.
type store struct {
	pingErr error
}

func (s *store) Connect(context.Context) error    { return nil }
func (s *store) Disconnect(context.Context) error { return nil }
func (s *store) Ping(context.Context) error       { return s.pingErr }
func (s *store) Describe() adapter.Descriptor {
	return adapter.Descriptor{Ports: []adapter.Port{storePort}, Name: "fake-store"}
}

func desc(name string, caps ...adapter.Capability) *adapter.Descriptor {
	return &adapter.Descriptor{Ports: []adapter.Port{publisherPort}, Name: name, Capabilities: caps}
}

// newOf returns a factory that hands out v itself on every call. The fakes
// that use it keep no state a second check could trip over; a fake whose
// release is stateful gets a fresh value per call instead.
func newOf(v any) func(*testing.T) (any, error) {
	return func(*testing.T) (any, error) { return v, nil }
}

func ownedTarget(v any) adaptertest.Target {
	return adaptertest.Target{Port: publisherPort, Ownership: adaptertest.Owned, New: newOf(v)}
}

// resultSet indexes results by check name.
type resultSet = map[string]adaptertest.Result

// outcomes logs every result and indexes them by check name.
func outcomes(t testing.TB, results []adaptertest.Result) resultSet {
	t.Helper()
	out := resultSet{}
	for _, r := range results {
		t.Logf("%-38s %-14s %s", r.Check, r.Outcome, r.Detail)
		out[r.Check] = r
	}
	return out
}

// The matchers below judge a resultSet. They replace the old require helpers:
// a failure goes through the spec and names the check that broke.

// resultIs matches one Result with the given outcome and, when detail is not
// empty, a Detail that contains it.
func resultIs(want adaptertest.Outcome, detail string) specs.Matcher {
	outcome := specs.Project("Outcome", func(r adaptertest.Result) adaptertest.Outcome { return r.Outcome }, specs.Equal(want))
	if detail == "" {
		return outcome
	}
	return specs.All(outcome, specs.Project("Detail", func(r adaptertest.Result) string { return r.Detail }, specs.Contain(detail)))
}

// checkIs lists the matchers that say check has the expected outcome (and
// detail, when given). They are kept flat so that a failure reads as one
// level: "AT-3/release twice.Outcome: expected passed to equal failed".
func checkIs(check string, want adaptertest.Outcome, detail string) []specs.Matcher {
	ms := []specs.Matcher{
		specs.HaveKey(check),
		specs.Project(check+".Outcome", func(set resultSet) adaptertest.Outcome { return set[check].Outcome }, specs.Equal(want)),
	}
	if detail != "" {
		ms = append(ms, specs.Project(check+".Detail", func(set resultSet) string { return set[check].Detail }, specs.Contain(detail)))
	}
	return ms
}

// outcomeIs matches a resultSet that has a result for check and whose outcome
// (and detail, when given) is the expected one.
func outcomeIs(check string, want adaptertest.Outcome, detail string) specs.Matcher {
	return specs.All(checkIs(check, want, detail)...)
}

// allPassed matches a resultSet in which every named check passed.
func allPassed(checks ...string) specs.Matcher {
	var ms []specs.Matcher
	for _, check := range checks {
		ms = append(ms, checkIs(check, adaptertest.Passed, "")...)
	}
	return specs.All(ms...)
}

// onlyFailure matches a resultSet in which check failed (mentioning detail)
// and no other check failed, so a broken fake is caught by the rule it breaks
// and by no other.
func onlyFailure(check, detail string) specs.Matcher {
	others := specs.Project("other failed checks", func(set resultSet) []string {
		var names []string
		for name, r := range set {
			if name != check && r.Outcome == adaptertest.Failed {
				names = append(names, fmt.Sprintf("%s (%q)", name, r.Detail))
			}
		}
		slices.Sort(names)
		return names
	}, specs.BeEmpty())
	return specs.All(append(checkIs(check, adaptertest.Failed, detail), others)...)
}

// everyResult matches a non-empty resultSet whose results all match m.
func everyResult(m specs.Matcher) specs.Matcher {
	return specs.Project("results", func(set resultSet) []adaptertest.Result {
		results := make([]adaptertest.Result, 0, len(set))
		for _, r := range set {
			results = append(results, r)
		}
		slices.SortFunc(results, func(a, b adaptertest.Result) int { return cmp.Compare(a.Check, b.Check) })
		return results
	}, specs.All(specs.Not(specs.BeEmpty()), specs.EveryElement(m)))
}

// ---------------------------------------------------------------------------
// Correct adapters pass, and Run reports what it could not exercise.
// ---------------------------------------------------------------------------

func TestRun_CorrectStarterPassesEveryCheck(t *testing.T) {
	specs.Describe(t, "Run passes a correct starter on every check", func(s *specs.Spec) {
		s.It("reports AT-1 to AT-5 as passed", func(ctx *specs.Context) {
			failing := errors.New("dial refused")
			results := adaptertest.Run(ctx.T, adaptertest.Target{
				Port:      publisherPort,
				Ownership: adaptertest.Owned,
				New: func(*testing.T) (any, error) {
					return starter{&owned{desc: desc("fake", adapter.CapStart, adapter.CapReady)}}, nil
				},
				FailStart: func(*testing.T) (any, error) {
					return starter{&owned{desc: desc("fake", adapter.CapStart, adapter.CapReady), startErr: failing}}, nil
				},
				Stall: func(*testing.T) {},
			})
			got := outcomes(ctx.T, results)
			ctx.Expect(got).To(allPassed("AT-1", "AT-2", "AT-3/release twice", "AT-3/release without acquire", "AT-3/release after failed acquire", "AT-4", "AT-5"))
			ctx.Expect(got).To(specs.HaveLen(7))
		})
	})
}

func TestRun_CorrectBorrowedStorePasses(t *testing.T) {
	specs.Describe(t, "Run passes a correct borrowed store and reports the hooks it lacks as not exercised", func(s *specs.Spec) {
		s.It("passes AT-1, release twice, release without acquire and AT-5, and does not exercise the hook checks", func(ctx *specs.Context) {
			got := outcomes(ctx.T, adaptertest.Run(ctx.T, adaptertest.Target{
				Port:      storePort,
				Ownership: adaptertest.Borrowed,
				New:       func(*testing.T) (any, error) { return &store{}, nil },
			}))
			ctx.Expect(got).To(outcomeIs("AT-1", adaptertest.Passed, ""))
			ctx.Expect(got).To(outcomeIs("AT-3/release twice", adaptertest.Passed, ""))
			ctx.Expect(got).To(outcomeIs("AT-3/release without acquire", adaptertest.Passed, ""))
			ctx.Expect(got).To(outcomeIs("AT-5", adaptertest.Passed, ""))
			ctx.Expect(got).To(outcomeIs("AT-2", adaptertest.NotExercised, "no hook"))
			ctx.Expect(got).To(outcomeIs("AT-3/release after failed acquire", adaptertest.NotExercised, "no hook"))
			ctx.Expect(got).To(outcomeIs("AT-4", adaptertest.NotExercised, "no hook"))
		})
	})
}

// Requirement "a skip means unreachable, nothing else": an adapter that
// acquires in its constructor has AT-2 and the failed-acquire case of AT-3
// reported as not exercised, never as passed, even when FailStart is set.
func TestCapture_ConstructorAcquireIsNotExercised(t *testing.T) {
	specs.Describe(t, "Capture reports an adapter that acquires in its constructor as not exercised", func(s *specs.Spec) {
		s.It("does not exercise AT-2, the failed-acquire release and AT-4, even with FailStart set", func(ctx *specs.Context) {
			target := ownedTarget(ownedDescribed{&owned{desc: desc("ctor")}})
			target.FailStart = func(*testing.T) (any, error) { return nil, errors.New("dial refused") }
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, target))
			ctx.Expect(got).To(outcomeIs("AT-2", adaptertest.NotExercised, "acquire happens in the constructor"))
			ctx.Expect(got).To(outcomeIs("AT-3/release after failed acquire", adaptertest.NotExercised, "acquire happens in the constructor"))
			ctx.Expect(got).To(outcomeIs("AT-4", adaptertest.NotExercised, "no hook"))
			ctx.Expect(got).To(outcomeIs("AT-5", adaptertest.NotExercised, "Pinger"))
			ctx.Expect(got).To(outcomeIs("AT-1", adaptertest.Passed, ""))
			ctx.Expect(got).To(outcomeIs("AT-3/release twice", adaptertest.Passed, ""))
		})
	})
}

func TestCapture_UndeclaredAdapterIsNotExercisedByAT1(t *testing.T) {
	specs.Describe(t, "Capture does not exercise AT-1 on an undeclared adapter", func(s *specs.Spec) {
		s.It("reports AT-1 as not exercised and undeclared", func(ctx *specs.Context) {
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, ownedTarget(&owned{})))
			ctx.Expect(got).To(outcomeIs("AT-1", adaptertest.NotExercised, "undeclared"))
		})
	})
}

// ---------------------------------------------------------------------------
// Only ErrUnreachable skips.
// ---------------------------------------------------------------------------

func TestCapture_OnlyErrUnreachableSkips(t *testing.T) {
	type skipCase struct {
		name   string
		target adaptertest.Target
		want   specs.Matcher // judges the resultSet Capture returns
	}
	// factoryFailing returns a target whose factory fails with err.
	factoryFailing := func(err error) adaptertest.Target {
		target := ownedTarget(nil)
		target.New = func(*testing.T) (any, error) { return nil, err }
		return target
	}
	failStartBadConfig := ownedTarget(starter{&owned{desc: desc("fake", adapter.CapStart, adapter.CapReady)}})
	failStartBadConfig.FailStart = func(*testing.T) (any, error) { return nil, errors.New("bad config") }
	var typedNil *owned

	specs.Describe(t, "Capture skips only on ErrUnreachable", func(s *specs.Spec) {
		specs.Table(s, []skipCase{
			{
				"wrapped ErrUnreachable skips every check",
				factoryFailing(fmt.Errorf("no broker at localhost:9092: %w", adaptertest.ErrUnreachable)),
				everyResult(resultIs(adaptertest.Skipped, "")),
			},
			{"any other factory error fails", factoryFailing(errors.New("bad config")), everyResult(resultIs(adaptertest.Failed, ""))},
			{"a nil value fails", ownedTarget(typedNil), everyResult(resultIs(adaptertest.Failed, "nil"))},
			{"a FailStart error other than ErrUnreachable fails AT-2", failStartBadConfig, outcomeIs("AT-2", adaptertest.Failed, "bad config")},
		}, func(c skipCase) string { return c.name }, func(ctx *specs.Context, c skipCase) {
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, c.target))
			ctx.Expect(got).To(c.want)
		})
	})
}

// ---------------------------------------------------------------------------
// AT-1: a lying or unstable descriptor fails.
// ---------------------------------------------------------------------------

// Spec scenario "a lying descriptor fails".
func TestCapture_LyingDescriptorFailsAT1NamingCapStart(t *testing.T) {
	specs.Describe(t, "Capture fails AT-1 for an adapter that declares CapStart without a Starter", func(s *specs.Spec) {
		s.It("fails only AT-1 and names CapStart", func(ctx *specs.Context) {
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, ownedTarget(ownedDescribed{&owned{desc: desc("liar", adapter.CapStart)}})))
			ctx.Expect(got).To(onlyFailure("AT-1", string(adapter.CapStart)))
		})
	})
}

func TestCapture_UndeclaredCapabilityFailsAT1(t *testing.T) {
	specs.Describe(t, "Capture fails AT-1 for an adapter that implements a lifecycle interface without declaring it", func(s *specs.Spec) {
		s.It("fails AT-1 and names CapStart", func(ctx *specs.Context) {
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, ownedTarget(starter{&owned{desc: desc("shy", adapter.CapReady)}})))
			ctx.Expect(got).To(outcomeIs("AT-1", adaptertest.Failed, string(adapter.CapStart)))
		})
	})
}

func TestCapture_DeclaredReadyWithoutPingFailsAT1(t *testing.T) {
	specs.Describe(t, "Capture fails AT-1 for an adapter that declares CapReady without a Pinger", func(s *specs.Spec) {
		s.It("fails only AT-1 and names CapReady", func(ctx *specs.Context) {
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, ownedTarget(ownedDescribed{&owned{desc: desc("liar", adapter.CapReady)}})))
			ctx.Expect(got).To(onlyFailure("AT-1", string(adapter.CapReady)))
		})
	})
}

func TestCapture_WrongPortFailsAT1(t *testing.T) {
	specs.Describe(t, "Capture fails AT-1 when the descriptor does not list the target port", func(s *specs.Spec) {
		s.It("fails only AT-1 and names the port", func(ctx *specs.Context) {
			target := ownedTarget(ownedDescribed{&owned{desc: desc("fake")}})
			target.Port = "publishing.StatePublisher"
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, target))
			ctx.Expect(got).To(onlyFailure("AT-1", "publishing.StatePublisher"))
		})
	})
}

func TestCapture_UnstableDescriptorFailsAT1(t *testing.T) {
	specs.Describe(t, "Capture fails AT-1 when the descriptor changes between calls", func(s *specs.Spec) {
		s.It("fails only AT-1 and says the descriptor is not stable", func(ctx *specs.Context) {
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, ownedTarget(ownedDescribed{&owned{desc: desc("fake"), unstable: true}})))
			ctx.Expect(got).To(onlyFailure("AT-1", "stable"))
		})
	})
}

func TestCapture_EmptyNameFailsAT1(t *testing.T) {
	specs.Describe(t, "Capture fails AT-1 for a descriptor with an empty Name", func(s *specs.Spec) {
		s.It("fails only AT-1 and names Name", func(ctx *specs.Context) {
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, ownedTarget(ownedDescribed{&owned{desc: desc("")}})))
			ctx.Expect(got).To(onlyFailure("AT-1", "Name"))
		})
	})
}

// Spec scenario "a capability without a check fails".
func TestCapture_CapabilityWithoutCheckFailsAT1(t *testing.T) {
	specs.Describe(t, "Capture fails AT-1 for a declared capability that has no check", func(s *specs.Spec) {
		s.It("fails only AT-1 with no check supplied", func(ctx *specs.Context) {
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, ownedTarget(flusher{ownedDescribed{&owned{desc: desc("fake", flush)}}})))
			ctx.Expect(got).To(onlyFailure("AT-1", "no check supplied"))
		})
	})
}

// Target.Capabilities is checked in both directions: declared implies
// implemented, and implemented implies declared.
func TestCapture_TargetCapabilitiesAreCheckedBothWays(t *testing.T) {
	type capabilityCase struct {
		name  string
		value any
		want  specs.Matcher // judges the resultSet Capture returns
	}
	implementsFlush := func(v any) bool {
		_, ok := v.(interface{ Flush() })
		return ok
	}
	specs.Describe(t, "Capture checks Target.Capabilities in both directions", func(s *specs.Spec) {
		specs.Table(s, []capabilityCase{
			{"declared and implemented", flusher{ownedDescribed{&owned{desc: desc("fake", flush)}}}, outcomeIs("AT-1", adaptertest.Passed, "")},
			{"declared, not implemented", ownedDescribed{&owned{desc: desc("fake", flush)}}, onlyFailure("AT-1", string(flush))},
			{"implemented, not declared", flusher{ownedDescribed{&owned{desc: desc("fake")}}}, onlyFailure("AT-1", string(flush))},
			{"neither", ownedDescribed{&owned{desc: desc("fake")}}, outcomeIs("AT-1", adaptertest.Passed, "")},
		}, func(c capabilityCase) string { return c.name }, func(ctx *specs.Context, c capabilityCase) {
			target := ownedTarget(c.value)
			target.Capabilities = map[adapter.Capability]func(any) bool{flush: implementsFlush}
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, target))
			ctx.Expect(got).To(c.want)
		})
	})
}

func TestCapture_TargetCapabilitiesMayNotListSuiteCapabilities(t *testing.T) {
	specs.Describe(t, "Capture rejects a Target.Capabilities entry for a suite capability", func(s *specs.Spec) {
		s.It("fails only AT-1 and names CapStart", func(ctx *specs.Context) {
			target := ownedTarget(ownedDescribed{&owned{desc: desc("fake")}})
			target.Capabilities = map[adapter.Capability]func(any) bool{adapter.CapStart: func(any) bool { return false }}
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, target))
			ctx.Expect(got).To(onlyFailure("AT-1", string(adapter.CapStart)))
		})
	})
}

// CapReady is implied by the store ports, so a store that implements Ping
// without declaring CapReady passes AT-1.
func TestCapture_ImpliedCapReadyIsNotRequiredForStores(t *testing.T) {
	specs.Describe(t, "Capture does not require CapReady to be declared on a store port", func(s *specs.Spec) {
		s.It("passes AT-1 for a store that pings without declaring CapReady", func(ctx *specs.Context) {
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, adaptertest.Target{
				Port: storePort, Ownership: adaptertest.Borrowed, New: newOf(&store{}),
			}))
			ctx.Expect(got).To(outcomeIs("AT-1", adaptertest.Passed, ""))
		})
	})
}

// ---------------------------------------------------------------------------
// AT-2, AT-3, AT-4, AT-5: broken lifecycles fail the matching check.
// ---------------------------------------------------------------------------

func TestCapture_NonIdempotentCloseFailsAT3(t *testing.T) {
	specs.Describe(t, "Capture fails AT-3 for a release that is not idempotent", func(s *specs.Spec) {
		s.It("fails only release twice and reports the second Close error", func(ctx *specs.Context) {
			target := ownedTarget(nil)
			target.New = func(*testing.T) (any, error) {
				return ownedDescribed{&owned{desc: desc("fake"), closeErr: errors.New("use of closed network connection")}}, nil
			}
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, target))
			ctx.Expect(got).To(onlyFailure("AT-3/release twice", "use of closed network connection"))
		})
	})
}

func TestCapture_FailStartWhoseAcquireSucceedsFailsAT2(t *testing.T) {
	specs.Describe(t, "Capture fails AT-2 when the FailStart value acquires successfully", func(s *specs.Spec) {
		s.It("fails AT-2 and says the acquire succeeded", func(ctx *specs.Context) {
			target := ownedTarget(starter{&owned{desc: desc("fake", adapter.CapStart, adapter.CapReady)}})
			target.FailStart = newOf(starter{&owned{desc: desc("fake", adapter.CapStart, adapter.CapReady)}})
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, target))
			ctx.Expect(got).To(outcomeIs("AT-2", adaptertest.Failed, "succeeded"))
		})
	})
}

func TestCapture_CloseAfterFailedStartFailsAT3(t *testing.T) {
	specs.Describe(t, "Capture fails AT-3 for a release that errors after a failed acquire", func(s *specs.Spec) {
		s.It("passes a tolerant release and fails only the failed-acquire release of a strict one", func(ctx *specs.Context) {
			failing := errors.New("dial refused")
			target := ownedTarget(starter{&owned{desc: desc("fake", adapter.CapStart, adapter.CapReady)}})
			target.FailStart = func(*testing.T) (any, error) {
				return starter{&owned{desc: desc("fake", adapter.CapStart, adapter.CapReady), startErr: failing}}, nil
			}
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, target))
			ctx.Expect(got).To(outcomeIs("AT-3/release after failed acquire", adaptertest.Passed, ""))

			target.FailStart = func(*testing.T) (any, error) {
				return failedCloser{starter{&owned{desc: desc("fake", adapter.CapStart, adapter.CapReady), startErr: failing}}}, nil
			}
			got = outcomes(ctx.T, adaptertest.Capture(ctx.T, target))
			ctx.Expect(got).To(onlyFailure("AT-3/release after failed acquire", "never started"))
		})
	})
}

// failedCloser's Close fails when Start never succeeded.
type failedCloser struct{ starter }

func (failedCloser) Close(context.Context) error { return errors.New("close: never started") }

func TestCapture_FailingPingFailsAT5(t *testing.T) {
	specs.Describe(t, "Capture fails AT-5 for an adapter whose Ping fails", func(s *specs.Spec) {
		s.It("fails only AT-5 and reports the Ping error", func(ctx *specs.Context) {
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, adaptertest.Target{
				Port: storePort, Ownership: adaptertest.Borrowed, New: newOf(&store{pingErr: errors.New("not ready")}),
			}))
			ctx.Expect(got).To(onlyFailure("AT-5", "not ready"))
		})
	})
}

// ---------------------------------------------------------------------------
// Target validation.
// ---------------------------------------------------------------------------

func TestCapture_InvalidTargetFails(t *testing.T) {
	type targetCase struct {
		name   string
		target adaptertest.Target
	}
	specs.Describe(t, "Capture fails every check for an invalid Target", func(s *specs.Spec) {
		specs.Table(s, []targetCase{
			{"no port", adaptertest.Target{Ownership: adaptertest.Owned, New: newOf(&owned{})}},
			{"no ownership", adaptertest.Target{Port: publisherPort, New: newOf(&owned{})}},
			{"no factory", adaptertest.Target{Port: publisherPort, Ownership: adaptertest.Owned}},
			{"owned, no Close", adaptertest.Target{Port: publisherPort, Ownership: adaptertest.Owned, New: newOf(struct{}{})}},
			{"borrowed, no Connect", adaptertest.Target{Port: storePort, Ownership: adaptertest.Borrowed, New: newOf(&owned{})}},
		}, func(c targetCase) string { return c.name }, func(ctx *specs.Context, c targetCase) {
			got := outcomes(ctx.T, adaptertest.Capture(ctx.T, c.target))
			ctx.Expect(got).To(everyResult(resultIs(adaptertest.Failed, "")))
		})
	})
}
