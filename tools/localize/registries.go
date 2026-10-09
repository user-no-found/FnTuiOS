package main

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
)

func registries(root string) ([]entry, error) {
	var result []entry
	constants := map[string]string{}
	labels := map[string]string{}
	dir := filepath.Join(root, "internal/config")
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		return nil, err
	}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, e := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if e != nil {
			return nil, e
		}
		ast.Inspect(file, func(n ast.Node) bool {
			if v, ok := n.(*ast.ValueSpec); ok && len(v.Names) == 1 && len(v.Values) == 1 {
				if s, ok := stringExpr(v.Values[0], constants); ok {
					constants[v.Names[0].Name] = s
				}
			}
			return true
		})
	}
	lf, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "internal/app/settings_registry.go"), nil, 0)
	if err != nil {
		return nil, err
	}
	ast.Inspect(lf, func(n ast.Node) bool {
		if v, ok := n.(*ast.ValueSpec); ok && len(v.Names) == 1 && v.Names[0].Name == "settingLabels" {
			ast.Inspect(v, func(n ast.Node) bool {
				if kv, ok := n.(*ast.KeyValueExpr); ok {
					k, ko := stringExpr(kv.Key, constants)
					val, vo := stringExpr(kv.Value, constants)
					if ko && vo {
						labels[k] = val
					}
				}
				return true
			})
		}
		return true
	})
	of, err := parser.ParseFile(token.NewFileSet(), filepath.Join(root, "internal/config/options.go"), nil, 0)
	if err != nil {
		return nil, err
	}
	ast.Inspect(of, func(n ast.Node) bool {
		cl, ok := n.(*ast.CompositeLit)
		if !ok {
			return true
		}
		fields := map[string]ast.Expr{}
		for _, el := range cl.Elts {
			if kv, ok := el.(*ast.KeyValueExpr); ok {
				if key, ok := kv.Key.(*ast.Ident); ok {
					fields[key.Name] = kv.Value
				}
			}
		}
		path, pok := stringExpr(fields["Path"], constants)
		desc, dok := stringExpr(fields["Description"], constants)
		if !pok {
			return true
		}
		if !dok {
			err = fmt.Errorf("cannot evaluate option description %s", path)
			return false
		}
		label := labels[path]
		if label == "" {
			parts := strings.Split(path, ".")
			label = strings.ReplaceAll(parts[len(parts)-1], "_", " ")
			if label != "" {
				label = strings.ToUpper(label[:1]) + label[1:]
			}
		}
		for field, value := range map[string]string{"label": label, "description": desc} {
			result = append(result, entry{ID: "settings." + path + "." + field, Source: value, Hash: fmt.Sprintf("%x", sha256.Sum256([]byte(value))), File: "internal/config/options.go", Context: path + " display " + field, Kind: "display", Reason: "registry display projection; original config values unchanged"})
		}
		return true
	})
	return result, err
}

func stringExpr(expr ast.Expr, constants map[string]string) (string, bool) {
	switch v := expr.(type) {
	case *ast.BasicLit:
		if v.Kind == token.STRING {
			s, e := strconv.Unquote(v.Value)
			return s, e == nil
		}
		if v.Kind == token.INT {
			return v.Value, true
		}
	case *ast.CallExpr:
		if sel, ok := v.Fun.(*ast.SelectorExpr); ok && sel.Sel.Name == "Sprintf" && len(v.Args) > 0 {
			f, ok := stringExpr(v.Args[0], constants)
			if !ok {
				return "", false
			}
			args := []any{}
			for _, a := range v.Args[1:] {
				s, ok := stringExpr(a, constants)
				if !ok {
					return "", false
				}
				n, e := strconv.Atoi(s)
				if e == nil {
					args = append(args, n)
				} else {
					args = append(args, s)
				}
			}
			return fmt.Sprintf(f, args...), true
		}
	case *ast.Ident:
		s, ok := constants[v.Name]
		return s, ok
	case *ast.BinaryExpr:
		if v.Op == token.ADD {
			a, ok := stringExpr(v.X, constants)
			b, bok := stringExpr(v.Y, constants)
			return a + b, ok && bok
		}
	}
	return "", false
}
