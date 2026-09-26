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

package main

import "github.com/pablogore/ego/v4/internal/cmd/archcheck/rules"

// repoBaseline is the repository's current, explicit set of known
// violations (odd/tasks/arch-boundary-check.md, "Baseline at the start").
// Every entry names an owner, a justification for why the violation exists
// today and the criterion that removes it; ValidateBaseline enforces that
// shape, and Evaluate reports an entry that stops matching a real
// violation as stale, so this list can only shrink.
var repoBaseline = []rules.BaselineEntry{
	{
		Importer:         "github.com/pablogore/ego/v4/publisher/kafka",
		Import:           "github.com/pablogore/ego/v4",
		Rule:             "external-adapter-no-runtime",
		Owner:            "@pablogore",
		Justification:    "publishers still import package ego for EventPublisher/StatePublisher; the contracts move to port/publishing in S1a (#103)",
		RemovalCriterion: "S1b: switch to port/publishing once #111 builds and verifies nested modules in CI",
	},
	{
		Importer:         "github.com/pablogore/ego/v4/publisher/nats",
		Import:           "github.com/pablogore/ego/v4",
		Rule:             "external-adapter-no-runtime",
		Owner:            "@pablogore",
		Justification:    "publishers still import package ego for EventPublisher/StatePublisher; the contracts move to port/publishing in S1a (#103)",
		RemovalCriterion: "S1b: switch to port/publishing once #111 builds and verifies nested modules in CI",
	},
	{
		Importer:         "github.com/pablogore/ego/v4/publisher/pulsar",
		Import:           "github.com/pablogore/ego/v4",
		Rule:             "external-adapter-no-runtime",
		Owner:            "@pablogore",
		Justification:    "publishers still import package ego for EventPublisher/StatePublisher; the contracts move to port/publishing in S1a (#103)",
		RemovalCriterion: "S1b: switch to port/publishing once #111 builds and verifies nested modules in CI",
	},
	{
		Importer:         "github.com/pablogore/ego/v4/publisher/websocket",
		Import:           "github.com/pablogore/ego/v4",
		Rule:             "external-adapter-no-runtime",
		Owner:            "@pablogore",
		Justification:    "publishers still import package ego for EventPublisher/StatePublisher; the contracts move to port/publishing in S1a (#103)",
		RemovalCriterion: "S1b: switch to port/publishing once #111 builds and verifies nested modules in CI",
	},
	{
		Importer:         "github.com/pablogore/ego/v4/migration",
		Import:           "github.com/pablogore/ego/v4",
		Rule:             "application-no-runtime",
		Owner:            "@pablogore",
		Justification:    "migration replays through ego's runtime types; no runtime-neutral contract exists for them yet",
		RemovalCriterion: "S3/S4 (#103, #11): runtime-neutral contracts for what migration uses",
	},
}
