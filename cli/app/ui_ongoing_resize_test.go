package app

import (
	"bytes"
	"core/cli/tui/ongoing"
	"core/cli/tui/transcriptrender"
	"core/internal/testharness/pty"
	"core/internal/testharness/pty/analyzer"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"io"
	"net"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestOngoingResizeCoalescesWhileHydrationPending(t *testing.T) {
	surface := ongoing.NewSurfaceWithOptions(nil, ongoing.SurfaceOptions{
		TerminalResize: ongoing.TerminalResizeRehydration,
		MarkdownLinks:  transcriptrender.MarkdownLinkLabelOnly,
	})
	var reasons []ongoing.RehydrateReason
	m := sizedTestUIModel(newProjectedStaticUIModel(
		WithUIOngoingSurface(surface),
		WithUIOngoingTranscriptReopen(func(reason ongoing.RehydrateReason) { reasons = append(reasons, reason) }),
	), 80, 11)
	m.ongoingTranscript = newNoopOngoingTranscriptController(surface, m.ongoingFrameInput)
	if _, _, err := m.ongoingTranscript.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatal(err)
	}
	if _, err := surface.Render(m.ongoingFrameInput()); err != nil {
		t.Fatal(err)
	}
	resize := func(height int) {
		m.reduceWindowMessage(tea.WindowSizeMsg{Width: 80, Height: height})
		m.reduceOngoingMessage(ongoingResizeDebounceMsg{token: m.ongoingRehydration.token})
	}
	resize(12)
	resize(13)
	if len(reasons) != 1 {
		t.Fatalf("concurrent reopen count=%d", len(reasons))
	}
	m.handleOngoingTranscriptEvent(ongoingTranscriptEvent{Kind: ongoingTranscriptEventLoss, Err: io.EOF})
	if len(reasons) != 2 {
		t.Fatalf("pre-hydration loss failed to admit recovery: %v", reasons)
	}
	m.handleOngoingTranscriptEvent(ongoingTranscriptEvent{Kind: ongoingTranscriptEventCutover, Reason: &reasons[1]})
	m.handleOngoingTranscriptEvent(ongoingTranscriptEvent{Kind: ongoingTranscriptEventMessage, Message: ongoingHydrationMessage(1)})
	if len(reasons) != 2 {
		t.Fatalf("shared due resize reopened twice: %v", reasons)
	}
	resize(14)
	if len(reasons) != 3 {
		t.Fatalf("subsequent resize was not admitted: %v", reasons)
	}
	m.handleOngoingTranscriptEvent(ongoingTranscriptEvent{Kind: ongoingTranscriptEventFailure, Err: &net.OpError{Op: "dial", Net: "tcp", Err: io.EOF}})
	resize(15)
	if len(reasons) != 3 {
		t.Fatalf("transport retry admitted a parallel reopen: %v", reasons)
	}
	m.handleOngoingTranscriptEvent(ongoingTranscriptEvent{Kind: ongoingTranscriptEventCutover, Reason: &reasons[2]})
	m.handleOngoingTranscriptEvent(ongoingTranscriptEvent{Kind: ongoingTranscriptEventMessage, Message: ongoingHydrationMessage(1)})
	if len(reasons) != 4 {
		t.Fatalf("pending resize was not admitted after retry hydration: %v", reasons)
	}
	m.reduceWindowMessage(tea.WindowSizeMsg{Width: 80, Height: 16})
	stale := m.ongoingRehydration.token
	m.reduceWindowMessage(tea.WindowSizeMsg{})
	m.reduceOngoingMessage(ongoingResizeDebounceMsg{token: stale})
	m.reduceWindowMessage(tea.WindowSizeMsg{Width: 80, Height: 16})
	if m.ongoingRehydration.token == stale || m.ongoingRehydration.pending == nil || m.ongoingRehydration.pending.due {
		t.Fatal("unknown geometry did not restart the unsettled resize")
	}
	m.handleOngoingTranscriptEvent(ongoingTranscriptEvent{Kind: ongoingTranscriptEventClosed})
	resize(16)
	if len(reasons) != 4 || m.ongoingRehydration.inFlight {
		t.Fatal("closed observation admitted stale reopen")
	}
}

