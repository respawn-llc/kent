package runtimeview

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"core/internal/testharness/runtimewirefixture"
	"core/internal/testharness/scriptedllm"
	"core/internal/testharness/toolfixture"
	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/server/tools"
	patchtool "core/server/tools/patch"
	shelltool "core/server/tools/shell"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/rollbacktarget"
	"core/shared/textutil"
	"core/shared/toolspec"
	"core/shared/transcript"
	"google.golang.org/protobuf/proto"
)

func durableParityRow(row *transcriptpb.CommittedRow) *transcriptpb.CommittedRow {
	// Live correlation IDs reconcile provisional items, not persisted facts.
	// TailSegment explicitly prohibits provisional reasoning IDs.
	durable := proto.Clone(row).(*transcriptpb.CommittedRow)
	if assistant := durable.GetAssistant(); assistant != nil {
		assistant.StreamId = nil
	}
	if reasoning := durable.GetReasoningTrace(); reasoning != nil {
		reasoning.ProvisionalIdentity = nil
	}
	return durable
}

func assertTranscriptRowOverlap(t *testing.T, reference, candidate []*transcriptpb.CommittedRow) int {
	t.Helper()
	rows := make(map[transcript.CommittedRowLocator]*transcriptpb.CommittedRow, len(reference))
	for _, row := range reference {
		if err := protoapi.Validate(row); err != nil {
			t.Fatalf("validate reference row: %v", err)
		}
		locator := transcript.CommittedRowLocator{
			EventSequence: row.Locator.EventSequence,
			RowOrdinal:    int64(row.Locator.RowOrdinal),
		}
		rows[locator] = row
	}
	overlap := 0
	for _, row := range candidate {
		if err := protoapi.Validate(row); err != nil {
			t.Fatalf("validate candidate row: %v", err)
		}
		locator := transcript.CommittedRowLocator{
			EventSequence: row.Locator.EventSequence,
			RowOrdinal:    int64(row.Locator.RowOrdinal),
		}
		previous, exists := rows[locator]
		if !exists {
			continue
		}
		overlap++
		if !proto.Equal(durableParityRow(previous), durableParityRow(row)) {
			t.Errorf("recurrent locator %+v has different durable payloads:\nreference: %v\ncandidate: %v", locator, previous, row)
		}
	}
	return overlap
}

func assertPersistedLiveTranscriptParity(t *testing.T, store *session.Store, events []runtime.Event) {
	t.Helper()
	eventLog, err := store.MaterializeEventLog()
	if err != nil {
		t.Fatalf("materialize event log: %v", err)
	}
	page, hydration := mustProjectNewestPersistedTranscript(t, store, eventLog)
	liveRows := make(map[transcript.CommittedRowLocator]*transcriptpb.CommittedRow)
	for _, event := range events {
		messages, err := TranscriptMessagesFromRuntimeEventChecked(event)
		if err != nil {
			t.Fatalf("project actual live event %q: %v", event.Kind, err)
		}
		for _, message := range messages {
			row := message.GetCommittedRow()
			if row == nil {
				continue
			}
			if err := protoapi.Validate(row); err != nil {
				t.Fatalf("validate actual live row: %v", err)
			}
			locator := transcript.CommittedRowLocator{
				EventSequence: row.Locator.EventSequence,
				RowOrdinal:    int64(row.Locator.RowOrdinal),
			}
			if previous, exists := liveRows[locator]; exists && !proto.Equal(previous, row) {
				t.Errorf("live deliveries disagree at %+v:\nfirst: %v\nnext: %v", locator, previous, row)
			}
			liveRows[locator] = row
		}
	}
	if len(liveRows) == 0 || len(page.Entries) == 0 {
		t.Fatalf("expected committed rows: live=%d page=%d", len(liveRows), len(page.Entries))
	}
	if len(liveRows) != len(page.Entries) {
		t.Errorf("live and bounded persisted row counts differ: live=%d page=%d", len(liveRows), len(page.Entries))
	}
	for index, persisted := range page.Entries {
		locator := transcript.CommittedRowLocator{
			EventSequence: persisted.Locator.EventSequence,
			RowOrdinal:    int64(persisted.Locator.RowOrdinal),
		}
		live, exists := liveRows[locator]
		if !exists {
			t.Errorf("persisted row has no actual live delivery at %+v: %v", locator, persisted)
			continue
		}
		if !proto.Equal(durableParityRow(live), persisted) {
			t.Errorf("live row differs from bounded persisted page at %+v:\nlive: %v\npage: %v", locator, live, persisted)
		}
		if !proto.Equal(persisted, hydration.TailSegment.Entries[index]) {
			t.Errorf("bounded page differs from hydration at %+v", locator)
		}
	}
}

