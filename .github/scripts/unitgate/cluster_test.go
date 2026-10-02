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

import (
	"testing"

	"github.com/getsyntegrity/go-specs/specs"
)

type clusterCase struct {
	name string
	path string
	src  string
	want []string
}

// clusterHead imports goakt's actor package and dynaport, the two things the rule looks for.
const clusterHead = "package x\nimport (\n\t\"testing\"\n\tgoakt \"github.com/tochemey/goakt/v4/actor\"\n\t\"github.com/travisjeffery/go-dynaport\"\n)\n"

const startsCluster = "goakt.WithCluster(nil)"

const misnamed = ", but a test that starts a cluster must be named TestCluster*"

func TestScanRequiresTestClusterNamesForMultiNodeTests(t *testing.T) {
	specs.Describe(t, "unitgate scan of the cluster-name rule for test files", func(s *specs.Spec) {
		specs.Table(s, []clusterCase{
			{name: "reports a test that calls goakt.WithCluster directly", path: "engine/a_test.go",
				src:  clusterHead + "func TestEngineMode(t *testing.T) { _ = " + startsCluster + " }\n",
				want: []string{"TestEngineMode: calls goakt actor.WithCluster" + misnamed}},
			{name: "reports a test that calls dynaport.Get directly", path: "engine/a_test.go",
				src:  clusterHead + "func TestEnginePorts(t *testing.T) { _ = dynaport.Get(3) }\n",
				want: []string{"TestEnginePorts: calls dynaport.Get" + misnamed}},
			{name: "reports a test that calls any dynaport function", path: "engine/a_test.go",
				src:  clusterHead + "func TestEnginePorts(t *testing.T) { _ = dynaport.GetS(3) }\n",
				want: []string{"TestEnginePorts: calls dynaport.GetS" + misnamed}},
			{name: "reports a cluster call inside a closure of the test", path: "engine/a_test.go",
				src:  clusterHead + "func TestEngineMode(t *testing.T) { run := func() { _ = " + startsCluster + " }; run() }\n",
				want: []string{"TestEngineMode: calls goakt actor.WithCluster" + misnamed}},
			{name: "reports a test that reaches the call through a helper of the same file", path: "engine/a_test.go",
				src:  clusterHead + "func newNode() { _ = " + startsCluster + " }\nfunc TestEngineMode(t *testing.T) { newNode() }\n",
				want: []string{"TestEngineMode: calls goakt actor.WithCluster through newNode" + misnamed}},
			{name: "follows helpers through several hops", path: "engine/a_test.go",
				src:  clusterHead + "func deep() { _ = dynaport.Get(1) }\nfunc mid() { deep() }\nfunc top() { mid() }\nfunc TestEngineMode(t *testing.T) { top() }\n",
				want: []string{"TestEngineMode: calls dynaport.Get through top" + misnamed}},
			{name: "follows a helper declared as a method of the same file", path: "engine/a_test.go",
				src:  clusterHead + "type rig struct{}\nfunc (rig) start() { _ = " + startsCluster + " }\nfunc TestEngineMode(t *testing.T) { rig{}.start() }\n",
				want: []string{"TestEngineMode: calls goakt actor.WithCluster through start" + misnamed}},
			{name: "follows mutually recursive helpers without looping", path: "engine/a_test.go",
				src:  clusterHead + "func a() { b() }\nfunc b() { a(); _ = dynaport.Get(1) }\nfunc TestEngineMode(t *testing.T) { a() }\n",
				want: []string{"TestEngineMode: calls dynaport.Get through a" + misnamed}},
			{name: "reports every misnamed test, in source order", path: "engine/a_test.go",
				src:  clusterHead + "func TestOne(t *testing.T) { _ = dynaport.Get(1) }\nfunc TestTwo(t *testing.T) { _ = " + startsCluster + " }\n",
				want: []string{"TestOne: calls dynaport.Get" + misnamed, "TestTwo: calls goakt actor.WithCluster" + misnamed}},
			{name: "reports the compose goakt WithCluster option too", path: "compose/goakt/a_test.go",
				src:  "package x\nimport (\n\t\"testing\"\n\tegoakt \"github.com/getsyntegrity/urd/compose/goakt\"\n)\nfunc TestApp(t *testing.T) { _ = egoakt.WithCluster(nil) }\n",
				want: []string{"TestApp: calls compose/goakt.WithCluster" + misnamed}},

			{name: "allows a TestCluster test that calls goakt.WithCluster", path: "engine/a_test.go",
				src: clusterHead + "func TestClusterEngineMode(t *testing.T) { _ = " + startsCluster + " }\n"},
			{name: "allows a TestCluster test that reaches dynaport through a helper", path: "engine/a_test.go",
				src: clusterHead + "func ports() []int { return dynaport.Get(3) }\nfunc TestCluster_AppTwoNodes(t *testing.T) { _ = ports() }\n"},
			{name: "allows a test file without any cluster call", path: "engine/a_test.go",
				src: clusterHead + "func TestEngineMode(t *testing.T) { _ = goakt.NewClusterConfig() }\n"},
			{name: "allows a helper that starts a cluster when no test reaches it", path: "engine/a_test.go",
				src: clusterHead + "func newNode() { _ = " + startsCluster + " }\nfunc TestEngineMode(t *testing.T) {}\n"},
			{name: "allows a local function named WithCluster", path: "engine/a_test.go",
				src: "package x\nimport \"testing\"\nfunc WithCluster(x any) any { return x }\nfunc TestEngineMode(t *testing.T) { _ = WithCluster(nil) }\n"},
			{name: "allows a WithCluster method on a value that is not an import", path: "engine/a_test.go",
				src: "package x\nimport \"testing\"\ntype cfg struct{}\nfunc (cfg) WithCluster() {}\nfunc TestEngineMode(t *testing.T) { cfg{}.WithCluster() }\n"},
			{name: "allows WithCluster from a package that is not goakt", path: "engine/a_test.go",
				src: "package x\nimport (\n\t\"testing\"\n\tother \"example.com/other\"\n)\nfunc TestEngineMode(t *testing.T) { _ = other.WithCluster(nil) }\n"},
			{name: "allows a helper named like a test with another signature", path: "engine/a_test.go",
				src: clusterHead + "func TestdataPorts() []int { return dynaport.Get(1) }\n"},
			{name: "does not apply to files that are not test files", path: "engine/a.go",
				src: clusterHead + "func TestEngineMode(t *testing.T) { _ = " + startsCluster + " }\n"},

			// The opposite direction. A TestCluster name without a cluster setup in the same file is NOT
			// reported: the setup may live in a helper of another file of the package (as it does for
			// TestClusterEngineNeutralBehaviors, which calls newTestCluster from engine_test.go), and a
			// syntactic per-file rule cannot tell that from a missing setup. A misnamed TestCluster test only
			// lands in the cluster lane, which costs time but never hides a test.
			{name: "allows a TestCluster test whose cluster comes from a helper of another file", path: "engine/a_test.go",
				src: "package x\nimport \"testing\"\nfunc TestClusterEngineNeutral(t *testing.T) { newTestCluster(t) }\n"},
			{name: "allows a TestCluster test that starts no cluster in its file", path: "engine/a_test.go",
				src: "package x\nimport \"testing\"\nfunc TestClusterKindsAreListed(t *testing.T) {}\n"},
		}, func(c clusterCase) string { return c.name }, func(ctx *specs.Context, c clusterCase) {
			var details []string
			for _, f := range scanFiles(ctx, map[string]string{c.path: c.src}) {
				if f.Rule == RuleNoSpecs {
					continue // the fixtures are not go-specs tests; that rule is not under test here
				}
				ctx.Expect(f.Rule).ToEqual(RuleCluster)
				ctx.Expect(f.Path).ToEqual(c.path)
				details = append(details, f.Detail)
			}
			ctx.Expect(details).ToEqual(c.want)
		})

		s.It("keeps failing a misnamed cluster test even when an allowlist entry covers the file", func(ctx *specs.Context) {
			findings := []Finding{{Path: "engine/a_test.go", Rule: RuleCluster, Detail: "TestEngineMode: calls dynaport.Get" + misnamed}}
			pending, err := ParseAllowlist("engine/ | not allowed")
			ctx.Expect(err).To(specs.BeNil())
			resources, err := ParseAllowlist("engine/a_test.go | not allowed")
			ctx.Expect(err).To(specs.BeNil())

			problems, _ := Evaluate(findings, pending, resources, false)
			ctx.Expect(problems).To(specs.HaveLen(1))
			ctx.Expect(problems[0]).ToEqual("engine/a_test.go: cluster-name: TestEngineMode: calls dynaport.Get" + misnamed + "; CI selects the cluster tests by this name")
		})
	})
}
