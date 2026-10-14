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
	PendingWorkItemKindManualCompaction   PendingWorkItemKind = "manual_compaction"
	PendingWorkItemKindWorktreeTransition PendingWorkItemKind = "worktree_transition"

	PendingWorkItemStatePending PendingWorkItemState = "pending"

	PendingWorkWorktreeTransitionEnter PendingWorkWorktreeTransitionKind = "enter"
	PendingWorkWorktreeTransitionLeave PendingWorkWorktreeTransitionKind = "leave"
)

type PendingWorkMessage struct {
	Text string `json:"text"`
}

type ManualCompactionAdmission struct {
	Guidance *string `json:"guidance,omitempty"`
}

type PendingWorkManualCompaction = ManualCompactionAdmission

type PendingWorkWorktreeTransition struct {
	Transition PendingWorkWorktreeTransitionKind `json:"transition"`
	Selector   *string                           `json:"selector,omitempty"`
}

type PendingWorkItem struct {
	ID                 runtimeids.QueueItemID         `json:"id"`
	Lane               PendingWorkLane                `json:"lane"`
	Kind               PendingWorkItemKind            `json:"kind"`
	State              PendingWorkItemState           `json:"state"`
	CanonicalInput     string                         `json:"canonical_input"`
	Message            *PendingWorkMessage            `json:"message,omitempty"`
	ManualCompaction   *PendingWorkManualCompaction   `json:"manual_compaction,omitempty"`
	WorktreeTransition *PendingWorkWorktreeTransition `json:"worktree_transition,omitempty"`
}

func (c ManualCompactionAdmission) Validate() error {
	if c.Guidance == nil {
		return nil
	}
	if *c.Guidance == "" {
		return errors.New("Pending Work compaction guidance must not be blank")
	}
	if NormalizePendingWorkArgument(*c.Guidance) != *c.Guidance {
		return errors.New("Pending Work compaction guidance must be normalized")
	}
	return nil
}

func (c ManualCompactionAdmission) CanonicalInput() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	if c.Guidance == nil {
		return "/compact", nil
	}
	return "/compact " + *c.Guidance, nil
}

func (t PendingWorkWorktreeTransition) Validate() error {
	switch t.Transition {
	case PendingWorkWorktreeTransitionEnter:
		if t.Selector == nil || *t.Selector == "" {
			return errors.New("Pending Work Worktree enter selector is required")
		}
		if NormalizePendingWorkArgument(*t.Selector) != *t.Selector {
			return errors.New("Pending Work Worktree enter selector must be normalized")
		}
	case PendingWorkWorktreeTransitionLeave:
		if t.Selector != nil {
			return errors.New("Pending Work Worktree leave selector is forbidden")
		}
	default:
		return fmt.Errorf("unknown Pending Work Worktree transition %q", t.Transition)
	}
	return nil
}

func (t PendingWorkWorktreeTransition) CanonicalInput() (string, error) {
	if err := t.Validate(); err != nil {
		return "", err
	}
	switch t.Transition {
	case PendingWorkWorktreeTransitionEnter:
		return "/wt switch " + *t.Selector, nil
	case PendingWorkWorktreeTransitionLeave:
		return "/wt leave", nil
	default:
		panic("validated Pending Work Worktree transition is exhaustive")
	}
}

type PendingWork struct {
	Items []PendingWorkItem `json:"items"`
}

type PendingWorkRestoration struct {
	Kind           PendingWorkItemKind `json:"kind"`
	CanonicalInput string              `json:"canonical_input"`
}

type PendingWorkTechnicalRestoration struct {
	ItemID         runtimeids.QueueItemID `json:"item_id"`
	Kind           PendingWorkItemKind    `json:"kind"`
	CanonicalInput string                 `json:"canonical_input"`
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
