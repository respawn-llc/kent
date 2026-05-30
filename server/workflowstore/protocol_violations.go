package workflowstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"builder/server/metadata/sqlitegen"
)

// RecordProtocolViolation increments the active run's protocol-violation
// counter. If the run became terminal before the increment, no new violation is
// recorded and the existing counter is returned with Interrupted=true so callers
// can stop retrying without surfacing a stale terminal-run error.
func (s *Store) RecordProtocolViolation(ctx context.Context, req RecordProtocolViolationRequest) (RecordProtocolViolationResult, error) {
	if strings.TrimSpace(string(req.RunID)) == "" {
		return RecordProtocolViolationResult{}, errors.New("run id is required")
	}
	if req.MaxCount <= 0 {
		return RecordProtocolViolationResult{}, errors.New("protocol violation max count must be > 0")
	}
	detail := strings.TrimSpace(req.Detail)
	if detail == "" {
		detail = "{}"
	}
	now := s.now().UnixMilli()
	var count int64
	var interruptedAt int64
	var err error
	switch req.Kind {
	case ProtocolViolationFinalAnswer:
		err = s.db.QueryRowContext(ctx, workflowStoreQuery(recordFinalAnswerProtocolViolationQuery),
			now, req.MaxCount, now, req.MaxCount, req.MaxCount, detail, string(req.RunID), boolToInt64(req.RequireGeneration), req.ExpectedGeneration,
		).Scan(&count, &interruptedAt)
	case ProtocolViolationInvalidCompletion:
		err = s.db.QueryRowContext(ctx, workflowStoreQuery(recordInvalidCompletionProtocolViolationQuery),
			now, req.MaxCount, now, req.MaxCount, req.MaxCount, detail, string(req.RunID), boolToInt64(req.RequireGeneration), req.ExpectedGeneration,
		).Scan(&count, &interruptedAt)
	default:
		return RecordProtocolViolationResult{}, fmt.Errorf("unsupported protocol violation kind %q", req.Kind)
	}
	if errors.Is(err, sql.ErrNoRows) {
		run, getErr := s.queries.GetTaskRun(ctx, string(req.RunID))
		if getErr != nil {
			return RecordProtocolViolationResult{}, getErr
		}
		if run.CompletedAtUnixMs != 0 {
			return RecordProtocolViolationResult{Count: protocolViolationCount(run, req.Kind), Interrupted: true}, nil
		}
		if run.InterruptedAtUnixMs != 0 {
			return RecordProtocolViolationResult{Count: protocolViolationCount(run, req.Kind), Interrupted: true}, nil
		}
		if req.RequireGeneration && run.RunGeneration != req.ExpectedGeneration {
			return RecordProtocolViolationResult{}, fmt.Errorf("stale workflow run generation: got %d want %d", req.ExpectedGeneration, run.RunGeneration)
		}
		return RecordProtocolViolationResult{}, sql.ErrNoRows
	}
	if err != nil {
		return RecordProtocolViolationResult{}, err
	}
	return RecordProtocolViolationResult{Count: count, Interrupted: interruptedAt != 0}, nil
}

func protocolViolationCount(run sqlitegen.TaskRunRecord, kind ProtocolViolationKind) int64 {
	switch kind {
	case ProtocolViolationFinalAnswer:
		return run.FinalAnswerViolationCount
	case ProtocolViolationInvalidCompletion:
		return run.InvalidCompletionCount
	default:
		return 0
	}
}
