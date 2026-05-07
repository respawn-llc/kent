package app

import (
	"fmt"
	"sort"
	"strings"

	"builder/server/auth"
	"builder/server/llm"
	"builder/shared/config"
	"builder/shared/theme"
	"builder/shared/toolspec"
)

type onboardingScreenKind string

const (
	onboardingScreenChoice  onboardingScreenKind = "choice"
	onboardingScreenInput   onboardingScreenKind = "input"
	onboardingScreenMulti   onboardingScreenKind = "multi"
	onboardingScreenLoading onboardingScreenKind = "loading"
)

type onboardingOption struct {
	ID          string
	Title       string
	Description string
	Warning     string
	Group       string
}

type onboardingScreen struct {
	ID              string
	Kind            onboardingScreenKind
	Title           string
	Body            string
	Helper          string
	ThemePreview    bool
	Options         []onboardingOption
	DefaultOptionID string
	InputValue      string
	Placeholder     string
	SensitiveInput  bool
	LoadingText     string
	LoadingDoneText string
	ErrorText       string
	ContinueLabel   string
	Selection       map[string]bool
}

type onboardingPendingAction string

const (
	onboardingPendingActionNone          onboardingPendingAction = ""
	onboardingPendingActionWriteDefaults onboardingPendingAction = "write_defaults"
	onboardingPendingActionWriteCustom   onboardingPendingAction = "write_custom"
	onboardingPendingActionRestart       onboardingPendingAction = "restart"
)

type onboardingImportMode string

const (
	onboardingImportModeNone          onboardingImportMode = "none"
	onboardingImportModeSymlinkSource onboardingImportMode = "symlink_source"
)

type onboardingImportSelection struct {
	Mode     onboardingImportMode
	Provider onboardingImportProviderID
}

type onboardingFlowState struct {
	settings                    config.Settings
	baselineSettings            config.Settings
	theme                       string
	authState                   auth.State
	providerCapabilities        llm.ProviderCapabilities
	pendingAction               onboardingPendingAction
	customThinking              bool
	reviewerCustomModel         bool
	reviewerCustomThinking      bool
	reviewerCustomThinkingInput bool
	reviewerThinkingDisabled    bool
	skillImport                 onboardingImportSelection
	commandImport               onboardingImportSelection
	skillSelection              map[string]bool
	imports                     onboardingImportDiscovery
}

type onboardingResult struct {
	Completed            bool
	CreatedDefaultConfig bool
	SettingsPath         string
}

type onboardingStepDefinition interface {
	ID() string
	Visible(*onboardingFlowState) bool
	Build(*onboardingFlowState) onboardingScreen
	ApplyChoice(*onboardingFlowState, string) error
	ApplyInput(*onboardingFlowState, string) error
	ApplyMultiSelect(*onboardingFlowState, map[string]bool) error
}

type onboardingChoiceStep struct {
	id      string
	visible func(*onboardingFlowState) bool
	build   func(*onboardingFlowState) onboardingScreen
	apply   func(*onboardingFlowState, string) error
}

func (s onboardingChoiceStep) ID() string { return s.id }
func (s onboardingChoiceStep) Visible(state *onboardingFlowState) bool {
	if s.visible == nil {
		return true
	}
	return s.visible(state)
}
func (s onboardingChoiceStep) Build(state *onboardingFlowState) onboardingScreen {
	return s.build(state)
}
func (s onboardingChoiceStep) ApplyChoice(state *onboardingFlowState, choiceID string) error {
	if s.apply == nil {
		return nil
	}
	return s.apply(state, choiceID)
}
func (onboardingChoiceStep) ApplyInput(*onboardingFlowState, string) error                { return nil }
func (onboardingChoiceStep) ApplyMultiSelect(*onboardingFlowState, map[string]bool) error { return nil }

type onboardingInputStep struct {
	id      string
	visible func(*onboardingFlowState) bool
	build   func(*onboardingFlowState) onboardingScreen
	apply   func(*onboardingFlowState, string) error
}

