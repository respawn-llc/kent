package app

import (
	"builder/cli/tui"
	"builder/shared/clientui"
	"errors"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type uiAskController struct {
	model *uiModel
}

type askPromptLineKind int

const (
	askPromptLineKindQuestion askPromptLineKind = iota
	askPromptLineKindOption
	askPromptLineKindHint
	askPromptLineKindInput
)

type askPromptLine struct {
	Text        string
	Kind        askPromptLineKind
	Selected    bool
	Recommended bool
	MutedSuffix string
	Disabled    bool
	InputPrefix string
	InputText   string
	InputCursor int
	ShowsCursor bool
}

type askFreeformMode int

const (
	askFreeformModeGeneric askFreeformMode = iota
	askFreeformModeApprovalCommentary
)

const askFreeformSelectionOptionText = "Freeform answer"

func (c uiAskController) acceptEvent(evt askEvent) {
	m := c.model
	if evt.isResolution() {
		c.resolvePrompt(evt.promptID())
		return
	}
	if !m.ask.hasCurrent() {
		c.setActiveAsk(evt)
		m.activity = uiActivityQuestion
		if m.inputMode() == uiInputModeMain && (m.view.Mode() == "" || m.view.Mode() == tui.ModeOngoing) {
			m.setInputMode(uiInputModeAsk)
		}
		return
	}
	m.ask.queue = append(m.ask.queue, evt)
}

func (c uiAskController) resolvePrompt(promptID string) {
	m := c.model
	targetID := strings.TrimSpace(promptID)
	if targetID == "" {
		return
	}
	filteredQueue := m.ask.queue[:0]
	for _, queued := range m.ask.queue {
		if strings.TrimSpace(queued.req.PromptID) == targetID {
			queued.cancelPending()
			continue
		}
		filteredQueue = append(filteredQueue, queued)
	}
	m.ask.queue = filteredQueue
	if !m.ask.hasCurrent() || strings.TrimSpace(m.ask.current.req.PromptID) != targetID {
		return
	}
	m.ask.current.cancelPending()
	if len(m.ask.queue) > 0 {
		next := m.ask.queue[0]
		m.ask.queue = m.ask.queue[1:]
		c.setActiveAsk(next)
		m.activity = uiActivityQuestion
		m.setInputMode(uiInputModeAsk)
		return
	}
	m.ask.current = nil
	m.ask.currentToken = nextNonZeroToken(m.ask.currentToken)
	m.ask.cursor = 0
	m.clearAskInput()
	m.ask.freeform = false
	m.ask.freeformMode = askFreeformModeGeneric
	m.restorePrimaryInputMode()
	if m.activity == uiActivityQuestion {
		if m.busy {
			m.activity = uiActivityRunning
		} else {
			m.activity = uiActivityIdle
		}
	}
}

func (c uiAskController) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := c.model
	if !m.ask.hasCurrent() {
		return m, nil
	}
	if msg.Type != tea.KeyEnter && msg.Type != keyTypeShiftEnterCSI {
		m.inputController().clearPendingCSIShiftEnter()
	}
	req := m.ask.current.req
	if m.ask.freeform && isClipboardImagePasteKey(msg) {
		return m, m.pasteClipboardImageCmd(uiClipboardPasteTargetAsk)
	}
	if m.ask.freeform && handleSharedInputEditKey(msg, uiSharedInputEditActions{
		Backspace:          m.backspaceAskInput,
		DeleteForward:      m.deleteForwardAskInput,
		DeleteBackwardWord: m.deleteBackwardWordAskInput,
		DeleteForwardWord:  m.deleteForwardWordAskInput,
		KillToLineStart:    m.killAskInputToLineStart,
		KillToLineEnd:      m.killAskInputToLineEnd,
		Yank:               m.yankAskInput,
		DeleteCurrentLine:  m.deleteCurrentAskInputLine,
	}) {
		return m, nil
	}

	switch msg.Type {
	case tea.KeyCtrlC:
		hasNext := c.answer(clientui.PromptAnswer{}, errors.New("interrupted"))
		if m.busy {
			_ = m.interruptRuntime()
			m.busy = false
		}
		if hasNext {
			m.activity = uiActivityQuestion
		} else {
			m.activity = uiActivityInterrupted
		}
		return m, nil
	case tea.KeyEsc:
		hasNext := c.answer(clientui.PromptAnswer{}, errors.New("question canceled"))
		if hasNext {
			m.activity = uiActivityQuestion
		} else {
			m.activity = uiActivityIdle
		}
		return m, nil
	case tea.KeyTab:
		if m.ask.freeform {
			if !askSupportsDraftRoundTrip(req) {
				return m, nil
			}
			m.ask.freeform = false
			return m, nil
		}
		m.ask.freeform = true
		if approvalSupportsCommentary(req) {
			m.ask.freeformMode = askFreeformModeApprovalCommentary
			m.clearAskInput()
		}
		return m, nil
	case tea.KeyEnter:
		m.inputController().normalizePendingCSIShiftEnterOnEnter()
		if m.ask.freeform {
			commentary := strings.TrimSpace(m.ask.input)
			if askRequiresFreeformSelectionCommentary(req, m.ask.cursor) && commentary == "" {
				return m, c.showFreeformSelectionCommentaryRequiredError()
			}
			resp := clientui.PromptAnswer{Answer: commentary, FreeformAnswer: commentary}
			if optionNumber, ok := selectedAskOptionNumber(req, m.ask.cursor); ok {
				resp.SelectedOptionNumber = optionNumber
			}
			if req.Approval {
				if m.ask.freeformMode == askFreeformModeApprovalCommentary {
					decision, ok := selectedApprovalDecision(req, m.ask.cursor)
					if !ok {
						return m, nil
					}
					if commentary != "" {
						m.enqueueInjectedInput(commentary)
					}
					resp = clientui.PromptAnswer{Approval: &clientui.ApprovalPromptAnswer{Decision: decision, Commentary: commentary}}
				}
			}
			hasNext := c.answer(resp, nil)
			if hasNext {
				m.activity = uiActivityQuestion
			} else {
				m.activity = uiActivityRunning
			}
			return m, nil
		}
		optionCount := askOptionCount(req)
		if optionCount == 0 {
			m.ask.freeform = true
			return m, nil
		}
		if askHasFreeformSelectionOption(req) && m.ask.cursor == len(askVisibleOptions(req)) {
			commentary := strings.TrimSpace(m.ask.input)
			if commentary == "" {
				m.ask.freeform = true
				return m, nil
			}
			hasNext := c.answer(clientui.PromptAnswer{Answer: commentary, FreeformAnswer: commentary}, nil)
			if hasNext {
				m.activity = uiActivityQuestion
			} else {
				m.activity = uiActivityRunning
			}
			return m, nil
		}
		visibleOptions := askVisibleOptions(req)
		if m.ask.cursor < 0 || m.ask.cursor >= len(visibleOptions) {
			m.ask.freeform = true
			m.clearAskInput()
			return m, nil
		}
		resp := clientui.PromptAnswer{SelectedOptionNumber: m.ask.cursor + 1}
		if commentary := strings.TrimSpace(m.ask.input); askSupportsDraftRoundTrip(req) && commentary != "" {
			resp.FreeformAnswer = commentary
		}
		if req.Approval && m.ask.cursor < len(req.ApprovalOptions) {
			resp = clientui.PromptAnswer{Approval: &clientui.ApprovalPromptAnswer{Decision: req.ApprovalOptions[m.ask.cursor].Decision}}
		}
		hasNext := c.answer(resp, nil)
		if hasNext {
			m.activity = uiActivityQuestion
		} else {
			m.activity = uiActivityRunning
		}
		return m, nil
	case tea.KeyUp:
		if m.ask.freeform {
			m.moveAskCursorUpLine()
			return m, nil
		}
		if m.ask.cursor > 0 {
			m.ask.cursor--
		}
		return m, nil
	case tea.KeyDown:
		if m.ask.freeform {
			m.moveAskCursorDownLine()
			return m, nil
		}
		maxIdx := askOptionCount(req) - 1
		if maxIdx >= 0 && m.ask.cursor < maxIdx {
			m.ask.cursor++
		}
		return m, nil
	case tea.KeyCtrlJ, keyTypeShiftEnterCSI:
		if !m.ask.freeform {
			return m, nil
		}
		m.insertAskInputRunes([]rune{'\n'})
		if msg.Type == keyTypeShiftEnterCSI {
			m.inputController().markPendingCSIShiftEnter()
		}
		return m, nil
	case tea.KeySpace:
		if m.ask.freeform {
			m.insertAskInputRunes([]rune{' '})
		}
		return m, nil
	case tea.KeyLeft:
		if !m.ask.freeform {
			return m, nil
		}
		if msg.Alt {
			m.moveAskCursorWordLeft()
			return m, nil
		}
		m.moveAskCursorLeft()
		return m, nil
	case tea.KeyRight:
		if !m.ask.freeform {
			return m, nil
		}
		if msg.Alt {
			m.moveAskCursorWordRight()
			return m, nil
		}
		m.moveAskCursorRight()
		return m, nil
	case tea.KeyHome, tea.KeyCtrlA:
		if m.ask.freeform {
			m.moveAskCursorStart()
		}
		return m, nil
	case tea.KeyEnd, tea.KeyCtrlE, tea.KeyCtrlEnd:
		if m.ask.freeform {
			m.moveAskCursorEnd()
		}
		return m, nil
	case tea.KeyCtrlLeft:
		if m.ask.freeform {
			m.moveAskCursorWordLeft()
		}
		return m, nil
	case tea.KeyCtrlRight:
		if m.ask.freeform {
			m.moveAskCursorWordRight()
		}
		return m, nil
	default:
		if isShiftEnterKey(msg) {
			if !m.ask.freeform {
				return m, nil
			}
			m.insertAskInputRunes([]rune{'\n'})
			return m, nil
		}
		if m.ask.freeform && msg.Type == tea.KeyRunes {
			m.insertAskInputRunes(msg.Runes)
			return m, nil
		}
		return m, nil
	}
}

