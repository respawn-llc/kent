package app

import runtimepb "core/shared/protoapi/gen/kent/api/runtime"

import (
	"strings"

	"core/shared/protoapi"
	"core/shared/textutil"
	"google.golang.org/protobuf/proto"

	tea "github.com/charmbracelet/bubbletea"
)

type statusLinePhase uint8

const (
	statusLinePhasePrimary statusLinePhase = iota
	statusLinePhaseSecondary
	statusLinePhaseSuccess
	statusLinePhaseError
)

func (m *uiModel) applyRuntimeMainViewState(view *runtimepb.MainView) tea.Cmd {
	if m == nil {
		return nil
	}
	status := view.Status
	m.status.snapshot.AgentRole = textutil.Pointer(view.Session.AgentRole)
	m.thinkingLevel = status.ThinkingLevel
	m.reviewerMode = status.ReviewerFrequency
	m.reviewerEnabled = status.ReviewerEnabled
	m.autoCompactionEnabled = status.AutoCompactionEnabled
	m.questionsEnabled = status.QuestionsEnabled
	m.fastModeAvailable = status.FastModeAvailable
	m.fastModeEnabled = status.FastModeEnabled
	m.conversationFreshness = status.ConversationFreshness
	m.setRuntimeContextUsage(view.Session.SessionId, status.ContextUsage)
	if view.Activity != nil {
		if err := m.applyRuntimeActivityProjection(view.Activity); err != nil {
			m.activity = uiActivityError
			_ = m.sendTransientStatusWithNoticeID("invalid runtime activity: "+err.Error(), uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
			return nil
		}
	}
	if view.Activity != nil && !protoapi.RuntimeActivityActiveForControl(view.Activity) && m.hasPendingInterrupt() {
		return m.acknowledgePendingInterrupt()
	}
	return nil
}

func (m *uiModel) runtimeMainView() *runtimepb.MainView {
	m.checkTUIBlockingOperation("runtime main-view read", "MainView")
	if client := m.runtimeClient(); client != nil {
		return client.MainView()
	}
	return &runtimepb.MainView{
		Status:  m.localRuntimeStatus(),
		Session: m.localRuntimeSessionView(),
	}
}

func (m *uiModel) cachedRuntimeMainView() *runtimepb.MainView {
	client := m.runtimeClient()
	if cached, ok := client.(interface {
		CachedMainView() (*runtimepb.MainView, bool)
	}); ok {
		if cachedView, hasCached := cached.CachedMainView(); hasCached {
			return cachedView
		}
	}
	return &runtimepb.MainView{
		Status:  m.localRuntimeStatus(),
		Session: m.localRuntimeSessionView(),
	}
}

func (m *uiModel) cachedRuntimeStatus() *runtimepb.Status {
	view := m.cachedRuntimeMainView()
	status := proto.Clone(view.Status).(*runtimepb.Status)
	if m.runtimeContextUsageAppliesTo(view.Session.SessionId) {
		status.ContextUsage = m.runtimeContextUsage
	}
	return status
}

func (m *uiModel) statusLinePhase() statusLinePhase {
	if m == nil {
		return statusLinePhasePrimary
	}
	if m.isCompacting() {
		return statusLinePhaseSecondary
	}
	if m.isReviewerActive() {
		return statusLinePhaseSuccess
	}
	if m.rollback.isActive() {
		return statusLinePhasePrimary
	}
	if goalIsActive(m.cachedRuntimeStatus().Goal) {
		return statusLinePhasePrimary
	}
	if m.activity == uiActivityError {
		return statusLinePhaseError
	}
	return statusLinePhasePrimary
}

func (m *uiModel) statusLineLabel() string {
	if m == nil {
		return ""
	}
	if m.isCompacting() {
		return "compacting"
	}
	if m.isReviewerActive() {
		return "review"
	}
	if m.rollback.isActive() {
		return "rollback"
	}
	if goalIsPresent(m.cachedRuntimeStatus().Goal) {
		return "goal"
	}
	if m.activity == uiActivityError {
		return "error"
	}
	return ""
}

func (m *uiModel) statusLineSpinning() bool {
	if m == nil {
		return false
	}
	return (m.runtimeActivityBusy() && m.activity != uiActivityQuestion) ||
		m.isReviewerActive()
}

func (m *uiModel) setRuntimeContextUsage(sessionID string, usage *runtimepb.ContextUsage) {
	if m == nil {
		return
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		m.runtimeContextUsage = nil
		m.runtimeContextUsageSession = ""
		return
	}
	m.runtimeContextUsage = usage
	m.runtimeContextUsageSession = sessionID
}

func (m *uiModel) runtimeContextUsageAppliesTo(sessionID string) bool {
	if m == nil || m.runtimeContextUsage.GetWindowTokens() <= 0 {
		return false
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		sessionID = strings.TrimSpace(m.sessionID)
	}
	return sessionID != "" && strings.TrimSpace(m.runtimeContextUsageSession) == sessionID
}

func (m *uiModel) currentRuntimeSessionID() string {
	if m == nil {
		return ""
	}
	if sessionID := strings.TrimSpace(m.sessionID); sessionID != "" {
		return sessionID
	}
	if client := m.runtimeClient(); client != nil {
		if cached, ok := client.(interface {
			CachedMainView() (*runtimepb.MainView, bool)
		}); ok {
			view, hasCached := cached.CachedMainView()
			if hasCached {
				return strings.TrimSpace(view.Session.SessionId)
			}
		}
	}
	return ""
}

func (m *uiModel) localRuntimeStatus() *runtimepb.Status {
	return &runtimepb.Status{
		ReviewerFrequency:     m.reviewerMode,
		ReviewerEnabled:       m.reviewerEnabled,
		AutoCompactionEnabled: m.autoCompactionEnabled,
		QuestionsEnabled:      m.questionsEnabled,
		FastModeAvailable:     m.fastModeAvailable,
		FastModeEnabled:       m.fastModeEnabled,
		ConversationFreshness: m.conversationFreshness,
		ThinkingLevel:         m.thinkingLevel,
	}
}
