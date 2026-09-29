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

	"github.com/getsyntegrity/go-specs/specs"

	"github.com/getsyntegrity/ego/port/runtime"
	"github.com/getsyntegrity/ego/tenancy"
)

// panics reports whether fn panics.
func panics(fn func()) (panicked bool) {
	defer func() {
		if recover() != nil {
			panicked = true
		}
	}()
	fn()
	return false
}

func TestResolveSpawnOptionsDefaults(t *testing.T) {
	specs.Describe(t, "ResolveSpawnOptions with no option yields the documented defaults", func(s *specs.Spec) {
		s.It("has no passivation, no relocation, restart directive, round-robin placement, no tenant and no adapter setting", func(ctx *specs.Context) {
			settings := runtime.ResolveSpawnOptions()
			// No passivation by default.
			ctx.Expect(settings.PassivateAfter()).ToEqual(time.Duration(0))
			// Relocation is disabled unless WithRelocation(true) is passed.
			ctx.Expect(settings.Relocation()).To(specs.BeFalse())
			ctx.Expect(settings.SupervisorDirective()).ToEqual(runtime.RestartDirective)
			ctx.Expect(settings.Placement()).ToEqual(runtime.RoundRobin)
			ctx.Expect(settings.Tenant()).ToEqual(tenancy.TenantID(""))
			_, ok := settings.AdapterSetting(keyA{})
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestResolveSpawnOptionsEachOptionReachesItsGetter(t *testing.T) {
	specs.Describe(t, "Each spawn option reaches its getter", func(s *specs.Spec) {
		s.It("resolves passivation, relocation, supervisor directive, placement and tenant", func(ctx *specs.Context) {
			settings := runtime.ResolveSpawnOptions(
				runtime.WithPassivateAfter(3*time.Second),
				runtime.WithRelocation(true),
				runtime.WithSupervisorDirective(runtime.StopDirective),
				runtime.WithPlacement(runtime.LeastLoad),
				runtime.WithTenant(tenancy.TenantID("acme")),
			)
			ctx.Expect(settings.PassivateAfter()).ToEqual(3 * time.Second)
			ctx.Expect(settings.Relocation()).To(specs.BeTrue())
			ctx.Expect(settings.SupervisorDirective()).ToEqual(runtime.StopDirective)
			ctx.Expect(settings.Placement()).ToEqual(runtime.LeastLoad)
			ctx.Expect(settings.Tenant()).ToEqual(tenancy.TenantID("acme"))
		})
	})
}

func TestResolveSpawnOptionsAppliesInOrder(t *testing.T) {
	specs.Describe(t, "ResolveSpawnOptions applies options in the order given", func(s *specs.Spec) {
		s.It("lets a later placement and a later relocation override an earlier one", func(ctx *specs.Context) {
			settings := runtime.ResolveSpawnOptions(
				runtime.WithPlacement(runtime.Local),
				runtime.WithPlacement(runtime.Random),
				runtime.WithRelocation(true),
				runtime.WithRelocation(false),
			)
			ctx.Expect(settings.Placement()).ToEqual(runtime.Random)
			ctx.Expect(settings.Relocation()).To(specs.BeFalse())
		})
	})
}

func TestResolveSpawnOptionsSkipsNilOption(t *testing.T) {
	specs.Describe(t, "ResolveSpawnOptions skips a nil option", func(s *specs.Spec) {
		s.It("does not panic and still applies the other options", func(ctx *specs.Context) {
			var settings runtime.SpawnSettings
			panicked := panics(func() {
				settings = runtime.ResolveSpawnOptions(nil, runtime.WithPlacement(runtime.Local), nil)
			})
			ctx.Expect(panicked).To(specs.BeFalse())
			ctx.Expect(settings.Placement()).ToEqual(runtime.Local)
		})
	})
}

// embedded satisfies SpawnOption the only way outside code can: by embedding
// it. A nil embedded option is not a nil SpawnOption, so its Apply panics, as
// it did before the move.
type embedded struct{ runtime.SpawnOption }

func TestResolveSpawnOptionsNonNilWrapperOfNilOptionPanics(t *testing.T) {
	specs.Describe(t, "ResolveSpawnOptions panics on a non-nil wrapper of a nil option", func(s *specs.Spec) {
		s.It("panics", func(ctx *specs.Context) {
			ctx.Expect(panics(func() { runtime.ResolveSpawnOptions(embedded{}) })).To(specs.BeTrue())
		})
	})
}

func TestResolveSpawnOptionsEmbeddedOptionApplies(t *testing.T) {
	specs.Describe(t, "ResolveSpawnOptions applies an option that is embedded in a wrapper", func(s *specs.Spec) {
		s.It("applies the embedded placement", func(ctx *specs.Context) {
			settings := runtime.ResolveSpawnOptions(embedded{runtime.WithPlacement(runtime.LeastLoad)})
			ctx.Expect(settings.Placement()).ToEqual(runtime.LeastLoad)
		})
	})
}

type keyA struct{}

type keyB struct{}

func TestAdapterSettingRoundTrip(t *testing.T) {
	specs.Describe(t, "An adapter setting round-trips through its key", func(s *specs.Spec) {
		s.It("is visible under its own key and invisible under another", func(ctx *specs.Context) {
			settings := runtime.ResolveSpawnOptions(runtime.WithAdapterSetting(keyA{}, 42))
			v, ok := settings.AdapterSetting(keyA{})
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(v).ToEqual(42)

			_, ok = settings.AdapterSetting(keyB{})
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestAdapterSettingLastWins(t *testing.T) {
	specs.Describe(t, "The last adapter setting for a key wins", func(s *specs.Spec) {
		s.It("resolves the second value", func(ctx *specs.Context) {
			settings := runtime.ResolveSpawnOptions(
				runtime.WithAdapterSetting(keyA{}, "first"),
				runtime.WithAdapterSetting(keyA{}, "second"),
			)
			v, ok := settings.AdapterSetting(keyA{})
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(v).ToEqual("second")
		})
	})
}

func TestAdapterSettingNilValueIsStored(t *testing.T) {
	specs.Describe(t, "An adapter setting with a nil value is stored", func(s *specs.Spec) {
		s.It("reports the key present with a nil value", func(ctx *specs.Context) {
			settings := runtime.ResolveSpawnOptions(runtime.WithAdapterSetting(keyA{}, nil))
			v, ok := settings.AdapterSetting(keyA{})
			ctx.Expect(ok).To(specs.BeTrue())
			ctx.Expect(v).To(specs.BeNil())
		})
	})
}

func TestAdapterSettingLookupWithNonComparableKeyIsAbsent(t *testing.T) {
	specs.Describe(t, "An adapter setting lookup with a non-comparable key is absent", func(s *specs.Spec) {
		s.It("does not panic and reports the key absent", func(ctx *specs.Context) {
			settings := runtime.ResolveSpawnOptions(runtime.WithAdapterSetting(keyA{}, 1))
			var ok bool
			panicked := panics(func() {
				_, ok = settings.AdapterSetting([]int{1})
			})
			ctx.Expect(panicked).To(specs.BeFalse())
			ctx.Expect(ok).To(specs.BeFalse())
		})
	})
}

func TestWithAdapterSettingPanicsAtBuildTime(t *testing.T) {
	specs.Describe(t, "WithAdapterSetting panics at build time for a non-comparable key", func(s *specs.Spec) {
		cases := []struct {
			name string
			key  any
		}{
			{"nil key", nil},
			{"slice key", []int{1}},
			{"map key", map[string]int{}},
			{"func key", func() {}},
			{"struct with slice", struct{ s []int }{}},
		}
		for _, tc := range cases {
			s.It(tc.name, func(ctx *specs.Context) {
				ctx.Expect(panics(func() { runtime.WithAdapterSetting(tc.key, 1) })).To(specs.BeTrue())
			})
		}
	})
}

func TestSpawnSettingsIsolatedFromLaterResolutions(t *testing.T) {
	specs.Describe(t, "Resolved SpawnSettings are isolated from later resolutions", func(s *specs.Spec) {
		s.It("keeps an earlier resolution unchanged when the same option is resolved again with more settings", func(ctx *specs.Context) {
			opt := runtime.WithAdapterSetting(keyA{}, 1)
			first := runtime.ResolveSpawnOptions(opt)
			second := runtime.ResolveSpawnOptions(opt, runtime.WithAdapterSetting(keyA{}, 2), runtime.WithAdapterSetting(keyB{}, 3))

			v, _ := first.AdapterSetting(keyA{})
			// Resolving again must not change an earlier SpawnSettings.
			ctx.Expect(v).ToEqual(1)
			_, ok := first.AdapterSetting(keyB{})
			ctx.Expect(ok).To(specs.BeFalse())
			v, _ = second.AdapterSetting(keyA{})
			ctx.Expect(v).ToEqual(2)
		})
	})
}