func (s onboardingInputStep) ID() string { return s.id }
func (s onboardingInputStep) Visible(state *onboardingFlowState) bool {
	if s.visible == nil {
		return true
	}
	return s.visible(state)
}
func (s onboardingInputStep) Build(state *onboardingFlowState) onboardingScreen {
	return s.build(state)
}
func (onboardingInputStep) ApplyChoice(*onboardingFlowState, string) error { return nil }
func (s onboardingInputStep) ApplyInput(state *onboardingFlowState, value string) error {
	if s.apply == nil {
		return nil
	}
	return s.apply(state, value)
}
func (onboardingInputStep) ApplyMultiSelect(*onboardingFlowState, map[string]bool) error { return nil }

type onboardingMultiSelectStep struct {
	id      string
	visible func(*onboardingFlowState) bool
	build   func(*onboardingFlowState) onboardingScreen
	apply   func(*onboardingFlowState, map[string]bool) error
}

func (s onboardingMultiSelectStep) ID() string { return s.id }
func (s onboardingMultiSelectStep) Visible(state *onboardingFlowState) bool {
	if s.visible == nil {
		return true
	}
	return s.visible(state)
}
func (s onboardingMultiSelectStep) Build(state *onboardingFlowState) onboardingScreen {
	return s.build(state)
}
func (onboardingMultiSelectStep) ApplyChoice(*onboardingFlowState, string) error { return nil }
func (onboardingMultiSelectStep) ApplyInput(*onboardingFlowState, string) error  { return nil }
func (s onboardingMultiSelectStep) ApplyMultiSelect(state *onboardingFlowState, value map[string]bool) error {
	if s.apply == nil {
		return nil
	}
	return s.apply(state, value)
}

type onboardingWorkflow struct {
	steps []onboardingStepDefinition
}

func (w onboardingWorkflow) visibleSteps(state *onboardingFlowState) []onboardingStepDefinition {
	visible := make([]onboardingStepDefinition, 0, len(w.steps))
	for _, step := range w.steps {
		if step.Visible(state) {
			visible = append(visible, step)
		}
	}
	return visible
}

