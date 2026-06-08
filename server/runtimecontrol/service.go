package runtimecontrol

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"builder/prompts"
	"builder/server/primaryrun"
	"builder/server/requestmemo"
	"builder/server/runtime"
	"builder/server/session"
	"builder/shared/serverapi"
	"builder/shared/servicecontract"
	"builder/shared/transcript"
)

type RuntimeResolver interface {
	ResolveRuntime(ctx context.Context, sessionID string) (*runtime.Engine, error)
}

type ControllerLeaseVerifier interface {
	RequireControllerLease(ctx context.Context, sessionID string, leaseID string) error
}

type Service struct {
	runtimes       RuntimeResolver
	gate           primaryrun.Gate
	control        ControllerLeaseVerifier
	sessionNames   *requestmemo.Memo[sessionStringMemoRequest, struct{}]
	thinkingLevels *requestmemo.Memo[sessionStringMemoRequest, struct{}]
	fastModes      *requestmemo.Memo[sessionBoolMemoRequest, serverapi.RuntimeSetFastModeEnabledResponse]
	reviewers      *requestmemo.Memo[sessionBoolMemoRequest, serverapi.RuntimeSetReviewerEnabledResponse]
	autoCompacts   *requestmemo.Memo[sessionBoolMemoRequest, serverapi.RuntimeSetAutoCompactionEnabledResponse]
	submits        *requestmemo.Memo[sessionTextMemoRequest, serverapi.RuntimeSubmitUserMessageResponse]
	turnSubmits    *requestmemo.Memo[sessionTextMemoRequest, serverapi.RuntimeSubmitUserTurnResponse]
	queues         *requestmemo.Memo[sessionTextMemoRequest, serverapi.RuntimeQueueUserMessageResponse]
	shells         *requestmemo.Memo[sessionCommandMemoRequest, struct{}]

	localEntries         *requestmemo.Memo[localEntryMemoRequest, struct{}]
	compactions          *requestmemo.Memo[sessionStringMemoRequest, struct{}]
	preSubmitCompactions *requestmemo.Memo[sessionOnlyMemoRequest, struct{}]
	queuedSubmits        *requestmemo.Memo[sessionOnlyMemoRequest, serverapi.RuntimeSubmitQueuedUserMessagesResponse]
	interrupts           *requestmemo.Memo[sessionOnlyMemoRequest, struct{}]
	queuedDiscards       *requestmemo.Memo[queuedUserMessageMemoRequest, serverapi.RuntimeDiscardQueuedUserMessageResponse]
	promptHistory        *requestmemo.Memo[sessionTextMemoRequest, struct{}]
	goals                *requestmemo.Memo[goalSetMemoRequest, serverapi.RuntimeGoalShowResponse]
	goalStatuses         *requestmemo.Memo[goalStatusMemoRequest, serverapi.RuntimeGoalShowResponse]
	goalClears           *requestmemo.Memo[goalClearMemoRequest, serverapi.RuntimeGoalShowResponse]
}

type sessionStringMemoRequest struct {
	SessionID string
	Value     string
}

type sessionBoolMemoRequest struct {
	SessionID string
	Enabled   bool
}

type sessionTextMemoRequest struct {
	SessionID string
	Text      string
}

type queuedUserMessageMemoRequest struct {
	SessionID   string
	QueueItemID string
}

type sessionCommandMemoRequest struct {
	SessionID string
	Command   string
}

type sessionOnlyMemoRequest struct {
	SessionID string
}

type localEntryMemoRequest struct {
	SessionID  string
	Role       string
	Text       string
	Visibility transcript.EntryVisibility
	NoticeID   string
}

type goalSetMemoRequest struct {
	SessionID string
	Objective string
	Actor     string
}

type goalStatusMemoRequest struct {
	SessionID string
	Status    string
	Actor     string
}

type goalClearMemoRequest struct {
	SessionID string
	Actor     string
}

