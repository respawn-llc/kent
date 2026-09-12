package app

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"core/cli/tui/ongoing"
	"core/cli/tui/transcriptrender"
	"core/shared/protoapi"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"

	tea "github.com/charmbracelet/bubbletea"
)

const ongoingTranscriptQueueLimit = 1000

type ongoingTranscriptSurface interface {
	ApplyTerminalMessage(*transcriptpb.Message, ongoing.FrameInput) (ongoing.Result, error)
	Render(ongoing.FrameInput) (ongoing.Result, error)
	Resize(ongoing.Size, ongoing.FrameInput) (ongoing.Result, error)
}

type ongoingFrameProvider func() ongoing.FrameInput

type ongoingTranscriptRuntimeAdmission func(*transcriptpb.Message) (runtimeTupleMergeResult, error)

type ongoingTranscriptAdmittedStateObserver func(*transcriptpb.Message, runtimeTupleMergeResult) tea.Cmd

type ongoingTranscriptController struct {
	surface          ongoingTranscriptSurface
	frameProvider    ongoingFrameProvider
	runtimeAdmission ongoingTranscriptRuntimeAdmission
	stateObserver    ongoingTranscriptAdmittedStateObserver
	normalOwned      bool
	hydrated         bool
	lastSequence     uint64
	queue            []*transcriptpb.Message
	queueOverflowed  bool
	renderPending    bool
	liveReadModel    ongoingTranscriptReadModel
}

func newOngoingTranscriptController(
	surface ongoingTranscriptSurface,
	frameProvider ongoingFrameProvider,
	runtimeAdmission ongoingTranscriptRuntimeAdmission,
	stateObserver ongoingTranscriptAdmittedStateObserver,
) *ongoingTranscriptController {
	if frameProvider == nil {
		panic("ongoing transcript controller requires frame provider")
	}
	if runtimeAdmission == nil {
		panic("ongoing transcript controller requires runtime admission")
	}
	if stateObserver == nil {
		panic("ongoing transcript controller requires state observer")
	}
	return &ongoingTranscriptController{
		surface:          surface,
		frameProvider:    frameProvider,
		runtimeAdmission: runtimeAdmission,
		stateObserver:    stateObserver,
		normalOwned:      true,
		liveReadModel:    newOngoingTranscriptReadModel(),
	}
}

func (c *ongoingTranscriptController) Accept(message *transcriptpb.Message) (ongoing.Result, tea.Cmd, error) {
	if err := protoapi.Validate(message); err != nil {
		return ongoing.Result{}, nil, fmt.Errorf("validate ongoing transcript message: %w", err)
	}
	if result, ok := c.classifyDelivery(message); ok {
		c.requestScratchRehydration()
		return result, nil, nil
	}
	admission, err := c.runtimeAdmission(message)
	if err != nil {
		return ongoing.Result{}, nil, c.diagnoseRuntimeAdmissionError(message, err)
	}
	c.commitDelivery(message)
	stateChanged := c.applyState(message)
	stateCmd := c.stateObserver(message, admission)
	if !c.normalOwned {
		if !isAppOwnedOngoingMessage(message.Event) {
			c.enqueue(message)
		}
		c.renderPending = c.renderPending || stateChanged
		return ongoing.Result{}, stateCmd, nil
	}
	result, err := c.applyNow(message, stateChanged)
	return result, stateCmd, err
}

func (c *ongoingTranscriptController) diagnoseRuntimeAdmissionError(
	message *transcriptpb.Message,
	err error,
) error {
	var conflict hydrationRuntimeTupleConflictError
	if !errors.As(err, &conflict) {
		return err
	}
	frame := c.frameProvider()
	facts := conflict.facts()
	facts["terminal_size"] = frame.Size
	facts["terminal_cursor"] = frame.Cursor
	hydration := message.Event.GetHydration()
	facts["quoted_payload"] = strconv.Quote(fmt.Sprintf("%+v", hydration))
	return ongoing.NewDeveloperError(
		"admit_transcript_hydration_runtime_tuple",
		conflict.Error(),
		facts,
	)
}

func (c *ongoingTranscriptController) SetNormalBufferOwned(owned bool) (ongoing.Result, error) {
	if c.normalOwned == owned {
		return ongoing.Result{}, nil
	}
	c.normalOwned = owned
	if !owned {
		return ongoing.Result{}, nil
	}
	if c.queueOverflowed {
		c.queue = nil
		c.queueOverflowed = false
		c.renderPending = false
		return ongoing.Result{Action: ongoing.ResultRequestScratchRehydration, Reason: ongoing.RehydrateReasonQueueOverflow}, nil
	}
	return c.drainQueued()
}