func newOnboardingWorkflow(state *onboardingFlowState) onboardingWorkflow {
	return onboardingWorkflow{steps: []onboardingStepDefinition{
		onboardingChoiceStep{
			id: "theme",
			build: func(state *onboardingFlowState) onboardingScreen {
				defaultOption := theme.Resolve(state.settings.Theme)
				return onboardingScreen{
					ID:              "theme",
					Kind:            onboardingScreenChoice,
					Title:           "Choose a theme",
					Body:            "Pick the theme Builder should use. The preview updates as you move. If you keep the detected default, Builder stays on auto.",
					ThemePreview:    true,
					DefaultOptionID: defaultOption,
					Options: []onboardingOption{
						{ID: "dark", Title: "Dark"},
						{ID: "light", Title: "Light"},
					},
				}
			},
			apply: func(state *onboardingFlowState, choiceID string) error {
				applyOnboardingThemeChoice(state, choiceID)
				return nil
			},
		},
		onboardingChoiceStep{
			id: "entry",
			build: func(state *onboardingFlowState) onboardingScreen {
				return onboardingScreen{
					ID:              "entry",
					Kind:            onboardingScreenChoice,
					Title:           "First-time setup",
					Body:            "Do you want to run the first-time setup wizard now or start with defaults?",
					DefaultOptionID: "configure",
					Options:         []onboardingOption{{ID: "configure", Title: "Yes, configure Builder"}, {ID: "defaults", Title: "No, set up defaults for me"}},
				}
			},
			apply: func(state *onboardingFlowState, choiceID string) error {
				if choiceID == "defaults" {
					state.pendingAction = onboardingPendingActionWriteDefaults
				}
				return nil
			},
		},
		onboardingInputStep{
			id: "model",
			build: func(state *onboardingFlowState) onboardingScreen {
				return onboardingScreen{ID: "model", Kind: onboardingScreenInput, Title: "Choose a default model", Helper: "Press Enter to continue.", InputValue: state.settings.Model}
			},
			apply: func(state *onboardingFlowState, value string) error {
				return applyOnboardingModel(state, value)
			},
		},
		onboardingChoiceStep{
			id: "context_window",
			visible: func(state *onboardingFlowState) bool {
				return llm.SupportsLargeContextWindowModel(state.settings.Model)
			},
			build: func(state *onboardingFlowState) onboardingScreen {
				meta, _ := llm.LookupModelMetadata(state.settings.Model)
				body := fmt.Sprintf("%s supports larger context windows. The larger window costs about 50%% more. Quality degrades as the model gets closer to its limit. If automatic compaction is off, Builder can still go above the limit anyway, so the smaller default is recommended.", state.settings.Model)
				return onboardingScreen{ID: "context_window", Kind: onboardingScreenChoice, Title: "Choose a context window", Body: body, DefaultOptionID: "default", Options: []onboardingOption{{ID: "default", Title: fmt.Sprintf("Default window: %s", formatTokenWindow(meta.ContextWindowTokens))}, {ID: "large", Title: fmt.Sprintf("Higher window: %s", formatTokenWindow(meta.LargeContextWindowTokens))}}}
			},
			apply: func(state *onboardingFlowState, choiceID string) error {
				applyContextWindowChoice(state, choiceID)
				return nil
			},
		},
		onboardingChoiceStep{
			id:      "thinking",
			visible: func(state *onboardingFlowState) bool { return llm.SupportsReasoningEffortModel(state.settings.Model) },
			build: func(state *onboardingFlowState) onboardingScreen {
				levels := llm.SupportedThinkingLevelsModel(state.settings.Model)
				options := []onboardingOption{{ID: "disable", Title: "Disable", Description: thinkingLevelEstimate("disable")}}
				for _, level := range levels {
					options = append(options, onboardingOption{ID: level, Title: titleCaseThinking(level)})
				}
				options = append(options, onboardingOption{ID: "custom", Title: "Enter a custom value"})
				defaultOption := strings.TrimSpace(state.settings.ThinkingLevel)
				if defaultOption == "" {
					defaultOption = "disable"
				}
				if !containsOnboardingOption(options, defaultOption) {
					defaultOption = "custom"
				}
				return onboardingScreen{ID: "thinking", Kind: onboardingScreenChoice, Title: "Choose a thinking level", Body: "Higher thinking levels usually improve results, but they also cost more, use more context, and respond more slowly.", Options: options, DefaultOptionID: defaultOption}
			},
			apply: func(state *onboardingFlowState, choiceID string) error {
				switch choiceID {
				case "disable":
					state.customThinking = false
					state.settings.ThinkingLevel = ""
				case "custom":
					state.customThinking = true
				default:
					state.customThinking = false
					state.settings.ThinkingLevel = choiceID
				}
				syncReviewerThinkingToPrimary(state)
				return nil
			},
		},
		onboardingInputStep{
			id: "thinking_custom",
			visible: func(state *onboardingFlowState) bool {
				return state.customThinking || (strings.TrimSpace(state.settings.ThinkingLevel) != "" && !isKnownThinkingLevel(state.settings.ThinkingLevel))
			},
			build: func(state *onboardingFlowState) onboardingScreen {
				value := state.settings.ThinkingLevel
				if state.customThinking && isKnownThinkingLevel(value) {
					value = ""
				}
				return onboardingScreen{ID: "thinking_custom", Kind: onboardingScreenInput, Title: "Enter a custom thinking level", Helper: "Press Enter to continue.", InputValue: value}
			},
			apply: func(state *onboardingFlowState, value string) error {
				trimmed := strings.TrimSpace(value)
				if trimmed == "" {
					return fmt.Errorf("thinking value must not be empty")
				}
				state.customThinking = true
				state.settings.ThinkingLevel = trimmed
				syncReviewerThinkingToPrimary(state)
				return nil
			},
		},
		onboardingChoiceStep{
			id:      "verbosity",
			visible: func(state *onboardingFlowState) bool { return llm.SupportsVerbosityModel(state.settings.Model) },
			build: func(state *onboardingFlowState) onboardingScreen {
				levels := llm.SupportedVerbosityLevelsModel(state.settings.Model)
				options := make([]onboardingOption, 0, len(levels))
				for _, level := range levels {
					options = append(options, onboardingOption{ID: level, Title: titleCaseASCII(level)})
				}
				return onboardingScreen{ID: "verbosity", Kind: onboardingScreenChoice, Title: "Choose a verbosity level", Body: "Choose how verbose the model should be when it responds.", Options: options, DefaultOptionID: string(state.settings.ModelVerbosity)}
			},
			apply: func(state *onboardingFlowState, choiceID string) error {
				state.settings.ModelVerbosity = config.ModelVerbosity(choiceID)
				return nil
			},
		},
		onboardingChoiceStep{
			id: "ask_question",
			build: func(state *onboardingFlowState) onboardingScreen {
				defaultChoice := "no"
				if state.settings.EnabledTools[toolspec.ToolAskQuestion] {
					defaultChoice = "yes"
				}
				return onboardingScreen{ID: "ask_question", Kind: onboardingScreenChoice, Title: "Allow follow-up questions?", Body: "Allow Builder to ask follow-up questions when it needs clarification.", Options: []onboardingOption{{ID: "yes", Title: "Yes"}, {ID: "no", Title: "No"}}, DefaultOptionID: defaultChoice}
			},
			apply: func(state *onboardingFlowState, choiceID string) error {
				state.settings.EnabledTools[toolspec.ToolAskQuestion] = choiceID == "yes"
				return nil
			},
		},
		onboardingChoiceStep{
			id: "reviewer",
			build: func(state *onboardingFlowState) onboardingScreen {
				return onboardingScreen{ID: "reviewer", Kind: onboardingScreenChoice, Title: "Enable Supervisor?", Body: "Supervisor reviews the model's output independently, like an always-on code reviewer. It usually improves results, but it costs about 20% more and takes extra time. You can adjust the supervisor model and thinking level later in config.toml.", Options: []onboardingOption{{ID: "all", Title: "Yes, always"}, {ID: "edits", Title: "Yes, after edits"}, {ID: "off", Title: "No"}}, DefaultOptionID: state.settings.Reviewer.Frequency}
			},
			apply: func(state *onboardingFlowState, choiceID string) error {
				state.settings.Reviewer.Frequency = choiceID
				syncReviewerDefaultsFromPrimary(state)
				return nil
			},
		},
		onboardingInputStep{
			id: "reviewer_model",
			visible: func(state *onboardingFlowState) bool {
				return reviewerEnabled(state)
			},
			build: func(state *onboardingFlowState) onboardingScreen {
				return onboardingScreen{
					ID:         "reviewer_model",
					Kind:       onboardingScreenInput,
					Title:      "Choose a Supervisor model",
					Body:       "By default, Supervisor uses the same model you chose above. Enter a different model only if you want a separate reviewer pass.",
					Helper:     "Press Enter to continue.",
					InputValue: valueOrFallback(strings.TrimSpace(state.settings.Reviewer.Model), state.settings.Model),
				}
			},
			apply: func(state *onboardingFlowState, value string) error {
				trimmed := strings.TrimSpace(value)
				if trimmed == "" {
					return fmt.Errorf("supervisor model must not be empty")
				}
				state.settings.Reviewer.Model = trimmed
				state.reviewerCustomModel = trimmed != strings.TrimSpace(state.settings.Model)
				if !llm.SupportsReasoningEffortModel(trimmed) {
					state.reviewerCustomThinking = false
					state.settings.Reviewer.ThinkingLevel = ""
					return nil
				}
				syncReviewerThinkingToPrimary(state)
				return nil
			},
		},
		onboardingChoiceStep{
			id: "reviewer_thinking",
			visible: func(state *onboardingFlowState) bool {
				return reviewerEnabled(state) && llm.SupportsReasoningEffortModel(state.settings.Reviewer.Model)
			},
			build: func(state *onboardingFlowState) onboardingScreen {
				levels := llm.SupportedThinkingLevelsModel(state.settings.Reviewer.Model)
				options := []onboardingOption{{ID: "disable", Title: "Disable", Description: thinkingLevelEstimate("disable")}}
				for _, level := range levels {
					options = append(options, onboardingOption{ID: level, Title: titleCaseThinking(level)})
				}
				options = append(options, onboardingOption{ID: "custom", Title: "Enter a custom value"})
				defaultOption := strings.TrimSpace(state.settings.Reviewer.ThinkingLevel)
				if defaultOption == "" {
					defaultOption = "disable"
				}
				if !containsOnboardingOption(options, defaultOption) {
					defaultOption = "custom"
				}
				return onboardingScreen{ID: "reviewer_thinking", Kind: onboardingScreenChoice, Title: "Choose a Supervisor thinking level", Body: "By default, Supervisor uses the same thinking level as the main model. Higher thinking levels usually improve results, but they also cost more, use more context, and respond more slowly.", Options: options, DefaultOptionID: defaultOption}
			},
			apply: func(state *onboardingFlowState, choiceID string) error {
				switch choiceID {
				case "disable":
					state.reviewerThinkingDisabled = true
					state.settings.Reviewer.ThinkingLevel = ""
					state.reviewerCustomThinking = false
					state.reviewerCustomThinkingInput = false
				case "custom":
					state.reviewerThinkingDisabled = false
					state.reviewerCustomThinking = true
					state.reviewerCustomThinkingInput = true
				default:
					state.reviewerThinkingDisabled = false
					state.settings.Reviewer.ThinkingLevel = choiceID
					state.reviewerCustomThinking = choiceID != strings.TrimSpace(state.settings.ThinkingLevel)
					state.reviewerCustomThinkingInput = false
				}
				return nil
			},
		},
		onboardingInputStep{
			id: "reviewer_thinking_custom",
			visible: func(state *onboardingFlowState) bool {
				return reviewerEnabled(state) && llm.SupportsReasoningEffortModel(state.settings.Reviewer.Model) && (state.reviewerCustomThinkingInput || (strings.TrimSpace(state.settings.Reviewer.ThinkingLevel) != "" && !isKnownThinkingLevel(state.settings.Reviewer.ThinkingLevel)))
			},
			build: func(state *onboardingFlowState) onboardingScreen {
				value := state.settings.Reviewer.ThinkingLevel
				if state.reviewerCustomThinkingInput && isKnownThinkingLevel(value) {
					value = ""
				}
				return onboardingScreen{ID: "reviewer_thinking_custom", Kind: onboardingScreenInput, Title: "Enter a custom Supervisor thinking level", Helper: "Press Enter to continue.", InputValue: value}
			},
			apply: func(state *onboardingFlowState, value string) error {
				trimmed := strings.TrimSpace(value)
				if trimmed == "" {
					return fmt.Errorf("supervisor thinking value must not be empty")
				}
				state.reviewerThinkingDisabled = false
				state.settings.Reviewer.ThinkingLevel = trimmed
				state.reviewerCustomThinking = trimmed != strings.TrimSpace(state.settings.ThinkingLevel)
				state.reviewerCustomThinkingInput = !isKnownThinkingLevel(trimmed)
				return nil
			},
		},
		onboardingChoiceStep{
			id: "compaction",
			build: func(state *onboardingFlowState) onboardingScreen {
				options := []onboardingOption{{ID: string(config.CompactionModeLocal), Title: "Local", Description: "Builder's high-quality, slow, costlier, proprietary compaction algorithm."}}
				if state.providerCapabilities.SupportsResponsesCompact {
					options = append(options, onboardingOption{ID: string(config.CompactionModeNative), Title: "Native", Description: "Model provider compacts the context on their own with varying quality."})
				}
				options = append(options, onboardingOption{ID: string(config.CompactionModeNone), Title: "Manual compaction only", Description: "Model requests will fail if threshold is reached."})
				return onboardingScreen{ID: "compaction", Kind: onboardingScreenChoice, Title: "Choose a compaction mode", Body: "Builder can automatically summarize the conversation when the model reaches its context limit. You can always compact manually with /compact.", Options: options, DefaultOptionID: string(state.settings.CompactionMode)}
			},
			apply: func(state *onboardingFlowState, choiceID string) error {
				state.settings.CompactionMode = config.CompactionMode(choiceID)
				return nil
			},
		},
		onboardingChoiceStep{
			id: "skills_import",
			visible: func(state *onboardingFlowState) bool {
				return state.imports.pending || state.imports.err != nil || (!state.imports.skipSkills && state.imports.hasSkillCandidates())
			},
			build: func(state *onboardingFlowState) onboardingScreen { return buildSkillImportScreen(state) },
			apply: func(state *onboardingFlowState, choiceID string) error {
				return applyImportChoice(&state.skillImport, choiceID)
			},
		},
		onboardingMultiSelectStep{
			id:      "skills_enabled",
			visible: func(state *onboardingFlowState) bool { return len(skillSelectionCandidates(state)) > 0 },
			build:   func(state *onboardingFlowState) onboardingScreen { return buildSkillSelectionScreen(state) },
			apply: func(state *onboardingFlowState, selection map[string]bool) error {
				state.skillSelection = cloneSelection(selection)
				state.settings.SkillToggles = buildSkillToggles(state, selection)
				return nil
			},
		},
		onboardingChoiceStep{
			id: "commands_import",
			visible: func(state *onboardingFlowState) bool {
				return state.imports.pending || state.imports.err != nil || (!state.imports.skipCommands && state.imports.hasCommandCandidates())
			},
			build: func(state *onboardingFlowState) onboardingScreen {
				return buildCommandImportScreen(state)
			},
			apply: func(state *onboardingFlowState, choiceID string) error {
				return applyImportChoice(&state.commandImport, choiceID)
			},
		},
		onboardingChoiceStep{
			id: "review",
			build: func(state *onboardingFlowState) onboardingScreen {
				return onboardingScreen{ID: "review", Kind: onboardingScreenChoice, Title: "Review setup", Body: "Review your first-time setup choices.", Options: []onboardingOption{{ID: "finish", Title: "Finish setup"}, {ID: "restart", Title: "Start over"}}, DefaultOptionID: "finish"}
			},
			apply: func(state *onboardingFlowState, choiceID string) error {
				switch choiceID {
				case "finish":
					state.pendingAction = onboardingPendingActionWriteCustom
				case "restart":
					state.pendingAction = onboardingPendingActionRestart
				}
				return nil
			},
		},
	}}
}

