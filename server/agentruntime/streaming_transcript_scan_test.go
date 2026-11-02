package runtime

import (
	"encoding/json"
	"reflect"
	"testing"

	"core/server/llm"
	"core/server/sessionstore"
	"core/shared/config"
	"core/shared/textutil"
	"core/shared/toolspec"
	"core/shared/transcriptmodel"
	patchformat "core/shared/transcriptmodel/patchformat"
)

func streamScanTestEvent(t *testing.T, kind string, payload any) session.EventRecord {
	return streamScanTestEventAtSequence(t, 1, kind, payload)
}

func streamScanTestEventAtSequence(t *testing.T, sequence int64, kind string, payload any) session.EventRecord {
	t.Helper()
	var record session.EventRecordPayload
	switch kind {
	case "message":
		message, ok := payload.(llm.Message)
		if !ok {
			t.Fatalf("message payload has type %T", payload)
		}
		adapted, err := sessionMessageRecordFromLLM(message)
		if err != nil {
			t.Fatalf("adapt message: %v", err)
		}
		record = adapted
	case "tool_completed":
		completion, ok := payload.(storedToolCompletion)
		if !ok {
			t.Fatalf("tool completion payload has type %T", payload)
		}
		record = session.ToolCompletionRecord{
			CallID:        completion.CallID,
			Name:          completion.Name,
			OutputKind:    session.ToolOutputKindFunction,
			IsError:       completion.IsError,
			Output:        completion.Output,
			Summary:       textutil.Pointer(completion.Summary),
			CondensedText: textutil.Pointer(completion.CondensedText),
		}
		if completion.Presentation != nil {
			typed := record.(session.ToolCompletionRecord)
			typed.Presentation = transcript.EncodeToolCallMeta(*completion.Presentation)
			record = typed
		}
	case "local_entry":
		entry, ok := payload.(storedLocalEntry)
		if !ok {
			t.Fatalf("local entry payload has type %T", payload)
		}
		adapted, err := sessionLocalEntryRecordFromRuntime(entry)
		if err != nil {
			t.Fatalf("adapt local entry: %v", err)
		}
		record = adapted
	case sessionEventCacheWarning:
		warning, ok := payload.(transcript.CacheWarning)
		if !ok {
			t.Fatalf("cache warning payload has type %T", payload)
		}
		if warning.Scope == "" {
			warning.Scope = transcript.CacheWarningScopeConversation
		}
		if warning.Reason == "" {
			warning.Reason = transcript.CacheWarningReasonNonPostfix
		}
		adapted, err := sessionCacheWarningRecordFromRuntime(warning)
		if err != nil {
			t.Fatalf("adapt cache warning: %v", err)
		}
		record = adapted
	case "history_replaced":
		replacement, ok := payload.(historyReplacementPayload)
		if !ok {
			t.Fatalf("history replacement payload has type %T", payload)
		}
		if replacement.Engine == "" || replacement.Engine == "compaction" {
			replacement.Engine = "local"
		}
		if replacement.Mode == "" {
			replacement.Mode = string(compactionModeAuto)
		}
		adapted, err := sessionHistoryReplacementRecordFromRuntime(replacement)
		if err != nil {
			t.Fatalf("adapt history replacement: %v", err)
		}
		record = adapted
	default:
		t.Fatalf("unsupported persisted test event kind %q", kind)
	}
	event, err := session.NewEventRecord(sequence, nil, record)
	if err != nil {
		t.Fatalf("create %s record: %v", kind, err)
	}
	return event
}