func TestWindowResizeRoundTripPreservesQueuedOutputBeforeDebounce(t *testing.T) {
	for _, offSurface := range []bool{false, true} {
		t.Run(fmt.Sprintf("offSurface=%t", offSurface), func(t *testing.T) {
			var output bytes.Buffer
			surface := ongoing.NewSurfaceWithOptions(&output, ongoing.SurfaceOptions{
				TerminalResize: ongoing.TerminalResizeRehydration, MarkdownLinks: transcriptrender.MarkdownLinkLabelOnly,
			})
			var reopens int
			m := sizedTestUIModel(newProjectedStaticUIModel(WithUIOngoingSurface(surface),
				WithUIOngoingTranscriptReopen(func(ongoing.RehydrateReason) { reopens++ })), 80, 11)
			m.ongoingTranscript = newNoopOngoingTranscriptController(surface, m.ongoingFrameInput)
			if _, _, err := m.ongoingTranscript.Accept(ongoingHydrationMessage(1)); err != nil {
				t.Fatal(err)
			}
			if _, err := surface.Render(m.ongoingFrameInput()); err != nil {
				t.Fatal(err)
			}
			terminal, err := analyzer.NewStream(pty.MustDimensions(11, 80))
			if err != nil {
				t.Fatal(err)
			}
			if err := terminal.Feed(output.Bytes()); err != nil {
				t.Fatal(err)
			}
			before, err := terminal.ScreenSnapshot()
			if err != nil {
				t.Fatal(err)
			}
			output.Reset()
			height := 3
			if offSurface {
				m.activeSurface = uiSurfaceTranscriptDetail
				m.reconcileOngoingOwnership()
				height = 4
			}
			m.reduceWindowMessage(tea.WindowSizeMsg{Width: 80, Height: height})
			old := committedMessageForOngoingResizeTest()
			old.Sequence = 2
			if _, _, err := m.ongoingTranscript.Accept(old); err != nil {
				t.Fatal(err)
			}
			if output.Len() != 0 {
				t.Fatal("unowned resize/delivery wrote terminal output")
			}
			m.reduceWindowMessage(tea.WindowSizeMsg{Width: 80, Height: 11})
			if offSurface {
				m.activeSurface = uiSurfaceOngoingTranscript
				m.reconcileOngoingOwnership()
			}
			if reopens != 0 {
				t.Fatal("restoration reopened before debounce")
			}
			var retired []string
			for _, b := range output.Bytes() {
				prior, err := terminal.ScreenSnapshot()
				if err != nil {
					t.Fatal(err)
				}
				if err := terminal.Feed([]byte{b}); err != nil {
					t.Fatal(err)
				}
				if b == '\n' && prior.Cursor.Row == 10 {
					retired = append(retired, strings.Split(prior.RenderText(), "\n")[0])
				}
			}
			for _, row := range strings.Split(before.RenderText(), "\n") {
				if !slices.Contains(retired, row) {
					t.Fatalf("round trip lost displaced row %q", row)
				}
			}
			if !strings.Contains(output.String(), old.GetEvent().GetCommittedRow().GetUser().Text) {
				t.Fatal("restoration lost queued committed output")
			}
		})
	}
}

func TestOngoingWidthRehydrationDebounceTokenRestarts(t *testing.T) {
	m := newProjectedStaticUIModel()
	if cmd := m.scheduleOngoingResizeRehydration(); cmd == nil {
		t.Fatal("first width rehydration schedule returned nil command")
	}
	first := m.ongoingRehydration.token
	if cmd := m.scheduleOngoingResizeRehydration(); cmd == nil {
		t.Fatal("second width rehydration schedule returned nil command")
	}
	if m.ongoingRehydration.token == first {
		t.Fatalf("width debounce token did not advance: %d", m.ongoingRehydration.token)
	}
}

func TestWindowResizeDoesNotWriteOngoingSurfaceWhileDetailOwnsTerminal(t *testing.T) {
	surface := &ongoingSurfaceSpy{}
	controller := newNoopOngoingTranscriptController(surface, ongoingTestFrameProvider)
	m := newProjectedStaticUIModel(withUIOngoingTranscriptController(controller))
	m.activeSurface = uiSurfaceTranscriptDetail

	result := m.reduceWindowMessage(tea.WindowSizeMsg{Width: 100, Height: 40})

	if !result.handled {
		t.Fatal("window resize was not handled")
	}
	size := m.terminalGeometry.Size()
	if !m.terminalGeometry.IsKnown() || size == nil || size.width != 100 || size.height != 40 {
		t.Fatalf("geometry = %+v, want stored resize", size)
	}
	if len(surface.calls) != 0 {
		t.Fatalf("ongoing surface calls while detail active = %v, want none", surface.calls)
	}
}

func TestWindowResizeWhileDetailOwnsTerminalRepaintsOnReturn(t *testing.T) {
	for _, target := range []ongoing.Size{
		{Width: 100, Height: 24},
		{Width: 80, Height: 30}} {
		t.Run(fmt.Sprintf("%dx%d", target.Width, target.Height), func(t *testing.T) {
			var raw bytes.Buffer
			nativeSurface := ongoing.NewSurface(&raw)
			surface := &ongoingSurfaceSpy{}
			m := sizedTestUIModel(newProjectedStaticUIModel(
				WithUIOngoingSurface(nativeSurface)), 80, 24)
			controller := newNoopOngoingTranscriptController(surface, m.ongoingFrameInput)
			m.ongoingTranscript = controller
			if _, _, err := controller.Accept(ongoingHydrationMessage(1)); err != nil {
				t.Fatalf("accept hydration: %v", err)
			}

			if cmd := m.activateSurface(uiSurfaceTranscriptDetail); cmd == nil {
				t.Fatal("expected detail activation command")
			}
			surface.calls = nil
			raw.Reset()

			result := m.reduceWindowMessage(tea.WindowSizeMsg{Width: target.Width, Height: target.Height})
			if !result.handled {
				t.Fatal("window resize was not handled")
			}
			if raw.Len() != 0 {
				t.Fatalf("ongoing surface wrote raw bytes while detail owned terminal: %q", raw.String())
			}

			if cmd := m.activateSurface(uiSurfaceOngoingTranscript); cmd == nil {
				t.Fatal("expected ongoing activation command")
			}
			if len(surface.calls) != 0 {
				t.Fatalf("surface calls before ownership restore = %v, want none", surface.calls)
			}

			if _, cmd := m.Update(ongoingNormalBufferOwnedMsg{owned: true}); cmd != nil {
				t.Fatalf("post-exit ownership update returned command, want nil")
			}
			if got, want := surface.callKinds(), []string{"render"}; !reflect.DeepEqual(got, want) {
				t.Fatalf("surface calls after ongoing restore = %v, want %v", got, want)
			}
			frame := surface.calls[len(surface.calls)-1].frame
			if frame.Size != target {
				t.Fatalf("repaint frame size = %+v, want %+v", frame.Size, target)
			}
		})
	}
}

