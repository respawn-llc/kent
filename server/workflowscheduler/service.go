package workflowscheduler

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"builder/server/workflow"
	"builder/server/workflowstore"
)

const (
	ReasonRuntimeStartFailed    = "workflow_runtime_start_failed"
	ReasonPendingAskUnavailable = "workflow_pending_ask_unavailable"
	ReasonStartupOrphanedRun    = "workflow_startup_orphaned_run"
)

var (
	ErrStopped            = errors.New("workflow scheduler stopped")
	ErrRuntimeStartFailed = errors.New("workflow runtime start failed")
)

type Store interface {
	ListRunnableRuns(ctx context.Context, limit int64) ([]workflowstore.RunnableRunRecord, error)
	ClaimRun(ctx context.Context, runID workflow.RunID, expectedGeneration int64) (workflowstore.RunnableRunRecord, error)
	InterruptRun(ctx context.Context, runID workflow.RunID, reason string, detailJSON string) error
	InterruptRunGeneration(ctx context.Context, runID workflow.RunID, generation int64, reason string, detailJSON string) error
	ReconcileStartedRuns(ctx context.Context, reason string) (int64, error)
	ListWaitingAskRuns(ctx context.Context) ([]workflowstore.RunRecord, error)
}

type RuntimeStarter interface {
	StartWorkflowRun(ctx context.Context, req StartRunRequest) error
}

type PendingAskResolver interface {
	CanRehydrate(ctx context.Context, sessionID string, runID workflow.RunID, askID string) (bool, error)
}

type Logger interface {
	Logf(format string, args ...any)
}

type StartRunRequest struct {
	RunID       workflow.RunID
	TaskID      workflow.TaskID
	PlacementID workflow.PlacementID
	NodeID      workflow.NodeID
	Generation  int64
}

type Config struct {
	Concurrency int
}

type Service struct {
	store              Store
	starter            RuntimeStarter
	pendingAskResolver PendingAskResolver
	concurrency        int
	claimRetries       int
	claimBackoff       time.Duration
	processInterval    time.Duration
	logger             Logger

	mu         sync.Mutex
	active     map[workflow.RunID]StartRunRequest
	stopped    bool
	started    bool
	loopCancel context.CancelFunc
	loopWG     sync.WaitGroup
	wake       chan struct{}
}

const (
	defaultClaimRetries    = 3
	defaultClaimBackoff    = 10 * time.Millisecond
	defaultProcessInterval = 5000 * time.Millisecond
	defaultWakeBuffer      = 1
)

func New(store Store, starter RuntimeStarter, cfg Config, opts ...Option) (*Service, error) {
	if store == nil {
		return nil, errors.New("workflow scheduler store is required")
	}
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	service := &Service{store: store, starter: starter, concurrency: concurrency, claimRetries: defaultClaimRetries, claimBackoff: defaultClaimBackoff, processInterval: defaultProcessInterval, active: map[workflow.RunID]StartRunRequest{}, wake: make(chan struct{}, defaultWakeBuffer)}
	for _, opt := range opts {
		opt(service)
	}
	return service, nil
}

type Option func(*Service)

func WithPendingAskResolver(resolver PendingAskResolver) Option {
	return func(s *Service) {
		s.pendingAskResolver = resolver
	}
}

func WithProcessInterval(interval time.Duration) Option {
	return func(s *Service) {
		if interval > 0 {
			s.processInterval = interval
		}
	}
}

func (s *Service) Start(ctx context.Context) error {
	if s == nil {
		return errors.New("workflow scheduler is required")
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return ErrStopped
	}
	if s.started {
		s.mu.Unlock()
		return s.Process(ctx)
	}
	s.mu.Unlock()
	if err := s.Reconcile(ctx); err != nil {
		return err
	}
	if err := s.Process(ctx); err != nil {
		if errors.Is(err, ErrRuntimeStartFailed) {
			s.logf("workflow.scheduler.startup_process_error error=%q", err.Error())
		} else {
			return err
		}
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return ErrStopped
	}
	if s.started {
		s.mu.Unlock()
		return nil
	}
	loopCtx, cancel := context.WithCancel(context.Background())
	s.loopCancel = cancel
	s.loopWG.Add(1)
	s.started = true
	s.mu.Unlock()
	go s.runLoop(loopCtx)
	return nil
}

func (s *Service) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	s.stopped = true
	cancel := s.loopCancel
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	s.loopWG.Wait()
	return nil
}

func (s *Service) Started() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.started
}

func (s *Service) Stopped() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stopped
}

func (s *Service) ActiveCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.active)
}

func (s *Service) RuntimeFinished(runID workflow.RunID, generation int64) {
	s.mu.Lock()
	current, ok := s.active[runID]
	if ok && current.Generation == generation {
		delete(s.active, runID)
	}
	s.mu.Unlock()
	s.Notify()
}

func (s *Service) Notify() {
	if s == nil {
		return
	}
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) runLoop(ctx context.Context) {
	defer s.loopWG.Done()
	ticker := time.NewTicker(s.processInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-ticker.C:
		}
		if err := s.Process(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, ErrStopped) {
			s.logf("workflow.scheduler.process_error error=%q", err.Error())
		}
	}
}

