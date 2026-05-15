package prompts

import (
	"bytes"
	"embed"
	_ "embed"
	"fmt"
	"strings"
	"text/template"

	"builder/cli/selfcmd"
)

type SystemPromptTemplateArgs struct {
	EstimatedToolCallsForContext int
	EditingToolName              string
}

type systemPromptTemplateData struct {
	BuilderRunCommand            string
	EstimatedToolCallsForContext int
	EditingToolName              string
	DefaultSystemPrompt          string
}

type WorkflowNodeContextArgs struct {
	TaskId          string
	TaskShortId     string
	TaskTitle       string
	TaskBody        string
	NodeId          string
	NodeKey         string
	NodeDisplayName string
	ContextMode     string
	SourceSessionID string
	CompletionMode  string
	OutputFields    []WorkflowOutputField
	Transitions     []WorkflowTransition
	InputValues     []WorkflowInputValue
	NodePrompt      string
}

type WorkflowOutputField struct {
	Name        string
	Description string
}

type WorkflowTransition struct {
	ID          string
	DisplayName string
	Description string
}

type WorkflowInputValue struct {
	Name  string
	Value string
}

//go:embed system_prompt.md
var SystemPrompt string

//go:embed tool_preambles_prompt.md
var ToolPreamblesPrompt string

//go:embed compaction_prompt.md
var CompactionPrompt string

//go:embed compaction_summary_prefix.md
var CompactionSummaryPrefix string

//go:embed compaction_soon_reminder.md
var CompactionSoonReminderPrompt string

//go:embed compaction_soon_reminder_trigger_handoff.md
var CompactionSoonReminderTriggerHandoffPrompt string

//go:embed goal/nudge.md
var GoalNudgePrompt string

//go:embed goal/set.md
var GoalSetPrompt string

//go:embed goal/pause.md
var GoalPausePrompt string

//go:embed goal/resume.md
var GoalResumePrompt string

//go:embed goal/clear.md
var GoalClearPrompt string

//go:embed goal/complete.md
var GoalCompletePrompt string

//go:embed goal/already_complete.md
var GoalAlreadyCompletePrompt string

//go:embed goal/agent_command_denied.md
var GoalAgentCommandDeniedPrompt string

//go:embed goal/complete_confirm_required.md
var GoalCompleteConfirmRequiredPrompt string

//go:embed review_prompt.md
var ReviewPrompt string

//go:embed init_prompt.md
var InitPrompt string

//go:embed reviewer_system_prompt.md
var ReviewerSystemPrompt string

//go:embed skills_prompt.md
var SkillsPrompt string

//go:embed skills/**
var GeneratedSkillsFS embed.FS

//go:embed headless_mode_prompt.md
var HeadlessModePrompt string

//go:embed headless_mode_exit_prompt.md
var HeadlessModeExitPrompt string

//go:embed workflow/tool_mode_prompt.md
var WorkflowToolModePrompt string

//go:embed workflow/structured_output_mode_prompt.md
var WorkflowStructuredOutputModePrompt string

//go:embed workflow/node_context.md
var WorkflowNodeContextPrompt string

//go:embed worktree_mode_prompt.md
var WorktreeModePrompt string

//go:embed worktree_mode_exit_prompt.md
var WorktreeModeExitPrompt string

func MainSystemPrompt(includeToolPreambles bool, args SystemPromptTemplateArgs) string {
	return WithToolPreambles(BaseSystemPrompt(args), includeToolPreambles)
}

func RenderCustomSystemPrompt(text string, includeToolPreambles bool, args SystemPromptTemplateArgs) (string, error) {
	rendered, err := renderSystemPromptTemplateErr(strings.TrimSpace(text), args, BaseSystemPrompt(args))
	if err != nil {
		return "", err
	}
	return WithToolPreambles(rendered, includeToolPreambles), nil
}

func WithToolPreambles(base string, includeToolPreambles bool) string {
	base = strings.TrimSpace(base)
	if !includeToolPreambles {
		return base
	}
	preambles := strings.TrimSpace(ToolPreamblesPrompt)
	if preambles == "" {
		return base
	}
	if base == "" {
		return preambles
	}
	return base + "\n\n" + preambles
}

