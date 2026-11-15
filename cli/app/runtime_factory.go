package app

import (
	"core/cli/tui/ongoing"
	"core/shared/apicontract"
	"core/shared/clientui"
	invocationpb "core/shared/protoapi/gen/kent/api/invocation"
)

type runtimeWiring struct {
	chatMutations          apicontract.ChatMutationService
	chatSettings           apicontract.ChatSettingsService
	controls               apicontract.RuntimeControlService
	liveControls           apicontract.RuntimeLiveControlService
	pendingWork            apicontract.RuntimePendingWorkService
	invocation             *invocationpb.RunPromptOverrides
	attentionNotifications apicontract.AttentionNotificationService
	turnQueueHook          turnQueueHook
	terminalFocus          *terminalFocusState
	eventDispatcher        *uiEventDispatcher
	requestTranscriptOpen  func(ongoing.RehydrateReason)
	promptAnswers          *transcriptPromptAnswerer
	promptAttention        promptAttentionSink
	runtimeClient          clientui.RuntimeClient
	worktrees              apicontract.WorktreeService
	processControls        apicontract.ProcessControlService
	processViews           apicontract.ProcessViewService
	lifecycleHookIssues    <-chan lifecycleHookIssue
	lifecycleHookDone      <-chan struct{}
}
