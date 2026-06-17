package startup

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"core/server/auth"
	"core/server/core"
	"core/server/transport"
	"core/shared/config"
	"core/shared/protocol"
)

type ServeServer struct {
	*core.Core
	ready atomic.Bool
}

var localSocketListener = listenLocalSocket

var (
	testListenReservationsMu sync.Mutex
	testListenReservations   = map[string]net.Listener{}
)

// ReserveTestListenReservation keeps a test-owned listener alive until the
// configured daemon bind path is ready to claim the same address.
func ReserveTestListenReservation(listener net.Listener) {
	if listener == nil {
		return
	}
	addr := strings.TrimSpace(listener.Addr().String())
	if addr == "" {
		_ = listener.Close()
		return
	}
	testListenReservationsMu.Lock()
	if existing := testListenReservations[addr]; existing != nil {
		_ = existing.Close()
	}
	testListenReservations[addr] = listener
	testListenReservationsMu.Unlock()
	go drainTestListenReservation(listener)
}

func drainTestListenReservation(listener net.Listener) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		_ = conn.Close()
	}
}

func ReleaseTestListenReservation(addr string) {
	trimmed := strings.TrimSpace(addr)
	if trimmed == "" {
		return
	}
	testListenReservationsMu.Lock()
	listener := testListenReservations[trimmed]
	delete(testListenReservations, trimmed)
	testListenReservationsMu.Unlock()
	if listener != nil {
		_ = listener.Close()
	}
}

func StartServeServer(ctx context.Context, req Request, authHandler AuthHandler, onboardingHandler OnboardingHandler) (*ServeServer, error) {
	appCore, err := StartCore(ctx, req, authHandler, onboardingHandler)
	if err != nil {
		return nil, err
	}
	return &ServeServer{Core: appCore}, nil
}

func (s *ServeServer) Serve(ctx context.Context) error {
	if ctx == nil {
		return errContextRequired
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil || s.Core == nil {
		return errors.New("server core is required")
	}
	listenAddress := config.ServerListenAddress(s.Config())
	ReleaseTestListenReservation(listenAddress)
	tcpListener, err := net.Listen("tcp", listenAddress)
	if err != nil {
		return fmt.Errorf("listen local control endpoint: %w", err)
	}
	cleanupFns := []func(){func() { _ = tcpListener.Close() }}
	defer func() {
		for _, cleanup := range cleanupFns {
			cleanup()
		}
	}()
	listeners := []net.Listener{tcpListener}
	if localListener, localCleanup, ok, err := localSocketListener(s.Config()); err != nil {
		// Derived same-machine UDS is additive only. Configured TCP stays authoritative.
	} else if ok {
		listeners = append(listeners, localListener)
		cleanupFns = append(cleanupFns, localCleanup)
	}

	identity := protocol.ServerIdentity{
		ProtocolVersion: protocol.Version,
		ServerID:        fmt.Sprintf(config.Command+":%d", os.Getpid()),
		PID:             os.Getpid(),
		Capabilities: protocol.CapabilityFlags{
			JSONRPCWebSocket:        true,
			AuthBootstrap:           true,
			ProjectAttach:           true,
			SessionAttach:           true,
			HealthEndpoint:          true,
			ReadinessEndpoint:       true,
			RunPrompt:               true,
			SessionPlan:             true,
			SessionLifecycle:        true,
			SessionTranscriptPaging: true,
			SessionRuntime:          true,
			RuntimeControl:          true,
			PromptControl:           true,
			PromptActivity:          true,
			SessionActivity:         true,
			ProcessOutput:           true,
		},
	}
	gateway, err := transport.NewGateway(s.Core, identity)
	if err != nil {
		return err
	}

	mux := http.NewServeMux()
	mux.HandleFunc(protocol.HealthPath, func(w http.ResponseWriter, r *http.Request) {
		authReady := serverAuthReady(r.Context(), s.Core)
		writeStatusJSON(w, http.StatusOK, map[string]any{
			"status":     protocol.HealthStatusOK,
			"server_id":  identity.ServerID,
			"pid":        identity.PID,
			"auth_ready": authReady,
		})
	})
	s.ready.Store(true)
	mux.HandleFunc(protocol.ReadinessPath, func(w http.ResponseWriter, r *http.Request) {
		authReady := serverAuthReady(r.Context(), s.Core)
		status := http.StatusServiceUnavailable
		body := map[string]any{"ready": false, "auth_ready": authReady}
		if s.ready.Load() && authReady {
			status = http.StatusOK
			body = map[string]any{
				"ready":      true,
				"server_id":  identity.ServerID,
				"pid":        identity.PID,
				"auth_ready": true,
			}
		} else if s.ready.Load() {
			body["transport_ready"] = true
			body["server_id"] = identity.ServerID
			body["pid"] = identity.PID
		}
		writeStatusJSON(w, status, body)
	})
	mux.Handle(protocol.RPCPath, gateway.Handler())

	httpServers := make([]*http.Server, 0, len(listeners))
	errCh := make(chan error, len(listeners))
	for _, listener := range listeners {
		httpServer := &http.Server{Handler: mux}
		httpServers = append(httpServers, httpServer)
		go func(server *http.Server, frontend net.Listener) {
			serveErr := server.Serve(frontend)
			if serveErr == nil || errors.Is(serveErr, http.ErrServerClosed) {
				errCh <- nil
				return
			}
			errCh <- serveErr
		}(httpServer, listener)
	}
	shutdownServers := func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		for _, httpServer := range httpServers {
			_ = httpServer.Shutdown(shutdownCtx)
		}
	}
	waitServers := func(expect int) {
		for i := 0; i < expect; i++ {
			<-errCh
		}
	}

	select {
	case <-ctx.Done():
		shutdownServers()
		waitServers(len(listeners))
		return ctx.Err()
	case serveErr := <-errCh:
		shutdownServers()
		waitServers(len(listeners) - 1)
		return serveErr
	}
}

func writeStatusJSON(w http.ResponseWriter, status int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func serverAuthReady(ctx context.Context, appCore *core.Core) bool {
	if appCore == nil || appCore.AuthManager() == nil {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	state, err := appCore.AuthManager().Load(ctx)
	if err != nil {
		return false
	}
	return auth.EvaluateStartupGate(state).Ready
}
