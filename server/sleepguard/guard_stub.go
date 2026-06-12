//go:build !darwin && !linux && !windows

package sleepguard

type platformGuardImpl struct {
	active bool
}

func newPlatformGuardImpl() platformGuard {
	return &platformGuardImpl{}
}

func (p *platformGuardImpl) start() error {
	p.active = true
	return nil
}

func (p *platformGuardImpl) stop() {
	p.active = false
}

func (p *platformGuardImpl) running() bool {
	return p.active
}

func (p *platformGuardImpl) exited() <-chan struct{} {
	return nil
}
