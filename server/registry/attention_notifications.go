package registry

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"core/server/attentionnotify"
	askquestion "core/server/tools"
	"core/shared/clientui"
	"core/shared/protoapi"
	attentionpb "core/shared/protoapi/gen/kent/api/attention"
	"core/shared/serverapi"
)

func (r *RuntimeRegistry) WithAttentionNotifications(broker *attentionnotify.Broker) *RuntimeRegistry {
	if r == nil {
		return r
	}
	r.attentionBroker = broker
	if broker != nil {
		r.questionBatches = attentionnotify.NewQuestionBatchTracker(broker)
	}
	return r
}

func (r *RuntimeRegistry) SubscribeAttentionNotifications(_ context.Context, req serverapi.AttentionNotificationSubscribeRequest) (serverapi.AttentionNotificationSubscription, error) {
	if err := req.Validate(); err != nil {
		return nil, err
	}
	if r == nil || r.attentionBroker == nil {
		return nil, fmt.Errorf("attention notification stream is unavailable: %w", serverapi.ErrStreamUnavailable)
	}
	return r.attentionBroker.SubscribeDesktop()
}

func (r *RuntimeRegistry) SubscribeSessionAttentionNotifications(_ context.Context, req *attentionpb.SubscribeRequest) (serverapi.SessionAttentionNotificationSubscription, error) {
	if err := protoapi.Validate(req); err != nil {
		return nil, err
	}
	if r == nil || r.attentionBroker == nil {
		return nil, fmt.Errorf("attention notification stream is unavailable: %w", serverapi.ErrStreamUnavailable)
	}
	sub, err := r.attentionBroker.SubscribeSession(req.SessionId)
	if err != nil {
		return nil, err
	}
	return &sessionAttentionSubscription{native: sub}, nil
}

func (r *RuntimeRegistry) publishAttentionPending(sessionID string, snapshot PendingPromptSnapshot) {
	if r == nil || r.attentionBroker == nil {
		return
	}
	req := snapshot.Request
	if req.QuestionBatch != nil && req.AttentionTarget != nil && req.AttentionTarget.Kind == clientui.AttentionNotificationTargetWorkflowTask {
		stepID := questionBatchStepID(*req.QuestionBatch)
		if err := r.PrepareTaskQuestionBatch(*req.QuestionBatch, sessionID, req.AttentionTarget, strings.TrimSpace(req.Question), snapshot.CreatedAt); err != nil {
			logAttentionNotificationOperationFailure("prepare task question batch", sessionID, req.ToolCallID, err)
			return
		}
		if err := r.questionBatches.MarkMaterialized(stepID, req.ToolCallID); err != nil {
			logAttentionNotificationOperationFailure("materialize task question batch", sessionID, req.ToolCallID, err)
		}
		return
	}
	event := attentionPendingEventFromPrompt(sessionID, snapshot)
	if event.Pending != nil {
		if err := r.attentionBroker.PublishPending(attentionScopeForRequest(sessionID, req), *event.Pending); err != nil {
			logAttentionNotificationOperationFailure("publish pending prompt", sessionID, req.ToolCallID, err)
		}
	}
}

func (r *RuntimeRegistry) publishAttentionResolved(sessionID string, snapshot PendingPromptSnapshot) {
	if r == nil || r.attentionBroker == nil {
		return
	}
	req := snapshot.Request
	if req.QuestionBatch != nil && req.AttentionTarget != nil && req.AttentionTarget.Kind == clientui.AttentionNotificationTargetWorkflowTask {
		return
	}
	if req.IsTaskScopedApprovalQuestion() {
		return
	}
	kind := promptNotificationKind(req)
	id := clientui.AttentionNotificationID{
		Kind: kind,
		UUID: strings.TrimSpace(req.ToolCallID),
	}
	if err := r.attentionBroker.PublishResolved(attentionScopeForRequest(sessionID, req), id, kind, time.Now().UTC()); err != nil {
		logAttentionNotificationOperationFailure("publish resolved prompt", sessionID, req.ToolCallID, err)
	}
}

func (r *RuntimeRegistry) MarkTaskApprovalQuestionCleared(target clientui.AttentionNotificationTarget, askID string) {
	if r == nil || r.attentionBroker == nil {
		return
	}
	id := clientui.AttentionNotificationID{
		Kind: clientui.AttentionNotificationKindQuestion,
		UUID: strings.TrimSpace(askID),
	}
	if err := r.attentionBroker.PublishResolved(attentionnotify.RoutingScope{Kind: attentionnotify.RoutingWorkflowTask, TaskID: target.TaskID, SessionID: target.SessionID}, id, clientui.AttentionNotificationKindQuestion, time.Now().UTC()); err != nil {
		logAttentionNotificationOperationFailure("publish task approval question resolved", target.SessionID, askID, err)
	}
}

