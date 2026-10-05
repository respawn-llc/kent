package protoapi

import (
	"strings"

	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
)

func OptionalSessionIDToProto(id *runtimeids.SessionID) *string {
	if id == nil {
		return nil
	}
	value := id.String()
	return &value
}

func NewReadModelVersion(epoch string, generation, sequence uint64) (*runtimepb.ReadModelVersion, error) {
	version := &runtimepb.ReadModelVersion{Epoch: strings.TrimSpace(epoch), Generation: generation, Sequence: sequence}
	if err := Validate(version); err != nil {
		return nil, err
	}
	return version, nil
}

func ReadModelVersionNewerThan(version, other *runtimepb.ReadModelVersion) bool {
	return version != nil && other != nil &&
		version.Epoch == other.Epoch && version.Generation == other.Generation && version.Sequence > other.Sequence
}

func ReadModelVersionsEqual(version, other *runtimepb.ReadModelVersion) bool {
	if version == nil || other == nil {
		return version == other
	}
	return version.Epoch == other.Epoch && version.Generation == other.Generation && version.Sequence == other.Sequence
}

func RuntimeActivityActiveForControl(activity *runtimepb.Activity) bool {
	switch activity.GetState() {
	case runtimepb.ActivityState_RUNTIME_ACTIVITY_STARTING,
		runtimepb.ActivityState_RUNTIME_ACTIVITY_RUNNING,
		runtimepb.ActivityState_RUNTIME_ACTIVITY_AWAITING_PROMPT,
		runtimepb.ActivityState_RUNTIME_ACTIVITY_DRAINING,
		runtimepb.ActivityState_RUNTIME_ACTIVITY_CLOSING:
		return true
	default:
		return false
	}
}
