# Test inventory

This file is generated from [`inventory.json`](inventory.json) by `go run ./internal/tools/testinventory -update`. Do not edit it by hand. It applies the lane contract in [`lanes.md`](lanes.md) to every `Test` function of every module and lists the tests that must leave the pull request lane. The plan behind it is epic #201; this inventory is issue #202.

The recorded run used go1.26.6 on linux/amd64 with 16 CPUs at commit `ffc3c9a5e7af`. Timings are indicative, taken from one local run, and are not a CI baseline. The race detector was not enabled.

## Counts

Top-level `Test` functions per module and lane.

| Module | unit | component | integration | architecture | example | Total |
|---|---|---|---|---|---|---|
| `.` | 659 | 189 | 9 | 18 | 0 | 875 |
| `benchmark` | 0 | 0 | 0 | 0 | 0 | 0 |
| `example/cluster` | 0 | 0 | 19 | 0 | 11 | 30 |
| `publisher/kafka` | 2 | 0 | 0 | 1 | 0 | 3 |
| `publisher/nats` | 2 | 0 | 0 | 1 | 0 | 3 |
| `publisher/pulsar` | 2 | 0 | 0 | 1 | 0 | 3 |
| `publisher/websocket` | 2 | 5 | 0 | 1 | 0 | 8 |
| `test/compat` | 1 | 0 | 0 | 0 | 0 | 1 |
| **Total** | 668 | 194 | 28 | 22 | 11 | 923 |

Tests with a recorded run: 923 of 923 (pass 904, skip 19, fail 0). Subtests recorded: 1089.

862 tests stay in the pull request lane (unit and component) and 61 leave it.

## Tests that leave the pull request lane

Grouped by the issue that will run them again. Each line is a package directory followed by its tests.

### #208: architecture and subprocess tests (22 tests)

- `engine`: `TestCommandArchitecture`, `TestTenancyArchitecture`
- `internal/instrumentation`: `TestInstrumentationStaysRuntimeNeutral`
- `internal/logging`: `TestLoggingStaysRuntimeNeutral`
- `internal/projectionrunner`: `TestProjectionRunnerStaysRuntimeNeutral`
- `internal/runtimeconsumer`: `TestProductionClosureExcludesRootAndGoAkt`
- `migration`: `TestProductionClosureExcludesRootAndGoAkt`
- `port/adapter`: `TestAdapterDependsOnlyOnStdlib`, `TestContractPackagesDoNotImportAdapter`, `TestNoPrivateCopiesOfOptionalInterfaces`, `TestOptionalInterfacesAreAssertedOnlyInTheirAccessors`, `TestPortNameConstantsAreUntyped`
- `port/adapter/adaptertest`: `TestAdaptertestDependsOnlyOnStdlibAndAdapter`
- `port/behavior`: `TestBehaviorDependsOnlyOnContracts`
- `port/publishing`: `TestPublishingDependsOnlyOnContracts`
- `port/publishing/publishingtest`: `TestPublishingtestDependsOnlyOnStdlibPublishingAndEgopb`
- `port/runtime`: `TestRuntimeDependsOnlyOnContracts`, `TestRuntimeTestClosureExcludesGoAktAndRoot`
- `publisher/kafka`: `TestUnitTestClosureExcludesRuntimeAndRoot`
- `publisher/nats`: `TestUnitTestClosureExcludesRuntimeAndRoot`
- `publisher/pulsar`: `TestUnitTestClosureExcludesRuntimeAndRoot`
- `publisher/websocket`: `TestUnitTestClosureExcludesRuntimeAndRoot`

### #211: PostgreSQL (19 tests)

