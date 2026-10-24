package architectureguard

import (
	"go/ast"
	"go/token"
	"net/url"
	"path"
	"strconv"
	"strings"
)

// CheckInMemoryTestSQLite checks test entry points and their shared fixture
// packages, including import aliases and same-package metadata constructors.
func CheckInMemoryTestSQLite(root string) error {
	return checkProductionGo(root, goASTPolicy{
		errorHeading: "tests must use in-memory SQLite and injected metadata stores",
		includeTests: true,
		inspect:      inspectTestSQLite,
	})
}

var diskSQLiteEntrypoints = map[string]map[string]bool{
	"core/server/metadata":                   {"Open": true, "OpenAtPath": true, "RegisterBinding": true, "ResolveBinding": true},
	"core/internal/testharness/testsetup":    {"PrepareMetadataPersistenceRoot": true},
	"core/internal/testharness/databaseseed": {"MaterializeCurrentMetadataDatabase": true},
	"core/server/core":                       {"New": true, "NewWithContext": true},
	"core/server/launch":                     {"ResolveBootstrapPlan": true},
	"core/server/startup":                    {"StartServeServer": true, "Start": true},
	"core/internal/testharness/pty/blackbox": {"NewIsolatedEnvironment": true, "newIsolatedEnvironment": true, "startServer": true},
}

var sqliteOwnerPackages = map[string]string{
	"server/metadata": "metadata",
	"server/core":     "core",
	"server/launch":   "launch",
	"server/startup":  "startup",
}

func sameSQLiteOwner(source goSourceFile, identifier *ast.Ident) string {
	directory := path.Dir(source.relativePath)
	if sqliteOwnerPackages[directory] != source.file.Name.Name {
		return ""
	}
	if identifier.Obj != nil && identifier.Obj.Kind != ast.Fun {
		return ""
	}
	return "core/" + directory
}

func sqliteSymbol(expression ast.Expr, source goSourceFile, imports map[string]string, dotImports []string) (string, string) {
	switch reference := expression.(type) {
	case *ast.SelectorExpr:
		if owner, ok := reference.X.(*ast.Ident); ok && owner.Obj == nil {
			return imports[owner.Name], reference.Sel.Name
		}
	case *ast.Ident:
		if owner := sameSQLiteOwner(source, reference); owner != "" {
			return owner, reference.Name
		}
		if reference.Obj == nil {
			for _, imported := range dotImports {
				if diskSQLiteEntrypoints[imported][reference.Name] ||
					imported == "database/sql" && (reference.Name == "Open" || reference.Name == "OpenDB") ||
					imported == "core/server/core" && reference.Name == "NewWithContextOptions" {
					return imported, reference.Name
				}
			}
		}
	}
	return "", ""
}

func inspectTestSQLite(source goSourceFile) []string {
	if !strings.HasSuffix(source.relativePath, "_test.go") &&
		!strings.HasPrefix(source.relativePath, "internal/testharness/") {
		return nil
	}
	imports := make(map[string]string)
	var dotImports []string
	for _, spec := range source.file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			continue
		}
		parts := strings.Split(path, "/")
		name := parts[len(parts)-1]
		if spec.Name != nil {
			name = spec.Name.Name
		}
		if name == "." {
			dotImports = append(dotImports, path)
		}
		imports[name] = path
	}
	var violations []string
	directCalls := make(map[ast.Expr]bool)
	selectorNames := make(map[*ast.Ident]bool)
	ast.Inspect(source.file, func(node ast.Node) bool {
		if call, ok := node.(*ast.CallExpr); ok {
			directCalls[call.Fun] = true
		}
		if selector, ok := node.(*ast.SelectorExpr); ok {
			selectorNames[selector.Sel] = true
		}
		return true
	})
	ast.Inspect(source.file, func(node ast.Node) bool {
		var expression ast.Expr
		switch reference := node.(type) {
		case *ast.SelectorExpr:
			expression = reference
		case *ast.Ident:
			if selectorNames[reference] {
				return true
			}
			expression = reference
		}
		path, name := sqliteSymbol(expression, source, imports, dotImports)
		if expression != nil && !directCalls[expression] &&
			(diskSQLiteEntrypoints[path][name] ||
				path == "database/sql" && (name == "Open" || name == "OpenDB") ||
				path == "core/server/core" && name == "NewWithContextOptions") {
			violations = append(violations, source.detailedViolation(expression, "database constructors must be called directly so memory injection can be checked"))
		}
		return true
	})
	ast.Inspect(source.file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		path, name := sqliteSymbol(call.Fun, source, imports, dotImports)
		if diskSQLiteEntrypoints[path][name] {
			violations = append(violations, source.detailedViolation(call, "use an injected in-memory metadata store instead of "+path+"."+name))
		}
		if path == "core/server/core" && name == "NewWithContextOptions" &&
			(len(call.Args) != 5 || !hasInjectedMetadataStore(call.Args[4])) {
			violations = append(violations, source.detailedViolation(call, "Core fixtures require an inline Options literal with an explicit non-nil MetadataStore"))
		}
		if path == "database/sql" && name == "Open" {
			if len(call.Args) != 2 || !memorySQLiteDSN(call.Args[1], imports) {
				violations = append(violations, source.detailedViolation(call, "use a literal memory DSN or inline net/url.URL with literal mode=memory in url.Values"))
			}
		}
		if path == "database/sql" && name == "OpenDB" {
			violations = append(violations, source.detailedViolation(call, "use the in-memory database fixture instead of an unchecked connector"))
		}
		return true
	})
	return violations
}

