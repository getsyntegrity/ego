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

package conformance

import (
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/pablogore/ego/v4/egopb"
	"github.com/pablogore/ego/v4/persistence"
	"github.com/pablogore/ego/v4/tenancy"
	testpb "github.com/pablogore/ego/v4/test/data/testpb"
)

// mustTenantScope builds a valid tenant persistence.Scope for id.
func mustTenantScope(t require.TestingT, id string) persistence.Scope {
	scope, err := persistence.NewTenantScope(tenancy.TenantID(id))
	require.NoError(t, err)
	return scope
}

// eventBatch builds a single-event batch for persistenceID carrying marker
// in its payload, so a later read can identify whose record was actually
// returned (see eventMarker).
func eventBatch(t require.TestingT, persistenceID string, sequenceNumber uint64, marker float64) []*egopb.Event {
	payload, err := anypb.New(&testpb.AccountCreated{AccountId: persistenceID, AccountBalance: marker})
	require.NoError(t, err)
	return []*egopb.Event{{
		PersistenceId:  persistenceID,
		SequenceNumber: sequenceNumber,
		Event:          payload,
		Timestamp:      time.Now().UnixMilli(),
		Shard:          1,
	}}
}

// eventMarker recovers the marker written by eventBatch.
func eventMarker(t require.TestingT, event *egopb.Event) float64 {
	var msg testpb.AccountCreated
	require.NoError(t, event.GetEvent().UnmarshalTo(&msg))
	return msg.GetAccountBalance()
}

// stateRecord builds an *egopb.DurableState for persistenceID carrying
// marker in its payload, so a later read can identify whose record was
// actually returned (see stateMarker).
func stateRecord(t require.TestingT, persistenceID string, versionNumber uint64, marker float64) *egopb.DurableState {
	payload, err := anypb.New(&testpb.Account{AccountId: persistenceID, AccountBalance: marker})
	require.NoError(t, err)
	return &egopb.DurableState{PersistenceId: persistenceID, ResultingState: payload, VersionNumber: versionNumber}
}

// stateMarker recovers the marker written by stateRecord.
func stateMarker(t require.TestingT, state *egopb.DurableState) float64 {
	var msg testpb.Account
	require.NoError(t, state.GetResultingState().UnmarshalTo(&msg))
	return msg.GetAccountBalance()
}

// snapshotRecord builds an *egopb.Snapshot for persistenceID carrying marker
// in its payload, so a later read can identify whose record was actually
// returned (see snapshotMarker).
func snapshotRecord(t require.TestingT, persistenceID string, sequenceNumber uint64, marker float64) *egopb.Snapshot {
	payload, err := anypb.New(&testpb.Account{AccountId: persistenceID, AccountBalance: marker})
	require.NoError(t, err)
	return &egopb.Snapshot{PersistenceId: persistenceID, SequenceNumber: sequenceNumber, State: payload}
}

// snapshotMarker recovers the marker written by snapshotRecord.
func snapshotMarker(t require.TestingT, snapshot *egopb.Snapshot) float64 {
	var msg testpb.Account
	require.NoError(t, snapshot.GetState().UnmarshalTo(&msg))
	return msg.GetAccountBalance()
}
