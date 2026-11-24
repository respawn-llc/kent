package runtime

type goalLoopLifecycleState uint8

const (
	goalLoopLifecycleIdle goalLoopLifecycleState = iota
	goalLoopLifecycleRunning
	goalLoopLifecycleStopping
	goalLoopLifecycleRestartPending
)

func (s goalLoopLifecycleState) IsRunning() bool {
	return s != goalLoopLifecycleIdle
}