func applyOnboardingModel(state *onboardingFlowState, value string) error {
	model := strings.TrimSpace(value)
	if model == "" {
		return fmt.Errorf("model must not be empty")
	}
	state.settings.Model = model
	llm.ApplyDerivedModelContextBudget(&state.settings, model, state.baselineSettings.ModelContextWindow, state.baselineSettings.ContextCompactionThresholdTokens)
	if !llm.SupportsVerbosityModel(model) {
		state.settings.ModelVerbosity = config.ModelVerbosity("")
	} else if strings.TrimSpace(string(state.settings.ModelVerbosity)) == "" {
		state.settings.ModelVerbosity = config.ModelVerbosityMedium
	}
	if !llm.SupportsReasoningEffortModel(model) {
		state.customThinking = false
		state.settings.ThinkingLevel = ""
	}
	applyContextWindowChoice(state, "default")
	syncReviewerDefaultsFromPrimary(state)
	return nil
}

func reviewerEnabled(state *onboardingFlowState) bool {
	mode := strings.TrimSpace(state.settings.Reviewer.Frequency)
	return mode != "" && mode != "off"
}

func syncReviewerDefaultsFromPrimary(state *onboardingFlowState) {
	if !state.reviewerCustomModel {
		state.settings.Reviewer.Model = state.settings.Model
	}
	syncReviewerThinkingToPrimary(state)
}

