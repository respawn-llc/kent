package app

import (
	"io"
	"os"
	"strings"
	"sync"

	"core/cli/tui/transcriptrender"
	"core/shared/clientui"

	xansi "github.com/charmbracelet/x/ansi"
)

const terminalBell = "\a"
const osc9Prefix = "\x1b]9;"
const terminalNotificationPreviewLimit = 80

const (
	notificationMethodAuto = "auto"
	notificationMethodOSC9 = "osc9"
	notificationMethodBEL  = "bel"
)

type terminalNotifier interface {
	Bell()
	Notify(message string)
}

type belTerminalNotifier struct {
	mu  sync.Mutex
	out io.Writer
}

type osc9TerminalNotifier struct {
	mu  sync.Mutex
	out io.Writer
}

func newBELTerminalNotifier(out io.Writer) *belTerminalNotifier {
	if out == nil {
		out = io.Discard
	}
	return &belTerminalNotifier{out: out}
}

func newOSC9TerminalNotifier(out io.Writer) *osc9TerminalNotifier {
	if out == nil {
		out = io.Discard
	}
	return &osc9TerminalNotifier{out: out}
}

func newTerminalNotifier(method string, out io.Writer, lookup func(string) (string, bool)) terminalNotifier {
	normalized := strings.ToLower(strings.TrimSpace(method))
	if normalized == "" {
		normalized = notificationMethodAuto
	}
	switch normalized {
	case notificationMethodOSC9:
		return newOSC9TerminalNotifier(out)
	case notificationMethodBEL:
		return newBELTerminalNotifier(out)
	default:
		if supportsOSC9(lookup) {
			return newOSC9TerminalNotifier(out)
		}
		return newBELTerminalNotifier(out)
	}
}

func supportsOSC9(lookup func(string) (string, bool)) bool {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	if _, ok := lookup("WT_SESSION"); ok {
		return false
	}
	if termProgram, ok := lookup("TERM_PROGRAM"); ok {
		switch termProgram {
		case "WezTerm", "ghostty":
			return true
		}
	}
	if _, ok := lookup("ITERM_SESSION_ID"); ok {
		return true
	}
	if term, ok := lookup("TERM"); ok {
		switch term {
		case "xterm-kitty", "wezterm", "wezterm-mux":
			return true
		}
	}
	return false
}

func (r *belTerminalNotifier) Notify(_ string) {
	r.Bell()
}

func (r *belTerminalNotifier) Bell() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = io.WriteString(r.out, terminalBell)
}

func (r *osc9TerminalNotifier) Notify(message string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	// The first BEL terminates the OSC 9 sequence. Emit a second BEL so asks and
	// turn-complete notifications still produce an audible bell on OSC-capable terminals.
	_, _ = io.WriteString(r.out, osc9Prefix+message+terminalBell+terminalBell)
}

func (r *osc9TerminalNotifier) Bell() {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = io.WriteString(r.out, terminalBell)
}

type bellHooks struct {
	mu       sync.Mutex
	notifier terminalNotifier
	title    func() string
	focused  func() bool
}

func newBellHooks(notifier terminalNotifier, title func() string, focused ...func() bool) *bellHooks {
	if notifier == nil {
		notifier = newBELTerminalNotifier(io.Discard)
	}
	if title == nil {
		title = func() string { return defaultSessionTitle }
	}
	focusedProvider := func() bool { return false }
	if len(focused) > 0 && focused[0] != nil {
		focusedProvider = focused[0]
	}
	return &bellHooks{notifier: notifier, title: title, focused: focusedProvider}
}

func (h *bellHooks) OnAttentionNotification(evt clientui.AttentionNotificationEvent) {
	h.onAttentionNotification(evt, nil)
}

func (h *bellHooks) OnSessionTurnEnd(content string) {
	h.notifyIfUnfocused(turnCompletionNotificationMessage(content))
}

func (h *bellHooks) onAttentionNotification(evt clientui.AttentionNotificationEvent, projectedBody *string) {
	if h == nil {
		return
	}
	if !tuiAcceptsAttentionNotification(evt) {
		return
	}
	notification := evt.Pending
	body := ""
	if projectedBody != nil {
		body = strings.TrimSpace(*projectedBody)
	} else {
		body = notificationMarkdownPreview(
			attentionNotificationBody(*notification),
			currentTerminalCapabilities().MarkdownLinks,
		)
	}
	if body == "" {
		body = attentionNotificationFallbackBody(*notification)
	}
	message := h.formatMessage(attentionNotificationTitle(*notification) + ": " + body)
	if h.focusedForAttention() {
		h.notifier.Bell()
		return
	}
	h.notifier.Notify(message)
}

