package transport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"

	"core/server/authmanager"
	"core/server/chatcontext"
	"core/server/metadata"
	"core/shared/apicontract"
	"core/shared/llmerrors"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/protocol"
	"core/shared/rpcwire"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

// ErrGatewayDependenciesRequired is returned by NewGateway when the supplied
// dependencies are nil. Callers match it via errors.Is.
var ErrGatewayDependenciesRequired = errors.New("gateway dependencies are required")

// canceledByClientMessage is the normalized protocol message used when a
// context.Canceled error carries no actionable wording. It is the source of
// truth for the cancellation message surfaced to clients.
const canceledByClientMessage = "request canceled by client"

type Gateway struct {
	deps                  GatewayDependencies
	identity              protocol.ServerIdentity
	registration          gatewayRegistration
	sessionReattachMu     sync.Mutex
	sessionReattach       *sessionReattachAuthority
	connectionLifecycle   context.Context
	closeConnections      context.CancelFunc
	connectionLifecycleMu sync.Mutex
	connections           sync.WaitGroup
}

type GatewayDependencies interface {
	GatewayServerStatusDependencies
	GatewayAuthDependencies
	GatewayCapabilityFactsDependencies
	GatewaySpeechDependencies
	GatewayOnboardingDependencies
	GatewayProjectDependencies
	GatewaySessionDependencies
	GatewayChatDependencies
	GatewayRuntimeDependencies
	GatewayPromptDependencies
	GatewayPromptCommandDependencies
	GatewayProcessDependencies
	GatewayWorktreeDependencies
}

type GatewayStartupLifecycle interface {
	RequireCoreActive() error
}

type GatewayServerStatusDependencies interface {
	ServerStatusClient() apicontract.ServerStatusService
}

type GatewayAuthDependencies interface {
	AuthManager() *auth.Manager
	AuthBootstrapClient() apicontract.AuthBootstrapService
	ConnectionManagementClient() apicontract.ConnectionManagementService
	AuthStatusClient() apicontract.AuthStatusService
}

type GatewayCapabilityFactsDependencies interface {
	CapabilityFactsClient() apicontract.CapabilityFactsService
}

type GatewaySpeechDependencies interface {
	SpeechClient() apicontract.SpeechService
}

type GatewayOnboardingDependencies interface {
	OnboardingFinalizeClient() apicontract.OnboardingFinalizeService
}

type GatewayProjectDependencies interface {
	MetadataStore() *metadata.Store
	ProjectID() string
	ProjectExists(context.Context, string) error
	ProjectViewClient() apicontract.ProjectViewService
	WorkflowClient() apicontract.WorkflowService
}

type GatewaySessionDependencies interface {
	SessionBelongsToProject(context.Context, string, string) error
	ChatSettingsClient() apicontract.ChatSettingsService
	SessionViewClient() apicontract.SessionViewService
	SessionLifecycleClient() apicontract.SessionLifecycleService
	SessionRuntimeClient() apicontract.SessionRuntimeService
	SessionTranscriptClient() apicontract.SessionTranscriptService
	GoalObservationClient() apicontract.GoalObservationService
	SessionLaunchClientForProjectWorkspace(context.Context, string, string) (apicontract.SessionLaunchService, error)
	SessionLaunchClientForProjectWorkspaceID(context.Context, string, string) (apicontract.SessionLaunchService, error)
	SessionChatContextOwner() chatcontext.SessionOwner
	RunPromptClientForProjectWorkspace(context.Context, string, string) (apicontract.RunPromptService, error)
	RunPromptClientForProjectWorkspaceID(context.Context, string, string) (apicontract.RunPromptService, error)
}

type GatewayChatDependencies interface {
	ChatMutationClient() apicontract.ChatMutationService
}

type GatewayRuntimeDependencies interface {
	RuntimeControlClient() apicontract.RuntimeControlService
	RuntimeLiveControlClient() apicontract.RuntimeLiveControlService
}

