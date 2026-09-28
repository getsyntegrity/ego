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

// Command ciselect decides which packages of the github.com/pablogore/ego/v4
// module a change must test and cover, and which Go modules of the
// repository it must verify, and writes that decision out for the CI
// workflows to consume. See internal/cmd/ciselect/selector for the
// selection rules.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/pablogore/ego/v4/internal/cmd/ciselect/selector"
)

// skipDirs are directories the satellite-module scan never descends into:
// they are either huge (module/build caches) or cannot contain a Go module
// relevant to this repository. testdata is ignored by the go command
// itself, so a fixture go.mod there is never one of the repository's
// modules.
var skipDirs = map[string]bool{
	".git":         true,
	"vendor":       true,
	"testdata":     true,
	"node_modules": true,
	".codegraph":   true,
	".atl":         true,
	"odd":          true,
}

func main() {
	if err := run(os.Args[1:], os.Stdout, os.Stderr); err != nil {
		fmt.Fprintf(os.Stderr, "ciselect: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string, stdout, stderr io.Writer) error {
	fs := flag.NewFlagSet("ciselect", flag.ContinueOnError)
	fs.SetOutput(stderr)
	changedFlag := fs.String("changed", "", "path to a newline-separated list of changed, repo-relative files (\"-\" reads stdin)")
	allFlag := fs.Bool("all", false, "select the full suite without reading -changed")
	reasonFlag := fs.String("reason", "", "optional extra context appended to the summary")
	moduleDirFlag := fs.String("module-dir", ".", "directory of the Go module to select packages from")
	repoRootFlag := fs.String("repo-root", ".", "repository root that -changed paths are relative to")
	baseFlag := fs.String("base", "", "optional git revision the -changed list was diffed from (for a three-dot diff, the merge-base); tells an added or deleted nested go.mod (a module boundary change) from an edited one")
	outDirFlag := fs.String("out-dir", "", "directory to write mode, packages.txt, coverpkg, modules.json, plan.json and summary.md into (required)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if *outDirFlag == "" {
		return errors.New("-out-dir is required")
	}
	if !*allFlag && *changedFlag == "" {
		return errors.New("either -changed or -all is required")
	}

	moduleDir, err := filepath.Abs(*moduleDirFlag)
	if err != nil {
		return fmt.Errorf("resolving -module-dir: %w", err)
	}
	repoRoot, err := filepath.Abs(*repoRootFlag)
	if err != nil {
		return fmt.Errorf("resolving -repo-root: %w", err)
	}

	modulePath, err := goListModulePath(moduleDir)
	if err != nil {
		return fmt.Errorf("determining module path: %w", err)
	}

	pkgs, err := goListPackages(moduleDir)
	if err != nil {
		return fmt.Errorf("loading package graph: %w", err)
	}

	graph := selector.Graph{
		ModulePath: modulePath,
		ModuleDir:  moduleDir,
		Packages:   pkgs,
	}

	var loadErrs []string
	for _, p := range graph.Packages {
		if p.Error != "" {
			loadErrs = append(loadErrs, fmt.Sprintf("%s: %s", p.ImportPath, p.Error))
		}
	}
	if len(loadErrs) > 0 {
		return fmt.Errorf("package graph has load errors:\n%s", strings.Join(loadErrs, "\n"))
	}

	var changed []string
	if !*allFlag {
		changed, err = readChangedFiles(*changedFlag)
		if err != nil {
			return fmt.Errorf("reading -changed: %w", err)
		}
	}
	moduleRelChanged := make([]string, len(changed))
	for i, c := range changed {
		moduleRelChanged[i] = toModuleRelPath(repoRoot, moduleDir, c)
	}

	// Without -base, every changed nested go.mod counts as a module
	// boundary change (the selector's conservative default). -all reads
	// no changed files, so it needs no base either.
	var goMods map[string]selector.GoModPresence
	if *baseFlag != "" && !*allFlag {
		presence, err := goModPresence(repoRoot, *baseFlag, changed)
		if err != nil {
			return fmt.Errorf("resolving -base: %w", err)
		}
		goMods = make(map[string]selector.GoModPresence, len(presence))
		for p, v := range presence {
			goMods[toModuleRelPath(repoRoot, moduleDir, p)] = v
		}
	}

	reason := *reasonFlag
	modules, err := discoverModules(moduleDir)
	if err != nil {
		if !*allFlag {
			return fmt.Errorf("building the module graph: %w", err)
		}
		// -all is the fallback for a failed selection, so it must not
		// fail on the same broken go.mod: every module is selected
		// anyway, and its own verification job reports the breakage.
		modules, err = discoverModuleDirs(moduleDir)
		if err != nil {
			return fmt.Errorf("scanning for nested modules: %w", err)
		}
		reason = strings.TrimSpace(reason + " (module graph unavailable; every discovered module selected by directory)")
	}

	result := selector.Select(graph, moduleRelChanged, selector.Options{
		All:     *allFlag,
		Reason:  reason,
		Modules: modules,
		GoMods:  goMods,
	})

	summary := selector.BuildSummary(result)
	if err := writeOutputs(*outDirFlag, result, summary); err != nil {
		return fmt.Errorf("writing outputs: %w", err)
	}

	fmt.Fprint(stdout, summary)
	return nil
}

// goCommand returns a `go` command running in dir with the caller's
// environment, except that GOWORK is forced to off: selection never runs
// in workspace mode, where a go.work could satisfy an import that a
// module's own go.mod does not require.
func goCommand(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("go", args...)
	cmd.Dir = dir
	env := make([]string, 0, len(os.Environ())+1)
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GOWORK=") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "GOWORK=off")
	return cmd
}

// goListModulePath returns the import path of the module rooted at dir.
func goListModulePath(dir string) (string, error) {
	cmd := goCommand(dir, "list", "-m")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("go list -m: %w: %s", err, stderr.String())
	}
	return strings.TrimSpace(string(out)), nil
}

// rawPackage mirrors the subset of `go list -e -json` fields the selector
// needs.
type rawPackage struct {
	ImportPath   string
	Dir          string
	Imports      []string
	TestImports  []string
	XTestImports []string
	Error        *struct {
		Err string
	}
}

// goListPackages runs `go list -e -json ./...` in dir and decodes the
// concatenated JSON stream it prints, one object per package.
func goListPackages(dir string) ([]selector.Package, error) {
	cmd := goCommand(dir, "list", "-e", "-json", "./...")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting go list: %w", err)
	}

	dec := json.NewDecoder(stdout)
	var pkgs []selector.Package
	for {
		var raw rawPackage
		if err := dec.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			_ = cmd.Wait()
			return nil, fmt.Errorf("decoding go list output: %w", err)
		}
		p := selector.Package{
			ImportPath:   raw.ImportPath,
			Dir:          raw.Dir,
			Imports:      raw.Imports,
			TestImports:  raw.TestImports,
			XTestImports: raw.XTestImports,
		}
		if raw.Error != nil {
			p.Error = raw.Error.Err
		}
		pkgs = append(pkgs, p)
	}

	if err := cmd.Wait(); err != nil {
		return nil, fmt.Errorf("go list -e -json ./...: %w: %s", err, stderr.String())
	}
	return pkgs, nil
}

