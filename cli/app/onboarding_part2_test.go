package app

import (
	tuiinput "core/cli/tui/input"
	"core/server/auth"
	"core/server/llm"
	"core/shared/brand"
	"core/shared/config"
	"core/shared/theme"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestOnboardingDefaultsPathPreservesAutoWhenUsingDetectedDefault(t *testing.T) {
	newAppTestHome(t)
	model := newOnboardingModelForWorkspace(t.TempDir(), "", onboardingFlowState{settings: config.Settings{Theme: theme.Auto}, theme: theme.Auto})
	msg := model.finalizeCmd(true)()
	done, ok := msg.(onboardingFinalizeDoneMsg)
	if !ok {
		t.Fatalf("expected onboarding finalize message, got %T", msg)
	}
	if done.err != nil {
		t.Fatalf("finalize defaults path: %v", done.err)
	}
	contents, err := os.ReadFile(done.result.SettingsPath)
	if err != nil {
		t.Fatalf("read written settings: %v", err)
	}
	if !strings.Contains(string(contents), "theme = \"auto\"") {
		t.Fatalf("expected defaults path to preserve auto theme, got %q", string(contents))
	}
}

func TestOnboardingImportDiscoveryKeepsTypedInput(t *testing.T) {
	model := newOnboardingModelForWorkspace(t.TempDir(), "", onboardingFlowState{settings: config.Settings{Model: "gpt-5.5"}})
	steps := model.workflow.visibleSteps(&model.state)
	modelStepIndex := -1
	for index, step := range steps {
		if step.id == "model" {
			modelStepIndex = index
			break
		}
	}
	if modelStepIndex < 0 {
		t.Fatal("expected model input step to be visible")
	}
	model.stepIndex = modelStepIndex
	model.syncScreen(true)
	model.input.Replace(strings.NewReplacer("\r", "", "\n", "").Replace("draft-model-alias"))
	next, _ := model.Update(onboardingImportDiscoveryDoneMsg{discovery: onboardingImportDiscovery{skillSymlinkItems: map[onboardingImportProviderID][]onboardingSkillImportItem{}, commandSymlinkItems: map[onboardingImportProviderID][]onboardingCommandImportItem{}}})
	updated := next.(*onboardingModel)
	if updated.currentScreen.ID != "model" {
		t.Fatalf("expected to stay on model input screen, got %q", updated.currentScreen.ID)
	}
	if got := updated.input.Text(); got != "draft-model-alias" {
		t.Fatalf("expected import discovery refresh to preserve typed input, got %q", got)
	}
}

func TestOnboardingInputRendersReusableEditorFieldCursor(t *testing.T) {
	model := newOnboardingModelForWorkspace(t.TempDir(), "", onboardingFlowState{theme: "dark"})
	model.currentScreen = onboardingScreen{Kind: onboardingScreenInput, Title: "Enter value"}
	model.input = newSingleLineEditor("abc")
	model.input.SetCursor(byteOffsetForRuneCursor(model.input.Text(), 1))

	content := model.buildContent(24)
	expected := tuiinput.RenderSoftCursorLines(24, renderSingleLineEditor(24, 0, model.input, "> ", true, 0, ""), model.styles.inputText)
	if content.cursorRow < 0 || content.cursorRow+len(expected) > len(content.lines) {
		t.Fatalf("input cursor row %d outside content lines %#v", content.cursorRow, content.lines)
	}
	got := content.lines[content.cursorRow : content.cursorRow+len(expected)]
	if strings.Join(got, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("onboarding input did not render through reusable text input, got %#v want %#v", got, expected)
	}
}

func TestOnboardingInputCursorRowTracksWrappedReusableEditorField(t *testing.T) {
	model := newOnboardingModelForWorkspace(t.TempDir(), "", onboardingFlowState{theme: "dark"})
	model.currentScreen = onboardingScreen{Kind: onboardingScreenInput, Title: "Enter value"}
	model.input = newSingleLineEditor("alpha beta gamma")

	content := model.buildContent(8)
	rendered := renderSingleLineEditor(8, 0, model.input, "> ", true, 0, "")
	if !rendered.Cursor.Visible || rendered.Cursor.Row < 1 {
		t.Fatalf("expected wrapped input cursor below first row, cursor=%+v lines=%#v", rendered.Cursor, rendered.Lines)
	}
	if got, want := content.cursorRow, rendered.Cursor.Row; got != want {
		t.Fatalf("content cursor row = %d, want %d", got, want)
	}
}

func TestOnboardingInputUsesRealAltScreenCursorWhenAvailable(t *testing.T) {
	state := newUITerminalCursorState()
	model := newOnboardingModelForWorkspace(t.TempDir(), "", onboardingFlowState{theme: "dark"})
	model.terminalCursor = state
	model.width = 24
	model.height = 12
	model.currentScreen = onboardingScreen{Kind: onboardingScreenInput, Title: "Enter value"}
	model.input = newSingleLineEditor("alpha beta gamma")

	view := model.View()
	placement, ok := state.Snapshot()
	if !ok {
		t.Fatalf("expected real cursor placement for onboarding input, view=%q", view)
	}
	if !placement.AltScreen {
		t.Fatalf("expected alt-screen cursor placement, got %+v", placement)
	}
	if placement.CursorCol >= model.width {
		t.Fatalf("cursor col %d outside width %d", placement.CursorCol, model.width)
	}
	if strings.Contains(view, "\x1b[7") {
		t.Fatal("did not expect soft cursor when real terminal cursor is available")
	}
}

func TestOnboardingEditorFieldDeleteCurrentLineUsesAppKeyAdapter(t *testing.T) {
	cases := []struct {
		name string
		key  tea.KeyMsg
	}{
		{name: "ctrl-backspace-csi", key: tea.KeyMsg{Type: keyTypeCtrlBackspaceCSI}},
		{name: "super-backspace-csi", key: tea.KeyMsg{Type: keyTypeSuperBackspaceCSI}},
	}
	if runtime.GOOS == "darwin" {
		cases = append(cases, struct {
			name string
			key  tea.KeyMsg
		}{name: "darwin-ctrl-u", key: tea.KeyMsg{Type: tea.KeyCtrlU}})
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			model := newOnboardingModelForWorkspace(t.TempDir(), "", onboardingFlowState{theme: "dark"})
			model.currentScreen = onboardingScreen{Kind: onboardingScreenInput, Title: "Enter value"}
			model.input = newSingleLineEditor("project name")
			model.input.SetCursor(byteOffsetForRuneCursor(model.input.Text(), len([]rune("project"))))

			next, _ := model.Update(tt.key)
			updated := next.(*onboardingModel)
			if got := updated.input.Text(); got != "" {
				t.Fatalf("value after delete-current-line key = %q, want empty", got)
			}
			if got := runeOffsetForByteCursor(updated.input.Text(), updated.input.Cursor()); got != 0 {
				t.Fatalf("cursor after delete-current-line key = %d, want 0", got)
			}
		})
	}
}

