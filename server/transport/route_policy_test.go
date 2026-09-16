package transport

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"core/internal/testharness/postprocessfixture"
	serverbootstrap "core/server/bootstrap"
	"core/server/core"
	"core/server/metadata"
	"core/server/session"
	shelltool "core/server/tools/shell"
	"core/server/tools/shell/postprocess"
	rpccontract "core/shared/apicontract"
	"core/shared/config"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	processpb "core/shared/protoapi/gen/kent/api/process"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/protocol"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestRoutePolicyAuthPolicyHandlesBlankAndUnknownMethods(t *testing.T) {
	registration, err := productionGatewayRegistration()
	if err != nil {
		t.Fatalf("production Gateway registration: %v", err)
	}
	if err := registration.Validate(); err != nil {
		t.Fatalf("validate production Gateway registration: %v", err)
	}
	executor := newRoutePolicyExecutor(&Gateway{registration: registration})
	if err := executor.requireAuth(context.Background(), nil, ""); err != nil {
		t.Fatalf("blank method auth: %v", err)
	}
	for name, operation := range registration.operations {
		activeIdentity := name
		if route, legacy := registration.legacy[name]; legacy {
			activeIdentity = route.Method
		}
		authErr := executor.requireAuth(context.Background(), nil, activeIdentity)
		requiresServerAuth := operation.Options.AuthenticationStage == sharedpb.AuthenticationStage_AUTHENTICATION_STAGE_SERVER
		if requiresServerAuth && !errors.Is(authErr, serverapi.ErrServerAuthRequired) {
			t.Fatalf("auth-required method %q error = %v, want server auth required", activeIdentity, authErr)
		}
		if !requiresServerAuth && authErr != nil {
			t.Fatalf("pre-server method %q auth: %v", activeIdentity, authErr)
		}
	}
	if err := executor.requireAuth(context.Background(), nil, "missing.method"); !errors.Is(err, serverapi.ErrServerAuthRequired) {
		t.Fatalf("unknown method error = %v, want server auth required", err)
	}
}

func TestRoutePolicyAllowsStatelessScopesWithoutGateway(t *testing.T) {
	executor := routePolicyExecutor{}
	for _, tc := range []struct {
		name   string
		method string
		params any
	}{
		{name: "notification", method: protocol.MethodWorkflowComplete, params: protocol.StreamCompleteParams{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := executor.authorizeScope(context.Background(), &connectionState{}, routeForTest(t, tc.method), tc.params); err != nil {
				t.Fatalf("authorize scope: %v", err)
			}
		})
	}
	if err := executor.authorizeScopeFacts(
		context.Background(),
		&connectionState{},
		routeScopePolicy(sharedpb.ScopePolicy_SCOPE_POLICY_PROJECT_VIEW),
		gatewayOperationName(
			t,
			projectpb.File_kent_api_project_project_proto.Services().
				ByName("ProjectCatalogService").Methods().ByName("List"),
		),
		routeScopeParams{},
	); err != nil {
		t.Fatalf("authorize Project view scope: %v", err)
	}
}

func TestChatTargetSemanticUnion(t *testing.T) {
	sessionID := runtimeids.NewSessionID().String()
	for _, test := range []struct {
		name    string
		target  *chatpb.ChatTarget
		wantErr bool
	}{
		{
			name: "existing Session",
			target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{
				Session: &chatpb.ExistingSessionTarget{SessionId: sessionID},
			}},
		},
		{
			name:   "New Chat",
			target: routePolicyNewChatTarget("project-1", "workspace-1"),
		},
		{
			name:    "missing target arm",
			target:  &chatpb.ChatTarget{},
			wantErr: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := protoapi.ChatTargetFromRequest(&chatpb.SteerRequest{
				Target: test.target,
				Activation: &chatpb.Activation{
					Input: &chatpb.Activation_Text{Text: "continue"},
				},
			})
			if (err != nil) != test.wantErr {
				t.Fatalf("ChatTargetFromRequest error = %v, want error %t", err, test.wantErr)
			}
		})
	}
}