func TestPersistedTranscriptMatchesActualEngineLiveDelivery(t *testing.T) {
	part := int64(0)
	output := int64(0)
	coordinate := &llm.ReasoningSourceCoordinate{OutputIndex: &output, PartIndex: &part}
	identity := &llm.ReasoningItemIdentity{ItemID: "reason_1", PartIndex: &part}
	for _, fixture := range []struct {
		name                  string
		script                scriptedllm.Script
		wantAssistantStream   bool
		wantReasoningIdentity bool
	}{
		{
			name:                  "correlated reasoning local",
			wantReasoningIdentity: true,
			script: scriptedllm.Script{Steps: []scriptedllm.Step{{
				Response: llm.Response{
					Assistant: scriptedllm.FinalAnswer("completed").Response.Assistant,
					Reasoning: []llm.ReasoningEntry{{Text: "Planning", SourceCoordinate: coordinate, ItemIdentity: identity}},
					Usage:     scriptedllm.FinalAnswer("completed").Response.Usage,
				},
				ReasoningDeltas: []llm.ReasoningSummaryDelta{{Text: "Planning", SourceCoordinate: coordinate, ItemIdentity: identity}},
			}}},
		},
		{
			name:   "assistant",
			script: scriptedllm.Script{Steps: []scriptedllm.Step{scriptedllm.FinalAnswer("completed")}},
		},
		{
			name:                "streamed assistant",
			wantAssistantStream: true,
			script: scriptedllm.Script{Steps: []scriptedllm.Step{{
				Response:     scriptedllm.FinalAnswer("completed").Response,
				StreamDeltas: []llm.AssistantDelta{{Text: "completed", Phase: llm.MessagePhaseFinal}},
			}}},
		},
		{
			name: "reasoning local",
			script: scriptedllm.Script{Steps: []scriptedllm.Step{{
				Response: llm.Response{
					Assistant: scriptedllm.FinalAnswer("completed").Response.Assistant,
					Reasoning: []llm.ReasoningEntry{{Text: "**Planning\nDetails**"}},
					Usage:     scriptedllm.FinalAnswer("completed").Response.Usage,
				},
			}}},
		},
		{
			name: "tool completion",
			script: scriptedllm.Script{Steps: []scriptedllm.Step{
				scriptedllm.ToolBatch("checking", llm.ToolCall{ID: "call-unknown", Name: "unknown_tool", Input: json.RawMessage(`{}`)}),
				{
					ExpectedToolResults: []scriptedllm.ExpectedToolResult{{CallID: "call-unknown", Name: "unknown_tool"}},
					Response:            scriptedllm.FinalAnswer("completed").Response,
				},
			}},
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			store := newRuntimeViewStore(t)
			var events []runtime.Event
			engine := newRuntimeViewEngine(t, store, scriptedllm.NewClient(fixture.script), runtime.Config{
				Model: "gpt-6-sol",
				OnEvent: func(event runtime.Event) {
					if event.CommittedTranscriptChanged {
						events = append(events, event)
					}
				},
			})
			if _, err := engine.SubmitUserMessage(t.Context(), "exercise live and persisted parity"); err != nil {
				t.Fatalf("submit user message: %v", err)
			}
			hasAssistantStream := false
			hasReasoningIdentity := false
			for _, event := range events {
				hasAssistantStream = hasAssistantStream || event.AssistantTranscriptStreamID != nil
				hasReasoningIdentity = hasReasoningIdentity || event.ReasoningTraceIdentity != nil
			}
			if hasAssistantStream != fixture.wantAssistantStream || hasReasoningIdentity != fixture.wantReasoningIdentity {
				t.Fatalf("live correlation identities: assistant=%t reasoning=%t, want assistant=%t reasoning=%t",
					hasAssistantStream, hasReasoningIdentity, fixture.wantAssistantStream, fixture.wantReasoningIdentity)
			}
			assertPersistedLiveTranscriptParity(t, store, events)
		})
	}
}