func BaseSystemPrompt(args SystemPromptTemplateArgs) string {
	return renderSystemPromptTemplate(strings.TrimSpace(SystemPrompt), args, "")
}

func BuilderRunCommand() string {
	return selfcmd.RunCommandPrefix()
}

func RenderCompactionSoonReminderPrompt(triggerHandoffEnabled bool) string {
	if triggerHandoffEnabled {
		return strings.TrimSpace(CompactionSoonReminderTriggerHandoffPrompt)
	}
	return strings.TrimSpace(CompactionSoonReminderPrompt)
}

func RenderGoalNudgePrompt(objective, status string) string {
	return renderTemplatePlaceholders(GoalNudgePrompt, map[string]string{
		"{{objective}}": strings.TrimSpace(objective),
		"{{status}}":    strings.TrimSpace(status),
	})
}

func RenderGoalSetPrompt(objective string) string {
	return renderTemplatePlaceholders(GoalSetPrompt, map[string]string{
		"{{objective}}": strings.TrimSpace(objective),
	})
}

func RenderGoalResumePrompt(objective string) string {
	return renderTemplatePlaceholders(GoalResumePrompt, map[string]string{
		"{{objective}}": strings.TrimSpace(objective),
	})
}

func RenderGoalAlreadyCompletePrompt(objective string) string {
	return renderTemplatePlaceholders(GoalAlreadyCompletePrompt, map[string]string{
		"{{objective}}": strings.TrimSpace(objective),
	})
}

func RenderWorktreeModePrompt(branch, cwd, worktreePath, workspaceRoot string) string {
	return renderTemplatePlaceholders(WorktreeModePrompt, map[string]string{
		"{{branch}}":         strings.TrimSpace(branch),
		"{{cwd}}":            strings.TrimSpace(cwd),
		"{{worktree_path}}":  strings.TrimSpace(worktreePath),
		"{{workspace_root}}": strings.TrimSpace(workspaceRoot),
	})
}

func RenderWorktreeModeExitPrompt(branch, cwd, worktreePath, workspaceRoot string) string {
	return renderTemplatePlaceholders(WorktreeModeExitPrompt, map[string]string{
		"{{branch}}":         strings.TrimSpace(branch),
		"{{cwd}}":            strings.TrimSpace(cwd),
		"{{worktree_path}}":  strings.TrimSpace(worktreePath),
		"{{workspace_root}}": strings.TrimSpace(workspaceRoot),
	})
}

func RenderWorkflowNodeContextPrompt(args WorkflowNodeContextArgs) (string, error) {
	return renderNamedTemplate("workflow node context", WorkflowNodeContextPrompt, args)
}

func renderSystemPromptTemplate(text string, args SystemPromptTemplateArgs, defaultSystemPrompt string) string {
	rendered, err := renderSystemPromptTemplateErr(text, args, defaultSystemPrompt)
	if err != nil {
		panic(err)
	}
	return rendered
}

func renderSystemPromptTemplateErr(text string, args SystemPromptTemplateArgs, defaultSystemPrompt string) (string, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", nil
	}
	return renderNamedTemplate("system prompt", trimmed, systemPromptTemplateData{
		BuilderRunCommand:            selfcmd.RunCommandPrefix(),
		EstimatedToolCallsForContext: args.EstimatedToolCallsForContext,
		EditingToolName:              strings.TrimSpace(args.EditingToolName),
		DefaultSystemPrompt:          strings.TrimSpace(defaultSystemPrompt),
	})
}

func renderNamedTemplate(name string, text string, data any) (string, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", nil
	}
	tmpl, err := template.New(name).Option("missingkey=error").Parse(trimmed)
	if err != nil {
		return "", fmt.Errorf("parse %s template: %w", name, err)
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("render %s template: %w", name, err)
	}
	return out.String(), nil
}

func renderTemplatePlaceholders(template string, replacements map[string]string) string {
	text := strings.TrimSpace(template)
	if text == "" {
		return ""
	}
	for placeholder, value := range replacements {
		text = strings.ReplaceAll(text, placeholder, value)
	}
	return text
}
