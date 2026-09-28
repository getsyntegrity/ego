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
	"fmt"
	"strconv"
	"strings"
)

// semver is the X.Y.Z this repository's tags carry — no pre-release or
// build metadata, since releaseplan only ever constructs tags of this
// shape itself (D2 (a)).
type semver struct {
	major, minor, patch int
}

func (v semver) String() string {
	return fmt.Sprintf("%d.%d.%d", v.major, v.minor, v.patch)
}

// compare returns <0, 0 or >0 as v is less than, equal to or greater than
// other.
func (v semver) compare(other semver) int {
	if v.major != other.major {
		return v.major - other.major
	}
	if v.minor != other.minor {
		return v.minor - other.minor
	}
	return v.patch - other.patch
}

// parseSemver parses "X.Y.Z" (no leading "v", no pre-release/build
// metadata); a version with a negative component is not valid.
func parseSemver(s string) (semver, bool) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return semver{}, false
	}
	var v semver
	var err error
	if v.major, err = strconv.Atoi(parts[0]); err != nil || v.major < 0 {
		return semver{}, false
	}
	if v.minor, err = strconv.Atoi(parts[1]); err != nil || v.minor < 0 {
		return semver{}, false
	}
	if v.patch, err = strconv.Atoi(parts[2]); err != nil || v.patch < 0 {
		return semver{}, false
	}
	return v, true
}

// tagPrefix returns the tag prefix for the module at repository-relative
// dir under D2 (a): "v" for the root ("."), "<dir>/v" for a nested
// module.
func tagPrefix(dir string) string {
	if dir == "." {
		return "v"
	}
	return dir + "/v"
}

// moduleSuffixMajor returns the major version a module path's trailing
// "/vN" element declares (N >= 2, Go's major-version-suffix convention —
// v0 and v1 are never suffixed), and whether the path has one.
func moduleSuffixMajor(modPath string) (major int, ok bool) {
	last := modPath
	if i := strings.LastIndex(modPath, "/"); i >= 0 {
		last = modPath[i+1:]
	}
	if len(last) < 2 || last[0] != 'v' {
		return 0, false
	}
	n, err := strconv.Atoi(last[1:])
	if err != nil || n < 2 {
		return 0, false
	}
	return n, true
}

// latestTag returns the highest tag in tags matching the prefix for
// module dir, and whether one was found.
func latestTag(dir string, tags []string) (v semver, tag string, found bool) {
	prefix := tagPrefix(dir)
	for _, t := range tags {
		rest, ok := strings.CutPrefix(t, prefix)
		if !ok {
			continue
		}
		parsed, ok := parseSemver(rest)
		if !ok {
			continue
		}
		if !found || parsed.compare(v) > 0 {
			v, tag, found = parsed, t, true
		}
	}
	return v, tag, found
}

// noTagBaseline is the synthetic "current version" nextTag bumps from
// when no existing tag matches a module's prefix: the largest major the
// D2 (a) scheme allows for that module's path before a bump would need a
// path-suffix change — the root's own forced major (equality with its
// path's "/vN" leaves only one legal value), or 1 for a path with no
// "/vN" suffix (the top of the {0,1} range Go permits there).
//
// Starting at that ceiling, rather than at 0, means a bare "-bump major"
// against a never-tagged module is refused exactly the way it would be
// once a real tag already sits at the ceiling — instead of silently
// succeeding once (0 -> 1 is always legal) and only failing on a second
// major bump. The rejected alternative was starting every module at
// "0.0.0" regardless of its path: simpler, but it hides the same major
// bump's refusal for a whole release cycle for any module without a path
// suffix, which is more surprising, not less. Overriding this baseline
// for a real first release is as trivial as seeding -tags with the
// desired starting tag; no separate flag was added for it.
func noTagBaseline(modPath string) semver {
	if major, ok := moduleSuffixMajor(modPath); ok {
		return semver{major: major}
	}
	return semver{major: 1}
}

// bumpVersion returns v with kind ("patch", "minor" or "major") applied.
func bumpVersion(v semver, kind string) (semver, error) {
	switch kind {
	case "patch":
		return semver{v.major, v.minor, v.patch + 1}, nil
	case "minor":
		return semver{v.major, v.minor + 1, 0}, nil
	case "major":
		return semver{v.major + 1, 0, 0}, nil
	default:
		return semver{}, fmt.Errorf("unknown -bump %q: must be patch, minor or major", kind)
	}
}

// validateMajor refuses a computed major that D2 (a) does not allow for
// modPath: it must equal a "/vN" path suffix exactly, or, with no
// suffix, be 0 or 1.
func validateMajor(modPath string, major int) error {
	if forced, ok := moduleSuffixMajor(modPath); ok {
		if major != forced {
			return fmt.Errorf("major v%d does not match the /v%d suffix of module path %s", major, forced, modPath)
		}
		return nil
	}
	if major > 1 {
		return fmt.Errorf("major v%d requires a /v%d suffix in module path %s (Go modules require v2+ to be suffixed)", major, major, modPath)
	}
	return nil
}

// nextTag computes the next release tag for the module at repository-
// relative dir with module path modPath, given every existing tag in the
// repository (tags, any subset may belong to other modules — only those
// matching dir's own prefix are considered) and the requested bump kind
// ("patch", "minor" or "major"). It returns the current tag (empty when
// none exists) and the next version, or a refusal naming dir, modPath and
// the requested version when the computed major violates D2 (a).
func nextTag(dir, modPath string, tags []string, bumpKind string) (currentTag string, next semver, err error) {
	base, curTag, found := latestTag(dir, tags)
	if !found {
		base = noTagBaseline(modPath)
	}

	next, err = bumpVersion(base, bumpKind)
	if err != nil {
		return "", semver{}, fmt.Errorf("module %s (%s): %w", dir, modPath, err)
	}
	if err := validateMajor(modPath, next.major); err != nil {
		return "", semver{}, fmt.Errorf("module %s (%s): refusing tag %s%s: %w", dir, modPath, tagPrefix(dir), next.String(), err)
	}

	if found {
		currentTag = curTag
	}
	return currentTag, next, nil
}
