package servicecontract

import (
	"context"
	"reflect"
	"testing"

	"builder/shared/protocol"
	"builder/shared/rpccontract"
)

func TestServiceContractsCoverEveryNonProtocolRoute(t *testing.T) {
	interfaces := map[rpccontract.Dependency]reflect.Type{
		rpccontract.DependencyApprovalView:     typeOfService[ApprovalViewService](),
		rpccontract.DependencyAskView:          typeOfService[AskViewService](),
		rpccontract.DependencyAuthBootstrap:    typeOfService[AuthBootstrapService](),
		rpccontract.DependencyAuthStatus:       typeOfService[AuthStatusService](),
		rpccontract.DependencyProcessControl:   typeOfService[ProcessControlService](),
		rpccontract.DependencyProcessOutput:    typeOfService[ProcessOutputService](),
		rpccontract.DependencyProcessView:      typeOfService[ProcessViewService](),
		rpccontract.DependencyProjectView:      typeOfService[ProjectViewService](),
		rpccontract.DependencyPromptActivity:   typeOfService[PromptActivityService](),
		rpccontract.DependencyPromptControl:    typeOfService[PromptControlService](),
		rpccontract.DependencyRunPrompt:        typeOfService[RunPromptService](),
		rpccontract.DependencyRuntimeControl:   typeOfService[RuntimeControlService](),
		rpccontract.DependencySessionActivity:  typeOfService[SessionActivityService](),
		rpccontract.DependencySessionLaunch:    typeOfService[SessionLaunchService](),
		rpccontract.DependencySessionLifecycle: typeOfService[SessionLifecycleService](),
		rpccontract.DependencySessionRuntime:   typeOfService[SessionRuntimeService](),
		rpccontract.DependencySessionView:      typeOfService[SessionViewService](),
		rpccontract.DependencyWorktree:         typeOfService[WorktreeService](),
		rpccontract.DependencyWorkflow:         typeOfService[WorkflowService](),
	}
	for _, route := range rpccontract.Routes() {
		if route.Kind == rpccontract.KindNotification || route.Dependency == rpccontract.DependencyProtocol || route.Dependency == rpccontract.DependencyStreamNotification {
			continue
		}
		serviceType, ok := interfaces[route.Dependency]
		if !ok {
			t.Fatalf("route %q dependency %q missing service contract interface", route.Method, route.Dependency)
		}
		if !serviceTypeHasRouteMethod(serviceType, route) {
			t.Fatalf("route %q request/response types missing from service contract %s", route.Method, serviceType.Name())
		}
	}
}

func typeOfService[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}

func serviceTypeHasRouteMethod(serviceType reflect.Type, route rpccontract.Route) bool {
	contextType := reflect.TypeOf((*context.Context)(nil)).Elem()
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	emptyResponseType := reflect.TypeOf(struct{}{})
	subscribeResponseType := reflect.TypeOf(protocol.SubscribeResponse{})
	for i := 0; i < serviceType.NumMethod(); i++ {
		method := serviceType.Method(i)
		if method.Type.NumIn() < 2 || method.Type.In(0) != contextType || method.Type.In(1) != route.RequestType {
			continue
		}
		if method.Type.NumOut() == 0 || method.Type.Out(method.Type.NumOut()-1) != errorType {
			continue
		}
		if route.Kind == rpccontract.KindSubscription {
			return true
		}
		if route.ResponseType == emptyResponseType {
			return method.Type.NumOut() == 1
		}
		if route.ResponseType == subscribeResponseType {
			return true
		}
		return method.Type.NumOut() >= 2 && method.Type.Out(0) == route.ResponseType
	}
	return false
}
