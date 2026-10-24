package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/server/session/sessiontest"
	"core/server/tools"
	"core/shared/config"
	"core/shared/runtimeids"
	"core/shared/textutil"
	"core/shared/toolspec"
)

func TestNamingPreviewFailureRejectsWithoutPendingInput(t *testing.T) {
	gate := sessiontest.NewPersistenceGate(runtimeTestSessionPersistence)
	store := mustCreateTestSessionAt(t, t.TempDir(), session.WithPersistenceObserver(gate))
	engine := mustNewExecTestEngine(t, store, &fakeClient{}, Config{Model: "gpt-6-sol"})
	gate.FailNext(errors.New("preview persistence failed"))
	if _, err := engine.QueueUserMessage(t.Context(), "Original task"); err == nil {
		t.Fatal("expected preview persistence failure")
	}
	pending, err := engine.PendingWorkSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending.Items) != 0 {
		t.Fatalf("rejected input remains pending: %+v", pending)
	}
}

func TestNamingAfterFullFirstStep(t *testing.T) {
	started := make(chan string, 1)
	release := make(chan struct{})
	submitted := make(chan SessionNamingInput, 2)
	client := &fakeClient{responses: []llm.Response{
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("Investigating totals"), Phase: textutil.Value(llm.MessagePhaseCommentary)},
			ToolCalls: []llm.ToolCall{{ID: "call", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"true"}`)}}},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("Fixed"), Phase: textutil.Value(llm.MessagePhaseFinal)}},
	}}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client,
		newTestToolRegistry(t, tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: concurrentSiblingTool{started: started, release: release}}),
		Config{Model: "gpt-6-sol", SessionNaming: &SessionNamingConfig{MaxInputRunes: 1000, Submit: func(input SessionNamingInput) { submitted <- input }}})
	done := make(chan error, 1)
	go func() { _, err := engine.SubmitUserMessage(context.Background(), "Fix invoice totals"); done <- err }()
	<-started
	select {
	case <-submitted:
		t.Fatal("naming started before the caused tool completed")
	default:
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	input := <-submitted
	if input.Task != "Fix invoice totals" || input.Commentary == nil || *input.Commentary != "Investigating totals" {
		t.Fatalf("first Step input = %+v", input)
	}
	select {
	case <-submitted:
		t.Fatal("later Step resubmitted naming")
	default:
	}
}

func TestNamingSilentToolStep(t *testing.T) {
	submitted := make(chan SessionNamingInput, 1)
	client := &fakeClient{responses: []llm.Response{
		{Assistant: llm.Message{Role: llm.RoleAssistant, Phase: textutil.Value(llm.MessagePhaseCommentary)},
			ToolCalls: []llm.ToolCall{{ID: "call", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"true"}`)}}},
		{Assistant: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("Done"), Phase: textutil.Value(llm.MessagePhaseFinal)}},
	}}
	engine := mustNewFakeToolEngine(t, mustCreateTestSession(t), client,
		Config{Model: "gpt-6-sol", HeadlessMode: true, SessionNaming: &SessionNamingConfig{MaxInputRunes: 1000, Submit: func(input SessionNamingInput) { submitted <- input }}}, toolspec.ToolExecCommand)
	if _, err := engine.SubmitUserMessage(t.Context(), "Fix invoices"); err != nil {
		t.Fatal(err)
	}
	input := <-submitted
	if input.Task != "Fix invoices" || input.Commentary != nil || !input.Headless {
		t.Fatalf("silent first Step = %+v", input)
	}
}

