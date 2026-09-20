package launch

import (
	"context"
	"errors"
	"path/filepath"

	"core/server/metadata"
	"core/server/session"
	"core/shared/clientui"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
)

type PreparedExecutionContext struct {
	ExecutionTarget          *worktreepb.SessionExecutionTarget
	ProjectWorkspaceBoundary metadata.ProjectWorkspaceBoundary
	ManagedWorktreeRoots     []string
}

type SessionPreparationRequest struct {
	Request                  SessionRequest
	SessionID                runtimeids.SessionID
	ExecutionTarget          *worktreepb.SessionExecutionTarget
	ProjectWorkspaceBoundary metadata.ProjectWorkspaceBoundary
	ManagedWorktreeRoots     []string
}

type PreparedSession struct {
	Plan     SessionPlan
	Creation session.CreationPlan
}

func (p Planner) PrepareSession(ctx context.Context, request SessionPreparationRequest) (PreparedSession, error) {
	if err := validateInitialChatSessionRequest(request.Request); err != nil {
		return PreparedSession{}, err
	}
	origin, ok := request.Request.Intent.CreateOrigin()
	if !ok {
		return PreparedSession{}, errors.New("Session preparation requires a create intent")
	}
	creation, err := p.prepareCreation(ctx, origin, request.Request.Mode, request.SessionID, request.Request.InitialChat)
	if err != nil {
		return PreparedSession{}, err
	}
	plan, err := p.PlanPreparedSession(ctx, request.Request, creation.Snapshot().Meta, PreparedExecutionContext{
		ExecutionTarget:          request.ExecutionTarget,
		ProjectWorkspaceBoundary: request.ProjectWorkspaceBoundary,
		ManagedWorktreeRoots:     request.ManagedWorktreeRoots,
	})
	if err != nil {
		return PreparedSession{}, err
	}
	continuation := session.ContinuationContext{}
	if plan.Continuation != nil {
		continuation = *plan.Continuation
	}
	creation, err = creation.WithLaunchMetadata(plan.SessionName, continuation)
	if err != nil {
		return PreparedSession{}, err
	}
	return PreparedSession{Plan: plan, Creation: creation}, nil
}

// PlanPreparedSession resolves the same settings as ordinary launch against
// private target facts. Neither retained metadata nor artifacts are mutated.
func (p Planner) PlanPreparedSession(ctx context.Context, request SessionRequest, meta session.Meta, executionContext PreparedExecutionContext) (SessionPlan, error) {
	if err := request.Intent.Validate(); err != nil {
		return SessionPlan{}, err
	}
	if id, present := request.Intent.SessionID(); present && id.String() != meta.SessionID {
		return SessionPlan{}, errors.New("Session preparation identity does not match")
	}
	if err := executionContext.ProjectWorkspaceBoundary.Validate(); err != nil {
		return SessionPlan{}, err
	}
	return p.planSessionWithExecutionContext(ctx, request, meta, nil, &executionContext)
}

func (p Planner) ApplyPreparedRunPromptOverridesFromMeta(plan SessionPlan, meta session.Meta, overrides serverapi.RunPromptOverrides, prepared PreparedRunPromptOverrides, options RunPromptOverrideOptions) (SessionPlan, []string, error) {
	next, warnings, err := p.applyPreparedRunPromptOverrides(plan, meta, nil, overrides, prepared, options)
	if err != nil {
		return SessionPlan{}, nil, err
	}
	next, err = finalizeRunPromptOverrides(next, overrides, options)
	return next, warnings, err
}

func (p Planner) resolveSessionPlanExecutionContext(ctx context.Context, sessionID string, prepared *PreparedExecutionContext) (PreparedExecutionContext, error) {
	if prepared != nil {
		copy := *prepared
		copy.ExecutionTarget = clientui.NormalizeSessionExecutionTarget(prepared.ExecutionTarget)
		return copy, nil
	}
	target, err := p.resolvePlannedExecutionTarget(ctx, sessionID)
	if err != nil {
		return PreparedExecutionContext{}, err
	}
	if p.ProjectWorkspaceBoundary == nil {
		return PreparedExecutionContext{}, errors.New("project workspace boundary resolver is required")
	}
	boundary, err := p.ProjectWorkspaceBoundary.ResolveSessionProjectWorkspaceBoundary(ctx, sessionID)
	if err != nil {
		return PreparedExecutionContext{}, err
	}
	roots, err := p.ProjectWorkspaceBoundary.ListManagedWorktreeRoots(ctx)
	return PreparedExecutionContext{ExecutionTarget: target, ProjectWorkspaceBoundary: boundary, ManagedWorktreeRoots: roots}, err
}

func (p Planner) prepareCreation(ctx context.Context, origin serverapi.SessionCreateOrigin, mode Mode, id runtimeids.SessionID, initialChat *session.ChatDraftState) (session.CreationPlan, error) {
	if err := ctx.Err(); err != nil {
		return session.CreationPlan{}, err
	}
	category := sessioncontract.SessionCategoryMain
	if mode == ModeHeadless {
		category = sessioncontract.SessionCategorySubagent
	}
	descriptor, err := session.NewCreateSessionDescriptor(id, p.ContainerDir, filepath.Base(p.ContainerDir), p.Config.WorkspaceRoot, category)
	if err != nil {
		return session.CreationPlan{}, err
	}
	request := session.CreationRequest{Descriptor: descriptor, InitialChat: initialChat}
	if origin.Kind() != serverapi.SessionCreateOriginIndependent {
		sourceID, present := origin.SessionID()
		if !present {
			return session.CreationPlan{}, errors.New("Session creation source is required")
		}
		record, err := session.ResolvePersistedSessionRecord(ctx, p.PersistedSessions, sourceID.String())
		if err != nil {
			return session.CreationPlan{}, err
		}
		request.Source = session.CreationContextSourceFromMeta(*record.Meta)
		switch origin.Kind() {
		case serverapi.SessionCreateOriginParentAgent:
			if err := (parentAgentDepthPolicy{sessions: p.PersistedSessions}).enforce(ctx, *record.Meta, p.Config.Settings.MaxSubagentDepth, p.Config.Settings.Debug); err != nil {
				return session.CreationPlan{}, err
			}
			request.SourceKind = session.SessionCreationSourceParentAgent
		case serverapi.SessionCreateOriginPreviousSession:
			request.SourceKind = session.SessionCreationSourcePreviousSession
			request.ContextOptions = session.ChildContextOptions{LockedContract: session.InheritFullContract, InheritContinuation: true}
		default:
			return session.CreationPlan{}, errors.New("Session creation origin is invalid")
		}
	}
	return session.PrepareCreation(request, p.StoreOptions...)
}
