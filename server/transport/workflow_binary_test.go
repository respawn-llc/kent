package transport

import (
	"database/sql"
	"errors"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"core/server/core"
	"core/server/metadata"
	"core/server/metadata/sqlitegen"
	remoteclient "core/shared/client"
	"core/shared/protoapi"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/protocol"
	"core/shared/serverapi"
	"core/shared/worktreecontract"

	"github.com/google/uuid"
	"google.golang.org/protobuf/proto"
)

func TestTaskDeleteBlockedWorktreeRoundTrip(t *testing.T) {
	app, server := newGatewayTestServer(t)
	first := createGatewaySearchableTask(t, app)
	second, err := app.WorkflowClient().CreateWorkflowTask(t.Context(), &taskpb.CreateRequest{
		ProjectId: app.ProjectID(), Title: "Other Task managing the same Worktree",
	})
	if err != nil {
		t.Fatal(err)
	}
	store := app.MetadataStore()
	workspace, err := store.ResolveProjectSourceWorkspace(t.Context(), app.ProjectID())
	if err != nil {
		t.Fatal(err)
	}
	worktreeID := uuid.NewString()
	if err := store.UpsertWorktreeRecord(t.Context(), metadata.WorktreeRecord{
		ID: worktreeID, WorkspaceID: workspace.ID, CanonicalRoot: t.TempDir(), Managed: true,
	}); err != nil {
		t.Fatal(err)
	}
	for _, taskID := range []string{first.Id, second.Task.Id} {
		updated, err := store.Queries().BindInitialTaskManagedWorktree(t.Context(), sqlitegen.BindInitialTaskManagedWorktreeParams{
			TaskID: taskID, ManagedWorktreeID: sql.NullString{String: worktreeID, Valid: true},
			UpdatedAtUnixMs: time.Now().UnixMilli(),
		})
		if err != nil || updated != 1 {
			t.Fatalf("bind managed Worktree: rows=%d error=%v", updated, err)
		}
	}
	remote, err := remoteclient.DialRemoteURL(t.Context(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = remote.Close() })
	_, err = remote.DeleteWorkflowTask(t.Context(), &taskpb.DeleteRequest{TaskId: first.Id})
	var blocked *worktreecontract.BlockedError
	if !errors.Is(err, worktreecontract.ErrWorktreeBlocked) || !errors.As(err, &blocked) || blocked.Details == nil {
		t.Fatalf("Task deletion lost the Worktree blocker: %T %v", err, err)
	}
	for _, expected := range []*taskpb.TaskSummary{first, second.Task} {
		retained, err := remote.GetWorkflowTask(t.Context(), &taskpb.GetRequest{TaskId: proto.String(expected.Id)})
		if err != nil || retained.GetTask().GetSummary().GetTitle() != expected.Title {
			t.Fatalf("blocked deletion changed Task %q: %v, %v", expected.Id, retained, err)
		}
	}
}

type workflowCoreGate struct {
	*core.Core
	failure error
}

