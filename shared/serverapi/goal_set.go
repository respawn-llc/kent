package serverapi

import runtimepb "core/shared/protoapi/gen/kent/api/runtime"

// ResolvedGoalSetCommit is the server-owned result of applying Goal Set to an
// already resolved Session. A non-nil Mutation proves the Goal metadata was
// committed; Diagnostic reports a later issue without changing that outcome.
type ResolvedGoalSetCommit struct {
	Mutation   *runtimepb.GoalMutationSuccess
	Diagnostic error
}
