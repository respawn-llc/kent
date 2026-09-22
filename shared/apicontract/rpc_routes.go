package apicontract

import (
	"reflect"
	"sort"

	"core/shared/protocol"
	"core/shared/serverapi"
)

type Kind string

const (
	KindUnary        Kind = "unary"
	KindSubscription Kind = "subscription"
	KindProgress     Kind = "progress"
	KindNotification Kind = "notification"
)

type AuthPolicy string

const (
	AuthNone          AuthPolicy = "none"
	AuthPreServerAuth AuthPolicy = "pre_server_auth"
	AuthServer        AuthPolicy = "server_auth"
)

type ScopePolicy string

const (
	ScopeNone                       ScopePolicy = "none"
	ScopeAttachProject              ScopePolicy = "attach_project"
	ScopeAttachSession              ScopePolicy = "attach_session"
	ScopeProjectView                ScopePolicy = "project_view"
	ScopeProjectWorkspace           ScopePolicy = "project_workspace"
	ScopeProjectWorkspaceBinding    ScopePolicy = "project_workspace_binding"
	ScopeSessionActiveProject       ScopePolicy = "session_active_project"
	ScopeSessionActiveProjectIfSet  ScopePolicy = "session_active_project_if_set"
	ScopeSessionDraftHandoffProject ScopePolicy = "session_draft_handoff_project"
	ScopeSessionAttachedProject     ScopePolicy = "session_attached_project"
	ScopeAttachedSession            ScopePolicy = "attached_session"
	ScopeGoalSession                ScopePolicy = "goal_session"
	ScopeRuntimeLiveSessionRequired ScopePolicy = "runtime_live_session_required"
	ScopeRuntimeLiveSessionOptional ScopePolicy = "runtime_live_session_optional"
	ScopeProcessActiveProject       ScopePolicy = "process_active_project"
	ScopeNotification               ScopePolicy = "notification"
	ScopeChatTarget                 ScopePolicy = "chat_target"
	ScopeWorktreeManagement         ScopePolicy = "worktree_management"
)

type ConnectionStrategy string

const (
	ConnectionControl      ConnectionStrategy = "control"
	ConnectionUnscoped     ConnectionStrategy = "unscoped_control"
	ConnectionDedicated    ConnectionStrategy = "dedicated"
	ConnectionSubscription ConnectionStrategy = "subscription"
	ConnectionProgress     ConnectionStrategy = "progress"
	ConnectionNotification ConnectionStrategy = "notification"
)

type Route struct {
	Method             string
	Kind               Kind
	Auth               AuthPolicy
	Scope              ScopePolicy
	Connection         ConnectionStrategy
	RequestType        reflect.Type
	ResponseType       reflect.Type
	EventMethod        string
	EventType          reflect.Type
	CompleteMethod     string
	CompleteType       reflect.Type
	DedicatedRequestID string
	ValidatesRequest   bool
}

const (
	UpdateStatusDedicatedRequestID = "get-update-status"
)

func unary[Req any, Resp any](method string, auth AuthPolicy, scope ScopePolicy, connection ConnectionStrategy) Route {
	reqType := reflect.TypeOf((*Req)(nil)).Elem()
	return Route{
		Method:           method,
		Kind:             KindUnary,
		Auth:             auth,
		Scope:            scope,
		Connection:       connection,
		RequestType:      reqType,
		ResponseType:     reflect.TypeOf((*Resp)(nil)).Elem(),
		ValidatesRequest: implementsValidator(reqType),
	}
}

func dedicatedUnary[Req any, Resp any](method string, requestID string, scope ScopePolicy) Route {
	route := unary[Req, Resp](method, AuthServer, scope, ConnectionDedicated)
	route.DedicatedRequestID = requestID
	return route
}

func subscription[Req any, Event any](method string, auth AuthPolicy, scope ScopePolicy, eventMethod string, completeMethod string) Route {
	reqType := reflect.TypeOf((*Req)(nil)).Elem()
	return Route{
		Method:           method,
		Kind:             KindSubscription,
		Auth:             auth,
		Scope:            scope,
		Connection:       ConnectionSubscription,
		RequestType:      reqType,
		ResponseType:     reflect.TypeOf((*protocol.SubscribeResponse)(nil)).Elem(),
		EventMethod:      eventMethod,
		EventType:        reflect.TypeOf((*Event)(nil)).Elem(),
		CompleteMethod:   completeMethod,
		CompleteType:     reflect.TypeOf((*protocol.StreamCompleteParams)(nil)).Elem(),
		ValidatesRequest: implementsValidator(reqType),
	}
}

func notification[Event any](method string) Route {
	return Route{
		Method:      method,
		Kind:        KindNotification,
		Auth:        AuthNone,
		Scope:       ScopeNotification,
		Connection:  ConnectionNotification,
		RequestType: reflect.TypeOf((*Event)(nil)).Elem(),
	}
}

func implementsValidator(t reflect.Type) bool {
	validator := reflect.TypeOf((*interface{ Validate() error })(nil)).Elem()
	return t != nil && t.Implements(validator)
}

var routeContracts = []Route{
	subscription[serverapi.AttentionNotificationSubscribeRequest, protocol.AttentionNotificationEventParams](protocol.MethodAttentionNotificationSubscribe, AuthServer, ScopeNone, protocol.MethodAttentionNotificationEvent, protocol.MethodAttentionNotificationComplete),
	notification[protocol.AttentionNotificationEventParams](protocol.MethodAttentionNotificationEvent),
	notification[protocol.StreamCompleteParams](protocol.MethodAttentionNotificationComplete),
}

func Routes() []Route {
	routes := append([]Route(nil), routeContracts...)
	sort.Slice(routes, func(i, j int) bool {
		return routes[i].Method < routes[j].Method
	})
	return routes
}

func RouteByMethod(method string) (Route, bool) {
	for _, route := range routeContracts {
		if route.Method == method {
			return route, true
		}
	}
	return Route{}, false
}

func SubscriptionMethods() []string {
	methods := make([]string, 0)
	for _, route := range routeContracts {
		if route.Kind == KindSubscription {
			methods = append(methods, route.Method)
		}
	}
	sort.Strings(methods)
	return methods
}
