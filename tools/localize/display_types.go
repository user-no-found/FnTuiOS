package main

import "go/ast"

// expressionType resolves declared receivers needed by display dependencies.
// Unknown receivers stay unknown and ambiguous local method names fail closed.
func (index *displayIndex) expressionType(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.StarExpr:
		return index.expressionType(value.X)
	case *ast.UnaryExpr:
		return index.expressionType(value.X)
	case *ast.CompositeLit:
		return declaredType(value.Type)
	case *ast.SelectorExpr:
		owner := index.expressionType(value.X)
		if fields := index.structs[owner]; fields != nil {
			return declaredType(fields[value.Sel.Name])
		}
	case *ast.CallExpr:
		return index.expressionResultType(value, 0)
	case *ast.Ident:
		if value.Obj == nil {
			return ""
		}
		switch declaration := value.Obj.Decl.(type) {
		case *ast.Field:
			return declaredType(declaration.Type)
		case *ast.ValueSpec:
			if declaration.Type != nil {
				return declaredType(declaration.Type)
			}
			for i, name := range declaration.Names {
				if name.Name == value.Name {
					if len(declaration.Values) == 1 && len(declaration.Names) > 1 {
						return index.expressionResultType(declaration.Values[0], i)
					}
					if i < len(declaration.Values) {
						return index.expressionType(declaration.Values[i])
					}
				}
			}
		case *ast.AssignStmt:
			for i, left := range declaration.Lhs {
				if name, ok := left.(*ast.Ident); ok && name.Name == value.Name {
					if len(declaration.Rhs) == 1 && len(declaration.Lhs) > 1 {
						return index.expressionResultType(declaration.Rhs[0], i)
					}
					if i < len(declaration.Rhs) {
						return index.expressionType(declaration.Rhs[i])
					}
				}
			}
		}
	}
	return ""
}

func declaredType(expr ast.Expr) string {
	switch value := expr.(type) {
	case *ast.Ident:
		return value.Name
	case *ast.StarExpr:
		return declaredType(value.X)
	case *ast.SelectorExpr:
		return "external"
	}
	return ""
}

// expressionResultType resolves one slot of a declared multi-result producer.
// Unknown producers remain unknown; the projection guard still rejects ambiguity.
func (index *displayIndex) expressionResultType(expr ast.Expr, position int) string {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		if position == 0 {
			return index.expressionType(expr)
		}
		return ""
	}
	var fn *displayFunction
	switch fun := call.Fun.(type) {
	case *ast.Ident:
		fn = index.functions[fun.Name]
	case *ast.SelectorExpr:
		if owner, ok := fun.X.(*ast.Ident); ok && owner.Obj == nil && index.packages[owner.Name] {
			return "external"
		}
		if receiver := index.expressionType(fun.X); receiver != "" {
			fn = index.functions[receiver+"."+fun.Sel.Name]
		}
	}
	if fn == nil || fn.node.Type.Results == nil {
		return ""
	}
	for _, field := range fn.node.Type.Results.List {
		slots := max(1, len(field.Names))
		if position < slots {
			return declaredType(field.Type)
		}
		position -= slots
	}
	return ""
}
