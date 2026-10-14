//go:build !windows

package driver_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"core/internal/testharness/pty"
	"core/internal/testharness/pty/driver"

	"github.com/google/uuid"
)

const (
	commandTestTimeout = 30 * time.Second
	commandTimeoutCap  = time.Minute
)

func TestRunCommandRejectsTimeoutAboveHarnessBudget(t *testing.T) {
	_, err := driver.RunCommand(context.Background(), driver.CommandSpec{
		Path:       "unused",
		Dimensions: pty.MustDimensions(2, 8),
		Timeout:    commandTimeoutCap + time.Nanosecond,
	})
	if err == nil || !strings.Contains(err.Error(), "exceeds PTY command timeout cap") {
		t.Fatalf("RunCommand error = %v, want timeout-cap error", err)
	}
}

func TestRunCommandCapturesOutputAndAnalyzes(t *testing.T) {
	t.Parallel()

	binary := buildHelper(t)
	capture, err := driver.RunCommand(context.Background(), driver.CommandSpec{
		Path:       binary,
		Args:       []string{"write"},
		Dimensions: pty.MustDimensions(3, 16),
	})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if len(capture.Chunks) == 0 {
		t.Fatalf("chunk count = 0, want captured output")
	}
	if capture.ProcessExit == nil || capture.ProcessExit.Code != 0 {
		t.Fatalf("process exit = %#v, want code 0", capture.ProcessExit)
	}
	analysis, err := pty.Analyze(capture)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got := analysis.Screen.TextInRegion(pty.Region{Top: 0, Bottom: 1, Left: 0, Right: 5}); got != "hello" {
		t.Fatalf("screen text = %q, want %q", got, "hello")
	}
}

func TestRunCommandPreservesEmptyCapture(t *testing.T) {
	t.Parallel()

	binary := buildHelper(t)
	capture, err := driver.RunCommand(context.Background(), driver.CommandSpec{
		Path:       binary,
		Args:       []string{"no-output"},
		Dimensions: pty.MustDimensions(2, 8),
	})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if len(capture.Chunks) != 0 || len(capture.Raw) != 0 {
		t.Fatalf("capture should stay empty, got chunks=%#v raw=%#v", capture.Chunks, capture.Raw)
	}
	analysis, err := pty.Analyze(capture)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if !analysis.Screen.IsBlank() {
		t.Fatalf("screen should be blank: %#v", analysis.Screen)
	}
}

