package clientui

import (
	"context"
	"errors"
	"strings"

	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"core/shared/textutil"
	"google.golang.org/protobuf/proto"
)

type QueuedUserMessage struct {
	ID   string
	Text string
}

type UserTurnResultKind string

const (
	UserTurnResultKindQueued         UserTurnResultKind = "queued"
	UserTurnResultKindNoFinal        UserTurnResultKind = "no_final"
	UserTurnResultKindAssistantFinal UserTurnResultKind = "assistant_final"
	UserTurnResultKindSilentFinal    UserTurnResultKind = "silent_final"
)

type UserTurnSubmission struct {
	Message    *string
	ResultKind UserTurnResultKind
	Queued     QueuedUserMessage
}

func NormalizeSessionExecutionTarget(target *worktreepb.SessionExecutionTarget) *worktreepb.SessionExecutionTarget {
	if target == nil {
		return nil
	}
	normalized := proto.Clone(target).(*worktreepb.SessionExecutionTarget)
	if normalized.WorkspaceId != nil {
		value := strings.TrimSpace(*normalized.WorkspaceId)
		normalized.WorkspaceId = &value
	}
	normalized.WorkspaceName = strings.TrimSpace(normalized.WorkspaceName)
	normalized.WorkspaceRoot = strings.TrimSpace(normalized.WorkspaceRoot)
	normalized.CwdRelpath = strings.TrimSpace(normalized.CwdRelpath)
	normalized.EffectiveWorkdir = strings.TrimSpace(normalized.EffectiveWorkdir)
	if worktree := normalized.Worktree; worktree != nil {
		worktree.Id = strings.TrimSpace(worktree.Id)
		worktree.Name = strings.TrimSpace(worktree.Name)
		worktree.Root = strings.TrimSpace(worktree.Root)
	}
	return normalized
}

func SessionExecutionTargetIsZero(target *worktreepb.SessionExecutionTarget) bool {
	return target == nil
}

func SessionExecutionTargetsEqual(a, b *worktreepb.SessionExecutionTarget) bool {
	normalizedA := NormalizeSessionExecutionTarget(a)
	normalizedB := NormalizeSessionExecutionTarget(b)
	if normalizedA == nil || normalizedB == nil {
		return normalizedA == normalizedB
	}
	worktreesEqual := normalizedA.Worktree == normalizedB.Worktree
	if normalizedA.Worktree != nil && normalizedB.Worktree != nil {
		worktreesEqual = normalizedA.Worktree.Id == normalizedB.Worktree.Id &&
			normalizedA.Worktree.Name == normalizedB.Worktree.Name &&
			normalizedA.Worktree.Root == normalizedB.Worktree.Root &&
			normalizedA.Worktree.Availability == normalizedB.Worktree.Availability
	}
	return textutil.EqualOptional(normalizedA.WorkspaceId, normalizedB.WorkspaceId) &&
		normalizedA.WorkspaceName == normalizedB.WorkspaceName &&
		normalizedA.WorkspaceRoot == normalizedB.WorkspaceRoot &&
		normalizedA.WorkspaceAvailability == normalizedB.WorkspaceAvailability &&
		worktreesEqual &&
		normalizedA.CwdRelpath == normalizedB.CwdRelpath &&
		normalizedA.EffectiveWorkdir == normalizedB.EffectiveWorkdir
}

func SessionExecutionWorkspaceRoot(target *worktreepb.SessionExecutionTarget, fallback string) (string, error) {
	if target.GetWorktree() == nil {
		return fallback, nil
	}
	root := strings.TrimSpace(target.Worktree.Root)
	if root == "" {
		return "", errors.New("session execution worktree root is required")
	}
	return root, nil
}

type RuntimeClient interface {
	MainView() *runtimepb.MainView
	RefreshMainView() (*runtimepb.MainView, error)
	Status() *runtimepb.Status
	SessionView() *runtimepb.SessionView
	SetSessionName(name string) error
	ShowGoal() (*runtimepb.GoalView, error)
	SetGoal(objective string) (*runtimepb.GoalSetSuccess, error)
	PauseGoal() (*runtimepb.GoalMutationSuccess, error)
	ResumeGoal() (*runtimepb.GoalMutationSuccess, error)
	CompleteGoal() (*runtimepb.GoalMutationSuccess, error)
	ClearGoal() (*runtimepb.GoalMutationSuccess, error)
	AppendCommittedEntry(role, text string) error
	AppendCommittedEntryWithNoticeID(role, text, noticeID string) error
	SubmitRuntimeInput(ctx context.Context, req RuntimeSubmitRequest) (UserTurnSubmission, error)
	RunUserShell(ctx context.Context, req RuntimeShellRequest) error
	CompactRuntime(ctx context.Context, req RuntimeCompactRequest) error
	Interrupt() error
	DiscardQueuedUserMessage(queueItemID string) bool
	RecordPromptHistory(text string) error
}
