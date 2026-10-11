package protoapi

import (
	"fmt"

	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
)

func SessionShortIDToProto(value *runtimeids.SessionShortID) *int64 {
	if value == nil {
		return nil
	}
	number := value.Int64()
	return &number
}

func SessionShortIDFromProto(value *int64) (*runtimeids.SessionShortID, error) {
	if value == nil {
		return nil, nil
	}
	number, err := runtimeids.NewSessionShortID(*value)
	if err != nil {
		return nil, err
	}
	return &number, nil
}

func SessionSelectorFromProto(value *sharedpb.SessionSelector) (runtimeids.SessionSelector, error) {
	if err := Validate(value); err != nil {
		return nil, err
	}
	switch selected := value.Value.(type) {
	case *sharedpb.SessionSelector_SessionId:
		return runtimeids.ParseSessionID(selected.SessionId)
	case *sharedpb.SessionSelector_ShortId:
		return runtimeids.NewSessionShortID(selected.ShortId)
	default:
		return nil, fmt.Errorf("Session selector is required")
	}
}

func SessionSelectorToProto(value runtimeids.SessionSelector) *sharedpb.SessionSelector {
	switch selected := value.(type) {
	case runtimeids.SessionID:
		return &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_SessionId{SessionId: selected.String()}}
	case runtimeids.SessionShortID:
		return &sharedpb.SessionSelector{Value: &sharedpb.SessionSelector_ShortId{ShortId: selected.Int64()}}
	default:
		panic("Session selector is required")
	}
}