func TestSessionFeedsInputAfterResizeCompletion(t *testing.T) {
	t.Parallel()

	for _, beforeOutput := range []bool{true, false} {
		name := "after-phase"
		if beforeOutput {
			name = "before-first-chunk"
		}
		t.Run(name, func(t *testing.T) {
			var path string
			var args []string
			if beforeOutput {
				path = buildHelper(t)
				args = []string{"echo-byte"}
			} else {
				path = phaseInputWriterBinary(t)
			}
			session, err := driver.StartSession(driver.SessionSpec{
				Path: path, Args: args,
				Env:        []string{"TERM=xterm-256color", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"},
				Dimensions: pty.MustDimensions(2, 12),
			})
			if err != nil {
				t.Fatalf("StartSession: %v", err)
			}
			t.Cleanup(func() { closeSession(t, session) })
			dimensions := pty.MustDimensions(3, 14)
			resizeID, inputID := uuid.New(), uuid.New()
			resizeQueued := false
			resizeCompleted, inputCompleted := false, false
			resize := func() {
				if err := session.Enqueue(driver.SessionCommand{
					ID: resizeID, Kind: driver.SessionCommandResize, Dimensions: &dimensions,
				}); err != nil {
					t.Fatalf("Enqueue resize: %v", err)
				}
				resizeQueued = true
			}
			if beforeOutput {
				resize()
			}
			for event := range session.Events() {
				if event.Err != nil {
					t.Fatalf("session event failed: %v", event.Err)
				}
				if !resizeQueued && event.Analysis != nil {
					for _, phase := range event.Analysis.PhaseEvents {
						if phase.Phase == pty.PhaseScenarioStart {
							resize()
							break
						}
					}
				}
				if event.Kind == driver.SessionEventCommandCompleted && event.CommandID == resizeID {
					resizeCompleted = true
					if err := session.Enqueue(driver.SessionCommand{
						ID: inputID, Kind: driver.SessionCommandWrite, Bytes: []byte("x\n"),
					}); err != nil {
						t.Fatalf("Enqueue input: %v", err)
					}
				}
				if event.Kind == driver.SessionEventCommandCompleted && event.CommandID == inputID {
					inputCompleted = true
				}
			}
			<-session.Done()
			capture, err := session.Capture()
			if err != nil {
				t.Fatalf("Capture: %v", err)
			}
			if !resizeCompleted || !inputCompleted {
				t.Fatalf("command completion resize=%v input=%v", resizeCompleted, inputCompleted)
			}
			if len(capture.Resizes) != 1 {
				t.Fatalf("resize count = %d, want 1", len(capture.Resizes))
			}
			if beforeOutput && capture.Resizes[0].Placement.Kind != pty.ResizeBeforeFirstChunk {
				t.Fatalf("resize placement = %#v, want before first chunk", capture.Resizes[0].Placement)
			}
			if !beforeOutput && capture.Resizes[0].Placement.Kind != pty.ResizeAfterChunk {
				t.Fatalf("resize placement = %#v, want after phase output", capture.Resizes[0].Placement)
			}
			analysis, err := pty.Analyze(capture)
			if err != nil {
				t.Fatalf("Analyze: %v", err)
			}
			var resizeIndex, inputIndex *int
			for index, operation := range analysis.Operations {
				if operation.Kind == pty.OperationResize {
					if operation.Region != (pty.Region{Top: 0, Bottom: 3, Left: 0, Right: 14}) {
						t.Fatalf("resize region = %#v", operation.Region)
					}
					resizeIndex = &index
				}
				for _, record := range pty.OperationRecords(operation) {
					if record.Write != nil && record.Write.Text() == "input:x" {
						inputIndex = &index
					}
				}
			}
			if resizeIndex == nil || inputIndex == nil || *resizeIndex >= *inputIndex {
				t.Fatalf("resize must precede input output: operations=%#v", analysis.Operations)
			}
			if beforeOutput && *resizeIndex != 0 {
				t.Fatalf("resize operation index = %d, want 0", *resizeIndex)
			}
			if !beforeOutput && len(analysis.PhaseEvents) != 1 {
				t.Fatalf("phase events = %#v, want scenario start", analysis.PhaseEvents)
			}
		})
	}
}

func TestRunCommandDispatchesInputWhenPhaseIsReady(t *testing.T) {
	output := phaseInputWriterBinary(t)
	capture, err := driver.RunCommand(context.Background(), driver.CommandSpec{
		Path:       output,
		Dimensions: pty.MustDimensions(2, 16),
		PhaseInputs: []driver.PhaseInputEvent{{
			Phase: pty.PhaseScenarioStart,
			Bytes: []byte("x\n"),
		}},
	})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	analysis, err := pty.Analyze(capture)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if len(analysis.PhaseEvents) != 1 {
		t.Fatalf("phase events = %#v, want one scenario start", analysis.PhaseEvents)
	}
	if len(capture.PhaseInputDispatches) != 1 {
		t.Fatalf("phase input dispatches = %#v, want one", capture.PhaseInputDispatches)
	}
	dispatch := capture.PhaseInputDispatches[0]
	if dispatch.Phase != pty.PhaseScenarioStart || dispatch.ScheduledAfter != 0 {
		t.Fatalf("phase input dispatch = %#v, want immediate scenario-start input", dispatch)
	}
	if dispatch.StartedAt < analysis.PhaseEvents[0].CapturedAt {
		t.Fatalf("input dispatched before phase was captured: %#v", dispatch)
	}
	var inputWriteAt *time.Duration
	for _, operation := range analysis.Operations {
		for _, record := range pty.OperationRecords(operation) {
			if record.Write != nil && record.Write.Text() == "input:x" {
				capturedAt := record.CapturedAt
				inputWriteAt = &capturedAt
				break
			}
		}
		if inputWriteAt != nil {
			break
		}
	}
	if inputWriteAt == nil {
		t.Fatalf("phase input output missing: operations=%#v", analysis.Operations)
	}
	if *inputWriteAt < dispatch.StartedAt {
		t.Fatalf("input output captured before dispatch: output=%s dispatch=%#v", *inputWriteAt, dispatch)
	}
}

func TestRunCommandDispatchesFrameInputSequenceAfterCompletedFrames(t *testing.T) {
	output := phaseInputWriterBinary(t)
	capture, err := driver.RunCommand(context.Background(), driver.CommandSpec{
		Path:       output,
		Args:       []string{"frame-sequence"},
		Dimensions: pty.MustDimensions(2, 16),
		FrameInputSequences: []driver.FrameInputSequence{{
			Phase: pty.PhaseScenarioStart,
			Inputs: []driver.FrameInput{
				{Readiness: pty.ReadinessRendererFrame, Bytes: []byte("x\n")},
				{Readiness: pty.ReadinessRendererFrame, Bytes: []byte("y\n")},
				{Readiness: pty.ReadinessRendererFrame, Bytes: []byte("z\n")},
			},
		}},
	})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if len(capture.FrameInputDispatches) != 3 {
		t.Fatalf("frame input dispatches = %#v, want three", capture.FrameInputDispatches)
	}
	for index, dispatch := range capture.FrameInputDispatches {
		if dispatch.Phase != pty.PhaseScenarioStart || dispatch.InputIndex != index {
			t.Fatalf("frame input dispatch %d = %#v", index, dispatch)
		}
		if dispatch.ReadyBoundary != pty.ReadinessRendererFrame {
			t.Fatalf("frame input dispatch %d boundary = %d, want renderer frame", index, dispatch.ReadyBoundary)
		}
		if dispatch.ReadyBoundaryEndByteOffset <= 0 {
			t.Fatalf("frame input dispatch %d ready boundary end = %d, want positive", index, dispatch.ReadyBoundaryEndByteOffset)
		}
		if index > 0 && dispatch.ReadyBoundaryEndByteOffset <= capture.FrameInputDispatches[index-1].ReadyBoundaryEndByteOffset {
			t.Fatalf("frame input ready offsets are not increasing: %#v", capture.FrameInputDispatches)
		}
	}
}

func TestRunCommandDispatchesFrameInputsAcrossTypedReadinessBoundaries(t *testing.T) {
	output := phaseInputWriterBinary(t)
	capture, err := driver.RunCommand(context.Background(), driver.CommandSpec{
		Path:       output,
		Args:       []string{"typed-readiness-sequence"},
		Dimensions: pty.MustDimensions(2, 16),
		FrameInputSequences: []driver.FrameInputSequence{{
			Phase: pty.PhaseScenarioStart,
			Inputs: []driver.FrameInput{
				{Readiness: pty.ReadinessRendererFrame, Bytes: []byte("x\n")},
				{Readiness: pty.ReadinessInputApplied, Bytes: []byte("y\n")},
				{Readiness: pty.ReadinessRendererFrame, Bytes: []byte("z\n")},
				{Readiness: pty.ReadinessNormalBufferRestored, Bytes: []byte("q\n")},
			},
		}},
	})
	if err != nil {
		t.Fatalf("RunCommand: %v", err)
	}
	if len(capture.FrameInputDispatches) != 4 {
		t.Fatalf("frame input dispatches = %#v, want four", capture.FrameInputDispatches)
	}
	wantBoundaries := []pty.ReadinessBoundaryKind{
		pty.ReadinessRendererFrame,
		pty.ReadinessInputApplied,
		pty.ReadinessRendererFrame,
		pty.ReadinessNormalBufferRestored,
	}
	for index, want := range wantBoundaries {
		if got := capture.FrameInputDispatches[index].ReadyBoundary; got != want {
			t.Fatalf("frame input dispatch %d boundary = %d, want %d", index, got, want)
		}
	}
}

func TestRunCommandRejectsFirstInputAppliedReadiness(t *testing.T) {
	binary := buildHelper(t)
	_, err := driver.RunCommand(context.Background(), driver.CommandSpec{
		Path:       binary,
		Args:       []string{"write"},
		Dimensions: pty.MustDimensions(2, 16),
		FrameInputSequences: []driver.FrameInputSequence{{
			Phase: pty.PhaseScenarioStart,
			Inputs: []driver.FrameInput{{
				Readiness: pty.ReadinessInputApplied,
				Bytes:     []byte("x\n"),
			}},
		}},
	})
	if err == nil {
		t.Fatal("RunCommand accepted input-applied readiness for the first sequence input")
	}
}

func TestRunCommandCancellationTerminatesProcessAndReader(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	binary, err := os.Executable()
	if err != nil {
		t.Fatalf("Executable: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ready := make(chan error, 1)
	go func() {
		defer cancel()
		connection, err := listener.Accept()
		if err != nil {
			ready <- err
			return
		}
		defer func() { _ = connection.Close() }()
		var boundary pty.ReadinessBoundaryKind
		if err := json.NewDecoder(connection).Decode(&boundary); err != nil {
			ready <- err
			return
		}
		if boundary != pty.ReadinessRendererFrame {
			ready <- errors.New("child did not signal renderer-frame readiness")
			return
		}
		ready <- nil
	}()
	capture, err := driver.RunCommand(ctx, driver.CommandSpec{
		Path:       binary,
		Args:       []string{"-test.run=^TestCancellationChild$"},
		Env:        []string{"KENT_PTY_CANCELLATION_ADDRESS=" + listener.Addr().String()},
		Dimensions: pty.MustDimensions(2, 8),
	})
	_ = listener.Close()
	if readinessErr := <-ready; readinessErr != nil {
		t.Fatalf("child readiness: %v; RunCommand: %v", readinessErr, err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || errors.As(err, new(*driver.TimeoutError)) {
		t.Fatalf("RunCommand error = %v, want canceled process exit without timeout", err)
	}
	if !errors.Is(ctx.Err(), context.Canceled) {
		t.Fatalf("context error = %v, want cancellation", ctx.Err())
	}
	if capture.ProcessExit == nil || capture.ProcessExit.Code == 0 {
		t.Fatalf("process exit = %#v, want terminated child", capture.ProcessExit)
	}
	if !capture.ReadLoopDone {
		t.Fatalf("read loop completion not recorded after cancellation cleanup")
	}
}

func TestCancellationChild(t *testing.T) {
	address, child := os.LookupEnv("KENT_PTY_CANCELLATION_ADDRESS")
	if !child {
		return
	}
	if _, err := os.Stdout.Write([]byte("\x1b[2;1H")); err != nil {
		t.Fatalf("write renderer frame: %v", err)
	}
	connection, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatalf("connect readiness signal: %v", err)
	}
	defer func() { _ = connection.Close() }()
	if err := json.NewEncoder(connection).Encode(pty.ReadinessRendererFrame); err != nil {
		t.Fatalf("signal renderer-frame readiness: %v", err)
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatalf("wait for cancellation: %v", err)
	}
}

func TestSessionCommandRequiresUUIDv4AndClosedPayloadShape(t *testing.T) {
	t.Parallel()

	dimensions := pty.MustDimensions(2, 8)
	for _, command := range []driver.SessionCommand{
		{ID: uuid.Nil, Kind: driver.SessionCommandWrite, Bytes: []byte("x")},
		{ID: uuid.Must(uuid.NewV7()), Kind: driver.SessionCommandWrite, Bytes: []byte("x")},
		{ID: uuid.New(), Kind: driver.SessionCommandWrite},
		{ID: uuid.New(), Kind: driver.SessionCommandResize},
		{ID: uuid.New(), Kind: driver.SessionCommandRuntimeControlByte},
		{ID: uuid.New(), Kind: driver.SessionCommandKind(99)},
		{ID: uuid.New(), Kind: driver.SessionCommandWrite, Bytes: []byte("x"), Dimensions: &dimensions},
		{ID: uuid.New(), Kind: driver.SessionCommandRuntimeControlByte, Bytes: []byte("x"), Dimensions: &dimensions},
		{ID: uuid.New(), Kind: driver.SessionCommandResize, Bytes: []byte("x"), Dimensions: &dimensions},
		{ID: uuid.New(), Kind: driver.SessionCommandResize, Bytes: []byte{}, Dimensions: &dimensions},
		{ID: uuid.New(), Kind: driver.SessionCommandTerminateProcess, Bytes: []byte("x")},
		{ID: uuid.New(), Kind: driver.SessionCommandTerminateProcess, Bytes: []byte{}},
		{ID: uuid.New(), Kind: driver.SessionCommandTerminateProcess, Dimensions: &dimensions},
	} {
		if err := command.Validate(); err == nil {
			t.Fatalf("Validate succeeded for %#v", command)
		}
	}
	if err := (driver.SessionCommand{ID: uuid.New(), Kind: driver.SessionCommandResize, Dimensions: &dimensions}).Validate(); err != nil {
		t.Fatalf("Validate resize: %v", err)
	}
}

func TestSessionPublishesTerminalAndProcessEvents(t *testing.T) {
	t.Parallel()

	session, err := driver.StartSession(driver.SessionSpec{
		Path:       buildHelper(t),
		Args:       []string{"write"},
		Env:        []string{"TERM=xterm-256color", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"},
		Dimensions: pty.MustDimensions(2, 8),
	})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	var terminal bool
	var exited bool
	for event := range session.Events() {
		if event.Kind == driver.SessionEventTerminalAnalysis && event.Analysis != nil {
			terminal = true
		}
		if event.Kind == driver.SessionEventProcessExit {
			exited = true
		}
	}
	if !terminal || !exited {
		t.Fatalf("events terminal=%v exited=%v", terminal, exited)
	}
}

func TestSessionChildReceivesOnlyDeclaredEnvironment(t *testing.T) {
	t.Setenv("KENT_OPENAI_BASE_URL", "http://poisoned.invalid/v1")
	t.Setenv("OPENAI_API_KEY", "poisoned")
	t.Setenv("HTTP_PROXY", "http://poisoned.invalid")
	t.Setenv("COLORTERM", "truecolor")

	session, err := driver.StartSession(driver.SessionSpec{
		Path:       buildHelper(t),
		Args:       []string{"env-json"},
		Env:        []string{"TERM=xterm-256color", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"},
		Dimensions: pty.MustDimensions(2, 8),
	})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	for range session.Events() {
	}
	capture, err := session.Capture()
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	var entries []string
	if err := json.Unmarshal(capture.Raw, &entries); err != nil {
		t.Fatalf("decode child environment: %v; raw=%q", err, capture.Raw)
	}
	environment := make(map[string]string, len(entries))
	for _, entry := range entries {
		key, value, found := strings.Cut(entry, "=")
		if !found || key == "" {
			t.Fatalf("invalid child environment entry %q", entry)
		}
		environment[key] = value
	}
	if len(environment) != 3 || environment["TERM"] != "xterm-256color" || environment["LANG"] != "C.UTF-8" || environment["LC_ALL"] != "C.UTF-8" {
		t.Fatalf("child environment = %#v", environment)
	}
	for _, poisoned := range []string{"KENT_OPENAI_BASE_URL", "OPENAI_API_KEY", "HTTP_PROXY", "COLORTERM"} {
		if _, exists := environment[poisoned]; exists {
			t.Fatalf("child inherited poisoned environment key %s", poisoned)
		}
	}
}

func TestSessionWriteCommandCompletesOnlyAfterLargePTYInputIsAccepted(t *testing.T) {
	t.Parallel()

	session, err := driver.StartSession(driver.SessionSpec{
		Path:       buildHelper(t),
		Args:       []string{"read-large"},
		Env:        []string{"TERM=xterm-256color", "LANG=C.UTF-8", "LC_ALL=C.UTF-8"},
		Dimensions: pty.MustDimensions(2, 32),
	})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	t.Cleanup(func() { closeSession(t, session) })
	ready := false
	for !ready {
		event, ok := <-session.Events()
		if !ok {
			t.Fatal("session exited before terminal raw-mode readiness")
		}
		if event.Analysis == nil {
			continue
		}
		for _, mode := range event.Analysis.PrivateModeChanges {
			if mode.Mode == 25 && mode.Enabled {
				ready = true
				break
			}
		}
	}
	commandID := uuid.New()
	payload := bytes.Repeat([]byte("x"), 256*1024)
	if err := session.Enqueue(driver.SessionCommand{ID: commandID, Kind: driver.SessionCommandWrite, Bytes: payload}); err != nil {
		t.Fatalf("Enqueue: %v", err)
	}

	completed := false
	var capture pty.Capture
	for event := range session.Events() {
		if event.Kind == driver.SessionEventCommandCompleted && event.CommandID == commandID {
			completed = true
		}
		if event.Kind == driver.SessionEventCommandFailed && event.CommandID == commandID {
			t.Fatalf("large write command failed: %v", event.Err)
		}
	}
	<-session.Done()
	capture, err = session.Capture()
	if err != nil {
		t.Fatalf("Capture: %v", err)
	}
	if !completed {
		t.Fatal("large write command never completed")
	}
	analysis, err := pty.Analyze(capture)
	if err != nil {
		t.Fatalf("Analyze: %v", err)
	}
	if got := analysis.Screen.TextInRegion(pty.Region{Top: 0, Bottom: 1, Left: 0, Right: 15}); got != "received:262144" {
		t.Fatalf("child receipt = %q, want complete payload acknowledgement", got)
	}
}

func closeSession(t *testing.T, session *driver.Session) {
	t.Helper()
	select {
	case <-session.Done():
		return
	default:
	}
	if err := session.Close(); err != nil && !errors.Is(err, os.ErrClosed) {
		t.Errorf("Close session: %v", err)
	}
	if err := session.ForceKill(); err != nil {
		t.Errorf("ForceKill session: %v", err)
	}
	for range session.Events() {
	}
	<-session.Done()
}

func buildHelper(t *testing.T) string {
	t.Helper()
	output, err := pty.BuildOrUsePrebuiltPackage(
		context.Background(),
		pty.AnsiWriterBinaryEnvName,
		"core/internal/testharness/pty/testdata/cmd/ansi-writer",
		filepath.Join(t.TempDir(), "ansi-writer"),
	)
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			t.Fatalf("BuildPackage: %v\n%s", err, string(exitErr.Stderr))
		}
		t.Fatalf("BuildPackage: %v", err)
	}
	return output
}

func phaseInputWriterBinary(t *testing.T) string {
	t.Helper()

	output, err := pty.BuildOrUsePrebuiltPackage(
		context.Background(),
		pty.PhaseInputWriterBinaryEnvName,
		"core/internal/testharness/pty/testdata/cmd/phase-input-writer",
		filepath.Join(t.TempDir(), "phase-input-writer"),
	)
	if err != nil {
		t.Fatalf("BuildPackage: %v", err)
	}
	return output
}