func TestChatResultClassification(t *testing.T) {
	sessionID := runtimeids.NewSessionID().String()
	queueItemID := runtimeids.NewQueueItemID().String()
	for _, test := range []struct {
		name        string
		result      *chatpb.SteerResult
		wantOutcome protoapi.OperationOutcome
		wantCode    string
		wantErr     bool
	}{
		{
			name:        "accepted",
			result:      chatSteerSuccessResult(sessionID, queueItemID, true),
			wantOutcome: protoapi.OperationSuccess,
		},
		{
			name:        "not accepted",
			result:      chatSteerSuccessResult(sessionID, queueItemID, false),
			wantOutcome: protoapi.OperationSuccess,
		},
		{
			name: "typed failure",
			result: &chatpb.SteerResult{Outcome: &chatpb.SteerResult_Error{
				Error: &chatpb.ChatOperationError{
					Code: "session_not_found",
					Detail: &chatpb.ChatOperationError_SessionNotFound{
						SessionNotFound: &chatsettingspb.SessionNotFoundDetails{SessionId: sessionID},
					},
				},
			}},
			wantOutcome: protoapi.OperationKnownFailure,
			wantCode:    "session_not_found",
		},
		{
			name: "malformed success",
			result: &chatpb.SteerResult{Outcome: &chatpb.SteerResult_Success{
				Success: &chatpb.InputMutationSuccess{
					Outcome: &chatpb.InputMutationSuccess_Accepted{
						Accepted: &chatpb.InputAccepted{
							QueueItem: &chatpb.QueueItemIdentity{Id: queueItemID},
						},
					},
				},
			}},
			wantErr: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			classified, err := protoapi.ClassifyResult(test.result)
			if (err != nil) != test.wantErr {
				t.Fatalf("ClassifyResult error = %v, want error %t", err, test.wantErr)
			}
			if test.wantErr {
				return
			}
			if classified.Outcome != test.wantOutcome {
				t.Fatalf("outcome = %v, want %v", classified.Outcome, test.wantOutcome)
			}
			if test.wantCode != "" &&
				(classified.Failure == nil || classified.Failure.Code != test.wantCode) {
				t.Fatalf("failure = %+v, want code %q", classified.Failure, test.wantCode)
			}
		})
	}
}

func TestBinaryChatFailureMapsAgentPreparationCategories(t *testing.T) {
	sessionID := runtimeids.NewSessionID()
	request := &chatpb.SteerRequest{
		Target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{
			Session: &chatpb.ExistingSessionTarget{SessionId: sessionID.String()},
		}},
	}
	for _, test := range []struct {
		category serverapi.ChatSettingsAgentPreparationCategory
		want     chatsettingspb.AgentPreparationCategory
	}{
		{
			category: serverapi.ChatSettingsAgentInvalidConfiguration,
			want: chatsettingspb.
				AgentPreparationCategory_AGENT_PREPARATION_CATEGORY_INVALID_CONFIGURATION,
		},
		{
			category: serverapi.ChatSettingsAgentProviderUnavailable,
			want: chatsettingspb.
				AgentPreparationCategory_AGENT_PREPARATION_CATEGORY_PROVIDER_UNAVAILABLE,
		},
		{
			category: serverapi.ChatSettingsAgentInternalPreparation,
			want: chatsettingspb.
				AgentPreparationCategory_AGENT_PREPARATION_CATEGORY_INTERNAL_PREPARATION,
		},
	} {
		t.Run(string(test.category), func(t *testing.T) {
			detail := binaryChatFailure(
				nil,
				nil,
				request,
				&serverapi.ChatSettingsAgentPreparationError{
					Agent:    "reviewer",
					Category: test.category,
				},
			)
			preparation, ok := detail.(*chatsettingspb.AgentPreparationDetails)
			if !ok ||
				preparation.Agent != "reviewer" ||
				preparation.Category != test.want {
				t.Fatalf("Agent preparation details = %+v", preparation)
			}
		})
	}
}

