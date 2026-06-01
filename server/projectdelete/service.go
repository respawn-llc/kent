package projectdelete

import (
	"context"
	"errors"
	"strings"

	"builder/server/metadata"
	"builder/server/projectgate"
	"builder/server/sessionartifact"
	shelltool "builder/server/tools/shell"
	"builder/server/workflowscheduler"
	"builder/shared/serverapi"
)

type RuntimeRegistry interface {
	IsSessionRuntimeActive(sessionID string) bool
	HasQueuedUserWork(sessionID string) bool
}

type Scheduler interface {
	ActiveRequests() []workflowscheduler.StartRunRequest
}

type ShellProcesses interface {
	List() []shelltool.Snapshot
}

type Service struct {
	metadata          *metadata.Store
	gate              *projectgate.Gate
	runtimes          RuntimeRegistry
	scheduler         Scheduler
	processes         ShellProcesses
	attachedProjectID string
}

type Option func(*Service)

func WithRuntimeRegistry(runtimes RuntimeRegistry) Option {
	return func(s *Service) {
		s.runtimes = runtimes
	}
}

func WithScheduler(scheduler Scheduler) Option {
	return func(s *Service) {
		s.scheduler = scheduler
	}
}

func WithProcesses(processes ShellProcesses) Option {
	return func(s *Service) {
		s.processes = processes
	}
}

func WithAttachedProjectID(projectID string) Option {
	return func(s *Service) {
		s.attachedProjectID = strings.TrimSpace(projectID)
	}
}

func New(metadataStore *metadata.Store, gate *projectgate.Gate, opts ...Option) (*Service, error) {
	if metadataStore == nil {
		return nil, errors.New("metadata store is required")
	}
	if gate == nil {
		gate = projectgate.New()
	}
	service := &Service{metadata: metadataStore, gate: gate}
	for _, opt := range opts {
		if opt != nil {
			opt(service)
		}
	}
	return service, nil
}

func (s *Service) SetAttachedProjectID(projectID string) {
	if s != nil {
		s.attachedProjectID = strings.TrimSpace(projectID)
	}
}

func (s *Service) PreviewProjectDelete(ctx context.Context, req serverapi.ProjectDeletePreviewRequest) (serverapi.ProjectDeletePreviewResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.ProjectDeletePreviewResponse{}, err
	}
	impact, err := s.previewImpact(ctx, req.ProjectID, false)
	if err != nil {
		return serverapi.ProjectDeletePreviewResponse{}, err
	}
	return serverapi.ProjectDeletePreviewResponse{Impact: impact}, nil
}

