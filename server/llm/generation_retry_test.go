package llm

import (
	"context"
	"io"
	"testing"
	"time"
)

func TestGenerationRetryResetsStatusOnlyAttempt(t *testing.T) {
	attempts, updates, resets := 0, 0, 0
	_, err := GenerateWithRetry(t.Context(), func(_ context.Context, callbacks StreamCallbacks, finished func()) (Response, error) {
		attempts++
		if attempts > 1 {
			return Response{}, nil
		}
		callbacks.OnReasoningSummaryDelta(ReasoningSummaryDelta{CurrentStatus: &ReasoningStatus{Text: "Transient status"}})
		finished()
		callbacks.OnReasoningSummaryDelta(ReasoningSummaryDelta{CurrentStatus: &ReasoningStatus{Text: "Late status"}})
		return Response{}, io.ErrUnexpectedEOF
	}, GenerationRetryOptions{
		Schedule: GenerationRetrySchedule{Failures: []time.Duration{time.Nanosecond}},
		Callbacks: StreamCallbacks{OnReasoningSummaryDelta: func(delta ReasoningSummaryDelta) {
			if delta.CurrentStatus == nil {
				t.Fatal("thinking status lost during retry delivery")
			}
			updates++
		}},
		OnAttemptReset: func() { resets++ },
	})
	if err != nil {
		t.Fatal(err)
	}
	if attempts != 2 || updates != 1 || resets != 1 {
		t.Fatalf("attempts=%d updates=%d resets=%d, want retry with one live status and reset", attempts, updates, resets)
	}
}