func (c *ongoingTranscriptController) ResetForScratchHydration() {
	c.queue = nil
	c.queueOverflowed = false
	c.hydrated = false
	c.lastSequence = 0
	c.renderPending = false
	c.liveReadModel.reset()
}

func (c *ongoingTranscriptController) Render() (ongoing.Result, error) {
	if c == nil || c.surface == nil {
		return ongoing.Result{}, nil
	}
	return c.surface.Render(c.frameInput())
}

func (c *ongoingTranscriptController) Resize(size ongoing.Size) (ongoing.Result, error) {
	if c == nil || c.surface == nil {
		return ongoing.Result{}, nil
	}
	return c.surface.Resize(size, c.frameInput())
}

func (c *ongoingTranscriptController) HandleSubscriptionLoss() ongoing.Result {
	c.requestScratchRehydration()
	return ongoing.Result{Action: ongoing.ResultRequestScratchRehydration, Reason: ongoing.RehydrateReasonSequenceGap}
}

func (c *ongoingTranscriptController) classifyDelivery(message *transcriptpb.Message) (ongoing.Result, bool) {
	if !c.hydrated {
		if message.Sequence != 1 || message.Event.GetHydration() == nil {
			return ongoing.Result{Action: ongoing.ResultRequestScratchRehydration, Reason: ongoing.RehydrateReasonSequenceGap}, true
		}
		return ongoing.Result{}, false
	}
	if message.Sequence != c.lastSequence+1 {
		return ongoing.Result{Action: ongoing.ResultRequestScratchRehydration, Reason: ongoing.RehydrateReasonSequenceGap}, true
	}
	if message.Event.GetHydration() != nil {
		return ongoing.Result{Action: ongoing.ResultRequestScratchRehydration, Reason: ongoing.RehydrateReasonSequenceGap}, true
	}
	return ongoing.Result{}, false
}

func (c *ongoingTranscriptController) commitDelivery(message *transcriptpb.Message) {
	if !c.hydrated {
		c.hydrated = true
	}
	c.lastSequence = message.Sequence
}

func (c *ongoingTranscriptController) acceptedHydration(message *transcriptpb.Message) bool {
	return message.Event.GetHydration() != nil &&
		c.hydrated &&
		c.lastSequence == message.Sequence
}

func (c *ongoingTranscriptController) requestScratchRehydration() {
	c.queue = nil
	c.queueOverflowed = true
	c.hydrated = false
	c.lastSequence = 0
	c.renderPending = false
	c.liveReadModel.reset()
}

func (c *ongoingTranscriptController) enqueue(message *transcriptpb.Message) {
	if c.queueOverflowed {
		return
	}
	if len(c.queue) >= ongoingTranscriptQueueLimit {
		c.queue = nil
		c.queueOverflowed = true
		return
	}
	c.queue = append(c.queue, message)
}

func (c *ongoingTranscriptController) drainQueued() (ongoing.Result, error) {
	queued := c.queue
	c.queue = nil
	needsRender := c.renderPending
	c.renderPending = false
	for _, message := range queued {
		if _, err := c.surface.ApplyTerminalMessage(message, c.frameInput()); err != nil {
			return ongoing.Result{}, err
		}
	}
	if needsRender {
		return c.surface.Render(c.frameInput())
	}
	return ongoing.Result{}, nil
}

func (c *ongoingTranscriptController) applyNow(message *transcriptpb.Message, stateChanged bool) (ongoing.Result, error) {
	if isAppOwnedOngoingMessage(message.Event) {
		if stateChanged {
			return c.surface.Render(c.frameInput())
		}
		return ongoing.Result{}, nil
	}
	if message.Event.GetHydration() != nil {
		hydration := message.Event.GetHydration()
		result, err := c.surface.ApplyTerminalMessage(message, c.frameInput())
		if err != nil {
			return ongoing.Result{}, err
		}
		if stateChanged && hydrationHasNoTerminalRows(hydration) {
			return c.surface.Render(c.frameInput())
		}
		return result, nil
	}
	return c.surface.ApplyTerminalMessage(message, c.frameInput())
}

func (c *ongoingTranscriptController) applyState(message *transcriptpb.Message) bool {
	if message.Event.GetHydration() != nil {
		hydration := message.Event.GetHydration()
		return c.applyHydrationAppOwnedFacts(hydration)
	}
	if message.Event.GetCommittedRow() != nil {
		row := message.Event.GetCommittedRow()
		if tool := row.GetTool(); tool != nil {
			c.liveReadModel.removePendingTool(tool.GetToolCallId())
			return true
		}
	}
	if !isAppOwnedOngoingMessage(message.Event) {
		return false
	}
	return c.applyAppOwnedMessage(message)
}