func (s *Service) DeleteProject(ctx context.Context, req serverapi.ProjectDeleteRequest) (serverapi.ProjectDeleteResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.ProjectDeleteResponse{}, err
	}
	if s == nil {
		return serverapi.ProjectDeleteResponse{}, errors.New("project delete service is required")
	}
	projectID := strings.TrimSpace(req.ProjectID)
	var response serverapi.ProjectDeleteResponse
	err := s.gate.WithProject(ctx, projectID, func(ctx context.Context) error {
		impact, err := s.previewImpact(ctx, projectID, false)
		if err != nil {
			return err
		}
		resumeExistingDelete := req.Resume && impact.ResumeRequired
		if !resumeExistingDelete && len(impact.Blockers) > 0 {
			response = serverapi.ProjectDeleteResponse{Deleted: false, Impact: impact, Blockers: impact.Blockers}
			return nil
		}
		prepared, err := s.metadata.PrepareProjectDelete(ctx, metadata.ProjectDeletePrepareRequest{
			ProjectID:                    projectID,
			ImpactToken:                  req.ImpactToken,
			ExpectedWorkspaceCount:       int64(req.ExpectedWorkspaceCount),
			ExpectedWorkflowLinkCount:    int64(req.ExpectedWorkflowLinkCount),
			ExpectedTaskCount:            int64(req.ExpectedTaskCount),
			ExpectedTerminalTaskCount:    int64(req.ExpectedTerminalTaskCount),
			ExpectedNonTerminalTaskCount: int64(req.ExpectedNonTerminalTaskCount),
			ExpectedSessionCount:         int64(req.ExpectedSessionCount),
			ExpectedSessionArtifactCount: int64(req.ExpectedSessionArtifactCount),
			Resume:                       resumeExistingDelete,
		})
		if err != nil {
			return err
		}
		warnings, cleanupBlocked, err := s.cleanArtifacts(ctx, projectID, prepared.Manifest)
		if err != nil {
			return err
		}
		latestImpact, err := s.previewImpact(ctx, projectID, true)
		if err != nil {
			return err
		}
		if cleanupBlocked {
			blocker := serverapi.ProjectDeleteBlocker{
				Code:    "artifact_cleanup_failed",
				Message: "Builder-owned session artifact cleanup did not complete. Fix the reported artifact path problem and retry project deletion.",
				Count:   latestImpact.FailedArtifactCount,
			}
			latestImpact.Blockers = append(latestImpact.Blockers, blocker)
			response = serverapi.ProjectDeleteResponse{Deleted: false, Impact: latestImpact, Blockers: latestImpact.Blockers, CleanupWarnings: warnings}
			return nil
		}
		if len(latestImpact.Blockers) > 0 {
			response = serverapi.ProjectDeleteResponse{Deleted: false, Impact: latestImpact, Blockers: latestImpact.Blockers, CleanupWarnings: warnings}
			return nil
		}
		if err := s.metadata.FinalizeProjectDelete(ctx, metadata.ProjectDeleteFinalizeRequest{ProjectID: projectID, ImpactToken: prepared.Impact.ImpactToken}); err != nil {
			return err
		}
		response = serverapi.ProjectDeleteResponse{Deleted: true, Impact: latestImpact, CleanupWarnings: warnings}
		return nil
	})
	if err != nil {
		if errors.Is(err, metadata.ErrProjectDeleteInProgress) || errors.Is(err, metadata.ErrProjectDeleteImpactChanged) {
			impact, previewErr := s.previewImpact(ctx, projectID, true)
			if previewErr == nil {
				return serverapi.ProjectDeleteResponse{Deleted: false, Impact: impact, Blockers: impact.Blockers}, nil
			}
		}
		return serverapi.ProjectDeleteResponse{}, err
	}
	return response, nil
}

func (s *Service) previewImpact(ctx context.Context, projectID string, resume bool) (serverapi.ProjectDeleteImpact, error) {
	if s == nil || s.metadata == nil {
		return serverapi.ProjectDeleteImpact{}, errors.New("project delete service is required")
	}
	base, err := s.metadata.GetProjectDeleteImpact(ctx, projectID)
	if err != nil {
		return serverapi.ProjectDeleteImpact{}, err
	}
	impact := impactFromMetadata(base)
	live, err := s.liveImpact(ctx, strings.TrimSpace(projectID))
	if err != nil {
		return serverapi.ProjectDeleteImpact{}, err
	}
	impact.LiveRuntimeSessionCount = live.LiveRuntimeSessionCount
	impact.RunningBackgroundProcessCount = live.RunningBackgroundProcessCount
	impact.QueuedWorkCount = live.QueuedWorkCount
	impact.SchedulerReservationCount = live.SchedulerReservationCount
	impact.ResumeRequired = strings.TrimSpace(impact.DeleteJobState) != ""
	impact.Blockers = s.blockersForImpact(impact, resume)
	return impact, nil
}

type liveProjectImpact struct {
	LiveRuntimeSessionCount       int
	RunningBackgroundProcessCount int
	QueuedWorkCount               int
	SchedulerReservationCount     int
}

