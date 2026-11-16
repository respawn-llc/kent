package workflowsvc

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"core/internal/testharness/httpclient"
	"core/server/taskimport"
	worktree "core/server/worktreeservice"
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	workflowpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
	"google.golang.org/protobuf/proto"
)

func TestImportCreatesSourceLinkedTask(t *testing.T) {
	ctx, service, binding, metadataStore := newWorkflowServiceTestContextWithMetadata(t)
	workflowID := createWorkflowServiceValidWorkflow(t, ctx, service)
	linkDefaultWorkflowServiceProject(t, ctx, service, binding.ProjectID, workflowID)
	existingLabel, err := service.CreateWorkflowProjectLabel(ctx, &workflowpb.ProjectLabelCreateRequest{ProjectId: binding.ProjectID, Name: "P1"})
	if err != nil {
		t.Fatal(err)
	}
	var accountCalls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/user":
			accountCalls.Add(1)
			json.NewEncoder(w).Encode(map[string]string{"login": "import-account"})
		case "/repos/owner/repo/issues/1", "/repos/owner/repo/issues/2", "/repos/owner/repo/issues/3":
			json.NewEncoder(w).Encode(map[string]any{"title": "Original title", "body": "    code\n", "user": map[string]string{"login": "author"}, "author_association": "MEMBER", "labels": []any{map[string]string{"name": "missing"}}, "type": map[string]string{"name": "Custom type"}})
		case "/repos/owner/repo/issues/1/issue-field-values", "/repos/owner/repo/issues/2/issue-field-values", "/repos/owner/repo/issues/3/issue-field-values":
			json.NewEncoder(w).Encode([]any{map[string]any{"issue_field_name": "Priority", "data_type": "single_select", "single_select_option": map[string]string{"name": "High"}}})
		case "/repos/owner/repo/issues/1/comments":
			json.NewEncoder(w).Encode([]any{map[string]any{"body": "    comment code\n", "user": map[string]string{"login": "commenter"}}})
		case "/repos/owner/repo/issues/2/comments":
			w.Write([]byte("[]"))
		case "/repos/owner/repo/issues/3/comments":
			if r.URL.Query().Get("page") != "1" {
				w.WriteHeader(503)
				return
			}
			comments := make([]any, 100)
			for i := range comments {
				comments[i] = map[string]any{"body": "comment", "user": map[string]string{"login": "commenter"}}
			}
			json.NewEncoder(w).Encode(comments)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	target, _ := url.Parse(server.URL)
	importer := taskimport.New(metadataStore, service, worktree.NewGitInspector(nil), &http.Client{Transport: httpclient.NewURLRewriteTransport(target, http.DefaultTransport, "")}, func(string) (string, bool) { return "", false })
	t.Setenv("PATH", t.TempDir())
	var items []*taskimportpb.Item
	var accounts []*taskimportpb.Account
	request := &taskimportpb.Request{
		Cwd: binding.CanonicalRoot, Project: proto.String(binding.ProjectID),
		Mode:       taskimportpb.ImportMode_IMPORT_MODE_SKIP_EXISTING,
		References: []*taskimportpb.Reference{{Value: &taskimportpb.Reference_IssueUrl{IssueUrl: "https://github.com/owner/repo/issues/1"}}},
	}
	sink := serverapi.TaskImportProgressFunc(func(event *taskimportpb.ProgressEvent) {
		if account := event.GetAccount(); account != nil {
			accounts = append(accounts, account)
		}
		if item := event.GetItem(); item != nil {
			items = append(items, item)
		}
	})
	result, err := importer.ImportTasks(ctx, request, sink)
	if err != nil || result.Created != 1 || len(items) != 1 || items[0].Task == nil {
		t.Fatalf("%+v %+v %v", result, items, err)
	}
	detail, err := service.GetWorkflowTask(ctx, &taskpb.GetRequest{TaskId: proto.String(items[0].Task.TaskId)})
	if err != nil {
		t.Fatal(err)
	}
	if detail.Task.Summary.Title != "Original title" || detail.Task.GetSourceUrl() != "https://github.com/owner/repo/issues/1" || detail.Task.Body[:len("    code\n")] != "    code\n" {
		t.Fatalf("%+v", detail.Task)
	}
	if detail.Task.Status.Kind != taskpb.TaskStatusKind_TASK_STATUS_KIND_BACKLOG {
		t.Fatalf("%+v", detail.Task.Status)
	}
	comments, err := service.ListWorkflowTaskComments(ctx, &taskpb.TaskOffsetPageRequest{TaskId: items[0].Task.TaskId})
	if err != nil || len(comments.Items) != 1 || comments.Items[0].Body != "    comment code\n" || comments.Items[0].GetAuthorId() != "commenter" {
		t.Fatalf("%+v %v", comments, err)
	}
	labels, err := service.GetWorkflowTaskLabels(ctx, &taskpb.LabelsGetRequest{TaskId: items[0].Task.TaskId})
	if err != nil || len(labels.Assignment.LabelIds) != 1 || labels.Assignment.LabelIds[0] != existingLabel.Label.Id {
		t.Fatalf("%+v %v", labels, err)
	}
	catalog, err := service.ListWorkflowProjectLabels(ctx, &workflowpb.ProjectLabelCatalogRequest{ProjectId: binding.ProjectID})
	if err != nil || len(catalog.Catalog.Labels) != 1 {
		t.Fatalf("%+v %v", catalog, err)
	}
	originalID := items[0].Task.TaskId
	if len(accounts) != 0 || accountCalls.Load() != 0 {
		t.Fatal("anonymous import used account attribution")
	}
	items = nil
	request.References[0] = &taskimportpb.Reference{Value: &taskimportpb.Reference_IssueUrl{IssueUrl: "https://github.com/OWNER/REPO/issues/1"}}
	result, err = importer.ImportTasks(ctx, request, sink)
	if err != nil || result.Skipped != 1 || items[0].Task.TaskId != originalID {
		t.Fatalf("%+v %+v %v", result, items, err)
	}
	request.Mode = taskimportpb.ImportMode_IMPORT_MODE_FORCE_CREATE
	request.References[0] = &taskimportpb.Reference{Value: &taskimportpb.Reference_IssueUrl{IssueUrl: "https://github.com/owner/repo/issues/1"}}
	request.CreateLabels = true
	items = nil
	result, err = importer.ImportTasks(ctx, request, sink)
	if err != nil || result.Created != 1 || items[0].Task.TaskId == originalID {
		t.Fatalf("%+v %+v %v", result, items, err)
	}
	original, err := service.GetWorkflowTask(ctx, &taskpb.GetRequest{TaskId: proto.String(originalID)})
	if err != nil || !proto.Equal(original, detail) {
		t.Fatalf("original changed: %v", err)
	}
	catalog, err = service.ListWorkflowProjectLabels(ctx, &workflowpb.ProjectLabelCatalogRequest{ProjectId: binding.ProjectID})
	if err != nil || len(catalog.Catalog.Labels) != 3 {
		t.Fatalf("%+v %v", catalog, err)
	}

	readModel := service.readModels.TaskDetail
	service.readModels.TaskDetail = &failOnceImportDetail{WorkflowTaskDetailReadModel: readModel}
	request.References = append(request.References, &taskimportpb.Reference{Value: &taskimportpb.Reference_IssueUrl{IssueUrl: "https://github.com/owner/repo/issues/2"}})
	items = nil
	result, err = importer.ImportTasks(ctx, request, sink)
	if err != nil || result.Failed != 1 || result.Created != 1 || len(items) != 2 || items[0].Task == nil || items[0].Task.TaskId == originalID || items[0].GetFailed().Phase != taskimportpb.FailurePhase_FAILURE_PHASE_CREATION {
		t.Fatalf("%+v %+v %v", result, items, err)
	}
	committed, err := readModel.GetTask(ctx, items[0].Task.TaskId)
	if err != nil || committed.Summary.ShortId != items[0].Task.ShortId {
		t.Fatalf("%+v %v", committed, err)
	}
	request.References = []*taskimportpb.Reference{
		{Value: &taskimportpb.Reference_IssueUrl{IssueUrl: "https://github.com/owner/repo/issues/3"}},
		{Value: &taskimportpb.Reference_IssueUrl{IssueUrl: "https://github.com/owner/repo/not-an-issue"}},
		{Value: &taskimportpb.Reference_IssueUrl{IssueUrl: "https://github.com/owner/repo/issues/99"}},
		{Value: &taskimportpb.Reference_IssueUrl{IssueUrl: "https://github.com/owner/repo/issues/2"}},
	}
	request.Repository = proto.String("owner/repo")
	items = nil
	result, err = importer.ImportTasks(ctx, request, sink)
	if err != nil || result.Failed != 3 || result.Created != 1 || len(items) != 4 || items[0].Task == nil || items[0].GetFailed().Phase != taskimportpb.FailurePhase_FAILURE_PHASE_COMMENTS || items[1].Task != nil {
		t.Fatalf("%+v %+v %v", result, items, err)
	}
	counted, err := service.ListWorkflowTaskComments(ctx, &taskpb.TaskOffsetPageRequest{TaskId: items[0].Task.TaskId})
	if err != nil || counted.TotalCount != 100 {
		t.Fatalf("%+v %v", counted, err)
	}
	request.Mode = taskimportpb.ImportMode_IMPORT_MODE_SKIP_EXISTING
	request.Credentials = &taskimportpb.CredentialCandidates{EnvironmentToken: proto.String("fake")}
	items = nil
	result, err = importer.ImportTasks(ctx, request, sink)
	if err != nil || len(accounts) != 1 || accountCalls.Load() != 1 || accounts[0].Login != "import-account" {
		t.Fatalf("%+v %v", result, err)
	}
}