- `example/cluster`: `TestPostgresEventStore_ConcurrentExpectRevisionHasExactlyOneWinner`, `TestPostgresEventStore_Conformance`, `TestPostgresEventStore_DeleteEventsLocksRevisionAgainstConcurrentWrite`, `TestPostgresEventStore_DeleteKeepsRevisionPerTenant`, `TestPostgresEventStore_ExpectGenesisConflictsOnExistingID`, `TestPostgresEventStore_PartialDeleteKeepsRevision`, `TestPostgresEventStore_PersistenceIDs_ZeroPageSize_WithData`, `TestPostgresEventStore_SchemaMigratesLegacyDatabase`, `TestPostgresEventStore_SchemaMigratesLegacyTenantMetadata`, `TestPostgresEventStore_TenantMetadataAbsent_ReadsAsNone`, `TestPostgresEventStore_TenantMetadataRoundTrips_ConditionalWrite`, `TestPostgresEventStore_TenantMetadataRoundTrips_GetShardEvents`, `TestPostgresEventStore_TenantMetadataRoundTrips_UnconditionalWrite`, `TestPostgresEventStore_TenantScopeIsolatesRecords`, `TestPostgresEventStore_TotalDeleteKeepsRevision`, `TestPostgresEventStore_UnconditionalMixedBatchesDoNotDeadlock`, `TestPostgresEventStore_UnconditionalRaceDistinctSequenceConflict`, `TestPostgresEventStore_UnconditionalRaceSameSequenceConflict`, `TestPostgresEventStore_UnconditionalWriteCannotBreakExpectRevision`

### #212: cluster and real network (9 tests)

- `compose/goakt`: `TestApp_TwoNodeClusterPlacesAndStopsCleanly`
- `engine`: `TestEngineClusterMode`, `TestEngineClusterModeStartProjectionAlreadyExists`, `TestEngineMultiNodeNeutralBehaviors`, `TestEngineMultiNodeRemoteEntitySpawn`, `TestEngineRejectsUnplaceableBehaviorsInClusterMode`, `TestEngineRemoteSpawnTenantBinding`, `TestEventPublisherClusterHighPartitionCount`, `TestNewEngineRejectsValueTypeKindInClusterMode`

### #214: examples and benchmarks (11 tests)

- `example/cluster`: `TestPostgresEventStore_DeleteEvents_InvalidScope`, `TestPostgresEventStore_GetLatestEvent_InvalidScope`, `TestPostgresEventStore_ImplementsEventsStore`, `TestPostgresEventStore_PersistenceIDs_InvalidScope`, `TestPostgresEventStore_PersistenceIDs_ZeroPageSize`, `TestPostgresEventStore_ReplayEvents_InvalidScope`, `TestPostgresEventStore_WriteEvents_EmptyBatchConditional`, `TestPostgresEventStore_WriteEvents_EmptyBatchUnconditionalSucceeds`, `TestPostgresEventStore_WriteEvents_InvalidPrecondition`, `TestPostgresEventStore_WriteEvents_InvalidScope`, `TestPostgresEventStore_WriteEvents_MixedIDBatchConditional`

## Names versus behavior

A test's name says nothing about its resources, so no test is classified by name. These are the tests whose name suggests integration.

### Tests named like integration or end-to-end but not integration (10)

| Test | Lane | Evidence | Package |
|---|---|---|---|
| `TestIntegrationMetadataEnvelopeResultCarrier` | unit | none | `command` |
| `TestIntegrationRejectedResultCarriesReconstructedMetadata` | unit | none | `command` |
| `TestRuntime_ConsumerDrivesTheAppEndToEnd` | component | lifecycle.start | `compose/goakt` |
| `TestDurableStateExpectedRevisionEndToEndPropagation` | component | actor.system, lifecycle.start | `engine` |
| `TestEventSourcedIntegrationConcurrentGenesisYieldsExactlyOneCommit` | component | actor.system, lifecycle.start | `engine` |
| `TestEventSourcedIntegrationExactRevisionCommitsAndAdvancesStore` | component | actor.system, lifecycle.start | `engine` |
| `TestEventSourcedIntegrationStaleRevisionRejectedStoreUnchanged` | component | actor.system, lifecycle.start | `engine` |
| `TestTenantWritePathE2E` | component | actor.system, lifecycle.start | `engine` |
| `TestRenderMarkdownCallsOutInMemoryTestsNamedIntegration` | unit | none | `internal/tools/testinventory/inventory` |
| `TestTenantAdopterEndToEndRecoveryThroughRealActor` | component | actor.system, lifecycle.start | `migration` |