func NewService(runtimes RuntimeResolver, gate primaryrun.Gate) *Service {
	return &Service{
		runtimes:       runtimes,
		gate:           gate,
		sessionNames:   requestmemo.New[sessionStringMemoRequest, struct{}](),
		thinkingLevels: requestmemo.New[sessionStringMemoRequest, struct{}](),
		fastModes:      requestmemo.New[sessionBoolMemoRequest, serverapi.RuntimeSetFastModeEnabledResponse](),
		reviewers:      requestmemo.New[sessionBoolMemoRequest, serverapi.RuntimeSetReviewerEnabledResponse](),
		autoCompacts:   requestmemo.New[sessionBoolMemoRequest, serverapi.RuntimeSetAutoCompactionEnabledResponse](),
		submits:        requestmemo.New[sessionTextMemoRequest, serverapi.RuntimeSubmitUserMessageResponse](),
		turnSubmits:    requestmemo.New[sessionTextMemoRequest, serverapi.RuntimeSubmitUserTurnResponse](),
		queues:         requestmemo.New[sessionTextMemoRequest, serverapi.RuntimeQueueUserMessageResponse](),
		shells:         requestmemo.New[sessionCommandMemoRequest, struct{}](),

		localEntries:         requestmemo.New[localEntryMemoRequest, struct{}](),
		compactions:          requestmemo.New[sessionStringMemoRequest, struct{}](),
		preSubmitCompactions: requestmemo.New[sessionOnlyMemoRequest, struct{}](),
		queuedSubmits:        requestmemo.New[sessionOnlyMemoRequest, serverapi.RuntimeSubmitQueuedUserMessagesResponse](),
		interrupts:           requestmemo.New[sessionOnlyMemoRequest, struct{}](),
		queuedDiscards:       requestmemo.New[queuedUserMessageMemoRequest, serverapi.RuntimeDiscardQueuedUserMessageResponse](),
		promptHistory:        requestmemo.New[sessionTextMemoRequest, struct{}](),
		goals:                requestmemo.New[goalSetMemoRequest, serverapi.RuntimeGoalShowResponse](),
		goalStatuses:         requestmemo.New[goalStatusMemoRequest, serverapi.RuntimeGoalShowResponse](),
		goalClears:           requestmemo.New[goalClearMemoRequest, serverapi.RuntimeGoalShowResponse](),
	}
}

func (s *Service) WithControllerLeaseVerifier(verifier ControllerLeaseVerifier) *Service {
	if s == nil {
		return nil
	}
	s.control = verifier
	return s
}

func (s *Service) requireControllerLease(ctx context.Context, sessionID string, leaseID string) error {
	if s == nil || s.control == nil {
		return nil
	}
	return s.control.RequireControllerLease(ctx, sessionID, leaseID)
}

func (s *Service) resolve(ctx context.Context, sessionID string) (*runtime.Engine, error) {
	if s == nil || s.runtimes == nil {
		return nil, fmt.Errorf("runtime resolver is required")
	}
	engine, err := s.runtimes.ResolveRuntime(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return nil, err
	}
	if engine == nil {
		return nil, errors.Join(serverapi.ErrRuntimeUnavailable, fmt.Errorf("runtime for session %q is unavailable", strings.TrimSpace(sessionID)))
	}
	return engine, nil
}

func (s *Service) SetSessionName(ctx context.Context, req serverapi.RuntimeSetSessionNameRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	memoReq := sessionStringMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Value: req.Name}
	_, err := s.sessionNames.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameSessionStringMemoRequest, func(ctx context.Context) (struct{}, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return struct{}{}, err
		}
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return struct{}{}, err
		}
		return struct{}{}, engine.SetSessionName(req.Name)
	})
	return err
}

