package config

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

func ProjectIDForWorkspaceRoot(workspaceRoot string) (string, error) {
	canonicalRoot, err := CanonicalWorkspaceRoot(workspaceRoot)
	if err != nil {
		return "", err
	}
	return deterministicProjectID(canonicalRoot), nil
}

func CanonicalWorkspaceRoot(workspaceRoot string) (string, error) {
	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}
	canonicalRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("canonicalize workspace root: %w", err)
		}
		canonicalRoot = absRoot
	}
	return filepath.Clean(canonicalRoot), nil
}

func deterministicProjectID(canonicalRoot string) string {
	sum := sha256.Sum256([]byte(canonicalRoot))
	return fmt.Sprintf("project-%s", hex.EncodeToString(sum[:]))
}
