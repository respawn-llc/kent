package toolcodec

import (
	"encoding/json"
	"strings"

	"core/shared/transcript"
)

func EncodeToolCallMeta(meta transcript.ToolCallMeta) json.RawMessage {
	normalized := transcript.NormalizeToolCallMeta(meta)
	if isEmptyToolCallMeta(normalized) {
		return nil
	}
	raw, err := json.Marshal(normalized)
	if err != nil {
		return nil
	}
	return raw
}

func DecodeToolCallMeta(raw json.RawMessage) (*transcript.ToolCallMeta, bool) {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil, false
	}
	var meta transcript.ToolCallMeta
	if err := json.Unmarshal(raw, &meta); err != nil {
		return nil, false
	}
	normalized := transcript.NormalizeToolCallMeta(meta)
	if isEmptyToolCallMeta(normalized) {
		return nil, false
	}
	return &normalized, true
}

func isEmptyToolCallMeta(meta transcript.ToolCallMeta) bool {
	return strings.TrimSpace(meta.ToolName) == "" &&
		strings.TrimSpace(meta.Command) == "" &&
		strings.TrimSpace(meta.CompactText) == "" &&
		strings.TrimSpace(meta.Question) == "" &&
		len(meta.Suggestions) == 0 &&
		meta.RecommendedOptionIndex == 0 &&
		!meta.RawOutputRequested &&
		!meta.OutputTruncated &&
		meta.RenderHint == nil &&
		meta.PatchRender == nil
}