// readChangedFiles reads newline-separated, repo-relative paths from path
// ("-" reads stdin), skipping blank lines.
func readChangedFiles(path string) ([]string, error) {
	var r io.Reader
	if path == "-" {
		r = os.Stdin
	} else {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		r = f
	}

	var lines []string
	scanner := bufio.NewScanner(r)
	// Long generated diffs can exceed bufio's default 64KiB line limit if a
	// single path were pathological; 1MiB is generous headroom.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

// toModuleRelPath converts a repo-relative changed-file path into a path
// relative to the module directory, using forward slashes, for the
// selector to classify.
func toModuleRelPath(repoRoot, moduleDir, repoRelPath string) string {
	abs := filepath.Join(repoRoot, repoRelPath)
	rel, err := filepath.Rel(moduleDir, abs)
	if err != nil {
		// Fall back to the original path; the selector will classify it
		// as unknown and fail safe to the full suite.
		return filepath.ToSlash(repoRelPath)
	}
	return filepath.ToSlash(rel)
}

// findSatelliteDirs scans under root for directories that hold their own
// go.mod (other than root's own go.mod), and returns their paths relative
// to root, using forward slashes. It does not descend into a satellite
// directory once found, nor into skipDirs.
func findSatelliteDirs(root string) ([]string, error) {
	var satellites []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && skipDirs[d.Name()] {
			return filepath.SkipDir
		}
		if path == root {
			return nil
		}
		if _, statErr := os.Stat(filepath.Join(path, "go.mod")); statErr == nil {
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			satellites = append(satellites, filepath.ToSlash(rel))
			return filepath.SkipDir
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return satellites, nil
}

// goModFile mirrors the subset of `go mod edit -json` output discovery
// needs.
type goModFile struct {
	Module struct {
		Path string
	}
	Require []struct {
		Path    string
		Version string
	}
	Replace []struct {
		Old struct {
			Path    string
			Version string
		}
		New struct {
			Path    string
			Version string
		}
	}
}

// readGoMod runs `go mod edit -json` in the module at dir: it only parses
// the go.mod file, with no network, no module download and no build.
func readGoMod(dir string) (goModFile, error) {
	var mod goModFile
	cmd := goCommand(dir, "mod", "edit", "-json")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return mod, fmt.Errorf("go mod edit -json in %s: %w: %s", dir, err, strings.TrimSpace(stderr.String()))
	}
	if err := json.Unmarshal(out, &mod); err != nil {
		return mod, fmt.Errorf("decoding go mod edit -json in %s: %w", dir, err)
	}
	if mod.Module.Path == "" {
		return mod, fmt.Errorf("go.mod in %s declares no module path", dir)
	}
	return mod, nil
}

// discoverModuleDirs returns the root module plus every nested module
// directory under root, root first then sorted, with no go.mod read.
func discoverModuleDirs(root string) ([]selector.ModuleInfo, error) {
	dirs, err := findSatelliteDirs(root)
	if err != nil {
		return nil, err
	}
	sort.Strings(dirs)
	out := []selector.ModuleInfo{{Dir: "."}}
	for _, d := range dirs {
		out = append(out, selector.ModuleInfo{Dir: d})
	}
	return out, nil
}

// discoverModules returns every Go module of the repository at root (the
// root module first, then nested modules sorted by directory) with its
// in-repository requirement edges, read from each go.mod. A requirement on
// another discovered module is:
//   - a Dep when a replace resolves it to that module's own directory in
//     the working tree;
//   - Pinned ("path@version") when no local replace applies, because the
//     module then builds against a published version, not the working tree.
//
// A local replace pointing an in-repository requirement at any other
// directory is ambiguous and is an error, as is any unreadable go.mod.
func discoverModules(root string) ([]selector.ModuleInfo, error) {
	infos, err := discoverModuleDirs(root)
	if err != nil {
		return nil, err
	}
	mods := make([]goModFile, len(infos))
	dirOfPath := make(map[string]string, len(infos))
	for i := range infos {
		mod, err := readGoMod(filepath.Join(root, filepath.FromSlash(infos[i].Dir)))
		if err != nil {
			return nil, err
		}
		if other, dup := dirOfPath[mod.Module.Path]; dup {
			return nil, fmt.Errorf("module path %s is declared by both %s and %s", mod.Module.Path, other, infos[i].Dir)
		}
		mods[i] = mod
		infos[i].Path = mod.Module.Path
		dirOfPath[mod.Module.Path] = infos[i].Dir
	}

	for i := range infos {
		mod := mods[i]
		if err := checkLocalReplaces(root, infos[i].Dir, mod, dirOfPath); err != nil {
			return nil, err
		}
		for _, req := range mod.Require {
			depDir, inRepo := dirOfPath[req.Path]
			if !inRepo || req.Path == infos[i].Path {
				continue
			}
			local, isLocal, err := localReplace(mod, req.Path, req.Version)
			if err != nil {
				return nil, err
			}
			if !isLocal {
				infos[i].Pinned = append(infos[i].Pinned, req.Path+"@"+req.Version)
				continue
			}
			resolved, err := repoRelDir(root, infos[i].Dir, local)
			if err != nil {
				return nil, err
			}
			if resolved != depDir {
				return nil, fmt.Errorf("module %s replaces %s with %s, which is %s, not that module's directory %s",
					infos[i].Dir, req.Path, local, resolved, depDir)
			}
			infos[i].Deps = append(infos[i].Deps, req.Path)
		}
		sort.Strings(infos[i].Deps)
		sort.Strings(infos[i].Pinned)

		imports, err := discoverModuleImports(root, infos[i].Dir, infos[i].Path, dirOfPath)
		if err != nil {
			return nil, fmt.Errorf("discovering imports for module %s: %w", infos[i].Dir, err)
		}
		infos[i].Imports = imports
	}
	return infos, nil
}

// discoverModuleImports parses every .go file of the module at
// repo-relative dir, including _test.go files and files behind any build
// constraint (a build-tagged or test-only importer must still count), with
// go/parser in imports-only mode: no module download, no network, no
// build. It does not descend into skipDirs or into a subdirectory holding
// its own go.mod (another module's files). It returns the sorted,
// de-duplicated import paths that belong to another discovered module
// (longest module path prefix in dirOfPath); the module's own packages,
// the standard library and third-party imports are dropped.
func discoverModuleImports(root, dir, modPath string, dirOfPath map[string]string) ([]string, error) {
	start := filepath.Join(root, filepath.FromSlash(dir))
	fset := token.NewFileSet()
	imports := make(map[string]bool)

	walkErr := filepath.WalkDir(start, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == start {
				return nil
			}
			if skipDirs[d.Name()] {
				return filepath.SkipDir
			}
			if _, statErr := os.Stat(filepath.Join(p, "go.mod")); statErr == nil {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		file, parseErr := parser.ParseFile(fset, p, nil, parser.ImportsOnly)
		if parseErr != nil {
			return fmt.Errorf("parsing %s: %w", p, parseErr)
		}
		for _, imp := range file.Imports {
			impPath, unquoteErr := strconv.Unquote(imp.Path.Value)
			if unquoteErr != nil {
				return fmt.Errorf("parsing import in %s: %w", p, unquoteErr)
			}
			if owner := owningModulePath(impPath, dirOfPath); owner != "" && owner != modPath {
				imports[impPath] = true
			}
		}
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
	}
	if len(imports) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(imports))
	for imp := range imports {
		out = append(out, imp)
	}
	sort.Strings(out)
	return out, nil
}

// owningModulePath returns the discovered module path that provides import
// path imp (the longest matching prefix), or "".
func owningModulePath(imp string, dirOfPath map[string]string) string {
	best := ""
	for p := range dirOfPath {
		if (imp == p || strings.HasPrefix(imp, p+"/")) && len(p) > len(best) {
			best = p
		}
	}
	return best
}

// goModPresence resolves, for every changed nested go.mod in changed
// (repo-relative paths), whether it exists at the base revision
// (`git cat-file -e <base>:<path>`) and at head (the working tree under
// repoRoot). base must be the revision the changed-file list was diffed
// from: for a three-dot diff, the merge-base. An unknown base is an error,
// never "absent at base".
func goModPresence(repoRoot, base string, changed []string) (map[string]selector.GoModPresence, error) {
	if strings.HasPrefix(base, "-") {
		return nil, fmt.Errorf("-base %q must not start with \"-\"", base)
	}
	// base is a revision (checked above not to be an option) and sp a
	// repo-relative path from the changed-file list; neither reaches a shell.
	verify := exec.Command("git", "rev-parse", "--verify", "--quiet", base+"^{commit}") //nolint:gosec // revision validated above, no shell
	verify.Dir = repoRoot
	if err := verify.Run(); err != nil {
		return nil, fmt.Errorf("-base %s is not a commit in %s: %w", base, repoRoot, err)
	}
	out := make(map[string]selector.GoModPresence)
	for _, c := range changed {
		sp := strings.TrimPrefix(filepath.ToSlash(c), "./")
		if path.Base(sp) != "go.mod" || path.Dir(sp) == "." {
			continue
		}
		var p selector.GoModPresence
		if _, err := os.Stat(filepath.Join(repoRoot, filepath.FromSlash(sp))); err == nil {
			p.AtHead = true
		}
		catFile := exec.Command("git", "cat-file", "-e", base+":"+sp) //nolint:gosec // revision validated above, no shell
		catFile.Dir = repoRoot
		if err := catFile.Run(); err == nil {
			p.AtBase = true
		}
		out[sp] = p
	}
	return out, nil
}

// checkLocalReplaces fails when a local replace of the module at moduleDir
// points inside the repository at a directory that is not a discovered
// module: the graph would silently miss that edge. Targets outside the
// repository are not in-repository edges and are allowed.
func checkLocalReplaces(root, moduleDir string, mod goModFile, dirOfPath map[string]string) error {
	discovered := make(map[string]bool, len(dirOfPath))
	for _, d := range dirOfPath {
		discovered[d] = true
	}
	for _, r := range mod.Replace {
		if r.New.Version != "" {
			continue
		}
		resolved, err := repoRelDir(root, moduleDir, r.New.Path)
		if err != nil {
			return err
		}
		if resolved == ".." || strings.HasPrefix(resolved, "../") {
			continue
		}
		if !discovered[resolved] {
			return fmt.Errorf("module %s replaces %s with %s, which is %s inside the repository but not a discovered module",
				moduleDir, r.Old.Path, r.New.Path, resolved)
		}
	}
	return nil
}

// localReplace returns the local directory a replace directive of mod maps
// requirement path@version to, if any. A version-specific replace wins
// over a path-wide one, as in the go command. A replace by another
// module version (not a directory) is not local.
func localReplace(mod goModFile, reqPath, reqVersion string) (string, bool, error) {
	var wide, exact *struct {
		Path    string
		Version string
	}
	for i := range mod.Replace {
		r := &mod.Replace[i]
		if r.Old.Path != reqPath {
			continue
		}
		switch r.Old.Version {
		case "":
			wide = &r.New
		case reqVersion:
			exact = &r.New
		}
	}
	chosen := exact
	if chosen == nil {
		chosen = wide
	}
	if chosen == nil || chosen.Version != "" {
		return "", false, nil
	}
	return chosen.Path, true, nil
}

// repoRelDir resolves a replace directory target, relative to the module
// at repo-relative moduleDir, to a clean repo-relative forward-slash
// directory ("." for the root).
func repoRelDir(root, moduleDir, target string) (string, error) {
	abs := filepath.FromSlash(target)
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(root, filepath.FromSlash(moduleDir), abs)
	}
	rel, err := filepath.Rel(root, filepath.Clean(abs))
	if err != nil {
		return "", fmt.Errorf("resolving replace target %s of module %s: %w", target, moduleDir, err)
	}
	return filepath.ToSlash(rel), nil
}

