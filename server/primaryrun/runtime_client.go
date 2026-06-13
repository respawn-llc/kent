package primaryrun

import (
	"context"
	"strings"

	"core/shared/clientui"
)

type gatedRuntimeClient struct {
	sessionID string
	clientui.RuntimeClient
	gate Gate
}

func NewGatedRuntimeClient(sessionID string, inner clientui.RuntimeClient, gate Gate) clientui.RuntimeClient {
	if inner == nil {
		return nil
	}
	if gate == nil || strings.TrimSpace(sessionID) == "" {
		return inner
	}
	return &gatedRuntimeClient{sessionID: strings.TrimSpace(sessionID), RuntimeClient: inner, gate: gate}
}

func (c *gatedRuntimeClient) SubmitUserMessage(ctx context.Context, text string) (string, error) {
	lease, err := c.gate.AcquirePrimaryRun(c.sessionID)
	if err != nil {
		return "", err
	}
	defer lease.Release()
	return c.RuntimeClient.SubmitUserMessage(ctx, text)
}

func (c *gatedRuntimeClient) SubmitUserShellCommand(ctx context.Context, command string) error {
	lease, err := c.gate.AcquirePrimaryRun(c.sessionID)
	if err != nil {
		return err
	}
	defer lease.Release()
	return c.RuntimeClient.SubmitUserShellCommand(ctx, command)
}

func (c *gatedRuntimeClient) SubmitQueuedUserMessages(ctx context.Context) (string, error) {
	lease, err := c.gate.AcquirePrimaryRun(c.sessionID)
	if err != nil {
		return "", err
	}
	defer lease.Release()
	return c.RuntimeClient.SubmitQueuedUserMessages(ctx)
}

var _ clientui.RuntimeClient = (*gatedRuntimeClient)(nil)
