package runtimeview

import (
	"errors"
	"testing"
	"time"

	"core/server/llm"
	"core/server/runtime"
	"core/shared/protoapi"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/textutil"
)

func TestTranscriptProjectsLiveRunResultWithoutRuntimeIDs(t *testing.T) {
	startedAt := time.Date(2026, time.July, 20, 10, 0, 0, 0, time.UTC)
	finishedAt := startedAt.Add(time.Second)
	tests := []struct {
		name   string
		result runtime.LiveRunResult
		check  func(*testing.T, *transcriptpb.LiveRunFinished)
	}{
		{
			name: "final answer",
			result: runtime.LiveRunResult{
				Status:           runtime.RunStatusCompleted,
				ResultKind:       runtime.LiveRunResultAssistantFinalAnswer,
				WorkPerformed:    true,
				AssistantMessage: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("done")},
				StartedAt:        startedAt,
				FinishedAt:       finishedAt,
			},
			check: func(t *testing.T, result *transcriptpb.LiveRunFinished) {
				if result.FinalAnswer == nil || *result.FinalAnswer != "done" || !result.WorkPerformed {
					t.Fatalf("result = %+v", result)
				}
			},
		},
		{
			name: "failure",
			result: runtime.LiveRunResult{
				Status:     runtime.RunStatusFailed,
				ResultKind: runtime.LiveRunResultNoFinalAnswer,
				Error:      errors.New("provider failed"),
				StartedAt:  startedAt,
				FinishedAt: finishedAt,
			},
			check: func(t *testing.T, result *transcriptpb.LiveRunFinished) {
				if result.Failure == nil || *result.Failure != "provider failed" {
					t.Fatalf("result = %+v", result)
				}
			},
		},
		{
			name: "failure after final answer",
			result: runtime.LiveRunResult{
				Status:           runtime.RunStatusFailed,
				ResultKind:       runtime.LiveRunResultAssistantFinalAnswer,
				AssistantMessage: llm.Message{Role: llm.RoleAssistant, Content: textutil.Value("partial final")},
				Error:            errors.New("follow-up failed"),
				StartedAt:        startedAt,
				FinishedAt:       finishedAt,
			},
			check: func(t *testing.T, result *transcriptpb.LiveRunFinished) {
				if result.FinalAnswer == nil || *result.FinalAnswer != "partial final" {
					t.Fatalf("final answer = %+v", result.FinalAnswer)
				}
				if result.Failure == nil || *result.Failure != "follow-up failed" {
					t.Fatalf("failure = %+v", result.Failure)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
				Kind:          runtime.EventLiveRunFinished,
				LiveRunResult: &test.result,
			})
			if err != nil {
				t.Fatalf("project live run result: %v", err)
			}
			if len(messages) != 1 || messages[0].GetLiveRunFinished() == nil {
				t.Fatalf("messages = %+v", messages)
			}
			projected := messages[0].GetLiveRunFinished()
			if err := protoapi.Validate(projected); err != nil {
				t.Fatalf("validate: %v", err)
			}
			test.check(t, projected)
		})
	}
}

func TestTranscriptProjectsMissingAssistantFinalTextAsNoFinalResult(t *testing.T) {
	startedAt := time.Date(2026, time.July, 22, 21, 25, 14, 0, time.UTC)
	finishedAt := startedAt.Add(time.Second)

	messages, err := TranscriptMessagesFromRuntimeEventChecked(runtime.Event{
		Kind: runtime.EventLiveRunFinished,
		LiveRunResult: &runtime.LiveRunResult{
			Status:     runtime.RunStatusCompleted,
			ResultKind: runtime.LiveRunResultAssistantFinalAnswer,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
		},
	})
	if err != nil {
		t.Fatalf("project missing assistant final text: %v", err)
	}

	if len(messages) != 1 {
		t.Fatalf("messages = %+v, want one live-run completion", messages)
	}
	projected := messages[0].GetLiveRunFinished()
	if projected.ResultKind != transcriptpb.LiveRunResultKind_LIVE_RUN_RESULT_KIND_NO_FINAL_ANSWER || projected.FinalAnswer != nil {
		t.Fatalf("projected live run = %+v, want no-final result without fabricated answer", projected)
	}
}
