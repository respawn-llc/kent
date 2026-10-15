package app

import (
	"context"
	"errors"
	"slices"
	"strings"

	"core/cli/app/commands"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	settingspb "core/shared/protoapi/gen/kent/api/chat_settings"
	invocationpb "core/shared/protoapi/gen/kent/api/invocation"
	"core/shared/textutil"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"
)

type uiNewSessionSettingsLoad interface{ isNewSettingsLoad() }
type uiNewSettingsLoading struct{}
type uiNewSettingsFailed struct{ err error }
type uiNewSettingsReady struct {
	choices   []*settingspb.NewChatAgentChoice
	readiness *authpb.BootstrapStatus
}

func (uiNewSettingsLoading) isNewSettingsLoad() {}
func (uiNewSettingsFailed) isNewSettingsLoad()  {}
func (uiNewSettingsReady) isNewSettingsLoad()   {}

type newSessionSettingsLoadedMsg struct {
	target    *chatpb.NewChatTarget
	catalog   *settingspb.NewChatCatalog
	readiness *authpb.BootstrapStatus
	err       error
	retry     *newSessionSettingsRetry
}

type newSessionSettingsRetry struct {
	text       string
	draftToken uint64
	command    *commands.Result
}

func (m *uiModel) loadNewSessionSettingsCmd(retry *newSessionSettingsRetry) tea.Cmd {
	target := m.newSessionTarget()
	if target == nil {
		return nil
	}
	target.settingsLoad = uiNewSettingsLoading{}
	m.showTransientStatusNotice(uiStatusNotice{Text: "Loading settings…", Kind: uiStatusNoticeInfo})
	creation := target.creation
	settings := m.chatSettings
	request := &settingspb.ReadRequest{Target: &settingspb.ReadRequest_NewChat{NewChat: &settingspb.NewChatTarget{
		ProjectId: creation.ProjectId, WorkspaceId: creation.WorkspaceId,
	}}}
	if creation.Invocation != nil {
		request.GetNewChat().Invocation = proto.Clone(creation.Invocation).(*invocationpb.RunPromptOverrides)
	}
	if initial := creation.InitialSettings; initial != nil {
		if request.GetNewChat().Invocation == nil {
			request.GetNewChat().Invocation = &invocationpb.RunPromptOverrides{}
		}
		request.GetNewChat().Invocation.AgentRole = &initial.AgentRole
		request.GetNewChat().Invocation.ThinkingLevel = initial.Thinking
	}
	return func() tea.Msg {
		result := newSessionSettingsLoadedMsg{target: creation, retry: retry}
		response, err := settings.ReadChatSettings(context.Background(), request)
		if err != nil {
			result.err = err
			return result
		}
		result.catalog = response.GetNewChat()
		if result.catalog == nil {
			result.err = errors.New("New Chat settings catalog is required")
			return result
		}
		initial := result.catalog.InitialSettings
		for _, choice := range result.catalog.Choices {
			if choice.Agent.Role != initial.AgentRole || choice.ConnectionId == nil {
				continue
			}
			result.readiness, result.err = target.auth.GetBootstrapStatus(context.Background(), &authpb.GetBootstrapStatusRequest{
				Target: &authpb.ConnectionTarget{Target: &authpb.ConnectionTarget_ConnectionId{ConnectionId: *choice.ConnectionId}},
			})
			break
		}
		return result
	}
}

