package app

import (
	"bytes"
	"core/internal/testharness/pty"
	"core/internal/testharness/pty/analyzer"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/textutil"
	"fmt"
)
import (
	"core/cli/tui/ongoing"
	"core/cli/tui/transcriptrender"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeinput"
	xansi "github.com/charmbracelet/x/ansi"
	"reflect"
	"strings"
	"testing"
)

func TestOngoingCompletionSelectionSurvivesIndependentBudgets(t *testing.T) {
	m := sizedTestUIModel(newProjectedStaticUIModel(), 40, 11)
	testSetMainInput(m, "@ab")
	m.pathReference.tracked = detectPathReferenceQuery("@ab", 3)
	for index := range 10 {
		m.pathReference.matches = append(m.pathReference.matches, uiPathReferenceCandidate{Path: fmt.Sprintf("match%d.go", index)})
	}
	m.navigatePathReferencePicker(8)
	var output bytes.Buffer
	surface := ongoing.NewSurface(&output)
	stream, err := analyzer.NewStream(pty.MustDimensions(11, 40))
	if err != nil {
		t.Fatal(err)
	}
	for _, height := range []int{11, 5, 4, 11} {
		m.terminalGeometry = terminalGeometryKnown(40, height)
		if err := stream.Resize(pty.MustDimensions(height, 40)); err != nil {
			t.Fatal(err)
		}
		frame := m.ongoingFrameInput()
		if _, err := surface.Resize(frame.Size, frame); err != nil {
			t.Fatal(err)
		}
		if err := stream.Feed(output.Bytes()); err != nil {
			t.Fatal(err)
		}
		output.Reset()
		snapshot, err := stream.ScreenSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		visible := strings.Contains(snapshot.RenderText(), "match8.go")
		if visible != (height > 4) || m.pathReference.selection != 8 {
			t.Fatalf("height %d selected result visibility=%t selection=%d", height, visible, m.pathReference.selection)
		}
	}
	testSetMainInput(m, strings.Repeat("draft ", 80))
	frame := m.ongoingFrameInput()
	if _, err := surface.Render(frame); err != nil {
		t.Fatal(err)
	}
	if err := stream.Feed(output.Bytes()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := stream.ScreenSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(snapshot.RenderText(), "match8.go") {
		t.Fatal("overlay reduced ordinary input allocation")
	}
}

func TestOngoingOverlayPrecedenceAndHelpFloor(t *testing.T) {
	m := sizedTestUIModel(newProjectedStaticUIModel(), 40, 11)
	slash := newSlashPickerScrollTestModel()
	m.commandRegistry = slash.commandRegistry
	testSetMainInput(m, "/")
	m.refreshSlashCommandFilterFromInput()
	m.pathReference.tracked = detectPathReferenceQuery("@ab", 3)
	m.pathReference.matches = []uiPathReferenceCandidate{{Path: "file-match.go"}}
	selected := m.activePickerPresentation()
	if selected.kind != uiPickerKindSlashCommand {
		t.Fatal("slash should take precedence over file completion")
	}
	m.helpVisible = true
	frame := m.ongoingFrameInput()
	help := frame.Overlay.Project(3)
	want := m.layout().renderHelpPane(40, 3, uiThemeStyles(m.theme))
	if !reflect.DeepEqual(help.Lines, want) {
		t.Fatal("help did not take precedence over completion")
	}
	var output bytes.Buffer
	surface := ongoing.NewSurface(&output)
	for _, height := range []int{6, 7} {
		frame.Size.Height = height
		if _, err := surface.Render(frame); err != nil {
			t.Fatal(err)
		}
		stream, err := analyzer.NewStream(pty.MustDimensions(height, 40))
		if err != nil {
			t.Fatal(err)
		}
		if err := stream.Feed(output.Bytes()); err != nil {
			t.Fatal(err)
		}
		output.Reset()
		snapshot, err := stream.ScreenSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		shown := strings.Contains(snapshot.RenderText(), strings.TrimSpace(xansi.Strip(want[1])))
		if shown != (height == 7) {
			t.Fatalf("help visibility at %d rows = %t", height, shown)
		}
	}
	m.helpVisible = false
	frame = m.ongoingFrameInput()
	got := frame.Overlay.Project(1).Lines
	selected.lineCount = 1
	selected.start = pickerViewportStart(selected.selection, len(selected.rows), 1)
	if !reflect.DeepEqual(got, m.layout().renderPicker(selected, 40)) {
		t.Fatal("closing help did not restore slash completion")
	}
}

func TestOngoingQueueUsesAllocatedHeightAndNewestMessages(t *testing.T) {
	m := sizedTestUIModel(newProjectedStaticUIModel(), 40, 20)
	for index := range 8 {
		m.queueInput(fmt.Sprintf("message%d", index))
	}
	var output bytes.Buffer
	surface := ongoing.NewSurface(&output)
	stream, err := analyzer.NewStream(pty.MustDimensions(20, 40))
	if err != nil {
		t.Fatal(err)
	}
	for _, height := range []int{20, 9, 6, 20} {
		m.terminalGeometry = terminalGeometryKnown(40, height)
		if err := stream.Resize(pty.MustDimensions(height, 40)); err != nil {
			t.Fatal(err)
		}
		frame := m.ongoingFrameInput()
		if _, err := surface.Resize(frame.Size, frame); err != nil {
			t.Fatal(err)
		}
		if err := stream.Feed(output.Bytes()); err != nil {
			t.Fatal(err)
		}
		output.Reset()
		snapshot, err := stream.ScreenSnapshot()
		if err != nil {
			t.Fatal(err)
		}
		visible := min(8, height-4)
		hidden := 0
		if visible < 8 {
			visible--
			hidden = 8 - visible
		}
		top := height - 4 - visible
		for index := range visible {
			text := snapshot.TextInRegion(analyzer.Region{Top: top + index, Bottom: top + index + 1, Left: 0, Right: 40})
			if !strings.Contains(text, fmt.Sprintf("message%d", hidden+index)) {
				t.Fatalf("height %d queue row %d: %q", height, index, text)
			}
		}
		if hidden > 0 {
			text := snapshot.TextInRegion(analyzer.Region{Top: top - 1, Bottom: top, Left: 0, Right: 40})
			if !strings.Contains(text, fmt.Sprintf("%d", hidden)) {
				t.Fatalf("hidden count %d missing: %q", hidden, text)
			}
		}
	}
}

func TestOngoingAllocatedInputFollowsCursor(t *testing.T) {
	for _, realCursor := range []bool{false, true} {
		options := []UIOption{}
		if realCursor {
			options = append(options, WithUITerminalCursorState(newUITerminalCursorState()))
		}
		m := sizedTestUIModel(newProjectedStaticUIModel(options...), 40, 10)
		var output bytes.Buffer
		surface := ongoing.NewSurface(&output)
		stream, err := analyzer.NewStream(pty.MustDimensions(10, 40))
		if err != nil {
			t.Fatal(err)
		}
		for index, height := range []int{10, 12, 10} {
			m.terminalGeometry = terminalGeometryKnown(40, height)
			if err := stream.Resize(pty.MustDimensions(height, 40)); err != nil {
				t.Fatal(err)
			}
			text := strings.Repeat("界e", 60) + "\nend"
			cursor := 2
			if index == 1 {
				cursor = len([]rune(text)) - 1
			}
			testSetMainInputAtRuneCursor(m, text, cursor)
			frame := m.ongoingFrameInput()
			frame.Sections = append([]ongoing.FrameSection{
				{Kind: ongoing.FrameSectionPendingTools, Lines: []string{"tool1", "tool2", "tool3"}},
				{Kind: ongoing.FrameSectionPendingPrompt, Lines: []string{"p1", "p2", "p3", "p4", "p5"}},
			}, frame.Sections...)
			if _, err := surface.Render(frame); err != nil {
				t.Fatal(err)
			}
			if err := stream.Feed(output.Bytes()); err != nil {
				t.Fatal(err)
			}
			output.Reset()
			snapshot, err := stream.ScreenSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			expected := m.layout().inputPaneProjectionWithBudget(40, height-9, uiThemeStyles(m.theme))
			for index, line := range expected.Lines {
				row := 6 + index
				got := snapshot.TextInRegion(analyzer.Region{Top: row, Bottom: row + 1, Left: 0, Right: 40})
				if strings.TrimRight(got, " ") != strings.TrimRight(xansi.Strip(line), " ") {
					t.Fatalf("height %d input row %d = %q, want %q", height, row, got, line)
				}
			}
			if realCursor && (snapshot.Cursor.Row != 6+expected.Cursor.Row-1 || snapshot.Cursor.Col != expected.Cursor.Col) {
				t.Fatalf("cursor = %+v, want input cursor %+v", snapshot.Cursor, expected.Cursor)
			}
		}
	}
}

func TestOngoingFrameInputIgnoresRuntimeMainViewCopiesOfTranscriptOwnedFacts(t *testing.T) {
	m := sizedTestUIModel(newProjectedStaticUIModel(), 48, 10)
	m.runtimeActivityProjection = &runtimepb.Activity{
		State:    runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE,
		Reviewer: runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE}
	m.runtimeContextUsage = &runtimepb.ContextUsage{UsedTokens: 123, WindowTokens: 456}

	frame := m.ongoingFrameInput()

	for _, kind := range frameSectionKinds(frame) {
		switch kind {
		case ongoing.FrameSectionRuntimeActivity, ongoing.FrameSectionContextUsage, ongoing.FrameSectionGoal, ongoing.FrameSectionPendingPrompt:
			t.Fatalf("frame section %s came from non-transcript runtime-main-view/session-activity state", kind)
		}
	}
}

func TestOngoingFrameInputRendersAvailabilityOnlyGoalProjection(t *testing.T) {
	availability := runtimepb.GoalAvailability_GOAL_AVAILABILITY_AVAILABLE
	client := &runtimeControlFakeClient{
		cachedMainView: &runtimepb.MainView{
			Session: &runtimepb.SessionView{},
			Status: &runtimepb.Status{
				Goal: &runtimepb.GoalView{Availability: &availability}}},
		hasCachedMainView: true}
	m := sizedTestUIModel(newProjectedTestUIModel(client), 48, 10)

	m.ongoingFrameInput()
}

func TestOngoingTranscriptControllerPlacesCursorAfterPrependedLiveSections(t *testing.T) {
	m := sizedTestUIModel(newProjectedStaticUIModel(
		WithUITerminalCursorState(newUITerminalCursorState())), 48, 10)
	testSetMainInputAtRuneCursor(m, "hello", 2)
	m.pendingWorkRefresh.collection = runtimeinput.PendingWork{Items: []runtimeinput.PendingWorkItem{
		pendingWorkMessageForTest(runtimeinput.PendingWorkLaneSteer, "server pending")}}
	var output bytes.Buffer
	surface := ongoing.NewSurface(&output)
	controller := newTestOngoingTranscriptController(surface, m.ongoingFrameInput)
	if _, err := controller.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatalf("accept hydration: %v", err)
	}
	if _, err := controller.Accept(ongoingTranscriptMessage(2, reflect.TypeFor[*transcriptpb.Event_ToolStart]())); err != nil {
		t.Fatalf("accept pending tool: %v", err)
	}
	snapshot := ongoingScreenFromOutput(t, ongoing.Size{Width: 48, Height: 10}, output.Bytes())
	if snapshot.Cursor.Row != 7 || snapshot.Cursor.Col != 4 {
		t.Fatalf("final cursor = %+v, want draft cell (7,4)", snapshot.Cursor)
	}
}

