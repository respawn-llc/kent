package transport

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"builder/shared/serverapi"
)

func (g *Gateway) resolveAttachedProjectWorkspace(ctx context.Context, projectID string, workspaceID string, workspaceRoot string) (string, string, error) {
	trimmedWorkspaceID := strings.TrimSpace(workspaceID)
	if trimmedWorkspaceID != "" {
		binding, err := g.deps.MetadataStore().LookupWorkspaceBindingByID(ctx, trimmedWorkspaceID)
		if err != nil {
			return "", "", err
		}
		if strings.TrimSpace(binding.ProjectID) != strings.TrimSpace(projectID) {
			return "", "", fmt.Errorf("workspace %q is not bound to project %q", binding.CanonicalRoot, strings.TrimSpace(projectID))
		}
		return binding.WorkspaceID, strings.TrimSpace(binding.CanonicalRoot), nil
	}
	trimmedWorkspaceRoot := strings.TrimSpace(workspaceRoot)
	if trimmedWorkspaceRoot == "" {
		overview, err := g.deps.ProjectViewClient().GetProjectOverview(ctx, serverapi.ProjectGetOverviewRequest{ProjectID: strings.TrimSpace(projectID)})
		if err != nil {
			return "", "", err
		}
		if len(overview.Overview.Workspaces) == 0 {
			return "", "", fmt.Errorf("project %q has no attached workspaces", strings.TrimSpace(projectID))
		}
		if len(overview.Overview.Workspaces) > 1 {
			return "", "", fmt.Errorf("project %q requires explicit workspace selection", strings.TrimSpace(projectID))
		}
		workspace := overview.Overview.Workspaces[0]
		return strings.TrimSpace(workspace.WorkspaceID), strings.TrimSpace(workspace.RootPath), nil
	}
	resolved, err := g.deps.ProjectViewClient().ResolveProjectPath(ctx, serverapi.ProjectResolvePathRequest{Path: trimmedWorkspaceRoot})
	if err != nil {
		return "", "", err
	}
	if resolved.Binding == nil {
		return "", "", errors.Join(serverapi.ErrWorkspaceNotRegistered, fmt.Errorf("workspace %q is not registered", resolved.CanonicalRoot))
	}
	if strings.TrimSpace(resolved.Binding.ProjectID) != strings.TrimSpace(projectID) {
		return "", "", fmt.Errorf("workspace %q is not bound to project %q", resolved.Binding.CanonicalRoot, strings.TrimSpace(projectID))
	}
	return strings.TrimSpace(resolved.Binding.WorkspaceID), strings.TrimSpace(resolved.Binding.CanonicalRoot), nil
}

func (g *Gateway) sessionLaunchClientForState(ctx context.Context, state *connectionState) (service serverapi.SessionLaunchService, _ error) {
	projectID, err := g.activeProjectID(ctx, state)
	if err != nil {
		return nil, err
	}
	var launchClient any
	if strings.TrimSpace(state.attachedWorkspaceID) == "" {
		launchClient, err = g.deps.SessionLaunchClientForProjectWorkspace(ctx, projectID, state.attachedWorkspaceRoot)
	} else {
		launchClient, err = g.deps.SessionLaunchClientForProjectWorkspaceID(ctx, projectID, state.attachedWorkspaceID)
	}
	if err != nil {
		return nil, err
	}
	loopback, ok := launchClient.(interface {
		PlanSession(context.Context, serverapi.SessionPlanRequest) (serverapi.SessionPlanResponse, error)
	})
	if !ok {
		return nil, errors.New("session launch client does not implement service contract")
	}
	return loopback, nil
}

func (g *Gateway) runPromptClientForState(ctx context.Context, state *connectionState) (serverapi.RunPromptService, error) {
	projectID, err := g.activeProjectID(ctx, state)
	if err != nil {
		return nil, err
	}
	var runClient any
	if strings.TrimSpace(state.attachedWorkspaceID) == "" {
		runClient, err = g.deps.RunPromptClientForProjectWorkspace(ctx, projectID, state.attachedWorkspaceRoot)
	} else {
		runClient, err = g.deps.RunPromptClientForProjectWorkspaceID(ctx, projectID, state.attachedWorkspaceID)
	}
	if err != nil {
		return nil, err
	}
	service, ok := runClient.(interface {
		RunPrompt(context.Context, serverapi.RunPromptRequest, serverapi.RunPromptProgressSink) (serverapi.RunPromptResponse, error)
	})
	if !ok {
		return nil, errors.New("run prompt client does not implement service contract")
	}
	return service, nil
}
