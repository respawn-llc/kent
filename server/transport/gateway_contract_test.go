package transport

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"testing"

	rpccontract "core/shared/apicontract"
)

func TestGatewayUnaryHandlersCoverRouteContract(t *testing.T) {
	for _, route := range rpccontract.Routes() {
		if route.Kind != rpccontract.KindUnary {
			continue
		}
		if _, ok := gatewayUnaryHandlers[route.Method]; !ok {
			t.Fatalf("unary route %q missing gateway handler", route.Method)
		}
	}
	for method := range gatewayUnaryHandlers {
		route, ok := rpccontract.RouteByMethod(method)
		if !ok {
			t.Fatalf("gateway unary handler %q missing route contract", method)
		}
		if route.Kind != rpccontract.KindUnary {
			t.Fatalf("gateway unary handler %q route kind = %q, want unary", method, route.Kind)
		}
	}
}

func TestGatewayUnaryHandlerDTOTypesMatchRouteContract(t *testing.T) {
	handlerTypes := gatewayUnaryHandlerDTOTypes(t)
	for _, route := range rpccontract.Routes() {
		if route.Kind != rpccontract.KindUnary {
			continue
		}
		types, ok := handlerTypes[route.Method]
		if !ok {
			t.Fatalf("unary route %q missing gateway handler DTO metadata", route.Method)
		}
		if types.request != contractTypeName(route.RequestType) {
			t.Fatalf("gateway route %q request type = %q, want %q", route.Method, types.request, contractTypeName(route.RequestType))
		}
		if types.response != contractTypeName(route.ResponseType) {
			t.Fatalf("gateway route %q response type = %q, want %q", route.Method, types.response, contractTypeName(route.ResponseType))
		}
	}
}

type gatewayHandlerDTOTypes struct {
	request  string
	response string
}

func gatewayUnaryHandlerDTOTypes(t *testing.T) map[string]gatewayHandlerDTOTypes {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	dir := filepath.Dir(filename)
	protocolMethods := gatewayProtocolMethodValues(t, dir)
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(dir, "gateway_unary_handlers.go"), nil, 0)
	if err != nil {
		t.Fatalf("parse gateway_unary_handlers.go: %v", err)
	}
	out := map[string]gatewayHandlerDTOTypes{}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.VAR {
			continue
		}
		for _, spec := range gen.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok || len(valueSpec.Names) != 1 || valueSpec.Names[0].Name != "gatewayUnaryHandlerEntries" || len(valueSpec.Values) != 1 {
				continue
			}
			lit, ok := valueSpec.Values[0].(*ast.CompositeLit)
			if !ok {
				continue
			}
			for _, elt := range lit.Elts {
				kv, ok := elt.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				method, ok := gatewayProtocolMethodValue(protocolMethods, kv.Key)
				if !ok {
					continue
				}
				types, ok := extractGatewayHandlerDTOTypes(kv.Value)
				if !ok {
					t.Fatalf("gateway unary handler %q does not expose decodable DTO types", method)
				}
				out[method] = types
			}
		}
	}
	return out
}

func extractGatewayHandlerDTOTypes(expr ast.Expr) (gatewayHandlerDTOTypes, bool) {
	if types, ok := extractGatewayHelperDTOTypes(expr); ok {
		return types, true
	}
	fn, ok := expr.(*ast.FuncLit)
	if !ok {
		return gatewayHandlerDTOTypes{}, false
	}
	var out gatewayHandlerDTOTypes
	ast.Inspect(fn.Body, func(node ast.Node) bool {
		if out.request != "" && out.response != "" {
			return false
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		if ident, ok := call.Fun.(*ast.Ident); ok && ident.Name == "decodeAndHandle" && len(call.Args) >= 2 {
			handler, ok := call.Args[1].(*ast.FuncLit)
			if !ok || handler.Type.Params == nil || len(handler.Type.Params.List) != 1 || handler.Type.Results == nil || len(handler.Type.Results.List) == 0 {
				return true
			}
			out.request = astTypeName(handler.Type.Params.List[0].Type)
			out.response = astTypeName(handler.Type.Results.List[0].Type)
			return false
		}
		if index, ok := call.Fun.(*ast.IndexExpr); ok {
			if ident, ok := index.X.(*ast.Ident); ok && ident.Name == "decodeParams" {
				out.request = astTypeName(index.Index)
				out.response = "protocol.HandshakeResponse"
				return false
			}
		}
		return true
	})
	return out, out.request != "" && out.response != ""
}

func extractGatewayHelperDTOTypes(expr ast.Expr) (gatewayHandlerDTOTypes, bool) {
	call, ok := expr.(*ast.CallExpr)
	if !ok {
		return gatewayHandlerDTOTypes{}, false
	}
	index, ok := call.Fun.(*ast.IndexListExpr)
	if !ok {
		return gatewayHandlerDTOTypes{}, false
	}
	ident, ok := index.X.(*ast.Ident)
	if !ok {
		return gatewayHandlerDTOTypes{}, false
	}
	switch ident.Name {
	case "gatewayClientCall":
		if len(index.Indices) != 3 {
			return gatewayHandlerDTOTypes{}, false
		}
		return gatewayHandlerDTOTypes{
			request:  astTypeName(index.Indices[1]),
			response: astTypeName(index.Indices[2]),
		}, true
	case "gatewayClientCallNoResponse":
		if len(index.Indices) != 2 {
			return gatewayHandlerDTOTypes{}, false
		}
		return gatewayHandlerDTOTypes{
			request:  astTypeName(index.Indices[1]),
			response: "struct{}",
		}, true
	default:
		return gatewayHandlerDTOTypes{}, false
	}
}

func astTypeName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.SelectorExpr:
		pkg, ok := typed.X.(*ast.Ident)
		if !ok {
			return ""
		}
		return pkg.Name + "." + typed.Sel.Name
	case *ast.StructType:
		if typed.Fields == nil || len(typed.Fields.List) == 0 {
			return "struct{}"
		}
		return "struct{...}"
	case *ast.Ident:
		return typed.Name
	default:
		return ""
	}
}

func contractTypeName(t reflect.Type) string {
	if t == nil {
		return ""
	}
	if t.Kind() == reflect.Struct && t.Name() == "" && t.NumField() == 0 {
		return "struct{}"
	}
	pkg := filepath.Base(t.PkgPath())
	if pkg == "" {
		return t.String()
	}
	return pkg + "." + t.Name()
}

func gatewayProtocolMethodValues(t *testing.T, transportDir string) map[string]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(transportDir, "..", "..", "shared", "protocol", "handshake.go"), nil, 0)
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

func gatewayProtocolMethodValue(values map[string]string, expr ast.Expr) (string, bool) {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return "", false
	}
	pkg, ok := selector.X.(*ast.Ident)
	if !ok || pkg.Name != "protocol" {
		return "", false
	}
	value, ok := values[selector.Sel.Name]
	return value, ok
}
