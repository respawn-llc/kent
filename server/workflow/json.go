package workflow

import (
	"encoding/json"
	"fmt"
	"strings"
)

func MarshalString(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func MustMarshalString(value any) string {
	raw, err := MarshalString(value)
	if err != nil {
		panic(fmt.Errorf("MustMarshalString: %w", err))
	}
	return raw
}

func UnmarshalString(raw string, target any) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("decode workflow JSON: %w", err)
	}
	return nil
}