func (c uiAskController) renderPrompt() string {
	lines := c.renderPromptLines()
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		out = append(out, line.Text)
	}
	return strings.Join(out, "\n")
}

func (c uiAskController) renderPromptLines() []askPromptLine {
	m := c.model
	if !m.ask.hasCurrent() {
		return nil
	}
	req := m.ask.current.req
	if isApprovalCommentaryPrompt(req, m.ask.freeform, m.ask.freeformMode) {
		return []askPromptLine{
			{Text: approvalCommentaryLabel(req, m.ask.cursor), Kind: askPromptLineKindHint},
			{Kind: askPromptLineKindInput, InputPrefix: m.askInputPrefix(), InputText: m.ask.input, InputCursor: m.ask.inputCursor, ShowsCursor: true},
		}
	}
	lines := askQuestionPromptTextLines(req.Question)
	if askOptionCount(req) > 0 && !m.ask.freeform {
		visibleOptions := askVisibleOptions(req)
		for i, s := range visibleOptions {
			selected := i == m.ask.cursor
			recommended := askOptionIsRecommended(req, i)
			marker := "  "
			if selected {
				marker = "✔︎ "
			} else if recommended {
				marker = "★ "
			}
			text := fmt.Sprintf("%s%d. %s", marker, i+1, s)
			mutedSuffix := ""
			if recommended {
				mutedSuffix = " • recommended"
				text += mutedSuffix
			}
			lines = append(lines, askPromptLine{Text: text, Kind: askPromptLineKindOption, Selected: selected, Recommended: recommended, MutedSuffix: mutedSuffix})
		}
		if askHasFreeformSelectionOption(req) {
			idx := len(visibleOptions) + 1
			selected := m.ask.cursor == len(visibleOptions)
			prefix := "  "
			if selected {
				prefix = "✔︎ "
			}
			lines = append(lines, askPromptLine{Text: fmt.Sprintf("%s%d. %s", prefix, idx, askFreeformSelectionOptionText), Kind: askPromptLineKindOption, Selected: selected})
		}
		if askSupportsDraftRoundTrip(req) && askHasPendingFreeformDraft(m.ask.input) {
			lines = append(lines, askPromptLine{Kind: askPromptLineKindInput, Disabled: true, InputPrefix: m.askInputPrefix(), InputText: m.ask.input, InputCursor: m.ask.inputCursor, ShowsCursor: false})
			return lines
		}
		hint := "Tab to add commentary • Enter to submit"
		if approvalSupportsCommentary(req) {
			hint = "Tab to add commentary • Enter to submit"
		}
		lines = append(lines, askPromptLine{Text: hint, Kind: askPromptLineKindHint})
		return lines
	}

	inputLabel := ""
	if isApprovalCommentaryPrompt(req, m.ask.freeform, m.ask.freeformMode) {
		inputLabel = approvalCommentaryLabel(req, m.ask.cursor)
	}
	if inputLabel != "" {
		lines = append(lines, askPromptLine{Text: inputLabel, Kind: askPromptLineKindHint})
	}
	lines = append(lines, askPromptLine{Kind: askPromptLineKindInput, InputPrefix: m.askInputPrefix(), InputText: m.ask.input, InputCursor: m.ask.inputCursor, ShowsCursor: true})
	hint := "Enter to submit"
	if askSupportsDraftRoundTrip(req) {
		hint = "Tab to return to picker • Enter to submit"
	}
	lines = append(lines, askPromptLine{Text: hint, Kind: askPromptLineKindHint})
	return lines
}

