package app

import (
	"context"
	tea "github.com/charmbracelet/bubbletea"
)

func waitRuntimeConnectionStateChange(events <-chan runtimeConnectionStateChangedMsg, scope context.Context) tea.Cmd {
	if events == nil {
		return nil
	}
	return func() tea.Msg {
		ctx := scope
		if ctx == nil {
			ctx = context.Background()
		}
		select {
		case <-ctx.Done():
			return nil
		case msg, ok := <-events:
			if !ok {
				return nil
			}
			msg.scope = scope
			return msg
		}
	}
}

func (m *uiModel) dropNativeSurface() {}
