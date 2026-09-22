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

package migration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"

	kitlog "github.com/pablogore/kit-logger/pkg/logger"
	"google.golang.org/protobuf/proto"

	ego "github.com/pablogore/ego/v4"
	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
	"github.com/pablogore/ego/v4/tenancy"
)

// maxReplayLimit is a safe "read everything" limit for ReplayEvents that
// won't overflow when cast to int, mirroring migrateEntity's own constant.
const maxReplayLimit = uint64(1<<63 - 1)

// ErrAssignmentRequired is returned by NewTenantAdopter when assign is nil.
// The framework has no way to decide which tenant an existing aggregate
// belongs to — that is a business fact only the operator holds — so the
// assignment function cannot be defaulted or made optional.
var ErrAssignmentRequired = errors.New("migration: a TenantAssignment is required")

// ErrNoStoresConfigured is returned by NewTenantAdopter when none of
// WithEventsStore, WithSnapshotStore, or WithStateStore was supplied: a
// TenantAdopter with nothing to read from or write to can never do useful
// work.
var ErrNoStoresConfigured = errors.New("migration: at least one of WithEventsStore, WithSnapshotStore, or WithStateStore is required")

// ErrInvalidScanPageSize is returned by NewTenantAdopter when WithScanPageSize
// was given zero: a zero page enumerates no persistence ids, so the run
// would scan nothing and still report success.
var ErrInvalidScanPageSize = errors.New("migration: WithScanPageSize must be greater than zero")

// errSourceChangedDuringAdoption is the per-kind failure recorded when a
// WithSourceDeletion run finds that the source gained records while this
// aggregate was being adopted: records newer than the verified copy would be
// left only in the source, so the run refuses to delete, or to report the
// deletion as a success.
var errSourceChangedDuringAdoption = errors.New("migration: the source changed while it was being adopted; the source must not receive writes during a WithSourceDeletion run")

// errNoSourceRecords is the per-aggregate failure recorded when an id was
// assigned a tenant but none of the configured stores actually held any
// record for it in the source scope — most often a caller mistake in the
// TenantAssignment (e.g. a stale or misspelled persistence ID supplied via
// WithPersistenceIDs).
var errNoSourceRecords = errors.New("migration: no source record found in any configured store for this persistence id")

// errTargetNotEquivalent is the per-kind failure recorded when the target
// tenant scope already holds a record for the persistence id that is not
// equivalent to what this adoption would write: a record not owned by the
// assigned tenant, or one that does not contain the source record exactly.
// A target that merely exists is never treated as a completed migration.
var errTargetNotEquivalent = errors.New("migration: target tenant scope holds a record for this persistence id that is not equivalent to this adoption")

// adoptionReceiptKey is the tenant_metadata key under which every record
// TenantAdopter writes carries its adoption receipt: "v1:" followed by the
// hex SHA-256 of the source scope and the record's deterministic protobuf
// encoding, taken with this key absent (see adoptionReceipt). Tenant-bound
// actors never write this key, so a record carrying a receipt that still
// matches its own content was written by an adoption from that source
// scope. It is what lets a re-run prove a target is already migrated after
// WithSourceDeletion removed the source it could otherwise compare with.
const adoptionReceiptKey = "ego.adoption.receipt"

// adoptionReceiptVersion prefixes every receipt value, so a future change
// to what a receipt covers is detectable rather than silently mismatched.
const adoptionReceiptVersion = "v1:"

// TenantAssignment decides which tenant an existing aggregate belongs to.
// The framework cannot make this decision itself: persistence_id is an
// opaque, caller-assigned string (persistence.Scope's own doc comment), and
// nothing about a legacy Unscoped() record says which tenant it should now
// belong to. That is a business fact only the operator holds, so it is
// supplied here rather than inferred, defaulted, or derived from
// persistence_id.
//
// Returning ok=false leaves the aggregate completely untouched in the
// source scope: TenantAdopter neither reads nor writes anything further for
// that persistence ID during this Run.
type TenantAssignment func(ctx context.Context, persistenceID string) (id tenancy.TenantID, ok bool, err error)

// RecordKind names one of the three kinds of record a TenantAdopter can
// copy for a single aggregate.
type RecordKind string

const (
	// KindEvents identifies the events-store record kind.
	KindEvents RecordKind = "events"
	// KindSnapshot identifies the snapshot-store record kind.
	KindSnapshot RecordKind = "snapshot"
	// KindDurableState identifies the durable-state-store record kind.
	KindDurableState RecordKind = "durable_state"
)

// RecordStatus is the outcome TenantAdopter reached for one RecordKind of
// one aggregate.
type RecordStatus string

const (
	// StatusNone means this record kind was not configured (no store), or
	// the source scope held no record of this kind for the aggregate.
	StatusNone RecordStatus = "none"
	// StatusCopied means a source record of this kind was written to the
	// target tenant scope (or, in dry-run, would have been).
	StatusCopied RecordStatus = "copied"
	// StatusAlreadyPresent means the target tenant scope already held a
	// record for this (kind, persistence ID) that is PROVEN to be this
	// adoption; nothing was written, overwritten, or deleted. While the
	// source exists, proof is an exact comparison: the events target holds
	// every source event proto.Equal (it may append later events), and a
	// snapshot or durable-state target is the identical record at the same
	// position. Once WithSourceDeletion removed the source, proof is the
	// adoption receipt every adopted record carries (adoptionReceiptKey).
	// Anything that cannot be proven — including same-tenant data at a later
	// position, or a record the actor rewrote after adoption — fails with
	// StatusFailed; tenant ownership or existence alone is never success.
	StatusAlreadyPresent RecordStatus = "already_present"
	// StatusSourceDeleted means the target was proven to hold this exact
	// adoption — either copied by this run and read back proto.Equal to the
	// record it intended to write (the source record with tenant_metadata
	// replaced by the target tenant's plus its adoption receipt), or already
	// present from an earlier run and compared exactly against the source —
	// and the verified source-scope copy was then removed under the guarded
	// deletion (WithSourceDeletion only). An aggregate deleted this way
	// without a fresh copy counts toward AlreadyPresent, not Copied.
	StatusSourceDeleted RecordStatus = "source_deleted"
	// StatusFailed means an unexpected error occurred while adopting this
	// record kind. See RecordOutcome.Err for detail.
	StatusFailed RecordStatus = "failed"
)

// RecordOutcome is the per-kind detail behind one AggregateOutcome.
type RecordOutcome struct {
	Status RecordStatus
	Err    error

	// wroteTarget is true when this run wrote the target record: always for
	// StatusCopied, and for StatusSourceDeleted only when the deletion
	// followed a fresh copy rather than an already-present target.
	wroteTarget bool
}

