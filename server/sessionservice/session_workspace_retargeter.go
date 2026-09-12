package sessionservice

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"core/server/metadata"
	"core/server/runtimewire"
	"core/server/session"
	sessionruntime "core/server/sessionruntime"
	"core/server/tools"
	shelltool "core/server/tools/shell"
	"core/shared/config"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/worktreecontract"
)

type sessionRetargetMetadata interface {
	PlanSessionWorkspaceRetarget(context.Context, metadata.SessionWorkspaceRetargetRequest) (metadata.SessionWorkspaceRetargetPlan, error)
	CommitSessionWorkspaceRetarget(context.Context, metadata.SessionWorkspaceRetargetPlan, time.Time) (metadata.SessionWorkspaceRetargetResult, error)
	ResolveProjectWorkspaceBoundary(context.Context, string) (metadata.ProjectWorkspaceBoundary, error)
	ProjectWorkspaceAttached(context.Context, string, string) (bool, error)
	ResolveSessionExecutionTarget(context.Context, string) (*worktreepb.SessionExecutionTarget, error)
}

type sessionIdentityPublisher interface {
	PublishSessionIdentity(sessionID string) error
}

type sessionProcessSource interface {
	List() []shelltool.Snapshot
}

type SessionWorkspaceRetargeter struct {
	metadata  sessionRetargetMetadata
	authority *sessionruntime.Authority
	publisher sessionIdentityPublisher
	processes sessionProcessSource
}

func NewSessionWorkspaceRetargeter(
	metadataStore sessionRetargetMetadata,
	authority *sessionruntime.Authority,
	publisher sessionIdentityPublisher,
	processes sessionProcessSource,
) *SessionWorkspaceRetargeter {
	return &SessionWorkspaceRetargeter{
		metadata:  metadataStore,
		authority: authority,
		publisher: publisher,
		processes: processes,
	}
}

func (s *SessionWorkspaceRetargeter) RetargetWorkspace(ctx context.Context, req metadata.SessionWorkspaceRetargetRequest) (*sessionlaunchpb.SessionRetargetWorkspaceSuccess, error) {
	return s.retargetWorkspaceResolution(ctx, req.SessionID, nil, worktreecontract.NewOperationID(),
		func(context.Context) (metadata.SessionWorkspaceRetargetRequest, error) { return req, nil }, nil)
}

func (s *SessionWorkspaceRetargeter) ScheduleWorkspaceRetarget(
	ctx context.Context,
	req metadata.SessionWorkspaceRetargetRequest,
	origin *sessionlaunchpb.RuntimeStepOrigin,
	operationID worktreecontract.OperationID,
) (*worktreepb.ScheduledAcknowledgement, error) {
	return s.ScheduleWorkspaceRetargetResolutionWithCompletion(
		ctx,
		req.SessionID,
		origin,
		operationID,
		func(context.Context) (metadata.SessionWorkspaceRetargetRequest, error) {
			return req, nil
		},
		nil,
	)
}

func (s *SessionWorkspaceRetargeter) ScheduleWorkspaceRetargetResolutionWithCompletion(
	ctx context.Context,
	sessionID string,
	origin *sessionlaunchpb.RuntimeStepOrigin,
	operationID worktreecontract.OperationID,
	resolve func(context.Context) (metadata.SessionWorkspaceRetargetRequest, error),
	completion func(error),
) (*worktreepb.ScheduledAcknowledgement, error) {
	_, err := s.retargetWorkspaceResolution(ctx, sessionID, origin, operationID, resolve, completion)
	if err != nil {
		return &worktreepb.ScheduledAcknowledgement{}, err
	}
	return &worktreepb.ScheduledAcknowledgement{OperationId: operationID.String()}, nil
}

