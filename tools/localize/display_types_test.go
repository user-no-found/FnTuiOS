package main

import (
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Multi-result producers must not become ambiguous method calls during projection.
func TestDisplayReceiversFromMultipleResults(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "internal/config")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	source := `package config
func PrefixMenuGroups() {}
func Scopes() {}
func AmbiguityVerdict() {}
type SidebarCustomConfig struct{}
func (SidebarCustomConfig) ResolvedTitle() {}
type KeybindRegistry struct{}
func (KeybindRegistry) Report() {}
func (KeybindRegistry) Fate() {}
func (KeybindRegistry) StillHeldBy() {}
type first struct{}
type second struct{}
func (first) Label() string { return "first" }
func (second) Label() string { return "second" }
func (first) Error() string { return "first error" }
func (second) Error() string { return "second error" }
func produce() (one, two second, err error) { return }
func ConfigWarnings() string { _, value, err := produce(); _ = value.Label(); return err.Error() }
func valueSpec() string { var _, value, err = produce(); _ = value.Label(); return err.Error() }
func unresolved() string { value := unknown(); return value.Label() }
`
	if err := os.WriteFile(filepath.Join(dir, "config.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	index, err := displaySources(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"ConfigWarnings", "valueSpec"} {
		fn := index.functions[name]
		if err := index.checkProjection(fn); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		calls := 0
		ast.Inspect(fn.node.Body, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "Label" {
				return true
			}
			calls++
			if got := index.callTarget(sel); got != "second.Label" {
				t.Errorf("%s resolves Label to %q", name, got)
			}
			return true
		})
		if calls != 1 {
			t.Fatalf("%s did not exercise its label call", name)
		}
	}
	if err := index.checkProjection(index.functions["unresolved"]); err == nil || !strings.Contains(err.Error(), "ambiguous display method") {
		t.Fatalf("unknown receiver must still fail closed, got %v", err)
	}
}