func (s *Service) SetThinkingLevel(ctx context.Context, req serverapi.RuntimeSetThinkingLevelRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	memoReq := sessionStringMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Value: req.Level}
	_, err := s.thinkingLevels.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameSessionStringMemoRequest, func(ctx context.Context) (struct{}, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return struct{}{}, err
		}
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return struct{}{}, err
		}
		return struct{}{}, engine.SetThinkingLevel(req.Level)
	})
	return err
}

func (s *Service) SetFastModeEnabled(ctx context.Context, req serverapi.RuntimeSetFastModeEnabledRequest) (serverapi.RuntimeSetFastModeEnabledResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.RuntimeSetFastModeEnabledResponse{}, err
	}
	memoReq := sessionBoolMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Enabled: req.Enabled}
	return s.fastModes.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameSessionBoolMemoRequest, func(ctx context.Context) (serverapi.RuntimeSetFastModeEnabledResponse, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return serverapi.RuntimeSetFastModeEnabledResponse{}, err
		}
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return serverapi.RuntimeSetFastModeEnabledResponse{}, err
		}
		changed, err := engine.SetFastModeEnabled(req.Enabled)
		if err != nil {
			return serverapi.RuntimeSetFastModeEnabledResponse{}, err
		}
		return serverapi.RuntimeSetFastModeEnabledResponse{Changed: changed}, nil
	})
}

func (s *Service) SetReviewerEnabled(ctx context.Context, req serverapi.RuntimeSetReviewerEnabledRequest) (serverapi.RuntimeSetReviewerEnabledResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.RuntimeSetReviewerEnabledResponse{}, err
	}
	memoReq := sessionBoolMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Enabled: req.Enabled}
	return s.reviewers.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameSessionBoolMemoRequest, func(ctx context.Context) (serverapi.RuntimeSetReviewerEnabledResponse, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return serverapi.RuntimeSetReviewerEnabledResponse{}, err
		}
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return serverapi.RuntimeSetReviewerEnabledResponse{}, err
		}
		changed, mode, err := engine.SetReviewerEnabled(req.Enabled)
		if err != nil {
			return serverapi.RuntimeSetReviewerEnabledResponse{}, err
		}
		return serverapi.RuntimeSetReviewerEnabledResponse{Changed: changed, Mode: mode}, nil
	})
}

func (s *Service) SetAutoCompactionEnabled(ctx context.Context, req serverapi.RuntimeSetAutoCompactionEnabledRequest) (serverapi.RuntimeSetAutoCompactionEnabledResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.RuntimeSetAutoCompactionEnabledResponse{}, err
	}
	memoReq := sessionBoolMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Enabled: req.Enabled}
	return s.autoCompacts.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameSessionBoolMemoRequest, func(ctx context.Context) (serverapi.RuntimeSetAutoCompactionEnabledResponse, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return serverapi.RuntimeSetAutoCompactionEnabledResponse{}, err
		}
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return serverapi.RuntimeSetAutoCompactionEnabledResponse{}, err
		}
		changed, enabled := engine.SetAutoCompactionEnabled(req.Enabled)
		return serverapi.RuntimeSetAutoCompactionEnabledResponse{Changed: changed, Enabled: enabled}, nil
	})
}

func (s *Service) AppendLocalEntry(ctx context.Context, req serverapi.RuntimeAppendLocalEntryRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	visibility := transcript.NormalizeEntryVisibility(transcript.EntryVisibility(req.Visibility))
	memoReq := localEntryMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Role: strings.TrimSpace(req.Role), Text: req.Text, Visibility: visibility, NoticeID: strings.TrimSpace(req.NoticeID)}
	_, err := s.localEntries.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameLocalEntryMemoRequest, func(ctx context.Context) (struct{}, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return struct{}{}, err
		}
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return struct{}{}, err
		}
		if visibility == transcript.EntryVisibilityAuto && strings.TrimSpace(req.NoticeID) != "" {
			engine.AppendLocalEntryWithNoticeID(req.Role, req.Text, req.NoticeID)
		} else if visibility == transcript.EntryVisibilityAuto {
			engine.AppendLocalEntry(req.Role, req.Text)
		} else {
			engine.AppendLocalEntryWithVisibility(req.Role, req.Text, visibility)
		}
		return struct{}{}, nil
	})
	return err
}

