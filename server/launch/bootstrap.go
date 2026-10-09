package launch

import (
	"context"
	"errors"
	"strings"

	"core/server/metadata"
	"core/server/session"
	"core/server/subagentpolicy"
	"core/shared/textutil"
)

type BootstrapRequest struct {
	WorkspaceRoot         string
	WorkspaceRootExplicit bool
	SessionID             string
}

type BootstrapPlan struct {
	WorkspaceRoot     string
	MainWorkspaceRoot *string
}

func ResolveSessionCaller(ctx context.Context, resolver session.PersistedSessionResolver, sessionID string) (subagentpolicy.Caller, error) {
	if _, err := session.ResolvePersistedSessionRecord(ctx, resolver, sessionID); err != nil {
		return subagentpolicy.Caller{}, err
	}
	owner, ok := resolver.(interface {
		SessionHasWorkflowTask(context.Context, string) (bool, error)
	})
	if !ok {
		return subagentpolicy.Caller{}, errors.New("Session caller ownership reader is required")
	}
	workflow, err := owner.SessionHasWorkflowTask(ctx, sessionID)
	if err != nil {
		return subagentpolicy.Caller{}, err
	}
	return subagentpolicy.Caller{Workflow: workflow}, nil
}

func ResolveBootstrapPlan(persistenceRoot string, req BootstrapRequest) (BootstrapPlan, error) {
	plan := BootstrapPlan{
		WorkspaceRoot: strings.TrimSpace(req.WorkspaceRoot),
	}
	if strings.TrimSpace(req.SessionID) == "" {
		return plan, nil
	}
	if strings.TrimSpace(persistenceRoot) == "" {
		return BootstrapPlan{}, errors.New("launch planner persistence root is required")
	}
	store, err := openSessionByID(persistenceRoot, req.SessionID)
	if err != nil {
		return BootstrapPlan{}, err
	}
	meta := store.Meta()
	metadataStore, err := metadata.Open(persistenceRoot)
	if err != nil {
		return BootstrapPlan{}, err
	}
	target, targetErr := metadataStore.ResolveSessionExecutionTarget(context.Background(), meta.SessionID)
	if err := errors.Join(targetErr, metadataStore.Close()); err != nil {
		return BootstrapPlan{}, err
	}
	plan.MainWorkspaceRoot = textutil.Value(target.WorkspaceRoot)
	if !req.WorkspaceRootExplicit && strings.TrimSpace(meta.WorkspaceRoot) != "" {
		plan.WorkspaceRoot = strings.TrimSpace(meta.WorkspaceRoot)
	}
	return plan, nil
}

func openSessionByID(persistenceRoot string, sessionID string) (*session.Store, error) {
	metadataStore, err := metadata.Open(persistenceRoot)
	if err != nil {
		return nil, err
	}
	defer func() { _ = metadataStore.Close() }()
	store, err := session.OpenByID(persistenceRoot, sessionID, metadataStore.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		return nil, err
	}
	return store, nil
}
