package app

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"core/cli/tui/ongoing"
	"core/cli/tui/transcriptrender"
	"core/internal/testharness/pty"
	"core/internal/testharness/pty/analyzer"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/serverapi"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"
)

func TestReopenHistoryPaintedBeforeWindowSizeRemainsVisible(t *testing.T) {
	const prompt = "persisted question visible on reopen"
	for _, policy := range []ongoing.TerminalResizePolicy{
		ongoing.TerminalResizeSemanticPrompt,
		ongoing.TerminalResizeRehydration,
	} {
		t.Run(fmt.Sprintf("policy=%d", policy), func(t *testing.T) {
			var output bytes.Buffer
			surface := ongoing.NewSurfaceWithOptions(&output, ongoing.SurfaceOptions{
				TerminalResize: policy,
				MarkdownLinks:  transcriptrender.MarkdownLinkLabelOnly,
			})
			model := newProjectedClosedUIModel(
				&runtimeControlFakeClient{sessionView: &runtimepb.SessionView{SessionId: detailTestSessionID}},
				WithUISessionID(detailTestSessionID),
				WithUIOngoingSurface(surface),
			)
			model.ongoingTranscript = newNoopOngoingTranscriptController(surface, model.ongoingFrameInput)
			if cmd := model.adoptOngoingTranscriptOwnership(); cmd != nil {
				t.Fatal("adopting ownership before the terminal size is known must not paint")
			}
			model = updateUIModel(t, model, ongoingTranscriptEvent{
				Kind: ongoingTranscriptEventHistory,
				Page: &transcriptpb.Page{Entries: []*transcriptpb.CommittedRow{
					historyTestUserRow(10, 1, prompt),
				}},
			})
			if strings.Contains(output.String(), prompt) {
				t.Fatal("history was painted before the terminal size was known")
			}
			model = updateUIModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 24})
			stream, err := analyzer.NewStream(pty.MustDimensions(24, 80))
			if err != nil {
				t.Fatal(err)
			}
			if err := stream.Feed(output.Bytes()); err != nil {
				t.Fatal(err)
			}
			screen, err := stream.ScreenSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(screen.RenderText(), prompt) {
				t.Fatalf("reopen history missing from the visible screen after the first window size: %q", screen.RenderText())
			}
		})
	}
}

func TestOngoingOpeningShowsStoredHistoryBeforeTranscriptStreamArrives(t *testing.T) {
	const prompt = "a previously persisted question"
	row := detailTestUserRow(prompt)
	row.Visibility = transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING
	sessionViews := &countingSessionViewClient{page: &transcriptpb.Page{
		SessionId:             detailTestSessionID,
		ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED,
		Entries:               []*transcriptpb.CommittedRow{row},
	}}
	var output bytes.Buffer
	surface := ongoing.NewSurface(&output)
	model := sizedTestUIModel(newProjectedClosedUIModel(
		&runtimeControlFakeClient{sessionView: &runtimepb.SessionView{SessionId: detailTestSessionID}},
		WithUISessionID(detailTestSessionID),
		WithUIStatusConfig(uiStatusConfig{SessionViews: sessionPromptHistoryClient{sessionViews}}),
		WithUIOngoingSurface(surface),
	), 80, 24)
	model.ongoingTranscript = newNoopOngoingTranscriptController(surface, model.ongoingFrameInput)

	events := startSessionTranscriptEvents(t.Context(), detailTestSessionID,
		sessionViews.GetSessionTranscriptPage,
		func(ctx context.Context, _ *transcriptpb.SubscribeRequest) (serverapi.TranscriptSubscription, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		})
	defer events.Stop()
	model = updateUIModel(t, model, nextTranscriptEvent(t, events.Events))

	stream, err := analyzer.NewStream(pty.MustDimensions(24, 80))
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Feed(output.Bytes()); err != nil {
		t.Fatal(err)
	}
	screen, err := stream.ScreenSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(screen.RenderText(), prompt) {
		t.Fatalf("opening omitted stored history before any live transcript event: %q", screen.RenderText())
	}
}

