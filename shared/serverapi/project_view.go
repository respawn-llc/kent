package serverapi

import (
	"context"
	"errors"
	"strings"

	"builder/shared/clientui"
)

type ProjectListRequest struct{}

type ProjectListResponse struct {
	Projects []clientui.ProjectSummary
}

type ProjectBinding struct {
	ProjectID       string `json:"project_id"`
	ProjectName     string `json:"project_name"`
	WorkspaceID     string `json:"workspace_id"`
	CanonicalRoot   string `json:"canonical_root"`
	WorkspaceName   string `json:"workspace_name"`
	WorkspaceStatus string `json:"workspace_status"`
}

type ProjectResolvePathRequest struct {
	Path string `json:"path"`
}

type ProjectResolvePathResponse struct {
	CanonicalRoot    string                       `json:"canonical_root"`
	PathAvailability clientui.ProjectAvailability `json:"path_availability"`
	Binding          *ProjectBinding              `json:"binding,omitempty"`
}

type ProjectBindingPlanMode string

const (
	ProjectBindingPlanModeInteractive ProjectBindingPlanMode = "interactive"
	ProjectBindingPlanModeHeadless    ProjectBindingPlanMode = "headless"
)

type ProjectBindingPlanKind string

const (
	ProjectBindingPlanKindBound                    ProjectBindingPlanKind = "bound"
	ProjectBindingPlanKindLocalUnbound             ProjectBindingPlanKind = "local_unbound"
	ProjectBindingPlanKindServerWorkspaceSelection ProjectBindingPlanKind = "server_workspace_selection"
	ProjectBindingPlanKindHeadlessRemoteSelected   ProjectBindingPlanKind = "headless_remote_selected"
	ProjectBindingPlanKindHeadlessRemoteAmbiguous  ProjectBindingPlanKind = "headless_remote_ambiguous"
)

type ProjectBindingPlanRequest struct {
	Path string                 `json:"path"`
	Mode ProjectBindingPlanMode `json:"mode"`
}

type ProjectBindingPlanResponse struct {
	Kind             ProjectBindingPlanKind        `json:"kind"`
	CanonicalRoot    string                        `json:"canonical_root"`
	PathAvailability clientui.ProjectAvailability  `json:"path_availability"`
	Binding          *ProjectBinding               `json:"binding,omitempty"`
	Projects         []clientui.ProjectSummary     `json:"projects,omitempty"`
	Workspace        *ProjectWorkspacePlanSelected `json:"workspace,omitempty"`
}

type ProjectWorkspacePlanSelected struct {
	ProjectID   string `json:"project_id"`
	WorkspaceID string `json:"workspace_id"`
}

type ProjectCreateRequest struct {
	DisplayName   string `json:"display_name"`
	WorkspaceRoot string `json:"workspace_root"`
}

type ProjectCreateResponse struct {
	Binding ProjectBinding `json:"binding"`
}

type ProjectAttachWorkspaceRequest struct {
	ProjectID     string `json:"project_id"`
	WorkspaceRoot string `json:"workspace_root"`
}

type ProjectAttachWorkspaceResponse struct {
	Binding ProjectBinding `json:"binding"`
}

type ProjectRebindWorkspaceRequest struct {
	OldWorkspaceRoot string `json:"old_workspace_root"`
	NewWorkspaceRoot string `json:"new_workspace_root"`
}

type ProjectRebindWorkspaceResponse struct {
	Binding ProjectBinding `json:"binding"`
}

type ProjectGetOverviewRequest struct {
	ProjectID string
}

type ProjectGetOverviewResponse struct {
	Overview clientui.ProjectOverview
}

type SessionListByProjectRequest struct {
	ProjectID string
}

type SessionListByProjectResponse struct {
	Sessions []clientui.SessionSummary
}

type ProjectViewService interface {
	ListProjects(ctx context.Context, req ProjectListRequest) (ProjectListResponse, error)
	ResolveProjectPath(ctx context.Context, req ProjectResolvePathRequest) (ProjectResolvePathResponse, error)
	PlanWorkspaceBinding(ctx context.Context, req ProjectBindingPlanRequest) (ProjectBindingPlanResponse, error)
	CreateProject(ctx context.Context, req ProjectCreateRequest) (ProjectCreateResponse, error)
	AttachWorkspaceToProject(ctx context.Context, req ProjectAttachWorkspaceRequest) (ProjectAttachWorkspaceResponse, error)
	RebindWorkspace(ctx context.Context, req ProjectRebindWorkspaceRequest) (ProjectRebindWorkspaceResponse, error)
	GetProjectOverview(ctx context.Context, req ProjectGetOverviewRequest) (ProjectGetOverviewResponse, error)
	ListSessionsByProject(ctx context.Context, req SessionListByProjectRequest) (SessionListByProjectResponse, error)
}

func (r ProjectResolvePathRequest) Validate() error {
	if strings.TrimSpace(r.Path) == "" {
		return errors.New("path is required")
	}
	return nil
}

func (r ProjectBindingPlanRequest) Validate() error {
	if strings.TrimSpace(r.Path) == "" {
		return errors.New("path is required")
	}
	switch r.Mode {
	case ProjectBindingPlanModeInteractive, ProjectBindingPlanModeHeadless:
		return nil
	default:
		return errors.New("mode must be interactive or headless")
	}
}

func (r ProjectCreateRequest) Validate() error {
	if strings.TrimSpace(r.DisplayName) == "" {
		return errors.New("display_name is required")
	}
	if strings.TrimSpace(r.WorkspaceRoot) == "" {
		return errors.New("workspace_root is required")
	}
	return nil
}

func (r ProjectAttachWorkspaceRequest) Validate() error {
	if strings.TrimSpace(r.ProjectID) == "" {
		return errors.New("project_id is required")
	}
	if strings.TrimSpace(r.WorkspaceRoot) == "" {
		return errors.New("workspace_root is required")
	}
	return nil
}

func (r ProjectRebindWorkspaceRequest) Validate() error {
	if strings.TrimSpace(r.OldWorkspaceRoot) == "" {
		return errors.New("old_workspace_root is required")
	}
	if strings.TrimSpace(r.NewWorkspaceRoot) == "" {
		return errors.New("new_workspace_root is required")
	}
	return nil
}

func (r ProjectGetOverviewRequest) Validate() error {
	if strings.TrimSpace(r.ProjectID) == "" {
		return errors.New("project_id is required")
	}
	return nil
}

func (r SessionListByProjectRequest) Validate() error {
	if strings.TrimSpace(r.ProjectID) == "" {
		return errors.New("project_id is required")
	}
	return nil
}
