package client

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"core/shared/protoapi"
	workflowpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"golang.org/x/net/websocket"
)

func TestRemoteWorkflowTaskSearchUsesDedicatedConnectionAndClosesIt(t *testing.T) {
	for _, cancelSearch := range []bool{false, true} {
		name := "complete"
		if cancelSearch {
			name = "cancel"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			var connectionCount atomic.Int32
			receivedSearch := make(chan struct{})
			dedicatedClosed := make(chan struct{})
			server := newRemoteTestServer(t, func(ws *websocket.Conn) {
				connection := connectionCount.Add(1)
				acceptRemoteHandshake(t, ws)
				if connection == 1 {
					for range 2 {
						request := &workflowpb.ProjectLabelCatalogRequest{}
						call := receiveRemoteGeneratedCall(t, ws, "ProjectLabelService", "List", request)
						sendRemoteGeneratedResult(t, ws, call, &workflowpb.ProjectLabelCatalogResult{
							Outcome: &workflowpb.ProjectLabelCatalogResult_Success{
								Success: &workflowpb.ProjectLabelCatalogSuccess{
									Catalog: &workflowpb.ProjectLabelCatalog{ProjectId: request.ProjectId},
								},
							},
						})
					}
					return
				}
				request := &taskpb.SearchRequest{}
				call := receiveRemoteGeneratedCall(t, ws, "TaskReadService", "Search", request)
				close(receivedSearch)
				if !cancelSearch {
					sendRemoteGeneratedResult(t, ws, call, &taskpb.SearchResult{
						Outcome: &taskpb.SearchResult_Success{Success: &taskpb.SearchSuccess{Mode: request.Mode}},
					})
				}
				var frame []byte
				if err := websocket.Message.Receive(ws, &frame); err == nil {
					t.Error("dedicated Search connection accepted another frame")
				}
				close(dedicatedClosed)
			})
			remote, err := DialRemoteURL(ctx, "ws"+server.URL[len("http"):])
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = remote.Close() }()
			readControl := func() {
				t.Helper()
				response, err := remote.ListWorkflowProjectLabels(ctx, &workflowpb.ProjectLabelCatalogRequest{ProjectId: "project-1"})
				if err != nil || response.GetCatalog().GetProjectId() != "project-1" {
					t.Fatalf("multiplexed label read = %v, %v", response, err)
				}
			}
			readControl()
			searchCtx, stopSearch := context.WithCancel(ctx)
			defer stopSearch()
			searchDone := make(chan error, 1)
			go func() {
				response, err := remote.SearchWorkflowTasks(searchCtx, &taskpb.SearchRequest{
					Mode: taskpb.SearchMode_SEARCH_MODE_LITERAL, Query: "needle", Context: 20, PageSize: 25,
				})
				if err == nil {
					err = protoapi.Validate(response)
				}
				searchDone <- err
			}()
			select {
			case <-receivedSearch:
			case <-ctx.Done():
				t.Fatal(context.Cause(ctx))
			}
			if cancelSearch {
				stopSearch()
			}
			select {
			case err := <-searchDone:
				if cancelSearch && !errors.Is(err, context.Canceled) {
					t.Fatalf("canceled Search error = %v", err)
				}
				if !cancelSearch && err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal(context.Cause(ctx))
			}
			select {
			case <-dedicatedClosed:
			case <-ctx.Done():
				t.Fatal(context.Cause(ctx))
			}
			readControl()
			if connections := connectionCount.Load(); connections != 2 {
				t.Fatalf("connection count = %d, want control plus dedicated", connections)
			}
		})
	}
}
