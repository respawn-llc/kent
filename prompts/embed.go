package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"strings"
	"text/template"

	"core/cli/selfcmd"
)

type SystemPromptTemplateArgs struct {
	EstimatedToolCallsForContext int
	EditingToolName              string
}

type systemPromptRuntimeTemplateData struct {
	BuilderCommand               string
	EstimatedToolCallsForContext int
	EditingToolName              string
}

type defaultSystemPromptTemplateData struct {
	BuilderCommand                               string
	EstimatedToolCallsForContext                 int
	EditingToolName                              string
	DefaultSystemPromptPersonality               string
	DefaultSystemPromptHarnessWorkflowAutonomy   string
	DefaultSystemPromptAmbiguityAndOutputQuality string
	DefaultSystemPromptFinalAnswerAndFormatting  string
	DefaultSystemPromptDelegation                string
}

type systemPromptTemplateData struct {
	BuilderCommand                               string
	EstimatedToolCallsForContext                 int
	EditingToolName                              string
	DefaultSystemPrompt                          string
	DefaultSystemPromptPersonality               string
	DefaultSystemPromptHarnessWorkflowAutonomy   string
	DefaultSystemPromptAmbiguityAndOutputQuality string
	DefaultSystemPromptFinalAnswerAndFormatting  string
	DefaultSystemPromptDelegation                string
}

type WorkflowNodeContextArgs struct {
	TaskId          string
	TaskShortId     string
	TaskTitle       string
	TaskBody        string
	WorkflowId      string
	WorkflowShortId string
	NodeId          string
	NodeKey         string
	NodeDisplayName string
	ContextMode     string
	SourceSessionID string
	CompletionMode  string
	Transitions     []WorkflowTransition
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

//go:embed *.md system_prompt/*.md goal/*.md workflow/*.md questions/*.md
var promptFS embed.FS

func mustPrompt(path string) string {
	data, err := promptFS.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("read prompt %s: %v", path, err))
	}
	return string(data)
}

var (
	SystemPrompt                                   = mustPrompt("system_prompt/system_prompt.md")
	SystemPromptPersonality                        = mustPrompt("system_prompt/personality.md")
	SystemPromptHarness                            = mustPrompt("system_prompt/harness_workflow_autonomy.md")
	SystemPromptAmbiguityAndQuality                = mustPrompt("system_prompt/ambiguity_output_quality.md")
	SystemPromptFinalAnswerAndFormatting           = mustPrompt("system_prompt/final_answer_formatting.md")
	SystemPromptDelegation                         = mustPrompt("system_prompt/delegation.md")
	ToolPreamblesPrompt                            = mustPrompt("tool_preambles_prompt.md")
	CompactionPrompt                               = mustPrompt("compaction_prompt.md")
	CompactionSummaryPrefix                        = mustPrompt("compaction_summary_prefix.md")
	CompactionSoonReminderPrompt                   = mustPrompt("compaction_soon_reminder.md")
	CompactionSoonReminderTriggerHandoffPrompt     = mustPrompt("compaction_soon_reminder_trigger_handoff.md")
	GoalNudgePrompt                                = mustPrompt("goal/nudge.md")
	GoalSetPrompt                                  = mustPrompt("goal/set.md")
	GoalPausePrompt                                = mustPrompt("goal/pause.md")
	GoalResumePrompt                               = mustPrompt("goal/resume.md")
	GoalClearPrompt                                = mustPrompt("goal/clear.md")
	GoalCompletePrompt                             = mustPrompt("goal/complete.md")
	GoalAlreadyCompletePrompt                      = mustPrompt("goal/already_complete.md")
	GoalAgentCommandDeniedPrompt                   = mustPrompt("goal/agent_command_denied.md")
	GoalAgentDuplicateSetDeniedPrompt              = mustPrompt("goal/agent_duplicate_set_denied.md")
	GoalCompleteConfirmRequiredPrompt              = mustPrompt("goal/complete_confirm_required.md")
	ReviewPrompt                                   = mustPrompt("review_prompt.md")
	InitPrompt                                     = mustPrompt("init_prompt.md")
	ReviewerSystemPrompt                           = mustPrompt("reviewer_system_prompt.md")
	SkillsPrompt                                   = mustPrompt("skills_prompt.md")
	HeadlessModePrompt                             = mustPrompt("headless_mode_prompt.md")
	HeadlessModeExitPrompt                         = mustPrompt("headless_mode_exit_prompt.md")
	WorkflowTaskInstructionsPrompt                 = mustPrompt("workflow/workflow_task_instructions.md")
	WorkflowToolCompletionInstructionsPrompt       = mustPrompt("workflow/tool_completion_instructions.md")
	WorkflowStructuredCompletionInstructionsPrompt = mustPrompt("workflow/structured_completion_instructions.md")
	WorkflowFinalAnswerNudgePrompt                 = mustPrompt("workflow/final_answer_nudge.md")
	WorkflowHumanOnlyTaskActionDeniedPrompt        = mustPrompt("workflow/human_only_task_action_denied.md")
	WorktreeModePrompt                             = mustPrompt("worktree_mode_prompt.md")
	WorktreeModeExitPrompt                         = mustPrompt("worktree_mode_exit_prompt.md")
	QuestionsDisabledPrompt                        = mustPrompt("questions/disabled.md")
)

