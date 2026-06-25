package apicontract

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"core/shared/protocol"
	"core/shared/serverapi"
)

func TestRouteContractsAreCompleteAndExplicit(t *testing.T) {
	routes := Routes()
	if len(routes) == 0 {
		t.Fatal("expected route contracts")
	}
	seen := map[string]Route{}
	for _, route := range routes {
		if route.Method == "" {
			t.Fatalf("route has empty method: %+v", route)
		}
		if _, exists := seen[route.Method]; exists {
			t.Fatalf("duplicate route method %q", route.Method)
		}
		seen[route.Method] = route
		if route.Kind == "" {
			t.Fatalf("route %q missing kind", route.Method)
		}
		if route.Auth == "" {
			t.Fatalf("route %q missing auth policy", route.Method)
		}
		if route.Scope == "" {
			t.Fatalf("route %q missing scope policy", route.Method)
		}
		if route.Connection == "" {
			t.Fatalf("route %q missing connection strategy", route.Method)
		}
		if route.Dependency == "" {
			t.Fatalf("route %q missing handler dependency", route.Method)
		}
		if route.Kind != KindNotification && (route.RequestType == nil || route.ResponseType == nil) {
			t.Fatalf("route %q missing request/response types", route.Method)
		}
		if route.Kind == KindSubscription {
			if route.EventMethod == "" || route.EventType == nil || route.CompleteMethod == "" || route.CompleteType == nil {
				t.Fatalf("subscription route %q missing stream semantics: %+v", route.Method, route)
			}
		}
		if route.Kind == KindProgress && (route.EventMethod == "" || route.EventType == nil) {
			t.Fatalf("progress route %q missing progress stream semantics: %+v", route.Method, route)
		}
	}
	for _, method := range declaredProtocolMethods(t) {
		if _, ok := seen[method]; !ok {
			t.Fatalf("protocol method %q missing route contract", method)
		}
	}
}

func TestAllowedPreAuthMethodsDerivedFromRoutes(t *testing.T) {
	got := map[string]struct{}{}
	for _, method := range AllowedPreAuthMethods() {
		got[method] = struct{}{}
		route, ok := RouteByMethod(method)
		if !ok {
			t.Fatalf("pre-auth method %q missing route", method)
		}
		if route.Auth != AuthPreServerAuth {
			t.Fatalf("pre-auth method %q has auth policy %q", method, route.Auth)
		}
	}
	for _, route := range Routes() {
		if route.Auth != AuthPreServerAuth {
			continue
		}
		if _, ok := got[route.Method]; !ok {
			t.Fatalf("route %q is pre-auth but missing from AllowedPreAuthMethods", route.Method)
		}
	}
}

func TestSubscriptionMethodsDerivedFromRoutes(t *testing.T) {
	got := map[string]struct{}{}
	for _, method := range SubscriptionMethods() {
		got[method] = struct{}{}
		route, ok := RouteByMethod(method)
		if !ok {
			t.Fatalf("subscription method %q missing route", method)
		}
		if route.Kind != KindSubscription {
			t.Fatalf("subscription method %q has kind %q", method, route.Kind)
		}
	}
	for _, route := range Routes() {
		if route.Kind != KindSubscription {
			continue
		}
		if _, ok := got[route.Method]; !ok {
			t.Fatalf("route %q is subscription but missing from SubscriptionMethods", route.Method)
		}
	}
}

func TestRouteRequestValidationMetadataMatchesTypes(t *testing.T) {
	validator := reflect.TypeOf((*interface{ Validate() error })(nil)).Elem()
	for _, route := range Routes() {
		if route.RequestType == nil {
			continue
		}
		if got := route.RequestType.Implements(validator); got != route.ValidatesRequest {
			t.Fatalf("route %q validation metadata = %t, want %t", route.Method, route.ValidatesRequest, got)
		}
	}
}

func TestWorkflowTaskListRouteContract(t *testing.T) {
	route, ok := RouteByMethod(protocol.MethodWorkflowTaskList)
	if !ok {
		t.Fatal("workflow task list route missing")
	}
	if route.Kind != KindUnary || route.Auth != AuthPreServerAuth || route.Scope != ScopeProjectView || route.Connection != ConnectionUnscoped || route.Dependency != DependencyWorkflow {
		t.Fatalf("workflow task list route = %+v, want read-only workflow project-view unary route", route)
	}
	if route.RequestType != reflect.TypeOf(serverapi.WorkflowTaskListRequest{}) || route.ResponseType != reflect.TypeOf(serverapi.WorkflowTaskListResponse{}) {
		t.Fatalf("workflow task list route DTOs = %v -> %v", route.RequestType, route.ResponseType)
	}
}

func declaredProtocolMethods(t *testing.T) []string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime caller unavailable")
	}
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(filepath.Dir(filename), "..", "protocol", "handshake.go"), nil, 0)
	if err != nil {
		t.Fatalf("parse protocol methods: %v", err)
	}
	values := make([]string, 0)
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
				if !strings.HasPrefix(name.Name, "Method") || index >= len(valueSpec.Values) {
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
				values = append(values, value)
			}
		}
	}
	if len(values) == 0 {
		t.Fatal("no protocol methods found")
	}
	unique := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		unique = append(unique, value)
	}
	return unique
}

func TestHandshakeMethodIsOnlyNoAuthRoute(t *testing.T) {
	for _, route := range Routes() {
		if route.Auth == AuthNone && route.Method != protocol.MethodHandshake && route.Kind != KindNotification {
			t.Fatalf("route %q unexpectedly has no auth policy", route.Method)
		}
	}
}