func projectedQuestionNotificationPreview(rows []string) string {
	plainRows := make([]string, 0, len(rows))
	for _, row := range rows {
		plainRows = append(plainRows, xansi.Strip(row))
	}
	normalized := terminalNotificationSingleLine(strings.Join(plainRows, " "))
	runes := []rune(normalized)
	if len(runes) <= terminalNotificationPreviewLimit {
		return normalized
	}
	return string(runes[:terminalNotificationPreviewLimit-3]) + "..."
}

func attentionNotificationTitle(notification clientui.AttentionNotification) string {
	if notification.Kind == clientui.AttentionNotificationKindApproval ||
		notification.Kind == clientui.AttentionNotificationKindWorkflowApproval {
		return "Action required"
	}
	return "Question"
}

func attentionNotificationBody(notification clientui.AttentionNotification) string {
	for _, candidate := range []string{
		attentionNotificationQuestionPreview(notification),
		attentionNotificationApprovalMessage(notification),
	} {
		if trimmed := strings.TrimSpace(candidate); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func attentionNotificationFallbackBody(notification clientui.AttentionNotification) string {
	if notification.Kind == clientui.AttentionNotificationKindApproval ||
		notification.Kind == clientui.AttentionNotificationKindWorkflowApproval {
		return "action required"
	}
	return "question from agent"
}

func attentionNotificationQuestionPreview(notification clientui.AttentionNotification) string {
	if notification.Question == nil {
		return ""
	}
	return notification.Question.Preview
}

func attentionNotificationApprovalMessage(notification clientui.AttentionNotification) string {
	if notification.Approval != nil {
		if notification.Approval.Message != nil {
			return *notification.Approval.Message
		}
		return clientui.FormatFileAccessApprovalMarkdown(notification.Approval.AccessTargets)
	}
	if notification.WorkflowApproval != nil {
		return notification.WorkflowApproval.Message
	}
	return ""
}

const compactionCompletionNotificationMessage = "Compaction finished"

func (h *bellHooks) OnUserCompactionCompleted() {
	if h == nil {
		return
	}
	h.notifyIfUnfocused(compactionCompletionNotificationMessage)
}

func turnCompletionNotificationMessage(assistantContent string) string {
	if preview := notificationMarkdownPreview(
		assistantContent,
		currentTerminalCapabilities().MarkdownLinks,
	); preview != "" {
		return preview
	}
	return "turn complete"
}

func notificationMarkdownPreview(
	content string,
	linkPresentation transcriptrender.MarkdownLinkPresentation,
) string {
	lines := transcriptrender.RenderMarkdownStableLinesWithLinkPresentation(
		transcriptrender.StyleRoleAssistant,
		content,
		terminalNotificationPreviewLimit,
		linkPresentation,
	)
	plain := strings.Join(transcriptrender.PlainLines(lines), " ")
	normalized := terminalNotificationSingleLine(plain)
	runes := []rune(normalized)
	if len(runes) <= terminalNotificationPreviewLimit {
		return normalized
	}
	return string(runes[:terminalNotificationPreviewLimit])
}

func (h *bellHooks) formatMessage(message string) string {
	title := defaultSessionTitle
	if h != nil && h.title != nil {
		title = h.title()
	}
	return formatTerminalNotificationMessage(title, message)
}

func formatTerminalNotificationMessage(title, message string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		title = defaultSessionTitle
	}
	composed := terminalNotificationSingleLine(title + ": " + message)
	runes := []rune(composed)
	if len(runes) <= terminalNotificationPreviewLimit {
		return composed
	}
	return string(runes[:terminalNotificationPreviewLimit-3]) + "..."
}

func terminalNotificationSingleLine(text string) string {
	return strings.Join(
		strings.Fields(transcriptrender.TerminalSafeSingleLine(text)),
		" ",
	)
}

func (h *bellHooks) notifyIfUnfocused(message string) {
	if h == nil {
		return
	}
	if h.focusedForAttention() {
		return
	}
	h.notifier.Notify(h.formatMessage(message))
}

func (h *bellHooks) focusedForAttention() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	focusedProvider := h.focused
	h.mu.Unlock()
	return focusedProvider != nil && focusedProvider()
}
