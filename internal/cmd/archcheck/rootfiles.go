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
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// rootNoGoFilesRule is the ID the root Go file guard reports under. It is a
// structural check on the module root directory rather than an import-edge
// rule, so it lives beside runCheck instead of in the rules package table.
const rootNoGoFilesRule = "root-no-go-files"

// rootGoFiles returns the names of the *.go files (production and test)
// that sit directly in repoRoot, sorted. Files in subdirectories are not
// reported: the root module's packages live below the root.
func rootGoFiles(repoRoot string) ([]string, error) {
	entries, err := os.ReadDir(repoRoot)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", repoRoot, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".go" {
			continue
		}
		files = append(files, e.Name())
	}
	sort.Strings(files)
	return files, nil
}

// reportRootGoFiles writes one block per root-level Go file to stdout and
// returns how many it reported.
func reportRootGoFiles(repoRoot string, stdout io.Writer) (int, error) {
	files, err := rootGoFiles(repoRoot)
	if err != nil {
		return 0, err
	}
	if len(files) == 0 {
		return 0, nil
	}
	fmt.Fprintf(stdout, "%d root Go file(s): rule %s (the module root must hold no Go files; packages live in subdirectories, see docs/ci.md)\n", len(files), rootNoGoFilesRule)
	fmt.Fprintf(stdout, "  files: %s\n\n", strings.Join(files, ", "))
	return len(files), nil
}
