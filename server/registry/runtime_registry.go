package registry

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"builder/server/primaryrun"
	"builder/server/runtime"
	"builder/server/runtimeview"
	askquestion "builder/server/tools/askquestion"
	"builder/shared/clientui"
	"builder/shared/serverapi"
)

const (
	sessionActivityBufferSize = 256
	promptActivityBufferSize  = 64
)

type RuntimeRegistry struct {
	mu         sync.RWMutex
	engines    map[string]*runtimeEntry
	primaryRun map[string]uint64
	nextLease  uint64
}

type runtimeEntry struct {
	engine        *runtime.Engine
	hub           *sessionActivityHub
	promptHub     *promptActivityHub
	pendingMu     sync.RWMutex
	pendingPrompt map[string]*pendingPromptEntry
}

type PendingPromptSnapshot struct {
	Request   askquestion.Request
	CreatedAt time.Time
}

type pendingPromptEntry struct {
	PendingPromptSnapshot
	response chan promptResponseResult
	closed   bool
}

type promptResponseResult struct {
	response askquestion.Response
	err      error
}

type sessionActivityHub struct {
	mu          sync.Mutex
	nextID      uint64
	nextSeq     uint64
	history     []clientui.Event
	closed      bool
	subscribers map[uint64]*sessionActivitySubscription
}

type promptActivityHub struct {
	mu          sync.Mutex
	nextID      uint64
	nextSeq     uint64
	history     []clientui.PendingPromptEvent
	closed      bool
	subscribers map[uint64]*promptActivitySubscription
}

type sessionActivitySubscription struct {
	ch      chan clientui.Event
	onClose func()

	mu   sync.Mutex
	err  error
	done bool
}

type promptActivitySubscription struct {
	ch      chan clientui.PendingPromptEvent
	onClose func()

	mu      sync.Mutex
	initial []clientui.PendingPromptEvent
	err     error
	done    bool
}

func NewRuntimeRegistry() *RuntimeRegistry {
	return &RuntimeRegistry{engines: make(map[string]*runtimeEntry), primaryRun: make(map[string]uint64)}
}

func (r *RuntimeRegistry) Register(sessionID string, engine *runtime.Engine) {
	if r == nil || engine == nil {
		return
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return
	}
	entry := &runtimeEntry{engine: engine, hub: newSessionActivityHub(), promptHub: newPromptActivityHub(), pendingPrompt: make(map[string]*pendingPromptEntry)}
	r.mu.Lock()
	previous := r.engines[id]
	r.engines[id] = entry
	r.mu.Unlock()
	if previous != nil {
		previous.closePendingPrompts(io.EOF)
	}
	if previous != nil && previous.promptHub != nil {
		previous.promptHub.close(io.EOF)
	}
	if previous != nil && previous.hub != nil {
		previous.hub.close(io.EOF)
	}
}

func (r *RuntimeRegistry) Unregister(sessionID string, engine *runtime.Engine) {
	if r == nil {
		return
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return
	}
	r.mu.Lock()
	entry := r.engines[id]
	if entry == nil || (engine != nil && entry.engine != engine) {
		r.mu.Unlock()
		return
	}
	delete(r.engines, id)
	delete(r.primaryRun, id)
	r.mu.Unlock()
	if entry != nil {
		entry.closePendingPrompts(io.EOF)
	}
	if entry != nil && entry.promptHub != nil {
		entry.promptHub.close(io.EOF)
	}
	if entry != nil && entry.hub != nil {
		entry.hub.close(io.EOF)
	}
}

func (r *RuntimeRegistry) ResolveRuntime(_ context.Context, sessionID string) (*runtime.Engine, error) {
	if r == nil {
		return nil, nil
	}
	id := strings.TrimSpace(sessionID)
	r.mu.RLock()
	entry := r.engines[id]
	r.mu.RUnlock()
	if entry == nil {
		return nil, nil
	}
	return entry.engine, nil
}

