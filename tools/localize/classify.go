package main

import (
	"go/ast"
	"go/token"
	"regexp"
	"strconv"
	"strings"
)

func classify(file, decl string, lit *ast.BasicLit, parents []ast.Node) (string, string) {
	value, _ := strconv.Unquote(lit.Value)
	if file == "internal/risk/builtin.go" {
		if strings.HasPrefix(decl, "Rule") {
			return "registry", "builtin risk name displayed separately from its canonical rule identifier"
		}
		for _, node := range parents {
			if field, ok := node.(*ast.KeyValueExpr); ok && directLiteral(field.Value, lit) {
				if key, ok := field.Key.(*ast.Ident); ok && key.Name == "Why" {
					return "registry", "builtin risk explanation rendered in the Inbox"
				}
			}
		}
	}
	if file == "internal/config/keybind_report.go" && (value == " (on a US layout)" || value == " (built in, not configurable)") {
		return "registry", "keybinding fate suffix rendered through action display projection"
	}
	if file == "internal/app/file_ops.go" && decl != "fileOpError" {
		for _, node := range parents {
			if call, ok := node.(*ast.CallExpr); ok {
				if sel, ok := call.Fun.(*ast.SelectorExpr); ok && (sel.Sel.Name == "New" || sel.Sel.Name == "Errorf") && directArgument(call, lit) {
					return "protected", "private filesystem error discriminator routed by fileOpError"
				}
			}
		}
	}
	if file == "internal/app/sidebar_accent.go" && decl == "accentSlotNames" {
		return "registry", "canonical accent payload names with display projection"
	}
	if file == "internal/app/inbox.go" && decl == "inboxReplyChanged" {
		return "protected", "RPC reply discriminator"
	}
	if file == "internal/app/inbox_approvals_ext.go" && decl == "inboxWordPlan" {
		return "registry", "approval kind display projection"
	}
	if file == "internal/app/help.go" && decl == "cat" {
		return "protected", "help category identity"
	}
	if file == "internal/app/help.go" && strings.HasPrefix(decl, "HelpCategory") {
		return "registry", "help category display projection"
	}
	if strings.HasPrefix(decl, "overlayKind") {
		return "protected", "overlay registry identifier"
	}
	if decl == "windowsClipTypes" || decl == "windowsClipImage" {
		return "protected", "fixed PowerShell clipboard program"
	}
	if decl == "imports" {
		return "protected", "Go import path"
	}
	if machinePattern.MatchString(value) {
		return "protected", "structured path, URL, configuration identifier or shortcut"
	}
	for _, node := range parents {
		switch n := node.(type) {
		case *ast.ValueSpec:
			for i, name := range n.Names {
				if machineTarget(name) && i < len(n.Values) && lit.Pos() >= n.Values[i].Pos() && lit.End() <= n.Values[i].End() {
					return "protected", "machine identifier declaration"
				}
			}
		case *ast.IndexExpr:
			if lit.Pos() >= n.Index.Pos() && lit.End() <= n.Index.End() {
				return "protected", "map lookup or indexed identifier"
			}
		case *ast.AssignStmt:
			for i, lhs := range n.Lhs {
				if len(n.Lhs) == len(n.Rhs) && machineValue(n.Rhs[i], lit) && machineTarget(lhs) {
					return "protected", "machine state or configuration assignment"
				}
			}
		case *ast.CallExpr:
			if id, ok := n.Fun.(*ast.Ident); ok {
				switch id.Name {
				case "clientLog", "kittyPassthroughLog", "sixelPassthroughLog", "traceSync", "traceLayerHold":
					if directArgument(n, lit) {
						return "protected", "developer diagnostic text"
					}
				case "opt", "custom", "row", "menu", "railKey", "reviewNoteCmd", "handleAction", "dispatchAction", "lookupAction":
					if len(n.Args) > 0 && directLiteral(n.Args[0], lit) {
						return "protected", "registry action or configuration argument"
					}
				}
				if id.Name == "sidebarNoteKeepLast" && len(n.Args) > 2 && directLiteral(n.Args[2], lit) {
					return "protected", "sidebar token discriminator"
				}
				if id.Name == "sidebarAgentMetaValue" && len(n.Args) > 1 && directLiteral(n.Args[1], lit) {
					return "protected", "agent metadata field name"
				}
				if id.Name == "traceRender" && directArgument(n, lit) {
					return "protected", "renderer diagnostic branch identifier"
				}
				if id.Name == "layer" && len(n.Args) > 3 && lit.Pos() >= n.Args[3].Pos() {
					return "protected", "compositor layer identifier"
				}
			}
			if sel, ok := n.Fun.(*ast.SelectorExpr); ok {
				owner := ""
				if id, ok := sel.X.(*ast.Ident); ok {
					owner = id.Name
				}
				if env, ok := sel.X.(*ast.SelectorExpr); ok && env.Sel.Name == "env" && sel.Sel.Name == "run" {
					return "protected", "fixed subprocess arguments"
				}
				if sel.Sel.Name == "sidebarAgentTokenValue" && len(n.Args) > 0 && directLiteral(n.Args[0], lit) {
					return "protected", "sidebar token identifier"
				}
				if sel.Sel.Name == "PrefixMenuGroups" && len(n.Args) > 1 && directLiteral(n.Args[1], lit) {
					return "protected", "prefix menu group identifier"
				}
				if sel.Sel.Name == "placeOverlayPanel" && len(n.Args) > 1 && directLiteral(n.Args[1], lit) {
					return "protected", "overlay registry identifier"
				}
				if sel.Sel.Name == "overlayOffset" && len(n.Args) > 0 && directLiteral(n.Args[0], lit) {
					return "protected", "overlay registry identifier"
				}
				if (sel.Sel.Name == "ShowNotification" || sel.Sel.Name == "ShowNotificationFrom" || sel.Sel.Name == "ShowNotificationCmd") && len(n.Args) > 1 && directLiteral(n.Args[1], lit) {
					return "protected", "notification severity"
				}
				if (sel.Sel.Name == "Call" || sel.Sel.Name == "CallWithTimeout") && len(n.Args) > 0 {
					return "protected", "RPC verb"
				}
				if !directArgument(n, lit) {
					continue
				}
				if owner == "filepath" || owner == "path" || owner == "exec" || owner == "os" || owner == "regexp" || owner == "lazyre" || (owner == "q" && sel.Sel.Name == "Set") {
					return "protected", "system path, process or expression argument"
				}
				if owner == "strings" {
					switch sel.Sel.Name {
					case "Cut", "HasPrefix", "HasSuffix", "TrimPrefix", "TrimSuffix", "CutPrefix", "CutSuffix", "Contains", "EqualFold", "Index", "IndexByte", "ReplaceAll", "Split", "Trim", "TrimLeft", "TrimRight":
						return "protected", "string parsing discriminator"
					}
				}
				switch sel.Sel.Name {
				case "LogInfo", "LogWarn", "LogError", "LogDebug", "NewProc", "NewLazySystemDLL", "Format":
					return "protected", "diagnostic, platform symbol or date format argument"
				case "getenv", "lookPath", "lp", "ID", "GetBindingsForAction", "LookupOption", "GetOptionValue", "SetOptionValue", "Getenv", "LookupEnv", "SendKeys", "SendText", "ExecuteAction", "HandleAction":
					return "protected", "configuration, protocol or terminal-input argument"
				}
			}

		case *ast.Field:
			if n.Tag == lit {
				return "protected", "serialization tag"
			}
		case *ast.CaseClause:
			for _, x := range n.List {
				if directLiteral(x, lit) {
					return "protected", "switch discriminator"
				}
			}
		case *ast.KeyValueExpr:
			if key, ok := n.Key.(*ast.Ident); ok && key.Name == "Name" && file == "internal/app/help.go" {
				return "registry", "help category display projection"
			}
			if directLiteral(n.Key, lit) {
				return "protected", "map or registry key"
			}
			if k, ok := n.Key.(*ast.Ident); ok && (k.Name == "Shortcut" || k.Name == "Keys" || k.Name == "Shortcuts") {
				return "protected", "keyboard shortcut or binding keys"
			}
			if k, ok := n.Key.(*ast.Ident); ok && directLiteral(n.Value, lit) {
				switch k.Name {
				case "Path", "ID", "Key", "Action", "Command", "Protocol", "Type", "Method", "Code", "Kind", "State", "Status", "Category", "TerminalName", "Icon", "Tag", "Extension", "Pattern", "URL", "Env":
					return "protected", "machine-addressable field " + k.Name
				}
			}
		case *ast.BinaryExpr:
			if (n.Op == token.EQL || n.Op == token.NEQ) && (directLiteral(n.X, lit) || directLiteral(n.Y, lit)) {
				return "protected", "comparison discriminator"
			}
		}
	}
	if strings.ContainsAny(value, "\x1b\x00") {
		return "protected", "terminal control sequence"
	}
	ui := strings.HasPrefix(file, "internal/app/") || strings.HasPrefix(file, "internal/input/") || strings.HasPrefix(file, "internal/overlay/") || strings.HasPrefix(file, "internal/scrollback/") || strings.HasPrefix(file, "internal/sessiontree/")
	if file == "internal/config/keybind_descriptions.go" && (decl == "scopedDescriptions" || decl == "numberedDescriptions") {
		return "registry", "scoped keybinding description display projection"
	}
	if file == "internal/config/registry.go" && decl == "ActionDescriptions" {
		return "registry", "action description display projection"
	}
	if file == "internal/config/options.go" {
		for _, node := range parents {
			if kv, ok := node.(*ast.KeyValueExpr); ok {
				if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Description" {
					return "protected", "option description translated only through settings projection"
				}
			}
		}
	}
	if ui && tupleMachineField(lit, parents) {
		return "protected", "unkeyed machine field in typed tuple"
	}
	if ui {
		return "candidate", "native UI package; human review decides display versus machine value"
	}
	return "inventory", "outside current native display packages; upstream changes require inventory review"
}