func askQuestionPromptTextLines(question string) []askPromptLine {
	normalized := strings.ReplaceAll(strings.ReplaceAll(question, "\r\n", "\n"), "\r", "\n")
	if strings.TrimSpace(normalized) == "" {
		return []askPromptLine{{Text: "", Kind: askPromptLineKindQuestion}}
	}
	parts := strings.Split(normalized, "\n")
	lines := make([]askPromptLine, 0, len(parts))
	for _, part := range parts {
		lines = append(lines, askPromptLine{Text: part, Kind: askPromptLineKindQuestion})
	}
	return lines
}

func (c uiAskController) answer(resp clientui.PromptAnswer, err error) bool {
	m := c.model
	if !m.ask.hasCurrent() {
		return false
	}
	if resp.PromptID == "" {
		resp.PromptID = m.ask.current.req.PromptID
	}
	m.ask.current.reply <- askReply{response: resp, err: err}
	if len(m.ask.queue) == 0 {
		m.ask.current = nil
		m.ask.currentToken = nextNonZeroToken(m.ask.currentToken)
		m.ask.cursor = 0
		m.clearAskInput()
		m.ask.freeform = false
		m.ask.freeformMode = askFreeformModeGeneric
		m.restorePrimaryInputMode()
		return false
	}
	next := m.ask.queue[0]
	m.ask.queue = m.ask.queue[1:]
	c.setActiveAsk(next)
	m.setInputMode(uiInputModeAsk)
	return true
}

