package workflowrunner

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"core/server/metadata"
	"core/server/session"
	"core/server/session/sessiontest"
	"core/server/workflow"
	"core/server/workflowruntime"
	"core/server/workflowstore"
	"core/shared/runtimeids"
)

type gatedMetadataSessionPersistence struct {
	sessions *sessiontest.Persistence
	metadata *metadata.Store
}

func (p gatedMetadataSessionPersistence) ObservePersistedStore(ctx context.Context, snapshot session.PersistedStoreSnapshot) error {
	if err := p.sessions.ObservePersistedStore(ctx, snapshot); err != nil {
		return err
	}
	return p.metadata.ImportSessionSnapshot(ctx, snapshot)
}

func (p gatedMetadataSessionPersistence) ObserveEventLogReconciliation(ctx context.Context, reconciliation session.PersistedEventLogReconciliation) error {
	if err := p.sessions.ObserveEventLogReconciliation(ctx, reconciliation); err != nil {
		return err
	}
	record, err := p.sessions.ResolvePersistedSession(ctx, reconciliation.SessionID)
	if err != nil {
		return err
	}
	if record.Meta == nil {
		return errors.New("reconciled session metadata is required")
	}
	return p.metadata.ImportSessionSnapshot(ctx, session.PersistedStoreSnapshot{
		SessionDir: record.SessionDir,
		Meta:       *record.Meta,
	})
}

func (p gatedMetadataSessionPersistence) ResolvePersistedSession(ctx context.Context, sessionID string) (session.PersistedSessionRecord, error) {
	return p.metadata.ResolvePersistedSession(ctx, sessionID)
}

func TestCurrentNodeSessionPolicyReusesTargetOwnedFanoutSession(t *testing.T) {
	sessionID := mustSessionID(t)
	policy, err := resolveCurrentNodeSessionPolicy(workflowstore.CurrentNodeStartContext{
		ContextMode:    workflow.ContextModeContinueSession,
		IsFanoutBranch: true,
		CurrentNode: workflow.CurrentNode{
			SessionID: &sessionID,
		},
		EnteringEdge: workflow.Edge{
			ContextSource: workflow.ContextSource{
				Kind: workflow.ContextSourcePreviousTargetOrNew,
			},
		},
	})
	if err != nil {
		t.Fatalf("resolveCurrentNodeSessionPolicy: %v", err)
	}
	if policy.cloneRetainedSession {
		t.Fatal("previous-target fan-out continuation must reuse its target-owned Session")
	}
	if policy.assignee != currentNodeSessionAssigneePreserve {
		t.Fatalf("previous-target assignee policy = %v, want preserve", policy.assignee)
	}
}

func TestPreparingRetainedContextPreservesItsIdentityAndMetadata(t *testing.T) {
	for _, source := range []workflow.ContextSourceKind{
		workflow.ContextSourceImmediateSource,
		workflow.ContextSourcePreviousTargetOrNew,
	} {
		t.Run(string(source), func(t *testing.T) {
			f, input := newMaterializedRoleSelectionStart(t)
			ctx := context.Background()
			ids, err := f.metadata.ListProjectSessionIDs(ctx, f.projectID)
			if err != nil || len(ids) != 2 {
				t.Fatalf("source Sessions = %v, %v", ids, err)
			}
			var id runtimeids.SessionID
			for _, candidate := range ids {
				parsed, err := runtimeids.ParseSessionID(candidate)
				if err != nil {
					t.Fatal(err)
				}
				if input.CurrentNode.SessionID == nil || parsed != *input.CurrentNode.SessionID {
					id = parsed
				}
			}
			before, err := f.metadata.ResolvePersistedSession(ctx, id.String())
			if err != nil {
				t.Fatal(err)
			}
			retained, err := session.Open(before.SessionDir, f.metadata.AuthoritativeSessionStoreOptions()...)
			if err != nil {
				t.Fatal(err)
			}
			if err := retained.MarkModelDispatchLocked(session.LockedContract{Model: "workflow-coder"}); err != nil {
				t.Fatal(err)
			}
			before, err = f.metadata.ResolvePersistedSession(ctx, id.String())
			if err != nil {
				t.Fatal(err)
			}
			input.CurrentNode.SessionID = &id
			input.SourceSessionID = &id
			input.ContextMode = workflow.ContextModeContinueSession
			input.EnteringEdge.ContextMode = workflow.ContextModeContinueSession
			input.EnteringEdge.ContextSource = workflow.ContextSource{Kind: source}
			input.IsFanoutBranch = source == workflow.ContextSourcePreviousTargetOrNew
			prepared, err := f.starter.PrepareCurrentNode(ctx, input, workflowruntime.TaskPromptDeliveryAssignment)
			if err != nil {
				t.Fatal(err)
			}
			if prepared.Session == nil || prepared.Session.SessionID != id || prepared.Session.Snapshot != nil {
				t.Fatalf("retained selection = %+v, want existing %s", prepared.Session, id)
			}
			after, err := f.metadata.ResolvePersistedSession(ctx, id.String())
			if err != nil || !reflect.DeepEqual(before.Meta, after.Meta) {
				t.Fatalf("preparation changed retained metadata: before=%+v after=%+v error=%v", before.Meta, after.Meta, err)
			}
			requests := f.runtimeRequests()
			if model := requests[len(requests)-1].ActiveSettings.Model; model != "workflow-coder" {
				t.Fatalf("retained model = %s, want source coder", model)
			}
		})
	}
}

func mustSessionID(t *testing.T) runtimeids.SessionID {
	t.Helper()
	return runtimeids.NewSessionID()
}
