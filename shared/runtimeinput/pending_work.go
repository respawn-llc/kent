package runtimeinput

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"core/shared/runtimeids"
)

const PendingWorkCapacity = 100

var ErrPendingWorkCapacity = errors.New("Pending Work capacity reached")
var ErrPendingWorkNotPending = errors.New("Pending Work item is not pending")

type PendingWorkRemovalError struct {
	ItemID runtimeids.QueueItemID
}

func (e *PendingWorkRemovalError) Error() string {
	if e == nil || e.ItemID.IsZero() {
		return ErrPendingWorkNotPending.Error()
	}
	return "Pending Work item " + e.ItemID.String() + " is not pending"
}

func (*PendingWorkRemovalError) Unwrap() error { return ErrPendingWorkNotPending }

type PendingWorkLane string
type PendingWorkItemKind string
type PendingWorkItemState string
type PendingWorkWorktreeTransitionKind string

const (
	PendingWorkLaneSteer PendingWorkLane = "steer"
	PendingWorkLaneQueue PendingWorkLane = "queue"

	PendingWorkItemKindMessage            PendingWorkItemKind = "message"
	PendingWorkItemKindShell              PendingWorkItemKind = "shell"
	PendingWorkItemKindManualCompaction   PendingWorkItemKind = "manual_compaction"
	PendingWorkItemKindWorktreeTransition PendingWorkItemKind = "worktree_transition"

	PendingWorkItemStatePending PendingWorkItemState = "pending"

	PendingWorkWorktreeTransitionEnter PendingWorkWorktreeTransitionKind = "enter"
	PendingWorkWorktreeTransitionLeave PendingWorkWorktreeTransitionKind = "leave"
)

type PendingWorkMessage struct {
	Text string
}

// ShellCommand is executable human input, never a model prompt. Its admission
// lane determines execution timing; acceptance does not report process completion.
type ShellCommand struct {
	Command string
}

func (s ShellCommand) CanonicalInput() string { return "$" + s.Command }

type ManualCompactionAdmission struct {
	Guidance *string
}

type PendingWorkManualCompaction = ManualCompactionAdmission

type PendingWorkWorktreeTransition struct {
	Transition PendingWorkWorktreeTransitionKind
	Selector   *string
}

type PendingWorkItem struct {
	ID                 runtimeids.QueueItemID
	Lane               PendingWorkLane
	Kind               PendingWorkItemKind
	State              PendingWorkItemState
	CanonicalInput     string
	Message            *PendingWorkMessage
	Shell              *ShellCommand
	ManualCompaction   *PendingWorkManualCompaction
	WorktreeTransition *PendingWorkWorktreeTransition
}

const ManualCompactionToken = "/compact"

func (c ManualCompactionAdmission) CanonicalInput() string {
	if c.Guidance == nil {
		return ManualCompactionToken
	}
	return ManualCompactionToken + " " + *c.Guidance
}

func (t PendingWorkWorktreeTransition) CanonicalInput() (string, error) {
	switch t.Transition {
	case PendingWorkWorktreeTransitionEnter:
		return "/wt switch " + *t.Selector, nil
	case PendingWorkWorktreeTransitionLeave:
		return "/wt leave", nil
	default:
		return "", fmt.Errorf("unknown Pending Work Worktree transition %q", t.Transition)
	}
}

type PendingWork struct {
	Items []PendingWorkItem
}

type PendingWorkRestoration struct {
	Kind           PendingWorkItemKind
	CanonicalInput string
}

type PendingWorkTechnicalRestoration struct {
	ItemID         runtimeids.QueueItemID
	Kind           PendingWorkItemKind
	CanonicalInput string
}

func (i PendingWorkItem) Restoration() PendingWorkRestoration {
	return PendingWorkRestoration{
		Kind:           i.Kind,
		CanonicalInput: i.CanonicalInput,
	}
}

func (i PendingWorkItem) TechnicalRestoration() PendingWorkTechnicalRestoration {
	restoration := i.Restoration()
	return PendingWorkTechnicalRestoration{
		ItemID:         i.ID,
		Kind:           restoration.Kind,
		CanonicalInput: restoration.CanonicalInput,
	}
}

func NormalizePendingWorkArgument(value string) string {
	return strings.Join(strings.FieldsFunc(value, func(character rune) bool {
		return unicode.IsSpace(character) || character == '\uFEFF'
	}), " ")
}
