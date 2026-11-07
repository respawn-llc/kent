package app

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

type uiFeatureUpdateResult struct {
	model   *uiModel
	cmd     tea.Cmd
	handled bool
}

type uiFeatureReducer func(tea.Msg) uiFeatureUpdateResult

func handledUIFeatureUpdate(model *uiModel, cmd tea.Cmd) uiFeatureUpdateResult {
	return uiFeatureUpdateResult{model: model, cmd: cmd, handled: true}
}

func (m *uiModel) reduceFeatureMessage(msg tea.Msg) uiFeatureUpdateResult {
	reducers := []uiFeatureReducer{
		m.reduceNativeProgressMessage,
		m.reduceKeyMessage,
		m.reduceWindowMessage,
		m.reduceDispatchedEvent,
		m.reduceOngoingMessage,
		m.reduceRuntimeMessage,
		m.reduceStatusMessage,
		m.reduceWorktreeMessage,
		m.reduceAskMessage,
		m.reducePathReferenceMessage,
		m.reduceNoticeMessage,
		m.reduceInputAsyncMessage,
		m.reduceProcessMessage,
		m.reduceClipboardMessage,
		m.reduceConnectionRemovalMessage,
	}
	for _, reducer := range reducers {
		if result := reducer(msg); result.handled {
			return result
		}
	}
	return uiFeatureUpdateResult{}
}

func (m *uiModel) reduceStatusMessage(msg tea.Msg) uiFeatureUpdateResult {
	switch msg := msg.(type) {
	case statusBaseRefreshDoneMsg:
		if !m.acceptsStatusCompletion(msg.owner) {
			m.layout().syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		m.status.mergeBase(msg.snapshot)
		m.finishStatusSectionRefresh(uiStatusSectionBase, msg.snapshot.CollectorWarning)
		m.layout().syncViewport()
		return handledUIFeatureUpdate(m, m.reconcileSpinnerTicking(false))
	case statusAuthRefreshDoneMsg:
		if !m.acceptsStatusCompletion(msg.owner) {
			m.layout().syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		m.status.snapshot.Auth = msg.result.Auth
		m.status.snapshot.Subscription = msg.result.Subscription
		m.finishStatusSectionRefresh(uiStatusSectionAuth, msg.result.Warning)
		m.layout().syncViewport()
		return handledUIFeatureUpdate(m, m.reconcileSpinnerTicking(false))
	case statusGitRefreshDoneMsg:
		if !m.acceptsStatusCompletion(msg.owner) {
			m.layout().syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		m.status.snapshot.Git = msg.result.Git
		if m.statusRepository != nil {
			m.statusRepository.StoreGit(msg.cacheKey, msg.result, time.Now())
		}
		m.finishStatusSectionRefresh(uiStatusSectionGit, "")
		m.layout().syncViewport()
		return handledUIFeatureUpdate(m, m.reconcileSpinnerTicking(false))
	case statusEnvironmentRefreshDoneMsg:
		if !m.acceptsStatusCompletion(msg.owner) {
			m.layout().syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		m.status.snapshot.SkillPolicy = msg.result.SkillPolicy
		m.status.snapshot.Skills = msg.result.Skills
		m.status.snapshot.SkillTokenCounts = msg.result.SkillTokenCounts
		m.status.snapshot.AgentsPaths = msg.result.AgentsPaths
		m.status.snapshot.AgentTokenCounts = msg.result.AgentTokenCounts
		if m.statusRepository != nil {
			m.statusRepository.StoreEnvironment(msg.cacheKey, msg.result, time.Now())
		}
		m.finishStatusSectionRefresh(uiStatusSectionEnvironment, msg.result.CollectorWarning)
		m.layout().syncViewport()
		return handledUIFeatureUpdate(m, m.reconcileSpinnerTicking(false))
	}
	return uiFeatureUpdateResult{}
}
