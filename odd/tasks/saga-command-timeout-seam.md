# saga-command-timeout-seam

## Problem

In `internal/engine/saga/saga_actor.go` a saga command with `Timeout == 0` gets a 5 second default before
`SendSync`. The spec case "sendCommand: default timeout when zero" never proved that value: its target does
not exist, so `SendSync` fails at once, and the case only showed that a zero timeout still dispatches.
Origin: follow-up of #259 (`saga-actor-go-specs`).

## What changes

`saga_actor.go` gets a small unexported helper, `effectiveCommandTimeout`, used by both `sendCommand` and
`compensate` (they had the same duplicated default). The helper also reports the timeout to an unexported
test seam, `commandTimeoutObserver` (an `atomic.Pointer` to a func, nil in production). The test installs an
observer and asserts the value `SendSync` receives, with no waiting. Every row of the table now also asserts
its effective timeout, so the default is checked against explicit values.

## What does not change, and why

No exported identifier changes, so the `api` job (apidiff) stays clean. Behaviour is identical: still 5 s.
I rejected a field on `Actor`: actors are built by the actor system, so in-package tests cannot set one
easily; a package variable is simpler. The `compensate` default and the manual-clock idea from the parent
document are not done here (see follow-up).

## Constraints

go-specs only (no testify), no `-race`, no workbench, no DB or network.

## Tasks

- [x] T1 Add the seam and assert the default precisely. Route: direct inline (2 files, small).
  RED: default changed to 4 s -> `expected 4s to equal 5s` in "sendCommand: default timeout when zero";
  reverted, GREEN. Commit: see git log (`test(saga): prove the 5s default command timeout via a seam`).

## Follow-up

`saga-compensate-timeout-observation`: a test that drives `compensate` with a zero-timeout command and
asserts the 5 s through the same seam; and manual-clock observation windows.

## Progress

- 2026-09-30: T1 done. build, vet, `golangci-lint` (0 issues), `gofmt -l` clean; `go test -count=5` passes;
  coverage 89.3% before, 89.6% after.