func TestOnboardingSpinnerTickDoesNotRescheduleOutsideLoadingOrFinalize(t *testing.T) {
	model := newOnboardingModelForWorkspace(t.TempDir(), "", onboardingFlowState{theme: "dark"})
	model.state.imports.pending = false
	model.syncScreen(true)
	if model.currentScreen.Kind == onboardingScreenLoading {
		t.Fatalf("expected non-loading onboarding screen, got %q", model.currentScreen.Kind)
	}
	tickAt := model.spinnerClock.anchor.Add(spinnerTickInterval)
	next, cmd := model.Update(onboardingSpinnerTickMsg{at: tickAt})
	updated := next.(*onboardingModel)
	if updated.spinnerFrame == 0 {
		t.Fatal("expected spinner tick to advance frame even when stopping animation")
	}
	if cmd != nil {
		t.Fatalf("did not expect spinner tick to reschedule on %q screen", updated.currentScreen.Kind)
	}
}

func TestOnboardingSpinnerTickReschedulesWhileLoading(t *testing.T) {
	model := newOnboardingModelForWorkspace(t.TempDir(), "", onboardingFlowState{theme: "dark"})
	model.currentScreen = onboardingScreen{Kind: onboardingScreenLoading}
	tickAt := model.spinnerClock.anchor.Add(spinnerTickInterval)
	next, cmd := model.Update(onboardingSpinnerTickMsg{at: tickAt})
	updated := next.(*onboardingModel)
	if updated.spinnerFrame == 0 {
		t.Fatal("expected loading spinner tick to advance frame")
	}
	if cmd == nil {
		t.Fatal("expected loading spinner tick to reschedule")
	}
}