func chatSteerSuccessResult(
	sessionID string,
	queueItemID string,
	accepted bool,
) *chatpb.SteerResult {
	success := &chatpb.InputMutationSuccess{
		Session: &chatpb.ExistingSessionTarget{SessionId: sessionID},
	}
	if accepted {
		success.Outcome = &chatpb.InputMutationSuccess_Accepted{
			Accepted: &chatpb.InputAccepted{
				QueueItem: &chatpb.QueueItemIdentity{Id: queueItemID},
			},
		}
	} else {
		success.Outcome = &chatpb.InputMutationSuccess_NotAccepted{
			NotAccepted: &chatpb.InputNotAccepted{
				Reason: &chatpb.InputNotAccepted_PendingWorkCapacity{
					PendingWorkCapacity: &chatpb.PendingWorkCapacityDetails{},
				},
			},
		}
	}
	return &chatpb.SteerResult{Outcome: &chatpb.SteerResult_Success{
		Success: success,
	}}
}

func TestRoutePolicyAuthorizesSessionScopesWithoutWebSocket(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	executor := newRoutePolicyExecutor(fixture.gateway)
	ctx := context.Background()

	for _, method := range []protoreflect.MethodDescriptor{
		sessionpb.File_kent_api_session_session_proto.Services().ByName("ReadService").Methods().ByName("GetMainView"),
		transcriptpb.File_kent_api_transcript_transcript_proto.Services().ByName("ReadService").Methods().ByName("GetPage"),
		transcriptpb.File_kent_api_transcript_transcript_proto.Services().ByName("ReadService").Methods().ByName("GetLatestFinalAnswer"),
	} {
		operation, err := protoapi.OperationFromDescriptor(method)
		if err != nil {
			t.Fatal(err)
		}
		if err := executor.authorizeScopeFacts(ctx, &connectionState{attachedProject: fixture.bindingA.ProjectID}, routeScopePolicy(operation.Options.ScopePolicy), operation.Name, routeScopeParams{sessionID: fixture.ownSessionID}); err != nil {
			t.Fatalf("active project own Session %s: %v", operation.Name, err)
		}
		if err := executor.authorizeScopeFacts(ctx, &connectionState{attachedProject: fixture.bindingA.ProjectID}, routeScopePolicy(operation.Options.ScopePolicy), operation.Name, routeScopeParams{sessionID: fixture.foreignSessionID}); err == nil {
			t.Fatalf("active project foreign Session %s unexpectedly allowed", operation.Name)
		}
	}
	draftOperation, err := protoapi.OperationFromDescriptor(sessionlaunchpb.File_kent_api_session_launch_session_lifecycle_proto.Services().ByName("SessionLifecycleService").Methods().ByName("PersistInputDraft"))
	if err != nil {
		t.Fatal(err)
	}
	reboundSessionID, err := runtimeids.ParseSessionID(fixture.reboundSessionID)
	if err != nil {
		t.Fatalf("parse rebound Session ID: %v", err)
	}
	draftState := &connectionState{
		attachedProject: fixture.bindingA.ProjectID,
		attachedSession: &reboundSessionID,
	}
	if err := executor.authorizeScopeFacts(
		ctx,
		draftState,
		routeScopePolicy(draftOperation.Options.ScopePolicy),
		draftOperation.Name,
		routeScopeParams{sessionID: fixture.reboundSessionID},
	); err != nil {
		t.Fatalf("rebind source project draft handoff: %v", err)
	}
	if err := executor.authorizeScopeFacts(
		ctx,
		&connectionState{attachedProject: fixture.bindingA.ProjectID},
		routeScopePolicy(draftOperation.Options.ScopePolicy),
		draftOperation.Name,
		routeScopeParams{sessionID: fixture.reboundSessionID},
	); err == nil {
		t.Fatal("detached source-project draft mutation unexpectedly allowed")
	}
	if err := executor.authorizeScopeFacts(
		ctx,
		draftState,
		routeScopePolicy(draftOperation.Options.ScopePolicy),
		draftOperation.Name,
		routeScopeParams{sessionID: fixture.foreignSessionID},
	); err == nil {
		t.Fatal("unrelated foreign-project draft mutation unexpectedly allowed")
	}
	executionEnvironmentOperation, err := protoapi.OperationFromDescriptor(sessionpb.File_kent_api_session_session_proto.Services().ByName("ReadService").Methods().ByName("GetExecutionEnvironment"))
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.authorizeScopeFacts(
		ctx,
		&connectionState{attachedProject: fixture.bindingA.ProjectID},
		routeScopePolicy(executionEnvironmentOperation.Options.ScopePolicy),
		executionEnvironmentOperation.Name,
		routeScopeParams{sessionID: fixture.ownSessionID},
	); err != nil {
		t.Fatalf("active project own execution environment: %v", err)
	}
	followUpOperation, err := protoapi.OperationFromDescriptor(promptpb.File_kent_api_prompt_prompt_proto.Services().ByName("FollowUpService").Methods().ByName("Watch"))
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.authorizeScopeFacts(
		ctx,
		&connectionState{attachedProject: fixture.bindingA.ProjectID},
		routeScopePolicy(followUpOperation.Options.ScopePolicy),
		followUpOperation.Name,
		routeScopeParams{sessionID: fixture.ownSessionID},
	); err != nil {
		t.Fatalf("active project own prompt follow-up watch: %v", err)
	}
	if err := executor.authorizeScopeFacts(
		ctx,
		&connectionState{attachedProject: fixture.bindingA.ProjectID},
		routeScopePolicy(followUpOperation.Options.ScopePolicy),
		followUpOperation.Name,
		routeScopeParams{sessionID: runtimeids.NewSessionID().String()},
	); err == nil {
		t.Fatal("active project foreign prompt follow-up watch unexpectedly allowed")
	}
	attachedOperation, err := protoapi.OperationFromDescriptor(sessionlaunchpb.File_kent_api_session_launch_session_lifecycle_proto.Services().ByName("SessionLifecycleService").Methods().ByName("RetargetWorkspace"))
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.authorizeScopeFacts(ctx, &connectionState{}, routeScopePolicy(attachedOperation.Options.ScopePolicy), attachedOperation.Name, routeScopeParams{sessionID: fixture.foreignSessionID}); err != nil {
		t.Fatalf("attached-project unscoped session: %v", err)
	}
	if err := executor.authorizeScopeFacts(ctx, &connectionState{attachedProject: fixture.bindingA.ProjectID}, routeScopePolicy(attachedOperation.Options.ScopePolicy), attachedOperation.Name, routeScopeParams{sessionID: fixture.foreignSessionID}); err == nil {
		t.Fatal("attached-project foreign session unexpectedly allowed")
	}

	optionalOperation, err := protoapi.OperationFromDescriptor(sessionlaunchpb.File_kent_api_session_launch_session_lifecycle_proto.Services().ByName("SessionLifecycleService").Methods().ByName("GetInitialInput"))
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.authorizeScopeFacts(ctx, &connectionState{attachedProject: fixture.bindingA.ProjectID}, routeScopePolicy(optionalOperation.Options.ScopePolicy), optionalOperation.Name, routeScopeParams{}); err != nil {
		t.Fatalf("optional empty session: %v", err)
	}
	if err := executor.authorizeScopeFacts(ctx, &connectionState{attachedProject: fixture.bindingA.ProjectID}, routeScopePolicy(optionalOperation.Options.ScopePolicy), optionalOperation.Name, routeScopeParams{sessionID: fixture.foreignSessionID}); err == nil {
		t.Fatal("optional foreign session unexpectedly allowed")
	}

	if err := executor.authorizeScopeFacts(
		ctx,
		&connectionState{attachedProject: fixture.bindingA.ProjectID},
		rpccontract.ScopeAttachSession,
		"AttachSession",
		routeScopeParams{sessionID: fixture.ownSessionID},
	); err != nil {
		t.Fatalf("attach own session: %v", err)
	}
	if err := executor.authorizeScopeFacts(
		ctx,
		&connectionState{attachedProject: fixture.bindingA.ProjectID},
		rpccontract.ScopeAttachSession,
		"AttachSession",
		routeScopeParams{sessionID: fixture.foreignSessionID},
	); err == nil {
		t.Fatal("attach foreign session unexpectedly allowed")
	}
}

