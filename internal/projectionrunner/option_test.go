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

package projectionrunner

import (
	"testing"
	"time"

	"github.com/getsyntegrity/go-specs/specs"
	"google.golang.org/protobuf/types/known/anypb"

	"github.com/getsyntegrity/ego/encryption"
	"github.com/getsyntegrity/ego/eventadapter"
	"github.com/getsyntegrity/ego/internal/instrumentation"
	"github.com/getsyntegrity/ego/projection"
	"github.com/getsyntegrity/ego/testkit"
)

func TestOption(t *testing.T) {
	specs.Describe(t, "each runner option applies its value to the Runner", func(s *specs.Spec) {
		ts := time.Second
		from := time.Now()
		to := time.Now().Add(ts)
		recovery := projection.NewRecovery()

		s.It("WithRefreshInterval", func(ctx *specs.Context) {
			var r Runner
			WithPullInterval(ts).Apply(&r)
			ctx.Expect(r.pullInterval).ToEqual(ts)
		})
		s.It("WithMaxBufferSize", func(ctx *specs.Context) {
			var r Runner
			WithMaxBufferSize(5).Apply(&r)
			ctx.Expect(r.maxBufferSize).ToEqual(5)
		})
		s.It("WithStartOffset", func(ctx *specs.Context) {
			var r Runner
			WithStartOffset(from).Apply(&r)
			ctx.Expect(r.startingOffset).ToEqual(from)
		})
		s.It("WithResetOffset", func(ctx *specs.Context) {
			var r Runner
			WithResetOffset(to).Apply(&r)
			ctx.Expect(r.resetOffsetTo).ToEqual(to)
		})
		s.It("WithLogger", func(ctx *specs.Context) {
			var r Runner
			WithLogger(discardLogger).Apply(&r)
			ctx.Expect(r.logger == discardLogger).To(specs.BeTrue())
		})
		s.It("WithRecoveryStrategy", func(ctx *specs.Context) {
			var r Runner
			WithRecoveryStrategy(recovery).Apply(&r)
			ctx.Expect(r.recovery).ToEqual(recovery)
		})
	})
}

func TestWithDeadLetterHandler(t *testing.T) {
	specs.Describe(t, "WithDeadLetterHandler sets the dead-letter handler", func(s *specs.Spec) {
		s.It("stores the given handler", func(ctx *specs.Context) {
			dlh := projection.NewDiscardDeadLetterHandler()
			var r Runner
			WithDeadLetterHandler(dlh).Apply(&r)
			ctx.Expect(r.deadLetterHandler != nil).To(specs.BeTrue())
			ctx.Expect(r.deadLetterHandler).ToEqual(dlh)
		})
	})
}

func TestWithDeadLetterHandlerNil(t *testing.T) {
	specs.Describe(t, "WithDeadLetterHandler with nil leaves the handler unset", func(s *specs.Spec) {
		s.It("keeps the dead-letter handler nil", func(ctx *specs.Context) {
			var r Runner
			WithDeadLetterHandler(nil).Apply(&r)
			ctx.Expect(r.deadLetterHandler == nil).To(specs.BeTrue())
		})
	})
}

func TestWithEventAdapters(t *testing.T) {
	specs.Describe(t, "WithEventAdapters sets the event adapters", func(s *specs.Spec) {
		s.It("stores the given adapters", func(ctx *specs.Context) {
			adapter := &runnerTestAdapter{}
			adapters := []eventadapter.EventAdapter{adapter}
			var r Runner
			WithEventAdapters(adapters).Apply(&r)
			ctx.Expect(len(r.eventAdapters)).ToEqual(1)
			ctx.Expect(r.eventAdapters[0]).ToEqual(eventadapter.EventAdapter(adapter))
		})
	})
}

func TestWithEventAdaptersEmpty(t *testing.T) {
	specs.Describe(t, "WithEventAdapters with nil leaves the adapters unset", func(s *specs.Spec) {
		s.It("keeps the event adapters nil", func(ctx *specs.Context) {
			var r Runner
			WithEventAdapters(nil).Apply(&r)
			ctx.Expect(r.eventAdapters == nil).To(specs.BeTrue())
		})
	})
}

func TestWithMetrics(t *testing.T) {
	specs.Describe(t, "WithMetrics sets the instruments", func(s *specs.Spec) {
		s.It("stores the given instruments", func(ctx *specs.Context) {
			m := &instrumentation.Instruments{}
			var r Runner
			WithMetrics(m).Apply(&r)
			ctx.Expect(r.metrics).ToEqual(m)
		})
	})
}

func TestWithEncryptor(t *testing.T) {
	specs.Describe(t, "WithEncryptor sets the encryptor", func(s *specs.Spec) {
		s.It("stores the given encryptor", func(ctx *specs.Context) {
			enc := encryption.NewAESEncryptor(testkit.NewKeyStore())
			var r Runner
			WithEncryptor(enc).Apply(&r)
			ctx.Expect(r.encryptor != nil).To(specs.BeTrue())
			ctx.Expect(r.encryptor).ToEqual(enc)
		})
	})
}

// runnerTestAdapter is a no-op EventAdapter for testing
type runnerTestAdapter struct{}

func (a *runnerTestAdapter) Adapt(event *anypb.Any, _ uint64) (*anypb.Any, error) {
	return event, nil
}