func (c *ongoingTranscriptController) applyAppOwnedMessage(message *transcriptpb.Message) bool {
	switch message.Event.Payload.(type) {
	case *transcriptpb.Event_StepState,
		*transcriptpb.Event_RuntimeReadModelUpdate:
		// Canonical runtime state is represented by the app status line.
	case *transcriptpb.Event_UserMessageFlushed:
		// The state observer owns input reconciliation. Re-render the client-local
		// queue after it removes the flushed operation identities.
	case *transcriptpb.Event_QueuedMessageState:
		// Queue lifecycle is reconciled by the state observer. Pending Work is
		// the sole server membership projection.
	case *transcriptpb.Event_PendingWorkChanged:
		// The hydration-scoped Pending Work refresh owner handles invalidation.
	case *transcriptpb.Event_PendingWorkRestored:
		// Composer restoration is owned by the transcript state observer.
		return false
	case *transcriptpb.Event_SessionSettingFeedback:
		// Typed setting feedback is owned by the app transient-status surface.
		return false
	case *transcriptpb.Event_SessionStatus:
		// Session status is already represented by the app status line.
	case *transcriptpb.Event_SessionIdentity:
		// Session identity is already represented by the app status line.
	case *transcriptpb.Event_CompactionStatus:
		// Compaction status is already represented by the app status line.
	case *transcriptpb.Event_Prompt:
		payload := message.Event.GetPrompt()
		c.liveReadModel.applyPendingPrompt(payload)
		return true
	case *transcriptpb.Event_ContextUsage:
		// Context usage is already represented by the app status line.
	case *transcriptpb.Event_GoalStatus:
		// Goal status is already represented by the app status line.
	case *transcriptpb.Event_BackgroundActivity:
		// Backgrounding ends the mutable foreground tool lifecycle. The immutable
		// tool row and later completion notice own its ongoing presentation.
	case *transcriptpb.Event_ToolStart:
		payload := message.Event.GetToolStart()
		c.liveReadModel.addPendingTool(payload)
		return true
	case *transcriptpb.Event_ToolAbort:
		payload := message.Event.GetToolAbort()
		c.liveReadModel.removePendingTool(string(payload.ToolCallId))
		return true
	default:
		panic(fmt.Sprintf("unsupported app-owned transcript message kind %q", message.Event))
	}
	return true
}

func (c *ongoingTranscriptController) applyHydrationAppOwnedFacts(hydration *transcriptpb.Hydration) bool {
	if hydration == nil {
		return false
	}
	changed := false
	if len(hydration.InFlightTools) > 0 {
		for _, tool := range hydration.InFlightTools {
			c.liveReadModel.addPendingTool(tool)
		}
		changed = true
	}
	if len(hydration.PendingPrompts) > 0 {
		for _, prompt := range hydration.PendingPrompts {
			c.liveReadModel.applyPendingPrompt(prompt)
		}
		changed = true
	}
	return changed
}

func hydrationHasNoTerminalRows(hydration *transcriptpb.Hydration) bool {
	if hydration == nil {
		return true
	}
	if hydration.ActiveAssistant != nil {
		return false
	}
	for _, row := range hydration.TailSegment.Entries {
		switch row.Visibility {
		case transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING, transcriptpb.EntryVisibility_ENTRY_VISIBILITY_ONGOING_COLLAPSED:
			return false
		}
	}
	return true
}

func (c *ongoingTranscriptController) frameInput() ongoing.FrameInput {
	frame := c.frameProvider()
	c.liveReadModel.refreshPendingToolSection(frame.Size.Width, frame.SpinnerFrame, frame.Theme)
	c.liveReadModel.refreshPendingPromptSection(frame.Size.Width)
	cursorSectionRow, cursorTargetsInput := ongoingFrameInputCursorSectionRow(frame)
	sections := make([]ongoing.FrameSection, 0, len(c.liveReadModel.sectionOrder))
	for _, kind := range c.liveReadModel.sectionOrder {
		sections = append(sections, c.liveReadModel.sections[kind])
	}
	frame.Sections = append(sections, frame.Sections...)
	if cursorTargetsInput {
		frame.Cursor.Row = ongoingFrameInputCursorTerminalRow(frame, cursorSectionRow)
	}
	return frame
}

