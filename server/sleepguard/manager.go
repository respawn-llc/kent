package sleepguard

import (
	"log"
	"sync/atomic"
	"time"

	"builder/shared/config"
)

const (
	defaultActiveIdleGrace    = time.Minute
	defaultActiveAcquireRetry = 5 * time.Second
)

type sleepInhibitor interface {
	Acquire() error
	Release()
	SetErrorHandler(func(error))
}

type sleepController interface {
	Close()
}

type releaseTimer interface {
	Stop() bool
}

type releaseTimerFactory func(time.Duration, func()) releaseTimer

type managerConfig struct {
	guard        sleepInhibitor
	idleGrace    time.Duration
	timerFactory releaseTimerFactory
	retryDelay   time.Duration
}

type managerOption func(*managerConfig)

// Manager owns sleep prevention mode composition.
type Manager struct {
	controller      sleepController
	runtimeObserver func(bool)
	guard           sleepInhibitor
}

// NewManager creates a Manager. onError is called whenever guard acquisition fails.
// Returns a non-nil error when mode is "always" and the guard cannot be acquired at
// startup - the manager is still returned and usable.
func NewManager(mode config.SleepPreventionMode, onError func(err error), opts ...managerOption) (*Manager, error) {
	cfg := managerConfig{
		idleGrace:    defaultActiveIdleGrace,
		retryDelay:   defaultActiveAcquireRetry,
		timerFactory: defaultReleaseTimerFactory,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.idleGrace <= 0 {
		cfg.idleGrace = defaultActiveIdleGrace
	}
	if cfg.timerFactory == nil {
		cfg.timerFactory = defaultReleaseTimerFactory
	}
	if cfg.retryDelay <= 0 {
		cfg.retryDelay = defaultActiveAcquireRetry
	}

	m := &Manager{}
	if mode == config.SleepPreventionModeNever {
		m.controller = noopController{}
		return m, nil
	}

	guard := cfg.guard
	if guard == nil {
		guard = &Guard{}
	}
	m.guard = guard

	switch mode {
	case config.SleepPreventionModeAlways:
		guard.SetErrorHandler(func(err error) {
			log.Printf("sleepguard: inhibitor restart failed: %v", err)
			if onError != nil {
				onError(err)
			}
		})
		m.controller = &alwaysController{guard: guard}
		if err := guard.Acquire(); err != nil {
			log.Printf("sleepguard: always-mode acquire failed: %v", err)
			if onError != nil {
				onError(err)
			}
			return m, err
		}
	case config.SleepPreventionModeActive:
		active := newActiveController(guard, cfg.idleGrace, cfg.retryDelay, cfg.timerFactory, onError)
		guard.SetErrorHandler(func(err error) {
			log.Printf("sleepguard: inhibitor restart failed: %v", err)
			if onError != nil {
				onError(err)
			}
			active.OnInhibitorFailed()
		})
		m.controller = active
		m.runtimeObserver = active.OnActiveStateChanged
	default:
		m.controller = noopController{}
	}
	return m, nil
}

func withGuard(guard sleepInhibitor) managerOption {
	return func(cfg *managerConfig) {
		cfg.guard = guard
	}
}

func withIdleGrace(duration time.Duration) managerOption {
	return func(cfg *managerConfig) {
		cfg.idleGrace = duration
	}
}

func withReleaseTimerFactory(factory releaseTimerFactory) managerOption {
	return func(cfg *managerConfig) {
		cfg.timerFactory = factory
	}
}

func withAcquireRetryDelay(duration time.Duration) managerOption {
	return func(cfg *managerConfig) {
		cfg.retryDelay = duration
	}
}

func (m *Manager) RuntimeActiveObserver() func(bool) {
	if m == nil {
		return nil
	}
	return m.runtimeObserver
}

func (m *Manager) Close() {
	if m == nil || m.controller == nil {
		return
	}
	m.controller.Close()
}

type noopController struct{}

func (noopController) Close() {}

type alwaysController struct {
	guard  sleepInhibitor
	closed atomic.Bool
}

func (c *alwaysController) Close() {
	if c != nil && c.guard != nil && c.closed.CompareAndSwap(false, true) {
		c.guard.Release()
	}
}

type activeCommandKind int

const (
	activeCommandStateChanged activeCommandKind = iota
	activeCommandTimerFired
	activeCommandRetryAcquire
	activeCommandInhibitorFailed
	activeCommandClose
)

type activeCommand struct {
	kind   activeCommandKind
	active bool
	epoch  uint64
}

type activeController struct {
	guard        sleepInhibitor
	idleGrace    time.Duration
	retryDelay   time.Duration
	timerFactory releaseTimerFactory
	onError      func(error)

	commands chan activeCommand
	done     chan struct{}
	closed   atomic.Bool
}

func newActiveController(guard sleepInhibitor, idleGrace time.Duration, retryDelay time.Duration, timerFactory releaseTimerFactory, onError func(error)) *activeController {
	controller := &activeController{
		guard:        guard,
		idleGrace:    idleGrace,
		retryDelay:   retryDelay,
		timerFactory: timerFactory,
		onError:      onError,
		commands:     make(chan activeCommand, 32),
		done:         make(chan struct{}),
	}
	go controller.run()
	return controller
}

func (c *activeController) OnActiveStateChanged(active bool) {
	if c == nil || c.closed.Load() {
		return
	}
	select {
	case c.commands <- activeCommand{kind: activeCommandStateChanged, active: active}:
	case <-c.done:
	}
}

func (c *activeController) OnInhibitorFailed() {
	if c == nil || c.closed.Load() {
		return
	}
	select {
	case c.commands <- activeCommand{kind: activeCommandInhibitorFailed}:
	case <-c.done:
	}
}

func (c *activeController) Close() {
	if c == nil || !c.closed.CompareAndSwap(false, true) {
		return
	}
	select {
	case c.commands <- activeCommand{kind: activeCommandClose}:
	case <-c.done:
		return
	}
	<-c.done
}

func (c *activeController) run() {
	defer close(c.done)
	var held bool
	var active bool
	var epoch uint64
	var idleTimer releaseTimer
	var retryTimer releaseTimer
	cancelRetry := func() {
		if retryTimer != nil {
			retryTimer.Stop()
			retryTimer = nil
		}
	}
	cancelRelease := func() {
		if idleTimer != nil {
			idleTimer.Stop()
			idleTimer = nil
		}
	}
	scheduleRetry := func() {
		if retryTimer != nil {
			return
		}
		epoch++
		timerEpoch := epoch
		retryTimer = c.timerFactory(c.retryDelay, func() {
			c.enqueueTimer(activeCommandRetryAcquire, timerEpoch)
		})
	}
	tryAcquire := func() {
		if held || !active {
			return
		}
		if err := c.guard.Acquire(); err != nil {
			log.Printf("sleepguard: active-mode acquire failed: %v", err)
			if c.onError != nil {
				c.onError(err)
			}
			scheduleRetry()
			return
		}
		cancelRetry()
		held = true
	}
	for cmd := range c.commands {
		switch cmd.kind {
		case activeCommandStateChanged:
			if cmd.active {
				active = true
				epoch++
				cancelRelease()
				tryAcquire()
				continue
			}
			active = false
			cancelRetry()
			if held && idleTimer == nil {
				epoch++
				timerEpoch := epoch
				idleTimer = c.timerFactory(c.idleGrace, func() {
					c.enqueueTimer(activeCommandTimerFired, timerEpoch)
				})
			}
		case activeCommandTimerFired:
			if cmd.epoch != epoch || active || !held {
				continue
			}
			idleTimer = nil
			held = false
			c.guard.Release()
		case activeCommandRetryAcquire:
			if cmd.epoch != epoch || !active || held {
				continue
			}
			retryTimer = nil
			tryAcquire()
		case activeCommandInhibitorFailed:
			if held {
				held = false
			}
			tryAcquire()
		case activeCommandClose:
			cancelRelease()
			cancelRetry()
			if held {
				c.guard.Release()
			}
			return
		}
	}
}

func (c *activeController) enqueueTimer(kind activeCommandKind, epoch uint64) {
	if c.closed.Load() {
		return
	}
	select {
	case c.commands <- activeCommand{kind: kind, epoch: epoch}:
	case <-c.done:
	}
}

func defaultReleaseTimerFactory(duration time.Duration, callback func()) releaseTimer {
	return time.AfterFunc(duration, callback)
}