var machinePattern = regexp.MustCompile(`^(?:[A-Z][A-Z0-9_]*=.*|[a-z][a-z0-9_]*(?:\.[a-z0-9_]+)+|[a-z][a-z0-9]*(?:_[a-z0-9]+)+|(?:https?|wss?)://\S+|(?:/|\./|\.\./)[^ ]+|(?:ctrl|alt|shift|super|opt)\+\S+)$`)

func machineTarget(expr ast.Expr) bool {
	name := ""
	switch x := expr.(type) {
	case *ast.Ident:
		name = x.Name
	case *ast.SelectorExpr:
		name = x.Sel.Name
	}
	lower := strings.ToLower(name)
	switch lower {
	case "state", "status", "action", "key", "path", "id", "mode", "kind", "protocol", "section", "category", "scheme", "shell", "command", "pattern", "host", "endpoint":
		return true
	}
	return strings.HasSuffix(name, "ID") || strings.HasSuffix(name, "Path") || strings.HasSuffix(name, "Key") || strings.HasSuffix(name, "Action") || strings.HasSuffix(name, "Mode") || strings.HasSuffix(name, "Actions") || strings.HasSuffix(name, "Keys")
}

func directLiteral(expr ast.Expr, lit *ast.BasicLit) bool {
	if expr == lit {
		return true
	}
	switch x := expr.(type) {
	case *ast.BinaryExpr:
		return directLiteral(x.X, lit) || directLiteral(x.Y, lit)
	case *ast.ParenExpr:
		return directLiteral(x.X, lit)
	}
	return false
}
func directArgument(call *ast.CallExpr, lit *ast.BasicLit) bool {
	for _, arg := range call.Args {
		if directLiteral(arg, lit) {
			return true
		}
	}
	return false
}
func formatLiteral(lit *ast.BasicLit, parents []ast.Node) bool {
	for _, node := range parents {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			continue
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			continue
		}
		index := -1
		switch sel.Sel.Name {
		case "Sprintf", "Errorf", "Printf":
			index = 0
		case "Fprintf", "Appendf":
			index = 1
		}
		if index >= 0 && len(call.Args) > index && directLiteral(call.Args[index], lit) {
			return true
		}
	}
	return false
}