func TestRoutePolicyAllowsRuntimeReleaseAfterSessionMovesProjects(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	operation, err := protoapi.OperationFromDescriptor(sessionlaunchpb.File_kent_api_session_launch_session_lifecycle_proto.Services().ByName("SessionRuntimeService").Methods().ByName("Release"))
	if err != nil {
		t.Fatal(err)
	}
	// The handler injects the connection-owned runtime owner ID; Project scope
	// must not reject the release before Runtime authority validates that owner.
	err = newRoutePolicyExecutor(fixture.gateway).authorizeScopeFacts(
		context.Background(),
		&connectionState{attachedProject: fixture.bindingA.ProjectID},
		routeScopePolicy(operation.Options.ScopePolicy),
		operation.Name,
		routeScopeParams{sessionID: fixture.foreignSessionID},
	)
	if err != nil {
		t.Fatalf("authorize moved Session runtime release: %v", err)
	}
}

func TestRoutePolicyAuthorizesGoalExceptionWithoutWebSocket(t *testing.T) {
	appCore, server := newUnboundGatewayTestServer(t)
	server.Close()
	gateway, err := NewGateway(appCore, gatewayTestIdentity())
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	operation, err := protoapi.OperationFromDescriptor(runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("GoalService").Methods().ByName("Show"))
	if err != nil {
		t.Fatal(err)
	}
	err = newRoutePolicyExecutor(gateway).authorizeScopeFacts(context.Background(), &connectionState{}, routeScopePolicy(operation.Options.ScopePolicy), operation.Name, routeScopeParams{sessionID: "missing-session"})
	if err != nil {
		t.Fatalf("unbound goal scope: %v", err)
	}

	fixture := newRoutePolicyFixture(t)
	err = newRoutePolicyExecutor(fixture.gateway).authorizeScopeFacts(
		context.Background(),
		&connectionState{attachedProject: fixture.bindingA.ProjectID},
		routeScopePolicy(operation.Options.ScopePolicy), operation.Name,
		routeScopeParams{sessionID: fixture.foreignSessionID},
	)
	if err == nil {
		t.Fatal("active-project foreign goal scope unexpectedly allowed")
	}
}

