package app

import (
	"context"
	"strings"

	"core/cli/app/internal/statuscollect"

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
		authInfo := statuscollect.FastAuthInfo(ctx, authManager, req.Settings)
		if authManager == nil && req.AuthStatus != nil {
			authInfo = collector.CollectAuth(ctx, req, base).Auth
		}

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
			auth:   statuscollect.AuthDisplayLabel(authInfo),
			model:  strings.TrimSpace(base.Model.Summary),
		}
	}
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
	if req.AuthStatus != nil {
		return true
	}
	return statuscollect.NormalizeAuthStateResolver(authManager) != nil
}