func syncReviewerThinkingToPrimary(state *onboardingFlowState) {
	if !llm.SupportsReasoningEffortModel(state.settings.Reviewer.Model) {
		state.reviewerCustomThinking = false
		state.reviewerCustomThinkingInput = false
		state.settings.Reviewer.ThinkingLevel = ""
		return
	}
	if state.reviewerThinkingDisabled {
		state.reviewerCustomThinking = false
		state.reviewerCustomThinkingInput = false
		state.settings.Reviewer.ThinkingLevel = ""
		return
	}
	if !state.reviewerCustomThinking {
		state.settings.Reviewer.ThinkingLevel = state.settings.ThinkingLevel
	}
}

func applyOnboardingThemeChoice(state *onboardingFlowState, choiceID string) {
	normalizedChoice := theme.Normalize(choiceID)
	if !theme.IsExplicit(state.settings.Theme) && normalizedChoice == theme.Resolve(theme.Auto) {
		state.settings.Theme = theme.Auto
		state.theme = theme.Auto
		return
	}
	state.settings.Theme = normalizedChoice
	state.theme = normalizedChoice
}

func applyContextWindowChoice(state *onboardingFlowState, choiceID string) {
	meta, ok := llm.LookupModelMetadata(state.settings.Model)
	if !ok || meta.ContextWindowTokens <= 0 {
		return
	}
	window := meta.ContextWindowTokens
	if choiceID == "large" && meta.LargeContextWindowTokens > 0 {
		window = meta.LargeContextWindowTokens
	}
	state.settings.ModelContextWindow = window
	state.settings.ContextCompactionThresholdTokens = window * 95 / 100
}