func (s *SessionWorkspaceRetargeter) retargetWorkspaceResolution(
	ctx context.Context,
	sessionID string,
	origin *sessionlaunchpb.RuntimeStepOrigin,
	operationID worktreecontract.OperationID,
	resolve func(context.Context) (metadata.SessionWorkspaceRetargetRequest, error),
	completion func(error),
) (*sessionlaunchpb.SessionRetargetWorkspaceSuccess, error) {
	if s == nil || s.metadata == nil || s.authority == nil || s.publisher == nil || s.processes == nil {
		return &sessionlaunchpb.SessionRetargetWorkspaceSuccess{}, errors.New("session workspace retarget dependencies are required")
	}
	if resolve == nil {
		return &sessionlaunchpb.SessionRetargetWorkspaceSuccess{}, errors.New("session workspace retarget resolver is required")
	}
	parsedSessionID, err := runtimeids.ParseSessionID(strings.TrimSpace(sessionID))
	if err != nil {
		return &sessionlaunchpb.SessionRetargetWorkspaceSuccess{}, err
	}
	sessionID = parsedSessionID.String()
	if err := context.Cause(ctx); err != nil {
		return &sessionlaunchpb.SessionRetargetWorkspaceSuccess{}, err
	}
	type outcome struct {
		response *sessionlaunchpb.SessionRetargetWorkspaceSuccess
		err      error
	}
	result := make(chan outcome, 1)
	runCtx, cancelRun := context.WithCancel(context.WithoutCancel(ctx))
	go func() {
		defer cancelRun()
		var completionErr error
		var scheduled *worktreepb.ScheduledAcknowledgement
		var completed *sessionlaunchpb.SessionRetargetWorkspaceSuccess
		defer func() {
			if completion != nil {
				completion(completionErr)
			}
			if scheduled == nil {
				result <- outcome{response: completed, err: completionErr}
			}
		}()
		failurePersisted := false
		var publicationErr error
		var failureCause error
		var plan *metadata.SessionWorkspaceRetargetPlan
		var sourceWorkdir string
		runErr := s.authority.RunSessionMaintenanceAtStepBoundary(
			runCtx,
			sessionID,
			origin,
			func() {
				scheduled = &worktreepb.ScheduledAcknowledgement{OperationId: operationID.String()}
				result <- outcome{response: &sessionlaunchpb.SessionRetargetWorkspaceSuccess{Scheduled: scheduled}}
			},
			func(boundaryCtx context.Context, store *session.Store, activeRuntime *sessionruntime.ActiveRuntimeMaintenance) (callbackErr error) {
				steerUnplannedFailure := func(cause error) error {
					if activeRuntime == nil {
						return cause
					}
					receipt, steerErr := activeRuntime.SteerSessionRebindFailureDiagnostic(cause)
					failurePersisted = receipt.Committed
					return errors.Join(cause, steerErr)
				}
				req, resolveErr := resolve(boundaryCtx)
				if resolveErr != nil {
					failureCause = resolveErr
					return steerUnplannedFailure(resolveErr)
				}
				if strings.TrimSpace(req.SessionID) != sessionID {
					failureCause = errors.New("resolved Session workspace retarget does not match the scheduled Session")
					return steerUnplannedFailure(failureCause)
				}
				currentPlan, planErr := s.metadata.PlanSessionWorkspaceRetarget(boundaryCtx, req)
				if planErr != nil {
					failureCause = planErr
					return steerUnplannedFailure(planErr)
				}
				plan = &currentPlan
				sourceTarget, targetErr := s.metadata.ResolveSessionExecutionTarget(boundaryCtx, currentPlan.SessionID)
				if targetErr != nil {
					failureCause = targetErr
					return steerUnplannedFailure(targetErr)
				}
				sourceWorkdir = sourceTarget.EffectiveWorkdir
				steerPlannedFailure := func(cause error) error {
					if activeRuntime == nil {
						return cause
					}
					reminder := rebindFailureReminder(currentPlan, sourceWorkdir, cause)
					receipt, steerErr := activeRuntime.SteerSessionRebindFailure(reminder)
					failurePersisted = receipt.Committed
					if failurePersisted {
						return errors.Join(cause, steerErr)
					}
					persistErr := store.SetSessionRebindReminder(&reminder)
					failurePersisted = persistErr == nil
					return errors.Join(cause, steerErr, persistErr)
				}
				applyCtx := boundaryCtx
				var releaseStarts sessionruntime.SessionStartBlockRelease
				if activeRuntime != nil && currentPlan.CrossProject() {
					releaseStarts, applyCtx, planErr = s.blockSessionStarts(boundaryCtx, currentPlan.SessionID)
					if planErr != nil {
						failureCause = planErr
						return steerPlannedFailure(planErr)
					}
					defer func() {
						callbackErr = errors.Join(callbackErr, releaseStarts.Close(context.Background()))
					}()
				}
				applied, applyErr := s.applyWorkspaceRetarget(applyCtx, req, store, activeRuntime, activeRuntime != nil)
				if applyErr != nil {
					failureCause = applyErr
					return steerPlannedFailure(applyErr)
				}
				var projectionErr error
				completed, projectionErr = completedWorkspaceRetargetResponse(applied)
				if projectionErr != nil {
					return projectionErr
				}
				publicationErr = s.publisher.PublishSessionIdentity(currentPlan.SessionID)
				return nil
			},
		)
		if scheduled != nil {
			if runErr != nil {
				completionErr = runErr
				persistCtx := context.WithoutCancel(runCtx)
				if failureCause == nil {
					failureCause = runErr
				}
				if !failurePersisted && plan != nil {
					persistErr := s.persistRebindFailure(persistCtx, *plan, sourceWorkdir, failureCause)
					if persistErr == nil {
						return
					}
					slog.ErrorContext(
						persistCtx,
						"persist scheduled Session rebind failure notice",
						"session_id", plan.SessionID,
						"rebind_error", runErr,
						"persistence_error", persistErr,
					)
				}
				if !failurePersisted && plan == nil {
					slog.ErrorContext(persistCtx, "scheduled Session rebind failed before a location plan was available", "session_id", sessionID, "error", runErr)
				}
				return
			}
			if publicationErr != nil {
				slog.ErrorContext(
					context.WithoutCancel(runCtx),
					"publish scheduled Session rebind identity",
					"session_id", plan.SessionID,
					"error", publicationErr,
				)
			}
			return
		}
		completionErr = runErr
		if publicationErr != nil {
			slog.ErrorContext(runCtx, "publish committed Session rebind identity", "session_id", sessionID, "error", publicationErr)
		}
	}()
	select {
	case outcome := <-result:
		return outcome.response, outcome.err
	case <-ctx.Done():
		return &sessionlaunchpb.SessionRetargetWorkspaceSuccess{}, context.Cause(ctx)
	}
}