func TestRoutePolicyAuthorizesRuntimeLiveControlsWithoutActiveProject(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	executor := newRoutePolicyExecutor(fixture.gateway)
	ctx := context.Background()
	authorize := func(method protoreflect.Name, sessionID string) error {
		operation, err := protoapi.OperationFromDescriptor(runtimepb.File_kent_api_runtime_runtime_proto.Services().ByName("LiveService").Methods().ByName(method))
		if err != nil {
			t.Fatal(err)
		}
		return executor.authorizeScopeFacts(ctx, &connectionState{}, routeScopePolicy(operation.Options.ScopePolicy), operation.Name, routeScopeParams{sessionID: sessionID})
	}
	if err := authorize("Steer", fixture.ownSessionID); err != nil {
		t.Fatalf("live steer root-scoped existing session: %v", err)
	}
	if err := authorize("Wait", fixture.ownSessionID); err != nil {
		t.Fatalf("live wait root-scoped existing session: %v", err)
	}
	missing := "6ff7ace4-e08b-43fc-b425-73242f0b3d26"
	if err := authorize("Steer", missing); !errors.Is(err, serverapi.ErrRuntimeUnavailable) {
		t.Fatalf("missing required live session error = %v, want ErrRuntimeUnavailable", err)
	}
	if err := authorize("Stop", missing); err != nil {
		t.Fatalf("optional live stop missing session: %v", err)
	}
}