func TestWorkflowBinaryMissingCustomRefDraftRoundTrip(t *testing.T) {
	app, server := newGatewayTestServer(t)
	defer server.Close()
	defer func() { _ = app.Close() }()
	remote, err := remoteclient.DialRemoteURL(t.Context(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = remote.Close() }()
	created, err := remote.CreateWorkflow(t.Context(), &pb.CreateRequest{Name: "Custom ref Draft"})
	if err != nil {
		t.Fatal(err)
	}
	current, err := remote.GetWorkflow(t.Context(), &pb.GetRequest{WorkflowId: created.Workflow.Id})
	if err != nil {
		t.Fatal(err)
	}
	graph := protoapi.WorkflowGraphDraftFromDefinition(current.Definition)
	metadata := &pb.GraphMetadata{
		Name:                  created.Workflow.Name,
		ExecutionTargetPolicy: &pb.ExecutionTargetConfiguration{Mode: pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_CUSTOM_REF},
	}
	preview, err := remote.PreviewWorkflowGraphSave(t.Context(), &pb.GraphSavePreviewRequest{
		WorkflowId: created.Workflow.Id, ExpectedVersion: created.Workflow.Version, Metadata: metadata, Graph: graph,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !preview.CanSave {
		t.Fatalf("missing-ref Draft cannot be saved: %v", preview)
	}
	requireMissingCustomRefDiagnostic(t, preview.ValidationResults)
	saved, err := remote.SaveWorkflowGraph(t.Context(), &pb.GraphSaveRequest{
		WorkflowId: created.Workflow.Id, ExpectedVersion: created.Workflow.Version, Metadata: metadata, Graph: graph,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Saved || saved.Definition == nil {
		t.Fatalf("Draft save = %v", saved)
	}
	requireMissingCustomRefDiagnostic(t, saved.ValidationResults)
	reopened, err := remote.GetWorkflow(t.Context(), &pb.GetRequest{WorkflowId: created.Workflow.Id})
	if err != nil {
		t.Fatal(err)
	}
	listed, err := remote.ListWorkflows(t.Context(), &pb.ListRequest{WorkflowId: &created.Workflow.Id})
	if err != nil || len(listed.GetWorkflows()) != 1 {
		t.Fatalf("List Draft = %v, %v", listed, err)
	}
	for _, record := range []*pb.WorkflowRecord{saved.Definition.Workflow, reopened.Definition.Workflow, listed.Workflows[0]} {
		if record.ExecutionTargetPolicy.Mode != pb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_CUSTOM_REF || record.ExecutionTargetPolicy.CustomRef != nil {
			t.Fatalf("missing-ref policy changed: %v", record.ExecutionTargetPolicy)
		}
	}
}

func requireMissingCustomRefDiagnostic(t *testing.T, results []*pb.ModeValidationResult) {
	t.Helper()
	for _, result := range results {
		for _, diagnostic := range result.Result.Errors {
			if diagnostic.Code == pb.ValidationErrorCode_VALIDATION_ERROR_CODE_EXECUTION_TARGET_CUSTOM_REF_REQUIRED {
				return
			}
		}
	}
	t.Fatalf("missing custom-ref diagnostic: %v", results)
}

func TestWorkflowBinaryLabelsInteroperateWithTaskDetails(t *testing.T) {
	app, server := newGatewayTestServer(t)
	defer server.Close()
	defer func() { _ = app.Close() }()
	task := createGatewaySearchableTask(t, app)
	remote, err := remoteclient.DialRemoteURL(t.Context(), "ws"+server.URL[len("http"):])
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = remote.Close() }()
	created, err := remote.CreateWorkflowProjectLabel(t.Context(), &pb.ProjectLabelCreateRequest{ProjectId: app.ProjectID(), Name: "Priority"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = remote.CreateWorkflowProjectLabel(t.Context(), &pb.ProjectLabelCreateRequest{ProjectId: app.ProjectID(), Name: "priority"})
	var failure *remoteclient.WorkflowLabelError
	if !errors.As(err, &failure) {
		t.Fatalf("duplicate label failure = %v", err)
	}
	if detail, ok := failure.Detail.(*pb.LabelNameConflictDetails); !ok || detail.ProjectId != app.ProjectID() {
		t.Fatalf("duplicate label detail = %v", failure.Detail)
	}
	updated, err := remote.UpdateWorkflowTaskLabels(t.Context(), &taskpb.LabelsUpdateRequest{
		TaskId: task.Id, AddLabelIds: []string{created.Label.Id},
	})
	if err != nil {
		t.Fatal(err)
	}
	assignment, err := remote.GetWorkflowTaskLabels(t.Context(), &taskpb.LabelsGetRequest{TaskId: task.Id})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := remote.GetWorkflowTask(t.Context(), &taskpb.GetRequest{TaskId: proto.String(task.Id)})
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(updated.Assignment.LabelIds, []string{created.Label.Id}) ||
		!slices.Equal(assignment.Assignment.LabelIds, updated.Assignment.LabelIds) ||
		!slices.Equal(detail.Task.LabelIds, updated.Assignment.LabelIds) {
		t.Fatalf("label assignments disagree: %v, %v, %v", updated, assignment, detail.Task.LabelIds)
	}
	normalized, err := remote.CreateWorkflowProjectLabel(t.Context(), &pb.ProjectLabelCreateRequest{
		ProjectId: app.ProjectID(), Name: " " + strings.Repeat("e\u0301", 64) + " ",
	})
	if err != nil || normalized.GetLabel().GetName() != strings.Repeat("é", 64) {
		t.Fatalf("normalized label at limit = %v, %v", normalized, err)
	}
	renamed, err := remote.RenameWorkflowProjectLabel(t.Context(), &pb.ProjectLabelRenameRequest{
		ProjectId: app.ProjectID(), LabelId: normalized.Label.Id, Name: " " + strings.Repeat("a\u0301", 64) + " ",
	})
	if err != nil || renamed.GetLabel().GetName() != strings.Repeat("á", 64) {
		t.Fatalf("renamed normalized label at limit = %v, %v", renamed, err)
	}
}

func TestWorkflowBinaryRejectsWrongEncodingAndMalformedPayloadWithoutClosingConnection(t *testing.T) {
	app, server := newGatewayTestServer(t)
	defer server.Close()
	defer func() { _ = app.Close() }()
	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	failure := callGatewayExpectError(t, conn, "old-workflow-list", "workflow.list", struct{}{})
	if failure.Code != protocol.ErrCodeMethodNotFound {
		t.Fatalf("obsolete encoding = %v", failure)
	}
	method := pb.File_kent_api_workflow_definition_workflow_definition_proto.Services().ByName("WorkflowDefinitionService").Methods().ByName("List")
	envelope := callGatewayDescriptorPayload(t, conn, "malformed-workflow", method, []byte{0xff})
	if envelope.GetTransportFailure().GetCode() != sharedpb.TransportFailureCode_TRANSPORT_FAILURE_CODE_INVALID_PAYLOAD {
		t.Fatalf("malformed payload = %v", envelope)
	}
	var valid pb.ListResult
	callGatewayDescriptor(t, conn, "valid-workflow-list", method, &pb.ListRequest{}, &valid)
	if valid.GetSuccess() == nil {
		t.Fatalf("valid call after rejected payloads = %v", &valid)
	}
}

func (gate *workflowCoreGate) RequireCoreActive() error { return gate.failure }

func TestWorkflowBinaryAuthenticationAndReadiness(t *testing.T) {
	for _, test := range []struct {
		name      string
		readiness error
		want      error
	}{
		{name: "active unauthenticated", want: serverapi.ErrServerAuthRequired},
		{name: "onboarding required", readiness: serverapi.NewServerNotReadyError(serverapi.ServerNotReadyOnboardingRequired, nil, nil), want: serverapi.ErrServerNotReadyOnboardingRequired},
		{name: "activation failed", readiness: serverapi.NewServerNotReadyError(serverapi.ServerNotReadyActivationFailed, nil, errors.New("activation failed")), want: serverapi.ErrServerNotReadyActivationFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, _ := newGatewayTestCore(t, true, false)
			defer func() { _ = app.Close() }()
			gateway, err := NewGateway(&workflowCoreGate{Core: app, failure: test.readiness}, gatewayTestIdentity())
			if err != nil {
				t.Fatal(err)
			}
			server := httptest.NewServer(gateway.Handler())
			defer server.Close()
			remote, err := remoteclient.DialRemoteURL(t.Context(), "ws"+server.URL[len("http"):])
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = remote.Close() }()
			_, err = remote.CreateWorkflow(t.Context(), &pb.CreateRequest{Name: "Gated"})
			if !errors.Is(err, test.want) {
				t.Fatalf("Create failure = %v, want %v", err, test.want)
			}
			_, err = remote.UpdateWorkflowTaskLabels(t.Context(), &taskpb.LabelsUpdateRequest{TaskId: "task-1"})
			if !errors.Is(err, test.want) {
				t.Fatalf("Task label update failure = %v, want %v", err, test.want)
			}
			list, err := remote.ListWorkflows(t.Context(), &pb.ListRequest{})
			if test.readiness == nil {
				if err != nil || list == nil {
					t.Fatalf("active unauthenticated List = %v, %v", list, err)
				}
			} else if !errors.Is(err, test.want) {
				t.Fatalf("List failure = %v, want %v", err, test.want)
			}
		})
	}
}