func (r *RuntimeRegistry) IsSessionRuntimeActive(sessionID string) bool {
	if r == nil {
		return false
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return false
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.engines[id] != nil
}

func (r *RuntimeRegistry) PublishRuntimeEvent(sessionID string, evt runtime.Event) {
	if r == nil {
		return
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return
	}
	r.mu.RLock()
	entry := r.engines[id]
	r.mu.RUnlock()
	if entry == nil || entry.hub == nil {
		return
	}
	entry.hub.publish(runtimeview.EventFromRuntime(evt))
}

func (r *RuntimeRegistry) SubscribeSessionActivity(_ context.Context, sessionID string) (serverapi.SessionActivitySubscription, error) {
	return r.SubscribeSessionActivityFrom(context.Background(), serverapi.SessionActivitySubscribeRequest{SessionID: sessionID})
}

func (r *RuntimeRegistry) SubscribeSessionActivityFrom(_ context.Context, req serverapi.SessionActivitySubscribeRequest) (serverapi.SessionActivitySubscription, error) {
	if r == nil {
		return nil, fmt.Errorf("runtime registry is required")
	}
	id := strings.TrimSpace(req.SessionID)
	r.mu.RLock()
	entry := r.engines[id]
	r.mu.RUnlock()
	if entry == nil || entry.hub == nil {
		return nil, fmt.Errorf("session activity stream for %q is unavailable: %w", id, serverapi.ErrSessionActivityUnavailable)
	}
	return entry.hub.subscribe(req.AfterSequence)
}

func (r *RuntimeRegistry) SubscribePromptActivity(_ context.Context, sessionID string) (serverapi.PromptActivitySubscription, error) {
	return r.SubscribePromptActivityFrom(context.Background(), serverapi.PromptActivitySubscribeRequest{SessionID: sessionID})
}

func (r *RuntimeRegistry) SubscribePromptActivityFrom(_ context.Context, req serverapi.PromptActivitySubscribeRequest) (serverapi.PromptActivitySubscription, error) {
	if r == nil {
		return nil, fmt.Errorf("runtime registry is required")
	}
	id := strings.TrimSpace(req.SessionID)
	r.mu.RLock()
	entry := r.engines[id]
	r.mu.RUnlock()
	if entry == nil || entry.promptHub == nil {
		return nil, fmt.Errorf("prompt activity stream for %q is unavailable: %w", id, serverapi.ErrStreamUnavailable)
	}
	initial := []clientui.PendingPromptEvent(nil)
	if req.AfterSequence == 0 {
		entry.pendingMu.Lock()
		initial = make([]clientui.PendingPromptEvent, 0, len(entry.pendingPrompt)+1)
		for _, item := range entry.listPendingPromptsLocked() {
			initial = append(initial, pendingPromptEventFromSnapshot(id, item, clientui.PendingPromptEventPending))
		}
		initial = append(initial, clientui.PendingPromptEvent{Type: clientui.PendingPromptEventSnapshot, SessionID: id})
		entry.pendingMu.Unlock()
	}
	sub, err := entry.promptHub.subscribe(initial, req.AfterSequence)
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, fmt.Errorf("prompt activity stream for %q is unavailable: %w", id, serverapi.ErrStreamUnavailable)
	}
	return sub, nil
}

func (r *RuntimeRegistry) BeginPendingPrompt(sessionID string, req askquestion.Request) {
	if r == nil {
		return
	}
	id := strings.TrimSpace(sessionID)
	requestID := strings.TrimSpace(req.ID)
	if id == "" || requestID == "" {
		return
	}
	r.mu.RLock()
	entry := r.engines[id]
	r.mu.RUnlock()
	if entry == nil {
		return
	}
	snapshot := PendingPromptSnapshot{Request: req, CreatedAt: time.Now()}
	entry.pendingMu.Lock()
	entry.pendingPrompt[requestID] = &pendingPromptEntry{PendingPromptSnapshot: snapshot}
	entry.pendingMu.Unlock()
	entry.publishPendingPrompt(id, snapshot, clientui.PendingPromptEventPending)
}

func (r *RuntimeRegistry) CompletePendingPrompt(sessionID string, requestID string) {
	if r == nil {
		return
	}
	id := strings.TrimSpace(sessionID)
	trimmedRequestID := strings.TrimSpace(requestID)
	if id == "" || trimmedRequestID == "" {
		return
	}
	r.mu.RLock()
	entry := r.engines[id]
	r.mu.RUnlock()
	if entry == nil {
		return
	}
	var snapshot PendingPromptSnapshot
	entry.pendingMu.Lock()
	if pending, ok := entry.pendingPrompt[trimmedRequestID]; ok {
		pending.closed = true
		snapshot = pending.PendingPromptSnapshot
	}
	delete(entry.pendingPrompt, trimmedRequestID)
	entry.pendingMu.Unlock()
	if snapshot.Request.ID != "" {
		entry.publishPendingPrompt(id, snapshot, clientui.PendingPromptEventResolved)
	}
}

