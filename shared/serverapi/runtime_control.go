package serverapi

import (
	"errors"
	"strings"
	"time"

	"core/shared/transcript"
)

type RuntimeSetSessionNameRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
	Name              string `json:"name"`
}

type RuntimeSetThinkingLevelRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
	Level             string `json:"level"`
}

type RuntimeSetFastModeEnabledRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
	Enabled           bool   `json:"enabled"`
}

type RuntimeSetFastModeEnabledResponse struct {
	Changed bool `json:"changed"`
}

type RuntimeSetReviewerEnabledRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
	Enabled           bool   `json:"enabled"`
}

type RuntimeSetReviewerEnabledResponse struct {
	Changed bool   `json:"changed"`
	Mode    string `json:"mode"`
}

type RuntimeSetAutoCompactionEnabledRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
	Enabled           bool   `json:"enabled"`
}

type RuntimeSetAutoCompactionEnabledResponse struct {
	Changed bool `json:"changed"`
	Enabled bool `json:"enabled"`
}

type RuntimeSetQuestionsEnabledRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
	Enabled           bool   `json:"enabled"`
}

type RuntimeSetQuestionsEnabledResponse struct {
	Changed bool `json:"changed"`
	Enabled bool `json:"enabled"`
}

type RuntimeAppendCommittedEntryRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
	Role              string `json:"role"`
	Text              string `json:"text"`
	Visibility        string `json:"visibility,omitempty"`
	NoticeID          string `json:"notice_id,omitempty"`
}

type RuntimeShouldCompactBeforeUserMessageRequest struct {
	SessionID string `json:"session_id"`
	Text      string `json:"text"`
}

type RuntimeShouldCompactBeforeUserMessageResponse struct {
	ShouldCompact bool `json:"should_compact"`
}

type RuntimeSubmitUserMessageRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
	Text              string `json:"text"`
}

type RuntimeSubmitUserMessageResponse struct {
	Message string `json:"message"`
}

type RuntimeSubmitUserTurnRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
	Text              string `json:"text"`
}

type RuntimeSubmitUserTurnResponse struct {
	Message   string `json:"message"`
	Compacted bool   `json:"compacted,omitempty"`
}

type RuntimeSubmitUserShellCommandRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
	Command           string `json:"command"`
}

type RuntimeCompactContextRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
	Args              string `json:"args"`
}

type RuntimeCompactContextForPreSubmitRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
}

type RuntimeHasQueuedUserWorkRequest struct {
	SessionID string `json:"session_id"`
}

type RuntimeHasQueuedUserWorkResponse struct {
	HasQueuedUserWork bool `json:"has_queued_user_work"`
}

type RuntimeSubmitQueuedUserMessagesRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
}

type RuntimeSubmitQueuedUserMessagesResponse struct {
	Message string `json:"message"`
}

type RuntimeInterruptRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
}

type RuntimeQueueUserMessageRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
	Text              string `json:"text"`
}

type RuntimeQueueUserMessageResponse struct {
	QueueItemID string `json:"queue_item_id"`
	Text        string `json:"text"`
}

type RuntimeDiscardQueuedUserMessageRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
	QueueItemID       string `json:"queue_item_id"`
}

type RuntimeDiscardQueuedUserMessageResponse struct {
	Discarded bool `json:"discarded"`
}

type RuntimeRecordPromptHistoryRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id"`
	Text              string `json:"text"`
}

type RuntimeGoal struct {
	ID        string    `json:"id"`
	Objective string    `json:"objective"`
	Status    string    `json:"status"`
	Suspended bool      `json:"suspended,omitempty"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type RuntimeGoalShowRequest struct {
	SessionID string `json:"session_id"`
}

type RuntimeGoalShowResponse struct {
	Goal *RuntimeGoal `json:"goal,omitempty"`
}

type RuntimeGoalSetRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id,omitempty"`
	Objective         string `json:"objective"`
	Actor             string `json:"actor"`
}

type RuntimeGoalStatusRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id,omitempty"`
	Actor             string `json:"actor"`
}

type RuntimeGoalClearRequest struct {
	ClientRequestID   string `json:"client_request_id"`
	SessionID         string `json:"session_id"`
	ControllerLeaseID string `json:"controller_lease_id,omitempty"`
	Actor             string `json:"actor"`
}

func validateControllerLeaseID(leaseID string) error {
	if strings.TrimSpace(leaseID) == "" {
		return errors.New("controller_lease_id is required")
	}
	return nil
}

func validateClientRequestID(clientRequestID string) error {
	if strings.TrimSpace(clientRequestID) == "" {
		return errors.New("client_request_id is required")
	}
	return nil
}

func validateGoalActor(actor string) error {
	switch strings.TrimSpace(actor) {
	case "user", "agent", "system":
		return nil
	default:
		return errors.New("actor must be user, agent, or system")
	}
}

func validateRuntimeControllerRequest(clientRequestID string, sessionID string, controllerLeaseID string) error {
	if err := validateClientRequestID(clientRequestID); err != nil {
		return err
	}
	if err := validateRequiredSessionID(sessionID); err != nil {
		return err
	}
	return validateControllerLeaseID(controllerLeaseID)
}

