package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// Display roots are the configuration APIs consumed by native UI renderers.
// Their dependency graph follows source symbols across files, not file names.
var displayRoots = []string{"PrefixMenuGroups", "ConfigWarnings", "SidebarCustomConfig.ResolvedTitle", "Scopes", "KeybindRegistry.Report", "KeybindRegistry.Fate", "AmbiguityVerdict", "KeybindRegistry.StillHeldBy"}

// Cached routing providers stay canonical; renderers project their returned labels.
var displayBarriers = map[string]bool{"NewKeybindRegistry": true, "KeybindRegistry.buildMappings": true, "KeybindRegistry.liveScope": true, "KeybindRegistry.Bindings": true}

type displayFunction struct {
	name, file string
	node       *ast.FuncDecl
	imports    []*ast.ImportSpec
	params     []string
	calls      map[string]bool
	locals     map[string][]string
	formatters map[string]int
	values     map[string]bool
}

type displayValue struct {
	file    string
	node    *ast.ValueSpec
	kind    token.Token
	imports []*ast.ImportSpec
}

type displayIndex struct {
	fset      *token.FileSet
	functions map[string]*displayFunction
	methods   map[string][]string
	reachable map[string]bool
	constants map[string]*ast.ValueSpec
	constFile map[string]string
	usedConst map[string]bool
	packages  map[string]bool
	values    map[string]displayValue
	textNodes map[string]bool
	textConst map[string]bool
	structs   map[string]map[string]ast.Expr
	globals   map[string]ast.Node
}

func displaySources(root string) (*displayIndex, error) {
	index := &displayIndex{fset: token.NewFileSet(), functions: map[string]*displayFunction{}, methods: map[string][]string{}, reachable: map[string]bool{}, constants: map[string]*ast.ValueSpec{}, constFile: map[string]string{}, usedConst: map[string]bool{}, packages: map[string]bool{}, values: map[string]displayValue{}, textNodes: map[string]bool{}, textConst: map[string]bool{}, structs: map[string]map[string]ast.Expr{}, globals: map[string]ast.Node{}}
	paths, err := filepath.Glob(filepath.Join(root, "internal/config/*.go"))
	if err != nil {
		return nil, err
	}
	for _, path := range paths {
		if strings.HasSuffix(path, "_test.go") || strings.HasPrefix(filepath.Base(path), "fntuios_display_") {
			continue
		}
		file, err := parser.ParseFile(index.fset, path, nil, 0)
		if err != nil {
			return nil, err
		}
		rel, _ := filepath.Rel(root, path)
		for _, imported := range file.Imports {
			index.packages[importName(imported)] = true
		}
		for _, decl := range file.Decls {
			if group, ok := decl.(*ast.GenDecl); ok && group.Tok == token.TYPE {
				for _, spec := range group.Specs {
					named := spec.(*ast.TypeSpec)
					if object, ok := named.Type.(*ast.StructType); ok {
						index.structs[named.Name.Name] = map[string]ast.Expr{}
						for _, field := range object.Fields.List {
							for _, name := range field.Names {
								index.structs[named.Name.Name][name.Name] = field.Type
							}
						}
					}
				}
			}
			if fn, ok := decl.(*ast.FuncDecl); ok {
				name := declaration(fn)
				if index.functions[name] != nil {
					return nil, fmt.Errorf("ambiguous display dependency %s", name)
				}
				index.functions[name] = &displayFunction{name: name, file: rel, node: fn, imports: file.Imports, params: parameterNames(fn.Type), calls: map[string]bool{}, locals: map[string][]string{}, formatters: map[string]int{}, values: map[string]bool{}}
				if fn.Recv != nil {
					index.methods[fn.Name.Name] = append(index.methods[fn.Name.Name], name)
				}
			}
			if group, ok := decl.(*ast.GenDecl); ok && (group.Tok == token.CONST || group.Tok == token.VAR) {
				for _, spec := range group.Specs {
					value := spec.(*ast.ValueSpec)
					for _, name := range value.Names {
						index.globals[name.Name] = value
					}
					if len(value.Names) == 1 && len(value.Values) == 1 {
						index.values[value.Names[0].Name] = displayValue{file: rel, node: value, kind: group.Tok, imports: file.Imports}
						if lit, ok := value.Values[0].(*ast.BasicLit); ok && lit.Kind == token.STRING {
							index.constants[value.Names[0].Name] = value
							index.constFile[value.Names[0].Name] = rel
						}
					}
				}
			}
		}
	}
	for _, fn := range index.functions {
		if displayBarriers[fn.name] {
			continue
		}
		ast.Inspect(fn.node.Body, func(node ast.Node) bool {
			if id, ok := node.(*ast.Ident); ok && index.values[id.Name].node != nil && (id.Obj == nil || id.Obj.Decl == index.values[id.Name].node) {
				fn.values[id.Name] = true
			}
			if call, ok := node.(*ast.CallExpr); ok {
				if target := index.callTarget(call.Fun); target != "" {
					fn.calls[target] = true
				}
			}
			if assign, ok := node.(*ast.AssignStmt); ok {
				for i, value := range assign.Rhs {
					if fun, ok := value.(*ast.FuncLit); ok && i < len(assign.Lhs) {
						if name, ok := assign.Lhs[i].(*ast.Ident); ok {
							fn.locals[name.Name] = parameterNames(fun.Type)
							fn.formatters[name.Name] = formatWrapper(fun)
						}
					}
				}
			}
			return true
		})
	}
	var visit func(string)
	visit = func(name string) {
		if index.reachable[name] {
			return
		}
		index.reachable[name] = true
		fn := index.functions[name]
		if displayBarriers[name] {
			return
		}
		for called := range fn.calls {
			visit(called)
		}
		ast.Inspect(fn.node.Body, func(node ast.Node) bool {
			if id, ok := node.(*ast.Ident); ok && index.constants[id.Name] != nil {
				index.usedConst[id.Name] = true
			}
			return true
		})
	}
	for _, name := range displayRoots {
		if index.functions[name] == nil {
			return nil, fmt.Errorf("native display API is missing: %s", name)
		}
		visit(name)
	}
	index.traceDisplayReturns()
	return index, nil
}

