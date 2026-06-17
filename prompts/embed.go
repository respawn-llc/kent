package prompts

import (
	"bytes"
	"embed"
	"fmt"
	"reflect"
	"strings"
	"text/template"
	"text/template/parse"
)

// UnknownTemplatePlaceholderError reports that a custom prompt template
// references a placeholder identifier that is not available in the prompt's
// template data. It carries the offending placeholder name so callers and
// operators can identify which placeholder to remove without parsing error
// strings.
type UnknownTemplatePlaceholderError struct {
	// Template is the human-readable name of the template being rendered.
	Template string
	// Placeholder is the unknown top-level field identifier, e.g.
	// "DefaultSystemPrompt".
	Placeholder string
}

func (e *UnknownTemplatePlaceholderError) Error() string {
	return fmt.Sprintf("%s template references unknown placeholder %q", e.Template, e.Placeholder)
}

// validateTemplatePlaceholders parses text as a template named name and
// returns an *UnknownTemplatePlaceholderError if it references a top-level
// field that the data struct does not expose. It only inspects field
// references at the root scope (those not rebound by range/with), which is
// sufficient for the flat field-substitution system prompt templates.
func validateTemplatePlaceholders(name, text string, data any) error {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return nil
	}
	tmpl, err := template.New(name).Option("missingkey=error").Parse(trimmed)
	if err != nil {
		return fmt.Errorf("parse %s template: %w", name, err)
	}
	available := availableTemplateFields(data)
	if available == nil {
		return nil
	}
	var unknown string
	walkRootFieldIdents(tmpl.Root, func(ident string) bool {
		if _, ok := available[ident]; !ok {
			unknown = ident
			return false
		}
		return true
	})
	if unknown != "" {
		return &UnknownTemplatePlaceholderError{Template: name, Placeholder: unknown}
	}
	return nil
}

// availableTemplateFields returns the set of exported field names on data
// (which may be a struct or pointer to struct). It returns nil when data is
// not a struct, signalling that placeholder validation cannot be performed.
func availableTemplateFields(data any) map[string]struct{} {
	value := reflect.ValueOf(data)
	for value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return nil
	}
	fields := value.Type()
	available := make(map[string]struct{}, fields.NumField())
	for i := 0; i < fields.NumField(); i++ {
		field := fields.Field(i)
		if field.PkgPath != "" {
			continue // unexported
		}
		available[field.Name] = struct{}{}
	}
	return available
}

// walkRootFieldIdents invokes visit for every single-segment root-scope field
// reference ({{.Field}}) in the parse tree. It does not descend into range or
// with bodies, where "." is rebound to a different value. visit returns false
// to stop the walk early.
func walkRootFieldIdents(node parse.Node, visit func(ident string) bool) bool {
	if node == nil {
		return true
	}
	switch typed := node.(type) {
	case *parse.ListNode:
		if typed == nil {
			return true
		}
		for _, child := range typed.Nodes {
			if !walkRootFieldIdents(child, visit) {
				return false
			}
		}
	case *parse.ActionNode:
		return walkPipeFieldIdents(typed.Pipe, visit)
	case *parse.IfNode:
		if !walkPipeFieldIdents(typed.Pipe, visit) {
			return false
		}
		if !walkRootFieldIdents(typed.List, visit) {
			return false
		}
		return walkRootFieldIdents(typed.ElseList, visit)
	}
	return true
}

func walkPipeFieldIdents(pipe *parse.PipeNode, visit func(ident string) bool) bool {
	if pipe == nil {
		return true
	}
	for _, cmd := range pipe.Cmds {
		for _, arg := range cmd.Args {
			field, ok := arg.(*parse.FieldNode)
			if !ok {
				continue
			}
			if len(field.Ident) != 1 {
				continue
			}
			if !visit(field.Ident[0]) {
				return false
			}
		}
	}
	return true
}

type SystemPromptTemplateArgs struct {
	EstimatedToolCallsForContext int
	EditingToolName              string
}

type systemPromptRuntimeTemplateData struct {
	LaunchCommand                string
	EstimatedToolCallsForContext int
	EditingToolName              string
}

type defaultSystemPromptTemplateData struct {
	LaunchCommand                                string
	BuilderCommand                               string // deprecated alias of LaunchCommand; kept so migrated custom prompts render during the Builder->Kent window.
	EstimatedToolCallsForContext                 int
	EditingToolName                              string
	DefaultSystemPromptPersonality               string
	DefaultSystemPromptHarnessWorkflowAutonomy   string
	DefaultSystemPromptAmbiguityAndOutputQuality string
	DefaultSystemPromptFinalAnswerAndFormatting  string
	DefaultSystemPromptDelegation                string
}