func validateRuntimeGoalActionRequest(clientRequestID string, sessionID string, actor string) error {
	if err := validateClientRequestID(clientRequestID); err != nil {
		return err
	}
	if err := validateRequiredSessionID(sessionID); err != nil {
		return err
	}
	return validateGoalActor(actor)
}

func (r RuntimeSetSessionNameRequest) Validate() error {
	return validateRuntimeControllerRequest(r.ClientRequestID, r.SessionID, r.ControllerLeaseID)
}
func (r RuntimeSetThinkingLevelRequest) Validate() error {
	return validateRuntimeControllerRequest(r.ClientRequestID, r.SessionID, r.ControllerLeaseID)
}
func (r RuntimeSetFastModeEnabledRequest) Validate() error {
	return validateRuntimeControllerRequest(r.ClientRequestID, r.SessionID, r.ControllerLeaseID)
}
func (r RuntimeSetReviewerEnabledRequest) Validate() error {
	return validateRuntimeControllerRequest(r.ClientRequestID, r.SessionID, r.ControllerLeaseID)
}
func (r RuntimeSetAutoCompactionEnabledRequest) Validate() error {
	return validateRuntimeControllerRequest(r.ClientRequestID, r.SessionID, r.ControllerLeaseID)
}
func (r RuntimeSetQuestionsEnabledRequest) Validate() error {
	return validateRuntimeControllerRequest(r.ClientRequestID, r.SessionID, r.ControllerLeaseID)
}
func (r RuntimeAppendCommittedEntryRequest) Validate() error {
	if err := validateRuntimeControllerRequest(r.ClientRequestID, r.SessionID, r.ControllerLeaseID); err != nil {
		return err
	}
	if visibility := transcript.NormalizeEntryVisibility(transcript.EntryVisibility(r.Visibility)); visibility != "" && visibility != transcript.EntryVisibilityAll && visibility != transcript.EntryVisibilityDetailOnly {
		return errors.New("visibility must be all or detail_only")
	}
	return nil
}
func (r RuntimeShouldCompactBeforeUserMessageRequest) Validate() error {
	return validateRequiredSessionID(r.SessionID)
}
func (r RuntimeSubmitUserMessageRequest) Validate() error {
	return validateRuntimeControllerRequest(r.ClientRequestID, r.SessionID, r.ControllerLeaseID)
}
func (r RuntimeSubmitUserTurnRequest) Validate() error {
	return validateRuntimeControllerRequest(r.ClientRequestID, r.SessionID, r.ControllerLeaseID)
}
func (r RuntimeSubmitUserShellCommandRequest) Validate() error {
	return validateRuntimeControllerRequest(r.ClientRequestID, r.SessionID, r.ControllerLeaseID)
}
func (r RuntimeCompactContextRequest) Validate() error {
	return validateRuntimeControllerRequest(r.ClientRequestID, r.SessionID, r.ControllerLeaseID)
}
func (r RuntimeCompactContextForPreSubmitRequest) Validate() error {
	return validateRuntimeControllerRequest(r.ClientRequestID, r.SessionID, r.ControllerLeaseID)
}
func (r RuntimeHasQueuedUserWorkRequest) Validate() error {
	return validateRequiredSessionID(r.SessionID)
}
func (r RuntimeSubmitQueuedUserMessagesRequest) Validate() error {
	return validateRuntimeControllerRequest(r.ClientRequestID, r.SessionID, r.ControllerLeaseID)
}
func (r RuntimeInterruptRequest) Validate() error {
	return validateRuntimeControllerRequest(r.ClientRequestID, r.SessionID, r.ControllerLeaseID)
}
func (r RuntimeQueueUserMessageRequest) Validate() error {
	return validateRuntimeControllerRequest(r.ClientRequestID, r.SessionID, r.ControllerLeaseID)
}
func (r RuntimeDiscardQueuedUserMessageRequest) Validate() error {
	if err := validateRuntimeControllerRequest(r.ClientRequestID, r.SessionID, r.ControllerLeaseID); err != nil {
		return err
	}
	if strings.TrimSpace(r.QueueItemID) == "" {
		return errors.New("queue_item_id is required")
	}
	return nil
}
func (r RuntimeRecordPromptHistoryRequest) Validate() error {
	return validateRuntimeControllerRequest(r.ClientRequestID, r.SessionID, r.ControllerLeaseID)
}
func (r RuntimeGoalShowRequest) Validate() error {
	return validateRequiredSessionID(r.SessionID)
}
func (r RuntimeGoalSetRequest) Validate() error {
	if err := validateClientRequestID(r.ClientRequestID); err != nil {
		return err
	}
	if err := validateRequiredSessionID(r.SessionID); err != nil {
		return err
	}
	if strings.TrimSpace(r.Objective) == "" {
		return errors.New("objective is required")
	}
	return validateGoalActor(r.Actor)
}
func (r RuntimeGoalStatusRequest) Validate() error {
	return validateRuntimeGoalActionRequest(r.ClientRequestID, r.SessionID, r.Actor)
}
func (r RuntimeGoalClearRequest) Validate() error {
	return validateRuntimeGoalActionRequest(r.ClientRequestID, r.SessionID, r.Actor)
}
