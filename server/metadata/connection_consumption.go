package metadata

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"core/server/metadata/sqlitegen"
	"core/shared/config"
)

func (s *Store) RecordConnectionConsumption(ctx context.Context, connection config.ConnectionID, now time.Time, score int64) error {
	if _, err := config.ParseConnectionID(string(connection)); err != nil {
		return err
	}
	if score < 0 {
		return fmt.Errorf("connection consumption must be nonnegative")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	q := s.queries.WithTx(tx)
	if err := q.AccumulateConnectionConsumption(ctx, sqlitegen.AccumulateConnectionConsumptionParams{
		ConnectionID: string(connection), HourStartUnix: now.UTC().Truncate(time.Hour).Unix(), Score: score,
	}); err != nil {
		return err
	}
	if err := q.ExpireConnectionConsumption(ctx, consumptionCutoff(now)); err != nil {
		return err
	}
	return tx.Commit()
}

func consumptionCutoff(now time.Time) int64 {
	return now.UTC().Add(-7 * 24 * time.Hour).Truncate(time.Hour).Unix()
}

func (s *Store) ConnectionConsumption(ctx context.Context, connections []config.ConnectionID, now time.Time) (map[config.ConnectionID]int64, error) {
	for _, connection := range connections {
		if _, err := config.ParseConnectionID(string(connection)); err != nil {
			return nil, err
		}
	}
	ids, err := json.Marshal(connections)
	if err != nil {
		return nil, err
	}
	rows, err := s.queries.RetainedConnectionConsumption(ctx, sqlitegen.RetainedConnectionConsumptionParams{
		Cutoff:            consumptionCutoff(now),
		ConnectionIdsJson: string(ids),
	})
	if err != nil {
		return nil, err
	}
	totals := make(map[config.ConnectionID]int64, len(rows))
	for _, row := range rows {
		totals[config.ConnectionID(row.ConnectionID)] = row.Score
	}
	return totals, nil
}
