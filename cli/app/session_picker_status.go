package app

import (
	"context"
	"strings"

	"core/cli/app/internal/status"
	"core/shared/textutil"

	tea "github.com/charmbracelet/bubbletea"
)

type sessionPickerStatusMsg struct {
	cwd    *string
	branch *string
}

type sessionPickerModelFactsMsg struct {
	facts *sessionPickerModelFacts
	err   error
}

func (m *sessionPickerModel) collectModelFactsCmd() tea.Cmd {
	if m.header.loadModelFacts == nil {
		return nil
	}
	load, ctx := m.header.loadModelFacts, m.requestContext
	return func() tea.Msg {
		facts, err := load(ctx)
		return sessionPickerModelFactsMsg{facts: facts, err: err}
	}
}

func collectSessionPickerStatusCmd(header sessionPickerHeaderInfo) tea.Cmd {
	req := populateStatusRequestCacheKeys(header.StatusRequest)
	if strings.TrimSpace(req.WorkspaceRoot) == "" {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), statusRefreshTimeout)
		defer cancel()

		collector := defaultUIStatusCollector()
		base := collector.CollectBase(req)
		gitResult := collector.CollectGit(ctx, req, base)

		branch := textutil.OptionalTrimmedString(gitResult.Git.Branch)
		if !gitResult.Git.Visible || strings.TrimSpace(gitResult.Git.Error) != "" ||
			(branch != nil && *branch == "unknown") {
			branch = nil
		}
		return sessionPickerStatusMsg{
			cwd:    textutil.OptionalTrimmedString(statusDisplayPath(base.Workdir, "")),
			branch: branch,
		}
	}
}

func sessionPickerModelSummary(facts *sessionPickerModelFacts) *string {
	if facts == nil {
		return nil
	}
	if facts.Name == nil {
		panic("session picker model facts require a model name")
	}
	name := strings.TrimSpace(*facts.Name)
	if name == "" || name != *facts.Name {
		panic("session picker model facts require a nonblank trimmed model name")
	}
	thinkingLevel := ""
	if facts.ThinkingLevel != nil {
		thinkingLevel = strings.TrimSpace(*facts.ThinkingLevel)
		if thinkingLevel == "" || thinkingLevel != *facts.ThinkingLevel {
			panic("session picker model facts require a nonblank trimmed thinking level")
		}
	}
	if facts.Verbosity != nil {
		verbosity := strings.TrimSpace(string(*facts.Verbosity))
		if verbosity == "" || verbosity != string(*facts.Verbosity) {
			panic("session picker model facts require nonblank trimmed verbosity")
		}
	}
	return textutil.OptionalTrimmedString(status.ModelDisplaySummary(name, thinkingLevel))
}
