package patch

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"core/server/tools/fsguard"
)

type OutsideWorkspaceRequest = fsguard.Request
type OutsideWorkspaceDecision = fsguard.Decision

const (
	OutsideWorkspaceDecisionDeny         = fsguard.DecisionDeny
	OutsideWorkspaceDecisionAllowOnce    = fsguard.DecisionAllowOnce
	OutsideWorkspaceDecisionAllowSession = fsguard.DecisionAllowSession
)

type OutsideWorkspaceApproval = fsguard.Approval
type OutsideWorkspaceApprover = fsguard.Approver

type Option func(*Tool)

func WithAllowOutsideWorkspace(allow bool) Option {
	return func(t *Tool) {
		t.allowOutsideWorkspace = allow
	}
}

func WithOutsideWorkspaceApprover(approver OutsideWorkspaceApprover) Option {
	return func(t *Tool) {
		t.outsideWorkspaceApprover = approver
	}
}

const outsideWorkspaceRejectionInstruction = "If it's essential to the task, ask the user to make the edit manually at the end of the task."

func (t *Tool) resolvePath(ctx context.Context, path string, mustExist bool, approvedOutside map[string]bool) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", fmt.Errorf("empty path")
	}
	candidate := path
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(t.workspaceRoot, candidate)
	}
	candidate = filepath.Clean(candidate)

	real := candidate
	if mustExist {
		var err error
		real, err = filepath.EvalSymlinks(real)
		if err != nil {
			return "", fmt.Errorf("resolve path %q: %w", path, err)
		}
	} else {
		parent := filepath.Dir(candidate)
		if _, err := os.Stat(parent); err == nil {
			parentReal, evalErr := filepath.EvalSymlinks(parent)
			if evalErr != nil {
				return "", fmt.Errorf("resolve parent path for %q: %w", path, evalErr)
			}
			real = filepath.Join(parentReal, filepath.Base(candidate))
		} else if errors.Is(err, os.ErrNotExist) {
			anchor := parent
			for {
				if _, statErr := os.Stat(anchor); statErr == nil {
					break
				} else if !errors.Is(statErr, os.ErrNotExist) {
					return "", fmt.Errorf("stat path anchor for %q: %w", path, statErr)
				}
				next := filepath.Dir(anchor)
				if next == anchor {
					return "", fmt.Errorf("resolve parent path for %q: no existing ancestor", path)
				}
				anchor = next
			}
			anchorReal, evalErr := filepath.EvalSymlinks(anchor)
			if evalErr != nil {
				return "", fmt.Errorf("resolve existing ancestor for %q: %w", path, evalErr)
			}
			relTail, relErr := filepath.Rel(anchor, candidate)
			if relErr != nil {
				return "", fmt.Errorf("build target tail for %q: %w", path, relErr)
			}
			real = filepath.Clean(filepath.Join(anchorReal, relTail))
		} else {
			return "", fmt.Errorf("stat parent path for %q: %w", path, err)
		}
	}

	guard := NewOutsideWorkspaceGuard(
		t.workspaceRoot,
		t.workspaceRootReal,
		t.workspaceRootInfo,
		t.workspaceOnly,
		t.allowOutsideWorkspace,
		t.outsideWorkspaceApprover,
		func() bool {
			t.outsideWorkspaceSessionMu.RLock()
			defer t.outsideWorkspaceSessionMu.RUnlock()
			return t.outsideWorkspaceSessionAllow
		},
		func(allow bool) {
			t.outsideWorkspaceSessionMu.Lock()
			t.outsideWorkspaceSessionAllow = allow
			t.outsideWorkspaceSessionMu.Unlock()
		},
		outsideWorkspaceRejectionInstruction,
		OutsideWorkspaceErrorLabels{
			OutsidePath:          "patch target outside workspace",
			ApprovalFailed:       "outside-workspace edit approval failed",
			RejectedByUserPrefix: "patch target outside workspace rejected by user",
		},
		OutsideWorkspaceFailureFactory{},
		IsPathInTemporaryDir,
		nil,
	)
	return guard.Allow(ctx, path, real, approvedOutside)
}
