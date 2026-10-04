package app

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"core/cli/app/internal/status"
	"core/shared/apicontract"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	"core/shared/textutil"

	tea "github.com/charmbracelet/bubbletea"
)

type sessionPickerStatusMsg struct {
	cwd    *string
	branch *string
}

type sessionPickerHeaderFactsMsg struct {
	facts *sessionPickerHeaderFacts
	err   error
}

func (m *sessionPickerModel) collectHeaderFactsCmd() tea.Cmd {
	if m.header.loadHeaderFacts == nil {
		return nil
	}
	load, ctx := m.header.loadHeaderFacts, m.requestContext
	return func() tea.Msg {
		facts, err := load(ctx)
		return sessionPickerHeaderFactsMsg{facts: facts, err: err}
	}
}

func loadSessionPickerProviderInfo(ctx context.Context, connections apicontract.ConnectionManagementService, auth apicontract.AuthStatusService) (sessionPickerProviderInfo, error) {
	catalog, err := connections.GetConnections(ctx, &authpb.GetConnectionsRequest{})
	if err != nil {
		return nil, err
	}
	if catalog == nil {
		return nil, errors.New("connection catalog is required")
	}
	switch len(catalog.Connections) {
	case 0:
		return nil, nil
	case 1:
		response, err := auth.GetStatus(ctx, &authpb.GetStatusRequest{
			Provider:              &authpb.ProviderSelection{ConnectionId: catalog.Connections[0].GetId()},
			SkipSubscriptionUsage: true,
		})
		if err != nil {
			return nil, err
		}
		return sessionPickerSingleConnection{auth: status.AuthStageFromResponse(response).Auth}, nil
	default:
		return sessionPickerConnectionCount(len(catalog.Connections)), nil
	}
}

func sessionPickerProviderSummary(info sessionPickerProviderInfo) string {
	switch provider := info.(type) {
	case nil:
		return ""
	case sessionPickerConnectionCount:
		return fmt.Sprintf("%d connections", provider)
	case sessionPickerSingleConnection:
		return status.AuthDisplayLabel(provider.auth)
	default:
		panic(fmt.Sprintf("unknown session picker provider information %T", info))
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
