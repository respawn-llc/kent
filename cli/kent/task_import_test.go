package main

import (
	"bytes"
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"testing"
)

func TestImportJSONPreservesWorkflowSelection(t *testing.T) {
	var stdout, stderr bytes.Buffer
	output := importOutput{stdout: &stdout, stderr: &stderr, json: true}
	expected := &taskpb.CreateSelectionDetails{
		ProjectId: "project", Reason: taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_AMBIGUOUS_WITHOUT_DEFAULT,
	}
	output.PublishTaskImportProgress(&taskimportpb.ProgressEvent{Payload: &taskimportpb.ProgressEvent_Item{Item: &taskimportpb.Item{
		Outcome: &taskimportpb.Item_Failed{Failed: &taskimportpb.Failure{
			Code:  taskimportpb.FailureCode_FAILURE_CODE_WORKFLOW_SELECTION,
			Phase: taskimportpb.FailurePhase_FAILURE_PHASE_CREATION, Diagnostic: "selection failed", WorkflowSelection: expected,
		}},
	}}})
	if output.finish(&taskimportpb.Success{Failed: 1}, nil) != 1 {
		t.Fatal("selection failure succeeded")
	}
	var document struct {
		Results []json.RawMessage `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if len(document.Results) != 1 {
		t.Fatalf("%+v", document)
	}
	item := &taskimportpb.Item{}
	if err := protojson.Unmarshal(document.Results[0], item); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(item.GetFailed().GetWorkflowSelection(), expected) {
		t.Fatalf("%+v", item)
	}
}

func TestImportWorkflowSelectionRecovery(t *testing.T) {
	for _, reason := range []taskpb.CreateSelectionReason{
		taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_NO_LINKED_WORKFLOWS,
		taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_WORKFLOW_NOT_LINKED,
		taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_AMBIGUOUS_WITHOUT_DEFAULT,
	} {
		details := &taskpb.CreateSelectionDetails{Reason: reason, ProjectId: "project"}
		if reason == taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_WORKFLOW_NOT_LINKED {
			details.WorkflowId = proto.String("2049d357-432c-4a59-b128-24e732458b74")
		}
		request := &taskimportpb.Request{Mode: taskimportpb.ImportMode_IMPORT_MODE_FORCE_CREATE, CreateLabels: true, SourceWorkspace: proto.String("workspace")}
		recovery, err := taskImportRecoveryForSelectionError(details, taskImportCommandContext{
			Request: request, SourceURL: "https://github.com/owner/repo/issues/1",
		})
		if err != nil {
			t.Fatal(err)
		}
		commands := map[taskWorkflowRecoveryCommandKind]bool{}
		for _, command := range recovery.Commands {
			commands[command.Kind] = true
			if command.Kind == taskWorkflowRecoveryCommandRetryTaskImport && (len(command.Args) < 4 || command.Args[2] != "import" || command.Args[3] != "https://github.com/owner/repo/issues/1") {
				t.Fatalf("incorrect import retry: %+v", command)
			}
		}
		if !commands[taskWorkflowRecoveryCommandRetryTaskImport] {
			t.Fatalf("missing import guidance: %+v", recovery)
		}
		if reason == taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_NO_LINKED_WORKFLOWS {
			if !commands[taskWorkflowRecoveryCommandCreateWorkflow] || !commands[taskWorkflowRecoveryCommandLinkExistingWorkflow] {
				t.Fatalf("%+v", recovery)
			}
		} else if !commands[taskWorkflowRecoveryCommandListProjectWorkflows] {
			t.Fatalf("%+v", recovery)
		}
	}
}

func TestImportJSONReportsKnownFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	output := importOutput{stdout: &stdout, stderr: &stderr, json: true}
	failure := &serverapi.TaskImportError{Detail: &taskimportpb.Failure{Phase: taskimportpb.FailurePhase_FAILURE_PHASE_CREDENTIALS, Code: taskimportpb.FailureCode_FAILURE_CODE_AUTHENTICATION, Diagnostic: "credential rejected"}}
	if output.finish(nil, failure) != 1 {
		t.Fatal("rejected import succeeded")
	}
	var document struct {
		Error            *json.RawMessage `json:"error"`
		ObservationError *string          `json:"observation_error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.Error == nil || document.ObservationError != nil {
		t.Fatalf("%+v", document)
	}
}

