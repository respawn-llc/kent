package app

import (
	"context"

	"builder/shared/clientui"

	"github.com/google/uuid"
)

func (m *uiModel) runtimeClient() clientui.RuntimeClient {
	if m == nil {
		return nil
	}
	return m.engine
}

func (m *uiModel) hasRuntimeClient() bool {
	return m.runtimeClient() != nil
}

func (m *uiModel) setRuntimeSessionName(name string) error {
	if client := m.runtimeClient(); client != nil {
		err := client.SetSessionName(name)
		m.observeRuntimeRequestResult(err)
		return err
	}
	return nil
}

func (m *uiModel) setRuntimeThinkingLevel(level string) error {
	if client := m.runtimeClient(); client != nil {
		err := client.SetThinkingLevel(level)
		m.observeRuntimeRequestResult(err)
		return err
	}
	return nil
}

func (m *uiModel) setRuntimeFastModeEnabled(enabled bool) (bool, error) {
	if client := m.runtimeClient(); client != nil {
		changed, err := client.SetFastModeEnabled(enabled)
		m.observeRuntimeRequestResult(err)
		return changed, err
	}
	return false, nil
}

func (m *uiModel) setRuntimeReviewerEnabled(enabled bool) (bool, string, error) {
	if client := m.runtimeClient(); client != nil {
		changed, mode, err := client.SetReviewerEnabled(enabled)
		m.observeRuntimeRequestResult(err)
		return changed, mode, err
	}
	return false, "", nil
}

func (m *uiModel) setRuntimeAutoCompactionEnabled(enabled bool) (bool, bool, error) {
	if client := m.runtimeClient(); client != nil {
		changed, nextEnabled, err := client.SetAutoCompactionEnabled(enabled)
		m.observeRuntimeRequestResult(err)
		return changed, nextEnabled, err
	}
	return false, false, nil
}

func (m *uiModel) showRuntimeGoal() (*clientui.RuntimeGoal, error) {
	if client := m.runtimeClient(); client != nil {
		goal, err := client.ShowGoal()
		m.observeRuntimeRequestResult(err)
		return goal, err
	}
	return nil, nil
}

func (m *uiModel) setRuntimeGoal(objective string) (*clientui.RuntimeGoal, error) {
	if client := m.runtimeClient(); client != nil {
		goal, err := client.SetGoal(objective)
		m.observeRuntimeRequestResult(err)
		return goal, err
	}
	return nil, nil
}

func (m *uiModel) pauseRuntimeGoal() (*clientui.RuntimeGoal, error) {
	if client := m.runtimeClient(); client != nil {
		goal, err := client.PauseGoal()
		m.observeRuntimeRequestResult(err)
		return goal, err
	}
	return nil, nil
}

func (m *uiModel) resumeRuntimeGoal() (*clientui.RuntimeGoal, error) {
	if client := m.runtimeClient(); client != nil {
		goal, err := client.ResumeGoal()
		m.observeRuntimeRequestResult(err)
		return goal, err
	}
	return nil, nil
}

func (m *uiModel) clearRuntimeGoal() (*clientui.RuntimeGoal, error) {
	if client := m.runtimeClient(); client != nil {
		goal, err := client.ClearGoal()
		m.observeRuntimeRequestResult(err)
		return goal, err
	}
	return nil, nil
}

func (m *uiModel) appendRuntimeLocalEntry(role, text string) error {
	return m.appendRuntimeLocalEntryWithNoticeID(role, text, "")
}

func (m *uiModel) appendRuntimeLocalEntryWithNoticeID(role, text, noticeID string) error {
	if client := m.runtimeClient(); client != nil {
		if withNoticeID, ok := client.(interface {
			AppendLocalEntryWithNoticeID(role, text, noticeID string) error
		}); ok {
			err := withNoticeID.AppendLocalEntryWithNoticeID(role, text, noticeID)
			m.observeRuntimeRequestResult(err)
			return err
		}
		err := client.AppendLocalEntry(role, text)
		m.observeRuntimeRequestResult(err)
		return err
	}
	return nil
}

func (m *uiModel) submitRuntimeUserMessage(ctx context.Context, text string) (string, error) {
	if client := m.runtimeClient(); client != nil {
		message, err := client.SubmitUserMessage(ctx, text)
		m.observeRuntimeRequestResult(err)
		return message, err
	}
	return "", nil
}

func (m *uiModel) submitRuntimeUserShellCommand(ctx context.Context, command string) error {
	if client := m.runtimeClient(); client != nil {
		err := client.SubmitUserShellCommand(ctx, command)
		m.observeRuntimeRequestResult(err)
		return err
	}
	return nil
}

func (m *uiModel) compactRuntimeContext(ctx context.Context, args string) error {
	if client := m.runtimeClient(); client != nil {
		err := client.CompactContext(ctx, args)
		m.observeRuntimeRequestResult(err)
		return err
	}
	return nil
}

func (m *uiModel) hasQueuedRuntimeUserWork() (bool, error) {
	if client := m.runtimeClient(); client != nil {
		hasWork, err := client.HasQueuedUserWork()
		m.observeRuntimeRequestResult(err)
		return hasWork, err
	}
	return false, nil
}

func (m *uiModel) submitQueuedRuntimeUserMessages(ctx context.Context) (string, error) {
	if client := m.runtimeClient(); client != nil {
		message, err := client.SubmitQueuedUserMessages(ctx)
		m.observeRuntimeRequestResult(err)
		return message, err
	}
	return "", nil
}

func (m *uiModel) interruptRuntime() error {
	if client := m.runtimeClient(); client != nil {
		err := client.Interrupt()
		m.observeRuntimeRequestResult(err)
		return err
	}
	return nil
}

func (m *uiModel) queueRuntimeUserMessage(text string) (clientui.QueuedUserMessage, error) {
	if client := m.runtimeClient(); client != nil {
		return client.QueueUserMessage(text)
	}
	return clientui.QueuedUserMessage{ID: uuid.NewString(), Text: text}, nil
}

func (m *uiModel) discardQueuedRuntimeUserMessage(queueItemID string) bool {
	if client := m.runtimeClient(); client != nil {
		return client.DiscardQueuedUserMessage(queueItemID)
	}
	return false
}

func (m *uiModel) recordRuntimePromptHistory(text string) error {
	if client := m.runtimeClient(); client != nil {
		err := client.RecordPromptHistory(text)
		m.observeRuntimeRequestResult(err)
		return err
	}
	return nil
}
