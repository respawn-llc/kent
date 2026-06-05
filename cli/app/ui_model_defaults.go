package app

import (
	"os"
	"strings"

	"builder/cli/app/commands"
	"builder/cli/tui"
	"builder/shared/clientui"
	"builder/shared/theme"
)

func newUIModelDefaults(runtimeClient clientui.RuntimeClient, runtimeEvents <-chan clientui.Event, askEvents <-chan askEvent) *uiModel {
	return &uiModel{
		uiRuntimeFeatureState:           newUIRuntimeFeatureState(runtimeClient, runtimeEvents, askEvents),
		uiInputFeatureState:             newUIInputFeatureState(),
		uiPresentationFeatureState:      newUIPresentationFeatureState(),
		uiConversationFeatureState:      newUIConversationFeatureState(),
		uiSessionTransitionFeatureState: newUISessionTransitionFeatureState(),
		uiStatusFeatureState:            newUIStatusFeatureState(),
		uiRollbackFeatureState:          newUIRollbackFeatureState(),
	}
}

func newUIRuntimeFeatureState(runtimeClient clientui.RuntimeClient, runtimeEvents <-chan clientui.Event, askEvents <-chan askEvent) uiRuntimeFeatureState {
	return uiRuntimeFeatureState{
		engine:        runtimeClient,
		view:          tui.NewModel(tui.WithCompactDetail()),
		runtimeEvents: runtimeEvents,
		askEvents:     askEvents,
	}
}

func newUIInputFeatureState() uiInputFeatureState {
	return uiInputFeatureState{
		activity:                 uiActivityIdle,
		inputCursor:              -1,
		mainInputDraftToken:      1,
		promptHistorySelection:   -1,
		promptHistoryDraftCursor: -1,
		commandRegistry:          commands.NewDefaultRegistry(),
		reviewerMode:             "off",
		autoCompactionEnabled:    true,
		conversationFreshness:    clientui.ConversationFreshnessFresh,
	}
}

func newUIPresentationFeatureState() uiPresentationFeatureState {
	return uiPresentationFeatureState{
		theme:         theme.Auto,
		terminalFocus: newTerminalFocusState(),
	}
}

func newUIConversationFeatureState() uiConversationFeatureState {
	return uiConversationFeatureState{
		interaction: uiInteractionState{Mode: uiInputModeMain},
		ask:         uiAskState{inputCursor: -1},
	}
}

func newUISessionTransitionFeatureState() uiSessionTransitionFeatureState {
	return uiSessionTransitionFeatureState{
		exitAction: UIActionNone,
	}
}

func newUIStatusFeatureState() uiStatusFeatureState {
	debug := envFlagEnabled("BUILDER_DEBUG")
	return uiStatusFeatureState{
		statusRepository:      newMemoryUIStatusRepository(),
		clipboardImagePaster:  newSystemClipboardImagePaster(),
		clipboardTextCopier:   newSystemClipboardTextCopier(),
		debugKeys:             envFlagEnabled("BUILDER_DEBUG_KEYS"),
		debugMode:             debug,
		transcriptDiagnostics: envFlagEnabled("BUILDER_TRANSCRIPT_DIAGNOSTICS"),
	}
}

func newUIRollbackFeatureState() uiRollbackFeatureState {
	return uiRollbackFeatureState{
		rollback: uiRollbackState{phase: uiRollbackPhaseInactive, selectedTranscriptEntry: -1},
	}
}

func envFlagEnabled(name string) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return false
	}
	switch strings.ToLower(value) {
	case "0", "false", "off", "no":
		return false
	default:
		return true
	}
}
