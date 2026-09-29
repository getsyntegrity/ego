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

package runtime_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/getsyntegrity/ego/port/runtime"
)

func TestErrUnsupportedWrapsStandardError(t *testing.T) {
	require.ErrorIs(t, runtime.ErrUnsupported, errors.ErrUnsupported)
}

func TestUnsupportedError(t *testing.T) {
	var err error = &runtime.UnsupportedError{Runtime: "inmem", Operation: "StartProjection"}
	require.ErrorIs(t, err, runtime.ErrUnsupported)
	require.ErrorIs(t, err, errors.ErrUnsupported)
	require.Equal(t, `eGo: runtime "inmem" does not support StartProjection`, err.Error())

	wrapped := fmt.Errorf("spawn: %w", err)
	var target *runtime.UnsupportedError
	require.ErrorAs(t, wrapped, &target)
	require.Equal(t, "inmem", target.Runtime)
	require.Equal(t, "StartProjection", target.Operation)
	require.ErrorIs(t, wrapped, runtime.ErrUnsupported)
}

func TestSentinelMessagesAreKept(t *testing.T) {
	cases := map[error]string{
		runtime.ErrEngineNotStarted:          "eGo engine has not started",
		runtime.ErrUndefinedEntityID:         "eGo entity id is not defined",
		runtime.ErrDurableStateStoreRequired: "durable state store is required",
		runtime.ErrEventsStoreRequired:       "events store is required",
		runtime.ErrProjectionNotRegistered:   "projection is not registered; register it with ego.WithProjection",
		runtime.ErrSpawnTenantUndetermined:   "eGo: tenant-aware spawn requires ego.WithTenant (the registered resolver exposes no fixed tenant); see tenancy.FixedTenantResolver",
		runtime.ErrSpawnTenantMismatch:       "eGo: entity id is already bound to a different tenant",
		runtime.ErrSpawnTenantUnverified:     "eGo: the spawned actor's tenant binding could not be verified",
		runtime.ErrNotACommand:               "eGo: payload is an engine-internal control message, not a command",
		runtime.ErrEntityFamilyNotDeclared:   "eGo: entity family is not declared; declare it with ego.WithEntityFamilies",
	}
	require.Len(t, cases, 10)
	for err, msg := range cases {
		require.EqualError(t, err, msg)
	}
}