func TestOnboardingSpinnerTickReschedulesWhileFinalizing(t *testing.T) {
	model := newOnboardingModelForWorkspace(t.TempDir(), "", onboardingFlowState{theme: "dark"})
	model.state.imports.pending = false
	model.syncScreen(true)
	model.finalizing = true
	tickAt := model.spinnerClock.anchor.Add(spinnerTickInterval)
	next, cmd := model.Update(onboardingSpinnerTickMsg{at: tickAt})
	updated := next.(*onboardingModel)
	if updated.spinnerFrame == 0 {
		t.Fatal("expected finalizing spinner tick to advance frame")
	}
	if cmd == nil {
		t.Fatal("expected finalizing spinner tick to reschedule")
	}
}

func TestOnboardingCustomPathPreservesAutoWhenUsingDetectedDefault(t *testing.T) {
	newAppTestHome(t)
	workspace := t.TempDir()
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	model := newOnboardingModelForWorkspace(t.TempDir(), "", onboardingFlowState{
		settings:         cfg.Settings,
		baselineSettings: cfg.Settings,
		theme:            theme.Auto,
		skillImport:      onboardingImportSelection{Mode: onboardingImportModeNone},
		commandImport:    onboardingImportSelection{Mode: onboardingImportModeNone},
	})
	msg := model.finalizeCmd(false)()
	done, ok := msg.(onboardingFinalizeDoneMsg)
	if !ok {
		t.Fatalf("expected onboarding finalize message, got %T", msg)
	}
	if done.err != nil {
		t.Fatalf("finalize custom path: %v", done.err)
	}
	contents, err := os.ReadFile(done.result.SettingsPath)
	if err != nil {
		t.Fatalf("read written settings: %v", err)
	}
	if !strings.Contains(string(contents), "theme = \"auto\"") {
		t.Fatalf("expected custom path to preserve auto theme, got %q", string(contents))
	}
}

func TestOnboardingCustomPathPersistsExplicitReviewerOverrides(t *testing.T) {
	newAppTestHome(t)
	workspace := t.TempDir()
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	state := onboardingFlowState{
		settings:               cfg.Settings,
		baselineSettings:       cfg.Settings,
		theme:                  theme.Auto,
		skillImport:            onboardingImportSelection{Mode: onboardingImportModeNone},
		commandImport:          onboardingImportSelection{Mode: onboardingImportModeNone},
		reviewerCustomModel:    true,
		reviewerCustomThinking: true,
	}
	state.settings.Reviewer.Model = "gpt-5.4-mini"
	state.settings.Reviewer.ThinkingLevel = "high"
	model := newOnboardingModelForWorkspace(t.TempDir(), "", state)
	msg := model.finalizeCmd(false)()
	done, ok := msg.(onboardingFinalizeDoneMsg)
	if !ok {
		t.Fatalf("expected onboarding finalize message, got %T", msg)
	}
	if done.err != nil {
		t.Fatalf("finalize custom path: %v", done.err)
	}
	contents, err := os.ReadFile(done.result.SettingsPath)
	if err != nil {
		t.Fatalf("read written settings: %v", err)
	}
	if !strings.Contains(string(contents), "model = \"gpt-5.4-mini\"") {
		t.Fatalf("expected reviewer model override to be persisted, got %q", string(contents))
	}
	if !strings.Contains(string(contents), "thinking_level = \"high\"") {
		t.Fatalf("expected reviewer thinking override to be persisted, got %q", string(contents))
	}
}

