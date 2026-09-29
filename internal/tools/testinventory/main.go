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

// Command testinventory keeps docs/testing/inventory.json and inventory.md in step
// with the Test functions of every module. See docs/testing/lanes.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/getsyntegrity/ego/internal/tools/testinventory/inventory"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

type config struct {
	root, inventoryFile, summaryFile, overridesFile string
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("testinventory", flag.ContinueOnError)
	fs.SetOutput(stderr)
	var cfg config
	fs.StringVar(&cfg.root, "root", "", "repository root (default: the enclosing git checkout)")
	fs.StringVar(&cfg.inventoryFile, "inventory", "docs/testing/inventory.json", "inventory file, relative to the root")
	fs.StringVar(&cfg.summaryFile, "summary", "docs/testing/inventory.md", "summary file, relative to the root")
	fs.StringVar(&cfg.overridesFile, "overrides", "docs/testing/inventory-overrides.json", "overrides file, relative to the root")
	check := fs.Bool("check", false, "fail when the committed inventory is missing a test, is stale or unclassified (static, fast)")
	update := fs.Bool("update", false, "regenerate the static part and keep the recorded run data")
	runTests := fs.Bool("run", false, "run go test -json in every module, then regenerate everything (slow)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	modes := 0
	for _, m := range []bool{*check, *update, *runTests} {
		if m {
			modes++
		}
	}
	if modes != 1 {
		_, _ = fmt.Fprintln(stderr, "choose exactly one of -check, -update or -run")
		return 2
	}
	if cfg.root == "" {
		root, err := findRoot()
		if err != nil {
			_, _ = fmt.Fprintln(stderr, err)
			return 2
		}
		cfg.root = root
	}
	var err error
	switch {
	case *check:
		var problems []string
		problems, err = doCheck(cfg)
		for _, p := range problems {
			_, _ = fmt.Fprintln(stderr, p)
		}
		if err == nil && len(problems) > 0 {
			_, _ = fmt.Fprintf(stderr, "%d problem(s): run `go run ./internal/tools/testinventory -update` and review the diff\n", len(problems))
			return 1
		}
		if err == nil {
			_, _ = fmt.Fprintln(stdout, "inventory is fresh")
		}
	case *update:
		err = doUpdate(cfg, stdout, false)
	default:
		err = doUpdate(cfg, stdout, true)
	}
	if err != nil {
		_, _ = fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

func findRoot() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("no git checkout found above the working directory; pass -root")
		}
		dir = parent
	}
}

func scanRepo(cfg config) ([]inventory.ModuleScan, inventory.Overrides, error) {
	ov, err := inventory.LoadOverrides(filepath.Join(cfg.root, cfg.overridesFile))
	if err != nil {
		return nil, inventory.Overrides{}, err
	}
	mods, err := inventory.DiscoverModules(cfg.root)
	if err != nil {
		return nil, ov, err
	}
	scans, err := inventory.ScanAll(mods)
	return scans, ov, err
}

func doCheck(cfg config) ([]string, error) {
	scans, ov, err := scanRepo(cfg)
	if err != nil {
		return nil, err
	}
	fresh, err := inventory.Assemble(scans, ov, nil)
	if err != nil {
		return []string{err.Error()}, nil
	}
	committed, err := inventory.ReadInventory(filepath.Join(cfg.root, cfg.inventoryFile))
	if err != nil {
		return []string{fmt.Sprintf("cannot read %s: %v", cfg.inventoryFile, err)}, nil
	}
	summary, err := os.ReadFile(filepath.Join(cfg.root, cfg.summaryFile))
	if err != nil {
		return []string{fmt.Sprintf("cannot read %s: %v", cfg.summaryFile, err)}, nil
	}
	return inventory.Check(committed, fresh, string(summary)), nil
}

func doUpdate(cfg config, stdout io.Writer, withRun bool) error {
	scans, ov, err := scanRepo(cfg)
	if err != nil {
		return err
	}
	invFile := filepath.Join(cfg.root, cfg.inventoryFile)
	var prev *inventory.Inventory
	if !withRun {
		if p, err := inventory.ReadInventory(invFile); err == nil {
			prev = &p
		}
	}
	inv, err := inventory.Assemble(scans, ov, prev)
	if err != nil {
		return err
	}
	if withRun {
		if err := recordRun(cfg, scans, &inv, stdout); err != nil {
			return err
		}
	}
	if err := inventory.WriteInventory(invFile, inv); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(cfg.root, cfg.summaryFile), []byte(inventory.RenderMarkdown(inv)), 0o644); err != nil {
		return err
	}
	_, _ = fmt.Fprintf(stdout, "wrote %d tests to %s\n", len(inv.Tests), cfg.inventoryFile)
	return nil
}

func recordRun(cfg config, scans []inventory.ModuleScan, inv *inventory.Inventory, stdout io.Writer) error {
	for _, ms := range scans {
		start := time.Now()
		run, err := inventory.RunModule(context.Background(), ms.Module)
		if err != nil {
			return err
		}
		inventory.AttachRun(inv, run)
		_, _ = fmt.Fprintf(stdout, "ran %s in %s\n", ms.Module.Rel, time.Since(start).Round(time.Second))
	}
	inv.Header = inventory.Header{
		Generator:   "go run ./internal/tools/testinventory -run",
		GoVersion:   goOutput(cfg.root, "env", "GOVERSION"),
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		CPUs:        runtime.NumCPU(),
		Commit:      gitCommit(cfg.root),
		RunRecorded: true,
	}
	return nil
}

func goOutput(dir string, args ...string) string {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}

func gitCommit(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--short=12", "HEAD")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "unknown"
	}
	return strings.TrimSpace(string(out))
}
