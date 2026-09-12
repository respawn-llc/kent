package runtime

import (
	"encoding/json"
	"reflect"
	"testing"

	"core/server/llm"
	"core/server/tools"
	"core/server/workflow"
	"core/server/workflowruntime"
	"core/shared/config"
	"core/shared/textutil"
	"core/shared/toolspec"
	"core/shared/transcript"
)

func TestWorkflowThinkingTranscriptAfterCompaction(t *testing.T) {
	for _, mode := range []string{"native", "local"} {
		t.Run(mode, func(t *testing.T) {
			completion := func(id string) llm.Response {
				return commentaryResponse("completed", completeNodeCall(id, json.RawMessage(`{"commentary":"completed","summary":"done"}`)))
			}
			client := &fakeCompactionClient{
				caps: llm.ProviderCapabilities{
					ProviderID: "openai", SupportsResponsesAPI: true,
					SupportsResponsesCompact: true, SupportsNativeThinkingUpdates: true,
				},
				responses:           []llm.Response{completion("source"), completion("target")},
				compactionResponses: []llm.CompactionResponse{remoteCompactionReplacement(100, 10, 200000)},
			}
			if mode == "local" {
				client.responses = []llm.Response{completion("source"), finalOutputItemResponse("summary"), completion("target")}
			}
			var delivered []TranscriptCommittedRowFact
			engine := mustNewFakeToolEngine(t, mustCreateTestSession(t), client, Config{
				Model: "gpt-6-astra", ThinkingLevel: "medium", CompactionMode: mode,
				OnEvent: func(event Event) {
					if event.LocalEntry != nil && event.LocalEntry.ThinkingEffort != nil {
						delivered = append(delivered, TranscriptCommittedRowFactsFromEvent(event)...)
					}
				},
			}, toolspec.ToolExecCommand)
			controller := &fakeWorkflowController{engine: engine}
			source, err := engine.BindCurrentNodeExecution(testWorkflowConfig(controller, config.WorkflowCompletionModeTool))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := engine.SubmitWorkflowTurn(t.Context()); err != nil {
				t.Fatal(err)
			}
			if _, err := engine.CompactContextForWorkflowPostCompletion(t.Context()); err != nil {
				t.Fatal(err)
			}
			compactionRequest := client.compactionCalls
			if mode == "local" {
				compactionRequest = client.calls[1:2]
			}
			if len(compactionRequest) != 1 || compactionRequest[0].ReasoningEffort != "medium" {
				t.Fatalf("outgoing compaction did not retain medium: %+v", compactionRequest)
			}
			if err := source.Close(); err != nil {
				t.Fatal(err)
			}
			target := testWorkflowConfig(controller, config.WorkflowCompletionModeTool)
			target.Instructions.CurrentNode.NodeID = "target-node"
			target.Instructions.ContextMode = string(workflow.ContextModeContinueSession)
			assignment, err := NewWorkflowAssignmentSnapshot(WorkflowAssignment{
				ContextMode: workflow.ContextModeContinueSession, CompletionMode: target.CompletionMode,
				Prompt: workflowruntime.PromptContract{
					Identity:       workflowruntime.CurrentNodePromptIdentity(target.Instructions.CurrentNode),
					CompletionMode: target.CompletionMode, Instructions: target.Instructions,
				},
			})
			if err != nil {
				t.Fatal(err)
			}
			steer, err := engine.SteerWorkflowAssignmentSnapshot(assignment.WithThinkingMutation(workflow.SetThinking("high")))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := steer.Wait(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(delivered) != 0 {
				t.Fatal("desired selection created a row before request preparation")
			}
			binding, err := engine.BindCurrentNodeExecution(target)
			if err != nil {
				t.Fatal(err)
			}
			defer binding.Close()
			if _, err := engine.SubmitWorkflowTurn(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(delivered) != 1 || delivered[0].Notice == nil ||
				delivered[0].Notice.ThinkingEffort == nil || *delivered[0].Notice.ThinkingEffort != "high" ||
				delivered[0].Visibility != transcript.EntryVisibilityDetail {
				t.Fatalf("expected one high update, with no intermediate medium update: %+v", delivered)
			}
			request := client.calls[len(client.calls)-1]
			if request.ReasoningEffort != "medium" {
				t.Fatal("target request changed its original provider baseline")
			}
			updates := 0
			for i, item := range request.Items {
				if item.Type == llm.ResponseItemTypeConfigurationUpdate {
					updates++
					if item.ConfigurationEffort == nil || *item.ConfigurationEffort != "high" ||
						i == 0 || request.Items[i-1].MessageType == nil ||
						*request.Items[i-1].MessageType != llm.MessageTypeWorkflowMode {
						t.Fatal("target high update did not follow its assignment")
					}
				}
			}
			if updates != 1 {
				t.Fatalf("target request has %d configuration updates, want one", updates)
			}
			page := mustEngineNewestSegmentPage(t, engine)
			facts := TranscriptCommittedRowFactsFromSnapshot(page.Snapshot)
			found := false
			for i, fact := range facts {
				if fact.Notice != nil && fact.Notice.ThinkingEffort != nil {
					found = true
					if !reflect.DeepEqual(fact, delivered[0]) || i == 0 || i+1 >= len(facts) ||
						facts[i-1].Notice == nil || facts[i-1].Notice.MessageType != llm.MessageTypeWorkflowMode ||
						facts[i+1].Kind != TranscriptCommittedRowFactAssistant {
						t.Fatalf("high update is not between assignment and response, or differs from live delivery: %+v", facts)
					}
				}
			}
			if !found {
				t.Fatal("high update missing from paginated transcript")
			}
		})
	}
}

func TestThinkingSelectionWaitsForCommittedRequest(t *testing.T) {
	var updates []Event
	client := &fakeClient{
		caps:      llm.ProviderCapabilities{ProviderID: "openai", SupportsResponsesAPI: true, SupportsNativeThinkingUpdates: true},
		responses: []llm.Response{finalOutputItemResponse("first"), finalOutputItemResponse("second")},
	}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), client, tools.NewRegistry(), Config{
		Model: "gpt-6-astra", ThinkingLevel: "medium",
		OnEvent: func(event Event) {
			if event.LocalEntry != nil && event.LocalEntry.ThinkingEffort != nil {
				updates = append(updates, event)
			}
		},
	})
	if _, err := engine.SubmitUserMessage(t.Context(), "first"); err != nil {
		t.Fatal(err)
	}
	for _, effort := range []string{"low", "high"} {
		if err := engine.SetThinkingLevel(t.Context(), effort); err != nil {
			t.Fatal(err)
		}
	}
	if len(updates) != 0 {
		t.Fatal("settings clicks created premature transcript rows")
	}
	if _, err := engine.SubmitUserMessage(t.Context(), "second"); err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 || !reflect.DeepEqual(updates[0].LocalEntry.ThinkingEffort, textutil.Value("high")) {
		t.Fatalf("expected one final selected effort: %+v", updates)
	}
}
