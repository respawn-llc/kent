package app

import (
	"context"
	"strings"

	"builder/cli/app/internal/statuscollect"
	tea "github.com/charmbracelet/bubbletea"
)

type sessionPickerStatusMsg struct {
	cwd    string
	branch string
	auth   string
	model  string
}

func collectSessionPickerStatusCmd(header sessionPickerHeaderInfo) tea.Cmd {
	req := populateStatusRequestCacheKeys(header.StatusRequest)
	if !sessionPickerStatusRequestUseful(req, header.AuthManager) {
		return nil
	}
	authManager := statuscollect.NormalizeAuthStateResolver(header.AuthManager)
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), statusRefreshTimeout)
		defer cancel()

		collector := defaultUIStatusCollector{authManager: authManager}.adapter()
		base := collector.CollectBase(req)
		gitResult := collector.CollectGit(ctx, req, base)

		branch := ""
		if gitResult.Git.Visible && strings.TrimSpace(gitResult.Git.Error) == "" {
			branch = strings.TrimSpace(gitResult.Git.Branch)
			if branch == "unknown" {
				branch = ""
			}
		}
		return sessionPickerStatusMsg{
			cwd:    statusDisplayPath(base.Workdir, ""),
			branch: branch,
			auth:   sessionPickerAuthLabel(statuscollect.FastAuthInfo(ctx, authManager, req.Settings)),
			model:  strings.TrimSpace(base.Model.Summary),
		}
	}
}

func sessionPickerAuthLabel(info uiStatusAuthInfo) string {
	return statuscollect.AuthDisplayLabel(info)
}

func sessionPickerStatusRequestUseful(req uiStatusRequest, authManager statuscollect.AuthStateResolver) bool {
	if strings.TrimSpace(req.WorkspaceRoot) != "" {
		return true
	}
	if strings.TrimSpace(req.ModelName) != "" || strings.TrimSpace(req.ConfiguredModelName) != "" {
		return true
	}
	if strings.TrimSpace(req.Settings.Model) != "" {
		return true
	}
	return statuscollect.NormalizeAuthStateResolver(authManager) != nil
}