func hasInjectedMetadataStore(expression ast.Expr) bool {
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		return false
	}
	for _, element := range literal.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		name, ok := field.Key.(*ast.Ident)
		if ok && name.Name == "MetadataStore" && !isNilExpression(field.Value) {
			return true
		}
	}
	return false
}

func isNilExpression(expression ast.Expr) bool {
	value, ok := expression.(*ast.Ident)
	return ok && value.Name == "nil"
}

func memorySQLiteDSN(expression ast.Expr, imports map[string]string) bool {
	if structuredMemorySQLiteDSN(expression, imports) {
		return true
	}
	literal, ok := expression.(*ast.BasicLit)
	if !ok {
		return false
	}
	dsn, err := strconv.Unquote(literal.Value)
	if err != nil {
		return false
	}
	if dsn == ":memory:" {
		return true
	}
	parsed, err := url.Parse(dsn)
	if err != nil || parsed.Scheme != "file" {
		return false
	}
	values, err := url.ParseQuery(parsed.RawQuery)
	return err == nil && values.Get("mode") == "memory"
}

// Recognize a URL whose storage mode is a literal query value. Other query
// values (such as numeric PRAGMAs) need not be compile-time strings.
func structuredMemorySQLiteDSN(expression ast.Expr, imports map[string]string) bool {
	call, ok := expression.(*ast.CallExpr)
	if !ok || len(call.Args) != 0 {
		return false
	}
	method, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || method.Sel.Name != "String" {
		return false
	}
	receiver := method.X
	if parens, ok := receiver.(*ast.ParenExpr); ok {
		receiver = parens.X
	}
	pointer, ok := receiver.(*ast.UnaryExpr)
	if !ok || pointer.Op != token.AND {
		return false
	}
	literal, ok := pointer.X.(*ast.CompositeLit)
	if !ok || !importedType(literal.Type, imports, "net/url", "URL") {
		return false
	}
	scheme, query := false, false
	for _, element := range literal.Elts {
		field, ok := element.(*ast.KeyValueExpr)
		if !ok {
			return false
		}
		key, ok := field.Key.(*ast.Ident)
		if !ok {
			return false
		}
		switch key.Name {
		case "Scheme":
			scheme = stringLiteral(field.Value, "file")
		case "RawQuery":
			encode, ok := field.Value.(*ast.CallExpr)
			if !ok || len(encode.Args) != 0 {
				continue
			}
			selector, ok := encode.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "Encode" {
				continue
			}
			values, ok := selector.X.(*ast.CompositeLit)
			if !ok || !importedType(values.Type, imports, "net/url", "Values") {
				continue
			}
			for _, entry := range values.Elts {
				pair, ok := entry.(*ast.KeyValueExpr)
				if !ok || !stringLiteral(pair.Key, "mode") {
					continue
				}
				modes, ok := pair.Value.(*ast.CompositeLit)
				query = ok && len(modes.Elts) == 1 && stringLiteral(modes.Elts[0], "memory")
			}
		}
	}
	return scheme && query
}

func importedType(expression ast.Expr, imports map[string]string, path, name string) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != name {
		return false
	}
	owner, ok := selector.X.(*ast.Ident)
	return ok && owner.Obj == nil && imports[owner.Name] == path
}

func stringLiteral(expression ast.Expr, expected string) bool {
	literal, ok := expression.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return false
	}
	value, err := strconv.Unquote(literal.Value)
	return err == nil && value == expected
}
