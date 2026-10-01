package main

import "go/ast"

// resourceFindings reports calls in a test file that reach a real resource. Filled in by the resource task.
func resourceFindings(string, *ast.File, map[string]string) []Finding { return nil }