type failOnceImportDetail struct {
	WorkflowTaskDetailReadModel
	failed bool
}

func TestImportPreservesWorkflowSelection(t *testing.T) {
	for _, reason := range []taskpb.CreateSelectionReason{
		taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_NO_LINKED_WORKFLOWS,
		taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_WORKFLOW_NOT_LINKED,
		taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_AMBIGUOUS_WITHOUT_DEFAULT,
	} {
		t.Run(reason.String(), func(t *testing.T) {
			ctx, service, binding, store := newWorkflowServiceTestContextWithMetadata(t)
			request := &taskimportpb.Request{
				Cwd: binding.CanonicalRoot, Project: proto.String(binding.ProjectID),
				Mode:       taskimportpb.ImportMode_IMPORT_MODE_SKIP_EXISTING,
				References: []*taskimportpb.Reference{{Value: &taskimportpb.Reference_IssueUrl{IssueUrl: "https://github.com/owner/repo/issues/1"}}},
			}
			switch reason {
			case taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_WORKFLOW_NOT_LINKED:
				request.WorkflowId = proto.String(createWorkflowServiceValidWorkflow(t, ctx, service).String())
			case taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_AMBIGUOUS_WITHOUT_DEFAULT:
				for range 2 {
					id := createWorkflowServiceValidWorkflow(t, ctx, service)
					linkWorkflowServiceProject(t, ctx, service, &workflowpb.LinkProjectRequest{
						ProjectId: binding.ProjectID, WorkflowId: id.String(),
						DefaultPolicy: workflowpb.ProjectLinkDefaultMode_WORKFLOW_PROJECT_LINK_DEFAULT_MODE_NEVER.Enum(),
					})
				}
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/repos/owner/repo/issues/1":
					json.NewEncoder(w).Encode(map[string]any{"title": "Issue", "user": map[string]string{"login": "author"}})
				case "/repos/owner/repo/issues/1/issue-field-values":
					w.Write([]byte("[]"))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			target, err := url.Parse(server.URL)
			if err != nil {
				t.Fatal(err)
			}
			importer := taskimport.New(store, service, worktree.NewGitInspector(nil), &http.Client{Transport: httpclient.NewURLRewriteTransport(target, http.DefaultTransport, "")}, func(string) (string, bool) { return "", false })
			t.Setenv("PATH", t.TempDir())
			var item *taskimportpb.Item
			result, err := importer.ImportTasks(ctx, request, serverapi.TaskImportProgressFunc(func(event *taskimportpb.ProgressEvent) { item = event.GetItem() }))
			if err != nil || result.Failed != 1 || item.Task != nil {
				t.Fatalf("%+v %+v %v", result, item, err)
			}
			selection := item.GetFailed().GetWorkflowSelection()
			if selection == nil || selection.Reason != reason || selection.ProjectId != binding.ProjectID || !proto.Equal(&taskpb.CreateSelectionDetails{WorkflowId: selection.WorkflowId}, &taskpb.CreateSelectionDetails{WorkflowId: request.WorkflowId}) {
				t.Fatalf("lost selection facts: %+v", item)
			}
		})
	}
}

func (d *failOnceImportDetail) GetTask(ctx context.Context, id string) (*taskpb.TaskDetail, error) {
	if !d.failed {
		d.failed = true
		return nil, errors.New("projection failed after commit")
	}
	return d.WorkflowTaskDetailReadModel.GetTask(ctx, id)
}
