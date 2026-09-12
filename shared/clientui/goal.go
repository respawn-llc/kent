package clientui

import runtimepb "core/shared/protoapi/gen/kent/api/runtime"

// GoalMutationResult also represents outcomes produced by local UI controls.
type GoalMutationResult struct {
	Kind         runtimepb.GoalMutationResultKind
	Goal         *runtimepb.Goal
	Availability *runtimepb.GoalAvailability
}