// writeOutputs writes mode, packages.txt, coverpkg, modules.json,
// plan.json and summary.md into outDir, creating it if necessary.
func writeOutputs(outDir string, result selector.Result, summary string) error {
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	modulesJSON, err := modulesJSON(selectedModuleDirs(result.Plan))
	if err != nil {
		return fmt.Errorf("encoding modules.json: %w", err)
	}
	planJSON, err := planJSON(result)
	if err != nil {
		return fmt.Errorf("encoding plan.json: %w", err)
	}
	files := map[string]string{
		"mode":         string(result.Mode) + "\n",
		"packages.txt": joinLines(result.Selected),
		"coverpkg":     strings.Join(result.Included, ","),
		"modules.json": modulesJSON,
		"plan.json":    planJSON,
		"summary.md":   summary,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(outDir, name), []byte(content), 0o600); err != nil {
			return fmt.Errorf("writing %s: %w", name, err)
		}
	}
	return nil
}

// selectedModuleDirs returns the directory of every selected entry of plan,
// in plan order (root first, then nested sorted by directory). The root
// directory "." is included exactly like any nested module's: the matrix
// job shape is the same for both (ego-arch-006 spec 1, C2), so ciselect
// makes no distinction here between "the root module is affected" and "a
// nested module is affected" — both simply mean "add this directory to the
// matrix".
func selectedModuleDirs(plan []selector.ModulePlan) []string {
	dirs := make([]string, 0, len(plan))
	for _, p := range plan {
		if p.Selected {
			dirs = append(dirs, p.Dir)
		}
	}
	return dirs
}