func streamScanRepresentativeEvents(t *testing.T) []session.EventRecord {
	t.Helper()
	return []session.EventRecord{
		streamScanTestEvent(t, "message", llm.Message{Role: llm.RoleUser, Content: textutil.Value("task one")}),
		streamScanTestEvent(t, "message", llm.Message{Role: llm.RoleAssistant, Phase: textutil.Value(llm.MessagePhaseFinal), Content: textutil.Value("answer one")}),
		streamScanTestEvent(t, "message", llm.Message{Role: llm.RoleUser, Content: textutil.Value("task two")}),
		streamScanTestEvent(t, "message", llm.Message{
			Role:    llm.RoleAssistant,
			Phase:   textutil.Value(llm.MessagePhaseCommentary),
			Content: textutil.Value("running one tool"),
			ToolCalls: []llm.ToolCall{
				{ID: "call-1", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"command":"ls"}`)},
			},
		}),
		streamScanTestEvent(t, "tool_completed", storedToolCompletion{CallID: "call-1", Name: string(toolspec.ToolExecCommand), Output: json.RawMessage(`{"output":"files"}`)}),
		streamScanTestEvent(t, "message", llm.Message{Role: llm.RoleTool, ToolCallID: textutil.Value("call-1"), Name: textutil.Value(string(toolspec.ToolExecCommand)), Content: textutil.Value(`{"output":"files"}`)}),
		streamScanTestEvent(t, "message", llm.Message{Role: llm.RoleAssistant, Phase: textutil.Value(llm.MessagePhaseFinal), Content: textutil.Value("done two")}),
		streamScanTestEvent(t, "local_entry", storedLocalEntry{Visibility: transcript.EntryVisibilityAuto, Role: "system", Text: "a local note"}),
		streamScanTestEvent(t, sessionEventCacheWarning, transcript.CacheWarning{}),
		streamScanTestEvent(t, "history_replaced", historyReplacementPayload{
			Engine: "compaction",
			Items: llm.ItemsFromMessages([]llm.Message{
				{Role: llm.RoleUser, MessageType: textutil.Value(llm.MessageTypeCompactionSummary), Content: textutil.Value("summary so far")},
			}),
		}),
		streamScanTestEvent(t, "message", llm.Message{Role: llm.RoleUser, Content: textutil.Value("task three")}),
		streamScanTestEvent(t, "message", llm.Message{
			Role:    llm.RoleAssistant,
			Phase:   textutil.Value(llm.MessagePhaseCommentary),
			Content: textutil.Value("running two tools"),
			ToolCalls: []llm.ToolCall{
				{ID: "call-2", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"command":"a"}`)},
				{ID: "call-3", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"command":"b"}`)},
			},
		}),
		streamScanTestEvent(t, "tool_completed", storedToolCompletion{CallID: "call-2", Name: string(toolspec.ToolExecCommand), Output: json.RawMessage(`{"output":"a-out"}`)}),
		streamScanTestEvent(t, "tool_completed", storedToolCompletion{CallID: "call-3", Name: string(toolspec.ToolExecCommand), Output: json.RawMessage(`{"output":"b-out"}`)}),
		streamScanTestEvent(t, "message", llm.Message{Role: llm.RoleTool, ToolCallID: textutil.Value("call-2"), Name: textutil.Value(string(toolspec.ToolExecCommand)), Content: textutil.Value(`{"output":"a-out"}`)}),
		streamScanTestEvent(t, "message", llm.Message{Role: llm.RoleTool, ToolCallID: textutil.Value("call-3"), Name: textutil.Value(string(toolspec.ToolExecCommand)), Content: textutil.Value(`{"output":"b-out"}`)}),
		streamScanTestEvent(t, "message", llm.Message{Role: llm.RoleAssistant, Phase: textutil.Value(llm.MessagePhaseFinal), Content: textutil.Value("final answer")}),
	}
}

func TestStreamingTranscriptScanSeedsLastFinalAnswerFromCompactionBoundary(t *testing.T) {
	t.Parallel()
	events := []session.EventRecord{
		streamScanTestEvent(t, "history_replaced", historyReplacementPayload{
			Engine:                            "compaction",
			LastCommittedAssistantFinalAnswer: textutil.Value("retained final answer"),
			Items: llm.ItemsFromMessages([]llm.Message{
				{Role: llm.RoleUser, MessageType: textutil.Value(llm.MessageTypeCompactionSummary), Content: textutil.Value("summary so far")},
			}),
		}),
	}
	scan := newStreamingTranscriptScan(config.CacheWarningModeDefault)
	applyEventsToStreaming(t, scan, events)
	if got, want := scan.LastCommittedAssistantFinalAnswer(), "retained final answer"; got == nil || *got != want {
		t.Fatalf("scan last final answer = %v, want boundary-seeded %q", got, want)
	}
}

func TestStreamingTranscriptScanProjectsLegacyCompactionWithoutNumber(t *testing.T) {
	t.Parallel()
	events := []session.EventRecord{
		streamScanTestEvent(t, "history_replaced", historyReplacementPayload{
			Items: llm.ItemsFromMessages([]llm.Message{
				{Role: llm.RoleUser, Content: textutil.Value("first preserved prompt")},
				{Role: llm.RoleUser, Content: textutil.Value("second preserved prompt")},
				{
					Role:        llm.RoleDeveloper,
					MessageType: textutil.Value(llm.MessageTypeEnvironment),
					Content:     textutil.Value("current environment"),
				},
			}),
		}),
	}
	scan := newStreamingTranscriptScan(config.CacheWarningModeDefault)
	applyEventsToStreaming(t, scan, events)
	entries := scan.PageSnapshot().Entries
	if len(entries) != 4 {
		t.Fatalf("projected entries = %+v, want summary, two preserved messages, and environment", entries)
	}
	wantRoles := []string{
		string(transcript.EntryRoleCompactionSummary),
		string(transcript.EntryRoleCompactionPreservedUserMessage),
		string(transcript.EntryRoleCompactionPreservedUserMessage),
		string(transcript.EntryRoleDeveloperContext),
	}
	wantVisibility := []transcript.EntryVisibility{
		transcript.EntryVisibilityOngoing,
		transcript.EntryVisibilityDetail,
		transcript.EntryVisibilityDetail,
		transcript.EntryVisibilityDetail,
	}
	for index := range entries {
		if entries[index].Role != wantRoles[index] ||
			entries[index].Visibility != wantVisibility[index] {
			t.Fatalf(
				"projected entry %d = %+v, want role %q visibility %q",
				index,
				entries[index],
				wantRoles[index],
				wantVisibility[index],
			)
		}
	}
	if entries[1].Text != "first preserved prompt" ||
		entries[2].Text != "second preserved prompt" {
		t.Fatalf("preserved message order/content = %+v", entries)
	}
}

func TestStreamingTranscriptScanBoundarySeedOverriddenByLaterFinalAnswer(t *testing.T) {
	t.Parallel()
	events := []session.EventRecord{
		streamScanTestEvent(t, "history_replaced", historyReplacementPayload{
			Engine:                            "compaction",
			LastCommittedAssistantFinalAnswer: textutil.Value("stale boundary answer"),
			Items: llm.ItemsFromMessages([]llm.Message{
				{Role: llm.RoleUser, MessageType: textutil.Value(llm.MessageTypeCompactionSummary), Content: textutil.Value("summary so far")},
			}),
		}),
		streamScanTestEvent(t, "message", llm.Message{Role: llm.RoleAssistant, Phase: textutil.Value(llm.MessagePhaseFinal), Content: textutil.Value("newer final answer")}),
	}
	scan := newStreamingTranscriptScan(config.CacheWarningModeDefault)
	applyEventsToStreaming(t, scan, events)
	if got, want := scan.LastCommittedAssistantFinalAnswer(), "newer final answer"; got == nil || *got != want {
		t.Fatalf("scan last final answer = %v, want later final %q", got, want)
	}
}

func TestStreamingTranscriptScanKeepsToolAttachedLocalEntryAfterMaterializedOutput(t *testing.T) {
	t.Parallel()
	fallbackCallID := "call-fallback"
	ordinaryCallID := "call-ordinary"
	fallbackPatchPresentation := patchformat.InvalidInputPresentation("presentation fallback")
	fallbackPresentation := transcript.ToolCallMeta{
		ToolName:          string(toolspec.ToolPatch),
		PatchPresentation: &fallbackPatchPresentation,
	}
	ordinaryPresentation := transcript.ToolCallMeta{
		ToolName: string(toolspec.ToolExecCommand),
	}
	expectedFallbackPresentation := transcript.NormalizeToolCallMeta(fallbackPresentation)
	events := []session.EventRecord{
		streamScanTestEvent(t, "message", llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{
				{
					ID:   fallbackCallID,
					Name: string(toolspec.ToolPatch),
				},
				{
					ID:   ordinaryCallID,
					Name: string(toolspec.ToolExecCommand),
				},
			},
		}),
		streamScanTestEvent(t, "tool_completed", storedToolCompletion{
			CallID:       fallbackCallID,
			Name:         string(toolspec.ToolPatch),
			Output:       json.RawMessage(`{"ok":true}`),
			Presentation: &fallbackPresentation,
		}),
		streamScanTestEvent(t, "local_entry", storedLocalEntry{
			Role:            string(transcript.EntryRoleDeveloperErrorFeedback),
			Text:            "presentation fallback",
			AfterToolCallID: &fallbackCallID,
		}),
		streamScanTestEvent(t, "tool_completed", storedToolCompletion{
			CallID:       ordinaryCallID,
			Name:         string(toolspec.ToolExecCommand),
			Output:       json.RawMessage(`{"output":"done"}`),
			Presentation: &ordinaryPresentation,
		}),
		streamScanTestEvent(t, "message", llm.Message{
			Role:        llm.RoleTool,
			ToolCallID:  textutil.Value(fallbackCallID),
			Name:        textutil.Value(string(toolspec.ToolPatch)),
			Content:     textutil.Value(`{"ok":true}`),
			MessageType: llm.ToolOutputMessageType(true),
		}),
		streamScanTestEvent(t, "message", llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: textutil.Value(ordinaryCallID),
			Name:       textutil.Value(string(toolspec.ToolExecCommand)),
			Content:    textutil.Value(`{"output":"done"}`),
		}),
		streamScanTestEvent(t, "message", llm.Message{
			Role:    llm.RoleAssistant,
			Phase:   textutil.Value(llm.MessagePhaseFinal),
			Content: textutil.Value("done"),
		}),
	}

	snapshot := fullStreamingProjection(t, events)
	if got := len(snapshot.Entries); got != 6 {
		t.Fatalf("entry count = %d, want two tool calls, two tool results, one operator row, and one assistant row: %+v", got, snapshot.Entries)
	}
	if got := snapshot.Entries[0]; got.Role != "tool_call" || got.ToolCallID != fallbackCallID {
		t.Fatalf("entry[0] = %+v, want fallback tool call row", got)
	}
	if got := snapshot.Entries[1]; got.Role != "tool_call" || got.ToolCallID != ordinaryCallID {
		t.Fatalf("entry[1] = %+v, want ordinary tool call row", got)
	}
	if got := snapshot.Entries[2]; got.Role != "tool_result_ok" || got.ToolCallID != fallbackCallID || got.ToolCall == nil || !reflect.DeepEqual(*got.ToolCall, expectedFallbackPresentation) {
		t.Fatalf("entry[2] = %+v, want one finalized fallback tool result row", got)
	}
	if got := snapshot.Entries[3]; got.Role != string(transcript.EntryRoleDeveloperErrorFeedback) {
		t.Fatalf("entry[3] = %+v, want operator feedback immediately after its tool output", got)
	}
	if got := snapshot.Entries[4]; got.Role != "tool_result_ok" || got.ToolCallID != ordinaryCallID {
		t.Fatalf("entry[4] = %+v, want later ordinary tool result row", got)
	}
	if got := snapshot.Entries[5]; got.Role != "assistant" {
		t.Fatalf("entry[5] = %+v, want following assistant row", got)
	}
}

func applyEventsToStreaming(t *testing.T, scan *streamingTranscriptScan, events []session.EventRecord) {
	t.Helper()
	for _, evt := range events {
		if err := scan.ApplyPersistedEvent(evt); err != nil {
			t.Fatalf("streaming apply %s: %v", mustSessionEventKind(evt), err)
		}
	}
}

func fullStreamingProjection(t *testing.T, events []session.EventRecord) ChatSnapshot {
	t.Helper()
	scan := newStreamingTranscriptScan(config.CacheWarningModeDefault)
	applyEventsToStreaming(t, scan, events)
	return scan.PageSnapshot()
}

func TestStreamingTranscriptScanIsolatesInputAndSnapshots(t *testing.T) {
	events := []session.EventRecord{
		streamScanTestEventAtSequence(t, 1, "message", llm.Message{
			Role: llm.RoleUser, Content: textutil.Value("task"),
		}),
		streamScanTestEventAtSequence(t, 2, "message", llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{
				{ID: "call-1", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"pwd"}`)},
			},
		}),
		streamScanTestEventAtSequence(t, 3, "tool_completed", storedToolCompletion{
			CallID: "call-1", Name: string(toolspec.ToolExecCommand),
			Output: json.RawMessage(`{"output":"workspace"}`),
			Presentation: &transcript.ToolCallMeta{
				ToolName:    string(toolspec.ToolExecCommand),
				Suggestions: []string{"inspect workspace"},
			},
		}),
		streamScanTestEventAtSequence(t, 4, "message", llm.Message{
			Role: llm.RoleAssistant, Phase: textutil.Value(llm.MessagePhaseFinal),
			Content: textutil.Value("finished"),
		}),
	}
	want := fullStreamingProjection(t, events)
	scan := newStreamingTranscriptScan(config.CacheWarningModeDefault)
	applyEventsToStreaming(t, scan, events)
	page := scan.PageSnapshot()
	for index := range page.Entries {
		entry := &page.Entries[index]
		entry.Text = "changed"
		if entry.RollbackTargetID != nil {
			*entry.RollbackTargetID = "changed"
		}
		if entry.CommittedProvenance != nil {
			entry.CommittedProvenance.EventSequence = 999
		}
		if entry.ToolCall != nil && len(entry.ToolCall.Suggestions) > 0 {
			entry.ToolCall.Suggestions[0] = "changed"
		}
	}
	finalAnswer := scan.LastCommittedAssistantFinalAnswer()
	if finalAnswer == nil {
		t.Fatal("missing final answer")
	}
	*finalAnswer = "changed"
	if got := scan.PageSnapshot(); !reflect.DeepEqual(got, want) {
		t.Fatalf("mutating a page changed the retained projection: got %+v, want %+v", got, want)
	}
	if got := scan.LastCommittedAssistantFinalAnswer(); got == nil || *got != "finished" {
		t.Fatalf("mutating the final answer changed the retained answer: %v", got)
	}
	if got := fullStreamingProjection(t, events); !reflect.DeepEqual(got, want) {
		t.Fatalf("mutating a page changed the input records: got %+v, want %+v", got, want)
	}
}

