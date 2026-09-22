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
	"errors"
	"fmt"

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

// errNoSourceRecords is the per-aggregate failure recorded when an id was
// assigned a tenant but none of the configured stores actually held any
// record for it in the source scope — most often a caller mistake in the
// TenantAssignment (e.g. a stale or misspelled persistence ID supplied via
// WithPersistenceIDs).
var errNoSourceRecords = errors.New("migration: no source record found in any configured store for this persistence id")

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
	// record for this (kind, persistence ID); nothing was written, and
	// nothing was overwritten.
	StatusAlreadyPresent RecordStatus = "already_present"
	// StatusSourceDeleted means the record was copied, the copy was read
	// back and verified, and the source-scope copy was then removed
	// (WithSourceDeletion only).
	StatusSourceDeleted RecordStatus = "source_deleted"
	// StatusFailed means an unexpected error occurred while adopting this
	// record kind. See RecordOutcome.Err for detail.
	StatusFailed RecordStatus = "failed"
)

// RecordOutcome is the per-kind detail behind one AggregateOutcome.
type RecordOutcome struct {
	Status RecordStatus
	Err    error
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
	// was read back and matched the source. Always 0 in dry-run.
	Verified int
	// SourceDeleted is the number of aggregates for which at least one
	// record kind's source-scope copy was removed after a verified copy
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
// from the events store. Default is 500.
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
// verified. Without this option the source scope is never modified. A
// failed verification never deletes, regardless of this option.
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

	outcome := AggregateOutcome{PersistenceID: id, Tenant: tenantID}
	found, copied, aggFailure := false, false, error(nil)

	if a.eventsStore != nil {
		outcome.Events, aggFailure = a.applyKind(ctx, KindEvents, id, target, metadata, report, a.adoptEvents)
		found = found || outcome.Events.Status != StatusNone
		copied = copied || outcome.Events.Status == StatusCopied || outcome.Events.Status == StatusSourceDeleted
	}

	if aggFailure == nil && a.snapshotStore != nil {
		outcome.Snapshot, aggFailure = a.applyKind(ctx, KindSnapshot, id, target, metadata, report, a.adoptSnapshot)
		found = found || outcome.Snapshot.Status != StatusNone
		copied = copied || outcome.Snapshot.Status == StatusCopied || outcome.Snapshot.Status == StatusSourceDeleted
	}

	if aggFailure == nil && a.stateStore != nil {
		outcome.DurableState, aggFailure = a.applyKind(ctx, KindDurableState, id, target, metadata, report, a.adoptState)
		found = found || outcome.DurableState.Status != StatusNone
		copied = copied || outcome.DurableState.Status == StatusCopied || outcome.DurableState.Status == StatusSourceDeleted
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
		return false, nil
	}
}

// kindAdopter is the shape shared by adoptEvents, adoptSnapshot, and
// adoptState.
type kindAdopter func(ctx context.Context, id string, target persistence.Scope, metadata map[string]string) RecordOutcome

// applyKind runs adopt for one record kind and, on RecordStatus StatusFailed,
// records an AdoptionFailure against report.
func (a *TenantAdopter) applyKind(ctx context.Context, kind RecordKind, id string, target persistence.Scope, metadata map[string]string, report *AdoptionReport, adopt kindAdopter) (RecordOutcome, error) {
	outcome := adopt(ctx, id, target, metadata)
	if outcome.Status == StatusFailed {
		f := AdoptionFailure{PersistenceID: id, Kind: kind, Err: outcome.Err}
		report.Failures = append(report.Failures, f)
		a.logger.ErrorContext(ctx, "tenant adoption: record kind failed", "persistence_id", id, "kind", string(kind), "error", outcome.Err)
		return outcome, f
	}
	return outcome, nil
}