//go:embed skills/**
var GeneratedSkillsFS embed.FS

func MainSystemPrompt(includeToolPreambles bool, args SystemPromptTemplateArgs) string {
	return WithToolPreambles(BaseSystemPrompt(args), includeToolPreambles)
}

func RenderCustomSystemPrompt(text string, includeToolPreambles bool, args SystemPromptTemplateArgs) (string, error) {
	sections, err := renderSystemPromptSections(args)
	if err != nil {
		return "", err
	}
	defaultPrompt, err := renderDefaultSystemPromptTemplateWithSections(strings.TrimSpace(SystemPrompt), args, sections)
	if err != nil {
		return "", err
	}
	rendered, err := renderSystemPromptTemplateWithSections(strings.TrimSpace(text), args, defaultPrompt, sections)
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
	sections, err := renderSystemPromptSections(args)
	if err != nil {
		panic(err)
	}
	rendered, err := renderDefaultSystemPromptTemplateWithSections(strings.TrimSpace(SystemPrompt), args, sections)
	if err != nil {
		panic(err)
	}
	return rendered
}

func BuilderCommand() string {
	return selfcmd.BuilderCommand()
}

func RenderCompactionSoonReminderPrompt(triggerHandoffEnabled bool) string {
	if triggerHandoffEnabled {
		return strings.TrimSpace(CompactionSoonReminderTriggerHandoffPrompt)
	}
	return strings.TrimSpace(CompactionSoonReminderPrompt)
}

// goalPromptData is the template data shared by every goal prompt. Goal
// prompts render through the same text/template engine and command variable
// ({{.BuilderCommand}}) as the system prompt, so the launch command is wired
// in one place instead of a per-prompt placeholder.
type goalPromptData struct {
	BuilderCommand string
	Objective      string
	Status         string
}

func renderGoalPrompt(name, text, objective, status string) string {
	rendered, err := renderNamedTemplate(name, text, goalPromptData{
		BuilderCommand: BuilderCommand(),
		Objective:      strings.TrimSpace(objective),
		Status:         strings.TrimSpace(status),
	})
	if err != nil {
		panic(err)
	}
	return rendered
}

func RenderGoalNudgePrompt(objective, status string) string {
	return renderGoalPrompt("goal nudge", GoalNudgePrompt, objective, status)
}

func RenderGoalSetPrompt(objective string) string {
	return renderGoalPrompt("goal set", GoalSetPrompt, objective, "")
}

func RenderGoalAgentCommandDeniedPrompt() string {
	return renderGoalPrompt("goal agent command denied", GoalAgentCommandDeniedPrompt, "", "")
}

func RenderGoalResumePrompt(objective string) string {
	return renderGoalPrompt("goal resume", GoalResumePrompt, objective, "")
}

func RenderGoalAlreadyCompletePrompt(objective string) string {
	return renderGoalPrompt("goal already complete", GoalAlreadyCompletePrompt, objective, "")
}

func RenderGoalAgentDuplicateSetDeniedPrompt(objective, status string) string {
	return renderGoalPrompt("goal agent duplicate set denied", GoalAgentDuplicateSetDeniedPrompt, objective, status)
}

