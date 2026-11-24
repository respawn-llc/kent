package taskimport

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"core/server/metadata"
	worktree "core/server/worktreeservice"
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	"core/shared/serverapi"
)

func resolveRepository(ctx context.Context, store *metadata.Store, git *worktree.GitInspector, request *taskimportpb.Request, projectID string) (repository, error) {
	if request.Repository != nil {
		return parseRepository(*request.Repository)
	}
	candidates, err := git.GitHubRepositories(ctx, request.Cwd)
	if err != nil {
		return repository{}, err
	}
	if len(candidates) == 0 {
		// Membership stays Project-specific even when a Workspace has several links.
		if _, err := store.FindContainingProjectWorkspace(ctx, projectID, request.Cwd); err != nil {
			return repository{}, err
		}
		project, err := store.GetProjectHomeSummary(ctx, projectID)
		if err != nil {
			return repository{}, err
		}
		candidates, err = git.GitHubRepositories(ctx, project.PrimaryWorkspace.RootPath)
		if err != nil {
			return repository{}, err
		}
	}
	if len(candidates) != 1 {
		code := taskimportpb.FailureCode_FAILURE_CODE_AMBIGUOUS
		if len(candidates) == 0 {
			code = taskimportpb.FailureCode_FAILURE_CODE_UNAVAILABLE
		}
		return repository{}, &serverapi.TaskImportError{Detail: &taskimportpb.Failure{
			Code: code, Phase: taskimportpb.FailurePhase_FAILURE_PHASE_RESOLUTION,
			Diagnostic: fmt.Sprintf("found %d GitHub repositories; supply --repo owner/repo or an issue URL", len(candidates)),
		}}
	}
	return parseRepository(candidates[0].Owner + "/" + candidates[0].Name)
}

func bindingAtAncestor(ctx context.Context, store *metadata.Store, path string) (*metadata.Binding, error) {
	for {
		_, binding, err := store.ResolveWorkspacePath(ctx, path)
		if err != nil || binding != nil {
			return binding, err
		}
		parent := filepath.Dir(path)
		if parent == path {
			return nil, serverapi.ErrWorkspaceNotRegistered
		}
		path = parent
	}
}

func selectorPath(cwd, selector string) (string, bool) {
	_, statErr := os.Stat(filepath.Join(cwd, selector))
	if filepath.IsAbs(selector) || selector == "." || selector == ".." || filepath.Dir(selector) != "." || statErr == nil {
		if !filepath.IsAbs(selector) {
			selector = filepath.Join(cwd, selector)
		}
		return filepath.Clean(selector), true
	}
	return selector, false
}

func resolveDestination(ctx context.Context, store *metadata.Store, request *taskimportpb.Request) (string, error) {
	if !filepath.IsAbs(request.Cwd) {
		return "", invalidReference("import cwd must be absolute")
	}
	if request.Project != nil {
		value, isPath := selectorPath(request.Cwd, *request.Project)
		if !isPath {
			project, err := store.GetProjectHomeSummary(ctx, value)
			return project.ProjectID, err
		}
		binding, err := bindingAtAncestor(ctx, store, value)
		if err != nil {
			return "", err
		}
		return binding.ProjectID, nil
	}
	binding, err := bindingAtAncestor(ctx, store, request.Cwd)
	if err != nil {
		return "", err
	}
	return binding.ProjectID, nil
}

func resolveSourceWorkspace(ctx context.Context, store *metadata.Store, request *taskimportpb.Request, projectID string) (*string, error) {
	if request.SourceWorkspace == nil {
		return nil, nil
	}
	selector := serverapi.NewProjectWorkspaceSelectorForID(*request.SourceWorkspace)
	if path, isPath := selectorPath(request.Cwd, *request.SourceWorkspace); isPath {
		resolution, err := store.ResolveProjectWorkspacePath(ctx, projectID, path)
		if err != nil {
			return nil, err
		}
		if resolution.Kind == metadata.ProjectWorkspacePathUnregistered {
			return nil, serverapi.ErrWorkspaceNotRegistered
		}
		id := resolution.Binding.WorkspaceID
		return &id, nil
	}
	binding, err := store.ResolveProjectWorkspaceSelector(ctx, projectID, selector)
	if err != nil {
		return nil, err
	}
	return &binding.WorkspaceID, nil
}