// AggregateOutcome is the per-aggregate detail behind an AdoptionReport.
type AggregateOutcome struct {
	PersistenceID string
	Tenant        tenancy.TenantID
	Events        RecordOutcome
	Snapshot      RecordOutcome
	DurableState  RecordOutcome
}

// AdoptionFailure records one aggregate-level or per-kind failure so a Run
// can continue past it (the default) while still surfacing exactly what
// went wrong. Kind is empty for a failure that happened before any
// record-kind processing began (TenantAssignment itself, or an invalid
// assigned tenant).
type AdoptionFailure struct {
	PersistenceID string
	Kind          RecordKind
	Err           error
}

// Error renders f for logs and AdoptionReport.String().
func (f AdoptionFailure) Error() string {
	if f.Kind == "" {
		return fmt.Sprintf("persistence_id=%s: %v", f.PersistenceID, f.Err)
	}
	return fmt.Sprintf("persistence_id=%s kind=%s: %v", f.PersistenceID, f.Kind, f.Err)
}

// Unwrap allows errors.Is/errors.As to see through an AdoptionFailure to
// its underlying cause.
func (f AdoptionFailure) Unwrap() error { return f.Err }

// AdoptionReport summarizes one TenantAdopter.Run. Every top-level counter
// is counted per AGGREGATE (persistence ID), not per record kind: an
// aggregate whose events were freshly copied while its snapshot was already
// present in the target still counts once toward Copied, since real work
// was done for it. AggregateOutcome carries the per-kind detail for a
// caller that needs it.
//
// In dry-run (DryRun == true), Scanned, Assigned, SkippedByAssignment,
// Copied, AlreadyPresent, and Failed reflect exactly what a real run would
// do. Verified and SourceDeleted stay zero in dry-run: nothing was actually
// written, so nothing was actually read back or deleted — reporting either
// as non-zero would fabricate evidence of a side effect that never
// happened.
type AdoptionReport struct {
	// DryRun is true when this report describes a plan (WithWriteEnabled
	// was not set) rather than a completed run.
	DryRun bool

	// Scanned is the number of distinct persistence IDs examined.
	Scanned int
	// Assigned is the number of persistence IDs for which TenantAssignment
	// returned ok=true.
	Assigned int
	// SkippedByAssignment is the number of persistence IDs for which
	// TenantAssignment returned ok=false; left untouched.
	SkippedByAssignment int
	// Copied is the number of aggregates for which at least one record kind
	// was (or, in dry-run, would be) written to the target tenant scope.
	Copied int
	// AlreadyPresent is the number of assigned aggregates for which every
	// configured record kind was already present in the target tenant
	// scope; nothing was written.
	AlreadyPresent int
	// Verified is the number of copied aggregates whose target-scope copy
	// was read back and matched the source: a full proto.Equal match of the
	// exact record this tool intended to write (the source record with
	// tenant_metadata replaced by the target tenant's plus its adoption
	// receipt) — not a proxy check against a count, sequence number, or
	// version number alone, none of
	// which can detect a corrupted payload, a dropped tenant_metadata, or a
	// missing encryption envelope. Always 0 in dry-run.
	Verified int
	// SourceDeleted is the number of aggregates for which at least one
	// record kind's source-scope copy was removed after its target was
	// verified — by a fresh copy, or as an exact already-present adoption
	// (WithSourceDeletion only). Always 0 in dry-run.
	SourceDeleted int
	// Failed is the number of persistence IDs (assignment failures and
	// aggregate failures alike) that did not complete successfully. See
	// Failures for detail.
	Failed int

	// Aggregates carries the per-kind detail for every assigned persistence
	// ID (including failed ones).
	Aggregates []AggregateOutcome
	// Failures carries every recorded failure, in the order encountered.
	Failures []AdoptionFailure
}

// String renders a human-readable, loggable summary of r, including every
// recorded failure.
func (r *AdoptionReport) String() string {
	mode := "run"
	if r.DryRun {
		mode = "dry-run"
	}
	out := fmt.Sprintf(
		"tenant adoption %s: scanned=%d assigned=%d skipped_by_assignment=%d copied=%d already_present=%d verified=%d source_deleted=%d failed=%d",
		mode, r.Scanned, r.Assigned, r.SkippedByAssignment, r.Copied, r.AlreadyPresent, r.Verified, r.SourceDeleted, r.Failed,
	)
	for _, f := range r.Failures {
		out += "\n  - " + f.Error()
	}
	return out
}

// AdoptionOption configures a TenantAdopter. It is a distinct type from
// this package's Option (which configures Migrator): the two tools are
// configured independently, even though both follow the same functional
// options shape.
type AdoptionOption interface {
	applyAdoption(a *TenantAdopter)
}

type adoptionOptionFunc func(a *TenantAdopter)

func (f adoptionOptionFunc) applyAdoption(a *TenantAdopter) { f(a) }

// WithEventsStore sets the events store TenantAdopter copies event records
// from and to. Omit it for a durable-state-only (or snapshot-only)
// deployment that never hosts event-sourced entities: nil is treated as
// "this record kind does not apply", never a panic.
func WithEventsStore(store persistence.EventsStore) AdoptionOption {
	return adoptionOptionFunc(func(a *TenantAdopter) { a.eventsStore = store })
}

// WithSnapshotStore sets the snapshot store TenantAdopter copies snapshot
// records from and to. Omit it for a deployment that does not snapshot.
func WithSnapshotStore(store persistence.SnapshotStore) AdoptionOption {
	return adoptionOptionFunc(func(a *TenantAdopter) { a.snapshotStore = store })
}

// WithStateStore sets the durable-state store TenantAdopter copies
// durable-state records from and to. Omit it for a deployment that has no
// durable-state entities.
//
// See the package doc comment's "Durable-state enumeration limitation"
// section: persistence.StateStore has no PersistenceIDs-style enumeration
// method, so a durable-state-only adoption also needs WithPersistenceIDs.
func WithStateStore(store persistence.StateStore) AdoptionOption {
	return adoptionOptionFunc(func(a *TenantAdopter) { a.stateStore = store })
}

// WithSourceScope overrides the scope TenantAdopter reads existing
// aggregates from. The default is persistence.Unscoped(), the normal case
// of adopting tenancy for the first time; a non-default source scope is
// only useful for re-partitioning an aggregate already under one tenant
// scope into another.
func WithSourceScope(scope persistence.Scope) AdoptionOption {
	return adoptionOptionFunc(func(a *TenantAdopter) { a.sourceScope = scope })
}