### Tests named like integration or end-to-end that really are integration (0)

None.

## Skips and failures

### EGO_EXAMPLE_POSTGRES_DSN not set, skipping Postgres-backed test (19)

- `example/cluster`: `TestPostgresEventStore_ConcurrentExpectRevisionHasExactlyOneWinner`, `TestPostgresEventStore_Conformance`, `TestPostgresEventStore_DeleteEventsLocksRevisionAgainstConcurrentWrite`, `TestPostgresEventStore_DeleteKeepsRevisionPerTenant`, `TestPostgresEventStore_ExpectGenesisConflictsOnExistingID`, `TestPostgresEventStore_PartialDeleteKeepsRevision`, `TestPostgresEventStore_PersistenceIDs_ZeroPageSize_WithData`, `TestPostgresEventStore_SchemaMigratesLegacyDatabase`, `TestPostgresEventStore_SchemaMigratesLegacyTenantMetadata`, `TestPostgresEventStore_TenantMetadataAbsent_ReadsAsNone`, `TestPostgresEventStore_TenantMetadataRoundTrips_ConditionalWrite`, `TestPostgresEventStore_TenantMetadataRoundTrips_GetShardEvents`, `TestPostgresEventStore_TenantMetadataRoundTrips_UnconditionalWrite`, `TestPostgresEventStore_TenantScopeIsolatesRecords`, `TestPostgresEventStore_TotalDeleteKeepsRevision`, `TestPostgresEventStore_UnconditionalMixedBatchesDoNotDeadlock`, `TestPostgresEventStore_UnconditionalRaceDistinctSequenceConflict`, `TestPostgresEventStore_UnconditionalRaceSameSequenceConflict`, `TestPostgresEventStore_UnconditionalWriteCannotBreakExpectRevision`

## Mixed files (27)

Files whose tests belong to more than one lane. Splitting them is left to the migration issues.

| File | Lanes |
|---|---|
| `compose/goakt/app_test.go` | unit 9, component 11 |
| `compose/goakt/runtime_test.go` | unit 1, component 2 |
| `engine/behavior_dependency_test.go` | unit 3, component 2, integration 1 |
| `engine/behavior_kind_test.go` | unit 2, component 3, integration 1 |
| `engine/engine_entity_family_test.go` | unit 2, component 5 |
| `engine/engine_tenant_respawn_test.go` | unit 1, component 4 |
| `engine/engine_test.go` | unit 10, component 46, integration 3 |
| `engine/logger_test.go` | unit 3, component 1 |
| `engine/option_test.go` | unit 23, component 6 |
| `engine/projection_actor_test.go` | unit 1, component 1 |
| `engine/publisher_test.go` | unit 2, component 5, integration 1 |
| `internal/engine/durablestate/durable_state_actor_tenant_persist_test.go` | unit 2, component 5 |
| `internal/engine/durablestate/durable_state_actor_test.go` | unit 1, component 4 |
| `internal/engine/eventsource/event_sourced_actor_tenant_persist_test.go` | unit 5, component 4 |
| `internal/engine/eventsource/event_sourced_actor_test.go` | unit 1, component 9 |
| `internal/engine/saga/saga_actor_tenant_test.go` | unit 5, component 3 |
| `internal/engine/saga/saga_test.go` | unit 1, component 2 |
| `internal/instrumentation/instrumentation_test.go` | unit 8, architecture 1 |
| `internal/logging/logging_test.go` | unit 2, architecture 1 |
| `internal/projectionrunner/runner_test.go` | unit 7, architecture 1 |
| `migration/tenant_adoption_test.go` | unit 44, component 2 |
| `port/adapter/assertion_sites_test.go` | unit 1, architecture 2 |
| `publisher/kafka/closure_test.go` | unit 1, architecture 1 |
| `publisher/nats/closure_test.go` | unit 1, architecture 1 |
| `publisher/pulsar/closure_test.go` | unit 1, architecture 1 |
| `publisher/websocket/closure_test.go` | unit 1, architecture 1 |
| `publisher/websocket/conformance_test.go` | unit 1, component 4 |