func parameterNames(fn *ast.FuncType) []string {
	var names []string
	for _, field := range fn.Params.List {
		for _, name := range field.Names {
			names = append(names, name.Name)
		}
	}
	return names
}

func (index *displayIndex) callTarget(expr ast.Expr) string {
	if id, ok := expr.(*ast.Ident); ok && index.functions[id.Name] != nil && (id.Obj == nil || id.Obj.Kind == ast.Fun) {
		return id.Name
	}
	if sel, ok := expr.(*ast.SelectorExpr); ok {
		if owner, ok := sel.X.(*ast.Ident); ok && owner.Obj == nil && index.packages[owner.Name] {
			return ""
		}
		if receiver := index.expressionType(sel.X); receiver != "" {
			if name := receiver + "." + sel.Sel.Name; index.functions[name] != nil {
				return name
			}
			return ""
		}
		if len(index.methods[sel.Sel.Name]) == 1 {
			return index.methods[sel.Sel.Name][0]
		}
	}
	return ""
}

func machineParameter(name string) bool {
	switch strings.ToLower(name) {
	case "action", "actions", "prefix", "key", "keys", "chord", "comms", "field", "scope", "section", "sectionname", "path", "command", "protocol", "verb":
		return true
	}
	return false
}

func displayParameter(name string) bool {
	switch strings.ToLower(name) {
	case "desc", "description", "title", "label", "message", "surface", "warning", "problem", "hint":
		return true
	}
	return strings.HasSuffix(name, "Title") || strings.HasSuffix(name, "Label")
}

func (index *displayIndex) role(file, decl string, lit *ast.BasicLit, parents []ast.Node, kind, reason string) (string, string) {
	if !strings.HasPrefix(file, "internal/config/") {
		return kind, reason
	}
	if guestDisplayLiteral(lit, parents) {
		return "config-display", "typed curated display field; canonical table is unchanged"
	}
	fn := index.functions[decl]
	if fn == nil || !index.reachable[decl] {
		name := decl
		for _, node := range parents {
			if spec, ok := node.(*ast.ValueSpec); ok && len(spec.Names) == 1 {
				name = spec.Names[0].Name
			}
		}
		if index.textConst[name] && kind == "inventory" {
			return "config-display", "display API default label constant"
		}
		return kind, reason
	}
	semanticDisplay := index.textNodes[fmt.Sprintf("%s:%d", file, int(lit.Pos())-1)]
	for _, node := range parents {
		if call, ok := node.(*ast.CallExpr); ok {
			var params []string
			if target := index.callTarget(call.Fun); target != "" {
				params = index.functions[target].params
			} else if id, ok := call.Fun.(*ast.Ident); ok {
				params = fn.locals[id.Name]
			}
			for position, arg := range call.Args {
				if position >= len(params) || !directLiteral(arg, lit) {
					continue
				}
				if machineParameter(params[position]) {
					return "protected", "display provider machine parameter " + params[position]
				}
				semanticDisplay = semanticDisplay || displayParameter(params[position])
			}
		}
		if field, ok := node.(*ast.KeyValueExpr); ok && directLiteral(field.Value, lit) {
			if key, ok := field.Key.(*ast.Ident); ok {
				if machineParameter(key.Name) {
					return "protected", "display provider machine field " + key.Name
				}
				semanticDisplay = semanticDisplay || displayParameter(key.Name) || (key.Name == "Name" && compositeName(parents) == "Scope")
			}
		}
	}
	if kind == "protected" && reason == "registry action or configuration argument" && semanticDisplay {
		return "config-display", "resolved display parameter overrides a shadowed helper name"
	}
	if kind != "inventory" {
		return kind, reason
	}
	value, _ := strconv.Unquote(lit.Value)
	humanPhrase := strings.ContainsFunc(value, unicode.IsSpace) && strings.ContainsFunc(value, unicode.IsLetter)
	if semanticDisplay || humanPhrase {
		return "config-display", "native display API dependency; literal projected before formatting"
	}
	return kind, reason
}
