package transport

import (
	"testing"

	"builder/shared/rpccontract"
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