// WithScanPageSize sets the page size used to enumerate persistence IDs
// from the events store. Default is 500. Zero is rejected by
// NewTenantAdopter (ErrInvalidScanPageSize): it would enumerate nothing.
func WithScanPageSize(size uint64) AdoptionOption {
	return adoptionOptionFunc(func(a *TenantAdopter) { a.pageSize = size })
}

// WithWriteEnabled turns off dry-run mode: without it, Run only plans and
// reports, writing nothing at all. This is the tool's one required
// opt-in for a real, data-moving run — a migration tool that writes by
// default is a foot-gun.
func WithWriteEnabled() AdoptionOption {
	return adoptionOptionFunc(func(a *TenantAdopter) { a.write = true })
}

// WithSourceDeletion opts in to removing an aggregate's source-scope copy
// once (and only once) its target-scope copy has been read back and
// verified against the FULL record this tool intended to write — a
// proto.Equal match of the source record with tenant_metadata replaced by
// the target tenant's plus its adoption receipt, events matched by
// SequenceNumber rather than slice position or count — not merely a count,
// sequence number, or version
// number. A count/sequence/version-only check cannot detect a truncated
// write, a dropped payload or tenant_metadata, or a missing encryption
// envelope; a full-record match can, and does. Without this option the
// source scope is never modified. A failed verification never deletes,
// regardless of this option.
//
// The source must not receive writes while a WithSourceDeletion run is in
// progress: the SPI offers no atomic read-verify-delete. The run re-reads
// the source before and after each deletion, so a source that gained newer
// records is either left undeleted or reported as failed — never as
// source_deleted — and the newer records always stay in the source; a
// write landing after that final re-read is outside what the tool can see.
//
// Durable state is never deleted by this option: persistence.StateStore has
// no delete method in the SPI (see state_store.go), so a durable-state
// source copy cannot be removed by this tool at all — see the package doc
// comment.
func WithSourceDeletion() AdoptionOption {
	return adoptionOptionFunc(func(a *TenantAdopter) { a.deleteSource = true })
}

// WithFailFast stops Run at the first per-aggregate failure instead of
// collecting it and continuing. The default (unset) is to keep going so
// one bad aggregate does not block an otherwise-successful adoption run.
func WithFailFast() AdoptionOption {
	return adoptionOptionFunc(func(a *TenantAdopter) { a.failFast = true })
}

// WithPersistenceIDs adds explicit persistence IDs to the set TenantAdopter
// scans, in addition to anything the events store enumerates. This is
// REQUIRED (the only source of IDs) whenever no events store is configured:
// persistence.SnapshotStore and persistence.StateStore have no
// PersistenceIDs-style enumeration method in the SPI, so a
// durable-state-only or snapshot-only deployment cannot be discovered on
// its own. See the package doc comment.
func WithPersistenceIDs(ids ...string) AdoptionOption {
	return adoptionOptionFunc(func(a *TenantAdopter) { a.explicitIDs = append(a.explicitIDs, ids...) })
}

// WithAdoptionLogger sets the kit-logger Logger used during adoption, the
// same logging seam Migrator uses. When not set, or when the given logger
// is nil or a typed-nil pointer, TenantAdopter logs through
// ego.DefaultLogger().
func WithAdoptionLogger(logger kitlog.Logger) AdoptionOption {
	return adoptionOptionFunc(func(a *TenantAdopter) { a.logger = logger })
}

// TenantAdopter copies aggregates from a source persistence.Scope (normally
// persistence.Unscoped()) into a per-aggregate target tenant scope, for a
// deployment that already has data and now wants to adopt tenancy. See
// NewTenantAdopter.
type TenantAdopter struct {
	eventsStore   persistence.EventsStore
	snapshotStore persistence.SnapshotStore
	stateStore    persistence.StateStore

	assign      TenantAssignment
	sourceScope persistence.Scope
	explicitIDs []string
	pageSize    uint64

	write        bool
	deleteSource bool
	failFast     bool

	logger kitlog.Logger
}

// NewTenantAdopter creates a TenantAdopter. assign is required and is not an
// option: the framework cannot decide which tenant an existing aggregate
// belongs to, so this business decision must always be supplied explicitly
// (see TenantAssignment). At least one of WithEventsStore, WithSnapshotStore,
// or WithStateStore must also be supplied.
//
// The returned TenantAdopter defaults to dry-run (see WithWriteEnabled) and
// a source scope of persistence.Unscoped() (see WithSourceScope).
func NewTenantAdopter(assign TenantAssignment, opts ...AdoptionOption) (*TenantAdopter, error) {
	if assign == nil {
		return nil, ErrAssignmentRequired
	}

	a := &TenantAdopter{
		assign:      assign,
		sourceScope: persistence.Unscoped(),
		pageSize:    500,
	}
	for _, opt := range opts {
		opt.applyAdoption(a)
	}
	// Options may have set a nil or typed-nil logger, which would panic on
	// the first log call. Resolving after the loop covers every option
	// path, exactly like Migrator's New.
	a.logger = ego.ResolveLogger(a.logger)

	if a.eventsStore == nil && a.snapshotStore == nil && a.stateStore == nil {
		return nil, ErrNoStoresConfigured
	}
	if a.pageSize == 0 {
		return nil, ErrInvalidScanPageSize
	}

	return a, nil
}

// Run scans the source scope for persistence IDs (from the events store's
// enumeration, plus any WithPersistenceIDs), asks TenantAssignment which
// tenant each belongs to, and copies every assigned aggregate's events,
// snapshot, and durable state into its target tenant scope.
//
// Run never returns a non-nil error for a single aggregate's failure unless
// WithFailFast was set: by default every failure is collected into the
// returned AdoptionReport and Run keeps going, returning a nil error once
// every persistence ID has been considered. Run only returns a non-nil
// error for a failure that prevents scanning at all (an unreachable store,
// or a failed PersistenceIDs page).
func (a *TenantAdopter) Run(ctx context.Context) (*AdoptionReport, error) {
	if err := a.pingStores(ctx); err != nil {
		return nil, err
	}

	ids, err := a.collectPersistenceIDs(ctx)
	if err != nil {
		return nil, err
	}

	report := &AdoptionReport{DryRun: !a.write}

	for _, id := range ids {
		report.Scanned++

		if stop, failure := a.processAggregate(ctx, id, report); failure != nil {
			if stop {
				return report, failure
			}
		}
	}

	a.logger.InfoContext(ctx, "tenant adoption: completed",
		"dry_run", report.DryRun,
		"scanned", report.Scanned,
		"assigned", report.Assigned,
		"skipped_by_assignment", report.SkippedByAssignment,
		"copied", report.Copied,
		"already_present", report.AlreadyPresent,
		"verified", report.Verified,
		"source_deleted", report.SourceDeleted,
		"failed", report.Failed,
	)

	return report, nil
}

