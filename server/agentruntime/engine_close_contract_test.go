package runtime

import (
	"context"
	"errors"
	"testing"

	"core/server/llm"
	"core/server/tools"
	"core/shared/textutil"
)

func TestRetirementBeforeSteerAcceptanceRejectsTheMessage(t *testing.T) {
	engine := mustNewTestEngine(t, mustCreateTestSession(t), &fakeClient{}, tools.NewRegistry(), Config{Model: "gpt-6-sol"})
	_, err := engine.Steer(t.Context(), "do not lose this message", func(commit func() (bool, error)) (bool, error) {
		if !engine.BeginRetirement() {
			t.Fatal("idle Runtime could not begin retirement before message acceptance")
		}
		return commit()
	})
	if !errors.Is(err, ErrEngineClosed) {
		t.Fatalf("Steer after retirement = %v, want closed Runtime", err)
	}
	if engine.HasQueuedUserWork() {
		t.Fatal("retired Runtime accepted a message it cannot deliver")
	}
}

func TestCloseRejectsUserTurnsAndSteeringWithoutNewWork(t *testing.T) {
	t.Parallel()
	store := mustCreateTestSession(t)
	client := &fakeClient{}
	engine := mustNewTestEngine(t, store, client, tools.NewRegistry(), Config{Model: "gpt-6-sol"})

	if err := engine.Close(); err != nil {
		t.Fatalf("close engine: %v", err)
	}
	if _, err := engine.SubmitUserMessage(context.Background(), "after-close"); !errors.Is(err, ErrEngineClosed) {
		t.Fatalf("submit after close error = %v, want ErrEngineClosed", err)
	}
	if err := engine.steerRuntime(steerMessagesWithPersistenceIntent(steeringPriorityNormal, steeringMessageEventDefault, true, []llm.Message{{Role: llm.RoleUser, Content: textutil.Value("after-close")}})); !errors.Is(err, ErrEngineClosed) {
		t.Fatalf("steer after close error = %v, want ErrEngineClosed", err)
	}
	if calls := len(client.calls); calls != 0 {
		t.Fatalf("provider dispatches after close = %d, want zero", calls)
	}

	window, err := mustMaterializeTestEventLog(t, store).ReadRecentRecords(8)
	if err != nil {
		t.Fatalf("read bounded records after close: %v", err)
	}
	if len(window.Records) != 0 {
		t.Fatalf("records persisted after close = %+v, want none", window.Records)
	}
}

type resourceModelClient struct {
	*fakeClient
	closed int
	err    error
}

func (c *resourceModelClient) Close() error {
	c.closed++
	return c.err
}

func TestEngineCloseReleasesMainAndReviewerResourcesAndReportsFailures(t *testing.T) {
	mainError, reviewerError := errors.New("main resource failure"), errors.New("reviewer resource failure")
	main := &resourceModelClient{fakeClient: &fakeClient{}, err: mainError}
	reviewer := &resourceModelClient{fakeClient: &fakeClient{}, err: reviewerError}
	engine := mustNewTestEngine(t, mustCreateTestSession(t), main, tools.NewRegistry(), Config{
		Model: "gpt-6-sol", Reviewer: ReviewerConfig{Client: reviewer},
	})
	err := engine.Close()
	if !errors.Is(err, mainError) || !errors.Is(err, reviewerError) || main.closed != 1 || reviewer.closed != 1 {
		t.Fatalf("resource cleanup: main=%d reviewer=%d error=%v", main.closed, reviewer.closed, err)
	}
	if err := engine.Close(); err != nil || main.closed != 1 || reviewer.closed != 1 {
		t.Fatalf("repeat cleanup: main=%d reviewer=%d error=%v", main.closed, reviewer.closed, err)
	}
}
