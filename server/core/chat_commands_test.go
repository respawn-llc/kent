package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/auth"
	serverbootstrap "core/server/bootstrap"
	"core/server/llm"
	"core/server/metadata"
	"core/server/runtimewire"
	"core/server/session"
	"core/shared/apicontract"
	"core/shared/config"
	"core/shared/protoapi"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
	"core/shared/textutil"
)

type chatCommandModel struct {
	sessionID string
	client    *chatSettingsBoundaryLLMClient
}

func newChatCommandCore(t *testing.T, clientError error) (*Core, config.App, metadata.Binding, <-chan chatCommandModel) {
	t.Helper()
	resolved, err := serverbootstrap.ResolveConfig(serverbootstrap.Request{
		WorkspaceRoot: t.TempDir(), LoadOptions: config.LoadOptions{ConfigRoot: t.TempDir()},
	})
	if err != nil {
		t.Fatal(err)
	}
	metadataStore := testsetup.OpenStore(t, resolved.Config.PersistenceRoot)
	binding, err := metadataStore.RegisterWorkspaceBinding(t.Context(), resolved.Config.WorkspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	models := make(chan chatCommandModel, 8)
	app := newCoreTestAppWithOptions(t, resolved.Config, auth.EmptyState(), Options{
		MetadataStore: metadataStore,
		RuntimeClientFactory: runtimewire.RuntimeClientFactoryFunc(func(_ context.Context, request runtimewire.RuntimeClientRequest) (llm.Client, error) {
			if clientError != nil {
				return nil, clientError
			}
			model := newChatSettingsBoundaryLLMClient()
			if request.Purpose != runtimewire.RuntimeClientPurposeMain {
				model.unblock()
				return model, nil
			}
			models <- chatCommandModel{sessionID: request.SessionID, client: model}
			return model, nil
		}),
	})
	t.Cleanup(func() {
		for {
			select {
			case model := <-models:
				model.client.unblock()
			default:
				return
			}
		}
	})
	return app, resolved.Config, binding, models
}

func awaitChatCommandModel(t *testing.T, models <-chan chatCommandModel) chatCommandModel {
	t.Helper()
	select {
	case model := <-models:
		t.Cleanup(model.client.unblock)
		select {
		case <-model.client.started:
			return model
		case <-time.After(5 * time.Second):
			t.Fatal("command did not reach the selected Session model")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("command did not open a Runtime")
	}
	return chatCommandModel{}
}

func chatCommandTarget(id string) *chatpb.ChatTarget {
	return &chatpb.ChatTarget{Target: &chatpb.ChatTarget_Session{Session: &chatpb.ExistingSessionRequestTarget{SessionId: &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: id}}}}}
}

func chatCommandActivation(identity, token string) *chatpb.Activation {
	return &chatpb.Activation{Input: &chatpb.Activation_Command{Command: &chatpb.CommandInvocation{
		CatalogIdentity: identity, Token: token, SeparatorWhitespace: " ", Arguments: "src",
	}}}
}

