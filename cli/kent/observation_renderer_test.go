package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"core/shared/clientui"
	"core/shared/config"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/serverapi"
)

func mustCLIObservationQuestion(t *testing.T, question serverapi.ObservationQuestion) *promptpb.ObservationQuestion {
	t.Helper()
	value, err := protoapi.ObservationQuestionToProto(question)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestObservedQuestionUsesDynamicQuestionAndAnswerTarget(t *testing.T) {
	question := serverapi.ObservationQuestion{Approval: &clientui.PendingApproval{
		ToolCallID: "approval-dynamic", SessionID: mustQuestionCommandSessionID("session-1"),
		StepID:  questionCommandStepID(),
		Options: []clientui.ApprovalOption{{Label: "dynamic allow", Decision: clientui.ApprovalDecisionAllowOnce}},
		AccessTargets: []clientui.FileAccessTarget{{
			RequestedPath: "/alias/file", ResolvedPath: "/real/file",
		}},
	}}
	var output bytes.Buffer
	writeObservedQuestion(&output, question, "kent question answer --session session-dynamic --option <number>")
	for _, value := range []string{
		clientui.FormatFileAccessApprovalMarkdown(question.Approval.AccessTargets),
		"dynamic allow",
		"session-dynamic",
	} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("output %q does not contain dynamic value %q", output.String(), value)
		}
	}
}

func TestRunWatchApprovalHintTargetsSession(t *testing.T) {
	sessionID := "session-1"
	var output bytes.Buffer
	code := writeRunWatchResponse(&output, io.Discard, &promptpb.LiveWatchSuccess{
		SessionId: sessionID,
		Outcome: &promptpb.LiveWatchOutcome{
			Outcome: &promptpb.LiveWatchOutcome_Question{
				Question: mustCLIObservationQuestion(t, serverapi.ObservationQuestion{Approval: &clientui.PendingApproval{
					ToolCallID: "approval-dynamic", SessionID: mustQuestionCommandSessionID("session-1"),
					StepID: questionCommandStepID(), Question: "Allow access?", CreatedAt: time.Unix(1, 0),
					Options: []clientui.ApprovalOption{{
						Label: "Allow once", Decision: clientui.ApprovalDecisionAllowOnce,
					}},
				}}),
			},
		},
	}, "")
	if code != 0 {
		t.Fatalf("writeRunWatchResponse exit code = %d", code)
	}
	hint := commandString([]string{
		config.Command, "question", "answer",
		"--session", sessionID,
		"--option", "<number>",
	})
	if !strings.Contains(output.String(), hint) {
		t.Fatalf("output %q does not contain answer hint %q", output.String(), hint)
	}
}

func TestRunWatchRendersInterruptedReasonAndDiagnostic(t *testing.T) {
	reason := "interrupted"
	diagnostic := "stop detail"
	var output bytes.Buffer
	code := writeRunWatchResponse(&output, io.Discard, &promptpb.LiveWatchSuccess{
		SessionId: "session-dynamic",
		Outcome: &promptpb.LiveWatchOutcome{
			Outcome: &promptpb.LiveWatchOutcome_Interrupted{Interrupted: &promptpb.LiveWatchFailure{
				Reason: reason, Diagnostic: &diagnostic,
			}},
		},
	}, "")
	if code != 130 {
		t.Fatalf("writeRunWatchResponse exit code = %d, want 130", code)
	}
	for _, value := range []string{reason, diagnostic} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("output %q does not contain %q", output.String(), value)
		}
	}
}
