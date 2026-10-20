package workflowview

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"core/server/metadata"
	"core/server/metadata/sqlitegen"
)

type sessionNameLookup func(context.Context, []string) ([]sqlitegen.ListSessionNamesByIDsRow, error)

type sessionPresentation struct {
	Name    *string
	ShortID *int64
}

func resolveSessionPresentations(
	ctx context.Context,
	lookup sessionNameLookup,
	sessionIDs []string,
) (map[string]sessionPresentation, error) {
	if lookup == nil {
		return nil, errors.New("session name lookup is required")
	}
	uniqueIDs := make([]string, 0, len(sessionIDs))
	seen := make(map[string]struct{}, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		if _, exists := seen[sessionID]; exists {
			continue
		}
		seen[sessionID] = struct{}{}
		uniqueIDs = append(uniqueIDs, sessionID)
	}
	if len(uniqueIDs) == 0 {
		return map[string]sessionPresentation{}, nil
	}
	rows, err := lookup(ctx, uniqueIDs)
	if err != nil {
		return nil, err
	}
	names := make(map[string]sessionPresentation, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.ID) == "" {
			return nil, errors.New("session name lookup returned a blank session id")
		}
		if _, exists := names[row.ID]; exists {
			return nil, fmt.Errorf("session name lookup returned duplicate session %q", row.ID)
		}
		name := metadata.OptionalString(row.Name)
		if name != nil && strings.TrimSpace(*name) == "" {
			return nil, fmt.Errorf("session name lookup returned a blank name for session %q", row.ID)
		}
		names[row.ID] = sessionPresentation{Name: name, ShortID: metadata.OptionalInt64(row.ShortID)}
	}
	for _, sessionID := range uniqueIDs {
		if _, exists := names[sessionID]; !exists {
			return nil, fmt.Errorf("session %q has no persisted metadata", sessionID)
		}
	}
	return names, nil
}