func newChatCommandTarget(t *testing.T, app *Core, projectID, workspaceID string) *chatpb.ChatTarget {
	t.Helper()
	settings, err := app.ChatSettingsClient().ReadChatSettings(t.Context(), &chatsettingspb.ReadRequest{
		Target: &chatsettingspb.ReadRequest_NewChat{NewChat: &chatsettingspb.NewChatTarget{
			ProjectId: projectID, WorkspaceId: workspaceID,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return &chatpb.ChatTarget{Target: &chatpb.ChatTarget_NewChat{NewChat: &chatpb.NewChatTarget{
		ProjectId: projectID, WorkspaceId: workspaceID,
		InitialSettings: settings.GetNewChat().InitialSettings,
	}}}
}

func requireChatSessionCount(t *testing.T, app *Core, projectID string, want int) {
	t.Helper()
	page, err := app.MetadataStore().ListSessionPage(t.Context(), projectID, sessioncontract.SessionCategoryMain, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Sessions) != want {
		t.Fatalf("Session count = %d, want %d", len(page.Sessions), want)
	}
}

func TestNewChatTextInputCreatesOneSessionAndAcceptsAction(t *testing.T) {
	for _, queued := range []bool{false, true} {
		name := "Send"
		if queued {
			name = "Queue"
		}
		t.Run(name, func(t *testing.T) {
			app, _, binding, models := newChatCommandCore(t, nil)
			target := newChatCommandTarget(t, app, binding.ProjectID, binding.WorkspaceID)
			activation := &chatpb.Activation{Input: &chatpb.Activation_Text{Text: "first text"}}
			var result *chatpb.InputMutationSuccess
			var err error
			if queued {
				result, err = app.ChatMutationClient().Queue(t.Context(), &chatpb.QueueRequest{Target: target, Activation: activation, QueueItemId: runtimeids.NewQueueItemID().String()})
			} else {
				result, err = app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{Target: target, Activation: activation, QueueItemId: runtimeids.NewQueueItemID().String()})
			}
			if err != nil || result.GetAccepted() == nil {
				t.Fatalf("first Send = %v, %v", result, err)
			}
			model := awaitChatCommandModel(t, models)
			if model.sessionID != result.Session.SessionId {
				t.Fatalf("action ran in %s, created %s", model.sessionID, result.Session.SessionId)
			}
			requireChatSessionCount(t, app, binding.ProjectID, 1)
		})
	}
}

func TestNewChatGoalSetCreatesOneSessionAndAcceptsAction(t *testing.T) {
	app, _, binding, models := newChatCommandCore(t, nil)
	result, err := app.ChatMutationClient().SetGoal(t.Context(), &runtimepb.GoalSetRequest{
		Target:    newChatCommandTarget(t, app, binding.ProjectID, binding.WorkspaceID),
		Objective: "finish the task", Actor: "user",
		ExecutionPolicy:   runtimepb.GoalExecutionPolicy_GOAL_EXECUTION_POLICY_START_OR_CONTINUE,
		InitialInputDraft: textutil.Value("unsent text"),
	})
	if err != nil || result.GetMutation() == nil {
		t.Fatalf("first Goal Set = %v, %v", result, err)
	}
	model := awaitChatCommandModel(t, models)
	if model.sessionID != result.Session.SessionId {
		t.Fatalf("Goal ran in %s, created %s", model.sessionID, result.Session.SessionId)
	}
	requireChatSessionCount(t, app, binding.ProjectID, 1)
	record, err := app.MetadataStore().ResolvePersistedSession(t.Context(), result.Session.SessionId)
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.InputDraft != "unsent text" {
		t.Fatalf("Goal Set lost unsent draft: %q", record.Meta.InputDraft)
	}
}

func TestNewChatCompactionRetainsCreatedSessionOnOrdinaryRejection(t *testing.T) {
	app, _, binding, _ := newChatCommandCore(t, nil)
	result, err := app.ChatMutationClient().Compact(t.Context(), &chatpb.CompactRequest{
		Target:     newChatCommandTarget(t, app, binding.ProjectID, binding.WorkspaceID),
		Invocation: &chatpb.CompactionInvocation{Token: "/compact"},
	})
	if err != nil || result.GetSession() == nil || result.GetNotAccepted().GetTooSoon() == nil {
		t.Fatalf("first compaction = %v, %v", result, err)
	}
	record, err := app.MetadataStore().ResolvePersistedSession(t.Context(), result.Session.SessionId)
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.ModelRequestCount != 0 {
		t.Fatalf("ineligible compaction made %d model requests", record.Meta.ModelRequestCount)
	}
	requireChatSessionCount(t, app, binding.ProjectID, 1)
}

func TestNewChatTargetFailureReturnsCreatedSessionWithoutReplay(t *testing.T) {
	app, _, binding, _ := newChatCommandCore(t, nil)
	target := newChatCommandTarget(t, app, binding.ProjectID, binding.WorkspaceID)
	target.GetNewChat().Name = textutil.Value("future name")
	target.GetNewChat().WorktreeSelector = textutil.Value("unavailable-worktree")
	result, err := app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{
		QueueItemId: runtimeids.NewQueueItemID().String(),
		Target:      target,
		Activation:  &chatpb.Activation{Input: &chatpb.Activation_Text{Text: "retained draft"}},
	})
	if err != nil || result.GetNotAccepted() == nil {
		t.Fatalf("target failure = %v, %v", result, err)
	}
	record, err := app.MetadataStore().ResolvePersistedSession(t.Context(), result.Session.SessionId)
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.Name == nil || *record.Meta.Name != "future name" || record.Meta.InputDraft != "retained draft" ||
		record.Meta.ModelRequestCount != 0 {
		t.Fatalf("known failure lost creation inputs or started inference: %+v", record.Meta)
	}
	requireChatSessionCount(t, app, binding.ProjectID, 1)
}

func TestChatReviewQueueCreatesChildAndLeavesParentRunning(t *testing.T) {
	app, cfg, binding, models := newChatCommandCore(t, nil)
	source := createCoreSettingsSession(t, app, cfg, binding.ProjectID)
	id := source.Meta().SessionID
	parentResult, err := app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{QueueItemId: runtimeids.NewQueueItemID().String(), Target: chatCommandTarget(id), Activation: &chatpb.Activation{Input: &chatpb.Activation_Text{Text: "parent work"}}})
	if err != nil || parentResult.GetAccepted() == nil {
		t.Fatalf("parent acceptance = %v, %v", parentResult, err)
	}
	parent := awaitChatCommandModel(t, models)
	result, err := app.ChatMutationClient().Queue(t.Context(), &chatpb.QueueRequest{QueueItemId: runtimeids.NewQueueItemID().String(), Target: chatCommandTarget(id), Activation: chatCommandActivation("prompt:review", "/review")})
	if err != nil || result.GetAccepted() == nil {
		t.Fatalf("review acceptance = %v, %v", result, err)
	}
	childID := result.GetSession().GetSessionId()
	if childID == id {
		t.Fatal("busy review was admitted to its parent instead of a fresh child")
	}
	child := awaitChatCommandModel(t, models)
	if child.sessionID != childID || parent.sessionID != id {
		t.Fatalf("selected runtime = %s, want child %s", child.sessionID, childID)
	}
	record, err := session.ResolvePersistedSessionRecord(t.Context(), app.MetadataStore(), childID)
	if err != nil {
		t.Fatal(err)
	}
	parentID, err := runtimeids.ParseSessionID(id)
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.PreviousSessionID == nil || *record.Meta.PreviousSessionID != parentID {
		t.Fatalf("child previous Session = %v", record.Meta.PreviousSessionID)
	}
	activity, err := app.bundles.Runtime.runtimeRegistry.RuntimeReadModelFeedSnapshot(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if !protoapi.RuntimeActivityActiveForControl(activity.GetActivity()) {
		t.Fatal("parent stopped when child command started")
	}
}

func TestChatBuiltinsSelectNewFreshAndEstablishedSessions(t *testing.T) {
	for _, test := range []struct {
		name, identity, token string
		newChat, established  bool
	}{
		{name: "review canonical New Chat", identity: "prompt:review", token: "/prompt:review", newChat: true},
		{name: "init alias idle fresh", identity: "prompt:init", token: "/init"},
		{name: "init canonical established", identity: "prompt:init", token: "/prompt:init", established: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, cfg, binding, models := newChatCommandCore(t, nil)
			source := createCoreSettingsSession(t, app, cfg, binding.ProjectID)
			id := source.Meta().SessionID
			target := chatCommandTarget(id)
			if test.established {
				log, err := source.MaterializeEventLog()
				if err != nil {
					t.Fatal(err)
				}
				text := "earlier conversation"
				if _, _, err := log.AppendRecord(nil, session.MessageRecord{Role: session.MessageRoleUser, Content: &text}); err != nil {
					t.Fatal(err)
				}
			}
			if test.newChat {
				enabled := true
				target = &chatpb.ChatTarget{Target: &chatpb.ChatTarget_NewChat{NewChat: &chatpb.NewChatTarget{
					ProjectId: binding.ProjectID, WorkspaceId: binding.WorkspaceID,
					InitialSettings: &chatsettingspb.InitialChatSettings{AgentRole: "default", Supervisor: chatsettingspb.SupervisorValue_SUPERVISOR_VALUE_OFF, QuestionsEnabled: &enabled, AutoCompactionEnabled: &enabled},
				}}}
			}
			result, err := app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{QueueItemId: runtimeids.NewQueueItemID().String(), Target: target, Activation: chatCommandActivation(test.identity, test.token)})
			if err != nil || result.GetAccepted() == nil {
				t.Fatalf("command result = %v, %v", result, err)
			}
			selected := result.GetSession().GetSessionId()
			if (selected != id) != (test.newChat || test.established) {
				t.Fatalf("selected Session = %s, source = %s", selected, id)
			}
			model := awaitChatCommandModel(t, models)
			if model.sessionID != selected {
				t.Fatalf("executing Session = %s, want %s", model.sessionID, selected)
			}
			record, err := session.ResolvePersistedSessionRecord(t.Context(), app.MetadataStore(), selected)
			if err != nil {
				t.Fatal(err)
			}
			if test.established {
				if record.Meta.PreviousSessionID == nil || record.Meta.PreviousSessionID.String() != id {
					t.Fatalf("previous Session = %v, want %s", record.Meta.PreviousSessionID, id)
				}
			} else if record.Meta.PreviousSessionID != nil {
				t.Fatalf("unexpected child: %v", record.Meta.PreviousSessionID)
			}
		})
	}
}

