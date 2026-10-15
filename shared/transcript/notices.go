package transcript

import (
	"core/shared/llmerrors"
	"strings"
)

const (
	NoticeReasonCacheWarning           = "cache_warning"
	NoticeReasonCompaction             = "compaction"
	NoticeReasonLegacyUntypedNotice    = "legacy_untyped_notice"
	NoticeReasonRuntimeDiagnostic      = "runtime_diagnostic"
	NoticeReasonToolOutputRepair       = "tool_output_repair"
	NoticeReasonProviderModelMismatch  = "provider_model_mismatch"
	NoticeReasonThinkingUpdate         = "thinking_update"
	NoticeReasonReasoningOmission      = "reasoning_omission"
	NoticeReasonConnectionFallback     = "connection_fallback"
	NoticeReasonConnectionsUnavailable = "connections_unavailable"

	NoticeSeverityInfo    = "info"
	NoticeSeverityWarning = "warning"
	NoticeSeverityError   = "error"
)

type ToolOutputRepairKind string

const (
	ToolOutputRepairFreshResource         ToolOutputRepairKind = "fresh_resource"
	ToolOutputRepairLiveProviderRejection ToolOutputRepairKind = "live_provider_rejection"
)

type ToolOutputRepairNotice struct {
	Kind  ToolOutputRepairKind `json:"kind"`
	Count int                  `json:"count"`
}

type ProviderModelMismatchNotice struct {
	RequestedModel string `json:"requested_model"`
	ServedModel    string `json:"served_model"`
}

type ReasoningOmissionNotice struct {
	Count int `json:"count"`
}

type ConnectionsUnavailableNotice struct {
	Members     []llmerrors.UnavailableConnection `json:"members"`
	Diagnostics []string                          `json:"diagnostics"`
}

func (n ConnectionsUnavailableNotice) Valid() bool {
	if len(n.Members) == 0 {
		return false
	}
	for _, member := range n.Members {
		if member.ID == "" {
			return false
		}
	}
	return true
}

func (n ReasoningOmissionNotice) Valid() bool {
	return n.Count > 0
}

func (n ProviderModelMismatchNotice) Valid() bool {
	return strings.TrimSpace(n.RequestedModel) != "" &&
		strings.TrimSpace(n.ServedModel) != ""
}

func (n ToolOutputRepairNotice) Valid() bool {
	switch n.Kind {
	case ToolOutputRepairFreshResource, ToolOutputRepairLiveProviderRejection:
		return n.Count > 0
	default:
		return false
	}
}

func LegacyNoticeReasonForRole(role string) string {
	if strings.TrimSpace(role) == NoticeReasonCacheWarning {
		return NoticeReasonCacheWarning
	}
	return NoticeReasonLegacyUntypedNotice
}

func LegacyNoticeSeverityForRole(role string) string {
	switch strings.TrimSpace(role) {
	case NoticeSeverityError,
		string(EntryRoleDeveloperErrorFeedback),
		string(EntryRoleInterruption),
		string(EntryRoleReviewerError):
		return NoticeSeverityError
	case NoticeSeverityWarning, NoticeReasonCacheWarning:
		return NoticeSeverityWarning
	default:
		return NoticeSeverityInfo
	}
}