func reviewSummaryLines(state *onboardingFlowState) []string {
	lines := []string{
		"Review your first-time setup choices.",
		"",
		"- Theme: `" + onboardingThemeSummary(state.settings.Theme) + "`",
		"- Model: `" + state.settings.Model + "`",
	}
	if meta, ok := llm.LookupModelMetadata(state.settings.Model); ok && meta.ContextWindowTokens > 0 {
		if state.settings.ModelContextWindow == meta.ContextWindowTokens {
			lines = append(lines, "- Context window: `default ("+formatTokenWindow(meta.ContextWindowTokens)+")`")
		} else {
			lines = append(lines, "- Context window: `"+formatTokenWindow(state.settings.ModelContextWindow)+"`")
		}
	}
	thinking := strings.TrimSpace(state.settings.ThinkingLevel)
	if thinking == "" {
		thinking = "off"
	}
	lines = append(lines,
		"- Thinking: `"+thinking+"`",
		"- Verbosity: `"+valueOrFallback(string(state.settings.ModelVerbosity), "off")+"`",
		"- Questions: `"+onOff(state.settings.EnabledTools[toolspec.ToolAskQuestion])+"`",
		"- Supervisor: `"+valueOrFallback(state.settings.Reviewer.Frequency, "off")+"`",
		"- Compaction: `"+string(state.settings.CompactionMode)+"`",
	)
	if reviewerEnabled(state) {
		reviewerThinking := strings.TrimSpace(state.settings.Reviewer.ThinkingLevel)
		if reviewerThinking == "" {
			reviewerThinking = "off"
		}
		lines = append(lines,
			"- Supervisor model: `"+state.settings.Reviewer.Model+"`",
			"- Supervisor thinking: `"+reviewerThinking+"`",
		)
	}
	if summary := skillImportSummary(state); summary != "" {
		lines = append(lines, "- Skills import: `"+summary+"`")
	}
	if enabled, disabled := selectedSkillCounts(state); enabled > 0 || disabled > 0 {
		lines = append(lines, fmt.Sprintf("- Enabled skills: `%d enabled, %d disabled`", enabled, disabled))
	}
	if summary := commandImportSummary(state); summary != "" {
		lines = append(lines, "- Slash commands: `"+summary+"`")
	}
	return lines
}