func TestOngoingTranscriptControllerPreservesWrappedDisplayCursorTargetWithPrependedSections(t *testing.T) {
	const width = 24
	m := sizedTestUIModel(newProjectedStaticUIModel(
		WithUITerminalCursorState(newUITerminalCursorState())), width, 12)
	testSetMainInput(m, strings.Repeat("界", 20)+" tail")
	projected := m.layout().inputPaneProjection(width, m.layout().effectiveHeight(), uiThemeStyles(m.theme)).Cursor
	if !projected.Visible {
		t.Fatal("shared editor cursor projection is absent")
	}
	m.pendingWorkRefresh.collection = runtimeinput.PendingWork{Items: []runtimeinput.PendingWorkItem{
		pendingWorkMessageForTest(runtimeinput.PendingWorkLaneSteer, "server pending")}}

	var output bytes.Buffer
	surface := ongoing.NewSurface(&output)
	controller := newTestOngoingTranscriptController(surface, m.ongoingFrameInput)
	if _, err := controller.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatalf("accept hydration: %v", err)
	}
	if _, err := controller.Accept(ongoingTranscriptMessage(2, reflect.TypeFor[*transcriptpb.Event_PendingWorkChanged]())); err != nil {
		t.Fatalf("accept queued message: %v", err)
	}

	snapshot := ongoingScreenFromOutput(t, ongoing.Size{Width: width, Height: 12}, output.Bytes())
	if snapshot.Cursor.Row != 9 || snapshot.Cursor.Col != projected.Col {
		t.Fatalf("wrapped cursor = %+v, want last content row and column %d", snapshot.Cursor, projected.Col)
	}
}

