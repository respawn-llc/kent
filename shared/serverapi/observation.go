package serverapi

import (
	"errors"
	"sort"
	"strings"
	"time"

	"core/shared/clientui"
	"core/shared/runtimeids"
)

type ObservationQuestion struct {
	Ask      *clientui.PendingAsk      `json:"ask,omitempty"`
	Approval *clientui.PendingApproval `json:"approval,omitempty"`
}

type PendingPromptObservation struct {
	ID        string
	CreatedAt time.Time
	Question  ObservationQuestion
}

func FirstPendingPromptObservation(
	asks []clientui.PendingAsk,
	approvals []clientui.PendingApproval,
) (PendingPromptObservation, bool) {
	prompts := make([]PendingPromptObservation, 0, len(asks)+len(approvals))
	for index := range asks {
		prompts = append(prompts, PendingPromptObservation{
			ID: string(asks[index].ToolCallID), CreatedAt: asks[index].CreatedAt,
			Question: ObservationQuestion{Ask: &asks[index]},
		})
	}
	for index := range approvals {
		prompts = append(prompts, PendingPromptObservation{
			ID: string(approvals[index].ToolCallID), CreatedAt: approvals[index].CreatedAt,
			Question: ObservationQuestion{Approval: &approvals[index]},
		})
	}
	sort.SliceStable(prompts, func(i, j int) bool {
		if !prompts[i].CreatedAt.Equal(prompts[j].CreatedAt) {
			return prompts[i].CreatedAt.Before(prompts[j].CreatedAt)
		}
		return prompts[i].ID < prompts[j].ID
	})
	if len(prompts) == 0 {
		return PendingPromptObservation{}, false
	}
	return prompts[0], true
}

func (q ObservationQuestion) Validate() error {
	if (q.Ask == nil) == (q.Approval == nil) {
		return errors.New("observation question must contain one ask or approval")
	}
	if q.Ask != nil {
		return validateObservationAsk(*q.Ask)
	}
	return validateObservationApproval(*q.Approval)
}

func validateObservationAsk(ask clientui.PendingAsk) error {
	if err := validateObservationToolCallIdentity(ask.ToolCallID, ask.SessionID, ask.StepID); err != nil {
		return err
	}
	if strings.TrimSpace(ask.Question) == "" {
		return errors.New("observation ask question is required")
	}
	if ask.RecommendedOptionIndex != nil &&
		(*ask.RecommendedOptionIndex <= 0 || *ask.RecommendedOptionIndex > len(ask.Suggestions)) {
		return errors.New("observation ask recommendation is invalid")
	}
	return nil
}

func validateObservationApproval(approval clientui.PendingApproval) error {
	if err := validateObservationToolCallIdentity(approval.ToolCallID, approval.SessionID, approval.StepID); err != nil {
		return err
	}
	if strings.TrimSpace(approval.Question) == "" && len(approval.AccessTargets) == 0 {
		return errors.New("observation approval question is required")
	}
	if strings.TrimSpace(approval.Question) != "" && len(approval.AccessTargets) > 0 {
		return errors.New("observation access approval cannot carry question copy")
	}
	if len(approval.Options) == 0 {
		return errors.New("observation approval options are required")
	}
	for _, option := range approval.Options {
		switch option.Decision {
		case clientui.ApprovalDecisionAllowOnce, clientui.ApprovalDecisionAllowSession, clientui.ApprovalDecisionDeny:
		default:
			return errors.New("observation approval option decision is invalid")
		}
	}
	for _, target := range approval.AccessTargets {
		if err := target.Validate(); err != nil {
			return errors.New("observation approval access target is invalid")
		}
	}
	return nil
}

func validateObservationToolCallIdentity(toolCallID clientui.ToolCallID, sessionID runtimeids.SessionID, stepID runtimeids.StepID) error {
	if err := toolCallID.Validate(); err != nil {
		return err
	}
	if sessionID.IsZero() {
		return errors.New("observation prompt session id is required")
	}
	if stepID.IsZero() {
		return errors.New("observation prompt step id is required")
	}
	return nil
}
