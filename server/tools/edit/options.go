package edit

import "core/server/tools/fsguard"

type Option func(*Tool)

func WithAllowOutsideWorkspace(allow bool) Option {
	return func(t *Tool) {
		t.allowOutsideWorkspace = allow
	}
}

func WithOutsideWorkspaceApprover(approver fsguard.Approver) Option {
	return func(t *Tool) {
		t.outsideWorkspaceApprover = approver
	}
}
