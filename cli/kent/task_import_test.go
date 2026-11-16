package main

import (
	"bytes"
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	"core/shared/serverapi"
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"testing"
)

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