func TestPersistedWebSearchTranscriptMatchesActualEngineLiveDelivery(t *testing.T) {
	caps := scriptedllm.DefaultProviderCapabilities()
	caps.SupportsNativeWebSearch = true
	for _, fixture := range []struct {
		name string
		raw  json.RawMessage
	}{
		{
			name: "completed",
			raw:  json.RawMessage(`{"type":"web_search_call","id":"ws_1","status":"completed","action":{"type":"search","query":"example"},"results":[{"title":"Example","url":"https://example.com"}]}`),
		},
		{
			name: "failed",
			raw:  json.RawMessage(`{"type":"web_search_call","id":"ws_1","status":"failed","action":{"type":"search","query":"example"},"error":{"message":"search timed out","type":"provider_error"}}`),
		},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			client := scriptedllm.NewClient(scriptedllm.Script{
				Capabilities: &caps,
				Steps: []scriptedllm.Step{
					{Response: llm.Response{Assistant: llm.Message{Role: llm.RoleAssistant}, OutputItems: []llm.ResponseItem{{Type: llm.ResponseItemTypeOther, Raw: fixture.raw}}}},
					scriptedllm.FinalAnswer("completed"),
				},
			})
			store := newRuntimeViewStore(t)
			var events []runtime.Event
			engine := newRuntimeViewEngine(t, store, client, runtime.Config{
				Model: "gpt-6-sol", WebSearchMode: "native", EnabledTools: []toolspec.ID{toolspec.ToolWebSearch},
				OnEvent: func(event runtime.Event) {
					if event.CommittedTranscriptChanged {
						events = append(events, event)
					}
				},
			})
			if _, err := engine.SubmitUserMessage(t.Context(), "search example"); err != nil {
				t.Fatalf("submit user message: %v", err)
			}
			assertPersistedLiveTranscriptParity(t, store, events)
		})
	}
}

func newParityToolRegistry(t *testing.T, store *session.Store) *tools.Registry {
	t.Helper()
	manager, err := shelltool.NewManager(t.TempDir())
	if err != nil {
		t.Fatalf("create shell manager: %v", err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Errorf("close shell manager: %v", err)
		}
	})
	patch, err := patchtool.New(runtimewirefixture.FilesystemContext(t, store.Meta().WorkspaceRoot))
	if err != nil {
		t.Fatalf("create patch tool: %v", err)
	}
	postprocessor, err := postprocess.NewRunner(postprocess.Settings{
		PersistenceRoot: t.TempDir(), Mode: config.ShellPostprocessingModeNone,
	})
	if err != nil {
		t.Fatalf("create shell postprocessor: %v", err)
	}
	shell := shelltool.NewExecCommandToolWithConfig(store.Meta().WorkspaceRoot, 16_000, 200_000, manager, store.Meta().SessionID, shelltool.ExecCommandToolConfig{Postprocessor: postprocessor})
	return toolfixture.NewRegistry(t,
		tools.HandlerRegistration{ID: toolspec.ToolExecCommand, Handler: shell},
		tools.HandlerRegistration{ID: toolspec.ToolPatch, Handler: patch},
	)
}