## Review queue (0)

Unit tests that call `Start` or `Spawn` on some value. Static analysis cannot tell a fake from a real actor system, so each one needs a person to confirm its lane in `inventory-overrides.json`.

None.

## Other lists

### Local sockets and `httptest` (5)

Classified by the loopback rule in `lanes.md`, so they stay out of `unit` but are not integration unless another signal says so.

- `publisher/websocket`: `TestCloseIsIdempotent`, `TestDurableStatePublisherAdapterConformance`, `TestDurableStatePublisherPublishingConformance`, `TestEventsPublisherAdapterConformance`, `TestEventsPublisherPublishingConformance`

### Reclassified by override (54)

- `TestApp_ValidSpecRunsAnEngine` in `compose/goakt` is component: App.Start runs the real start steps, including a real single-node GoAkt actor system and the engine; the failure or the stop being tested happens after they are up. The scanner does not resolve methods such as App.Start.
- `TestEngine_UndeclaredFamilyReturnsTypedError` in `compose/goakt` is component: App.Start runs the real start steps, including a real single-node GoAkt actor system and the engine; the failure or the stop being tested happens after they are up. The scanner does not resolve methods such as App.Start.
- `TestNew_G1_ClusterRequiresEntityKinds` in `compose/goakt` is unit: Passes a cluster configuration to New only to validate it. New starts nothing and opens no socket, so the cluster signal is a false positive.
- `TestNew_ReportsEveryProblem` in `compose/goakt` is unit: Passes a cluster configuration to New only to validate it. New starts nothing and opens no socket, so the cluster signal is a false positive.
- `TestRuntime_ConsumerDrivesTheAppEndToEnd` in `compose/goakt` is component: App.Start runs the real start steps, including a real single-node GoAkt actor system and the engine; the failure or the stop being tested happens after they are up. The scanner does not resolve methods such as App.Start.
- `TestRuntime_IsTheEngineAfterStartAndAfterStop` in `compose/goakt` is component: App.Start runs the real start steps, including a real single-node GoAkt actor system and the engine; the failure or the stop being tested happens after they are up. The scanner does not resolve methods such as App.Start.
- `TestRuntime_NilAfterFailedStart` in `compose/goakt` is component: App.Start runs the real start steps, including a real single-node GoAkt actor system and the engine; the failure or the stop being tested happens after they are up. The scanner does not resolve methods such as App.Start.
- `TestStart_ActorSystemStepFailsForReal` in `compose/goakt` is component: Starts a real actor system whose cluster configuration GoAkt rejects inside step 2 (no discovery provider, no remoting), so no cluster socket is opened. The cluster signal only comes from building the configuration.
- `TestStart_AttachStepStartsAndProbesPublishersFirst` in `compose/goakt` is component: App.Start runs the real start steps, including a real single-node GoAkt actor system and the engine; the failure or the stop being tested happens after they are up. The scanner does not resolve methods such as App.Start.
- `TestStart_CancelledContextStartsNothing` in `compose/goakt` is unit: Start fails at its first step (a cancelled context or a failing store probe) before any actor system exists; the stores are in-memory fakes. Confirmed after review of the Start call.
- `TestStart_FailureAtEachStepReleasesEverything` in `compose/goakt` is component: App.Start runs the real start steps, including a real single-node GoAkt actor system and the engine; the failure or the stop being tested happens after they are up. The scanner does not resolve methods such as App.Start.
- `TestStart_ProbeFailureNamesTheStore` in `compose/goakt` is unit: Start fails at its first step (a cancelled context or a failing store probe) before any actor system exists; the stores are in-memory fakes. Confirmed after review of the Start call.
- `TestStart_PublisherFailureAtK` in `compose/goakt` is component: App.Start runs the real start steps, including a real single-node GoAkt actor system and the engine; the failure or the stop being tested happens after they are up. The scanner does not resolve methods such as App.Start.
- `TestStart_PublisherPingFailureNamesTheAdapter` in `compose/goakt` is component: App.Start runs the real start steps, including a real single-node GoAkt actor system and the engine; the failure or the stop being tested happens after they are up. The scanner does not resolve methods such as App.Start.
- `TestStop_AfterStopIsNoOp` in `compose/goakt` is component: App.Start runs the real start steps, including a real single-node GoAkt actor system and the engine; the failure or the stop being tested happens after they are up. The scanner does not resolve methods such as App.Start.
- `TestStop_D7OpenQuestion_StateFlushedDuringActorShutdown` in `compose/goakt` is component: App.Start runs the real start steps, including a real single-node GoAkt actor system and the engine; the failure or the stop being tested happens after they are up. The scanner does not resolve methods such as App.Start.
- `TestStop_OrderMatchesD7` in `compose/goakt` is component: App.Start runs the real start steps, including a real single-node GoAkt actor system and the engine; the failure or the stop being tested happens after they are up. The scanner does not resolve methods such as App.Start.
- `TestCleanup_RunsUnderWithoutCancelAndTimeout` in `compose/internal/lifecycle` is unit: Start and Stop belong to an in-memory step runner that is only given function values; it starts no actor system and does no I/O. Confirmed after review.
- `TestStartAndStop_AreSerialized` in `compose/internal/lifecycle` is unit: Start and Stop belong to an in-memory step runner that is only given function values; it starts no actor system and does no I/O. Confirmed after review.
- `TestStart_ChecksContextBeforeEachStep` in `compose/internal/lifecycle` is unit: Start and Stop belong to an in-memory step runner that is only given function values; it starts no actor system and does no I/O. Confirmed after review.
- `TestStart_FailureAtEachStepRollsBackInReverseThenReleases` in `compose/internal/lifecycle` is unit: Start and Stop belong to an in-memory step runner that is only given function values; it starts no actor system and does no I/O. Confirmed after review.
- `TestStart_IsSingleUse` in `compose/internal/lifecycle` is unit: Start and Stop belong to an in-memory step runner that is only given function values; it starts no actor system and does no I/O. Confirmed after review.
- `TestStart_PanickingStepRollsBackReleasesAndFails` in `compose/internal/lifecycle` is unit: Start and Stop belong to an in-memory step runner that is only given function values; it starts no actor system and does no I/O. Confirmed after review.
- `TestStart_RollbackAttemptsEveryUndoAndReportsEveryError` in `compose/internal/lifecycle` is unit: Start and Stop belong to an in-memory step runner that is only given function values; it starts no actor system and does no I/O. Confirmed after review.
- `TestStart_RunsStepsInOrder` in `compose/internal/lifecycle` is unit: Start and Stop belong to an in-memory step runner that is only given function values; it starts no actor system and does no I/O. Confirmed after review.
- `TestStart_StepsWithoutStopAreSkippedOnRollback` in `compose/internal/lifecycle` is unit: Start and Stop belong to an in-memory step runner that is only given function values; it starts no actor system and does no I/O. Confirmed after review.
- `TestState_TransitionsAreVisibleInsideSteps` in `compose/internal/lifecycle` is unit: Start and Stop belong to an in-memory step runner that is only given function values; it starts no actor system and does no I/O. Confirmed after review.
- `TestStop_AfterFailedStartIsNoOp` in `compose/internal/lifecycle` is unit: Start and Stop belong to an in-memory step runner that is only given function values; it starts no actor system and does no I/O. Confirmed after review.
- `TestStop_FailureAtEachStepStillRunsTheRest` in `compose/internal/lifecycle` is unit: Start and Stop belong to an in-memory step runner that is only given function values; it starts no actor system and does no I/O. Confirmed after review.
- `TestStop_IsIdempotent` in `compose/internal/lifecycle` is unit: Start and Stop belong to an in-memory step runner that is only given function values; it starts no actor system and does no I/O. Confirmed after review.
- `TestStop_JoinsEveryError` in `compose/internal/lifecycle` is unit: Start and Stop belong to an in-memory step runner that is only given function values; it starts no actor system and does no I/O. Confirmed after review.
- `TestStop_UndoesEveryStepInReverseOrder` in `compose/internal/lifecycle` is unit: Start and Stop belong to an in-memory step runner that is only given function values; it starts no actor system and does no I/O. Confirmed after review.
- `TestBatchAdmissionGateRejectsStaleRevision_ForcesEarlyFlushThenFoundsFreshBatch` in `engine` is component: The engine comes from a wrapper (newBatchHarness, newRespawnTestEngine or familyTestEngine) around newTestEngine, which creates a real single-node GoAkt actor system. That is two helper levels deep and the scanner follows one.
- `TestBatchedExternalWriterWinsCAS_RejectsWholeBatchWithoutAdvancingCounter` in `engine` is component: The engine comes from a wrapper (newBatchHarness, newRespawnTestEngine or familyTestEngine) around newTestEngine, which creates a real single-node GoAkt actor system. That is two helper levels deep and the scanner follows one.
- `TestBatchedPhysicalBaseAnchorsToPreBatchRevision_NotLogicalCounter` in `engine` is component: The engine comes from a wrapper (newBatchHarness, newRespawnTestEngine or familyTestEngine) around newTestEngine, which creates a real single-node GoAkt actor system. That is two helper levels deep and the scanner follows one.
- `TestBatchedZeroEventAdmittedCommandStillPreservesLaterPrecondition` in `engine` is component: The engine comes from a wrapper (newBatchHarness, newRespawnTestEngine or familyTestEngine) around newTestEngine, which creates a real single-node GoAkt actor system. That is two helper levels deep and the scanner follows one.
- `TestBatchedZeroEventFounderNeverOpensBatch` in `engine` is component: The engine comes from a wrapper (newBatchHarness, newRespawnTestEngine or familyTestEngine) around newTestEngine, which creates a real single-node GoAkt actor system. That is two helper levels deep and the scanner follows one.
- `TestDispatchRejectsTenantBindingQuery` in `engine` is component: The engine comes from a wrapper (newBatchHarness, newRespawnTestEngine or familyTestEngine) around newTestEngine, which creates a real single-node GoAkt actor system. That is two helper levels deep and the scanner follows one.
- `TestEngineConcurrentCrossTenantSpawnHasExactlyOneWinner` in `engine` is component: The engine comes from a wrapper (newBatchHarness, newRespawnTestEngine or familyTestEngine) around newTestEngine, which creates a real single-node GoAkt actor system. That is two helper levels deep and the scanner follows one.
- `TestEngineRespawnUnderAnotherTenantIsRejected` in `engine` is component: The engine comes from a wrapper (newBatchHarness, newRespawnTestEngine or familyTestEngine) around newTestEngine, which creates a real single-node GoAkt actor system. That is two helper levels deep and the scanner follows one.
- `TestEngineStartWithoutActorSystem` in `engine` is unit: Builds a bare Engine struct with no actor system and expects ErrActorSystemRequired from Start. Confirmed after review.
- `TestWithEntityFamilies_Combined` in `engine` is component: The engine comes from a wrapper (newBatchHarness, newRespawnTestEngine or familyTestEngine) around newTestEngine, which creates a real single-node GoAkt actor system. That is two helper levels deep and the scanner follows one.
- `TestWithEntityFamilies_NotDeclaredAllowsEveryFamily` in `engine` is component: The engine comes from a wrapper (newBatchHarness, newRespawnTestEngine or familyTestEngine) around newTestEngine, which creates a real single-node GoAkt actor system. That is two helper levels deep and the scanner follows one.
- `TestWithEntityFamilies_UndeclaredFamilyIsRejected` in `engine` is component: The engine comes from a wrapper (newBatchHarness, newRespawnTestEngine or familyTestEngine) around newTestEngine, which creates a real single-node GoAkt actor system. That is two helper levels deep and the scanner follows one.
- `TestWithEntityFamilies_UnknownBitsAreIgnored` in `engine` is component: The engine comes from a wrapper (newBatchHarness, newRespawnTestEngine or familyTestEngine) around newTestEngine, which creates a real single-node GoAkt actor system. That is two helper levels deep and the scanner follows one.
- `TestStartCommandSpan` in `internal/instrumentation` is unit: Start begins a span on an in-memory recording tracer. Confirmed after review.
- `TestProjectionRunnerErrorPaths` in `internal/projectionrunner` is unit: Runner.Start drives the projection loop over in-memory fakes. The only real waits are pause.For timers, which issue #207 owns. Confirmed after review.
- `TestProjectionRunnerFatalPaths` in `internal/projectionrunner` is unit: Runner.Start drives the projection loop over in-memory fakes. The only real waits are pause.For timers, which issue #207 owns. Confirmed after review.
- `TestProjectionRunnerLagMetrics` in `internal/projectionrunner` is unit: Runner.Start drives the projection loop over in-memory fakes. The only real waits are pause.For timers, which issue #207 owns. Confirmed after review.
- `TestRunner` in `internal/projectionrunner` is unit: Runner.Start drives the projection loop over in-memory fakes. The only real waits are pause.For timers, which issue #207 owns. Confirmed after review.
- `TestRunnerPullEfficiency` in `internal/projectionrunner` is unit: Runner.Start drives the projection loop over in-memory fakes. The only real waits are pause.For timers, which issue #207 owns. Confirmed after review.
- `TestTicker` in `internal/ticker` is unit: Ticker.Start starts a timer and nothing else. It waits about half a second of real time. Confirmed after review.
- `TestAccessors_ImplementingValueIsReturned` in `port/adapter` is unit: Start is the adapter.Starter method of an in-memory fake. Confirmed after review.
- `TestAssertionSitesNegativeControl` in `port/adapter` is unit: Parses an in-memory source string with go/parser as a negative control and reads no repository file, so it is not an architecture check.

