package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

var errForkReplayBoundary = errors.New("fork replay boundary reached")

func ForkAtUserMessage(parent *Store, userMessageIndex int, forkName string) (*Store, error) {
	if parent == nil {
		return nil, fmt.Errorf("parent store is required")
	}
	if userMessageIndex <= 0 {
		return nil, fmt.Errorf("user message index must be >= 1")
	}

	parentMeta := parent.Meta()
	replay := make([]ReplayEvent, 0)
	visibleUserCount := 0
	err := parent.WalkEvents(func(evt Event) error {
		if hasVisibleUserMessageEvent(evt.Kind, evt.Payload) {
			visibleUserCount++
			if visibleUserCount == userMessageIndex {
				return errForkReplayBoundary
			}
		}
		replay = append(replay, ReplayEvent{StepID: evt.StepID, Kind: evt.Kind, Payload: append([]byte(nil), evt.Payload...)})
		return nil
	})
	if err != nil && !errors.Is(err, errForkReplayBoundary) {
		return nil, fmt.Errorf("read parent events: %w", err)
	}

	if visibleUserCount < userMessageIndex {
		return nil, fmt.Errorf("user message index %d is out of range", userMessageIndex)
	}

	containerDir := filepath.Dir(parent.Dir())
	child, err := newLazyWithStoreOptions(containerDir, parentMeta.WorkspaceContainer, parentMeta.WorkspaceRoot, parent.options)
	if err != nil {
		return nil, err
	}

	child.mu.Lock()
	child.meta.Locked = cloneLockedContract(parentMeta.Locked)
	child.meta.AgentsInjected = parentMeta.AgentsInjected
	child.meta.CompactionSoonReminderIssued = reminderIssuedFromReplayEvents(replay)
	child.meta.WorktreeReminder = forkedWorktreeReminderState(parentMeta.WorktreeReminder)
	child.meta.UsageState = nil
	child.meta.ParentSessionID = parentMeta.SessionID
	child.meta.Name = strings.TrimSpace(forkName)
	child.meta.Continuation = cloneContinuationContext(parentMeta.Continuation)
	child.mu.Unlock()

	if len(replay) == 0 {
		if err := child.EnsureDurable(); err != nil {
			return nil, fmt.Errorf("persist empty fork replay: %w", err)
		}
		return child, nil
	}
	if _, err := child.AppendReplayEvents(replay); err != nil {
		return nil, fmt.Errorf("append fork replay events: %w", err)
	}
	return child, nil
}

func cloneLockedContract(in *LockedContract) *LockedContract {
	if in == nil {
		return nil
	}
	copyLocked := *in
	if len(in.EnabledTools) > 0 {
		copyLocked.EnabledTools = append([]string(nil), in.EnabledTools...)
	}
	if in.ToolPreambles != nil {
		toolPreambles := *in.ToolPreambles
		copyLocked.ToolPreambles = &toolPreambles
	}
	copyLocked.SystemPrompt = strings.TrimSpace(in.SystemPrompt)
	copyLocked.ReviewerPrompt = strings.TrimSpace(in.ReviewerPrompt)
	return &copyLocked
}

func cloneContinuationContext(in *ContinuationContext) *ContinuationContext {
	if in == nil {
		return nil
	}
	copyContext := *in
	return &copyContext
}

type ChildContextOptions struct {
	// InheritLockedContract preserves the parent's model/tool/prompt lock for
	// interactive child sessions. Headless subagent launches leave this false
	// so their first dispatch locks against role/config-provided settings.
	InheritLockedContract bool
	// InheritContinuation preserves parent continuation settings for
	// interactive children. Headless subagent launches leave this false so
	// parent role/base URL state cannot override the selected subagent config.
	InheritContinuation bool
}

// InitializeChildFromParent initializes a fresh child session with parent-owned
// execution context while leaving conversational state empty. The caller owns
// durability so launch planning can finish cross-store setup atomically.
func InitializeChildFromParent(child *Store, parent *Store) error {
	return InitializeChildFromParentWithOptions(child, parent, ChildContextOptions{
		InheritLockedContract: true,
		InheritContinuation:   true,
	})
}

// InitializeChildFromParentWithOptions initializes a fresh child session with
// selected parent-owned context while leaving conversational state empty. Use
// this for child sessions that need parent workspace/worktree targeting without
// inheriting the parent's model/tool/prompt lock.
func InitializeChildFromParentWithOptions(child *Store, parent *Store, opts ChildContextOptions) error {
	if child == nil {
		return fmt.Errorf("child store is required")
	}
	if parent == nil {
		return fmt.Errorf("parent store is required")
	}
	parentMeta := parent.Meta()
	child.mu.Lock()
	if opts.InheritLockedContract {
		child.meta.Locked = cloneLockedContract(parentMeta.Locked)
	} else {
		child.meta.Locked = nil
	}
	child.meta.AgentsInjected = false
	child.meta.WorkspaceRoot = parentMeta.WorkspaceRoot
	child.meta.WorkspaceContainer = parentMeta.WorkspaceContainer
	child.meta.WorktreeReminder = forkedWorktreeReminderState(parentMeta.WorktreeReminder)
	child.meta.UsageState = nil
	child.meta.ParentSessionID = parentMeta.SessionID
	if opts.InheritContinuation {
		child.meta.Continuation = cloneContinuationContext(parentMeta.Continuation)
	} else {
		child.meta.Continuation = nil
	}
	child.meta.UpdatedAt = time.Now().UTC()
	child.mu.Unlock()
	return nil
}

func cloneWorktreeReminderState(in *WorktreeReminderState) *WorktreeReminderState {
	if in == nil {
		return nil
	}
	copyState := *in
	return &copyState
}

func forkedWorktreeReminderState(in *WorktreeReminderState) *WorktreeReminderState {
	copyState := cloneWorktreeReminderState(in)
	if copyState == nil {
		return nil
	}
	copyState.HasIssuedInGeneration = false
	copyState.IssuedCompactionCount = 0
	return copyState
}

func reminderIssuedFromReplayEvents(events []ReplayEvent) bool {
	issued := false
	for _, evt := range events {
		switch evt.Kind {
		case "message":
			var msg reminderEventMessage
			if err := json.Unmarshal(evt.Payload, &msg); err != nil {
				continue
			}
			if isCompactionSoonReminderMessage(msg) {
				issued = true
			}
		case "history_replaced":
			var payload struct {
				Engine string `json:"engine"`
			}
			if err := json.Unmarshal(evt.Payload, &payload); err != nil {
				continue
			}
			issued = false
		}
	}
	return issued
}

func isCompactionSoonReminderMessage(msg reminderEventMessage) bool {
	return strings.TrimSpace(msg.Role) == "developer" && strings.TrimSpace(msg.MessageType) == "compaction_soon_reminder" && strings.TrimSpace(msg.Content) != ""
}

type reminderEventMessage struct {
	Role        string `json:"role"`
	MessageType string `json:"message_type"`
	Content     string `json:"content"`
}