// processAggregate handles one persistence ID end to end: assignment,
// target-scope resolution, and per-kind adoption. It returns the last
// recorded failure (nil when the id completed without one) and whether
// WithFailFast requires Run to stop immediately.
func (a *TenantAdopter) processAggregate(ctx context.Context, id string, report *AdoptionReport) (stop bool, failure error) {
	tenantID, ok, err := a.assign(ctx, id)
	if err != nil {
		f := AdoptionFailure{PersistenceID: id, Err: fmt.Errorf("tenant assignment: %w", err)}
		report.Failed++
		report.Failures = append(report.Failures, f)
		a.logger.ErrorContext(ctx, "tenant adoption: assignment failed", "persistence_id", id, "error", err)
		return a.failFast, f
	}
	if !ok {
		report.SkippedByAssignment++
		return false, nil
	}
	report.Assigned++

	target, err := persistence.NewTenantScope(tenantID)
	if err != nil {
		f := AdoptionFailure{PersistenceID: id, Err: fmt.Errorf("assigned tenant %q is not a valid target scope: %w", tenantID, err)}
		report.Failed++
		report.Failures = append(report.Failures, f)
		return a.failFast, f
	}

	tenantContext, err := tenancy.NewTenantContext(target.TenantID())
	if err != nil {
		// Unreachable in practice: NewTenantScope above already validated
		// the same tenancy.TenantID through the same rules
		// (tenancy.NewTenantID). Handled anyway rather than assumed, per
		// R1 (validate, don't assume).
		f := AdoptionFailure{PersistenceID: id, Err: fmt.Errorf("building tenant context for %q: %w", tenantID, err)}
		report.Failed++
		report.Failures = append(report.Failures, f)
		return a.failFast, f
	}
	metadata := tenancy.MarshalMetadata(tenantContext)

	intent := adoptionIntent{target: target, tenant: tenantContext, metadata: metadata}
	outcome := AggregateOutcome{PersistenceID: id, Tenant: tenantID}
	found, copied, aggFailure := false, false, error(nil)

	if a.eventsStore != nil {
		outcome.Events, aggFailure = a.applyKind(ctx, KindEvents, id, intent, report, a.adoptEvents)
		found = found || outcome.Events.Status != StatusNone
		copied = copied || outcome.Events.wroteTarget
	}

	if aggFailure == nil && a.snapshotStore != nil {
		outcome.Snapshot, aggFailure = a.applyKind(ctx, KindSnapshot, id, intent, report, a.adoptSnapshot)
		found = found || outcome.Snapshot.Status != StatusNone
		copied = copied || outcome.Snapshot.wroteTarget
	}

	if aggFailure == nil && a.stateStore != nil {
		outcome.DurableState, aggFailure = a.applyKind(ctx, KindDurableState, id, intent, report, a.adoptState)
		found = found || outcome.DurableState.Status != StatusNone
		copied = copied || outcome.DurableState.wroteTarget
	}

	// A record kind only ever reaches StatusCopied or StatusSourceDeleted
	// in a real (non-dry-run) run AFTER its own read-back verification
	// succeeded (adoptEvents/adoptSnapshot/adoptState return StatusFailed
	// otherwise) — so "copied" already means "verified" whenever a.write
	// is true. In dry-run, copied instead reflects the plan and nothing
	// was actually verified or deleted.
	deleted := outcome.Events.Status == StatusSourceDeleted || outcome.Snapshot.Status == StatusSourceDeleted || outcome.DurableState.Status == StatusSourceDeleted

	report.Aggregates = append(report.Aggregates, outcome)

	switch {
	case aggFailure != nil:
		report.Failed++
		return a.failFast, aggFailure
	case !found:
		f := AdoptionFailure{PersistenceID: id, Err: errNoSourceRecords}
		report.Failed++
		report.Failures = append(report.Failures, f)
		return a.failFast, f
	case copied:
		report.Copied++
		if !a.write {
			// Dry-run: Copied reflects the plan, but nothing was actually
			// written, so nothing was actually verified or deleted.
			return false, nil
		}
		report.Verified++
		if deleted {
			report.SourceDeleted++
		}
		return false, nil
	default:
		report.AlreadyPresent++
		if deleted {
			// The target was already the exact adoption, and this
			// WithSourceDeletion run removed the verified source.
			report.SourceDeleted++
		}
		return false, nil
	}
}

// adoptionIntent is what one aggregate's adoption writes: the target tenant
// scope, the tenant it belongs to, and the tenant_metadata stamped on every
// copied record.
type adoptionIntent struct {
	target   persistence.Scope
	tenant   tenancy.TenantContext
	metadata map[string]string
}

// kindAdopter is the shape shared by adoptEvents, adoptSnapshot, and
// adoptState.
type kindAdopter func(ctx context.Context, id string, intent adoptionIntent) RecordOutcome

// applyKind runs adopt for one record kind and, on RecordStatus StatusFailed,
// records an AdoptionFailure against report.
func (a *TenantAdopter) applyKind(ctx context.Context, kind RecordKind, id string, intent adoptionIntent, report *AdoptionReport, adopt kindAdopter) (RecordOutcome, error) {
	outcome := adopt(ctx, id, intent)
	if outcome.Status == StatusFailed {
		f := AdoptionFailure{PersistenceID: id, Kind: kind, Err: outcome.Err}
		report.Failures = append(report.Failures, f)
		a.logger.ErrorContext(ctx, "tenant adoption: record kind failed", "persistence_id", id, "kind", string(kind), "error", outcome.Err)
		return outcome, f
	}
	return outcome, nil
}

