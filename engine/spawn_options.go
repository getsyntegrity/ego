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
	goakt "github.com/tochemey/goakt/v4/actor"
	"github.com/tochemey/goakt/v4/passivation"
	"github.com/tochemey/goakt/v4/supervisor"
)

func buildSpawnOptionsFromConfig(config *spawnConfig) []goakt.SpawnOption {
	sOptions := []goakt.SpawnOption{
		goakt.WithLongLived(),
	}

	if config.passivateAfter > 0 {
		sOptions = append(sOptions, goakt.WithPassivationStrategy(passivation.NewTimeBasedStrategy(config.passivateAfter)))
	}

	if !config.toRelocate {
		sOptions = append(sOptions, goakt.WithRelocationDisabled())
	}

	sOptions = append(sOptions,
		goakt.WithPlacement(toSpawnPlacement(config.entitiesPlacement)),
		goakt.WithSupervisor(newSupervisor(config.supervisorDirective)),
	)

	// Stashing is needed unconditionally, not only when batching is enabled:
	// EventSourcedActor's direct (non-batched) command path also stashes the
	// in-flight command around its async persist write (see persistAsync in
	// event_sourced_actor.go, fixing issue #64). The stash buffer is inert
	// until Stash is actually called, so enabling it here is a no-op for any
	// spawn — including DurableStateActor's, via buildSpawnOptionsFromConfig
	// — that never calls it.
	sOptions = append(sOptions, goakt.WithStashing())

	return sOptions
}

func newSupervisor(directive SupervisorDirective) *supervisor.Supervisor {
	return supervisor.NewSupervisor(supervisor.WithAnyErrorDirective(toSupervisorDirective(directive)))
}

func toSpawnPlacement(placement EntitiesPlacement) goakt.SpawnPlacement {
	switch placement {
	case LeastLoad:
		return goakt.LeastLoad
	case Random:
		return goakt.Random
	case Local:
		return goakt.Local
	default:
		return goakt.RoundRobin
	}
}

func toSupervisorDirective(directive SupervisorDirective) supervisor.Directive {
	switch directive {
	case StopDirective:
		return supervisor.StopDirective
	default:
		return supervisor.RestartDirective
	}
}