// adoptEvents copies persistence ID id's events from the source scope into
// target. See the package doc comment for the overall algorithm.
func (a *TenantAdopter) adoptEvents(ctx context.Context, id string, target persistence.Scope, metadata map[string]string) RecordOutcome {
	sourceEvents, err := a.eventsStore.ReplayEvents(ctx, a.sourceScope, id, 1, maxReplayLimit, maxReplayLimit)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("read source events: %w", err)}
	}
	if len(sourceEvents) == 0 {
		return RecordOutcome{Status: StatusNone}
	}

	if !a.write {
		existing, err := a.eventsStore.GetLatestEvent(ctx, target, id)
		if err != nil {
			return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("check target events: %w", err)}
		}
		if existing != nil {
			return RecordOutcome{Status: StatusAlreadyPresent}
		}
		return RecordOutcome{Status: StatusCopied}
	}

	cloned := make([]*egopb.Event, len(sourceEvents))
	var maxSeq uint64
	for i, evt := range sourceEvents {
		clone, ok := proto.Clone(evt).(*egopb.Event)
		if !ok {
			return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("clone event at sequence %d: unexpected cloned type", evt.GetSequenceNumber())}
		}
		clone.TenantMetadata = metadata
		cloned[i] = clone
		if seq := clone.GetSequenceNumber(); seq > maxSeq {
			maxSeq = seq
		}
	}

	if err := a.eventsStore.WriteEvents(ctx, target, cloned, persistence.ExpectGenesis()); err != nil {
		var conflict *persistence.ConflictError
		if errors.As(err, &conflict) {
			return RecordOutcome{Status: StatusAlreadyPresent}
		}
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("write target events: %w", err)}
	}

	written, err := a.eventsStore.ReplayEvents(ctx, target, id, 1, maxReplayLimit, maxReplayLimit)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("verify target events: %w", err)}
	}
	if len(written) != len(sourceEvents) {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("verify target events: wrote %d, read back %d", len(sourceEvents), len(written))}
	}

	if a.deleteSource {
		if err := a.eventsStore.DeleteEvents(ctx, a.sourceScope, id, maxSeq); err != nil {
			return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("delete source events: %w", err)}
		}
		return RecordOutcome{Status: StatusSourceDeleted}
	}

	return RecordOutcome{Status: StatusCopied}
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
func (a *TenantAdopter) adoptSnapshot(ctx context.Context, id string, target persistence.Scope, metadata map[string]string) RecordOutcome {
	snapshot, err := a.snapshotStore.GetLatestSnapshot(ctx, a.sourceScope, id)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("read source snapshot: %w", err)}
	}
	if snapshot == nil {
		return RecordOutcome{Status: StatusNone}
	}

	existing, err := a.snapshotStore.GetLatestSnapshot(ctx, target, id)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("check target snapshot: %w", err)}
	}
	if existing != nil {
		return RecordOutcome{Status: StatusAlreadyPresent}
	}

	if !a.write {
		return RecordOutcome{Status: StatusCopied}
	}

	clone, ok := proto.Clone(snapshot).(*egopb.Snapshot)
	if !ok {
		return RecordOutcome{Status: StatusFailed, Err: errors.New("clone snapshot: unexpected cloned type")}
	}
	clone.TenantMetadata = metadata

	if err := a.snapshotStore.WriteSnapshot(ctx, target, clone); err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("write target snapshot: %w", err)}
	}

	written, err := a.snapshotStore.GetLatestSnapshot(ctx, target, id)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("verify target snapshot: %w", err)}
	}
	if written == nil || written.GetSequenceNumber() != snapshot.GetSequenceNumber() {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("verify target snapshot: expected sequence %d, got %v", snapshot.GetSequenceNumber(), written)}
	}

	if a.deleteSource {
		if err := a.snapshotStore.DeleteSnapshots(ctx, a.sourceScope, id, snapshot.GetSequenceNumber()); err != nil {
			return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("delete source snapshot: %w", err)}
		}
		return RecordOutcome{Status: StatusSourceDeleted}
	}

	return RecordOutcome{Status: StatusCopied}
}

// adoptState copies persistence ID id's latest durable state from the
// source scope into target.
//
// Source deletion never applies to durable state, regardless of
// WithSourceDeletion: persistence.StateStore has no delete method at all
// (see state_store.go) — there is nothing this tool could call. See the
// package doc comment.
func (a *TenantAdopter) adoptState(ctx context.Context, id string, target persistence.Scope, metadata map[string]string) RecordOutcome {
	state, err := a.stateStore.GetLatestState(ctx, a.sourceScope, id)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("read source state: %w", err)}
	}
	if state == nil {
		return RecordOutcome{Status: StatusNone}
	}

	if !a.write {
		existing, err := a.stateStore.GetLatestState(ctx, target, id)
		if err != nil {
			return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("check target state: %w", err)}
		}
		if existing != nil {
			return RecordOutcome{Status: StatusAlreadyPresent}
		}
		return RecordOutcome{Status: StatusCopied}
	}

	clone, ok := proto.Clone(state).(*egopb.DurableState)
	if !ok {
		return RecordOutcome{Status: StatusFailed, Err: errors.New("clone durable state: unexpected cloned type")}
	}
	clone.TenantMetadata = metadata

	if err := a.stateStore.WriteState(ctx, target, clone, persistence.ExpectGenesis()); err != nil {
		var conflict *persistence.ConflictError
		if errors.As(err, &conflict) {
			return RecordOutcome{Status: StatusAlreadyPresent}
		}
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("write target state: %w", err)}
	}

	written, err := a.stateStore.GetLatestState(ctx, target, id)
	if err != nil {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("verify target state: %w", err)}
	}
	if written == nil || written.GetVersionNumber() != state.GetVersionNumber() {
		return RecordOutcome{Status: StatusFailed, Err: fmt.Errorf("verify target state: expected version %d, got %v", state.GetVersionNumber(), written)}
	}

	return RecordOutcome{Status: StatusCopied}
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