func (s *Service) liveImpact(ctx context.Context, projectID string) (liveProjectImpact, error) {
	sessions, err := s.metadata.ListSessionsByProject(ctx, projectID)
	if err != nil {
		return liveProjectImpact{}, err
	}
	sessionIDs := make(map[string]struct{}, len(sessions))
	out := liveProjectImpact{}
	for _, session := range sessions {
		id := strings.TrimSpace(session.SessionID)
		if id == "" {
			continue
		}
		sessionIDs[id] = struct{}{}
		if s.runtimes != nil && s.runtimes.IsSessionRuntimeActive(id) {
			out.LiveRuntimeSessionCount++
		}
		if s.runtimes != nil && s.runtimes.HasQueuedUserWork(id) {
			out.QueuedWorkCount++
		}
	}
	if s.processes != nil {
		for _, process := range s.processes.List() {
			if !process.Running {
				continue
			}
			if _, ok := sessionIDs[strings.TrimSpace(process.OwnerSessionID)]; ok {
				out.RunningBackgroundProcessCount++
			}
		}
	}
	if s.scheduler != nil {
		for _, active := range s.scheduler.ActiveRequests() {
			runProjectID, err := s.metadata.ProjectIDForRun(ctx, string(active.RunID))
			if err != nil {
				return liveProjectImpact{}, err
			}
			if strings.TrimSpace(runProjectID) == projectID {
				out.SchedulerReservationCount++
			}
		}
	}
	return out, nil
}

func (s *Service) blockersForImpact(impact serverapi.ProjectDeleteImpact, resume bool) []serverapi.ProjectDeleteBlocker {
	blockers := []serverapi.ProjectDeleteBlocker{}
	add := func(code, message string, count int) {
		if count > 0 {
			blockers = append(blockers, serverapi.ProjectDeleteBlocker{Code: code, Message: message, Count: count})
		}
	}
	if impact.ResumeRequired && !resume {
		blockers = append(blockers, serverapi.ProjectDeleteBlocker{Code: "deletion_in_progress", Message: "Project deletion has already started. Retry deletion to resume cleanup."})
		return blockers
	}
	if !resume && strings.TrimSpace(s.attachedProjectID) == strings.TrimSpace(impact.ProjectID) {
		blockers = append(blockers, serverapi.ProjectDeleteBlocker{Code: "active_attached_project", Message: "The attached startup project cannot be deleted from this server process."})
	}
	add("non_terminal_tasks", "Project has active or non-terminal tasks.", impact.NonTerminalTaskCount)
	add("active_sessions", "Project has sessions with in-flight steps.", impact.ActiveSessionCount)
	add("active_node_placements", "Project has active workflow node placements.", impact.ActiveNodePlacementCount)
	add("pending_approvals", "Project has workflow transitions pending approval.", impact.PendingApprovalCount)
	add("waiting_questions", "Project has workflow task questions waiting for an answer.", impact.WaitingQuestionCount)
	add("active_runs", "Project has active workflow runs.", impact.ActiveRunCount)
	add("runnable_runs", "Project has runnable workflow runs.", impact.RunnableRunCount)
	add("live_runtime_sessions", "Project has live runtime sessions.", impact.LiveRuntimeSessionCount)
	add("running_background_processes", "Project has running background processes.", impact.RunningBackgroundProcessCount)
	add("queued_work", "Project has queued runtime work.", impact.QueuedWorkCount)
	add("scheduler_reservations", "Project has workflow scheduler reservations.", impact.SchedulerReservationCount)
	add("data_integrity", "Project has task runs linked to sessions from another project.", impact.CrossProjectRunSessionCount)
	return blockers
}

