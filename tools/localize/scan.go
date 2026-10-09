package main

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

func scan(root string) ([]entry, error) {
	var entries []entry
	display, err := displaySources(root)
	if err != nil {
		return nil, err
	}
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if strings.HasPrefix(d.Name(), ".") && path != root || d.Name() == "testdata" || d.Name() == "vendor" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		fset := token.NewFileSet()
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, decl := range file.Decls {
			name := declaration(decl)
			counts := map[string]int{}
			var parents []ast.Node
			ast.Inspect(decl, func(node ast.Node) bool {
				if node == nil {
					parents = parents[:len(parents)-1]
					return true
				}
				parents = append(parents, node)
				lit, ok := node.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					return true
				}
				if !strings.ContainsFunc(value, func(r rune) bool { return unicode.IsLetter(r) }) {
					return true
				}
				sum := fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
				counts[sum]++
				kind, reason := classify(rel, name, lit, parents)
				kind, reason = display.role(rel, name, lit, parents, kind, reason)
				pos := fset.Position(lit.Pos())
				end := fset.Position(lit.End()).Offset
				context := string(content[pos.Offset:end])
				if len(parents) > 1 {
					p := parents[len(parents)-2]
					a, b := fset.Position(p.Pos()).Offset, fset.Position(p.End()).Offset
					if b-a < 500 {
						context = string(content[a:b])
					}
				}
				entries = append(entries, entry{ID: rel + ":" + name + ":" + sum[:12] + fmt.Sprintf(":%d", counts[sum]), Source: value, Hash: sum, File: rel, Context: name + " | " + context, Kind: kind, Reason: reason, Format: formatLiteral(lit, parents) || display.formatted(name, lit, parents) || name == "numberedDescriptions", Line: pos.Line, Start: pos.Offset, End: end})
				return true
			})
		}
		return nil
	})
	return entries, err
}

func declaration(decl ast.Decl) string {
	switch n := decl.(type) {
	case *ast.FuncDecl:
		if n.Recv != nil && len(n.Recv.List) > 0 {
			return receiverName(n.Recv.List[0].Type) + "." + n.Name.Name
		}
		return n.Name.Name
	case *ast.GenDecl:
		for _, spec := range n.Specs {
			switch s := spec.(type) {
			case *ast.ValueSpec:
				if len(s.Names) > 0 {
					return s.Names[0].Name
				}
			case *ast.TypeSpec:
				return s.Name.Name
			}
		}
	}
	return "imports"
}
