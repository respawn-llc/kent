package runtimewirefixture

import (
	"context"

	"core/server/tools"
)

type FileAccessApprover func(context.Context, tools.FileAccessApprovalRequest) (tools.FileAccessApproval, error)

func (FileAccessApprover) SessionAllowed() bool { return false }
func (f FileAccessApprover) Approve(ctx context.Context, request tools.FileAccessApprovalRequest) (tools.FileAccessApproval, error) {
	return f(ctx, request)
}
