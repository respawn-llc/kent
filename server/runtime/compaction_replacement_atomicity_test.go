package runtime

import (
	"encoding/json"
	"reflect"
	"testing"

	"core/server/llm"
	"core/server/session"
	"core/shared/textutil"
	"core/shared/transcript"
)

func TestCompactionReplacementAtomicallyEmbedsReinjectedMetaAndPreservedUserMessage(t *testing.T) {
	t.Parallel()
	for name, placement := range map[string]llm.CompactionContextPlacement{
		"OpenAI checkpoint": llm.CompactionContextBeforeOutput,
		"Grok full bundle":  llm.CompactionContextAfterOutput,
	} {
		t.Run(name, func(t *testing.T) {
			testCompactionReplacementContextPlacement(t, placement)
		})
	}
}

func testCompactionReplacementContextPlacement(t *testing.T, placement llm.CompactionContextPlacement) {
	store, globalConfigDir := mustCreateBaseMetaContextTestSession(t)
	client := &fakeCompactionClient{compactionResponses: []llm.CompactionResponse{
		remoteCompactionReplacement(1_000, 100, 200_000),
	}}
	client.compactionResponses[0].OutputItems[0].Raw = json.RawMessage(`{"type":"compaction","id":"compaction-checkpoint","encrypted_content":"encrypted","provider_extension":{"retained":true}}`)
	checkpoint := llm.CloneResponseItems([]llm.ResponseItem{client.compactionResponses[0].OutputItems[0]})[0]
	client.compactionResponses[0].ContextPlacement = placement
	if placement == llm.CompactionContextAfterOutput {
		bundle := llm.PrepareResponsesInputItems([]llm.ResponseItem{{
			Type: llm.ResponseItemTypeMessage, Role: textutil.Value(llm.RoleUser), Content: textutil.Value("provider-retained input"),
		}})
		bundle = append(bundle, checkpoint)
		bundle = append(bundle, llm.ResponseItem{
			Type: llm.ResponseItemTypeReasoning, EncryptedContent: textutil.Value("opaque-reasoning"),
			Raw: json.RawMessage(`{"type":"reasoning","encrypted_content":"opaque-reasoning","summary":[],"extension":{"retain":true}}`),
		})
		client.compactionResponses[0].OutputItems = bundle
	}
	bundle := llm.CloneResponseItems(client.compactionResponses[0].OutputItems)
	assertReplacement := func(items []llm.ResponseItem) {
		t.Helper()
		if placement == llm.CompactionContextAfterOutput {
			if len(items) < len(bundle) || !reflect.DeepEqual(items[:len(bundle)], bundle) {
				t.Fatalf("provider output bundle changed: got %+v, want prefix %+v", items, bundle)
			}
		} else {
			assertCompactionReplacementOrder(t, items, false)
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

	window, err := mustMaterializeTestEventLog(t, store).ReadRecentRecords(16)
	if err != nil {
		t.Fatalf("read bounded compaction records: %v", err)
	}
	var replacement session.HistoryReplacementRecord
	replacementIndex := -1
	for index, event := range window.Records {
		record, ok := mustSessionEventPayload(event).(session.HistoryReplacementRecord)
		if !ok {
			continue
		}
		if replacementIndex >= 0 {
			t.Fatalf("bounded compaction records contain multiple replacements: %+v", window.Records)
		}
		replacementIndex = index
		replacement = record
	}
	if replacementIndex < 0 {
		t.Fatalf("bounded compaction records contain no history replacement: %+v", window.Records)
	}

	messageTypes := make([]llm.MessageType, 0, len(replacement.Items))
	for _, item := range replacement.Items {
		if item.Type != session.ProviderHistoryItemTypeMessage || item.MessageType == nil {
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
	persistedItems := make([]llm.ResponseItem, 0, len(replacement.Items))
	for _, item := range replacement.Items {
		persistedItems = append(persistedItems, llmResponseItemFromSessionHistory(item))
	}
	assertReplacement(persistedItems)

	for _, event := range window.Records[replacementIndex+1:] {
		message, ok := mustSessionEventPayload(event).(session.MessageRecord)
		if !ok || message.Role != session.MessageRoleDeveloper || message.MessageType == nil {
			continue
		}
		t.Fatalf("replacement followed by typed developer meta record: %+v", event)
	}

	reopenedStore := mustOpenTestSession(t, store.Dir())
	reopenedClient := &fakeCompactionClient{compactionResponses: []llm.CompactionResponse{{
		OutputItems: llm.CloneResponseItems(bundle), ContextPlacement: placement,
	}}}
	reopened := mustNewTestEngine(t, reopenedStore, reopenedClient, newTestToolRegistry(t), Config{
		Model:           "gpt-6-sol",
		GlobalConfigDir: globalConfigDir,
	})
	for range 2 {
		request, err := reopened.buildRequest(t.Context(), "", true)
		if err != nil {
			t.Fatalf("build reopened request: %v", err)
		}
		assertReplacement(request.Items)
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
	if placement == llm.CompactionContextAfterOutput {
		assertReplacement(reopenedClient.compactionCalls[0].Items)
	} else {
		assertCompactionCheckpointUnchanged(t, reopenedClient.compactionCalls[0].Items, checkpoint)
	}
	request, err := reopened.buildRequest(t.Context(), "", true)
	if err != nil {
		t.Fatal(err)
	}
	if placement == llm.CompactionContextAfterOutput {
		assertReplacement(request.Items)
	} else {
		assertCompactionCheckpointUnchanged(t, request.Items, checkpoint)
	}
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