func TestRoutePolicyAuthorizesProcessScopesWithoutWebSocket(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	fixture.appCore.Background().SetMinimumExecToBgTime(time.Millisecond)
	ctx := context.Background()
	own, err := fixture.appCore.Background().Start(ctx, shelltool.ExecRequest{
		Postprocessor:  postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:        []string{"/bin/sh", "-lc", "printf own\\n; sleep 1"},
		DisplayCommand: "printf own; sleep 1",
		OwnerSessionID: fixture.ownSessionID,
		Workdir:        fixture.appCore.Config().WorkspaceRoot,
		YieldTime:      time.Millisecond,
	})
	if err != nil {
		t.Fatalf("start own process: %v", err)
	}
	foreign, err := fixture.appCore.Background().Start(ctx, shelltool.ExecRequest{
		Postprocessor:  postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:        []string{"/bin/sh", "-lc", "printf foreign\\n; sleep 1"},
		DisplayCommand: "printf foreign; sleep 1",
		OwnerSessionID: fixture.foreignSessionID,
		Workdir:        fixture.workspaceB,
		YieldTime:      time.Millisecond,
	})
	if err != nil {
		t.Fatalf("start foreign process: %v", err)
	}
	ownerless, err := fixture.appCore.Background().Start(ctx, shelltool.ExecRequest{
		Postprocessor:  postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}),
		Command:        []string{"/bin/sh", "-lc", "printf ownerless\\n; sleep 1"},
		DisplayCommand: "printf ownerless; sleep 1",
		Workdir:        fixture.appCore.Config().WorkspaceRoot,
		YieldTime:      time.Millisecond,
	})
	if err != nil {
		t.Fatalf("start ownerless process: %v", err)
	}

	executor := newRoutePolicyExecutor(fixture.gateway)
	state := &connectionState{attachedProject: fixture.bindingA.ProjectID}
	processOperation, err := protoapi.OperationFromDescriptor(processpb.File_kent_api_process_process_proto.Services().ByName("ViewService").Methods().ByName("Get"))
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.authorizeScopeFacts(ctx, state, routeScopePolicy(processOperation.Options.ScopePolicy), processOperation.Name, routeScopeParams{processID: own.SessionID}); err != nil {
		t.Fatalf("own process: %v", err)
	}
	if err := executor.authorizeScopeFacts(ctx, state, routeScopePolicy(processOperation.Options.ScopePolicy), processOperation.Name, routeScopeParams{processID: foreign.SessionID}); err == nil {
		t.Fatal("foreign process unexpectedly allowed")
	}
	if err := executor.authorizeScopeFacts(ctx, state, routeScopePolicy(processOperation.Options.ScopePolicy), processOperation.Name, routeScopeParams{processID: ownerless.SessionID}); err == nil {
		t.Fatal("ownerless process unexpectedly allowed")
	}

	listOperation, err := protoapi.OperationFromDescriptor(processpb.File_kent_api_process_process_proto.Services().ByName("ViewService").Methods().ByName("List"))
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.authorizeScopeFacts(ctx, state, routeScopePolicy(listOperation.Options.ScopePolicy), listOperation.Name, routeScopeParams{
		projectID: fixture.bindingA.ProjectID,
	}); err != nil {
		t.Fatalf("project-scoped process list: %v", err)
	}
}