func (r *RuntimeRegistry) ListPendingPrompts(sessionID string) []PendingPromptSnapshot {
	if r == nil {
		return nil
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return nil
	}
	r.mu.RLock()
	entry := r.engines[id]
	r.mu.RUnlock()
	if entry == nil {
		return nil
	}
	entry.pendingMu.RLock()
	items := make([]PendingPromptSnapshot, 0, len(entry.pendingPrompt))
	for _, item := range entry.pendingPrompt {
		if item == nil {
			continue
		}
		items = append(items, item.PendingPromptSnapshot)
	}
	entry.pendingMu.RUnlock()
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].Request.ID < items[j].Request.ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items
}

func (r *RuntimeRegistry) AwaitPromptResponse(ctx context.Context, sessionID string, req askquestion.Request) (askquestion.Response, error) {
	if r == nil {
		return askquestion.Response{}, fmt.Errorf("runtime registry is required")
	}
	id := strings.TrimSpace(sessionID)
	requestID := strings.TrimSpace(req.ID)
	if id == "" || requestID == "" {
		return askquestion.Response{}, fmt.Errorf("session id and request id are required")
	}
	r.mu.RLock()
	entry := r.engines[id]
	r.mu.RUnlock()
	if entry == nil {
		return askquestion.Response{}, fmt.Errorf("runtime %q is unavailable", id)
	}
	pending := &pendingPromptEntry{
		PendingPromptSnapshot: PendingPromptSnapshot{Request: req, CreatedAt: time.Now()},
		response:              make(chan promptResponseResult, 1),
	}
	entry.pendingMu.Lock()
	if _, exists := entry.pendingPrompt[requestID]; exists {
		entry.pendingMu.Unlock()
		return askquestion.Response{}, fmt.Errorf("prompt %q is already pending", requestID)
	}
	entry.pendingPrompt[requestID] = pending
	entry.pendingMu.Unlock()
	entry.publishPendingPrompt(id, pending.PendingPromptSnapshot, clientui.PendingPromptEventPending)
	defer func() {
		var shouldPublishResolved bool
		entry.pendingMu.Lock()
		current, ok := entry.pendingPrompt[requestID]
		if ok && current == pending {
			shouldPublishResolved = !current.closed
			current.closed = true
			delete(entry.pendingPrompt, requestID)
		}
		entry.pendingMu.Unlock()
		if shouldPublishResolved {
			entry.publishPendingPrompt(id, pending.PendingPromptSnapshot, clientui.PendingPromptEventResolved)
		}
	}()
	select {
	case <-ctx.Done():
		return askquestion.Response{}, ctx.Err()
	case result := <-pending.response:
		return result.response, result.err
	}
}

func (r *RuntimeRegistry) SubmitPromptResponse(sessionID string, resp askquestion.Response, err error) error {
	if r == nil {
		return fmt.Errorf("runtime registry is required")
	}
	id := strings.TrimSpace(sessionID)
	requestID := strings.TrimSpace(resp.RequestID)
	if id == "" || requestID == "" {
		return fmt.Errorf("session id and request id are required")
	}
	r.mu.RLock()
	entry := r.engines[id]
	r.mu.RUnlock()
	if entry == nil {
		return fmt.Errorf("runtime %q is unavailable", id)
	}
	entry.pendingMu.Lock()
	pending := entry.pendingPrompt[requestID]
	if pending == nil {
		entry.pendingMu.Unlock()
		return fmt.Errorf("prompt %q not found: %w", requestID, serverapi.ErrPromptNotFound)
	}
	if pending.closed {
		entry.pendingMu.Unlock()
		return fmt.Errorf("prompt %q is already resolved: %w", requestID, serverapi.ErrPromptAlreadyResolved)
	}
	if pending.response == nil {
		entry.pendingMu.Unlock()
		return fmt.Errorf("prompt %q cannot be answered through the shared boundary: %w", requestID, serverapi.ErrPromptUnsupported)
	}
	pending.closed = true
	snapshot := pending.PendingPromptSnapshot
	ch := pending.response
	delete(entry.pendingPrompt, requestID)
	entry.pendingMu.Unlock()
	ch <- promptResponseResult{response: resp, err: err}
	entry.publishPendingPrompt(id, snapshot, clientui.PendingPromptEventResolved)
	return nil
}