func TestChatFileCommandRetainsSessionAndRequestedAdmission(t *testing.T) {
	app, cfg, binding, models := newChatCommandCore(t, nil)
	writeCorePromptFixture(t, cfg.WorkspaceRoot, "file", "file prompt $ARGUMENTS")
	source := createCoreSettingsSession(t, app, cfg, binding.ProjectID)
	id := source.Meta().SessionID
	activation := chatCommandActivation("prompt:file", "/prompt:file")
	result, err := app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{QueueItemId: runtimeids.NewQueueItemID().String(), Target: chatCommandTarget(id), Activation: activation})
	if err != nil || result.GetAccepted() == nil || result.GetSession().GetSessionId() != id {
		t.Fatalf("idle file Send = %v, %v", result, err)
	}
	awaitChatCommandModel(t, models)
	pendingService := app.RuntimeControlClient().(apicontract.RuntimePendingWorkService)
	for _, queue := range []bool{false, true} {
		wantLane := runtimepb.PendingWorkLane_PENDING_WORK_LANE_STEER
		if queue {
			result, err = app.ChatMutationClient().Queue(t.Context(), &chatpb.QueueRequest{QueueItemId: runtimeids.NewQueueItemID().String(), Target: chatCommandTarget(id), Activation: activation})
			wantLane = runtimepb.PendingWorkLane_PENDING_WORK_LANE_QUEUE
		} else {
			result, err = app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{QueueItemId: runtimeids.NewQueueItemID().String(), Target: chatCommandTarget(id), Activation: activation})
		}
		if err != nil || result.GetAccepted() == nil || result.GetSession().GetSessionId() != id {
			t.Fatalf("busy file submission = %v, %v", result, err)
		}
		pending, err := pendingService.ListPendingWork(t.Context(), &runtimepb.ListPendingWorkRequest{SessionId: id})
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, item := range pending.PendingWork.Items {
			if item.Id == result.GetAccepted().GetQueueItem().GetId() {
				found = true
				if item.Lane != wantLane {
					t.Fatalf("file command lane = %v, want %v", item.Lane, wantLane)
				}
			}
		}
		if !found {
			t.Fatal("accepted file command is absent from parent Pending Work")
		}
	}
}