type systemPromptTemplateData struct {
	LaunchCommand                                string
	BuilderCommand                               string // deprecated alias of LaunchCommand; kept so migrated custom prompts render during the Builder->Kent window.
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
	TaskId               string
	TaskShortId          string
	TaskTitle            string
	TaskBody             string
	WorkflowId           string
	WorkflowShortId      string
	NodeId               string
	NodeKey              string
	NodeDisplayName      string
	ContextMode          string
	SourceSessionID      string
	CompletionMode       string
	TaskNumberOfComments int64
	Transitions          []WorkflowTransition
	NodePrompt           string
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

type workflowTaskInstructionsTemplateData struct {
	WorkflowNodeContextArgs
	LaunchCommand              string
	NodeCompletionInstructions string
	ShowTaskCommentsReminder   bool
	TaskCommentsLabel          string
	TaskCommentListCommand     string
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

type compactionSoonReminderTemplateData struct {
	EstimatedToolCallsTillForcedHandoff int
}

func RenderCompactionSoonReminderPrompt(triggerHandoffEnabled bool, estimatedToolCallsTillForcedHandoff int) string {
	text := CompactionSoonReminderPrompt
	name := "compaction soon reminder"
	if triggerHandoffEnabled {
		text = CompactionSoonReminderTriggerHandoffPrompt
		name = "compaction soon reminder trigger handoff"
	}
	rendered, err := renderNamedTemplate(name, text, compactionSoonReminderTemplateData{
		EstimatedToolCallsTillForcedHandoff: estimatedToolCallsTillForcedHandoff,
	})
	if err != nil {
		panic(err)
	}
	return rendered
}

// goalPromptData is the template data shared by every goal prompt. Goal
// prompts render through the same text/template engine and command variable
// ({{.LaunchCommand}}) as the system prompt, so the launch command is wired
// in one place instead of a per-prompt placeholder.
type goalPromptData struct {
	LaunchCommand string
	Objective     string
	Status        string
}

func renderGoalPrompt(name, text, objective, status string) string {
	rendered, err := renderNamedTemplate(name, text, goalPromptData{
		LaunchCommand: LaunchCommand(),
		Objective:     strings.TrimSpace(objective),
		Status:        strings.TrimSpace(status),
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
	return renderNamedTemplate("workflow task instructions", WorkflowTaskInstructionsPrompt, newWorkflowTaskInstructionsTemplateData(args, nodeCompletionInstructions))
}

func newWorkflowTaskInstructionsTemplateData(args WorkflowNodeContextArgs, nodeCompletionInstructions string) workflowTaskInstructionsTemplateData {
	return workflowTaskInstructionsTemplateData{
		WorkflowNodeContextArgs:    args,
		LaunchCommand:              LaunchCommand(),
		NodeCompletionInstructions: strings.TrimSpace(nodeCompletionInstructions),
		ShowTaskCommentsReminder:   args.TaskNumberOfComments > 0,
		TaskCommentsLabel:          taskCommentsLabel(args.TaskNumberOfComments),
		TaskCommentListCommand:     strings.Join([]string{LaunchCommand(), "task", "comment", "list", strings.TrimSpace(args.TaskShortId)}, " "),
	}
}

func taskCommentsLabel(numberOfComments int64) string {
	if numberOfComments == 1 {
		return "1 comment"
	}
	return fmt.Sprintf("%d comments", numberOfComments)
}

func RenderWorkflowToolCompletionInstructions(workflowShortId string) (string, error) {
	return renderNamedTemplate("workflow tool completion instructions", WorkflowToolCompletionInstructionsPrompt, struct {
		LaunchCommand   string
		WorkflowShortId string
	}{
		LaunchCommand:   LaunchCommand(),
		WorkflowShortId: strings.TrimSpace(workflowShortId),
	})
}

func RenderWorkflowStructuredCompletionInstructions(workflowShortId string) (string, error) {
	return renderNamedTemplate("workflow structured completion instructions", WorkflowStructuredCompletionInstructionsPrompt, struct {
		LaunchCommand   string
		WorkflowShortId string
	}{
		LaunchCommand:   LaunchCommand(),
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
		LaunchCommand:                LaunchCommand(),
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
	data := defaultSystemPromptTemplateData{
		LaunchCommand:                                LaunchCommand(),
		BuilderCommand:                               LaunchCommand(),
		EstimatedToolCallsForContext:                 args.EstimatedToolCallsForContext,
		EditingToolName:                              strings.TrimSpace(args.EditingToolName),
		DefaultSystemPromptPersonality:               strings.TrimSpace(sections.personality),
		DefaultSystemPromptHarnessWorkflowAutonomy:   strings.TrimSpace(sections.harness),
		DefaultSystemPromptAmbiguityAndOutputQuality: strings.TrimSpace(sections.ambiguityAndQuality),
		DefaultSystemPromptFinalAnswerAndFormatting:  strings.TrimSpace(sections.finalAnswerAndFormatting),
		DefaultSystemPromptDelegation:                strings.TrimSpace(sections.delegation),
	}
	if err := validateTemplatePlaceholders("system prompt", trimmed, data); err != nil {
		return "", err
	}
	return renderNamedTemplate("system prompt", trimmed, data)
}

func renderSystemPromptTemplateWithSections(text string, args SystemPromptTemplateArgs, defaultSystemPrompt string, sections systemPromptSections) (string, error) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return "", nil
	}
	data := systemPromptTemplateData{
		LaunchCommand:                                LaunchCommand(),
		BuilderCommand:                               LaunchCommand(),
		EstimatedToolCallsForContext:                 args.EstimatedToolCallsForContext,
		EditingToolName:                              strings.TrimSpace(args.EditingToolName),
		DefaultSystemPrompt:                          strings.TrimSpace(defaultSystemPrompt),
		DefaultSystemPromptPersonality:               strings.TrimSpace(sections.personality),
		DefaultSystemPromptHarnessWorkflowAutonomy:   strings.TrimSpace(sections.harness),
		DefaultSystemPromptAmbiguityAndOutputQuality: strings.TrimSpace(sections.ambiguityAndQuality),
		DefaultSystemPromptFinalAnswerAndFormatting:  strings.TrimSpace(sections.finalAnswerAndFormatting),
		DefaultSystemPromptDelegation:                strings.TrimSpace(sections.delegation),
	}
	if err := validateTemplatePlaceholders("system prompt", trimmed, data); err != nil {
		return "", err
	}
	return renderNamedTemplate("system prompt", trimmed, data)
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
