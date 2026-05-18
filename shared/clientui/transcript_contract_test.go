package clientui

import (
	"encoding/json"
	"testing"
)

func TestTranscriptMessageContractWireValues(t *testing.T) {
	phases := map[MessagePhase]string{
		MessagePhaseCommentary: "commentary",
		MessagePhaseFinal:      "final_answer",
	}
	for phase, want := range phases {
		if got := string(phase); got != want {
			t.Fatalf("phase %q wire value = %q, want %q", phase, got, want)
		}
	}

	messageTypes := map[MessageType]string{
		MessageTypeAgentsMD:                  "agents.md",
		MessageTypeSkills:                    "skills",
		MessageTypeSubagents:                 "subagents",
		MessageTypeEnvironment:               "environment",
		MessageTypeCompactionSummary:         "compaction_summary",
		MessageTypeInterruption:              "interruption",
		MessageTypeErrorFeedback:             "error_feedback",
		MessageTypeCompactionSoonReminder:    "compaction_soon_reminder",
		MessageTypeHandoffFutureMessage:      "handoff_future_message",
		MessageTypeReviewerFeedback:          "reviewer_feedback",
		MessageTypeBackgroundNotice:          "background_notice",
		MessageTypeCustomToolCallOutput:      "custom_tool_call_output",
		MessageTypeManualCompactionCarryover: "manual_compaction_carryover",
		MessageTypeHeadlessMode:              "headless_mode",
		MessageTypeHeadlessModeExit:          "headless_mode_exit",
		MessageTypeWorkflowMode:              "workflow_mode",
		MessageTypeWorktreeMode:              "worktree_mode",
		MessageTypeWorktreeModeExit:          "worktree_mode_exit",
		MessageTypeGoal:                      "goal",
	}
	for messageType, want := range messageTypes {
		if got := string(messageType); got != want {
			t.Fatalf("message type %q wire value = %q, want %q", messageType, got, want)
		}
	}
}

func TestMessagePhaseNormalization(t *testing.T) {
	tests := map[string]MessagePhase{
		"commentary":   MessagePhaseCommentary,
		"COMMENTARY":   MessagePhaseCommentary,
		"final_answer": MessagePhaseFinal,
		"finalanswer":  MessagePhaseFinal,
		"final":        MessagePhaseFinal,
		"unknown":      "",
		"":             "",
	}
	for raw, want := range tests {
		if got := NormalizeMessagePhase(raw); got != want {
			t.Fatalf("NormalizeMessagePhase(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestTranscriptMessageContractJSONRoundTrip(t *testing.T) {
	type payload struct {
		Phase       MessagePhase `json:"phase,omitempty"`
		MessageType MessageType  `json:"message_type,omitempty"`
	}

	input := payload{Phase: MessagePhaseFinal, MessageType: MessageTypeReviewerFeedback}
	encoded, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal transcript contract: %v", err)
	}
	if string(encoded) != `{"phase":"final_answer","message_type":"reviewer_feedback"}` {
		t.Fatalf("encoded transcript contract = %s", encoded)
	}

	var decoded payload
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatalf("unmarshal transcript contract: %v", err)
	}
	if decoded != input {
		t.Fatalf("decoded transcript contract = %#v, want %#v", decoded, input)
	}
}
