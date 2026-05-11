package protocol

import "time"

const (
	Version           = "2"
	RPCPath           = "/rpc"
	HealthPath        = "/healthz"
	HealthStatusOK    = "ok"
	ReadinessPath     = "/readyz"
	DiscoveryFilename = "app-server.json"
)

type CapabilityFlags struct {
	JSONRPCWebSocket        bool `json:"jsonrpc_websocket"`
	AuthBootstrap           bool `json:"auth_bootstrap"`
	ProjectAttach           bool `json:"project_attach"`
	SessionAttach           bool `json:"session_attach"`
	HealthEndpoint          bool `json:"health_endpoint"`
	ReadinessEndpoint       bool `json:"readiness_endpoint"`
	RunPrompt               bool `json:"run_prompt"`
	SessionPlan             bool `json:"session_plan"`
	SessionLifecycle        bool `json:"session_lifecycle"`
	SessionTranscriptPaging bool `json:"session_transcript_paging"`
	SessionRuntime          bool `json:"session_runtime"`
	RuntimeControl          bool `json:"runtime_control"`
	PromptControl           bool `json:"prompt_control"`
	PromptActivity          bool `json:"prompt_activity"`
	SessionActivity         bool `json:"session_activity"`
	ProcessOutput           bool `json:"process_output"`
}

type ServerIdentity struct {
	ProtocolVersion string          `json:"protocol_version"`
	ServerID        string          `json:"server_id"`
	PID             int             `json:"pid"`
	Capabilities    CapabilityFlags `json:"capabilities"`
}

type DiscoveryRecord struct {
	Identity  ServerIdentity `json:"identity"`
	HTTPURL   string         `json:"http_url"`
	RPCURL    string         `json:"rpc_url"`
	HealthURL string         `json:"health_url"`
	ReadyURL  string         `json:"ready_url"`
	StartedAt time.Time      `json:"started_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}
