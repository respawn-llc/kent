package tools

import (
	"context"
	"errors"
	"sync"
)

type WorkspaceRootLookup func(context.Context, string, string) (*FilesystemRoot, error)

// WorkspacePermissions belongs to one Runtime, not to a filesystem context or
// a tool handler. Only successful grants survive handler replacement.
type WorkspacePermissions struct {
	lookup WorkspaceRootLookup
	mu     sync.Mutex
	roots  map[string]FilesystemRoot
}

func NewWorkspacePermissions(lookup WorkspaceRootLookup) *WorkspacePermissions {
	return &WorkspacePermissions{lookup: lookup, roots: make(map[string]FilesystemRoot)}
}

func (p *WorkspacePermissions) Retain(root FilesystemRoot) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.roots[root.RealPath] = root
}

func (p *WorkspacePermissions) Contains(path string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, root := range p.roots {
		inside, err := FilesystemRootContains(root, path)
		if err != nil || inside {
			return inside, err
		}
	}
	return false, nil
}

func (p *WorkspacePermissions) Learn(ctx context.Context, projectID, path string) (bool, error) {
	root, err := p.lookup(ctx, projectID, path)
	if err != nil || root == nil {
		return false, err
	}
	inside, err := FilesystemRootContains(*root, path)
	if err != nil {
		return false, err
	}
	if !inside {
		return false, errors.New("attached Workspace does not contain the resolved target")
	}
	p.Retain(*root)
	return true, nil
}