### Fixed waits

49 tests contain constant `time.Sleep` or `pause.For` waits that add up to 272.4 seconds (once per occurrence, a lower bound). Issue #207 owns them.

## Slowest packages

Elapsed time per package in the recorded run. `inventory.json` has all of them.

| Package | Seconds | Status |
|---|---|---|
| `github.com/getsyntegrity/ego/internal/engine/eventsource` | 225.4 | pass |
| `github.com/getsyntegrity/ego/engine` | 66.4 | pass |
| `github.com/getsyntegrity/ego/internal/projectionrunner` | 41.3 | pass |
| `github.com/getsyntegrity/ego/internal/engine/durablestate` | 22.2 | pass |
| `github.com/getsyntegrity/ego/internal/engine/projection` | 19.1 | pass |
| `github.com/getsyntegrity/ego/internal/engine/saga` | 13.0 | pass |
| `github.com/getsyntegrity/ego/eventstream` | 2.1 | pass |
| `github.com/getsyntegrity/ego/port/adapter/adaptertest` | 1.2 | pass |
| `github.com/getsyntegrity/ego/compose/goakt` | 0.8 | pass |
| `github.com/getsyntegrity/ego/internal/ticker` | 0.5 | pass |
| `github.com/getsyntegrity/ego/internal/tools/testinventory/inventory` | 0.2 | pass |
| `github.com/getsyntegrity/ego/port/adapter` | 0.2 | pass |
| `github.com/getsyntegrity/ego/migration` | 0.2 | pass |
| `github.com/getsyntegrity/ego/publisher/websocket` | 0.1 | pass |
| `github.com/getsyntegrity/ego/publisher/pulsar` | 0.1 | pass |

## Limits

Signals come from each `Test` function and the local helpers it calls, one level deep. Subtests exist only in the recorded run. What static analysis cannot see is corrected by [`inventory-overrides.json`](inventory-overrides.json). See `lanes.md` for the full list.

The run reported 1 tests that the static scan did not find: github.com/getsyntegrity/ego/persistence.FuzzParseConflictErrorRoundTrip.