func (s *Service) Reconcile(ctx context.Context) error {
	if s == nil {
		return errors.New("workflow scheduler is required")
	}
	waiting, err := s.store.ListWaitingAskRuns(ctx)
	if err != nil {
		return err
	}
	for _, run := range waiting {
		canRehydrate := false
		if s.pendingAskResolver != nil {
			canRehydrate, err = s.pendingAskResolver.CanRehydrate(ctx, run.SessionID, run.ID, run.WaitingAskID)
			if err != nil {
				return err
			}
		}
		if !canRehydrate {
			s.logf("workflow.scheduler.recovery run_id=%s action=interrupt reason=%s", run.ID, ReasonPendingAskUnavailable)
			if err := s.store.InterruptRun(ctx, run.ID, ReasonPendingAskUnavailable, "{}"); err != nil {
				return err
			}
		} else {
			s.logf("workflow.scheduler.recovery run_id=%s action=preserve_waiting_ask ask_id=%s", run.ID, run.WaitingAskID)
		}
	}
	s.logf("workflow.scheduler.recovery action=interrupt_orphaned_started reason=%s", ReasonStartupOrphanedRun)
	_, err = s.store.ReconcileStartedRuns(ctx, ReasonStartupOrphanedRun)
	return err
}

func (s *Service) Process(ctx context.Context) error {
	if s == nil {
		return errors.New("workflow scheduler is required")
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return ErrStopped
	}
	if s.starter == nil {
		s.mu.Unlock()
		return nil
	}
	capacity := s.concurrency - len(s.active)
	s.mu.Unlock()
	if capacity <= 0 {
		return nil
	}
	candidates, err := s.store.ListRunnableRuns(ctx, int64(capacity))
	if err != nil {
		return err
	}
	for _, candidate := range candidates {
		s.mu.Lock()
		if s.stopped {
			s.mu.Unlock()
			return ErrStopped
		}
		if len(s.active) >= s.concurrency {
			s.mu.Unlock()
			return nil
		}
		if _, ok := s.active[candidate.ID]; ok {
			s.mu.Unlock()
			continue
		}
		reserved := StartRunRequest{RunID: candidate.ID, TaskID: candidate.TaskID, PlacementID: candidate.PlacementID, NodeID: candidate.NodeID, Generation: candidate.Generation}
		s.active[candidate.ID] = reserved
		s.mu.Unlock()

		claimed, err := s.claimRunWithRetry(ctx, candidate)
		if err != nil {
			s.RuntimeFinished(candidate.ID, candidate.Generation)
			if errors.Is(err, sql.ErrNoRows) {
				continue
			}
			return err
		}
		req := StartRunRequest{RunID: claimed.ID, TaskID: claimed.TaskID, PlacementID: claimed.PlacementID, NodeID: claimed.NodeID, Generation: claimed.Generation}
		s.logf("workflow.scheduler.selection run_id=%s task_id=%s generation=%d action=start", req.RunID, req.TaskID, req.Generation)
		s.mu.Lock()
		s.active[claimed.ID] = req
		s.mu.Unlock()
		if err := s.starter.StartWorkflowRun(ctx, req); err != nil {
			s.RuntimeFinished(claimed.ID, claimed.Generation)
			s.logf("workflow.scheduler.runtime_start run_id=%s action=interrupt reason=%s", claimed.ID, ReasonRuntimeStartFailed)
			if interruptErr := s.store.InterruptRunGeneration(context.WithoutCancel(ctx), claimed.ID, claimed.Generation, ReasonRuntimeStartFailed, fmt.Sprintf(`{"error":%q}`, err.Error())); interruptErr != nil {
				return errors.Join(fmt.Errorf("%w: %w", ErrRuntimeStartFailed, err), interruptErr)
			}
			return fmt.Errorf("%w: %w", ErrRuntimeStartFailed, err)
		}
	}
	return nil
}

func (s *Service) claimRunWithRetry(ctx context.Context, candidate workflowstore.RunnableRunRecord) (workflowstore.RunnableRunRecord, error) {
	var lastErr error
	for attempt := 0; attempt <= s.claimRetries; attempt++ {
		claimed, err := s.store.ClaimRun(ctx, candidate.ID, candidate.Generation)
		if err == nil {
			return claimed, nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			return workflowstore.RunnableRunRecord{}, err
		}
		lastErr = err
		s.logf("workflow.scheduler.claim_retry run_id=%s attempt=%d error=%q", candidate.ID, attempt+1, err.Error())
		if s.claimBackoff > 0 && attempt < s.claimRetries {
			timer := time.NewTimer(s.claimBackoff)
			select {
			case <-ctx.Done():
				timer.Stop()
				return workflowstore.RunnableRunRecord{}, ctx.Err()
			case <-timer.C:
			}
		}
	}
	return workflowstore.RunnableRunRecord{}, lastErr
}

func (s *Service) logf(format string, args ...any) {
	if s.logger != nil {
		s.logger.Logf(format, args...)
	}
}
