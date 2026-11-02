package client

import (
	"context"
	"io"
	"sync"
	"testing"

	"core/shared/protoapi"
	projectpb "core/shared/protoapi/gen/kent/api/project"
	"core/shared/rpcwire"
	"google.golang.org/protobuf/types/known/emptypb"
)

type completionConn struct {
	events chan rpcwire.Event
	closed chan struct{}
	close  sync.Once
	send   func(rpcwire.Frame) error
}

func (c *completionConn) Send(_ context.Context, frame rpcwire.Frame) error { return c.send(frame) }
func (c *completionConn) Events() <-chan rpcwire.Event                      { return c.events }
func (c *completionConn) Closed() <-chan struct{}                           { return c.closed }
func (c *completionConn) Close() error {
	c.close.Do(func() { close(c.closed) })
	return nil
}

func TestCompletedResponseSurvivesImmediateConnectionEOF(t *testing.T) {
	for range 100 {
		conn := &completionConn{events: make(chan rpcwire.Event, 2), closed: make(chan struct{})}
		control := newRemoteControlConn(conn)
		conn.send = func(frame rpcwire.Frame) error {
			envelope, err := protoapi.DecodeEnvelope(frame.Payload)
			if err != nil {
				return err
			}
			result, err := remoteProjectListResultFrame("project-1", envelope.GetCall().Correlation)
			if err != nil {
				return err
			}
			conn.events <- rpcwire.Event{Frame: result}
			conn.events <- rpcwire.Event{Err: io.EOF}
			// A buffered response remains valid even if the reader finishes
			// processing the following EOF before Send returns.
			<-control.done
			return nil
		}
		method := projectpb.File_kent_api_project_project_proto.Services().
			ByName("ProjectCatalogService").Methods().ByName("List")
		result := &projectpb.ProjectListResult{}
		err := control.callBinary(t.Context(), method, &emptypb.Empty{}, result)
		if closeErr := control.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		if err != nil {
			t.Fatalf("completed response lost to EOF: %v", err)
		}
		if len(result.GetSuccess().GetProjects()) != 1 {
			t.Fatalf("completed result missing: %v", result)
		}
	}
}
