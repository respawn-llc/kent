package serverapi

import (
	"context"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

type TranscriptSubscription interface {
	Next(ctx context.Context) (*transcriptpb.Message, error)
	Close() error
}
