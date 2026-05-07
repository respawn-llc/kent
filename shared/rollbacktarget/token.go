package rollbacktarget

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
)

const tokenVersion = 1

type tokenPayload struct {
	Version          int `json:"v"`
	UserMessageIndex int `json:"u"`
}

func EncodeUserMessageIndex(index int) string {
	if index <= 0 {
		return ""
	}
	payload, err := json.Marshal(tokenPayload{Version: tokenVersion, UserMessageIndex: index})
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(payload)
}

func DecodeUserMessageIndex(raw string) (int, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, fmt.Errorf("rollback target id is required")
	}
	payload, err := base64.RawURLEncoding.DecodeString(trimmed)
	if err != nil {
		return 0, fmt.Errorf("invalid rollback target id")
	}
	var decoded tokenPayload
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return 0, fmt.Errorf("invalid rollback target id")
	}
	if decoded.Version != tokenVersion {
		return 0, fmt.Errorf("unsupported rollback target id version")
	}
	if decoded.UserMessageIndex <= 0 {
		return 0, fmt.Errorf("invalid rollback target id")
	}
	return decoded.UserMessageIndex, nil
}