func (r *RuntimeRegistry) PrepareTaskQuestionBatch(batch askquestion.AskQuestionBatchMetadata, sessionID string, target *clientui.AttentionNotificationTarget, preview string, occurredAt time.Time) error {
	if r == nil || r.attentionBroker == nil {
		return nil
	}
	if target == nil || target.Kind != clientui.AttentionNotificationTargetWorkflowTask {
		return fmt.Errorf("task question batch Step %q cannot be prepared without task-detail target", batch.StepID)
	}
	if r.questionBatches == nil {
		r.questionBatches = attentionnotify.NewQuestionBatchTracker(r.attentionBroker)
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	return r.questionBatches.Prepare(attentionnotify.QuestionBatch{
		StepID:         questionBatchStepID(batch),
		Route:          attentionnotify.RoutingScope{Kind: attentionnotify.RoutingWorkflowTask, TaskID: target.TaskID, SessionID: sessionID},
		Target:         *target,
		Preview:        strings.TrimSpace(preview),
		PreparedAskIDs: append([]string(nil), batch.BatchToolCallIDs...),
		OccurredAt:     occurredAt,
	})
}

func (r *RuntimeRegistry) MarkTaskQuestionCleared(batch askquestion.AskQuestionBatchMetadata, askID string) {
	if r == nil || r.questionBatches == nil {
		return
	}
	if err := r.questionBatches.MarkDurablyCleared(questionBatchStepID(batch), askID); err != nil {
		logAttentionNotificationOperationFailure("mark task question cleared", "", askID, err)
	}
}

func (r *RuntimeRegistry) MarkTaskQuestionSkipped(batch askquestion.AskQuestionBatchMetadata) {
	if r == nil || r.questionBatches == nil {
		return
	}
	if err := r.questionBatches.MarkSkipped(questionBatchStepID(batch), batch.ToolCallID); err != nil {
		logAttentionNotificationOperationFailure("mark task question skipped", "", batch.ToolCallID, err)
	}
}

func logAttentionNotificationOperationFailure(operation string, sessionID string, askID string, err error) {
	if err == nil {
		return
	}
	slog.Warn("attention notification operation failed", "operation", operation, "session_id", sessionID, "ask_id", askID, "error", err)
}

func attentionPendingEventFromPrompt(sessionID string, snapshot PendingPromptSnapshot) clientui.AttentionNotificationEvent {
	kind := promptNotificationKind(snapshot.Request)
	target := clientui.AttentionNotificationTarget{
		Kind:      clientui.AttentionNotificationTargetSessionPrompt,
		SessionID: sessionID,
	}
	if snapshot.Request.IsTaskScopedApprovalQuestion() && snapshot.Request.AttentionTarget != nil {
		target = *snapshot.Request.AttentionTarget
	}
	notification := clientui.AttentionNotification{
		ID: clientui.AttentionNotificationID{
			Kind: kind,
			UUID: strings.TrimSpace(snapshot.Request.ToolCallID),
		},
		Kind:       kind,
		OccurredAt: snapshot.CreatedAt,
		Revision:   1,
		Target:     target,
	}
	if snapshot.Request.Approval && !snapshot.Request.IsTaskScopedApprovalQuestion() {
		notification.Approval = &clientui.AttentionNotificationApprovalState{
			AccessTargets: append([]clientui.FileAccessTarget(nil), snapshot.Request.AccessTargets...),
		}
		if message := strings.TrimSpace(snapshot.Request.Question); message != "" {
			notification.Approval.Message = &message
		}
	} else {
		notification.Question = &clientui.AttentionNotificationQuestionState{
			PreparedAskIDs:          []string{snapshot.Request.ToolCallID},
			MaterializedAskIDs:      []string{snapshot.Request.ToolCallID},
			CurrentUnresolvedAskIDs: []string{snapshot.Request.ToolCallID},
			Preview:                 strings.TrimSpace(snapshot.Request.Question),
			DisplayCount:            1,
			MaterializedCount:       1,
		}
	}
	return clientui.AttentionNotificationEvent{
		Type:    clientui.AttentionNotificationEventPending,
		Pending: &notification,
	}
}

func attentionScopeForRequest(sessionID string, req askquestion.AskQuestionRequest) attentionnotify.RoutingScope {
	if req.AttentionTarget != nil && req.AttentionTarget.Kind == clientui.AttentionNotificationTargetWorkflowTask {
		return attentionnotify.RoutingScope{Kind: attentionnotify.RoutingWorkflowTask, TaskID: req.AttentionTarget.TaskID, SessionID: sessionID}
	}
	return attentionnotify.RoutingScope{Kind: attentionnotify.RoutingSessionPrompt, SessionID: sessionID}
}

func promptNotificationKind(req askquestion.AskQuestionRequest) clientui.AttentionNotificationKind {
	if req.Approval && !req.IsTaskScopedApprovalQuestion() {
		return clientui.AttentionNotificationKindApproval
	}
	return clientui.AttentionNotificationKindQuestion
}

func questionBatchStepID(batch askquestion.AskQuestionBatchMetadata) string {
	return strings.TrimSpace(batch.StepID)
}