func TestPersistedTranscriptParityAtIntermediateToolCommits(t *testing.T) {
	store := newRuntimeViewStore(t)
	registry := newParityToolRegistry(t, store)
	patchInput, err := json.Marshal(struct {
		Patch string `json:"patch"`
	}{Patch: "*** Begin Patch\n*** Add File: parity.txt\n+parity content\n*** End Patch\n"})
	if err != nil {
		t.Fatalf("encode patch input: %v", err)
	}
	failedShellInput, err := json.Marshal(struct {
		Cmd     string `json:"cmd"`
		Workdir string `json:"workdir"`
	}{Cmd: "echo parity", Workdir: filepath.Join(store.Meta().WorkspaceRoot, "missing")})
	if err != nil {
		t.Fatalf("encode failed shell input: %v", err)
	}
	client := scriptedllm.NewClient(scriptedllm.Script{Steps: []scriptedllm.Step{
		scriptedllm.ToolBatch("checking and editing",
			llm.ToolCall{ID: "call-shell", Name: string(toolspec.ToolExecCommand), Input: json.RawMessage(`{"cmd":"echo parity","shell":"/bin/sh","login":false}`)},
			llm.ToolCall{ID: "call-patch", Name: string(toolspec.ToolPatch), Input: patchInput},
			llm.ToolCall{ID: "call-shell-failed", Name: string(toolspec.ToolExecCommand), Input: failedShellInput},
		),
		{
			Response: scriptedllm.FinalAnswer("completed").Response,
			ExpectedToolResults: []scriptedllm.ExpectedToolResult{
				{CallID: "call-shell", Name: string(toolspec.ToolExecCommand)},
				{CallID: "call-patch", Name: string(toolspec.ToolPatch)},
				{CallID: "call-shell-failed", Name: string(toolspec.ToolExecCommand)},
			},
		},
	}})
	eventLog, err := store.MaterializeEventLog()
	if err != nil {
		t.Fatalf("materialize event log: %v", err)
	}
	var events []runtime.Event
	var observed []*transcriptpb.CommittedRow
	var previousPage []*transcriptpb.CommittedRow
	intermediatePoints := 0
	toolRows := 0
	expectedOutcomes := map[string]bool{
		"call-shell": false, "call-patch": false, "call-shell-failed": true,
	}
	engine, err := runtime.New(store, eventLog, client, registry, runtime.Config{
		Model: "gpt-6-sol", EnabledTools: []toolspec.ID{toolspec.ToolExecCommand, toolspec.ToolPatch},
		OnEvent: func(event runtime.Event) {
			if !event.CommittedTranscriptChanged {
				return
			}
			events = append(events, event)
			if result := event.ToolResult; event.Kind == runtime.EventToolCallCompleted && result != nil {
				isError, exists := expectedOutcomes[result.CallID]
				if !exists || result.IsError != isError {
					t.Errorf("tool outcome %q: failed=%t, expected known=%t failed=%t", result.CallID, result.IsError, exists, isError)
				}
			}
			messages, err := TranscriptMessagesFromRuntimeEventChecked(event)
			if err != nil {
				t.Errorf("project intermediate event %q: %v", event.Kind, err)
				return
			}
			for _, message := range messages {
				if row := message.GetCommittedRow(); row != nil {
					observed = append(observed, row)
					if tool := row.GetTool(); tool != nil {
						toolRows++
						if tool.Presentation == nil {
							t.Errorf("actual tool completion lacks structured presentation: %v", row)
						}
					}
				}
			}
			page, _ := mustProjectNewestPersistedTranscript(t, store, eventLog)
			// Each read is independent: only already recurring locators must agree.
			assertTranscriptRowOverlap(t, observed, page.Entries)
			assertTranscriptRowOverlap(t, previousPage, page.Entries)
			previousPage = page.Entries
			intermediatePoints++
		},
	})
	if err != nil {
		t.Fatalf("create engine: %v", err)
	}
	t.Cleanup(func() {
		if err := engine.Close(); err != nil {
			t.Errorf("close engine: %v", err)
		}
	})
	if _, err := engine.SubmitUserMessage(t.Context(), "exercise intermediate shell and patch commits"); err != nil {
		t.Fatalf("submit user message: %v", err)
	}
	if intermediatePoints < 5 || toolRows != 3 {
		t.Fatalf("intermediate coverage: points=%d tools=%d, want assistant and three tool commits", intermediatePoints, toolRows)
	}
	assertPersistedLiveTranscriptParity(t, store, events)
}

