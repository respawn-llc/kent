package runtime

func (e *Engine) TurnEndAttentionEligible(result *LiveRunResult) bool {
	return e != nil && !e.cfg.HeadlessMode && !e.currentNodeExecutionActive() &&
		result != nil && result.Status == RunStatusCompleted &&
		result.ResultKind == LiveRunResultAssistantFinalAnswer && result.WorkPerformed
}