func (s *SessionWorkspaceRetargeter) blockSessionStarts(
	ctx context.Context,
	sessionID string,
) (sessionruntime.SessionStartBlockRelease, context.Context, error) {
	id, err := runtimeids.ParseSessionID(strings.TrimSpace(sessionID))
	if err != nil {
		return nil, nil, err
	}
	release, err := s.authority.BlockSessionStarts(
		ctx,
		[]runtimeids.SessionID{id},
		sessionruntime.SessionStartBlockMaintenance,
	)
	if err != nil {
		return nil, nil, err
	}
	return release, release.AuthorizeMaintenance(ctx), nil
}

func (s *SessionWorkspaceRetargeter) applyWorkspaceRetarget(
	ctx context.Context,
	req metadata.SessionWorkspaceRetargetRequest,
	store *session.Store,
	activeRuntime *sessionruntime.ActiveRuntimeMaintenance,
	ignoreBackgroundProcesses bool,
) (metadata.SessionWorkspaceRetargetResult, error) {
	currentPlan, err := s.metadata.PlanSessionWorkspaceRetarget(ctx, req)
	if err != nil {
		return metadata.SessionWorkspaceRetargetResult{}, err
	}
	sourceTarget, err := s.metadata.ResolveSessionExecutionTarget(ctx, currentPlan.SessionID)
	if err != nil {
		return metadata.SessionWorkspaceRetargetResult{}, err
	}
	if !ignoreBackgroundProcesses {
		ownedProcessActive, processErr := s.ownedBackgroundProcessActive(currentPlan.SessionID)
		if processErr != nil {
			return metadata.SessionWorkspaceRetargetResult{}, processErr
		}
		if ownedProcessActive {
			return metadata.SessionWorkspaceRetargetResult{}, &serverapi.SessionRetargetError{
				Reason:        serverapi.SessionRetargetBackgroundProcess,
				SessionID:     currentPlan.SessionID,
				SourceProject: currentPlan.SourceProject,
				TargetRoot:    currentPlan.TargetWorkspaceRoot,
			}
		}
	}
	if store == nil {
		return metadata.SessionWorkspaceRetargetResult{}, errors.New("session store is required")
	}
	storeDir, err := config.CanonicalWorkspaceRoot(store.Dir())
	if err != nil {
		return metadata.SessionWorkspaceRetargetResult{}, fmt.Errorf("canonicalize session store path: %w", err)
	}
	sourceDir, err := config.CanonicalWorkspaceRoot(currentPlan.SourceSessionDir)
	if err != nil {
		return metadata.SessionWorkspaceRetargetResult{}, fmt.Errorf("canonicalize source session artifact path: %w", err)
	}
	if storeDir != sourceDir {
		return metadata.SessionWorkspaceRetargetResult{}, fmt.Errorf("session store path %q does not match source artifact %q", store.Dir(), currentPlan.SourceSessionDir)
	}
	if err := validateSessionArtifactSource(currentPlan.SourceSessionDir); err != nil {
		return metadata.SessionWorkspaceRetargetResult{}, err
	}
	targetFilesystemContext, err := s.targetFilesystemContext(ctx, currentPlan, activeRuntime)
	if err != nil {
		return metadata.SessionWorkspaceRetargetResult{}, err
	}
	if currentPlan.CrossProject() {
		if err := prepareSessionArtifactTarget(currentPlan.TargetSessionDir); err != nil {
			return metadata.SessionWorkspaceRetargetResult{}, err
		}
	}
	workingDirectoryChanged := sourceTarget.EffectiveWorkdir != currentPlan.TargetExecutionRoot
	if currentPlan.CrossProject() || workingDirectoryChanged {
		workingDirectory := currentPlan.TargetExecutionRoot
		currentPlan.RebindReminder = &session.SessionRebindReminder{
			Kind:          session.SessionRebindReminderSucceeded,
			SourceProject: currentPlan.SourceProject,
			TargetProject: currentPlan.TargetProject,
		}
		if workingDirectoryChanged {
			currentPlan.RebindReminder.WorkingDirectory = &workingDirectory
		}
	}
	updatedAt := time.Now().UTC()
	var result metadata.SessionWorkspaceRetargetResult
	err = store.RunArtifactRelocation(session.ArtifactRelocationTarget{
		SessionDir:         currentPlan.TargetSessionDir,
		WorkspaceRoot:      currentPlan.TargetWorkspaceRoot,
		WorkspaceContainer: filepath.Base(currentPlan.TargetWorkspaceRoot),
		UpdatedAt:          updatedAt,
		RebindReminder:     currentPlan.RebindReminder,
		WorktreeReminder:   currentPlan.WorktreeReminder,
	}, func() error {
		if activeRuntime != nil {
			if err := activeRuntime.Replace(targetFilesystemContext); err != nil {
				return err
			}
		}
		runtimeRebound := activeRuntime != nil
		rollbackRuntime := func() error {
			if !runtimeRebound {
				return nil
			}
			runtimeRebound = false
			return activeRuntime.Replace(activeRuntime.PreviousFilesystemContext)
		}
		moved := false
		if currentPlan.CrossProject() {
			if err := os.Rename(currentPlan.SourceSessionDir, currentPlan.TargetSessionDir); err != nil {
				return errors.Join(fmt.Errorf("move session artifact: %w", err), rollbackRuntime())
			}
			moved = true
		}
		result, err = s.metadata.CommitSessionWorkspaceRetarget(ctx, currentPlan, updatedAt)
		if err != nil {
			var rollbackErr error
			if moved {
				if moveErr := os.Rename(currentPlan.TargetSessionDir, currentPlan.SourceSessionDir); moveErr != nil {
					rollbackErr = fmt.Errorf("restore session artifact: %w", moveErr)
				}
			}
			return errors.Join(err, rollbackErr, rollbackRuntime())
		}
		runtimeRebound = false
		return nil
	})
	return result, err
}

