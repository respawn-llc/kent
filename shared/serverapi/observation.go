package serverapi

import (
	"sort"
	"time"

	"core/shared/clientui"
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
