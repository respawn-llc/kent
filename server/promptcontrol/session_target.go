package promptcontrol

import (
	"context"

	"core/server/metadata"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func promptTarget(ctx context.Context, store *metadata.Store, target *sharedpb.SessionSelector, caller *string) (runtimeids.SessionID, error) {
	id, err := store.ResolveSessionTarget(ctx, target)
	if err != nil {
		return runtimeids.SessionID{}, err
	}
	if caller != nil && *caller == id.String() {
		return runtimeids.SessionID{}, &serverapi.SessionSelfTargetError{SessionID: id}
	}
	return id, nil
}