func (r *RuntimeRegistry) AcquirePrimaryRun(sessionID string) (primaryrun.Lease, error) {
	if r == nil {
		return nil, primaryrun.ErrActivePrimaryRun
	}
	id := strings.TrimSpace(sessionID)
	if id == "" {
		return nil, primaryrun.ErrActivePrimaryRun
	}
	r.mu.Lock()
	if _, busy := r.primaryRun[id]; busy {
		r.mu.Unlock()
		return nil, primaryrun.ErrActivePrimaryRun
	}
	r.nextLease++
	leaseID := r.nextLease
	r.primaryRun[id] = leaseID
	r.mu.Unlock()
	return primaryrun.LeaseFunc(func() {
		r.mu.Lock()
		defer r.mu.Unlock()
		if current, ok := r.primaryRun[id]; ok && current == leaseID {
			delete(r.primaryRun, id)
		}
	}), nil
}

func newSessionActivityHub() *sessionActivityHub {
	return &sessionActivityHub{subscribers: make(map[uint64]*sessionActivitySubscription)}
}

func newPromptActivityHub() *promptActivityHub {
	return &promptActivityHub{subscribers: make(map[uint64]*promptActivitySubscription)}
}

func (e *runtimeEntry) closePendingPrompts(err error) {
	if e == nil {
		return
	}
	e.pendingMu.Lock()
	items := make([]*pendingPromptEntry, 0, len(e.pendingPrompt))
	for id, pending := range e.pendingPrompt {
		if pending == nil {
			delete(e.pendingPrompt, id)
			continue
		}
		pending.closed = true
		items = append(items, pending)
		delete(e.pendingPrompt, id)
	}
	e.pendingMu.Unlock()
	for _, pending := range items {
		if pending.response == nil {
			continue
		}
		select {
		case pending.response <- promptResponseResult{err: err}:
		default:
		}
	}
}

func (e *runtimeEntry) listPendingPrompts() []PendingPromptSnapshot {
	if e == nil {
		return nil
	}
	e.pendingMu.RLock()
	items := e.listPendingPromptsLocked()
	e.pendingMu.RUnlock()
	return items
}

func (e *runtimeEntry) listPendingPromptsLocked() []PendingPromptSnapshot {
	if e == nil {
		return nil
	}
	items := make([]PendingPromptSnapshot, 0, len(e.pendingPrompt))
	for _, item := range e.pendingPrompt {
		if item == nil {
			continue
		}
		items = append(items, item.PendingPromptSnapshot)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].CreatedAt.Equal(items[j].CreatedAt) {
			return items[i].Request.ID < items[j].Request.ID
		}
		return items[i].CreatedAt.Before(items[j].CreatedAt)
	})
	return items
}

func (e *runtimeEntry) publishPendingPrompt(sessionID string, snapshot PendingPromptSnapshot, eventType clientui.PendingPromptEventType) {
	if e == nil || e.promptHub == nil || snapshot.Request.ID == "" {
		return
	}
	e.promptHub.publish(pendingPromptEventFromSnapshot(sessionID, snapshot, eventType))
}

func (h *sessionActivityHub) subscribe(afterSequence uint64) (*sessionActivitySubscription, error) {
	if h == nil {
		return nil, fmt.Errorf("session activity stream is unavailable: %w", serverapi.ErrSessionActivityUnavailable)
	}
	sub := &sessionActivitySubscription{ch: make(chan clientui.Event, sessionActivityBufferSize)}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		sub.closeWithError(io.EOF)
		return sub, nil
	}
	if afterSequence > 0 && !h.canReplayLocked(afterSequence) {
		h.mu.Unlock()
		return nil, fmt.Errorf("session activity cursor %d is outside retained range: %w", afterSequence, serverapi.ErrStreamGap)
	}
	id := h.nextID
	h.nextID++
	replay := h.replayAfterLocked(afterSequence)
	for _, evt := range replay {
		if !sub.publish(evt) {
			h.mu.Unlock()
			sub.closeWithError(serverapi.ErrStreamGap)
			return sub, nil
		}
	}
	sub.onClose = func() {
		h.mu.Lock()
		delete(h.subscribers, id)
		h.mu.Unlock()
	}
	h.subscribers[id] = sub
	h.mu.Unlock()
	return sub, nil
}

