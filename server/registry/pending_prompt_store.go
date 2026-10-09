package registry

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"core/server/sessionruntime"
	askquestion "core/server/tools"
	"core/shared/clientui"
	"core/shared/runtimeids"
)

func (s *pendingPromptStore) Visit(ctx context.Context, emit func(string, PendingPromptSnapshot) error) error {
	var err error
	s.sessions.Range(func(key, _ any) bool {
		sessionID := key.(string)
		for _, prompt := range s.List(sessionID) {
			if err = ctx.Err(); err != nil {
				return false
			}
			if err = emit(sessionID, prompt); err != nil {
				return false
			}
		}
		return true
	})
	return err
}

type PendingPromptSnapshot struct {
	Request   askquestion.AskQuestionRequest
	CreatedAt time.Time
	Resource  runtimeids.SessionResourceRef
	ScopeID   runtimeids.ExecutionScopeID
	Batch     *clientui.QuestionBatch
}

type pendingPromptStore struct {
	mu       sync.Mutex
	sessions sync.Map
	batches  map[string]*pendingQuestionBatch
}

type pendingQuestionBatch struct {
	resource       runtimeids.SessionResourceRef
	stepID         string
	toolCallIDs    []string
	unmaterialized map[string]struct{}
}

func (s *pendingPromptStore) Prepare(resource runtimeids.SessionResourceRef, stepID string, toolCallIDs []string) error {
	if err := resource.Validate(); err != nil {
		return err
	}
	if _, err := runtimeids.ParseStepID(stepID); err != nil {
		return err
	}
	if len(toolCallIDs) == 0 {
		return fmt.Errorf("prepared Question batch is empty")
	}
	unmaterialized := make(map[string]struct{}, len(toolCallIDs))
	for _, id := range toolCallIDs {
		if err := clientui.ToolCallID(id).Validate(); err != nil {
			return err
		}
		if _, exists := unmaterialized[id]; exists {
			return fmt.Errorf("prepared Question batch repeats tool call %q", id)
		}
		unmaterialized[id] = struct{}{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.batches == nil {
		s.batches = make(map[string]*pendingQuestionBatch)
	}
	s.batches[resource.SessionID().String()] = &pendingQuestionBatch{
		resource: resource, stepID: stepID, toolCallIDs: slices.Clone(toolCallIDs), unmaterialized: unmaterialized,
	}
	return nil
}

func (s *pendingPromptStore) Finished(resource runtimeids.SessionResourceRef, stepID, toolCallID string) bool {
	id := resource.SessionID().String()
	s.mu.Lock()
	defer s.mu.Unlock()
	batch := s.batches[id]
	if batch == nil || batch.resource != resource || batch.stepID != stepID {
		return false
	}
	if _, exists := batch.unmaterialized[toolCallID]; !exists {
		return false
	}
	delete(batch.unmaterialized, toolCallID)
	pending := maps.Clone(s.load(id))
	s.projectBatchLocked(pending, batch)
	s.sessions.Store(id, pending)
	if len(pending) == 0 && len(batch.unmaterialized) == 0 {
		s.sessions.Delete(id)
		delete(s.batches, id)
	}
	return true
}

func (s *pendingPromptStore) projectBatchLocked(pending map[string]PendingPromptSnapshot, batch *pendingQuestionBatch) {
	projection := &clientui.QuestionBatch{
		ToolCallIDs: batch.toolCallIDs, UnmaterializedCount: uint32(len(batch.unmaterialized)),
	}
	for id, item := range pending {
		if item.Resource == batch.resource && item.Request.StepID == batch.stepID {
			item.Batch = projection
			pending[id] = item
		}
	}
}

func (s *pendingPromptStore) Begin(sessionID string, resource runtimeids.SessionResourceRef, scopeID runtimeids.ExecutionScopeID, req askquestion.AskQuestionRequest, createdAt time.Time) (PendingPromptSnapshot, bool) {
	id, toolCallID := strings.TrimSpace(sessionID), strings.TrimSpace(req.ToolCallID)
	if id == "" || toolCallID == "" {
		return PendingPromptSnapshot{}, false
	}
	snapshot := PendingPromptSnapshot{Request: req.Clone(), CreatedAt: createdAt, Resource: resource, ScopeID: scopeID}
	s.mu.Lock()
	pending := maps.Clone(s.load(id))
	if pending == nil {
		pending = make(map[string]PendingPromptSnapshot)
	}
	pending[toolCallID] = snapshot
	batch := s.batches[id]
	if batch != nil && (batch.resource != resource || batch.stepID != req.StepID) {
		batch = nil
	}
	if req.QuestionBatch != nil {
		if batch == nil || !slices.Equal(batch.toolCallIDs, req.QuestionBatch.BatchToolCallIDs) {
			s.mu.Unlock()
			return PendingPromptSnapshot{}, false
		}
		delete(batch.unmaterialized, toolCallID)
	}
	if batch != nil {
		s.projectBatchLocked(pending, batch)
	}
	snapshot = pending[toolCallID]
	s.sessions.Store(id, pending)
	s.mu.Unlock()
	return clonePendingPromptSnapshot(snapshot), true
}

func (s *pendingPromptStore) Complete(sessionID string, resource runtimeids.SessionResourceRef, scopeID runtimeids.ExecutionScopeID, requestID string) (PendingPromptSnapshot, bool) {
	id, askID := strings.TrimSpace(sessionID), strings.TrimSpace(requestID)
	if id == "" || askID == "" {
		return PendingPromptSnapshot{}, false
	}
	s.mu.Lock()
	pending := s.load(id)
	entry, exists := pending[askID]
	if exists && resource.Validate() == nil && !scopeID.IsZero() &&
		(entry.Resource != resource || entry.ScopeID != scopeID) {
		exists = false
	}
	if exists {
		next := maps.Clone(pending)
		delete(next, askID)
		if len(next) == 0 {
			s.sessions.Delete(id)
			if batch := s.batches[id]; batch != nil && len(batch.unmaterialized) == 0 {
				delete(s.batches, id)
			}
		} else {
			s.sessions.Store(id, next)
		}
	}
	s.mu.Unlock()
	if !exists {
		return PendingPromptSnapshot{}, false
	}
	return clonePendingPromptSnapshot(entry), true
}

func (s *pendingPromptStore) List(sessionID string) []PendingPromptSnapshot {
	if s == nil {
		return nil
	}
	return listPendingPrompts(s.load(strings.TrimSpace(sessionID)))
}

func (s *pendingPromptStore) CloseSession(sessionID string, resolve func(PendingPromptSnapshot)) {
	id := strings.TrimSpace(sessionID)
	s.mu.Lock()
	items := listPendingPrompts(s.load(id))
	s.sessions.Delete(id)
	delete(s.batches, id)
	s.mu.Unlock()
	for _, item := range items {
		if resolve != nil {
			resolve(item)
		}
	}
}

func (s *pendingPromptStore) load(sessionID string) map[string]PendingPromptSnapshot {
	value, ok := s.sessions.Load(sessionID)
	if !ok {
		return nil
	}
	items, ok := value.(map[string]PendingPromptSnapshot)
	if !ok {
		panic("Pending Prompt index contains an invalid entry")
	}
	return items
}

func listPendingPrompts(pending map[string]PendingPromptSnapshot) []PendingPromptSnapshot {
	items := make([]PendingPromptSnapshot, 0, len(pending))
	for _, item := range pending {
		items = append(items, clonePendingPromptSnapshot(item))
	}
	sort.Slice(items, func(i, j int) bool {
		return sessionruntime.PendingPromptOrderLess(items[i].Request, items[i].CreatedAt, items[j].Request, items[j].CreatedAt)
	})
	return items
}

func clonePendingPromptSnapshot(snapshot PendingPromptSnapshot) PendingPromptSnapshot {
	snapshot.Request = snapshot.Request.Clone()
	if snapshot.Batch != nil {
		batch := *snapshot.Batch
		batch.ToolCallIDs = append([]string(nil), batch.ToolCallIDs...)
		snapshot.Batch = &batch
	}
	return snapshot
}
