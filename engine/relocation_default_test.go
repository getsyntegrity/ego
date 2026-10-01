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

package engine

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"

	runtimeport "github.com/getsyntegrity/ego/port/runtime"
)

// TestRelocationDisabledByDefault pins the default that #154 found
// contradicting WithRelocation's documentation: in cluster mode, an entity
// spawned without WithRelocation is NOT eligible for relocation. It is the
// exact reproduction the issue describes ("se puede verificar sin cluster
// leyendo las opciones de spawn que arma el engine: toRelocate == false por
// defecto"), asserted at both the layer engine.go reads
// (newSpawnConfig/spawnConfig.toRelocate) and the runtime SPI layer other
// runtimes read (runtimeport.ResolveSpawnOptions(...).Relocation()). If
// either default is ever flipped without updating WithRelocation's docs, this
// test fails.
func TestRelocationDisabledByDefault(t *testing.T) {
	specs.Describe(t, "relocation is disabled by default and WithRelocation(true) is required to opt in", func(s *specs.Spec) {
		s.It("newSpawnConfig with no options disables relocation", func(ctx *specs.Context) {
			config := newSpawnConfig()
			ctx.Expect(config.toRelocate).To(specs.BeFalse())
		})

		s.It("newSpawnConfig with WithRelocation(true) enables relocation", func(ctx *specs.Context) {
			config := newSpawnConfig(WithRelocation(true))
			ctx.Expect(config.toRelocate).To(specs.BeTrue())
		})

		s.It("runtimeport.ResolveSpawnOptions with no options disables relocation", func(ctx *specs.Context) {
			settings := runtimeport.ResolveSpawnOptions()
			ctx.Expect(settings.Relocation()).To(specs.BeFalse())
		})

		s.It("runtimeport.ResolveSpawnOptions with WithRelocation(true) enables relocation", func(ctx *specs.Context) {
			settings := runtimeport.ResolveSpawnOptions(WithRelocation(true))
			ctx.Expect(settings.Relocation()).To(specs.BeTrue())
		})
	})
}