func (s *SessionWorkspaceRetargeter) targetFilesystemContext(
	ctx context.Context,
	plan metadata.SessionWorkspaceRetargetPlan,
	activeRuntime *sessionruntime.ActiveRuntimeMaintenance,
) (tools.FilesystemContext, error) {
	if activeRuntime == nil {
		return tools.FilesystemContext{}, nil
	}
	previous := activeRuntime.PreviousFilesystemContext
	var target tools.FilesystemContext
	var err error
	if plan.CrossProject() {
		targetBoundary, boundaryErr := s.metadata.ResolveProjectWorkspaceBoundary(ctx, plan.TargetProject.ID)
		if boundaryErr != nil {
			return tools.FilesystemContext{}, boundaryErr
		}
		attached, attachedErr := s.metadata.ProjectWorkspaceAttached(ctx, plan.TargetProject.ID, plan.TargetWorkspaceRoot)
		if attachedErr != nil {
			return tools.FilesystemContext{}, attachedErr
		}
		if !attached {
			targetBoundary, _, err = targetBoundary.WithWorkspace(metadata.ProjectWorkspace{CanonicalRoot: plan.TargetWorkspaceRoot})
			if err != nil {
				return tools.FilesystemContext{}, err
			}
		}
		target, err = runtimewire.NewFilesystemContext(plan.TargetExecutionRoot, plan.TargetExecutionRoot, targetBoundary)
	} else {
		managed := previous.ManagedWorktree
		if managed != nil {
			var currentRoot *string
			if plan.TargetWorktreeID != nil {
				currentRoot = &plan.TargetExecutionRoot
			}
			managed, err = managed.WithCurrentWorktreeRoot(currentRoot)
			if err != nil {
				return tools.FilesystemContext{}, err
			}
		}
		target, err = runtimewire.WithExecutionTarget(previous, plan.TargetExecutionRoot, plan.TargetExecutionRoot, managed)
	}
	if err != nil {
		return tools.FilesystemContext{}, err
	}
	if previous.ManagedWorktree != nil && plan.TargetWorktreeID == nil {
		target.ManagedWorktree, err = previous.ManagedWorktree.WithCurrentWorktreeRoot(nil)
	}
	return target, err
}