// adoptEvents copies persistence ID id's events from the source scope into
// the target. See the package doc comment for the overall algorithm. The
// target is read first: when it already holds events they are classified by
// verifyEventsEquivalent (already present or fail closed) and nothing is
// written — this is what keeps a re-run after WithSourceDeletion, whose
// source is now empty, a no-op.
func (a *TenantAdopter) adoptEvents(ctx context.Context, id string, intent adoptionIntent) RecordOutcome {
	sourceEvents, err := a.eventsStore.ReplayEvents(ctx, a.sourceScope, id, 1, maxReplayLimit, maxReplayLimit)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("read source events: %w", err)}
	}

	expected := make([]*egopb.Event, len(sourceEvents))
	var maxSeq uint64
	for i, evt := range sourceEvents {
		clone, ok := proto.Clone(evt).(*egopb.Event)
		if !ok {
			return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("clone event at sequence %d: unexpected cloned type", evt.GetSequenceNumber())}
		}
		if err := stampAdoptionReceipt(clone, a.sourceScope, intent.metadata); err != nil {
			return RecordOutcome{Status: StatusFailed, Err: err}
		}
		expected[i] = clone
		if seq := clone.GetSequenceNumber(); seq > maxSeq {
			maxSeq = seq
		}
	}

	existing, err := a.eventsStore.ReplayEvents(ctx, intent.target, id, 1, maxReplayLimit, maxReplayLimit)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("check target events: %w", err)}
	}
	if len(existing) > 0 {
		return a.afterVerifiedEvents(ctx, id, sourceEvents, maxSeq, verifyEventsEquivalent(id, expected, existing, intent.tenant, a.sourceScope))
	}

	if len(sourceEvents) == 0 {
		return RecordOutcome{Status: StatusNone}
	}
	if !a.write {
		return RecordOutcome{Status: StatusCopied, wroteTarget: true}
	}

	if err := a.eventsStore.WriteEvents(ctx, intent.target, expected, persistence.ExpectGenesis()); err != nil {
		var conflict *persistence.ConflictError
		if !errors.As(err, &conflict) {
			return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("write target events: %w", err)}
		}
		// Another writer created the target between the check above and
		// this write: classify what it wrote instead of trusting it.
		raced, err := a.eventsStore.ReplayEvents(ctx, intent.target, id, 1, maxReplayLimit, maxReplayLimit)
		if err != nil {
			return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("check target events: %w", err)}
		}
		return a.afterVerifiedEvents(ctx, id, sourceEvents, maxSeq, verifyEventsEquivalent(id, expected, raced, intent.tenant, a.sourceScope))
	}

	written, err := a.eventsStore.ReplayEvents(ctx, intent.target, id, 1, maxReplayLimit, maxReplayLimit)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("verify target events: %w", err)}
	}
	if err := verifyEventsMatchBySequence(id, expected, written); err != nil {
		return RecordOutcome{Status: StatusFailed, Err: err}
	}

	if a.deleteSource {
		return a.deleteVerifiedSourceEvents(ctx, id, sourceEvents, maxSeq, true)
	}

	return RecordOutcome{Status: StatusCopied, wroteTarget: true}
}

// adoptSnapshot copies persistence ID id's latest snapshot from the source
// scope into target.
//
// Unlike events and durable state, persistence.SnapshotStore.WriteSnapshot
// takes no persistence.WritePrecondition (see snapshot_store.go): there is
// no atomic "write only if absent" for snapshots in the SPI. adoptSnapshot
// therefore reads the target first and skips the write if anything is
// already there. This has an unavoidable (read, then write) window — the
// best the current SPI allows — rather than the atomic ExpectGenesis
// guarantee events and durable state get.
func (a *TenantAdopter) adoptSnapshot(ctx context.Context, id string, intent adoptionIntent) RecordOutcome {
	snapshot, err := a.snapshotStore.GetLatestSnapshot(ctx, a.sourceScope, id)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("read source snapshot: %w", err)}
	}

	var clone *egopb.Snapshot
	if snapshot != nil {
		var ok bool
		clone, ok = proto.Clone(snapshot).(*egopb.Snapshot)
		if !ok {
			return RecordOutcome{Status: StatusFailed, Err: errors.New("clone snapshot: unexpected cloned type")}
		}
		if err := stampAdoptionReceipt(clone, a.sourceScope, intent.metadata); err != nil {
			return RecordOutcome{Status: StatusFailed, Err: err}
		}
	}

	existing, err := a.snapshotStore.GetLatestSnapshot(ctx, intent.target, id)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("check target snapshot: %w", err)}
	}
	if existing != nil {
		var expected proto.Message
		if clone != nil {
			expected = clone
		}
		outcome := classifyExisting(verifyRecordEquivalent(id, KindSnapshot, expected, clone.GetSequenceNumber(), existing, existing.GetSequenceNumber(), existing.GetTenantMetadata(), intent.tenant, a.sourceScope))
		if outcome.Status == StatusAlreadyPresent && snapshot != nil && a.write && a.deleteSource {
			return a.deleteVerifiedSourceSnapshot(ctx, id, snapshot, false)
		}
		return outcome
	}

	if snapshot == nil {
		return RecordOutcome{Status: StatusNone}
	}
	if !a.write {
		return RecordOutcome{Status: StatusCopied, wroteTarget: true}
	}

	if err := a.snapshotStore.WriteSnapshot(ctx, intent.target, clone); err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("write target snapshot: %w", err)}
	}

	written, err := a.snapshotStore.GetLatestSnapshot(ctx, intent.target, id)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("verify target snapshot: %w", err)}
	}
	// Full-record match, not a sequence-number proxy: clone is exactly the
	// record this tool intended to write (the source snapshot with
	// tenant_metadata replaced by target's plus its adoption receipt), so
	// anything proto.Equal disagrees on — payload, tenant_metadata,
	// timestamps, encryption
	// envelope — means the store did not durably persist what was written,
	// and the source must not be deleted.
	if written == nil || !proto.Equal(clone, written) {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("verify target snapshot for persistence_id %q: read-back does not match what was written (expected sequence %d)", id, snapshot.GetSequenceNumber())}
	}

	if a.deleteSource {
		return a.deleteVerifiedSourceSnapshot(ctx, id, snapshot, true)
	}

	return RecordOutcome{Status: StatusCopied, wroteTarget: true}
}

