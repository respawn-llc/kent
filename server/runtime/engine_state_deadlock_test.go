package runtime

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestEngineStateAccessorsAreNotCalledWhileEngineMutexHeld(t *testing.T) {
	forbidden := map[string]struct{}{
		"compactionPlanningSnapshot": {},
		"lockedContractState":        {},
		"modelRequests":              {},
		"transcriptPersistence":      {},
		"transcriptRuntimeState":     {},
	}

	fset := token.NewFileSet()
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("glob runtime go files: %v", err)
	}
	var failures []string
	for _, path := range files {
		source, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		file, err := parser.ParseFile(fset, path, source, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			scanEngineMutexHeldAccessors(fset, fn.Body.List, map[string]bool{}, forbidden, &failures)
		}
	}
	sort.Strings(failures)
	if len(failures) > 0 {
		t.Fatalf("state accessors called while Engine.mu may be held:\n%s", strings.Join(failures, "\n"))
	}
}

func scanEngineMutexHeldAccessors(fset *token.FileSet, stmts []ast.Stmt, locked map[string]bool, forbidden map[string]struct{}, failures *[]string) {
	for _, stmt := range stmts {
		if recv, ok := engineMutexCall(stmt, "Lock"); ok {
			locked[recv] = true
			continue
		}
		if recv, ok := engineMutexCall(stmt, "Unlock"); ok {
			locked[recv] = false
			continue
		}

		ast.Inspect(stmt, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if _, blocked := forbidden[selector.Sel.Name]; !blocked {
				return true
			}
			ident, ok := selector.X.(*ast.Ident)
			if !ok || !locked[ident.Name] {
				return true
			}
			*failures = append(*failures, fmt.Sprintf("%s: %s.%s()", fset.Position(call.Pos()), ident.Name, selector.Sel.Name))
			return true
		})

		switch s := stmt.(type) {
		case *ast.BlockStmt:
			scanEngineMutexHeldAccessors(fset, s.List, cloneLockState(locked), forbidden, failures)
		case *ast.IfStmt:
			if s.Init != nil {
				scanEngineMutexHeldAccessors(fset, []ast.Stmt{s.Init}, locked, forbidden, failures)
			}
			scanEngineMutexHeldAccessors(fset, s.Body.List, cloneLockState(locked), forbidden, failures)
			if s.Else != nil {
				scanEngineMutexHeldAccessors(fset, []ast.Stmt{s.Else}, cloneLockState(locked), forbidden, failures)
			}
		case *ast.ForStmt:
			scanEngineMutexHeldAccessors(fset, s.Body.List, cloneLockState(locked), forbidden, failures)
		case *ast.RangeStmt:
			scanEngineMutexHeldAccessors(fset, s.Body.List, cloneLockState(locked), forbidden, failures)
		case *ast.SwitchStmt:
			for _, stmt := range s.Body.List {
				if clause, ok := stmt.(*ast.CaseClause); ok {
					scanEngineMutexHeldAccessors(fset, clause.Body, cloneLockState(locked), forbidden, failures)
				}
			}
		case *ast.TypeSwitchStmt:
			for _, stmt := range s.Body.List {
				if clause, ok := stmt.(*ast.CaseClause); ok {
					scanEngineMutexHeldAccessors(fset, clause.Body, cloneLockState(locked), forbidden, failures)
				}
			}
		case *ast.SelectStmt:
			for _, stmt := range s.Body.List {
				if clause, ok := stmt.(*ast.CommClause); ok {
					scanEngineMutexHeldAccessors(fset, clause.Body, cloneLockState(locked), forbidden, failures)
				}
			}
		}
	}
}

func engineMutexCall(stmt ast.Stmt, method string) (string, bool) {
	expr, ok := stmt.(*ast.ExprStmt)
	if !ok {
		return "", false
	}
	call, ok := expr.X.(*ast.CallExpr)
	if !ok {
		return "", false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != method {
		return "", false
	}
	mu, ok := selector.X.(*ast.SelectorExpr)
	if !ok || mu.Sel.Name != "mu" {
		return "", false
	}
	recv, ok := mu.X.(*ast.Ident)
	if !ok {
		return "", false
	}
	return recv.Name, true
}

func cloneLockState(in map[string]bool) map[string]bool {
	out := make(map[string]bool, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