func (m *uiModel) applyNewSessionSettingsLoaded(msg newSessionSettingsLoadedMsg) tea.Cmd {
	target := m.newSessionTarget()
	if target == nil || target.creation != msg.target {
		return nil
	}
	if msg.err != nil {
		target.settingsLoad = uiNewSettingsFailed{err: msg.err}
		return m.showTransientStatusNotice(uiStatusNotice{Text: msg.err.Error(), Kind: uiStatusNoticeError})
	}
	target.settingsLoad = uiNewSettingsReady{choices: msg.catalog.Choices, readiness: msg.readiness}
	if target.creation.InitialSettings == nil {
		target.creation.InitialSettings = proto.Clone(msg.catalog.InitialSettings).(*settingspb.InitialChatSettings)
	}
	if invocation := target.creation.Invocation; invocation != nil {
		invocation.AgentRole = nil
		invocation.ThinkingLevel = nil
	}
	m.projectNewSessionSettings()
	var clear tea.Cmd
	if msg.readiness != nil && msg.readiness.AuthRequired && !msg.readiness.AuthReady {
		clear = m.showTransientStatusNotice(uiStatusNotice{Text: "Sign in with /login to send a message.", Kind: uiStatusNoticeInfo})
		if !m.newSessionConnectionReady() {
			return clear
		}
	} else {
		clear = m.advanceTransientStatusQueue()
	}
	if msg.retry == nil {
		return clear
	}
	if m.mainInputDraftToken == msg.retry.draftToken {
		m.clearInput()
	}
	if msg.retry.command != nil {
		_, action := m.inputController().applyCommandResultWithPreSubmitQueuePositionAndSubmittedText(*msg.retry.command, preSubmitQueueBack, activeSubmitOriginDirect, msg.retry.text)
		return tea.Batch(clear, action)
	}
	return tea.Batch(clear, m.inputController().startSubmissionWithPromptHistoryAndQueuePositionAndID(msg.retry.text, preSubmitQueueBack, ""))
}

type newSessionInputIntent uint8

const (
	newSessionInputEnter newSessionInputIntent = iota
	newSessionInputQueue
)

func (m *uiModel) newSessionConnectionReady() bool {
	target := m.newSessionTarget()
	if target == nil {
		return false
	}
	state, ok := target.settingsLoad.(uiNewSettingsReady)
	if !ok {
		return false
	}
	choice := m.selectedNewSessionChoice()
	if choice != nil && len(choice.GetConnectionSelection().GetIds()) > 1 {
		return true
	}
	return state.readiness == nil || !state.readiness.AuthRequired || state.readiness.AuthReady
}

func (m *uiModel) gateNewSessionInput(text string, intent newSessionInputIntent) (bool, tea.Cmd) {
	target := m.newSessionTarget()
	if target == nil {
		return false, nil
	}
	commandText := strings.TrimSpace(text)
	if selection := m.resolveSlashCommandSelection(commandText); selection.hasCommand && selection.commandText() != "" {
		commandText = selection.commandText()
	}
	command := m.commandRegistry.Execute(commandText)
	if command.Handled && !(command.SubmitUser || command.PromptCommand != nil || command.Action == commands.ActionCompact || (command.Action == commands.ActionGoal && command.GoalMode == commands.GoalModeSet)) {
		return false, nil
	}
	switch state := target.settingsLoad.(type) {
	case uiNewSettingsReady:
		if !m.newSessionConnectionReady() {
			return true, m.showTransientStatusNotice(uiStatusNotice{Text: "Sign in with /login to send a message.", Kind: uiStatusNoticeInfo})
		}
		return false, nil
	case uiNewSettingsFailed:
		if intent != newSessionInputEnter {
			return true, m.showTransientStatusNotice(uiStatusNotice{Text: state.err.Error(), Kind: uiStatusNoticeError})
		}
		retry := &newSessionSettingsRetry{text: text, draftToken: m.mainInputDraftToken}
		if command.Handled {
			retry.command = &command
		}
		return true, m.loadNewSessionSettingsCmd(retry)
	default:
		return true, nil
	}
}

func (m *uiModel) selectedNewSessionChoice() *settingspb.NewChatAgentChoice {
	target := m.newSessionTarget()
	if target == nil || target.creation.InitialSettings == nil {
		return nil
	}
	ready, ok := target.settingsLoad.(uiNewSettingsReady)
	if !ok {
		return nil
	}
	for _, choice := range ready.choices {
		if choice.Agent.Role == target.creation.InitialSettings.AgentRole {
			return choice
		}
	}
	return nil
}

func (m *uiModel) projectNewSessionSettings() {
	settings := m.projectedNewSessionSettings()
	if settings == nil {
		return
	}
	m.applyChatSettingsProjection(settings)
	m.autoCompactionEnabled = settings.AutoCompaction.Effective
}

