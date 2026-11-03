package executionfixture

import (
	"context"
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/tools"
	"core/shared/clientui"

	"github.com/google/uuid"
)

// ExecutionContext supplies the identity required by directly invoked tools.
func ExecutionContext(t testing.TB, ctx context.Context, callID string) context.Context {
	t.Helper()
	shortID := testsetup.ShellLogSessionShortID(t)
	return tools.WithExecutionIdentity(ctx, tools.ExecutionIdentity{
		StepID:         uuid.NewString(),
		ToolCallID:     clientui.ToolCallID(callID),
		SessionShortID: &shortID,
	})
}