func TestNamingSubagentUsesAssignmentWithoutChangingSteer(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(map[bool]string{false: "direct", true: "queued"}[queued], func(t *testing.T) {
			const assignment = "Fix invoice totals\nReport rounding errors"
			const commentary = "Checking invoice rounding"
			submitted := make(chan SessionNamingInput, 1)
			var events []Event
			client := &fakeClient{responses: []llm.Response{finalTextResponse(commentary)}}
			engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t), Config{
				Model: "gpt-6-sol",
				SessionNaming: &SessionNamingConfig{MaxInputRunes: 1000, Submit: func(input SessionNamingInput) {
					submitted <- input
				}},
				OnEvent: func(event Event) { events = append(events, event) },
			})
			steer := NewAgentSteer(runtimeids.SessionIdentity{ID: runtimeids.NewSessionID()}, assignment)
			var err error
			if queued {
				_, err = engine.QueueAgentSteer(t.Context(), steer, nil)
				waitEngineLifecycleTasks(t, engine)
			} else {
				_, err = engine.SubmitAgentSteerWithHooks(t.Context(), steer, nil, nil)
			}
			if err != nil {
				t.Fatal(err)
			}
			select {
			case input := <-submitted:
				if input.Task != assignment || input.Commentary == nil || *input.Commentary != commentary {
					t.Errorf("naming input = %+v, want raw assignment and first Step commentary", input)
				}
			default:
				t.Fatal("subagent naming was not submitted")
			}
			assertAgentSteerConversationEvent(t, events)
			want := steer.Message()
			assertSteer := func(messages []llm.Message) {
				t.Helper()
				for _, message := range messages {
					if message.MessageType == nil || *message.MessageType != llm.MessageTypeAgentSteer {
						continue
					}
					if message.Role != llm.RoleDeveloper || message.Content == nil ||
						*message.Content != *want.Content || *message.Content == assignment {
						t.Fatalf("agent steer wrapper was changed: %+v", message)
					}
					return
				}
				t.Fatal("typed agent steer is missing")
			}
			if len(client.calls) != 1 {
				t.Fatalf("model calls = %d, want one", len(client.calls))
			}
			assertSteer(requestMessages(client.calls[0]))
			var committed []llm.Message
			for _, event := range events {
				if event.Kind == EventConversationUpdated {
					committed = append(committed, event.Message)
				}
			}
			assertSteer(committed)
		})
	}
}

func TestNamingResumeAndGoalInput(t *testing.T) {
	for _, useGoal := range []bool{false, true} {
		t.Run(map[bool]string{false: "resume preview", true: "initial goal"}[useGoal], func(t *testing.T) {
			submitted := make(chan SessionNamingInput, 1)
			naming := &SessionNamingConfig{MaxInputRunes: 1000, Submit: func(input SessionNamingInput) { submitted <- input }}
			if !useGoal {
				naming.ResumeTask = textutil.Value("Original invoice task")
			}
			client := &fakeClient{responses: []llm.Response{{Assistant: llm.Message{
				Role: llm.RoleAssistant, Content: textutil.Value("Done"), Phase: textutil.Value(llm.MessagePhaseFinal),
			}}}}
			engine := mustNewTestEngine(t, mustCreateTestSession(t), client, newTestToolRegistry(t), Config{Model: "gpt-6-sol", SessionNaming: naming, EnabledTools: []toolspec.ID{toolspec.ToolAskQuestion}})
			select {
			case <-submitted:
				t.Fatal("opening a Runtime started naming")
			default:
			}
			if useGoal {
				if _, err := engine.SetGoal(t.Context(), "Original invoice task", session.GoalActorUser); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := engine.SubmitUserMessage(t.Context(), "continue"); err != nil {
				t.Fatal(err)
			}
			input := <-submitted
			if input.Task != "Original invoice task" {
				t.Fatalf("selected input = %q", input.Task)
			}
		})
	}
}

func TestNamingWorkflowAgentExcludedButSubagentAllowed(t *testing.T) {
	for _, workflowAgent := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary subagent", true: "workflow agent"}[workflowAgent], func(t *testing.T) {
			submitted := make(chan SessionNamingInput, 1)
			settings := Config{Model: "gpt-6-sol", SessionNaming: &SessionNamingConfig{MaxInputRunes: 1000, Submit: func(input SessionNamingInput) { submitted <- input }}}
			client := &fakeClient{responses: []llm.Response{{Assistant: llm.Message{
				Role: llm.RoleAssistant, Content: textutil.Value("Done"), Phase: textutil.Value(llm.MessagePhaseFinal),
			}}}}
			store := mustCreateTestSession(t)
			engine := mustNewTestEngine(t, store, client, newTestToolRegistry(t), settings)
			if workflowAgent {
				client.responses = []llm.Response{commentaryResponse("Complete",
					completeNodeCall("complete", json.RawMessage(`{"commentary":"complete","summary":"done"}`)))}
				publishTestWorkflowExecution(t, engine, testWorkflowConfig(&externallyCompletedWorkflowController{}, config.WorkflowCompletionModeTool))
			}
			steer := NewAgentSteer(runtimeids.SessionIdentity{ID: runtimeids.NewSessionID()}, "Fix invoices")
			if _, err := engine.SubmitAgentSteerWithHooks(t.Context(), steer, nil, nil); err != nil {
				t.Fatal(err)
			}
			select {
			case <-submitted:
				if workflowAgent {
					t.Fatal("Workflow agent was named")
				}
			default:
				if !workflowAgent {
					t.Fatal("ordinary subagent was excluded")
				}
			}
		})
	}
}