func onboardingThemeSummary(value string) string {
	if theme.IsExplicit(value) {
		return theme.Resolve(value)
	}
	return theme.Auto + " (" + theme.Resolve(value) + ")"
}

func titleCaseASCII(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	runes := []rune(trimmed)
	runes[0] = []rune(strings.ToUpper(string(runes[0])))[0]
	return string(runes)
}

func containsOnboardingOption(options []onboardingOption, target string) bool {
	for _, option := range options {
		if option.ID == target {
			return true
		}
	}
	return false
}

func isKnownThinkingLevel(level string) bool {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "low", "medium", "high", "xhigh":
		return true
	default:
		return false
	}
}

func titleCaseThinking(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "xhigh":
		return "Extra high"
	case "":
		return ""
	default:
		return strings.Title(strings.ToLower(strings.TrimSpace(level)))
	}
}

func valueOrFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func formatTokenWindow(tokens int) string {
	if tokens >= 1_000_000 {
		return fmt.Sprintf("%dm", tokens/1_000_000)
	}
	if tokens >= 1_000 {
		return fmt.Sprintf("%dk", tokens/1_000)
	}
	return fmt.Sprintf("%d", tokens)
}

func cloneSelection(selection map[string]bool) map[string]bool {
	if len(selection) == 0 {
		return nil
	}
	cloned := make(map[string]bool, len(selection))
	for key, value := range selection {
		cloned[key] = value
	}
	return cloned
}