func TestChatCommandFailuresRetainCreatedSessionIdentity(t *testing.T) {
	t.Run("New Chat resolution failure", func(t *testing.T) {
		app, _, binding, _ := newChatCommandCore(t, nil)
		enabled := true
		result, err := app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{QueueItemId: runtimeids.NewQueueItemID().String(), Target: &chatpb.ChatTarget{Target: &chatpb.ChatTarget_NewChat{NewChat: &chatpb.NewChatTarget{
			ProjectId: binding.ProjectID, WorkspaceId: binding.WorkspaceID,
			InitialSettings: &chatsettingspb.InitialChatSettings{AgentRole: "default", Supervisor: chatsettingspb.SupervisorValue_SUPERVISOR_VALUE_OFF, QuestionsEnabled: &enabled, AutoCompactionEnabled: &enabled},
		}}},
			Activation: chatCommandActivation("prompt:missing", "/prompt:missing"),
		})
		if err != nil || result.GetNotAccepted().GetPromptCommandNotFound() == nil {
			t.Fatalf("missing command result = %v, %v", result, err)
		}
		if _, err := session.ResolvePersistedSessionRecord(t.Context(), app.MetadataStore(), result.GetSession().GetSessionId()); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("child Runtime opening failure", func(t *testing.T) {
		app, cfg, binding, _ := newChatCommandCore(t, errors.New("model unavailable"))
		source := createCoreSettingsSession(t, app, cfg, binding.ProjectID)
		log, err := source.MaterializeEventLog()
		if err != nil {
			t.Fatal(err)
		}
		text := "earlier conversation"
		if _, _, err := log.AppendRecord(nil, session.MessageRecord{Role: session.MessageRoleUser, Content: &text}); err != nil {
			t.Fatal(err)
		}
		result, err := app.ChatMutationClient().Steer(t.Context(), &chatpb.SteerRequest{QueueItemId: runtimeids.NewQueueItemID().String(), Target: chatCommandTarget(source.Meta().SessionID), Activation: chatCommandActivation("prompt:init", "/init")})
		if err != nil || result.GetNotAccepted() == nil {
			t.Fatalf("child opening failure = %v, %v", result, err)
		}
		id := result.GetSession().GetSessionId()
		if id == source.Meta().SessionID {
			t.Fatal("failed child was not delivered")
		}
		if _, err := session.ResolvePersistedSessionRecord(t.Context(), app.MetadataStore(), id); err != nil {
			t.Fatal(err)
		}
	})
}
