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
		if name, ok := value.Fun.(*ast.Ident); ok {
			if fn := index.functions[name.Name]; fn != nil && fn.node.Type.Results != nil && len(fn.node.Type.Results.List) > 0 {
				return declaredType(fn.node.Type.Results.List[0].Type)
			}
		}
		if method, ok := value.Fun.(*ast.SelectorExpr); ok {
			if owner, ok := method.X.(*ast.Ident); ok && owner.Obj == nil && index.packages[owner.Name] {
				return "external"
			}
		}
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
				if name.Name == value.Name && i < len(declaration.Values) {
					return index.expressionType(declaration.Values[i])
				}
			}
		case *ast.AssignStmt:
			for i, left := range declaration.Lhs {
				if name, ok := left.(*ast.Ident); ok && name.Name == value.Name && i < len(declaration.Rhs) {
					return index.expressionType(declaration.Rhs[i])
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