func (s *SessionWorkspaceRetargeter) persistRebindFailure(
	ctx context.Context,
	plan metadata.SessionWorkspaceRetargetPlan,
	workingDirectory string,
	cause error,
) error {
	sessionID, err := runtimeids.ParseSessionID(plan.SessionID)
	if err != nil {
		return err
	}
	descriptor, err := session.NewOpenSessionDescriptor(sessionID)
	if err != nil {
		return err
	}
	return s.authority.WithSessionStore(ctx, descriptor, func(_ context.Context, store *session.Store) error {
		return persistRebindFailure(store, plan, workingDirectory, cause)
	})
}

func persistRebindFailure(
	store *session.Store,
	plan metadata.SessionWorkspaceRetargetPlan,
	workingDirectory string,
	cause error,
) error {
	if store == nil {
		return errors.New("session store is required")
	}
	reminder := rebindFailureReminder(plan, workingDirectory, cause)
	return store.SetSessionRebindReminder(&reminder)
}

func rebindFailureReminder(
	plan metadata.SessionWorkspaceRetargetPlan,
	workingDirectory string,
	cause error,
) session.SessionRebindReminder {
	diagnostic := cause.Error()
	return session.SessionRebindReminder{
		Kind:              session.SessionRebindReminderFailed,
		SourceProject:     plan.SourceProject,
		TargetProject:     plan.TargetProject,
		WorkingDirectory:  &workingDirectory,
		FailureDiagnostic: &diagnostic,
	}
}

func (s *SessionWorkspaceRetargeter) ownedBackgroundProcessActive(sessionID string) (bool, error) {
	id := strings.TrimSpace(sessionID)
	for _, process := range s.processes.List() {
		if !process.Running {
			continue
		}
		ownerSessionID := strings.TrimSpace(process.OwnerSessionID)
		if ownerSessionID == "" {
			return false, fmt.Errorf("running background process %q has no owner session id", process.ID)
		}
		if ownerSessionID == id {
			return true, nil
		}
	}
	return false, nil
}

func validateSessionArtifactSource(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("stat session artifact source %q: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return fmt.Errorf("session artifact source %q is not a real directory", path)
	}
	return nil
}

func prepareSessionArtifactTarget(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("session artifact target %q already exists", path)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat session artifact target %q: %w", path, err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create session artifact target parent: %w", err)
	}
	return nil
}
