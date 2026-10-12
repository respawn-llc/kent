package core

import (
	"context"
	"core/internal/testharness/testsetup"
	"sync"
	"testing"
	"time"

	"core/server/auth"
	serverbootstrap "core/server/bootstrap"
	"core/server/llm"
	"core/server/runtimewire"
	brand "core/shared/config"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/serverapi"
	"core/shared/textutil"
)

type chatSettingsBoundaryLLMClient struct {
	started      chan struct{}
	startedOnce  sync.Once
	release      chan struct{}
	releaseOnce  sync.Once
	firstContext context.Context
}

func newChatSettingsBoundaryLLMClient() *chatSettingsBoundaryLLMClient {
	return &chatSettingsBoundaryLLMClient{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (c *chatSettingsBoundaryLLMClient) Generate(
	ctx context.Context,
	_ llm.Request,
	_ llm.StreamCallbacks,
) (llm.Response, error) {
	c.startedOnce.Do(func() { c.firstContext = ctx; close(c.started) })
	select {
	case <-c.release:
		return llm.Response{
			Assistant: llm.Message{
				Role:    llm.RoleAssistant,
				Content: textutil.Value("done"),
				Phase:   textutil.Value(llm.MessagePhaseFinal),
			},
			Usage: llm.Usage{WindowTokens: 200_000},
		}, nil
	case <-ctx.Done():
		return llm.Response{}, context.Cause(ctx)
	}
}

func (*chatSettingsBoundaryLLMClient) ProviderCapabilities(context.Context) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilities{
		ProviderID:           "openai",
		SupportsResponsesAPI: true,
		IsOpenAIFirstParty:   true,
	}, nil
}

func (c *chatSettingsBoundaryLLMClient) unblock() {
	c.releaseOnce.Do(func() { close(c.release) })
}

func TestChatSettingsMutationReturnsAfterRuntimeAcceptance(t *testing.T) {
	testChatSettingsRuntimeMutation(t, false)
}

func TestChatSettingsAgentReplacementStopsActiveRun(t *testing.T) {
	testChatSettingsRuntimeMutation(t, true)
}

func testChatSettingsRuntimeMutation(t *testing.T, switchAgent bool) {
	workspace := t.TempDir()
	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{
		WorkspaceRoot: workspace,
		LoadOptions:   brand.LoadOptions{ConfigRoot: t.TempDir()},
	})
	if err != nil {
		t.Fatalf("ResolveConfig: %v", err)
	}
	metadataStore := testsetup.OpenStore(t, resolved.Config.PersistenceRoot)
	binding, err := metadataStore.RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatalf("RegisterBinding: %v", err)
	}
	authSupport, err := serverbootstrap.BuildAuthSupport(t.Context(), resolved.Config.PersistenceRoot, auth.NewMemoryStore(auth.EmptyState()), nil, nil)
	if err != nil {
		t.Fatalf("BuildAuthSupport: %v", err)
	}
	resolved.Config.Settings = testsetup.WriteProviderSettings(t, resolved.Config.PersistenceRoot, resolved.Config.Settings)
	background, err := serverbootstrap.BuildShellManager(resolved.Config)
	if err != nil {
		t.Fatalf("BuildShellManager: %v", err)
	}
	t.Cleanup(func() { _ = background.Close() })
	model := newChatSettingsBoundaryLLMClient()
	appCore, err := NewWithContextOptions(t.Context(), resolved.Config, authSupport, background, Options{
		MetadataStore: metadataStore,
		RuntimeClientFactory: runtimewire.RuntimeClientFactoryFunc(func(
			context.Context,
			runtimewire.RuntimeClientRequest,
		) (llm.Client, error) {
			return model, nil
		}),
	})
	if err != nil {
		t.Fatalf("NewWithContextOptions: %v", err)
	}
	t.Cleanup(func() { _ = appCore.Close() })

	launch, err := appCore.SessionLaunchClientForProjectWorkspace(t.Context(), binding.ProjectID, workspace)
	if err != nil {
		t.Fatalf("SessionLaunchClientForProjectWorkspace: %v", err)
	}
	intent, err := protoapi.SessionLaunchIntentToProto(
		serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
	)
	if err != nil {
		t.Fatalf("convert Session launch intent: %v", err)
	}
	planned, err := launch.PlanSession(t.Context(), &sessionlaunchpb.SessionPlanRequest{
		Mode:   sessionlaunchpb.SessionLaunchMode_SESSION_LAUNCH_MODE_INTERACTIVE,
		Intent: intent,
	})
	if err != nil {
		t.Fatalf("PlanSession: %v", err)
	}
	sessionID, err := runtimeids.ParseSessionID(planned.Plan.SessionId)
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	chatSettings := appCore.ChatSettingsClient()
	before, err := chatSettings.ReadChatSettings(t.Context(), &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_Session{Session: &chatsettingspb.SessionTarget{SessionId: sessionID.String()}},
	})
	if err != nil {
		t.Fatalf("ReadChatSettings before mutation: %v", err)
	}
	initialQuestions := before.GetSession().Settings.Questions.Enabled
	requestedQuestions := !initialQuestions

	const ownerID = "chat-settings-acceptance-test"
	activation, err := appCore.SessionRuntimeClient().ActivateSessionRuntime(t.Context(), serverapi.SessionRuntimeActivateRequest{
		SessionID:             sessionID.String(),
		OwnerID:               ownerID,
		ActiveSettings:        resolved.Config.Settings,
		QuestionsEnabled:      textutil.Value(initialQuestions),
		AutoCompactionEnabled: textutil.Value(before.GetSession().Settings.AutoCompaction.Effective),
		Source:                resolved.Config.Source,
	})
	if err != nil {
		t.Fatalf("ActivateSessionRuntime: %v", err)
	}
	t.Cleanup(func() {
		_, _ = appCore.SessionRuntimeClient().ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
			Attachment:  activation,
			OwnerID:     ownerID,
			DropOwner:   true,
			ClosePolicy: serverapi.SessionRuntimeReleaseClosePolicyDetachOnly,
		})
	})
	t.Cleanup(model.unblock)

	turnDone := make(chan error, 1)
	input, err := protoapi.UserTurnInputToProto(runtimeinput.Text("hold the Agent Step"))
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		_, submitErr := appCore.RuntimeControlClient().SubmitUserTurn(t.Context(), &runtimepb.SubmitUserTurnRequest{
			SessionId: sessionID.String(),
			Input:     input,
		})
		turnDone <- submitErr
	}()
	select {
	case <-model.started:
	case <-time.After(5 * time.Second):
		t.Fatal("Agent Step did not reach the model request")
	}
	if switchAgent {
		for _, role := range []string{"unavailable-agent", brand.DefaultSubagentRole} {
			result, err := chatSettings.MutateChatSettings(t.Context(), &chatsettingspb.MutationRequest{
				Session: &chatsettingspb.SessionTarget{SessionId: sessionID.String()},
				Operation: &chatsettingspb.MutationOperation{
					Operation: &chatsettingspb.MutationOperation_AgentRole{AgentRole: role},
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			if role == brand.DefaultSubagentRole {
				if result.Result.GetApplied() == nil {
					t.Fatalf("current Agent selection was rejected: %+v", result)
				}
			} else if result.Result.GetRejected() == nil {
				t.Fatalf("unavailable Agent accepted: %+v", result)
			}
			if err := context.Cause(model.firstContext); err != nil {
				t.Fatalf("rejected/no-op selection stopped current run: %v", err)
			}
		}
		switched, err := chatSettings.MutateChatSettings(t.Context(), &chatsettingspb.MutationRequest{
			Session: &chatsettingspb.SessionTarget{SessionId: sessionID.String()},
			Operation: &chatsettingspb.MutationOperation{
				Operation: &chatsettingspb.MutationOperation_AgentRole{AgentRole: brand.BuiltInSubagentRoleFast},
			},
		})
		if err != nil {
			t.Fatal(err)
		}
		if switched.Result.GetApplied() == nil || switched.Settings.SelectedAgent.Role != brand.BuiltInSubagentRoleFast || switched.Settings.CachingLocked {
			t.Fatalf("Agent was not replaced: %+v", switched)
		}
		if context.Cause(model.firstContext) == nil {
			t.Fatal("old model request was not canceled")
		}
		record, err := appCore.MetadataStore().ResolvePersistedSession(t.Context(), sessionID.String())
		if err != nil {
			t.Fatal(err)
		}
		if record.Meta.SessionID != sessionID.String() || record.Meta.Continuation == nil || record.Meta.Continuation.AgentRole == nil || *record.Meta.Continuation.AgentRole != brand.BuiltInSubagentRoleFast || record.Meta.Locked != nil {
			t.Fatalf("persisted Agent replacement: %+v", record.Meta)
		}
		model.unblock()
		sent, err := appCore.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{
			Target:     chatCommandTarget(sessionID.String()),
			Activation: &chatpb.Activation{Input: &chatpb.Activation_Text{Text: "continue with the new Agent"}},
		})
		if err != nil || sent.GetAccepted() == nil {
			t.Fatalf("next send after replacement: %v, %v", sent, err)
		}
		deadline := time.Now().Add(5 * time.Second)
		for {
			record, err = appCore.MetadataStore().ResolvePersistedSession(t.Context(), sessionID.String())
			if err != nil {
				t.Fatal(err)
			}
			if record.Meta.Locked != nil && record.Meta.Locked.Model == switched.Settings.SelectedAgent.Model {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("next request did not rebuild the selected model: %+v", record.Meta.Locked)
			}
			time.Sleep(time.Millisecond)
		}
		return
	}

	type mutationResult struct {
		response *chatsettingspb.MutationSuccess
		err      error
	}
	mutationDone := make(chan mutationResult, 1)
	go func() {
		response, mutationErr := chatSettings.MutateChatSettings(t.Context(), &chatsettingspb.MutationRequest{
			Session: &chatsettingspb.SessionTarget{SessionId: sessionID.String()},
			Operation: &chatsettingspb.MutationOperation{
				Operation: &chatsettingspb.MutationOperation_QuestionsEnabled{QuestionsEnabled: requestedQuestions},
			},
		})
		mutationDone <- mutationResult{response: response, err: mutationErr}
	}()

	var mutation mutationResult
	select {
	case mutation = <-mutationDone:
	case <-time.After(5 * time.Second):
		t.Fatal("Chat settings mutation waited for the active Agent Step to end")
	}
	if mutation.err != nil {
		t.Fatalf("MutateChatSettings: %v", mutation.err)
	}
	if mutation.response.Result.GetApplied() == nil || !mutation.response.Result.GetApplied().Changed {
		t.Fatalf("mutation result = %+v, want a durable change", mutation.response.Result)
	}
	if mutation.response.Settings.Questions.Enabled != requestedQuestions {
		t.Fatalf(
			"response Questions = %t, want durably committed %t",
			mutation.response.Settings.Questions.Enabled,
			requestedQuestions,
		)
	}
	if live := currentCoreRuntimeQuestions(t, appCore, sessionID); live != initialQuestions {
		t.Fatalf("live Questions = %t before Agent Step boundary, want %t", live, initialQuestions)
	}

	model.unblock()
	select {
	case err := <-turnDone:
		if err != nil {
			t.Fatalf("SubmitUserTurn: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Agent Step did not finish after model release")
	}
	waitForCoreRuntimeQuestions(t, appCore, sessionID, requestedQuestions)
}

func currentCoreRuntimeQuestions(t *testing.T, appCore *Core, sessionID runtimeids.SessionID) bool {
	t.Helper()
	response, err := appCore.SessionViewClient().GetSessionMainView(t.Context(), &sessionpb.MainViewRequest{
		SessionId: sessionID.String(),
	})
	if err != nil {
		t.Fatalf("GetSessionMainView: %v", err)
	}
	return response.MainView.Status.QuestionsEnabled
}

func waitForCoreRuntimeQuestions(t *testing.T, appCore *Core, sessionID runtimeids.SessionID, expected bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		if currentCoreRuntimeQuestions(t, appCore, sessionID) == expected {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("live Questions did not converge to %t after the Agent Step boundary", expected)
		}
		time.Sleep(time.Millisecond)
	}
}
