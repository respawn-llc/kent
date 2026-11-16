package transport

import (
	"context"
	"core/shared/apicontract"
	remoteclient "core/shared/client"
	taskimportpb "core/shared/protoapi/gen/kent/api/task_import"
	"core/shared/rpcwire"
	"core/shared/serverapi"
	"google.golang.org/protobuf/types/known/emptypb"
	"net/http/httptest"
	"testing"
)

type importTestDependencies struct {
	GatewayDependencies
	importer apicontract.TaskImportService
}

func (d importTestDependencies) TaskImportClient() apicontract.TaskImportService { return d.importer }

type importOutcomeFixture struct{}

func (importOutcomeFixture) ImportTasks(_ context.Context, request *taskimportpb.Request, sink serverapi.TaskImportProgressSink) (*taskimportpb.Success, error) {
	sink.PublishTaskImportProgress(&taskimportpb.ProgressEvent{Payload: &taskimportpb.ProgressEvent_Account{Account: &taskimportpb.Account{Login: "account"}}})
	sink.PublishTaskImportProgress(&taskimportpb.ProgressEvent{Payload: &taskimportpb.ProgressEvent_Item{Item: &taskimportpb.Item{
		Reference: request.References[0],
		Task:      &taskimportpb.TaskIdentity{TaskId: "task-2049d357-432c-4a59-b128-24e732458b74", ShortId: "IMP-1", ProjectId: "project"},
		Outcome:   &taskimportpb.Item_Created{Created: &emptypb.Empty{}},
	}}})
	return &taskimportpb.Success{Created: 1}, nil
}
func TestImportBinaryStructuredProgress(t *testing.T) {
	appCore, _ := newGatewayTestCore(t, true, true)
	defer appCore.Close()
	gateway, err := NewGateway(importTestDependencies{GatewayDependencies: appCore, importer: importOutcomeFixture{}}, gatewayTestIdentity())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(rpcwire.NewWebSocketTransport().Handler(gateway.handleConn))
	defer server.Close()
	client, err := remoteclient.DialRemoteURLForProject(t.Context(), "ws"+server.URL[len("http"):], appCore.ProjectID())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	var events []*taskimportpb.ProgressEvent
	result, err := client.ImportTasks(t.Context(), &taskimportpb.Request{
		Cwd: t.TempDir(), Mode: taskimportpb.ImportMode_IMPORT_MODE_SKIP_EXISTING,
		References: []*taskimportpb.Reference{{Value: &taskimportpb.Reference_Number{Number: 1}}},
	}, serverapi.TaskImportProgressFunc(func(event *taskimportpb.ProgressEvent) { events = append(events, event) }))
	if err != nil || result.Created != 1 || len(events) != 2 || events[0].GetAccount().Login != "account" || events[1].GetItem().GetCreated() == nil || events[1].GetItem().Task.TaskId != "task-2049d357-432c-4a59-b128-24e732458b74" {
		t.Fatalf("%+v %+v %v", result, events, err)
	}
}
