package core

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"core/server/metadata"
	"core/server/promptcommands"
	"core/server/runtimecontrol"
	"core/shared/apicontract"
	"core/shared/clientui"
	"core/shared/config"
	"core/shared/protoapi"
	promptcommandpb "core/shared/protoapi/gen/kent/api/prompt_command"
	"core/shared/serverapi"
)

type promptCommandCatalogService struct {
	catalog promptcommands.Service
}

func (s promptCommandCatalogService) GetPromptCommandCatalog(context.Context, *promptcommandpb.GetCatalogRequest) (*promptcommandpb.Catalog, error) {
	entries, err := s.catalog.Catalog()
	if err != nil {
		return nil, publicPromptCommandError(err)
	}
	response := &promptcommandpb.Catalog{Commands: make([]*promptcommandpb.CatalogEntry, 0, len(entries))}
	for _, entry := range entries {
		response.Commands = append(response.Commands, &promptcommandpb.CatalogEntry{Name: entry.Name, Preview: entry.Preview})
	}
	return response, protoapi.Validate(response)
}

type promptCommandRuntimeResolver struct {
	effectiveWorkspace promptCommandEffectiveWorkspaceResolver
	metadataStore      *metadata.Store
}

type promptCommandSessionCatalogService struct {
	core      *Core
	sessionID string
}

func (s promptCommandSessionCatalogService) GetPromptCommandCatalog(ctx context.Context, req *promptcommandpb.GetCatalogRequest) (*promptcommandpb.Catalog, error) {
	if s.core == nil {
		return nil, errors.New("core is required")
	}
	projectID, workspaceRoot, err := resolvePromptCommandSessionWorkspace(ctx, s.core.MetadataStore(), s.sessionID)
	if err != nil {
		return nil, err
	}
	catalog, err := s.core.PromptCommandCatalogClientForProjectWorkspace(ctx, projectID, workspaceRoot)
	if err != nil {
		return nil, err
	}
	return catalog.GetPromptCommandCatalog(ctx, req)
}

type promptCommandEffectiveWorkspaceResolver struct {
	persistenceRoot string
}

func (r promptCommandEffectiveWorkspaceResolver) ResolvePromptCommandForWorkspace(ctx context.Context, workspaceRoot, name, arguments string) (promptcommands.ResolvedCommand, error) {
	content, err := promptcommands.New(r.persistenceRoot, workspaceRoot).Resolve(name, arguments)
	if err != nil {
		return promptcommands.ResolvedCommand{}, publicPromptCommandError(err)
	}
	return content, nil
}

func (r promptCommandRuntimeResolver) ResolvePromptCommand(ctx context.Context, sessionID, name, arguments string) (promptcommands.ResolvedCommand, error) {
	_, workspaceRoot, err := resolvePromptCommandSessionWorkspace(ctx, r.metadataStore, sessionID)
	if err != nil {
		return promptcommands.ResolvedCommand{}, err
	}
	return r.effectiveWorkspace.ResolvePromptCommandForWorkspace(ctx, workspaceRoot, name, arguments)
}

func resolvePromptCommandSessionWorkspace(ctx context.Context, metadataStore *metadata.Store, sessionID string) (string, string, error) {
	if metadataStore == nil {
		return "", "", errors.New("metadata store is required")
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return "", "", errors.New("session id is required")
	}
	target, err := metadataStore.ResolveSessionExecutionTarget(ctx, trimmedSessionID)
	if err != nil {
		return "", "", err
	}
	binding, err := metadataStore.LookupWorkspaceBindingByID(ctx, target.GetWorkspaceId())
	if err != nil {
		return "", "", err
	}
	workspaceRoot, err := clientui.SessionExecutionWorkspaceRoot(target, binding.CanonicalRoot)
	if err != nil {
		return "", "", err
	}
	return binding.ProjectID, workspaceRoot, nil
}

var _ runtimecontrol.PromptCommandResolver = promptCommandRuntimeResolver{}

func publicPromptCommandError(err error) error {
	var source *promptcommands.Error
	if !errors.As(err, &source) {
		return err
	}
	kind := serverapi.PromptCommandErrorKindCatalogRead
	switch source.Kind {
	case promptcommands.ErrorKindCommandNotFound:
		kind = serverapi.PromptCommandErrorKindCommandNotFound
	case promptcommands.ErrorKindCommandRead:
		kind = serverapi.PromptCommandErrorKindCommandRead
	}
	return &promptCommandPublicError{
		err:   &serverapi.PromptCommandError{Kind: kind, Command: source.Command},
		cause: source,
	}
}

type promptCommandPublicError struct {
	err   *serverapi.PromptCommandError
	cause error
}

func (e *promptCommandPublicError) Error() string {
	return e.err.Error()
}

func (e *promptCommandPublicError) As(target any) bool {
	if typed, ok := target.(**serverapi.PromptCommandError); ok {
		*typed = e.err
		return true
	}
	return false
}

func (e *promptCommandPublicError) diagnosticCause() error {
	return e.cause
}

func (s *Core) PromptCommandCatalogClientForProjectWorkspace(ctx context.Context, projectID, workspaceRoot string) (apicontract.PromptCommandCatalogService, error) {
	projectCtx, err := s.resolvePromptCommandCatalogContext(ctx, projectID, workspaceRoot)
	if err != nil {
		return nil, err
	}
	return promptCommandCatalogService{
		catalog: promptcommands.New(projectCtx.config.PersistenceRoot, projectCtx.projectRoot),
	}, nil
}

func (s *Core) PromptCommandCatalogClientForSession(ctx context.Context, sessionID string) (apicontract.PromptCommandCatalogService, error) {
	if s == nil {
		return nil, errors.New("core is required")
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("session id is required")
	}
	return promptCommandSessionCatalogService{core: s, sessionID: strings.TrimSpace(sessionID)}, nil
}

func (s *Core) resolvePromptCommandCatalogContext(ctx context.Context, projectID, workspaceRoot string) (projectContext, error) {
	if s == nil || s.safeBundles().Persistence.metadataStore == nil {
		return projectContext{}, errors.New("metadata store is required")
	}
	canonicalRoot, err := config.CanonicalWorkspaceRoot(workspaceRoot)
	if err != nil {
		return projectContext{}, err
	}
	worktree, err := s.safeBundles().Persistence.metadataStore.GetWorktreeRecordByCanonicalRoot(ctx, canonicalRoot)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			return projectContext{}, err
		}
		return s.resolveProjectContext(ctx, projectID, "", canonicalRoot)
	}
	binding, err := s.safeBundles().Persistence.metadataStore.LookupWorkspaceBindingByID(ctx, worktree.WorkspaceID)
	if err != nil {
		return projectContext{}, err
	}
	if strings.TrimSpace(binding.ProjectID) != strings.TrimSpace(projectID) {
		return projectContext{}, fmt.Errorf("worktree %q is not bound to project %q", canonicalRoot, projectID)
	}
	projectCtx, err := s.resolveProjectContext(ctx, projectID, binding.WorkspaceID, "")
	if err != nil {
		return projectContext{}, err
	}
	projectCtx.projectRoot = canonicalRoot
	return projectCtx, nil
}