func queuedOrSteeredText(state *transcriptpb.QueuedMessageState) string {
	if state == nil {
		return ""
	}
	if state.Text != nil && strings.TrimSpace(*state.Text) != "" {
		return *state.Text
	}
	failureReason := ""
	if state.FailureReason != nil {
		switch *state.FailureReason {
		case transcriptpb.QueuedMessageFailureReason_QUEUED_MESSAGE_FAILURE_REASON_CLOSING:
			failureReason = "closing"
		case transcriptpb.QueuedMessageFailureReason_QUEUED_MESSAGE_FAILURE_REASON_TERMINAL_WORKFLOW_COMPLETION:
			failureReason = "terminal_workflow_completion"
		case transcriptpb.QueuedMessageFailureReason_QUEUED_MESSAGE_FAILURE_REASON_RUNTIME_UNAVAILABLE:
			failureReason = "runtime_unavailable"
		case transcriptpb.QueuedMessageFailureReason_QUEUED_MESSAGE_FAILURE_REASON_STOPPED:
			failureReason = "stopped"
		default:
			panic("invalid queued message failure reason")
		}
	}
	status := ""
	switch state.Status {
	case transcriptpb.QueuedMessageStatus_QUEUED_MESSAGE_STATUS_ACCEPTED:
		status = "accepted"
	case transcriptpb.QueuedMessageStatus_QUEUED_MESSAGE_STATUS_SUBMITTED:
		status = "submitted"
	case transcriptpb.QueuedMessageStatus_QUEUED_MESSAGE_STATUS_FAILED:
		status = "failed"
	case transcriptpb.QueuedMessageStatus_QUEUED_MESSAGE_STATUS_DISCARDED:
		status = "discarded"
	default:
		panic("invalid queued message status")
	}
	lines := joinFacts(compactNonEmptyStrings(humanizeTranscriptFact(status), humanizeTranscriptFact(failureReason)))
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

func pendingPromptLines(prompt *transcriptpb.Prompt) []string {
	if prompt == nil || prompt.Status != transcriptpb.PromptStatus_PROMPT_STATUS_PENDING {
		return nil
	}
	return joinFacts(compactNonEmptyStrings(transcriptPromptQuestion(prompt)))
}

func pendingPromptListLines(prompts []*transcriptpb.Prompt) []string {
	lines := make([]string, 0, len(prompts))
	for _, prompt := range prompts {
		lines = append(lines, pendingPromptLines(prompt)...)
	}
	return lines
}

func compactNonEmptyStrings(values ...string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func joinFacts(parts []string) []string {
	if len(parts) == 0 {
		return nil
	}
	return []string{strings.Join(parts, " · ")}
}

func terminalSafeFrameLines(lines []string) []string {
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if safe := ongoing.TerminalSafeSingleLine(line); safe != "" {
			out = append(out, safe)
		}
	}
	return out
}

func terminalSafeFrameLinesForWidth(lines []string, width int) []string {
	if width <= 0 {
		width = 80
	}
	out := make([]string, 0, len(lines))
	for _, line := range terminalSafeFrameLines(lines) {
		truncated := transcriptrender.TruncateLine(transcriptrender.Line{
			Spans: []transcriptrender.Span{transcriptrender.SemanticSpan(line, transcriptrender.StyleRoleNotice)},
		}, width, false).Plain()
		if truncated != "" {
			out = append(out, truncated)
		}
	}
	return out
}

func humanizeTranscriptFact(value string) string {
	return strings.ReplaceAll(strings.TrimSpace(value), "_", " ")
}

func isAppOwnedOngoingMessage(event *transcriptpb.Event) bool {
	switch event.Payload.(type) {
	case *transcriptpb.Event_StepState,
		*transcriptpb.Event_RuntimeReadModelUpdate,
		*transcriptpb.Event_QueuedMessageState,
		*transcriptpb.Event_PendingWorkChanged,
		*transcriptpb.Event_PendingWorkRestored,
		*transcriptpb.Event_SessionSettingFeedback,
		*transcriptpb.Event_UserMessageFlushed,
		*transcriptpb.Event_SessionStatus,
		*transcriptpb.Event_SessionIdentity,
		*transcriptpb.Event_CompactionStatus,
		*transcriptpb.Event_ContextUsage,
		*transcriptpb.Event_GoalStatus,
		*transcriptpb.Event_BackgroundActivity,
		*transcriptpb.Event_Prompt,
		*transcriptpb.Event_ToolStart,
		*transcriptpb.Event_ToolAbort:
		return true
	default:
		return false
	}
}
