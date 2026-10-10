package protoapi

import (
	"errors"

	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/sessioncontract"
)

// SessionNameFromMutation returns the normalized optional fact for a typed action.
func SessionNameFromMutation(mutation *runtimepb.SessionNameMutation) (*string, error) {
	if err := Validate(mutation); err != nil {
		return nil, err
	}
	switch action := mutation.Action.(type) {
	case *runtimepb.SessionNameMutation_Set:
		return sessioncontract.NormalizeSessionName(&action.Set)
	case *runtimepb.SessionNameMutation_Clear:
		return nil, nil
	default:
		return nil, errors.New("Session name mutation action is required")
	}
}