func (h *sessionActivityHub) publish(evt clientui.Event) {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.nextSeq++
	evt.Sequence = h.nextSeq
	h.history = append(h.history, evt)
	if len(h.history) > sessionActivityBufferSize {
		copy(h.history, h.history[len(h.history)-sessionActivityBufferSize:])
		h.history = h.history[:sessionActivityBufferSize]
	}
	subs := make([]*sessionActivitySubscription, 0, len(h.subscribers))
	for _, sub := range h.subscribers {
		subs = append(subs, sub)
	}
	h.mu.Unlock()
	for _, sub := range subs {
		if !sub.publish(evt) {
			sub.closeWithError(serverapi.ErrStreamGap)
		}
	}
}

func (h *sessionActivityHub) canReplayLocked(afterSequence uint64) bool {
	if afterSequence == 0 || afterSequence == h.nextSeq {
		return true
	}
	if afterSequence > h.nextSeq {
		return false
	}
	if len(h.history) == 0 {
		return false
	}
	return afterSequence >= h.history[0].Sequence-1
}

func (h *sessionActivityHub) replayAfterLocked(afterSequence uint64) []clientui.Event {
	if afterSequence == 0 || len(h.history) == 0 {
		return nil
	}
	replay := make([]clientui.Event, 0)
	for _, evt := range h.history {
		if evt.Sequence > afterSequence {
			replay = append(replay, evt)
		}
	}
	return replay
}

func (h *sessionActivityHub) close(err error) {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	subs := make([]*sessionActivitySubscription, 0, len(h.subscribers))
	for id, sub := range h.subscribers {
		subs = append(subs, sub)
		delete(h.subscribers, id)
	}
	h.mu.Unlock()
	for _, sub := range subs {
		sub.closeWithError(err)
	}
}

func (s *sessionActivitySubscription) publish(evt clientui.Event) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return false
	}
	select {
	case s.ch <- evt:
		return true
	default:
		return false
	}
}

func (s *sessionActivitySubscription) Next(ctx context.Context) (clientui.Event, error) {
	if s == nil {
		return clientui.Event{}, io.EOF
	}
	select {
	case <-ctx.Done():
		return clientui.Event{}, ctx.Err()
	case evt, ok := <-s.ch:
		if ok {
			return evt, nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.err != nil {
			return clientui.Event{}, serverapi.NormalizeStreamError(s.err)
		}
		return clientui.Event{}, io.EOF
	}
}

func (s *sessionActivitySubscription) Close() error {
	if s == nil {
		return nil
	}
	s.closeWithError(io.EOF)
	return nil
}

func (s *sessionActivitySubscription) closeWithError(err error) {
	if s == nil {
		return
	}
	var onClose func()
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.done = true
	s.err = err
	close(s.ch)
	onClose = s.onClose
	s.mu.Unlock()
	if onClose != nil {
		onClose()
	}
}

func (h *promptActivityHub) subscribe(initial []clientui.PendingPromptEvent, afterSequence uint64) (*promptActivitySubscription, error) {
	if h == nil {
		return nil, fmt.Errorf("prompt activity stream is unavailable: %w", serverapi.ErrStreamUnavailable)
	}
	sub := &promptActivitySubscription{ch: make(chan clientui.PendingPromptEvent, promptActivityBufferSize), initial: append([]clientui.PendingPromptEvent(nil), initial...)}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		sub.closeWithError(io.EOF)
		return sub, nil
	}
	if afterSequence > 0 && !h.canReplayLocked(afterSequence) {
		h.mu.Unlock()
		return nil, fmt.Errorf("prompt activity cursor %d is outside retained range: %w", afterSequence, serverapi.ErrStreamGap)
	}
	if afterSequence > 0 {
		for _, evt := range h.replayAfterLocked(afterSequence) {
			if !sub.publish(evt) {
				h.mu.Unlock()
				sub.closeWithError(serverapi.ErrStreamGap)
				return sub, nil
			}
		}
	}
	id := h.nextID
	h.nextID++
	h.subscribers[id] = sub
	h.mu.Unlock()
	sub.onClose = func() {
		h.mu.Lock()
		delete(h.subscribers, id)
		h.mu.Unlock()
	}
	return sub, nil
}

