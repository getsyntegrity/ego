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
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/getsyntegrity/ego/v4/port/runtime"
	"github.com/getsyntegrity/ego/v4/tenancy"
)

func TestResolveSpawnOptionsDefaults(t *testing.T) {
	s := runtime.ResolveSpawnOptions()
	require.Zero(t, s.PassivateAfter(), "no passivation by default")
	require.False(t, s.Relocation(), "relocation is disabled unless WithRelocation(true) is passed")
	require.Equal(t, runtime.RestartDirective, s.SupervisorDirective())
	require.Equal(t, runtime.RoundRobin, s.Placement())
	require.Empty(t, s.Tenant())
	_, ok := s.AdapterSetting(keyA{})
	require.False(t, ok)
}

func TestResolveSpawnOptionsEachOptionReachesItsGetter(t *testing.T) {
	s := runtime.ResolveSpawnOptions(
		runtime.WithPassivateAfter(3*time.Second),
		runtime.WithRelocation(true),
		runtime.WithSupervisorDirective(runtime.StopDirective),
		runtime.WithPlacement(runtime.LeastLoad),
		runtime.WithTenant(tenancy.TenantID("acme")),
	)
	require.Equal(t, 3*time.Second, s.PassivateAfter())
	require.True(t, s.Relocation())
	require.Equal(t, runtime.StopDirective, s.SupervisorDirective())
	require.Equal(t, runtime.LeastLoad, s.Placement())
	require.Equal(t, tenancy.TenantID("acme"), s.Tenant())
}

func TestResolveSpawnOptionsAppliesInOrder(t *testing.T) {
	s := runtime.ResolveSpawnOptions(
		runtime.WithPlacement(runtime.Local),
		runtime.WithPlacement(runtime.Random),
		runtime.WithRelocation(true),
		runtime.WithRelocation(false),
	)
	require.Equal(t, runtime.Random, s.Placement())
	require.False(t, s.Relocation())
}

func TestResolveSpawnOptionsSkipsNilOption(t *testing.T) {
	require.NotPanics(t, func() {
		s := runtime.ResolveSpawnOptions(nil, runtime.WithPlacement(runtime.Local), nil)
		require.Equal(t, runtime.Local, s.Placement())
	})
}

// embedded satisfies SpawnOption the only way outside code can: by embedding
// it. A nil embedded option is not a nil SpawnOption, so its Apply panics, as
// it did before the move.
type embedded struct{ runtime.SpawnOption }

func TestResolveSpawnOptionsNonNilWrapperOfNilOptionPanics(t *testing.T) {
	require.Panics(t, func() { runtime.ResolveSpawnOptions(embedded{}) })
}

func TestResolveSpawnOptionsEmbeddedOptionApplies(t *testing.T) {
	s := runtime.ResolveSpawnOptions(embedded{runtime.WithPlacement(runtime.LeastLoad)})
	require.Equal(t, runtime.LeastLoad, s.Placement())
}

type keyA struct{}

type keyB struct{}

func TestAdapterSettingRoundTrip(t *testing.T) {
	s := runtime.ResolveSpawnOptions(runtime.WithAdapterSetting(keyA{}, 42))
	v, ok := s.AdapterSetting(keyA{})
	require.True(t, ok)
	require.Equal(t, 42, v)

	_, ok = s.AdapterSetting(keyB{})
	require.False(t, ok, "a setting is invisible under another key")
}

func TestAdapterSettingLastWins(t *testing.T) {
	s := runtime.ResolveSpawnOptions(
		runtime.WithAdapterSetting(keyA{}, "first"),
		runtime.WithAdapterSetting(keyA{}, "second"),
	)
	v, ok := s.AdapterSetting(keyA{})
	require.True(t, ok)
	require.Equal(t, "second", v)
}

func TestAdapterSettingNilValueIsStored(t *testing.T) {
	s := runtime.ResolveSpawnOptions(runtime.WithAdapterSetting(keyA{}, nil))
	v, ok := s.AdapterSetting(keyA{})
	require.True(t, ok)
	require.Nil(t, v)
}

func TestAdapterSettingLookupWithNonComparableKeyIsAbsent(t *testing.T) {
	s := runtime.ResolveSpawnOptions(runtime.WithAdapterSetting(keyA{}, 1))
	require.NotPanics(t, func() {
		_, ok := s.AdapterSetting([]int{1})
		require.False(t, ok)
	})
}

func TestWithAdapterSettingPanicsAtBuildTime(t *testing.T) {
	cases := map[string]any{
		"nil key":           nil,
		"slice key":         []int{1},
		"map key":           map[string]int{},
		"func key":          func() {},
		"struct with slice": struct{ s []int }{},
	}
	for name, key := range cases {
		t.Run(name, func(t *testing.T) {
			require.Panics(t, func() { runtime.WithAdapterSetting(key, 1) })
		})
	}
}

func TestSpawnSettingsIsolatedFromLaterResolutions(t *testing.T) {
	opt := runtime.WithAdapterSetting(keyA{}, 1)
	first := runtime.ResolveSpawnOptions(opt)
	second := runtime.ResolveSpawnOptions(opt, runtime.WithAdapterSetting(keyA{}, 2), runtime.WithAdapterSetting(keyB{}, 3))

	v, _ := first.AdapterSetting(keyA{})
	require.Equal(t, 1, v, "resolving again must not change an earlier SpawnSettings")
	_, ok := first.AdapterSetting(keyB{})
	require.False(t, ok)
	v, _ = second.AdapterSetting(keyA{})
	require.Equal(t, 2, v)
}
