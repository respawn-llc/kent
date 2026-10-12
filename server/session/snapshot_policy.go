package session

import "core/shared/sessioncontract"

type SnapshotBoundary uint8

const (
	SnapshotBoundaryCompaction SnapshotBoundary = iota + 1
	SnapshotBoundaryWorkflowContextReset
)

type snapshotRefreshPolicy uint8

const (
	refreshAtGenerationBoundary snapshotRefreshPolicy = iota + 1
	refreshAtWorkflowContextReset
)

func (policy snapshotRefreshPolicy) eligible(boundary SnapshotBoundary) bool {
	switch policy {
	case refreshAtGenerationBoundary:
		return true
	case refreshAtWorkflowContextReset:
		return boundary == SnapshotBoundaryWorkflowContextReset
	default:
		panic("invalid snapshot refresh policy")
	}
}

// ProjectSnapshotBoundary applies eligible refresh policies at an approved
// context boundary without mutating the outgoing metadata.
func ProjectSnapshotBoundary(meta Meta, boundary SnapshotBoundary) Meta {
	switch boundary {
	case SnapshotBoundaryCompaction, SnapshotBoundaryWorkflowContextReset:
	default:
		panic("invalid snapshot boundary")
	}
	meta = cloneMeta(meta)
	if refreshAtGenerationBoundary.eligible(boundary) {
		meta.UsageState = nil
		meta.OriginalThinkingEffort = nil
		meta.Locked = nil
	}
	if refreshAtWorkflowContextReset.eligible(boundary) {
		meta.WorkflowSnapshot = nil
	}
	return meta
}

func ValidateWorkflowSnapshot(snapshot *WorkflowSnapshot) error {
	if snapshot == nil {
		return nil
	}
	_, err := sessioncontract.ParseWorkflowCompletionMode(string(snapshot.CompletionMode))
	return err
}

func (s *Store) SetWorkflowSnapshot(snapshot WorkflowSnapshot) error {
	if err := ValidateWorkflowSnapshot(&snapshot); err != nil {
		return err
	}
	return s.mutateAndPersist(func() error {
		s.meta.WorkflowSnapshot = cloneWorkflowSnapshot(&snapshot)
		return nil
	})
}
