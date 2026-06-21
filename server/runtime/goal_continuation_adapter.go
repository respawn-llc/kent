package runtime

import (
	"context"
	"strings"

	"core/prompts"
	"core/server/llm"
	"core/server/session"
)

// goalContinuation is the self-declared completion adapter of the objective/continuation seam, the
// counterpart to the workflow output-completion adapter (evaluateWorkflowOutputCompletion). A goal
// completes out-of-band (kent goal complete, or the workflow soft cascade), never from a model
// final answer, so this adapter owns only the continuation side: detecting the active objective and
// rendering its reminder. The two adapters deliberately do NOT share a driver — the goal loop
// re-runs the exclusive step across runs while the workflow loop continues within one step — so the
// shared surface is the continuation nudge, centralized here instead of duplicated.
type goalContinuation struct {
	engine *Engine
}

func (e *Engine) goalContinuation() goalContinuation {
	return goalContinuation{engine: e}
}

// Evaluate implements CompletionAdapter: a goal completes out-of-band (kent goal complete, or the
// workflow soft cascade), never from a model final answer, so it ignores final and reports Done once
// the goal is no longer active. The completion side effect is nil — the status change already
// happened before the driver observes it. While active, the driver injects nudgeMessage and keeps
// going.
func (c goalContinuation) Evaluate(_ context.Context, _ llm.Message) (objectiveOutcome, error) {
	_, active := c.activeGoal()
	return objectiveOutcome{Applicable: true, Done: !active}, nil
}

// activeGoal returns the goal iff it is active — the only state that drives continuation. Paused,
// complete, and absent goals report false.
func (c goalContinuation) activeGoal() (session.GoalState, bool) {
	goal := c.engine.Goal()
	if goal == nil || goal.Status != session.GoalStatusActive {
		return session.GoalState{}, false
	}
	return *goal, true
}

// nudgeMessage is the full developer goal-continuation message injected before a goal-loop turn.
func (c goalContinuation) nudgeMessage() (llm.Message, bool) {
	goal, ok := c.activeGoal()
	if !ok {
		return llm.Message{}, false
	}
	return normalizeMessageForTranscript(llm.Message{
		Role:           llm.RoleDeveloper,
		MessageType:    llm.MessageTypeGoal,
		Content:        prompts.RenderGoalNudgePrompt(goal.Objective, string(goal.Status)),
		CompactContent: goalNudgeCompactText(goal),
	}, c.engine.transcriptWorkingDir()), true
}

// reminderText is the goal reminder appended to another objective's continuation nudge (the
// workflow invalid-completion nudge), keeping the foreground objective in view while the workflow
// loop drives.
func (c goalContinuation) reminderText() (string, bool) {
	goal, ok := c.activeGoal()
	if !ok {
		return "", false
	}
	return strings.TrimSpace(prompts.RenderGoalNudgePrompt(goal.Objective, string(goal.Status))), true
}