func (s *Service) AppendSessionEntry(ctx context.Context, sessionID string, role string, text string) error {
	trimmedSessionID := strings.TrimSpace(sessionID)
	trimmedRole := strings.TrimSpace(role)
	trimmedText := strings.TrimSpace(text)
	if trimmedSessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if trimmedRole == "" {
		return fmt.Errorf("role is required")
	}
	if trimmedText == "" {
		return fmt.Errorf("text is required")
	}
	engine, err := s.resolve(ctx, trimmedSessionID)
	if err != nil {
		return err
	}
	engine.AppendLocalEntry(trimmedRole, trimmedText)
	return nil
}

func (s *Service) ShouldCompactBeforeUserMessage(ctx context.Context, req serverapi.RuntimeShouldCompactBeforeUserMessageRequest) (serverapi.RuntimeShouldCompactBeforeUserMessageResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.RuntimeShouldCompactBeforeUserMessageResponse{}, err
	}
	engine, err := s.resolve(ctx, req.SessionID)
	if err != nil {
		return serverapi.RuntimeShouldCompactBeforeUserMessageResponse{}, err
	}
	shouldCompact, err := engine.ShouldCompactBeforeUserMessage(ctx, req.Text)
	if err != nil {
		return serverapi.RuntimeShouldCompactBeforeUserMessageResponse{}, err
	}
	return serverapi.RuntimeShouldCompactBeforeUserMessageResponse{ShouldCompact: shouldCompact}, nil
}

func (s *Service) SubmitUserMessage(ctx context.Context, req serverapi.RuntimeSubmitUserMessageRequest) (serverapi.RuntimeSubmitUserMessageResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.RuntimeSubmitUserMessageResponse{}, err
	}
	memoReq := sessionTextMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Text: req.Text}
	return s.submits.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameSessionTextMemoRequest, func(ctx context.Context) (serverapi.RuntimeSubmitUserMessageResponse, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return serverapi.RuntimeSubmitUserMessageResponse{}, err
		}
		lease, err := s.acquirePrimaryRun(memoReq.SessionID)
		if err != nil {
			return serverapi.RuntimeSubmitUserMessageResponse{}, err
		}
		defer lease.Release()
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return serverapi.RuntimeSubmitUserMessageResponse{}, err
		}
		runCtx := context.Background()
		if ctx != nil {
			runCtx = context.WithoutCancel(ctx)
		}
		msg, err := engine.SubmitUserMessage(runCtx, memoReq.Text)
		if err != nil {
			return serverapi.RuntimeSubmitUserMessageResponse{}, err
		}
		return serverapi.RuntimeSubmitUserMessageResponse{Message: msg.Content}, nil
	})
}

func (s *Service) SubmitUserShellCommand(ctx context.Context, req serverapi.RuntimeSubmitUserShellCommandRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	memoReq := sessionCommandMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Command: req.Command}
	_, err := s.shells.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameSessionCommandMemoRequest, func(ctx context.Context) (struct{}, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return struct{}{}, err
		}
		lease, err := s.acquirePrimaryRun(memoReq.SessionID)
		if err != nil {
			return struct{}{}, err
		}
		defer lease.Release()
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return struct{}{}, err
		}
		runCtx := context.Background()
		if ctx != nil {
			runCtx = context.WithoutCancel(ctx)
		}
		_, err = engine.SubmitUserShellCommand(runCtx, memoReq.Command)
		return struct{}{}, err
	})
	return err
}

