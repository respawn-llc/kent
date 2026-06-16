package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"core/server/metadata/sqlitegen"
)

type PromptHistorySource string

const (
	PromptHistorySourceSubmitUserMessage   PromptHistorySource = "submit_user_message"
	PromptHistorySourceSubmitUserTurn      PromptHistorySource = "submit_user_turn"
	PromptHistorySourceQueueUserMessage    PromptHistorySource = "queue_user_message"
	PromptHistorySourceRecordPromptHistory PromptHistorySource = "record_prompt_history"
	PromptHistorySourceRunPrompt           PromptHistorySource = "run_prompt"
)

type PromptHistoryQueueState string

const (
	PromptHistoryQueueStateRecorded  PromptHistoryQueueState = "recorded"
	PromptHistoryQueueStatePending   PromptHistoryQueueState = "pending"
	PromptHistoryQueueStateConsumed  PromptHistoryQueueState = "consumed"
	PromptHistoryQueueStateDiscarded PromptHistoryQueueState = "discarded"
)

var ErrPromptHistoryConflict = errors.New("prompt history conflict")

type PromptHistoryEntry struct {
	SessionID       string
	Source          PromptHistorySource
	SourceID        string
	ClientRequestID string
	QueueItemID     string
	QueueState      PromptHistoryQueueState
	Text            string
	CreatedAt       time.Time
}

type PromptHistoryRecord struct {
	Sequence        int64
	SessionID       string
	Source          PromptHistorySource
	SourceID        string
	ClientRequestID string
	QueueItemID     string
	QueueState      PromptHistoryQueueState
	Text            string
	CreatedAt       time.Time
}

func (s *Store) ReadPromptHistory(ctx context.Context, sessionID string) ([]string, error) {
	if s == nil || s.queries == nil {
		return nil, errors.New("metadata store is required")
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return nil, errors.New("session_id is required")
	}
	history, err := s.queries.ListSessionPromptHistoryText(ctx, trimmedSessionID)
	if err != nil {
		return nil, fmt.Errorf("list prompt history: %w", err)
	}
	return history, nil
}

func (s *Store) RecordPromptHistoryEntry(ctx context.Context, entry PromptHistoryEntry) (PromptHistoryRecord, bool, error) {
	if s == nil || s.queries == nil {
		return PromptHistoryRecord{}, false, errors.New("metadata store is required")
	}
	normalized, err := normalizePromptHistoryEntry(entry)
	if err != nil {
		return PromptHistoryRecord{}, false, err
	}
	inserted, err := s.queries.InsertSessionPromptHistoryEntry(ctx, sqlitegen.InsertSessionPromptHistoryEntryParams{
		SessionID:       normalized.SessionID,
		Source:          string(normalized.Source),
		SourceID:        normalized.SourceID,
		ClientRequestID: normalized.ClientRequestID,
		QueueItemID:     normalized.QueueItemID,
		QueueState:      string(normalized.QueueState),
		Text:            normalized.Text,
		CreatedAtUnixMs: normalized.CreatedAt.UTC().UnixMilli(),
	})
	if err != nil {
		return PromptHistoryRecord{}, false, fmt.Errorf("insert prompt history: %w", err)
	}
	if inserted > 0 {
		record, err := s.promptHistoryRecordBySource(ctx, normalized.SessionID, normalized.Source, normalized.SourceID)
		return record, true, err
	}
	existing, err := s.promptHistoryConflictRecord(ctx, normalized)
	if err != nil {
		return PromptHistoryRecord{}, false, err
	}
	if !promptHistoryEquivalent(normalized, existing) {
		return PromptHistoryRecord{}, false, fmt.Errorf("%w: source=%s source_id=%q client_request_id=%q", ErrPromptHistoryConflict, normalized.Source, normalized.SourceID, normalized.ClientRequestID)
	}
	return existing, false, nil
}

