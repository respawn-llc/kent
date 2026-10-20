package app

import (
	"core/shared/apicontract"
	"core/shared/clientui"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
)

type runtimeWiring struct {
	chatMutations          apicontract.ChatMutationService
	chatSettings           apicontract.ChatSettingsService
	controls               apicontract.RuntimeControlService
	pendingWork            apicontract.RuntimePendingWorkService
	preparation            *sessionlaunchpb.SessionRuntimePreparationOptions
	attentionNotifications apicontract.AttentionNotificationService
	turnQueueHook          turnQueueHook
	terminalFocus          *terminalFocusState
	eventDispatcher        *uiEventDispatcher
	requestTranscriptOpen  func()
	promptAnswers          *transcriptPromptAnswerer
	promptAttention        promptAttentionSink
	runtimeClient          clientui.RuntimeClient
	worktrees              apicontract.WorktreeService
	processControls        apicontract.ProcessControlService
	processViews           apicontract.ProcessViewService
	lifecycleHookIssues    <-chan lifecycleHookIssue
	lifecycleHookDone      <-chan struct{}
}