func (s *Service) CompactContext(ctx context.Context, req serverapi.RuntimeCompactContextRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	memoReq := sessionStringMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Value: req.Args}
	_, err := s.compactions.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameSessionStringMemoRequest, func(ctx context.Context) (struct{}, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return struct{}{}, err
		}
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return struct{}{}, err
		}
		runCtx := context.Background()
		if ctx != nil {
			runCtx = context.WithoutCancel(ctx)
		}
		return struct{}{}, engine.CompactContext(runCtx, req.Args)
	})
	return err
}

func (s *Service) CompactContextForPreSubmit(ctx context.Context, req serverapi.RuntimeCompactContextForPreSubmitRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	memoReq := sessionOnlyMemoRequest{SessionID: strings.TrimSpace(req.SessionID)}
	_, err := s.preSubmitCompactions.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, func(a sessionOnlyMemoRequest, b sessionOnlyMemoRequest) bool { return a.SessionID == b.SessionID }, func(ctx context.Context) (struct{}, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return struct{}{}, err
		}
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return struct{}{}, err
		}
		runCtx := context.Background()
		if ctx != nil {
			runCtx = context.WithoutCancel(ctx)
		}
		return struct{}{}, engine.CompactContextForPreSubmit(runCtx)
	})
	return err
}

func (s *Service) HasQueuedUserWork(ctx context.Context, req serverapi.RuntimeHasQueuedUserWorkRequest) (serverapi.RuntimeHasQueuedUserWorkResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.RuntimeHasQueuedUserWorkResponse{}, err
	}
	engine, err := s.resolve(ctx, req.SessionID)
	if err != nil {
		return serverapi.RuntimeHasQueuedUserWorkResponse{}, err
	}
	return serverapi.RuntimeHasQueuedUserWorkResponse{HasQueuedUserWork: engine.HasQueuedUserWork()}, nil
}

func (s *Service) SubmitQueuedUserMessages(ctx context.Context, req serverapi.RuntimeSubmitQueuedUserMessagesRequest) (serverapi.RuntimeSubmitQueuedUserMessagesResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.RuntimeSubmitQueuedUserMessagesResponse{}, err
	}
	memoReq := sessionOnlyMemoRequest{SessionID: strings.TrimSpace(req.SessionID)}
	return s.queuedSubmits.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, func(a sessionOnlyMemoRequest, b sessionOnlyMemoRequest) bool { return a.SessionID == b.SessionID }, func(ctx context.Context) (serverapi.RuntimeSubmitQueuedUserMessagesResponse, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return serverapi.RuntimeSubmitQueuedUserMessagesResponse{}, err
		}
		lease, err := s.acquirePrimaryRun(req.SessionID)
		if err != nil {
			return serverapi.RuntimeSubmitQueuedUserMessagesResponse{}, err
		}
		defer lease.Release()
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return serverapi.RuntimeSubmitQueuedUserMessagesResponse{}, err
		}
		runCtx := context.Background()
		if ctx != nil {
			runCtx = context.WithoutCancel(ctx)
		}
		msg, err := engine.SubmitQueuedUserMessages(runCtx)
		if err != nil {
			return serverapi.RuntimeSubmitQueuedUserMessagesResponse{}, err
		}
		return serverapi.RuntimeSubmitQueuedUserMessagesResponse{Message: msg.Content}, nil
	})
}

func (s *Service) Interrupt(ctx context.Context, req serverapi.RuntimeInterruptRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	memoReq := sessionOnlyMemoRequest{SessionID: strings.TrimSpace(req.SessionID)}
	_, err := s.interrupts.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, func(a sessionOnlyMemoRequest, b sessionOnlyMemoRequest) bool { return a.SessionID == b.SessionID }, func(ctx context.Context) (struct{}, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return struct{}{}, err
		}
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return struct{}{}, err
		}
		return struct{}{}, engine.Interrupt()
	})
	return err
}