func TestInitialHistoryHandoffEmitsOnlyNewRowsAndDoesNotDeduplicateRecovery(t *testing.T) {
	for _, owned := range []bool{true, false} {
		t.Run(map[bool]string{true: "ongoing", false: "off-surface"}[owned], func(t *testing.T) {
			old := historyTestUserRow(10, 1, "stored old question")
			last := historyTestUserRow(10, 2, "stored last question")
			added := historyTestUserRow(11, 1, "newly committed question")
			var output bytes.Buffer
			controller := newNoopOngoingTranscriptController(ongoing.NewSurface(&output), ongoingTestFrameProvider)
			if _, err := controller.SetNormalBufferOwned(owned); err != nil {
				t.Fatal(err)
			}
			if _, err := controller.AcceptHistory(&transcriptpb.Page{Entries: []*transcriptpb.CommittedRow{old, last}}); err != nil {
				t.Fatal(err)
			}
			hydration := ongoingHydrationMessage(1)
			hydration.Event.GetHydration().TailSegment.Entries = []*transcriptpb.CommittedRow{old, last, added}
			if _, _, err := controller.Accept(hydration); err != nil {
				t.Fatal(err)
			}
			if _, err := controller.SetNormalBufferOwned(true); err != nil {
				t.Fatal(err)
			}
			for _, row := range []*transcriptpb.CommittedRow{old, last, added} {
				if count := strings.Count(output.String(), row.GetUser().Text); count != 1 {
					t.Fatalf("initial handoff emitted %q %d times", row.GetUser().Text, count)
				}
			}
			if strings.Index(output.String(), last.GetUser().Text) > strings.Index(output.String(), added.GetUser().Text) {
				t.Fatal("initial handoff reordered newly committed history")
			}
			if len(hydration.Event.GetHydration().TailSegment.Entries) != 3 {
				t.Fatal("terminal projection mutated the admitted hydration")
			}
			if _, err := controller.AcceptCutover(ongoing.RehydrateReasonSequenceGap); err != nil {
				t.Fatal(err)
			}
			if _, _, err := controller.Accept(hydration); err != nil {
				t.Fatal(err)
			}
			if count := strings.Count(output.String(), old.GetUser().Text); count != 2 {
				t.Fatalf("Scratch Rehydration suppressed authoritative history: count=%d", count)
			}
		})
	}
}

func TestInitialHistoryHandoffKeepsNewCompletionAtAnExistingToolCallLocator(t *testing.T) {
	surface := &ongoingSurfaceSpy{}
	controller := newNoopOngoingTranscriptController(surface, ongoingTestFrameProvider)
	call := &transcriptpb.CommittedRow{
		Locator: &transcriptpb.CommittedRowLocator{EventSequence: 10, RowOrdinal: 1},
		Row: &transcriptpb.CommittedRow_ToolCall{ToolCall: &transcriptpb.ToolStart{
			ToolCallId: "pending", ToolName: "exec_command",
		}},
	}
	last := historyTestUserRow(11, 1, "later persisted question")
	if _, err := controller.AcceptHistory(&transcriptpb.Page{Entries: []*transcriptpb.CommittedRow{call, last}}); err != nil {
		t.Fatal(err)
	}
	completed := &transcriptpb.CommittedRow{
		Locator: proto.CloneOf(call.Locator),
		Row: &transcriptpb.CommittedRow_Tool{Tool: &transcriptpb.ToolRow{
			ToolCallId: proto.String("pending"), ToolName: proto.String("exec_command"),
		}},
	}
	hydration := ongoingHydrationMessage(1)
	hydration.Event.GetHydration().TailSegment.Entries = []*transcriptpb.CommittedRow{completed, last}
	if _, _, err := controller.Accept(hydration); err != nil {
		t.Fatal(err)
	}
	for _, received := range surface.calls {
		if received.message != nil {
			rows := received.message.Event.GetHydration().TailSegment.Entries
			if len(rows) != 1 || !proto.Equal(rows[0], completed) {
				t.Fatalf("initial handoff lost a newly completed tool: %+v", rows)
			}
			return
		}
	}
	t.Fatal("initial hydration did not reach the terminal")
}

func TestInitialHistoryHandoffSupportsDifferentNewestSegmentBounds(t *testing.T) {
	for _, includeOverlap := range []bool{true, false} {
		t.Run(map[bool]string{true: "partial-overlap", false: "disjoint-segments"}[includeOverlap], func(t *testing.T) {
			old := historyTestUserRow(10, 1, "old persisted content")
			last := historyTestUserRow(10, 2, "last persisted content")
			added := historyTestUserRow(11, 1, old.GetUser().Text)
			var output bytes.Buffer
			controller := newNoopOngoingTranscriptController(ongoing.NewSurface(&output), ongoingTestFrameProvider)
			if _, err := controller.AcceptHistory(&transcriptpb.Page{Entries: []*transcriptpb.CommittedRow{old, last}}); err != nil {
				t.Fatal(err)
			}
			hydration := ongoingHydrationMessage(1)
			rows := []*transcriptpb.CommittedRow{added}
			if includeOverlap {
				rows = append([]*transcriptpb.CommittedRow{last}, rows...)
			}
			hydration.Event.GetHydration().TailSegment.Entries = rows
			if _, _, err := controller.Accept(hydration); err != nil {
				t.Fatal(err)
			}
			if count := strings.Count(output.String(), old.GetUser().Text); count != 2 {
				t.Fatalf("distinct committed rows with identical text emitted %d times, want 2", count)
			}
			if count := strings.Count(output.String(), last.GetUser().Text); count != 1 {
				t.Fatalf("overlap repeated old row: count=%d", count)
			}
		})
	}
}

func historyTestUserRow(sequence int64, ordinal int32, text string) *transcriptpb.CommittedRow {
	row := ongoingTranscriptMessage(1, reflect.TypeFor[*transcriptpb.Event_CommittedRow]()).Event.GetCommittedRow()
	row.Locator = &transcriptpb.CommittedRowLocator{EventSequence: sequence, RowOrdinal: ordinal}
	row.GetUser().Text = text
	return row
}