// adoptState copies persistence ID id's latest durable state from the
// source scope into target.
//
// Source deletion never applies to durable state, regardless of
// WithSourceDeletion: persistence.StateStore has no delete method at all
// (see state_store.go) — there is nothing this tool could call. See the
// package doc comment.
func (a *TenantAdopter) adoptState(ctx context.Context, id string, intent adoptionIntent) RecordOutcome {
	state, err := a.stateStore.GetLatestState(ctx, a.sourceScope, id)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("read source state: %w", err)}
	}

	var clone *egopb.DurableState
	if state != nil {
		var ok bool
		clone, ok = proto.Clone(state).(*egopb.DurableState)
		if !ok {
			return RecordOutcome{Status: StatusFailed, Err: errors.New("clone durable state: unexpected cloned type")}
		}
		if err := stampAdoptionReceipt(clone, a.sourceScope, intent.metadata); err != nil {
			return RecordOutcome{Status: StatusFailed, Err: err}
		}
	}

	classifyTarget := func() RecordOutcome {
		existing, err := a.stateStore.GetLatestState(ctx, intent.target, id)
		if err != nil {
			return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("check target state: %w", err)}
		}
		if existing == nil {
			return RecordOutcome{Status: StatusNone}
		}
		var expected proto.Message
		if clone != nil {
			expected = clone
		}
		return classifyExisting(verifyRecordEquivalent(id, KindDurableState, expected, clone.GetVersionNumber(), existing, existing.GetVersionNumber(), existing.GetTenantMetadata(), intent.tenant, a.sourceScope))
	}

	if outcome := classifyTarget(); outcome.Status != StatusNone {
		return outcome
	}
	if state == nil {
		return RecordOutcome{Status: StatusNone}
	}
	if !a.write {
		return RecordOutcome{Status: StatusCopied, wroteTarget: true}
	}

	if err := a.stateStore.WriteState(ctx, intent.target, clone, persistence.ExpectGenesis()); err != nil {
		var conflict *persistence.ConflictError
		if !errors.As(err, &conflict) {
			return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("write target state: %w", err)}
		}
		// Another writer created the target between the check above and
		// this write: classify what it wrote instead of trusting it.
		if outcome := classifyTarget(); outcome.Status != StatusNone {
			return outcome
		}
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("write target state: %w", err)}
	}

	written, err := a.stateStore.GetLatestState(ctx, intent.target, id)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("verify target state: %w", err)}
	}
	// Full-record match, not a version-number proxy: clone is exactly the
	// record this tool intended to write. adoptState never deletes (there is
	// no delete method on persistence.StateStore), but its verification
	// still feeds AdoptionReport.Verified, so it must be just as strict as
	// the events/snapshot checks above.
	if written == nil || !proto.Equal(clone, written) {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("verify target state for persistence_id %q: read-back does not match what was written (expected version %d)", id, state.GetVersionNumber())}
	}

	return RecordOutcome{Status: StatusCopied, wroteTarget: true}
}

// verifyEventsMatchBySequence proves that written — the target scope's
// read-back — is exactly expected, matched by SequenceNumber rather than
// slice position or count: a store that returns events in a different order
// would still pass a position-based comparison, and a store that corrupts a
// payload while preserving the count would still pass the old
// len(written) != len(sourceEvents) check. Every expected event must be
// present in written under the same sequence number and proto.Equal it
// exactly; no extra sequence numbers may appear in written; a sequence
// number is never expected more than once (WriteEvents already guards a
// batch to one persistence_id, and eGo's own event log de-duplicates by
// sequence number — see testkit/eventstore.go's newEventLog). On any
// mismatch the returned error names id and the sequence number that
// differed, so the caller never deletes based on a check that cannot detect
// corruption.
func verifyEventsMatchBySequence(id string, expected, written []*egopb.Event) error {
	expectedBySeq := make(map[uint64]*egopb.Event, len(expected))
	for _, e := range expected {
		expectedBySeq[e.GetSequenceNumber()] = e
	}
	writtenBySeq := make(map[uint64]*egopb.Event, len(written))
	for _, e := range written {
		writtenBySeq[e.GetSequenceNumber()] = e
	}

	if len(writtenBySeq) != len(written) {
		return fmt.Errorf("verify target events for persistence_id %q: read-back carries more than one row for a sequence number", id)
	}
	if len(writtenBySeq) != len(expectedBySeq) {
		return fmt.Errorf("verify target events for persistence_id %q: wrote %d distinct sequence numbers, read back %d", id, len(expectedBySeq), len(writtenBySeq))
	}
	for seq, want := range expectedBySeq {
		got, ok := writtenBySeq[seq]
		if !ok {
			return fmt.Errorf("verify target events for persistence_id %q: sequence %d is missing from the target read-back", id, seq)
		}
		if !proto.Equal(want, got) {
			return fmt.Errorf("verify target events for persistence_id %q: sequence %d does not match what was written", id, seq)
		}
	}
	return nil
}

// afterVerifiedEvents turns the verdict on an events target that already
// existed into an outcome. A target proven to contain the source exactly is
// already present, and a write-enabled WithSourceDeletion run then deletes
// that verified source under the same guard as a fresh copy.
func (a *TenantAdopter) afterVerifiedEvents(ctx context.Context, id string, sourceEvents []*egopb.Event, maxSeq uint64, verdict error) RecordOutcome {
	outcome := classifyExisting(verdict)
	if outcome.Status == StatusAlreadyPresent && len(sourceEvents) > 0 && a.write && a.deleteSource {
		return a.deleteVerifiedSourceEvents(ctx, id, sourceEvents, maxSeq, false)
	}
	return outcome
}

// deleteVerifiedSourceEvents deletes the source events this run verified
// (sourceEvents, through maxSeq). Nothing makes read-verify-delete atomic in
// the SPI, so the source is re-read on both sides of the deletion: before
// it, the source must still be exactly sourceEvents — no newer event, and no
// event rewritten at the same sequence number — or nothing is deleted;
// after it, a source that gained an event during the delete is reported as
// failed, never as source_deleted. The newer or rewritten events always stay
// in the source. A write landing after the final re-read is outside what
// this tool can observe; WithSourceDeletion requires a quiesced source.
func (a *TenantAdopter) deleteVerifiedSourceEvents(ctx context.Context, id string, sourceEvents []*egopb.Event, maxSeq uint64, wroteTarget bool) RecordOutcome {
	current, err := a.eventsStore.ReplayEvents(ctx, a.sourceScope, id, 1, maxReplayLimit, maxReplayLimit)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("re-read source events: %w", err)}
	}
	if len(current) != len(sourceEvents) {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("%w: persistence_id %q kind %s: source holds %d events, %d were verified; nothing was deleted", errSourceChangedDuringAdoption, id, KindEvents, len(current), len(sourceEvents))}
	}
	for i := range current {
		if !proto.Equal(current[i], sourceEvents[i]) {
			return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("%w: persistence_id %q kind %s: source sequence %d changed after it was verified; nothing was deleted", errSourceChangedDuringAdoption, id, KindEvents, current[i].GetSequenceNumber())}
		}
	}

	if err := a.eventsStore.DeleteEvents(ctx, a.sourceScope, id, maxSeq); err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("delete source events: %w", err)}
	}

	latest, err := a.eventsStore.GetLatestEvent(ctx, a.sourceScope, id)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("re-read source events: %w", err)}
	}
	if latest != nil && latest.GetSequenceNumber() > maxSeq {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("%w: persistence_id %q kind %s: source gained sequence %d during the deletion; the verified events through %d were deleted, but the newer ones remain only in the source", errSourceChangedDuringAdoption, id, KindEvents, latest.GetSequenceNumber(), maxSeq)}
	}
	return RecordOutcome{Status: StatusSourceDeleted, wroteTarget: wroteTarget}
}

