package operation

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

var ErrOperationOwnerClosed = errors.New("operation owner is closed")

type OperationOwner struct {
	mu         sync.Mutex
	closing    bool
	nextID     uint64
	operations map[uint64]context.CancelCauseFunc
	wait       sync.WaitGroup
}

type OperationScope struct {
	ctx context.Context
}

func (s OperationScope) Context() context.Context {
	return s.ctx
}

type Operation struct {
	done chan error
}

func NewOperationOwner() *OperationOwner {
	return &OperationOwner{
		operations: make(map[uint64]context.CancelCauseFunc),
	}
}

// Run starts server-owned work and waits only as long as the caller observes it.
func Run[T any](callerCtx context.Context, owner *OperationOwner, run func(OperationScope) (T, error)) (T, error) {
	var result T
	operation, err := owner.Start(callerCtx, func(scope OperationScope) error {
		var runErr error
		result, runErr = run(scope)
		return runErr
	})
	if err != nil {
		return result, err
	}
	if err := operation.Await(callerCtx); err != nil {
		var empty T
		return empty, err
	}
	return result, nil
}

func (o *OperationOwner) Start(
	callerCtx context.Context,
	run func(OperationScope) error,
) (*Operation, error) {
	if o == nil {
		return nil, errors.New("operation owner is required")
	}
	if run == nil {
		return nil, errors.New("operation is required")
	}
	if callerCtx == nil {
		callerCtx = context.Background()
	}
	ctx, cancel := context.WithCancelCause(context.WithoutCancel(callerCtx))
	operation := &Operation{done: make(chan error, 1)}

	o.mu.Lock()
	if o.closing {
		o.mu.Unlock()
		cancel(ErrOperationOwnerClosed)
		return nil, ErrOperationOwnerClosed
	}
	o.nextID++
	id := o.nextID
	o.operations[id] = cancel
	o.wait.Add(1)
	o.mu.Unlock()

	go func() {
		defer func() {
			cancel(nil)
			o.mu.Lock()
			delete(o.operations, id)
			o.mu.Unlock()
			o.wait.Done()
		}()
		err := run(OperationScope{ctx: ctx})
		// errors.Is would also suppress joined errors with independent failures.
		if err != nil && err != context.Cause(ctx) && err != ctx.Err() {
			slog.ErrorContext(ctx, "server operation failed", "error", err)
		}
		operation.done <- err
		close(operation.done)
	}()
	return operation, nil
}

func (o *Operation) Await(ctx context.Context) error {
	if o == nil {
		return errors.New("operation is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	select {
	case err := <-o.done:
		return err
	case <-ctx.Done():
		return context.Cause(ctx)
	}
}

func (o *OperationOwner) Close() error {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	if !o.closing {
		o.closing = true
		for _, cancel := range o.operations {
			cancel(ErrOperationOwnerClosed)
		}
	}
	o.mu.Unlock()
	o.wait.Wait()
	return nil
}
