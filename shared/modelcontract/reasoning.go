package modelcontract

import "fmt"

type ReasoningType string

const (
	ReasoningTypeOpenAI      ReasoningType = "openai"
	ReasoningTypeAnthropic   ReasoningType = "anthropic"
	ReasoningTypeGrok        ReasoningType = "grok"
	ReasoningTypeUnencrypted ReasoningType = "unencrypted"
)

// An absent record is legacy unattributed context; a present null Type records
// an unknown producing format and must not be inferred later.
type ReasoningAttribution struct {
	Type *ReasoningType `json:"type"`
}

func (a *ReasoningAttribution) Clone() *ReasoningAttribution {
	if a == nil {
		return nil
	}
	copy := *a
	if a.Type != nil {
		value := *a.Type
		copy.Type = &value
	}
	return &copy
}

func (a *ReasoningAttribution) Validate() error {
	if a == nil || a.Type == nil {
		return nil
	}
	switch *a.Type {
	case ReasoningTypeOpenAI, ReasoningTypeAnthropic, ReasoningTypeGrok, ReasoningTypeUnencrypted:
		return nil
	default:
		return fmt.Errorf("invalid reasoning type %q", *a.Type)
	}
}
