package llm

import (
	"encoding/json"
	"fmt"

	"core/server/llm/openaiwire"
)

func ValidateVisionInputs(items []ResponseItem, supported bool) error {
	if supported {
		return nil
	}
	for _, item := range items {
		var raw json.RawMessage
		switch item.Type {
		case ResponseItemTypeMessage:
			if len(item.Raw) == 0 {
				continue
			}
			var message struct {
				Content json.RawMessage `json:"content"`
			}
			if err := json.Unmarshal(item.Raw, &message); err != nil {
				return fmt.Errorf("%w: decode caller message content: %w", ErrInvalidRequest, err)
			}
			raw = message.Content
		case ResponseItemTypeFunctionCallOutput:
			raw = item.Output
		default:
			continue
		}
		content, structured := openaiwire.DecodeInputContentItems(raw)
		if !structured {
			continue
		}
		for _, part := range content {
			switch part.Type {
			case "input_image", "input_file":
				return fmt.Errorf("%w: nano model does not support vision inputs", ErrInvalidRequest)
			}
		}
	}
	return nil
}
