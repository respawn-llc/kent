package clientui

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

type GoalAvailability string

const (
	GoalAvailabilityAvailable              GoalAvailability = "available"
	GoalAvailabilityAgentCapabilityMissing GoalAvailability = "agent_capability_missing"
)

func (a GoalAvailability) Validate() error {
	switch a {
	case GoalAvailabilityAvailable, GoalAvailabilityAgentCapabilityMissing:
		return nil
	default:
		return fmt.Errorf("unknown goal availability %q", a)
	}
}

type Goal struct {
	ID        string            `json:"id"`
	Objective string            `json:"objective"`
	Status    RuntimeGoalStatus `json:"status"`
	CreatedAt time.Time         `json:"created_at"`
	UpdatedAt time.Time         `json:"updated_at"`
}

func (g Goal) Validate() error {
	if strings.TrimSpace(g.ID) == "" || strings.TrimSpace(g.Objective) == "" || !validGoalStatus(g.Status) || g.CreatedAt.IsZero() || g.UpdatedAt.IsZero() {
		return fmt.Errorf("invalid goal fields")
	}
	return nil
}

type GoalEnvelope struct {
	Goal         *Goal            `json:"goal,omitempty"`
	Availability GoalAvailability `json:"availability"`
}

type GoalProjection struct {
	Goal         *Goal             `json:"goal"`
	Availability *GoalAvailability `json:"availability"`
}

type GoalObservationKind string

const (
	GoalObservationHydration GoalObservationKind = "hydration"
	GoalObservationUpdate    GoalObservationKind = "update"
)

type GoalObservation struct {
	Sequence uint64              `json:"sequence"`
	Kind     GoalObservationKind `json:"kind"`
	Status   GoalProjection      `json:"status"`
}

func (o GoalObservation) Validate() error {
	if o.Sequence == 0 {
		return errors.New("Goal observation sequence must be positive")
	}
	switch o.Kind {
	case GoalObservationHydration:
		if o.Sequence != 1 {
			return errors.New("Goal observation hydration must be sequence 1")
		}
	case GoalObservationUpdate:
		if o.Sequence < 2 {
			return errors.New("Goal observation update must follow hydration")
		}
	default:
		return errors.New("Goal observation kind is invalid")
	}
	return o.Status.Validate()
}

func (p GoalProjection) Validate() error {
	if p.Goal != nil {
		if err := p.Goal.Validate(); err != nil {
			return err
		}
	}
	if p.Availability != nil {
		return p.Availability.Validate()
	}
	return nil
}

type GoalPreview struct {
	Objective string            `json:"objective"`
	Status    RuntimeGoalStatus `json:"status"`
}

type GoalMutationResultKind string

const (
	GoalMutationResultAuthoritativeGoal  GoalMutationResultKind = "authoritative_goal"
	GoalMutationResultAuthoritativeClear GoalMutationResultKind = "authoritative_clear"
	GoalMutationResultPendingPreview     GoalMutationResultKind = "pending_preview"
	GoalMutationResultAcceptanceOnly     GoalMutationResultKind = "acceptance_only"
)

type GoalMutationResult struct {
	Kind         GoalMutationResultKind `json:"kind"`
	Goal         *Goal                  `json:"goal,omitempty"`
	Pending      *GoalPreview           `json:"pending,omitempty"`
	Availability *GoalAvailability      `json:"availability"`
}

func (g GoalEnvelope) Validate() error {
	if err := g.Availability.Validate(); err != nil {
		return err
	}
	if g.Goal == nil {
		return nil
	}
	return g.Goal.Validate()
}

func (r GoalMutationResult) Validate() error {
	switch r.Kind {
	case GoalMutationResultAuthoritativeGoal:
		if r.Goal == nil || r.Pending != nil {
			return fmt.Errorf("authoritative Goal result requires only Goal")
		}
		if err := r.Goal.Validate(); err != nil {
			return err
		}
	case GoalMutationResultAuthoritativeClear, GoalMutationResultAcceptanceOnly:
		if r.Goal != nil || r.Pending != nil {
			return fmt.Errorf("%s result cannot contain Goal or pending preview", r.Kind)
		}
	case GoalMutationResultPendingPreview:
		if r.Goal != nil || r.Pending == nil {
			return fmt.Errorf("pending Goal result requires only pending preview")
		}
		if strings.TrimSpace(r.Pending.Objective) == "" || !validGoalStatus(r.Pending.Status) {
			return fmt.Errorf("invalid goal preview fields")
		}
	default:
		return fmt.Errorf("unknown Goal mutation result kind %q", r.Kind)
	}
	if r.Availability != nil {
		return r.Availability.Validate()
	}
	return nil
}

func validGoalStatus(status RuntimeGoalStatus) bool {
	return status == RuntimeGoalStatusActive || status == RuntimeGoalStatusPaused || status == RuntimeGoalStatusComplete
}