func (c uiAskController) setActiveAsk(evt askEvent) {
	m := c.model
	current := evt
	m.ask.currentToken = nextNonZeroToken(m.ask.currentToken)
	m.ask.current = &current
	m.ask.cursor = 0
	m.clearAskInput()
	m.ask.freeform = askOptionCount(current.req) == 0
	m.ask.freeformMode = askFreeformModeGeneric
}

func (m *uiModel) askInputPrefix() string {
	return "› "
}

func askVisibleOptions(req clientui.PendingPromptEvent) []string {
	if req.Approval && len(req.ApprovalOptions) > 0 {
		out := make([]string, 0, len(req.ApprovalOptions))
		for _, option := range req.ApprovalOptions {
			out = append(out, option.Label)
		}
		return out
	}
	return req.Suggestions
}

func approvalOptionIndex(req clientui.PendingPromptEvent, decision clientui.ApprovalDecision) int {
	for i, option := range req.ApprovalOptions {
		if option.Decision == decision {
			return i
		}
	}
	return -1
}

func approvalSupportsCommentary(req clientui.PendingPromptEvent) bool {
	if !req.Approval {
		return false
	}
	return len(askVisibleOptions(req)) > 0
}

func askHasFreeformSelectionOption(req clientui.PendingPromptEvent) bool {
	if req.Approval {
		return false
	}
	return len(askVisibleOptions(req)) > 0
}

