package projectview

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"builder/server/metadata"
	"builder/shared/clientui"
	"builder/shared/serverapi"
	"builder/shared/servicecontract"
)

type Service struct {
	metadata     *metadata.Store
	projectID    string
	containerDir string
	syncOnce     sync.Once
	syncErr      error
}

const (
	defaultProjectHomePageSize = 50
	maxProjectHomePageSize     = 100
)

func NewMetadataService(metadataStore *metadata.Store, projectID string, containerDir string) (*Service, error) {
	if metadataStore == nil {
		return nil, errors.New("metadata store is required")
	}
	return &Service{metadata: metadataStore, projectID: strings.TrimSpace(projectID), containerDir: strings.TrimSpace(containerDir)}, nil
}

func (s *Service) ProjectID() string {
	if s == nil {
		return ""
	}
	return s.projectID
}

func (s *Service) ListProjects(ctx context.Context, _ serverapi.ProjectListRequest) (serverapi.ProjectListResponse, error) {
	if s == nil {
		return serverapi.ProjectListResponse{}, errors.New("project service is required")
	}
	if err := s.syncMetadata(ctx); err != nil {
		return serverapi.ProjectListResponse{}, err
	}
	projects, err := s.metadata.ListProjects(ctx)
	if err != nil {
		return serverapi.ProjectListResponse{}, err
	}
	if trimmedProjectID := strings.TrimSpace(s.projectID); trimmedProjectID != "" {
		filtered := make([]clientui.ProjectSummary, 0, 1)
		for _, project := range projects {
			if strings.TrimSpace(project.ProjectID) == trimmedProjectID {
				filtered = append(filtered, project)
				break
			}
		}
		return serverapi.ProjectListResponse{Projects: filtered}, nil
	}
	return serverapi.ProjectListResponse{Projects: projects}, nil
}

func (s *Service) ListProjectHome(ctx context.Context, req serverapi.ProjectHomeListRequest) (serverapi.ProjectHomeListResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.ProjectHomeListResponse{}, err
	}
	if s == nil {
		return serverapi.ProjectHomeListResponse{}, errors.New("project service is required")
	}
	if err := s.syncMetadata(ctx); err != nil {
		return serverapi.ProjectHomeListResponse{}, err
	}
	pageSize := req.PageSize
	if pageSize == 0 {
		pageSize = defaultProjectHomePageSize
	}
	if pageSize > maxProjectHomePageSize {
		pageSize = maxProjectHomePageSize
	}
	offset, err := parseProjectHomePageToken(req.PageToken)
	if err != nil {
		return serverapi.ProjectHomeListResponse{}, err
	}
	summaries, err := s.metadata.ListProjectHomeSummaries(ctx, pageSize+1, offset)
	if err != nil {
		return serverapi.ProjectHomeListResponse{}, err
	}
	if trimmedProjectID := strings.TrimSpace(s.projectID); trimmedProjectID != "" {
		filtered := make([]serverapi.ProjectHomeSummary, 0, 1)
		for _, summary := range summaries {
			if strings.TrimSpace(summary.ProjectID) == trimmedProjectID {
				filtered = append(filtered, summary)
				break
			}
		}
		summaries = filtered
	}
	nextPageToken := ""
	if len(summaries) > pageSize {
		summaries = summaries[:pageSize]
		nextPageToken = strconv.Itoa(offset + pageSize)
	}
	return serverapi.ProjectHomeListResponse{
		Projects:            summaries,
		NextPageToken:       nextPageToken,
		GeneratedAtUnixMs:   time.Now().UTC().UnixMilli(),
		LatestEventSequence: 0,
	}, nil
}

func (s *Service) ResolveProjectPath(ctx context.Context, req serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.ProjectResolvePathResponse{}, err
	}
	if s == nil {
		return serverapi.ProjectResolvePathResponse{}, errors.New("project service is required")
	}
	canonicalRoot, binding, err := s.metadata.ResolveWorkspacePath(ctx, req.Path)
	if err != nil {
		return serverapi.ProjectResolvePathResponse{}, err
	}
	resp := serverapi.ProjectResolvePathResponse{CanonicalRoot: canonicalRoot}
	resp.PathAvailability = clientui.ProjectAvailability(availabilityForProjectPath(canonicalRoot))
	if binding != nil {
		mapped := projectBindingFromMetadata(*binding)
		resp.Binding = &mapped
	}
	return resp, nil
}

