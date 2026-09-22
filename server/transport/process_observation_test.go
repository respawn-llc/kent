package transport

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"core/shared/apicontract"
	connectionpb "core/shared/protoapi/gen/kent/api/connection"
	processpb "core/shared/protoapi/gen/kent/api/process"
	"core/shared/serverapi"
)

type processObservationDependencies struct {
	GatewayDependencies
	observer apicontract.ProcessObservationService
}

func (d processObservationDependencies) ProcessObservationClient() apicontract.ProcessObservationService {
	return d.observer
}

type observedProcessClient struct {
	apicontract.ProcessObservationService
	closed chan struct{}
}

func (c observedProcessClient) ObserveProcesses(ctx context.Context, req *processpb.ObserveRequest) (serverapi.ProcessObservationSubscription, error) {
	sub, err := c.ProcessObservationService.ObserveProcesses(ctx, req)
	if err != nil {
		return nil, err
	}
	return observedProcessSubscription{ProcessObservationSubscription: sub, closed: c.closed}, nil
}

type observedProcessSubscription struct {
	serverapi.ProcessObservationSubscription
	closed chan struct{}
}

func (s observedProcessSubscription) Close() error {
	err := s.ProcessObservationSubscription.Close()
	select {
	case s.closed <- struct{}{}:
	default:
	}
	return err
}

func TestProcessObservationGatewayInitialAndClose(t *testing.T) {
	app, _ := newGatewayTestCore(t, true, true)
	defer app.Close()
	store := createGatewayAuthoritativeSession(t, app)
	closed := make(chan struct{}, 1)
	gateway, err := NewGateway(processObservationDependencies{
		GatewayDependencies: app,
		observer:            observedProcessClient{ProcessObservationService: app.ProcessObservationClient(), closed: closed},
	}, gatewayTestIdentity())
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(gateway.Handler())
	defer server.Close()
	conn := dialGateway(t, server)
	defer conn.Close()
	handshakeGateway(t, conn)
	requireGatewayProjectAttachment(t, conn, "attach", &connectionpb.AttachProjectRequest{ProjectId: app.ProjectID()})
	service := processpb.File_kent_api_process_process_proto.Services().ByName("ViewService")
	var started processpb.ObserveResult
	callGatewayDescriptor(t, conn, "observe", service.Methods().ByName("Observe"),
		&processpb.ObserveRequest{ProjectId: app.ProjectID(), SessionId: store.Meta().SessionID}, &started)
	if started.GetSuccess() == nil {
		t.Fatalf("observe failed: %v", &started)
	}
	var initial processpb.ListSuccess
	receiveGatewayDescriptorNotification(t, conn, service.Methods().ByName("Event"), &initial)
	if len(initial.Processes) != 0 {
		t.Fatalf("initial: %v", &initial)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("connection close did not release observation")
	}
}
