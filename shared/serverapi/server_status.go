package serverapi

type ServerReadinessRequest struct{}

type ServerReadinessResponse struct {
	Ready           bool                   `json:"ready"`
	ServerID        string                 `json:"server_id"`
	ServerVersion   string                 `json:"server_version"`
	ProtocolVersion string                 `json:"protocol_version"`
	AuthReady       bool                   `json:"auth_ready"`
	AuthRequired    bool                   `json:"auth_required"`
	Endpoint        string                 `json:"endpoint"`
	Causes          []ServerReadinessCause `json:"causes,omitempty"`
}

type ServerReadinessCause struct {
	Code         string `json:"code"`
	Severity     string `json:"severity"`
	Summary      string `json:"summary"`
	NextAction   string `json:"next_action"`
	DiagnosticID string `json:"diagnostic_id,omitempty"`
}

type ServerCapabilitiesRequest struct{}

type ServerCapabilitiesResponse struct {
	Capabilities    []ServerCapability `json:"capabilities"`
	ServerVersion   string             `json:"server_version"`
	ProtocolVersion string             `json:"protocol_version"`
}

type ServerCapability struct {
	ID             string `json:"id"`
	Available      bool   `json:"available"`
	Reason         string `json:"reason,omitempty"`
	RequiredForMVP bool   `json:"required_for_mvp"`
}