func (s *Service) PlanWorkspaceBinding(ctx context.Context, req serverapi.ProjectBindingPlanRequest) (serverapi.ProjectBindingPlanResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.ProjectBindingPlanResponse{}, err
	}
	resolved, err := s.ResolveProjectPath(ctx, serverapi.ProjectResolvePathRequest{Path: req.Path})
	if err != nil {
		return serverapi.ProjectBindingPlanResponse{}, err
	}
	resp := serverapi.ProjectBindingPlanResponse{
		CanonicalRoot:    resolved.CanonicalRoot,
		PathAvailability: resolved.PathAvailability,
		Binding:          resolved.Binding,
	}
	if resolved.Binding != nil {
		resp.Kind = serverapi.ProjectBindingPlanKindBound
		return resp, nil
	}
	switch req.Mode {
	case serverapi.ProjectBindingPlanModeInteractive:
		projects, err := s.ListProjects(ctx, serverapi.ProjectListRequest{})
		if err != nil {
			return serverapi.ProjectBindingPlanResponse{}, err
		}
		resp.Projects = projects.Projects
		if resolved.PathAvailability == clientui.ProjectAvailabilityMissing || resolved.PathAvailability == clientui.ProjectAvailabilityInaccessible {
			resp.Kind = serverapi.ProjectBindingPlanKindServerWorkspaceSelection
			return resp, nil
		}
		resp.Kind = serverapi.ProjectBindingPlanKindLocalUnbound
		return resp, nil
	case serverapi.ProjectBindingPlanModeHeadless:
		if resolved.PathAvailability == clientui.ProjectAvailabilityAvailable {
			resp.Kind = serverapi.ProjectBindingPlanKindLocalUnbound
			return resp, nil
		}
		workspace, found, err := s.selectSingleAvailableWorkspace(ctx)
		if err != nil {
			return serverapi.ProjectBindingPlanResponse{}, err
		}
		if !found {
			resp.Kind = serverapi.ProjectBindingPlanKindHeadlessRemoteAmbiguous
			return resp, nil
		}
		resp.Kind = serverapi.ProjectBindingPlanKindHeadlessRemoteSelected
		resp.Workspace = &workspace
		return resp, nil
	default:
		return serverapi.ProjectBindingPlanResponse{}, errors.New("mode must be interactive or headless")
	}
}

func (s *Service) CreateProject(ctx context.Context, req serverapi.ProjectCreateRequest) (serverapi.ProjectCreateResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.ProjectCreateResponse{}, err
	}
	if s == nil {
		return serverapi.ProjectCreateResponse{}, errors.New("project service is required")
	}
	binding, err := s.metadata.CreateProjectForWorkspaceWithKey(ctx, req.WorkspaceRoot, req.DisplayName, req.ProjectKey)
	if err != nil {
		return serverapi.ProjectCreateResponse{}, err
	}
	return serverapi.ProjectCreateResponse{Binding: projectBindingFromMetadata(binding)}, nil
}

func (s *Service) selectSingleAvailableWorkspace(ctx context.Context) (serverapi.ProjectWorkspacePlanSelected, bool, error) {
	projects, err := s.ListProjects(ctx, serverapi.ProjectListRequest{})
	if err != nil {
		return serverapi.ProjectWorkspacePlanSelected{}, false, err
	}
	selection := serverapi.ProjectWorkspacePlanSelected{}
	count := 0
	for _, project := range projects.Projects {
		overview, err := s.GetProjectOverview(ctx, serverapi.ProjectGetOverviewRequest{ProjectID: project.ProjectID})
		if err != nil {
			return serverapi.ProjectWorkspacePlanSelected{}, false, err
		}
		for _, workspace := range overview.Overview.Workspaces {
			availability := strings.TrimSpace(string(workspace.Availability))
			if availability != "" && workspace.Availability != clientui.ProjectAvailabilityAvailable {
				continue
			}
			count++
			selection = serverapi.ProjectWorkspacePlanSelected{ProjectID: project.ProjectID, WorkspaceID: workspace.WorkspaceID}
			if count > 1 {
				return serverapi.ProjectWorkspacePlanSelected{}, false, nil
			}
		}
	}
	if count == 0 {
		return serverapi.ProjectWorkspacePlanSelected{}, false, nil
	}
	return selection, true, nil
}

func (s *Service) ListProjectWorkspaces(ctx context.Context, req serverapi.ProjectWorkspaceListRequest) (serverapi.ProjectWorkspaceListResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.ProjectWorkspaceListResponse{}, err
	}
	if s == nil {
		return serverapi.ProjectWorkspaceListResponse{}, errors.New("project service is required")
	}
	if err := s.requireProjectID(req.ProjectID); err != nil {
		return serverapi.ProjectWorkspaceListResponse{}, err
	}
	workspaces, err := s.metadata.ListProjectWorkspaces(ctx, req.ProjectID)
	if err != nil {
		return serverapi.ProjectWorkspaceListResponse{}, err
	}
	response := serverapi.ProjectWorkspaceListResponse{
		ProjectID:  strings.TrimSpace(req.ProjectID),
		Workspaces: make([]serverapi.ProjectWorkspaceSummary, 0, len(workspaces)),
	}
	for _, workspace := range workspaces {
		summary := projectWorkspaceSummaryFromClientUI(workspace)
		response.Workspaces = append(response.Workspaces, summary)
		if workspace.IsPrimary {
			response.DefaultWorkspaceID = workspace.WorkspaceID
		}
	}
	return response, nil
}