func askOptionCount(req clientui.PendingPromptEvent) int {
	count := len(askVisibleOptions(req))
	if askHasFreeformSelectionOption(req) {
		count++
	}
	return count
}

func isApprovalCommentaryPrompt(req clientui.PendingPromptEvent, freeform bool, mode askFreeformMode) bool {
	if !freeform || mode != askFreeformModeApprovalCommentary {
		return false
	}
	return req.Approval
}

func selectedApprovalDecision(req clientui.PendingPromptEvent, cursor int) (clientui.ApprovalDecision, bool) {
	if !req.Approval || cursor < 0 || cursor >= len(req.ApprovalOptions) {
		return "", false
	}
	return req.ApprovalOptions[cursor].Decision, true
}

func approvalCommentaryLabel(req clientui.PendingPromptEvent, cursor int) string {
	if !req.Approval || cursor < 0 || cursor >= len(req.ApprovalOptions) {
		return "Commentary:"
	}
	return fmt.Sprintf("Commentary for %s:", req.ApprovalOptions[cursor].Label)
}

func selectedAskOptionNumber(req clientui.PendingPromptEvent, cursor int) (int, bool) {
	if req.Approval {
		return 0, false
	}
	visibleOptions := askVisibleOptions(req)
	if cursor < 0 || cursor >= len(visibleOptions) {
		return 0, false
	}
	return cursor + 1, true
}

func askOptionIsRecommended(req clientui.PendingPromptEvent, index int) bool {
	if req.Approval {
		return false
	}
	return req.RecommendedOptionIndex == index+1
}

func askRequiresFreeformSelectionCommentary(req clientui.PendingPromptEvent, cursor int) bool {
	if !askHasFreeformSelectionOption(req) {
		return false
	}
	return cursor == len(askVisibleOptions(req))
}

func askHasPendingFreeformDraft(input string) bool {
	return strings.TrimSpace(input) != ""
}

func askSupportsDraftRoundTrip(req clientui.PendingPromptEvent) bool {
	return !req.Approval && len(askVisibleOptions(req)) > 0
}

func (c uiAskController) showFreeformSelectionCommentaryRequiredError() tea.Cmd {
	return sequenceCmds(
		c.model.setTransientStatusWithKind("Write your response before submitting the freeform option", uiStatusNoticeError),
		ringBellCmd(),
	)
}

func (m *uiModel) renderAskPrompt() string {
	return m.askController().renderPrompt()
}

func (m *uiModel) renderAskPromptLines() []askPromptLine {
	return m.askController().renderPromptLines()
}

func (m *uiModel) answerAsk(answer string, err error) bool {
	return m.askController().answer(clientui.PromptAnswer{Answer: answer}, err)
}

func (m *uiModel) setActiveAsk(evt askEvent) {
	m.askController().setActiveAsk(evt)
}
