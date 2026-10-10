package session_test

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"testing"

	"core/server/metadata"
	"core/server/session"
	"core/shared/rollbacktarget"
	"core/shared/sessioncontract"
	"core/shared/textutil"
)

func TestCompactedOutputSurvivesReopenAndClone(t *testing.T) {
	for _, engine := range []session.CompactionEngine{session.CompactionEngineLocal, session.CompactionEngineRemote} {
		t.Run(string(engine), func(t *testing.T) {
			root, workspace := t.TempDir(), t.TempDir()
			persistence, err := metadata.Open(root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = persistence.Close() })
			binding, err := persistence.RegisterWorkspaceBinding(t.Context(), workspace)
			if err != nil {
				t.Fatal(err)
			}
			options := persistence.AuthoritativeSessionStoreOptions()
			store, err := session.Create(filepath.Join(root, "projects", binding.ProjectID, "sessions"),
				"sessions", workspace, sessioncontract.SessionCategoryMain, options...)
			if err != nil {
				t.Fatal(err)
			}
			log, err := store.MaterializeEventLog()
			if err != nil {
				t.Fatal(err)
			}
			prompt := "continue the work"
			user, err := log.AppendRecordWithEndByteCursor(nil, session.MessageRecord{Role: session.MessageRoleUser, Content: &prompt})
			if err != nil {
				t.Fatal(err)
			}
			assignmentType, assignment := session.MessageTypeWorkflowMode, "source assignment"
			if _, _, err := log.AppendRecord(nil, session.MessageRecord{
				Role: session.MessageRoleDeveloper, MessageType: &assignmentType, Content: &assignment,
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := store.SetUsageState(&session.UsageState{InputTokens: 42}); err != nil {
				t.Fatal(err)
			}
			if err := store.MarkModelDispatchLocked(session.LockedContract{Model: "outgoing-model"}); err != nil {
				t.Fatal(err)
			}
			if err := store.AdoptOriginalThinkingEffort("medium"); err != nil {
				t.Fatal(err)
			}
			content, final := "completed source summary", "source done"
			role := session.MessageRoleUser
			summaryType := session.MessageTypeCompactionSummary
			item := session.ProviderHistoryItem{
				Type: session.ProviderHistoryItemTypeMessage, Role: &role,
				MessageType: &summaryType, Content: &content,
				Raw: json.RawMessage(`{"type":"message","role":"user","content":[{"type":"input_text","text":"completed source summary"}]}`),
			}
			if engine == session.CompactionEngineRemote {
				checkpoint := "encrypted-checkpoint"
				item = session.ProviderHistoryItem{
					Type: session.ProviderHistoryItemTypeCompaction, EncryptedContent: &checkpoint,
					Raw: json.RawMessage(`{"type":"compaction","encrypted_content":"encrypted-checkpoint"}`),
				}
			}
			carryoverType := session.MessageTypeCompactionPreservedUserMessage
			record := session.HistoryReplacementRecord{
				Engine: string(engine), Mode: session.CompactionModeWorkflowPostCompletion,
				CompactionNumber: textutil.Value(1), CommittedEntryStart: textutil.Value(5),
				CompactedOutput: &session.CompactedOutput{
					Summary:              []session.ProviderHistoryItem{item},
					PreservedUserMessage: &session.MessageRecord{Role: session.MessageRoleDeveloper, MessageType: &carryoverType, Content: &prompt},
				},
				LastCommittedAssistantFinalAnswer: &final,
				LatestRollbackCandidate: &rollbacktarget.CandidateLocator{
					UserMessageSeq: user.Record.Seq(), CandidatePageEndByte: *user.EndByteCursor,
				},
			}
			invalid := record
			invalid.CompactionNumber = textutil.Value(0)
			if _, receipt, err := log.AppendHistoryReplacement(nil, invalid); err == nil || receipt.Committed {
				t.Fatalf("invalid pending summary committed: receipt=%+v err=%v", receipt, err)
			}
			if meta := store.Meta(); meta.Locked == nil || meta.OriginalThinkingEffort == nil || meta.UsageState == nil {
				t.Fatal("rejected summary reset outgoing context")
			}
			appended, receipt, err := log.AppendHistoryReplacement(nil, record)
			if err != nil || !receipt.Committed {
				t.Fatalf("append: receipt=%+v err=%v", receipt, err)
			}
			meta := store.Meta()
			if meta.UsageState != nil || meta.Locked != nil || meta.OriginalThinkingEffort != nil {
				t.Fatal("pending summary did not reset outgoing context metadata")
			}
			activeAssignment, err := store.ActiveWorkflowAssignmentProjection()
			if err != nil || activeAssignment != nil {
				t.Fatalf("completed assignment remains active: %v %v", activeAssignment, err)
			}
			want, err := appended.Payload()
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := session.Open(store.Dir(), options...)
			if err != nil {
				t.Fatal(err)
			}
			reopenedLog, err := reopened.MaterializeEventLog()
			if err != nil {
				t.Fatal(err)
			}
			assertPendingCompactedOutput(t, reopenedLog, want)
			child, err := session.CloneSession(reopenedLog, "copy", sessioncontract.SessionCategoryMain,
				session.ForkThinking{Desired: "medium", PreserveNativeUpdates: true})
			if err != nil {
				t.Fatal(err)
			}
			assertCopiedPendingCompactedOutput(t, child, want.(session.HistoryReplacementRecord))
			target, _, err := reopenedLog.AppendRecord(nil, session.MessageRecord{Role: session.MessageRoleUser, Content: &prompt})
			if err != nil {
				t.Fatal(err)
			}
			fork, _, err := session.ForkAtUserMessage(reopenedLog, target.Seq(), "fork",
				sessioncontract.SessionCategoryMain, session.ForkThinking{Desired: "medium", PreserveNativeUpdates: true})
			if err != nil {
				t.Fatal(err)
			}
			assertCopiedPendingCompactedOutput(t, fork, want.(session.HistoryReplacementRecord))
			if !reopened.Meta().ConversationEstablished {
				t.Fatal("pending compaction must retain established conversation")
			}
		})
	}
}

func assertCopiedPendingCompactedOutput(t *testing.T, store *session.Store, want session.HistoryReplacementRecord) {
	t.Helper()
	log, err := store.MaterializeEventLog()
	if err != nil {
		t.Fatal(err)
	}
	assignment, err := store.ActiveWorkflowAssignmentProjection()
	if err != nil || assignment != nil {
		t.Fatalf("copy restored completed assignment: %v %v", assignment, err)
	}
	candidateWindow, err := log.ReadSegmentForward(0, func(record session.EventRecord) bool {
		kind, err := record.Kind()
		if err != nil {
			t.Fatal(err)
		}
		return kind == session.EventKindHistoryReplace
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate := *want.LatestRollbackCandidate
	candidate.CandidatePageEndByte = candidateWindow.EndOffset
	want.LatestRollbackCandidate = &candidate
	assertPendingCompactedOutput(t, log, want)
}

func assertPendingCompactedOutput(t *testing.T, log session.MaterializedEventLog, want session.EventRecordPayload) {
	t.Helper()
	window, err := log.ReadNewestSegmentBackward(func(record session.EventRecord) bool {
		kind, err := record.Kind()
		if err != nil {
			t.Fatal(err)
		}
		return kind == session.EventKindHistoryReplace
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(window.Records) != 1 {
		t.Fatalf("active context includes %d records, want only pending summary", len(window.Records))
	}
	got, err := window.Records[0].Payload()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		if pending, ok := got.(session.HistoryReplacementRecord); ok {
			t.Logf("rollback candidate: got=%+v want=%+v", pending.LatestRollbackCandidate, want.(session.HistoryReplacementRecord).LatestRollbackCandidate)
		}
		t.Fatalf("pending summary changed: got=%+v want=%+v", got, want)
	}
}
