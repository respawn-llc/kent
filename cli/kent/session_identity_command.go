package main

import (
	"context"
	"fmt"
	"io"

	"core/shared/apicontract"
	"core/shared/protoapi"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	"core/shared/runtimeids"
)

func writeSessionIdentity(ctx context.Context, remote apicontract.SessionViewService, id runtimeids.SessionID, stdout io.Writer) error {
	result, err := remote.GetSessionIdentityLocation(ctx, &sessionpb.IdentityLocationRequest{
		SessionId: protoapi.SessionSelectorToProto(id),
	})
	if err != nil {
		return err
	}
	if err := protoapi.Validate(result); err != nil {
		return err
	}
	shortID := "unassigned"
	if result.ShortId != nil {
		number, err := protoapi.SessionShortIDFromProto(result.ShortId)
		if err != nil {
			return err
		}
		shortID = number.String()
	}
	_, err = fmt.Fprintf(stdout, "Short ID: %s\nUUID: %s\nLog path: %s\n", shortID, result.SessionId, result.LogPath)
	return err
}
