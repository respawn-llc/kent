package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"core/shared/brand"
	"core/shared/clientui"
	"core/shared/serverapi"
)

func formatBindingCommandWorkspaceLabel(path string) string {
	trimmedPath := strings.TrimSpace(path)
	if trimmedPath == "" {
		trimmedPath = "."
	}
	absolutePath, err := filepath.Abs(trimmedPath)
	if err != nil {
		return trimmedPath
	}
	return absolutePath
}

func formatProjectLookupCommandError(path string, err error) error {
	if !errors.Is(err, errWorkspaceNotRegistered) {
		return err
	}
	return fmt.Errorf("%w: %s is not attached to a project", errWorkspaceNotRegistered, formatBindingCommandWorkspaceLabel(path))
}

func formatAttachWorkspaceCommandError(targetPath string, explicitProjectID string, err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, serverapi.ErrProjectNotFound):
		trimmedProjectID := strings.TrimSpace(explicitProjectID)
		if trimmedProjectID == "" {
			trimmedProjectID = "selected project"
		}
		return fmt.Errorf("project %q does not exist in this "+brand.Product+" state: %w", trimmedProjectID, err)
	case errors.Is(err, serverapi.ErrProjectUnavailable):
		if unavailable, ok := serverapi.AsProjectUnavailable(err); ok {
			switch unavailable.Availability {
			case clientui.ProjectAvailabilityMissing:
				return fmt.Errorf("project %q root %q is missing. Rebind affected sessions from their new workspace roots: %w", unavailable.ProjectID, unavailable.RootPath, err)
			case clientui.ProjectAvailabilityInaccessible:
				return fmt.Errorf("project %q root %q is inaccessible. Restore access or rebind affected sessions from another workspace root: %w", unavailable.ProjectID, unavailable.RootPath, err)
			}
		}
	case errors.Is(err, errWorkspaceNotRegistered):
		return err
	}
	_ = targetPath
	return err
}