func receiverName(expr ast.Expr) string {
	switch x := expr.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.StarExpr:
		return receiverName(x.X)
	case *ast.IndexExpr:
		return receiverName(x.X)
	}
	return "receiver"
}

func tupleMachineField(lit *ast.BasicLit, parents []ast.Node) bool {
	for i, node := range parents {
		inner, ok := node.(*ast.CompositeLit)
		if !ok {
			continue
		}
		var fields *ast.StructType
		if own, ok := inner.Type.(*ast.StructType); ok {
			fields = own
		}
		if fields == nil && inner.Type == nil {
			for j := i - 1; j >= 0; j-- {
				if parent, ok := parents[j].(*ast.CompositeLit); ok {
					if array, ok := parent.Type.(*ast.ArrayType); ok {
						fields, _ = array.Elt.(*ast.StructType)
					}
					break
				}
			}
		}
		if fields == nil {
			continue
		}
		names := []*ast.Ident{}
		for _, f := range fields.Fields.List {
			names = append(names, f.Names...)
		}
		for j, element := range inner.Elts {
			if j < len(names) && machineTarget(names[j]) && lit.Pos() >= element.Pos() && lit.End() <= element.End() {
				return true
			}
		}
	}
	return false
}

func machineValue(expr ast.Expr, lit *ast.BasicLit) bool {
	if directLiteral(expr, lit) {
		return true
	}
	if list, ok := expr.(*ast.CompositeLit); ok {
		for _, element := range list.Elts {
			if value, ok := element.(ast.Expr); ok && machineValue(value, lit) {
				return true
			}
		}
	}
	return false
}
