package app

import (
	"core/shared/clientui"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/textutil"
	"google.golang.org/protobuf/proto"
)

func (c *sessionRuntimeClient) mergeRuntimeTuple(
	candidate runtimeTupleCandidate,
	ingress runtimeTupleIngress,
) runtimeTupleMergeResult {
	if c == nil {
		return runtimeTupleMergeResult{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	decision := decideRuntimeTuple(c.mainView.Version, candidate.Version, ingress)
	if decision == runtimeTupleApply {
		applyRuntimeTuple(c.mainView, candidate)
		if c.mainView.Session.SessionId == "" {
			c.mainView.Session.SessionId = c.sessionID
			c.advanceMetadataRevision()
		}
		c.hasMainView = true
	}
	return runtimeTupleMergeResult{decision: decision, view: proto.CloneOf(c.mainView), project: decision == runtimeTupleApply}
}

func (c *sessionRuntimeClient) admitTranscriptMessageState(message *transcriptpb.Message) (runtimeTupleMergeResult, error) {
	if c == nil {
		return runtimeTupleMergeResult{}, nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	result := runtimeTupleMergeResult{decision: runtimeTupleIgnore, view: proto.CloneOf(c.mainView)}
	switch message.Event.Payload.(type) {
	case *transcriptpb.Event_Hydration:
		hydration := message.Event.GetHydration()
		candidate := runtimeTupleFromReadModelUpdate(hydration.RuntimeReadModelUpdate)
		decision := decideRuntimeTuple(c.mainView.Version, candidate.Version, runtimeTupleIngressHydration)
		if decision == runtimeTupleIgnore && !runtimeTupleMatchesView(candidate, c.mainView) {
			return runtimeTupleMergeResult{}, hydrationRuntimeTupleError(c.mainView, candidate)
		}
		if decision == runtimeTupleApply {
			applyRuntimeTuple(c.mainView, candidate)
		}
		applyTranscriptHydrationMetadataToMainView(c.mainView, hydration)
		c.ensureMainViewIdentity()
		c.advanceMetadataRevision()
		result = runtimeTupleMergeResult{decision: decision, view: proto.CloneOf(c.mainView), project: true}
	case *transcriptpb.Event_RuntimeReadModelUpdate:
		candidate := runtimeTupleFromReadModelUpdate(message.Event.GetRuntimeReadModelUpdate())
		decision := decideRuntimeTuple(c.mainView.Version, candidate.Version, runtimeTupleIngressIncremental)
		if decision == runtimeTupleApply {
			applyRuntimeTuple(c.mainView, candidate)
			if c.ensureMainViewIdentity() {
				c.advanceMetadataRevision()
			}
		}
		result = runtimeTupleMergeResult{decision: decision, view: proto.CloneOf(c.mainView), project: decision == runtimeTupleApply}
	default:
		metadataChanged := applyTranscriptMetadataToMainView(c.mainView, message)
		if c.ensureMainViewIdentity() || metadataChanged {
			c.advanceMetadataRevision()
		}
		result.view = proto.CloneOf(c.mainView)
	}
	return result, nil
}

func (c *sessionRuntimeClient) ensureMainViewIdentity() bool {
	if c.mainView.Session.SessionId == "" {
		c.mainView.Session.SessionId = c.sessionID
		c.hasMainView = true
		return true
	}
	c.hasMainView = true
	return false
}

func applyTranscriptMetadataToMainView(view *runtimepb.MainView, message *transcriptpb.Message) bool {
	switch message.Event.Payload.(type) {
	case *transcriptpb.Event_SessionStatus:
		applyTranscriptSessionStatusToRuntimeStatus(view.Status, message.Event.GetSessionStatus())
	case *transcriptpb.Event_SessionIdentity:
		applyTranscriptSessionIdentityToRuntimeMainView(view, message.Event.GetSessionIdentity())
	case *transcriptpb.Event_ContextUsage:
		view.Status.ContextUsage = proto.CloneOf(message.Event.GetContextUsage())
	case *transcriptpb.Event_GoalStatus:
		view.Status.Goal = proto.CloneOf(message.Event.GetGoalStatus())
	default:
		return false
	}
	return true
}

func applyTranscriptHydrationMetadataToMainView(view *runtimepb.MainView, hydration *transcriptpb.Hydration) {
	applyTranscriptSessionStatusToRuntimeStatus(view.Status, hydration.SessionStatus)
	applyTranscriptSessionIdentityToRuntimeMainView(view, hydration.SessionIdentity)
	view.Status.ContextUsage = proto.CloneOf(hydration.ContextUsage)
	view.Status.Goal = nil
	if hydration.GoalStatus != nil {
		view.Status.Goal = proto.CloneOf(hydration.GoalStatus)
	}
}

func applyTranscriptSessionStatusToRuntimeStatus(status *runtimepb.Status, update *transcriptpb.SessionStatus) {
	status.ReviewerFrequency = update.ReviewerFrequency
	status.ReviewerEnabled = update.ReviewerEnabled
	status.AutoCompactionEnabled = update.AutoCompactionEnabled
	status.QuestionsEnabled = update.QuestionsEnabled
	status.FastModeAvailable = update.FastModeAvailable
	status.FastModeEnabled = update.FastModeEnabled
	status.ThinkingLevel = update.ThinkingLevel
	status.CompactionMode = update.CompactionMode
	status.CompactionCount = update.CompactionCount
	status.PreviousSessionId = textutil.Pointer(update.PreviousSessionId)
	status.ParentAgentSessionId = textutil.Pointer(update.ParentAgentSessionId)
	status.NavigationTargetSessionId = textutil.Pointer(update.NavigationTargetSessionId)
	status.WorkflowSession = proto.CloneOf(update.Workflow)
}

func applyTranscriptSessionIdentityToRuntimeMainView(view *runtimepb.MainView, identity *transcriptpb.SessionIdentity) {
	view.Session.SessionId = identity.SessionId
	view.Session.SessionName = textutil.Pointer(identity.SessionName)
	view.Session.ConversationFreshness = identity.ConversationFreshness
	view.Status.ConversationFreshness = identity.ConversationFreshness
	view.Session.ExecutionTarget = clientui.NormalizeSessionExecutionTarget(identity.ExecutionTarget)
}