func (m *uiModel) projectedNewSessionSettings() *settingspb.Settings {
	choice := m.selectedNewSessionChoice()
	if choice == nil {
		return nil
	}
	selected := m.newChatTarget().InitialSettings
	settings := &settingspb.Settings{
		SelectedAgent:  &settingspb.AgentSummary{Role: selected.AgentRole, Model: choice.Agent.GetModel(), Thinking: choice.Agent.GetThinking()},
		Supervisor:     proto.Clone(choice.Supervisor).(*settingspb.Supervisor),
		Questions:      proto.Clone(choice.Questions).(*settingspb.Questions),
		AutoCompaction: proto.Clone(choice.AutoCompaction).(*settingspb.AutoCompaction),
	}
	settings.Supervisor.Value = selected.Supervisor
	settings.Questions.Enabled = selected.GetQuestionsEnabled()
	settings.AutoCompaction.Stored = selected.GetAutoCompactionEnabled()
	settings.AutoCompaction.Effective = selected.GetAutoCompactionEnabled()
	if choice.Thinking != nil {
		settings.Thinking = proto.Clone(choice.Thinking).(*settingspb.Thinking)
		settings.Thinking.Value = selected.GetThinking()
		settings.SelectedAgent.Thinking = selected.GetThinking()
	}
	if choice.Fast != nil {
		settings.Fast = proto.Clone(choice.Fast).(*settingspb.Fast)
		settings.Fast.Value = selected.GetFast()
	}
	return settings
}

func (m *uiModel) mutateNewSessionSettings(operation *settingspb.MutationOperation) tea.Cmd {
	choice := m.selectedNewSessionChoice()
	if choice == nil {
		return m.sendTransientStatusWithNoticeID("Settings are not loaded.", uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
	}
	selected := m.newChatTarget().InitialSettings
	var rejection *settingspb.MutationRejectionReason
	reject := func(reason settingspb.MutationRejectionReason) { rejection = &reason }
	switch operation := operation.Operation.(type) {
	case *settingspb.MutationOperation_QuestionsEnabled:
		selected.QuestionsEnabled = textutil.Value(operation.QuestionsEnabled)
	case *settingspb.MutationOperation_Supervisor:
		selected.Supervisor = operation.Supervisor
	case *settingspb.MutationOperation_Thinking:
		if choice.Thinking == nil || (choice.Thinking.Kind == settingspb.ThinkingKind_THINKING_KIND_ENUMERATED && !slices.Contains(choice.Thinking.Values, operation.Thinking)) {
			reject(settingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_THINKING_UNAVAILABLE)
		} else {
			selected.Thinking = textutil.Value(operation.Thinking)
		}
	case *settingspb.MutationOperation_FastEnabled:
		if choice.Fast == nil {
			reject(settingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_FAST_UNAVAILABLE)
		} else {
			selected.Fast = textutil.Value(operation.FastEnabled)
		}
	case *settingspb.MutationOperation_AutoCompactionEnabled:
		if choice.AutoCompaction.Editability != settingspb.Editability_EDITABILITY_EDITABLE {
			reject(settingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_AUTO_COMPACTION_POLICY_LOCKED)
		} else {
			selected.AutoCompactionEnabled = textutil.Value(operation.AutoCompactionEnabled)
		}
	case *settingspb.MutationOperation_AgentRole:
		ready := m.newSessionTarget().settingsLoad.(uiNewSettingsReady)
		for _, candidate := range ready.choices {
			if candidate.Agent.Role == operation.AgentRole {
				if selected.AgentRole == operation.AgentRole {
					return nil
				}
				m.newChatTarget().InitialSettings = proto.Clone(candidate.Baseline).(*settingspb.InitialChatSettings)
				m.projectNewSessionSettings()
				return m.loadNewSessionSettingsCmd(nil)
			}
		}
		reject(settingspb.MutationRejectionReason_MUTATION_REJECTION_REASON_AGENT_UNAVAILABLE)
	default:
		panic("invalid Chat settings operation")
	}
	if rejection != nil {
		return m.sendTransientStatusWithNoticeID(chatSettingsRejectionNotices[*rejection], uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
	}
	m.projectNewSessionSettings()
	return nil
}
