package transcript

import (
	"core/shared/config"
	"core/shared/llmerrors"
	"errors"
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

type ConnectionNoticeFacts struct {
	Fallback              *config.ConnectionReplacement
	Unavailable           *ConnectionsUnavailableNotice
	ToolOutputRepair      *ToolOutputRepairNotice
	ProviderModelMismatch *ProviderModelMismatchNotice
	ReasoningOmission     *ReasoningOmissionNotice
}

func (facts ConnectionNoticeFacts) Validate() error {
	if facts.Fallback == nil && facts.Unavailable == nil {
		return nil
	}
	if facts.Fallback != nil && !facts.Fallback.Valid() {
		return errors.New("connection fallback identities are invalid")
	}
	if facts.Unavailable != nil && !facts.Unavailable.Valid() {
		return errors.New("unavailable connection facts are invalid")
	}
	if facts.Fallback != nil && facts.Unavailable != nil ||
		facts.ToolOutputRepair != nil || facts.ProviderModelMismatch != nil || facts.ReasoningOmission != nil {
		return errors.New("local entry cannot carry multiple typed notice facts")
	}
	return nil
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
