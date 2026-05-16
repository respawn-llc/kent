package serverapi

import (
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
	ProjectKey      string `json:"project_key"`
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
	ProjectKey    string `json:"project_key,omitempty"`
	WorkspaceRoot string `json:"workspace_root"`
}

type ProjectCreateResponse struct {
	Binding ProjectBinding `json:"binding"`
}

type ProjectHomeListRequest struct {
	PageSize  int    `json:"page_size"`
	PageToken string `json:"page_token"`
}

type ProjectHomeListResponse struct {
	Projects            []ProjectHomeSummary `json:"projects"`
	NextPageToken       string               `json:"next_page_token"`
	GeneratedAtUnixMs   int64                `json:"generated_at_unix_ms"`
	LatestEventSequence int64                `json:"latest_event_sequence"`
}

type ProjectHomeSummary struct {
	ProjectID            string                  `json:"project_id"`
	ProjectKey           string                  `json:"project_key"`
	DisplayName          string                  `json:"display_name"`
	PrimaryWorkspace     ProjectWorkspaceSummary `json:"primary_workspace"`
	DefaultWorkflowID    string                  `json:"default_workflow_id"`
	DefaultWorkflowName  string                  `json:"default_workflow_name"`
	DefaultWorkflowValid bool                    `json:"default_workflow_valid"`
	UpdatedAtUnixMs      int64                   `json:"updated_at_unix_ms"`
	TaskCount            int                     `json:"task_count"`
	AttentionCount       int                     `json:"attention_count"`
	WorkflowCount        int                     `json:"workflow_count"`
}

type ProjectWorkspaceListRequest struct {
	ProjectID string `json:"project_id"`
}

type ProjectWorkspaceListResponse struct {
	ProjectID          string                    `json:"project_id"`
	Workspaces         []ProjectWorkspaceSummary `json:"workspaces"`
	DefaultWorkspaceID string                    `json:"default_workspace_id"`
}

type ProjectWorkspaceSummary struct {
	WorkspaceID     string `json:"workspace_id"`
	DisplayName     string `json:"display_name"`
	RootPath        string `json:"root_path"`
	Availability    string `json:"availability"`
	IsPrimary       bool   `json:"is_primary"`
	UpdatedAtUnixMs int64  `json:"updated_at_unix_ms"`
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
	if trimmedKey := strings.TrimSpace(r.ProjectKey); trimmedKey != "" && !isValidProjectKey(trimmedKey) {
		return errors.New("project_key must match ^[A-Z][A-Z0-9]{1,7}$")
	}
	return nil
}

func (r ProjectHomeListRequest) Validate() error {
	if r.PageSize < 0 {
		return errors.New("page_size must be non-negative")
	}
	if strings.TrimSpace(r.PageToken) != r.PageToken {
		return errors.New("page_token must not have leading or trailing whitespace")
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

func (r ProjectWorkspaceListRequest) Validate() error {
	if strings.TrimSpace(r.ProjectID) == "" {
		return errors.New("project_id is required")
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

func isValidProjectKey(key string) bool {
	if len(key) < 2 || len(key) > 8 {
		return false
	}
	for index, r := range key {
		if index == 0 {
			if r < 'A' || r > 'Z' {
				return false
			}
			continue
		}
		if (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}