func TestPersistedHistoryReplacementMatchesActualEngineLiveDelivery(t *testing.T) {
	for _, mode := range []string{"local", "native"} {
		t.Run(mode, func(t *testing.T) {
			store := newRuntimeViewStore(t)
			caps := scriptedllm.DefaultProviderCapabilities()
			caps.SupportsResponsesCompact = true
			failedShellInput, err := json.Marshal(struct {
				Cmd     string `json:"cmd"`
				Workdir string `json:"workdir"`
			}{Cmd: "echo parity", Workdir: filepath.Join(store.Meta().WorkspaceRoot, "missing")})
			if err != nil {
				t.Fatalf("encode failed shell input: %v", err)
			}
			steps := []scriptedllm.Step{scriptedllm.FinalAnswer("completed")}
			if mode == "local" {
				steps = append(steps, scriptedllm.FinalAnswer("first compacted summary"))
			}
			steps = append(steps,
				scriptedllm.ToolBatch("checking after compaction",
					llm.ToolCall{
						ID: "call-shell", Name: string(toolspec.ToolExecCommand),
						Input: json.RawMessage(`{"cmd":"echo parity","shell":"/bin/sh","login":false}`),
					},
					llm.ToolCall{ID: "call-shell-failed", Name: string(toolspec.ToolExecCommand), Input: failedShellInput},
				),
				scriptedllm.Step{
					Response: scriptedllm.FinalAnswer("continued after compaction").Response,
					ExpectedToolResults: []scriptedllm.ExpectedToolResult{
						{CallID: "call-shell", Name: string(toolspec.ToolExecCommand)},
						{CallID: "call-shell-failed", Name: string(toolspec.ToolExecCommand)},
					},
				},
			)
			if mode == "local" {
				steps = append(steps, scriptedllm.FinalAnswer("second compacted summary"))
			}
			steps = append(steps, scriptedllm.FinalAnswer("continued in the target context"))
			checkpoint := llm.CompactionResponse{
				Checkpoint: llm.ResponseItem{
					Type: llm.ResponseItemTypeCompaction, ID: textutil.Value("checkpoint"),
					EncryptedContent: textutil.Value("encrypted"),
				},
				Usage: llm.Usage{InputTokens: 100, WindowTokens: 200_000},
			}
			client := scriptedllm.NewClient(scriptedllm.Script{
				Capabilities: &caps,
				Steps:        steps,
				Compactions:  []llm.CompactionResponse{checkpoint, checkpoint},
			})
			eventLog, err := store.MaterializeEventLog()
			if err != nil {
				t.Fatalf("materialize event log: %v", err)
			}
			var events []runtime.Event
			engine, err := runtime.New(store, eventLog, client, newParityToolRegistry(t, store), runtime.Config{
				Model: "gpt-6-sol", CompactionMode: mode,
				EnabledTools: []toolspec.ID{toolspec.ToolExecCommand, toolspec.ToolPatch},
				OnEvent: func(event runtime.Event) {
					if event.CommittedTranscriptChanged {
						events = append(events, event)
					}
				},
			})
			if err != nil {
				t.Fatalf("create engine: %v", err)
			}
			t.Cleanup(func() {
				if err := engine.Close(); err != nil {
					t.Errorf("close engine: %v", err)
				}
			})
			if _, err := engine.SubmitUserMessage(t.Context(), "preserve this user context"); err != nil {
				t.Fatalf("submit user message: %v", err)
			}
			if _, err := engine.CompactContextForWorkflowPostCompletion(t.Context()); err != nil {
				t.Fatalf("first compact context: %v", err)
			}
			if _, err := engine.SubmitUserMessage(t.Context(), "context in the second segment"); err != nil {
				t.Fatalf("submit post-compaction user message: %v", err)
			}
			before, err := engine.TranscriptNewestSegmentPage()
			if err != nil {
				t.Fatalf("read pre-compaction segment: %v", err)
			}
			beforeWire, err := TranscriptTailSegmentFromSegment(before)
			if err != nil {
				t.Fatalf("project pre-compaction segment: %v", err)
			}
			if before.OlderCursor <= 0 || before.NewerCursor <= before.OlderCursor {
				t.Fatalf("expected noninitial bounded segment cursors: older=%d newer=%d", before.OlderCursor, before.NewerCursor)
			}
			var beforeLive []*transcriptpb.CommittedRow
			for _, event := range events {
				messages, err := TranscriptMessagesFromRuntimeEventChecked(event)
				if err != nil {
					t.Fatalf("project pre-compaction live event: %v", err)
				}
				for _, message := range messages {
					if row := message.GetCommittedRow(); row != nil {
						beforeLive = append(beforeLive, row)
					}
				}
			}
			assertTranscriptRowOverlap(t, beforeLive, beforeWire.Entries)
			events = nil
			if _, err := engine.CompactContextForWorkflowPostCompletion(t.Context()); err != nil {
				t.Fatalf("compact context: %v", err)
			}
			replacementRows := 0
			for _, event := range events {
				if event.Kind == runtime.EventLocalEntryAdded && event.LocalEntryProjected {
					replacementRows++
				}
			}
			if replacementRows == 0 {
				t.Fatal("completed compaction did not display its summary")
			}
			assertPersistedLiveTranscriptParity(t, store, events)
			if _, err := engine.SubmitUserMessage(t.Context(), "start the next context"); err != nil {
				t.Fatalf("submit target user message: %v", err)
			}
			assertPersistedLiveTranscriptParity(t, store, events)
			for _, direction := range []struct {
				name string
				read func() (runtime.TranscriptSegmentPage, error)
			}{
				{name: "backward", read: func() (runtime.TranscriptSegmentPage, error) {
					return engine.TranscriptSegmentPage(before.NewerCursor)
				}},
				{name: "forward", read: func() (runtime.TranscriptSegmentPage, error) {
					return engine.TranscriptSegmentPageForward(before.OlderCursor)
				}},
			} {
				t.Run(direction.name, func(t *testing.T) {
					recurrent, err := direction.read()
					if err != nil {
						t.Fatalf("read recurrent segment: %v", err)
					}
					wire, err := TranscriptTailSegmentFromSegment(recurrent)
					if err != nil {
						t.Fatalf("project recurrent segment: %v", err)
					}
					if overlap := assertTranscriptRowOverlap(t, beforeWire.Entries, wire.Entries); overlap == 0 {
						t.Error("old cursor produced no recurrent locator for parity coverage")
					}
					assertTranscriptRowOverlap(t, beforeLive, wire.Entries)
				})
			}
		})
	}
}