func TestImportReferenceGrammar(t *testing.T) {
	refs, err := parseImportReferences([]string{"1,3-5", "https://github.com/owner/repo/issues/9"}, true)
	if err != nil || len(refs) != 3 || refs[0].GetNumber() != 1 || refs[1].GetRange().Last != 5 || refs[2].GetIssueUrl() == "" {
		t.Fatalf("%+v %v", refs, err)
	}
	for _, invalid := range []string{"0", "5-3", "1-x", "", "not-url"} {
		if _, err := parseImportReferences([]string{invalid}, true); err == nil {
			t.Fatalf("accepted %q", invalid)
		}
	}
	if _, err := parseImportReferences([]string{"1"}, false); err == nil {
		t.Fatal("generic import accepted bare number")
	}
}

func TestImportJSONMixedOutcomes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	output := importOutput{stdout: &stdout, stderr: &stderr, json: true}
	output.PublishTaskImportProgress(&taskimportpb.ProgressEvent{Payload: &taskimportpb.ProgressEvent_Account{Account: &taskimportpb.Account{Login: "selected-account"}}})
	identity := &taskimportpb.TaskIdentity{TaskId: "2049d357-432c-4a59-b128-24e732458b74", ShortId: "IMP-1", ProjectId: "project"}
	for _, item := range []*taskimportpb.Item{
		{Task: identity, Outcome: &taskimportpb.Item_Created{Created: &emptypb.Empty{}}},
		{Task: identity, Outcome: &taskimportpb.Item_Skipped{Skipped: &emptypb.Empty{}}},
		{Task: identity, Outcome: &taskimportpb.Item_Failed{Failed: &taskimportpb.Failure{Code: taskimportpb.FailureCode_FAILURE_CODE_UNAVAILABLE, Phase: taskimportpb.FailurePhase_FAILURE_PHASE_COMMENTS, Diagnostic: "unavailable", HttpStatus: proto.Int32(503)}}},
	} {
		output.PublishTaskImportProgress(&taskimportpb.ProgressEvent{Payload: &taskimportpb.ProgressEvent_Item{Item: item}})
	}
	if output.finish(&taskimportpb.Success{Created: 1, Skipped: 1, Failed: 1}, nil) != 1 {
		t.Fatal("failed item produced successful exit")
	}
	var document struct {
		Account struct {
			Login string `json:"login"`
		} `json:"account"`
		Results []struct {
			Task struct {
				TaskID string `json:"task_id"`
			} `json:"task"`
		} `json:"results"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.Account.Login != "selected-account" || len(document.Results) != 3 {
		t.Fatalf("%+v", document)
	}
	for _, item := range document.Results {
		if item.Task.TaskID != identity.TaskId {
			t.Fatalf("%+v", item)
		}
	}
}

func TestImportJSONIsSingleDocumentOnObservationFailure(t *testing.T) {
	var stdout, stderr bytes.Buffer
	output := importOutput{stdout: &stdout, stderr: &stderr, json: true}
	output.PublishTaskImportProgress(&taskimportpb.ProgressEvent{Payload: &taskimportpb.ProgressEvent_Item{Item: &taskimportpb.Item{Reference: &taskimportpb.Reference{Value: &taskimportpb.Reference_Number{Number: 1}}, Outcome: &taskimportpb.Item_Skipped{Skipped: &emptypb.Empty{}}}}})
	if output.finish(nil, errors.New("observation lost")) != 1 {
		t.Fatal("observation failure succeeded")
	}
	var document struct {
		Account          *json.RawMessage  `json:"account"`
		Results          []json.RawMessage `json:"results"`
		ObservationError *string           `json:"observation_error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &document); err != nil {
		t.Fatalf("%s: %v", stdout.String(), err)
	}
	if document.Account != nil || len(document.Results) != 1 || document.ObservationError == nil {
		t.Fatalf("%+v", document)
	}
}
