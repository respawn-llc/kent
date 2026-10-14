package protoapi

import (
	"errors"
	"fmt"

	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
	"core/shared/serverapi"

	"google.golang.org/protobuf/types/known/emptypb"
)

func SessionTargetErrorContext(err error) *sharedpb.SessionTargetContext {
	var failure *serverapi.SessionTargetError
	if !errors.As(err, &failure) {
		return nil
	}
	switch target := failure.Target.(type) {
	case serverapi.UnresolvedSessionTarget:
		return &sharedpb.SessionTargetContext{Stage: &sharedpb.SessionTargetContext_Unresolved{Unresolved: &emptypb.Empty{}}}
	case serverapi.CanonicalSessionTarget:
		return &sharedpb.SessionTargetContext{Stage: &sharedpb.SessionTargetContext_CanonicalSessionId{CanonicalSessionId: target.ID.String()}}
	case serverapi.ProjectedSessionTarget:
		return &sharedpb.SessionTargetContext{Stage: &sharedpb.SessionTargetContext_Projected{Projected: &sharedpb.SessionIdentity{
			SessionId: target.Identity.ID.String(), ShortId: SessionShortIDToProto(target.Identity.ShortID),
		}}}
	default:
		panic("Session failure requires target context")
	}
}

func SessionTargetContextErrorFromProto(value *sharedpb.SessionTargetContext, cause error) error {
	if err := Validate(value); err != nil {
		return err
	}
	switch stage := value.Stage.(type) {
	case *sharedpb.SessionTargetContext_Unresolved:
		return serverapi.WithSessionTarget(serverapi.UnresolvedSessionTarget{}, cause)
	case *sharedpb.SessionTargetContext_CanonicalSessionId:
		id := runtimeids.SessionIDFromProto(stage.CanonicalSessionId)
		return serverapi.WithSessionTarget(serverapi.CanonicalSessionTarget{ID: id}, cause)
	case *sharedpb.SessionTargetContext_Projected:
		return SessionTargetErrorFromProto(stage.Projected, cause)
	default:
		return errors.New("Session target context is required")
	}
}

func SessionTargetErrorIdentity(err error) (*sharedpb.SessionIdentity, error) {
	var target *serverapi.SessionTargetError
	if !errors.As(err, &target) {
		return nil, errors.New("Session operation failure omitted its resolved identity")
	}
	identity := target.Projection()
	if identity == nil {
		return nil, errors.New("Session operation failure omitted its display projection")
	}
	return &sharedpb.SessionIdentity{SessionId: identity.ID.String(), ShortId: SessionShortIDToProto(identity.ShortID)}, nil
}

func SessionTargetErrorFromProto(value *sharedpb.SessionIdentity, cause error) error {
	if err := Validate(value); err != nil {
		return err
	}
	identity, err := SessionIdentityFromProto(value.SessionId, value.ShortId)
	if err != nil {
		return err
	}
	return serverapi.WithSessionTarget(serverapi.ProjectedSessionTarget{Identity: identity}, cause)
}

func SessionIdentityFromProto(id string, short *int64) (runtimeids.SessionIdentity, error) {
	shortID, err := SessionShortIDFromProto(short)
	if err != nil {
		return runtimeids.SessionIdentity{}, err
	}
	return runtimeids.SessionIdentity{ID: runtimeids.SessionIDFromProto(id), ShortID: shortID}, nil
}

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
