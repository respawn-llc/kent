package lifecyclehook

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"testing"

	"core/shared/lifecyclecontract"
)

const invokeHookProcessAddressEnv = "KENT_TEST_LIFECYCLE_HOOK_PROCESS_ADDRESS"

// A deadline signal supplied by the test makes expiry deterministic without
// waiting for a wall-clock deadline or changing the hook's production budget.
type hookDeadlineContext struct {
	context.Context
	done <-chan struct{}
}

func (c hookDeadlineContext) Done() <-chan struct{} { return c.done }

func (c hookDeadlineContext) Err() error {
	select {
	case <-c.done:
		return context.DeadlineExceeded
	default:
		return nil
	}
}

func TestInvokeHookReportsTypedTimeoutAfterTerminatingLiveProcess(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for blocking hook: %v", err)
	}
	t.Cleanup(func() {
		if err := listener.Close(); err != nil {
			t.Errorf("close hook listener: %v", err)
		}
	})
	t.Setenv(invokeHookProcessAddressEnv, listener.Addr().String())
	done := make(chan struct{})
	cancel := context.CancelFunc(func() { close(done) })
	parent := hookDeadlineContext{Context: context.Background(), done: done}
	issues := make(chan *Issue, 1)
	go func() {
		issues <- invokeHook(parent, []string{
			os.Args[0], "-test.run=^TestInvokeHookBlockingProcess$",
		}, lifecyclecontract.Event{Category: lifecyclecontract.CategorySessionStart})
	}()
	reaped := false
	t.Cleanup(func() {
		if !reaped {
			cancel()
			<-issues
		}
	})

	conn, err := listener.Accept()
	if err != nil {
		t.Fatalf("accept blocking hook: %v", err)
	}
	t.Cleanup(func() {
		if err := conn.Close(); err != nil {
			t.Errorf("close blocking hook connection: %v", err)
		}
	})
	var ready [1]byte
	if _, err := io.ReadFull(conn, ready[:]); err != nil {
		t.Fatalf("wait for hook readiness: %v", err)
	}
	cancel()
	// invokeHook returns only after exec.Cmd.Run has reaped the child.
	issue := <-issues
	reaped = true
	if issue == nil {
		t.Fatal("timed-out lifecycle hook produced no issue")
	}
	process, ok := issue.Detail.(ProcessIssue)
	if !ok {
		t.Fatalf("issue detail = %T, want ProcessIssue", issue.Detail)
	}
	var timeoutError *timeoutError
	if !errors.As(process.Cause, &timeoutError) {
		t.Fatalf("process cause = %T, want timeoutError", process.Cause)
	}
	if timeoutError.Limit != timeout {
		t.Fatalf("timeout limit = %s, want %s", timeoutError.Limit, timeout)
	}
}

func TestInvokeHookBlockingProcess(t *testing.T) {
	address := os.Getenv(invokeHookProcessAddressEnv)
	if address == "" {
		return
	}
	conn, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatalf("connect blocking hook: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte{1}); err != nil {
		t.Fatalf("signal blocking hook readiness: %v", err)
	}
	if _, err := io.Copy(io.Discard, conn); err != nil {
		t.Fatalf("wait for hook release: %v", err)
	}
}