func TestOngoingFrameInputAskViewportAndCursorShareBoundedProjection(t *testing.T) {
	const (
		width  = 24
		height = 10
	)
	m := sizedTestUIModel(newProjectedStaticUIModel(
		WithUITerminalCursorState(newUITerminalCursorState())), width, height)
	event := testQuestionAskEvent("ask-1", "Question source", "First", "Second")
	testSetActiveAsk(m, &event)
	m.ask.activeProjection.rows = []string{
		"question row one",
		"question row two",
		"question row three",
		"question row four",
		"question row five"}
	m.ask.freeform = true
	testSetAskInputAtRuneCursor(m, "draft text", 5)

	pane := m.layout().inputPaneProjectionWithBudget(width, height-3, uiThemeStyles(m.theme))
	if !pane.Cursor.Visible {
		t.Fatal("bounded ask pane did not expose the editor cursor")
	}
	frame := m.ongoingFrameInput()
	input, ok := frameSection(frame, ongoing.FrameSectionInput)
	if !ok {
		t.Fatal("bounded ask input section missing from ongoing frame")
	}
	if got, want := input.Lines, pane.Lines; !reflect.DeepEqual(got, want) {
		t.Fatalf("frame input rows differ from bounded pane projection: got %d want %d", len(got), len(want))
	}
	if len(input.Lines) > height-1 {
		t.Fatalf("framed ask rows = %d, bounded maximum = %d", len(input.Lines), height-1)
	}
	var output bytes.Buffer
	if _, err := ongoing.NewSurface(&output).Render(frame); err != nil {
		t.Fatal(err)
	}
	snapshot := ongoingScreenFromOutput(t, frame.Size, output.Bytes())
	if snapshot.Cursor.Row != pane.Cursor.Row-1 || snapshot.Cursor.Col != pane.Cursor.Col {
		t.Fatalf("ask cursor = %+v, want bounded pane cursor %+v", snapshot.Cursor, pane.Cursor)
	}
}