func (s *Service) QueueUserMessage(ctx context.Context, req serverapi.RuntimeQueueUserMessageRequest) (serverapi.RuntimeQueueUserMessageResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.RuntimeQueueUserMessageResponse{}, err
	}
	memoReq := sessionTextMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Text: req.Text}
	return s.queues.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameSessionTextMemoRequest, func(ctx context.Context) (serverapi.RuntimeQueueUserMessageResponse, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return serverapi.RuntimeQueueUserMessageResponse{}, err
		}
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return serverapi.RuntimeQueueUserMessageResponse{}, err
		}
		item := engine.QueueUserMessage(memoReq.Text)
		return serverapi.RuntimeQueueUserMessageResponse{QueueItemID: item.ID, Text: item.Text}, nil
	})
}

func (s *Service) DiscardQueuedUserMessage(ctx context.Context, req serverapi.RuntimeDiscardQueuedUserMessageRequest) (serverapi.RuntimeDiscardQueuedUserMessageResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.RuntimeDiscardQueuedUserMessageResponse{}, err
	}
	memoReq := queuedUserMessageMemoRequest{SessionID: strings.TrimSpace(req.SessionID), QueueItemID: strings.TrimSpace(req.QueueItemID)}
	return s.queuedDiscards.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameQueuedUserMessageMemoRequest, func(ctx context.Context) (serverapi.RuntimeDiscardQueuedUserMessageResponse, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return serverapi.RuntimeDiscardQueuedUserMessageResponse{}, err
		}
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return serverapi.RuntimeDiscardQueuedUserMessageResponse{}, err
		}
		return serverapi.RuntimeDiscardQueuedUserMessageResponse{Discarded: engine.DiscardQueuedUserMessage(req.QueueItemID)}, nil
	})
}

func (s *Service) RecordPromptHistory(ctx context.Context, req serverapi.RuntimeRecordPromptHistoryRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	memoReq := sessionTextMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Text: req.Text}
	_, err := s.promptHistory.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameSessionTextMemoRequest, func(ctx context.Context) (struct{}, error) {
		if err := s.requireControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return struct{}{}, err
		}
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return struct{}{}, err
		}
		return struct{}{}, engine.RecordPromptHistory(req.Text)
	})
	return err
}

func (s *Service) ShowGoal(ctx context.Context, req serverapi.RuntimeGoalShowRequest) (serverapi.RuntimeGoalShowResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.RuntimeGoalShowResponse{}, err
	}
	engine, err := s.resolve(ctx, req.SessionID)
	if err != nil {
		return serverapi.RuntimeGoalShowResponse{}, err
	}
	goal := engine.Goal()
	if goal == nil {
		return serverapi.RuntimeGoalShowResponse{}, nil
	}
	return serverapi.RuntimeGoalShowResponse{Goal: &serverapi.RuntimeGoal{
		ID:        strings.TrimSpace(goal.ID),
		Objective: goal.Objective,
		Status:    strings.TrimSpace(string(goal.Status)),
		Suspended: engine.GoalLoopSuspended(),
		CreatedAt: goal.CreatedAt,
		UpdatedAt: goal.UpdatedAt,
	}}, nil
}