func TestRoutePolicyAuthorizesAttachmentAndProjectWorkspaceScopesWithoutWebSocket(t *testing.T) {
	fixture := newRoutePolicyFixture(t)
	executor := newRoutePolicyExecutor(fixture.gateway)
	ctx := context.Background()

	transcriptOperation, err := protoapi.OperationFromDescriptor(transcriptpb.File_kent_api_transcript_transcript_proto.Services().ByName("StreamService").Methods().ByName("Subscribe"))
	if err != nil {
		t.Fatal(err)
	}
	ownSessionID, err := runtimeids.ParseSessionID(fixture.ownSessionID)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.authorizeScopeFacts(ctx, &connectionState{attachedSession: &ownSessionID}, routeScopePolicy(transcriptOperation.Options.ScopePolicy), transcriptOperation.Name, routeScopeParams{sessionID: fixture.ownSessionID}); err != nil {
		t.Fatalf("attached transcript subscription: %v", err)
	}
	err = executor.authorizeScopeFacts(ctx, &connectionState{attachedSession: &ownSessionID}, routeScopePolicy(transcriptOperation.Options.ScopePolicy), transcriptOperation.Name, routeScopeParams{sessionID: fixture.foreignSessionID})
	var routeErr gatewayRouteError
	if !errors.As(err, &routeErr) || routeErr.code != protocol.ErrCodeInvalidRequest {
		t.Fatalf("attached transcript mismatch error = %v, want invalid request route error", err)
	}
	questionHistoryOperation, err := protoapi.OperationFromDescriptor(sessionpb.File_kent_api_session_session_proto.Services().ByName("QuestionHistoryService").Methods().ByName("Subscribe"))
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.authorizeScopeFacts(ctx, &connectionState{attachedSession: &ownSessionID}, routeScopePolicy(questionHistoryOperation.Options.ScopePolicy), questionHistoryOperation.Name, routeScopeParams{
		sessionID: fixture.ownSessionID,
	}); err != nil {
		t.Fatalf("attached Question-history subscription: %v", err)
	}
	err = executor.authorizeScopeFacts(ctx, &connectionState{attachedSession: &ownSessionID}, routeScopePolicy(questionHistoryOperation.Options.ScopePolicy), questionHistoryOperation.Name, routeScopeParams{
		sessionID: fixture.foreignSessionID,
	})
	if !errors.As(err, &routeErr) || routeErr.code != protocol.ErrCodeInvalidRequest {
		t.Fatalf("attached Question-history mismatch error = %v, want invalid request route error", err)
	}

	projectWorkspaceMethod := sessionlaunchpb.File_kent_api_session_launch_session_launch_proto.Services().
		ByName("SessionLaunchService").Methods().ByName("Plan")
	projectWorkspaceOperation, err := protoapi.OperationFromDescriptor(projectWorkspaceMethod)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.authorizeScopeFacts(
		ctx,
		&connectionState{attachedProject: fixture.bindingA.ProjectID},
		routeScopePolicy(projectWorkspaceOperation.Options.ScopePolicy),
		projectWorkspaceOperation.Name,
		routeScopeParams{},
	); err != nil {
		t.Fatalf("project workspace with attached project: %v", err)
	}
	workspaceListMethod := worktreepb.File_kent_api_worktree_worktree_proto.Services().
		ByName("ListService").Methods().ByName("ListWorkspace")
	workspaceListOperation, err := protoapi.OperationFromDescriptor(workspaceListMethod)
	if err != nil {
		t.Fatal(err)
	}
	authorizeWorkspaceList := func(request *worktreepb.WorkspaceListRequest) error {
		scopeParams, err := worktreeWorkspaceScope(request)
		if err != nil {
			return err
		}
		return executor.authorizeScopeFacts(
			ctx,
			&connectionState{attachedProject: fixture.bindingA.ProjectID, attachedWorkspaceID: fixture.bindingA.WorkspaceID},
			routeScopePolicy(workspaceListOperation.Options.ScopePolicy),
			workspaceListOperation.Name,
			scopeParams,
		)
	}
	if err := authorizeWorkspaceList(&worktreepb.WorkspaceListRequest{
		ProjectId:   fixture.bindingA.ProjectID,
		WorkspaceId: fixture.bindingA.WorkspaceID,
	}); err != nil {
		t.Fatalf("workspace list with matching project/workspace: %v", err)
	}
	for _, request := range []*worktreepb.WorkspaceListRequest{
		{ProjectId: fixture.bindingB.ProjectID, WorkspaceId: fixture.bindingB.WorkspaceID},
		{ProjectId: fixture.bindingA.ProjectID, WorkspaceId: fixture.bindingB.WorkspaceID},
	} {
		if err := authorizeWorkspaceList(request); err == nil {
			t.Fatalf("foreign workspace list unexpectedly allowed: %+v", request)
		}
	}
	unboundCore, unboundServer := newUnboundGatewayTestServer(t)
	unboundServer.Close()
	unboundGateway, err := NewGateway(unboundCore, gatewayTestIdentity())
	if err != nil {
		t.Fatalf("NewGateway unbound: %v", err)
	}
	if err := newRoutePolicyExecutor(unboundGateway).authorizeScopeFacts(
		ctx,
		&connectionState{},
		routeScopePolicy(projectWorkspaceOperation.Options.ScopePolicy),
		projectWorkspaceOperation.Name,
		routeScopeParams{},
	); err == nil {
		t.Fatal("project workspace without active project unexpectedly allowed")
	}
}