func TestOnboardingCustomPathRollsBackImportsWhenSettingsWriteFails(t *testing.T) {
	home := newAppTestHome(t)
	globalRoot := t.TempDir()
	workspace := t.TempDir()
	cfg := loadAppTestConfig(t, workspace, config.LoadOptions{})
	sourceDir := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(filepath.Join(sourceDir, "demo-skill"), 0o755); err != nil {
		t.Fatalf("mkdir skill source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sourceDir, "demo-skill", "SKILL.md"), []byte("---\nname: demo\ndescription: demo\n---\n"), 0o644); err != nil {
		t.Fatalf("write skill source: %v", err)
	}
	configPath := filepath.Join(home, brand.ConfigDirName, "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	if err := os.WriteFile(configPath, []byte("model = \"existing\"\n"), 0o644); err != nil {
		t.Fatalf("write existing config: %v", err)
	}
	state := onboardingFlowState{
		settings: cfg.Settings,
		imports: onboardingImportDiscovery{skillSymlinkRoots: map[onboardingImportProviderID]string{
			onboardingImportProviderClaudeCode: sourceDir,
		}},
		skillImport:   onboardingImportSelection{Mode: onboardingImportModeSymlinkSource, Provider: onboardingImportProviderClaudeCode},
		commandImport: onboardingImportSelection{Mode: onboardingImportModeNone},
	}
	model := newOnboardingModelForWorkspace(globalRoot, "", state)
	msg := model.finalizeCmd(false)()
	done, ok := msg.(onboardingFinalizeDoneMsg)
	if !ok {
		t.Fatalf("expected onboarding finalize message, got %T", msg)
	}
	if done.err == nil {
		t.Fatal("expected settings write failure when config file already exists")
	}
	if _, err := os.Lstat(filepath.Join(globalRoot, "skills")); !os.IsNotExist(err) {
		t.Fatalf("expected symlinked skills root to be rolled back, got err=%v", err)
	}
}