func (h *promptActivityHub) publish(evt clientui.PendingPromptEvent) {
	if h == nil || evt.IsZero() {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.nextSeq++
	evt.Sequence = h.nextSeq
	h.history = append(h.history, evt)
	if len(h.history) > promptActivityBufferSize {
		copy(h.history, h.history[len(h.history)-promptActivityBufferSize:])
		h.history = h.history[:promptActivityBufferSize]
	}
	subs := make([]*promptActivitySubscription, 0, len(h.subscribers))
	for _, sub := range h.subscribers {
		subs = append(subs, sub)
	}
	h.mu.Unlock()
	for _, sub := range subs {
		if !sub.publish(evt) {
			sub.closeWithError(serverapi.ErrStreamGap)
		}
	}
}

func (h *promptActivityHub) canReplayLocked(afterSequence uint64) bool {
	if afterSequence == 0 || afterSequence == h.nextSeq {
		return true
	}
	if afterSequence > h.nextSeq {
		return false
	}
	if len(h.history) == 0 {
		return false
	}
	return afterSequence >= h.history[0].Sequence-1
}

func (h *promptActivityHub) replayAfterLocked(afterSequence uint64) []clientui.PendingPromptEvent {
	if afterSequence == 0 || len(h.history) == 0 {
		return nil
	}
	replay := make([]clientui.PendingPromptEvent, 0)
	for _, evt := range h.history {
		if evt.Sequence > afterSequence {
			replay = append(replay, evt)
		}
	}
	return replay
}

func (h *promptActivityHub) close(err error) {
	if h == nil {
		return
	}
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.closed = true
	subs := make([]*promptActivitySubscription, 0, len(h.subscribers))
	for id, sub := range h.subscribers {
		subs = append(subs, sub)
		delete(h.subscribers, id)
	}
	h.mu.Unlock()
	for _, sub := range subs {
		sub.closeWithError(err)
	}
}

func (s *promptActivitySubscription) publish(evt clientui.PendingPromptEvent) bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.done {
		return false
	}
	select {
	case s.ch <- evt:
		return true
	default:
		return false
	}
}

func (s *promptActivitySubscription) Next(ctx context.Context) (clientui.PendingPromptEvent, error) {
	if s == nil {
		return clientui.PendingPromptEvent{}, io.EOF
	}
	s.mu.Lock()
	if len(s.initial) > 0 {
		evt := s.initial[0]
		s.initial = s.initial[1:]
		s.mu.Unlock()
		return evt, nil
	}
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return clientui.PendingPromptEvent{}, ctx.Err()
	case evt, ok := <-s.ch:
		if ok {
			return evt, nil
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.err != nil {
			return clientui.PendingPromptEvent{}, serverapi.NormalizeStreamError(s.err)
		}
		return clientui.PendingPromptEvent{}, io.EOF
	}
}

func (s *promptActivitySubscription) Close() error {
	if s == nil {
		return nil
	}
	s.closeWithError(io.EOF)
	return nil
}

func (s *promptActivitySubscription) closeWithError(err error) {
	if s == nil {
		return
	}
	var onClose func()
	s.mu.Lock()
	if s.done {
		s.mu.Unlock()
		return
	}
	s.done = true
	s.err = err
	close(s.ch)
	onClose = s.onClose
	s.mu.Unlock()
	if onClose != nil {
		onClose()
	}
}

func pendingPromptEventFromSnapshot(sessionID string, snapshot PendingPromptSnapshot, eventType clientui.PendingPromptEventType) clientui.PendingPromptEvent {
	evt := clientui.PendingPromptEvent{
		Type:                   eventType,
		PromptID:               snapshot.Request.ID,
		SessionID:              sessionID,
		Question:               snapshot.Request.Question,
		Suggestions:            append([]string(nil), snapshot.Request.Suggestions...),
		RecommendedOptionIndex: snapshot.Request.RecommendedOptionIndex,
		Approval:               snapshot.Request.Approval,
		CreatedAt:              snapshot.CreatedAt,
	}
	if len(snapshot.Request.ApprovalOptions) > 0 {
		evt.ApprovalOptions = make([]clientui.ApprovalOption, 0, len(snapshot.Request.ApprovalOptions))
		for _, option := range snapshot.Request.ApprovalOptions {
			evt.ApprovalOptions = append(evt.ApprovalOptions, clientui.ApprovalOption{Decision: clientui.ApprovalDecision(option.Decision), Label: option.Label})
		}
	}
	return evt
}

var _ serverapi.SessionActivitySubscription = (*sessionActivitySubscription)(nil)
var _ serverapi.PromptActivitySubscription = (*promptActivitySubscription)(nil)
