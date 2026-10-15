package app

import (
	"core/shared/apicontract"
	workflowpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	tea "github.com/charmbracelet/bubbletea"
)

type sessionPickerSubscribedMsg struct {
	subscription apicontract.WorkflowEventSubscription
	err          error
}

type sessionPickerCatalogChangedMsg struct{ err error }

func (m *sessionPickerModel) subscribeCatalogCmd() tea.Cmd {
	return func() tea.Msg {
		subscription, err := m.loader.Subscribe(m.requestContext)
		return sessionPickerSubscribedMsg{subscription: subscription, err: err}
	}
}

func (m *sessionPickerModel) waitCatalogChangeCmd() tea.Cmd {
	return func() tea.Msg {
		for {
			event, err := m.subscription.Next(m.requestContext)
			if err != nil {
				return sessionPickerCatalogChangedMsg{err: err}
			}
			if event.Resource == workflowpb.ProjectEventResource_WORKFLOW_PROJECT_EVENT_RESOURCE_SESSION_CATALOG {
				return sessionPickerCatalogChangedMsg{}
			}
		}
	}
}
