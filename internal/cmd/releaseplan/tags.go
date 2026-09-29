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

// legalMajor reports whether major may appear in a version of the module
// at modPath under Go's rules: exactly N for a "/vN" suffix, otherwise 0 or
// 1.
func legalMajor(modPath string, major int) bool {
	return validateMajor(modPath, major) == nil
}

// latestLegalTag is latestTag restricted to tags whose major is legal for
// modPath. The other tags under the same prefix are returned in ignored, in
// input order: they belong to another major or another module path (the ego
// root's v4.0.0 was published under the old .../ego/v4 path, so it is
// illegal for the suffix-less path) and must not decide the next version,
// nor refuse the whole plan. Tags that are not X.Y.Z under the prefix are
// neither used nor reported.
func latestLegalTag(dir, modPath string, tags []string) (v semver, tag string, found bool, ignored []string) {
	prefix := tagPrefix(dir)
	var legal []string
	for _, t := range tags {
		rest, ok := strings.CutPrefix(t, prefix)
		if !ok {
			continue
		}
		parsed, ok := parseSemver(rest)
		if !ok {
			continue
		}
		if !legalMajor(modPath, parsed.major) {
			ignored = append(ignored, t)
			continue
		}
		legal = append(legal, t)
	}
	v, tag, found = latestTag(dir, legal)
	return v, tag, found, ignored
}

// noTagBaseline is the synthetic "current version" nextTag bumps from
// when no existing tag matches a module's prefix. A path with a "/vN"
// suffix starts at vN.0.0, the only major D2 (a) allows for it — and,
// per nextTag, that baseline IS the first release itself for such a
// module, never something bumped further. A path without a suffix starts
// at 0.0.0, which is what release.yml does today for a publisher with no
// tag (CURRENT="0.0.0"), so a first patch is v0.0.1 and a first major is
// v1.0.0, both legal without a suffix.
//
// Rejected: starting a suffix-less module at 1.0.0, so that an untagged
// "-bump major" is refused at once. It would make every first release a
// v1, which declares a stable API, and it would diverge from release.yml
// without anyone deciding it.
func noTagBaseline(modPath string) semver {
	if major, ok := moduleSuffixMajor(modPath); ok {
		return semver{major: major}
	}
	return semver{}
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
//
// A module with no tag at all and a "/vN" path suffix has no earlier vN
// release to bump from, so its next tag is exactly vN.0.0 for every valid
// bump kind — never vN.0.1, vN.1.0 or v(N+1).0.0. An invalid bump kind is
// still an error (bumpVersion below validates it before this case is
// applied). A module without a suffix, or one that already has a
// matching tag, is unaffected and keeps bumping normally.
//
// Rejected: keeping "always bump the synthetic baseline" and special-
// casing only "-bump major" (the one case that currently errors). That
// would leave the patch and minor cases silently advertising vN.0.1 /
// vN.1.0 — a version nobody chose — on every dry run of an untagged
// module, which is the actual defect this function fixes.
func nextTag(dir, modPath string, tags []string, bumpKind string) (currentTag string, next semver, err error) {
	currentTag, next, _, err = nextTagDetailed(dir, modPath, tags, bumpKind)
	return currentTag, next, err
}

// nextTagDetailed is nextTag plus the tags it ignored because their major
// is not legal for modPath (see latestLegalTag). Ignored tags never cause a
// refusal; the caller reports them.
func nextTagDetailed(dir, modPath string, tags []string, bumpKind string) (currentTag string, next semver, ignored []string, err error) {
	base, curTag, found, ignored := latestLegalTag(dir, modPath, tags)
	if !found {
		base = noTagBaseline(modPath)
	}

	next, err = bumpVersion(base, bumpKind)
	if err != nil {
		return "", semver{}, nil, fmt.Errorf("module %s (%s): %w", dir, modPath, err)
	}

	if !found {
		if _, ok := moduleSuffixMajor(modPath); ok {
			next = base
		}
	}

	if err := validateMajor(modPath, next.major); err != nil {
		return "", semver{}, nil, fmt.Errorf("module %s (%s): refusing tag %s%s: %w", dir, modPath, tagPrefix(dir), next.String(), err)
	}

	if found {
		currentTag = curTag
	}
	return currentTag, next, ignored, nil
}
