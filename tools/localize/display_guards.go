package main

import (
	"fmt"
	"go/ast"
)

func (index *displayIndex) checkProjection(fn *displayFunction) error {
	if displayBarriers[fn.name] {
		return fmt.Errorf("routing cache cannot be a display projection: %s", fn.name)
	}
	receiver := ""
	if fn.node.Recv != nil && len(fn.node.Recv.List[0].Names) > 0 {
		receiver = fn.node.Recv.List[0].Names[0].Name
	}
	var failure error
	checkWrite := func(expr ast.Expr) {
		root := assignmentRoot(expr)
		if root != nil && (root.Name == receiver || (index.globals[root.Name] != nil && (root.Obj == nil || root.Obj.Decl == index.globals[root.Name]))) {
			failure = fmt.Errorf("display producer mutates shared state: %s", fn.name)
		}
	}
	ast.Inspect(fn.node.Body, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			if name, ok := call.Fun.(*ast.Ident); ok && (name.Name == "delete" || name.Name == "clear") && len(call.Args) > 0 {
				checkWrite(call.Args[0])
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok && len(index.methods[sel.Sel.Name]) > 1 && index.expressionType(sel.X) == "" {
				if owner, ok := sel.X.(*ast.Ident); !ok || owner.Obj != nil || !index.packages[owner.Name] {
					failure = fmt.Errorf("ambiguous display method in %s: %s", fn.name, sel.Sel.Name)
				}
			}
		}
		if assign, ok := node.(*ast.AssignStmt); ok {
			for _, lhs := range assign.Lhs {
				checkWrite(lhs)
			}
		}
		if mutation, ok := node.(*ast.IncDecStmt); ok {
			checkWrite(mutation.X)
		}
		return true
	})
	return failure
}

func assignmentRoot(expr ast.Expr) *ast.Ident {
	switch value := expr.(type) {
	case *ast.Ident:
		return value
	case *ast.SelectorExpr:
		return assignmentRoot(value.X)
	case *ast.IndexExpr:
		return assignmentRoot(value.X)
	case *ast.StarExpr:
		return assignmentRoot(value.X)
	}
	return nil
}
