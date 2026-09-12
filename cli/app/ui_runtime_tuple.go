package app

import (
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"

	"google.golang.org/protobuf/proto"
)

type runtimeTupleIngress uint8

const (
	runtimeTupleIngressIncremental runtimeTupleIngress = iota + 1
	runtimeTupleIngressAuthoritativeSnapshot
	runtimeTupleIngressHydration
)

type runtimeTupleDecision uint8

const (
	runtimeTupleIgnore runtimeTupleDecision = iota
	runtimeTupleApply
	runtimeTupleRefresh
)

type runtimeTupleCandidate struct {
	Version  *runtimepb.ReadModelVersion
	Activity *runtimepb.Activity
}

type runtimeTupleMergeResult struct {
	decision runtimeTupleDecision
	view     *runtimepb.MainView
	project  bool
}

func decideRuntimeTuple(
	current *runtimepb.ReadModelVersion,
	incoming *runtimepb.ReadModelVersion,
	ingress runtimeTupleIngress,
) runtimeTupleDecision {
	if incoming == nil || protoapi.Validate(incoming) != nil {
		return runtimeTupleIgnore
	}
	if current == nil || protoapi.Validate(current) != nil {
		return runtimeTupleApply
	}
	if incoming.Epoch != current.Epoch {
		if ingress == runtimeTupleIngressIncremental {
			return runtimeTupleRefresh
		}
		return runtimeTupleApply
	}
	if incoming.Generation != current.Generation {
		if incoming.Generation < current.Generation {
			return runtimeTupleIgnore
		}
		if ingress == runtimeTupleIngressIncremental {
			return runtimeTupleRefresh
		}
		return runtimeTupleApply
	}
	if incoming.Sequence <= current.Sequence {
		return runtimeTupleIgnore
	}
	return runtimeTupleApply
}

func runtimeTupleFromMainView(view *runtimepb.MainView) runtimeTupleCandidate {
	return runtimeTupleCandidate{
		Version:  view.Version,
		Activity: view.Activity,
	}
}

func runtimeTupleFromReadModelUpdate(update *runtimepb.ReadModelUpdate) runtimeTupleCandidate {
	return runtimeTupleCandidate{
		Version:  update.Version,
		Activity: update.Activity,
	}
}

func applyRuntimeTuple(view *runtimepb.MainView, candidate runtimeTupleCandidate) {
	view.Version = proto.Clone(candidate.Version).(*runtimepb.ReadModelVersion)
	view.Activity = proto.Clone(candidate.Activity).(*runtimepb.Activity)
}

func runtimeTupleMatchesView(candidate runtimeTupleCandidate, view *runtimepb.MainView) bool {
	return protoapi.ReadModelVersionsEqual(candidate.Version, view.Version) && runtimeActivitiesEqual(candidate.Activity, view.Activity)
}

func runtimeActivitiesEqual(left, right *runtimepb.Activity) bool {
	return proto.Equal(left, right)
}

type hydrationRuntimeTupleConflictError struct {
	current  *runtimepb.MainView
	incoming runtimeTupleCandidate
}

func (e hydrationRuntimeTupleConflictError) Error() string {
	return "stale or conflicting transcript hydration runtime tuple"
}

func (e hydrationRuntimeTupleConflictError) facts() map[string]any {
	return map[string]any{
		"current_version":   e.current.Version,
		"incoming_version":  e.incoming.Version,
		"current_activity":  e.current.Activity,
		"incoming_activity": e.incoming.Activity,
	}
}

func hydrationRuntimeTupleError(current *runtimepb.MainView, incoming runtimeTupleCandidate) error {
	return hydrationRuntimeTupleConflictError{current: current, incoming: incoming}
}

func runtimeReadModelResetMainViewRefreshRequest() runtimeMainViewRefreshRequest {
	return runtimeMainViewRefreshRequest{
		cause:    runtimeMainViewRefreshCauseManual,
		class:    runtimeSyncPolicyClassAllowed,
		priority: 100,
	}
}