func mustProjectNewestPersistedTranscript(
	t *testing.T,
	store *session.Store,
	eventLog session.MaterializedEventLog,
) (*transcriptpb.Page, *transcriptpb.Hydration) {
	t.Helper()
	segment, err := runtime.TranscriptNewestSegmentPageFromEventLog(eventLog, "")
	if err != nil {
		t.Fatalf("scan newest transcript page: %v", err)
	}
	page, err := TranscriptPageFromSegment(
		store.Meta().SessionID,
		"",
		runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED,
		segment,
	)
	if err != nil {
		t.Fatalf("project persisted page: %v", err)
	}
	hydration := mustTranscriptHydration(t, runtime.TranscriptHydrationSnapshot{
		CommittedRows: runtime.TranscriptCommittedRowFactsFromSnapshot(segment.Snapshot),
	})
	return page, hydration
}

func TestPersistedUserRollbackTargetMatchesLiveDelivery(t *testing.T) {
	const text = "persisted user message"

	store := newRuntimeViewStore(t)
	eventLog, err := store.MaterializeEventLog()
	if err != nil {
		t.Fatalf("materialize event log: %v", err)
	}
	stepID := transcriptProjectionStepID
	record, _, err := eventLog.AppendRecord(&stepID, session.MessageRecord{
		Role:    session.MessageRoleUser,
		Content: textutil.Value(text),
	})
	if err != nil {
		t.Fatalf("append user message: %v", err)
	}

	projectedPage, hydration := mustProjectNewestPersistedTranscript(t, store, eventLog)
	if len(projectedPage.Entries) != 1 || projectedPage.Entries[0].GetUser() == nil ||
		len(hydration.TailSegment.Entries) != 1 {
		t.Fatalf("persisted rows: page=%d hydration=%d, want one each", len(projectedPage.Entries), len(hydration.TailSegment.Entries))
	}

	provenance := &runtime.TranscriptCommittedRowProvenance{
		EventSequence:     record.Seq(),
		CommittedAtUnixMs: record.CommittedAtUnixMs(),
	}
	targetID := rollbacktarget.EncodeUserMessageSeq(record.Seq())
	if got := projectedPage.Entries[0].GetUser().RollbackTargetId; got == nil || *got != string(targetID) {
		t.Fatalf("persisted rollback target = %v, want %q", got, targetID)
	}

	liveEvents := []struct {
		name  string
		event runtime.Event
	}{
		{
			name: "flushed",
			event: runtime.Event{
				Kind:                runtime.EventUserMessageFlushed,
				UserMessage:         text,
				StepID:              textutil.Value(stepID),
				CommittedProvenance: provenance,
			},
		},
		{
			name: "message",
			event: runtime.Event{
				Kind:   runtime.EventConversationUpdated,
				StepID: textutil.Value(stepID),
				Message: llm.Message{
					Role:    llm.RoleUser,
					Content: textutil.Value(text),
				},
				CommittedProvenance: provenance,
			},
		},
	}
	for _, fixture := range liveEvents {
		t.Run(fixture.name, func(t *testing.T) {
			liveMessages, err := TranscriptMessagesFromRuntimeEventChecked(fixture.event)
			if err != nil {
				t.Fatalf("project live event: %v", err)
			}
			if len(liveMessages) != 1 || liveMessages[0].GetCommittedRow() == nil {
				t.Fatalf("live transcript messages = %+v, want one committed row", liveMessages)
			}
			liveRow := liveMessages[0].GetCommittedRow()
			if !proto.Equal(liveRow, projectedPage.Entries[0]) {
				t.Fatalf("live row differs from persisted page:\nlive: %+v\npage: %+v", liveRow, projectedPage.Entries[0])
			}
			if !proto.Equal(liveRow, hydration.TailSegment.Entries[0]) {
				t.Fatalf("live row differs from hydration:\nlive: %+v\nhydration: %+v", liveRow, hydration.TailSegment.Entries[0])
			}
		})
	}
}