func ongoingScreenFromOutput(t *testing.T, size ongoing.Size, output []byte) analyzer.ScreenSnapshot {
	t.Helper()
	stream, err := analyzer.NewStream(pty.MustDimensions(size.Height, size.Width))
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Feed(output); err != nil {
		t.Fatal(err)
	}
	snapshot, err := stream.ScreenSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestOngoingFrameInputKeepsServerBackedQueuedStateTranscriptOwned(t *testing.T) {
	m := sizedTestUIModel(newProjectedTestUIModel(&runtimeControlFakeClient{}), 48, 10)
	m.injectedQueue = []*injectedRuntimeQueueItem{{

		ServerID: textutil.Value("11111111-1111-4111-8111-111111111111"),
		Text:     "server accepted",
		State:    injectedRuntimeQueueEnqueued}}

	frame := m.ongoingFrameInput()

	if section, ok := frameSection(frame, ongoing.FrameSectionQueuedOrSteered); ok {
		t.Fatalf("legacy queued section = %+v, want absent for server-backed accepted items", section)
	}
}

func TestOngoingFrameInputStillRendersClientLocalQueuedMessages(t *testing.T) {
	m := sizedTestUIModel(newProjectedStaticUIModel(), 48, 10)
	m.queueInput("queued before server acceptance")
	m.pendingWorkRefresh.collection = runtimeinput.PendingWork{Items: []runtimeinput.PendingWorkItem{pendingWorkMessageForTest(runtimeinput.PendingWorkLaneSteer, "server pending")}}

	frame := m.ongoingFrameInput()

	section, ok := frameSection(frame, ongoing.FrameSectionQueuedOrSteered)
	if !ok {
		t.Fatal("local queued section missing")
	}
	if len(section.StyledLines) != 2 || len(section.StyledLines[0].Spans) == 0 {
		t.Fatalf("merged queued styled lines = %+v, want server and local lines", section.StyledLines)
	}
	span := section.StyledLines[0].Spans[0]
	role, semantic := span.Style.Role()
	if !semantic ||
		role != transcriptrender.StyleRoleNoticeSecondary ||
		!span.Style.Has(transcriptrender.SpanAttributeFaint) {
		t.Fatalf("local queued span = %+v, want secondary/faint", span)
	}
}

func TestOngoingFrameInputRendersPendingInjectedMessagesBeforeServerAcceptance(t *testing.T) {
	m := sizedTestUIModel(newProjectedStaticUIModel(), 48, 10)
	m.injectedQueue = []*injectedRuntimeQueueItem{{

		Text:  "pending injected before server acceptance",
		State: injectedRuntimeQueuePendingCreate}}
	frame := m.ongoingFrameInput()

	section, ok := frameSection(frame, ongoing.FrameSectionQueuedOrSteered)
	if !ok {
		t.Fatal("pending injected section missing")
	}
	if len(section.StyledLines) != 1 || len(section.StyledLines[0].Spans) == 0 {
		t.Fatalf("pending injected styled lines = %+v, want one typed line", section.StyledLines)
	}
	span := section.StyledLines[0].Spans[0]
	role, semantic := span.Style.Role()
	if !semantic ||
		role != transcriptrender.StyleRoleNoticePrimary ||
		span.Style.Has(transcriptrender.SpanAttributeFaint) {
		t.Fatalf("pending injected span = %+v, want primary/full-strength", span)
	}
}

func TestOngoingFrameInputRendersNoRuntimeInjectedMessages(t *testing.T) {
	m := sizedTestUIModel(newProjectedStaticUIModel(), 48, 10)
	m.injectedQueue = []*injectedRuntimeQueueItem{{

		ServerID: textutil.Value("11111111-1111-4111-8111-111111111111"),
		Text:     "local injected without runtime client",
		State:    injectedRuntimeQueueEnqueued}}
	frame := m.ongoingFrameInput()

	section, ok := frameSection(frame, ongoing.FrameSectionQueuedOrSteered)
	if !ok {
		t.Fatal("no-runtime injected section missing")
	}
	if len(section.StyledLines) != 1 || len(section.StyledLines[0].Spans) == 0 {
		t.Fatalf("no-runtime injected styled lines = %+v, want one typed line", section.StyledLines)
	}
	span := section.StyledLines[0].Spans[0]
	role, semantic := span.Style.Role()
	if !semantic ||
		role != transcriptrender.StyleRoleNoticePrimary ||
		span.Style.Has(transcriptrender.SpanAttributeFaint) {
		t.Fatalf("no-runtime injected span = %+v, want primary/full-strength", span)
	}
}

func TestOngoingFrameInputDoesNotEchoRecalledPrompt(t *testing.T) {
	const prompt = "recalled draft"
	m := sizedTestUIModel(newProjectedStaticUIModel(), 48, 10)
	m.Update(promptHistoryLoadedMsg{prompts: []string{prompt}})
	if !m.navigatePromptHistory(-1) || testMainInput(m) != prompt {
		t.Fatal("history recall did not populate editable input")
	}
	var output bytes.Buffer
	if _, err := ongoing.NewSurface(&output).Render(m.ongoingFrameInput()); err != nil {
		t.Fatal(err)
	}
	stream, err := analyzer.NewStream(pty.MustDimensions(10, 48))
	if err != nil {
		t.Fatal(err)
	}
	if err := stream.Feed(output.Bytes()); err != nil {
		t.Fatal(err)
	}
	snapshot, err := stream.ScreenSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(snapshot.RenderText(), prompt); got != 1 {
		t.Fatalf("recalled prompt appears %d times, want one editable presentation", got)
	}
}

func frameSection(frame ongoing.FrameInput, kind ongoing.FrameSectionKind) (ongoing.FrameSection, bool) {
	for _, section := range frame.Sections {
		if section.Kind == kind {
			if section.Demand != nil {
				projection := section.Demand.Project(section.Demand.PreferredHeight)
				section.Lines, section.StyledLines = projection.Lines, projection.StyledLines
			}
			return section, true
		}
	}
	return ongoing.FrameSection{}, false
}

func frameSectionKinds(frame ongoing.FrameInput) []ongoing.FrameSectionKind {
	kinds := make([]ongoing.FrameSectionKind, 0, len(frame.Sections))
	for _, section := range frame.Sections {
		kinds = append(kinds, section.Kind)
	}
	return kinds
}
