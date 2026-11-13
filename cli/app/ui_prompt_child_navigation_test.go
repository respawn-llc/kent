package app

import (
	"context"
	"testing"

	"core/server/sessionstore"
	"core/shared/apicontract"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	"core/shared/serverapi"
)

type countingPromptMutationClient struct {
	apicontract.ChatMutationService
	submissions int
}

func (c *countingPromptMutationClient) Steer(ctx context.Context, request *chatpb.SteerRequest) (*chatpb.InputMutationSuccess, error) {
	c.submissions++
	return c.ChatMutationService.Steer(ctx, request)
}

func TestPromptCommandFollowsServerSelectedSession(t *testing.T) {
	for _, test := range []struct {
		name        string
		command     string
		established bool
		deleteChild bool
	}{
		{name: "review established", command: "/review", established: true},
		{name: "init established", command: "/init", established: true},
		{name: "review empty", command: "/review"},
		{name: "init empty", command: "/init"},
		{name: "missing child", command: "/review", established: true, deleteChild: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("REMOTE_TEST_KEY", "ready-key")
			fixture := startRemoteAuthTestFixture(t)
			owner := fixture.daemon.Core.MetadataStore()
			binding := mustRegisterAppBinding(t, owner, fixture.config.WorkspaceRoot)
			parent := createAttachedAuthoritativeAppSession(t, owner, binding.ProjectID, fixture.config.WorkspaceRoot)
			if test.established {
				eventLog, err := parent.MaterializeEventLog()
				if err != nil {
					t.Fatal(err)
				}
				text := "earlier conversation"
				_, receipt, err := eventLog.AppendRecord(nil, session.MessageRecord{Role: session.MessageRoleUser, Content: &text})
				if err != nil || !receipt.Committed {
					t.Fatalf("establish parent conversation: receipt=%+v error=%v", receipt, err)
				}
			}
			client := &countingPromptMutationClient{ChatMutationService: fixture.server.SessionUIClients().ChatMutations}
			model := newProjectedStaticUIModel(WithUISessionID(parent.Meta().SessionID))
			model.chatMutations = client
			handled, _, command := model.inputController().handleEnteredSlashCommandInput(test.command, test.command)
			if !handled || command == nil {
				t.Fatal("prompt invocation was not submitted")
			}
			for _, message := range collectCmdMessages(t, command) {
				model = updateUIModel(t, model, message)
			}
			transition := model.Transition()
			if !test.established {
				if transition.Action != UIActionNone || model.currentRuntimeSessionID() != parent.Meta().SessionID {
					t.Fatalf("empty Session unexpectedly navigated: %+v", transition)
				}
				return
			}
			childID := transition.TargetSessionID
			if childID == "" || childID == parent.Meta().SessionID {
				t.Fatalf("prompt did not select a child: transition=%+v", transition)
			}
			child, err := session.ResolvePersistedSessionRecord(t.Context(), owner, childID)
			if err != nil {
				t.Fatalf("resolve selected child: %v", err)
			}
			if child.Meta.PreviousSessionID == nil || child.Meta.PreviousSessionID.String() != parent.Meta().SessionID {
				t.Fatalf("selected child lost parent provenance: %+v", child.Meta)
			}
			t.Logf("accepted prompt: parent=%s child=%s", parent.Meta().SessionID, childID)
			if test.deleteChild {
				if err := owner.DeleteSession(t.Context(), childID); err != nil {
					t.Fatalf("delete selected child before navigation: %v", err)
				}
			}
			released := false
			observation := &runtimeLaunchPlan{stopEventStreams: func() { released = true }}
			directive, err := resolveAndReleaseSessionAction(t.Context(), fixture.server, newHeadlessAuthInteractor(), parent.Meta().SessionID, transition, observation, model)
			if !released {
				t.Fatal("parent observation was not released")
			}
			if client.submissions != 1 {
				t.Fatalf("prompt submitted %d times, want once", client.submissions)
			}
			if test.deleteChild {
				if err == nil {
					t.Fatal("missing selected child unexpectedly opened")
				}
				return
			}
			if err != nil {
				t.Fatalf("open server-selected child: %v", err)
			}
			if selected := requireSessionOpenDestination(t, directive); selected != childID {
				t.Fatalf("opened Session = %s, want selected child %s", selected, childID)
			}
			plan, err := newSessionLaunchPlanner(fixture.server).PlanSession(t.Context(), sessionLaunchRequestFromLifecycleResult(t, directive, serverapi.RunPromptOverrides{}))
			if err != nil {
				t.Fatalf("plan selected child launch: %v", err)
			}
			if plan.SessionID != childID {
				t.Fatalf("launch plan selected %s, want %s", plan.SessionID, childID)
			}
		})
	}
}