func TestPersistedDeveloperNoticeDiagnosticCodeMatchesLiveDelivery(t *testing.T) {
	const text = "workspace environment details"

	store := newRuntimeViewStore(t)
	eventLog, err := store.MaterializeEventLog()
	if err != nil {
		t.Fatalf("materialize event log: %v", err)
	}
	stepID := transcriptProjectionStepID
	messageType := session.MessageTypeEnvironment
	record, _, err := eventLog.AppendRecord(&stepID, session.MessageRecord{
		Role:        session.MessageRoleDeveloper,
		MessageType: &messageType,
		Content:     textutil.Value(text),
	})
	if err != nil {
		t.Fatalf("append developer message: %v", err)
	}

	projectedPage, hydration := mustProjectNewestPersistedTranscript(t, store, eventLog)
	if len(projectedPage.Entries) != 1 || len(hydration.TailSegment.Entries) != 1 {
		t.Fatalf("persisted rows: page=%d hydration=%d, want one each", len(projectedPage.Entries), len(hydration.TailSegment.Entries))
	}

	provenance := &runtime.TranscriptCommittedRowProvenance{
		EventSequence:     record.Seq(),
		CommittedAtUnixMs: record.CommittedAtUnixMs(),
	}
	liveMessages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind:   runtime.EventConversationUpdated,
		StepID: textutil.Value(stepID),
		Message: llm.Message{
			Role:        llm.RoleDeveloper,
			MessageType: textutil.Value(llm.MessageType(messageType)),
			Content:     textutil.Value(text),
		},
		CommittedProvenance: provenance,
	})
	if err != nil {
		t.Fatalf("project live developer message: %v", err)
	}
	if len(liveMessages) != 1 || liveMessages[0].GetCommittedRow() == nil {
		t.Fatalf("live transcript messages = %+v, want one committed row", liveMessages)
	}
	liveRow := liveMessages[0].GetCommittedRow()
	if notice := liveRow.GetNotice(); notice == nil ||
		notice.Diagnostic == nil ||
		notice.Diagnostic.Code != string(transcript.EntryRoleDeveloperContext) {
		t.Fatalf("live developer notice diagnostic = %+v, want canonical developer-context code", notice)
	}
	for name, persistedRow := range map[string]*transcriptpb.CommittedRow{
		"page":      projectedPage.Entries[0],
		"hydration": hydration.TailSegment.Entries[0],
	} {
		if !proto.Equal(liveRow, persistedRow) {
			t.Fatalf("live row differs from %s:\nlive: %+v\n%s: %+v", name, liveRow, name, persistedRow)
		}
	}
}