type routePolicyFixture struct {
	appCore          *core.Core
	gateway          *Gateway
	bindingA         metadata.Binding
	bindingB         metadata.Binding
	ownSessionID     string
	foreignSessionID string
	reboundSessionID string
	workspaceB       string
}

func newRoutePolicyFixture(t *testing.T) routePolicyFixture {
	t.Helper()
	home := t.TempDir()
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	t.Setenv("HOME", home)
	configureGatewayTestServerPort(t)

	resolvedA, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspaceA})
	if err != nil {
		t.Fatalf("ResolveConfig A: %v", err)
	}
	bindingA, err := metadata.RegisterBinding(context.Background(), resolvedA.Config.PersistenceRoot, resolvedA.Config.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterBinding A: %v", err)
	}
	resolvedB, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{WorkspaceRoot: workspaceB})
	if err != nil {
		t.Fatalf("ResolveConfig B: %v", err)
	}
	bindingB, err := metadata.RegisterBinding(context.Background(), resolvedB.Config.PersistenceRoot, resolvedB.Config.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterBinding B: %v", err)
	}
	metadataStore, err := metadata.Open(resolvedA.Config.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = metadataStore.Close() })
	authSupport := newGatewayTestAuthSupport(t, true)
	background, err := serverbootstrap.BuildShellManager(resolvedA.Config)
	if err != nil {
		t.Fatalf("BuildShellManager: %v", err)
	}
	t.Cleanup(func() { _ = background.Close() })
	appCore, err := core.New(resolvedA.Config, authSupport, background)
	if err != nil {
		t.Fatalf("core.New: %v", err)
	}
	t.Cleanup(func() { _ = appCore.Close() })
	ownStore := createGatewayAuthoritativeSession(t, appCore)
	foreignStore, err := session.Create(
		filepath.Join(filepath.Join(resolvedB.Config.PersistenceRoot, "projects"), bindingB.ProjectID, "sessions"),
		"workspace-b",
		resolvedB.Config.WorkspaceRoot, sessioncontract.SessionCategoryMain, metadataStore.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create foreign: %v", err)
	}
	if err := foreignStore.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable foreign: %v", err)
	}
	reboundStore, err := session.Create(
		filepath.Join(filepath.Join(resolvedB.Config.PersistenceRoot, "projects"), bindingB.ProjectID, "sessions"),
		"workspace-b",
		resolvedB.Config.WorkspaceRoot, sessioncontract.SessionCategoryMain, metadataStore.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create rebound: %v", err)
	}
	if err := reboundStore.EnsureDurable(); err != nil {
		t.Fatalf("EnsureDurable rebound: %v", err)
	}
	gateway, err := NewGateway(appCore, gatewayTestIdentity())
	if err != nil {
		t.Fatalf("NewGateway: %v", err)
	}
	return routePolicyFixture{
		appCore:          appCore,
		gateway:          gateway,
		bindingA:         bindingA,
		bindingB:         bindingB,
		ownSessionID:     ownStore.Meta().SessionID,
		foreignSessionID: foreignStore.Meta().SessionID,
		reboundSessionID: reboundStore.Meta().SessionID,
		workspaceB:       resolvedB.Config.WorkspaceRoot,
	}
}

func routeForTest(t *testing.T, method string) rpccontract.Route {
	t.Helper()
	route, ok := rpccontract.RouteByMethod(method)
	if !ok {
		t.Fatalf("route %q missing", method)
	}
	return route
}

func routePolicyNewChatTarget(projectID string, workspaceID string) *chatpb.ChatTarget {
	questions, autoCompaction := true, true
	return &chatpb.ChatTarget{
		Target: &chatpb.ChatTarget_NewChat{NewChat: &chatpb.NewChatTarget{
			ProjectId:   projectID,
			WorkspaceId: workspaceID,
			InitialSettings: &chatsettingspb.InitialChatSettings{
				AgentRole:             "default",
				Supervisor:            chatsettingspb.SupervisorValue_SUPERVISOR_VALUE_OFF,
				QuestionsEnabled:      &questions,
				AutoCompactionEnabled: &autoCompaction,
			},
		}},
	}
}