func (s *Service) AttachWorkspaceToProject(ctx context.Context, req serverapi.ProjectAttachWorkspaceRequest) (serverapi.ProjectAttachWorkspaceResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.ProjectAttachWorkspaceResponse{}, err
	}
	if s == nil {
		return serverapi.ProjectAttachWorkspaceResponse{}, errors.New("project service is required")
	}
	if err := s.requireProjectID(req.ProjectID); err != nil {
		return serverapi.ProjectAttachWorkspaceResponse{}, err
	}
	binding, err := s.metadata.AttachWorkspaceToProject(ctx, req.ProjectID, req.WorkspaceRoot)
	if err != nil {
		return serverapi.ProjectAttachWorkspaceResponse{}, err
	}
	return serverapi.ProjectAttachWorkspaceResponse{Binding: projectBindingFromMetadata(binding)}, nil
}

func (s *Service) RebindWorkspace(ctx context.Context, req serverapi.ProjectRebindWorkspaceRequest) (serverapi.ProjectRebindWorkspaceResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.ProjectRebindWorkspaceResponse{}, err
	}
	if s == nil {
		return serverapi.ProjectRebindWorkspaceResponse{}, errors.New("project service is required")
	}
	binding, err := s.metadata.RebindWorkspace(ctx, req.OldWorkspaceRoot, req.NewWorkspaceRoot)
	if err != nil {
		return serverapi.ProjectRebindWorkspaceResponse{}, err
	}
	return serverapi.ProjectRebindWorkspaceResponse{Binding: projectBindingFromMetadata(binding)}, nil
}

func (s *Service) GetProjectOverview(ctx context.Context, req serverapi.ProjectGetOverviewRequest) (serverapi.ProjectGetOverviewResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.ProjectGetOverviewResponse{}, err
	}
	if err := s.syncMetadata(ctx); err != nil {
		return serverapi.ProjectGetOverviewResponse{}, err
	}
	if err := s.requireProjectID(req.ProjectID); err != nil {
		return serverapi.ProjectGetOverviewResponse{}, err
	}
	overview, err := s.metadata.GetProjectOverview(ctx, req.ProjectID)
	if err != nil {
		return serverapi.ProjectGetOverviewResponse{}, err
	}
	return serverapi.ProjectGetOverviewResponse{Overview: overview}, nil
}

func (s *Service) ListSessionsByProject(ctx context.Context, req serverapi.SessionListByProjectRequest) (serverapi.SessionListByProjectResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.SessionListByProjectResponse{}, err
	}
	if err := s.syncMetadata(ctx); err != nil {
		return serverapi.SessionListByProjectResponse{}, err
	}
	if err := s.requireProjectID(req.ProjectID); err != nil {
		return serverapi.SessionListByProjectResponse{}, err
	}
	sessions, err := s.metadata.ListSessionsByProject(ctx, req.ProjectID)
	if err != nil {
		return serverapi.SessionListByProjectResponse{}, err
	}
	return serverapi.SessionListByProjectResponse{Sessions: sessions}, nil
}

func (s *Service) requireProjectID(projectID string) error {
	if s == nil {
		return errors.New("project service is required")
	}
	if trimmedProjectID := strings.TrimSpace(s.projectID); trimmedProjectID != "" && strings.TrimSpace(projectID) != trimmedProjectID {
		return fmt.Errorf("project %q not available", strings.TrimSpace(projectID))
	}
	return nil
}

func parseProjectHomePageToken(token string) (int, error) {
	trimmed := strings.TrimSpace(token)
	if trimmed == "" {
		return 0, nil
	}
	offset, err := strconv.Atoi(trimmed)
	if err != nil || offset < 0 {
		return 0, errors.New("page_token is invalid")
	}
	return offset, nil
}

func availabilityForProjectPath(path string) string {
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return string(clientui.ProjectAvailabilityMissing)
		}
		return string(clientui.ProjectAvailabilityInaccessible)
	}
	return string(clientui.ProjectAvailabilityAvailable)
}

func projectWorkspaceSummaryFromClientUI(workspace clientui.ProjectWorkspaceSummary) serverapi.ProjectWorkspaceSummary {
	return serverapi.ProjectWorkspaceSummary{
		WorkspaceID:     workspace.WorkspaceID,
		DisplayName:     workspace.DisplayName,
		RootPath:        workspace.RootPath,
		Availability:    string(workspace.Availability),
		IsPrimary:       workspace.IsPrimary,
		UpdatedAtUnixMs: workspace.UpdatedAt.UnixMilli(),
	}
}

func projectBindingFromMetadata(binding metadata.Binding) serverapi.ProjectBinding {
	return serverapi.ProjectBinding{
		ProjectID:       binding.ProjectID,
		ProjectKey:      binding.ProjectKey,
		ProjectName:     binding.ProjectName,
		WorkspaceID:     binding.WorkspaceID,
		CanonicalRoot:   binding.CanonicalRoot,
		WorkspaceName:   binding.WorkspaceName,
		WorkspaceStatus: binding.WorkspaceStatus,
	}
}

func (s *Service) syncMetadata(ctx context.Context) error {
	if s == nil || s.metadata == nil || strings.TrimSpace(s.containerDir) == "" {
		return nil
	}
	s.syncOnce.Do(func() {
		s.syncErr = s.metadata.SyncLegacyContainer(context.WithoutCancel(ctx), s.containerDir)
	})
	return s.syncErr
}

var _ servicecontract.ProjectViewService = (*Service)(nil)
