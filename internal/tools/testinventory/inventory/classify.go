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

package inventory

import "strings"

// Lane is a test lane, see docs/testing/lanes.md.
type Lane string

// The five lanes.
const (
	LaneUnit         Lane = "unit"
	LaneComponent    Lane = "component"
	LaneIntegration  Lane = "integration"
	LaneArchitecture Lane = "architecture"
	LaneExample      Lane = "example"
)

// Lanes lists every lane in contract order.
var Lanes = []Lane{LaneUnit, LaneComponent, LaneIntegration, LaneArchitecture, LaneExample}

// DestPR is the destination of tests that stay in the pull request lane.
const DestPR = "pr"

// Valid reports whether l is one of the five lanes.
func (l Lane) Valid() bool {
	for _, known := range Lanes {
		if l == known {
			return true
		}
	}
	return false
}

// LeavesPR reports whether tests of the lane stop running on every pull request.
func (l Lane) LeavesPR() bool {
	return l != LaneUnit && l != LaneComponent
}

// Classification is the lane decision for one test with the evidence behind it.
type Classification struct {
	Lane        Lane   `json:"lane"`
	Destination string `json:"destination"`
	Reason      string `json:"lane_reason"`
}

type integrationRule struct {
	signal string
	dest   string
}

// integrationRules are ordered: the first match picks the destination issue.
var integrationRules = []integrationRule{
	{SigDBSQL, "#211"},
	{SigDBPostgres, "#211"},
	{SigBrokerKafka, "#213"},
	{SigBrokerNATS, "#213"},
	{SigBrokerPulsar, "#213"},
	{SigCluster, "#212"},
	{SigExternalEndpoint, "#210"},
	{SigProcessExec, "#210"},
}

// Classify decides the lane of a test from its signals. inExampleModule is true when
// the test lives in a module declared as an example module.
func Classify(signals []Signal, inExampleModule bool) Classification {
	has := make(map[string]bool, len(signals))
	for _, s := range signals {
		has[s.ID] = true
	}
	for _, r := range integrationRules {
		if has[r.signal] {
			return Classification{LaneIntegration, r.dest, "signal " + r.signal}
		}
	}
	for _, id := range []string{SigGoToolchain, SigSourceInspect} {
		if has[id] {
			return Classification{LaneArchitecture, "#208", "signal " + id}
		}
	}
	if inExampleModule {
		return Classification{LaneExample, "#214", "test lives in an example module"}
	}
	var runtime []string
	for _, id := range []string{SigActorSystem, SigHTTPTestServer, SigNetListen, SigNetDial} {
		if has[id] {
			runtime = append(runtime, id)
		}
	}
	if len(runtime) > 0 {
		return Classification{LaneComponent, DestPR, "signal " + strings.Join(runtime, ", ")}
	}
	return Classification{LaneUnit, DestPR, "no resource signal"}
}
