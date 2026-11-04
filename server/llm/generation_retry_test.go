package llm

import (
	"context"
	"errors"
	"io"
	"testing"
	"testing/synctest"
	"time"
)

func TestGenerationRetryBudgets(t *testing.T) {
	for _, test := range []struct {
		name         string
		err          error
		succeedAt    int
		wantAttempts int
		wantWait     time.Duration
	}{
		{"message limit exhausted", &ProviderAPIError{ProviderType: "response.incomplete", ProviderCode: "max_messages"}, 0, 4, 7 * time.Second},
		{"message limit recovers", &ProviderAPIError{ProviderType: "response.incomplete", ProviderCode: "max_messages"}, 3, 3, 3 * time.Second},
		{"other incomplete", &ProviderAPIError{ProviderType: "response.incomplete", ProviderCode: "max_output_tokens"}, 0, 1, 0},
		{"ordinary transient", io.ErrUnexpectedEOF, 0, 6, 31 * time.Second},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				start := time.Now()
				attempts := 0
				_, err := GenerateWithRetry(t.Context(), func(context.Context, StreamCallbacks, func()) (Response, error) {
					attempts++
					if attempts == test.succeedAt {
						return Response{}, nil
					}
					return Response{}, test.err
				}, GenerationRetryOptions{Schedule: DefaultGenerationRetrySchedule()})
				if attempts != test.wantAttempts || time.Since(start) != test.wantWait {
					t.Fatalf("attempts=%d elapsed=%s", attempts, time.Since(start))
				}
				if test.succeedAt > 0 {
					if err != nil {
						t.Fatal(err)
					}
				} else if !errors.Is(err, test.err) {
					t.Fatalf("lost provider error: %v", err)
				}
			})
		})
	}
}

func TestGenerationMessageLimitRetryCancellation(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go func() { time.Sleep(500 * time.Millisecond); cancel() }()
		attempts := 0
		_, err := GenerateWithRetry(ctx, func(context.Context, StreamCallbacks, func()) (Response, error) {
			attempts++
			return Response{}, &ProviderAPIError{ProviderType: "response.incomplete", ProviderCode: "max_messages"}
		}, GenerationRetryOptions{Schedule: DefaultGenerationRetrySchedule()})
		if !errors.Is(err, context.Canceled) || attempts != 1 {
			t.Fatalf("attempts=%d err=%v", attempts, err)
		}
	})
}

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