func TestStreamingTranscriptScanOrdersMaterializedToolsByCompletionProvenance(t *testing.T) {
	t.Parallel()
	events := []session.EventRecord{
		streamScanTestEventAtSequence(t, 1, "message", llm.Message{
			Role: llm.RoleAssistant,
			ToolCalls: []llm.ToolCall{
				{ID: "call-a", Name: string(toolspec.ToolExecCommand)},
				{ID: "call-b", Name: string(toolspec.ToolExecCommand)},
			},
		}),
		streamScanTestEventAtSequence(t, 2, "tool_completed", storedToolCompletion{
			CallID: "call-b",
			Name:   string(toolspec.ToolExecCommand),
			Output: json.RawMessage(`{"output":"b"}`),
		}),
		streamScanTestEventAtSequence(t, 3, "tool_completed", storedToolCompletion{
			CallID: "call-a",
			Name:   string(toolspec.ToolExecCommand),
			Output: json.RawMessage(`{"output":"a"}`),
		}),
		streamScanTestEventAtSequence(t, 4, "message", llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: textutil.Value("call-a"),
			Name:       textutil.Value(string(toolspec.ToolExecCommand)),
			Content:    textutil.Value(`{"output":"a"}`),
		}),
		streamScanTestEventAtSequence(t, 5, "message", llm.Message{
			Role:       llm.RoleTool,
			ToolCallID: textutil.Value("call-b"),
			Name:       textutil.Value(string(toolspec.ToolExecCommand)),
			Content:    textutil.Value(`{"output":"b"}`),
		}),
		streamScanTestEventAtSequence(t, 6, "message", llm.Message{
			Role:    llm.RoleAssistant,
			Phase:   textutil.Value(llm.MessagePhaseFinal),
			Content: textutil.Value("done"),
		}),
	}

	scan := newStreamingTranscriptScan(config.CacheWarningModeDefault)
	applyEventsToStreaming(t, scan, events)
	page := scan.PageSnapshot()
	if len(page.Entries) != 5 {
		t.Fatalf("projected entries = %+v, want two tool calls, two results, and assistant answer", page.Entries)
	}
	for _, test := range []struct {
		index  int
		callID string
		seq    int64
	}{
		{index: 2, callID: "call-b", seq: 2},
		{index: 3, callID: "call-a", seq: 3},
	} {
		entry := page.Entries[test.index]
		if entry.Role != "tool_result_ok" || entry.ToolCallID != test.callID {
			t.Fatalf("entry %d = %+v, want tool result for %s", test.index, entry, test.callID)
		}
		if entry.CommittedProvenance == nil || entry.CommittedProvenance.EventSequence != test.seq {
			t.Fatalf(
				"entry %d provenance = %+v, want event sequence %d",
				test.index,
				entry.CommittedProvenance,
				test.seq,
			)
		}
	}
}

func TestStreamingTranscriptScanMetadata(t *testing.T) {
	t.Parallel()
	events := streamScanRepresentativeEvents(t)
	scan := newStreamingTranscriptScan(config.CacheWarningModeDefault)
	applyEventsToStreaming(t, scan, events)

	if got, want := scan.LastCommittedAssistantFinalAnswer(), "final answer"; got == nil || *got != want {
		t.Fatalf("last committed final answer: got %v want %q", got, want)
	}
}