type GatewayPromptDependencies interface {
	AskViewClient() apicontract.AskViewService
	ApprovalViewClient() apicontract.ApprovalViewService
	PromptControlClient() apicontract.PromptControlService
	AttentionNotificationClient() apicontract.AttentionNotificationService
}

type GatewayPromptCommandDependencies interface {
	PromptCommandCatalogClientForProjectWorkspace(context.Context, string, string) (apicontract.PromptCommandCatalogService, error)
}

type GatewayProcessDependencies interface {
	ProcessViewClient() apicontract.ProcessViewService
	ProcessObservationClient() apicontract.ProcessObservationService
	ProcessControlClient() apicontract.ProcessControlService
}

type GatewayWorktreeDependencies interface {
	WorktreeClient() apicontract.WorktreeService
}

type gatewayRequestScheduleKind uint8

const (
	gatewayRequestScheduleOrdinary gatewayRequestScheduleKind = iota
	gatewayRequestScheduleExclusive
	gatewayRequestScheduleProgress
	gatewayRequestScheduleSubscription
)

type gatewayRequestSchedule struct {
	kind gatewayRequestScheduleKind
}

type gatewayEstablishedRequest struct {
	binary  *gatewayBinaryRequest
	failure *sharedpb.TransportFailure
}

type connectionState struct {
	handshakeDone         bool
	attachedProject       string
	attachedWorkspaceID   string
	attachedWorkspaceRoot string
	attachedSession       *runtimeids.SessionID
}

func NewGateway(deps GatewayDependencies, identity protocol.ServerIdentity) (*Gateway, error) {
	if isNilGatewayDependencies(deps) {
		return nil, ErrGatewayDependenciesRequired
	}
	if strings.TrimSpace(identity.ProtocolVersion) == "" {
		return nil, errors.New("server identity is required")
	}
	registration, err := productionGatewayRegistration()
	if err != nil {
		return nil, fmt.Errorf("build Gateway registration: %w", err)
	}
	if err := registration.Validate(); err != nil {
		return nil, fmt.Errorf("validate Gateway registration: %w", err)
	}
	connectionLifecycle, closeConnections := context.WithCancel(context.Background())
	return &Gateway{
		deps:                deps,
		identity:            identity,
		registration:        registration,
		connectionLifecycle: connectionLifecycle,
		closeConnections:    closeConnections,
	}, nil
}

// Close disconnects clients and drains their handlers before server resources
// are released. HTTP shutdown alone does not wait for upgraded WebSockets.
func (g *Gateway) Close() {
	g.connectionLifecycleMu.Lock()
	g.closeConnections()
	g.connectionLifecycleMu.Unlock()
	g.connections.Wait()
}

func (g *Gateway) sessionReattachAuthority() (*sessionReattachAuthority, error) {
	if g == nil || g.deps == nil {
		return nil, ErrGatewayDependenciesRequired
	}
	g.sessionReattachMu.Lock()
	defer g.sessionReattachMu.Unlock()
	if g.sessionReattach != nil {
		return g.sessionReattach, nil
	}
	metadataStore := g.deps.MetadataStore()
	if metadataStore == nil {
		return nil, errors.New("metadata store is required")
	}
	authority, err := loadSessionReattachAuthority(metadataStore.PersistenceRoot())
	if err != nil {
		return nil, err
	}
	g.sessionReattach = authority
	return authority, nil
}

func isNilGatewayDependencies(deps GatewayDependencies) bool {
	if deps == nil {
		return true
	}
	value := reflect.ValueOf(deps)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func (g *Gateway) Handler() http.Handler {
	return rpcwire.NewWebSocketTransport().Handler(g.handleConn)
}

func (g *Gateway) ViewedImageHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != ViewedImageBodyPath {
			http.NotFound(w, r)
			return
		}
		g.serveViewedImageOriginal(w, r)
	})
}

