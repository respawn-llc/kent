package ongoing

import (
	"bytes"
	"strings"
	"testing"

	"core/internal/testharness/pty"
	"core/internal/testharness/pty/analyzer"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/textutil"

	"github.com/google/uuid"
)

func TestUnknownAssistantAbortRequiresUnfinishedSource(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("unknown unfinished-source abort was accepted")
		}
	}()
	_, _ = NewSurface(&bytes.Buffer{}).ApplyTerminalMessage(&transcriptpb.Message{Event: &transcriptpb.Event{
		Payload: &transcriptpb.Event_AssistantStreamAbort{AssistantStreamAbort: &transcriptpb.AssistantStreamAbort{
			StreamId: uuid.NewString(), Reason: transcriptpb.AssistantAbortReason_ASSISTANT_ABORT_REASON_INTERRUPTED,
			SourceState: transcriptpb.AssistantSourceState_ASSISTANT_SOURCE_STATE_STREAMING,
		}},
	}}, blankFinalFrame())
}

func TestStableAssistantAfterEarlierProviderItemsPromotesBeforeAcceptance(t *testing.T) {
	var output bytes.Buffer
	surface := NewSurface(&output)
	frame := blankFinalFrame()
	id := uuid.NewString()
	source := "A stable assistant paragraph.\n\n"
	events := []*transcriptpb.Event{
		{Payload: &transcriptpb.Event_AssistantDelta{AssistantDelta: &transcriptpb.AssistantDelta{
			StreamId: id, OutputIndex: textutil.Value(int64(2)), Delta: source,
			Phase: transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY,
		}}},
		{Payload: &transcriptpb.Event_AssistantStreamAbort{AssistantStreamAbort: &transcriptpb.AssistantStreamAbort{
			StreamId: id, Reason: transcriptpb.AssistantAbortReason_ASSISTANT_ABORT_REASON_INTERRUPTED,
		}}},
	}
	for _, event := range events {
		if _, err := surface.ApplyTerminalMessage(&transcriptpb.Message{Event: event}, frame); err != nil {
			t.Fatal(err)
		}
	}
	terminal, err := analyzer.NewStream(pty.MustDimensions(frame.Size.Height, frame.Size.Width))
	if err != nil {
		t.Fatal(err)
	}
	if err := terminal.Feed(output.Bytes()); err != nil {
		t.Fatal(err)
	}
	screen, err := terminal.ScreenSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(screen.RenderText(), strings.TrimSpace(source)) {
		t.Fatal("eligible assistant text waited for preceding provider items or native acceptance")
	}
}

func TestPendingNativeToolDoesNotEmitAndCompletionUsesOrdinaryPresentation(t *testing.T) {
	var output, ordinary bytes.Buffer
	surface := NewSurface(&output)
	frame := blankFinalFrame()
	callID := uuid.NewString()
	apply := func(target *Surface, row *transcriptpb.CommittedRow) {
		t.Helper()
		_, err := target.ApplyTerminalMessage(&transcriptpb.Message{Event: &transcriptpb.Event{
			Payload: &transcriptpb.Event_CommittedRow{CommittedRow: row},
		}}, frame)
		if err != nil {
			t.Fatal(err)
		}
	}
	apply(surface, &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_ToolCall{ToolCall: &transcriptpb.ToolStart{
			StepId: uuid.NewString(), ToolCallId: callID, ToolName: "exec_command",
		}},
	})
	if output.Len() != 0 {
		t.Fatal("unfinished tool entered Scrollback")
	}
	result := &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_Tool{Tool: &transcriptpb.ToolRow{
			ToolCallId: &callID, ToolName: textutil.Value("exec_command"), ResultSummary: textutil.Value("completed"),
		}},
	}
	apply(surface, result)
	apply(NewSurface(&ordinary), result)
	if ordinary.Len() == 0 || !bytes.Equal(output.Bytes(), ordinary.Bytes()) {
		t.Fatal("tool completion did not use one ordinary finished presentation")
	}
}

func TestFinishedToolWaitsOnlyForUnfinishedAssistantMessage(t *testing.T) {
	var output bytes.Buffer
	surface := NewSurface(&output)
	frame := blankFinalFrame()
	id := uuid.NewString()
	source, result := "An unfinished assistant message", "completed-tool-result"
	apply := func(event *transcriptpb.Event) {
		t.Helper()
		if _, err := surface.ApplyTerminalMessage(&transcriptpb.Message{Event: event}, frame); err != nil {
			t.Fatal(err)
		}
	}
	apply(&transcriptpb.Event{Payload: &transcriptpb.Event_AssistantDelta{AssistantDelta: &transcriptpb.AssistantDelta{
		StreamId: id, Delta: source, Phase: transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY,
	}}})
	apply(&transcriptpb.Event{Payload: &transcriptpb.Event_CommittedRow{CommittedRow: &transcriptpb.CommittedRow{
		Visibility: transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED,
		Integrity:  transcriptpb.RowIntegrity_ROW_INTEGRITY_VALID,
		Row: &transcriptpb.CommittedRow_Tool{Tool: &transcriptpb.ToolRow{
			ToolName: textutil.Value("exec_command"), ResultSummary: &result,
		}},
	}}})
	snapshot := func() string {
		t.Helper()
		terminal, err := analyzer.NewStream(pty.MustDimensions(frame.Size.Height, frame.Size.Width))
		if err != nil {
			t.Fatal(err)
		}
		if err := terminal.Feed(output.Bytes()); err != nil {
			t.Fatal(err)
		}
		screen, err := terminal.ScreenSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		return screen.RenderText()
	}
	if strings.Contains(snapshot(), result) {
		t.Fatal("finished tool split an unfinished assistant message")
	}
	apply(&transcriptpb.Event{Payload: &transcriptpb.Event_CommittedRow{CommittedRow: &transcriptpb.CommittedRow{
		Row: &transcriptpb.CommittedRow_Assistant{Assistant: &transcriptpb.AssistantRow{
			StreamId: &id, Text: source, Phase: transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY,
		}},
	}}})
	text := snapshot()
	if strings.Count(text, result) != 1 || strings.Index(text, source) > strings.Index(text, result) {
		t.Fatal("finished tool did not emit once at the next coherent opportunity")
	}
}
