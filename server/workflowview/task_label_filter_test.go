package workflowview

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"

	"core/server/metadata/sqlitegen"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
)

type workflowProjectLabelByIDReaderStub struct {
	rows []sqlitegen.ListProjectLabelsByIDsRow
}

func (r *workflowProjectLabelByIDReaderStub) ListProjectLabelsByIDs(_ context.Context, labelIDs []string) ([]sqlitegen.ListProjectLabelsByIDsRow, error) {
	return append([]sqlitegen.ListProjectLabelsByIDsRow(nil), r.rows...), nil
}

func TestResolveWorkflowTaskLabelFilterCanonicalizesIncludedAndExcludedPartitions(t *testing.T) {
	const (
		projectID = "project-1"
		alphaID   = "00000000-0000-4000-8000-000000000001"
		betaID    = "00000000-0000-4000-8000-000000000002"
		gammaID   = "00000000-0000-4000-8000-000000000003"
		deltaID   = "00000000-0000-4000-8000-000000000004"
	)
	reader := &workflowProjectLabelByIDReaderStub{
		rows: []sqlitegen.ListProjectLabelsByIDsRow{
			{ID: betaID, ProjectID: projectID},
			{ID: alphaID, ProjectID: projectID},
			{ID: deltaID, ProjectID: projectID},
			{ID: gammaID, ProjectID: projectID},
		},
	}

	facts, err := resolveWorkflowTaskLabelFilter(
		t.Context(),
		reader,
		projectID, &taskpb.
			LabelFilter{Filter: &taskpb.LabelFilter_Named{Named: &taskpb.NamedLabelFilter{
			Mode:             taskpb.NamedLabelFilterMode_NAMED_LABEL_FILTER_MODE_ANY,
			LabelIds:         []string{deltaID, betaID},
			ExcludedLabelIds: []string{gammaID, alphaID},
		}},
		},
	)
	if err != nil {
		t.Fatalf("resolveWorkflowTaskLabelFilter: %v", err)
	}
	wantIDs := []string{betaID, deltaID}
	wantExcludedIDs := []string{alphaID, gammaID}
	var included, excluded []string
	if err := json.Unmarshal([]byte(facts.labelIDsJSON), &included); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(facts.excludedLabelIDsJSON), &excluded); err != nil {
		t.Fatal(err)
	}
	if facts.kind != "named" || !facts.mode.Valid || facts.mode.String != "any" ||
		!reflect.DeepEqual(included, wantIDs) ||
		!reflect.DeepEqual(excluded, wantExcludedIDs) {
		t.Fatalf("resolved facts = %+v, want named any included %v excluded %v", facts, wantIDs, wantExcludedIDs)
	}
	noneArgs, err := resolveWorkflowTaskLabelFilter(t.Context(), reader, projectID, noLabelFilter())
	if err != nil {
		t.Fatalf("none query args: %v", err)
	}
	if noneArgs.mode.Valid {
		t.Fatalf("none mode query arg = %+v, want SQL null", noneArgs.mode)
	}
}

func TestResolveWorkflowTaskLabelFilterRetainsTypedMissingAndWrongProjectErrorsAcrossPartitions(t *testing.T) {
	const (
		projectID = "project-1"
		labelID   = "00000000-0000-4000-8000-000000000001"
	)
	filter := &taskpb.LabelFilter{Filter: &taskpb.LabelFilter_Named{Named: &taskpb.NamedLabelFilter{
		Mode:             taskpb.NamedLabelFilterMode_NAMED_LABEL_FILTER_MODE_ANY,
		ExcludedLabelIds: []string{labelID},
	}},
	}
	for _, tt := range []struct {
		name   string
		rows   []sqlitegen.ListProjectLabelsByIDsRow
		reason serverapi.WorkflowLabelErrorReason
	}{
		{
			name:   "missing label",
			reason: serverapi.WorkflowLabelErrorReasonLabelNotFound,
		},
		{
			name: "wrong project label",
			rows: []sqlitegen.ListProjectLabelsByIDsRow{{
				ID:        labelID,
				ProjectID: "other-project",
			}},
			reason: serverapi.WorkflowLabelErrorReasonWrongProject,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := resolveWorkflowTaskLabelFilter(t.Context(), &workflowProjectLabelByIDReaderStub{rows: tt.rows}, projectID, filter)
			var typed *serverapi.WorkflowLabelError
			if !errors.As(err, &typed) || typed.Reason != tt.reason {
				t.Fatalf("resolveWorkflowTaskLabelFilter() error = %T %+v, want %q", err, err, tt.reason)
			}
		})
	}
}
