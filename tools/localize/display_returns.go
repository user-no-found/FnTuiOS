package main

import (
	"fmt"
	"go/ast"
)

// traceDisplayReturns follows helper return values used in named display fields
// and parameters. It catches a one-word label in a newly introduced source file.
func (index *displayIndex) traceDisplayReturns() {
	returns := map[string]bool{"SidebarCustomConfig.ResolvedTitle": true, "AmbiguityVerdict": true}
	for changed := true; changed; {
		changed = false
		for name, fn := range index.functions {
			if !index.reachable[name] || displayBarriers[name] {
				continue
			}
			var text func(ast.Expr)
			text = func(expr ast.Expr) {
				switch value := expr.(type) {
				case *ast.BasicLit:
					index.textNodes[fmt.Sprintf("%s:%d", fn.file, index.fset.Position(value.Pos()).Offset)] = true
				case *ast.BinaryExpr:
					text(value.X)
					text(value.Y)
				case *ast.ParenExpr:
					text(value.X)
				case *ast.CallExpr:
					if target := index.callTarget(value.Fun); target != "" && !returns[target] {
						returns[target], changed = true, true
					}
				case *ast.Ident:
					if index.constants[value.Name] != nil {
						index.textConst[value.Name] = true
					}
				}
			}
			ast.Inspect(fn.node.Body, func(node ast.Node) bool {
				if field, ok := node.(*ast.KeyValueExpr); ok {
					if key, ok := field.Key.(*ast.Ident); ok && displayParameter(key.Name) {
						text(field.Value)
					}
				}
				if result, ok := node.(*ast.ReturnStmt); ok && returns[name] {
					for _, value := range result.Results {
						text(value)
					}
				}
				if call, ok := node.(*ast.CallExpr); ok {
					var params []string
					if target := index.callTarget(call.Fun); target != "" {
						params = index.functions[target].params
					} else if id, ok := call.Fun.(*ast.Ident); ok {
						params = fn.locals[id.Name]
					}
					for position, arg := range call.Args {
						if position < len(params) && displayParameter(params[position]) {
							text(arg)
						}
					}
				}
				return true
			})
		}
	}
}
