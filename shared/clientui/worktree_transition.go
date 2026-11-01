package clientui

import (
	"errors"
	"strings"

	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"github.com/google/uuid"
)

type WorktreeTransitionID uuid.UUID

func NewWorktreeTransitionID() WorktreeTransitionID {
	return WorktreeTransitionID(uuid.New())
}

func WorktreeTransitionIDFromProto(value string) (WorktreeTransitionID, error) {
	parsed, err := uuid.Parse(value)
	return WorktreeTransitionID(parsed), err
}

func (id WorktreeTransitionID) String() string {
	value := uuid.UUID(id)
	if value == uuid.Nil {
		return ""
	}
	return value.String()
}

type WorktreeTransitionKind string

const (
	WorktreeTransitionEnter  WorktreeTransitionKind = "enter"
	WorktreeTransitionLeave  WorktreeTransitionKind = "leave"
	WorktreeTransitionDelete WorktreeTransitionKind = "delete"
)

type WorktreeTransitionState string

const (
	WorktreeTransitionCompleted WorktreeTransitionState = "completed"
	WorktreeTransitionFailed    WorktreeTransitionState = "failed"
)

type WorktreeTransitionFailure struct {
	Diagnostic         string
	SelectorError      *worktreepb.SelectorErrorDetails
	DeletePrecondition *WorktreeDirtyState
}

type WorktreeTransitionOutcome struct {
	OperationID WorktreeTransitionID
	Transition  WorktreeTransitionKind
	State       WorktreeTransitionState
	Failure     *WorktreeTransitionFailure
}

func (outcome WorktreeTransitionOutcome) ValidateSourceDiagnostic() error {
	// Selector failures omit the native diagnostic from their wire projection.
	if outcome.Failure != nil && outcome.Failure.SelectorError != nil &&
		strings.TrimSpace(outcome.Failure.Diagnostic) == "" {
		return errors.New("worktree transition failure diagnostic is required")
	}
	return nil
}
