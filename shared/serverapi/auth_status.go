package serverapi

import (
	"time"

	"core/shared/auth"
)

type AuthStatusRequest struct{}

type AuthStatusResponse struct {
	Auth         AuthStatusInfo       `json:"auth"`
	Subscription AuthSubscriptionInfo `json:"subscription"`
	Warning      string               `json:"warning,omitempty"`
}

type AuthStatusInfo struct {
	Summary     string          `json:"summary,omitempty"`
	Details     []string        `json:"details,omitempty"`
	Visible     bool            `json:"visible,omitempty"`
	Method      auth.MethodType `json:"method,omitempty"`
	Provider    string          `json:"provider,omitempty"`
	Unavailable bool            `json:"unavailable,omitempty"`
}

type AuthSubscriptionInfo struct {
	Applicable bool                     `json:"applicable,omitempty"`
	Summary    string                   `json:"summary,omitempty"`
	Error      string                   `json:"error,omitempty"`
	Windows    []AuthSubscriptionWindow `json:"windows,omitempty"`
}

type AuthSubscriptionWindow struct {
	Label       string    `json:"label,omitempty"`
	Qualifier   string    `json:"qualifier,omitempty"`
	UsedPercent float64   `json:"used_percent,omitempty"`
	ResetAt     time.Time `json:"reset_at,omitzero"`
}