func (s *Service) cleanArtifacts(ctx context.Context, projectID string, entries []metadata.ProjectDeleteArtifactEntry) ([]serverapi.ProjectDeleteWarning, bool, error) {
	warnings := []serverapi.ProjectDeleteWarning{}
	cleanupBlocked := false
	for _, entry := range entries {
		state := strings.TrimSpace(entry.State)
		if state == "cleaned" || state == "missing" || state == "skipped_not_builder_owned" {
			result, err := sessionartifact.CleanProjectSessionDir(ctx, s.metadata.PersistenceRoot(), projectID, entry.SessionID, entry.ArtifactRelpath)
			if err != nil {
				if updateErr := s.metadata.UpdateProjectDeleteArtifactState(ctx, metadata.ProjectDeleteArtifactStateRequest{ProjectID: projectID, SessionID: entry.SessionID, State: string(sessionartifact.StateFailed), LastError: err.Error()}); updateErr != nil {
					return nil, false, updateErr
				}
				cleanupBlocked = true
				continue
			}
			state = string(result.State)
			if err := s.metadata.UpdateProjectDeleteArtifactState(ctx, metadata.ProjectDeleteArtifactStateRequest{ProjectID: projectID, SessionID: entry.SessionID, State: state}); err != nil {
				return nil, false, err
			}
		}
		if state != "pending" && state != "failed" {
			if state == "skipped_not_builder_owned" {
				warnings = append(warnings, serverapi.ProjectDeleteWarning{Code: "skipped_not_builder_owned", Message: "A session artifact path was not recognized as a Builder-owned project session directory and was skipped.", SessionID: entry.SessionID})
			}
			continue
		}
		result, err := sessionartifact.CleanProjectSessionDir(ctx, s.metadata.PersistenceRoot(), projectID, entry.SessionID, entry.ArtifactRelpath)
		if err != nil {
			if updateErr := s.metadata.UpdateProjectDeleteArtifactState(ctx, metadata.ProjectDeleteArtifactStateRequest{ProjectID: projectID, SessionID: entry.SessionID, State: string(sessionartifact.StateFailed), LastError: err.Error()}); updateErr != nil {
				return nil, false, updateErr
			}
			cleanupBlocked = true
			continue
		}
		if err := s.metadata.UpdateProjectDeleteArtifactState(ctx, metadata.ProjectDeleteArtifactStateRequest{ProjectID: projectID, SessionID: entry.SessionID, State: string(result.State)}); err != nil {
			return nil, false, err
		}
		if result.State == sessionartifact.StateSkippedNotBuilderOwned {
			warnings = append(warnings, serverapi.ProjectDeleteWarning{Code: "skipped_not_builder_owned", Message: "A session artifact path was not recognized as a Builder-owned project session directory and was skipped.", SessionID: entry.SessionID})
		}
	}
	return warnings, cleanupBlocked, nil
}

func impactFromMetadata(impact metadata.ProjectDeleteImpact) serverapi.ProjectDeleteImpact {
	return serverapi.ProjectDeleteImpact{
		ProjectID:                   impact.ProjectID,
		ProjectKey:                  impact.ProjectKey,
		DisplayName:                 impact.DisplayName,
		WorkspaceCount:              int(impact.WorkspaceCount),
		WorkflowLinkCount:           int(impact.WorkflowLinkCount),
		TaskCount:                   int(impact.TaskCount),
		TerminalTaskCount:           int(impact.TerminalTaskCount),
		NonTerminalTaskCount:        int(impact.NonTerminalTaskCount),
		SessionCount:                int(impact.SessionCount),
		SessionArtifactCount:        int(impact.SessionArtifactCount),
		ActiveSessionCount:          int(impact.ActiveSessionCount),
		ActiveNodePlacementCount:    int(impact.ActiveNodePlacementCount),
		PendingApprovalCount:        int(impact.PendingApprovalCount),
		WaitingQuestionCount:        int(impact.WaitingQuestionCount),
		ActiveRunCount:              int(impact.ActiveRunCount),
		RunnableRunCount:            int(impact.RunnableRunCount),
		CrossProjectRunSessionCount: int(impact.CrossProjectRunSessionCount),
		ImpactToken:                 impact.ImpactToken,
		DeleteJobState:              impact.DeleteJobState,
		PendingArtifactCount:        int(impact.PendingArtifactCount),
		CleanedArtifactCount:        int(impact.CleanedArtifactCount),
		MissingArtifactCount:        int(impact.MissingArtifactCount),
		FailedArtifactCount:         int(impact.FailedArtifactCount),
		SkippedNotBuilderOwnedCount: int(impact.SkippedNotBuilderOwnedCount),
	}
}
