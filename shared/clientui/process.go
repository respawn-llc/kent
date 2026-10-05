package clientui

import (
	"context"
	processpb "core/shared/protoapi/gen/kent/api/process"
)

type ProcessClient interface {
	ListProcesses(ctx context.Context) ([]*processpb.BackgroundProcess, error)
	KillProcess(ctx context.Context, id string) error
	InlineOutput(ctx context.Context, id string, maxChars int) (string, string, error)
}
