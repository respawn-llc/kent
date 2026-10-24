package clientui

import (
	"fmt"
	"strings"
	"time"

	"core/shared/runtimeids"
	"core/shared/sessioncontract"
)

type ApprovalDecision = sessioncontract.PromptApprovalDecision

const (
	ApprovalDecisionAllowOnce    = sessioncontract.PromptApprovalDecisionAllowOnce
	ApprovalDecisionAllowSession = sessioncontract.PromptApprovalDecisionAllowSession
	ApprovalDecisionDeny         = sessioncontract.PromptApprovalDecisionDeny
)

type ApprovalOption struct {
	Decision ApprovalDecision
}

type FileAccessTarget struct {
	RequestedPath string `json:"requested_path"`
	ResolvedPath  string `json:"resolved_path"`
}

func FormatFileAccessApprovalMarkdown(targets []FileAccessTarget) string {
	var formatted strings.Builder
	single := len(targets) == 1
	if single {
		formatted.WriteString("Agent wants to access a file ")
	} else {
		fmt.Fprintf(&formatted, "Agent wants to access a batch of files, but %d are outside workspace dir:", len(targets))
		formatted.WriteByte('\n')
	}
	for _, target := range targets {
		if !single {
			formatted.WriteString("\n- ")
		}
		formatted.WriteString(target.RequestedPath)
		if target.RequestedPath != target.ResolvedPath {
			fmt.Fprintf(&formatted, " → %s", target.ResolvedPath)
		}
	}
	if single {
		formatted.WriteString(".\n\nAllow?")
	} else {
		formatted.WriteString("\n\nAllow this access?")
	}
	return formatted.String()
}

type PendingApproval struct {
	ShortID       *runtimeids.SessionShortID
	ToolCallID    ToolCallID
	SessionID     runtimeids.SessionID
	StepID        runtimeids.StepID
	Question      string
	Options       []ApprovalOption
	AccessTargets []FileAccessTarget
	CreatedAt     time.Time
	Batch         *QuestionBatch
}
