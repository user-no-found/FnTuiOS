package main

import "go/ast"

func compositeName(parents []ast.Node) string {
	for i := len(parents) - 1; i >= 0; i-- {
		lit, ok := parents[i].(*ast.CompositeLit)
		if !ok || lit.Type == nil {
			continue
		}
		var expr ast.Expr = lit.Type
		if array, ok := expr.(*ast.ArrayType); ok {
			expr = array.Elt
		}
		if name, ok := expr.(*ast.Ident); ok {
			return name.Name
		}
	}
	return ""
}

func guestDisplayLiteral(lit *ast.BasicLit, parents []ast.Node) bool {
	typeName := compositeName(parents)
	if typeName != "GuestProgram" && typeName != "Ambiguity" {
		return false
	}
	for i := len(parents) - 1; i >= 0; i-- {
		field, ok := parents[i].(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		if directLiteral(field.Key, lit) {
			return false
		}
		if name, ok := field.Key.(*ast.Ident); ok {
			if typeName == "Ambiguity" {
				return name.Name == "Why"
			}
			return name.Name == "Name" || name.Name == "Note" || name.Name == "Keys"
		}
	}
	return false
}

// menuDisplayLiteral follows the typed menu tables, including menuPart's
// positional description. Action identifiers remain canonical.
func menuDisplayLiteral(lit *ast.BasicLit, parents []ast.Node) bool {
	typeName := compositeName(parents)
	if typeName != "menuPart" && typeName != "menuRow" && typeName != "menuSection" {
		return false
	}
	for i := len(parents) - 1; i >= 0; i-- {
		if field, ok := parents[i].(*ast.KeyValueExpr); ok && directLiteral(field.Value, lit) {
			key, ok := field.Key.(*ast.Ident)
			return ok && displayParameter(key.Name)
		}
		if object, ok := parents[i].(*ast.CompositeLit); ok {
			for position, value := range object.Elts {
				if directLiteral(value, lit) {
					return typeName == "menuPart" && position == 1
				}
			}
		}
	}
	return false
}

func formatWrapper(fn *ast.FuncLit) int {
	params := parameterNames(fn.Type)
	if len(params) == 0 {
		return -1
	}
	found := -1
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || sel.Sel.Name != "Sprintf" {
			return true
		}
		owner, ok := sel.X.(*ast.Ident)
		first, isName := call.Args[0].(*ast.Ident)
		if ok && owner.Name == "fmt" && isName {
			for position, name := range params {
				if first.Name == name {
					found = position
				}
			}
		}
		return true
	})
	return found
}

func (index *displayIndex) formatted(decl string, lit *ast.BasicLit, parents []ast.Node) bool {
	fn := index.functions[decl]
	if fn == nil {
		return false
	}
	for _, node := range parents {
		call, ok := node.(*ast.CallExpr)
		if !ok || len(call.Args) == 0 {
			continue
		}
		name, ok := call.Fun.(*ast.Ident)
		if ok {
			if position, exists := fn.formatters[name.Name]; exists && position >= 0 && position < len(call.Args) && directLiteral(call.Args[position], lit) {
				return true
			}
		}
	}
	return false
}
