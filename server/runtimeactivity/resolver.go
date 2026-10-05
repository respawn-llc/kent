package runtimeactivity

import (
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"fmt"
)

type RegistrySnapshot struct {
	Registered     bool
	QueueAccepting bool
	Draining       bool
	Closing        bool
	Starting       bool
}

type PendingContinuationSnapshot struct {
	Promoted bool
}

type ActiveStepSnapshot struct {
	RunID      string
	StepID     string
	ActiveKind runtimepb.ActivityActiveKind
}

type ResolverSnapshot struct {
	Registry            RegistrySnapshot
	Active              *ActiveStepSnapshot
	Reviewer            runtimepb.ReviewerActivity
	LiveRunActive       bool
	PromptWait          bool
	PendingContinuation PendingContinuationSnapshot
}

func ResolveRuntimeActivity(snapshot ResolverSnapshot) (*runtimepb.Activity, error) {
	return resolveRuntimeFeedActivity(snapshot)
}

func resolveRuntimeFeedActivity(snapshot ResolverSnapshot) (*runtimepb.Activity, error) {
	activity := &runtimepb.Activity{Reviewer: snapshot.Reviewer}
	if activity.Reviewer == runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_UNSPECIFIED {
		activity.Reviewer = runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE
	}
	if !snapshot.Registry.Registered {
		activity.State = runtimepb.ActivityState_RUNTIME_ACTIVITY_UNAVAILABLE
	} else if snapshot.Registry.Closing {
		activity.State = runtimepb.ActivityState_RUNTIME_ACTIVITY_CLOSING
	} else if snapshot.Registry.Draining {
		activity.State = runtimepb.ActivityState_RUNTIME_ACTIVITY_DRAINING
	} else if snapshot.Active != nil {
		activity.State = runtimepb.ActivityState_RUNTIME_ACTIVITY_RUNNING
		if snapshot.PromptWait {
			activity.State = runtimepb.ActivityState_RUNTIME_ACTIVITY_AWAITING_PROMPT
		}
		runID, err := runtimeids.ParseRunID(snapshot.Active.RunID)
		if err != nil {
			return &runtimepb.Activity{}, fmt.Errorf("parse runtime active run id: %w", err)
		}
		stepID, err := runtimeids.ParseStepID(snapshot.Active.StepID)
		if err != nil {
			return &runtimepb.Activity{}, fmt.Errorf("parse runtime active step id: %w", err)
		}
		activity.ActiveStep = &runtimepb.ActiveStep{
			RunId:      runID.String(),
			StepId:     stepID.String(),
			ActiveKind: snapshot.Active.ActiveKind,
		}
		activity.QueueAccepting = snapshot.Registry.QueueAccepting
	} else if snapshot.LiveRunActive {
		activity.State = runtimepb.ActivityState_RUNTIME_ACTIVITY_DRAINING
	} else if snapshot.Registry.Starting || snapshot.PendingContinuation.Promoted {
		activity.State = runtimepb.ActivityState_RUNTIME_ACTIVITY_STARTING
	} else {
		activity.State = runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE
		activity.QueueAccepting = snapshot.Registry.QueueAccepting
	}
	if err := protoapi.Validate(activity); err != nil {
		return &runtimepb.Activity{}, err
	}
	return activity, nil
}
