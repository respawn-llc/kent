package runtimeview

import (
	"core/server/goalview"
	"core/server/runtime"
	"core/server/session"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/textutil"
)

func MainViewFromRuntimeActivity(
	engine *runtime.Engine,
	version *runtimepb.ReadModelVersion,
	activity *runtimepb.Activity,
) (*runtimepb.MainView, error) {
	if engine == nil {
		return &runtimepb.MainView{}, nil
	}
	sessionView, err := SessionViewFromRuntime(engine)
	if err != nil {
		return &runtimepb.MainView{}, err
	}
	status, err := StatusFromRuntime(engine)
	if err != nil {
		return &runtimepb.MainView{}, err
	}
	if err := protoapi.Validate(activity); err != nil {
		activity = &runtimepb.Activity{
			State:              runtimepb.ActivityState_RUNTIME_ACTIVITY_UNAVAILABLE,
			Reviewer:           runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
			DiagnosticRecovery: true,
		}
	}
	return &runtimepb.MainView{
		Version:  version,
		Status:   status,
		Session:  sessionView,
		Activity: activity,
	}, nil
}

func StatusFromRuntime(engine *runtime.Engine) (*runtimepb.Status, error) {
	if engine == nil {
		return &runtimepb.Status{}, nil
	}
	goalAvailability, err := engine.GoalAvailability()
	if err != nil {
		return &runtimepb.Status{}, err
	}
	freshness, err := engine.ConversationFreshness()
	if err != nil {
		return &runtimepb.Status{}, err
	}
	usage := engine.ContextUsage()
	usedTokens, err := protoapi.Int32(usage.UsedTokens, "used tokens")
	if err != nil {
		return nil, err
	}
	windowTokens, err := protoapi.Int32(usage.WindowTokens, "context window tokens")
	if err != nil {
		return nil, err
	}
	compactionCount, err := protoapi.Int32(engine.CompactionCount(), "compaction count")
	if err != nil {
		return nil, err
	}
	fastModeAvailable := engine.FastModeAvailable()
	goal, err := goalview.FromSessionState(engine.Goal(), goalAvailability, engine.GoalLoopSuspended())
	if err != nil {
		return nil, err
	}
	status := &runtimepb.Status{
		ReviewerFrequency:         engine.ReviewerFrequency(),
		ReviewerEnabled:           engine.ReviewerEnabled(),
		AutoCompactionEnabled:     engine.AutoCompactionEnabled(),
		QuestionsEnabled:          engine.QuestionsEnabled(),
		FastModeAvailable:         fastModeAvailable,
		FastModeEnabled:           engine.FastModeEnabled(),
		ConversationFreshness:     ConversationFreshnessFromSession(freshness),
		PreviousSessionId:         protoapi.OptionalSessionIDToProto(engine.PreviousSessionID()),
		ParentAgentSessionId:      protoapi.OptionalSessionIDToProto(engine.ParentAgentSessionID()),
		NavigationTargetSessionId: protoapi.OptionalSessionIDToProto(engine.NavigationTargetSessionID()),
		ThinkingLevel:             engine.ThinkingLevel(),
		CompactionMode:            engine.CompactionMode(),
		ContextUsage: &runtimepb.ContextUsage{
			UsedTokens:   usedTokens,
			WindowTokens: windowTokens,
		},
		CompactionCount: compactionCount,
		Goal:            goal,
	}
	if usage.HasCacheHitPercentage {
		percentage, err := protoapi.Int32(usage.CacheHitPercent, "cache hit percentage")
		if err != nil {
			return nil, err
		}
		status.ContextUsage.CacheHitPercent = &percentage
	}
	if workflowState, err := engine.WorkflowSessionState(); err != nil {
		return &runtimepb.Status{}, err
	} else if workflowState != nil && !engine.WorkflowTerminalState().Completed {
		status.WorkflowSession = &runtimepb.WorkflowSessionStatus{
			TaskId:     string(workflowState.TaskID),
			WorkflowId: workflowState.WorkflowID.String(),
		}
	}
	return status, nil
}

func TranscriptSessionStatusFromRuntime(engine *runtime.Engine) (*transcriptpb.SessionStatus, error) {
	if engine == nil {
		return &transcriptpb.SessionStatus{}, nil
	}
	compactionCount, err := protoapi.Int32(engine.CompactionCount(), "compaction count")
	if err != nil {
		return nil, err
	}
	fastModeAvailable := engine.FastModeAvailable()
	status := &transcriptpb.SessionStatus{
		ReviewerFrequency:         engine.ReviewerFrequency(),
		ReviewerEnabled:           engine.ReviewerEnabled(),
		AutoCompactionEnabled:     engine.AutoCompactionEnabled(),
		QuestionsEnabled:          engine.QuestionsEnabled(),
		FastModeAvailable:         fastModeAvailable,
		FastModeEnabled:           engine.FastModeEnabled(),
		ThinkingLevel:             engine.ThinkingLevel(),
		CompactionMode:            engine.CompactionMode(),
		CompactionCount:           compactionCount,
		PreviousSessionId:         protoapi.OptionalSessionIDToProto(engine.PreviousSessionID()),
		ParentAgentSessionId:      protoapi.OptionalSessionIDToProto(engine.ParentAgentSessionID()),
		NavigationTargetSessionId: protoapi.OptionalSessionIDToProto(engine.NavigationTargetSessionID()),
	}
	workflowState, err := engine.WorkflowSessionState()
	if err != nil {
		return &transcriptpb.SessionStatus{}, err
	}
	if workflowState != nil && !engine.WorkflowTerminalState().Completed {
		status.Workflow = &runtimepb.WorkflowSessionStatus{
			TaskId:     string(workflowState.TaskID),
			WorkflowId: workflowState.WorkflowID.String(),
		}
	}
	return status, nil
}

func SessionViewFromRuntime(engine *runtime.Engine) (*runtimepb.SessionView, error) {
	if engine == nil {
		return &runtimepb.SessionView{}, nil
	}
	freshness, err := engine.ConversationFreshness()
	if err != nil {
		return &runtimepb.SessionView{}, err
	}
	return &runtimepb.SessionView{
		SessionId:             engine.SessionID(),
		SessionName:           textutil.OptionalExactString(engine.SessionName()),
		AgentRole:             engine.ContinuationAgentRole(),
		ConversationFreshness: ConversationFreshnessFromSession(freshness),
	}, nil
}

func ConversationFreshnessFromSession(freshness session.ConversationFreshness) runtimepb.ConversationFreshness {
	if freshness.IsFresh() {
		return runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_FRESH
	}
	return runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED
}
