package runtimeview

import (
	"encoding/json"
	"strconv"
	"testing"

	"core/server/llm"
	"core/server/runtime"
	"core/server/session"
	"core/shared/protoapi"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/textutil"
	"core/shared/transcript"
	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

func TestPersistedShellCompletionHydratesFullExitStatus(t *testing.T) {
	for _, status := range []int{-1, 0, 255, 2147483648, 3221225477, 4294967295} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			store := newRuntimeViewStore(t)
			eventLog, err := store.MaterializeEventLog()
			if err != nil {
				t.Fatal(err)
			}
			presentation, err := json.Marshal(transcript.ToolCallMeta{
				ToolName: "exec_command", IsShell: true,
				Presentation:   transcript.ToolPresentationShell,
				RenderBehavior: transcript.ToolCallRenderBehaviorShell,
				Command:        "child", ShellExitCode: &status,
			})
			if err != nil {
				t.Fatal(err)
			}
			stepID := transcriptProjectionStepID
			if _, _, err := eventLog.AppendRecord(&stepID, session.MessageRecord{
				Role: session.MessageRoleAssistant,
				ToolCalls: []session.MessageToolCallRecord{{
					CallID: "call-status", Name: "exec_command",
					Kind: session.ToolCallKindFunction, Input: json.RawMessage(`{"cmd":"child"}`),
				}},
			}); err != nil {
				t.Fatal(err)
			}
			if _, _, err := eventLog.AppendRecord(&stepID, session.ToolCompletionRecord{
				CallID: "call-status", Name: "exec_command",
				OutputKind: session.ToolOutputKindFunction,
				Output:     json.RawMessage(`"completed"`), Presentation: presentation,
			}); err != nil {
				t.Fatal(err)
			}
			page, hydration := mustProjectNewestPersistedTranscript(t, store, eventLog)
			for _, rows := range [][]*transcriptpb.CommittedRow{page.Entries, hydration.TailSegment.Entries} {
				if len(rows) != 1 || rows[0].GetTool() == nil {
					t.Fatalf("expected one completed tool row: %v", rows)
				}
				wire := rows[0].GetTool().Presentation
				encoded, err := proto.Marshal(wire)
				if err != nil {
					t.Fatal(err)
				}
				decoded := &transcriptpb.ToolPresentation{}
				if err := proto.Unmarshal(encoded, decoded); err != nil {
					t.Fatal(err)
				}
				native, err := protoapi.ToolPresentationFromProto("exec_command", decoded)
				if err != nil {
					t.Fatal(err)
				}
				if native.ShellExitCode == nil || *native.ShellExitCode != status {
					t.Fatalf("exit status = %v, want %d", native.ShellExitCode, status)
				}
			}
		})
	}
}

func TestBackgroundExitStatusRoundTrips(t *testing.T) {
	for _, status := range []int{-1, 0, 255, 2147483648, 3221225477, 4294967295} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			activityID := uuid.New()
			events := []runtime.Event{
				{
					Kind: runtime.EventBackgroundUpdated,
					Background: &runtime.BackgroundShellEvent{
						Type: runtime.BackgroundShellEventCompleted, ID: "process-status",
						ActivityID: activityID, OwnerRunID: transcriptProjectionRunID,
						OwnerStepID: transcriptProjectionStepID, Command: "child",
						Workdir: "/tmp", ExitCode: &status,
					},
				},
				{
					Kind:                runtime.EventConversationUpdated,
					StepID:              runtimeStepIDPointer(transcriptProjectionStepID),
					CommittedProvenance: &runtime.TranscriptCommittedRowProvenance{EventSequence: 1},
					Message: llm.Message{
						Role: llm.RoleDeveloper, Name: textutil.Value("process-status"),
						MessageType: textutil.Value(llm.MessageTypeBackgroundNotice),
						Content:     textutil.Value("completed"), CompactContent: textutil.Value("completed"),
						BackgroundActivityID: textutil.Value(activityID.String()), BackgroundExitCode: &status,
					},
				},
			}
			for _, event := range events {
				messages, err := TranscriptMessagesFromRuntimeEventChecked(event)
				if err != nil {
					t.Fatal(err)
				}
				if len(messages) != 1 {
					t.Fatalf("expected one message: %v", messages)
				}
				encoded, err := proto.Marshal(messages[0])
				if err != nil {
					t.Fatal(err)
				}
				decoded := &transcriptpb.Event{}
				if err := proto.Unmarshal(encoded, decoded); err != nil {
					t.Fatal(err)
				}
				if err := protoapi.Validate(decoded); err != nil {
					t.Fatal(err)
				}
				if activity := decoded.GetBackgroundActivity(); activity != nil {
					if activity.ExitCode == nil || int64(*activity.ExitCode) != int64(status) {
						t.Fatalf("activity exit status = %v, want %d", activity.ExitCode, status)
					}
				} else {
					notice := decoded.GetCommittedRow().GetNotice().Background
					if notice.ExitCode == nil || int64(*notice.ExitCode) != int64(status) {
						t.Fatalf("notice exit status = %v, want %d", notice.ExitCode, status)
					}
				}
			}
		})
	}
}
