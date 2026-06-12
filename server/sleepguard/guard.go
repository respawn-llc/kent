package sleepguard

import (
	"sync"
	"time"
)

const minimumGuardRestartInterval = time.Second

type platformGuard interface {
	start() error
	stop()
	running() bool
	exited() <-chan struct{}
}

// Guard is a thread-safe, idempotent sleep inhibitor.
// Platform-specific logic lives in guard_<os>.go files via platformGuardImpl.
type Guard struct {
	mu         sync.Mutex
	active     bool
	generation uint64
	impl       platformGuard
	onError    func(error)
	startedAt  time.Time
}

func (g *Guard) Acquire() error {
	g.mu.Lock()
	impl := g.implLocked()
	if g.active && impl.running() {
		g.mu.Unlock()
		return nil
	}
	if g.active {
		impl.stop()
	}
	generation, exited, err := g.startLocked()
	g.mu.Unlock()
	if err != nil {
		return err
	}
	g.monitor(generation, exited)
	return nil
}

func (g *Guard) Release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.active {
		return
	}
	g.active = false
	if g.impl != nil {
		g.impl.stop()
	}
}

func (g *Guard) SetErrorHandler(onError func(error)) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.onError = onError
}

func (g *Guard) startLocked() (uint64, <-chan struct{}, error) {
	impl := g.implLocked()
	if err := impl.start(); err != nil {
		g.active = false
		return 0, nil, err
	}
	g.active = true
	g.generation++
	g.startedAt = time.Now()
	return g.generation, impl.exited(), nil
}

func (g *Guard) monitor(generation uint64, exited <-chan struct{}) {
	if exited == nil {
		return
	}
	go func() {
		<-exited
		g.mu.Lock()
		if !g.active || g.generation != generation {
			g.mu.Unlock()
			return
		}
		delay := g.restartDelayLocked(time.Now())
		g.mu.Unlock()
		if delay > 0 {
			time.Sleep(delay)
		}
		g.mu.Lock()
		if !g.active || g.generation != generation {
			g.mu.Unlock()
			return
		}
		g.implLocked().stop()
		nextGeneration, nextExited, err := g.startLocked()
		onError := g.onError
		g.mu.Unlock()
		if err != nil {
			if onError != nil {
				onError(err)
			}
			return
		}
		g.monitor(nextGeneration, nextExited)
	}()
}

func (g *Guard) implLocked() platformGuard {
	if g.impl == nil {
		g.impl = newPlatformGuardImpl()
	}
	return g.impl
}

func (g *Guard) restartDelayLocked(now time.Time) time.Duration {
	if g.startedAt.IsZero() {
		return 0
	}
	next := g.startedAt.Add(minimumGuardRestartInterval)
	if !now.Before(next) {
		return 0
	}
	return next.Sub(now)
}
