package runtime

type goalLoopLifecycleState string

const (
	goalLoopLifecycleIdle           goalLoopLifecycleState = "idle"
	goalLoopLifecycleRunning        goalLoopLifecycleState = "running"
	goalLoopLifecycleSuspending     goalLoopLifecycleState = "suspending"
	goalLoopLifecycleRestartPending goalLoopLifecycleState = "restart_pending"
	goalLoopLifecycleSuspended      goalLoopLifecycleState = "suspended"
)

func (s goalLoopLifecycleState) IsRunning() bool {
	return s == goalLoopLifecycleRunning || s == goalLoopLifecycleSuspending || s == goalLoopLifecycleRestartPending
}

func (s goalLoopLifecycleState) IsSuspended() bool {
	return s == goalLoopLifecycleSuspended || s == goalLoopLifecycleSuspending
}