func buildSkillToggles(state *onboardingFlowState, selection map[string]bool) map[string]bool {
	if len(selection) == 0 {
		return nil
	}
	toggles := map[string]bool{}
	for _, item := range skillSelectionCandidates(state) {
		if selection[item.ID] {
			continue
		}
		toggles[item.SkillName] = false
	}
	if len(toggles) == 0 {
		return nil
	}
	return toggles
}

func selectedSkillCounts(state *onboardingFlowState) (int, int) {
	selected := effectiveSkillSelection(state)
	enabled := 0
	disabled := 0
	for _, item := range skillSelectionCandidates(state) {
		if selected[item.ID] {
			enabled++
		} else {
			disabled++
		}
	}
	return enabled, disabled
}

func effectiveSkillSelection(state *onboardingFlowState) map[string]bool {
	items := skillSelectionCandidates(state)
	selection := cloneSelection(state.skillSelection)
	if selection == nil {
		selection = make(map[string]bool, len(items))
	}
	for _, item := range items {
		if _, ok := selection[item.ID]; !ok {
			selection[item.ID] = true
		}
	}
	return selection
}

func sortedImportProviders[T any](byProvider map[onboardingImportProviderID][]T) []onboardingImportProviderID {
	providers := make([]onboardingImportProviderID, 0, len(byProvider))
	for provider := range byProvider {
		providers = append(providers, provider)
	}
	sort.Slice(providers, func(i, j int) bool {
		leftOrder := onboardingImportProviderOrder(providers[i])
		rightOrder := onboardingImportProviderOrder(providers[j])
		if leftOrder != rightOrder {
			return leftOrder < rightOrder
		}
		return providers[i] < providers[j]
	})
	return providers
}

func thinkingLevelEstimate(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "disable":
		return ""
	case "low":
		return "Lowest reasoning budget. Best for quick edits and straightforward tasks."
	case "medium":
		return "Balanced reasoning budget. Good default for most work."
	case "high":
		return "Heavier reasoning budget. Better for deeper planning and harder bugs."
	case "xhigh":
		return "Maximum reasoning budget. Slowest and costliest, for the hardest tasks."
	default:
		return ""
	}
}