func RenderGoalCompleteConfirmRequiredPrompt(objective string) string {
	return renderGoalPrompt("goal complete confirm required", GoalCompleteConfirmRequiredPrompt, objective, "")
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

func RenderWorkflowTaskInstructions(args WorkflowNodeContextArgs, nodeCompletionInstructions string) (string, error) {
	type workflowTaskInstructionsTemplateData struct {
		WorkflowNodeContextArgs
		BuilderCommand             string
		NodeCompletionInstructions string
	}
	return renderNamedTemplate("workflow task instructions", WorkflowTaskInstructionsPrompt, workflowTaskInstructionsTemplateData{
		WorkflowNodeContextArgs:    args,
		BuilderCommand:             selfcmd.BuilderCommand(),
		NodeCompletionInstructions: strings.TrimSpace(nodeCompletionInstructions),
	})
}

func RenderWorkflowToolCompletionInstructions(workflowShortId string) (string, error) {
	return renderNamedTemplate("workflow tool completion instructions", WorkflowToolCompletionInstructionsPrompt, struct {
		BuilderCommand  string
		WorkflowShortId string
	}{
		BuilderCommand:  selfcmd.BuilderCommand(),
		WorkflowShortId: strings.TrimSpace(workflowShortId),
	})
}

func RenderWorkflowStructuredCompletionInstructions(workflowShortId string) (string, error) {
	return renderNamedTemplate("workflow structured completion instructions", WorkflowStructuredCompletionInstructionsPrompt, struct {
		BuilderCommand  string
		WorkflowShortId string
	}{
		BuilderCommand:  selfcmd.BuilderCommand(),
		WorkflowShortId: strings.TrimSpace(workflowShortId),
	})
}

func renderSystemPromptTemplate(text string, args SystemPromptTemplateArgs, defaultSystemPrompt string) string {
	rendered, err := renderSystemPromptTemplateErr(text, args, defaultSystemPrompt)
	if err != nil {
		panic(err)
	}
	return rendered
}

func renderSystemPromptTemplateErr(text string, args SystemPromptTemplateArgs, defaultSystemPrompt string) (string, error) {
	sections, err := renderSystemPromptSections(args)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(defaultSystemPrompt) == "" {
		return renderDefaultSystemPromptTemplateWithSections(text, args, sections)
	}
	return renderSystemPromptTemplateWithSections(text, args, defaultSystemPrompt, sections)
}

type systemPromptSections struct {
	personality              string
	harness                  string
	ambiguityAndQuality      string
	finalAnswerAndFormatting string
	delegation               string
}

func renderSystemPromptSections(args SystemPromptTemplateArgs) (systemPromptSections, error) {
	runtimeTemplateData := systemPromptRuntimeTemplateData{
		BuilderCommand:               selfcmd.BuilderCommand(),
		EstimatedToolCallsForContext: args.EstimatedToolCallsForContext,
		EditingToolName:              strings.TrimSpace(args.EditingToolName),
	}
	personality, err := renderNamedTemplate("system prompt personality", SystemPromptPersonality, runtimeTemplateData)
	if err != nil {
		return systemPromptSections{}, err
	}
	harness, err := renderNamedTemplate("system prompt harness", SystemPromptHarness, runtimeTemplateData)
	if err != nil {
		return systemPromptSections{}, err
	}
	ambiguityAndQuality, err := renderNamedTemplate("system prompt ambiguity and quality", SystemPromptAmbiguityAndQuality, runtimeTemplateData)
	if err != nil {
		return systemPromptSections{}, err
	}
	finalAnswerAndFormatting, err := renderNamedTemplate("system prompt final answer and formatting", SystemPromptFinalAnswerAndFormatting, runtimeTemplateData)
	if err != nil {
		return systemPromptSections{}, err
	}
	delegation, err := renderNamedTemplate("system prompt delegation", SystemPromptDelegation, runtimeTemplateData)
	if err != nil {
		return systemPromptSections{}, err
	}
	return systemPromptSections{
		personality:              personality,
		harness:                  harness,
		ambiguityAndQuality:      ambiguityAndQuality,
		finalAnswerAndFormatting: finalAnswerAndFormatting,
		delegation:               delegation,
	}, nil
}

func renderDefaultSystemPromptTemplateWithSections(text string, args SystemPromptTemplateArgs, sections systemPromptSections) (string, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", nil
	}
	return renderNamedTemplate("system prompt", trimmed, defaultSystemPromptTemplateData{
		BuilderCommand:                               selfcmd.BuilderCommand(),
		EstimatedToolCallsForContext:                 args.EstimatedToolCallsForContext,
		EditingToolName:                              strings.TrimSpace(args.EditingToolName),
		DefaultSystemPromptPersonality:               strings.TrimSpace(sections.personality),
		DefaultSystemPromptHarnessWorkflowAutonomy:   strings.TrimSpace(sections.harness),
		DefaultSystemPromptAmbiguityAndOutputQuality: strings.TrimSpace(sections.ambiguityAndQuality),
		DefaultSystemPromptFinalAnswerAndFormatting:  strings.TrimSpace(sections.finalAnswerAndFormatting),
		DefaultSystemPromptDelegation:                strings.TrimSpace(sections.delegation),
	})
}

func renderSystemPromptTemplateWithSections(text string, args SystemPromptTemplateArgs, defaultSystemPrompt string, sections systemPromptSections) (string, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", nil
	}
	return renderNamedTemplate("system prompt", trimmed, systemPromptTemplateData{
		BuilderCommand:                               selfcmd.BuilderCommand(),
		EstimatedToolCallsForContext:                 args.EstimatedToolCallsForContext,
		EditingToolName:                              strings.TrimSpace(args.EditingToolName),
		DefaultSystemPrompt:                          strings.TrimSpace(defaultSystemPrompt),
		DefaultSystemPromptPersonality:               strings.TrimSpace(sections.personality),
		DefaultSystemPromptHarnessWorkflowAutonomy:   strings.TrimSpace(sections.harness),
		DefaultSystemPromptAmbiguityAndOutputQuality: strings.TrimSpace(sections.ambiguityAndQuality),
		DefaultSystemPromptFinalAnswerAndFormatting:  strings.TrimSpace(sections.finalAnswerAndFormatting),
		DefaultSystemPromptDelegation:                strings.TrimSpace(sections.delegation),
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
