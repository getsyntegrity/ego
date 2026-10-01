# Keep publish order in the in-process event stream (#241 follow-up)

## Problem

`engine.TestEventPublisherReceivesEventsFromEntity` failed now and then in CI: a registered event publisher
received sequence 2 before sequence 1 of the same entity (about 1 run in 300-400). The PR #241 writer called it
a goakt event stream reordering. It is not in goakt. Ego does not use goakt's event stream at all: the engine
builds its stream with `eventstream.New()` from ego's own package (`engine/option.go:103`), and nothing in the
repository imports goakt's `eventstream`.

## Where the order is lost

Per-entity order is guaranteed up to the stream. An entity sends its next command only after the previous write
returned, and the events writer actor publishes before it replies (`internal/engine/eventsource/events_writer_actor.go:121-125`).
So `Publish(seq 1)` has returned before `Publish(seq 2)` starts.

The loss happens inside `Publish`. `eventstream/stream.go` (`publishToTopic`, previously line 155) ran
`go sub.signal(message)` for every subscriber. Each message got its own goroutine, and nothing orders two such
goroutines: the one for sequence 2 can enqueue on the subscriber queue before the one for sequence 1. The
publisher goroutine in `engine/streams.go:213` (`sendEvent`) then drains the queue in that swapped order. This
matches the failing log from #241: `WRITER-PUB 1, WRITER-PUB 2, SEND-EVT 2, SEND-EVT 1`.

The earlier guess that the queue reuses nodes through a `sync.Pool` is not the cause: the new test fails
without touching the queue, and the fix does not touch it.

## What changes

`publishToTopic` calls `sub.signal(message)` directly instead of in a goroutine. `signal` only enqueues on a
lock-free queue and does a non-blocking wake-up, so it cannot block the publisher. Consecutive `Publish` calls
from one producer are now enqueued in call order. The public API does not change.

## What does not change, and why

- **goakt and its version.** It is not involved, so no bump is needed.
- **A re-sequencing step in the engine consumer.** It would hide the problem in one consumer while the
  projection runner and saga actors read the same stream, and it would add state per persistence id.
- **Cross-producer order.** Messages from different goroutines still have no defined order; ego only promises
  order per entity.
- `engine/publisher_test.go`: PR #241 rewrites it.

## Constraints

Strict TDD (runner: `go test ./eventstream`). Unit tests use no real time and no actor system. No `-race`, no
workbench. Release note: the ordering fix.

## Tasks

- [x] T1 Reproduce the reorder deterministically. Route: inline (one new file). Evidence: commit `b28a888`,
      `eventstream/stream_order_test.go`. It pins the scheduler to one processor (`GOMAXPROCS(1)`), where the
      newest goroutine runs first, publishes 1..50 and drains. RED: `[0]: expected 1, actual 50`,
      `[1]: expected 2, actual 1`.
- [x] T2 Deliver synchronously. Route: inline (one function). Evidence: commit `9918056`. GREEN: the test
      passes with `-count=5`; `eventstream` coverage stays 100.0%.
- [x] T3 Verify and deliver. Route: inline. Evidence: see the PR body (vet, lint, gofmt, dependants).

## Follow-up

None required. If the engine test still flakes after this merges, the cause is elsewhere, and the next
step is the entity actor's reply path, not the stream.

## Progress

All three tasks are done; the change is one function and one test file.
