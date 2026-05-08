package client

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"testing"

	"builder/shared/rpccontract"
)

type remoteRouteCall struct {
	connection rpccontract.ConnectionStrategy
	requestID  string
	methodName string
}

func TestRemoteClientRoutesMatchRPCContractConnectionStrategy(t *testing.T) {
	calls := remoteRouteCalls(t)
	for _, route := range rpccontract.Routes() {
		if route.Kind == rpccontract.KindNotification || route.Dependency == rpccontract.DependencyProtocol {
			continue
		}
		call, ok := calls[route.Method]
		if !ok {
			t.Fatalf("remote client missing binding for route %q", route.Method)
		}
		if call.connection != route.Connection {
			t.Fatalf("remote route %q connection = %q, want %q", route.Method, call.connection, route.Connection)
		}
		if route.Connection == rpccontract.ConnectionDedicated && call.requestID != route.DedicatedRequestID {
			t.Fatalf("remote route %q dedicated request id = %q, want %q", route.Method, call.requestID, route.DedicatedRequestID)
		}
	}
}

func TestRemoteClientRouteTypesMatchRPCContract(t *testing.T) {
	calls := remoteRouteCalls(t)
	remoteType := reflect.TypeOf((*Remote)(nil))
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	for _, route := range rpccontract.Routes() {
		if route.Kind == rpccontract.KindNotification || route.Dependency == rpccontract.DependencyProtocol {
			continue
		}
		call, ok := calls[route.Method]
		if !ok {
			t.Fatalf("remote client missing binding for route %q", route.Method)
		}
		method, ok := remoteType.MethodByName(call.methodName)
		if !ok {
			t.Fatalf("remote route %q references missing method %q", route.Method, call.methodName)
		}
		if method.Type.NumIn() < 3 {
			t.Fatalf("remote method %s has %d inputs, want receiver, context, request", method.Name, method.Type.NumIn())
		}
		if method.Type.In(1) != contextType {
			t.Fatalf("remote method %s input 1 = %v, want context.Context", method.Name, method.Type.In(1))
		}
		if method.Type.In(2) != route.RequestType {
			t.Fatalf("remote method %s request type = %v, want %v for route %q", method.Name, method.Type.In(2), route.RequestType, route.Method)
		}
		if method.Type.NumOut() == 0 || method.Type.Out(method.Type.NumOut()-1) != errorType {
			t.Fatalf("remote method %s must return error last, got %v", method.Name, method.Type)
		}
		if route.Kind == rpccontract.KindSubscription {
			continue
		}
		if route.ResponseType == reflect.TypeOf(struct{}{}) {
			if method.Type.NumOut() != 1 {
				t.Fatalf("remote method %s returns %d values for empty response route %q, want error only", method.Name, method.Type.NumOut(), route.Method)
			}
			continue
		}
		if method.Type.NumOut() != 2 || method.Type.Out(0) != route.ResponseType {
			t.Fatalf("remote method %s response type = %v, want %v for route %q", method.Name, method.Type, route.ResponseType, route.Method)
		}
	}
}

func remoteRouteCalls(t *testing.T) map[string]remoteRouteCall {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	dir := filepath.Dir(filename)
	methodValues := protocolMethodValues(t, dir)
	calls := map[string]remoteRouteCall{}
	for _, name := range []string{"remote.go", "remote_stream.go"} {
		file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			funcDecl, ok := decl.(*ast.FuncDecl)
			if !ok || funcDecl.Body == nil || !isRemoteMethod(funcDecl) {
				continue
			}
			ast.Inspect(funcDecl.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if selector, ok := call.Fun.(*ast.SelectorExpr); ok {
					receiver, _ := selector.X.(*ast.Ident)
					if receiver == nil || receiver.Name != "c" {
						return true
					}
					switch selector.Sel.Name {
					case "call":
						recordRemoteCall(calls, methodValues, call.Args, 1, "", rpccontract.ConnectionControl, funcDecl.Name.Name)
					case "callUnscoped":
						recordRemoteCall(calls, methodValues, call.Args, 1, "", rpccontract.ConnectionUnscoped, funcDecl.Name.Name)
					case "callDedicated":
						recordRemoteCall(calls, methodValues, call.Args, 2, stringArg(call.Args, 1), rpccontract.ConnectionDedicated, funcDecl.Name.Name)
					}
					return true
				}
				if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "callRPC" {
					recordRemoteCall(calls, methodValues, call.Args, 3, stringArg(call.Args, 2), rpccontract.ConnectionSubscription, funcDecl.Name.Name)
				}
				return true
			})
		}
		if name != "remote_stream.go" {
			continue
		}
		for _, decl := range file.Decls {
			funcDecl, ok := decl.(*ast.FuncDecl)
			if !ok || funcDecl.Body == nil || !isRemoteMethod(funcDecl) {
				continue
			}
			ast.Inspect(funcDecl.Body, func(node ast.Node) bool {
				lit, ok := node.(*ast.CompositeLit)
				if !ok {
					return true
				}
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					key, ok := kv.Key.(*ast.Ident)
					if !ok || key.Name != "Method" {
						continue
					}
					method, ok := methodValue(methodValues, kv.Value)
					if ok {
						calls[method] = remoteRouteCall{connection: rpccontract.ConnectionProgress, methodName: funcDecl.Name.Name}
					}
				}
				return true
			})
		}
	}
	return calls
}

func isRemoteMethod(decl *ast.FuncDecl) bool {
	if decl == nil || decl.Recv == nil || len(decl.Recv.List) != 1 {
		return false
	}
	star, ok := decl.Recv.List[0].Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	ident, ok := star.X.(*ast.Ident)
	return ok && ident.Name == "Remote"
}

func recordRemoteCall(calls map[string]remoteRouteCall, methodValues map[string]string, args []ast.Expr, methodIndex int, requestID string, connection rpccontract.ConnectionStrategy, methodName string) {
	method, ok := methodArg(methodValues, args, methodIndex)
	if !ok {
		return
	}
	calls[method] = remoteRouteCall{connection: connection, requestID: requestID, methodName: methodName}
}

func methodArg(methodValues map[string]string, args []ast.Expr, index int) (string, bool) {
	if index < 0 || index >= len(args) {
		return "", false
	}
	return methodValue(methodValues, args[index])
}

func methodValue(methodValues map[string]string, expr ast.Expr) (string, bool) {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "protocol" {
		return "", false
	}
	value, ok := methodValues[selector.Sel.Name]
	return value, ok
}

func stringArg(args []ast.Expr, index int) string {
	if index < 0 || index >= len(args) {
		return ""
	}
	lit, ok := args[index].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return value
}

func protocolMethodValues(t *testing.T, clientDir string) map[string]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(clientDir, "..", "protocol", "handshake.go"), nil, 0)
	if err != nil {
		t.Fatalf("parse protocol methods: %v", err)
	}
	values := map[string]string{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			for index, name := range valueSpec.Names {
				if index >= len(valueSpec.Values) {
					continue
				}
				lit, ok := valueSpec.Values[index].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("unquote method %s: %v", name.Name, err)
				}
				values[name.Name] = value
			}
		}
	}
	return values
}