// deleteVerifiedSourceSnapshot is deleteVerifiedSourceEvents for the source
// snapshot this run verified: before deleting, the source's latest snapshot
// must still be exactly verified (a snapshot rewritten at the same sequence
// number fails, not just a newer one); after, a newer snapshot written
// during the delete is reported as failed.
func (a *TenantAdopter) deleteVerifiedSourceSnapshot(ctx context.Context, id string, verified *egopb.Snapshot, wroteTarget bool) RecordOutcome {
	current, err := a.snapshotStore.GetLatestSnapshot(ctx, a.sourceScope, id)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("re-read source snapshot: %w", err)}
	}
	if !proto.Equal(current, verified) {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("%w: persistence_id %q kind %s: the source snapshot changed after it was verified (verified sequence %d); nothing was deleted", errSourceChangedDuringAdoption, id, KindSnapshot, verified.GetSequenceNumber())}
	}

	if err := a.snapshotStore.DeleteSnapshots(ctx, a.sourceScope, id, verified.GetSequenceNumber()); err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("delete source snapshot: %w", err)}
	}

	latest, err := a.snapshotStore.GetLatestSnapshot(ctx, a.sourceScope, id)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("re-read source snapshot: %w", err)}
	}
	if latest != nil && latest.GetSequenceNumber() > verified.GetSequenceNumber() {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("%w: persistence_id %q kind %s: source gained a snapshot at %d during the deletion; the verified snapshot at %d was deleted, but the newer one remains only in the source", errSourceChangedDuringAdoption, id, KindSnapshot, latest.GetSequenceNumber(), verified.GetSequenceNumber())}
	}
	return RecordOutcome{Status: StatusSourceDeleted, wroteTarget: wroteTarget}
}

// classifyExisting turns the equivalence verdict on a pre-existing target
// into a RecordOutcome: equivalent is StatusAlreadyPresent, anything else
// fails closed.
func classifyExisting(err error) RecordOutcome {
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: err}
	}
	return RecordOutcome{Status: StatusAlreadyPresent}
}

// ownedByTenant reports whether metadata decodes to exactly tenant.
func ownedByTenant(metadata map[string]string, tenant tenancy.TenantContext) bool {
	decoded, err := tenancy.UnmarshalMetadata(tenancy.Metadata(metadata))
	return err == nil && tenancy.VerifyUnchanged(tenant, decoded) == nil
}

// verifyEventsEquivalent decides whether the events a target scope already
// holds are an adoption of expected (the stamped copies of the source
// events; empty when the source is gone). Every target event must be owned
// by tenant and carry a distinct sequence number.
//
// While the source exists, every expected event must be present under its
// sequence number and proto.Equal it, and any other target event must come
// after all of them: the tenant-bound actor appends to the adopted stream,
// it never rewrites its past. An event stream can prove containment of the
// source this way; a single latest snapshot or state record cannot (see
// verifyRecordEquivalent).
//
// Once the source is gone there is nothing left to compare with, so the
// proof is the adoption receipt: the target's lowest sequence numbers must be
// an unbroken run of events whose receipts are valid for sourceScope, and
// only later events may lack one. Same-tenant events that no adoption wrote
// never carry a valid receipt, so they are never mistaken for an adoption.
func verifyEventsEquivalent(id string, expected, existing []*egopb.Event, tenant tenancy.TenantContext, sourceScope persistence.Scope) error {
	notEquivalent := func(format string, args ...any) error {
		return fmt.Errorf("%w: persistence_id %q kind %s: %s", errTargetNotEquivalent, id, KindEvents, fmt.Sprintf(format, args...))
	}

	existingBySeq := make(map[uint64]*egopb.Event, len(existing))
	seqs := make([]uint64, 0, len(existing))
	for _, e := range existing {
		seq := e.GetSequenceNumber()
		if _, dup := existingBySeq[seq]; dup {
			return notEquivalent("sequence %d appears more than once", seq)
		}
		if !ownedByTenant(e.GetTenantMetadata(), tenant) {
			return notEquivalent("sequence %d is not owned by the assigned tenant", seq)
		}
		existingBySeq[seq] = e
		seqs = append(seqs, seq)
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })

	if len(expected) > 0 {
		var maxExpected uint64
		expectedSeqs := make(map[uint64]struct{}, len(expected))
		for _, want := range expected {
			seq := want.GetSequenceNumber()
			got, ok := existingBySeq[seq]
			if !ok || !proto.Equal(want, got) {
				return notEquivalent("sequence %d differs from the source", seq)
			}
			expectedSeqs[seq] = struct{}{}
			if seq > maxExpected {
				maxExpected = seq
			}
		}
		for _, seq := range seqs {
			if _, adopted := expectedSeqs[seq]; !adopted && seq < maxExpected {
				return notEquivalent("sequence %d is not part of the source stream", seq)
			}
		}
		return nil
	}

	adopted := 0
	for i, seq := range seqs {
		if _, hasReceipt := existingBySeq[seq].GetTenantMetadata()[adoptionReceiptKey]; !hasReceipt {
			break
		}
		if i > 0 && seq != seqs[i-1]+1 {
			return notEquivalent("adopted sequence %d does not follow %d", seq, seqs[i-1])
		}
		if !hasValidAdoptionReceipt(existingBySeq[seq], sourceScope) {
			return notEquivalent("sequence %d does not match its adoption receipt", seq)
		}
		adopted++
	}
	if adopted == 0 {
		return notEquivalent("the source is gone and no target event carries an adoption receipt, so an adoption cannot be proven")
	}
	for _, seq := range seqs[adopted:] {
		if _, hasReceipt := existingBySeq[seq].GetTenantMetadata()[adoptionReceiptKey]; hasReceipt {
			return notEquivalent("adopted sequence %d follows events written after adoption", seq)
		}
	}
	return nil
}

