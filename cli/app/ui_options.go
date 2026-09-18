package app

import runtimepb "core/shared/protoapi/gen/kent/api/runtime"

import (
	"fmt"
	"strings"

	"core/cli/app/commands"
	"core/cli/tui"
	"core/cli/tui/transcriptrender"
	"core/shared/apicontract"
	"core/shared/clientui"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"
	"core/shared/textutil"
)

type UIOption func(*uiModel)

type UIAction string

type UITransition struct {
	Action                       UIAction
	Exit                         bool
	InitialPrompt                string
	InitialPromptHistoryRecorded bool
	InitialInput                 *string
	TargetSessionID              string
	ForkRollbackTargetID         string
	PreviousSessionID            *runtimeids.SessionID
	SessionRetargeted            bool
}

const (
	UIActionNone         UIAction = "none"
	UIActionExit         UIAction = "exit"
	UIActionNewSession   UIAction = "new_session"
	UIActionResume       UIAction = "resume"
	UIActionLogout       UIAction = "logout"
	UIActionForkRollback UIAction = "fork_rollback"
	UIActionOpenSession  UIAction = "open_session"
)

func (a UIAction) transitionAction() (sessionlaunchpb.SessionTransitionAction, error) {
	switch a {
	case UIActionNone:
		return sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_NONE, nil
	case UIActionNewSession:
		return sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_NEW_SESSION, nil
	case UIActionResume:
		return sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_RESUME, nil
	case UIActionLogout:
		return sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_LOGOUT, nil
	case UIActionForkRollback:
		return sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_FORK_ROLLBACK, nil
	case UIActionOpenSession:
		return sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_OPEN_SESSION, nil
	default:
		return sessionlaunchpb.SessionTransitionAction_SESSION_TRANSITION_ACTION_UNSPECIFIED, fmt.Errorf("UI action %q has no server transition", a)
	}
}

func WithUILogger(logger uiLogger) UIOption {
	return func(m *uiModel) {
		m.logger = logger
	}
}

func WithUIDebug(enabled bool) UIOption {
	return func(m *uiModel) {
		m.debugMode = enabled
	}
}

func WithUITerminalCursorState(state *uiTerminalCursorState) UIOption {
	return func(m *uiModel) {
		m.terminalCursor = state
	}
}

func WithUIRendererOutputGateState(state *uiRendererOutputGateState) UIOption {
	return func(m *uiModel) {
		m.rendererOutputGate = state
		m.syncRendererOutputGate()
	}
}

func WithUIModelName(model string) UIOption {
	return func(m *uiModel) {
		m.modelName = strings.TrimSpace(model)
	}
}

func WithUIConfiguredModelName(model *string) UIOption {
	return func(m *uiModel) {
		configured, present := textutil.OptionalTrimmed(model)
		if !present {
			m.configuredModelName = nil
			return
		}
		m.configuredModelName = textutil.Value(configured)
	}
}

func WithUIThinkingLevel(thinkingLevel string) UIOption {
	return func(m *uiModel) {
		m.thinkingLevel = strings.TrimSpace(thinkingLevel)
	}
}

func WithUIConversationFreshness(freshness runtimepb.ConversationFreshness) UIOption {
	return func(m *uiModel) {
		m.conversationFreshness = freshness
	}
}

func WithUIModelContractLocked(locked bool) UIOption {
	return func(m *uiModel) {
		m.modelContractLocked = locked
	}
}

func WithUITheme(theme string) UIOption {
	return func(m *uiModel) {
		m.theme = strings.TrimSpace(theme)
		m.rebuildTranscriptView()
	}
}

func WithUINativeProgressBar(enabled bool) UIOption {
	return func(m *uiModel) {
		m.tuiNativeProgressBar = enabled
	}
}

func WithUITerminalOutput(output *uiTerminalOutput) UIOption {
	return func(m *uiModel) {
		m.terminalOutput = output
	}
}

func WithUIMarkdownLinkPresentation(
	linkPresentation transcriptrender.MarkdownLinkPresentation,
) UIOption {
	if !linkPresentation.Valid() {
		panic(fmt.Sprintf("configure UI with invalid Markdown link presentation %d", linkPresentation))
	}
	return func(m *uiModel) {
		m.markdownLinks = linkPresentation
		m.rebuildTranscriptView()
	}
}

func (m *uiModel) rebuildTranscriptView() {
	m.view = tui.NewModel(
		tui.WithTheme(m.theme),
		tui.WithMarkdownLinkPresentation(m.markdownLinks),
	)
}

func WithUICommandRegistry(registry *commands.Registry) UIOption {
	return func(m *uiModel) {
		if registry == nil {
			return
		}
		m.commandRegistry = registry
	}
}

func WithUIPromptCommandCatalog(catalog apicontract.PromptCommandCatalogService) UIOption {
	return func(m *uiModel) {
		m.promptCatalog = catalog
	}
}

func WithUIPromptCommandCatalogEntries(entries []commands.PromptCommandCatalogEntry) UIOption {
	return func(m *uiModel) {
		m.promptCatalogEntries = append([]commands.PromptCommandCatalogEntry(nil), entries...)
	}
}

func WithUIStartupSubmit(text string) UIOption {
	return func(m *uiModel) {
		m.startupSubmit = text
	}
}

func WithUIStartupSubmitPromptHistoryRecorded(recorded bool) UIOption {
	return func(m *uiModel) {
		m.startupSubmitPromptHistoryRecorded = recorded
	}
}

func WithUIInitialInput(text string) UIOption {
	return func(m *uiModel) {
		if text == "" || m.mainEditor.Text() != "" {
			return
		}
		m.replaceMainInputAtEnd(text)
	}
}

func WithUISessionName(name string) UIOption {
	return func(m *uiModel) {
		m.sessionName = strings.TrimSpace(name)
	}
}

func WithUISessionID(sessionID string) UIOption {
	return func(m *uiModel) {
		m.sessionID = strings.TrimSpace(sessionID)
	}
}

func WithUIProcessClient(client clientui.ProcessClient) UIOption {
	return func(m *uiModel) {
		m.processClient = client
		m.processClientExplicit = true
	}
}

func WithUIWorktreeClient(client apicontract.WorktreeService) UIOption {
	return func(m *uiModel) {
		m.worktreeClient = client
	}
}

func WithUITurnQueueHook(hook turnQueueHook) UIOption {
	return func(m *uiModel) {
		m.turnQueueHook = hook
	}
}

func WithUITerminalFocusState(state *terminalFocusState) UIOption {
	return func(m *uiModel) {
		if state != nil {
			m.terminalFocus = state
		}
	}
}

func WithUIClipboardPaster(paster uiClipboardPaster) UIOption {
	return func(m *uiModel) {
		m.clipboardPaster = paster
	}
}

func WithUIClipboardTextCopier(copier uiClipboardTextCopier) UIOption {
	return func(m *uiModel) {
		m.clipboardTextCopier = copier
	}
}
