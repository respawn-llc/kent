package sleepguard

import "sync"

// Guard is a thread-safe, idempotent sleep inhibitor.
// Platform-specific logic lives in guard_<os>.go files via platformGuardImpl.
type Guard struct {
	mu     sync.Mutex
	active bool
	impl   platformGuardImpl
}

func (g *Guard) Acquire() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.active {
		return nil
	}
	if err := g.impl.start(); err != nil {
		return err
	}
	g.active = true
	return nil
}

func (g *Guard) Release() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if !g.active {
		return
	}
	g.active = false
	g.impl.stop()
}