func TestExecuteOnboardingImportsRollsBackSkillsWhenCommandImportFails(t *testing.T) {
	globalRoot := t.TempDir()
	home := newAppTestHome(t)
	skillSourceDir := filepath.Join(home, ".claude", "skills")
	if err := os.MkdirAll(filepath.Join(skillSourceDir, "demo-skill"), 0o755); err != nil {
		t.Fatalf("mkdir skill source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillSourceDir, "demo-skill", "SKILL.md"), []byte("---\nname: demo\ndescription: demo\n---\n"), 0o644); err != nil {
		t.Fatalf("write skill source: %v", err)
	}
	commandSourceDir := filepath.Join(home, ".claude", "commands")
	if err := os.MkdirAll(commandSourceDir, 0o755); err != nil {
		t.Fatalf("mkdir command source: %v", err)
	}
	if err := os.WriteFile(filepath.Join(commandSourceDir, "review.md"), []byte("review"), 0o644); err != nil {
		t.Fatalf("write command source: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(globalRoot, "prompts"), 0o755); err != nil {
		t.Fatalf("mkdir prompts target: %v", err)
	}
	if err := os.WriteFile(filepath.Join(globalRoot, "prompts", "review.md"), []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing prompt: %v", err)
	}
	_, err := executeOnboardingImports(globalRoot, onboardingFlowState{
		imports: onboardingImportDiscovery{
			skillSymlinkRoots: map[onboardingImportProviderID]string{
				onboardingImportProviderClaudeCode: skillSourceDir,
			},
			commandSymlinkRoots: map[onboardingImportProviderID]string{
				onboardingImportProviderClaudeCode: commandSourceDir,
			},
		},
		skillImport:   onboardingImportSelection{Mode: onboardingImportModeSymlinkSource, Provider: onboardingImportProviderClaudeCode},
		commandImport: onboardingImportSelection{Mode: onboardingImportModeSymlinkSource, Provider: onboardingImportProviderClaudeCode},
	})
	if err == nil {
		t.Fatal("expected command import failure")
	}
	if _, err := os.Lstat(filepath.Join(globalRoot, "skills")); !os.IsNotExist(err) {
		t.Fatalf("expected symlinked skills root to be rolled back after command import failure, got err=%v", err)
	}
}

func TestOnboardingProviderCapabilitiesFromAuthMode(t *testing.T) {
	oauthCaps, err := llm.ProviderCapabilitiesForSettings(auth.State{Method: auth.Method{Type: auth.MethodOAuth}}, config.Settings{})
	if err != nil {
		t.Fatalf("oauth provider capabilities: %v", err)
	}
	if oauthCaps.ProviderID != "chatgpt-codex" || !oauthCaps.SupportsResponsesCompact {
		t.Fatalf("unexpected oauth provider capabilities: %+v", oauthCaps)
	}
	apiCaps, err := llm.ProviderCapabilitiesForSettings(auth.State{Method: auth.Method{Type: auth.MethodAPIKey}}, config.Settings{})
	if err != nil {
		t.Fatalf("api key provider capabilities: %v", err)
	}
	if apiCaps.ProviderID != "openai" || !apiCaps.SupportsResponsesCompact {
		t.Fatalf("unexpected api key provider capabilities: %+v", apiCaps)
	}
	compatibleCaps, err := llm.ProviderCapabilitiesForSettings(auth.State{Method: auth.Method{Type: auth.MethodAPIKey}}, config.Settings{OpenAIBaseURL: "https://example.test/v1"})
	if err != nil {
		t.Fatalf("openai-compatible provider capabilities: %v", err)
	}
	if compatibleCaps.ProviderID != "openai-compatible" || compatibleCaps.SupportsResponsesCompact {
		t.Fatalf("unexpected openai-compatible provider capabilities: %+v", compatibleCaps)
	}
	defaultOpenAICaps, err := llm.ProviderCapabilitiesForSettings(auth.State{Method: auth.Method{Type: auth.MethodAPIKey}}, config.Settings{OpenAIBaseURL: "https://api.openai.com"})
	if err != nil {
		t.Fatalf("default openai base url provider capabilities: %v", err)
	}
	if defaultOpenAICaps.ProviderID != "openai" || !defaultOpenAICaps.SupportsResponsesCompact {
		t.Fatalf("expected explicit default OpenAI base url to preserve openai capabilities, got %+v", defaultOpenAICaps)
	}
	oauthCustomBaseCaps, err := llm.ProviderCapabilitiesForSettings(auth.State{Method: auth.Method{Type: auth.MethodOAuth}}, config.Settings{OpenAIBaseURL: "https://example.test/v1"})
	if err != nil {
		t.Fatalf("oauth custom base url capabilities: %v", err)
	}
	if oauthCustomBaseCaps.ProviderID != "chatgpt-codex" || !oauthCustomBaseCaps.SupportsResponsesCompact {
		t.Fatalf("expected oauth auth mode to keep chatgpt-codex capabilities over custom base url, got %+v", oauthCustomBaseCaps)
	}
	noAuthCompatibleCaps, err := llm.ProviderCapabilitiesForSettings(auth.EmptyState(), config.Settings{OpenAIBaseURL: "https://example.test/v1"})
	if err != nil {
		t.Fatalf("no-auth openai-compatible provider capabilities: %v", err)
	}
	if noAuthCompatibleCaps.ProviderID != "openai-compatible" || noAuthCompatibleCaps.SupportsResponsesCompact {
		t.Fatalf("unexpected no-auth openai-compatible provider capabilities: %+v", noAuthCompatibleCaps)
	}
	providerOverrideCaps, err := llm.ProviderCapabilitiesForSettings(auth.EmptyState(), config.Settings{ProviderOverride: "openai", OpenAIBaseURL: "https://example.test/v1"})
	if err != nil {
		t.Fatalf("provider override capabilities: %v", err)
	}
	if providerOverrideCaps.ProviderID != "openai" || !providerOverrideCaps.SupportsResponsesCompact {
		t.Fatalf("expected explicit provider override to win over base url, got %+v", providerOverrideCaps)
	}
}

func TestApplyOnboardingModelUpdatesKnownContextWindow(t *testing.T) {
	state := &onboardingFlowState{settings: config.Settings{Model: "gpt-5", ThinkingLevel: "medium", Reviewer: config.ReviewerSettings{Frequency: "edits"}}, baselineSettings: config.Settings{ModelContextWindow: 272_000, ContextCompactionThresholdTokens: 272_000 * 95 / 100}}
	if err := applyOnboardingModel(state, "gpt-5.5"); err != nil {
		t.Fatalf("apply onboarding model: %v", err)
	}
	if state.settings.ModelContextWindow != 272_000 {
		t.Fatalf("expected gpt-5.5 default context window, got %d", state.settings.ModelContextWindow)
	}
	if state.settings.ContextCompactionThresholdTokens != 272_000*95/100 {
		t.Fatalf("unexpected compaction threshold: %d", state.settings.ContextCompactionThresholdTokens)
	}
	if state.settings.Reviewer.Model != "gpt-5.5" {
		t.Fatalf("expected reviewer model to follow main model, got %q", state.settings.Reviewer.Model)
	}
	if state.settings.Reviewer.ThinkingLevel != "medium" {
		t.Fatalf("expected reviewer thinking to follow main thinking, got %q", state.settings.Reviewer.ThinkingLevel)
	}
}

func TestApplyOnboardingModelResetsUnknownModelContextWindowToBaseline(t *testing.T) {
	state := &onboardingFlowState{
		settings: config.Settings{
			Model:                            "gpt-5.3-codex",
			ModelContextWindow:               400_000,
			ContextCompactionThresholdTokens: 400_000 * 95 / 100,
			ThinkingLevel:                    "medium",
			Reviewer:                         config.ReviewerSettings{Frequency: "edits"},
		},
		baselineSettings: config.Settings{
			ModelContextWindow:               272_000,
			ContextCompactionThresholdTokens: 272_000 * 95 / 100,
		},
	}
	if err := applyOnboardingModel(state, "my-team-alias"); err != nil {
		t.Fatalf("apply onboarding model: %v", err)
	}
	if state.settings.ModelContextWindow != 272_000 {
		t.Fatalf("expected unknown model context window to reset to onboarding baseline, got %d", state.settings.ModelContextWindow)
	}
	if state.settings.ContextCompactionThresholdTokens != 272_000*95/100 {
		t.Fatalf("expected unknown model compaction threshold to reset to onboarding baseline, got %d", state.settings.ContextCompactionThresholdTokens)
	}
}

func TestReviewerModelStepDefaultsToMainModel(t *testing.T) {
	state := &onboardingFlowState{settings: config.Settings{Model: "gpt-5.5", Reviewer: config.ReviewerSettings{Frequency: "edits", Model: "gpt-5.5"}}}
	screen := findWorkflowStep(t, state, "reviewer_model").build(state)
	if screen.InputValue != "gpt-5.5" {
		t.Fatalf("expected reviewer model default to follow main model, got %q", screen.InputValue)
	}
}

func TestReviewerThinkingStepDefaultsToMainThinking(t *testing.T) {
	state := &onboardingFlowState{settings: config.Settings{Model: "gpt-5.5", ThinkingLevel: "high", Reviewer: config.ReviewerSettings{Frequency: "edits", Model: "gpt-5.5", ThinkingLevel: "high"}}}
	screen := findWorkflowStep(t, state, "reviewer_thinking").build(state)
	if screen.DefaultOptionID != "high" {
		t.Fatalf("expected reviewer thinking default to follow main thinking, got %q", screen.DefaultOptionID)
	}
}

func TestMainThinkingChoiceSynchronizesReviewerThinking(t *testing.T) {
	state := &onboardingFlowState{settings: config.Settings{Model: "gpt-5.5", ThinkingLevel: "medium", Reviewer: config.ReviewerSettings{Frequency: "edits", Model: "gpt-5.5", ThinkingLevel: "medium"}}}
	if err := findWorkflowStep(t, state, "thinking").apply(state, "high"); err != nil {
		t.Fatalf("apply thinking choice: %v", err)
	}
	if state.settings.Reviewer.ThinkingLevel != "high" {
		t.Fatalf("expected reviewer thinking to track updated main thinking, got %q", state.settings.Reviewer.ThinkingLevel)
	}
}

func TestMainThinkingChoicePreservesCustomReviewerThinking(t *testing.T) {
	state := &onboardingFlowState{
		settings:               config.Settings{Model: "gpt-5.5", ThinkingLevel: "medium", Reviewer: config.ReviewerSettings{Frequency: "edits", Model: "gpt-5.5", ThinkingLevel: "low"}},
		reviewerCustomThinking: true,
	}
	if err := findWorkflowStep(t, state, "thinking").apply(state, "high"); err != nil {
		t.Fatalf("apply thinking choice: %v", err)
	}
	if state.settings.Reviewer.ThinkingLevel != "low" {
		t.Fatalf("expected custom reviewer thinking to be preserved, got %q", state.settings.Reviewer.ThinkingLevel)
	}
}

func TestReviewerThinkingDisableDoesNotForceCustomInput(t *testing.T) {
	state := &onboardingFlowState{settings: config.Settings{Model: "gpt-5.5", ThinkingLevel: "high", Reviewer: config.ReviewerSettings{Frequency: "edits", Model: "gpt-5.5", ThinkingLevel: "high"}}}
	if err := findWorkflowStep(t, state, "reviewer_thinking").apply(state, "disable"); err != nil {
		t.Fatalf("apply reviewer disable choice: %v", err)
	}
	if state.settings.Reviewer.ThinkingLevel != "" {
		t.Fatalf("expected reviewer thinking to be disabled, got %q", state.settings.Reviewer.ThinkingLevel)
	}
	if state.reviewerCustomThinking {
		t.Fatal("expected disable choice not to force custom reviewer thinking input")
	}
	if workflowIncludesStep(newOnboardingWorkflow(state).visibleSteps(state), "reviewer_thinking_custom") {
		t.Fatal("expected custom reviewer thinking step to stay hidden after disable choice")
	}
}

func TestReviewerThinkingPresetChoiceDoesNotForceCustomInput(t *testing.T) {
	state := &onboardingFlowState{settings: config.Settings{Model: "gpt-5.5", ThinkingLevel: "medium", Reviewer: config.ReviewerSettings{Frequency: "edits", Model: "gpt-5.5", ThinkingLevel: "medium"}}}
	if err := findWorkflowStep(t, state, "reviewer_thinking").apply(state, "low"); err != nil {
		t.Fatalf("apply reviewer preset choice: %v", err)
	}
	if state.settings.Reviewer.ThinkingLevel != "low" {
		t.Fatalf("expected reviewer thinking preset to be preserved, got %q", state.settings.Reviewer.ThinkingLevel)
	}
	if !state.reviewerCustomThinking {
		t.Fatal("expected non-primary reviewer preset to remain an override")
	}
	if state.reviewerCustomThinkingInput {
		t.Fatal("expected preset reviewer thinking choice not to open custom input")
	}
	if workflowIncludesStep(newOnboardingWorkflow(state).visibleSteps(state), "reviewer_thinking_custom") {
		t.Fatal("expected custom reviewer thinking step to stay hidden after preset choice")
	}
}

func TestMainThinkingChoicePreservesDisabledReviewerThinking(t *testing.T) {
	state := &onboardingFlowState{settings: config.Settings{Model: "gpt-5.5", ThinkingLevel: "medium", Reviewer: config.ReviewerSettings{Frequency: "edits", Model: "gpt-5.5", ThinkingLevel: "medium"}}}
	if err := findWorkflowStep(t, state, "reviewer_thinking").apply(state, "disable"); err != nil {
		t.Fatalf("apply reviewer disable choice: %v", err)
	}
	if err := findWorkflowStep(t, state, "thinking").apply(state, "high"); err != nil {
		t.Fatalf("apply main thinking choice: %v", err)
	}
	if state.settings.Reviewer.ThinkingLevel != "" {
		t.Fatalf("expected reviewer thinking to remain disabled after main thinking change, got %q", state.settings.Reviewer.ThinkingLevel)
	}
	if !state.reviewerThinkingDisabled {
		t.Fatal("expected explicit reviewer disable choice to remain sticky")
	}
}

func TestApplyOnboardingModelPreservesCustomReviewerOverrides(t *testing.T) {
	state := &onboardingFlowState{
		settings: config.Settings{
			Model:                            "gpt-5.5",
			ThinkingLevel:                    "medium",
			ModelContextWindow:               272_000,
			ContextCompactionThresholdTokens: 272_000 * 95 / 100,
			Reviewer:                         config.ReviewerSettings{Frequency: "edits", Model: "gpt-4.1", ThinkingLevel: "low"},
		},
		baselineSettings:       config.Settings{ModelContextWindow: 272_000, ContextCompactionThresholdTokens: 272_000 * 95 / 100},
		reviewerCustomModel:    true,
		reviewerCustomThinking: true,
	}
	if err := applyOnboardingModel(state, "gpt-5.3-codex"); err != nil {
		t.Fatalf("apply onboarding model: %v", err)
	}
	if state.settings.Reviewer.Model != "gpt-4.1" {
		t.Fatalf("expected custom reviewer model to be preserved, got %q", state.settings.Reviewer.Model)
	}
	if state.settings.Reviewer.ThinkingLevel != "low" {
		t.Fatalf("expected custom reviewer thinking to be preserved, got %q", state.settings.Reviewer.ThinkingLevel)
	}
}

func findWorkflowStep(t *testing.T, state *onboardingFlowState, id string) onboardingStepDefinition {
	t.Helper()
	for _, step := range newOnboardingWorkflow(state).visibleSteps(state) {
		if step.id == id {
			return step
		}
	}
	t.Fatalf("expected workflow step %q", id)
	return onboardingStepDefinition{}
}

func workflowIncludesStep(steps []onboardingStepDefinition, id string) bool {
	for _, step := range steps {
		if step.id == id {
			return true
		}
	}
	return false
}
