package runtime

import (
	"encoding/json"
	"reflect"
	"testing"

	"core/server/llm"
	"core/server/sessionstore"
	"core/shared/modelcontract"
	"core/shared/textutil"
	"core/shared/transcriptmodel"
)

func TestCompactionOperationContextSurvivesReopen(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		fullBundle bool
	}{{"OpenAI checkpoint", false}, {"Grok full bundle", true}} {
		t.Run(test.name, func(t *testing.T) {
			testCompactionOutputReopen(t, test.fullBundle)
		})
	}
}

func testCompactionOutputReopen(t *testing.T, fullBundle bool) {
	store, globalConfigDir := mustCreateBaseMetaContextTestSession(t)
	client := &fakeCompactionClient{compactionResponses: []llm.CompactionResponse{
		remoteCompactionReplacement(1_000, 100, 200_000),
	}}
	client.compactionResponses[0].OutputItems[0].Raw = json.RawMessage(`{"type":"compaction","id":"compaction-checkpoint","encrypted_content":"encrypted","provider_extension":{"retained":true}}`)
	client.compactionResponses[0].ProviderEvidence.ProviderID = textutil.Value("openai")
	checkpoint := llm.CloneResponseItems([]llm.ResponseItem{client.compactionResponses[0].OutputItems[0]})[0]
	checkpoint.Attribution = &modelcontract.ReasoningAttribution{Type: textutil.Value(modelcontract.ReasoningTypeOpenAI)}
	client.compactionResponses[0].OutputItems[0] = checkpoint
	if fullBundle {
		bundle := llm.PrepareResponsesInputItems([]llm.ResponseItem{{
			Type: llm.ResponseItemTypeMessage, Role: textutil.Value(llm.RoleUser), Content: textutil.Value("provider-retained input"),
		}})
		bundle = append(bundle, checkpoint)
		bundle = append(bundle, llm.ResponseItem{
			Attribution: &modelcontract.ReasoningAttribution{Type: textutil.Value(modelcontract.ReasoningTypeOpenAI)},
			Type:        llm.ResponseItemTypeReasoning, EncryptedContent: textutil.Value("opaque-reasoning"),
			Raw: json.RawMessage(`{"type":"reasoning","encrypted_content":"opaque-reasoning","summary":[],"extension":{"retain":true}}`),
		})
		client.compactionResponses[0].OutputItems = bundle
	}
	bundle := llm.CloneResponseItems(client.compactionResponses[0].OutputItems)
	assertReplacement := func(items []llm.ResponseItem) {
		t.Helper()
		var bundleStart *int
		for index := 0; index+len(bundle) <= len(items); index++ {
			if reflect.DeepEqual(items[index:index+len(bundle)], bundle) {
				bundleStart = textutil.Value(index)
				break
			}
		}
		if bundleStart == nil {
			t.Fatalf("provider output bundle missing or changed: got %+v, want contiguous %+v", items, bundle)
		}
		end := *bundleStart + len(bundle)
		if end >= len(items) || items[end].MessageType == nil || *items[end].MessageType != llm.MessageTypeEnvironment {
			t.Fatal("Environment must follow the complete provider output")
		}
		assertCompactionCheckpointUnchanged(t, items, checkpoint)
	}
	engine := mustNewTestEngine(t, store, client, newTestToolRegistry(t), Config{
		Model:           "gpt-6-sol",
		GlobalConfigDir: globalConfigDir,
	})
	if _, err := engine.SetGoal(t.Context(), "goal", session.GoalActorUser); err != nil {
		t.Fatalf("set active goal: %v", err)
	}
	if err := engine.drainRuntimeOperations(t.Context()); err != nil {
		t.Fatalf("persist active goal notice: %v", err)
	}
	mustSetWorktreeReminderState(t, store, testWorktreeReminderState(
		session.WorktreeReminderModeEnter,
		"feature/goal",
		t.TempDir(),
		t.TempDir(),
		t.TempDir(),
	))
	if err := steerTestActiveStep(engine, "input", steerMessagesWithPersistenceIntent(steeringPriorityNormal, steeringMessageEventNone, true, []llm.Message{{Role: llm.RoleUser, Content: textutil.Value("input")}})); err != nil {
		t.Fatalf("persist compaction input: %v", err)
	}

	scheduleManualCompactionAndWait(t, engine)
	request := buildActiveTurnRequestForTest(t, engine, nil, true)
	messageTypes := make([]llm.MessageType, 0, len(request.Items))
	for _, item := range request.Items {
		if item.Type != llm.ResponseItemTypeMessage || item.MessageType == nil {
			continue
		}
		messageTypes = append(messageTypes, llm.MessageType(*item.MessageType))
	}
	assertOrderedReplacementMessageTypes(t, messageTypes, []llm.MessageType{
		llm.MessageTypeSkills,
		llm.MessageTypeWorktreeMode,
		llm.MessageTypeAgentsMD,
		llm.MessageTypeActiveGoalContinuation,
		llm.MessageTypeEnvironment,
		llm.MessageTypeCompactionPreservedUserMessage,
	})
	assertReplacement(request.Items)

	reopenedStore := mustOpenTestSession(t, store.Dir())
	reopenedClient := &fakeCompactionClient{compactionResponses: []llm.CompactionResponse{{
		ProviderEvidence: modelcontract.ProviderUsageEvidence{ProviderID: textutil.Value("openai")},
		OutputItems:      llm.CloneResponseItems(bundle),
	}}}
	reopened := mustNewTestEngine(t, reopenedStore, reopenedClient, newTestToolRegistry(t), Config{
		Model:           "gpt-6-sol",
		GlobalConfigDir: globalConfigDir,
	})
	for range 2 {
		restored := buildActiveTurnRequestForTest(t, reopened, nil, true)
		assertCompactionRequestItemsEqual(t, request.Items, restored.Items)
		assertReplacement(restored.Items)
	}

	page, err := TranscriptNewestSegmentPageFromEventLog(mustMaterializeTestEventLog(t, reopenedStore), "")
	if err != nil {
		t.Fatalf("project persisted replacement: %v", err)
	}
	notices := 0
	for _, entry := range page.Snapshot.Entries {
		if entry.Role != string(transcript.EntryRoleDeveloperContext) || entry.MessageType != "" {
			continue
		}
		notices++
		if entry.Visibility != transcript.EntryVisibilityOngoing {
			t.Fatalf("native reminder must use regular developer-context visibility: %+v", entry)
		}
	}
	if notices != 1 {
		t.Fatalf("persisted native reminder notices = %d, want one", notices)
	}
	scheduleManualCompactionAndWait(t, reopened)
	if len(reopenedClient.compactionCalls) != 1 {
		t.Fatalf("recompaction requests = %d", len(reopenedClient.compactionCalls))
	}
	assertReplacement(reopenedClient.compactionCalls[0].Items)
	next := buildActiveTurnRequestForTest(t, reopened, nil, true)
	assertReplacement(next.Items)
}

func assertCompactionCheckpointUnchanged(t *testing.T, items []llm.ResponseItem, want llm.ResponseItem) {
	t.Helper()
	count := 0
	for _, item := range items {
		if item.Type != llm.ResponseItemTypeCompaction {
			continue
		}
		count++
		if !reflect.DeepEqual(item, want) {
			t.Fatalf("encrypted checkpoint changed: got %+v, want %+v", item, want)
		}
	}
	if count != 1 {
		t.Fatalf("encrypted checkpoints = %d, want one", count)
	}
}

func assertOrderedReplacementMessageTypes(
	t *testing.T,
	messageTypes []llm.MessageType,
	want []llm.MessageType,
) {
	t.Helper()
	next := 0
	for _, messageType := range messageTypes {
		if next < len(want) && messageType == want[next] {
			next++
		}
	}
	if next != len(want) {
		t.Fatalf("replacement message types = %+v, want ordered subsequence %+v", messageTypes, want)
	}
	if len(messageTypes) == 0 || messageTypes[len(messageTypes)-1] != llm.MessageTypeCompactionPreservedUserMessage {
		t.Fatalf("replacement message types = %+v, want carryover last", messageTypes)
	}
}
