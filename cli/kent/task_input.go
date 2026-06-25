package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

func readTaskBodyFlag(body string, bodyFile string) (string, error) {
	trimmedBody := strings.TrimSpace(body)
	if strings.TrimSpace(bodyFile) == "" {
		if trimmedBody == "" {
			return "", errors.New("either --body or --body-file is required")
		}
		return body, nil
	}
	if body != "" {
		return "", errors.New("--body cannot be combined with --body-file")
	}
	content, err := os.ReadFile(bodyFile)
	if err != nil {
		return "", fmt.Errorf("read --body-file: %w", err)
	}
	return string(content), nil
}
