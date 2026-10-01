# Unit test migration list (#202)

Parent epic: #201. Related: #203 (go-specs pilot), #205 (unit migration).

## Scope change

PR #218 first delivered a five-lane contract, a generator and a committed inventory (tasks T1 to T5 of the first
version of this file, all done at `9a99274`). Two things changed the scope: the maintainer's comment on the PR, and the
updated bodies of #201, #202 and #205. The first phase is now to migrate every unit test to go-specs with mocks, stubs
or fakes, without introducing more classes or lanes. So the old T1 to T5 are superseded: the lane contract, the
generator and its artifacts were removed from the branch (history is kept, nothing was rewritten or force-pushed).
Tests that need real components or resources are documented as out of phase, with their execution unchanged. The
component pilot and #206 to #214 are deferred.

## What this change is

One document, `docs/testing/unit-migration.md`. It has a totals table per module, then one row per unit test (package,
test, status `pending` or `migrated`, dependencies to substitute), then the out-of-phase tests with the real
dependency of each. No test is moved, tagged or skipped and no workflow changes.

## Tasks

- [x] **T1 Remove the generator.** Delete `internal/tools/testinventory`, `docs/testing/lanes.md`, the inventory files
  and the overrides. Check: the branch diff against `develop` holds only the two documents; `go vet ./...`,
  `golangci-lint run ./...` and `go test ./...` still pass.
- [x] **T2 Write the list.** Generate `docs/testing/unit-migration.md` from the recorded data of the removed tool.
  Check: every `Test` function of `develop` appears once, in the unit rows or in the out-of-phase section, and the
  totals reconcile with `go test -list`.

## Evidence

Recorded at `develop` `0de4249`: 873 `Test` functions in 8 modules, of which 617 unit pending, 1 unit migrated
(`internal/engine/protocol.TestPreconditionFromRevisionMapsPerD4`) and 255 out of phase (194 start an actor system, 28
use Postgres, a cluster or the network, 22 run `go list` or parse sources, 11 test example programs).
Dependencies come from static signals plus the 65 tests reviewed by hand, and may need confirmation in each PR. The
verification results are in the pull request description.