func TestAppleTerminalWidthResizeWhileDetailOwnsTerminalRehydratesOnReturn(t *testing.T) {
	var raw bytes.Buffer
	nativeSurface := ongoing.NewSurfaceWithOptions(
		&raw,
		ongoing.SurfaceOptions{
			TerminalResize: ongoing.TerminalResizeRehydration,
			MarkdownLinks:  transcriptrender.MarkdownLinkLabelOnly})
	if _, err := nativeSurface.ApplyTerminalMessage(
		committedMessageForOngoingResizeTest(),
		ongoing.FrameInput{Size: ongoing.Size{Width: 80, Height: 24}}); err != nil {
		t.Fatalf("prime immutable ongoing scrollback: %v", err)
	}
	raw.Reset()
	surface := &ongoingSurfaceSpy{}
	controller := newNoopOngoingTranscriptController(surface, ongoingTestFrameProvider)
	if _, _, err := controller.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatalf("accept hydration: %v", err)
	}
	reopenCount := 0
	m := newProjectedStaticUIModel(
		WithUIOngoingSurface(nativeSurface),
		withUIOngoingTranscriptController(controller),
		WithUIOngoingTranscriptReopen(func(ongoing.RehydrateReason) { reopenCount++ }))

	if cmd := m.activateSurface(uiSurfaceTranscriptDetail); cmd == nil {
		t.Fatal("expected detail activation command")
	}
	result := m.reduceWindowMessage(tea.WindowSizeMsg{Width: 100, Height: 24})
	if !result.handled {
		t.Fatal("window resize was not handled")
	}
	if raw.Len() != 0 {
		t.Fatalf("ongoing surface wrote raw bytes while detail owned terminal: %q", raw.String())
	}
	if m.ongoingRehydration.pending == nil {
		t.Fatal("off-surface width change did not mark pending width rehydration")
	}

	if cmd := m.activateSurface(uiSurfaceOngoingTranscript); cmd == nil {
		t.Fatal("expected ongoing activation command")
	}
	m.Update(ongoingNormalBufferOwnedMsg{owned: true})
	raw.Reset()

	token := m.ongoingRehydration.token
	if _, cmd := m.Update(ongoingResizeDebounceMsg{token: token}); cmd != nil {
		t.Fatalf("debounced width rehydration returned command, want nil")
	}
	if raw.Len() != 0 {
		t.Fatal("timer reset terminal before ordered cutover marker")
	}
	if reopenCount != 1 {
		t.Fatalf("reopen count = %d, want 1", reopenCount)
	}
}

func TestWindowResizeKeepsControllerLiveFrameSections(t *testing.T) {
	var raw bytes.Buffer
	nativeSurface := ongoing.NewSurface(&raw)
	surface := &ongoingSurfaceSpy{}
	m := sizedTestUIModel(newProjectedStaticUIModel(
		WithUIOngoingSurface(nativeSurface)), 40, 10)
	m.ongoingTranscript = newNoopOngoingTranscriptController(surface, m.ongoingFrameInput)
	if _, _, err := m.ongoingTranscript.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatalf("accept hydration: %v", err)
	}
	if _, _, err := m.ongoingTranscript.Accept(ongoingTranscriptMessage(2, reflect.TypeFor[*transcriptpb.Event_Prompt]())); err != nil {
		t.Fatalf("accept pending prompt: %v", err)
	}
	surface.calls = nil

	result := m.reduceWindowMessage(tea.WindowSizeMsg{Width: 40, Height: 10})

	if !result.handled {
		t.Fatal("window resize was not handled")
	}
	if got, want := surface.callKinds(), []string{"resize"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("surface calls = %v, want %v", got, want)
	}
	if got, want := surface.lastFrameSectionLines(ongoing.FrameSectionPendingPrompt), []string{"Approve command?"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("prompt section lines = %v, want %v", got, want)
	}
}

func committedMessageForOngoingResizeTest() *transcriptpb.Message {
	message := ongoingTranscriptMessage(2, reflect.TypeFor[*transcriptpb.Event_CommittedRow]())
	message.GetEvent().GetCommittedRow().GetUser().Text = "committed"
	return message
}
