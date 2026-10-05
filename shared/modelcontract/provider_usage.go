package modelcontract

import (
	"encoding/json"
	"fmt"
	"time"

	"core/shared/textutil"
)

type ProviderOperationPurpose string

const (
	ProviderOperationPurposeGeneration ProviderOperationPurpose = "generation"
	ProviderOperationPurposeReviewer   ProviderOperationPurpose = "reviewer"
	ProviderOperationPurposeCompaction ProviderOperationPurpose = "compaction"
)

func (p ProviderOperationPurpose) Validate() error {
	switch p {
	case ProviderOperationPurposeGeneration, ProviderOperationPurposeReviewer, ProviderOperationPurposeCompaction:
		return nil
	default:
		return fmt.Errorf("unsupported provider operation purpose %q", p)
	}
}

type ProviderUsageEvidence struct {
	ProviderID           *string                   `json:"provider_id"`
	RequestedModel       string                    `json:"requested_model"`
	ServedModel          *string                   `json:"served_model"`
	RequestedServiceTier *string                   `json:"requested_service_tier"`
	ServedServiceTier    *string                   `json:"served_service_tier"`
	ResponseID           *string                   `json:"response_id"`
	ResponseCreatedAt    *time.Time                `json:"response_created_at"`
	Usage                *json.RawMessage          `json:"usage"`
	UsageMetadata        *json.RawMessage          `json:"usage_metadata"`
	HostedTools          []HostedToolUsageEvidence `json:"hosted_tools,omitempty"`
	RequestedHostedTools []HostedToolConfiguration `json:"requested_hosted_tools,omitempty"`
}

type HostedToolUsageEvidence struct {
	ID         *string          `json:"id"`
	Type       *string          `json:"type"`
	Status     *string          `json:"status"`
	ActionKind *string          `json:"action_kind"`
	Usage      *json.RawMessage `json:"usage"`
}

type HostedToolConfiguration struct {
	Type    string          `json:"type"`
	Options json.RawMessage `json:"options"`
}

func (e ProviderUsageEvidence) Clone() ProviderUsageEvidence {
	cloned := e
	cloned.ProviderID = textutil.Pointer(e.ProviderID)
	cloned.ServedModel = textutil.Pointer(e.ServedModel)
	cloned.RequestedServiceTier = textutil.Pointer(e.RequestedServiceTier)
	cloned.ServedServiceTier = textutil.Pointer(e.ServedServiceTier)
	cloned.ResponseID = textutil.Pointer(e.ResponseID)
	cloned.ResponseCreatedAt = textutil.Pointer(e.ResponseCreatedAt)
	cloned.Usage = cloneRawPointer(e.Usage)
	cloned.UsageMetadata = cloneRawPointer(e.UsageMetadata)
	if len(e.HostedTools) > 0 {
		cloned.HostedTools = make([]HostedToolUsageEvidence, len(e.HostedTools))
		for i, tool := range e.HostedTools {
			cloned.HostedTools[i] = tool.Clone()
		}
	}
	if len(e.RequestedHostedTools) > 0 {
		cloned.RequestedHostedTools = make([]HostedToolConfiguration, len(e.RequestedHostedTools))
		for i, tool := range e.RequestedHostedTools {
			cloned.RequestedHostedTools[i] = tool.Clone()
		}
	}
	return cloned
}

func (e HostedToolUsageEvidence) Clone() HostedToolUsageEvidence {
	e.ID = textutil.Pointer(e.ID)
	e.Type = textutil.Pointer(e.Type)
	e.Status = textutil.Pointer(e.Status)
	e.ActionKind = textutil.Pointer(e.ActionKind)
	e.Usage = cloneRawPointer(e.Usage)
	return e
}

func (c HostedToolConfiguration) Clone() HostedToolConfiguration {
	c.Options = append(json.RawMessage(nil), c.Options...)
	return c
}

func cloneRawPointer(value *json.RawMessage) *json.RawMessage {
	if value == nil {
		return nil
	}
	cloned := append(json.RawMessage(nil), (*value)...)
	return &cloned
}
