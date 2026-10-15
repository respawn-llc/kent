package app

import (
	"context"
	"errors"

	"core/cli/app/commands"
	"core/shared/protoapi"
	promptcommandpb "core/shared/protoapi/gen/kent/api/prompt_command"
	"core/shared/serverapi"

	tea "github.com/charmbracelet/bubbletea"
)

type initialCommandCatalogDoneMsg struct {
	owner   *uiModel
	catalog uiCommandCatalog
}

func (m *uiModel) installInitialCommandCatalog(msg initialCommandCatalogDoneMsg) tea.Cmd {
	if msg.owner != m || m.observationContext.Err() != nil {
		return nil
	}
	m.commandRegistry = msg.catalog.registry
	m.promptCatalog = msg.catalog.client
	m.promptCatalogEntries = msg.catalog.entries
	m.refreshAutocompleteFromInput()
	if msg.catalog.status != nil {
		return m.sendTransientStatusWithNoticeID(*msg.catalog.status, uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
	}
	return nil
}

func promptCatalogSnapshot(response *promptcommandpb.Catalog) ([]commands.PromptCommandCatalogEntry, error) {
	if err := protoapi.Validate(response); err != nil {
		return nil, err
	}
	entries := make([]commands.PromptCommandCatalogEntry, 0, len(response.Commands))
	for _, entry := range response.Commands {
		entries = append(entries, commands.PromptCommandCatalogEntry{Name: entry.Name, Preview: entry.Preview})
	}
	return entries, nil
}

func (m *uiModel) removePromptCatalogEntry(name string) {
	if m == nil {
		return
	}
	filtered := make([]commands.PromptCommandCatalogEntry, 0, len(m.promptCatalogEntries))
	for _, entry := range m.promptCatalogEntries {
		if entry.Name == name {
			continue
		}
		filtered = append(filtered, entry)
	}
	m.promptCatalogEntries = filtered
	m.commandRegistry = commands.NewDefaultRegistryWithPromptCatalog(filtered)
}

func (m *uiModel) startPromptCatalogRefresh(name string) tea.Cmd {
	if m == nil || m.promptCatalog == nil {
		return nil
	}
	m.removePromptCatalogEntry(name)
	catalog := m.promptCatalog
	return func() tea.Msg {
		response, err := catalog.GetPromptCommandCatalog(context.Background(), &promptcommandpb.GetCatalogRequest{})
		if err != nil {
			return promptCatalogRefreshDoneMsg{err: err}
		}
		entries, err := promptCatalogSnapshot(response)
		if err != nil {
			return promptCatalogRefreshDoneMsg{err: err}
		}
		return promptCatalogRefreshDoneMsg{entries: entries}
	}
}

func (m *uiModel) handlePromptCatalogRefreshDone(msg promptCatalogRefreshDoneMsg) tea.Cmd {
	if m == nil {
		return nil
	}
	if msg.err != nil {
		return m.sendTransientStatusWithNoticeID("Custom prompt commands are unavailable for this session.", uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
	}
	m.promptCatalogEntries = append([]commands.PromptCommandCatalogEntry(nil), msg.entries...)
	m.commandRegistry = commands.NewDefaultRegistryWithPromptCatalog(m.promptCatalogEntries)
	return nil
}

func promptCommandNotFound(err error) (*serverapi.PromptCommandError, bool) {
	var typed *serverapi.PromptCommandError
	if !errors.As(err, &typed) || typed.Kind != serverapi.PromptCommandErrorKindCommandNotFound {
		return nil, false
	}
	return typed, true
}
