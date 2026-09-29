# Unit tests: migration to go-specs

First phase of epic #201: move every unit test to go-specs, with mocks, stubs or fakes injected in place of real dependencies. This list has one row per unit test, with its status and what it still depends on. Tests that need a real component or resource are listed separately as out of phase and keep running exactly as they do today. Nothing is moved, tagged or skipped and no workflow changes.

## How to read it

A test is a unit test when it needs no real component or resource: no database, broker, socket, subprocess, actor system or cluster. Its status is `migrated` when it already uses go-specs (today only `TestPreconditionFromRevisionMapsPerD4`, from #215) and `pending` otherwise. The migration unit is the test, so unit tests that share a file with actor tests are in the unit list.

The list is recorded at `develop` `0de4249`: 873 `Test` functions in the 8 modules, of which 617 are unit and 256 out of phase. Subtests are not listed; they travel with their parent test.

The dependencies column comes from static signals found in each test body and the local helpers it calls, plus a manual review of 65 ambiguous tests. It is a starting point and may need confirmation in each migration PR. `none` means no signal was found. The labels mean:

- `fixed wait`: `pause.For` or `time.Sleep`. Replace with a controllable clock or a fake, or wait on an observable condition.
- `real timer`: a real ticker. Replace with a fake ticker.
- `temp file`: writes or reads a file. Replace with an in-memory writer or reader.
- `env-gated skip`: behavior depends on an environment variable. Inject the value.
- `local socket`: opens a loopback listener or HTTP test server. Replace with an in-memory transport.
- `testify mock`: the file uses `testify/mock`. Keep it or map it to a fake.
- `generated mocks`: the file uses the generated `mocks/*` packages. Keep them or map them to fakes.
- Stores from `testkit` are in-memory fakes already and are not listed.
- The mock labels are file-level: they mean the test file imports the package, not that every test in it uses a mock.

## Totals

| Module | Unit pending | Unit migrated | Out of phase | Total |
|---|---|---|---|---|
| `.` | 607 | 1 | 217 | 825 |
| `benchmark` | 0 | 0 | 0 | 0 |
| `example/cluster` | 0 | 0 | 30 | 30 |
| `publisher/kafka` | 2 | 0 | 1 | 3 |
| `publisher/nats` | 2 | 0 | 1 | 3 |
| `publisher/pulsar` | 2 | 0 | 1 | 3 |
| `publisher/websocket` | 2 | 0 | 6 | 8 |
| `test/compat` | 1 | 0 | 0 | 1 |
| **Total** | 616 | 1 | 256 | 873 |

## Unit tests

### Module `.`

#### `command`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestCarrierDelegatesTenantSerializationToTenancyPackage` | pending | none |
| `TestCarrierRoundTripExpectedRevisionAbsentStaysAbsent` | pending | none |
| `TestCarrierRoundTripExpectedRevisionMaxUint64` | pending | none |
| `TestCarrierRoundTripExpectedRevisionPresent` | pending | none |
| `TestCarrierRoundTripOptionalFieldsAbsent` | pending | none |
| `TestCarrierRoundTripOptionalFieldsPresent` | pending | none |
| `TestCarrierRoundTripPreservesIdentity` | pending | none |
| `TestEnvelopeDeriveDelegatesToMetadataDerive` | pending | none |
| `TestEnvelopeDeriveRejectsNilPayload` | pending | none |
| `TestEnvelopeExpectedRevisionAbsentSurvivesCarrierRoundTrip` | pending | none |
| `TestEnvelopeWithoutExpectedRevisionUnaffectedByNewField` | pending | none |
| `TestErrorAs` | pending | none |
| `TestErrorClassification` | pending | none |
| `TestErrorMessage` | pending | none |
| `TestErrorUnwrap` | pending | none |
| `TestErrorUnwrapNilCause` | pending | none |
| `TestFailureWithCode` | pending | none |
| `TestGenerateOperationID` | pending | none |
| `TestGenerateOperationIDUniqueness` | pending | none |
| `TestIdentityDefinedTypesAreDistinct` | pending | none |
| `TestIntegrationMetadataEnvelopeResultCarrier` | pending | none |
| `TestIntegrationRejectedResultCarriesReconstructedMetadata` | pending | none |
| `TestMarshalMetadataUsesCanonicalKeys` | pending | none |
| `TestMetadataDeriveCustomNotInherited` | pending | none |
| `TestMetadataDeriveDeadlineMayOnlyShorten` | pending | none |
| `TestMetadataDeriveDoesNotInheritExpectedRevision` | pending | none |
| `TestMetadataDeriveInheritsCorrelationAndChainsCausation` | pending | none |
| `TestMetadataDerivePrincipalInheritedUnlessOverridden` | pending | none |
| `TestMetadataDeriveRejectsSameOperationID` | pending | none |
| `TestMetadataDeriveTenantInheritedWhenUnspecified` | pending | none |
| `TestMetadataDeriveTenantMustNotChange` | pending | none |
| `TestMetadataElapsedDeadlineIsRecognized` | pending | none |
| `TestNewCanceledDefaultCause` | pending | none |
| `TestNewEnvelope` | pending | none |
| `TestNewEnvelopeRejectsNilPayload` | pending | none |
| `TestNewFailed` | pending | none |
| `TestNewFailureRequiresMessage` | pending | none |
| `TestNewMetadataCustomDefensiveCopy` | pending | none |
| `TestNewMetadataCustomValue` | pending | none |
| `TestNewMetadataRoot` | pending | none |
| `TestNewMetadataWithCorrelationID` | pending | none |
| `TestNewMetadataWithCustomAcceptsValidKeyValue` | pending | none |
| `TestNewMetadataWithCustomRejectsCanonicalKey` | pending | none |
| `TestNewMetadataWithCustomRejectsInvalidValue` | pending | none |
| `TestNewMetadataWithCustomRejectsReservedPrefix` | pending | none |
| `TestNewMetadataWithDeadline` | pending | none |
| `TestNewMetadataWithExpectedRevisionPositive` | pending | none |
| `TestNewMetadataWithExpectedRevisionZeroIsGenesisNotAbsence` | pending | none |
| `TestNewMetadataWithPrincipal` | pending | none |
| `TestNewMetadataWithTenant` | pending | none |
| `TestNewMetadataWithTimestampDefault` | pending | none |
| `TestNewMetadataWithTimestampOverride` | pending | none |
| `TestNewMetadataWithoutDeadline` | pending | none |
| `TestNewMetadataWithoutExpectedRevision` | pending | none |
| `TestNewMetadataWithoutTenant` | pending | none |
| `TestNewOperationID` | pending | none |
| `TestNewPrincipal` | pending | none |
| `TestNewRejected` | pending | none |
| `TestNewRejectedConcurrencyConflictCodeCheckableWithoutStringInspection` | pending | none |
| `TestNewSuccessNoState` | pending | none |
| `TestNewSuccessRequiresState` | pending | none |
| `TestNewSuccessWithState` | pending | none |
| `TestNewTimedOutDefaultCause` | pending | none |
| `TestOutcomeKindsMutuallyExclusive` | pending | none |
| `TestOutcomeStringPerKind` | pending | none |
| `TestOutcomeZeroValueInvalid` | pending | none |
| `TestPayloadAsTypedExtraction` | pending | none |
| `TestPrincipalIsAbstract` | pending | none |
| `TestResultErrAsCommandError` | pending | none |
| `TestStateAsFalseWhenNoState` | pending | none |
| `TestStateAsTypedExtraction` | pending | none |
| `TestUnmarshalMetadataIgnoresUnknownEgoCmdKey` | pending | none |
| `TestUnmarshalMetadataRejectsExpectedRevisionOverflow` | pending | none |
| `TestUnmarshalMetadataRejectsInvalidCustomValue` | pending | none |
| `TestUnmarshalMetadataRejectsMalformedExpectedRevision` | pending | none |
| `TestUnmarshalMetadataRejectsMissingOperationID` | pending | none |
| `TestUnmarshalMetadataRejectsNegativeExpectedRevision` | pending | none |
| `TestUnmarshalMetadataRejectsReservedBareKey` | pending | none |
| `TestUnmarshalMetadataRejectsUnrecognizedEgoNamespace` | pending | none |
| `TestValidationSentinelsAreDistinct` | pending | none |

#### `compose`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestSpecValidate_MinimalSpecsPass` | pending | none |
| `TestSpecValidate_ReportsEveryProblem` | pending | none |
| `TestSpecValidate_V1_ZeroFamilies` | pending | none |
| `TestSpecValidate_V2_EventsStoreRequired` | pending | none |
| `TestSpecValidate_V2_NotRequiredForDurableStateOnly` | pending | none |
| `TestSpecValidate_V3_NotRequiredWithoutDurableState` | pending | none |
| `TestSpecValidate_V3_StateStoreRequired` | pending | none |
| `TestSpecValidate_V4_Projections` | pending | none |
| `TestSpecValidate_V5_NilEventAdapterElement` | pending | none |
| `TestSpecValidate_V5_TypedNilPerInterfaceField` | pending | none |
| `TestSpecValidate_V6_DuplicatePublisherIDsPerKind` | pending | none |
| `TestSpecValidate_V6_NilPublisher` | pending | none |
| `TestSpecValidate_V7_NegativeShutdownTimeout` | pending | none |
| `TestSpecValidate_V8_ReportsInFieldOrder` | pending | none |
| `TestSpecValidate_V8_SkipsValuesV5AndV6Rejected` | pending | none |
| `TestSpecValidate_V8_TruthfulDeclarationsPass` | pending | none |
| `TestSpecValidate_V8_UndeclaredAdaptersAreNotInspected` | pending | none |
| `TestSpecValidate_V8a_SlotPortMustBeDeclared` | pending | none |
| `TestSpecValidate_V8b_DeclarationMatchesMethods` | pending | none |
| `TestSpecValidate_V8b_DeclarationOnlyCapabilityIsOneDirectional` | pending | none |
| `TestSpecValidate_V8b_ImpliedCapabilitiesAreSkipped` | pending | none |
| `TestSpecValidate_V8b_UnknownCapabilityIsAccepted` | pending | none |
| `TestSpecValidate_V8c_RequiredCapabilities` | pending | none |
| `TestSpecValidate_ValidSpecPasses` | pending | none |
| `TestStartError` | pending | none |

#### `compose/goakt`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestNew_G1_ClusterRequiresEntityKinds` | pending | none |
| `TestNew_MissingRequiredDependencyFailsWithNothingStarted` | pending | none |
| `TestNew_NegativeShutdownTimeoutFailsAtNew` | pending | none |
| `TestNew_ReportsEveryProblem` | pending | none |
| `TestNew_StartsNothing` | pending | none |
| `TestNew_V8RejectsALyingPublisherWithNothingStarted` | pending | none |
| `TestRuntime_NilBeforeStart` | pending | none |
| `TestStart_CancelledContextStartsNothing` | pending | none |
| `TestStart_ProbeFailureNamesTheStore` | pending | none |
| `TestStop_NeverStartedClosesPublishers` | pending | none |

#### `compose/internal/adapters`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestStartAndProbe_Empty` | pending | none |
| `TestStartAndProbe_SkipsTypedNil` | pending | none |
| `TestStartAndProbe_StartsThenPingsEachInOrder` | pending | none |
| `TestStartAndProbe_StopsAtFirstFailure` | pending | none |

#### `compose/internal/lifecycle`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestCleanup_RunsUnderWithoutCancelAndTimeout` | pending | none |
| `TestNew_RejectsIncompleteSteps` | pending | none |
| `TestNew_RejectsNegativeShutdownTimeout` | pending | none |
| `TestStartAndStop_AreSerialized` | pending | none |
| `TestStart_ChecksContextBeforeEachStep` | pending | none |
| `TestStart_FailureAtEachStepRollsBackInReverseThenReleases` | pending | none |
| `TestStart_IsSingleUse` | pending | none |
| `TestStart_PanickingStepRollsBackReleasesAndFails` | pending | none |
| `TestStart_RollbackAttemptsEveryUndoAndReportsEveryError` | pending | none |
| `TestStart_RunsStepsInOrder` | pending | none |
| `TestStart_StepsWithoutStopAreSkippedOnRollback` | pending | none |
| `TestState_TransitionsAreVisibleInsideSteps` | pending | none |
| `TestStop_AfterFailedStartIsNoOp` | pending | none |
| `TestStop_FailureAtEachStepStillRunsTheRest` | pending | none |
| `TestStop_IsIdempotent` | pending | none |
| `TestStop_JoinsEveryError` | pending | none |
| `TestStop_NeverStartedOnlyReleases` | pending | none |
| `TestStop_UndoesEveryStepInReverseOrder` | pending | none |

#### `egopb`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestDescriptor_IsSoundAndCarriesTheModulePath` | pending | none |

#### `encryption`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestAESEncryptor_DecryptShortCiphertext` | pending | none |
| `TestAESEncryptor_DecryptWithWrongKeyID` | pending | none |
| `TestAESEncryptor_EncryptDecryptRoundTrip` | pending | none |
| `TestAESEncryptor_EncryptProducesDifferentCiphertext` | pending | none |

#### `engine`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestBehaviorFrom` | pending | none |
| `TestBehaviorKindAssignability` | pending | none |
| `TestBehaviorPlacementError` | pending | none |
| `TestBuildSpawnOptionsFromConfig` | pending | testify mock, generated mocks |
| `TestClassifyTenantBinding` | pending | none |
| `TestClusterKindsExposesEgoActors` | pending | none |
| `TestDefaultLoggerIsKitLoggerGlobal` | pending | none |
| `TestDiscardLoggerDisablesEveryLevel` | pending | none |
| `TestEgoSpawnOptionsResolveThroughRuntime` | pending | none |
| `TestEngineEraseEntityStoreErrors` | pending | testify mock, generated mocks |
| `TestEngineHotPathGuards` | pending | testify mock, generated mocks |
| `TestEngineProjectionLagComputation` | pending | testify mock, generated mocks |
| `TestEngineProjectionLagStoreErrors` | pending | testify mock, generated mocks |
| `TestEngineStartWithoutActorSystem` | pending | testify mock, generated mocks |
| `TestEntityFamily_String` | pending | none |
| `TestErrBehaviorNotPointerMessage` | pending | none |
| `TestMissingRequiredExtensionsSentinel` | pending | none |
| `TestNewSpawnConfigRoundTrip` | pending | none |
| `TestNewSpawnConfigSkipsNilOption` | pending | none |
| `TestOptionWithEncryptor` | pending | none |
| `TestOptionWithEventAdapters` | pending | none |
| `TestOptionWithEventAdaptersMultiple` | pending | none |
| `TestOptionWithLogger` | pending | none |
| `TestOptionWithLoggerNilFallback` | pending | none |
| `TestOptionWithOffsetStore` | pending | none |
| `TestOptionWithProjection` | pending | none |
| `TestOptionWithProjectionMultiple` | pending | none |
| `TestOptionWithProjectionNil` | pending | none |
| `TestOptionWithSnapshotStore` | pending | none |
| `TestOptionWithStateStore` | pending | none |
| `TestOptionWithTelemetry` | pending | none |
| `TestOptionWithTelemetryNil` | pending | none |
| `TestOptionWithTenantResolver` | pending | none |
| `TestOptionWithTenantResolverAmbiguousCount` | pending | none |
| `TestOptionWithTenantResolverCountsOnlyNonNilRegistrations` | pending | none |
| `TestOptionWithTenantResolverFuncTypedNil` | pending | none |
| `TestOptionWithTenantResolverNil` | pending | none |
| `TestOptionWithTenantResolverNilAfterNonNil` | pending | none |
| `TestOptionWithTenantResolverTypedNil` | pending | none |
| `TestOptionWithTenantResolverTypedNilThenValid` | pending | none |
| `TestOptionWithTenantResolverValidThenTypedNil` | pending | none |
| `TestParseCommandReply` | pending | testify mock, generated mocks |
| `TestProjectionSupervisorContract` | pending | none |
| `TestPublisherContractsAliasPortPublishing` | pending | none |
| `TestRelocationDisabledByDefault` | pending | none |
| `TestResolveLogger` | pending | none |
| `TestRuntimeMovedTypesAreAliases` | pending | none |
| `TestRuntimeSentinelsAreTheSameValues` | pending | none |
| `TestSagaActionAndSagaCommandAreAliases` | pending | none |
| `TestSpawnDependency` | pending | none |
| `TestSpawnOption` | pending | none |
| `TestTelemetryFields` | pending | none |
| `TestToSpawnPlacement` | pending | testify mock, generated mocks |
| `TestToSupervisorDirective` | pending | testify mock, generated mocks |
| `TestToSupervisorDirectiveStop` | pending | testify mock, generated mocks |
| `TestTopicConstantsAreFixed` | pending | none |
| `TestTopicConstantsAreNotPartitionedFormats` | pending | none |
| `TestWithEventStream_NilKeepsTheDefault` | pending | none |

#### `eventadapter`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestChainAdapterReturnsError` | pending | none |
| `TestChainAdapterUsesRevision` | pending | none |
| `TestChainErrorStopsEarly` | pending | none |
| `TestChainMixedNoopAndTransform` | pending | none |
| `TestChainMultipleAdaptersAppliedInOrder` | pending | none |
| `TestChainNoAdapters` | pending | none |
| `TestChainNoopAdapter` | pending | none |
| `TestChainSingleAdapterTransforms` | pending | none |

#### `eventstream`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestStream` | pending | fixed wait |

#### `internal/engine/durablestate`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestActorFallsBackToHandleCommandWithoutMetadata` | pending | none |
| `TestDurableStateActorPersistStateAndPublishWritesTenantMetadata` | pending | testify mock, generated mocks |
| `TestDurableStateActorRecoverFromStoreSeedsActorTenant` | pending | testify mock, generated mocks |
| `TestDurableStateActorVerifyTenantForPersist` | pending | testify mock, generated mocks |
| `TestProvablyInSyncAfterConflict` | pending | none |

#### `internal/engine/eventsource`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestEventSourcedActorFallsBackToHandleCommandWithoutMetadata` | pending | none |
| `TestEventSourcedActorMarshalEventWritesTenantMetadata` | pending | none |
| `TestEventSourcedActorNewSnapshotEnvelopeWritesTenantMetadata` | pending | none |
| `TestEventSourcedActorRecoverRejectsMismatchedSpawnBoundTenant` | pending | none |
| `TestEventSourcedActorRecoverSeedsActorTenant` | pending | none |
| `TestEventSourcedActorSeedActorTenant` | pending | none |
| `TestEventSourcedActorVerifyTenantForPersist` | pending | testify mock, generated mocks |
| `TestResolveBatchPrecondition` | pending | none |
| `TestRetryWithBackoff` | pending | none |
| `TestShouldStayAliveAfterConflict` | pending | none |

#### `internal/engine/protocol`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestAnswerTenantBinding` | pending | none |
| `TestAttachCarrier_RoundTrip` | pending | none |
| `TestCarrierFromContext_NoneAttached` | pending | none |
| `TestClassifierRegistrySentinelsDoNotPrefixEachOther` | pending | none |
| `TestClassifyErrorReplyConcurrencyConflict` | pending | none |
| `TestClassifyErrorReplyContextCanceled` | pending | none |
| `TestClassifyErrorReplyDeadlineExceeded` | pending | none |
| `TestClassifyErrorReplyDefaultsToFailed` | pending | none |
| `TestClassifyErrorReplyWrappedConflictDegradesToFailed` | pending | none |
| `TestMetadataFromContext_InvalidCarrierFailsClosed` | pending | none |
| `TestMetadataFromContext_NoneAttached` | pending | none |
| `TestMetadataFromContext_RematerializesMetadata` | pending | none |
| `TestPreconditionFromRevisionMapsPerD4` | migrated | none |

#### `internal/engine/saga`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestActorAttachCommandMetadata` | pending | none |
| `TestSagaActionIsNoop` | pending | none |
| `TestSagaActorBindOnFirstEvent` | pending | testify mock, generated mocks |
| `TestSagaActorCheckStateReadTenant` | pending | testify mock, generated mocks |
| `TestSagaActorEventContext` | pending | testify mock, generated mocks |
| `TestSagaActorPersistAndApplyEventsWritesTenantMetadata` | pending | testify mock, generated mocks |
| `TestSagaActorRecoverReplayTenantValidation` | pending | testify mock, generated mocks |
| `TestSagaStatusWireRoundTrip` | pending | none |
| `TestSagaStatus_String` | pending | testify mock, generated mocks |

#### `internal/extensions`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestDurableStateStore` | pending | none |
| `TestEncryptorExtension` | pending | none |
| `TestEntityConfig` | pending | none |
| `TestEventAdapters` | pending | none |
| `TestEventsStore` | pending | none |
| `TestEventsStream` | pending | none |
| `TestLocalBehavior` | pending | none |
| `TestOffsetStore` | pending | none |
| `TestProjectionExtension` | pending | none |
| `TestSagaConfig` | pending | none |
| `TestSnapshotStoreExt` | pending | none |
| `TestTelemetryExtension` | pending | none |
| `TestTenancyMarker` | pending | none |

#### `internal/goaktlog`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestBackend` | pending | none |
| `TestBackendAttributesRecordsToItsDirectCaller` | pending | none |
| `TestDiscardingBackendDisablesEveryLevel` | pending | none |
| `TestGoaktArgsToMsg` | pending | none |
| `TestGoaktToSlogLevel` | pending | none |
| `TestLoggerAdapterAttributesRecordsToTheGoaktCallSite` | pending | none |
| `TestLoggerAdapterFlush` | pending | none |
| `TestLoggerAdapterFormattedMethodsSkipFormattingWhenDisabled` | pending | none |
| `TestLoggerAdapterLevelTracksTheBackendAtRuntime` | pending | none |
| `TestLoggerAdapterNonStringFirstArgumentBecomesTheMessage` | pending | none |
| `TestLoggerAdapterRoutesEveryLevel` | pending | none |
| `TestLoggerAdapterStdLogger` | pending | none |
| `TestLoggerAdapterWithBuildsTheChildInTheBackend` | pending | none |
| `TestLoggerWriterTrimsLineEndings` | pending | none |
| `TestNewWrapsTheBackendInAnAdapter` | pending | none |

#### `internal/instrumentation`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestInstallPropagator` | pending | none |
| `TestNewCreatesTheCatalog` | pending | none |
| `TestNewWithoutMeterDisablesMetrics` | pending | none |
| `TestNilInstrumentsRecordNothing` | pending | none |
| `TestRecordingMethods` | pending | none |
| `TestSendCommandSpan` | pending | none |
| `TestShardRecordsTheGaugesWithProjectionAttributes` | pending | none |
| `TestStartCommandSpan` | pending | none |

#### `internal/logging`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestDefaultLoggerIsKitLoggerGlobal` | pending | none |
| `TestResolveLogger` | pending | none |

#### `internal/projectionrunner`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestOption` | pending | none |
| `TestProjectionRunnerDefaultLogger` | pending | testify mock, generated mocks |
| `TestProjectionRunnerErrorPaths` | pending | fixed wait, testify mock, generated mocks |
| `TestProjectionRunnerFatalPaths` | pending | fixed wait, testify mock, generated mocks |
| `TestProjectionRunnerLagMetrics` | pending | fixed wait, testify mock, generated mocks |
| `TestRunner` | pending | fixed wait, testify mock, generated mocks |
| `TestRunnerPullEfficiency` | pending | fixed wait, testify mock, generated mocks |
| `TestStoreRetryDelay` | pending | testify mock, generated mocks |
| `TestWithDeadLetterHandler` | pending | none |
| `TestWithDeadLetterHandlerNil` | pending | none |
| `TestWithEncryptor` | pending | none |
| `TestWithEventAdapters` | pending | none |
| `TestWithEventAdaptersEmpty` | pending | none |
| `TestWithMetrics` | pending | none |

#### `internal/queue`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestQueueDequeueEmpty` | pending | none |
| `TestQueueIsEmpty` | pending | none |
| `TestQueueLength` | pending | none |

#### `internal/runner`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestAddContextRunner` | pending | none |
| `TestAddContextRunnerIf` | pending | none |
| `TestChain` | pending | none |

#### `internal/syncmap`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestDelete` | pending | none |
| `TestForEach` | pending | none |
| `TestGet` | pending | none |
| `TestLen` | pending | none |
| `TestNewAndSet` | pending | none |
| `TestReset` | pending | none |
| `TestValues` | pending | none |

#### `internal/ticker`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestTicker` | pending | real timer |

#### `migration`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestConsumeTag` | pending | none |
| `TestConsumeVarint` | pending | none |
| `TestExtractLegacyResultingState` | pending | none |
| `TestMaxReplayLimitFitsInAnInt` | pending | none |
| `TestMigratorOptions` | pending | none |
| `TestMigratorReplaysSequencesBeyondTheLimitValue` | pending | none |
| `TestMigratorRun` | pending | none |
| `TestMigratorRunWithNilLogger` | pending | none |
| `TestMigratorScope` | pending | none |
| `TestMigratorUsesKitLogger` | pending | none |
| `TestNewRejectsAnInvalidScope` | pending | none |
| `TestNewTenantAdopter` | pending | none |
| `TestNewTenantAdopterRejectsAnInvalidSourceScope` | pending | none |
| `TestNewTenantAdopterRejectsZeroScanPageSize` | pending | none |
| `TestNewTenantAdopterRequiresAFenceToWrite` | pending | none |
| `TestScopedMigratorFailsClosedOnUnprovableTenantMetadata` | pending | none |
| `TestTenantAdopterAcquiresFencesInDeterministicOrder` | pending | none |
| `TestTenantAdopterAdoptsEveryAggregateAcrossMultiplePages` | pending | none |
| `TestTenantAdopterAlreadyPresentInTargetIsNeverOverwritten` | pending | none |
| `TestTenantAdopterAssignmentOkFalseLeavesUntouched` | pending | none |
| `TestTenantAdopterChainedEventReceipts` | pending | none |
| `TestTenantAdopterCountsSideEffectsOfAFailedAggregate` | pending | none |
| `TestTenantAdopterDeletesSourceOfVerifiedExistingTarget` | pending | none |
| `TestTenantAdopterDryRunWritesNothing` | pending | none |
| `TestTenantAdopterDurableStateTargetRaceIsStoppedByItsPrecondition` | pending | none |
| `TestTenantAdopterEventsVerificationCatchesCorruptedWrite` | pending | none |
| `TestTenantAdopterEventsVerificationRejectsDuplicateSequenceRows` | pending | none |
| `TestTenantAdopterExplicitPersistenceIDsForDurableStateOnly` | pending | none |
| `TestTenantAdopterFencedSourceWriterCannotInterleaveWithDeletion` | pending | none |
| `TestTenantAdopterLaterSameTenantTargetIsNotEquivalent` | pending | none |
| `TestTenantAdopterMissingSourceClassification` | pending | none |
| `TestTenantAdopterNeverOverwritesAConcurrentlyCreatedTargetSnapshot` | pending | none |
| `TestTenantAdopterNeverReportsDeletionOfASourceThatStillExists` | pending | none |
| `TestTenantAdopterPerAggregateFailureDoesNotAbortRun` | pending | none |
| `TestTenantAdopterPreDeleteCheckIgnoresReplayOrder` | pending | none |
| `TestTenantAdopterReRunIsANoOp` | pending | none |
| `TestTenantAdopterRealRunCopiesAndKeepsSource` | pending | none |
| `TestTenantAdopterReceiptProvesAdoptionAfterSourceDeletion` | pending | none |
| `TestTenantAdopterRefusesDeletionOfReplacedSameSequenceSnapshot` | pending | none |
| `TestTenantAdopterRefusesDeletionOfRewrittenSourceEvent` | pending | none |
| `TestTenantAdopterRejectsATargetEqualToTheSource` | pending | none |
| `TestTenantAdopterReleasesItsFencesOnEveryPath` | pending | none |
| `TestTenantAdopterReplaysSequencesBeyondTheLimitValue` | pending | none |
| `TestTenantAdopterSamePositionTargetClassification` | pending | none |
| `TestTenantAdopterSnapshotDeletionRefusesSuccessUnderConcurrentWrites` | pending | none |
| `TestTenantAdopterSnapshotOnlyReRunAfterDeletionIsIdempotent` | pending | none |
| `TestTenantAdopterSnapshotVerificationCatchesCorruptedWrite` | pending | none |
| `TestTenantAdopterSourceDeletingReRunIsIdempotent` | pending | none |
| `TestTenantAdopterSourceDeletionOnlyAfterVerification` | pending | none |
| `TestTenantAdopterSourceDeletionRefusesSuccessUnderConcurrentWrites` | pending | none |
| `TestTenantAdopterStampsTargetTenantMetadata` | pending | none |
| `TestTenantAdopterStateVerificationCatchesCorruptedWrite` | pending | none |
| `TestTenantAdopterTargetExtendedByLiveWritesIsAlreadyPresent` | pending | none |
| `TestTenantAdopterTwoTenantsAreIsolated` | pending | none |

#### `persistence`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestConflictErrorActualRevisionUnknownWhenNotSupplied` | pending | none |
| `TestConflictErrorDoesNotMatchUnrelatedSentinel` | pending | none |
| `TestConflictErrorErrorMessageCanonicalGrammar` | pending | none |
| `TestConflictErrorIdentifiableViaErrorsAs` | pending | none |
| `TestConflictErrorIdentifiableViaErrorsIs` | pending | none |
| `TestConflictErrorScopeAccessor` | pending | none |
| `TestConflictErrorWrappedIsStillIdentifiable` | pending | none |
| `TestNewTenantScopeRejectsEmptyTenantID` | pending | none |
| `TestParseConflictErrorIsExactInverseOfError` | pending | none |
| `TestParseConflictErrorRejectsMalformedMessages` | pending | none |
| `TestParseConflictErrorRoundTripsAdversarialIdentifiers` | pending | none |
| `TestScopeIsUnscoped` | pending | none |
| `TestScopeNewTenantScopeIsValid` | pending | none |
| `TestScopeStringDistinguishesKinds` | pending | none |
| `TestScopeTenantIDRoundTrips` | pending | none |
| `TestScopeTenantNamedUnscopedDoesNotEqualUnscoped` | pending | none |
| `TestScopeTwoTenantScopesWithDifferentIDsAreNotEqual` | pending | none |
| `TestScopeTwoTenantScopesWithSameIDAreEqual` | pending | none |
| `TestScopeUnscopedIsValid` | pending | none |
| `TestScopeUnscopedNotEqualToTenantScope` | pending | none |
| `TestScopeZeroValueIsInvalid` | pending | none |
| `TestWritePreconditionComparable` | pending | none |
| `TestWritePreconditionExpectGenesis` | pending | none |
| `TestWritePreconditionExpectRevision` | pending | none |
| `TestWritePreconditionExpectRevisionZeroIsNotGenesisOrUnconditional` | pending | none |
| `TestWritePreconditionUnconditional` | pending | none |
| `TestWritePreconditionZeroValueIsInvalid` | pending | none |

#### `port/adapter`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestAccessors_AreIndependent` | pending | none |
| `TestAccessors_ImplementingValueIsReturned` | pending | none |
| `TestAccessors_TypedNilIsTreatedAsAbsent` | pending | none |
| `TestAccessors_UndeclaredValueReturnsZeroAndFalse` | pending | none |
| `TestAssertionSitesNegativeControl` | pending | none |
| `TestDescriptor_DeclaresAndServes` | pending | none |
| `TestLifecycleCapabilities` | pending | none |

#### `port/adapter/adaptertest`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestCapture_CapabilityWithoutCheckFailsAT1` | pending | none |
| `TestCapture_CloseAfterFailedStartFailsAT3` | pending | none |
| `TestCapture_CloseIgnoringTheDeadlineFailsAT4` | pending | none |
| `TestCapture_ConstructorAcquireIsNotExercised` | pending | none |
| `TestCapture_DeclaredReadyWithoutPingFailsAT1` | pending | none |
| `TestCapture_EmptyNameFailsAT1` | pending | none |
| `TestCapture_FailStartWhoseAcquireSucceedsFailsAT2` | pending | none |
| `TestCapture_FailingPingFailsAT5` | pending | none |
| `TestCapture_ImpliedCapReadyIsNotRequiredForStores` | pending | none |
| `TestCapture_InvalidTargetFails` | pending | none |
| `TestCapture_LyingDescriptorFailsAT1NamingCapStart` | pending | none |
| `TestCapture_NonIdempotentCloseFailsAT3` | pending | none |
| `TestCapture_OnlyErrUnreachableSkips` | pending | none |
| `TestCapture_TargetCapabilitiesAreCheckedBothWays` | pending | none |
| `TestCapture_TargetCapabilitiesMayNotListSuiteCapabilities` | pending | none |
| `TestCapture_UndeclaredAdapterIsNotExercisedByAT1` | pending | none |
| `TestCapture_UndeclaredCapabilityFailsAT1` | pending | none |
| `TestCapture_UnstableDescriptorFailsAT1` | pending | none |
| `TestCapture_WrongPortFailsAT1` | pending | none |
| `TestImpliesReadyMatchesTheStorePortConstants` | pending | none |
| `TestRun_CorrectBorrowedStorePasses` | pending | none |
| `TestRun_CorrectStarterPassesEveryCheck` | pending | none |

#### `port/behavior`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestDomainOnlyBehaviorsRunThroughTheContracts` | pending | none |

#### `port/publishing/publishingtest`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestCapture_LostEventFailsPT3` | pending | none |
| `TestCapture_NoObserverIsNotExercised` | pending | none |
| `TestCapture_OnlyUnreachableSkips` | pending | none |
| `TestCapture_PublishingAfterCloseFailsPT1` | pending | none |
| `TestCapture_RealAdaptertestErrUnreachableSkips` | pending | none |
| `TestCapture_UnstableIDFailsPT2` | pending | none |
| `TestRunEvents_CorrectPublisherPasses` | pending | none |
| `TestRunState_CorrectPublisherPasses` | pending | none |

#### `port/runtime`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestAdapterSettingLastWins` | pending | none |
| `TestAdapterSettingLookupWithNonComparableKeyIsAbsent` | pending | none |
| `TestAdapterSettingNilValueIsStored` | pending | none |
| `TestAdapterSettingRoundTrip` | pending | none |
| `TestDoubleSendCommandRunsTheBehavior` | pending | none |
| `TestDoubleSpawnAppliesOptionsInOrderAndSkipsNil` | pending | none |
| `TestDoubleSpawnResolvesDocumentedDefaults` | pending | none |
| `TestDoubleUnsupportedOperations` | pending | none |
| `TestEnumValuesAreUnchanged` | pending | none |
| `TestErrUnsupportedWrapsStandardError` | pending | none |
| `TestResolveSpawnOptionsAppliesInOrder` | pending | none |
| `TestResolveSpawnOptionsDefaults` | pending | none |
| `TestResolveSpawnOptionsEachOptionReachesItsGetter` | pending | none |
| `TestResolveSpawnOptionsEmbeddedOptionApplies` | pending | none |
| `TestResolveSpawnOptionsNonNilWrapperOfNilOptionPanics` | pending | none |
| `TestResolveSpawnOptionsSkipsNilOption` | pending | none |
| `TestSagaStatusString` | pending | none |
| `TestSentinelMessagesAreKept` | pending | none |
| `TestSpawnSettingsIsolatedFromLaterResolutions` | pending | none |
| `TestUnsupportedError` | pending | none |
| `TestWithAdapterSettingPanicsAtBuildTime` | pending | none |

#### `projection`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestDiscardDeadLetterHandler_Handle` | pending | none |
| `TestDiscardDeadLetterHandler_InterfaceCompliance` | pending | none |
| `TestDiscardHandler_Handle` | pending | none |
| `TestDiscardHandler_InterfaceCompliance` | pending | none |
| `TestNewDiscardDeadLetterHandler` | pending | none |
| `TestNewDiscardHandler` | pending | none |
| `TestNewRecovery_Defaults` | pending | none |
| `TestNewRecovery_WithAllOptions` | pending | none |
| `TestRecoveryOption` | pending | none |

#### `tenancy`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestAdministrative_WithCorrelationIDIsOptional` | pending | none |
| `TestAsFixedTenantResolver` | pending | none |
| `TestAttach_BindsTenantContextRetrievableViaFrom` | pending | none |
| `TestAttach_IsIdempotentForTheSameTenantContext` | pending | none |
| `TestAttach_RejectsChangingAlreadyBoundTenantContext` | pending | none |
| `TestAttach_RejectsZeroValueTenantContext` | pending | none |
| `TestAttach_ValidAdministrativeContextStillFlowsThroughUnchanged` | pending | none |
| `TestAttach_ValidTenantScopedContextStillFlowsThroughUnchanged` | pending | none |
| `TestCapFixedTenant_IsAnUntypedConstant` | pending | none |
| `TestError_ErrorMessage` | pending | none |
| `TestError_IsDoesNotMatchOtherSentinels` | pending | none |
| `TestError_IsMatchesSentinelByReason` | pending | none |
| `TestError_ReasonAccessor` | pending | none |
| `TestError_TenantAccessorWithAttribution` | pending | none |
| `TestError_TenantAccessorWithoutAttribution` | pending | none |
| `TestError_UnwrapReturnsCause` | pending | none |
| `TestError_UnwrapReturnsNilWithoutCause` | pending | none |
| `TestFixedTenantAccessors_NeverResolve` | pending | none |
| `TestFixedTenantOf` | pending | none |
| `TestFrom_ReturnsFalseWhenNothingAttached` | pending | none |
| `TestInvocation_EntrypointResolvesAndAttaches_BehaviorOnlyRequires` | pending | none |
| `TestInvocation_SkippingEntrypointAttachMeansBehaviorFails` | pending | none |
| `TestMarshalMetadata_AdministrativeScope_OmitsCorrelationIDWhenAbsent` | pending | none |
| `TestMarshalMetadata_AdministrativeScope_UsesEgoTenantKeys` | pending | none |
| `TestMarshalMetadata_TenantScope_UsesEgoTenantKeys` | pending | none |
| `TestMetadata_RoundTrip_AdministrativeScope` | pending | none |
| `TestMetadata_RoundTrip_TenantScope` | pending | none |
| `TestNewAdministrativeContext_IsTypeDistinctAndAttributed` | pending | none |
| `TestNewAdministrativeContext_RejectsZeroValueAdministrative` | pending | none |
| `TestNewAdministrative_AcceptsActorAndReason` | pending | none |
| `TestNewAdministrative_RequiresActorAndReason` | pending | none |
| `TestNewTenantContext_DifferentTenantsProduceDifferentContexts` | pending | none |
| `TestNewTenantContext_ProducesTenantScopedContext` | pending | none |
| `TestNewTenantContext_RejectsEmptyTenantID` | pending | none |
| `TestNewTenantContext_RejectsZeroValueTenantID` | pending | none |
| `TestNewTenantContext_RevalidatesTenantIDBypassingConstructor` | pending | none |
| `TestNewTenantID_AcceptsArbitraryNonUUIDIdentifiers` | pending | none |
| `TestNewTenantID_AcceptsInteriorWhitespace` | pending | none |
| `TestNewTenantID_AcceptsMaxLength` | pending | none |
| `TestNewTenantID_DoesNotNormalizeCase` | pending | none |
| `TestNewTenantID_RejectsControlRune` | pending | none |
| `TestNewTenantID_RejectsEmpty` | pending | none |
| `TestNewTenantID_RejectsInvalidUTF8` | pending | none |
| `TestNewTenantID_RejectsLeadingOrTrailingWhitespace` | pending | none |
| `TestNewTenantID_RejectsTabAndNewline` | pending | none |
| `TestNewTenantID_RejectsTooLong` | pending | none |
| `TestNewTenantID_RejectsWhitespaceOnly` | pending | none |
| `TestRequire_AcceptsValidTenantContextBoundDirectly` | pending | none |
| `TestRequire_RejectsInvalidTenantContextEvenIfSomehowBound` | pending | none |
| `TestRequire_ReturnsBoundTenantContext` | pending | none |
| `TestRequire_ReturnsErrMissingWhenNothingAttached` | pending | none |
| `TestSagaBoundary_ReconstructsTenantIdentityFromCarriedMetadata` | pending | none |
| `TestSagaBoundary_SkippingMetadataReconstructionFailsClosed` | pending | none |
| `TestSentinels_AreDistinctFromEachOther` | pending | none |
| `TestTenantContext_ZeroValueIsNeitherScope` | pending | none |
| `TestUnmarshalMetadata_RejectsAdministrativeScopeMissingAttribution` | pending | none |
| `TestUnmarshalMetadata_RejectsMissingScope` | pending | none |
| `TestUnmarshalMetadata_RejectsTenantScopeWithInvalidID` | pending | none |
| `TestUnmarshalMetadata_RejectsUnrecognizedScope` | pending | none |
| `TestVerifyUnchanged_ReturnsErrDeniedWhenDifferent` | pending | none |
| `TestVerifyUnchanged_ReturnsNilWhenEqual` | pending | none |
| `TestWithSingleTenant_IgnoresIncomingContext` | pending | none |
| `TestWithSingleTenant_IndistinguishableFromAnyResolver` | pending | none |
| `TestWithSingleTenant_ProducesTenantScopedContext` | pending | none |
| `TestWithSingleTenant_RejectsInvalidTenantID` | pending | none |

#### `test/data/testpb`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestDescriptor_IsSoundAndCarriesTheModulePath` | pending | none |

#### `testkit`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestConformanceCatchesNonIsolatingStore` | pending | none |
| `TestDurableStateScenario_GivenStateWhenCommandThenStateAndVersion` | pending | none |
| `TestDurableStateScenario_UnhandledCommandReturnsError` | pending | none |
| `TestDurableStateScenario_WhenCommandFromInitialState` | pending | none |
| `TestDurableStateScenario_WhenCommandReturnsError` | pending | none |
| `TestDurableStoreConformance` | pending | none |
| `TestDurableStore_CheckPreconditionsAloneDoesNotPreventStateStoreConflict` | pending | none |
| `TestDurableStore_Connect` | pending | none |
| `TestDurableStore_Disconnect` | pending | none |
| `TestDurableStore_GetLatestState_NotConnected` | pending | none |
| `TestDurableStore_InvalidScopeRejected` | pending | none |
| `TestDurableStore_NewDurableStore` | pending | none |
| `TestDurableStore_Ping` | pending | none |
| `TestDurableStore_T10_ConcurrentGenesisHasExactlyOneWinner` | pending | none |
| `TestDurableStore_T9_ConcurrentExpectRevisionHasExactlyOneWinner` | pending | none |
| `TestDurableStore_WriteAndGetState` | pending | none |
| `TestDurableStore_WriteState_ExactRevisionSucceedsWhenCurrent` | pending | none |
| `TestDurableStore_WriteState_GenesisConflictsOnExisting` | pending | none |
| `TestDurableStore_WriteState_GenesisSucceedsOnEmpty` | pending | none |
| `TestDurableStore_WriteState_InvalidPreconditionIsRejected` | pending | none |
| `TestDurableStore_WriteState_NotConnected` | pending | none |
| `TestDurableStore_WriteState_StaleRevisionIsConflict` | pending | none |
| `TestDurableStore_WriteState_UnconditionalIsLegacyBehavior` | pending | none |
| `TestEventSourcedScenario_GivenEventsApplyOnTopOfGivenState` | pending | none |
| `TestEventSourcedScenario_GivenEventsBuildTheState` | pending | none |
| `TestEventSourcedScenario_GivenEventsFailureIsReportedAsArrangementFailure` | pending | none |
| `TestEventSourcedScenario_GivenStateIsPassedToCommandHandler` | pending | none |
| `TestEventSourcedScenario_GivenStateWhenCommandThenEventsAndState` | pending | none |
| `TestEventSourcedScenario_HandleEventFailsOnProducedEvent` | pending | none |
| `TestEventSourcedScenario_UnhandledCommandReturnsError` | pending | none |
| `TestEventSourcedScenario_WhenCommandFromInitialState` | pending | none |
| `TestEventSourcedScenario_WhenCommandProducesNoEvents` | pending | none |
| `TestEventSourcedScenario_WhenCommandReturnsError` | pending | none |
| `TestEventStoreConformance` | pending | none |
| `TestEventStoreReplayEventsAcceptsTheMigrationReplayBounds` | pending | none |
| `TestEventStore_Connect` | pending | none |
| `TestEventStore_DeleteEvents` | pending | none |
| `TestEventStore_Disconnect` | pending | none |
| `TestEventStore_GetLatestEvent` | pending | none |
| `TestEventStore_GetShardEvents` | pending | none |
| `TestEventStore_InvalidScopeRejected` | pending | none |
| `TestEventStore_NewEventsStore` | pending | none |
| `TestEventStore_PersistenceIDs` | pending | none |
| `TestEventStore_PersistenceIDsPaginationExhaustive` | pending | none |
| `TestEventStore_Ping` | pending | none |
| `TestEventStore_ShardOffsets` | pending | none |
| `TestEventStore_T10_ConcurrentGenesisHasExactlyOneWinner` | pending | none |
| `TestEventStore_T8_ConcurrentExpectRevisionHasExactlyOneWinner` | pending | none |
| `TestEventStore_T8_ConcurrentExpectRevisionHoldsAcrossManyAggregates` | pending | none |
| `TestEventStore_UnscopedDoesNotCollideWithTenantScope` | pending | none |
| `TestEventStore_WriteAndReplayEvents` | pending | none |
| `TestEventStore_WriteEvents_ConditionalBatchMustShareOnePersistenceID` | pending | none |
| `TestEventStore_WriteEvents_DuplicateSequenceNumberDoesNotDuplicateShardEventsOrOffsets` | pending | none |
| `TestEventStore_WriteEvents_DuplicateSequenceNumberOverwritesNotAccumulates` | pending | none |
| `TestEventStore_WriteEvents_DuplicateSequenceNumberThenDeleteEventsLeavesNoResidual` | pending | none |
| `TestEventStore_WriteEvents_DuplicateSequenceNumberWithinConditionalWriteOverwrites` | pending | none |
| `TestEventStore_WriteEvents_ExactRevisionSucceedsWhenCurrent` | pending | none |
| `TestEventStore_WriteEvents_GenesisConflictsOnExisting` | pending | none |
| `TestEventStore_WriteEvents_GenesisSucceedsOnEmpty` | pending | none |
| `TestEventStore_WriteEvents_InvalidPreconditionIsRejected` | pending | none |
| `TestEventStore_WriteEvents_StaleRevisionIsConflict` | pending | none |
| `TestEventStore_WriteEvents_UnconditionalIsLegacyBehavior` | pending | none |
| `TestKeyStore_DeleteKey` | pending | none |
| `TestKeyStore_GetKey` | pending | none |
| `TestKeyStore_GetOrCreateKey` | pending | none |
| `TestKeyStore_NewKeyStore` | pending | none |
| `TestOffsetStore_Connect` | pending | none |
| `TestOffsetStore_Disconnect` | pending | none |
| `TestOffsetStore_NewOffsetStore` | pending | none |
| `TestOffsetStore_Ping` | pending | none |
| `TestOffsetStore_WriteAndGetOffset` | pending | none |
| `TestSnapshotStoreConformance` | pending | none |
| `TestSnapshotStore_Connect` | pending | none |
| `TestSnapshotStore_DeleteSnapshots` | pending | none |
| `TestSnapshotStore_Disconnect` | pending | none |
| `TestSnapshotStore_InvalidScopeRejected` | pending | none |
| `TestSnapshotStore_NewSnapshotStore` | pending | none |
| `TestSnapshotStore_Ping` | pending | none |
| `TestSnapshotStore_WriteAndGetSnapshot` | pending | none |
| `TestStoreDescriptors` | pending | none |
| `TestStoresAdapterConformance` | pending | none |

### Module `publisher/kafka`

#### `publisher/kafka`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestClosureGuardRejectsCompositionRoot` | pending | none |
| `TestPublishBeforeStartMatchesPublishingSentinel` | pending | none |

### Module `publisher/nats`

#### `publisher/nats`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestClosureGuardRejectsCompositionRoot` | pending | none |
| `TestPublishBeforeStartMatchesPublishingSentinel` | pending | none |

### Module `publisher/pulsar`

#### `publisher/pulsar`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestClosureGuardRejectsCompositionRoot` | pending | none |
| `TestPublishBeforeStartMatchesPublishingSentinel` | pending | none |

### Module `publisher/websocket`

#### `publisher/websocket`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestClosureGuardRejectsCompositionRoot` | pending | none |
| `TestDescriptors` | pending | none |

### Module `test/compat`

#### `test/compat`

| Test | Status | Dependencies to substitute |
|---|---|---|
| `TestEgoSentinelIsThePublishingSentinel` | pending | none |

## Out of phase

256 tests need a real component or resource, so they are not part of this phase and keep their current execution. Each one is listed with the real dependency that keeps it out. The 19 Postgres tests skip today because `EGO_EXAMPLE_POSTGRES_DSN` is not set. Twenty-five of these tests (12 in `engine`, 13 in `compose/goakt`) look like unit tests but start an actor system through a helper or through `App.Start`.

### Module `.`

- `compose/goakt` (15)
  - starts a GoAkt cluster on loopback ports: `TestApp_TwoNodeClusterPlacesAndStopsCleanly`
  - starts an actor system: `TestNew_G2_ActorSystemName`
  - starts an actor system through a helper: `TestApp_ValidSpecRunsAnEngine`, `TestEngine_UndeclaredFamilyReturnsTypedError`, `TestRuntime_ConsumerDrivesTheAppEndToEnd`, `TestRuntime_IsTheEngineAfterStartAndAfterStop`, `TestRuntime_NilAfterFailedStart`, `TestStart_ActorSystemStepFailsForReal`, `TestStart_AttachStepStartsAndProbesPublishersFirst`, `TestStart_FailureAtEachStepReleasesEverything`, `TestStart_PublisherFailureAtK`, `TestStart_PublisherPingFailureNamesTheAdapter`, `TestStop_AfterStopIsNoOp`, `TestStop_D7OpenQuestion_StateFlushedDuringActorShutdown`, `TestStop_OrderMatchesD7`
- `engine` (143)
  - runs `go list`: `TestCommandArchitecture`, `TestTenancyArchitecture`
  - reads the repository source files: `TestKitLoggerIsTheOnlyLoggingBackend`
  - starts a GoAkt cluster on loopback ports: `TestEngineClusterMode`, `TestEngineClusterModeStartProjectionAlreadyExists`, `TestEngineMultiNodeNeutralBehaviors`, `TestEngineMultiNodeRemoteEntitySpawn`, `TestEngineRejectsUnplaceableBehaviorsInClusterMode`, `TestEngineRemoteSpawnTenantBinding`, `TestEventPublisherClusterHighPartitionCount`, `TestNewEngineRejectsValueTypeKindInClusterMode`
  - starts an actor system: `TestAddPublishersRejectsDuplicateIDs`, `TestAdministrativeScopeIsNeverAnAggregateTenantScope`, `TestBatchedPreconditionMatrix_GenesisBase`, `TestConfigGoaktOptionsEncryptor`, `TestConfigGoaktOptionsNoTenancyMarkerWithTypedNilResolver`, `TestConfigGoaktOptionsNoTenancyMarkerWithoutResolver`, `TestConfigGoaktOptionsProjectionDefaultsRecovery`, `TestConfigGoaktOptionsTelemetry`, `TestConfigGoaktOptionsTenancyMarker`, `TestDurableStateActorDiscardsHandlerOutputAfterDeadlineExpiry`, `TestDurableStateCheckPreconditionsPassesYetExpectedRevisionConflicts`, `TestDurableStateConcurrentGenesisWritersYieldExactlyOneCommit`, `TestDurableStateConditionalWriteEvaluatedAgainstStorageRevision`, `TestDurableStateConflictResultShape`, `TestDurableStateExpectedRevisionEndToEndPropagation`, `TestDurableStateExpectedRevisionExactMatchCommits`, `TestDurableStateHandlerShapeUnchangedByExpectedRevision`, `TestDurableStateNoPartialCommitOnConflict`, `TestDurableStateNonAdjacentVersionIsNeverConcurrencyConflict`, `TestDurableStateStaleExpectedRevisionRejectedAtStoreNotCache`, `TestEngineActorSystemAccessor`, `TestEngineAddEventPublishers`, `TestEngineAddEventPublishersGuards`, `TestEngineAddStatePublishers`, `TestEngineAddStatePublishersGuards`, `TestEngineCommandRejectsTenantMismatchWithSpawnDeclaredTenant`, `TestEngineConfigRegistersAllExtensions`, `TestEngineDispatchClampsTimeoutToDeadline`, `TestEngineDispatchDispatchesHandleEnvelope`, `TestEngineDispatchEffectiveDeadlinePrecedence`, `TestEngineDispatchRejectsExpiredDeadlineWithoutInvokingHandler`, `TestEngineDispatchRejectsInvalidMetadataWithoutInvokingHandler`, `TestEngineDispatchRejectsZeroValueEnvelopeWithoutPanicking`, `TestEngineDurableState`, `TestEngineDurableStateRequiresStateStore`, `TestEngineEntityExists`, `TestEngineEntitySpawnRequiresExplicitTenantWhenResolverHasNoFixedTenant`, `TestEngineEntitySpawnWithExplicitTenantResolvesOnce`, `TestEngineEntitySpawnWithoutResolverStaysUnscoped`, `TestEngineEntityValueTypeBehaviorSingleNode`, `TestEngineEntityWithRetentionPolicy`, `TestEngineEraseEntity`, `TestEngineEraseEntityCannotEraseAnotherTenantsRecord`, `TestEngineEraseEntityErrors`, `TestEngineEventPublisherKeepsGoingOnPublishError`, `TestEngineEventSourced`, `TestEngineIsProjectionRunningActorOfError`, `TestEngineNotStartedGuardsDirect`, `TestEngineProjection`, `TestEngineProjectionLagClampsNegative`, `TestEngineProjectionLagErrors`, `TestEngineProjectionLagHappyPath`, `TestEngineProjectionLagWithEvents`, `TestEngineProjectionsOwnHandlers`, `TestEnginePublisherIdleCPU`, `TestEngineRebuildProjectionErrors`, `TestEngineRebuildProjectionRemoveError`, `TestEngineRebuildProjectionResetOffsetError`, `TestEngineRebuildProjectionRestartError`, `TestEngineRebuildProjectionSuccess`, `TestEngineRejectsNilBehaviorsSingleNode`, `TestEngineRespawnInLegacyModeIsUnchanged`, `TestEngineSagaHappyPath`, `TestEngineSagaSpawnError`, `TestEngineSagaStatusErrorPaths`, `TestEngineSagaStatusMapsWireStatus`, `TestEngineSagaStatusReportsLifecycleStatus`, `TestEngineSagaStatusTenantIsolation`, `TestEngineSendCommandDispatchesDurableStateHandleEnvelope`, `TestEngineSendCommandDispatchesHandleEnvelope`, `TestEngineSendCommandErrors`, `TestEngineSendCommandUnexpectedReply`, `TestEngineSendCommandWithTelemetry`, `TestEngineSpawnMethodsDomainOnlySingleNode`, `TestEngineSpawnWithMultiTenantFixedTenantResolverNeedsWithTenant`, `TestEngineSpawnsDomainOnlyBehaviorsSingleNode`, `TestEngineStartProjectionNotRegistered`, `TestEngineStartProjectionStandaloneSpawnError`, `TestEngineStartWithTelemetry`, `TestEngineStatePublisherKeepsGoingOnPublishError`, `TestEngineStopAttemptsEveryStep`, `TestEngineStopReturnsEventPublisherCloseError`, `TestEngineStopReturnsStatePublisherCloseError`, `TestEngineSubscribeBeforeStart`, `TestEngineSubscribeReceivesEventsAndStates`, `TestEngineWithSingleTenantSpawnNeedsNoWithTenant`, `TestEventPayloadCarriesShard`, `TestEventPublisherFanOutToMultipleSubscribers`, `TestEventPublisherReceivesEventsFromEntity`, `TestEventSourcedActorBatchPathDoesNotContaminateBatchStateAfterDeadlineExpiry`, `TestEventSourcedActorDirectPathDiscardsHandlerOutputAfterDeadlineExpiry`, `TestEventSourcedActorStaysConsistentAfterConflict`, `TestEventSourcedBatchedExpectedRevisionSuccessAndConflict`, `TestEventSourcedExpectedRevisionGenesisConflictsOnExistingAggregate`, `TestEventSourcedExpectedRevisionGenesisSucceedsOnNewAggregate`, `TestEventSourcedExpectedRevisionPropagatesToPersistencePrecondition`, `TestEventSourcedExpectedRevisionStaleIsConcurrencyConflict`, `TestEventSourcedExpectedRevisionSuccessMatchesCurrent`, `TestEventSourcedHandlerArgumentsNeverCarryExpectedRevision`, `TestEventSourcedIntegrationConcurrentGenesisYieldsExactlyOneCommit`, `TestEventSourcedIntegrationExactRevisionCommitsAndAdvancesStore`, `TestEventSourcedIntegrationStaleRevisionRejectedStoreUnchanged`, `TestEventSourcedLegacyCommandIsUnconditional`, `TestGoaktOptionsCarryTheResolvedLogger`, `TestLegacyCompatEventSourcedAndDurableStateNeverConflict`, `TestNewEngineAcceptsTypedNilPointerKind`, `TestNewEngineRejectsUnregistrableKindsSingleNode`, `TestNewEngineTenantResolverValidation`, `TestNewEngineValidation`, `TestProjectionActorRunnerFailure`, `TestSendCommandResolverSwapIdenticalSequence`, `TestSendCommandSingleTenantZeroPlumbing`, `TestSendCommandTenantResolution`, `TestSpawnWithoutEventsStore`, `TestStatePublisherReceivesDurableStateUpdates`, `TestTelemetryContract`, `TestTelemetryDisabled`, `TestTenantWritePathE2E`, `TestWithEntityKindsAndWithBehaviorKindsShareRegistration`, `TestWithEventStream_UsesTheGivenStream`
  - starts an actor system through a helper: `TestBatchAdmissionGateRejectsStaleRevision_ForcesEarlyFlushThenFoundsFreshBatch`, `TestBatchedExternalWriterWinsCAS_RejectsWholeBatchWithoutAdvancingCounter`, `TestBatchedPhysicalBaseAnchorsToPreBatchRevision_NotLogicalCounter`, `TestBatchedZeroEventAdmittedCommandStillPreservesLaterPrecondition`, `TestBatchedZeroEventFounderNeverOpensBatch`, `TestDispatchRejectsTenantBindingQuery`, `TestEngineConcurrentCrossTenantSpawnHasExactlyOneWinner`, `TestEngineRespawnUnderAnotherTenantIsRejected`, `TestWithEntityFamilies_Combined`, `TestWithEntityFamilies_NotDeclaredAllowsEveryFamily`, `TestWithEntityFamilies_UndeclaredFamilyIsRejected`, `TestWithEntityFamilies_UnknownBitsAreIgnored`
- `internal/engine/durablestate` (9)
  - starts an actor system: `TestDurableStateActorFailedFirstCommandDoesNotAppropriateActor`, `TestDurableStateActorGetStateCommandTenancyGate`, `TestDurableStateActorPostStopTenantPersist`, `TestDurableStateActorPreStartExtensions`, `TestDurableStateActorProcessCommandRejectsCrossTenant`, `TestDurableStateActorRecoverFromStoreLegacyVersionZeroGenesis`, `TestDurableStateActorTenancyGate`, `TestDurableStateActorTenancyWritePath`, `TestDurableStateBehavior`
- `internal/engine/eventsource` (23)
  - starts an actor system: `TestEventSourcedActor`, `TestEventSourcedActorBatch`, `TestEventSourcedActorBatchTenantHomogeneity`, `TestEventSourcedActorBatchTenantHomogeneity_ZeroEventCrossTenant`, `TestEventSourcedActorBatchTenantHomogeneity_ZeroEventSameTenant`, `TestEventSourcedActorErrorPaths`, `TestEventSourcedActorGetStateCommandRejectsCrossTenant`, `TestEventSourcedActorGetStateCommandRequiresTenantWhenTenantAware`, `TestEventSourcedActorGetStateDuringPersist`, `TestEventSourcedActorLegacyModeAlwaysUsesUnscopedStore`, `TestEventSourcedActorPreStartFailsClosedWithoutTenantScope`, `TestEventSourcedActorProcessCommandAndReplyRejectsCrossTenant`, `TestEventSourcedActorResetBatchDoesNotClearActorTenant`, `TestEventSourcedActorSpawnBindsExactTenantScope`, `TestEventSourcedActorTenancyGate`, `TestEventSourcedActorTenantIdentitySurvivesRestart`, `TestEventWriteObservableSequence`, `TestEventsJanitorActor`, `TestEventsWriterActor`, `TestSnapshotAndRetentionObservableSequence`, `TestSnapshotsWriterActor`, `TestSnapshotsWriterContract`, `TestWriterContract`
- `internal/engine/projection` (2)
  - starts an actor system: `TestProjection`, `TestProjectionActorPreStartFailure`
- `internal/engine/saga` (5)
  - starts an actor system: `TestSagaActor`, `TestSagaActorCompensateUsesBoundTenant`, `TestSagaActorDurableTenantBinding`, `TestSagaActorSendCommandThreadsTenantContext`, `TestSagaFailsClosed`
- `internal/extensions` (2)
  - starts an actor system: `TestOptionalExtension`, `TestRequireExtension`
- `internal/instrumentation` (1)
  - runs `go list`: `TestInstrumentationStaysRuntimeNeutral`
- `internal/logging` (1)
  - runs `go list`: `TestLoggingStaysRuntimeNeutral`
- `internal/projectionrunner` (1)
  - runs `go list`: `TestProjectionRunnerStaysRuntimeNeutral`
- `internal/runtimeconsumer` (1)
  - runs `go list`: `TestProductionClosureExcludesRootAndGoAkt`
- `migration` (3)
  - runs `go list`: `TestProductionClosureExcludesRootAndGoAkt`
  - starts an actor system: `TestScopedMigratorSnapshotRecoversThroughTenantAwareActor`, `TestTenantAdopterEndToEndRecoveryThroughRealActor`
- `port/adapter` (5)
  - parses the repository source files: `TestNoPrivateCopiesOfOptionalInterfaces`, `TestOptionalInterfacesAreAssertedOnlyInTheirAccessors`, `TestPortNameConstantsAreUntyped`
  - runs `go list`: `TestAdapterDependsOnlyOnStdlib`, `TestContractPackagesDoNotImportAdapter`
- `port/adapter/adaptertest` (1)
  - runs `go list`: `TestAdaptertestDependsOnlyOnStdlibAndAdapter`
- `port/behavior` (1)
  - runs `go list`: `TestBehaviorDependsOnlyOnContracts`
- `port/publishing` (1)
  - runs `go list`: `TestPublishingDependsOnlyOnContracts`
- `port/publishing/publishingtest` (1)
  - runs `go list`: `TestPublishingtestDependsOnlyOnStdlibPublishingAndEgopb`
- `port/runtime` (2)
  - runs `go list`: `TestRuntimeDependsOnlyOnContracts`, `TestRuntimeTestClosureExcludesGoAktAndRoot`

### Module `example/cluster`

- `example/cluster` (30)
  - Postgres via EGO_EXAMPLE_POSTGRES_DSN, currently skipped: `TestPostgresEventStore_ConcurrentExpectRevisionHasExactlyOneWinner`, `TestPostgresEventStore_Conformance`, `TestPostgresEventStore_DeleteEventsLocksRevisionAgainstConcurrentWrite`, `TestPostgresEventStore_DeleteKeepsRevisionPerTenant`, `TestPostgresEventStore_ExpectGenesisConflictsOnExistingID`, `TestPostgresEventStore_PartialDeleteKeepsRevision`, `TestPostgresEventStore_PersistenceIDs_ZeroPageSize_WithData`, `TestPostgresEventStore_SchemaMigratesLegacyDatabase`, `TestPostgresEventStore_SchemaMigratesLegacyTenantMetadata`, `TestPostgresEventStore_TenantMetadataAbsent_ReadsAsNone`, `TestPostgresEventStore_TenantMetadataRoundTrips_ConditionalWrite`, `TestPostgresEventStore_TenantMetadataRoundTrips_GetShardEvents`, `TestPostgresEventStore_TenantMetadataRoundTrips_UnconditionalWrite`, `TestPostgresEventStore_TenantScopeIsolatesRecords`, `TestPostgresEventStore_TotalDeleteKeepsRevision`, `TestPostgresEventStore_UnconditionalMixedBatchesDoNotDeadlock`, `TestPostgresEventStore_UnconditionalRaceDistinctSequenceConflict`, `TestPostgresEventStore_UnconditionalRaceSameSequenceConflict`, `TestPostgresEventStore_UnconditionalWriteCannotBreakExpectRevision`
  - tests the example program in example/cluster or benchmark: `TestPostgresEventStore_DeleteEvents_InvalidScope`, `TestPostgresEventStore_GetLatestEvent_InvalidScope`, `TestPostgresEventStore_ImplementsEventsStore`, `TestPostgresEventStore_PersistenceIDs_InvalidScope`, `TestPostgresEventStore_PersistenceIDs_ZeroPageSize`, `TestPostgresEventStore_ReplayEvents_InvalidScope`, `TestPostgresEventStore_WriteEvents_EmptyBatchConditional`, `TestPostgresEventStore_WriteEvents_EmptyBatchUnconditionalSucceeds`, `TestPostgresEventStore_WriteEvents_InvalidPrecondition`, `TestPostgresEventStore_WriteEvents_InvalidScope`, `TestPostgresEventStore_WriteEvents_MixedIDBatchConditional`

### Module `publisher/kafka`

- `publisher/kafka` (1)
  - runs `go list`: `TestUnitTestClosureExcludesRuntimeAndRoot`

### Module `publisher/nats`

- `publisher/nats` (1)
  - runs `go list`: `TestUnitTestClosureExcludesRuntimeAndRoot`

### Module `publisher/pulsar`

- `publisher/pulsar` (1)
  - runs `go list`: `TestUnitTestClosureExcludesRuntimeAndRoot`

### Module `publisher/websocket`

- `publisher/websocket` (6)
  - runs `go list`: `TestUnitTestClosureExcludesRuntimeAndRoot`
  - runs a local HTTP test server: `TestCloseIsIdempotent`, `TestDurableStatePublisherAdapterConformance`, `TestDurableStatePublisherPublishingConformance`, `TestEventsPublisherAdapterConformance`, `TestEventsPublisherPublishingConformance`