// verifyRecordEquivalent decides whether the single latest snapshot or
// durable-state record a target holds is an adoption of expected (the
// stamped copy of the source record; nil when the source is gone).
//
// A latest record keeps no history, so it can only prove equivalence by
// being the exact record this adoption writes. While the source exists that
// means the same position and proto.Equal; an earlier position, a different
// record at the same position, and a LATER position all fail closed — a
// later record owned by the same tenant may be unrelated data, and nothing
// in it proves it descends from this source. Once the source is gone, the
// record must still carry a valid adoption receipt for sourceScope; a record
// the tenant-bound actor rewrote after adoption carries none, so the re-run
// fails closed rather than presuming the adoption happened.
func verifyRecordEquivalent(id string, kind RecordKind, expected proto.Message, expectedPos uint64, existing proto.Message, existingPos uint64, existingMetadata map[string]string, tenant tenancy.TenantContext, sourceScope persistence.Scope) error {
	if !ownedByTenant(existingMetadata, tenant) {
		return fmt.Errorf("%w: persistence_id %q kind %s: target record is not owned by the assigned tenant", errTargetNotEquivalent, id, kind)
	}
	if expected == nil {
		if !hasValidAdoptionReceipt(existing, sourceScope) {
			return fmt.Errorf("%w: persistence_id %q kind %s: the source is gone and the target record at %d carries no valid adoption receipt, so an adoption cannot be proven", errTargetNotEquivalent, id, kind, existingPos)
		}
		return nil
	}
	if existingPos != expectedPos || !proto.Equal(expected, existing) {
		return fmt.Errorf("%w: persistence_id %q kind %s: target record at %d is not the exact adoption of the source at %d", errTargetNotEquivalent, id, kind, existingPos, expectedPos)
	}
	return nil
}

// stampAdoptionReceipt sets record's tenant_metadata to a fresh copy of
// metadata plus the adoption receipt computed over that stamped record. A
// fresh copy per record keeps one record's receipt out of another's map.
func stampAdoptionReceipt(record proto.Message, sourceScope persistence.Scope, metadata map[string]string) error {
	stamped := make(map[string]string, len(metadata)+1)
	for key, value := range metadata {
		stamped[key] = value
	}
	if !setTenantMetadata(record, stamped) {
		return fmt.Errorf("stamp adoption receipt: unsupported record type %T", record)
	}
	receipt, err := adoptionReceipt(record, sourceScope)
	if err != nil {
		return err
	}
	stamped[adoptionReceiptKey] = receipt
	return nil
}

// hasValidAdoptionReceipt reports whether record carries a receipt that
// matches its own content and sourceScope.
func hasValidAdoptionReceipt(record proto.Message, sourceScope persistence.Scope) bool {
	stripped := proto.Clone(record)
	metadata := tenantMetadataOf(stripped)
	receipt, ok := metadata[adoptionReceiptKey]
	if !ok {
		return false
	}
	withoutReceipt := make(map[string]string, len(metadata))
	for key, value := range metadata {
		if key != adoptionReceiptKey {
			withoutReceipt[key] = value
		}
	}
	if !setTenantMetadata(stripped, withoutReceipt) {
		return false
	}
	want, err := adoptionReceipt(stripped, sourceScope)
	return err == nil && want == receipt
}

// adoptionReceipt is the receipt value for record (which must not carry a
// receipt itself) adopted from sourceScope. It hashes the scope's kind and
// tenant id, not Scope.String(), followed by the record's deterministic
// protobuf encoding. If that encoding ever changed between the run that
// wrote a receipt and the run that checks it, the check fails closed.
func adoptionReceipt(record proto.Message, sourceScope persistence.Scope) (string, error) {
	encoded, err := proto.MarshalOptions{Deterministic: true}.Marshal(record)
	if err != nil {
		return "", fmt.Errorf("compute adoption receipt: %w", err)
	}
	scopeToken := "unscoped"
	if !sourceScope.IsUnscoped() {
		scopeToken = "tenant:" + strconv.Quote(string(sourceScope.TenantID()))
	}
	sum := sha256.New()
	sum.Write([]byte(scopeToken))
	sum.Write([]byte{0})
	sum.Write(encoded)
	return adoptionReceiptVersion + hex.EncodeToString(sum.Sum(nil)), nil
}

// tenantMetadataOf returns record's tenant_metadata for the three record
// kinds TenantAdopter copies.
func tenantMetadataOf(record proto.Message) map[string]string {
	switch r := record.(type) {
	case *egopb.Event:
		return r.GetTenantMetadata()
	case *egopb.Snapshot:
		return r.GetTenantMetadata()
	case *egopb.DurableState:
		return r.GetTenantMetadata()
	default:
		return nil
	}
}

// setTenantMetadata replaces record's tenant_metadata and reports whether
// record is one of the three kinds TenantAdopter copies.
func setTenantMetadata(record proto.Message, metadata map[string]string) bool {
	switch r := record.(type) {
	case *egopb.Event:
		r.TenantMetadata = metadata
	case *egopb.Snapshot:
		r.TenantMetadata = metadata
	case *egopb.DurableState:
		r.TenantMetadata = metadata
	default:
		return false
	}
	return true
}

// pingStores preflights every configured store, exactly like Migrator.Run.
func (a *TenantAdopter) pingStores(ctx context.Context) error {
	if a.eventsStore != nil {
		if err := a.eventsStore.Ping(ctx); err != nil {
			return fmt.Errorf("migration: events store not reachable: %w", err)
		}
	}
	if a.snapshotStore != nil {
		if err := a.snapshotStore.Ping(ctx); err != nil {
			return fmt.Errorf("migration: snapshot store not reachable: %w", err)
		}
	}
	if a.stateStore != nil {
		if err := a.stateStore.Ping(ctx); err != nil {
			return fmt.Errorf("migration: state store not reachable: %w", err)
		}
	}
	return nil
}

// collectPersistenceIDs enumerates every distinct persistence ID to scan:
// every page the events store reports for a.sourceScope (when an events
// store is configured), plus every id from WithPersistenceIDs.
//
// See the package doc comment's "Durable-state enumeration limitation":
// persistence.SnapshotStore and persistence.StateStore have no
// PersistenceIDs-style method, so when a.eventsStore is nil, WithPersistenceIDs
// is the ONLY source of ids — collectPersistenceIDs logs a warning rather
// than silently scanning nothing when that leaves it with an empty set.
func (a *TenantAdopter) collectPersistenceIDs(ctx context.Context) ([]string, error) {
	seen := make(map[string]struct{})
	var ids []string
	add := func(id string) {
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}

	if a.eventsStore != nil {
		var pageToken string
		for {
			page, nextToken, err := a.eventsStore.PersistenceIDs(ctx, a.sourceScope, a.pageSize, pageToken)
			if err != nil {
				return nil, fmt.Errorf("migration: failed to list persistence IDs: %w", err)
			}
			for _, id := range page {
				add(id)
			}
			if nextToken == "" || len(page) == 0 {
				break
			}
			pageToken = nextToken
		}
	} else if len(a.explicitIDs) == 0 {
		a.logger.WarnContext(ctx, "tenant adoption: no events store configured and no explicit persistence IDs supplied; nothing to scan (see the migration package doc's enumeration limitation)")
	}

	for _, id := range a.explicitIDs {
		add(id)
	}

	return ids, nil
}