func (s *Service) SetGoal(ctx context.Context, req serverapi.RuntimeGoalSetRequest) (serverapi.RuntimeGoalShowResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.RuntimeGoalShowResponse{}, err
	}
	trimmedObjective := strings.TrimSpace(req.Objective)
	memoReq := goalSetMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Objective: trimmedObjective, Actor: strings.TrimSpace(req.Actor)}
	return s.goals.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameGoalSetMemoRequest, func(ctx context.Context) (serverapi.RuntimeGoalShowResponse, error) {
		if err := s.requireOptionalControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return serverapi.RuntimeGoalShowResponse{}, err
		}
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return serverapi.RuntimeGoalShowResponse{}, err
		}
		if strings.TrimSpace(req.Actor) == string(session.GoalActorAgent) {
			currentGoal := engine.Goal()
			if goalBlocksAgentSet(currentGoal) {
				return serverapi.RuntimeGoalShowResponse{}, errors.New(strings.TrimSpace(prompts.RenderGoalAgentDuplicateSetDeniedPrompt(currentGoal.Objective, string(currentGoal.Status))))
			}
		}
		if err := engine.RequireGoalLoopStartAllowed(); err != nil {
			return serverapi.RuntimeGoalShowResponse{}, err
		}
		goal, err := engine.SetGoal(trimmedObjective, session.GoalActor(req.Actor))
		if err != nil {
			var blocked session.GoalAgentOverwriteBlockedError
			if errors.As(err, &blocked) {
				return serverapi.RuntimeGoalShowResponse{}, errors.New(strings.TrimSpace(prompts.RenderGoalAgentDuplicateSetDeniedPrompt(blocked.Goal.Objective, string(blocked.Goal.Status))))
			}
			return serverapi.RuntimeGoalShowResponse{}, err
		}
		if err := engine.StartGoalLoop(); err != nil {
			return serverapi.RuntimeGoalShowResponse{}, err
		}
		return serverapi.RuntimeGoalShowResponse{Goal: &serverapi.RuntimeGoal{
			ID:        strings.TrimSpace(goal.ID),
			Objective: goal.Objective,
			Status:    strings.TrimSpace(string(goal.Status)),
			Suspended: false,
			CreatedAt: goal.CreatedAt,
			UpdatedAt: goal.UpdatedAt,
		}}, nil
	})
}

func goalBlocksAgentSet(goal *session.GoalState) bool {
	return goal != nil && goal.Status != session.GoalStatusComplete
}

func (s *Service) PauseGoal(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return s.setGoalStatus(ctx, req, session.GoalStatusPaused)
}

func (s *Service) ResumeGoal(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return s.setGoalStatus(ctx, req, session.GoalStatusActive)
}

func (s *Service) CompleteGoal(ctx context.Context, req serverapi.RuntimeGoalStatusRequest) (serverapi.RuntimeGoalShowResponse, error) {
	return s.setGoalStatus(ctx, req, session.GoalStatusComplete)
}

func (s *Service) setGoalStatus(ctx context.Context, req serverapi.RuntimeGoalStatusRequest, status session.GoalStatus) (serverapi.RuntimeGoalShowResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.RuntimeGoalShowResponse{}, err
	}
	memoReq := goalStatusMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Status: strings.TrimSpace(string(status)), Actor: strings.TrimSpace(req.Actor)}
	return s.goalStatuses.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameGoalStatusMemoRequest, func(ctx context.Context) (serverapi.RuntimeGoalShowResponse, error) {
		if err := s.requireOptionalControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return serverapi.RuntimeGoalShowResponse{}, err
		}
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return serverapi.RuntimeGoalShowResponse{}, err
		}
		if status == session.GoalStatusComplete {
			current := engine.Goal()
			if current != nil && current.Status == session.GoalStatusComplete {
				return serverapi.RuntimeGoalShowResponse{Goal: &serverapi.RuntimeGoal{
					ID:        strings.TrimSpace(current.ID),
					Objective: current.Objective,
					Status:    strings.TrimSpace(string(current.Status)),
					Suspended: false,
					CreatedAt: current.CreatedAt,
					UpdatedAt: current.UpdatedAt,
				}}, nil
			}
		}
		if status == session.GoalStatusActive {
			if err := engine.RequireGoalLoopStartAllowed(); err != nil {
				return serverapi.RuntimeGoalShowResponse{}, err
			}
		}
		goal, err := engine.SetGoalStatus(status, session.GoalActor(req.Actor))
		if err != nil {
			return serverapi.RuntimeGoalShowResponse{}, err
		}
		if status == session.GoalStatusActive {
			if err := engine.StartGoalLoop(); err != nil {
				return serverapi.RuntimeGoalShowResponse{}, err
			}
		}
		return serverapi.RuntimeGoalShowResponse{Goal: &serverapi.RuntimeGoal{
			ID:        strings.TrimSpace(goal.ID),
			Objective: goal.Objective,
			Status:    strings.TrimSpace(string(goal.Status)),
			Suspended: false,
			CreatedAt: goal.CreatedAt,
			UpdatedAt: goal.UpdatedAt,
		}}, nil
	})
}