// modulesJSON renders dirs as a JSON array of strings, always valid JSON:
// "[]" when none were selected, never "null". This is what
// pull_request.yml and build.yml feed into a matrix job's fromJSON().
func modulesJSON(dirs []string) (string, error) {
	b, err := json.Marshal(dirs)
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

// planDoc is plan.json: the whole decision in a CI-neutral form, every
// discovered module listed, selected or not.
type planDoc struct {
	Global  bool         `json:"global"`
	Reasons []string     `json:"reasons"`
	Root    planRoot     `json:"root"`
	Modules []planModule `json:"modules"`
}

type planRoot struct {
	Mode     string   `json:"mode"`
	Selected []string `json:"selected"`
}

type planModule struct {
	Dir string `json:"dir"`
	// Path is omitted when unknown: in the -all fallback with an
	// unreadable go.mod, modules are discovered by directory only.
	Path     string   `json:"path,omitempty"`
	Selected bool     `json:"selected"`
	Reason   string   `json:"reason"`
	Chain    []string `json:"chain"`
}

// planJSON renders result as plan.json. Every list is a JSON array, never
// null.
func planJSON(result selector.Result) (string, error) {
	doc := planDoc{
		Global:  result.Global,
		Reasons: nonNil(result.Reasons),
		Root:    planRoot{Mode: string(result.Mode), Selected: nonNil(result.Selected)},
		Modules: make([]planModule, 0, len(result.Plan)),
	}
	for _, p := range result.Plan {
		doc.Modules = append(doc.Modules, planModule{
			Dir:      p.Dir,
			Path:     p.Path,
			Selected: p.Selected,
			Reason:   p.Reason,
			Chain:    nonNil(p.Chain),
		})
	}
	b, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func joinLines(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return strings.Join(lines, "\n") + "\n"
}