func (g *Gateway) handleConn(ctx context.Context, conn rpcwire.Conn) {
	g.connectionLifecycleMu.Lock()
	if g.connectionLifecycle.Err() != nil {
		g.connectionLifecycleMu.Unlock()
		_ = conn.Close()
		return
	}
	g.connections.Add(1)
	g.connectionLifecycleMu.Unlock()
	defer g.connections.Done()

	connCtx, cancel := context.WithCancel(ctx)
	var stopOnce sync.Once
	stop := func() {
		stopOnce.Do(func() {
			cancel()
			_ = conn.Close()
		})
	}
	stopShutdown := context.AfterFunc(g.connectionLifecycle, stop)
	defer stopShutdown()
	go func() {
		select {
		case <-ctx.Done():
			stop()
		case <-conn.Closed():
			stop()
		}
	}()
	state := &connectionState{}
	const ordinaryAdmissionLimit = 16
	admission := make(chan struct{}, ordinaryAdmissionLimit)
	var ordinary sync.WaitGroup
	defer func() {
		stop()
		ordinary.Wait()
	}()
	for {
		if !state.handshakeDone {
			request, err := g.receiveEstablishedRequest(connCtx, conn)
			if err != nil {
				return
			}
			if request.failure != nil {
				_ = sendTransportFailure(connCtx, conn, request.failure)
				return
			}
			if request.binary == nil || !isBinaryHandshake(request.binary.binding) {
				return
			}
			if !g.serveBinaryRequest(conn, connCtx, state, *request.binary) {
				return
			}
			if !state.handshakeDone {
				return
			}
			continue
		}

		request, err := g.receiveEstablishedRequest(connCtx, conn)
		if err != nil {
			return
		}
		if request.failure != nil {
			if !sendTransportFailure(connCtx, conn, request.failure) {
				stop()
				return
			}
			continue
		}
		schedule := g.gatewayRequestScheduleForEstablished(request)
		if schedule.kind == gatewayRequestScheduleExclusive {
			ordinary.Wait()
			if !g.serveBinaryRequest(conn, connCtx, state, *request.binary) {
				stop()
				return
			}
			continue
		}
		if schedule.kind == gatewayRequestScheduleProgress || schedule.kind == gatewayRequestScheduleSubscription {
			if !g.serveBinaryRequest(conn, connCtx, state, *request.binary) {
				stop()
				return
			}
			continue
		}

		select {
		case admission <- struct{}{}:
		case <-connCtx.Done():
			return
		}

		ordinary.Add(1)
		go func(request gatewayBinaryRequest) {
			defer ordinary.Done()
			defer func() { <-admission }()
			if !g.serveBinaryRequest(conn, connCtx, state, request) {
				stop()
			}
		}(*request.binary)
	}
}

func isBinaryHandshake(binding gatewayBinaryBinding) bool {
	return binding.operation.Descriptor.Parent().Name() == "ConnectionService" &&
		binding.operation.Descriptor.Name() == "Handshake"
}

func (g *Gateway) gatewayRequestScheduleForEstablished(request gatewayEstablishedRequest) gatewayRequestSchedule {
	if request.binary != nil &&
		request.binary.binding.operation.Options.Kind == sharedpb.OperationKind_OPERATION_KIND_SUBSCRIPTION {
		return gatewayRequestSchedule{kind: gatewayRequestScheduleSubscription}
	}
	if request.binary != nil &&
		request.binary.binding.operation.Options.Kind == sharedpb.OperationKind_OPERATION_KIND_PROGRESS {
		return gatewayRequestSchedule{kind: gatewayRequestScheduleProgress}
	}
	if request.binary == nil {
		panic("established Gateway request is required")
	}
	binding := request.binary.binding
	if binding.operation.Options.Kind != sharedpb.OperationKind_OPERATION_KIND_UNARY {
		panic(fmt.Sprintf("unsupported binary operation kind %s", binding.operation.Options.Kind))
	}
	if isGatewayExclusiveBinaryBinding(binding) {
		return gatewayRequestSchedule{kind: gatewayRequestScheduleExclusive}
	}
	return gatewayRequestSchedule{kind: gatewayRequestScheduleOrdinary}
}

func (g *Gateway) requireCoreActive() error {
	lifecycle, ok := g.deps.(GatewayStartupLifecycle)
	if !ok {
		return nil
	}
	return lifecycle.RequireCoreActive()
}