func (s *Store) MarkPromptHistoryQueueState(ctx context.Context, sessionID string, queueItemID string, state PromptHistoryQueueState) (PromptHistoryRecord, bool, error) {
	if s == nil || s.queries == nil {
		return PromptHistoryRecord{}, false, errors.New("metadata store is required")
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	trimmedQueueItemID := strings.TrimSpace(queueItemID)
	normalizedState := PromptHistoryQueueState(strings.TrimSpace(string(state)))
	if trimmedSessionID == "" {
		return PromptHistoryRecord{}, false, errors.New("session_id is required")
	}
	if trimmedQueueItemID == "" {
		return PromptHistoryRecord{}, false, errors.New("queue_item_id is required")
	}
	if !validPromptHistoryQueueState(normalizedState) {
		return PromptHistoryRecord{}, false, fmt.Errorf("invalid queue state %q", state)
	}
	updated, err := s.queries.UpdateSessionPromptHistoryQueueState(ctx, sqlitegen.UpdateSessionPromptHistoryQueueStateParams{
		SessionID:   trimmedSessionID,
		QueueItemID: trimmedQueueItemID,
		QueueState:  string(normalizedState),
	})
	if err != nil {
		return PromptHistoryRecord{}, false, fmt.Errorf("update prompt history queue state: %w", err)
	}
	if updated == 0 {
		record, err := s.promptHistoryRecordBySource(ctx, trimmedSessionID, PromptHistorySourceQueueUserMessage, trimmedQueueItemID)
		if err == nil && (record.QueueState == PromptHistoryQueueStateConsumed || record.QueueState == PromptHistoryQueueStateDiscarded) {
			return record, false, nil
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return PromptHistoryRecord{}, false, err
		}
		return PromptHistoryRecord{}, false, sql.ErrNoRows
	}
	record, err := s.promptHistoryRecordBySource(ctx, trimmedSessionID, PromptHistorySourceQueueUserMessage, trimmedQueueItemID)
	return record, true, err
}

func (s *Store) ReadPromptHistoryQueueItem(ctx context.Context, sessionID string, queueItemID string) (PromptHistoryRecord, error) {
	if s == nil || s.queries == nil {
		return PromptHistoryRecord{}, errors.New("metadata store is required")
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	trimmedQueueItemID := strings.TrimSpace(queueItemID)
	if trimmedSessionID == "" {
		return PromptHistoryRecord{}, errors.New("session_id is required")
	}
	if trimmedQueueItemID == "" {
		return PromptHistoryRecord{}, errors.New("queue_item_id is required")
	}
	return s.promptHistoryRecordBySource(ctx, trimmedSessionID, PromptHistorySourceQueueUserMessage, trimmedQueueItemID)
}

func (s *Store) MarkPromptHistoryQueueItemsConsumed(ctx context.Context, sessionID string, queueItemIDs []string) (int64, error) {
	if s == nil || s.queries == nil {
		return 0, errors.New("metadata store is required")
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return 0, errors.New("session_id is required")
	}
	ids := make([]string, 0, len(queueItemIDs))
	seen := map[string]bool{}
	for _, raw := range queueItemIDs {
		id := strings.TrimSpace(raw)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return 0, nil
	}
	updated, err := s.queries.MarkSessionPromptHistoryQueueItemsConsumed(ctx, sqlitegen.MarkSessionPromptHistoryQueueItemsConsumedParams{
		SessionID:    trimmedSessionID,
		QueueItemIds: ids,
	})
	if err != nil {
		return 0, fmt.Errorf("mark prompt history queue items consumed: %w", err)
	}
	return updated, nil
}

func (s *Store) promptHistoryConflictRecord(ctx context.Context, entry PromptHistoryEntry) (PromptHistoryRecord, error) {
	if entry.ClientRequestID != "" {
		record, err := s.promptHistoryRecordByClientRequest(ctx, entry.SessionID, entry.Source, entry.ClientRequestID)
		if err == nil {
			return record, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return PromptHistoryRecord{}, err
		}
	}
	return s.promptHistoryRecordBySource(ctx, entry.SessionID, entry.Source, entry.SourceID)
}

func (s *Store) promptHistoryRecordBySource(ctx context.Context, sessionID string, source PromptHistorySource, sourceID string) (PromptHistoryRecord, error) {
	row, err := s.queries.GetSessionPromptHistoryEntryBySource(ctx, sqlitegen.GetSessionPromptHistoryEntryBySourceParams{
		SessionID: strings.TrimSpace(sessionID),
		Source:    string(source),
		SourceID:  strings.TrimSpace(sourceID),
	})
	if err != nil {
		return PromptHistoryRecord{}, fmt.Errorf("get prompt history by source: %w", err)
	}
	return promptHistoryRecordFromRow(row), nil
}

func (s *Store) promptHistoryRecordByClientRequest(ctx context.Context, sessionID string, source PromptHistorySource, clientRequestID string) (PromptHistoryRecord, error) {
	row, err := s.queries.GetSessionPromptHistoryEntryByClientRequest(ctx, sqlitegen.GetSessionPromptHistoryEntryByClientRequestParams{
		SessionID:       strings.TrimSpace(sessionID),
		Source:          string(source),
		ClientRequestID: strings.TrimSpace(clientRequestID),
	})
	if err != nil {
		return PromptHistoryRecord{}, fmt.Errorf("get prompt history by client request: %w", err)
	}
	return promptHistoryRecordFromRow(row), nil
}

func normalizePromptHistoryEntry(entry PromptHistoryEntry) (PromptHistoryEntry, error) {
	normalized := PromptHistoryEntry{
		SessionID:       strings.TrimSpace(entry.SessionID),
		Source:          PromptHistorySource(strings.TrimSpace(string(entry.Source))),
		SourceID:        strings.TrimSpace(entry.SourceID),
		ClientRequestID: strings.TrimSpace(entry.ClientRequestID),
		QueueItemID:     strings.TrimSpace(entry.QueueItemID),
		QueueState:      PromptHistoryQueueState(strings.TrimSpace(string(entry.QueueState))),
		Text:            entry.Text,
		CreatedAt:       entry.CreatedAt,
	}
	if normalized.CreatedAt.IsZero() {
		normalized.CreatedAt = time.Now().UTC()
	}
	if normalized.SessionID == "" {
		return PromptHistoryEntry{}, errors.New("session_id is required")
	}
	if !validPromptHistorySource(normalized.Source) {
		return PromptHistoryEntry{}, fmt.Errorf("invalid prompt history source %q", entry.Source)
	}
	if normalized.SourceID == "" {
		return PromptHistoryEntry{}, errors.New("source_id is required")
	}
	if strings.TrimSpace(normalized.Text) == "" {
		return PromptHistoryEntry{}, errors.New("text is required")
	}
	if normalized.Source == PromptHistorySourceQueueUserMessage {
		if normalized.QueueItemID == "" {
			return PromptHistoryEntry{}, errors.New("queue_item_id is required")
		}
		if !validPromptHistoryQueueState(normalized.QueueState) {
			return PromptHistoryEntry{}, fmt.Errorf("invalid queue state %q", entry.QueueState)
		}
	} else {
		if normalized.QueueItemID != "" || normalized.QueueState != "" {
			return PromptHistoryEntry{}, errors.New("queue metadata is only valid for queued prompt history")
		}
	}
	return normalized, nil
}

func validPromptHistorySource(source PromptHistorySource) bool {
	switch source {
	case PromptHistorySourceSubmitUserMessage, PromptHistorySourceSubmitUserTurn, PromptHistorySourceQueueUserMessage, PromptHistorySourceRecordPromptHistory, PromptHistorySourceRunPrompt:
		return true
	default:
		return false
	}
}

func validPromptHistoryQueueState(state PromptHistoryQueueState) bool {
	switch state {
	case PromptHistoryQueueStateRecorded, PromptHistoryQueueStatePending, PromptHistoryQueueStateConsumed, PromptHistoryQueueStateDiscarded:
		return true
	default:
		return false
	}
}

func promptHistoryEquivalent(entry PromptHistoryEntry, record PromptHistoryRecord) bool {
	return entry.SessionID == record.SessionID &&
		entry.Source == record.Source &&
		entry.SourceID == record.SourceID &&
		entry.ClientRequestID == record.ClientRequestID &&
		entry.QueueItemID == record.QueueItemID &&
		entry.Text == record.Text
}

func promptHistoryRecordFromRow(row sqlitegen.SessionPromptHistoryEntry) PromptHistoryRecord {
	return PromptHistoryRecord{
		Sequence:        row.Sequence,
		SessionID:       row.SessionID,
		Source:          PromptHistorySource(row.Source),
		SourceID:        row.SourceID,
		ClientRequestID: row.ClientRequestID,
		QueueItemID:     row.QueueItemID,
		QueueState:      PromptHistoryQueueState(row.QueueState),
		Text:            row.Text,
		CreatedAt:       time.UnixMilli(row.CreatedAtUnixMs).UTC(),
	}
}
