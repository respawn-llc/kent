package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"core/server/metadata/sqlitegen"
	"core/server/session"
	"core/shared/runtimeids"
)

// Both creation paths already hold the metadata write lock in their transaction.
func persistSessionWithShortID(ctx context.Context, q *sqlitegen.Queries, params sqlitegen.UpsertSessionParams) error {
	_, err := q.GetSessionShortID(sqlitegen.WithExpectedNoRows(ctx), params.ID)
	if errors.Is(err, sql.ErrNoRows) {
		value, err := q.AllocateSessionShortID(ctx, runtimeids.MaxSessionShortID)
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("Session short ID sequence exhausted")
		}
		if err != nil {
			return fmt.Errorf("allocate Session short ID: %w", err)
		}
		params.ShortID = sql.NullInt64{Int64: value, Valid: true}
	} else if err != nil {
		return fmt.Errorf("read Session short ID: %w", err)
	}
	return q.UpsertSession(ctx, params)
}

func sessionShortID(value sql.NullInt64) (*runtimeids.SessionShortID, error) {
	if !value.Valid {
		return nil, nil
	}
	id, err := runtimeids.NewSessionShortID(value.Int64)
	if err != nil {
		return nil, err
	}
	return &id, nil
}

func (s *Store) ResolveSessionSelector(ctx context.Context, selector runtimeids.SessionSelector) (runtimeids.SessionID, error) {
	switch value := selector.(type) {
	case runtimeids.SessionID:
		if !value.IsCanonicalUUIDv4() {
			return runtimeids.SessionID{}, errors.New("Session selector requires a canonical UUIDv4")
		}
		return value, nil
	case runtimeids.SessionShortID:
		if _, err := runtimeids.NewSessionShortID(value.Int64()); err != nil {
			return runtimeids.SessionID{}, err
		}
		raw, err := s.queries.GetSessionIDByShortID(sqlitegen.WithExpectedNoRows(ctx), sql.NullInt64{Int64: value.Int64(), Valid: true})
		if errors.Is(err, sql.ErrNoRows) {
			return runtimeids.SessionID{}, fmt.Errorf("%w: %s; it might have been deleted", session.ErrSessionNotFound, value)
		}
		if err != nil {
			return runtimeids.SessionID{}, fmt.Errorf("resolve Session selector: %w", err)
		}
		return runtimeids.ParseSessionID(raw)
	default:
		return runtimeids.SessionID{}, errors.New("Session selector is required")
	}
}