func (g *Gateway) receiveEstablishedRequest(ctx context.Context, conn rpcwire.Conn) (gatewayEstablishedRequest, error) {
	frame, err := receiveFrame(ctx, conn)
	if err != nil {
		return gatewayEstablishedRequest{}, err
	}
	switch frame.Kind {
	case rpcwire.FrameBinary:
		request, failure := g.resolveBinaryRequest(frame.Payload)
		if failure != nil {
			return gatewayEstablishedRequest{failure: failure}, nil
		}
		return gatewayEstablishedRequest{binary: request}, nil
	default:
		return gatewayEstablishedRequest{failure: &sharedpb.TransportFailure{
			Code: sharedpb.TransportFailureCode_TRANSPORT_FAILURE_CODE_MALFORMED_ENVELOPE,
		}}, nil
	}
}

func receiveFrame(ctx context.Context, conn rpcwire.Conn) (rpcwire.Frame, error) {
	select {
	case <-ctx.Done():
		return rpcwire.Frame{}, ctx.Err()
	case event, ok := <-conn.Events():
		if !ok {
			return rpcwire.Frame{}, io.EOF
		}
		if event.Err != nil {
			return rpcwire.Frame{}, event.Err
		}
		return event.Frame, nil
	}
}

func streamFailure(err error) (sharedpb.StreamFailureCode, string) {
	if err == nil {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_INTERNAL_FAILURE, "internal error"
	}
	message := strings.TrimSpace(err.Error())
	if errors.Is(err, context.Canceled) {
		if message == "" || message == context.Canceled.Error() {
			message = canceledByClientMessage
		}
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_REQUEST_CANCELED, message
	}
	if errors.Is(err, llmerrors.ErrModelStreamStalled) {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_MODEL_STREAM_STALLED, message
	}
	if errors.Is(err, serverapi.ErrStreamGap) {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_STREAM_GAP, message
	}
	if errors.Is(err, serverapi.ErrWorkspaceNotRegistered) {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_WORKSPACE_NOT_REGISTERED, message
	}
	if errors.Is(err, serverapi.ErrProjectNotFound) {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_PROJECT_NOT_FOUND, message
	}
	if errors.Is(err, serverapi.ErrProjectUnavailable) {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_PROJECT_UNAVAILABLE, message
	}
	if errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_RUNTIME_UNAVAILABLE, message
	}
	if errors.Is(err, serverapi.ErrRuntimeNoActiveRun) {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_RUNTIME_NO_ACTIVE_RUN, message
	}
	if errors.Is(err, serverapi.ErrRuntimeNoFinalAnswer) {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_RUNTIME_NO_FINAL_ANSWER, message
	}
	if errors.Is(err, serverapi.ErrStreamUnavailable) {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_STREAM_UNAVAILABLE, message
	}
	if errors.Is(err, serverapi.ErrStreamFailed) {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_STREAM_FAILED, message
	}
	if errors.Is(err, serverapi.ErrPromptNotFound) {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_PROMPT_NOT_FOUND, message
	}
	if errors.Is(err, serverapi.ErrPromptAlreadyResolved) {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_PROMPT_RESOLVED, message
	}
	if errors.Is(err, serverapi.ErrPromptUnsupported) {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_PROMPT_UNSUPPORTED, message
	}
	if errors.Is(err, serverapi.ErrWorkflowTaskNotFound) {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_TASK_NOT_FOUND, message
	}
	if errors.Is(err, serverapi.ErrWorkflowTaskCompleteTargetNotFound) {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_TASK_COMPLETE_NOT_FOUND, message
	}
	if errors.Is(err, serverapi.ErrWorkflowTaskCompleteSelectorAmbiguous) {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_TASK_COMPLETE_AMBIGUOUS, message
	}
	if errors.Is(err, serverapi.ErrServerAuthRequired) || errors.Is(err, auth.ErrAuthNotConfigured) {
		return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_AUTH_REQUIRED, message
	}
	return sharedpb.StreamFailureCode_STREAM_FAILURE_CODE_INTERNAL_FAILURE, message
}