func (s *Service) ClearGoal(ctx context.Context, req serverapi.RuntimeGoalClearRequest) (serverapi.RuntimeGoalShowResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.RuntimeGoalShowResponse{}, err
	}
	memoReq := goalClearMemoRequest{SessionID: strings.TrimSpace(req.SessionID), Actor: strings.TrimSpace(req.Actor)}
	return s.goalClears.Do(ctx, strings.TrimSpace(req.ClientRequestID), memoReq, sameGoalClearMemoRequest, func(ctx context.Context) (serverapi.RuntimeGoalShowResponse, error) {
		if err := s.requireOptionalControllerLease(ctx, req.SessionID, req.ControllerLeaseID); err != nil {
			return serverapi.RuntimeGoalShowResponse{}, err
		}
		engine, err := s.resolve(ctx, req.SessionID)
		if err != nil {
			return serverapi.RuntimeGoalShowResponse{}, err
		}
		if _, err := engine.ClearGoal(session.GoalActor(req.Actor)); err != nil {
			return serverapi.RuntimeGoalShowResponse{}, err
		}
		return serverapi.RuntimeGoalShowResponse{}, nil
	})
}

func (s *Service) requireOptionalControllerLease(ctx context.Context, sessionID string, leaseID string) error {
	if strings.TrimSpace(leaseID) == "" {
		return nil
	}
	return s.requireControllerLease(ctx, sessionID, leaseID)
}

func (s *Service) acquirePrimaryRun(sessionID string) (primaryrun.Lease, error) {
	if s == nil || s.gate == nil {
		return primaryrun.LeaseFunc(func() {}), nil
	}
	return s.gate.AcquirePrimaryRun(strings.TrimSpace(sessionID))
}

func sameSessionTextMemoRequest(a sessionTextMemoRequest, b sessionTextMemoRequest) bool {
	return a.SessionID == b.SessionID && a.Text == b.Text
}

func sameQueuedUserMessageMemoRequest(a queuedUserMessageMemoRequest, b queuedUserMessageMemoRequest) bool {
	return a.SessionID == b.SessionID && a.QueueItemID == b.QueueItemID
}

func sameSessionStringMemoRequest(a sessionStringMemoRequest, b sessionStringMemoRequest) bool {
	return a.SessionID == b.SessionID && a.Value == b.Value
}

func sameSessionBoolMemoRequest(a sessionBoolMemoRequest, b sessionBoolMemoRequest) bool {
	return a.SessionID == b.SessionID && a.Enabled == b.Enabled
}

func sameSessionCommandMemoRequest(a sessionCommandMemoRequest, b sessionCommandMemoRequest) bool {
	return a.SessionID == b.SessionID && a.Command == b.Command
}

func sameLocalEntryMemoRequest(a localEntryMemoRequest, b localEntryMemoRequest) bool {
	return a.SessionID == b.SessionID && a.Role == b.Role && a.Text == b.Text && a.Visibility == b.Visibility && a.NoticeID == b.NoticeID
}

func sameGoalSetMemoRequest(a goalSetMemoRequest, b goalSetMemoRequest) bool {
	return a.SessionID == b.SessionID && a.Objective == b.Objective && a.Actor == b.Actor
}

func sameGoalStatusMemoRequest(a goalStatusMemoRequest, b goalStatusMemoRequest) bool {
	return a.SessionID == b.SessionID && a.Status == b.Status && a.Actor == b.Actor
}

func sameGoalClearMemoRequest(a goalClearMemoRequest, b goalClearMemoRequest) bool {
	return a.SessionID == b.SessionID && a.Actor == b.Actor
}

var _ servicecontract.RuntimeControlService = (*Service)(nil)
